package nya

import (
	"bytes"
	"testing"
)

func TestLzmaRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"short", []byte("hello world")},
		{"repeated", bytes.Repeat([]byte("ABCDEFGH"), 1000)},
		{"zeros", make([]byte, 10000)},
		{"sequential", func() []byte {
			b := make([]byte, 8192)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_lzma", func(t *testing.T) {
			if len(tt.data) == 0 {
				t.Skip("empty data not supported by plain LZMA header")
			}
			comp, err := LzmaCompress(tt.data)
			if err != nil {
				t.Fatalf("compress: %v", err)
			}
			dec, err := LzmaDecompress(comp)
			if err != nil {
				t.Fatalf("decompress: %v (compressed %d bytes)", err, len(comp))
			}
			if !bytes.Equal(dec, tt.data) {
				t.Fatalf("roundtrip mismatch: got %d bytes, want %d", len(dec), len(tt.data))
			}
			t.Logf("OK: %d → %d bytes (%.1f:1)", len(tt.data), len(comp), float64(len(tt.data))/float64(len(comp)))
		})

		t.Run(tt.name+"_lzma2", func(t *testing.T) {
			comp, err := Lzma2Compress(tt.data, 0)
			if err != nil {
				t.Fatalf("compress: %v", err)
			}
			dec, err := decompressLZMA2Raw(comp)
			if err != nil {
				t.Fatalf("decompress: %v (compressed %d bytes)", err, len(comp))
			}
			if !bytes.Equal(dec, tt.data) {
				t.Fatalf("roundtrip mismatch: got %d bytes, want %d", len(dec), len(tt.data))
			}
			t.Logf("OK: %d → %d bytes", len(tt.data), len(comp))
		})
	}
}

// decompressLZMA2Raw decompresses raw LZMA2 data (no XZ container).
func decompressLZMA2Raw(data []byte) ([]byte, error) {
	r := newLzma2Reader(bytes.NewReader(data), 1<<22)
	var out bytes.Buffer
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
	}
	return out.Bytes(), nil
}
