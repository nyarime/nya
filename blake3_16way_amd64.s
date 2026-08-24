#include "textflag.h"

// blake3Process16Chunks processes 16 contiguous 1024-byte chunks using AVX-512
// with VPGATHERDD for message loading (16-way SoA parallelism).
//
// func blake3Process16Chunks(results *[8][16]uint32, data *byte, startCounter uint64)
//
// Register allocation (32 ZMM registers available):
//   Z0-Z15:   state elements s[0]..s[15]
//   Z16:      gather index vector (constant: chunk byte offsets 0,1024,...,15*1024)
//   Z17-Z18:  message words (mx, my) from gather
//   Z19:      rotation temp
//   Z20-Z27:  saved CV (s[0]..s[7]) for finalize
//   Z28-Z31:  additional temps
//   K1:       gather mask (reset per gather)
//
// Stack layout:
//     0-63:   counter_lo vector (16 × 4 bytes)
//    64-127:  counter_hi vector (16 × 4 bytes)
//   128-191:  scratch
//   192-255:  scratch2

#define SCRATCH  128
#define SCRATCH2 192

// G function macro for AVX-512 with native VPRORD
// a, b, c, d are ZMM registers; mx, my are ZMM regs with message words
// TMP is a ZMM temp register
#define G_AVX512(a, b, c, d, mx, my, TMP) \
    VPADDD  b, a, a;          /* a += b     */ \
    VPADDD  mx, a, a;         /* a += mx    */ \
    VPXORD  a, d, d;          /* d ^= a     */ \
    VPRORD  $16, d, d;        /* d = ror(d, 16) */ \
    VPADDD  d, c, c;          /* c += d     */ \
    VPXORD  c, b, b;          /* b ^= c     */ \
    VPRORD  $12, b, b;        /* b = ror(b, 12) */ \
    VPADDD  b, a, a;          /* a += b     */ \
    VPADDD  my, a, a;         /* a += my    */ \
    VPXORD  a, d, d;          /* d ^= a     */ \
    VPRORD  $8, d, d;         /* d = ror(d, 8) */ \
    VPADDD  d, c, c;          /* c += d     */ \
    VPXORD  c, b, b;          /* b ^= c     */ \
    VPRORD  $7, b, b          /* b = ror(b, 7) */

// GATHER_MSG: gather message word from 16 chunks into dst ZMM
// base = GP register with data + block*64 + word*4
// idx = Z16 (index vector), dst = destination ZMM, K1 is mask
#define GATHER_MSG(base, dst) \
    KXNORW  K1, K1, K1;               \
    VPGATHERDD (base)(Z16*1), K1, dst

