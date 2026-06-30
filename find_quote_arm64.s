#include "textflag.h"

// func findQuoteAsm(data []byte) (index int)
TEXT ·findQuoteAsm(SB), NOSPLIT, $0-32
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD $0, R2 // index = 0

loop:
    CMP R1, R2
    BGE not_found
    ADD R2, R0, R12
    MOVBU (R12), R3
    CMP $0x22, R3
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

// func skipSpaceASM(data []byte, start int) int
TEXT ·skipSpaceASM(SB), NOSPLIT, $0-40
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD start+24(FP), R2

skip_loop:
    CMP R1, R2
    BGE skip_eof
    
    ADD R2, R0, R12
    MOVBU (R12), R3
    CMP $0x20, R3
    BEQ skip_next
    CMP $0x09, R3
    BEQ skip_next
    CMP $0x0A, R3
    BEQ skip_next
    CMP $0x0D, R3
    BEQ skip_next
    
    MOVD R2, ret+32(FP)
    RET

skip_next:
    ADD $1, R2
    B skip_loop

skip_eof:
    MOVD R1, ret+32(FP)
    RET

// func findObjectBoundariesASM(data []byte, chunks []Chunk) (int, int)
TEXT ·findObjectBoundariesASM(SB), NOSPLIT, $0-64
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD chunks_base+24(FP), R2
    MOVD chunks_len+32(FP), R3

    MOVD $0, R4 // count
    MOVD $0, R5 // depth
    MOVD $0, R6 // maxSize
    MOVD $-1, R7 // currentStart
    MOVD $0, R8 // i
    MOVD $0, R9 // inString
    MOVD $0, R10 // escaped

fob_loop:
    CMP R1, R8
    BGE fob_end

    ADD R8, R0, R12
    MOVBU (R12), R11

    CBZ R9, fob_not_in_string

    // inString == 1
    CBZ R10, fob_in_string_not_escaped
    
    // escaped == 1
    MOVD $0, R10
    B fob_next

fob_in_string_not_escaped:
    CMP $0x5C, R11 // '\\'
    BNE fob_check_quote
    MOVD $1, R10
    B fob_next

fob_check_quote:
    CMP $0x22, R11 // '"'
    BNE fob_next
    MOVD $0, R9 // inString = 0
    B fob_next

fob_not_in_string:
    CMP $0x22, R11
    BEQ fob_case_quote
    CMP $0x7B, R11
    BEQ fob_case_lbrace
    CMP $0x7D, R11
    BEQ fob_case_rbrace
    CMP $0x5B, R11
    BEQ fob_case_lbracket
    CMP $0x5D, R11
    BEQ fob_case_rbracket
    B fob_next

fob_case_quote:
    MOVD $1, R9
    B fob_next

fob_case_lbrace:
    CBNZ R5, fob_lbrace_inc
    MOVD R8, R7 // currentStart = i
fob_lbrace_inc:
    ADD $1, R5
    B fob_next

fob_case_lbracket:
    ADD $1, R5
    B fob_next

fob_case_rbracket:
    SUB $1, R5
    B fob_next

fob_case_rbrace:
    SUB $1, R5
    CBNZ R5, fob_next
    MOVD $-1, R15
    CMP R15, R7
    BEQ fob_next

    CMP R3, R4
    BGE fob_rbrace_maxsize

    LSL $4, R4, R12
    ADD R2, R12, R12
    MOVD R7, 0(R12)
    ADD $1, R8, R20
    MOVD R20, 8(R12)

fob_rbrace_maxsize:
    ADD $1, R8, R20
    SUB R7, R20, R20
    CMP R20, R6
    BGE fob_rbrace_finish
    MOVD R20, R6

fob_rbrace_finish:
    ADD $1, R4
    MOVD $-1, R7
    B fob_next

fob_next:
    ADD $1, R8
    B fob_loop

fob_end:
    MOVD R4, ret0+48(FP)
    MOVD R6, ret1+56(FP)
    RET


