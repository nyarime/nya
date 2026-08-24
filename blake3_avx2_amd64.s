#include "textflag.h"

// AVX2 rotation macro: right-rotate 32-bit lanes by N bits.
// Uses one temp register. Destroys REG (replaced with rotated value).
#define VROTR(N, REG, TMP) \
    VPSRLD $(N), REG, TMP;      \
    VPSLLD $(32-(N)), REG, REG;  \
    VPOR   TMP, REG, REG

// blake3Compress2 processes 2 independent BLAKE3 compressions using AVX2.
// YMM layout: low 128 bits = compress 0, high 128 bits = compress 1.
//
// func blake3Compress2(
//     results *[2][16]uint32,
//     msgs    *[2][16]uint32,
//     cvs     *[2][8]uint32,
//     counter0 uint64, counter1 uint64,
//     blockLen uint32, flags uint32,
// )
TEXT ·blake3Compress2(SB), NOSPLIT, $128-48
    MOVQ    results+0(FP), AX
    MOVQ    msgs+8(FP), BX
    MOVQ    cvs+16(FP), CX

    // ---- Load state rows into Y0-Y3 ----

    // Row 0: cv[0..3] for each compress
    VMOVDQU (CX), X0               // cv0[0..3]
    VINSERTI128 $1, 32(CX), Y0, Y0 // cv1[0..3]

    // Row 1: cv[4..7] for each compress
    VMOVDQU 16(CX), X1             // cv0[4..7]
    VINSERTI128 $1, 48(CX), Y1, Y1 // cv1[4..7]

    // Row 2: IV (identical both lanes)
    MOVL    $0x6A09E667, 0(SP)
    MOVL    $0xBB67AE85, 4(SP)
    MOVL    $0x3C6EF372, 8(SP)
    MOVL    $0xA54FF53A, 12(SP)
    VMOVDQU 0(SP), X2
    VINSERTI128 $1, 0(SP), Y2, Y2

    // Row 3: [counter_lo, counter_hi, blockLen, flags] per lane
    MOVQ    counter0+24(FP), DX
    MOVL    DX, 16(SP)
    SHRQ    $32, DX
    MOVL    DX, 20(SP)
    MOVL    blockLen+40(FP), SI
    MOVL    SI, 24(SP)
    MOVL    flags+44(FP), DI
    MOVL    DI, 28(SP)
    VMOVDQU 16(SP), X3

    MOVQ    counter1+32(FP), DX
    MOVL    DX, 32(SP)
    SHRQ    $32, DX
    MOVL    DX, 36(SP)
    MOVL    SI, 40(SP)
    MOVL    DI, 44(SP)
    VINSERTI128 $1, 32(SP), Y3, Y3

    // Save original CVs for finalization
    VMOVDQA Y0, Y12
    VMOVDQA Y1, Y13

    // ---- Round loop ----
    LEAQ    ·blake3PermRounds(SB), R12
    XORL    R13, R13            // round counter

