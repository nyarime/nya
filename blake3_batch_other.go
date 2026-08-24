//go:build !amd64

package nya

// blake3BatchChunks — no batching on non-amd64 platforms.
func blake3BatchChunks(_ []byte, _ [][8]uint32, _ int, _ [8]uint32) int {
	return 0
}
