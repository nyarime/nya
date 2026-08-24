#include "textflag.h"

// blake3Compress4 processes 4 independent BLAKE3 compressions using AVX-512.
// ZMM layout: 4 x 128-bit lanes, one per compress.
//   lane 0 (bits 0-127)   = compress 0
//   lane 1 (bits 128-255) = compress 1
//   lane 2 (bits 256-383) = compress 2
//   lane 3 (bits 384-511) = compress 3
//
// Key AVX-512 wins:
//   VPRORD $N, ZMM, ZMM — native 32-bit rotate, no shift+or!
//   VPSHUFD per 128-bit lane — diagonal rotation works unchanged.
//
// func blake3Compress4(
//     results  *[4][16]uint32,   // +0(FP)
//     msgs     *[4][16]uint32,   // +8(FP)
//     cvs      *[4][8]uint32,    // +16(FP)
//     counters *[4]uint64,       // +24(FP)
//     blockLen uint32,           // +32(FP)
//     flags    uint32,           // +36(FP)
// )
TEXT ·blake3Compress4(SB), NOSPLIT, $192-40
    MOVQ    results+0(FP), AX
    MOVQ    msgs+8(FP), BX
    MOVQ    cvs+16(FP), CX
    MOVQ    counters+24(FP), DX

    // ---- Load state rows into Z0-Z3 ----

    // Row 0: cv[0..3] for each of 4 compresses
    // cvs is [4][8]uint32 = 128 bytes: cv0 at +0, cv1 at +32, cv2 at +64, cv3 at +96
    VMOVDQU (CX), X0                       // cv0[0..3]
    VINSERTI32X4 $1, 32(CX), Z0, Z0        // cv1[0..3]
    VINSERTI32X4 $2, 64(CX), Z0, Z0        // cv2[0..3]
    VINSERTI32X4 $3, 96(CX), Z0, Z0        // cv3[0..3]

    // Row 1: cv[4..7] for each compress
    VMOVDQU 16(CX), X1
    VINSERTI32X4 $1, 48(CX), Z1, Z1
    VINSERTI32X4 $2, 80(CX), Z1, Z1
    VINSERTI32X4 $3, 112(CX), Z1, Z1

    // Row 2: IV (identical all 4 lanes)
    MOVL    $0x6A09E667, 0(SP)
    MOVL    $0xBB67AE85, 4(SP)
    MOVL    $0x3C6EF372, 8(SP)
    MOVL    $0xA54FF53A, 12(SP)
    VMOVDQU 0(SP), X2
    VINSERTI32X4 $1, 0(SP), Z2, Z2
    VINSERTI32X4 $2, 0(SP), Z2, Z2
    VINSERTI32X4 $3, 0(SP), Z2, Z2

    // Row 3: [counter_lo, counter_hi, blockLen, flags] per lane
    // counters is *[4]uint64
    MOVL    blockLen+32(FP), SI
    MOVL    flags+36(FP), DI

    // Lane 0
    MOVQ    (DX), R8
    MOVL    R8, 16(SP)
    SHRQ    $32, R8
    MOVL    R8, 20(SP)
    MOVL    SI, 24(SP)
    MOVL    DI, 28(SP)
    VMOVDQU 16(SP), X3

    // Lane 1
    MOVQ    8(DX), R8
    MOVL    R8, 32(SP)
    SHRQ    $32, R8
    MOVL    R8, 36(SP)
    MOVL    SI, 40(SP)
    MOVL    DI, 44(SP)
    VINSERTI32X4 $1, 32(SP), Z3, Z3

    // Lane 2
    MOVQ    16(DX), R8
    MOVL    R8, 48(SP)
    SHRQ    $32, R8
    MOVL    R8, 52(SP)
    MOVL    SI, 56(SP)
    MOVL    DI, 60(SP)
    VINSERTI32X4 $2, 48(SP), Z3, Z3

    // Lane 3
    MOVQ    24(DX), R8
    MOVL    R8, 64(SP)
    SHRQ    $32, R8
    MOVL    R8, 68(SP)
    MOVL    SI, 72(SP)
    MOVL    DI, 76(SP)
    VINSERTI32X4 $3, 64(SP), Z3, Z3

    // Save original CVs for finalization
    VMOVDQU32 Z0, Z12
    VMOVDQU32 Z1, Z13

    // ---- Round loop ----
    LEAQ    ·blake3PermRounds(SB), R12
    XORL    R13, R13

