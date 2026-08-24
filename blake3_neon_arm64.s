#include "textflag.h"

// func blake3CompressNEON(state *[16]uint32, msg *[16]uint32, cv *[8]uint32, counter uint64, blockLen uint32, flags uint32)
//
// ARM64 NEON BLAKE3 compression: 4 parallel G operations using 128-bit V registers.
// State rows: V0=row0(a) V1=row1(b) V2=row2(c) V3=row3(d)
// Message pairs: V10(mx) V11(my)
// Temps: V8 for rotations
// Saved cv: V12, V13

// ROTR32(N, Vreg, Vtmp) rotates each uint32 in Vreg right by N bits
#define ROTR32(N, REG, TMP) \
	VUSHR $(N), REG.S4, TMP.S4;    \
	VSHL  $(32-(N)), REG.S4, REG.S4; \
	VORR  TMP.B16, REG.B16, REG.B16

TEXT ·blake3CompressNEON(SB), NOSPLIT, $64-44
	MOVD	state+0(FP), R0      // *[16]uint32 output
	MOVD	msg+8(FP), R1        // *[16]uint32 message
	MOVD	cv+16(FP), R2        // *[8]uint32 chaining value
	MOVD	counter+24(FP), R3   // uint64 counter
	MOVW	blockLen+32(FP), R4  // uint32
	MOVW	flags+36(FP), R5     // uint32

	// Load chaining value into rows
	VLD1	(R2), [V0.S4, V1.S4]  // row0=cv[0..3], row1=cv[4..7]

	// Row 2 = IV constants
	MOVW	$0x6A09E667, R6
	MOVW	$0xBB67AE85, R7
	MOVW	$0x3C6EF372, R8
	MOVW	$0xA54FF53A, R9
	VMOV	R6, V2.S[0]
	VMOV	R7, V2.S[1]
	VMOV	R8, V2.S[2]
	VMOV	R9, V2.S[3]

	// Row 3 = [counter_lo, counter_hi, blockLen, flags]
	MOVW	R3, R6               // counter lo
	LSR	$32, R3, R7          // counter hi
	VMOV	R6, V3.S[0]
	VMOV	R7, V3.S[1]
	VMOV	R4, V3.S[2]
	VMOV	R5, V3.S[3]

	// Save cv for finalize
	VMOV	V0.B16, V12.B16
	VMOV	V1.B16, V13.B16

	// Permutation table base
	MOVD	$·blake3PermRounds(SB), R10
	MOVW	$0, R11              // round counter

