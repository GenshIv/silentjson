#include "textflag.h"

// func findQuoteAsm(data []byte) (index int)
TEXT ·findQuoteAsm(SB), NOSPLIT, $0-32
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD $0, R2 // index = 0

loop:
    CMP R2, R1
    BGE not_found
    ADD R2, R0, R12
    MOVBU (R12), R3 // load 1 byte
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

// func skipSpaceASM(data []byte, start int) int
TEXT ·skipSpaceASM(SB), NOSPLIT, $0-40
    MOVD data_base+0(FP), R0
    MOVD data_len+8(FP), R1
    MOVD start+24(FP), R2

skip_loop:
    CMP R2, R1
    BGE skip_eof
    
    ADD R2, R0, R12
    MOVBU (R12), R3
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