TEXT ·blake3Process16Chunks(SB), NOSPLIT, $256-24
    MOVQ    results+0(FP), R9     // R9 = results ptr
    MOVQ    data+8(FP), R10       // R10 = data ptr
    MOVQ    startCounter+16(FP), R11 // R11 = startCounter

    // Build gather index vector: byte offsets for 16 chunks (1024 bytes apart)
    // Z16 = [0, 1024, 2048, ..., 15*1024]
    MOVL    $0, (SCRATCH)(SP)
    MOVL    $1024, (SCRATCH+4)(SP)
    MOVL    $2048, (SCRATCH+8)(SP)
    MOVL    $3072, (SCRATCH+12)(SP)
    MOVL    $4096, (SCRATCH+16)(SP)
    MOVL    $5120, (SCRATCH+20)(SP)
    MOVL    $6144, (SCRATCH+24)(SP)
    MOVL    $7168, (SCRATCH+28)(SP)
    MOVL    $8192, (SCRATCH+32)(SP)
    MOVL    $9216, (SCRATCH+36)(SP)
    MOVL    $10240, (SCRATCH+40)(SP)
    MOVL    $11264, (SCRATCH+44)(SP)
    MOVL    $12288, (SCRATCH+48)(SP)
    MOVL    $13312, (SCRATCH+52)(SP)
    MOVL    $14336, (SCRATCH+56)(SP)
    MOVL    $15360, (SCRATCH+60)(SP)
    VMOVDQU64 SCRATCH(SP), Z16

    // Build counter vectors for s[12] and s[13]
    // s[12] = low 32 bits of (startCounter + i) for each chunk i
    MOVQ    R11, AX
    MOVL    AX, 0(SP)
    ADDQ    $1, AX; MOVL    AX, 4(SP)
    ADDQ    $1, AX; MOVL    AX, 8(SP)
    ADDQ    $1, AX; MOVL    AX, 12(SP)
    ADDQ    $1, AX; MOVL    AX, 16(SP)
    ADDQ    $1, AX; MOVL    AX, 20(SP)
    ADDQ    $1, AX; MOVL    AX, 24(SP)
    ADDQ    $1, AX; MOVL    AX, 28(SP)
    ADDQ    $1, AX; MOVL    AX, 32(SP)
    ADDQ    $1, AX; MOVL    AX, 36(SP)
    ADDQ    $1, AX; MOVL    AX, 40(SP)
    ADDQ    $1, AX; MOVL    AX, 44(SP)
    ADDQ    $1, AX; MOVL    AX, 48(SP)
    ADDQ    $1, AX; MOVL    AX, 52(SP)
    ADDQ    $1, AX; MOVL    AX, 56(SP)
    ADDQ    $1, AX; MOVL    AX, 60(SP)
    // counter_lo saved at 0(SP)..63(SP)

    // High 32 bits of counters
    MOVQ    R11, AX;            SHRQ $32, AX; MOVL AX, 64(SP)
    MOVQ    R11, AX; ADDQ $1, AX;  SHRQ $32, AX; MOVL AX, 68(SP)
    MOVQ    R11, AX; ADDQ $2, AX;  SHRQ $32, AX; MOVL AX, 72(SP)
    MOVQ    R11, AX; ADDQ $3, AX;  SHRQ $32, AX; MOVL AX, 76(SP)
    MOVQ    R11, AX; ADDQ $4, AX;  SHRQ $32, AX; MOVL AX, 80(SP)
    MOVQ    R11, AX; ADDQ $5, AX;  SHRQ $32, AX; MOVL AX, 84(SP)
    MOVQ    R11, AX; ADDQ $6, AX;  SHRQ $32, AX; MOVL AX, 88(SP)
    MOVQ    R11, AX; ADDQ $7, AX;  SHRQ $32, AX; MOVL AX, 92(SP)
    MOVQ    R11, AX; ADDQ $8, AX;  SHRQ $32, AX; MOVL AX, 96(SP)
    MOVQ    R11, AX; ADDQ $9, AX;  SHRQ $32, AX; MOVL AX, 100(SP)
    MOVQ    R11, AX; ADDQ $10, AX; SHRQ $32, AX; MOVL AX, 104(SP)
    MOVQ    R11, AX; ADDQ $11, AX; SHRQ $32, AX; MOVL AX, 108(SP)
    MOVQ    R11, AX; ADDQ $12, AX; SHRQ $32, AX; MOVL AX, 112(SP)
    MOVQ    R11, AX; ADDQ $13, AX; SHRQ $32, AX; MOVL AX, 116(SP)
    MOVQ    R11, AX; ADDQ $14, AX; SHRQ $32, AX; MOVL AX, 120(SP)
    MOVQ    R11, AX; ADDQ $15, AX; SHRQ $32, AX; MOVL AX, 124(SP)
    // counter_hi saved at 64(SP)..127(SP)

    // Initialize CV = IV broadcast to all 16 lanes
    VPBROADCASTD ·blake3IV+0(SB), Z0
    VPBROADCASTD ·blake3IV+4(SB), Z1
    VPBROADCASTD ·blake3IV+8(SB), Z2
    VPBROADCASTD ·blake3IV+12(SB), Z3
    VPBROADCASTD ·blake3IV+16(SB), Z4
    VPBROADCASTD ·blake3IV+20(SB), Z5
    VPBROADCASTD ·blake3IV+24(SB), Z6
    VPBROADCASTD ·blake3IV+28(SB), Z7

    // Permutation table base
    LEAQ    ·blake3PermRounds(SB), BX

    // Block loop: DI = block index (0..15)
    XORL    DI, DI

block_loop_16way:
    // Save CV for finalization: Z20-Z27 = CV s[0]..s[7]
    VMOVDQA64 Z0, Z20
    VMOVDQA64 Z1, Z21
    VMOVDQA64 Z2, Z22
    VMOVDQA64 Z3, Z23
    VMOVDQA64 Z4, Z24
    VMOVDQA64 Z5, Z25
    VMOVDQA64 Z6, Z26
    VMOVDQA64 Z7, Z27

    // Initialize state s[8..11] = IV[0..3]
    VPBROADCASTD ·blake3IV+0(SB), Z8
    VPBROADCASTD ·blake3IV+4(SB), Z9
    VPBROADCASTD ·blake3IV+8(SB), Z10
    VPBROADCASTD ·blake3IV+12(SB), Z11

    // s[12] = counter_lo, s[13] = counter_hi
    VMOVDQU64 0(SP), Z12
    VMOVDQU64 64(SP), Z13

    // s[14] = blockLen = 64 (broadcast)
    MOVL    $64, AX
    VPBROADCASTD AX, Z14

    // s[15] = flags
    XORL    AX, AX
    CMPL    DI, $0
    JNE     not_first_16
    ORL     $1, AX                // CHUNK_START
