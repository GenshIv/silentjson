#include "textflag.h"

// parseJSONStringCompleteAVX2(src []byte, dst []byte) (written int64, consumed int64)
//
// Single-pass JSON string decoder using AVX2. Processes input bytes as they're
// encountered, handling escapes inline and stopping at the unescaped closing
// quote. Uses 32-byte vector operations for plain runs, byte-by-byte processing
// only when special characters are detected.

TEXT ·parseJSONStringCompleteAVX2(SB), NOSPLIT, $0-64

    // Load arguments
    MOVQ src_base+0(FP), SI       // SI = source pointer
    MOVQ src_len+8(FP), BX        // BX = source length
    MOVQ dst_base+24(FP), DI      // DI = destination pointer

    XORQ CX, CX                   // Read position (CX)
    XORQ DX, DX                   // Write position (DX)

main_loop:
    CMPQ CX, BX
    JGE main_eof

    MOVQ BX, R8
    SUBQ CX, R8                   // Remaining bytes
    CMPQ R8, $32
    JL  main_scalar_tail

    // AVX2 scan for '"' and '\' in this block
    VMOVDQU (SI)(CX*1), Y0

    MOVQ $0x2222222222222222, R9
    MOVQ R9, X1
    VPBROADCASTQ X1, Y1
    VPCMPEQB Y1, Y0, Y2
    VPMOVMSKB Y2, AX              // quote mask

    MOVQ $0x5c5c5c5c5c5c5c5c, R9
    MOVQ R9, X2
    VPBROADCASTQ X2, Y3
    VPCMPEQB Y3, Y0, Y4
    VPMOVMSKB Y4, DX              // slash mask (temporarily in DX)

    TESTL AX, AX
    JZ  main_no_quotes            // No quotes in this block

    // Quotes exist - find the first one
    BSFL AX, R10                  // R10 = bit index of first quote

main_no_quotes:
    TESTL DX, DX
    JZ  main_block_done           // No slashes either -> plain run

    // Slashes exist - find the first one
    BSFL DX, R12                  // R12 = bit index of first slash

    // Compare positions: quote vs slash
    TESTL AX, AX
    JZ  main_slash_first          // No quotes -> slash comes first

    CMPQ R10, R12
    JL  main_quote_first          // Quote bit is lower -> quote comes first

main_slash_first:
    MOVQ CX, R11
    ADDQ R12, R11                 // Absolute position of first backslash

    // Copy plain bytes before the escape with vector store
    CMPQ CX, R11
    JE  main_at_escape            // No plain bytes to copy

    MOVQ R11, R8
    SUBQ CX, R8                   // Count of plain bytes
    VMOVDQU (SI)(CX*1), Y5
    VMOVDQU Y5, (DI)(DX*1)        // Copy with vector store
    ADDQ R8, DX
    MOVQ R11, CX

main_at_escape:
    INCQ CX                       // Skip backslash
    MOVB (SI)(CX*1), AL
    INCQ CX

    CMPB AL, $0x6E                // 'n' -> newline
    JE  emit_newline
    CMPB AL, $0x72                // 'r' -> carriage return
    JE  emit_cr
    CMPB AL, $0x74                // 't' -> tab
    JE  emit_tab
    CMPB AL, $0x62                // 'b' -> backspace
    JE  emit_bs
    CMPB AL, $0x66                // 'f' -> form feed
    JE  emit_ff
    CMPB AL, $0x22                // '"' -> quote
    JE  emit_quote
    CMPB AL, $0x5C                // '\' -> backslash
    JE  emit_bs2
    CMPB AL, $0x2F                // '/' -> slash
    JE  emit_slash

    // Check for \uXXXX unicode escape
    CMPB AL, $0x75                // 'u'
    JNE  emit_unknown_escape      // Unknown escape - emit as-is

    // Verify we have at least 4 bytes remaining (for the hex digits)
    MOVQ BX, R8
    SUBQ CX, R8
    CMPQ R8, $4
    JL  parse_hex_invalid

    // Parse 4 hex digits into R14 (codepoint)
    XORQ R14, R14
    MOVQ $4, R15                  // digit counter

parse_hex_digits:
    MOVB (SI)(CX*1), AL
    INCQ CX

    CMPB AL, $0x39                // '9'
    JBE parse_digit_0_9           // 0-9

    CMPB AL, $0x46                // 'F'
    JA  check_lowercase           // Not A-F, check if lowercase

parse_digit_A_F:
    SUBB $0x37, AL                // Convert A-F to 10-15
    JMP parse_digit_done