// func findObjectBoundariesEarlyExitASM(data []byte, chunks []Chunk) (int, int)
TEXT ·findObjectBoundariesEarlyExitASM(SB), NOSPLIT, $0-64
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD chunks_base+24(FP), R2
    MOVD chunks_len+32(FP), R3

    MOVD $0, R4 // count
    MOVD $0, R5 // depth
    MOVD $0, R6 // maxSize
    MOVD $-1, R7 // currentStart
    MOVD $0, R8 // i
    MOVD $0, R9 // inString
    MOVD $0, R10 // escaped

fobe_loop:
    CMP R1, R8
    BGE fobe_end

    ADD R8, R0, R12
    MOVBU (R12), R11

    CBZ R9, fobe_not_in_string

    CBZ R10, fobe_in_string_not_escaped
    
    MOVD $0, R10
    B fobe_next

fobe_in_string_not_escaped:
    CMP $0x5C, R11
    BNE fobe_check_quote
    MOVD $1, R10
    B fobe_next

fobe_check_quote:
    CMP $0x22, R11
    BNE fobe_next
    MOVD $0, R9
    B fobe_next

fobe_not_in_string:
    CMP $0x22, R11
    BEQ fobe_case_quote
    CMP $0x7B, R11
    BEQ fobe_case_lbrace
    CMP $0x7D, R11
    BEQ fobe_case_rbrace
    CMP $0x5B, R11
    BEQ fobe_case_lbracket
    CMP $0x5D, R11
    BEQ fobe_case_rbracket
    B fobe_next

fobe_case_quote:
    MOVD $1, R9
    B fobe_next

fobe_case_lbrace:
    CBNZ R5, fobe_lbrace_inc
    MOVD R8, R7
fobe_lbrace_inc:
    ADD $1, R5
    B fobe_next

fobe_case_lbracket:
    ADD $1, R5
    B fobe_next

fobe_case_rbracket:
    SUB $1, R5
    B fobe_next

fobe_case_rbrace:
    SUB $1, R5
    CBNZ R5, fobe_next
    MOVD $-1, R15
    CMP R15, R7
    BEQ fobe_next

    CMP R3, R4
    BGE fobe_rbrace_maxsize

    LSL $4, R4, R12
    ADD R2, R12, R12
    MOVD R7, 0(R12)
    ADD $1, R8, R20
    MOVD R20, 8(R12)

fobe_rbrace_maxsize:
    ADD $1, R8, R20
    SUB R7, R20, R20
    CMP R20, R6
    BGE fobe_rbrace_finish
    MOVD R20, R6

fobe_rbrace_finish:
    ADD $1, R4
    MOVD $-1, R7
    CMP R3, R4
    BGE fobe_end // early exit
    B fobe_next

fobe_next:
    ADD $1, R8
    B fobe_loop

fobe_end:
    MOVD R4, ret0+48(FP)
    MOVD R6, ret1+56(FP)
    RET


// func findArrayElementsEarlyExitASM(data []byte, chunks []Chunk) (int, int)
TEXT ·findArrayElementsEarlyExitASM(SB), NOSPLIT, $0-64
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD chunks_base+24(FP), R2
    MOVD chunks_len+32(FP), R3

    MOVD data_base+0(FP), R0 // data pointer
    MOVD data_len+8(FP), R1 // data slice length
    MOVD chunks_base+24(FP), R2 // chunks pointer
    MOVD chunks_len+32(FP), R3 // chunk slice length
    MOVD $0, R4 // count
    MOVD $1, R5 // depth
    MOVD R5, R6 // maxSize/maxDepth
    MOVD $-1, R16 // CONSTANT -1
    MOVD R16, R7 // currentStart = -1
    MOVD $0, R8 // i
    MOVD $0, R9 // inString
    MOVD $0, R10 // stringEscape

fae_loop:
    CMP R1, R8
    BGE fae_end

    ADD R8, R0, R12
    MOVBU (R12), R11

    CBZ R9, fae_not_in_string

fae_in_string:
    CMP $0x22, R11 // quote
    BNE fae_in_string_escape

    // quote found
    CMP $1, R10
    BEQ fae_in_string_clear_escape
    MOVD $0, R9
    B fae_next

