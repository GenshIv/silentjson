#include "textflag.h"

// func findQuoteAsm(data []byte) (index int)
TEXT ·findQuoteAsm(SB), NOSPLIT, $0-32
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD $0, R2 // index = 0

loop:
    CMP R2, R1
    BGE not_found
    MOVBU (R0)(R2), R3 // load 1 byte
    CMP $0x22, R3      // '"'
    BEQ found
    ADD $1, R2
    B loop

found:
    MOVD R2, ret+24(FP)
    RET

not_found:
    MOVD $-1, R2
    MOVD R2, ret+24(FP)
    RET


// func scanJSONStringASM(src []byte) (end int, hasEscape bool)
TEXT ·scanJSONStringASM(SB), NOSPLIT, $0-33
    MOVD src_base+0(FP), R0
    MOVD src_len+8(FP), R1
    MOVD $0, R2 // index
    MOVD $0, R3 // hasEscape
    MOVD $0, R4 // escapeNext

scan_loop:
    CMP R2, R1
    BGE scan_eof

    MOVBU (R0)(R2), R5

    CBNZ R4, scan_escaped

    CMP $0x5C, R5 // '\'
    BEQ scan_escape
    CMP $0x22, R5 // '"'
    BEQ scan_quote

    ADD $1, R2
    B scan_loop

scan_escaped:
    MOVD $0, R4
    ADD $1, R2
    B scan_loop

scan_escape:
    MOVD $1, R3
    MOVD $1, R4
    ADD $1, R2
    B scan_loop

scan_quote:
    MOVD R2, ret+24(FP)
    MOVB R3, hasEscape+32(FP)
    RET

scan_eof:
    MOVD $-1, R2
    MOVD R2, ret+24(FP)
    MOVB $0, R5
    MOVB R5, hasEscape+32(FP)
    RET


// func skipSpaceASM(data []byte, start int) int
TEXT ·skipSpaceASM(SB), NOSPLIT, $0-40
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD start+24(FP), R2

skip_loop:
    CMP R2, R1
    BGE skip_eof
    
    MOVBU (R0)(R2), R3
    CMP $0x20, R3 // ' '
    BEQ skip_next
    CMP $0x09, R3 // '\t'
    BEQ skip_next
    CMP $0x0A, R3 // '\n'
    BEQ skip_next
    CMP $0x0D, R3 // '\r'
    BEQ skip_next
    
    // not space
    MOVD R2, ret+32(FP)
    RET

skip_next:
    ADD $1, R2
    B skip_loop

skip_eof:
    MOVD R1, ret+32(FP)
    RET


// func findQuoteOrEscapeASM(b []byte) (idx int, isEscape bool)
TEXT ·findQuoteOrEscapeASM(SB), NOSPLIT, $0-33
    MOVD b_base+0(FP), R0
    MOVD b_len+8(FP), R1
    MOVD $0, R2 // index

fq_loop:
    CMP R2, R1
    BGE fq_not_found

    MOVBU (R0)(R2), R3
    CMP $0x22, R3 // '"'
    BEQ fq_quote
    CMP $0x5C, R3 // '\'
    BEQ fq_escape

    ADD $1, R2
    B fq_loop

fq_quote:
    MOVD R2, idx+24(FP)
    MOVD $0, R4
    MOVB R4, isEscape+32(FP)
    RET

fq_escape:
    MOVD R2, idx+24(FP)
    MOVD $1, R4
    MOVB R4, isEscape+32(FP)
    RET

fq_not_found:
    MOVD $-1, R2
    MOVD R2, idx+24(FP)
    MOVD $0, R4
    MOVB R4, isEscape+32(FP)
    RET


// func appendStringASM(buf []byte, s string) ([]byte, int)
TEXT ·appendStringASM(SB), NOSPLIT, $0-72
    MOVD buf_base+0(FP), R0   // buf.data
    MOVD buf_len+8(FP), R1    // buf.len
    MOVD buf_cap+16(FP), R2   // buf.cap
    MOVD s_base+24(FP), R3    // s.data
    MOVD s_len+32(FP), R4     // s.len

    // check capacity: buf.len + s.len + 2 <= buf.cap
    MOVD R1, R5
    ADD R4, R5
    ADD $2, R5
    CMP R5, R2
    BLT no_space

    // buf.data[buf.len] = '"'
    MOVD R0, R6
    ADD R1, R6 // R6 = ptr to buf end
    MOVD $0x22, R7
    MOVB R7, (R6)
    ADD $1, R6

    MOVD $0, R8 // index i = 0
    MOVD $-1, R9 // specialPos = -1

