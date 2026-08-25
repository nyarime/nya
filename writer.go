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
	multiChunk        bool // split large non-solid files (default true)
	chunkSize         int  // 0 = auto per SPEC-MULTICHUNK
	hasMultiChunk     bool // any entry emitted ChunkCount > 1
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
	w2 := &Writer{w: w, fecPercent: fecPercent, solid: solid, fecType: DefaultFECType, multiChunk: true}
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

func (nw *Writer) SetDict(dict []byte) { nw.dict = dict }
func (nw *Writer) SetWorkers(n int)    { nw.workers = n }

// SetMultiChunk enables splitting non-solid files larger than 4 MiB into
// multiple on-disk chunks (VersionMinor 3). Solid archives are unaffected.
func (nw *Writer) SetMultiChunk(on bool) { nw.multiChunk = on }

// SetChunkSize overrides the raw chunk size for multi-chunk entries (0 = auto).
func (nw *Writer) SetChunkSize(n int) { nw.chunkSize = n }

func (nw *Writer) archiveVersionMinor() uint16 {
	if nw.hasMultiChunk {
		return VersionMinorMultiChunk
	}
	if len(nw.password) > 0 {
		return 2
	}
	return VersionMinor
}

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
	case len(nw.dict) > 0:
		return CompressZstdDict
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
		if len(nw.dict) > 0 {
			return ZstdCompressWithDict(data, nw.compressLevel, nw.dict), nil
		}
		return ZstdCompressWithWindow(data, nw.compressLevel), nil
	default:
		opts := nw.lzmaOpts
		// Optimal parse is opt-in (Lzma2Options.OptimalParse); benchmarks show
		// extension sorting helps multi-file solid streams, while optimal parse
		// helps highly repetitive single streams only — see docs/BENCHMARK-COMPRESS.md.
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

// addFileNormal compresses each file independently. BCJ is decided on the
// whole file (like solid mode) and applied before chunk split; 512 KiB blocks
// inside each chunk are compressed without re-applying BCJ.
func (nw *Writer) addFileNormal(relPath string, raw []byte, info os.FileInfo) error {
	raw, bcjID := nw.chooseBCJForFile(raw)
	chunkSizes := splitRawChunkSizes(len(raw), nw.chunkSize, nw.multiChunk)
	if len(chunkSizes) == 0 {
		return fmt.Errorf("nya: empty file %s", relPath)
	}
	if len(chunkSizes) > 1 {
		nw.hasMultiChunk = true
	}

	firstOff := nw.dataOff
	var entryFEC FECParams
	var entryFECType uint8
	payloads := make([]chunkPayload, len(chunkSizes))

	workers := nw.workersForChunks()
	if workers <= 1 || len(chunkSizes) == 1 {
		rawOff := 0
		for i, sz := range chunkSizes {
			p, err := nw.buildChunkPayload(raw[rawOff:rawOff+sz])
			if err != nil {
				return fmt.Errorf("nya: compress %s chunk %d: %w", relPath, i, err)
			}
			payloads[i] = p
			rawOff += sz
		}
	} else {
		if err := nw.buildChunkPayloadsParallel(raw, chunkSizes, payloads); err != nil {
			return fmt.Errorf("nya: compress %s: %w", relPath, err)
		}
	}

	for i, p := range payloads {
		repairCount := p.plan.repairPerBlock()
		if repairCount < 1 {
			repairCount = 32
		}
		bh := Blake3Sum256(p.comp)
		hash := binary.LittleEndian.Uint64(bh[:8])
		ch := ChunkHeader{
			OriginalSize:   uint64(p.rawLen),
			CompressedSize: uint64(len(p.comp)),
			RepairCount:    uint32(repairCount),
			SymbolSize:     uint32(p.plan.SymbolSize),
			Blake3Short:    hash,
		}
		var chBuf bytes.Buffer
		ch.Write(&chBuf)
		chBuf.Write(p.comp)
		nw.fecBuf.Write(p.fec)
		nw.dataBuf.Write(chBuf.Bytes())
		nw.dataOff += uint64(chBuf.Len())
		if len(p.hashes) > 0 {
			nw.hashTables = append(nw.hashTables, p.hashes)
		} else {
			nw.hashTables = append(nw.hashTables, GetHashTable())
		}
		if i == 0 {
			entryFEC = p.plan.toParams()
			entryFECType = p.plan.Type
		}
	}

	nw.entries = append(nw.entries, DirEntry{
		Path:          relPath,
		EntryType:     EntryFile,
		Mode:          uint32(info.Mode()),
		MTimeNano:     info.ModTime().UnixNano(),
		OriginalSize:  uint64(len(raw)),
		ChunkCount:    uint32(len(chunkSizes)),
		CompressionID: nw.compressionID(),
		FECType:       entryFECType,
		BCJFilter:     bcjID,
		FECParams:     entryFEC,
		FirstDataOff:  firstOff,
	})
	fillUnixMeta(&nw.entries[len(nw.entries)-1], info)
	nw.entries[len(nw.entries)-1].Xattrs = listXattr(nw.resolveAbsPath(relPath))
	return nil
}

