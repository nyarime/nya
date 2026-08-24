package nya

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
)

// ---- Correctness: compare all paths against pure Go ----

// blake3CompressGoReference wraps the pure-Go compress for reference.
func blake3CompressGoReference(cv [8]uint32, block [16]uint32, counter uint64, blockLen uint32, flags uint32) [16]uint32 {
	return blake3CompressGo(cv, block, counter, blockLen, flags)
}

// Test single compress: SSE2 vs Go
func TestBlake3CompressSSE2(t *testing.T) {
	cv := blake3IV
	var block [16]uint32
	for i := range block {
		block[i] = uint32(i * 0x01010101)
	}

	want := blake3CompressGoReference(cv, block, 42, 64, blake3FlagChunkStart)
	got := blake3Compress(cv, block, 42, 64, blake3FlagChunkStart)

	if want != got {
		t.Fatalf("SSE2 mismatch:\n  want %v\n  got  %v", want, got)
	}
}

// Test AVX2 compress2: each lane should match individual SSE2 compress.
func TestBlake3Compress2(t *testing.T) {
	if !hasAVX2 {
		t.Skip("AVX2 not available")
	}

	cv0, cv1 := blake3IV, blake3IV
	cv1[0] ^= 0xDEADBEEF

	var msgs [2][16]uint32
	for i := range msgs[0] {
		msgs[0][i] = uint32(i * 0x11111111)
		msgs[1][i] = uint32(i * 0x22222222)
	}

	counter0, counter1 := uint64(0), uint64(1)
	blockLen := uint32(64)
	flags := uint32(blake3FlagChunkStart)

	// Reference: two individual compresses
	want0 := blake3CompressGoReference(cv0, msgs[0], counter0, blockLen, flags)
	want1 := blake3CompressGoReference(cv1, msgs[1], counter1, blockLen, flags)

	// AVX2 batched
	var cvs [2][8]uint32
	cvs[0] = cv0
	cvs[1] = cv1
	var results [2][16]uint32
	blake3Compress2(&results, &msgs, &cvs, counter0, counter1, blockLen, flags)

	if results[0] != want0 {
		t.Errorf("AVX2 lane 0 mismatch:\n  want %v\n  got  %v", want0, results[0])
	}
	if results[1] != want1 {
		t.Errorf("AVX2 lane 1 mismatch:\n  want %v\n  got  %v", want1, results[1])
	}
}

// Test AVX-512 compress4: each lane should match individual compress.
func TestBlake3Compress4(t *testing.T) {
	if !hasAVX512 {
		t.Skip("AVX-512 not available")
	}

	var cvArr [4][8]uint32
	for j := 0; j < 4; j++ {
		cvArr[j] = blake3IV
		cvArr[j][0] ^= uint32(j * 0x11111111)
	}

	var msgs [4][16]uint32
	for j := 0; j < 4; j++ {
		for i := range msgs[j] {
			msgs[j][i] = uint32((j+1)*0x01010101 + i*0x00010001)
		}
	}

	counters := [4]uint64{10, 11, 12, 13}
	blockLen := uint32(64)
	flags := uint32(blake3FlagChunkEnd)

	// Reference
	var want [4][16]uint32
	for j := 0; j < 4; j++ {
		want[j] = blake3CompressGoReference(cvArr[j], msgs[j], counters[j], blockLen, flags)
	}

	// AVX-512 batched
	var results [4][16]uint32
	blake3Compress4(&results, &msgs, &cvArr, &counters, blockLen, flags)

	for j := 0; j < 4; j++ {
		if results[j] != want[j] {
			t.Errorf("AVX-512 lane %d mismatch:\n  want %v\n  got  %v", j, want[j], results[j])
		}
	}
}

// Test full Blake3Sum256 produces correct output for known vectors.
func TestBlake3Sum256Vectors(t *testing.T) {
	// Official BLAKE3 test vectors (from https://github.com/BLAKE3-team/BLAKE3)
	// Input: incrementing bytes 0,1,2,...,N-1 mod 251
	makeInput := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i % 251)
		}
		return b
	}

	tests := []struct {
		len  int
		hash string
	}{
		{0, "af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262"},
		{1, "2d3adedff11b61f14c886e35afa036736dcd87a74d27b5c1510225d0f592e213"},
		{1023, "10108970eeda3eb932baac1428c7a2163b0e924c9a9e25b35bba72b28f70bd11"},
		{1024, "42214739f095a406f3fc83deb889744ac00df831c10daa55189b5d121c855af7"},
		{1025, "d00278ae47eb27b34faecf67b4fe263f82d5412916c1ffd97c8cb7fb814b8444"},
		{2048, "e776b6028c7cd22a4d0ba182a8bf62205d2ef576467e838ed6f2529b85fba24a"},
		{2049, "5f4d72f40d7a5f82b15ca2b2e44b1de3c2ef86c426c95c1af0b6879522563030"},
		{8192, "aae792484c8efe4f19e2ca7d371d8c467ffb10748d8a5a1ae579948f718a2a63"},   // 8 chunks
		{16384, "f875d6646de28985646f34ee13be9a576fd515f76b5b0a26bb324735041ddde4"},  // 16 chunks
		{31744, "62b6960e1a44bcc1eb1a611a8d6235b6b4b78f32e7abc4fb4c6cdcce94895c47"},  // 31 chunks
		{65536, "68d647e619a930e7b1082f74f334b0c65a315725569bdc123f0ee11881717bfe"},  // 64 chunks
		{102400, "bc3e3d41a1146b069abffad3c0d44860cf664390afce4d9661f7902e7943e085"}, // 100 chunks
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("len=%d", tc.len), func(t *testing.T) {
			input := makeInput(tc.len)
			got := Blake3Sum256(input)
			gotHex := hex.EncodeToString(got[:])
			if gotHex != tc.hash {
				t.Errorf("Blake3Sum256(len=%d)\n  want %s\n  got  %s", tc.len, tc.hash, gotHex)
			}
		})
	}
}

