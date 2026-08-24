package nya

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildRawFrame constructs a valid zstd frame with a single raw block
func buildRawFrame(payload []byte) []byte {
	var buf bytes.Buffer
	magic := make([]byte, 4)
	binary.LittleEndian.PutUint32(magic, zstdMagic)
	buf.Write(magic)
	buf.WriteByte(0x20) // single segment, fcs_flag=0
	buf.WriteByte(byte(len(payload)))
	bh := uint32(1) | (0 << 1) | (uint32(len(payload)) << 3)
	buf.WriteByte(byte(bh))
	buf.WriteByte(byte(bh >> 8))
	buf.WriteByte(byte(bh >> 16))
	buf.Write(payload)
	return buf.Bytes()
}

// buildRLEFrame constructs a valid zstd frame with a single RLE block
func buildRLEFrame(b byte, count int) []byte {
	var buf bytes.Buffer
	magic := make([]byte, 4)
	binary.LittleEndian.PutUint32(magic, zstdMagic)
	buf.Write(magic)
	buf.WriteByte(0x20)
	buf.WriteByte(byte(count))
	bh := uint32(1) | (1 << 1) | (uint32(count) << 3)
	buf.WriteByte(byte(bh))
	buf.WriteByte(byte(bh >> 8))
	buf.WriteByte(byte(bh >> 16))
	buf.WriteByte(b)
	return buf.Bytes()
}

func TestDecompressZstdRawBlock(t *testing.T) {
	payload := []byte("Hello, World!")
	frame := buildRawFrame(payload)
	result, err := DecompressZstd(frame)
	if err != nil {
		t.Fatalf("DecompressZstd raw block: %v", err)
	}
	if !bytes.Equal(result, payload) {
		t.Fatalf("raw block mismatch: got %q, want %q", result, payload)
	}
}

func TestDecompressZstdRLEBlock(t *testing.T) {
	result, err := DecompressZstd(buildRLEFrame('X', 100))
	if err != nil {
		t.Fatalf("DecompressZstd RLE block: %v", err)
	}
	expected := bytes.Repeat([]byte{'X'}, 100)
	if !bytes.Equal(result, expected) {
		t.Fatalf("RLE block mismatch: got len=%d, want len=100", len(result))
	}
}

func TestDecompressZstdCompressedBlock(t *testing.T) {
	// Real zstd-compressed data: "AAA...BBB...AAA...\n" (200+200+200+newline)
	compressed := []byte{
		0x28, 0xb5, 0x2f, 0xfd, 0x04, 0x58, 0x7d, 0x00,
		0x00, 0x28, 0x41, 0x41, 0x42, 0x41, 0x0a, 0x03,
		0x14, 0x00, 0x2b, 0x44, 0x21, 0xde, 0x21, 0x16,
		0x52, 0xa7, 0x54, 0x8f,
	}

	result, err := DecompressZstd(compressed)
	if err != nil {
		t.Fatalf("DecompressZstd compressed block: %v", err)
	}

	expected := make([]byte, 0, 601)
	for i := 0; i < 200; i++ {
		expected = append(expected, 'A')
	}
	for i := 0; i < 200; i++ {
		expected = append(expected, 'B')
	}
	for i := 0; i < 200; i++ {
		expected = append(expected, 'A')
	}
	expected = append(expected, '\n')

	if !bytes.Equal(result, expected) {
		t.Fatalf("compressed block mismatch: got len=%d, want len=%d", len(result), len(expected))
	}
}

func TestDecompressZstdInvalidMagic(t *testing.T) {
	_, err := DecompressZstd([]byte{0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Fatal("expected error for invalid magic")
	}
}

func TestDecompressZstdTooShort(t *testing.T) {
	_, err := DecompressZstd([]byte{0x28, 0xb5})
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}
