
#include "textflag.h"

// Signature for function: func findQuoteAsm(data []byte) (ret int)
// data (s_ptr, s_len): +0(FP), +8(FP)
// Invisible space: +16(FP) (8 bytes)
// ret: +24(FP)
TEXT ·findQuoteAsm(SB), NOSPLIT, $0-32 // 32 bytes to cover all offsets

    // Read arguments from stack.
    MOVQ s_ptr+0(FP), AX     // AX = pointer to start of slice.
    MOVQ s_len+8(FP), CX      // CX = length of slice.

    // Use BX as index counter.
    XORQ BX, BX

LOOP:
    // Compare index (BX) with length (CX).
    CMPQ BX, CX
    JGE  NOT_FOUND

    // Compare byte at address [pointer + index].
    CMPB (AX)(BX*1), $'"'
    JE   FOUND

    // Continue loop.
    INCQ BX
    JMP  LOOP

FOUND:
    // Index in BX. Write it to return value slot.
    MOVQ BX, ret+24(FP)
    RET

NOT_FOUND:
    // Character not found. Write -1.
    MOVQ $-1, ret+24(FP)
    RET


// func scanJSONStringASM(src []byte) (end int, hasEscape bool)
TEXT ·scanJSONStringASM(SB), NOSPLIT, $0-33
    MOVQ src_base+0(FP), AX
    MOVQ src_len+8(FP), BX

    XORQ CX, CX // index
    XORQ R8, R8  // hasEscape
    XORQ R9, R9  // escapeNext

scan_loop:
    CMPQ CX, BX
    JGE scan_eof

    MOVB (AX)(CX*1), AL

    TESTQ R9, R9
    JZ scan_not_escaped

    XORQ R9, R9
    INCQ CX
    JMP scan_loop

scan_not_escaped:
    CMPB AL, $0x5C
    JEQ scan_escape
    CMPB AL, $0x22
    JEQ scan_quote

    INCQ CX
    JMP scan_loop

scan_escape:
    MOVQ $1, R8
    MOVQ $1, R9
    INCQ CX
    JMP scan_loop

scan_quote:
    MOVQ CX, ret+24(FP)
    MOVB R8, hasEscape+32(FP)
    RET

scan_eof:
    MOVQ $-1, ret+24(FP)
    MOVB $0, hasEscape+32(FP)
    RET


// func appendStringASM(buf []byte, s string) (newBuf []byte, specialPos int)
// AVX2: copies s into buf with surrounding quotes, scanning for '"' and '\\'.
// Returns (newBuf, -1)   if string is fully written.
// Returns (buf, -2)      if not enough space in buf.
TEXT ·appendStringASM(SB), NOSPLIT, $0-72
    MOVQ buf_base+0(FP), DI    // DI = buf.data
    MOVQ buf_len+8(FP), R11    // R11 = buf.len
    MOVQ buf_cap+16(FP), DX    // DX = buf.cap
    MOVQ s_base+24(FP), SI     // SI = s.data
    MOVQ s_len+32(FP), BX      // BX = s.len

    // Check capacity: need R11 + BX + 2 <= DX
    MOVQ R11, AX
    ADDQ BX, AX
    ADDQ $2, AX
    CMPQ AX, DX
    JG   asw_overflow

    // Move DI to current write position in buf, write opening quote
    ADDQ R11, DI
    MOVB $0x22, 0(DI)
    INCQ DI
    INCQ R11

    // Broadcast '"'  into Y1, '\\'  into Y2
    MOVQ $0x2222222222222222, R8
    MOVQ R8, X1
    VPBROADCASTQ X1, Y1

    MOVQ $0x5c5c5c5c5c5c5c5c, R8
    MOVQ R8, X2
    VPBROADCASTQ X2, Y2

    XORQ CX, CX                // CX = index into s

asw_avx2_loop:
    MOVQ BX, R9
    SUBQ CX, R9                // R9 = remaining bytes
    CMPQ R9, $32
    JL   asw_scalar_loop       // < 32 bytes remaining => scalar tail

    // Load 32 bytes from s[CX:]
    VMOVDQU (SI)(CX*1), Y0

    // Compare with '"'  and '\\'  
    VPCMPEQB Y1, Y0, Y3        // Y3: 0xFF where byte == '"'
    VPCMPEQB Y2, Y0, Y4        // Y4: 0xFF where byte == '\\'
    VPOR Y3, Y4, Y5            // Y5: combined special-char mask

    VPMOVMSKB Y5, R13          // R13 = 32-bit bitmask
    TESTL R13, R13
    JNZ  asw_avx2_special      // found a special char in this chunk

    // No special chars -- bulk copy 32 bytes
    VMOVDQU Y0, 0(DI)
    ADDQ $32, DI
    ADDQ $32, CX
    ADDQ $32, R11
    JMP  asw_avx2_loop