as_loop:
    CMP R8, R4
    BGE as_end

    MOVBU (R3)(R8), R10 // load s[i]

    // check special
    CMP $0x22, R10
    BEQ as_special
    CMP $0x5C, R10
    BEQ as_special
    CMP $0x20, R10
    BLT as_special
    B as_write

as_special:
    CMP $-1, R9
    BNE as_write
    MOVD R8, R9

as_write:
    MOVB R10, (R6)
    ADD $1, R6
    ADD $1, R8
    B as_loop

as_end:
    // write final '"'
    MOVD $0x22, R7
    MOVB R7, (R6)
    
    // new buf len
    MOVD R1, R11
    ADD R4, R11
    ADD $2, R11

    // write return values
    MOVD R0, ret_base+40(FP)
    MOVD R11, ret_len+48(FP)
    MOVD R2, ret_cap+56(FP)
    MOVD R9, ret1+64(FP)
    RET

no_space:
    // return (buf, -2) if not enough space
    MOVD R0, ret_base+40(FP)
    MOVD R1, ret_len+48(FP)
    MOVD R2, ret_cap+56(FP)
    MOVD $-2, R9
    MOVD R9, ret1+64(FP)
    RET

// func findObjectBoundariesASM(data []byte, chunks []Chunk) (int, int)
TEXT ·findObjectBoundariesASM(SB), NOSPLIT, $0-64
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD chunks_base+24(FP), R2 // chunks pointer
    MOVD chunks_len+32(FP), R3  // chunks max count

    MOVD $0, R4 // count
    MOVD $0, R5 // depth
    MOVD $0, R6 // inString (0 = false, 1 = true)
    MOVD $0, R7 // escaped (0 = false, 1 = true)
    MOVD $0, R8 // maxSize
    MOVD $-1, R9 // currentStart

    MOVD $0, R10 // i (index)

fob_loop:
    CMP R10, R1
    BGE fob_end

    MOVBU (R0)(R10), R11 // c = data[i]

    CBNZ R6, fob_in_string

    // not in string
    CMP $0x22, R11 // '"'
    BEQ fob_quote
    CMP $0x7B, R11 // '{'
    BEQ fob_open
    CMP $0x7D, R11 // '}'
    BEQ fob_close
    
    ADD $1, R10
    B fob_loop

fob_in_string:
    CBNZ R7, fob_was_escaped

    CMP $0x5C, R11 // '\'
    BEQ fob_escape
    CMP $0x22, R11 // '"'
    BEQ fob_end_string

    ADD $1, R10
    B fob_loop

fob_was_escaped:
    MOVD $0, R7
    ADD $1, R10
    B fob_loop

fob_escape:
    MOVD $1, R7
    ADD $1, R10
    B fob_loop

fob_end_string:
    MOVD $0, R6
    ADD $1, R10
    B fob_loop

fob_quote:
    MOVD $1, R6
    ADD $1, R10
    B fob_loop

fob_open:
    CBNZ R5, fob_open_skip
    MOVD R10, R9 // currentStart = i
fob_open_skip:
    ADD $1, R5 // depth++
    ADD $1, R10
    B fob_loop

fob_close:
    SUB $1, R5 // depth--
    CBNZ R5, fob_close_skip
    CMP $-1, R9
    BEQ fob_close_skip

    // record chunk if count < len(chunks)
    CMP R4, R3
    BLE fob_close_inc_count

    LSL $4, R4, R12 // offset = count * 16
    ADD R2, R12, R12 // R12 = pointer to chunks[count]

    MOVD R9, 0(R12) // chunks[count].Start
    MOVD R10, R13
    ADD $1, R13
    MOVD R13, 8(R12) // chunks[count].End

    // size = (i + 1) - currentStart
    SUB R9, R13, R14
    CMP R8, R14
    BLE fob_close_inc_count
    MOVD R14, R8 // maxSize = size

fob_close_inc_count:
    ADD $1, R4
    MOVD $-1, R9

fob_close_skip:
    ADD $1, R10
    B fob_loop