avx512_round:
    MOVQ    R13, R14
    SHLQ    $4, R14
    ADDQ    R12, R14            // R14 = &blake3PermRounds[round]

    // ======== Column step ========
    // Build mx: for each lane, [msg_N[p[0]], msg_N[p[2]], msg_N[p[4]], msg_N[p[6]]]
    // msgs is [4][16]uint32 = 256 bytes: msg0 +0, msg1 +64, msg2 +128, msg3 +192

    // Lane 0 column mx
    MOVBLZX 0(R14), R15; MOVL (BX)(R15*4), R8;      MOVL R8, 80(SP)
    MOVBLZX 2(R14), R15; MOVL (BX)(R15*4), R8;      MOVL R8, 84(SP)
    MOVBLZX 4(R14), R15; MOVL (BX)(R15*4), R8;      MOVL R8, 88(SP)
    MOVBLZX 6(R14), R15; MOVL (BX)(R15*4), R8;      MOVL R8, 92(SP)
    VMOVDQU 80(SP), X10
    // Lane 1
    MOVBLZX 0(R14), R15; MOVL 64(BX)(R15*4), R8;    MOVL R8, 80(SP)
    MOVBLZX 2(R14), R15; MOVL 64(BX)(R15*4), R8;    MOVL R8, 84(SP)
    MOVBLZX 4(R14), R15; MOVL 64(BX)(R15*4), R8;    MOVL R8, 88(SP)
    MOVBLZX 6(R14), R15; MOVL 64(BX)(R15*4), R8;    MOVL R8, 92(SP)
    VINSERTI32X4 $1, 80(SP), Z10, Z10
    // Lane 2
    MOVBLZX 0(R14), R15; MOVL 128(BX)(R15*4), R8;   MOVL R8, 80(SP)
    MOVBLZX 2(R14), R15; MOVL 128(BX)(R15*4), R8;   MOVL R8, 84(SP)
    MOVBLZX 4(R14), R15; MOVL 128(BX)(R15*4), R8;   MOVL R8, 88(SP)
    MOVBLZX 6(R14), R15; MOVL 128(BX)(R15*4), R8;   MOVL R8, 92(SP)
    VINSERTI32X4 $2, 80(SP), Z10, Z10
    // Lane 3
    MOVBLZX 0(R14), R15; MOVL 192(BX)(R15*4), R8;   MOVL R8, 80(SP)
    MOVBLZX 2(R14), R15; MOVL 192(BX)(R15*4), R8;   MOVL R8, 84(SP)
    MOVBLZX 4(R14), R15; MOVL 192(BX)(R15*4), R8;   MOVL R8, 88(SP)
    MOVBLZX 6(R14), R15; MOVL 192(BX)(R15*4), R8;   MOVL R8, 92(SP)
    VINSERTI32X4 $3, 80(SP), Z10, Z10

    // Build my: [msg_N[p[1]], msg_N[p[3]], msg_N[p[5]], msg_N[p[7]]]
    // Lane 0
    MOVBLZX 1(R14), R15; MOVL (BX)(R15*4), R8;      MOVL R8, 80(SP)
    MOVBLZX 3(R14), R15; MOVL (BX)(R15*4), R8;      MOVL R8, 84(SP)
    MOVBLZX 5(R14), R15; MOVL (BX)(R15*4), R8;      MOVL R8, 88(SP)
    MOVBLZX 7(R14), R15; MOVL (BX)(R15*4), R8;      MOVL R8, 92(SP)
    VMOVDQU 80(SP), X11
    // Lane 1
    MOVBLZX 1(R14), R15; MOVL 64(BX)(R15*4), R8;    MOVL R8, 80(SP)
    MOVBLZX 3(R14), R15; MOVL 64(BX)(R15*4), R8;    MOVL R8, 84(SP)
    MOVBLZX 5(R14), R15; MOVL 64(BX)(R15*4), R8;    MOVL R8, 88(SP)
    MOVBLZX 7(R14), R15; MOVL 64(BX)(R15*4), R8;    MOVL R8, 92(SP)
    VINSERTI32X4 $1, 80(SP), Z11, Z11
    // Lane 2
    MOVBLZX 1(R14), R15; MOVL 128(BX)(R15*4), R8;   MOVL R8, 80(SP)
    MOVBLZX 3(R14), R15; MOVL 128(BX)(R15*4), R8;   MOVL R8, 84(SP)
    MOVBLZX 5(R14), R15; MOVL 128(BX)(R15*4), R8;   MOVL R8, 88(SP)
    MOVBLZX 7(R14), R15; MOVL 128(BX)(R15*4), R8;   MOVL R8, 92(SP)
    VINSERTI32X4 $2, 80(SP), Z11, Z11
    // Lane 3
    MOVBLZX 1(R14), R15; MOVL 192(BX)(R15*4), R8;   MOVL R8, 80(SP)
    MOVBLZX 3(R14), R15; MOVL 192(BX)(R15*4), R8;   MOVL R8, 84(SP)
    MOVBLZX 5(R14), R15; MOVL 192(BX)(R15*4), R8;   MOVL R8, 88(SP)
    MOVBLZX 7(R14), R15; MOVL 192(BX)(R15*4), R8;   MOVL R8, 92(SP)
    VINSERTI32X4 $3, 80(SP), Z11, Z11

    // Column G (native VPRORD — no shift+or!)
    VPADDD  Z1, Z0, Z0
    VPADDD  Z10, Z0, Z0
    VPXORD  Z0, Z3, Z3
    VPRORD  $16, Z3, Z3
    VPADDD  Z3, Z2, Z2
    VPXORD  Z2, Z1, Z1
    VPRORD  $12, Z1, Z1
    VPADDD  Z1, Z0, Z0
    VPADDD  Z11, Z0, Z0
    VPXORD  Z0, Z3, Z3
    VPRORD  $8, Z3, Z3
    VPADDD  Z3, Z2, Z2
    VPXORD  Z2, Z1, Z1
    VPRORD  $7, Z1, Z1

    // Diagonal rotation (VPSHUFD per 128-bit lane in ZMM)
    VPSHUFD $0x39, Z1, Z1
    VPSHUFD $0x4E, Z2, Z2
    VPSHUFD $0x93, Z3, Z3

    // ======== Diagonal step ========
    // Build diagonal mx: [msg_N[p[8]], msg_N[p[10]], msg_N[p[12]], msg_N[p[14]]]
    // Lane 0
    MOVBLZX 8(R14),  R15; MOVL (BX)(R15*4), R8;     MOVL R8, 80(SP)
    MOVBLZX 10(R14), R15; MOVL (BX)(R15*4), R8;     MOVL R8, 84(SP)
    MOVBLZX 12(R14), R15; MOVL (BX)(R15*4), R8;     MOVL R8, 88(SP)
    MOVBLZX 14(R14), R15; MOVL (BX)(R15*4), R8;     MOVL R8, 92(SP)
    VMOVDQU 80(SP), X10
    // Lane 1
    MOVBLZX 8(R14),  R15; MOVL 64(BX)(R15*4), R8;   MOVL R8, 80(SP)
    MOVBLZX 10(R14), R15; MOVL 64(BX)(R15*4), R8;   MOVL R8, 84(SP)
    MOVBLZX 12(R14), R15; MOVL 64(BX)(R15*4), R8;   MOVL R8, 88(SP)
    MOVBLZX 14(R14), R15; MOVL 64(BX)(R15*4), R8;   MOVL R8, 92(SP)
    VINSERTI32X4 $1, 80(SP), Z10, Z10
    // Lane 2
    MOVBLZX 8(R14),  R15; MOVL 128(BX)(R15*4), R8;  MOVL R8, 80(SP)
    MOVBLZX 10(R14), R15; MOVL 128(BX)(R15*4), R8;  MOVL R8, 84(SP)
    MOVBLZX 12(R14), R15; MOVL 128(BX)(R15*4), R8;  MOVL R8, 88(SP)
    MOVBLZX 14(R14), R15; MOVL 128(BX)(R15*4), R8;  MOVL R8, 92(SP)
    VINSERTI32X4 $2, 80(SP), Z10, Z10
    // Lane 3
    MOVBLZX 8(R14),  R15; MOVL 192(BX)(R15*4), R8;  MOVL R8, 80(SP)
    MOVBLZX 10(R14), R15; MOVL 192(BX)(R15*4), R8;  MOVL R8, 84(SP)
    MOVBLZX 12(R14), R15; MOVL 192(BX)(R15*4), R8;  MOVL R8, 88(SP)
    MOVBLZX 14(R14), R15; MOVL 192(BX)(R15*4), R8;  MOVL R8, 92(SP)
    VINSERTI32X4 $3, 80(SP), Z10, Z10

    // Build diagonal my: [msg_N[p[9]], msg_N[p[11]], msg_N[p[13]], msg_N[p[15]]]
    // Lane 0
    MOVBLZX 9(R14),  R15; MOVL (BX)(R15*4), R8;     MOVL R8, 80(SP)
    MOVBLZX 11(R14), R15; MOVL (BX)(R15*4), R8;     MOVL R8, 84(SP)
    MOVBLZX 13(R14), R15; MOVL (BX)(R15*4), R8;     MOVL R8, 88(SP)
    MOVBLZX 15(R14), R15; MOVL (BX)(R15*4), R8;     MOVL R8, 92(SP)
    VMOVDQU 80(SP), X11
    // Lane 1
    MOVBLZX 9(R14),  R15; MOVL 64(BX)(R15*4), R8;   MOVL R8, 80(SP)
    MOVBLZX 11(R14), R15; MOVL 64(BX)(R15*4), R8;   MOVL R8, 84(SP)
    MOVBLZX 13(R14), R15; MOVL 64(BX)(R15*4), R8;   MOVL R8, 88(SP)
    MOVBLZX 15(R14), R15; MOVL 64(BX)(R15*4), R8;   MOVL R8, 92(SP)
    VINSERTI32X4 $1, 80(SP), Z11, Z11
    // Lane 2
    MOVBLZX 9(R14),  R15; MOVL 128(BX)(R15*4), R8;  MOVL R8, 80(SP)
    MOVBLZX 11(R14), R15; MOVL 128(BX)(R15*4), R8;  MOVL R8, 84(SP)
    MOVBLZX 13(R14), R15; MOVL 128(BX)(R15*4), R8;  MOVL R8, 88(SP)
    MOVBLZX 15(R14), R15; MOVL 128(BX)(R15*4), R8;  MOVL R8, 92(SP)
    VINSERTI32X4 $2, 80(SP), Z11, Z11
    // Lane 3
    MOVBLZX 9(R14),  R15; MOVL 192(BX)(R15*4), R8;  MOVL R8, 80(SP)
    MOVBLZX 11(R14), R15; MOVL 192(BX)(R15*4), R8;  MOVL R8, 84(SP)
    MOVBLZX 13(R14), R15; MOVL 192(BX)(R15*4), R8;  MOVL R8, 88(SP)
    MOVBLZX 15(R14), R15; MOVL 192(BX)(R15*4), R8;  MOVL R8, 92(SP)
    VINSERTI32X4 $3, 80(SP), Z11, Z11

    // Diagonal G
    VPADDD  Z1, Z0, Z0
    VPADDD  Z10, Z0, Z0
    VPXORD  Z0, Z3, Z3
    VPRORD  $16, Z3, Z3
    VPADDD  Z3, Z2, Z2
    VPXORD  Z2, Z1, Z1
    VPRORD  $12, Z1, Z1
    VPADDD  Z1, Z0, Z0
    VPADDD  Z11, Z0, Z0
    VPXORD  Z0, Z3, Z3
    VPRORD  $8, Z3, Z3
    VPADDD  Z3, Z2, Z2
    VPXORD  Z2, Z1, Z1
    VPRORD  $7, Z1, Z1

    // Un-rotate diagonals
    VPSHUFD $0x93, Z1, Z1
    VPSHUFD $0x4E, Z2, Z2
    VPSHUFD $0x39, Z3, Z3

    INCL    R13
    CMPL    R13, $7
    JL      avx512_round

    // ---- Finalize ----
    VPXORD  Z2, Z0, Z0
    VPXORD  Z3, Z1, Z1
    VPXORD  Z12, Z2, Z2
    VPXORD  Z13, Z3, Z3

    // ---- Store results ----
    // results is *[4][16]uint32 = 256 bytes
    // result[i] at offset i*64, each is 64 bytes (16 uint32s = 4 XMMs)

    // Compress 0 (lane 0)
    VEXTRACTI32X4 $0, Z0, X14
    VMOVDQU X14, (AX)
    VEXTRACTI32X4 $0, Z1, X14
    VMOVDQU X14, 16(AX)
    VEXTRACTI32X4 $0, Z2, X14
    VMOVDQU X14, 32(AX)
    VEXTRACTI32X4 $0, Z3, X14
    VMOVDQU X14, 48(AX)

    // Compress 1 (lane 1)
    VEXTRACTI32X4 $1, Z0, X14
    VMOVDQU X14, 64(AX)
    VEXTRACTI32X4 $1, Z1, X14
    VMOVDQU X14, 80(AX)
    VEXTRACTI32X4 $1, Z2, X14
    VMOVDQU X14, 96(AX)
    VEXTRACTI32X4 $1, Z3, X14
    VMOVDQU X14, 112(AX)

    // Compress 2 (lane 2)
    VEXTRACTI32X4 $2, Z0, X14
    VMOVDQU X14, 128(AX)
    VEXTRACTI32X4 $2, Z1, X14
    VMOVDQU X14, 144(AX)
    VEXTRACTI32X4 $2, Z2, X14
    VMOVDQU X14, 160(AX)
    VEXTRACTI32X4 $2, Z3, X14
    VMOVDQU X14, 176(AX)

    // Compress 3 (lane 3)
    VEXTRACTI32X4 $3, Z0, X14
    VMOVDQU X14, 192(AX)
    VEXTRACTI32X4 $3, Z1, X14
    VMOVDQU X14, 208(AX)
    VEXTRACTI32X4 $3, Z2, X14
    VMOVDQU X14, 224(AX)
    VEXTRACTI32X4 $3, Z3, X14
    VMOVDQU X14, 240(AX)

    VZEROUPPER
    RET

