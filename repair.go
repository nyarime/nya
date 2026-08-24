package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// RepairResult 修复结果
type RepairResult struct {
	TotalChunks    int
	CorruptedChunks int
	RepairedChunks  int
	FailedChunks    int
}

// Repair 修复损坏的.nar文件
func Repair(path string, outputPath string) (*RepairResult, error) {
	r, err := Open(path)
	if err != nil {
		// Central Dir损坏, 尝试raw repair
		return rawRepair(path, outputPath)
	}

	result := &RepairResult{}
	
	// 从文件读FEC data(不在内存中)
	if r.FecLen > 0 && len(r.fecData) == 0 {
		ff, err := os.Open(path)
		if err == nil {
			r.fecData = make([]byte, r.FecLen)
			ff.ReadAt(r.fecData, r.FecOffset)
			ff.Close()
		}
	}
	if len(r.HashTables) > 0 && len(r.HashTables[0]) >= 3 {
	}

	// 固实模式: 整个solid chunk作为一个单元修复
	if r.Header.Flags&FlagSolidCompress != 0 {
		return repairSolid(r, path, outputPath)
	}

	for _, e := range r.Entries {
		if e.EntryType != EntryFile { continue }

		off := e.FirstDataOff
		if off+ChunkHeaderSize > uint64(len(r.data)) { continue }

		chBuf := bytes.NewReader(r.data[off:])
		ch, err := ReadChunkHeader(chBuf)
		if err != nil { continue }

		result.TotalChunks++

		// 读压缩数据
		compStart := off + ChunkHeaderSize
		compEnd := compStart + ch.CompressedSize
		if compEnd > uint64(len(r.data)) { continue }
		compData := r.data[compStart:compEnd]

		// FEC数据在文件末尾(Central Dir后), 不在Data Area
		fecData := r.fecData

		// CRC校验
		h := Blake3Sum256(compData); actualHash := binary.LittleEndian.Uint64(h[:8])
		if actualHash != ch.Blake3Short {
			result.CorruptedChunks++
			logf("  chunk %s: CRC mismatch (expected %x, got %x)\n", e.Path, ch.Blake3Short, actualHash)
			logf("  ⚠️ %s: CRC不匹配, 尝试修复...\n", e.Path)

			// RaptorQ修复
			// 合并所有hash为flat
			var allH []uint32; if len(r.HashTables) > 0 { allH = r.HashTables[0] }
			repaired, err := raptorqRepair(compData, fecData, int(e.FECParams.Param3), allH)
			if err != nil {
				result.FailedChunks++
				logf("  ❌ %s: 修复失败\n", e.Path)
				continue
			}

			// 写回修复数据
			copy(r.data[compStart:compEnd], repaired[:len(compData)])

			// 更新CRC
			nh := Blake3Sum256(repaired[:len(compData)]); newHash := binary.LittleEndian.Uint64(nh[:8])
			r.data[off+24] = byte(newHash)
			r.data[off+25] = byte(newHash >> 8)
			r.data[off+26] = byte(newHash >> 16)
			r.data[off+27] = byte(newHash >> 24)
			r.data[off+28] = byte(newHash >> 32)
			r.data[off+29] = byte(newHash >> 40)
			r.data[off+30] = byte(newHash >> 48)
			r.data[off+31] = byte(newHash >> 56)

			result.RepairedChunks++
			logf("  ✅ %s: 修复成功!\n", e.Path)
		}
	}

	if result.CorruptedChunks > 0 {
		// 直接写回修复后的数据到文件
		out := outputPath
		if out == "" { out = path }
		wf, werr := os.OpenFile(out, os.O_RDWR, 0644)
		if werr == nil {
			// 写回整个data area
			wf.WriteAt(r.data, GlobalHeaderSize)
			wf.Close()
		}
	}

	return result, nil
}

