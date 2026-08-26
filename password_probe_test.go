package nya

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRequireSourcePasswordWrappedError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	arch := filepath.Join(t.TempDir(), "enc.nya")
	f, err := os.Create(arch)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, LevelFastest, false, []byte("pw"))
	if err := w.AddDirectoryContents(dir); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	err = RequireSourcePassword(arch, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("errors.Is: %v", err)
	}
}

func TestConvertHubMissingInput(t *testing.T) {
	_, err := ConvertHub(filepath.Join(t.TempDir(), "missing.zip"), filepath.Join(t.TempDir(), "out.nya"), ConvertOptions{}, ExportOptions{})
	if err == nil {
		t.Fatal("expected error for missing input")
	}
}
