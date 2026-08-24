//go:build amd64

package nya

import (
	"encoding/binary"
	"math/bits"
	"unsafe"
)

// ---- Assembly declarations ----

//go:noescape
func blake3CompressSSE2(state *[16]uint32, msg *[16]uint32, cv *[8]uint32, counter uint64, blockLen uint32, flags uint32)

//go:noescape
func blake3Compress2(results *[2][16]uint32, msgs *[2][16]uint32, cvs *[2][8]uint32, counter0 uint64, counter1 uint64, blockLen uint32, flags uint32)

//go:noescape
func blake3Compress4(results *[4][16]uint32, msgs *[4][16]uint32, cvs *[4][8]uint32, counters *[4]uint64, blockLen uint32, flags uint32)

// Pre-transposed variants: message words pre-arranged for direct vector loading.
// transposed layout: [7 rounds][4 vectors: col_mx, col_my, diag_mx, diag_my][4 uint32s] = [112]uint32

//go:noescape
func blake3CompressSSE2T(state *[16]uint32, tmsg *[112]uint32, cv *[8]uint32, counter uint64, blockLen uint32, flags uint32)

//go:noescape
func blake3Compress2T(results *[2][16]uint32, tmsgs *[2][112]uint32, cvs *[2][8]uint32, counter0 uint64, counter1 uint64, blockLen uint32, flags uint32)

//go:noescape
func blake3Compress4T(results *[4][16]uint32, tmsgs *[4][112]uint32, cvs *[4][8]uint32, counters *[4]uint64, blockLen uint32, flags uint32)

// Full-chunk variants: process all 16 blocks in one ASM call.

// blake3ChunkCV1Full processes 1 full chunk with pre-transposed messages.
// tmsgs: 16 blocks × [112]uint32 (7 rounds × 4 vectors × 4 words)
//
//go:noescape
func blake3ChunkCV1Full(result *[8]uint32, tmsgs *[16][112]uint32, counter uint64)

//go:noescape
func blake3ChunkCV2Full(result *[2][8]uint32, data []byte, chunkIdx uint64)

//go:noescape
func blake3ChunkCV4Full(result *[4][8]uint32, data []byte, chunkIdx uint64)

// blake3Process8Chunks processes 8 contiguous full chunks using AVX2 8-way SoA
// with VPGATHERDD for message loading.
//
//go:noescape
func blake3Process8Chunks(results *[8][8]uint32, data *byte, startCounter uint64)

// blake3Process16Chunks processes 16 contiguous full chunks using AVX-512 16-way SoA
// with VPGATHERDD for message loading and VPRORD for native rotation.
// Results are stored in SoA layout: 8 ZMMs × 16 uint32s = [8][16]uint32
// where result[word][chunk] = CV word 'word' for chunk 'chunk'.
//
//go:noescape
func blake3Process16Chunks(results *[8][16]uint32, data *byte, startCounter uint64)

// blake3ChunkCV1FullWrap pre-transposes all 16 blocks of a full chunk,
// then calls the SSE2 fullchunk ASM function.
func blake3ChunkCV1FullWrap(result *[8]uint32, data []byte, chunkIdx uint64) {
	base := int(chunkIdx) * blake3ChunkSize
	var tmsgs [16][112]uint32
	for b := 0; b < 16; b++ {
		block := blake3WordsFromBytesUnsafe((*[64]byte)(data[base+b*64:]))
		tmsgs[b] = blake3TransposeMsg(&block)
	}
	blake3ChunkCV1Full(result, &tmsgs, chunkIdx)
}

//go:noescape
func cpuidHasAVX2() bool

//go:noescape
func cpuidHasAVX512() bool

// ---- CPU feature detection (set once at init) ----

var (
	hasAVX2   = cpuidHasAVX2()
	hasAVX512 = cpuidHasAVX512()
)

