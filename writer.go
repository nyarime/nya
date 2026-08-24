package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"
)

const FECChunkRaw = 512 * 1024 // 512KB raw per FEC chunk (larger blocks = better compression)

type solidFileEntry struct {
	path string
	info os.FileInfo
	size int64
}

type Writer struct {
	w                 io.WriteSeeker
	entries           []DirEntry
	dataBuf           bytes.Buffer
	fecPercent        int
	fecType           uint8
	compressLevel     int
	solid             bool
	password          []byte
	kdfSalt           [argon2SaltLen]byte
	kdfSaltSet        bool
	dict              []byte
	dataOff           uint64
	hashTables        [][]uint32
	fecBuf            bytes.Buffer
	globalMetaFec     []byte
	basePath          string
	workers           int
	zstdEnc           interface{}
	compressionMethod string // see CompressionLZMA2, CompressionZstd, CompressionStore
	codecPinned       bool   // SetCompression was called, so a level must not override it
	level             int
	lzmaOpts          Lzma2Options

	// Solid mode accumulation
	solidBuf     bytes.Buffer
	solidEntries []solidFileEntry
	solidBCJArch string // detected BCJ arch for solid stream
}

// NewWriter creates a writer at the given compression level. See the Level
// constants; LevelBest is 9.
func NewWriter(w io.WriteSeeker, fecPercent int, level int) *Writer {
	return NewWriterOpts(w, fecPercent, level, false)
}

// NewWriterOpts creates a writer with the full set of options. level runs
// from LevelStore to LevelBest and selects both the codec and how hard it
// searches; a negative level picks LevelDefault. fecPercent is how much
// recovery data to add, as a percentage of the payload.
func NewWriterOpts(w io.WriteSeeker, fecPercent int, level int, solid bool, password ...[]byte) *Writer {
	if fecPercent < 0 {
		fecPercent = 0
	}
	if level < 0 {
		level = LevelDefault
	}
	w2 := &Writer{w: w, fecPercent: fecPercent, solid: solid, fecType: DefaultFECType}
	w2.SetLevel(level)
	if len(password) > 0 {
		w2.password = password[0]
	}
	return w2
}

// SetFECType selects the forward-error-correction codec for new archives.
// Supported values: FECRaptorQ, FECLDPC, FECHybrid (default).
func (nw *Writer) SetFECType(t uint8) {
	nw.fecType = t
}

const (
	// CompressionLZMA2 is the default. It compresses smaller than the zstd
	// encoder on every corpus we measure, and on text it is faster to
	// compress as well; the trade is a slower decompressor.
	CompressionLZMA2 = "lzma2"

	// CompressionZstd trades ratio for a much faster decompressor.
	CompressionZstd = "zstd"

	// CompressionStore writes the payload uncompressed.
	CompressionStore = "store"
)

// lzma2DictSize is the dictionary the writer asks LZMA2 for. It matches the
// size the reader uses, so the two must be changed together.
const lzma2DictSize = 4 * 1024 * 1024

func (nw *Writer) SetDict(dict []byte) { nw.dict = dict }
func (nw *Writer) SetWorkers(n int)    { nw.workers = n }

// SetCompression selects the codec: CompressionLZMA2 (the default),
// CompressionZstd or CompressionStore. Setting it explicitly overrides the
// codec a level would have chosen.
func (nw *Writer) SetCompression(method string) {
	nw.compressionMethod = method
	nw.codecPinned = true
}

// SetLevel applies a compression level between LevelStore and LevelBest,
// choosing the codec and how hard it searches. A codec set explicitly with
// SetCompression is left alone.
func (nw *Writer) SetLevel(level int) {
	spec := specForLevel(level)
	nw.level = level
	nw.lzmaOpts = spec.lzma
	nw.compressLevel = spec.zstdLevel
	if !nw.codecPinned {
		nw.compressionMethod = spec.codec
	}
}

// usesZstd reports whether this writer emits zstd rather than LZMA2. A
// dictionary implies zstd, since only that encoder can use one.
func (nw *Writer) usesZstd() bool {
	return nw.compressionMethod == CompressionZstd || len(nw.dict) > 0
}

func (nw *Writer) usesStore() bool {
	return nw.compressionMethod == CompressionStore && len(nw.dict) == 0
}

// compressionID returns the format compression ID for the current codec.
func (nw *Writer) compressionID() uint16 {
	switch {
	case nw.usesStore():
		return CompressNone
	case nw.usesZstd():
		return CompressZstd
	default:
		return CompressLzma2
	}
}

