package nya

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepeatLogLikeZstdRatio(t *testing.T) {
	dir := t.TempDir()
	pat := []byte("Are you sure? (y/n)")
	var buf []byte
	for len(buf) < 8<<20 {
		buf = append(buf, pat...)
	}
	buf = buf[:8<<20]
	path := filepath.Join(dir, "repeat.log")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	z := ZstdCompressWithWindow(buf, 9)
	t.Logf("raw=%d zstd9=%d ratio=%.4f", len(buf), len(z), float64(len(z))/float64(len(buf)))

	arch := filepath.Join(dir, "out.nya")
	f, err := os.Create(arch)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, LevelFast, false)
	if err := w.AddFile(path); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	st, _ := os.Stat(arch)
	t.Logf("nya L3 archive=%d ratio=%.4f", st.Size(), float64(st.Size())/float64(len(buf)))
	if st.Size() > int64(len(buf))/10 {
		t.Fatalf("expected strong compression, got %d", st.Size())
	}
}
