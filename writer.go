package nya

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"
)

const FECChunkRaw = 512 * 1024 // 512KB raw per FEC chunk (larger blocks = better compression)

type solidFileEntry struct {
	path string
	info os.FileInfo
	size int64
}

type Writer struct {
	w             io.WriteSeeker
	entries       []DirEntry
	dataBuf       bytes.Buffer
	fecPercent    int
	compressLevel int
	solid         bool
	password      []byte
	dict          []byte
	dataOff       uint64
	hashTables    [][]uint32
	fecBuf        bytes.Buffer
	basePath      string
	workers       int
	zstdEnc           interface{}
	compressionMethod string // "zstd" (default) or "lzma2"

	// Solid mode accumulation
	solidBuf      bytes.Buffer
	solidEntries  []solidFileEntry
	solidBCJArch  string // detected BCJ arch for solid stream
}

func NewWriter(w io.WriteSeeker, fecPercent int, compressLevel int) *Writer {
	return NewWriterOpts(w, fecPercent, compressLevel, false)
}

func NewWriterOpts(w io.WriteSeeker, fecPercent int, compressLevel int, solid bool, password ...[]byte) *Writer {
	if fecPercent < 0 {
		fecPercent = 0
	}
	if compressLevel <= 0 {
		compressLevel = 9
	}
	w2 := &Writer{w: w, fecPercent: fecPercent, compressLevel: compressLevel, solid: solid}
	if len(password) > 0 {
		w2.password = password[0]
	}
	return w2
}

func (nw *Writer) SetDict(dict []byte)      { nw.dict = dict }
func (nw *Writer) SetWorkers(n int)          { nw.workers = n }
func (nw *Writer) SetCompression(method string) { nw.compressionMethod = method }

// compressionID returns the format compression ID for the current method.
func (nw *Writer) compressionID() uint16 {
	if nw.compressionMethod == "lzma2" {
		return CompressLzma2
	}
	return CompressZstd
}

// compressRaw compresses data using the configured method (no BCJ).
func (nw *Writer) compressRaw(data []byte) []byte {
	if nw.compressionMethod == "lzma2" {
		compressed, err := Lzma2Compress(data, 4*1024*1024)
		if err == nil {
			return compressed
		}
	}
	return ZstdCompressWithWindow(data, nw.compressLevel)
}

func (nw *Writer) AddFile(path string) error {
	absPath, _ := filepath.Abs(path)
	nw.basePath = filepath.Dir(absPath)

	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		var allPaths []string
		filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// Collect dirs (except root) and files/symlinks
			if p != path {
				allPaths = append(allPaths, p)
			}
			return nil
		})
		// Also collect symlinks that Walk doesn't follow — re-walk with Lstat
		var files []string
		for _, p := range allPaths {
			fi, err := os.Lstat(p)
			if err != nil {
				continue
			}
			if fi.IsDir() {
				// Add directory entry
				relPath, _ := filepath.Rel(nw.basePath, p)
				nw.addDirEntry(relPath, fi)
				continue
			}
			files = append(files, p)
		}
		if nw.solid {
			// Solid mode: sequential add (order matters)
			for _, f := range files {
				fi, _ := os.Stat(f)
				if err := nw.addFile(f, fi); err != nil {
					return err
				}
			}
			return nil
		}
		workers := runtime.NumCPU()
		if nw.workers > 0 {
			workers = nw.workers
		}
		if workers > 4 {
			workers = 4
		}
		sem := make(chan struct{}, workers)
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, f := range files {
			wg.Add(1)
			sem <- struct{}{}
			go func(fp string) {
				defer wg.Done()
				defer func() { <-sem }()
				fi, _ := os.Stat(fp)
				mu.Lock()
				nw.addFile(fp, fi)
				mu.Unlock()
			}(f)
		}
		wg.Wait()
		return nil
	}
	return nw.addFile(path, info)
}

// addDirEntry adds a directory entry to the archive.
func (nw *Writer) addDirEntry(relPath string, info os.FileInfo) {
	e := DirEntry{
		Path:      relPath,
		EntryType: EntryDir,
		Mode:      uint32(info.Mode()),
		MTimeNano: info.ModTime().UnixNano(),
	}
	fillUnixMeta(&e, info)
	nw.entries = append(nw.entries, e)
}