// Test that batched chunk processing gives same results as non-batched.
func TestBlake3BatchConsistency(t *testing.T) {
	// Use random data to exercise batched paths
	sizes := []int{2048, 3072, 4096, 5120, 8192, 10240, 65536, 131072}
	for _, size := range sizes {
		t.Run(fmt.Sprintf("size=%d", size), func(t *testing.T) {
			data := make([]byte, size)
			rand.Read(data)

			// Compute with batching (normal path)
			got := Blake3Sum256(data)

			// Compute without batching: use pure Go compress
			want := blake3Sum256NoBatch(data)

			if got != want {
				t.Errorf("Batch mismatch for size %d:\n  want %x\n  got  %x", size, want, got)
			}
		})
	}
}

// blake3Sum256NoBatch computes BLAKE3 using only pure-Go single compress (no batching).
func blake3Sum256NoBatch(data []byte) [32]byte {
	iv := blake3IV
	nchunks := (len(data) + blake3ChunkSize - 1) / blake3ChunkSize
	if nchunks == 0 {
		nchunks = 1
	}

	cvs := make([][8]uint32, nchunks)
	for i := 0; i < nchunks; i++ {
		start := i * blake3ChunkSize
		end := start + blake3ChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunkData := data[start:end]
		if len(chunkData) == 0 {
			chunkData = nil
		}
		// Use Go reference compress
		cv := iv
		nblocks := (len(chunkData) + blake3BlockSize - 1) / blake3BlockSize
		if nblocks == 0 {
			nblocks = 1
		}
		for b := 0; b < nblocks; b++ {
			bstart := b * blake3BlockSize
			bend := bstart + blake3BlockSize
			if bend > len(chunkData) {
				bend = len(chunkData)
			}
			blockBytes := make([]byte, blake3BlockSize)
			copy(blockBytes, chunkData[bstart:bend])
			blockWords := blake3WordsFromBytes(blockBytes)
			blockLen := uint32(bend - bstart)

			var flags uint32
			if b == 0 {
				flags |= blake3FlagChunkStart
			}
			if b == nblocks-1 {
				flags |= blake3FlagChunkEnd
			}
			full := blake3CompressGo(cv, blockWords, uint64(i), blockLen, flags)
			copy(cv[:], full[:8])
		}
		cvs[i] = cv
	}

	if len(cvs) == 1 {
		return blake3RootFromChunk(data, 0, iv)
	}
	return blake3BuildTree(cvs, iv)
}

// ---- Benchmarks ----

func BenchmarkBlake3_SSE2_1MB(b *testing.B) {
	data := make([]byte, 1<<20)
	rand.Read(data)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Blake3Sum256(data)
	}
}

func BenchmarkBlake3_SingleCompress(b *testing.B) {
	cv := blake3IV
	var block [16]uint32
	for i := range block {
		block[i] = uint32(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blake3Compress(cv, block, uint64(i), 64, blake3FlagChunkStart)
	}
}

func BenchmarkBlake3_Compress2_AVX2(b *testing.B) {
	if !hasAVX2 {
		b.Skip("AVX2 not available")
	}
	var cvs [2][8]uint32
	cvs[0] = blake3IV
	cvs[1] = blake3IV
	var msgs [2][16]uint32
	var results [2][16]uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blake3Compress2(&results, &msgs, &cvs, uint64(i), uint64(i+1), 64, blake3FlagChunkStart)
	}
}

func BenchmarkBlake3_Compress4_AVX512(b *testing.B) {
	if !hasAVX512 {
		b.Skip("AVX-512 not available")
	}
	var cvs [4][8]uint32
	for j := 0; j < 4; j++ {
		cvs[j] = blake3IV
	}
	var msgs [4][16]uint32
	var results [4][16]uint32
	counters := [4]uint64{0, 1, 2, 3}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blake3Compress4(&results, &msgs, &cvs, &counters, 64, blake3FlagChunkStart)
	}
}

func BenchmarkBlake3_1MB(b *testing.B) {
	data := make([]byte, 1<<20)
	rand.Read(data)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Blake3Sum256(data)
	}
}

func BenchmarkBlake3_4KB(b *testing.B) {
	data := make([]byte, 4096)
	rand.Read(data)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Blake3Sum256(data)
	}
}

func BenchmarkBlake3_64KB(b *testing.B) {
	data := make([]byte, 65536)
	rand.Read(data)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Blake3Sum256(data)
	}
}
