#include "textflag.h"

// AVX2 rotation macro
#define VROTR(N, REG, TMP) \
    VPSRLD $(N), REG, TMP;      \
    VPSLLD $(32-(N)), REG, REG;  \
    VPOR   TMP, REG, REG

// blake3ChunkCV2Full processes 2 full 1024-byte chunks entirely in AVX2 assembly.
// Eliminates per-block Go→asm overhead (1 call instead of 16).
//
// func blake3ChunkCV2Full(result *[2][8]uint32, data []byte, chunkIdx uint64)
//
// data must contain at least (chunkIdx+2)*1024 bytes.
// result[0] = CV of chunk at chunkIdx, result[1] = CV of chunk at chunkIdx+1.
//
// Stack layout (640 bytes):
//   0-447:   transposed message for current block (7 rounds × 2 × 32 bytes)
//            Actually: 7 rounds × 64 bytes = 448 bytes
//            Each round: col_mx(32B YMM) + col_my(32B YMM) + diag_mx(32B YMM) + diag_my(32B YMM) = 128B
//            7 rounds × 128B = 896 bytes... too much.
//            Use SSE-width per lane: 7 × (16+16+16+16) × 2 = 896. That's a lot.
//
// Simpler: reuse the existing compress2T approach but loop in asm.
// We pre-transpose each block into a 448-byte buffer on the stack (per lane, so 896 total)
// then run the 7-round compress, then move to next block.
//
// Actually even simpler: just replicate the non-transposed compress2 approach
// (using permutation table) but loop over blocks internally.
// This avoids the Go→asm overhead AND the Go transpose overhead.
//
// Stack: 128 bytes for scratch + saved registers
TEXT ·blake3ChunkCV2Full(SB), NOSPLIT, $128-40
    MOVQ    result+0(FP), R9      // R9 = result ptr
    MOVQ    data+8(FP), R10       // R10 = data ptr (slice base)
    // data+16(FP) = len, data+24(FP) = cap (ignored, we trust caller)
    MOVQ    chunkIdx+32(FP), R11   // R11 = chunkIdx

    // Compute chunk base addresses
    // chunk0 = data + chunkIdx * 1024
    // chunk1 = data + (chunkIdx+1) * 1024
    MOVQ    R11, CX
    SHLQ    $10, CX                // CX = chunkIdx * 1024
    LEAQ    (R10)(CX*1), R10       // R10 = &data[chunkIdx*1024] = chunk0 base
    LEAQ    1024(R10), R15         // R15 = chunk1 base (saved, we'll use BX for perm)

    // Initialize CVs = IV for both lanes
    // Row 0: IV[0..3] | IV[0..3]
    MOVL    $0x6A09E667, 0(SP)
    MOVL    $0xBB67AE85, 4(SP)
    MOVL    $0x3C6EF372, 8(SP)
    MOVL    $0xA54FF53A, 12(SP)
    MOVL    $0x510E527F, 16(SP)
    MOVL    $0x9B05688C, 20(SP)
    MOVL    $0x1F83D9AB, 24(SP)
    MOVL    $0x5BE0CD19, 28(SP)

    // Y14 = IV[0..3] (both lanes)
    VMOVDQU 0(SP), X14
    VINSERTI128 $1, 0(SP), Y14, Y14
    // Y15 = IV[4..7] (both lanes)
    VMOVDQU 16(SP), X15
    VINSERTI128 $1, 16(SP), Y15, Y15

    // Current CV state - starts as IV
    VMOVDQA Y14, Y4               // cv_row0 = IV[0..3]
    VMOVDQA Y15, Y5               // cv_row1 = IV[4..7]

    // Permutation table address
    LEAQ    ·blake3PermRounds(SB), BX

    // Counter setup: counter_lo and counter_hi for both lanes
    // Lane 0: chunkIdx, Lane 1: chunkIdx+1
    MOVL    R11, 32(SP)           // counter0_lo
    MOVQ    R11, DX
    SHRQ    $32, DX
    MOVL    DX, 36(SP)            // counter0_hi
    LEAQ    1(R11), DX
    MOVL    DX, 48(SP)            // counter1_lo
    MOVQ    DX, SI
    SHRQ    $32, SI
    MOVL    SI, 52(SP)            // counter1_hi

    // Block loop counter
    XORL    DI, DI                // DI = block index (0..15)

block_loop:
    // ---- Set up compress state for this block ----

    // Row 0 = current CV row0
    VMOVDQA Y4, Y0
    // Row 1 = current CV row1
    VMOVDQA Y5, Y1
    // Row 2 = IV (same for all blocks)
    VMOVDQA Y14, Y2

    // Row 3 = [counter_lo, counter_hi, blockLen=64, flags]
    // Flags: block 0 gets CHUNK_START(1), block 15 gets CHUNK_END(2)
    MOVL    $64, 40(SP)           // blockLen
    XORL    AX, AX                // flags = 0
    CMPL    DI, $0
    JNE     not_first
    ORL     $1, AX                // CHUNK_START
not_first:
    CMPL    DI, $15
    JNE     not_last
    ORL     $2, AX                // CHUNK_END