func (nw *Writer) workersForChunks() int {
	if nw.workers > 0 {
		return nw.workers
	}
	return 4
}

func (nw *Writer) buildChunkPayload(raw []byte) (chunkPayload, error) {
	var allCompressed bytes.Buffer
	for off := 0; off < len(raw); off += FECChunkRaw {
		end := off + FECChunkRaw
		if end > len(raw) {
			end = len(raw)
		}
		block := raw[off:end]
		compressed, err := nw.compressBlock(block)
		if err != nil {
			return chunkPayload{}, err
		}
		var lenBuf [4]byte
		binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(compressed)))
		allCompressed.Write(lenBuf[:])
		allCompressed.Write(compressed)
	}
	compData := allCompressed.Bytes()
	if len(nw.password) > 0 {
		encrypted, err := nw.sealCompressed(compData)
		if err != nil {
			return chunkPayload{}, err
		}
		compData = encrypted
	}
	var fec []byte
	var hashes []uint32
	var plan fecPlan
	if len(compData) >= fecMinPayload && nw.fecPercent > 0 {
		fec, hashes, plan = encodeFEC(compData, nw.fecPercent, nw.fecType, false)
	}
	return chunkPayload{comp: compData, fec: fec, hashes: hashes, plan: plan, rawLen: len(raw)}, nil
}

type chunkPayload struct {
	comp   []byte
	fec    []byte
	hashes []uint32
	plan   fecPlan
	rawLen int
}

func (nw *Writer) buildChunkPayloadsParallel(raw []byte, sizes []int, out []chunkPayload) error {
	type job struct {
		i   int
		off int
		sz  int
	}
	jobs := make(chan job)
	errs := make(chan error, len(sizes))
	var wg sync.WaitGroup
	workers := nw.workersForChunks()
	if workers > len(sizes) {
		workers = len(sizes)
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				p, err := nw.buildChunkPayload(raw[j.off:j.off+j.sz])
				if err != nil {
					errs <- err
					return
				}
				out[j.i] = p
			}
		}()
	}
	off := 0
	for i, sz := range sizes {
		jobs <- job{i: i, off: off, sz: sz}
		off += sz
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errs:
		return err
	default:
	}
	return nil
}

// chooseBCJForFile decides whether to apply BCJ to the whole file before
// chunking. Like solid mode, BCJ is only kept when it shrinks the blocked
// compressed payload; this avoids false-positive pattern detection corrupting
// non-code data.
func (nw *Writer) chooseBCJForFile(raw []byte) ([]byte, uint8) {
	bcjArch := DetectBCJArch(raw)
	filtered, id, _ := nw.tryBCJForArchive(raw, bcjArch, false)
	return filtered, id
}

func (nw *Writer) blockedCompressedLen(raw []byte) (int, error) {
	total := 0
	for off := 0; off < len(raw); off += FECChunkRaw {
		end := off + FECChunkRaw
		if end > len(raw) {
			end = len(raw)
		}
		comp, err := nw.compressBlock(raw[off:end])
		if err != nil {
			return 0, err
		}
		total += 4 + len(comp)
	}
	return total, nil
}

// compressBlock compresses one raw block (BCJ already applied at file level).
func (nw *Writer) compressBlock(block []byte) ([]byte, error) {
	if nw.usesStore() {
		return append([]byte(nil), block...), nil
	}
	if !nw.usesZstd() {
		return Lzma2CompressOpts(block, nw.lzmaOpts)
	}
	if len(nw.dict) > 0 {
		return ZstdCompressWithDict(block, nw.compressLevel, nw.dict), nil
	}
	return ZstdCompressWithWindow(block, nw.compressLevel), nil
}

// addFileSolid accumulates file data for solid compression.
func (nw *Writer) addFileSolid(relPath string, raw []byte, info os.FileInfo) error {
	// Record offset within solid stream
	solidOff := uint64(nw.solidBuf.Len())

	// Detect BCJ arch for solid stream (signed members may still use BCJ after roundtrip verify).
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
		filtered, _, ok := nw.tryBCJForArchive(solidData, bcjArch, true)
		if ok {
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
		VersionMinor: nw.archiveVersionMinor(),
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
		VersionMinor: nw.archiveVersionMinor(),
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

	if len(nw.dict) > 0 {
		if err := nw.appendEmbeddedDict(&gh); err != nil {
			return err
		}
	}

	return nil
}

// appendEmbeddedDict writes tail type 0x0006 and patches the global header.
func (nw *Writer) appendEmbeddedDict(gh *GlobalHeader) error {
	tailOff, err := nw.w.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	rec := WrapTailRecord(TailTypeZstdDictionary, EncodeZstdDictPayload(nw.dict))
	if _, err := nw.w.Write(rec); err != nil {
		return err
	}
	gh.Flags |= FlagHasZstdDict
	SetTailChainReserved(gh, uint64(tailOff), uint64(len(rec)))
	if _, err := nw.w.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := gh.Write(nw.w); err != nil {
		return err
	}
	_, err = nw.w.Seek(0, io.SeekEnd)
	return err
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
