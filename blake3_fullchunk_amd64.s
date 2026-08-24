#include "textflag.h"

#define ROTR(N, REG, TMP) \
    MOVO REG, TMP;        \
    PSRLL $(N), REG;      \
    PSLLL $(32-(N)), TMP; \
    POR TMP, REG

#define PSHUFD_X1_X1(imm) BYTE $0x66; BYTE $0x0F; BYTE $0x70; BYTE $0xC9; BYTE $(imm)
#define PSHUFD_X2_X2(imm) BYTE $0x66; BYTE $0x0F; BYTE $0x70; BYTE $0xD2; BYTE $(imm)
#define PSHUFD_X3_X3(imm) BYTE $0x66; BYTE $0x0F; BYTE $0x70; BYTE $0xDB; BYTE $(imm)

// blake3ChunkCV1Full processes 1 full 1024-byte chunk entirely in SSE2 assembly.
// Eliminates per-block Go→asm overhead (1 call instead of 16).
// Messages are pre-transposed by the Go caller for direct vector loading.
//
// func blake3ChunkCV1Full(result *[8]uint32, tmsgs *[16][112]uint32, counter uint64)
//
// tmsgs: 16 blocks × 112 uint32 (7 rounds × 4 vectors × 4 words) = 7168 bytes
// Each block's transposed data is 448 bytes (112 uint32s).
// Each round within a block is 64 bytes: [col_mx(16B), col_my(16B), diag_mx(16B), diag_my(16B)]
//
// Stack layout (48 bytes):
//   0-15:  IV constant
//   16-31: counter/blockLen/flags scratch
//   32-47: spare
TEXT ·blake3ChunkCV1Full(SB), NOSPLIT, $48-24
    MOVQ    result+0(FP), R9       // R9 = result ptr
    MOVQ    tmsgs+8(FP), BX        // BX = pre-transposed messages ptr
    MOVQ    counter+16(FP), CX     // CX = chunk counter

    // IV constants
    MOVL    $0x6A09E667, 0(SP)
    MOVL    $0xBB67AE85, 4(SP)
    MOVL    $0x3C6EF372, 8(SP)
    MOVL    $0xA54FF53A, 12(SP)
    MOVL    $0x510E527F, 16(SP)
    MOVL    $0x9B05688C, 20(SP)
    MOVL    $0x1F83D9AB, 24(SP)
    MOVL    $0x5BE0CD19, 28(SP)

    // X14 = IV[0..3], X15 = IV[4..7]
    MOVOU   0(SP), X14
    MOVOU   16(SP), X15

    // Current CV = IV
    MOVO    X14, X4
    MOVO    X15, X5

    // Counter in stack for row3
    MOVL    CX, 32(SP)            // counter_lo
    MOVQ    CX, DX
    SHRQ    $32, DX
    MOVL    DX, 36(SP)            // counter_hi

    // Block loop
    XORL    DI, DI                 // DI = block index (0..15)
    // R8 = current tmsg offset (starts at 0, incremented by 448 per block)
    XORL    R8, R8

block_loop:
    // State setup
    MOVO    X4, X0                 // row0 = cv[0..3]
    MOVO    X5, X1                 // row1 = cv[4..7]
    MOVO    X14, X2                // row2 = IV[0..3]

    // Row 3: [counter_lo, counter_hi, 64, flags]
    XORL    AX, AX
    CMPL    DI, $0
    JNE     not_first
    ORL     $1, AX
not_first:
    CMPL    DI, $15
    JNE     not_last
    ORL     $2, AX
not_last:
    MOVL    $64, 40(SP)
    MOVL    AX, 44(SP)
    MOVOU   32(SP), X3

    // Save CV for finalize (not needed — we only take first 8 words = XOR top/bottom)
    // Actually for BLAKE3 chunk CV, finalize is: state[i] ^= state[i+8] for i<8
    // We DON'T need the second half (state[8+i] ^= cv[i]).
    // So no need to save CV.

    // 7 rounds — direct loads from pre-transposed buffer
    XORL    SI, SI                 // round counter
    // R8 already points to current block's tmsg offset

round_loop:
    // Each round: 64 bytes at BX+R8 + round*64
    // col_mx at +0, col_my at +16, diag_mx at +32, diag_my at +48
    MOVQ    SI, DX
    SHLQ    $6, DX                 // DX = round * 64
    ADDQ    R8, DX                 // DX = block_offset + round*64

    // Column step
    MOVOU   (BX)(DX*1), X10       // col_mx
    MOVOU   16(BX)(DX*1), X11     // col_my

    PADDD   X1, X0
    PADDD   X10, X0
    PXOR    X0, X3
    ROTR(16, X3, X8)
    PADDD   X3, X2
    PXOR    X2, X1
    ROTR(12, X1, X8)
    PADDD   X1, X0
    PADDD   X11, X0
    PXOR    X0, X3
    ROTR(8, X3, X8)
    PADDD   X3, X2
    PXOR    X2, X1
    ROTR(7, X1, X8)

    // Diagonal rotation
    PSHUFD_X1_X1(0x39)
    PSHUFD_X2_X2(0x4E)
    PSHUFD_X3_X3(0x93)

    // Diagonal step
    MOVOU   32(BX)(DX*1), X10     // diag_mx
    MOVOU   48(BX)(DX*1), X11     // diag_my

    PADDD   X1, X0
    PADDD   X10, X0
    PXOR    X0, X3
    ROTR(16, X3, X8)
    PADDD   X3, X2
    PXOR    X2, X1
    ROTR(12, X1, X8)
    PADDD   X1, X0
    PADDD   X11, X0
    PXOR    X0, X3
    ROTR(8, X3, X8)
    PADDD   X3, X2
    PXOR    X2, X1
    ROTR(7, X1, X8)

    // Un-rotate
    PSHUFD_X1_X1(0x93)
    PSHUFD_X2_X2(0x4E)
    PSHUFD_X3_X3(0x39)

    INCL    SI
    CMPL    SI, $7
    JL      round_loop

    // Finalize: new_cv = state[0..7] ^ state[8..15]
    PXOR    X2, X0
    PXOR    X3, X1

    // New CV for next block
    MOVO    X0, X4
    MOVO    X1, X5

    // Advance to next block's transposed data (112 uint32 = 448 bytes)
    ADDQ    $448, R8
    INCL    DI
    CMPL    DI, $16
    JL      block_loop

    // Store final CV
    MOVOU   X4, (R9)
    MOVOU   X5, 16(R9)
    RET