// compressRaw compresses data using the configured codec (no BCJ).
//
// A failure is reported rather than quietly retried with the other codec:
// the directory entry has already committed to one CompressionID for the
// whole entry, so silently switching would produce an archive that cannot be
// read back.
func (nw *Writer) compressRaw(data []byte) ([]byte, error) {
	switch {
	case nw.usesStore():
		return data, nil
	case nw.usesZstd():
		return ZstdCompressWithWindow(data, nw.compressLevel), nil
	default:
		opts := nw.lzmaOpts
		if nw.solid {
			opts.OptimalParse = true
		}
		return Lzma2CompressOpts(data, opts)
	}
}

func (nw *Writer) AddFile(path string) error {
	absPath, _ := filepath.Abs(path)
	nw.basePath = filepath.Dir(absPath)

	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		allPaths, err := collectDirectoryPaths(path)
		if err != nil {
			return err
		}
		files := splitDirectoryPaths(nw.basePath, allPaths, nw)
		return nw.addCollectedFiles(files)
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

		compressed, err := nw.compressBlockWithBCJ(block, bcjArch)
		if err != nil {
			return fmt.Errorf("nya: compress %s: %w", relPath, err)
		}

		var lenBuf [4]byte
		binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(compressed)))
		allCompressed.Write(lenBuf[:])
		allCompressed.Write(compressed)
	}

	compData := allCompressed.Bytes()

	// Encrypt
	if len(nw.password) > 0 {
		encrypted, err := nw.sealCompressed(compData)
		if err != nil {
			return err
		}
		compData = encrypted
	}

	// FEC
	var fec []byte
	var fecPlan fecPlan
	if len(compData) >= fecMinPayload && nw.fecPercent > 0 {
		fec, _, fecPlan = encodeFEC(compData, nw.fecPercent, nw.fecType, nw.solid)
	}
	repairCount := fecPlan.repairPerBlock()
	if repairCount < 1 {
		repairCount = 32
	}

	// BLAKE3
	bh := Blake3Sum256(compData)
	hash := binary.LittleEndian.Uint64(bh[:8])

	ch := ChunkHeader{
		OriginalSize:   uint64(len(raw)),
		CompressedSize: uint64(len(compData)),
		RepairCount:    uint32(repairCount),
		SymbolSize:     uint32(fecPlan.SymbolSize),
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

	fecParams := fecPlan.toParams()
	nw.entries = append(nw.entries, DirEntry{
		Path:          relPath,
		EntryType:     EntryFile,
		Mode:          uint32(info.Mode()),
		MTimeNano:     info.ModTime().UnixNano(),
		OriginalSize:  uint64(len(raw)),
		ChunkCount:    1,
		CompressionID: nw.compressionID(),
		FECType:       fecPlan.Type,
		BCJFilter:     bcjID,
		FECParams:     fecParams,
		FirstDataOff:  firstOff,
	})
	// Fill Unix metadata on last entry
	fillUnixMeta(&nw.entries[len(nw.entries)-1], info)
	nw.entries[len(nw.entries)-1].Xattrs = listXattr(nw.resolveAbsPath(relPath))
	return nil
}

