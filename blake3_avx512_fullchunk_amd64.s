#include "textflag.h"

// blake3ChunkCV4Full processes 4 full 1024-byte chunks entirely in AVX-512.
// Eliminates per-block Go→asm overhead AND Go-side transpose.
// One Go→asm call instead of 16.
//
// func blake3ChunkCV4Full(result *[4][8]uint32, data []byte, chunkIdx uint64)
//
// data must contain at least (chunkIdx+4)*1024 bytes from the start.
// result[i] = 8-word chaining value of chunk (chunkIdx+i).
//
// Stack layout (256 bytes):
//   0-15:   IV constants
//   16-79:  row3 for 4 lanes (16 bytes each)
//   80-95:  scratch for message gathering
//   96-127: additional scratch
//   128-191: saved CV row0 (Y12 backup isn't needed with ZMM, use stack)
//   192-255: saved CV row1
TEXT ·blake3ChunkCV4Full(SB), NOSPLIT, $256-40
    MOVQ    result+0(FP), R9       // R9 = result ptr
    MOVQ    data+8(FP), R10        // R10 = data slice base
    MOVQ    chunkIdx+32(FP), R11   // R11 = chunkIdx

    // Compute chunk base addresses
    MOVQ    R11, AX
    SHLQ    $10, AX                // AX = chunkIdx * 1024
    ADDQ    R10, AX                // AX = &data[chunkIdx*1024] = chunk0 base

    // Store chunk bases on stack for later (we'll compute block offsets from these)
    // chunk0 = AX, chunk1 = AX+1024, chunk2 = AX+2048, chunk3 = AX+3072
    // We'll keep AX as chunk0 base throughout

    // Initialize CVs = IV for all 4 lanes
    MOVL    $0x6A09E667, 0(SP)
    MOVL    $0xBB67AE85, 4(SP)
    MOVL    $0x3C6EF372, 8(SP)
    MOVL    $0xA54FF53A, 12(SP)
    MOVL    $0x510E527F, 16(SP)
    MOVL    $0x9B05688C, 20(SP)
    MOVL    $0x1F83D9AB, 24(SP)
    MOVL    $0x5BE0CD19, 28(SP)

    // Z4 = CV row0 (IV[0..3], replicated 4 lanes)
    VMOVDQU 0(SP), X4
    VINSERTI32X4 $1, 0(SP), Z4, Z4
    VINSERTI32X4 $2, 0(SP), Z4, Z4
    VINSERTI32X4 $3, 0(SP), Z4, Z4

    // Z5 = CV row1 (IV[4..7], replicated 4 lanes)
    VMOVDQU 16(SP), X5
    VINSERTI32X4 $1, 16(SP), Z5, Z5
    VINSERTI32X4 $2, 16(SP), Z5, Z5
    VINSERTI32X4 $3, 16(SP), Z5, Z5

    // Z6 = IV row2 constant (same as Z4, save for reuse)
    VMOVDQU32 Z4, Z6

    // Prepare counter values for row3
    // Lane 0: chunkIdx, Lane 1: chunkIdx+1, Lane 2: chunkIdx+2, Lane 3: chunkIdx+3
    MOVL    R11, 32(SP)            // counter0_lo
    MOVQ    R11, DX
    SHRQ    $32, DX
    MOVL    DX, 36(SP)             // counter0_hi

    LEAQ    1(R11), CX
    MOVL    CX, 48(SP)
    MOVQ    CX, DX
    SHRQ    $32, DX
    MOVL    DX, 52(SP)

    LEAQ    2(R11), CX
    MOVL    CX, 64(SP)
    MOVQ    CX, DX
    SHRQ    $32, DX
    MOVL    DX, 68(SP)

    LEAQ    3(R11), CX
    MOVL    CX, 80(SP)
    MOVQ    CX, DX
    SHRQ    $32, DX
    MOVL    DX, 84(SP)

    // Permutation table
    LEAQ    ·blake3PermRounds(SB), BX

    // Block loop
    XORL    DI, DI                 // DI = block index (0..15)