// blake3Compress4T — pre-transposed message variant of blake3Compress4.
// tmsgs is *[4][112]uint32 = 1792 bytes: tmsg0 +0, tmsg1 +448, tmsg2 +896, tmsg3 +1344
// Each tmsg: [7 rounds][col_mx(16B), col_my(16B), diag_mx(16B), diag_my(16B)]
TEXT ·blake3Compress4T(SB), NOSPLIT, $128-40
    MOVQ    results+0(FP), AX
    MOVQ    tmsgs+8(FP), BX
    MOVQ    cvs+16(FP), CX
    MOVQ    counters+24(FP), DX

    // Row 0: cv[0..3] for 4 compresses
    VMOVDQU (CX), X0
    VINSERTI32X4 $1, 32(CX), Z0, Z0
    VINSERTI32X4 $2, 64(CX), Z0, Z0
    VINSERTI32X4 $3, 96(CX), Z0, Z0

    // Row 1: cv[4..7]
    VMOVDQU 16(CX), X1
    VINSERTI32X4 $1, 48(CX), Z1, Z1
    VINSERTI32X4 $2, 80(CX), Z1, Z1
    VINSERTI32X4 $3, 112(CX), Z1, Z1

    // Row 2: IV
    MOVL    $0x6A09E667, 0(SP)
    MOVL    $0xBB67AE85, 4(SP)
    MOVL    $0x3C6EF372, 8(SP)
    MOVL    $0xA54FF53A, 12(SP)
    VMOVDQU 0(SP), X2
    VINSERTI32X4 $1, 0(SP), Z2, Z2
    VINSERTI32X4 $2, 0(SP), Z2, Z2
    VINSERTI32X4 $3, 0(SP), Z2, Z2

    // Row 3: counters/blockLen/flags per lane
    MOVL    blockLen+32(FP), SI
    MOVL    flags+36(FP), DI

    // Lane 0
    MOVQ    (DX), R8
    MOVL    R8, 16(SP)
    SHRQ    $32, R8
    MOVL    R8, 20(SP)
    MOVL    SI, 24(SP)
    MOVL    DI, 28(SP)
    VMOVDQU 16(SP), X3

    // Lane 1
    MOVQ    8(DX), R8
    MOVL    R8, 32(SP)
    SHRQ    $32, R8
    MOVL    R8, 36(SP)
    MOVL    SI, 40(SP)
    MOVL    DI, 44(SP)
    VINSERTI32X4 $1, 32(SP), Z3, Z3

    // Lane 2
    MOVQ    16(DX), R8
    MOVL    R8, 48(SP)
    SHRQ    $32, R8
    MOVL    R8, 52(SP)
    MOVL    SI, 56(SP)
    MOVL    DI, 60(SP)
    VINSERTI32X4 $2, 48(SP), Z3, Z3

    // Lane 3
    MOVQ    24(DX), R8
    MOVL    R8, 64(SP)
    SHRQ    $32, R8
    MOVL    R8, 68(SP)
    MOVL    SI, 72(SP)
    MOVL    DI, 76(SP)
    VINSERTI32X4 $3, 64(SP), Z3, Z3

    VMOVDQU32 Z0, Z12
    VMOVDQU32 Z1, Z13

    XORL    R13, R13