asw_avx2_special:
    // Find index of first set bit (first special char in the 32-byte chunk)
    BSFL R13, R13              // R13 = bit offset

    // Copy s[CX .. CX+R13) into buf byte-by-byte
    TESTL R13, R13
    JZ   asw_escape_special    // nothing to copy before the special char

asw_avx2_copy_prefix:
    MOVB (SI)(CX*1), AL
    MOVB AL, 0(DI)
    INCQ DI
    INCQ CX
    INCQ R11
    DECL R13
    JNZ  asw_avx2_copy_prefix

asw_escape_special:
    // Check capacity for the rest of string + closing quote + the '\\' we add.
    // Required: (BX - CX) + 2 <= DX - R11
    MOVQ BX, AX
    SUBQ CX, AX
    ADDQ $2, AX
    MOVQ DX, R14
    SUBQ R11, R14
    CMPQ AX, R14
    JG   asw_overflow

    MOVB $0x5C, 0(DI)          // '\\'
    INCQ DI
    MOVB (SI)(CX*1), AL
    MOVB AL, 0(DI)             // the escaped char
    INCQ DI
    INCQ CX
    ADDQ $2, R11
    JMP  asw_avx2_loop         // loop back to process remaining bytes

asw_scalar_loop:
    CMPQ CX, BX
    JGE  asw_clean_done

    MOVB (SI)(CX*1), AL
    CMPB AL, $0x22
    JEQ  asw_scalar_special
    CMPB AL, $0x5C
    JEQ  asw_scalar_special

    MOVB AL, 0(DI)
    INCQ DI
    INCQ CX
    INCQ R11
    JMP  asw_scalar_loop

asw_scalar_special:
    // Capacity check
    MOVQ BX, AX
    SUBQ CX, AX
    ADDQ $2, AX
    MOVQ DX, R14
    SUBQ R11, R14
    CMPQ AX, R14
    JG   asw_overflow

    MOVB $0x5C, 0(DI)
    INCQ DI
    MOVB (SI)(CX*1), AL        // <--- FIXED: Reload AL after AX was overwritten
    MOVB AL, 0(DI)
    INCQ DI
    INCQ CX
    ADDQ $2, R11
    JMP  asw_scalar_loop

asw_clean_done:
    // Write closing quote
    MOVB $0x22, 0(DI)
    INCQ R11

    MOVQ buf_base+0(FP), AX
    MOVQ AX, ret_base+40(FP)
    MOVQ R11, ret_len+48(FP)
    MOVQ DX, ret_cap+56(FP)
    MOVQ $-1, ret1+64(FP)
    RET

asw_overflow:
    MOVQ buf_base+0(FP), AX
    MOVQ AX, ret_base+40(FP)
    MOVQ buf_len+8(FP), AX
    MOVQ AX, ret_len+48(FP)
    MOVQ DX, ret_cap+56(FP)
    MOVQ $-2, ret1+64(FP)
    RET

TEXT ·parseShortStringASM2(SB), NOSPLIT, $0-40
    MOVQ src_base+0(FP), SI
    MOVQ src_len+8(FP), BX

    XORQ CX, CX // readIdx
    XORQ DX, DX // writeIdx

    MOVQ $0x2222222222222222, R8
    MOVQ R8, X1
    VPBROADCASTQ X1, Y1
    MOVQ $0x5c5c5c5c5c5c5c5c, R8
    MOVQ R8, X2
    VPBROADCASTQ X2, Y2

pss_avx_loop:
    MOVQ BX, AX
    SUBQ CX, AX
    CMPQ AX, $32
    JL loop

    VMOVDQU (SI)(CX*1), Y0
    VPCMPEQB Y1, Y0, Y3
    VPCMPEQB Y2, Y0, Y4
    VPOR Y3, Y4, Y5
    VPMOVMSKB Y5, R8
    TESTL R8, R8
    JNZ loop

    CMPQ CX, DX
    JEQ pss_skip_copy
    VMOVDQU Y0, (SI)(DX*1)
pss_skip_copy:
    ADDQ $32, CX
    ADDQ $32, DX
    JMP pss_avx_loop

loop:
    CMPQ CX, BX                // Check: read everything?
    JGE eof

    MOVB (SI)(CX*1), AL        // Read byte src[readIdx]
    INCQ CX                    // readIdx++

    CMPB AL, $0x22             // Quote? (end of string)
    JEQ done

    CMPB AL, $0x5C             // Slash? (escaping)
    JEQ escape

    MOVB AL, (SI)(DX*1)        // Normal byte: write to src[writeIdx]
    INCQ DX                    // writeIdx++
    JMP loop

escape:
    MOVB (SI)(CX*1), AL        // Read byte after '\'
    INCQ CX

    // Simple mapping of special characters
    CMPB AL, $0x6E       // 'n' -> 0x0A
    JEQ  is_n
    CMPB AL, $0x72       // 'r' -> 0x0D
    JEQ  is_r
    CMPB AL, $0x74       // 't' -> 0x09
    JEQ  is_t
    CMPB AL, $0x62       // 'b' -> 0x08
    JEQ  is_b
    CMPB AL, $0x66       // 'f' -> 0x0C
    JEQ  is_f
    // If character not in list (e.g., \" or \\ or \/),
    // it simply writes as is (AL already contains the required byte)
    JMP  write

