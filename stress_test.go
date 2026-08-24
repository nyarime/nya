package nya

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStressLargeFileHighFEC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large stress test in -short mode")
	}
	payload := make([]byte, 32*1024*1024)
	for i := range payload {
		payload[i] = byte(i ^ (i >> 9))
	}
	src := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "stress.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 50, 7, false)
	if err := w.AddFile(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Verify() {
		t.Fatal("verify failed")
	}
	out := t.TempDir()
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(payload) {
		t.Fatalf("size mismatch: got %d want %d", len(got), len(payload))
	}
}

func TestStressEncryptedSolid(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping encrypted solid stress test in -short mode")
	}
	dir, err := build120FileTree(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "enc-solid.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 30, 9, true, []byte("stress-test-password"))
	if err := w.AddFile(dir); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if _, err := Open(archive); err != ErrPasswordRequired {
		t.Fatalf("Open without password: got %v want ErrPasswordRequired", err)
	}
	r, err := Open(archive, []byte("stress-test-password"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.VersionMinor < 2 {
		t.Errorf("encrypted archive VersionMinor=%d, want >= 2", r.Header.VersionMinor)
	}
	if !r.Verify() {
		t.Fatal("verify failed")
	}
	out := t.TempDir()
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
}