check_lowercase:
    CMPB AL, $0x61                // 'a'
    JB  parse_hex_invalid         // Between F and a - invalid
    CMPB AL, $0x66                // 'f'
    JA  parse_hex_invalid         // Not a-f

parse_digit_a_f:
    SUBB $0x57, AL                // Convert a-f to 10-15
    JMP parse_digit_done

parse_digit_0_9:
    CMPB AL, $0x30                // '0'
    JB  parse_hex_invalid
    SUBB $0x30, AL                // Convert 0-9 to 0-9

parse_digit_done:
    SHLQ $4, R14                  // Shift codepoint left by 4 bits
    MOVBLZX AL, R10               // Zero-extend AL to 64-bit
    ORQ R10, R14                  // OR in the new digit
    DECQ R15
    JNZ parse_hex_digits

    // Check for surrogate pair (high surrogate: U+D800 to U+DBFF)
    CMPQ R14, $0xD800
    JB  emit_unicode_char         // Not a high surrogate
    CMPQ R14, $0xDBFF
    JA  emit_unicode_char         // Not in high surrogate range

    // High surrogate found - check for low surrogate
    MOVQ BX, R8
    SUBQ CX, R8
    CMPQ R8, $6                    // Need at least 6 bytes: \uXXXX
    JL  parse_hex_invalid          // Not enough for surrogate pair

    MOVB (SI)(CX*1), AL
    CMPB AL, $0x5C                 // '\'?
    JNE  parse_hex_invalid         // No backslash - invalid surrogate pair
    INCQ CX

    MOVB (SI)(CX*1), AL
    CMPB AL, $0x75                 // 'u'?
    JNE  parse_hex_invalid         // Not \u - invalid surrogate pair
    INCQ CX

    // Parse low surrogate hex digits into R10
    XORQ R10, R10
    MOVQ $4, R15

parse_low_surrogate_digits:
    MOVB (SI)(CX*1), AL
    INCQ CX

    CMPB AL, $0x39
    JBE parse_low_digit_0_9

    CMPB AL, $0x46
    JA  check_lowercase_low

parse_low_digit_A_F:
    SUBB $0x37, AL
    JMP parse_low_digit_done

check_lowercase_low:
    CMPB AL, $0x61
    JB  parse_hex_invalid
    CMPB AL, $0x66
    JA  parse_hex_invalid

parse_low_digit_a_f:
    SUBB $0x57, AL
    JMP parse_low_digit_done

parse_low_digit_0_9:
    CMPB AL, $0x30
    JB  parse_hex_invalid
    SUBB $0x30, AL

parse_low_digit_done:
    SHLQ $4, R10
    MOVBLZX AL, R11
    ORQ R11, R10
    DECQ R15
    JNZ parse_low_surrogate_digits

    // Validate low surrogate range (U+DC00 to U+DFFF)
    CMPQ R10, $0xDC00
    JB  parse_hex_invalid
    CMPQ R10, $0xDFFF
    JA  parse_hex_invalid

    // Combine surrogates: codepoint = 0x10000 + ((high-0xD800)<<10) + (low-0xDC00)
    MOVQ R14, R12
    SUBQ $0xD800, R12
    SHLQ $10, R12
    MOVQ R10, R13
    SUBQ $0xDC00, R13
    ADDQ R12, R13
    ADDQ $0x10000, R13
    MOVQ R13, R14

emit_unicode_char:
    // Convert codepoint to UTF-8 and emit
    CMPQ R14, $0x7F
    JBE emit_utf8_1byte           // U+0000 to U+007F

    CMPQ R14, $0x7FF
    JBE emit_utf8_2bytes          // U+0080 to U+07FF

    CMPQ R14, $0xFFFF
    JBE emit_utf8_3bytes          // U+0800 to U+FFFF

emit_utf8_4bytes:                 // U+10000 to U+10FFFF
    MOVQ R14, R10
    SHRQ $18, R10
    ANDQ $0x7, R10
    ORQ $0xF0, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX

    MOVQ R14, R10
    SHRQ $12, R10
    ANDQ $0x3F, R10
    ORQ $0x80, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX

    MOVQ R14, R10
    SHRQ $6, R10
    ANDQ $0x3F, R10
    ORQ $0x80, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX

    MOVQ R14, R10
    ANDQ $0x3F, R10
    ORQ $0x80, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX
    JMP main_loop

emit_utf8_3bytes:                 // U+0800 to U+FFFF
    MOVQ R14, R10
    SHRQ $12, R10
    ANDQ $0xF, R10
    ORQ $0xE0, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX

    MOVQ R14, R10
    SHRQ $6, R10
    ANDQ $0x3F, R10
    ORQ $0x80, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX

    MOVQ R14, R10
    ANDQ $0x3F, R10
    ORQ $0x80, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX
    JMP main_loop

