package nya

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// buildTree writes a small but varied directory: compressible text, a binary
// blob, an incompressible blob and a nested directory.
func buildTree(t *testing.T) (dir string, want map[string][]byte) {
	t.Helper()
	dir = t.TempDir()
	want = map[string][]byte{}

	var text bytes.Buffer
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&text, "line %d: the quick brown fox jumps over the lazy dog\n", i)
	}
	want["text.txt"] = text.Bytes()

	binary := make([]byte, 40000)
	for i := range binary {
		binary[i] = byte(i % 251)
	}
	want["data.bin"] = binary

	noise := make([]byte, 20000)
	rand.New(rand.NewSource(5)).Read(noise)
	want["noise.bin"] = noise

	want[filepath.Join("nested", "inner.txt")] = bytes.Repeat([]byte("nested payload\n"), 500)

	for name, content := range want {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, want
}

func TestArchiveRoundtripModes(t *testing.T) {
	srcDir, want := buildTree(t)
	base := filepath.Base(srcDir)

	cases := []struct {
		name     string
		fec      int
		solid    bool
		lzma2    bool
		password string
	}{
		{name: "zstd"},
		{name: "lzma2", lzma2: true},
		{name: "solid", solid: true},
		{name: "solid-lzma2", solid: true, lzma2: true},
		{name: "fec", fec: 30},
		{name: "encrypted", password: "correct horse battery staple"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "test.nya")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}

			var w *Writer
			if tc.password != "" {
				w = NewWriterOpts(f, tc.fec, 9, tc.solid, []byte(tc.password))
			} else {
				w = NewWriterOpts(f, tc.fec, 9, tc.solid)
			}
			if tc.lzma2 {
				w.SetCompression("lzma2")
			}
			if err := w.AddFile(srcDir); err != nil {
				t.Fatal("AddFile:", err)
			}
			if err := w.Close(); err != nil {
				t.Fatal("Close:", err)
			}
			f.Close()

			var r *Reader
			if tc.password != "" {
				r, err = Open(archive, []byte(tc.password))
			} else {
				r, err = Open(archive)
			}
			if err != nil {
				t.Fatal("Open:", err)
			}
			if got := r.Header.VersionMinor; got != VersionMinor {
				t.Errorf("archive minor version = %d, want %d", got, VersionMinor)
			}
			if !r.Verify() {
				t.Error("Verify reported a damaged archive")
			}

			out := t.TempDir()
			if err := r.Extract(out); err != nil {
				t.Fatal("Extract:", err)
			}
			for name, content := range want {
				got, err := os.ReadFile(filepath.Join(out, base, name))
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				if !bytes.Equal(got, content) {
					t.Errorf("%s: content mismatch (%d bytes extracted, %d expected)",
						name, len(got), len(content))
				}
			}
		})
	}
}

// Compressible input must actually shrink; a codec that quietly falls back to
// stored blocks would otherwise look healthy in a round-trip test.
func TestArchiveCompressionRatio(t *testing.T) {
	srcDir, want := buildTree(t)
	var raw int
	for _, c := range want {
		raw += len(c)
	}

	for _, mode := range []string{"zstd", "lzma2"} {
		archive := filepath.Join(t.TempDir(), "ratio.nya")
		f, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		w := NewWriterOpts(f, 0, 9, false)
		if mode == "lzma2" {
			w.SetCompression("lzma2")
		}
		if err := w.AddFile(srcDir); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		fi, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		f.Close()

		ratio := float64(fi.Size()) / float64(raw)
		// The tree is ~30% incompressible noise, so anything under 0.75 shows
		// the codec is doing real work.
		if ratio > 0.75 {
			t.Errorf("%s: archive is %.1f%% of the input, expected under 75%%", mode, ratio*100)
		}
		t.Logf("%s: %d -> %d bytes (%.1f%%)", mode, raw, fi.Size(), ratio*100)
	}
}

func TestSanitizePathRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{
		"../escape",
		"../../etc/passwd",
		"nested/../../escape",
		"/absolute/path",
	} {
		if got, err := sanitizePath(dir, bad); err == nil {
			t.Errorf("sanitizePath(%q) = %q, want an error", bad, got)
		}
	}
	for _, ok := range []string{
		"file.txt",
		"nested/file.txt",
		"./nested/file.txt",
		"nested/../file.txt",
	} {
		if _, err := sanitizePath(dir, ok); err != nil {
			t.Errorf("sanitizePath(%q) returned %v, want success", ok, err)
		}
	}
}