is_n: MOVB $0x0A, AL; JMP write
is_r: MOVB $0x0D, AL; JMP write
is_t: MOVB $0x09, AL; JMP write
is_b: MOVB $0x08, AL; JMP write
is_f: MOVB $0x0C, AL; JMP write

check_r:
    CMPB AL, $0x72             // 'r' -> 0x0D
    JNE write
    MOVB $0x0D, AL

write:
    MOVB AL, (SI)(DX*1)        // Write unescaped byte to src[writeIdx]
    INCQ DX
    JMP loop

done:
    MOVQ DX, ret+24(FP)
    MOVQ CX, ret+32(FP)
    RET

eof:
    MOVQ $-1, ret+24(FP)
    MOVQ $-1, ret+32(FP)
    RET


// func parseShortStringASM(src []byte) ([]byte, int64)
TEXT ·parseShortStringASM(SB), $24-56
    MOVQ src_base+0(FP), R12
    MOVQ src_len+8(FP), BX

    XORQ CX, CX // readIdx
    XORQ R8, R8  // hadEscape
    MOVQ $0x2222222222222222, R9
    MOVQ $0x5c5c5c5c5c5c5c5c, R10
    MOVQ $0x0101010101010101, R11
    MOVQ $0x8080808080808080, R13

scan_loop:
    CMPQ CX, BX
    JGE scan_eof

    MOVQ BX, R14
    SUBQ CX, R14
    CMPQ R14, $8
    JL scan_tail

    MOVQ (R12)(CX*1), AX

    MOVQ AX, R14
    XORQ R9, R14
    MOVQ R14, R15
    SUBQ R11, R14
    NOTQ R15
    ANDQ R14, R15
    ANDQ R13, R15

    MOVQ AX, R14
    XORQ R10, R14
    MOVQ R14, SI
    SUBQ R11, R14
    NOTQ SI
    ANDQ R14, SI
    ANDQ R13, SI

    ORQ SI, R15
    TESTQ R15, R15
    JNZ scan_slow

    ADDQ $8, CX
    JMP scan_loop

scan_slow:
    MOVQ $8, R14

scan_slow_loop:
    CMPQ R14, $0
    JE scan_loop
    CMPQ CX, BX
    JGE scan_eof

    MOVB (R12)(CX*1), AL
    INCQ CX
    DECQ R14

    CMPB AL, $0x22
    JEQ scan_done
    CMPB AL, $0x5C
    JEQ scan_escape

    JMP scan_slow_loop

scan_escape:
    MOVQ $1, R8
    CMPQ CX, BX
    JGE scan_eof
    INCQ CX
    JMP scan_slow_loop

scan_tail:
scan_tail_loop:
    CMPQ CX, BX
    JGE scan_eof

    MOVB (R12)(CX*1), AL
    INCQ CX

    CMPB AL, $0x22
    JEQ scan_done
    CMPB AL, $0x5C
    JEQ scan_tail_escape

    JMP scan_tail_loop

scan_tail_escape:
    MOVQ $1, R8
    CMPQ CX, BX
    JGE scan_eof
    INCQ CX
    JMP scan_tail_loop

scan_done:
    MOVQ CX, R14
    CMPQ R8, $0
    JNE escaped_copy

    MOVQ R14, R15
    DECQ R15
    MOVQ R12, ret+24(FP)
    MOVQ R15, ret+32(FP)
    MOVQ R15, ret+40(FP)
    MOVQ R14, ret+48(FP)
    RET

escaped_copy:
    MOVQ R14, BX
    DECQ BX
    MOVQ BX, 0(SP)
    MOVQ R12, 8(SP)
    MOVQ R14, 16(SP)
    CALL ·allocBytes(SB)
    MOVQ 8(SP), R12
    MOVQ 16(SP), R14
    MOVQ AX, DI
    XORQ DX, DX
    XORQ R13, R13
    MOVQ R14, R15
    DECQ R15

copy_loop:
    CMPQ R13, R15
    JGE copy_done

    MOVB (R12)(R13*1), AL
    INCQ R13

    CMPB AL, $0x22
    JEQ copy_done
    CMPB AL, $0x5C
    JEQ copy_escape

    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP copy_loop

copy_escape:
    CMPQ R13, R15
    JGE copy_done

    MOVB (R12)(R13*1), AL
    INCQ R13

    CMPB AL, $0x6E
    JEQ copy_n
    CMPB AL, $0x72
    JEQ copy_r
    CMPB AL, $0x74
    JEQ copy_t
    CMPB AL, $0x62
    JEQ copy_b
    CMPB AL, $0x66
    JEQ copy_f
    CMPB AL, $0x22
    JEQ copy_quote
    CMPB AL, $0x5C
    JEQ copy_backslash
    CMPB AL, $0x2F
    JEQ copy_slash
    JMP copy_write

