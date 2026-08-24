package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"os"
	"path/filepath"
	"time"

)

const (
	MaxDecompressSize = 0 // no hard limit; use ratio detection
	BombRatioThreshold = 1000 // single chunk compression ratio > 1000:1 = suspicious
	BombRepeatThreshold = 10  // 10+ consecutive identical chunk hashes = bomb
)

type Reader struct {
	HashTables [][]uint32
	fecData    []byte
	FecOffset  int64
	FecLen     int64
	Password []byte
	Header  *GlobalHeader
	Entries []DirEntry
	data    []byte
}

func Open(path string, password ...[]byte) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil { return nil, err }
	defer f.Close()

	gh, err := ReadGlobalHeader(f)
	if err != nil { return nil, err }

	data := make([]byte, gh.DataAreaSize)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, fmt.Errorf("read data: %w", err)
	}

	var entryCount uint64
	binary.Read(f, binary.LittleEndian, &entryCount)

	// Bounds check: 防止损坏的entryCount导致panic
	if entryCount > 10000000 {
		return nil, fmt.Errorf("corrupt archive: entry count %d exceeds limit", entryCount)
	}

	entries := make([]DirEntry, 0, entryCount)
	for i := uint64(0); i < entryCount; i++ {
		e, err := ReadDirEntry(f)
		if err != nil { break }
		entries = append(entries, *e)
	}

	// 记录FEC位置: 用GlobalHeader精确定位(不依赖CD解析位置)
	fecLenPos := int64(gh.CentralDirOffset) + int64(gh.CentralDirSize)
	f.Seek(fecLenPos, 0)
	var fecDataLen uint32
	binary.Read(f, binary.LittleEndian, &fecDataLen)
	fecOffset, _ := f.Seek(0, 1)
	f.Seek(int64(fecDataLen), 1)  // 跳过FEC data

	// 读hash表
	var hashTables [][]uint32
	var totalHashes uint32
	binary.Read(f, binary.LittleEndian, &totalHashes)
	if totalHashes > 0 && totalHashes < 100000000 {
		allH := make([]uint32, totalHashes)
		for j := uint32(0); j < totalHashes; j++ { binary.Read(f, binary.LittleEndian, &allH[j]) }
		hashTables = append(hashTables, allH) // 单个flat数组
	}
	r := &Reader{Header: gh, Entries: entries, data: data, HashTables: hashTables, FecOffset: fecOffset, FecLen: int64(fecDataLen)}
	if len(password) > 0 { r.Password = password[0] }
	return r, nil
}

func (r *Reader) List() []DirEntry {
	return r.Entries
}

