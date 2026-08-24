package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

var (
	zipLocalSig   = []byte{0x50, 0x4b, 0x03, 0x04}
	zipCentralSig = []byte{0x50, 0x4b, 0x01, 0x02}
	zipEndSig     = []byte{0x50, 0x4b, 0x05, 0x06}
)

func repairZipArchive(inputPath, outputPath string) (*RepairResult, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("repair zip: read: %w", err)
	}

	result := &RepairResult{Format: FormatZIP}

	type localFile struct {
		offset     int
		nameLen    uint16
		extraLen   uint16
		compSize   uint32
		uncompSize uint32
		method     uint16
		crc32      uint32
		name       string
		dataOffset int
		dataLen    int
	}

	var files []localFile
	for i := 0; i <= len(data)-30; i++ {
		if !bytes.Equal(data[i:i+4], zipLocalSig) {
			continue
		}
		if i+30 > len(data) {
			break
		}

		var lf localFile
		lf.offset = i
		lf.method = binary.LittleEndian.Uint16(data[i+8 : i+10])
		lf.crc32 = binary.LittleEndian.Uint32(data[i+14 : i+18])
		lf.compSize = binary.LittleEndian.Uint32(data[i+18 : i+22])
		lf.uncompSize = binary.LittleEndian.Uint32(data[i+22 : i+26])
		lf.nameLen = binary.LittleEndian.Uint16(data[i+26 : i+28])
		lf.extraLen = binary.LittleEndian.Uint16(data[i+28 : i+30])

		nameStart := i + 30
		if nameStart+int(lf.nameLen) > len(data) {
			break
		}
		lf.name = string(data[nameStart : nameStart+int(lf.nameLen)])
		lf.dataOffset = nameStart + int(lf.nameLen) + int(lf.extraLen)

		if lf.compSize > 0 {
			lf.dataLen = int(lf.compSize)
		} else {
			nextSig := len(data)
			for j := lf.dataOffset + 1; j <= len(data)-4; j++ {
				if bytes.Equal(data[j:j+4], zipLocalSig) ||
					bytes.Equal(data[j:j+4], zipCentralSig) ||
					bytes.Equal(data[j:j+4], zipEndSig) {
					nextSig = j
					break
				}
			}
			lf.dataLen = nextSig - lf.dataOffset
		}

		if lf.dataOffset+lf.dataLen <= len(data) {
			files = append(files, lf)
			result.FilesFound++
		}
		i = lf.dataOffset + lf.dataLen - 1
	}

	if len(files) == 0 {
		return result, fmt.Errorf("repair zip: no valid local file headers found")
	}

	var out bytes.Buffer
	type cdEntry struct {
		offset     uint32
		method     uint16
		crc32      uint32
		compSize   uint32
		uncompSize uint32
		name       string
	}
	var cdEntries []cdEntry
	var currentOffset uint32

	for _, lf := range files {
		headerEnd := lf.dataOffset + lf.dataLen
		if headerEnd > len(data) {
			result.FailedChunks++
			continue
		}
		n, err := out.Write(data[lf.offset:headerEnd])
		if err != nil {
			result.FailedChunks++
			continue
		}
		cdEntries = append(cdEntries, cdEntry{
			offset:     currentOffset,
			method:     lf.method,
			crc32:      lf.crc32,
			compSize:   lf.compSize,
			uncompSize: lf.uncompSize,
			name:       lf.name,
		})
		currentOffset += uint32(n)
		result.RepairedChunks++
	}

	cdOffset := currentOffset
	for _, cd := range cdEntries {
		var buf bytes.Buffer
		buf.Write(zipCentralSig)
		binary.Write(&buf, binary.LittleEndian, uint16(20))
		binary.Write(&buf, binary.LittleEndian, uint16(20))
		binary.Write(&buf, binary.LittleEndian, uint16(0))
		binary.Write(&buf, binary.LittleEndian, cd.method)
		binary.Write(&buf, binary.LittleEndian, uint16(0))
		binary.Write(&buf, binary.LittleEndian, uint16(0))
		binary.Write(&buf, binary.LittleEndian, cd.crc32)
		binary.Write(&buf, binary.LittleEndian, cd.compSize)
		binary.Write(&buf, binary.LittleEndian, cd.uncompSize)
		binary.Write(&buf, binary.LittleEndian, uint16(len(cd.name)))
		binary.Write(&buf, binary.LittleEndian, uint16(0))
		binary.Write(&buf, binary.LittleEndian, uint16(0))
		binary.Write(&buf, binary.LittleEndian, uint16(0))
		binary.Write(&buf, binary.LittleEndian, uint16(0))
		binary.Write(&buf, binary.LittleEndian, uint32(0))
		binary.Write(&buf, binary.LittleEndian, cd.offset)
		buf.WriteString(cd.name)
		out.Write(buf.Bytes())
	}
	cdSize := uint32(out.Len()) - cdOffset

	var endBuf bytes.Buffer
	endBuf.Write(zipEndSig)
	binary.Write(&endBuf, binary.LittleEndian, uint16(0))
	binary.Write(&endBuf, binary.LittleEndian, uint16(0))
	binary.Write(&endBuf, binary.LittleEndian, uint16(len(cdEntries)))
	binary.Write(&endBuf, binary.LittleEndian, uint16(len(cdEntries)))
	binary.Write(&endBuf, binary.LittleEndian, cdSize)
	binary.Write(&endBuf, binary.LittleEndian, cdOffset)
	binary.Write(&endBuf, binary.LittleEndian, uint16(0))
	out.Write(endBuf.Bytes())

	outPath := defaultRepairOutput(inputPath, outputPath)
	if err := os.WriteFile(outPath, out.Bytes(), 0o644); err != nil {
		return result, err
	}
	result.OutputPath = outPath
	result.TotalChunks = result.FilesFound
	result.CorruptedChunks = result.FilesFound - result.RepairedChunks
	return result, nil
}