copy_n:
    MOVB $0x0A, AL
    JMP copy_write
copy_r:
    MOVB $0x0D, AL
    JMP copy_write
copy_t:
    MOVB $0x09, AL
    JMP copy_write
copy_b:
    MOVB $0x08, AL
    JMP copy_write
copy_f:
    MOVB $0x0C, AL
    JMP copy_write
copy_quote:
    MOVB $0x22, AL
    JMP copy_write
copy_backslash:
    MOVB $0x5C, AL
    JMP copy_write
copy_slash:
    MOVB $0x2F, AL
    JMP copy_write

copy_write:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP copy_loop

copy_done:
    MOVQ DI, ret+24(FP)
    MOVQ DX, ret+32(FP)
    MOVQ DX, ret+40(FP)
    MOVQ R14, ret+48(FP)
    RET

scan_eof:
    MOVQ $0, ret+24(FP)
    MOVQ $0, ret+32(FP)
    MOVQ $0, ret+40(FP)
    MOVQ $-1, ret+48(FP)
    RET

// func parseShortStringCopyASM(src []byte, dst []byte) int64
TEXT ·parseShortStringCopyASM(SB), NOSPLIT, $0-56
    MOVQ src_ptr+0(FP), SI
    MOVQ src_len+8(FP), BX
    MOVQ dst_ptr+24(FP), DI

    XORQ CX, CX // readIdx
    XORQ DX, DX // writeIdx

copy_loop:
    CMPQ CX, BX
    JGE copy_done

    MOVB (SI)(CX*1), AL
    INCQ CX

    CMPB AL, $0x22
    JEQ  copy_done
    CMPB AL, $0x5C
    JEQ  copy_escape

    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP copy_loop

copy_escape:
    CMPQ CX, BX
    JGE copy_eof

    MOVB (SI)(CX*1), AL
    INCQ CX

    CMPB AL, $0x6E
    JEQ  copy_n
    CMPB AL, $0x72
    JEQ  copy_r
    CMPB AL, $0x74
    JEQ  copy_t
    CMPB AL, $0x62
    JEQ  copy_b
    CMPB AL, $0x66
    JEQ  copy_f
    JMP  copy_write

copy_n:
    MOVB $0x0A, AL
    JMP copy_write
copy_r:
    MOVB $0x0D, AL
    JMP copy_write
copy_t:
    MOVB $0x09, AL
    JMP copy_write
copy_b:
    MOVB $0x08, AL
    JMP copy_write
copy_f:
    MOVB $0x0C, AL
    JMP copy_write

copy_write:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP copy_loop

copy_done:
    MOVQ DX, written+48(FP)
    RET

copy_eof:
    MOVQ $-1, written+48(FP)
    RET

// func findObjectBoundariesASM(data []byte, chunks []Chunk) (ret0 int, ret1 int)
TEXT ·findObjectBoundariesASM(SB), NOSPLIT, $8-64
    // 1. Arguments:
    // data_ptr (0), data_len (8), data_cap (16)
    // chunks_ptr (24), chunks_len (32), chunks_cap (40)
    MOVQ data_base+0(FP), SI
    MOVQ data_len+8(FP), BX
    MOVQ chunks_base+24(FP), DI
    MOVQ chunks_len+32(FP), DX

    // 2. SIMD constants
    MOVQ $0x2222222222222222, R14
    MOVQ R14, X1
    VPBROADCASTQ X1, Y1

    MOVQ $0x5c5c5c5c5c5c5c5c, R14
    MOVQ R14, X2
    VPBROADCASTQ X2, Y2

    MOVQ $0x7b7b7b7b7b7b7b7b, R14
    MOVQ R14, X3
    VPBROADCASTQ X3, Y3

    MOVQ $0x7d7d7d7d7d7d7d7d, R14
    MOVQ R14, X4
    VPBROADCASTQ X4, Y4

    MOVQ $0x5b5b5b5b5b5b5b5b, R14
    MOVQ R14, X5
    VPBROADCASTQ X5, Y5

    MOVQ $0x5d5d5d5d5d5d5d5d, R14
    MOVQ R14, X6
    VPBROADCASTQ X6, Y6

    // 3. Initialization
    XORQ CX, CX        // i
    XORQ R8, R8        // totalCount
    XORQ R9, R9        // objectDepth
    XORQ R12, R12      // totalDepth
    MOVQ $-1, R10      // start
    XORQ R11, R11      // inString
    XORQ R13, R13      // storedCount

    MOVQ $0, 0(SP)     // escape_carry = 0

    // Protection against nil
    TESTQ SI, SI
    JZ    done
    TESTQ DI, DI
    JZ    done