func repairSolid(r *Reader, path, outputPath string) (*RepairResult, error) {
	result := &RepairResult{TotalChunks: 1}

	if len(r.data) < ChunkHeaderSize {
		return result, ErrCorrupted
	}

	chBuf := bytes.NewReader(r.data)
	ch, err := ReadChunkHeader(chBuf)
	if err != nil { return result, err }

	compData := r.data[ChunkHeaderSize : ChunkHeaderSize+ch.CompressedSize]
	fecData := r.fecData

	h := Blake3Sum256(compData)
	actualHash := binary.LittleEndian.Uint64(h[:8])

	if actualHash != ch.Blake3Short {
		result.CorruptedChunks = 1
		logf("  ⚠️ Solid chunk损坏, 尝试修复..." + "\n")

		repaired, err := raptorqRepair(compData, fecData, 10)
		if err != nil {
			result.FailedChunks = 1
			return result, nil
		}
		copy(r.data[ChunkHeaderSize:], repaired[:len(compData)])
		result.RepairedChunks = 1
		logf("  ✅ Solid chunk修复成功!" + "\n")

		out := outputPath
		if out == "" { out = path }
		raw, _ := os.ReadFile(path)
		copy(raw[GlobalHeaderSize:], r.data)
		os.WriteFile(out, raw, 0644)
	}

	return result, nil
}

// rawRepair: CentralDir damaged, use GlobalHeader to locate FEC
func rawRepair(path string, outputPath string) (*RepairResult, error) {
	f, err := os.Open(path)
	if err != nil { return nil, err }
	defer f.Close()

	gh, err := ReadGlobalHeader(f)
	if err != nil { return nil, fmt.Errorf("global header corrupt: %w", err) }

	logf("  Raw repair mode (CentralDir damaged)\n")
	logf("  DataArea: %d, CentralDir at %d (%d bytes)\n",
		gh.DataAreaSize, gh.CentralDirOffset, gh.CentralDirSize)

	data := make([]byte, gh.DataAreaSize)
	f.Seek(GlobalHeaderSize, 0)
	io.ReadFull(f, data)

	// FEC位置: 用GlobalHeader精确定位
	fecLenPos := int64(gh.CentralDirOffset) + int64(gh.CentralDirSize)
	f.Seek(fecLenPos, 0)
	var fecLen uint32
	binary.Read(f, binary.LittleEndian, &fecLen)

	if fecLen == 0 || int64(fecLen) > (1<<30) {
		return nil, fmt.Errorf("FEC header corrupt (fecLen=%d)", fecLen)
	}

	fecStart := fecLenPos + 4
	logf("  FEC: %d bytes at offset %d\n", fecLen, fecStart)

	fecData := make([]byte, fecLen)
	f.ReadAt(fecData, fecStart)

	// Hash表
	hashPos := fecStart + int64(fecLen)
	f.Seek(hashPos, 0)
	var hashCount uint32
	binary.Read(f, binary.LittleEndian, &hashCount)
	if hashCount > 100000000 { hashCount = 0 }
	var hashes []uint32
	for i := uint32(0); i < hashCount; i++ {
		var h uint32
		binary.Read(f, binary.LittleEndian, &h)
		hashes = append(hashes, h)
	}
	logf("  Hashes: %d\n", hashCount)

	if len(data) < ChunkHeaderSize {
		return nil, fmt.Errorf("data too small")
	}
	chBuf := bytes.NewReader(data)
	ch, err := ReadChunkHeader(chBuf)
	if err != nil { return nil, fmt.Errorf("chunk header corrupt: %w", err) }

	compStart := uint64(ChunkHeaderSize)
	compEnd := compStart + uint64(ch.CompressedSize)
	if compEnd > uint64(len(data)) { compEnd = uint64(len(data)) }
	compData := data[compStart:compEnd]

	logf("  Chunk: comp=%d, repair=%d, sym=%d\n",
		ch.CompressedSize, ch.RepairCount, ch.SymbolSize)

	repaired, err := raptorqRepair(compData, fecData, int(ch.RepairCount), hashes)
	if err != nil {
		return &RepairResult{TotalChunks: 1, CorruptedChunks: 1, FailedChunks: 1},
			fmt.Errorf("FEC failed: %w", err)
	}

	copy(data[compStart:compEnd], repaired[:len(compData)])

	out := outputPath
	if out == "" { out = path }
	wf, _ := os.OpenFile(out, os.O_RDWR, 0644)
	if wf != nil {
		wf.WriteAt(data, GlobalHeaderSize)
		wf.Close()
	}

	logf("  ✅ Raw repair!" + "\n")
	return &RepairResult{TotalChunks: 1, CorruptedChunks: 1, RepairedChunks: 1}, nil
}
