package nya

import (
	"encoding/binary"
	"fmt"
)

const (
	lz4FrameMagic  = 0x184D2204
	lz4LegacyMagic = 0x184C2102
)

// DecompressLZ4Frame decompresses LZ4 frame format data (magic 0x04224D18).
func DecompressLZ4Frame(data []byte) ([]byte, error) {
	if len(data) < 7 {
		return nil, fmt.Errorf("lz4: data too short for frame header")
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != lz4FrameMagic {
		return nil, fmt.Errorf("lz4: invalid frame magic: 0x%08X", magic)
	}

	pos := 4

	// FLG byte
	if pos >= len(data) {
		return nil, fmt.Errorf("lz4: truncated frame header")
	}
	flg := data[pos]
	pos++

	version := (flg >> 6) & 0x03
	if version != 1 {
		return nil, fmt.Errorf("lz4: unsupported version %d", version)
	}
	blockIndep := (flg>>5)&1 == 1
	_ = blockIndep
	hasBlockChecksum := (flg>>4)&1 == 1
	hasContentSize := (flg>>3)&1 == 1
	hasContentChecksum := (flg>>2)&1 == 1
	hasDictID := (flg >> 0) & 1

	// BD byte
	if pos >= len(data) {
		return nil, fmt.Errorf("lz4: truncated frame header")
	}
	_ = data[pos] // BD byte - block max size, we don't enforce limits
	pos++

	// Optional content size (8 bytes)
	if hasContentSize {
		if pos+8 > len(data) {
			return nil, fmt.Errorf("lz4: truncated content size")
		}
		_ = binary.LittleEndian.Uint64(data[pos : pos+8])
		pos += 8
	}

	// Optional dict ID (4 bytes)
	if hasDictID == 1 {
		if pos+4 > len(data) {
			return nil, fmt.Errorf("lz4: truncated dict id")
		}
		pos += 4
	}

	// Header checksum (1 byte)
	if pos >= len(data) {
		return nil, fmt.Errorf("lz4: truncated header checksum")
	}
	pos++ // skip HC byte

	// Read blocks
	var out []byte
	for {
		if pos+4 > len(data) {
			return nil, fmt.Errorf("lz4: truncated block header")
		}
		blockSize := binary.LittleEndian.Uint32(data[pos : pos+4])
		pos += 4

		if blockSize == 0 {
			// End mark
			break
		}

		uncompressed := blockSize>>31 == 1
		blockSize &= 0x7FFFFFFF

		if pos+int(blockSize) > len(data) {
			return nil, fmt.Errorf("lz4: truncated block data (need %d, have %d)", blockSize, len(data)-pos)
		}

		blockData := data[pos : pos+int(blockSize)]
		pos += int(blockSize)

		if hasBlockChecksum {
			if pos+4 > len(data) {
				return nil, fmt.Errorf("lz4: truncated block checksum")
			}
			pos += 4 // skip block checksum
		}

		if uncompressed {
			out = append(out, blockData...)
		} else {
			decoded, err := decompressLZ4Block(blockData, out)
			if err != nil {
				return nil, fmt.Errorf("lz4: block decompress error: %w", err)
			}
			out = decoded
		}
	}

	// Optional content checksum
	if hasContentChecksum {
		if pos+4 > len(data) {
			return nil, fmt.Errorf("lz4: truncated content checksum")
		}
		// skip content checksum validation
	}

	return out, nil
}

// DecompressLZ4Legacy decompresses LZ4 legacy format data (magic 0x02214C18).
func DecompressLZ4Legacy(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("lz4: data too short")
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != lz4LegacyMagic {
		return nil, fmt.Errorf("lz4: invalid legacy magic: 0x%08X", magic)
	}

	pos := 4
	var out []byte

	for pos < len(data) {
		if pos+4 > len(data) {
			return nil, fmt.Errorf("lz4: truncated legacy block header")
		}
		blockSize := binary.LittleEndian.Uint32(data[pos : pos+4])
		pos += 4

		if blockSize == 0 {
			break
		}

		uncompressed := blockSize>>31 == 1
		blockSize &= 0x7FFFFFFF

		if pos+int(blockSize) > len(data) {
			return nil, fmt.Errorf("lz4: truncated legacy block data")
		}

		blockData := data[pos : pos+int(blockSize)]
		pos += int(blockSize)

		if uncompressed {
			out = append(out, blockData...)
		} else {
			decoded, err := decompressLZ4Block(blockData, out)
			if err != nil {
				return nil, fmt.Errorf("lz4: legacy block decompress error: %w", err)
			}
			out = decoded
		}
	}

	return out, nil
}

// decompressLZ4Block decompresses a single LZ4 block.
// prevOutput is used for back-references in dependent blocks.
func decompressLZ4Block(src []byte, prevOutput []byte) ([]byte, error) {
	out := prevOutput
	sPos := 0

	for sPos < len(src) {
		// Token byte
		token := src[sPos]
		sPos++

		// Literal length
		litLen := int(token >> 4)
		if litLen == 15 {
			for {
				if sPos >= len(src) {
					return nil, fmt.Errorf("lz4: truncated literal length")
				}
				b := src[sPos]
				sPos++
				litLen += int(b)
				if b != 255 {
					break
				}
			}
		}

		// Copy literals
		if sPos+litLen > len(src) {
			return nil, fmt.Errorf("lz4: truncated literals (need %d at pos %d, have %d)", litLen, sPos, len(src))
		}
		out = append(out, src[sPos:sPos+litLen]...)
		sPos += litLen

		// Check if this was the last sequence (no match after literals)
		if sPos >= len(src) {
			break
		}

		// Match offset (2 bytes LE)
		if sPos+2 > len(src) {
			return nil, fmt.Errorf("lz4: truncated match offset")
		}
		offset := int(binary.LittleEndian.Uint16(src[sPos : sPos+2]))
		sPos += 2

		if offset == 0 {
			return nil, fmt.Errorf("lz4: invalid zero offset")
		}

		// Match length
		matchLen := int(token&0x0F) + 4
		if token&0x0F == 15 {
			for {
				if sPos >= len(src) {
					return nil, fmt.Errorf("lz4: truncated match length")
				}
				b := src[sPos]
				sPos++
				matchLen += int(b)
				if b != 255 {
					break
				}
			}
		}

		// Copy match (byte-by-byte for overlapping)
		matchStart := len(out) - offset
		if matchStart < 0 {
			return nil, fmt.Errorf("lz4: match offset %d exceeds output size %d", offset, len(out))
		}

		for i := 0; i < matchLen; i++ {
			out = append(out, out[matchStart+i])
		}
	}

	return out, nil
}
