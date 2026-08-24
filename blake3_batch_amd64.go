//go:build amd64

package nya

// blake3BatchChunks dispatches batched chunk hashing via AVX-512 (4x) / AVX2 (2x) / SSE2 fullchunk (1x).
// Returns the number of chunks processed (caller handles the rest).
func blake3BatchChunks(data []byte, cvs [][8]uint32, fullChunks int, iv [8]uint32) int {
	// Only batch chunks where we have full 1024-byte data
	if len(data) < blake3ChunkSize {
		return 0
	}
	maxFull := len(data) / blake3ChunkSize
	if fullChunks > maxFull {
		fullChunks = maxFull
	}
	i := 0

	// AVX-512 16-way: 16 chunks at a time with VPGATHERDD
	if hasAVX512 {
		for ; i+16 <= fullChunks; i += 16 {
			var soaResults [8][16]uint32
			blake3Process16Chunks(&soaResults, &data[i*blake3ChunkSize], uint64(i))
			for c := 0; c < 16; c++ {
				for w := 0; w < 8; w++ {
					cvs[i+c][w] = soaResults[w][c]
				}
			}
		}
	}

	// AVX2 8-way: 8 chunks at a time with VPGATHERDD
	if hasAVX2 {
		for ; i+8 <= fullChunks; i += 8 {
			var results [8][8]uint32
			blake3Process8Chunks(&results, &data[i*blake3ChunkSize], uint64(i))
			for j := 0; j < 8; j++ {
				cvs[i+j] = results[j]
			}
		}
	}

	// AVX-512: 4 chunks at a time
	if hasAVX512 {
		for ; i+4 <= fullChunks; i += 4 {
			cvs[i], cvs[i+1], cvs[i+2], cvs[i+3] = blake3ChunkCV4(data, i, iv)
		}
	}

	// AVX2: 2 chunks at a time
	if hasAVX2 {
		for ; i+2 <= fullChunks; i += 2 {
			cvs[i], cvs[i+1] = blake3ChunkCV2(data, i, iv)
		}
	}

	// SSE2 fullchunk: 1 chunk at a time (pre-transposed, all 16 blocks in one ASM call)
	for ; i < fullChunks; i++ {
		blake3ChunkCV1FullWrap(&cvs[i], data, uint64(i))
	}

	return i
}