func (r *Reader) Extract(dir string) error {
	if r.Header.Flags&FlagSolidCompress != 0 {
		return r.extractSolid(dir)
	}

	for _, e := range r.Entries {
		outPath, err := sanitizePath(dir, e.Path)
		if err != nil { return err }
		os.MkdirAll(filepath.Dir(outPath), 0755)

		// Handle special entry types
		switch e.EntryType {
		case EntryDir:
			os.MkdirAll(outPath, os.FileMode(e.Mode))
			restoreMeta(outPath, &e)
			continue
		case EntrySymlink:
			os.Remove(outPath)
			os.Symlink(e.LinkTarget, outPath)
			os.Lchown(outPath, int(e.Uid), int(e.Gid))
			fmt.Printf("  ✅ %s -> %s (symlink)\n", e.Path, e.LinkTarget)
			continue
		case EntryHardlink:
			target, _ := sanitizePath(dir, e.LinkTarget)
			os.Remove(outPath)
			os.Link(target, outPath)
			fmt.Printf("  ✅ %s => %s (hardlink)\n", e.Path, e.LinkTarget)
			continue
		case EntryCharDev, EntryBlockDev:
			var mode uint32 = 0666
			if e.EntryType == EntryCharDev {
				mode |= 0020000 // S_IFCHR
			} else {
				mode |= 0060000 // S_IFBLK
			}
			if err := mknod(outPath, mode, e.DevMajor, e.DevMinor); err != nil {
				fmt.Printf("  ⚠️ %s (device node: %v)\n", e.Path, err)
			} else {
				restoreMeta(outPath, &e)
				fmt.Printf("  ✅ %s (device %d:%d)\n", e.Path, e.DevMajor, e.DevMinor)
			}
			continue
		case EntryFifo:
			mkfifo(outPath, e.Mode)
			restoreMeta(outPath, &e)
			fmt.Printf("  ✅ %s (fifo)\n", e.Path)
			continue
		}

		// EntryFile
		if e.EntryType != EntryFile { continue }

		var fullData bytes.Buffer
		off := e.FirstDataOff

		for c := uint32(0); c < e.ChunkCount; c++ {
			if off+ChunkHeaderSize > uint64(len(r.data)) { break }

			chBuf := bytes.NewReader(r.data[off:])
			ch, err := ReadChunkHeader(chBuf)
			if err != nil { break }

			compData := make([]byte, ch.CompressedSize)
			chBuf.Read(compData)

			// 跳过FEC数据 + per-symbol hash表
			fecSize := uint64(ch.RepairCount) * uint64(ch.SymbolSize)

			// 解密(可选)
			if len(r.Password) > 0 {
				dec2, err := Decrypt(compData, r.Password)
				if err == nil { compData = dec2 }
			}
			// 解压独立帧
			var raw []byte
			pos := 0
			for pos+4 <= len(compData) {
				blockLen := int(binary.LittleEndian.Uint32(compData[pos : pos+4]))
				pos += 4
				if pos+blockLen > len(compData) { break }
				blockData := compData[pos : pos+blockLen]
				var block []byte
				if e.CompressionID == CompressLzma2 {
					block, err = decompressLzma2Block(blockData)
				} else {
					dec, derr := ZstdNewReader(bytes.NewReader(blockData))
					if derr != nil { break }
					block, err = io.ReadAll(dec)
					dec.Close()
				}
				if err != nil { break }
				raw = append(raw, block...)
				pos += blockLen
			}

			// Reverse BCJ filter if applied
			if e.BCJFilter != BCJNone {
				arch := BCJIDToArch(e.BCJFilter)
				if arch != "" {
					ApplyBCJFilterArch(raw, arch, false)
				}
			}

			fullData.Write(raw)
			// Bomb detection: check compression ratio per chunk
			if ch.CompressedSize > 0 && len(raw) > 0 {
				ratio := uint64(len(raw)) / uint64(ch.CompressedSize)
				if ratio > BombRatioThreshold {
					return fmt.Errorf("bomb detected: chunk ratio %d:1 exceeds threshold %d:1", ratio, BombRatioThreshold)
				}
			}
			off += ChunkHeaderSize + uint64(ch.CompressedSize) + fecSize
		}

		if err := checkSymlink(outPath); err != nil { return err }
		os.WriteFile(outPath, fullData.Bytes(), os.FileMode(e.Mode))
		restoreMeta(outPath, &e)
		fmt.Printf("  ✅ %s (%s, %d chunks)\n", e.Path, HumanSize(fullData.Len()), e.ChunkCount)
	}
	return nil
}

func (r *Reader) Verify() bool {
	for _, e := range r.Entries {
		if e.EntryType != EntryFile { continue }
		off := e.FirstDataOff
		for c := uint32(0); c < e.ChunkCount; c++ {
			if off+ChunkHeaderSize > uint64(len(r.data)) { return false }
			chBuf := bytes.NewReader(r.data[off:])
			ch, _ := ReadChunkHeader(chBuf)
			
			// BLAKE3校验
			compStart := off + ChunkHeaderSize
			compEnd := compStart + ch.CompressedSize
			if compEnd > uint64(len(r.data)) { return false }
			compData := r.data[compStart:compEnd]
			h := Blake3Sum256(compData)
			actual := binary.LittleEndian.Uint64(h[:8])
			if actual != ch.Blake3Short {
				return false
			}
			
			_ = uint64(ch.RepairCount) * uint64(ch.SymbolSize)
		}
	}
	return true
}