func (nw *Writer) addFile(path string, info os.FileInfo) error {
	// Use Lstat to detect symlinks
	linfo, err := os.Lstat(path)
	if err != nil {
		linfo = info
	}

	relPath, _ := filepath.Rel(nw.basePath, path)
	if relPath == "" {
		relPath = filepath.Base(path)
	}

	// Handle symlinks
	if linfo.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		e := DirEntry{
			Path:       relPath,
			EntryType:  EntrySymlink,
			Mode:       uint32(linfo.Mode()),
			MTimeNano:  linfo.ModTime().UnixNano(),
			LinkTarget: target,
		}
		fillUnixMeta(&e, linfo)
		nw.entries = append(nw.entries, e)
		return nil
	}

	// Handle special files (fifo, device nodes)
	if linfo.Mode()&(os.ModeNamedPipe|os.ModeDevice|os.ModeCharDevice) != 0 {
		e := DirEntry{
			Path:      relPath,
			Mode:      uint32(linfo.Mode()),
			MTimeNano: linfo.ModTime().UnixNano(),
		}
		switch {
		case linfo.Mode()&os.ModeNamedPipe != 0:
			e.EntryType = EntryFifo
		case linfo.Mode()&os.ModeCharDevice != 0:
			e.EntryType = EntryCharDev
		default:
			e.EntryType = EntryBlockDev
		}
		fillUnixMeta(&e, linfo)
		e.DevMajor, e.DevMinor = getDevNumbers(linfo)
		nw.entries = append(nw.entries, e)
		return nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if nw.solid {
		return nw.addFileSolid(relPath, raw, linfo)
	}
	return nw.addFileNormal(relPath, raw, linfo)
}

// fillUnixMeta populates uid/gid/username/groupname and xattrs.
func fillUnixMeta(e *DirEntry, info os.FileInfo) {
	if uid, gid, ok := getUnixStat(info); ok {
		e.Uid = uid
		e.Gid = gid
		if u, err := user.LookupId(strconv.Itoa(int(uid))); err == nil {
			e.UserName = u.Username
		}
		if g, err := user.LookupGroupId(strconv.Itoa(int(gid))); err == nil {
			e.GroupName = g.Name
		}
	}
}

func unix_major(rdev uint64) uint32 { return uint32((rdev >> 8) & 0xfff) }
func unix_minor(rdev uint64) uint32 { return uint32((rdev & 0xff) | ((rdev >> 12) & 0xfff00)) }

// resolveAbsPath converts a relative archive path back to filesystem path for xattr reading.
func (nw *Writer) resolveAbsPath(relPath string) string {
	if nw.basePath != "" {
		return filepath.Join(nw.basePath, relPath)
	}
	return relPath
}

// addFileNormal compresses each file independently with BCJ pre-filtering.
func (nw *Writer) addFileNormal(relPath string, raw []byte, info os.FileInfo) error {
	// Detect BCJ architecture for the entire file
	bcjArch := DetectBCJArch(raw)
	bcjID := BCJArchToID(bcjArch)

	var allCompressed bytes.Buffer

	for off := 0; off < len(raw); off += FECChunkRaw {
		end := off + FECChunkRaw
		if end > len(raw) {
			end = len(raw)
		}
		block := raw[off:end]

		compressed := nw.compressBlockWithBCJ(block, bcjArch)

		var lenBuf [4]byte
		binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(compressed)))
		allCompressed.Write(lenBuf[:])
		allCompressed.Write(compressed)
	}

	compData := allCompressed.Bytes()

	// Encrypt
	if len(nw.password) > 0 {
		encrypted, err := Encrypt(compData, nw.password)
		if err == nil {
			compData = encrypted
		}
	}

	// FEC
	K := 32
	var fec []byte
	if len(compData) >= 4096 && nw.fecPercent > 0 {
		fec = raptorqFEC(compData, nw.fecPercent)
	}
	repairCount := len(fec) / fecSymbolSize
	if repairCount < 1 {
		repairCount = K
	}

	// BLAKE3
	bh := Blake3Sum256(compData)
	hash := binary.LittleEndian.Uint64(bh[:8])

	ch := ChunkHeader{
		OriginalSize:   uint64(len(raw)),
		CompressedSize: uint64(len(compData)),
		RepairCount:    uint32(repairCount),
		SymbolSize:     uint32(fecSymbolSize),
		Blake3Short:    hash,
	}
	firstOff := nw.dataOff

	var chBuf bytes.Buffer
	ch.Write(&chBuf)
	chBuf.Write(compData)
	nw.fecBuf.Write(fec)
	nw.dataBuf.Write(chBuf.Bytes())
	nw.dataOff += uint64(chBuf.Len())

	nw.hashTables = append(nw.hashTables, GetHashTable())

	nw.entries = append(nw.entries, DirEntry{
		Path:          relPath,
		EntryType:     EntryFile,
		Mode:          uint32(info.Mode()),
		MTimeNano:     info.ModTime().UnixNano(),
		OriginalSize:  uint64(len(raw)),
		ChunkCount:    1,
		CompressionID: nw.compressionID(),
		FECType:       FECRaptorQ,
		BCJFilter:     bcjID,
		FECParams: FECParams{
			Param1: uint32(K),
			Param2: uint32(fecSymbolSize),
			Param3: uint32(nw.fecPercent),
		},
		FirstDataOff: firstOff,
	})
	// Fill Unix metadata on last entry
	fillUnixMeta(&nw.entries[len(nw.entries)-1], info)
	nw.entries[len(nw.entries)-1].Xattrs = listXattr(nw.resolveAbsPath(relPath))
	return nil
}

