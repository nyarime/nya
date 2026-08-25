package nya

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// EmbedOptions controls embedding a download index into a .nya file.
type EmbedOptions struct {
	BlockSize  int64
	InPlace    bool
	OutputPath string
}

// EmbedResult summarizes an embed operation.
type EmbedResult struct {
	Path            string
	BodySize        int64 // bytes before tail+footer (covered by transport blocks)
	FinalSize       int64
	BlockCount      int
	TailChainOffset int64
	TailChainSize   int64
}

// EmbedDownloadIndex appends DownloadIndex tail type 0x0001 and an EOF footer.
//
// Transport blocks cover [0, BodySize) only. The tail+footer are fetched during
// bootstrap (footer Range → tail Range) and appended locally. archiveBlake3 is
// the digest of the entire final file (body+tail+footer).
func EmbedDownloadIndex(path string, opt EmbedOptions) (*EmbedResult, error) {
	if opt.BlockSize <= 0 {
		opt.BlockSize = DefaultDownloadBlock
	}
	work := path
	if !opt.InPlace {
		if opt.OutputPath == "" {
			return nil, fmt.Errorf("embed: OutputPath required when not in-place")
		}
		if err := copyFile(path, opt.OutputPath); err != nil {
			return nil, err
		}
		work = opt.OutputPath
	}

	fi, err := os.Stat(work)
	if err != nil {
		return nil, err
	}
	bodySize := fi.Size()
	if bodySize < GlobalHeaderSize {
		return nil, fmt.Errorf("embed: file too small")
	}
	if n, err := stripEmbeddedDownloadIndex(work); err != nil {
		return nil, err
	} else {
		bodySize = n
	}

	f, err := os.OpenFile(work, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := f.Truncate(bodySize); err != nil {
		return nil, err
	}

	blocks, err := buildTransportBlocks(f, bodySize, opt.BlockSize)
	if err != nil {
		return nil, err
	}

	// Placeholder archive hash; patched after append.
	var zero [32]byte
	payload, err := EncodeDownloadIndexPayload(opt.BlockSize, blocks, zero)
	if err != nil {
		return nil, err
	}
	tailRecord := WrapTailRecord(TailTypeDownloadIndex, payload)
	tailOff := bodySize
	tailSize := int64(len(tailRecord))

	if _, err := f.Seek(tailOff, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := f.Write(tailRecord); err != nil {
		return nil, err
	}
	footer := DownloadIndexFooter{
		Magic:           DownloadIndexFooterMagic,
		TailChainOffset: uint64(tailOff),
		TailChainSize:   uint64(tailSize),
	}
	if _, err := f.Write(footer.Marshal()); err != nil {
		return nil, err
	}
	finalSize := bodySize + tailSize + DownloadIndexFooterSize

	gh, err := readGlobalHeaderAt(f)
	if err != nil {
		return nil, err
	}
	gh.Flags |= FlagHasDownloadIndex
	SetTailChainReserved(gh, uint64(tailOff), uint64(tailSize))
	if err := writeAt(f, 0, mustHeaderBytes(gh)); err != nil {
		return nil, err
	}

	// Header patch changed body bytes — rebuild block hashes for [0, bodySize).
	blocks, err = buildTransportBlocks(f, bodySize, opt.BlockSize)
	if err != nil {
		return nil, err
	}
	// archiveBlake3 is the body digest (pre-tail). Hashing the whole file would
	// be self-referential because the digest sits inside the tail payload.
	bodyHash, err := hashFileBytes(f, 0, bodySize)
	if err != nil {
		return nil, err
	}
	payload, err = EncodeDownloadIndexPayload(opt.BlockSize, blocks, bodyHash)
	if err != nil {
		return nil, err
	}
	tailRecord = WrapTailRecord(TailTypeDownloadIndex, payload)
	if int64(len(tailRecord)) != tailSize {
		return nil, fmt.Errorf("embed: tail size changed")
	}
	if _, err := f.WriteAt(tailRecord, tailOff); err != nil {
		return nil, err
	}

	return &EmbedResult{
		Path:            work,
		BodySize:        bodySize,
		FinalSize:       finalSize,
		BlockCount:      len(blocks),
		TailChainOffset: tailOff,
		TailChainSize:   tailSize,
	}, nil
}

func hashFileBytes(f *os.File, off, size int64) ([32]byte, error) {
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return [32]byte{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, size))
	if err != nil {
		return [32]byte{}, err
	}
	if int64(len(data)) != size {
		return [32]byte{}, fmt.Errorf("embed: short read for hash")
	}
	return Blake3Sum256(data), nil
}

func stripEmbeddedDownloadIndex(path string) (bodySize int64, err error) {
	res, err := StripDownloadIndex(path)
	if err != nil {
		return 0, err
	}
	return res.BodySize, nil
}

// StripResult describes the outcome of removing an embedded download index.
type StripResult struct {
	Path      string
	BodySize  int64
	HadIndex  bool // false when file had no NYADIDX1 footer (already clean)
	FinalSize int64
}

// HasEmbeddedDownloadIndex reports whether path ends with a valid download-index footer.
func HasEmbeddedDownloadIndex(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false, err
	}
	size := fi.Size()
	if size < DownloadIndexFooterSize+GlobalHeaderSize {
		return false, nil
	}
	footerBuf := make([]byte, DownloadIndexFooterSize)
	if _, err := f.ReadAt(footerBuf, size-DownloadIndexFooterSize); err != nil {
		return false, err
	}
	foot, err := ParseDownloadIndexFooter(footerBuf)
	if err != nil {
		return false, nil
	}
	return int64(foot.TailChainOffset+foot.TailChainSize+DownloadIndexFooterSize) == size, nil
}

