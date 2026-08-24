package nya

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildLZ4Frame builds a minimal LZ4 frame with a single compressed block.
func buildLZ4Frame(compressedBlock []byte, uncompressed bool) []byte {
	var buf bytes.Buffer

	// Magic
	binary.Write(&buf, binary.LittleEndian, uint32(lz4FrameMagic))

	// FLG: version=1 (bits 6-7 = 01), block independence=1, no other flags
	flg := byte(0x60) // version 01, block indep
	buf.WriteByte(flg)

	// BD: block max size = 4 (64KB), bits 4-6
	bd := byte(0x40)
	buf.WriteByte(bd)

	// HC (header checksum) - we skip validation so any byte works
	buf.WriteByte(0x00)

	// Block
	blockSize := uint32(len(compressedBlock))
	if uncompressed {
		blockSize |= 0x80000000
	}
	binary.Write(&buf, binary.LittleEndian, blockSize)
	buf.Write(compressedBlock)

	// End mark
	binary.Write(&buf, binary.LittleEndian, uint32(0))

	return buf.Bytes()
}

func TestDecompressLZ4FrameUncompressedBlock(t *testing.T) {
	original := []byte("Hello, LZ4 world! This is a test of uncompressed blocks.")
	frame := buildLZ4Frame(original, true)

	out, err := DecompressLZ4Frame(frame)
	if err != nil {
		t.Fatalf("DecompressLZ4Frame error: %v", err)
	}
	if !bytes.Equal(out, original) {
		t.Fatalf("mismatch: got %q, want %q", out, original)
	}
}

func TestDecompressLZ4FrameCompressedBlock(t *testing.T) {
	// Hand-craft a compressed block:
	// "ABCDABCD" - 4 literals "ABCD", then match offset=4 length=4
	// Token: litLen=4, matchLen=4-4=0 => token = 0x40
	// Literals: ABCD
	// Offset: 4 (LE)
	compressed := []byte{
		0x40,               // token: litLen=4, matchLen=0 (meaning 4)
		'A', 'B', 'C', 'D', // literals
		0x04, 0x00, // offset = 4
	}

	frame := buildLZ4Frame(compressed, false)

	out, err := DecompressLZ4Frame(frame)
	if err != nil {
		t.Fatalf("DecompressLZ4Frame error: %v", err)
	}

	expected := []byte("ABCDABCD")
	if !bytes.Equal(out, expected) {
		t.Fatalf("mismatch: got %q, want %q", out, expected)
	}
}

func TestDecompressLZ4FrameOverlappingMatch(t *testing.T) {
	// "AAAAAAAAAAAA" (12 A's)
	// Token: litLen=1, matchLen=11-4=7 => token = 0x17
	// Literal: A
	// Offset: 1
	compressed := []byte{
		0x17,       // token: litLen=1, matchLen=7 (meaning 11)
		'A',        // literal
		0x01, 0x00, // offset = 1
	}

	frame := buildLZ4Frame(compressed, false)

	out, err := DecompressLZ4Frame(frame)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	expected := bytes.Repeat([]byte("A"), 12)
	if !bytes.Equal(out, expected) {
		t.Fatalf("mismatch: got %q (len %d), want %q (len %d)", out, len(out), expected, len(expected))
	}
}

func TestDecompressLZ4FrameExtendedLiterals(t *testing.T) {
	// 20 literal bytes, no match (last sequence)
	// litLen=20: high nibble=15, then extra byte=5
	// Token: 0xF0, extra: 0x05
	lits := bytes.Repeat([]byte("X"), 20)
	compressed := []byte{0xF0, 0x05}
	compressed = append(compressed, lits...)

	frame := buildLZ4Frame(compressed, false)

	out, err := DecompressLZ4Frame(frame)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !bytes.Equal(out, lits) {
		t.Fatalf("mismatch: got len %d, want 20", len(out))
	}
}

func TestDecompressLZ4Legacy(t *testing.T) {
	original := []byte("Legacy LZ4 test data here!")

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(lz4LegacyMagic))

	// Uncompressed block
	blockSize := uint32(len(original)) | 0x80000000
	binary.Write(&buf, binary.LittleEndian, blockSize)
	buf.Write(original)

	out, err := DecompressLZ4Legacy(buf.Bytes())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !bytes.Equal(out, original) {
		t.Fatalf("mismatch: got %q, want %q", out, original)
	}
}

func TestDecompressLZ4FrameWithContentSize(t *testing.T) {
	original := []byte("Content size test")

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(lz4FrameMagic))

	// FLG: version=1, block indep, content size present
	flg := byte(0x68) // 0110 1000
	buf.WriteByte(flg)
	buf.WriteByte(0x40) // BD

	// Content size (8 bytes)
	binary.Write(&buf, binary.LittleEndian, uint64(len(original)))

	// HC
	buf.WriteByte(0x00)

	// Uncompressed block
	blockSize := uint32(len(original)) | 0x80000000
	binary.Write(&buf, binary.LittleEndian, blockSize)
	buf.Write(original)

	// End mark
	binary.Write(&buf, binary.LittleEndian, uint32(0))

	out, err := DecompressLZ4Frame(buf.Bytes())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !bytes.Equal(out, original) {
		t.Fatalf("mismatch")
	}
}
