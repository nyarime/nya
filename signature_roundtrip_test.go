package nya

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestArchivePreservesSignedPEBytes ensures signed executables are stored with
// lossless codecs only and no BCJ pre-filter — extract must match source hash.
func TestArchivePreservesSignedPEBytes(t *testing.T) {
	pe := buildSyntheticPE(0x8664, 0x400, 256)
	opt := int(bytesToU32(pe[0x3C:0x40])) + 24
	secDir := opt + 144
	sigStart := len(pe)
	pe = append(pe, 0x30, 0x82, 0x01, 0x22, 0x33, 0x44, 0x55)
	putU32(pe[secDir:], uint32(sigStart))
	putU32(pe[secDir+4:], uint32(len(pe)-sigStart))

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "signed.exe")
	if err := os.WriteFile(srcPath, pe, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := Blake3Sum256(pe)
	origHash := hex.EncodeToString(orig[:])

	for _, codec := range []string{CompressionZstd, CompressionLZMA2} {
		t.Run(codec, func(t *testing.T) {
			var buf bytes.Buffer
			ws := &seekBuf{buf: &buf}
			w := NewWriterOpts(ws, 0, 3, false)
			w.SetCompression(codec)
			if err := w.AddFile(srcPath); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			nyaFile := filepath.Join(t.TempDir(), "test.nya")
			if err := os.WriteFile(nyaFile, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}

			outDir := filepath.Join(t.TempDir(), "out")
			r, err := Open(nyaFile)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Extract(outDir); err != nil {
				t.Fatal(err)
			}

			got, err := os.ReadFile(filepath.Join(outDir, "signed.exe"))
			if err != nil {
				t.Fatal(err)
			}
			gotHash := Blake3Sum256(got)
			if hex.EncodeToString(gotHash[:]) != origHash {
				t.Fatalf("%s: hash mismatch after extract", codec)
			}
			if !bytes.Equal(got, pe) {
				t.Fatalf("%s: byte mismatch after extract", codec)
			}
		})
	}
}

func bytesToU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func putU32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