avx_loop:
    MOVQ BX, R15
    SUBQ CX, R15
    CMPQ R15, $32
    JL   scalar_tail

    VMOVDQU (SI)(CX*1), Y0
    
    // Compare against special characters
    VPCMPEQB Y1, Y0, Y7   // "
    VPCMPEQB Y2, Y0, Y8   // \
    VPOR Y7, Y8, Y7
    VPCMPEQB Y3, Y0, Y8   // {
    VPOR Y7, Y8, Y7
    VPCMPEQB Y4, Y0, Y8   // }
    VPOR Y7, Y8, Y7
    VPCMPEQB Y5, Y0, Y8   // [
    VPOR Y7, Y8, Y7
    VPCMPEQB Y6, Y0, Y8   // ]
    VPOR Y7, Y8, Y7

    VPMOVMSKB Y7, R15     // R15 = 32-bit mask of special characters

    // Apply escape carry from previous chunk
    CMPQ 0(SP), $0
    JEQ  no_carry
    BTRL $0, R15          // Clear bit 0
    MOVQ $0, 0(SP)        // Clear carry flag
no_carry:

    TESTL R15, R15
    JZ    avx_advance

mask_loop:
    BSFL R15, R14         // R14 = index of lowest set bit
    MOVQ CX, AX
    ADDQ R14, AX
    MOVB (SI)(AX*1), AL

    TESTQ R11, R11
    JNZ   inside_string

    // --- OUTSIDE STRING ---
    CMPB AL, $0x22
    JEQ  quote_found
    CMPB AL, $0x7B
    JEQ  brace_open_found
    CMPB AL, $0x7D
    JEQ  brace_close_found
    CMPB AL, $0x5B
    JEQ  bracket_open_found
    CMPB AL, $0x5D
    JEQ  bracket_close_found
    JMP  next_bit

    // --- INSIDE STRING ---
inside_string:
    CMPB AL, $0x22
    JEQ  quote_found
    CMPB AL, $0x5C
    JEQ  backslash_found
    JMP  next_bit

quote_found:
    XORQ $1, R11
    JMP  next_bit

backslash_found:
    CMPL R14, $31
    JEQ  escape_carry
    // Clear next bit
    MOVL R14, AX
    INCL AX
    BTRL AX, R15
    JMP  next_bit
escape_carry:
    MOVQ $1, 0(SP)
    JMP  next_bit

brace_open_found:
    INCQ R9
    INCQ R12
    CMPQ R9, $1
    JNE  next_bit
    MOVQ CX, R10
    ADDQ R14, R10
    JMP  next_bit

brace_close_found:
    TESTQ R9, R9
    JZ   bracket_close_found
    DECQ R9
    TESTQ R12, R12
    JZ   next_bit
    DECQ R12
    TESTQ R9, R9
    JNZ  next_bit

    INCQ R8
    CMPQ R13, DX
    JGE  reset_start

    MOVQ R13, AX
    SHLQ $4, AX
    MOVQ R10, 0(DI)(AX*1)
    MOVQ CX, 8(DI)(AX*1)
    ADDQ R14, 8(DI)(AX*1)
    INCQ 8(DI)(AX*1)
    INCQ R13
reset_start:
    MOVQ $-1, R10
    JMP  next_bit

bracket_open_found:
    INCQ R12
    JMP  next_bit

bracket_close_found:
    TESTQ R12, R12
    JZ   next_bit
    DECQ R12
    JMP  next_bit

next_bit:
    LEAL -1(R15), AX
    ANDL AX, R15
    JNZ  mask_loop

avx_advance:
    ADDQ $32, CX
    JMP  avx_loop

scalar_tail:
    CMPQ CX, BX
    JGE  done
    MOVB (SI)(CX*1), AL
    
    TESTQ R11, R11
    JNZ  tail_inside_string

    CMPB AL, $0x22
    JEQ  tail_quote_found
    CMPB AL, $0x5C
    JEQ  tail_backslash_found
    CMPB AL, $0x7B
    JEQ  tail_brace_open_found
    CMPB AL, $0x7D
    JEQ  tail_brace_close_found
    CMPB AL, $0x5B
    JEQ  tail_bracket_open_found
    CMPB AL, $0x5D
    JEQ  tail_bracket_close_found
    JMP  tail_next

tail_inside_string:
    CMPB AL, $0x22
    JEQ  tail_quote_found
    CMPB AL, $0x5C
    JEQ  tail_backslash_found
    JMP  tail_next

tail_quote_found:
    XORQ $1, R11
    JMP  tail_next

tail_backslash_found:
    INCQ CX
    JMP  tail_next

tail_brace_open_found:
    INCQ R9
    INCQ R12
    CMPQ R9, $1
    JNE  tail_next
    MOVQ CX, R10
    JMP  tail_next

