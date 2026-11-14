#include "textflag.h"

// escTableASM maps the byte following a backslash to its decoded value for
// the simple (single-byte) JSON escapes; 0 means "not a simple escape".
// Byte-identical to escTable in scalar.go - TestParseStringBlockEscTable
// pins the two together.
//
//   0x22 '"' -> 0x22   0x2f '/'  -> 0x2f   0x5c '\' -> 0x5c
//   0x62 'b' -> 0x08   0x66 'f'  -> 0x0c   0x6e 'n' -> 0x0a
//   0x72 'r' -> 0x0d   0x74 't'  -> 0x09
DATA escTableASM<>+0x00(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x08(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x10(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x18(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x20(SB)/8, $0x0000000000220000 // 0x22 '"'
DATA escTableASM<>+0x28(SB)/8, $0x2f00000000000000 // 0x2f '/'
DATA escTableASM<>+0x30(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x38(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x40(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x48(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x50(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x58(SB)/8, $0x0000005c00000000 // 0x5c '\'
DATA escTableASM<>+0x60(SB)/8, $0x000c000000080000 // 0x62 'b', 0x66 'f'
DATA escTableASM<>+0x68(SB)/8, $0x000a000000000000 // 0x6e 'n'
DATA escTableASM<>+0x70(SB)/8, $0x00000009000d0000 // 0x72 'r', 0x74 't'
DATA escTableASM<>+0x78(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x80(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x88(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x90(SB)/8, $0x0000000000000000
DATA escTableASM<>+0x98(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xa0(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xa8(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xb0(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xb8(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xc0(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xc8(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xd0(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xd8(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xe0(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xe8(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xf0(SB)/8, $0x0000000000000000
DATA escTableASM<>+0xf8(SB)/8, $0x0000000000000000
GLOBL escTableASM<>(SB), RODATA|NOPTR, $256

// func parseStringBlockAVX2(src *byte, dst *byte) (consumed int, written int, status int)
//
// Decodes one 32-byte block of JSON string content: plain bytes are copied
// with 32-byte vector stores, simple escapes are folded through escTableASM,
// and the routine stops at the closing quote or at an escape that only Go can
// decode. See parseStringBlockScalar (scalar.go) for the reference semantics.
//
// Register map inside the loop:
//   SI  src        DI  dst        R12 stack copy of the block
//   R8  pos (read cursor in the block)
//   R9  o   (write cursor in dst)
//   R10 quoteMask  R11 mask (quote|slash, bits below pos cleared)
//   BX  k (index of the next special byte)
//
// The 64-byte local frame holds a copy of the block so that the run copy can
// load 32 bytes from an arbitrary offset without reading past src+31: a
// VMOVDQU from src+pos with pos up to 31 would touch src[62].
TEXT ·parseStringBlockAVX2(SB), NOSPLIT, $64-40
	MOVQ src+0(FP), SI
	MOVQ dst+8(FP), DI

	VMOVDQU (SI), Y0

	// quoteMask = bytes equal to '"' (0x22)
	MOVQ $0x2222222222222222, AX
	MOVQ AX, X1
	VPBROADCASTQ X1, Y1
	VPCMPEQB Y1, Y0, Y2
	VPMOVMSKB Y2, R10

	// slashMask = bytes equal to '\' (0x5c)
	MOVQ $0x5c5c5c5c5c5c5c5c, AX
	MOVQ AX, X3
	VPBROADCASTQ X3, Y3
	VPCMPEQB Y3, Y0, Y4
	VPMOVMSKB Y4, R11

	ORL R10, R11 // mask = quoteMask | slashMask

	// Fast path: nothing special in the block, copy it verbatim.
	TESTL R11, R11
	JNZ   setup
	VMOVDQU Y0, (DI)
	MOVQ    $32, consumed+16(FP)
	MOVQ    $32, written+24(FP)
	MOVQ    $0, status+32(FP)
	VZEROUPPER
	RET

setup:
	LEAQ block-64(SP), R12
	VMOVDQU Y0, (R12)
	XORL R8, R8 // pos = 0
	XORL R9, R9 // o = 0

loop:
	TESTL R11, R11
	JZ    tail_run

	BSFL R11, BX // k = index of the next '"' or '\'

	// Copy the plain run src[pos:k]. The store is a full 32-byte vector; the
	// bytes past the run are overwritten by the next store or ignored by the
	// caller, which is why dst needs 64 bytes of headroom.
	VMOVDQU (R12)(R8*1), Y5
	VMOVDQU Y5, (DI)(R9*1)
	MOVQ    BX, AX
	SUBQ    R8, AX
	ADDQ    AX, R9 // o += k - pos

	// Closing quote?
	MOVL  R10, AX
	MOVQ  BX, CX
	SHRL  CX, AX
	TESTL $1, AX
	JNZ   is_quote

	// A backslash on the last byte of the block: its partner is in the next
	// block, so hand the escape to Go.
	CMPQ BX, $31
	JEQ  is_defer

	// d = escTableASM[src[k+1]]; zero means \u or an invalid escape.
	MOVBLZX 1(SI)(BX*1), AX
	LEAQ    escTableASM<>(SB), CX
	MOVBLZX (CX)(AX*1), AX
	TESTB   AX, AX
	JZ      is_defer

	MOVB AX, (DI)(R9*1)
	INCQ R9

	LEAQ 2(BX), R8 // pos = k + 2
	CMPQ R8, $32
	JEQ  is_eob

	// Drop every mask bit below the new pos. This also clears the bit of an
	// escaped '"' or '\', which must not be seen as a terminator.
	MOVL $-1, AX
	MOVQ R8, CX
	SHLL CX, AX
	ANDL AX, R11
	JMP  loop

tail_run:
	// No specials left: copy src[pos:32] and report the block as exhausted.
	VMOVDQU (R12)(R8*1), Y5
	VMOVDQU Y5, (DI)(R9*1)
	MOVQ    $32, AX
	SUBQ    R8, AX
	ADDQ    AX, R9

is_eob:
	MOVQ $32, consumed+16(FP)
	MOVQ R9, written+24(FP)
	MOVQ $0, status+32(FP)
	VZEROUPPER
	RET

is_quote:
	LEAQ 1(BX), AX
	MOVQ AX, consumed+16(FP) // consumed includes the quote byte
	MOVQ R9, written+24(FP)  // written does not
	MOVQ $1, status+32(FP)
	VZEROUPPER
	RET

is_defer:
	MOVQ BX, consumed+16(FP) // stop ON the backslash
	MOVQ R9, written+24(FP)
	MOVQ $2, status+32(FP)
	VZEROUPPER
	RET