not_first_16:
    CMPL    DI, $15
    JNE     not_last_16
    ORL     $2, AX                // CHUNK_END
not_last_16:
    VPBROADCASTD AX, Z15

    // Message base for this block: R8 = data + blockIdx * 64
    MOVQ    DI, CX
    SHLQ    $6, CX
    LEAQ    (R10)(CX*1), R8

    // 7 rounds
    XORL    SI, SI

round_loop_16way:
    // DX = &blake3PermRounds[round][0]
    MOVQ    SI, DX
    SHLQ    $4, DX
    LEAQ    (BX)(DX*1), DX

    // ==== Column step ====

    // G(0, 4, 8, 12) with perm[0], perm[1]
    MOVBLZX 0(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z17)

    MOVBLZX 1(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z18)

    G_AVX512(Z0, Z4, Z8, Z12, Z17, Z18, Z19)

    // G(1, 5, 9, 13) with perm[2], perm[3]
    MOVBLZX 2(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z17)

    MOVBLZX 3(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z18)

    G_AVX512(Z1, Z5, Z9, Z13, Z17, Z18, Z19)

    // G(2, 6, 10, 14) with perm[4], perm[5]
    MOVBLZX 4(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z17)

    MOVBLZX 5(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z18)

    G_AVX512(Z2, Z6, Z10, Z14, Z17, Z18, Z19)

    // G(3, 7, 11, 15) with perm[6], perm[7]
    MOVBLZX 6(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z17)

    MOVBLZX 7(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z18)

    G_AVX512(Z3, Z7, Z11, Z15, Z17, Z18, Z19)

    // ==== Diagonal step ====

    // G(0, 5, 10, 15) with perm[8], perm[9]
    MOVBLZX 8(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z17)

    MOVBLZX 9(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z18)

    G_AVX512(Z0, Z5, Z10, Z15, Z17, Z18, Z19)

    // G(1, 6, 11, 12) with perm[10], perm[11]
    MOVBLZX 10(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z17)

    MOVBLZX 11(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z18)

    G_AVX512(Z1, Z6, Z11, Z12, Z17, Z18, Z19)

    // G(2, 7, 8, 13) with perm[12], perm[13]
    MOVBLZX 12(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z17)

    MOVBLZX 13(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z18)

    G_AVX512(Z2, Z7, Z8, Z13, Z17, Z18, Z19)

    // G(3, 4, 9, 14) with perm[14], perm[15]
    MOVBLZX 14(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z17)

    MOVBLZX 15(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    GATHER_MSG(CX, Z18)

    G_AVX512(Z3, Z4, Z9, Z14, Z17, Z18, Z19)

    // Next round
    INCL    SI
    CMPL    SI, $7
    JL      round_loop_16way

    // ---- Finalize: new_cv[i] = s[i] ^ s[i+8] for i=0..7 ----
    VPXORD  Z8, Z0, Z0
    VPXORD  Z9, Z1, Z1
    VPXORD  Z10, Z2, Z2
    VPXORD  Z11, Z3, Z3
    VPXORD  Z12, Z4, Z4
    VPXORD  Z13, Z5, Z5
    VPXORD  Z14, Z6, Z6
    VPXORD  Z15, Z7, Z7

    // Next block
    INCL    DI
    CMPL    DI, $16
    JL      block_loop_16way

    // ---- Store results in SoA layout: [8][16]uint32 ----
    // Z0..Z7 hold CVs in SoA: Zi = word i for all 16 chunks
    // Store directly — Go caller does the transpose
    VMOVDQU64 Z0, 0(R9)
    VMOVDQU64 Z1, 64(R9)
    VMOVDQU64 Z2, 128(R9)
    VMOVDQU64 Z3, 192(R9)
    VMOVDQU64 Z4, 256(R9)
    VMOVDQU64 Z5, 320(R9)
    VMOVDQU64 Z6, 384(R9)
    VMOVDQU64 Z7, 448(R9)

    VZEROUPPER
    RET