tail_brace_close_found:
    TESTQ R9, R9
    JZ   tail_bracket_close_found
    DECQ R9
    TESTQ R12, R12
    JZ   tail_next
    DECQ R12
    TESTQ R9, R9
    JNZ  tail_next

    INCQ R8
    CMPQ R13, DX
    JGE  tail_reset_start

    MOVQ R13, AX
    SHLQ $4, AX
    MOVQ R10, 0(DI)(AX*1)
    MOVQ CX, 8(DI)(AX*1)
    INCQ 8(DI)(AX*1)
    INCQ R13
tail_reset_start:
    MOVQ $-1, R10
    JMP  tail_next

tail_bracket_open_found:
    INCQ R12
    JMP  tail_next

tail_bracket_close_found:
    TESTQ R12, R12
    JZ   tail_next
    DECQ R12
    JMP  tail_next

tail_next:
    INCQ CX
    JMP  scalar_tail

done:
    VZEROUPPER
    MOVQ R8, ret0+48(FP)
    MOVQ R12, ret1+56(FP)
    RET


// func skipValueASM(raw []byte, start int) int
TEXT ·skipValueASM(SB), NOSPLIT, $0-24
    // Load arguments (Go ABI)
    MOVQ raw_base+0(FP), SI
    MOVQ raw_len+8(FP), BX
    MOVQ start+24(FP), CX

    // Check input bounds
    CMPQ CX, BX
    JGE  done_err

loop:
    CMPQ CX, BX
    JGE  done_err

    MOVB (SI)(CX*1), AL

    // Search for quote
    CMPB AL, $0x22
    JEQ  skip_string

    // Stop at struct
    CMPB AL, $0x2C
    JEQ  done
    CMPB AL, $0x7D
    JEQ  done
    CMPB AL, $0x5D
    JEQ  done

    INCQ CX
    JMP  loop

skip_string:
    INCQ CX
string_loop:
    CMPQ CX, BX
    JGE  done_err

    MOVB (SI)(CX*1), AL

    CMPB AL, $0x5C // '\'
    JEQ  handle_escape

    CMPB AL, $0x22 // '"'
    JEQ  next_loop

    INCQ CX
    JMP  string_loop

handle_escape:
    ADDQ $2, CX
    JMP  string_loop

next_loop:
    INCQ CX
    JMP  loop

done:
    MOVQ CX, ret+32(FP)
    RET

done_err:
    MOVQ BX, ret+32(FP)
    RET

// func appendIntASM(buf []byte, val int64) []byte
TEXT ·appendIntASM(SB), NOSPLIT, $0-56
    MOVQ buf_base+0(FP), DI    // Base
    MOVQ buf_len+8(FP), SI     // Len
    MOVQ buf_cap+16(FP), R12   // Cap
    MOVQ val+24(FP), AX        // Val

    TESTQ DI, DI
    JZ   overflow

    MOVQ SI, R8
    ADDQ $20, R8
    CMPQ R8, R12
    JGT  overflow

    TESTQ AX, AX
    JNE   check_negative
    MOVB  $0x30, 0(DI)(SI*1)
    INCQ  SI
    JMP   done

check_negative:
    JGE   prepare_div
    MOVB  $0x2D, 0(DI)(SI*1)
    INCQ  SI
    NEGQ  AX

prepare_div:
    MOVQ  SI, R8
    MOVQ  $10, CX

loop:
    CMPQ SI, R12
    JGE  overflow

    MOVQ  AX, BX
    MOVQ  $0xCCCCCCCCCCCCCCCD, R9
    MULQ R9
    MOVQ  DX, AX
    SHRQ  $3, AX
    MOVQ  AX, R10
    IMULQ $10, R10
    SUBQ  R10, BX

    ADDQ  $0x30, BX
    MOVB  BL, 0(DI)(SI*1)
    INCQ  SI

    TESTQ AX, AX
    JNE   loop

    MOVQ  R8, DX
    MOVQ  SI, CX
    DECQ  CX

reverse:
    CMPQ  DX, CX
    JGE   done
    MOVB  (DI)(DX*1), R10
    MOVB  (DI)(CX*1), R11
    MOVB  R11, (DI)(DX*1)
    MOVB  R10, (DI)(CX*1)
    INCQ  DX
    DECQ  CX
    JMP   reverse

overflow:
    MOVQ buf_base+0(FP), AX
    MOVQ AX, ret_base+32(FP)
    MOVQ buf_len+8(FP), AX
    MOVQ AX, ret_len+40(FP)
    MOVQ R12, ret_cap+48(FP)
    RET

done:
    MOVQ buf_base+0(FP), AX
    MOVQ AX, ret_base+32(FP)
    MOVQ SI, ret_len+40(FP)
    MOVQ R12, ret_cap+48(FP)
    RET