emit_utf8_2bytes:                 // U+0080 to U+07FF
    MOVQ R14, R10
    SHRQ $6, R10
    ANDQ $0x1F, R10
    ORQ $0xC0, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX

    MOVQ R14, R10
    ANDQ $0x3F, R10
    ORQ $0x80, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX
    JMP main_loop

emit_utf8_1byte:                  // U+0000 to U+007F
    MOVQ R14, R10
    ANDQ $0x7F, R10
    MOVB R10, (DI)(DX*1)
    INCQ DX
    JMP main_loop

parse_hex_invalid:
    // Invalid hex digit - emit replacement character U+FFFD
    MOVQ $0xFFFD, R14
    JMP emit_utf8_3bytes

emit_unknown_escape:
    MOVB AL, (DI)(DX*1)           // Unknown escape - emit as-is
    INCQ DX
    JMP main_loop

emit_newline:
    MOVB $0x0A, (DI)(DX*1)
    INCQ DX
    JMP main_loop
emit_cr:
    MOVB $0x0D, (DI)(DX*1)
    INCQ DX
    JMP main_loop
emit_tab:
    MOVB $0x09, (DI)(DX*1)
    INCQ DX
    JMP main_loop
emit_bs:
    MOVB $0x08, (DI)(DX*1)
    INCQ DX
    JMP main_loop
emit_ff:
    MOVB $0x0C, (DI)(DX*1)
    INCQ DX
    JMP main_loop
emit_quote:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP main_loop
emit_bs2:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP main_loop
emit_slash:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP main_loop

main_quote_first:
    MOVQ CX, R11
    ADDQ R10, R11                 // Absolute position of first quote

    // Copy plain bytes before the quote with vector store
    CMPQ CX, R11
    JE  main_at_quote             // No plain bytes to copy

    MOVQ R11, R8
    SUBQ CX, R8                   // Count of plain bytes
    VMOVDQU (SI)(CX*1), Y5
    VMOVDQU Y5, (DI)(DX*1)        // Copy with vector store
    ADDQ R8, DX
    MOVQ R11, CX

main_at_quote:
    // Found the closing quote - done!
    MOVQ DX, written+48(FP)
    INCQ CX                       // Include the quote in consumed count
    MOVQ CX, consumed+56(FP)
    VZEROUPPER
    RET

main_block_done:
    // Plain run - copy entire 32-byte block with vector store
    VMOVDQU Y0, (DI)(DX*1)
    ADDQ $32, DX
    ADDQ $32, CX
    JMP main_loop

main_scalar_tail:
    // Scalar processing for remaining bytes (< 32)
    CMPQ CX, BX
    JGE main_eof
    MOVB (SI)(CX*1), AL
    INCQ CX
    CMPB AL, $0x5C
    JE  handle_escape_scalar
    CMPB AL, $0x22
    JE  found_quote_scalar
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP main_scalar_tail

handle_escape_scalar:
    MOVB (SI)(CX*1), AL
    INCQ CX
    CMPB AL, $0x6E
    JE  emit_newline
    CMPB AL, $0x72
    JE  emit_cr
    CMPB AL, $0x74
    JE  emit_tab
    CMPB AL, $0x62
    JE  emit_bs
    CMPB AL, $0x66
    JE  emit_ff
    CMPB AL, $0x22
    JE  emit_quote
    CMPB AL, $0x5C
    JE  emit_bs2
    CMPB AL, $0x2F
    JE  emit_slash

    // Check for \uXXXX unicode escape
    CMPB AL, $0x75                // 'u'
    JNE  emit_unknown_escape_scalar

    // Verify we have at least 4 bytes remaining (for the hex digits)
    MOVQ BX, R8
    SUBQ CX, R8
    CMPQ R8, $4
    JL  parse_hex_invalid_scalar

    // Parse 4 hex digits into R14 (codepoint)
    XORQ R14, R14
    MOVQ $4, R15                  // digit counter

parse_hex_digits_scalar:
    MOVB (SI)(CX*1), AL
    INCQ CX

    CMPB AL, $0x39                // '9'
    JBE parse_digit_0_9_scalar    // 0-9

    CMPB AL, $0x46                // 'F'
    JA  check_lowercase_scalar    // Not A-F, check if lowercase

parse_digit_A_F_scalar:
    SUBB $0x37, AL                // Convert A-F to 10-15
    JMP parse_digit_done_scalar