block_loop:
    // ---- Set up compress state for this block ----
    VMOVDQU32 Z4, Z0              // row0 = current CV row0
    VMOVDQU32 Z5, Z1              // row1 = current CV row1
    VMOVDQU32 Z6, Z2              // row2 = IV constant

    // Row 3: [counter_lo, counter_hi, blockLen=64, flags]
    XORL    CX, CX                // flags = 0
    CMPL    DI, $0
    JNE     not_first
    ORL     $1, CX                // CHUNK_START
not_first:
    CMPL    DI, $15
    JNE     not_last
    ORL     $2, CX                // CHUNK_END
not_last:
    // Build row3 for all 4 lanes
    MOVL    $64, 40(SP);  MOVL CX, 44(SP)     // lane 0: blockLen, flags
    MOVL    $64, 56(SP);  MOVL CX, 60(SP)     // lane 1
    MOVL    $64, 72(SP);  MOVL CX, 76(SP)     // lane 2
    MOVL    $64, 88(SP);  MOVL CX, 92(SP)     // lane 3

    VMOVDQU 32(SP), X3
    VINSERTI32X4 $1, 48(SP), Z3, Z3
    VINSERTI32X4 $2, 64(SP), Z3, Z3
    VINSERTI32X4 $3, 80(SP), Z3, Z3

    // Save current CV for finalization
    VMOVDQU32 Z0, Z12
    VMOVDQU32 Z1, Z13

    // Compute block data pointers
    // chunk0_block = AX + DI*64
    // chunk1_block = AX + 1024 + DI*64
    // chunk2_block = AX + 2048 + DI*64
    // chunk3_block = AX + 3072 + DI*64
    MOVQ    DI, R8
    SHLQ    $6, R8                 // R8 = blockIdx * 64
    // R8 = offset within chunk
    // Chunk base ptrs: AX, AX+1024, AX+2048, AX+3072
    // We'll use R8 as block offset, AX as chunk0 base

    // ---- 7 round loop ----
    XORL    SI, SI                 // round counter

