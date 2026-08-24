package nya

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectArchiveFormat(t *testing.T) {
	cases := []struct {
		name   string
		ext    string
		header []byte
		want   string
	}{
		{"zip ext", ".zip", nil, FormatZIP},
		{"7z ext", ".7z", nil, FormatSevenZ},
		{"rar ext", ".rar", nil, FormatRAR},
		{"zip magic", ".bin", []byte("PK\x03\x04"), FormatZIP},
		{"7z magic", ".bin", []byte("7z\xBC\xAF\x27\x1C"), FormatSevenZ},
		{"rar magic", ".bin", []byte("Rar!\x1a\x07\x00"), FormatRAR},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test"+tc.ext)
			if tc.header != nil {
				path = filepath.Join(t.TempDir(), "test"+tc.ext)
			}
			if err := os.WriteFile(path, tc.header, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := DetectArchiveFormat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConvertZipRoundtrip(t *testing.T) {
	srcDir := t.TempDir()
	want := map[string][]byte{
		"hello.txt":                  []byte("hello world\n"),
		"中文/文件.txt":                  []byte("中文内容"),
		filepath.Join("nested", "a.bin"): bytes.Repeat([]byte{0xAB}, 4096),
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

	zipPath := filepath.Join(t.TempDir(), "input.zip")
	if err := writeZip(zipPath, srcDir); err != nil {
		t.Fatal(err)
	}

	nyaPath := filepath.Join(t.TempDir(), "output.nya")
	res, err := ConvertArchive(zipPath, nyaPath, ConvertOptions{
		FECPercent: 10,
		Level:      LevelFastest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceFormat != FormatZIP {
		t.Errorf("SourceFormat=%q, want zip", res.SourceFormat)
	}

	r, err := Open(nyaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Verify() {
		t.Fatal("verify failed after convert")
	}

	outDir := t.TempDir()
	if err := r.Extract(outDir); err != nil {
		t.Fatal(err)
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Errorf("extract %q: %v", name, err)
			continue
		}
		if !bytes.Equal(got, content) {
			t.Errorf("content mismatch for %q", name)
		}
	}
}

func TestConvert7zRoundtrip(t *testing.T) {
	if _, err := find7z(); err != nil {
		t.Skip("7z not installed")
	}

	srcDir := t.TempDir()
	payload := []byte("convert from 7z to nya with FEC\n")
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	sevenZ := filepath.Join(t.TempDir(), "input.7z")
	cmd := exec.Command("7z", "a", "-bd", sevenZ, filepath.Join(srcDir, "data.txt"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z create: %v: %s", err, out)
	}

	nyaPath := filepath.Join(t.TempDir(), "output.nya")
	if _, err := ConvertArchive(sevenZ, nyaPath, ConvertOptions{FECPercent: 5, Level: LevelFastest}); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	r, err := Open(nyaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Extract(outDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "data.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("payload mismatch")
	}
}

func writeZip(zipPath, srcDir string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if info.IsDir() {
			name += "/"
		}
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(info.Mode())
		h.Flags |= 0x800 // UTF-8
		if info.IsDir() {
			_, err = w.CreateHeader(h)
			return err
		}
		wr, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = wr.Write(data)
		return err
	})
}