check_lowercase_scalar:
    CMPB AL, $0x61                // 'a'
    JB  parse_hex_invalid_scalar  // Between F and a - invalid
    CMPB AL, $0x66                // 'f'
    JA  parse_hex_invalid_scalar  // Not a-f

parse_digit_a_f_scalar:
    SUBB $0x57, AL                // Convert a-f to 10-15
    JMP parse_digit_done_scalar

parse_digit_0_9_scalar:
    CMPB AL, $0x30                // '0'
    JB  parse_hex_invalid_scalar
    SUBB $0x30, AL                // Convert 0-9 to 0-9

parse_digit_done_scalar:
    SHLQ $4, R14                  // Shift codepoint left by 4 bits
    MOVBLZX AL, R10               // Zero-extend AL to 64-bit
    ORQ R10, R14                  // OR in the new digit
    DECQ R15
    JNZ parse_hex_digits_scalar

    // Check for surrogate pair (high surrogate: U+D800 to U+DBFF)
    CMPQ R14, $0xD800
    JB  emit_unicode_char_scalar  // Not a high surrogate
    CMPQ R14, $0xDBFF
    JA  emit_unicode_char_scalar  // Not in high surrogate range

    // High surrogate found - check for low surrogate
    MOVQ BX, R8
    SUBQ CX, R8
    CMPQ R8, $6                    // Need at least 6 bytes: \uXXXX
    JL  parse_hex_invalid_scalar   // Not enough for surrogate pair

    MOVB (SI)(CX*1), AL
    CMPB AL, $0x5C                 // '\'?
    JNE  parse_hex_invalid_scalar  // No backslash - invalid surrogate pair
    INCQ CX

    MOVB (SI)(CX*1), AL
    CMPB AL, $0x75                 // 'u'?
    JNE  parse_hex_invalid_scalar  // Not \u - invalid surrogate pair
    INCQ CX

    // Parse low surrogate hex digits into R10
    XORQ R10, R10
    MOVQ $4, R15

parse_low_surrogate_digits_scalar:
    MOVB (SI)(CX*1), AL
    INCQ CX

    CMPB AL, $0x39
    JBE parse_low_digit_0_9_scalar

    CMPB AL, $0x46
    JA  check_lowercase_low_scalar

parse_low_digit_A_F_scalar:
    SUBB $0x37, AL
    JMP parse_low_digit_done_scalar

check_lowercase_low_scalar:
    CMPB AL, $0x61
    JB  parse_hex_invalid_scalar
    CMPB AL, $0x66
    JA  parse_hex_invalid_scalar

parse_low_digit_a_f_scalar:
    SUBB $0x57, AL
    JMP parse_low_digit_done_scalar

parse_low_digit_0_9_scalar:
    CMPB AL, $0x30
    JB  parse_hex_invalid_scalar
    SUBB $0x30, AL

parse_low_digit_done_scalar:
    SHLQ $4, R10
    MOVBLZX AL, R11
    ORQ R11, R10
    DECQ R15
    JNZ parse_low_surrogate_digits_scalar

    // Validate low surrogate range (U+DC00 to U+DFFF)
    CMPQ R10, $0xDC00
    JB  parse_hex_invalid_scalar
    CMPQ R10, $0xDFFF
    JA  parse_hex_invalid_scalar

    // Combine surrogates: codepoint = 0x10000 + ((high-0xD800)<<10) + (low-0xDC00)
    MOVQ R14, R12
    SUBQ $0xD800, R12
    SHLQ $10, R12
    MOVQ R10, R13
    SUBQ $0xDC00, R13
    ADDQ R12, R13
    ADDQ $0x10000, R13
    MOVQ R13, R14

emit_unicode_char_scalar:
    // Convert codepoint to UTF-8 and emit (reuse existing code)
    CMPQ R14, $0x7F
    JBE emit_utf8_1byte           // U+0000 to U+007F

    CMPQ R14, $0x7FF
    JBE emit_utf8_2bytes          // U+0080 to U+07FF

    CMPQ R14, $0xFFFF
    JBE emit_utf8_3bytes          // U+0800 to U+FFFF

    JMP emit_utf8_4bytes          // U+10000 to U+10FFFF

parse_hex_invalid_scalar:
    MOVQ $0xFFFD, R14
    JMP emit_utf8_3bytes

emit_unknown_escape_scalar:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP main_scalar_tail

found_quote_scalar:
    // Found the closing quote - done!
    MOVQ DX, written+48(FP)
    MOVQ CX, consumed+56(FP)
    VZEROUPPER
    RET

main_eof:
    MOVQ $-1, written+48(FP)
    MOVQ $-1, consumed+56(FP)
    VZEROUPPER
    RET
