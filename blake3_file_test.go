package nya

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBlake3Sum256FileMatchesReadAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mid.bin")
	data := make([]byte, 128<<10)
	for i := range data {
		data[i] = byte(i)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	want := Blake3Sum256(data)
	got, err := blake3Sum256File(path, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("file hash mismatch")
	}
}

func TestBlake3Sum256FileLargeUsesMmap(t *testing.T) {
	if blake3FileMmapMin <= 0 {
		t.Skip("no mmap threshold")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	size := blake3FileMmapMin + 1024
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(size)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got, err := blake3Sum256File(path, int64(size))
	if err != nil {
		t.Fatal(err)
	}
	empty := make([]byte, size)
	want := Blake3Sum256(empty)
	if got != want {
		t.Fatalf("mmap hash mismatch")
	}
}