// blake3WordsFromBytes loads 16 little-endian uint32s from a 64-byte block.
// On amd64, binary.LittleEndian.Uint32 compiles to a single MOV instruction.
func blake3WordsFromBytes(b []byte) [16]uint32 {
	_ = b[63] // BCE hint
	var w [16]uint32
	w[0] = binary.LittleEndian.Uint32(b[0:])
	w[1] = binary.LittleEndian.Uint32(b[4:])
	w[2] = binary.LittleEndian.Uint32(b[8:])
	w[3] = binary.LittleEndian.Uint32(b[12:])
	w[4] = binary.LittleEndian.Uint32(b[16:])
	w[5] = binary.LittleEndian.Uint32(b[20:])
	w[6] = binary.LittleEndian.Uint32(b[24:])
	w[7] = binary.LittleEndian.Uint32(b[28:])
	w[8] = binary.LittleEndian.Uint32(b[32:])
	w[9] = binary.LittleEndian.Uint32(b[36:])
	w[10] = binary.LittleEndian.Uint32(b[40:])
	w[11] = binary.LittleEndian.Uint32(b[44:])
	w[12] = binary.LittleEndian.Uint32(b[48:])
	w[13] = binary.LittleEndian.Uint32(b[52:])
	w[14] = binary.LittleEndian.Uint32(b[56:])
	w[15] = binary.LittleEndian.Uint32(b[60:])
	return w
}

// blake3WordsFromBytesUnsafe is a fast path for aligned 64-byte blocks on little-endian amd64.
func blake3WordsFromBytesUnsafe(b *[64]byte) [16]uint32 {
	return *(*[16]uint32)(unsafe.Pointer(b))
}

// blake3TransposeMsg pre-arranges message words for all 7 rounds.
// Output: [112]uint32 — for each round: [col_mx(4), col_my(4), diag_mx(4), diag_my(4)]
func blake3TransposeMsg(block *[16]uint32) [112]uint32 {
	var out [112]uint32
	// Round 0: identity permutation
	out[0] = block[0]
	out[1] = block[2]
	out[2] = block[4]
	out[3] = block[6]
	out[4] = block[1]
	out[5] = block[3]
	out[6] = block[5]
	out[7] = block[7]
	out[8] = block[8]
	out[9] = block[10]
	out[10] = block[12]
	out[11] = block[14]
	out[12] = block[9]
	out[13] = block[11]
	out[14] = block[13]
	out[15] = block[15]
	// Rounds 1-6: use permutation table
	for round := 1; round < 7; round++ {
		p := &blake3PermRounds[round]
		base := round * 16
		out[base+0] = block[p[0]]
		out[base+1] = block[p[2]]
		out[base+2] = block[p[4]]
		out[base+3] = block[p[6]]
		out[base+4] = block[p[1]]
		out[base+5] = block[p[3]]
		out[base+6] = block[p[5]]
		out[base+7] = block[p[7]]
		out[base+8] = block[p[8]]
		out[base+9] = block[p[10]]
		out[base+10] = block[p[12]]
		out[base+11] = block[p[14]]
		out[base+12] = block[p[9]]
		out[base+13] = block[p[11]]
		out[base+14] = block[p[13]]
		out[base+15] = block[p[15]]
	}
	return out
}

// blake3Compress is the single-compress entry point (SSE2 with pre-transposed messages).
func blake3Compress(cv [8]uint32, block [16]uint32, counter uint64, blockLen uint32, flags uint32) [16]uint32 {
	var result [16]uint32
	tmsg := blake3TransposeMsg(&block)
	blake3CompressSSE2T(&result, &tmsg, &cv, counter, blockLen, flags)
	return result
}

// blake3ChunkCV2 processes 2 full chunks in lockstep using AVX2.
// Each chunk must be exactly blake3ChunkSize (1024) bytes.
func blake3ChunkCV2(data []byte, chunkIdx int, iv [8]uint32) (cv0, cv1 [8]uint32) {
	const nblocks = blake3ChunkSize / blake3BlockSize // 16

	var cvs [2][8]uint32
	cvs[0] = iv
	cvs[1] = iv

	c0base := chunkIdx * blake3ChunkSize
	c1base := (chunkIdx + 1) * blake3ChunkSize

	counter0 := uint64(chunkIdx)
	counter1 := uint64(chunkIdx + 1)

	for blk := 0; blk < nblocks; blk++ {
		off := blk * blake3BlockSize
		var msgs [2][16]uint32
		msgs[0] = blake3WordsFromBytesUnsafe((*[64]byte)(data[c0base+off:]))
		msgs[1] = blake3WordsFromBytesUnsafe((*[64]byte)(data[c1base+off:]))

		var tmsgs [2][112]uint32
		tmsgs[0] = blake3TransposeMsg(&msgs[0])
		tmsgs[1] = blake3TransposeMsg(&msgs[1])

		var flags uint32
		if blk == 0 {
			flags |= blake3FlagChunkStart
		}
		if blk == nblocks-1 {
			flags |= blake3FlagChunkEnd
		}

		var results [2][16]uint32
		blake3Compress2T(&results, &tmsgs, &cvs, counter0, counter1, blake3BlockSize, flags)

		copy(cvs[0][:], results[0][:8])
		copy(cvs[1][:], results[1][:8])
	}
	return cvs[0], cvs[1]
}