// func skipSpaceASM(raw []byte, start int) int
TEXT ·skipSpaceASM(SB), NOSPLIT, $0-40
    MOVQ raw_base+0(FP), SI
    MOVQ raw_len+8(FP), BX
    MOVQ start+24(FP), CX

    MOVQ $0x2020202020202020, R8
    MOVQ R8, X1
    VPBROADCASTQ X1, Y1
    MOVQ $0x0909090909090909, R8
    MOVQ R8, X2
    VPBROADCASTQ X2, Y2
    MOVQ $0x0A0A0A0A0A0A0A0A, R8
    MOVQ R8, X3
    VPBROADCASTQ X3, Y3
    MOVQ $0x0D0D0D0D0D0D0D0D, R8
    MOVQ R8, X4
    VPBROADCASTQ X4, Y4

ss_avx_loop:
    MOVQ BX, AX
    SUBQ CX, AX
    CMPQ AX, $32
    JL ss_scalar_loop

    VMOVDQU (SI)(CX*1), Y0
    VPCMPEQB Y1, Y0, Y5
    VPCMPEQB Y2, Y0, Y6
    VPOR Y5, Y6, Y5
    VPCMPEQB Y3, Y0, Y6
    VPOR Y5, Y6, Y5
    VPCMPEQB Y4, Y0, Y6
    VPOR Y5, Y6, Y5

    VPMOVMSKB Y5, DX
    NOTL DX
    TESTL DX, DX
    JZ ss_all_spaces

    BSFL DX, DX
    ADDQ DX, CX
    JMP ss_done

ss_all_spaces:
    ADDQ $32, CX
    JMP ss_avx_loop

ss_scalar_loop:
    CMPQ CX, BX
    JGE ss_done
    MOVB (SI)(CX*1), AL
    CMPB AL, $0x20
    JEQ ss_next_char
    CMPB AL, $0x09
    JEQ ss_next_char
    CMPB AL, $0x0A
    JEQ ss_next_char
    CMPB AL, $0x0D
    JEQ ss_next_char
    JMP ss_done

ss_next_char:
    INCQ CX
    JMP ss_scalar_loop

ss_done:
    VZEROUPPER
    MOVQ CX, ret+32(FP)
    RET


// func findObjectBoundariesEarlyExitASM(data []byte, chunks []Chunk) (ret0 int, ret1 int)
TEXT ·findObjectBoundariesEarlyExitASM(SB), NOSPLIT, $8-64
    // 1. Arguments:
    // data_ptr (0), data_len (8), data_cap (16)
    // chunks_ptr (24), chunks_len (32), chunks_cap (40)
    MOVQ data_base+0(FP), SI
    MOVQ data_len+8(FP), BX
    MOVQ chunks_base+24(FP), DI
    MOVQ chunks_len+32(FP), DX

    // 2. SIMD constants
    MOVQ $0x2222222222222222, R14
    MOVQ R14, X1
    VPBROADCASTQ X1, Y1

    MOVQ $0x5c5c5c5c5c5c5c5c, R14
    MOVQ R14, X2
    VPBROADCASTQ X2, Y2

    MOVQ $0x7b7b7b7b7b7b7b7b, R14
    MOVQ R14, X3
    VPBROADCASTQ X3, Y3

    MOVQ $0x7d7d7d7d7d7d7d7d, R14
    MOVQ R14, X4
    VPBROADCASTQ X4, Y4

    MOVQ $0x5b5b5b5b5b5b5b5b, R14
    MOVQ R14, X5
    VPBROADCASTQ X5, Y5

    MOVQ $0x5d5d5d5d5d5d5d5d, R14
    MOVQ R14, X6
    VPBROADCASTQ X6, Y6

    // 3. Initialization
    XORQ CX, CX        // i
    XORQ R8, R8        // totalCount
    XORQ R9, R9        // objectDepth
    XORQ R12, R12      // totalDepth
    MOVQ $-1, R10      // start
    XORQ R11, R11      // inString
    XORQ R13, R13      // storedCount

    MOVQ $0, 0(SP)     // escape_carry = 0

    // Protection against nil
    TESTQ SI, SI
    JZ    done
    TESTQ DI, DI
    JZ    done

avx_loop:
    MOVQ BX, R15
    SUBQ CX, R15
    CMPQ R15, $32
    JL   scalar_tail

    VMOVDQU (SI)(CX*1), Y0
    
    // Compare against special characters
    VPCMPEQB Y1, Y0, Y7   // "
    VPCMPEQB Y2, Y0, Y8   // \
    VPOR Y7, Y8, Y7
    VPCMPEQB Y3, Y0, Y8   // {
    VPOR Y7, Y8, Y7
    VPCMPEQB Y4, Y0, Y8   // }
    VPOR Y7, Y8, Y7
    VPCMPEQB Y5, Y0, Y8   // [
    VPOR Y7, Y8, Y7
    VPCMPEQB Y6, Y0, Y8   // ]
    VPOR Y7, Y8, Y7

    VPMOVMSKB Y7, R15     // R15 = 32-bit mask of special characters

    // Apply escape carry from previous chunk
    CMPQ 0(SP), $0
    JEQ  no_carry
    BTRL $0, R15          // Clear bit 0
    MOVQ $0, 0(SP)        // Clear carry flag
