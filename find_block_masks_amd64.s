#include "textflag.h"

// func findBlockMasksAVX2(src *byte) (quoteMask uint32, slashMask uint32)
// src: +0(FP) (8 bytes)
// Returns: AX = quoteMask, BX = slashMask
TEXT ·findBlockMasksAVX2(SB), NOSPLIT, $0-8

    MOVQ src+0(FP), SI

    // Load 32 bytes into YMM0
    VMOVDQU (SI), Y0

    // Create quote byte pattern using VPINSRB + VPBROADCASTB
    // Byte value for '"' is 0x22
    XORPS X9, X9
    MOVQ $0x22, R8
    VPINSRB $0, R8, X9, X1
    VPBROADCASTB X1, Y1

    // Compare with quote: YMM2 = (Y0 == 0x22) per byte
    VPCMPEQB Y1, Y0, Y2

    // Convert mask to integer using VPMOVMSKB
    VPMOVMSKB Y2, AX   // quoteMask in AX

    // Create backslash byte pattern using VPINSRB + VPBROADCASTB
    // Byte value for '\' is 0x5C
    MOVQ $0x5C, R8
    VPINSRB $0, R8, X9, X3
    VPBROADCASTB X3, Y3

    // Compare with backslash: YMM4 = (Y0 == 0x5C) per byte
    VPCMPEQB Y3, Y0, Y4

    // Convert mask to integer using VPMOVMSKB
    VPMOVMSKB Y4, BX   // slashMask in BX

    VZEROUPPER
    RET
