package nya

import (
	"encoding/binary"
	"hash/crc32"
)

func XzCompress(src []byte, dictSize int) ([]byte, error) {
	if dictSize <= 0 {
		dictSize = 1 << 22
	}

	lzma2Data, err := Lzma2Compress(src, dictSize)
	if err != nil {
		return nil, err
	}

	var out []byte

	// Stream Header (12 bytes)
	out = append(out, 0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00) // magic
	streamFlags := []byte{0x00, 0x00}                       // no integrity check
	crc := crc32.ChecksumIEEE(streamFlags)
	out = append(out, streamFlags...)
	out = binary.LittleEndian.AppendUint32(out, crc)

	// Block Header
	dictProp := xzEncodeDictSize(dictSize)
	filterData := []byte{0x21, 0x01, dictProp} // filter ID=0x21 (LZMA2), size=1, props
	blockFlags := byte(0x00)                    // 1 filter, no sizes

	var blockHeader []byte
	blockHeader = append(blockHeader, 0x00) // placeholder for size
	blockHeader = append(blockHeader, blockFlags)
	blockHeader = append(blockHeader, filterData...)
	// Pad so that total size (including 4-byte CRC) is 4-byte aligned
	for (len(blockHeader)+4)%4 != 0 {
		blockHeader = append(blockHeader, 0x00)
	}
	blockHeader[0] = byte((len(blockHeader)+4)/4 - 1) // headerSize = (byte+1)*4 includes CRC
	hcrc := crc32.ChecksumIEEE(blockHeader)
	blockHeader = binary.LittleEndian.AppendUint32(blockHeader, hcrc)

	out = append(out, blockHeader...)
	out = append(out, lzma2Data...)
	blockPad := (4 - len(lzma2Data)%4) % 4
	for i := 0; i < blockPad; i++ {
		out = append(out, 0x00)
	}

	// Index
	indexStart := len(out)
	out = append(out, 0x00) // index indicator
	out = append(out, 0x01) // 1 record
	unpaddedSize := len(blockHeader) + len(lzma2Data)
	out = append(out, xzEncodeMultiByte(unpaddedSize)...)
	out = append(out, xzEncodeMultiByte(len(src))...)
	for (len(out)-indexStart)%4 != 0 {
		out = append(out, 0x00)
	}
	indexCRC := crc32.ChecksumIEEE(out[indexStart:])
	out = binary.LittleEndian.AppendUint32(out, indexCRC)

	// Stream Footer (12 bytes)
	backwardSize := (len(out) - indexStart) / 4 - 1
	var footer []byte
	footer = binary.LittleEndian.AppendUint32(footer, uint32(backwardSize))
	footer = append(footer, streamFlags...)
	footerCRC := crc32.ChecksumIEEE(footer)
	out = binary.LittleEndian.AppendUint32(out, footerCRC)
	out = append(out, footer...)
	out = append(out, 'Y', 'Z')

	return out, nil
}

func xzEncodeDictSize(size int) byte {
	if size <= 4096 {
		return 0
	}
	for i := byte(0); i < 40; i++ {
		if int((2|uint32(i&1))<<(i/2+11)) >= size {
			return i
		}
	}
	return 39
}

func xzEncodeMultiByte(val int) []byte {
	var out []byte
	for val >= 0x80 {
		out = append(out, byte(val)|0x80)
		val >>= 7
	}
	out = append(out, byte(val))
	return out
}