avx512_round_t:
    MOVQ    R13, R14
    SHLQ    $6, R14        // R14 = round * 64

    // Load col_mx from 4 transposed message arrays
    VMOVDQU (BX)(R14*1), X10
    VINSERTI32X4 $1, 448(BX)(R14*1), Z10, Z10
    VINSERTI32X4 $2, 896(BX)(R14*1), Z10, Z10
    VINSERTI32X4 $3, 1344(BX)(R14*1), Z10, Z10

    // Load col_my
    VMOVDQU 16(BX)(R14*1), X11
    VINSERTI32X4 $1, 464(BX)(R14*1), Z11, Z11
    VINSERTI32X4 $2, 912(BX)(R14*1), Z11, Z11
    VINSERTI32X4 $3, 1360(BX)(R14*1), Z11, Z11

    // Column G
    VPADDD  Z1, Z0, Z0
    VPADDD  Z10, Z0, Z0
    VPXORD  Z0, Z3, Z3
    VPRORD  $16, Z3, Z3
    VPADDD  Z3, Z2, Z2
    VPXORD  Z2, Z1, Z1
    VPRORD  $12, Z1, Z1
    VPADDD  Z1, Z0, Z0
    VPADDD  Z11, Z0, Z0
    VPXORD  Z0, Z3, Z3
    VPRORD  $8, Z3, Z3
    VPADDD  Z3, Z2, Z2
    VPXORD  Z2, Z1, Z1
    VPRORD  $7, Z1, Z1

    // Diagonal rotation
    VPSHUFD $0x39, Z1, Z1
    VPSHUFD $0x4E, Z2, Z2
    VPSHUFD $0x93, Z3, Z3

    // Load diag_mx
    VMOVDQU 32(BX)(R14*1), X10
    VINSERTI32X4 $1, 480(BX)(R14*1), Z10, Z10
    VINSERTI32X4 $2, 928(BX)(R14*1), Z10, Z10
    VINSERTI32X4 $3, 1376(BX)(R14*1), Z10, Z10

    // Load diag_my
    VMOVDQU 48(BX)(R14*1), X11
    VINSERTI32X4 $1, 496(BX)(R14*1), Z11, Z11
    VINSERTI32X4 $2, 944(BX)(R14*1), Z11, Z11
    VINSERTI32X4 $3, 1392(BX)(R14*1), Z11, Z11

    // Diagonal G
    VPADDD  Z1, Z0, Z0
    VPADDD  Z10, Z0, Z0
    VPXORD  Z0, Z3, Z3
    VPRORD  $16, Z3, Z3
    VPADDD  Z3, Z2, Z2
    VPXORD  Z2, Z1, Z1
    VPRORD  $12, Z1, Z1
    VPADDD  Z1, Z0, Z0
    VPADDD  Z11, Z0, Z0
    VPXORD  Z0, Z3, Z3
    VPRORD  $8, Z3, Z3
    VPADDD  Z3, Z2, Z2
    VPXORD  Z2, Z1, Z1
    VPRORD  $7, Z1, Z1

    // Un-rotate
    VPSHUFD $0x93, Z1, Z1
    VPSHUFD $0x4E, Z2, Z2
    VPSHUFD $0x39, Z3, Z3

    INCL    R13
    CMPL    R13, $7
    JL      avx512_round_t

    // Finalize
    VPXORD  Z2, Z0, Z0
    VPXORD  Z3, Z1, Z1
    VPXORD  Z12, Z2, Z2
    VPXORD  Z13, Z3, Z3

    // Store results
    VEXTRACTI32X4 $0, Z0, X14
    VMOVDQU X14, (AX)
    VEXTRACTI32X4 $0, Z1, X14
    VMOVDQU X14, 16(AX)
    VEXTRACTI32X4 $0, Z2, X14
    VMOVDQU X14, 32(AX)
    VEXTRACTI32X4 $0, Z3, X14
    VMOVDQU X14, 48(AX)

    VEXTRACTI32X4 $1, Z0, X14
    VMOVDQU X14, 64(AX)
    VEXTRACTI32X4 $1, Z1, X14
    VMOVDQU X14, 80(AX)
    VEXTRACTI32X4 $1, Z2, X14
    VMOVDQU X14, 96(AX)
    VEXTRACTI32X4 $1, Z3, X14
    VMOVDQU X14, 112(AX)

    VEXTRACTI32X4 $2, Z0, X14
    VMOVDQU X14, 128(AX)
    VEXTRACTI32X4 $2, Z1, X14
    VMOVDQU X14, 144(AX)
    VEXTRACTI32X4 $2, Z2, X14
    VMOVDQU X14, 160(AX)
    VEXTRACTI32X4 $2, Z3, X14
    VMOVDQU X14, 176(AX)

    VEXTRACTI32X4 $3, Z0, X14
    VMOVDQU X14, 192(AX)
    VEXTRACTI32X4 $3, Z1, X14
    VMOVDQU X14, 208(AX)
    VEXTRACTI32X4 $3, Z2, X14
    VMOVDQU X14, 224(AX)
    VEXTRACTI32X4 $3, Z3, X14
    VMOVDQU X14, 240(AX)

    VZEROUPPER
    RET
