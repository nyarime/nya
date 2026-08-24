//go:build !amd64 && !arm64

package nya

import (
	"encoding/binary"
	"math/bits"
)

var blake3MsgPermutation = [16]int{2, 6, 3, 10, 7, 0, 4, 13, 1, 11, 12, 5, 9, 14, 15, 8}

func blake3WordsFromBytes(b []byte) [16]uint32 {
	var w [16]uint32
	for i := 0; i < 16 && i*4+4 <= len(b); i++ {
		w[i] = binary.LittleEndian.Uint32(b[i*4 : i*4+4])
	}
	if rem := len(b) % 4; rem != 0 && len(b)/4 < 16 {
		idx := len(b) / 4
		var tmp [4]byte
		copy(tmp[:], b[idx*4:])
		w[idx] = binary.LittleEndian.Uint32(tmp[:])
	}
	return w
}

func blake3G(state *[16]uint32, a, b, c, d int, mx, my uint32) {
	state[a] = state[a] + state[b] + mx
	state[d] = bits.RotateLeft32(state[d]^state[a], -16)
	state[c] = state[c] + state[d]
	state[b] = bits.RotateLeft32(state[b]^state[c], -12)
	state[a] = state[a] + state[b] + my
	state[d] = bits.RotateLeft32(state[d]^state[a], -8)
	state[c] = state[c] + state[d]
	state[b] = bits.RotateLeft32(state[b]^state[c], -7)
}

func blake3Compress(cv [8]uint32, block [16]uint32, counter uint64, blockLen uint32, flags uint32) [16]uint32 {
	state := [16]uint32{
		cv[0], cv[1], cv[2], cv[3],
		cv[4], cv[5], cv[6], cv[7],
		blake3IV[0], blake3IV[1], blake3IV[2], blake3IV[3],
		uint32(counter), uint32(counter >> 32), blockLen, flags,
	}
	m := block
	for i := 0; i < 7; i++ {
		// columns
		blake3G(&state, 0, 4, 8, 12, m[0], m[1])
		blake3G(&state, 1, 5, 9, 13, m[2], m[3])
		blake3G(&state, 2, 6, 10, 14, m[4], m[5])
		blake3G(&state, 3, 7, 11, 15, m[6], m[7])
		// diagonals
		blake3G(&state, 0, 5, 10, 15, m[8], m[9])
		blake3G(&state, 1, 6, 11, 12, m[10], m[11])
		blake3G(&state, 2, 7, 8, 13, m[12], m[13])
		blake3G(&state, 3, 4, 9, 14, m[14], m[15])
		if i < 6 {
			var tmp [16]uint32
			for j := 0; j < 16; j++ {
				tmp[j] = m[blake3MsgPermutation[j]]
			}
			m = tmp
		}
	}
	for i := 0; i < 8; i++ {
		state[i] ^= state[i+8]
		state[i+8] ^= cv[i]
	}
	return state
}