// StripDownloadIndex removes an embedded download index (tail + EOF footer) and
// clears FlagHasDownloadIndex. Idempotent: if no index is present, returns
// HadIndex=false and leaves the file unchanged.
func StripDownloadIndex(path string) (*StripResult, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	out := &StripResult{Path: path, BodySize: size, FinalSize: size, HadIndex: false}
	if size < DownloadIndexFooterSize+GlobalHeaderSize {
		return out, nil
	}
	footerBuf := make([]byte, DownloadIndexFooterSize)
	if _, err := f.ReadAt(footerBuf, size-DownloadIndexFooterSize); err != nil {
		return nil, err
	}
	foot, err := ParseDownloadIndexFooter(footerBuf)
	if err != nil {
		return out, nil
	}
	if int64(foot.TailChainOffset+foot.TailChainSize+DownloadIndexFooterSize) != size {
		return out, nil
	}
	body := int64(foot.TailChainOffset)
	if err := f.Truncate(body); err != nil {
		return nil, err
	}
	gh, err := readGlobalHeaderAt(f)
	if err != nil {
		return nil, fmt.Errorf("strip download index: read header: %w", err)
	}
	gh.Flags &^= FlagHasDownloadIndex
	if gh.Flags&FlagKDFArgon2id == 0 {
		binary.LittleEndian.PutUint64(gh.Reserved[0:8], 0)
		binary.LittleEndian.PutUint64(gh.Reserved[8:16], 0)
	}
	if err := writeAt(f, 0, mustHeaderBytes(gh)); err != nil {
		return nil, err
	}
	out.HadIndex = true
	out.BodySize = body
	out.FinalSize = body
	return out, nil
}

