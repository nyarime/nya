#include "textflag.h"

// func zstdMatchLen(src []byte, pos, matchPos int) int
//
// Returns the number of matching bytes starting at src[pos] vs src[matchPos].
// Uses ARM64 NEON to compare 16 bytes at a time.
TEXT ·zstdMatchLen(SB), NOSPLIT, $0-56
	MOVD src_base+0(FP), R0   // R0 = &src[0]
	MOVD src_len+8(FP), R1    // R1 = len(src)
	MOVD pos+24(FP), R2       // R2 = pos
	MOVD matchPos+32(FP), R3  // R3 = matchPos

	// maxLen = min(len-pos, len-matchPos)
	SUB R2, R1, R4             // R4 = len - pos
	MOVD src_len+8(FP), R5
	SUB R3, R5, R5             // R5 = len - matchPos
	CMP R5, R4
	CSEL LT, R5, R4, R4       // R4 = maxLen

	// Set up base pointers: R6 = &src[pos], R7 = &src[matchPos]
	ADD R0, R2, R6
	ADD R0, R3, R7
	MOVD ZR, R8               // R8 = matched count

	// Need at least 16 bytes for NEON
	CMP $16, R4
	BLT scalar

neon_loop:
	// Compute current addresses
	ADD R6, R8, R9             // R9 = &src[pos + matched]
	ADD R7, R8, R10            // R10 = &src[matchPos + matched]

	VLD1 (R9), [V0.B16]
	VLD1 (R10), [V1.B16]

	// Compare: V2[i] = 0xFF where equal, 0x00 where not
	VCMEQ V0.B16, V1.B16, V2.B16

	// UMINV B16, V2.16B → minimum byte into V16
	WORD $0x6E31AA50            // UMINV B16, V2.16B
	VMOV V16.B[0], R9

	CMP $0xFF, R9
	BNE neon_mismatch

	ADD $16, R8
	// Check if we have 16 more bytes
	SUB R8, R4, R10
	CMP $16, R10
	BGE neon_loop
	B scalar

neon_mismatch:
	// Find first mismatch byte in this 16-byte block
	ADD R6, R8, R9
	ADD R7, R8, R10
	SUB R8, R4, R11            // remaining
	MOVD $16, R12
	CMP R11, R12
	CSEL LT, R11, R12, R12    // R12 = min(16, remaining)
	MOVD ZR, R13               // offset in block

find_byte:
	CMP R13, R12
	BEQ found_end
	MOVBU (R9)(R13), R14
	MOVBU (R10)(R13), R15
	CMP R14, R15
	BNE found_end
	ADD $1, R13
	B find_byte

found_end:
	ADD R13, R8
	B done

scalar:
	CMP R8, R4
	BGE done
	ADD R6, R8, R9
	ADD R7, R8, R10
	MOVBU (R9), R11
	MOVBU (R10), R12
	CMP R11, R12
	BNE done
	ADD $1, R8
	B scalar

done:
	MOVD R8, ret+40(FP)
	RET
