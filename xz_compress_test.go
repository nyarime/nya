package nya

import (
	"bytes"
	"testing"
)

func TestXzCompressRoundtrip(t *testing.T) {
	original := []byte("Hello, XZ compression roundtrip test! This needs to be long enough to actually compress meaningfully. " +
		"Adding more text here to ensure we have enough data for LZMA2 to work with properly.")

	compressed, err := XzCompress(original, 0)
	if err != nil {
		t.Fatalf("XzCompress failed: %v", err)
	}

	// Check XZ magic
	if len(compressed) < 6 || compressed[0] != 0xFD || compressed[1] != 0x37 {
		t.Fatal("missing XZ magic bytes")
	}

	decompressed, err := XzDecompress(compressed)
	if err != nil {
		t.Fatalf("XzDecompress failed: %v", err)
	}

	if !bytes.Equal(original, decompressed) {
		t.Fatalf("roundtrip mismatch: got %d bytes, want %d bytes", len(decompressed), len(original))
	}
}

func TestDeltaFilterRoundtrip(t *testing.T) {
	original := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	data := make([]byte, len(original))
	copy(data, original)

	DeltaFilter(data, 1, true)
	DeltaFilter(data, 1, false)

	if !bytes.Equal(original, data) {
		t.Fatalf("delta roundtrip mismatch")
	}
}
