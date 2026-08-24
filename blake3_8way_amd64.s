#include "textflag.h"

// blake3Process8Chunks processes 8 contiguous 1024-byte chunks using AVX2
// with VPGATHERDD for message loading (8-way SoA parallelism).
//
// func blake3Process8Chunks(results *[8][8]uint32, data *byte, startCounter uint64)
//
// State layout (element-oriented / SoA):
//   YMM register lane i = state element value for chunk i (i=0..7)
//
// Register allocation:
//   YMM0-YMM7:   state elements s[0]..s[7] (kept in registers)
//   Stack 0-255:  state elements s[8]..s[15] (8 YMMs × 32 bytes)
//   YMM8-YMM9:   temp for state_c, state_d during G calls
//   YMM10:        gather index vector (constant: chunk byte offsets)
//   YMM11:        temp for rotation
//   YMM12-YMM13:  message words (mx, my) loaded via VPGATHERDD
//   YMM14-YMM15:  additional temps
//
// Stack layout (total ~640 bytes):
//     0-255:  state s[8]..s[15] (8 × 32 bytes)
//   256-287:  saved CV s[0]..s[7] row by row for finalize (reuse after block loop)
//   288-319:  gather index vector (if needed)
//   320-351:  scratch
//   352-383:  scratch2
//   384+:     callee-saved registers

// Rotation macro for AVX2 (no VPRORD in AVX2)
#define VROTR(N, REG, TMP) \
    VPSRLD $(N), REG, TMP;      \
    VPSLLD $(32-(N)), REG, REG;  \
    VPOR   TMP, REG, REG

// Stack offsets for spilled state s[8]..s[15]
#define S8  0
#define S9  32
#define S10 64
#define S11 96
#define S12 128
#define S13 160
#define S14 192
#define S15 224

// Scratch areas
#define SCRATCH 256
#define SCRATCH2 288
#define CV_SAVE 320
// CV_SAVE: 8 YMMs = 256 bytes (320..575)
// Total stack: 576 + some padding = 640

TEXT ·blake3Process8Chunks(SB), NOSPLIT, $640-24
    MOVQ    results+0(FP), R9     // R9 = results ptr
    MOVQ    data+8(FP), R10       // R10 = data ptr (start of first chunk)
    MOVQ    startCounter+16(FP), R11 // R11 = startCounter

    // Build gather index vector: byte offsets for 8 chunks (1024 bytes apart)
    // YMM10 = [0, 1024, 2048, 3072, 4096, 5120, 6144, 7168]
    MOVL    $0, 0+SCRATCH(SP)
    MOVL    $1024, 4+SCRATCH(SP)
    MOVL    $2048, 8+SCRATCH(SP)
    MOVL    $3072, 12+SCRATCH(SP)
    MOVL    $4096, 16+SCRATCH(SP)
    MOVL    $5120, 20+SCRATCH(SP)
    MOVL    $6144, 24+SCRATCH(SP)
    MOVL    $7168, 28+SCRATCH(SP)
    VMOVDQU SCRATCH(SP), Y10

    // Build counter vectors for s[12] and s[13]
    // s[12] = low 32 bits of counter for each chunk
    // s[13] = high 32 bits of counter for each chunk
    // Counter for chunk i = startCounter + i
    MOVQ    R11, AX
    MOVL    AX, 0+SCRATCH2(SP)
    ADDQ    $1, AX; MOVL    AX, 4+SCRATCH2(SP)
    ADDQ    $1, AX; MOVL    AX, 8+SCRATCH2(SP)
    ADDQ    $1, AX; MOVL    AX, 12+SCRATCH2(SP)
    ADDQ    $1, AX; MOVL    AX, 16+SCRATCH2(SP)
    ADDQ    $1, AX; MOVL    AX, 20+SCRATCH2(SP)
    ADDQ    $1, AX; MOVL    AX, 24+SCRATCH2(SP)
    ADDQ    $1, AX; MOVL    AX, 28+SCRATCH2(SP)
    VMOVDQU SCRATCH2(SP), Y14    // Y14 = counter_lo for all 8 chunks

    // High 32 bits of counters
    MOVQ    R11, AX
    SHRQ    $32, AX
    MOVL    AX, 0+SCRATCH2(SP)
    MOVQ    R11, AX; ADDQ $1, AX; SHRQ $32, AX; MOVL AX, 4+SCRATCH2(SP)
    MOVQ    R11, AX; ADDQ $2, AX; SHRQ $32, AX; MOVL AX, 8+SCRATCH2(SP)
    MOVQ    R11, AX; ADDQ $3, AX; SHRQ $32, AX; MOVL AX, 12+SCRATCH2(SP)
    MOVQ    R11, AX; ADDQ $4, AX; SHRQ $32, AX; MOVL AX, 16+SCRATCH2(SP)
    MOVQ    R11, AX; ADDQ $5, AX; SHRQ $32, AX; MOVL AX, 20+SCRATCH2(SP)
    MOVQ    R11, AX; ADDQ $6, AX; SHRQ $32, AX; MOVL AX, 24+SCRATCH2(SP)
    MOVQ    R11, AX; ADDQ $7, AX; SHRQ $32, AX; MOVL AX, 28+SCRATCH2(SP)
    VMOVDQU SCRATCH2(SP), Y15    // Y15 = counter_hi for all 8 chunks

    // Save counter vectors to known stack locations for reuse
    VMOVDQU Y14, CV_SAVE(SP)         // counter_lo at 320
    VMOVDQU Y15, (CV_SAVE+32)(SP)    // counter_hi at 352

    // Initialize CV = IV (broadcast to all 8 lanes)
    // IV = [0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
    //        0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19]
    VPBROADCASTD ·blake3IV+0(SB), Y0   // s[0] = IV[0] across 8 chunks
    VPBROADCASTD ·blake3IV+4(SB), Y1   // s[1] = IV[1]
    VPBROADCASTD ·blake3IV+8(SB), Y2   // s[2] = IV[2]
    VPBROADCASTD ·blake3IV+12(SB), Y3  // s[3] = IV[3]
    VPBROADCASTD ·blake3IV+16(SB), Y4  // s[4] = IV[4]
    VPBROADCASTD ·blake3IV+20(SB), Y5  // s[5] = IV[5]
    VPBROADCASTD ·blake3IV+24(SB), Y6  // s[6] = IV[6]
    VPBROADCASTD ·blake3IV+28(SB), Y7  // s[7] = IV[7]

    // Permutation table base
    LEAQ    ·blake3PermRounds(SB), BX

    // Block loop
    XORL    DI, DI                // DI = block index (0..15)