fob_end:
    MOVD R4, ret0+48(FP)
    MOVD R8, ret1+56(FP)
    RET


// func findObjectBoundariesEarlyExitASM(data []byte, chunks []Chunk) (int, int)
TEXT ·findObjectBoundariesEarlyExitASM(SB), NOSPLIT, $0-64
    JMP ·findObjectBoundariesASM(SB)


// func findArrayElementsEarlyExitASM(data []byte, chunks []Chunk) (int, int)
TEXT ·findArrayElementsEarlyExitASM(SB), NOSPLIT, $0-64
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD chunks_base+24(FP), R2
    MOVD chunks_len+32(FP), R3

    MOVD $0, R4 // count
    MOVD $0, R5 // depth
    MOVD $0, R6 // inString
    MOVD $0, R7 // escaped
    MOVD $-1, R9 // currentStart
    MOVD $0, R10 // i

fae_loop:
    CMP R10, R1
    BGE fae_end_check

    MOVBU (R0)(R10), R11

    CBNZ R6, fae_in_string

    // not in string
    CMP $0x22, R11 // '"'
    BEQ fae_quote
    CMP $0x5B, R11 // '['
    BEQ fae_open
    CMP $0x5D, R11 // ']'
    BEQ fae_close
    CMP $0x2C, R11 // ','
    BEQ fae_comma

    // check if it's the start of a value
    CMP $1, R5
    BNE fae_next
    CMP $-1, R9
    BNE fae_next

    CMP $0x7B, R11 // '{'
    BEQ fae_set_start
    CMP $0x74, R11 // 't'
    BEQ fae_set_start
    CMP $0x66, R11 // 'f'
    BEQ fae_set_start
    CMP $0x6E, R11 // 'n'
    BEQ fae_set_start
    CMP $0x2D, R11 // '-'
    BEQ fae_set_start
    CMP $0x30, R11 // '0'
    BLT fae_next
    CMP $0x39, R11 // '9'
    BGT fae_next

fae_set_start:
    MOVD R10, R9
fae_next:
    ADD $1, R10
    B fae_loop

fae_in_string:
    CBNZ R7, fae_was_escaped
    CMP $0x5C, R11
    BEQ fae_escape
    CMP $0x22, R11
    BEQ fae_end_string
    ADD $1, R10
    B fae_loop

fae_was_escaped:
    MOVD $0, R7
    ADD $1, R10
    B fae_loop

fae_escape:
    MOVD $1, R7
    ADD $1, R10
    B fae_loop

fae_end_string:
    MOVD $0, R6
    ADD $1, R10
    B fae_loop

fae_quote:
    MOVD $1, R6
    ADD $1, R10
    B fae_loop

fae_open:
    ADD $1, R5
    ADD $1, R10
    B fae_loop

fae_close:
    SUB $1, R5
    CMP $1, R5
    BNE fae_close_skip
    CMP $-1, R9
    BEQ fae_close_skip

    // record chunk
    CMP R4, R3
    BLE fae_close_inc_count

    LSL $4, R4, R12
    ADD R2, R12, R12
    MOVD R9, 0(R12)
    MOVD R10, R13
    ADD $1, R13
    MOVD R13, 8(R12)

fae_close_inc_count:
    ADD $1, R4
    MOVD $-1, R9

fae_close_skip:
    ADD $1, R10
    B fae_loop

fae_comma:
    CMP $1, R5
    BNE fae_next
    CMP $-1, R9
    BEQ fae_next

    CMP R4, R3
    BLE fae_comma_inc_count

    LSL $4, R4, R12
    ADD R2, R12, R12
    MOVD R9, 0(R12)
    MOVD R10, 8(R12)

fae_comma_inc_count:
    ADD $1, R4
    MOVD $-1, R9
    ADD $1, R10
    B fae_loop

fae_end_check:
    CMP $1, R5
    BNE fae_end
    CMP $-1, R9
    BEQ fae_end
    CMP R4, R3
    BLE fae_end_inc

    LSL $4, R4, R12
    ADD R2, R12, R12
    MOVD R9, 0(R12)
    MOVD R1, 8(R12)
fae_end_inc:
    ADD $1, R4

fae_end:
    MOVD R4, ret0+48(FP)
    MOVD $0, ret1+56(FP)
    RET
