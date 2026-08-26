package nya

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExportZipRoundtrip(t *testing.T) {
	srcDir := t.TempDir()
	want := map[string][]byte{
		"hello.txt":     []byte("hello export\n"),
		"中文/文件.txt": []byte("导出"),
	}
	for name, content := range want {
		full := filepath.Join(srcDir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	nyaPath := filepath.Join(t.TempDir(), "src.nya")
	f, err := os.Create(nyaPath)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, LevelFastest, false)
	if err := w.AddDirectoryContents(srcDir); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	zipPath := filepath.Join(t.TempDir(), "out.zip")
	res, err := ExportArchive(nyaPath, zipPath, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceFormat != FormatNYA || res.DestFormat != FormatZIP {
		t.Fatalf("formats: %s → %s", res.SourceFormat, res.DestFormat)
	}

	outDir := t.TempDir()
	if err := ExtractForeignArchive(zipPath, outDir, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Errorf("missing %q: %v", name, err)
			continue
		}
		if !bytes.Equal(got, content) {
			t.Errorf("mismatch %q", name)
		}
	}
}

func TestConvertHubZipNyaZip(t *testing.T) {
	srcDir := t.TempDir()
	payload := []byte("hub zip to nya to zip\n")
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	inZip := filepath.Join(t.TempDir(), "in.zip")
	if err := writeZip(inZip, srcDir); err != nil {
		t.Fatal(err)
	}
	nyaPath := filepath.Join(t.TempDir(), "mid.nya")
	if _, err := ConvertHub(inZip, nyaPath, ConvertOptions{Level: LevelFastest, FECPercent: 0}, ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	outZip := filepath.Join(t.TempDir(), "out.zip")
	if _, err := ConvertHub(nyaPath, outZip, ConvertOptions{}, ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if err := ExtractForeignArchive(outZip, outDir, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("payload mismatch")
	}
}

func TestConvertHubSameFormatError(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	inZip := filepath.Join(t.TempDir(), "in.zip")
	if err := writeZip(inZip, srcDir); err != nil {
		t.Fatal(err)
	}
	_, err := ConvertHub(inZip, filepath.Join(t.TempDir(), "out.zip"), ConvertOptions{}, ExportOptions{})
	if err == nil {
		t.Fatal("expected same-format error")
	}
}

func TestEncryptedNyaRequiresPassword(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "secret.txt"), []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	nyaPath := filepath.Join(t.TempDir(), "enc.nya")
	f, err := os.Create(nyaPath)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, LevelFastest, false, []byte("s3cret"))
	if err := w.AddDirectoryContents(srcDir); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	needs, err := ArchiveNeedsPassword(nyaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !needs {
		t.Fatal("expected encrypted nya to need password")
	}
	if err := RequireSourcePassword(nyaPath, ""); err == nil {
		t.Fatal("expected RequireSourcePassword to fail")
	} else if !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("got %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "out.zip")
	_, err = ConvertHub(nyaPath, zipPath, ConvertOptions{}, ExportOptions{})
	if err == nil || !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("convert without password: %v", err)
	}
	_, err = ConvertHub(nyaPath, zipPath, ConvertOptions{SourcePassword: "s3cret"}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestZipNeedsPasswordFlag(t *testing.T) {
	// Unencrypted zip must not require password.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "plain.zip")
	if err := writeZip(zipPath, srcDir); err != nil {
		t.Fatal(err)
	}
	needs, err := ArchiveNeedsPassword(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if needs {
		t.Fatal("plain zip should not need password")
	}
}