// blake3ChunkCV4 processes 4 full chunks in lockstep using AVX-512.
// Each chunk must be exactly blake3ChunkSize (1024) bytes.
func blake3ChunkCV4(data []byte, chunkIdx int, iv [8]uint32) (cv0, cv1, cv2, cv3 [8]uint32) {
	const nblocks = blake3ChunkSize / blake3BlockSize

	var cvs [4][8]uint32
	cvs[0] = iv
	cvs[1] = iv
	cvs[2] = iv
	cvs[3] = iv

	bases := [4]int{
		chunkIdx * blake3ChunkSize,
		(chunkIdx + 1) * blake3ChunkSize,
		(chunkIdx + 2) * blake3ChunkSize,
		(chunkIdx + 3) * blake3ChunkSize,
	}

	counters := [4]uint64{
		uint64(chunkIdx),
		uint64(chunkIdx + 1),
		uint64(chunkIdx + 2),
		uint64(chunkIdx + 3),
	}

	for blk := 0; blk < nblocks; blk++ {
		off := blk * blake3BlockSize
		var tmsgs [4][112]uint32
		for j := 0; j < 4; j++ {
			msg := blake3WordsFromBytesUnsafe((*[64]byte)(data[bases[j]+off:]))
			tmsgs[j] = blake3TransposeMsg(&msg)
		}

		var flags uint32
		if blk == 0 {
			flags |= blake3FlagChunkStart
		}
		if blk == nblocks-1 {
			flags |= blake3FlagChunkEnd
		}

		var results [4][16]uint32
		blake3Compress4T(&results, &tmsgs, &cvs, &counters, blake3BlockSize, flags)

		for j := 0; j < 4; j++ {
			copy(cvs[j][:], results[j][:8])
		}
	}
	return cvs[0], cvs[1], cvs[2], cvs[3]
}