block_loop_8way:
    // ---- Initialize compress state for this block ----
    // s[0..7] = current CV (already in Y0..Y7)
    // Save CV for finalization
    VMOVDQU Y0, (CV_SAVE+64)(SP)
    VMOVDQU Y1, (CV_SAVE+96)(SP)
    VMOVDQU Y2, (CV_SAVE+128)(SP)
    VMOVDQU Y3, (CV_SAVE+160)(SP)
    VMOVDQU Y4, (CV_SAVE+192)(SP)
    VMOVDQU Y5, (CV_SAVE+224)(SP)
    VMOVDQU Y6, (CV_SAVE+256)(SP)
    VMOVDQU Y7, (CV_SAVE+288)(SP)

    // s[8..11] = IV[0..3] (broadcast)
    VPBROADCASTD ·blake3IV+0(SB), Y8
    VMOVDQU Y8, S8(SP)
    VPBROADCASTD ·blake3IV+4(SB), Y8
    VMOVDQU Y8, S9(SP)
    VPBROADCASTD ·blake3IV+8(SB), Y8
    VMOVDQU Y8, S10(SP)
    VPBROADCASTD ·blake3IV+12(SB), Y8
    VMOVDQU Y8, S11(SP)

    // s[12] = counter_lo (per-chunk)
    VMOVDQU CV_SAVE(SP), Y8
    VMOVDQU Y8, S12(SP)

    // s[13] = counter_hi (per-chunk)
    VMOVDQU (CV_SAVE+32)(SP), Y8
    VMOVDQU Y8, S13(SP)

    // s[14] = blockLen = 64 (broadcast)
    MOVL    $64, AX
    MOVL    AX, SCRATCH(SP)
    VPBROADCASTD SCRATCH(SP), Y8
    VMOVDQU Y8, S14(SP)

    // s[15] = flags
    XORL    AX, AX
    CMPL    DI, $0
    JNE     not_first_8
    ORL     $1, AX                // CHUNK_START
not_first_8:
    CMPL    DI, $15
    JNE     not_last_8
    ORL     $2, AX                // CHUNK_END
not_last_8:
    MOVL    AX, SCRATCH(SP)
    VPBROADCASTD SCRATCH(SP), Y8
    VMOVDQU Y8, S15(SP)

    // Compute message base for this block:
    // base = data + blockIdx * 64
    MOVQ    DI, CX
    SHLQ    $6, CX
    LEAQ    (R10)(CX*1), R8       // R8 = data + block*64

    // ---- 7 rounds ----
    XORL    SI, SI

