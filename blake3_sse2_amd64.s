#include "textflag.h"

#define ROTR(N, REG, TMP) \
    MOVO REG, TMP;        \
    PSRLL $(N), REG;      \
    PSLLL $(32-(N)), TMP; \
    POR TMP, REG

// PSHUFD Xdst, Xsrc, imm8
// 66 0F 70 ModRM imm8
// ModRM = 0xC0 | (dst<<3) | src  (mod=11, reg=dst, rm=src)
// X0=0, X1=1, X2=2, X3=3
#define PSHUFD_X1_X1(imm) BYTE $0x66; BYTE $0x0F; BYTE $0x70; BYTE $0xC9; BYTE $(imm)
#define PSHUFD_X2_X2(imm) BYTE $0x66; BYTE $0x0F; BYTE $0x70; BYTE $0xD2; BYTE $(imm)
#define PSHUFD_X3_X3(imm) BYTE $0x66; BYTE $0x0F; BYTE $0x70; BYTE $0xDB; BYTE $(imm)

TEXT ·blake3CompressSSE2(SB), NOSPLIT, $64-40
    MOVQ    state+0(FP), AX
    MOVQ    msg+8(FP), BX
    MOVQ    cv+16(FP), CX
    MOVQ    counter+24(FP), DX
    MOVL    blockLen+32(FP), SI
    MOVL    flags+36(FP), DI

    // Load rows
    MOVOU   (CX), X0           // row0 = cv[0..3]
    MOVOU   16(CX), X1         // row1 = cv[4..7]

    // IV into stack, then load
    MOVL    $0x6A09E667, 0(SP)
    MOVL    $0xBB67AE85, 4(SP)
    MOVL    $0x3C6EF372, 8(SP)
    MOVL    $0xA54FF53A, 12(SP)
    MOVOU   0(SP), X2

    // Counter/blockLen/flags
    MOVL    DX, 16(SP)
    SHRQ    $32, DX
    MOVL    DX, 20(SP)
    MOVL    SI, 24(SP)
    MOVL    DI, 28(SP)
    MOVOU   16(SP), X3

    // Save cv for finalize
    MOVOU   (CX), X12
    MOVOU   16(CX), X13

    // Permutation table
    LEAQ    ·blake3PermRounds(SB), R12
    XORL    R13, R13

round_loop:
    MOVQ    R13, R14
    SHLQ    $4, R14
    ADDQ    R12, R14

    // Load column mx: [msg[p[0]], msg[p[2]], msg[p[4]], msg[p[6]]]
    MOVBLZX 0(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 32(SP)
    MOVBLZX 2(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 36(SP)
    MOVBLZX 4(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 40(SP)
    MOVBLZX 6(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 44(SP)
    MOVOU   32(SP), X10

    // Load column my: [msg[p[1]], msg[p[3]], msg[p[5]], msg[p[7]]]
    MOVBLZX 1(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 32(SP)
    MOVBLZX 3(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 36(SP)
    MOVBLZX 5(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 40(SP)
    MOVBLZX 7(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 44(SP)
    MOVOU   32(SP), X11

    // Column step
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
    PSHUFD_X1_X1(0x39)      // row1 rotate left 1
    PSHUFD_X2_X2(0x4E)      // row2 rotate left 2
    PSHUFD_X3_X3(0x93)      // row3 rotate left 3

    // Load diagonal mx
    MOVBLZX 8(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 32(SP)
    MOVBLZX 10(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 36(SP)
    MOVBLZX 12(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 40(SP)
    MOVBLZX 14(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 44(SP)
    MOVOU   32(SP), X10

    // Load diagonal my
    MOVBLZX 9(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 32(SP)
    MOVBLZX 11(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 36(SP)
    MOVBLZX 13(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 40(SP)
    MOVBLZX 15(R14), R15; MOVL (BX)(R15*4), R8; MOVL R8, 44(SP)
    MOVOU   32(SP), X11

    // Diagonal step
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
    PSHUFD_X1_X1(0x93)      // row1 rotate right 1
    PSHUFD_X2_X2(0x4E)      // row2 rotate right 2
    PSHUFD_X3_X3(0x39)      // row3 rotate right 3

    INCL    R13
    CMPL    R13, $7
    JL      round_loop

    // Finalize
    PXOR    X2, X0
    PXOR    X3, X1
    PXOR    X12, X2
    PXOR    X13, X3

    MOVOU   X0, (AX)
    MOVOU   X1, 16(AX)
    MOVOU   X2, 32(AX)
    MOVOU   X3, 48(AX)
    RET

// blake3CompressSSE2T — pre-transposed message variant.
// tmsg layout: [7 rounds][col_mx(16B), col_my(16B), diag_mx(16B), diag_my(16B)] = 448 bytes
TEXT ·blake3CompressSSE2T(SB), NOSPLIT, $32-40
    MOVQ    state+0(FP), AX
    MOVQ    tmsg+8(FP), BX
    MOVQ    cv+16(FP), CX
    MOVQ    counter+24(FP), DX
    MOVL    blockLen+32(FP), SI
    MOVL    flags+36(FP), DI

    // Load rows
    MOVOU   (CX), X0
    MOVOU   16(CX), X1

    // IV into row2
    MOVL    $0x6A09E667, 0(SP)
    MOVL    $0xBB67AE85, 4(SP)
    MOVL    $0x3C6EF372, 8(SP)
    MOVL    $0xA54FF53A, 12(SP)
    MOVOU   0(SP), X2

    // Counter/blockLen/flags into row3
    MOVL    DX, 16(SP)
    SHRQ    $32, DX
    MOVL    DX, 20(SP)
    MOVL    SI, 24(SP)
    MOVL    DI, 28(SP)
    MOVOU   16(SP), X3

    // Save cv for finalize
    MOVOU   (CX), X12
    MOVOU   16(CX), X13

    XORL    R13, R13

round_loop_t:
    MOVQ    R13, R14
    SHLQ    $6, R14        // R14 = round * 64

    // Column step
    MOVOU   (BX)(R14*1), X10      // col_mx
    MOVOU   16(BX)(R14*1), X11    // col_my

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
    MOVOU   32(BX)(R14*1), X10   // diag_mx
    MOVOU   48(BX)(R14*1), X11   // diag_my

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

    INCL    R13
    CMPL    R13, $7
    JL      round_loop_t

    // Finalize
    PXOR    X2, X0
    PXOR    X3, X1
    PXOR    X12, X2
    PXOR    X13, X3

    MOVOU   X0, (AX)
    MOVOU   X1, 16(AX)
    MOVOU   X2, 32(AX)
    MOVOU   X3, 48(AX)
    RET