// blake3CompressGo is the pure Go fallback (kept for reference/testing).
func blake3CompressGo(cv [8]uint32, block [16]uint32, counter uint64, blockLen uint32, flags uint32) [16]uint32 {
	var s [16]uint32
	s[0] = cv[0]
	s[1] = cv[1]
	s[2] = cv[2]
	s[3] = cv[3]
	s[4] = cv[4]
	s[5] = cv[5]
	s[6] = cv[6]
	s[7] = cv[7]
	s[8] = blake3IV[0]
	s[9] = blake3IV[1]
	s[10] = blake3IV[2]
	s[11] = blake3IV[3]
	s[12] = uint32(counter)
	s[13] = uint32(counter >> 32)
	s[14] = blockLen
	s[15] = flags

	for round := 0; round < 7; round++ {
		p := &blake3PermRounds[round]
		m0 := block[p[0]]
		m1 := block[p[1]]
		m2 := block[p[2]]
		m3 := block[p[3]]
		m4 := block[p[4]]
		m5 := block[p[5]]
		m6 := block[p[6]]
		m7 := block[p[7]]
		m8 := block[p[8]]
		m9 := block[p[9]]
		m10 := block[p[10]]
		m11 := block[p[11]]
		m12 := block[p[12]]
		m13 := block[p[13]]
		m14 := block[p[14]]
		m15 := block[p[15]]

		// Column step
		s[0] = s[0] + s[4] + m0
		s[12] = bits.RotateLeft32(s[12]^s[0], -16)
		s[8] = s[8] + s[12]
		s[4] = bits.RotateLeft32(s[4]^s[8], -12)
		s[0] = s[0] + s[4] + m1
		s[12] = bits.RotateLeft32(s[12]^s[0], -8)
		s[8] = s[8] + s[12]
		s[4] = bits.RotateLeft32(s[4]^s[8], -7)

		s[1] = s[1] + s[5] + m2
		s[13] = bits.RotateLeft32(s[13]^s[1], -16)
		s[9] = s[9] + s[13]
		s[5] = bits.RotateLeft32(s[5]^s[9], -12)
		s[1] = s[1] + s[5] + m3
		s[13] = bits.RotateLeft32(s[13]^s[1], -8)
		s[9] = s[9] + s[13]
		s[5] = bits.RotateLeft32(s[5]^s[9], -7)

		s[2] = s[2] + s[6] + m4
		s[14] = bits.RotateLeft32(s[14]^s[2], -16)
		s[10] = s[10] + s[14]
		s[6] = bits.RotateLeft32(s[6]^s[10], -12)
		s[2] = s[2] + s[6] + m5
		s[14] = bits.RotateLeft32(s[14]^s[2], -8)
		s[10] = s[10] + s[14]
		s[6] = bits.RotateLeft32(s[6]^s[10], -7)

		s[3] = s[3] + s[7] + m6
		s[15] = bits.RotateLeft32(s[15]^s[3], -16)
		s[11] = s[11] + s[15]
		s[7] = bits.RotateLeft32(s[7]^s[11], -12)
		s[3] = s[3] + s[7] + m7
		s[15] = bits.RotateLeft32(s[15]^s[3], -8)
		s[11] = s[11] + s[15]
		s[7] = bits.RotateLeft32(s[7]^s[11], -7)

		// Diagonal step
		s[0] = s[0] + s[5] + m8
		s[15] = bits.RotateLeft32(s[15]^s[0], -16)
		s[10] = s[10] + s[15]
		s[5] = bits.RotateLeft32(s[5]^s[10], -12)
		s[0] = s[0] + s[5] + m9
		s[15] = bits.RotateLeft32(s[15]^s[0], -8)
		s[10] = s[10] + s[15]
		s[5] = bits.RotateLeft32(s[5]^s[10], -7)

		s[1] = s[1] + s[6] + m10
		s[12] = bits.RotateLeft32(s[12]^s[1], -16)
		s[11] = s[11] + s[12]
		s[6] = bits.RotateLeft32(s[6]^s[11], -12)
		s[1] = s[1] + s[6] + m11
		s[12] = bits.RotateLeft32(s[12]^s[1], -8)
		s[11] = s[11] + s[12]
		s[6] = bits.RotateLeft32(s[6]^s[11], -7)

		s[2] = s[2] + s[7] + m12
		s[13] = bits.RotateLeft32(s[13]^s[2], -16)
		s[8] = s[8] + s[13]
		s[7] = bits.RotateLeft32(s[7]^s[8], -12)
		s[2] = s[2] + s[7] + m13
		s[13] = bits.RotateLeft32(s[13]^s[2], -8)
		s[8] = s[8] + s[13]
		s[7] = bits.RotateLeft32(s[7]^s[8], -7)

		s[3] = s[3] + s[4] + m14
		s[14] = bits.RotateLeft32(s[14]^s[3], -16)
		s[9] = s[9] + s[14]
		s[4] = bits.RotateLeft32(s[4]^s[9], -12)
		s[3] = s[3] + s[4] + m15
		s[14] = bits.RotateLeft32(s[14]^s[3], -8)
		s[9] = s[9] + s[14]
		s[4] = bits.RotateLeft32(s[4]^s[9], -7)
	}

	// Finalize
	s[0] ^= s[8]
	s[1] ^= s[9]
	s[2] ^= s[10]
	s[3] ^= s[11]
	s[4] ^= s[12]
	s[5] ^= s[13]
	s[6] ^= s[14]
	s[7] ^= s[15]
	s[8] ^= cv[0]
	s[9] ^= cv[1]
	s[10] ^= cv[2]
	s[11] ^= cv[3]
	s[12] ^= cv[4]
	s[13] ^= cv[5]
	s[14] ^= cv[6]
	s[15] ^= cv[7]

	return s
}
