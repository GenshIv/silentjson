#include "textflag.h"

// parseJSONStringCompleteAVX2(src []byte, dst []byte) (written int64, consumed int64)
//
// Takes a JSON string starting just after the opening quote. Finds the closing
// quote using AVX2 scanning, decodes all escape sequences, and writes the
// complete decoded string to dst. Returns bytes written and bytes consumed
// (including closing quote).
//
// Uses AVX2 to process 32-byte blocks: plain runs are copied with vector stores,
// special characters ('"', '\') are detected via SIMD comparison and processed
// byte-by-byte only when needed.

TEXT ·parseJSONStringCompleteAVX2(SB), NOSPLIT, $0-64

    // Load arguments
    MOVQ src_base+0(FP), SI       // SI = source pointer
    MOVQ src_len+8(FP), BX        // BX = source length
    MOVQ dst_base+24(FP), DI      // DI = destination pointer

    // Phase 1: Find closing quote using AVX2 scanning with escape handling
    XORQ CX, CX                   // CX = scan position

scan_loop:
    CMPQ CX, BX
    JGE scan_eof

    MOVQ BX, R8
    SUBQ CX, R8                   // R8 = remaining bytes
    CMPQ R8, $32
    JL  scan_scalar

    // AVX2 block scan for '"' and '\'
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
    VPMOVMSKB Y4, DX              // slash mask

    ORL AX, DX
    TESTL AX, AX
    JZ  scan_block_done           // No specials in this block

    BSFL AX, R10                  // First special byte bit index
    MOVQ CX, R11
    ADDQ R10, R11                 // Absolute position of first special byte

    // Check if it's a backslash (escape) or quote
    TESTL DX, DX
    JZ  found_quote_simd          // Only quotes -> must be quote

    BSFL DX, R12
    CMPQ R10, R12
    JL  found_quote_simd          // Quote comes first

    // Backslash comes first - skip the escape sequence
found_slash_simd:
    MOVQ R11, CX                  // Move to backslash position
    INCQ CX                       // Skip backslash
    INCQ CX                       // Skip escaped character
    JMP scan_loop

found_quote_simd:
    MOVQ R11, R13                 // Position of closing quote
    JMP start_decode

scan_block_done:
    ADDQ $32, CX
    JMP scan_loop

scan_scalar:
    // Scalar scan for remaining bytes (< 32)
    MOVQ BX, R14
    SUBQ CX, R14                  // Count of remaining bytes

scan_scalar_loop:
    TESTQ R14, R14
    JZ  scan_eof
    MOVB (SI)(CX*1), AL
    CMPB AL, $0x5C                // Backslash? Skip next char
    JE  skip_escape_scalar
    CMPB AL, $0x22                // Quote?
    JE  found_quote_scalar
    INCQ CX
    DECQ R14
    JMP scan_scalar_loop

skip_escape_scalar:
    INCQ CX
    INCQ CX
    SUBQ $2, R14
    JBE scan_eof
    JMP scan_scalar_loop

found_quote_scalar:
    MOVQ CX, R13
    JMP start_decode

scan_eof:
    MOVQ $-1, written+48(FP)
    MOVQ $-1, consumed+56(FP)
    VZEROUPPER
    RET

start_decode:
    // Phase 2: Decode string using AVX2 for plain runs
    XORQ CX, CX                   // Read position
    XORQ DX, DX                   // Write position

decode_loop:
    CMPQ CX, R13
    JGE decode_done

    MOVQ R13, R8
    SUBQ CX, R8                   // Remaining bytes to decode
    CMPQ R8, $32
    JL  decode_scalar

    // AVX2 scan for specials in this block
    VMOVDQU (SI)(CX*1), Y0

    MOVQ $0x5c5c5c5c5c5c5c5c, R9
    MOVQ R9, X2
    VPBROADCASTQ X2, Y3
    VPCMPEQB Y3, Y0, Y4
    VPMOVMSKB Y4, AX              // slash mask

    TESTL AX, AX
    JNZ decode_has_escapes        // Block has escapes

    // Plain run - copy with vector store
    VMOVDQU Y0, (DI)(DX*1)
    ADDQ $32, DX
    ADDQ $32, CX
    JMP decode_loop

decode_has_escapes:
    // Process byte-by-byte until end of block or next special
    BSFL AX, R10                  // Position of first escape in block
    MOVQ CX, R11
    ADDQ R10, R11                 // Absolute position of first escape

    // Copy plain bytes before the escape
    CMPQ CX, R11
    JE  decode_at_escape          // No plain bytes to copy

    MOVQ R11, R8
    SUBQ CX, R8                   // Count of plain bytes
    VMOVDQU (SI)(CX*1), Y5
    VMOVDQU Y5, (DI)(DX*1)        // Copy with vector store (overwrites past escape, OK)
    ADDQ R8, DX
    MOVQ R11, CX

decode_at_escape:
    // Process the escape sequence
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
    MOVB AL, (DI)(DX*1)           // Unknown escape - emit as-is
    INCQ DX
    JMP decode_loop

decode_scalar:
    // Scalar decode for remaining bytes
    CMPQ CX, R13
    JGE decode_done
    MOVB (SI)(CX*1), AL
    INCQ CX
    CMPB AL, $0x5C
    JE  handle_escape_scalar
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP decode_scalar

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
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP decode_scalar

emit_newline:
    MOVB $0x0A, (DI)(DX*1)
    INCQ DX
    JMP decode_loop
emit_cr:
    MOVB $0x0D, (DI)(DX*1)
    INCQ DX
    JMP decode_loop
emit_tab:
    MOVB $0x09, (DI)(DX*1)
    INCQ DX
    JMP decode_loop
emit_bs:
    MOVB $0x08, (DI)(DX*1)
    INCQ DX
    JMP decode_loop
emit_ff:
    MOVB $0x0C, (DI)(DX*1)
    INCQ DX
    JMP decode_loop
emit_quote:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP decode_loop
emit_bs2:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP decode_loop
emit_slash:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP decode_loop

decode_done:
    MOVQ DX, written+48(FP)
    MOVQ R13, consumed+56(FP)
    INCQ consumed+56(FP)           // Include closing quote
    VZEROUPPER
    RET