round_loop:
	// R12 = &perm[round]
	MOVW	R11, R12
	LSL	$4, R12
	ADD	R10, R12, R12

	// Load column mx: [msg[p[0]], msg[p[2]], msg[p[4]], msg[p[6]]]
	MOVBU	(R12), R6;          MOVW (R1)(R6<<2), R13; MOVW R13, 0(RSP)
	MOVBU	2(R12), R6;         MOVW (R1)(R6<<2), R13; MOVW R13, 4(RSP)
	MOVBU	4(R12), R6;         MOVW (R1)(R6<<2), R13; MOVW R13, 8(RSP)
	MOVBU	6(R12), R6;         MOVW (R1)(R6<<2), R13; MOVW R13, 12(RSP)
	VLD1	(RSP), [V10.S4]

	// Load column my
	MOVBU	1(R12), R6;         MOVW (R1)(R6<<2), R13; MOVW R13, 0(RSP)
	MOVBU	3(R12), R6;         MOVW (R1)(R6<<2), R13; MOVW R13, 4(RSP)
	MOVBU	5(R12), R6;         MOVW (R1)(R6<<2), R13; MOVW R13, 8(RSP)
	MOVBU	7(R12), R6;         MOVW (R1)(R6<<2), R13; MOVW R13, 12(RSP)
	VLD1	(RSP), [V11.S4]

	// === COLUMN STEP ===
	VADD	V1.S4, V0.S4, V0.S4
	VADD	V10.S4, V0.S4, V0.S4
	VEOR	V0.B16, V3.B16, V3.B16
	ROTR32(16, V3, V8)
	VADD	V3.S4, V2.S4, V2.S4
	VEOR	V2.B16, V1.B16, V1.B16
	ROTR32(12, V1, V8)
	VADD	V1.S4, V0.S4, V0.S4
	VADD	V11.S4, V0.S4, V0.S4
	VEOR	V0.B16, V3.B16, V3.B16
	ROTR32(8, V3, V8)
	VADD	V3.S4, V2.S4, V2.S4
	VEOR	V2.B16, V1.B16, V1.B16
	ROTR32(7, V1, V8)

	// === DIAGONAL ROTATION ===
	// Row1 rotate left by 1: VEXT #4 (4 bytes = 1 element)
	VEXT	$4, V1.B16, V1.B16, V1.B16
	// Row2 rotate left by 2: VEXT #8
	VEXT	$8, V2.B16, V2.B16, V2.B16
	// Row3 rotate left by 3 = right by 1: VEXT #12
	VEXT	$12, V3.B16, V3.B16, V3.B16

	// Load diagonal mx
	MOVBU	8(R12), R6;         MOVW (R1)(R6<<2), R13; MOVW R13, 0(RSP)
	MOVBU	10(R12), R6;        MOVW (R1)(R6<<2), R13; MOVW R13, 4(RSP)
	MOVBU	12(R12), R6;        MOVW (R1)(R6<<2), R13; MOVW R13, 8(RSP)
	MOVBU	14(R12), R6;        MOVW (R1)(R6<<2), R13; MOVW R13, 12(RSP)
	VLD1	(RSP), [V10.S4]

	// Load diagonal my
	MOVBU	9(R12), R6;         MOVW (R1)(R6<<2), R13; MOVW R13, 0(RSP)
	MOVBU	11(R12), R6;        MOVW (R1)(R6<<2), R13; MOVW R13, 4(RSP)
	MOVBU	13(R12), R6;        MOVW (R1)(R6<<2), R13; MOVW R13, 8(RSP)
	MOVBU	15(R12), R6;        MOVW (R1)(R6<<2), R13; MOVW R13, 12(RSP)
	VLD1	(RSP), [V11.S4]

	// === DIAGONAL STEP ===
	VADD	V1.S4, V0.S4, V0.S4
	VADD	V10.S4, V0.S4, V0.S4
	VEOR	V0.B16, V3.B16, V3.B16
	ROTR32(16, V3, V8)
	VADD	V3.S4, V2.S4, V2.S4
	VEOR	V2.B16, V1.B16, V1.B16
	ROTR32(12, V1, V8)
	VADD	V1.S4, V0.S4, V0.S4
	VADD	V11.S4, V0.S4, V0.S4
	VEOR	V0.B16, V3.B16, V3.B16
	ROTR32(8, V3, V8)
	VADD	V3.S4, V2.S4, V2.S4
	VEOR	V2.B16, V1.B16, V1.B16
	ROTR32(7, V1, V8)

	// === UN-ROTATE ===
	VEXT	$12, V1.B16, V1.B16, V1.B16  // row1 right by 1
	VEXT	$8, V2.B16, V2.B16, V2.B16   // row2 right by 2
	VEXT	$4, V3.B16, V3.B16, V3.B16   // row3 right by 3

	ADD	$1, R11
	CMP	$7, R11
	BLT	round_loop

	// === FINALIZE ===
	VEOR	V2.B16, V0.B16, V0.B16
	VEOR	V3.B16, V1.B16, V1.B16
	VEOR	V12.B16, V2.B16, V2.B16
	VEOR	V13.B16, V3.B16, V3.B16

	// Store
	VST1	[V0.S4, V1.S4, V2.S4, V3.S4], (R0)
	RET
