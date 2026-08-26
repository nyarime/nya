//go:build !unix

package nya

import "os"

func blake3Sum256File(path string, size int64) ([32]byte, error) {
	if size <= 0 {
		return Blake3Sum256(nil), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return [32]byte{}, err
	}
	return Blake3Sum256(data), nil
}