round_loop:
    MOVQ    SI, DX
    SHLQ    $4, DX
    LEAQ    (BX)(DX*1), DX        // DX = &blake3PermRounds[round]

    // ---- Column step: build mx for 4 lanes ----
    // For each lane N, mx = [msg_N[p[0]], msg_N[p[2]], msg_N[p[4]], msg_N[p[6]]]
    // msg_N = chunk_N data at block offset R8, treated as [16]uint32

    // Lane 0 (chunk0)
    LEAQ    (AX)(R8*1), R14       // R14 = chunk0 block ptr
    MOVBLZX 0(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 2(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 4(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 6(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VMOVDQU 96(SP), X10

    // Lane 1 (chunk1)
    LEAQ    1024(AX)(R8*1), R14
    MOVBLZX 0(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 2(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 4(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 6(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $1, 96(SP), Z10, Z10

    // Lane 2 (chunk2)
    LEAQ    2048(AX)(R8*1), R14
    MOVBLZX 0(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 2(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 4(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 6(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $2, 96(SP), Z10, Z10

    // Lane 3 (chunk3)
    LEAQ    3072(AX)(R8*1), R14
    MOVBLZX 0(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 2(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 4(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 6(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $3, 96(SP), Z10, Z10

    // ---- Build my for 4 lanes ----
    // Lane 0
    LEAQ    (AX)(R8*1), R14
    MOVBLZX 1(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 3(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 5(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 7(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VMOVDQU 96(SP), X11

    // Lane 1
    LEAQ    1024(AX)(R8*1), R14
    MOVBLZX 1(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 3(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 5(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 7(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $1, 96(SP), Z11, Z11

    // Lane 2
    LEAQ    2048(AX)(R8*1), R14
    MOVBLZX 1(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 3(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 5(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 7(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $2, 96(SP), Z11, Z11

    // Lane 3
    LEAQ    3072(AX)(R8*1), R14
    MOVBLZX 1(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 3(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 5(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 7(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $3, 96(SP), Z11, Z11

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

    // ---- Diagonal step: build diag_mx for 4 lanes ----
    // Lane 0
    LEAQ    (AX)(R8*1), R14
    MOVBLZX 8(DX), R15;  MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 10(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 12(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 14(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VMOVDQU 96(SP), X10

    // Lane 1
    LEAQ    1024(AX)(R8*1), R14
    MOVBLZX 8(DX), R15;  MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 10(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 12(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 14(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $1, 96(SP), Z10, Z10

    // Lane 2
    LEAQ    2048(AX)(R8*1), R14
    MOVBLZX 8(DX), R15;  MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 10(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 12(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 14(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $2, 96(SP), Z10, Z10

    // Lane 3
    LEAQ    3072(AX)(R8*1), R14
    MOVBLZX 8(DX), R15;  MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 10(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 12(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 14(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $3, 96(SP), Z10, Z10

    // ---- Build diag_my for 4 lanes ----
    // Lane 0
    LEAQ    (AX)(R8*1), R14
    MOVBLZX 9(DX), R15;  MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 11(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 13(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 15(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VMOVDQU 96(SP), X11

    // Lane 1
    LEAQ    1024(AX)(R8*1), R14
    MOVBLZX 9(DX), R15;  MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 11(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 13(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 15(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $1, 96(SP), Z11, Z11

    // Lane 2
    LEAQ    2048(AX)(R8*1), R14
    MOVBLZX 9(DX), R15;  MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 11(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 13(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 15(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $2, 96(SP), Z11, Z11

    // Lane 3
    LEAQ    3072(AX)(R8*1), R14
    MOVBLZX 9(DX), R15;  MOVL (R14)(R15*4), R15; MOVL R15, 96(SP)
    MOVBLZX 11(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 100(SP)
    MOVBLZX 13(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 104(SP)
    MOVBLZX 15(DX), R15; MOVL (R14)(R15*4), R15; MOVL R15, 108(SP)
    VINSERTI32X4 $3, 96(SP), Z11, Z11

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

    INCL    SI
    CMPL    SI, $7
    JL      round_loop

    // ---- Finalize: extract new chaining value ----
    // cv[0..3] = state[0..3] ^ state[8..11]
    // cv[4..7] = state[4..7] ^ state[12..15]
    VPXORD  Z2, Z0, Z4            // new cv_row0
    VPXORD  Z3, Z1, Z5            // new cv_row1

    // Next block
    INCL    DI
    CMPL    DI, $16
    JL      block_loop

    // ---- Store results ----
    // result is *[4][8]uint32 = 128 bytes
    VEXTRACTI32X4 $0, Z4, X0
    VMOVDQU X0, (R9)              // result[0][0..3]
    VEXTRACTI32X4 $0, Z5, X0
    VMOVDQU X0, 16(R9)            // result[0][4..7]

    VEXTRACTI32X4 $1, Z4, X0
    VMOVDQU X0, 32(R9)            // result[1][0..3]
    VEXTRACTI32X4 $1, Z5, X0
    VMOVDQU X0, 48(R9)            // result[1][4..7]

    VEXTRACTI32X4 $2, Z4, X0
    VMOVDQU X0, 64(R9)            // result[2][0..3]
    VEXTRACTI32X4 $2, Z5, X0
    VMOVDQU X0, 80(R9)            // result[2][4..7]

    VEXTRACTI32X4 $3, Z4, X0
    VMOVDQU X0, 96(R9)            // result[3][0..3]
    VEXTRACTI32X4 $3, Z5, X0
    VMOVDQU X0, 112(R9)           // result[3][4..7]

    VZEROUPPER
    RET
