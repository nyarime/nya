#include "textflag.h"

// func zstdMatchLen(src []byte, pos, matchPos int) int
//
// Returns the number of matching bytes starting at src[pos] vs src[matchPos].
// Uses AVX2 VPCMPEQB to compare 32 bytes at a time.
// Falls back to scalar if < 32 bytes remain or no AVX2.
TEXT ·zstdMatchLen(SB), NOSPLIT, $0-48
	// Args: src base+len+cap (24), pos (8), matchPos (8) → ret (8)
	MOVQ src_base+0(FP), SI   // SI = &src[0]
	MOVQ src_len+8(FP), DX    // DX = len(src)
	MOVQ pos+24(FP), R8       // R8 = pos
	MOVQ matchPos+32(FP), R9  // R9 = matchPos

	// Compute max match length = min(len-pos, len-matchPos)
	MOVQ DX, CX
	SUBQ R8, CX       // CX = len - pos
	MOVQ DX, R10
	SUBQ R9, R10      // R10 = len - matchPos
	CMPQ R10, CX
	CMOVQLT R10, CX   // CX = min(len-pos, len-matchPos) = maxLen

	// Set up pointers
	LEAQ (SI)(R8*1), R11   // R11 = &src[pos]
	LEAQ (SI)(R9*1), R12   // R12 = &src[matchPos]
	XORQ AX, AX           // AX = matched count = 0

	CMPQ CX, $32
	JB scalar

avx2_loop:
	VMOVDQU (R11)(AX*1), Y0
	VMOVDQU (R12)(AX*1), Y1
	VPCMPEQB Y0, Y1, Y2
	VPMOVMSKB Y2, R13
	CMPL R13, $0xFFFFFFFF
	JNE avx2_mismatch

	ADDQ $32, AX
	LEAQ -32(CX), R14
	CMPQ AX, R14
	JBE avx2_loop
	JMP scalar

avx2_mismatch:
	// R13 has bitmask, find first 0 bit = first mismatch
	NOTL R13
	BSFL R13, R13
	ADDQ R13, AX
	// Clamp to maxLen
	CMPQ AX, CX
	CMOVQGT CX, AX
	MOVQ AX, ret+40(FP)
	VZEROUPPER
	RET

scalar:
	CMPQ AX, CX
	JGE done
	MOVB (R11)(AX*1), R13
	CMPB R13, (R12)(AX*1)
	JNE done
	INCQ AX
	JMP scalar

done:
	MOVQ AX, ret+40(FP)
	VZEROUPPER
	RET