avx2_round:
    MOVQ    R13, R14
    SHLQ    $4, R14
    ADDQ    R12, R14            // R14 = &blake3PermRounds[round]

    // ======== Column step: G on columns (0,4,8,12), (1,5,9,13), (2,6,10,14), (3,7,11,15) ========

    // Build mx = [msg0[p[0]], msg0[p[2]], msg0[p[4]], msg0[p[6]] | msg1[...]]
    MOVBLZX 0(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 48(SP)
    MOVBLZX 2(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 52(SP)
    MOVBLZX 4(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 56(SP)
    MOVBLZX 6(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 60(SP)
    VMOVDQU 48(SP), X10
    MOVBLZX 0(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 48(SP)
    MOVBLZX 2(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 52(SP)
    MOVBLZX 4(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 56(SP)
    MOVBLZX 6(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 60(SP)
    VINSERTI128 $1, 48(SP), Y10, Y10

    // Build my = [msg0[p[1]], msg0[p[3]], msg0[p[5]], msg0[p[7]] | msg1[...]]
    MOVBLZX 1(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 48(SP)
    MOVBLZX 3(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 52(SP)
    MOVBLZX 5(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 56(SP)
    MOVBLZX 7(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 60(SP)
    VMOVDQU 48(SP), X11
    MOVBLZX 1(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 48(SP)
    MOVBLZX 3(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 52(SP)
    MOVBLZX 5(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 56(SP)
    MOVBLZX 7(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 60(SP)
    VINSERTI128 $1, 48(SP), Y11, Y11

    // Column G
    VPADDD  Y1, Y0, Y0          // a += b
    VPADDD  Y10, Y0, Y0         // a += mx
    VPXOR   Y0, Y3, Y3          // d ^= a
    VROTR(16, Y3, Y8)           // d >>>= 16
    VPADDD  Y3, Y2, Y2          // c += d
    VPXOR   Y2, Y1, Y1          // b ^= c
    VROTR(12, Y1, Y8)           // b >>>= 12
    VPADDD  Y1, Y0, Y0          // a += b
    VPADDD  Y11, Y0, Y0         // a += my
    VPXOR   Y0, Y3, Y3          // d ^= a
    VROTR(8, Y3, Y8)            // d >>>= 8
    VPADDD  Y3, Y2, Y2          // c += d
    VPXOR   Y2, Y1, Y1          // b ^= c
    VROTR(7, Y1, Y8)            // b >>>= 7

    // Diagonal rotation (VPSHUFD operates per 128-bit lane)
    VPSHUFD $0x39, Y1, Y1       // row1 left-rotate 1
    VPSHUFD $0x4E, Y2, Y2       // row2 left-rotate 2
    VPSHUFD $0x93, Y3, Y3       // row3 left-rotate 3

    // ======== Diagonal step: G on diags (0,5,10,15), (1,6,11,12), (2,7,8,13), (3,4,9,14) ========

    // Build diagonal mx = [msg0[p[8]], msg0[p[10]], msg0[p[12]], msg0[p[14]] | msg1[...]]
    MOVBLZX 8(R14),  R15; MOVL (BX)(R15*4), R8;    MOVL R8, 48(SP)
    MOVBLZX 10(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 52(SP)
    MOVBLZX 12(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 56(SP)
    MOVBLZX 14(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 60(SP)
    VMOVDQU 48(SP), X10
    MOVBLZX 8(R14),  R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 48(SP)
    MOVBLZX 10(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 52(SP)
    MOVBLZX 12(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 56(SP)
    MOVBLZX 14(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 60(SP)
    VINSERTI128 $1, 48(SP), Y10, Y10

    // Build diagonal my = [msg0[p[9]], msg0[p[11]], msg0[p[13]], msg0[p[15]] | msg1[...]]
    MOVBLZX 9(R14),  R15; MOVL (BX)(R15*4), R8;    MOVL R8, 48(SP)
    MOVBLZX 11(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 52(SP)
    MOVBLZX 13(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 56(SP)
    MOVBLZX 15(R14), R15; MOVL (BX)(R15*4), R8;    MOVL R8, 60(SP)
    VMOVDQU 48(SP), X11
    MOVBLZX 9(R14),  R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 48(SP)
    MOVBLZX 11(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 52(SP)
    MOVBLZX 13(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 56(SP)
    MOVBLZX 15(R14), R15; MOVL 64(BX)(R15*4), R8;  MOVL R8, 60(SP)
    VINSERTI128 $1, 48(SP), Y11, Y11

    // Diagonal G
    VPADDD  Y1, Y0, Y0
    VPADDD  Y10, Y0, Y0
    VPXOR   Y0, Y3, Y3
    VROTR(16, Y3, Y8)
    VPADDD  Y3, Y2, Y2
    VPXOR   Y2, Y1, Y1
    VROTR(12, Y1, Y8)
    VPADDD  Y1, Y0, Y0
    VPADDD  Y11, Y0, Y0
    VPXOR   Y0, Y3, Y3
    VROTR(8, Y3, Y8)
    VPADDD  Y3, Y2, Y2
    VPXOR   Y2, Y1, Y1
    VROTR(7, Y1, Y8)

    // Un-rotate diagonals
    VPSHUFD $0x93, Y1, Y1       // row1 right-rotate 1
    VPSHUFD $0x4E, Y2, Y2       // row2 right-rotate 2
    VPSHUFD $0x39, Y3, Y3       // row3 right-rotate 3

    INCL    R13
    CMPL    R13, $7
    JL      avx2_round

    // ---- Finalize: XOR state with saved CVs ----
    VPXOR   Y2, Y0, Y0          // state[0..3] ^= state[8..11]
    VPXOR   Y3, Y1, Y1          // state[4..7] ^= state[12..15]
    VPXOR   Y12, Y2, Y2         // state[8..11] ^= orig_cv[0..3]
    VPXOR   Y13, Y3, Y3         // state[12..15] ^= orig_cv[4..7]

    // ---- Store results ----
    // results[0] = low lanes, results[1] = high lanes
    // results is *[2][16]uint32 = 128 bytes: [0..63] = compress0, [64..127] = compress1
    VEXTRACTI128 $0, Y0, X14
    VMOVDQU X14, (AX)           // result0[0..3]
    VEXTRACTI128 $0, Y1, X14
    VMOVDQU X14, 16(AX)         // result0[4..7]
    VEXTRACTI128 $0, Y2, X14
    VMOVDQU X14, 32(AX)         // result0[8..11]
    VEXTRACTI128 $0, Y3, X14
    VMOVDQU X14, 48(AX)         // result0[12..15]

    VEXTRACTI128 $1, Y0, X14
    VMOVDQU X14, 64(AX)         // result1[0..3]
    VEXTRACTI128 $1, Y1, X14
    VMOVDQU X14, 80(AX)         // result1[4..7]
    VEXTRACTI128 $1, Y2, X14
    VMOVDQU X14, 96(AX)         // result1[8..11]
    VEXTRACTI128 $1, Y3, X14
    VMOVDQU X14, 112(AX)        // result1[12..15]

    VZEROUPPER
    RET

// blake3Compress2T — pre-transposed message variant of blake3Compress2.
// tmsgs is *[2][112]uint32 = 896 bytes: tmsg0 at +0, tmsg1 at +448
// Each tmsg: [7 rounds][col_mx(16B), col_my(16B), diag_mx(16B), diag_my(16B)]
TEXT ·blake3Compress2T(SB), NOSPLIT, $64-48
    MOVQ    results+0(FP), AX
    MOVQ    tmsgs+8(FP), BX
    MOVQ    cvs+16(FP), CX

    // Row 0: cv[0..3]
    VMOVDQU (CX), X0
    VINSERTI128 $1, 32(CX), Y0, Y0

    // Row 1: cv[4..7]
    VMOVDQU 16(CX), X1
    VINSERTI128 $1, 48(CX), Y1, Y1

    // Row 2: IV
    MOVL    $0x6A09E667, 0(SP)
    MOVL    $0xBB67AE85, 4(SP)
    MOVL    $0x3C6EF372, 8(SP)
    MOVL    $0xA54FF53A, 12(SP)
    VMOVDQU 0(SP), X2
    VINSERTI128 $1, 0(SP), Y2, Y2

    // Row 3: counters/blockLen/flags
    MOVQ    counter0+24(FP), DX
    MOVL    DX, 16(SP)
    SHRQ    $32, DX
    MOVL    DX, 20(SP)
    MOVL    blockLen+40(FP), SI
    MOVL    SI, 24(SP)
    MOVL    flags+44(FP), DI
    MOVL    DI, 28(SP)
    VMOVDQU 16(SP), X3

    MOVQ    counter1+32(FP), DX
    MOVL    DX, 32(SP)
    SHRQ    $32, DX
    MOVL    DX, 36(SP)
    MOVL    SI, 40(SP)
    MOVL    DI, 44(SP)
    VINSERTI128 $1, 32(SP), Y3, Y3

    VMOVDQA Y0, Y12
    VMOVDQA Y1, Y13

    XORL    R13, R13

avx2_round_t:
    MOVQ    R13, R14
    SHLQ    $6, R14        // R14 = round * 64

    // Load col_mx: low=tmsg0, high=tmsg1
    VMOVDQU (BX)(R14*1), X10
    VINSERTI128 $1, 448(BX)(R14*1), Y10, Y10

    // Load col_my
    VMOVDQU 16(BX)(R14*1), X11
    VINSERTI128 $1, 464(BX)(R14*1), Y11, Y11

    // Column G
    VPADDD  Y1, Y0, Y0
    VPADDD  Y10, Y0, Y0
    VPXOR   Y0, Y3, Y3
    VROTR(16, Y3, Y8)
    VPADDD  Y3, Y2, Y2
    VPXOR   Y2, Y1, Y1
    VROTR(12, Y1, Y8)
    VPADDD  Y1, Y0, Y0
    VPADDD  Y11, Y0, Y0
    VPXOR   Y0, Y3, Y3
    VROTR(8, Y3, Y8)
    VPADDD  Y3, Y2, Y2
    VPXOR   Y2, Y1, Y1
    VROTR(7, Y1, Y8)

    // Diagonal rotation
    VPSHUFD $0x39, Y1, Y1
    VPSHUFD $0x4E, Y2, Y2
    VPSHUFD $0x93, Y3, Y3

    // Load diag_mx
    VMOVDQU 32(BX)(R14*1), X10
    VINSERTI128 $1, 480(BX)(R14*1), Y10, Y10

    // Load diag_my
    VMOVDQU 48(BX)(R14*1), X11
    VINSERTI128 $1, 496(BX)(R14*1), Y11, Y11

    // Diagonal G
    VPADDD  Y1, Y0, Y0
    VPADDD  Y10, Y0, Y0
    VPXOR   Y0, Y3, Y3
    VROTR(16, Y3, Y8)
    VPADDD  Y3, Y2, Y2
    VPXOR   Y2, Y1, Y1
    VROTR(12, Y1, Y8)
    VPADDD  Y1, Y0, Y0
    VPADDD  Y11, Y0, Y0
    VPXOR   Y0, Y3, Y3
    VROTR(8, Y3, Y8)
    VPADDD  Y3, Y2, Y2
    VPXOR   Y2, Y1, Y1
    VROTR(7, Y1, Y8)

    // Un-rotate
    VPSHUFD $0x93, Y1, Y1
    VPSHUFD $0x4E, Y2, Y2
    VPSHUFD $0x39, Y3, Y3

    INCL    R13
    CMPL    R13, $7
    JL      avx2_round_t

    // Finalize
    VPXOR   Y2, Y0, Y0
    VPXOR   Y3, Y1, Y1
    VPXOR   Y12, Y2, Y2
    VPXOR   Y13, Y3, Y3

    // Store results
    VEXTRACTI128 $0, Y0, X14
    VMOVDQU X14, (AX)
    VEXTRACTI128 $0, Y1, X14
    VMOVDQU X14, 16(AX)
    VEXTRACTI128 $0, Y2, X14
    VMOVDQU X14, 32(AX)
    VEXTRACTI128 $0, Y3, X14
    VMOVDQU X14, 48(AX)

    VEXTRACTI128 $1, Y0, X14
    VMOVDQU X14, 64(AX)
    VEXTRACTI128 $1, Y1, X14
    VMOVDQU X14, 80(AX)
    VEXTRACTI128 $1, Y2, X14
    VMOVDQU X14, 96(AX)
    VEXTRACTI128 $1, Y3, X14
    VMOVDQU X14, 112(AX)

    VZEROUPPER
    RET