// compressBlockWithBCJ applies BCJ filter (if arch set) then compresses.
// BCJ is always applied when arch != "" for consistency with reader.
func (nw *Writer) compressBlockWithBCJ(block []byte, bcjArch string) []byte {
	data := block
	if bcjArch != "" {
		filtered := make([]byte, len(block))
		copy(filtered, block)
		ApplyBCJFilterArch(filtered, bcjArch, true)
		data = filtered
	}

	if nw.compressionMethod == "lzma2" {
		compressed, err := Lzma2Compress(data, 4*1024*1024)
		if err == nil {
			return compressed
		}
		// fallback to zstd on error
	}
	if len(nw.dict) > 0 {
		return ZstdCompressWithDict(data, nw.compressLevel, nw.dict)
	}
	return ZstdCompressWithWindow(data, nw.compressLevel)
}

// addFileSolid accumulates file data for solid compression.
func (nw *Writer) addFileSolid(relPath string, raw []byte, info os.FileInfo) error {
	// Record offset within solid stream
	solidOff := uint64(nw.solidBuf.Len())

	// Detect BCJ arch — use majority vote across files
	arch := DetectBCJArch(raw)
	if arch != "" && nw.solidBCJArch == "" {
		nw.solidBCJArch = arch
	}

	nw.solidBuf.Write(raw)
	nw.solidEntries = append(nw.solidEntries, solidFileEntry{
		path: relPath,
		info: info,
		size: int64(len(raw)),
	})

	// Store entry with offset within solid stream
	nw.entries = append(nw.entries, DirEntry{
		Path:          relPath,
		EntryType:     EntryFile,
		Mode:          uint32(info.Mode()),
		MTimeNano:     info.ModTime().UnixNano(),
		OriginalSize:  uint64(len(raw)),
		ChunkCount:    1,
		CompressionID: nw.compressionID(),
		FECType:       FECRaptorQ,
		BCJFilter:     BCJNone, // set later in Close
		FirstDataOff:  solidOff, // offset within decompressed solid stream
	})
	fillUnixMeta(&nw.entries[len(nw.entries)-1], info)
	nw.entries[len(nw.entries)-1].Xattrs = listXattr(nw.resolveAbsPath(relPath))
	return nil
}

func (nw *Writer) Close() error {
	if nw.solid {
		return nw.closeSolid()
	}
	return nw.closeNormal()
}

