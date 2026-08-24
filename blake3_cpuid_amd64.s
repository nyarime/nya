#include "textflag.h"

// func cpuidHasAVX2() bool
// Checks OSXSAVE + XGETBV XCR0 (AVX state) + CPUID.7 EBX bit 5.
TEXT ·cpuidHasAVX2(SB), NOSPLIT, $0-1
    MOVL    $1, AX
    CPUID
    // Check OSXSAVE (ECX bit 27)
    TESTL   $(1<<27), CX
    JZ      noAVX2
    // XGETBV: check XCR0 bits 2:1 (SSE state + AVX state)
    XORL    CX, CX
    XGETBV
    ANDL    $6, AX
    CMPL    AX, $6
    JNE     noAVX2
    // CPUID leaf 7: check AVX2 (EBX bit 5)
    MOVL    $7, AX
    XORL    CX, CX
    CPUID
    TESTL   $(1<<5), BX
    JZ      noAVX2
    MOVB    $1, ret+0(FP)
    RET
noAVX2:
    MOVB    $0, ret+0(FP)
    RET

// func cpuidHasAVX512(SB) bool
// Checks OSXSAVE + XGETBV XCR0 (opmask + ZMM) + CPUID.7 AVX512F + AVX512VL.
TEXT ·cpuidHasAVX512(SB), NOSPLIT, $0-1
    MOVL    $1, AX
    CPUID
    TESTL   $(1<<27), CX
    JZ      no512
    // XGETBV: bits 7,6,5 (ZMM hi16, ZMM hi256, opmask) + 2,1 (AVX, SSE)
    XORL    CX, CX
    XGETBV
    ANDL    $0xE6, AX
    CMPL    AX, $0xE6
    JNE     no512
    // AVX512F (EBX bit 16) + AVX512VL (EBX bit 31)
    MOVL    $7, AX
    XORL    CX, CX
    CPUID
    MOVL    BX, AX
    ANDL    $(1<<16 | 1<<31), AX
    CMPL    AX, $(1<<16 | 1<<31)
    JNE     no512
    MOVB    $1, ret+0(FP)
    RET
no512:
    MOVB    $0, ret+0(FP)
    RET