no_carry:

    TESTL R15, R15
    JZ    avx_advance

mask_loop:
    BSFL R15, R14         // R14 = index of lowest set bit
    MOVQ CX, AX
    ADDQ R14, AX
    MOVB (SI)(AX*1), AL

    TESTQ R11, R11
    JNZ   inside_string

    // --- OUTSIDE STRING ---
    CMPB AL, $0x22
    JEQ  quote_found
    CMPB AL, $0x7B
    JEQ  brace_open_found
    CMPB AL, $0x7D
    JEQ  brace_close_found
    CMPB AL, $0x5B
    JEQ  bracket_open_found
    CMPB AL, $0x5D
    JEQ  bracket_close_found
    JMP  next_bit

    // --- INSIDE STRING ---
inside_string:
    CMPB AL, $0x22
    JEQ  quote_found
    CMPB AL, $0x5C
    JEQ  backslash_found
    JMP  next_bit

quote_found:
    XORQ $1, R11
    JMP  next_bit

backslash_found:
    CMPL R14, $31
    JEQ  escape_carry
    // Clear next bit
    MOVL R14, AX
    INCL AX
    BTRL AX, R15
    JMP  next_bit
escape_carry:
    MOVQ $1, 0(SP)
    JMP  next_bit

brace_open_found:
    INCQ R9
    INCQ R12
    CMPQ R9, $1
    JNE  next_bit
    MOVQ CX, R10
    ADDQ R14, R10
    JMP  next_bit

brace_close_found:
    TESTQ R9, R9
    JZ   bracket_close_found
    DECQ R9
    TESTQ R12, R12
    JZ   next_bit
    DECQ R12
    TESTQ R9, R9
    JNZ  next_bit

    INCQ R8
    CMPQ R13, DX
    JGE  done

    MOVQ R13, AX
    SHLQ $4, AX
    MOVQ R10, 0(DI)(AX*1)
    MOVQ CX, 8(DI)(AX*1)
    ADDQ R14, 8(DI)(AX*1)
    INCQ 8(DI)(AX*1)
    INCQ R13
reset_start:
    MOVQ $-1, R10
    JMP  next_bit

bracket_open_found:
    INCQ R12
    JMP  next_bit

bracket_close_found:
    TESTQ R12, R12
    JZ   next_bit
    DECQ R12
    JMP  next_bit

next_bit:
    LEAL -1(R15), AX
    ANDL AX, R15
    JNZ  mask_loop

avx_advance:
    ADDQ $32, CX
    JMP  avx_loop

scalar_tail:
    CMPQ CX, BX
    JGE  done
    MOVB (SI)(CX*1), AL
    
    TESTQ R11, R11
    JNZ  tail_inside_string

    CMPB AL, $0x22
    JEQ  tail_quote_found
    CMPB AL, $0x5C
    JEQ  tail_backslash_found
    CMPB AL, $0x7B
    JEQ  tail_brace_open_found
    CMPB AL, $0x7D
    JEQ  tail_brace_close_found
    CMPB AL, $0x5B
    JEQ  tail_bracket_open_found
    CMPB AL, $0x5D
    JEQ  tail_bracket_close_found
    JMP  tail_next

tail_inside_string:
    CMPB AL, $0x22
    JEQ  tail_quote_found
    CMPB AL, $0x5C
    JEQ  tail_backslash_found
    JMP  tail_next

tail_quote_found:
    XORQ $1, R11
    JMP  tail_next

tail_backslash_found:
    INCQ CX
    JMP  tail_next

tail_brace_open_found:
    INCQ R9
    INCQ R12
    CMPQ R9, $1
    JNE  tail_next
    MOVQ CX, R10
    JMP  tail_next

tail_brace_close_found:
    TESTQ R9, R9
    JZ   tail_bracket_close_found
    DECQ R9
    TESTQ R12, R12
    JZ   tail_next
    DECQ R12
    TESTQ R9, R9
    JNZ  tail_next

    MOVQ R13, AX
    SHLQ $4, AX
    MOVQ R10, 0(DI)(AX*1)
    MOVQ CX, 8(DI)(AX*1)
    INCQ 8(DI)(AX*1)
    INCQ R13
    INCQ R8
    CMPQ R13, DX
    JGE  done
tail_reset_start:
    MOVQ $-1, R10
    JMP  tail_next

tail_bracket_open_found:
    INCQ R12
    JMP  tail_next

tail_bracket_close_found:
    TESTQ R12, R12
    JZ   tail_next
    DECQ R12
    JMP  tail_next

tail_next:
    INCQ CX
    JMP  scalar_tail

done:
    VZEROUPPER
    MOVQ R8, ret0+48(FP)
    MOVQ R12, ret1+56(FP)
    RET


// func skipValueASM(raw []byte, start int) int
