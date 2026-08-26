//go:build unix

package nya

import (
	"os"

	"golang.org/x/sys/unix"
)

// blake3Sum256File hashes file contents without loading the entire file into the Go heap.
func blake3Sum256File(path string, size int64) ([32]byte, error) {
	if size <= 0 {
		return Blake3Sum256(nil), nil
	}
	if size <= blake3FileMmapMin {
		data, err := os.ReadFile(path)
		if err != nil {
			return [32]byte{}, err
		}
		return Blake3Sum256(data), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	data, err := unix.Mmap(int(f.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return [32]byte{}, err
	}
	defer unix.Munmap(data)
	return Blake3Sum256(data), nil
}
