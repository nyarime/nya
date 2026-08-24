//go:build arm64

package nya

import (
	"encoding/binary"
)

//go:noescape
func blake3CompressNEON(state *[16]uint32, msg *[16]uint32, cv *[8]uint32, counter uint64, blockLen uint32, flags uint32)

// blake3WordsFromBytes loads 16 little-endian uint32s from a 64-byte block.
func blake3WordsFromBytes(b []byte) [16]uint32 {
	_ = b[63]
	var w [16]uint32
	for i := 0; i < 16; i++ {
		w[i] = binary.LittleEndian.Uint32(b[i*4:])
	}
	return w
}

// blake3Compress dispatches to NEON on arm64.
func blake3Compress(cv [8]uint32, block [16]uint32, counter uint64, blockLen uint32, flags uint32) [16]uint32 {
	var result [16]uint32
	blake3CompressNEON(&result, &block, &cv, counter, blockLen, flags)
	return result
}
