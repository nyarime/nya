package nya

import (
	"encoding/binary"
)

// BLAKE3 hash function — pure Go implementation.
// Public API: Blake3Sum256(data []byte) [32]byte
//
// Compression hot path is in blake3_amd64.go (optimized) / blake3_generic.go (fallback).

const (
	blake3BlockSize = 64
	blake3ChunkSize = 1024

	blake3FlagChunkStart = 1
	blake3FlagChunkEnd   = 2
	blake3FlagParent     = 4
	blake3FlagRoot       = 8
)

var blake3IV = [8]uint32{
	0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
	0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19,
}

func blake3ChainingValue(cv [8]uint32, block [16]uint32, counter uint64, blockLen uint32, flags uint32) [8]uint32 {
	full := blake3Compress(cv, block, counter, blockLen, flags)
	var out [8]uint32
	copy(out[:], full[:8])
	return out
}

// blake3ChunkCV processes one chunk and returns its 8-word chaining value.
func blake3ChunkCV(data []byte, chunkCounter uint64, iv [8]uint32) [8]uint32 {
	cv := iv
	nblocks := (len(data) + blake3BlockSize - 1) / blake3BlockSize
	if nblocks == 0 {
		nblocks = 1
	}
	for i := 0; i < nblocks; i++ {
		start := i * blake3BlockSize
		end := start + blake3BlockSize
		if end > len(data) {
			end = len(data)
		}
		blockBytes := make([]byte, blake3BlockSize)
		copy(blockBytes, data[start:end])
		blockWords := blake3WordsFromBytes(blockBytes)
		blockLen := uint32(end - start)

		var flags uint32
		if i == 0 {
			flags |= blake3FlagChunkStart
		}
		if i == nblocks-1 {
			flags |= blake3FlagChunkEnd
		}
		cv = blake3ChainingValue(cv, blockWords, chunkCounter, blockLen, flags)
	}
	return cv
}

func blake3ParentCV(left, right [8]uint32, iv [8]uint32, flags uint32) [8]uint32 {
	var block [16]uint32
	copy(block[:8], left[:])
	copy(block[8:], right[:])
	return blake3ChainingValue(iv, block, 0, blake3BlockSize, flags|blake3FlagParent)
}

// Blake3Sum256 computes the BLAKE3 hash of data, returning a 32-byte digest.
// Compatible with github.com/zeebo/blake3.Sum256.
func Blake3Sum256(data []byte) [32]byte {
	iv := blake3IV

	nchunks := (len(data) + blake3ChunkSize - 1) / blake3ChunkSize
	if nchunks == 0 {
		nchunks = 1
	}

	// Number of guaranteed full-size (1024-byte) chunks.
	// Only full chunks can be batched; the last chunk may be short.
	fullChunks := nchunks
	if len(data)%blake3ChunkSize != 0 {
		fullChunks = nchunks - 1
	}

	cvs := make([][8]uint32, nchunks)
	i := blake3BatchChunks(data, cvs, fullChunks, iv)

	// Process remaining chunks (tail, or all chunks on non-amd64)
	for ; i < nchunks; i++ {
		start := i * blake3ChunkSize
		end := start + blake3ChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunkData := data[start:end]
		if len(chunkData) == 0 {
			chunkData = nil
		}
		cvs[i] = blake3ChunkCV(chunkData, uint64(i), iv)
	}

	// Single chunk → root output from re-processing with ROOT flag
	if len(cvs) == 1 {
		chunkData := data
		if len(chunkData) > blake3ChunkSize {
			chunkData = data[:blake3ChunkSize]
		}
		return blake3RootFromChunk(chunkData, 0, iv)
	}

	return blake3BuildTree(cvs, iv)
}

