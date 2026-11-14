#include "textflag.h"

// func findBlockMasksAVX2(src *byte) (quoteMask uint32, slashMask uint32)
// src: +0(FP) (8 bytes)
// Returns: AX = quoteMask, DX = slashMask
TEXT ·findBlockMasksAVX2(SB), NOSPLIT, $0-8

    MOVQ src+0(FP), SI

    // Load 32 bytes into YMM0
    VMOVDQU (SI), Y0

    // Broadcast quote byte (0x22) to YMM1
    MOVQ $0x2222222222222222, R8
    MOVQ R8, X9
    VPBROADCASTQ X9, Y1

    // Compare with quote: YMM2 = (Y0 == 0x22) per byte
    VPCMPEQB Y1, Y0, Y2
    VPMOVMSKB Y2, AX   // quoteMask in AX

    // Broadcast backslash byte (0x5C) to YMM3
    MOVQ $0x5c5c5c5c5c5c5c5c, R8
    MOVQ R8, X10
    VPBROADCASTQ X10, Y3

    // Compare with backslash: YMM4 = (Y0 == 0x5C) per byte
    VPCMPEQB Y3, Y0, Y4
    VPMOVMSKB Y4, DX   // slashMask in DX

    VZEROUPPER
    RET
