package nya

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestNYARoundtrip creates a NYA archive from real /usr/bin/ binaries,
// extracts it, and verifies file contents match.
func TestNYARoundtrip(t *testing.T) {
	// Pick a few small binaries from /usr/bin/
	candidates := []string{"true", "false", "yes", "env", "whoami", "id", "pwd", "uname"}
	var srcFiles []string
	for _, name := range candidates {
		p := filepath.Join("/usr/bin", name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Size() < 1<<20 {
			srcFiles = append(srcFiles, p)
		}
		if len(srcFiles) >= 4 {
			break
		}
	}
	if len(srcFiles) == 0 {
		t.Skip("no suitable binaries in /usr/bin/")
	}

	// Stage files into a temp source dir
	srcDir := t.TempDir()
	origData := map[string][]byte{}
	for _, src := range srcFiles {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(src)
		dst := filepath.Join(srcDir, name)
		if err := os.WriteFile(dst, data, 0755); err != nil {
			t.Fatal(err)
		}
		origData[name] = data
	}

	for _, codec := range []string{CompressionLZMA2, CompressionZstd} {
		t.Run(codec, func(t *testing.T) {
			// Create NYA archive
			var buf bytes.Buffer
			ws := &seekBuf{buf: &buf}
			w := NewWriterOpts(ws, 0, 9, false)
			w.SetCompression(codec)
			if err := w.AddFile(srcDir); err != nil {
				t.Fatal("AddFile:", err)
			}
			if err := w.Close(); err != nil {
				t.Fatal("Close:", err)
			}

			// Write to temp file for Open()
			nyaFile := filepath.Join(t.TempDir(), "test.nya")
			if err := os.WriteFile(nyaFile, buf.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: archive size = %d bytes", codec, buf.Len())

			// Extract
			extractDir := filepath.Join(t.TempDir(), "out")
			r, err := Open(nyaFile)
			if err != nil {
				t.Fatal("Open:", err)
			}
			if err := r.Extract(extractDir); err != nil {
				t.Fatal("Extract:", err)
			}

			// Verify each file
			for name, orig := range origData {
				// The extracted path includes the source dir basename
				extracted, err := findFile(extractDir, name)
				if err != nil {
					t.Errorf("file %s not found in extracted output: %v", name, err)
					continue
				}
				data, err := os.ReadFile(extracted)
				if err != nil {
					t.Errorf("read %s: %v", name, err)
					continue
				}
				if !bytes.Equal(orig, data) {
					t.Errorf("file %s: content mismatch (orig %d bytes, got %d bytes)", name, len(orig), len(data))
				}
			}
		})
	}
}

// seekBuf wraps bytes.Buffer to implement io.WriteSeeker.
type seekBuf struct {
	buf *bytes.Buffer
	pos int
}

func (s *seekBuf) Write(p []byte) (int, error) {
	// Extend buffer if needed
	for s.buf.Len() < s.pos+len(p) {
		s.buf.WriteByte(0)
	}
	n := copy(s.buf.Bytes()[s.pos:], p)
	s.pos += n
	return n, nil
}

func (s *seekBuf) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case 0:
		s.pos = int(offset)
	case 1:
		s.pos += int(offset)
	case 2:
		s.pos = s.buf.Len() + int(offset)
	}
	return int64(s.pos), nil
}

// findFile recursively searches for a file by name under root.
func findFile(root, name string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && info.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", os.ErrNotExist
	}
	return found, err
}