func blake3RootFromChunk(data []byte, chunkCounter uint64, iv [8]uint32) [32]byte {
	cv := iv
	nblocks := (len(data) + blake3BlockSize - 1) / blake3BlockSize
	if nblocks == 0 {
		nblocks = 1
	}
	for i := 0; i < nblocks; i++ {
		start := i * blake3BlockSize
		end := start + blake3BlockSize
		if end > len(data) {
			end = len(data)
		}
		blockBytes := make([]byte, blake3BlockSize)
		copy(blockBytes, data[start:end])
		blockWords := blake3WordsFromBytes(blockBytes)
		blockLen := uint32(end - start)
		if len(data) == 0 {
			blockLen = 0
		}

		var flags uint32
		if i == 0 {
			flags |= blake3FlagChunkStart
		}
		if i == nblocks-1 {
			flags |= blake3FlagChunkEnd | blake3FlagRoot
		}

		if i == nblocks-1 {
			full := blake3Compress(cv, blockWords, chunkCounter, blockLen, flags)
			var out [32]byte
			for j := 0; j < 8; j++ {
				binary.LittleEndian.PutUint32(out[j*4:], full[j])
			}
			return out
		}
		cv = blake3ChainingValue(cv, blockWords, chunkCounter, blockLen, flags)
	}
	var out [32]byte
	return out
}

func blake3BuildTree(cvs [][8]uint32, iv [8]uint32) [32]byte {
	if len(cvs) == 1 {
		var out [32]byte
		for i := 0; i < 8; i++ {
			binary.LittleEndian.PutUint32(out[i*4:], cvs[0][i])
		}
		return out
	}
	// Iterative merge until 2 CVs remain, pre-allocating buffers
	current := make([][8]uint32, len(cvs))
	copy(current, cvs)
	for len(current) > 2 {
		n := len(current)
		nNext := (n + 1) / 2
		next := make([][8]uint32, nNext)
		idx := 0
		for i := 0; i+1 < n; i += 2 {
			var block [16]uint32
			copy(block[:8], current[i][:])
			copy(block[8:], current[i+1][:])
			full := blake3Compress(iv, block, 0, blake3BlockSize, blake3FlagParent)
			copy(next[idx][:], full[:8])
			idx++
		}
		if n%2 == 1 {
			next[idx] = current[n-1]
		}
		current = next
	}
	// Final merge with ROOT flag
	var block [16]uint32
	copy(block[:8], current[0][:])
	copy(block[8:], current[1][:])
	full := blake3Compress(iv, block, 0, blake3BlockSize, blake3FlagParent|blake3FlagRoot)
	var out [32]byte
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint32(out[i*4:], full[i])
	}
	return out
}

func blake3MergeCVs(cvs [][8]uint32, iv [8]uint32) [8]uint32 {
	if len(cvs) == 1 {
		return cvs[0]
	}
	// Iterative bottom-up merge: process pairs level by level
	// Pre-allocate next slice and call blake3Compress directly (skip blake3ParentCV/blake3ChainingValue wrappers)
	current := make([][8]uint32, len(cvs))
	copy(current, cvs)
	for len(current) > 1 {
		n := len(current)
		nNext := (n + 1) / 2
		next := make([][8]uint32, nNext)
		idx := 0
		for i := 0; i+1 < n; i += 2 {
			var block [16]uint32
			copy(block[:8], current[i][:])
			copy(block[8:], current[i+1][:])
			full := blake3Compress(iv, block, 0, blake3BlockSize, blake3FlagParent)
			copy(next[idx][:], full[:8])
			idx++
		}
		if n%2 == 1 {
			next[idx] = current[n-1]
		}
		current = next
	}
	return current[0]
}

// blake3PermRounds contains pre-computed message word permutations for 7 rounds.
// Shared across all architectures (referenced by assembly).
var blake3PermRounds = [7][16]uint8{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
	{2, 6, 3, 10, 7, 0, 4, 13, 1, 11, 12, 5, 9, 14, 15, 8},
	{3, 4, 10, 12, 13, 2, 7, 14, 6, 5, 9, 0, 11, 15, 8, 1},
	{10, 7, 12, 9, 14, 3, 13, 15, 4, 0, 11, 2, 5, 8, 1, 6},
	{12, 13, 9, 11, 15, 10, 14, 8, 7, 2, 5, 3, 0, 1, 6, 4},
	{9, 14, 11, 5, 8, 12, 15, 1, 13, 3, 0, 10, 2, 6, 4, 7},
	{11, 15, 5, 0, 1, 9, 8, 6, 14, 10, 2, 12, 3, 4, 7, 13},
}