func (nw *Writer) closeSolid() error {
	solidData := nw.solidBuf.Bytes()

	// Apply BCJ filter to entire solid stream if arch detected
	bcjArch := nw.solidBCJArch
	bcjID := BCJArchToID(bcjArch)
	useBCJ := false

	if bcjArch != "" {
		// Try with and without BCJ, pick smaller
		filtered := make([]byte, len(solidData))
		copy(filtered, solidData)
		ApplyBCJFilterArch(filtered, bcjArch, true)

		compOrig := nw.compressRaw(solidData)
		compBCJ := nw.compressRaw(filtered)

		if len(compBCJ) < len(compOrig) {
			solidData = filtered
			useBCJ = true
		}
	}

	// Compress entire solid stream as one zstd frame
	compData := nw.compressRaw(solidData)

	// Encrypt
	if len(nw.password) > 0 {
		encrypted, err := Encrypt(compData, nw.password)
		if err == nil {
			compData = encrypted
		}
	}

	// FEC
	K := 32
	var fec []byte
	if len(compData) >= 4096 && nw.fecPercent > 0 {
		fec = raptorqFEC(compData, nw.fecPercent)
	}
	repairCount := len(fec) / fecSymbolSize
	if repairCount < 1 {
		repairCount = K
	}

	// BLAKE3
	bh := Blake3Sum256(compData)
	hash := binary.LittleEndian.Uint64(bh[:8])

	ch := ChunkHeader{
		OriginalSize:   uint64(nw.solidBuf.Len()),
		CompressedSize: uint64(len(compData)),
		RepairCount:    uint32(repairCount),
		SymbolSize:     uint32(fecSymbolSize),
		Blake3Short:    hash,
	}

	var chBuf bytes.Buffer
	ch.Write(&chBuf)
	chBuf.Write(compData)
	nw.fecBuf.Write(fec)
	nw.dataBuf.Write(chBuf.Bytes())

	// Update BCJ filter on all entries
	if useBCJ {
		for i := range nw.entries {
			nw.entries[i].BCJFilter = bcjID
		}
	}

	// Write archive
	data := nw.dataBuf.Bytes()

	gh := GlobalHeader{
		Magic:        MagicHeader,
		VersionMajor: 1,
		VersionMinor: 0,
		Flags:        FlagSolidCompress,
		DataAreaSize: uint64(len(data)),
		CreationTime: time.Now().UnixNano(),
	}
	for _, e := range nw.entries {
		gh.TotalOrigSize += e.OriginalSize
	}
	gh.CentralDirOffset = GlobalHeaderSize + uint64(len(data))

	var dirBuf bytes.Buffer
	binary.Write(&dirBuf, binary.LittleEndian, uint64(len(nw.entries)))
	for i := range nw.entries {
		WriteDirEntry(&dirBuf, &nw.entries[i])
	}
	gh.CentralDirSize = uint64(dirBuf.Len())

	gh.Write(nw.w)
	nw.w.Write(data)
	nw.w.Write(dirBuf.Bytes())

	binary.Write(nw.w, binary.LittleEndian, uint32(nw.fecBuf.Len()))
	nw.w.Write(nw.fecBuf.Bytes())

	totalHashes := uint32(0)
	for _, ht := range nw.hashTables {
		totalHashes += uint32(len(ht))
	}
	binary.Write(nw.w, binary.LittleEndian, totalHashes)
	for _, ht := range nw.hashTables {
		for _, h := range ht {
			binary.Write(nw.w, binary.LittleEndian, h)
		}
	}

	return nil
}

func (nw *Writer) closeNormal() error {
	data := nw.dataBuf.Bytes()

	gh := GlobalHeader{
		Magic:        MagicHeader,
		VersionMajor: 1,
		VersionMinor: 0,
		DataAreaSize: uint64(len(data)),
		CreationTime: time.Now().UnixNano(),
	}
	for _, e := range nw.entries {
		gh.TotalOrigSize += e.OriginalSize
	}
	gh.CentralDirOffset = GlobalHeaderSize + uint64(len(data))

	var dirBuf bytes.Buffer
	binary.Write(&dirBuf, binary.LittleEndian, uint64(len(nw.entries)))
	for i := range nw.entries {
		WriteDirEntry(&dirBuf, &nw.entries[i])
	}
	gh.CentralDirSize = uint64(dirBuf.Len())

	gh.Write(nw.w)
	nw.w.Write(data)
	nw.w.Write(dirBuf.Bytes())

	binary.Write(nw.w, binary.LittleEndian, uint32(nw.fecBuf.Len()))
	nw.w.Write(nw.fecBuf.Bytes())

	totalHashes := uint32(0)
	for _, ht := range nw.hashTables {
		totalHashes += uint32(len(ht))
	}
	binary.Write(nw.w, binary.LittleEndian, totalHashes)
	for _, ht := range nw.hashTables {
		for _, h := range ht {
			binary.Write(nw.w, binary.LittleEndian, h)
		}
	}

	return nil
}