round_loop_8way:
    // DX = &blake3PermRounds[round][0]
    MOVQ    SI, DX
    SHLQ    $4, DX
    LEAQ    (BX)(DX*1), DX

    // ==== Column step: G(0,4,8,12), G(1,5,9,13), G(2,6,10,14), G(3,7,11,15) ====
    // Process each G sequentially to manage register pressure

    // ---- G(0, 4, 8, 12) with mx=perm[2i], my=perm[2i+1] where i=0 ----
    // mx = msg[perm[0]], my = msg[perm[1]]
    // Gather mx from 8 chunks: word at offset perm[0]*4 from each chunk's block
    MOVBLZX 0(DX), AX             // AX = perm[round][0]
    SHLL    $2, AX                 // AX = perm[0] * 4
    LEAQ    (R8)(AX*1), CX        // CX = base + perm[0]*4

    VPCMPEQD Y12, Y12, Y12        // all-ones mask
    VPGATHERDD Y12, (CX)(Y10*1), Y13  // Y13 = mx from 8 chunks

    MOVBLZX 1(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX

    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y14  // Y14 = my from 8 chunks

    // Load s[8], s[12] from stack
    VMOVDQU S8(SP), Y8             // c = s[8]
    VMOVDQU S12(SP), Y9            // d = s[12]

    // G function: a=Y0, b=Y4, c=Y8, d=Y9, mx=Y13, my=Y14
    VPADDD  Y4, Y0, Y0            // a += b
    VPADDD  Y13, Y0, Y0           // a += mx
    VPXOR   Y0, Y9, Y9            // d ^= a
    VROTR(16, Y9, Y11)            // d = rotr(d, 16)
    VPADDD  Y9, Y8, Y8            // c += d
    VPXOR   Y8, Y4, Y4            // b ^= c
    VROTR(12, Y4, Y11)            // b = rotr(b, 12)
    VPADDD  Y4, Y0, Y0            // a += b
    VPADDD  Y14, Y0, Y0           // a += my
    VPXOR   Y0, Y9, Y9            // d ^= a
    VROTR(8, Y9, Y11)             // d = rotr(d, 8)
    VPADDD  Y9, Y8, Y8            // c += d
    VPXOR   Y8, Y4, Y4            // b ^= c
    VROTR(7, Y4, Y11)             // b = rotr(b, 7)

    // Store s[8], s[12] back
    VMOVDQU Y8, S8(SP)
    VMOVDQU Y9, S12(SP)

    // ---- G(1, 5, 9, 13) ----
    MOVBLZX 2(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y13

    MOVBLZX 3(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y14

    VMOVDQU S9(SP), Y8
    VMOVDQU S13(SP), Y9

    VPADDD  Y5, Y1, Y1
    VPADDD  Y13, Y1, Y1
    VPXOR   Y1, Y9, Y9
    VROTR(16, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y5, Y5
    VROTR(12, Y5, Y11)
    VPADDD  Y5, Y1, Y1
    VPADDD  Y14, Y1, Y1
    VPXOR   Y1, Y9, Y9
    VROTR(8, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y5, Y5
    VROTR(7, Y5, Y11)

    VMOVDQU Y8, S9(SP)
    VMOVDQU Y9, S13(SP)

    // ---- G(2, 6, 10, 14) ----
    MOVBLZX 4(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y13

    MOVBLZX 5(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y14

    VMOVDQU S10(SP), Y8
    VMOVDQU S14(SP), Y9

    VPADDD  Y6, Y2, Y2
    VPADDD  Y13, Y2, Y2
    VPXOR   Y2, Y9, Y9
    VROTR(16, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y6, Y6
    VROTR(12, Y6, Y11)
    VPADDD  Y6, Y2, Y2
    VPADDD  Y14, Y2, Y2
    VPXOR   Y2, Y9, Y9
    VROTR(8, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y6, Y6
    VROTR(7, Y6, Y11)

    VMOVDQU Y8, S10(SP)
    VMOVDQU Y9, S14(SP)

    // ---- G(3, 7, 11, 15) ----
    MOVBLZX 6(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y13

    MOVBLZX 7(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y14

    VMOVDQU S11(SP), Y8
    VMOVDQU S15(SP), Y9

    VPADDD  Y7, Y3, Y3
    VPADDD  Y13, Y3, Y3
    VPXOR   Y3, Y9, Y9
    VROTR(16, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y7, Y7
    VROTR(12, Y7, Y11)
    VPADDD  Y7, Y3, Y3
    VPADDD  Y14, Y3, Y3
    VPXOR   Y3, Y9, Y9
    VROTR(8, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y7, Y7
    VROTR(7, Y7, Y11)

    VMOVDQU Y8, S11(SP)
    VMOVDQU Y9, S15(SP)

    // ==== Diagonal step: G(0,5,10,15), G(1,6,11,12), G(2,7,8,13), G(3,4,9,14) ====

    // ---- G(0, 5, 10, 15) ----
    MOVBLZX 8(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y13

    MOVBLZX 9(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y14

    VMOVDQU S10(SP), Y8            // c = s[10]
    VMOVDQU S15(SP), Y9            // d = s[15]

    VPADDD  Y5, Y0, Y0
    VPADDD  Y13, Y0, Y0
    VPXOR   Y0, Y9, Y9
    VROTR(16, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y5, Y5
    VROTR(12, Y5, Y11)
    VPADDD  Y5, Y0, Y0
    VPADDD  Y14, Y0, Y0
    VPXOR   Y0, Y9, Y9
    VROTR(8, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y5, Y5
    VROTR(7, Y5, Y11)

    VMOVDQU Y8, S10(SP)
    VMOVDQU Y9, S15(SP)

    // ---- G(1, 6, 11, 12) ----
    MOVBLZX 10(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y13

    MOVBLZX 11(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y14

    VMOVDQU S11(SP), Y8            // c = s[11]
    VMOVDQU S12(SP), Y9            // d = s[12]

    VPADDD  Y6, Y1, Y1
    VPADDD  Y13, Y1, Y1
    VPXOR   Y1, Y9, Y9
    VROTR(16, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y6, Y6
    VROTR(12, Y6, Y11)
    VPADDD  Y6, Y1, Y1
    VPADDD  Y14, Y1, Y1
    VPXOR   Y1, Y9, Y9
    VROTR(8, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y6, Y6
    VROTR(7, Y6, Y11)

    VMOVDQU Y8, S11(SP)
    VMOVDQU Y9, S12(SP)

    // ---- G(2, 7, 8, 13) ----
    MOVBLZX 12(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y13

    MOVBLZX 13(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y14

    VMOVDQU S8(SP), Y8             // c = s[8]
    VMOVDQU S13(SP), Y9            // d = s[13]

    VPADDD  Y7, Y2, Y2
    VPADDD  Y13, Y2, Y2
    VPXOR   Y2, Y9, Y9
    VROTR(16, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y7, Y7
    VROTR(12, Y7, Y11)
    VPADDD  Y7, Y2, Y2
    VPADDD  Y14, Y2, Y2
    VPXOR   Y2, Y9, Y9
    VROTR(8, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y7, Y7
    VROTR(7, Y7, Y11)

    VMOVDQU Y8, S8(SP)
    VMOVDQU Y9, S13(SP)

    // ---- G(3, 4, 9, 14) ----
    MOVBLZX 14(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y13

    MOVBLZX 15(DX), AX
    SHLL    $2, AX
    LEAQ    (R8)(AX*1), CX
    VPCMPEQD Y12, Y12, Y12
    VPGATHERDD Y12, (CX)(Y10*1), Y14

    VMOVDQU S9(SP), Y8             // c = s[9]
    VMOVDQU S14(SP), Y9            // d = s[14]

    VPADDD  Y4, Y3, Y3
    VPADDD  Y13, Y3, Y3
    VPXOR   Y3, Y9, Y9
    VROTR(16, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y4, Y4
    VROTR(12, Y4, Y11)
    VPADDD  Y4, Y3, Y3
    VPADDD  Y14, Y3, Y3
    VPXOR   Y3, Y9, Y9
    VROTR(8, Y9, Y11)
    VPADDD  Y9, Y8, Y8
    VPXOR   Y8, Y4, Y4
    VROTR(7, Y4, Y11)

    VMOVDQU Y8, S9(SP)
    VMOVDQU Y9, S14(SP)

    // Next round
    INCL    SI
    CMPL    SI, $7
    JL      round_loop_8way

    // ---- Finalize: new_cv[i] = s[i] ^ s[i+8] for i=0..7 ----
    VMOVDQU S8(SP), Y8
    VPXOR   Y8, Y0, Y0            // s[0] ^= s[8]
    VMOVDQU S9(SP), Y8
    VPXOR   Y8, Y1, Y1
    VMOVDQU S10(SP), Y8
    VPXOR   Y8, Y2, Y2
    VMOVDQU S11(SP), Y8
    VPXOR   Y8, Y3, Y3
    VMOVDQU S12(SP), Y8
    VPXOR   Y8, Y4, Y4
    VMOVDQU S13(SP), Y8
    VPXOR   Y8, Y5, Y5
    VMOVDQU S14(SP), Y8
    VPXOR   Y8, Y6, Y6
    VMOVDQU S15(SP), Y8
    VPXOR   Y8, Y7, Y7

    // Next block
    INCL    DI
    CMPL    DI, $16
    JL      block_loop_8way

    // ---- Store results ----
    // Y0..Y7 hold the final CVs in SoA layout:
    // Y0 = [cv0[0], cv1[0], cv2[0], ..., cv7[0]]
    // Y1 = [cv0[1], cv1[1], ..., cv7[1]]
    // etc.
    // We need to transpose to AoS: results[chunk][word]
    // results is [8][8]uint32 = 8 chunks × 8 words × 4 bytes = 256 bytes

    // Transpose 8x8 matrix of uint32s using unpack + permute
    // Step 1: interleave pairs (0,1), (2,3), (4,5), (6,7)
    VPUNPCKLDQ Y1, Y0, Y8         // [a0b0, a1b1, a4b4, a5b5]
    VPUNPCKHDQ Y1, Y0, Y9         // [a2b2, a3b3, a6b6, a7b7]
    VPUNPCKLDQ Y3, Y2, Y12        // [c0d0, c1d1, c4d4, c5d5]
    VPUNPCKHDQ Y3, Y2, Y13        // [c2d2, c3d3, c6d6, c7d7]
    VPUNPCKLDQ Y5, Y4, Y14        // [e0f0, e1f1, e4f4, e5f5]
    VPUNPCKHDQ Y5, Y4, Y15        // [e2f2, e3f3, e6f6, e7f7]
    VPUNPCKLDQ Y7, Y6, Y0         // [g0h0, g1h1, g4h4, g5h5]
    VPUNPCKHDQ Y7, Y6, Y1         // [g2h2, g3h3, g6h6, g7h7]

    // Step 2: interleave quads
    VPUNPCKLQDQ Y12, Y8, Y2       // [a0b0c0d0, a4b4c4d4]
    VPUNPCKHQDQ Y12, Y8, Y3       // [a1b1c1d1, a5b5c5d5]
    VPUNPCKLQDQ Y13, Y9, Y4       // [a2b2c2d2, a6b6c6d6]
    VPUNPCKHQDQ Y13, Y9, Y5       // [a3b3c3d3, a7b7c7d7]
    VPUNPCKLQDQ Y0, Y14, Y6       // [e0f0g0h0, e4f4g4h4]
    VPUNPCKHQDQ Y0, Y14, Y7       // [e1f1g1h1, e5f5g5h5]
    VPUNPCKLQDQ Y1, Y15, Y8       // [e2f2g2h2, e6f6g6h6]
    VPUNPCKHQDQ Y1, Y15, Y9       // [e3f3g3h3, e7f7g7h7]

    // Step 3: permute 128-bit lanes to get final rows
    // chunk0 = low128(Y2) | low128(Y6)
    // chunk4 = high128(Y2) | high128(Y6)
    VPERM2I128 $0x20, Y6, Y2, Y0  // chunk0: [a0b0c0d0 | e0f0g0h0]
    VPERM2I128 $0x31, Y6, Y2, Y12 // chunk4: [a4b4c4d4 | e4f4g4h4]
    VPERM2I128 $0x20, Y7, Y3, Y1  // chunk1
    VPERM2I128 $0x31, Y7, Y3, Y13 // chunk5
    VPERM2I128 $0x20, Y8, Y4, Y14 // chunk2
    VPERM2I128 $0x31, Y8, Y4, Y15 // chunk6
    VPERM2I128 $0x20, Y9, Y5, Y10 // chunk3
    VPERM2I128 $0x31, Y9, Y5, Y11 // chunk7

    // Store all 8 CVs
    VMOVDQU Y0, 0(R9)             // results[0]
    VMOVDQU Y1, 32(R9)            // results[1]
    VMOVDQU Y14, 64(R9)           // results[2]
    VMOVDQU Y10, 96(R9)           // results[3]
    VMOVDQU Y12, 128(R9)          // results[4]
    VMOVDQU Y13, 160(R9)          // results[5]
    VMOVDQU Y15, 192(R9)          // results[6]
    VMOVDQU Y11, 224(R9)          // results[7]

    VZEROUPPER
    RET
