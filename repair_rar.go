package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
)

var (
	rarSig4 = []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00}
	rarSig5 = []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x01, 0x00}
)

const (
	r4TypeFile = 0x74
	r4TypeEnd  = 0x7b
	r4FlagData = 0x8000

	r5TypeFile = 2
	r5TypeEnd  = 5
	r5FlagData = 0x0002
)

func repairRarArchive(inputPath, outputPath string) (*RepairResult, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("repair rar: read: %w", err)
	}
	result := &RepairResult{Format: FormatRAR}

	switch {
	case bytes.HasPrefix(data, rarSig5):
		err = repairRAR5(data, inputPath, outputPath, result)
	case bytes.HasPrefix(data, rarSig4):
		err = repairRAR4(data, inputPath, outputPath, result)
	default:
		return nil, fmt.Errorf("repair rar: missing RAR signature")
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func repairRAR4(data []byte, inputPath, outputPath string, result *RepairResult) error {
	var out bytes.Buffer
	out.Write(rarSig4)
	writeRAR4ArcBlock(&out)
	pos := len(rarSig4)
	seenEnd := false

	for pos+7 <= len(data) {
		blockStart := pos
		typ := data[pos+2]
		flags := binary.LittleEndian.Uint16(data[pos+3 : pos+5])
		hdrSize := int(binary.LittleEndian.Uint16(data[pos+5 : pos+7]))
		if hdrSize < 7 || blockStart+hdrSize > len(data) {
			pos++
			continue
		}
		if !rar4HeaderCRCOK(data[blockStart : blockStart+hdrSize]) {
			pos++
			continue
		}

		blockEnd := blockStart + hdrSize
		if flags&r4FlagData != 0 {
			payloadLen, ok := rar4PayloadSize(data[blockStart+7 : blockStart+hdrSize])
			if !ok || blockEnd+payloadLen > len(data) {
				pos++
				continue
			}
			blockEnd += payloadLen
		}

		switch typ {
		case r4TypeFile:
			result.FilesFound++
			out.Write(data[blockStart:blockEnd])
			result.RepairedChunks++
		case r4TypeEnd:
			seenEnd = true
		}
		pos = blockEnd
	}

	if !seenEnd {
		writeRAR4EndBlock(&out)
	}
	if result.RepairedChunks == 0 {
		return fmt.Errorf("repair rar: no recoverable file blocks")
	}
	outPath := defaultRepairOutput(inputPath, outputPath)
	if err := os.WriteFile(outPath, out.Bytes(), 0o644); err != nil {
		return err
	}
	result.OutputPath = outPath
	result.TotalChunks = result.FilesFound
	result.CorruptedChunks = result.FilesFound - result.RepairedChunks
	return nil
}

func repairRAR5(data []byte, inputPath, outputPath string, result *RepairResult) error {
	var out bytes.Buffer
	out.Write(rarSig5)
	writeRAR5ArcBlock(&out)
	seenEnd := false

	pos := len(rarSig5)
	for pos+4 < len(data) {
		blockStart := pos
		wantCRC := binary.LittleEndian.Uint32(data[pos : pos+4])
		size, sizeLen, ok := readVint(data, pos+4)
		if !ok {
			pos++
			continue
		}
		bodyStart := pos + 4 + sizeLen
		bodyEnd := bodyStart + int(size)
		if bodyEnd > len(data) {
			pos++
			continue
		}
		crcPayload := append(append([]byte{}, data[pos+4:bodyStart]...), data[bodyStart:bodyEnd]...)
		if crc32.ChecksumIEEE(crcPayload) != wantCRC {
			pos++
			continue
		}

		typ, n1, ok := readVint(data, bodyStart)
		if !ok {
			pos++
			continue
		}
		flags, n2, ok := readVint(data, bodyStart+n1)
		if !ok {
			pos++
			continue
		}
		off := n1 + n2
		var dataLen uint64
		if flags&r5FlagData != 0 {
			var n3 int
			dataLen, n3, ok = readVint(data, bodyStart+off)
			if !ok {
				pos++
				continue
			}
			off += n3
		}
		next := bodyEnd + int(dataLen)
		if next > len(data) {
			pos++
			continue
		}

		switch typ {
		case r5TypeFile:
			result.FilesFound++
			out.Write(data[blockStart:next])
			result.RepairedChunks++
		case r5TypeEnd:
			seenEnd = true
		}
		pos = next
	}

	if !seenEnd {
		writeRAR5EndBlock(&out)
	}
	if result.RepairedChunks == 0 {
		return fmt.Errorf("repair rar: no recoverable file blocks")
	}
	outPath := defaultRepairOutput(inputPath, outputPath)
	if err := os.WriteFile(outPath, out.Bytes(), 0o644); err != nil {
		return err
	}
	result.OutputPath = outPath
	result.TotalChunks = result.FilesFound
	result.CorruptedChunks = result.FilesFound - result.RepairedChunks
	return nil
}

func rar4HeaderCRCOK(header []byte) bool {
	if len(header) < 7 {
		return false
	}
	want := binary.LittleEndian.Uint16(header[:2])
	got := uint16(crc32.ChecksumIEEE(header[2:]) & 0xffff)
	return want == got
}

func rar4PayloadSize(body []byte) (int, bool) {
	if len(body) < 4 {
		return 0, false
	}
	return int(binary.LittleEndian.Uint32(body[:4])), true
}

func writeRAR4ArcBlock(out *bytes.Buffer) {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0})
	buf.WriteByte(0x73)
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(7))
	block := buf.Bytes()
	crc := uint16(crc32.ChecksumIEEE(block[2:]) & 0xffff)
	binary.LittleEndian.PutUint16(block[:2], crc)
	out.Write(block)
}

func writeRAR4EndBlock(out *bytes.Buffer) {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0})
	buf.WriteByte(r4TypeEnd)
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(7))
	block := buf.Bytes()
	crc := uint16(crc32.ChecksumIEEE(block[2:]) & 0xffff)
	binary.LittleEndian.PutUint16(block[:2], crc)
	out.Write(block)
}

func writeRAR5ArcBlock(out *bytes.Buffer) {
	var body bytes.Buffer
	writeVint(&body, 1)
	writeVint(&body, 0)
	var sizeVint bytes.Buffer
	writeVint(&sizeVint, uint64(body.Len()))
	crcPayload := append(sizeVint.Bytes(), body.Bytes()...)
	binary.Write(out, binary.LittleEndian, crc32.ChecksumIEEE(crcPayload))
	out.Write(crcPayload)
}

func writeRAR5EndBlock(out *bytes.Buffer) {
	var body bytes.Buffer
	writeVint(&body, r5TypeEnd)
	writeVint(&body, 0)
	var sizeVint bytes.Buffer
	writeVint(&sizeVint, uint64(body.Len()))
	crcPayload := append(sizeVint.Bytes(), body.Bytes()...)
	binary.Write(out, binary.LittleEndian, crc32.ChecksumIEEE(crcPayload))
	out.Write(crcPayload)
}

func readVint(data []byte, off int) (uint64, int, bool) {
	var v uint64
	var shift uint
	n := 0
	for off+n < len(data) {
		b := data[off+n]
		n++
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return v, n, true
		}
		shift += 7
		if shift > 63 {
			return 0, 0, false
		}
	}
	return 0, 0, false
}

func writeVint(buf *bytes.Buffer, v uint64) {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v > 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if v == 0 {
			break
		}
	}
}