not_last:
    MOVL    AX, 44(SP)            // flags for lane 0
    VMOVDQU 32(SP), X3

    MOVL    AX, 60(SP)            // flags for lane 1
    MOVL    $64, 56(SP)           // blockLen for lane 1
    VINSERTI128 $1, 48(SP), Y3, Y3

    // Save current CV for finalization
    VMOVDQA Y0, Y12
    VMOVDQA Y1, Y13

    // ---- Compute message base for this block ----
    // chunk0_block = R10 + DI*64
    // chunk1_block = R15 + DI*64
    MOVQ    DI, CX
    SHLQ    $6, CX                // CX = blockIdx * 64
    LEAQ    (R10)(CX*1), R8       // R8 = chunk0 block ptr

    // We'll use CX to point to chunk1 block
    LEAQ    (R15)(CX*1), CX       // CX = chunk1 block ptr

    // ---- 7 round loop ----
    XORL    SI, SI                // round counter

    // Save block ptrs and other state we need across rounds
    // R8 = chunk0 block, CX = chunk1 block, BX = perm table
    // DI = block index (need to preserve)

round_loop:
    MOVQ    SI, DX
    SHLQ    $4, DX
    LEAQ    (BX)(DX*1), DX        // DX = &blake3PermRounds[round]

    // ---- Column step ----
    // Build col_mx: [msg0[p[0]], msg0[p[2]], msg0[p[4]], msg0[p[6]] | msg1[...]]
    MOVBLZX 0(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 64(SP)
    MOVBLZX 2(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 68(SP)
    MOVBLZX 4(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 72(SP)
    MOVBLZX 6(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 76(SP)
    VMOVDQU 64(SP), X10
    MOVBLZX 0(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 64(SP)
    MOVBLZX 2(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 68(SP)
    MOVBLZX 4(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 72(SP)
    MOVBLZX 6(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 76(SP)
    VINSERTI128 $1, 64(SP), Y10, Y10

    // Build col_my
    MOVBLZX 1(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 64(SP)
    MOVBLZX 3(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 68(SP)
    MOVBLZX 5(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 72(SP)
    MOVBLZX 7(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 76(SP)
    VMOVDQU 64(SP), X11
    MOVBLZX 1(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 64(SP)
    MOVBLZX 3(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 68(SP)
    MOVBLZX 5(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 72(SP)
    MOVBLZX 7(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 76(SP)
    VINSERTI128 $1, 64(SP), Y11, Y11

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

    // ---- Diagonal step ----
    MOVBLZX 8(DX), AX;   MOVL (R8)(AX*4), AX;  MOVL AX, 64(SP)
    MOVBLZX 10(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 68(SP)
    MOVBLZX 12(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 72(SP)
    MOVBLZX 14(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 76(SP)
    VMOVDQU 64(SP), X10
    MOVBLZX 8(DX), AX;   MOVL (CX)(AX*4), AX;  MOVL AX, 64(SP)
    MOVBLZX 10(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 68(SP)
    MOVBLZX 12(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 72(SP)
    MOVBLZX 14(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 76(SP)
    VINSERTI128 $1, 64(SP), Y10, Y10

    MOVBLZX 9(DX), AX;   MOVL (R8)(AX*4), AX;  MOVL AX, 64(SP)
    MOVBLZX 11(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 68(SP)
    MOVBLZX 13(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 72(SP)
    MOVBLZX 15(DX), AX;  MOVL (R8)(AX*4), AX;  MOVL AX, 76(SP)
    VMOVDQU 64(SP), X11
    MOVBLZX 9(DX), AX;   MOVL (CX)(AX*4), AX;  MOVL AX, 64(SP)
    MOVBLZX 11(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 68(SP)
    MOVBLZX 13(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 72(SP)
    MOVBLZX 15(DX), AX;  MOVL (CX)(AX*4), AX;  MOVL AX, 76(SP)
    VINSERTI128 $1, 64(SP), Y11, Y11

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

    INCL    SI
    CMPL    SI, $7
    JL      round_loop

    // ---- Finalize this block: extract chaining value ----
    // new_cv[0..3] = state[0..3] ^ state[8..11]
    // new_cv[4..7] = state[4..7] ^ state[12..15]
    VPXOR   Y2, Y0, Y4            // new cv_row0
    VPXOR   Y3, Y1, Y5            // new cv_row1

    // Next block
    INCL    DI
    CMPL    DI, $16
    JL      block_loop

    // ---- Store results ----
    // result[0] = low lane CV, result[1] = high lane CV
    // Each [8]uint32 = 32 bytes
    VEXTRACTI128 $0, Y4, X0
    VMOVDQU X0, (R9)              // result[0][0..3]
    VEXTRACTI128 $0, Y5, X0
    VMOVDQU X0, 16(R9)            // result[0][4..7]

    VEXTRACTI128 $1, Y4, X0
    VMOVDQU X0, 32(R9)            // result[1][0..3]
    VEXTRACTI128 $1, Y5, X0
    VMOVDQU X0, 48(R9)            // result[1][4..7]

    VZEROUPPER
    RET
