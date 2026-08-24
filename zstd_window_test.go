package nya

import (
	"bytes"
	"fmt"
	"testing"
)

func TestZstdCompressWithWindow(t *testing.T) {
	// 2MB of repeating pattern — crosses 512KB block boundaries
	data := bytes.Repeat([]byte("pattern_abcdefgh"), 128*1024) // 2MB

	comp1 := ZstdCompress(data, 3)
	comp2 := ZstdCompressWithWindow(data, 3)

	fmt.Printf("Original:  %d bytes\n", len(data))
	fmt.Printf("No window: %d bytes (%.1f%%)\n", len(comp1), float64(len(comp1))*100/float64(len(data)))
	fmt.Printf("Windowed:  %d bytes (%.1f%%)\n", len(comp2), float64(len(comp2))*100/float64(len(data)))

	// Verify decompression
	dec1, err := ZstdDecompress(comp1)
	if err != nil {
		t.Fatalf("decompress no-window: %v", err)
	}
	if !bytes.Equal(dec1, data) {
		t.Fatal("no-window roundtrip mismatch")
	}

	dec2, err := ZstdDecompress(comp2)
	if err != nil {
		t.Fatalf("decompress windowed: %v", err)
	}
	if !bytes.Equal(dec2, data) {
		t.Fatal("windowed roundtrip mismatch")
	}

	// Windowed should be <= no-window (ideally smaller)
	if len(comp2) > len(comp1)*105/100 {
		t.Errorf("windowed compression worse than expected: %d vs %d", len(comp2), len(comp1))
	}
}

func TestZstdCompressWithDict(t *testing.T) {
	dict := bytes.Repeat([]byte("dictionary_data_"), 1024) // 16KB dict
	data := append([]byte("some unique header "), bytes.Repeat([]byte("dictionary_data_"), 512)...)

	comp1 := ZstdCompress(data, 3)
	comp2 := ZstdCompressWithDict(data, 3, dict)

	fmt.Printf("Dict test - Original: %d, No dict: %d, With dict: %d\n", len(data), len(comp1), len(comp2))

	dec2, err := ZstdDecompress(comp2)
	if err != nil {
		t.Fatalf("decompress with dict: %v", err)
	}
	if !bytes.Equal(dec2, data) {
		t.Fatal("dict roundtrip mismatch")
	}
}

func TestZstdWindowSmallData(t *testing.T) {
	// Small data should still work
	data := []byte("hello world")
	comp := ZstdCompressWithWindow(data, 3)
	dec, err := ZstdDecompress(comp)
	if err != nil {
		t.Fatalf("decompress small: %v", err)
	}
	if !bytes.Equal(dec, data) {
		t.Fatal("small roundtrip mismatch")
	}
}

func TestZstdWindowEmpty(t *testing.T) {
	comp := ZstdCompressWithWindow(nil, 3)
	dec, err := ZstdDecompress(comp)
	if err != nil {
		t.Fatalf("decompress empty: %v", err)
	}
	if len(dec) != 0 {
		t.Fatal("empty roundtrip mismatch")
	}
}