func (r *Reader) extractSolid(dir string) error {
	// 读整个solid chunk
	if len(r.data) < ChunkHeaderSize { return ErrCorrupted }

	chBuf := bytes.NewReader(r.data)
	ch, err := ReadChunkHeader(chBuf)
	if err != nil { return err }

	compData := make([]byte, ch.CompressedSize)
	chBuf.Read(compData)

	// 解密(可选)
	if len(r.Password) > 0 {
		dec2, err := Decrypt(compData, r.Password)
		if err == nil { compData = dec2 }
	}
	// 解压整个solid流
	isLzma2 := false
	for _, e := range r.Entries {
		if e.CompressionID == CompressLzma2 { isLzma2 = true; break }
	}
	var solidData []byte
	if isLzma2 {
		var derr error
		solidData, derr = decompressLzma2Block(compData)
		if derr != nil { return derr }
	} else {
		dec, derr := ZstdNewReader(bytes.NewReader(compData))
		if derr != nil { return derr }
		solidData, err = io.ReadAll(dec)
		dec.Close()
		if err != nil { return err }
	}
	// Bomb detection for solid: check ratio
	if len(compData) > 0 {
		ratio := uint64(len(solidData)) / uint64(len(compData))
		if ratio > BombRatioThreshold {
			return fmt.Errorf("bomb detected: solid ratio %d:1 exceeds threshold %d:1", ratio, BombRatioThreshold)
		}
	}

	// 按entry切文件
	for _, e := range r.Entries {
		outPath, sErr := sanitizePath(dir, e.Path)
		if sErr != nil { return sErr }
		os.MkdirAll(filepath.Dir(outPath), 0755)

		switch e.EntryType {
		case EntryDir:
			os.MkdirAll(outPath, os.FileMode(e.Mode))
			restoreMeta(outPath, &e)
			continue
		case EntrySymlink:
			os.Remove(outPath)
			os.Symlink(e.LinkTarget, outPath)
			os.Lchown(outPath, int(e.Uid), int(e.Gid))
			fmt.Printf("  ✅ %s -> %s (symlink)\n", e.Path, e.LinkTarget)
			continue
		case EntryHardlink:
			target, _ := sanitizePath(dir, e.LinkTarget)
			os.Remove(outPath)
			os.Link(target, outPath)
			continue
		case EntryCharDev, EntryBlockDev, EntryFifo:
			if e.EntryType == EntryFifo {
				mkfifo(outPath, e.Mode)
			} else {
				var mode uint32 = 0666
				if e.EntryType == EntryCharDev { mode |= 0020000 } else { mode |= 0060000 }
				mknod(outPath, mode, e.DevMajor, e.DevMinor)
			}
			restoreMeta(outPath, &e)
			continue
		}

		if e.EntryType != EntryFile { continue }

		start := e.FirstDataOff // solid内偏移
		end := start + e.OriginalSize
		if end > uint64(len(solidData)) {
			end = uint64(len(solidData))
		}

		fileData := solidData[start:end]

		// Reverse BCJ filter if applied
		if e.BCJFilter != BCJNone {
			arch := BCJIDToArch(e.BCJFilter)
			if arch != "" {
				ApplyBCJFilterArch(fileData, arch, false)
			}
		}

		if err := checkSymlink(outPath); err != nil { return err }
		os.WriteFile(outPath, fileData, os.FileMode(e.Mode))
		restoreMeta(outPath, &e)
		fmt.Printf("  ✅ %s (%s)\n",e.Path, HumanSize(len(fileData)))
	}
	return nil
}

func (r *Reader) GetData() []byte { return r.data }

// decompressLzma2Block decompresses a raw LZMA2 block.
func decompressLzma2Block(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	// Use the LZMA2 reader from xz_decompress.go
	lr := newLzma2Reader(bytes.NewReader(data), 1<<22) // 4MB dict
	out, err := io.ReadAll(lr)
	lr.Close()
	return out, err
}

// restoreMeta restores ownership, timestamps, and xattrs on an extracted path.
func restoreMeta(path string, e *DirEntry) {
	if e.MTimeNano > 0 {
		t := time.Unix(0, e.MTimeNano)
		os.Chtimes(path, t, t)
	}
	// Restore ownership (best-effort, requires root)
	if e.Uid != 0 || e.Gid != 0 {
		os.Lchown(path, int(e.Uid), int(e.Gid))
	}
	// Restore xattrs
	if len(e.Xattrs) > 0 {
		setXattrs(path, e.Xattrs)
	}
}