// compressBlockWithBCJ applies BCJ filter (if arch set) then compresses.
// BCJ is always applied when arch != "" for consistency with reader.
func (nw *Writer) compressBlockWithBCJ(block []byte, bcjArch string) ([]byte, error) {
	data := block
	if bcjArch != "" {
		filtered := make([]byte, len(block))
		copy(filtered, block)
		ApplyBCJFilterArch(filtered, bcjArch, true)
		data = filtered
	}

	if nw.usesStore() {
		return data, nil
	}
	if !nw.usesZstd() {
		return Lzma2CompressOpts(data, nw.lzmaOpts)
	}
	if len(nw.dict) > 0 {
		return ZstdCompressWithDict(data, nw.compressLevel, nw.dict), nil
	}
	return ZstdCompressWithWindow(data, nw.compressLevel), nil
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
		BCJFilter:     BCJNone,  // set later in Close
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

		compOrig, err := nw.compressRaw(solidData)
		if err != nil {
			return fmt.Errorf("nya: compress solid stream: %w", err)
		}
		compBCJ, err := nw.compressRaw(filtered)
		if err != nil {
			return fmt.Errorf("nya: compress solid stream: %w", err)
		}

		if len(compBCJ) < len(compOrig) {
			solidData = filtered
			useBCJ = true
		}
	}

	// Compress the whole solid stream as a single frame.
	compData, err := nw.compressRaw(solidData)
	if err != nil {
		return fmt.Errorf("nya: compress solid stream: %w", err)
	}

	// Encrypt
	if len(nw.password) > 0 {
		encrypted, err := nw.sealCompressed(compData)
		if err != nil {
			return err
		}
		compData = encrypted
	}

	// FEC
	var fec []byte
	var fecPlan fecPlan
	if len(compData) >= fecMinPayload && nw.fecPercent > 0 {
		fec, _, fecPlan = encodeFEC(compData, nw.fecPercent, nw.fecType, nw.solid)
	}
	repairCount := fecPlan.repairPerBlock()
	if repairCount < 1 {
		repairCount = 32
	}

	// BLAKE3
	bh := Blake3Sum256(compData)
	hash := binary.LittleEndian.Uint64(bh[:8])

	ch := ChunkHeader{
		OriginalSize:   uint64(nw.solidBuf.Len()),
		CompressedSize: uint64(len(compData)),
		RepairCount:    uint32(repairCount),
		SymbolSize:     uint32(fecPlan.SymbolSize),
		Blake3Short:    hash,
	}

	var chBuf bytes.Buffer
	ch.Write(&chBuf)
	chBuf.Write(compData)
	nw.fecBuf.Write(fec)
	nw.dataBuf.Write(chBuf.Bytes())
	nw.hashTables = append(nw.hashTables, GetHashTable())

	fecParams := fecPlan.toParams()
	for i := range nw.entries {
		nw.entries[i].FECType = fecPlan.Type
		nw.entries[i].FECParams = fecParams
	}

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
		VersionMajor: VersionMajor,
		VersionMinor: VersionMinor,
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

	return nw.finalizeArchive(gh, data, dirBuf.Bytes())
}

func (nw *Writer) sealCompressed(compData []byte) ([]byte, error) {
	if len(nw.password) == 0 {
		return compData, nil
	}
	if !nw.kdfSaltSet {
		salt, err := NewWriterKDFSalt()
		if err != nil {
			return nil, err
		}
		nw.kdfSalt = salt
		nw.kdfSaltSet = true
	}
	p := KDFParams{
		Argon2id:  true,
		Salt:      nw.kdfSalt,
		MemoryKiB: argon2MemoryKiB,
		Time:      argon2Time,
		Threads:   argon2Threads,
	}
	return EncryptPayload(compData, nw.password, p)
}

func (nw *Writer) closeNormal() error {
	data := nw.dataBuf.Bytes()

	gh := GlobalHeader{
		Magic:        MagicHeader,
		VersionMajor: VersionMajor,
		VersionMinor: VersionMinor,
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

	return nw.finalizeArchive(gh, data, dirBuf.Bytes())
}

// finalizeArchive writes the global header, payload, central directory, FEC
// parity, symbol hashes and optional global metadata FEC.
func (nw *Writer) finalizeArchive(gh GlobalHeader, data, dirBytes []byte) error {
	if nw.fecPercent > 0 {
		meta := nw.buildGlobalMetaPayload(dirBytes)
		nw.globalMetaFec = encodeGlobalMetaFEC(meta)
		if len(nw.globalMetaFec) > 0 {
			gh.Flags |= FlagHasGlobalFEC
		}
	}
	if nw.kdfSaltSet {
		WriteKDFParams(&gh, nw.kdfSalt)
	}

	gh.Write(nw.w)
	nw.w.Write(data)
	nw.w.Write(dirBytes)

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

	if len(nw.globalMetaFec) > 0 {
		binary.Write(nw.w, binary.LittleEndian, uint32(len(nw.globalMetaFec)))
		nw.w.Write(nw.globalMetaFec)
	}

	return nil
}

func (nw *Writer) buildGlobalMetaPayload(dirBytes []byte) []byte {
	var meta bytes.Buffer
	meta.Write(dirBytes)
	totalHashes := uint32(0)
	for _, ht := range nw.hashTables {
		totalHashes += uint32(len(ht))
	}
	binary.Write(&meta, binary.LittleEndian, totalHashes)
	for _, ht := range nw.hashTables {
		for _, h := range ht {
			binary.Write(&meta, binary.LittleEndian, h)
		}
	}
	return meta.Bytes()
}