fae_in_string_escape:
    CMP $0x5C, R11 // backslash
    BNE fae_in_string_clear_escape
    CMP $1, R10
    BEQ fae_in_string_clear_escape
    MOVD $1, R10
    B fae_next

fae_in_string_clear_escape:
    MOVD $0, R10
    B fae_next

fae_not_in_string:
    CMP $0x22, R11
    BEQ fae_case_quote
    CMP $0x5B, R11
    BEQ fae_case_lbracket
    CMP $0x5D, R11
    BEQ fae_case_rbracket
    CMP $0x7B, R11
    BEQ fae_case_lbrace
    CMP $0x7D, R11
    BEQ fae_case_rbrace
    CMP $0x2C, R11
    BEQ fae_case_comma

    // value check
    CMP $0x20, R11
    BEQ fae_next
    CMP $0x09, R11
    BEQ fae_next
    CMP $0x0A, R11
    BEQ fae_next
    CMP $0x0D, R11
    BEQ fae_next

    CMP $1, R5
    BNE fae_next
    CMP R16, R7 // CMP with CONSTANT -1
    BNE fae_next
    MOVD R8, R7
    B fae_next

fae_case_quote:
    MOVD $1, R9
    CMP $1, R5
    BNE fae_next
    CMP R16, R7 // CMP with CONSTANT -1
    BNE fae_next
    MOVD R8, R7
    B fae_next

fae_case_lbracket:
    CMP $1, R5
    BNE fae_lbracket_inc
    CMP R16, R7 // CMP with CONSTANT -1
    BNE fae_lbracket_inc
    MOVD R8, R7
fae_lbracket_inc:
    ADD $1, R5
    CMP R5, R6
    BGE fae_next
    MOVD R5, R6 // update maxDepth
    B fae_next

fae_case_rbracket:
    CMP $1, R5
    BNE fae_rbracket_dec

    // We hit the outer ']' (depth == 1)!
    CMP R16, R7 // CMP with CONSTANT -1
    BEQ fae_ret_rbracket // no pending chunk (e.g. empty array), just exit

    CMP R3, R4
    BGE fae_ret_rbracket // chunks full, exit
    
    // Save pending chunk
    LSL $4, R4, R12
    ADD R2, R12, R12
    MOVD R7, 0(R12)
    MOVD R8, 8(R12) // End = i
    ADD $1, R4
    B fae_ret_rbracket // UNCONDITIONALLY EXIT after outer ']'

fae_rbracket_dec:
    SUB $1, R5
    B fae_next

fae_ret_rbracket:
    B fae_ret

fae_case_lbrace:
    CMP $1, R5
    BNE fae_lbrace_inc
    CMP R16, R7 // CMP with CONSTANT -1
    BNE fae_lbrace_inc
    MOVD R8, R7
fae_lbrace_inc:
    ADD $1, R5
    CMP R5, R6
    BGE fae_next
    MOVD R5, R6 // update maxDepth
    B fae_next

fae_case_rbrace:
    SUB $1, R5
    B fae_next

fae_case_comma:
    CMP $1, R5
    BNE fae_next
    CMP R16, R7 // CMP with CONSTANT -1
    BEQ fae_next

    CMP R3, R4
    BGE fae_comma_finish
    LSL $4, R4, R12
    ADD R2, R12, R12
    MOVD R7, 0(R12)
    MOVD R8, 8(R12) // End = i
fae_comma_finish:
    ADD $1, R4
    MOVD R16, R7 // currentStart = -1
    CMP R3, R4
    BGE fae_ret // early exit
    B fae_next

fae_next:
    ADD $1, R8
    B fae_loop

fae_end:
    CMP $1, R5
    BNE fae_ret
    CMP R16, R7 // CMP with CONSTANT -1
    BEQ fae_ret

    CMP R3, R4
    BGE fae_ret
    LSL $4, R4, R12
    ADD R2, R12, R12
    MOVD R7, 0(R12)
    MOVD R8, 8(R12) // End = i
    ADD $1, R4

fae_ret:
    MOVD R4, ret0+48(FP)
    SUB $1, R5, R12 // calculate final depth (R5 - 1)
    MOVD R12, ret1+56(FP) // return final depth
    RET