func mustHeaderBytes(h *GlobalHeader) []byte {
	buf := make([]byte, GlobalHeaderSize)
	copy(buf[0:8], h.Magic[:])
	binary.LittleEndian.PutUint16(buf[8:10], h.VersionMajor)
	binary.LittleEndian.PutUint16(buf[10:12], h.VersionMinor)
	binary.LittleEndian.PutUint32(buf[12:16], h.Flags)
	binary.LittleEndian.PutUint64(buf[16:24], h.DataAreaSize)
	binary.LittleEndian.PutUint64(buf[24:32], h.CentralDirOffset)
	binary.LittleEndian.PutUint64(buf[32:40], h.CentralDirSize)
	binary.LittleEndian.PutUint64(buf[40:48], uint64(h.CreationTime))
	binary.LittleEndian.PutUint64(buf[48:56], h.TotalOrigSize)
	copy(buf[56:88], h.Blake3[:])
	copy(buf[88:128], h.Reserved[:])
	return buf
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// ManifestFromEmbeddedFile reads an embedded download index into a Manifest.
// Archive.Size/Blake3 describe the archive body (pre-tail); Open/extract work
// on the body alone. Tail+footer are only needed to rediscover the index.
func ManifestFromEmbeddedFile(path string, sourceURL string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return ManifestFromEmbeddedReader(f, fi.Size(), filepath.Base(path), sourceURL)
}

// ManifestFromEmbeddedReader loads download index via EOF footer.
func ManifestFromEmbeddedReader(r io.ReaderAt, size int64, name, sourceURL string) (*Manifest, error) {
	body, blocks, blockSize, archHash, err := readEmbeddedIndex(r, size)
	if err != nil {
		return nil, err
	}
	m := &Manifest{
		Format:  ManifestFormat,
		Version: ManifestVersion,
		Archive: ArchiveMeta{
			Name:   name,
			Size:   body,
			Blake3: hex.EncodeToString(archHash[:]),
		},
		Download: DownloadIndex{
			BlockSize: blockSize,
			Blocks:    blocks,
		},
	}
	if sourceURL != "" {
		m.Sources = []ManifestSource{{URL: sourceURL, Priority: 10}}
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func readEmbeddedIndex(r io.ReaderAt, size int64) (body int64, blocks []DownloadBlock, blockSize int64, archHash [32]byte, err error) {
	if size < DownloadIndexFooterSize+GlobalHeaderSize {
		return 0, nil, 0, archHash, fmt.Errorf("embedded index: file too small")
	}
	footerBuf := make([]byte, DownloadIndexFooterSize)
	if _, err := r.ReadAt(footerBuf, size-DownloadIndexFooterSize); err != nil {
		return 0, nil, 0, archHash, err
	}
	foot, err := ParseDownloadIndexFooter(footerBuf)
	if err != nil {
		return 0, nil, 0, archHash, fmt.Errorf("embedded index: %w (run: nya manifest --embed)", err)
	}
	if foot.TailChainSize == 0 || int64(foot.TailChainOffset+foot.TailChainSize+DownloadIndexFooterSize) != size {
		return 0, nil, 0, archHash, fmt.Errorf("embedded index: invalid tail chain bounds")
	}
	raw := make([]byte, foot.TailChainSize)
	if _, err := r.ReadAt(raw, int64(foot.TailChainOffset)); err != nil {
		return 0, nil, 0, archHash, err
	}
	if len(raw) < 8 {
		return 0, nil, 0, archHash, fmt.Errorf("embedded index: short tail")
	}
	typeID := binary.LittleEndian.Uint32(raw[0:4])
	plen := binary.LittleEndian.Uint32(raw[4:8])
	if typeID != TailTypeDownloadIndex {
		return 0, nil, 0, archHash, fmt.Errorf("embedded index: unexpected tail type 0x%x", typeID)
	}
	if int(plen)+8 > len(raw) {
		return 0, nil, 0, archHash, fmt.Errorf("embedded index: truncated record")
	}
	blockSize, blocks, archHash, err = DecodeDownloadIndexPayload(raw[8 : 8+plen])
	if err != nil {
		return 0, nil, 0, archHash, err
	}
	return int64(foot.TailChainOffset), blocks, blockSize, archHash, nil
}

// ReadEmbeddedTailAndFooter returns the raw tail+footer bytes for local assembly.
func ReadEmbeddedTailAndFooter(r io.ReaderAt, size int64) (tailOff int64, blob []byte, err error) {
	footerBuf := make([]byte, DownloadIndexFooterSize)
	if _, err := r.ReadAt(footerBuf, size-DownloadIndexFooterSize); err != nil {
		return 0, nil, err
	}
	foot, err := ParseDownloadIndexFooter(footerBuf)
	if err != nil {
		return 0, nil, err
	}
	n := int64(foot.TailChainSize) + DownloadIndexFooterSize
	blob = make([]byte, n)
	if _, err := r.ReadAt(blob, int64(foot.TailChainOffset)); err != nil {
		return 0, nil, err
	}
	return int64(foot.TailChainOffset), blob, nil
}
