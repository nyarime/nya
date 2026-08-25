package nya

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ManifestFormat        = "nyam"
	ManifestVersion       = 1
	DefaultDownloadBlock  = 4 * 1024 * 1024 // 4 MiB
)

// Manifest is the .nyam sidecar for resumable parallel downloads.
type Manifest struct {
	Format   string           `json:"format"`
	Version  int              `json:"version"`
	Archive  ArchiveMeta      `json:"archive"`
	Download DownloadIndex    `json:"download"`
	Entries  []ManifestEntry  `json:"entries,omitempty"`
	Sources  []ManifestSource `json:"sources,omitempty"`
}

// ManifestEntry maps one file path to on-disk data-area chunk byte ranges.
type ManifestEntry struct {
	Path         string              `json:"path"`
	OriginalSize int64               `json:"original_size"`
	ChunkCount   int                 `json:"chunk_count"`
	Chunks       []ManifestFileChunk `json:"chunks"`
}

// ManifestFileChunk is one on-disk chunk (ChunkHeader + compressed payload).
type ManifestFileChunk struct {
	Index        int    `json:"index"`
	Offset       int64  `json:"offset"`
	Size         int64  `json:"size"`
	OriginalSize int64  `json:"original_size"`
	Blake3       string `json:"blake3"`
}

// ArchiveMeta describes the target .nya file.
type ArchiveMeta struct {
	Name             string `json:"name"`
	Size             int64  `json:"size"`
	Blake3           string `json:"blake3"`
	NYAVersion       string `json:"nya_version,omitempty"`
	CentralDirOffset int64  `json:"central_dir_offset,omitempty"`
	FECOffset        int64  `json:"fec_offset,omitempty"`
	FECBytes         int64  `json:"fec_bytes,omitempty"`
}

// DownloadIndex lists HTTP Range transport blocks.
type DownloadIndex struct {
	BlockSize int64           `json:"block_size"`
	Blocks    []DownloadBlock `json:"blocks"`
}

// DownloadBlock is one byte range of the on-disk archive.
type DownloadBlock struct {
	ID     uint32 `json:"id"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	Blake3 string `json:"blake3"`
}

// ManifestSource is a download URL with optional priority.
type ManifestSource struct {
	URL      string `json:"url"`
	Priority int    `json:"priority,omitempty"`
}

// BuildManifest indexes path in transport blocks for parallel HTTP download.
func BuildManifest(path string, blockSize int64, sources ...ManifestSource) (*Manifest, error) {
	if blockSize <= 0 {
		blockSize = DefaultDownloadBlock
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	if size == 0 {
		return nil, fmt.Errorf("manifest: empty archive")
	}

	wholeHash, nyaVer, fecOff, fecLen, err := archiveDigestMeta(f, size)
	if err != nil {
		return nil, err
	}

	blocks, err := buildTransportBlocks(f, size, blockSize)
	if err != nil {
		return nil, err
	}

	fileEntries, cdOff, err := buildManifestFileEntries(path)
	if err != nil {
		return nil, err
	}

	m := &Manifest{
		Format:  ManifestFormat,
		Version: ManifestVersion,
		Archive: ArchiveMeta{
			Name:             filepath.Base(path),
			Size:             size,
			Blake3:           hex.EncodeToString(wholeHash[:]),
			NYAVersion:       nyaVer,
			CentralDirOffset: cdOff,
			FECOffset:        fecOff,
			FECBytes:         fecLen,
		},
		Download: DownloadIndex{
			BlockSize: blockSize,
			Blocks:    blocks,
		},
		Entries: fileEntries,
	}
	if len(sources) > 0 {
		m.Sources = append([]ManifestSource(nil), sources...)
		sort.SliceStable(m.Sources, func(i, j int) bool {
			return m.Sources[i].Priority > m.Sources[j].Priority
		})
	}
	return m, nil
}

func archiveDigestMeta(f *os.File, size int64) ([32]byte, string, int64, int64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return [32]byte{}, "", 0, 0, err
	}

	var nyaVer string
	var fecOff, fecLen int64
	if gh, err := ReadGlobalHeader(f); err == nil {
		nyaVer = fmt.Sprintf("%d.%d", gh.VersionMajor, gh.VersionMinor)
		fecOff = int64(gh.CentralDirOffset) + int64(gh.CentralDirSize)
		if fecOff+4 <= size {
			if _, err := f.Seek(fecOff, io.SeekStart); err == nil {
				var n uint32
				if binaryReadU32(f, &n) == nil {
					fecLen = int64(n)
				}
			}
		}
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return [32]byte{}, "", 0, 0, err
	}
	data, err := io.ReadAll(io.LimitReader(f, size))
	if err != nil {
		return [32]byte{}, "", 0, 0, err
	}
	return Blake3Sum256(data), nyaVer, fecOff, fecLen, nil
}

func binaryReadU32(r io.Reader, out *uint32) error {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	*out = uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
	return nil
}

func buildTransportBlocks(f *os.File, size, blockSize int64) ([]DownloadBlock, error) {
	buf := make([]byte, blockSize)
	var blocks []DownloadBlock
	var id uint32
	var offset int64

	for offset < size {
		n := size - offset
		if n > blockSize {
			n = blockSize
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
		chunk := buf[:n]
		if _, err := io.ReadFull(f, chunk); err != nil {
			return nil, fmt.Errorf("manifest: read block %d: %w", id, err)
		}
		h := Blake3Sum256(chunk)
		blocks = append(blocks, DownloadBlock{
			ID:     id,
			Offset: offset,
			Size:   n,
			Blake3: hex.EncodeToString(h[:]),
		})
		id++
		offset += n
	}
	return blocks, nil
}

// Validate checks manifest invariants.
func (m *Manifest) Validate() error {
	if m.Format != ManifestFormat {
		return fmt.Errorf("manifest: unknown format %q", m.Format)
	}
	if m.Version != ManifestVersion {
		return fmt.Errorf("manifest: unsupported version %d", m.Version)
	}
	if m.Archive.Size <= 0 {
		return fmt.Errorf("manifest: invalid archive size")
	}
	if len(m.Archive.Blake3) != 64 {
		return fmt.Errorf("manifest: archive blake3 must be 64 hex chars")
	}
	if len(m.Download.Blocks) == 0 {
		return fmt.Errorf("manifest: no download blocks")
	}

	var total int64
	var expectOff int64
	for i, b := range m.Download.Blocks {
		if b.ID != uint32(i) {
			return fmt.Errorf("manifest: block id gap at index %d", i)
		}
		if b.Offset != expectOff {
			return fmt.Errorf("manifest: block %d offset mismatch", b.ID)
		}
		if b.Size <= 0 || b.Offset+b.Size > m.Archive.Size {
			return fmt.Errorf("manifest: block %d out of range", b.ID)
		}
		if len(b.Blake3) != 64 {
			return fmt.Errorf("manifest: block %d blake3 invalid", b.ID)
		}
		total += b.Size
		expectOff += b.Size
	}
	if total != m.Archive.Size {
		return fmt.Errorf("manifest: blocks cover %d bytes, want %d", total, m.Archive.Size)
	}
	for i, ent := range m.Entries {
		if ent.Path == "" {
			return fmt.Errorf("manifest: entry %d missing path", i)
		}
		if len(ent.Chunks) == 0 {
			return fmt.Errorf("manifest: entry %q has no chunks", ent.Path)
		}
		if ent.ChunkCount != len(ent.Chunks) {
			return fmt.Errorf("manifest: entry %q chunk_count mismatch", ent.Path)
		}
		for _, ch := range ent.Chunks {
			if ch.Size <= 0 || ch.Offset+ch.Size > m.Archive.Size {
				return fmt.Errorf("manifest: entry %q chunk %d out of range", ent.Path, ch.Index)
			}
			if len(ch.Blake3) != 64 {
				return fmt.Errorf("manifest: entry %q chunk %d blake3 invalid", ent.Path, ch.Index)
			}
		}
	}
	return nil
}

func buildManifestFileEntries(archivePath string) ([]ManifestEntry, int64, error) {
	r, err := Open(archivePath)
	if err != nil {
		// Transport blocks still work for non-NYA or truncated files.
		return nil, 0, nil
	}
	cdOff := int64(r.Header.CentralDirOffset)

	f, err := os.Open(archivePath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var entries []ManifestEntry
	for _, e := range r.Entries {
		if e.EntryType != EntryFile {
			continue
		}
		chunks, err := manifestChunksForEntry(r, f, &e)
		if err != nil {
			return nil, 0, fmt.Errorf("manifest: entry %s: %w", e.Path, err)
		}
		entries = append(entries, ManifestEntry{
			Path:         e.Path,
			OriginalSize: int64(e.OriginalSize),
			ChunkCount:   len(chunks),
			Chunks:       chunks,
		})
	}
	return entries, cdOff, nil
}

func manifestChunksForEntry(r *Reader, f *os.File, e *DirEntry) ([]ManifestFileChunk, error) {
	var chunks []ManifestFileChunk
	if r.Header.Flags&FlagSolidCompress != 0 {
		off := uint64(0)
		if off+ChunkHeaderSize > uint64(len(r.data)) {
			return nil, fmt.Errorf("solid data truncated")
		}
		ch, err := ReadChunkHeader(bytes.NewReader(r.data[off:]))
		if err != nil {
			return nil, err
		}
		stride := chunkDataStride(ch)
		absOff := int64(GlobalHeaderSize)
		h, err := hashFileRange(f, absOff, int64(stride))
		if err != nil {
			return nil, err
		}
		return []ManifestFileChunk{{
			Index:        0,
			Offset:       absOff,
			Size:         int64(stride),
			OriginalSize: int64(ch.OriginalSize),
			Blake3:       h,
		}}, nil
	}

	off := e.FirstDataOff
	for c := uint32(0); c < e.ChunkCount; c++ {
		if off+ChunkHeaderSize > uint64(len(r.data)) {
			return nil, fmt.Errorf("chunk %d truncated", c)
		}
		ch, err := ReadChunkHeader(bytes.NewReader(r.data[off:]))
		if err != nil {
			return nil, err
		}
		stride := chunkDataStride(ch)
		absOff := int64(GlobalHeaderSize) + int64(off)
		h, err := hashFileRange(f, absOff, int64(stride))
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, ManifestFileChunk{
			Index:        int(c),
			Offset:       absOff,
			Size:         int64(stride),
			OriginalSize: int64(ch.OriginalSize),
			Blake3:       h,
		})
		off += stride
	}
	return chunks, nil
}

func hashFileRange(f *os.File, offset, size int64) (string, error) {
	buf := make([]byte, size)
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	if _, err := io.ReadFull(f, buf); err != nil {
		return "", err
	}
	h := Blake3Sum256(buf)
	return hex.EncodeToString(h[:]), nil
}

// FetchRangesForPaths returns byte ranges needed to fetch the listed paths
// (global header, each on-disk chunk, and central directory tail).
func (m *Manifest) FetchRangesForPaths(paths []string) ([]fetchByteRange, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if len(m.Entries) == 0 {
		return nil, fmt.Errorf("manifest: no file entries for partial fetch")
	}
	want := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		want[p] = struct{}{}
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("manifest: empty path list")
	}

	var ranges []fetchByteRange
	ranges = append(ranges, fetchByteRange{0, int64(GlobalHeaderSize)})

	found := 0
	for _, ent := range m.Entries {
		if _, ok := want[ent.Path]; !ok {
			continue
		}
		found++
		for _, ch := range ent.Chunks {
			ranges = append(ranges, fetchByteRange{ch.Offset, ch.Offset + ch.Size})
		}
	}
	if found != len(want) {
		var missing []string
		for p := range want {
			var ok bool
			for _, ent := range m.Entries {
				if ent.Path == p {
					ok = true
					break
				}
			}
			if !ok {
				missing = append(missing, p)
			}
		}
		return nil, fmt.Errorf("manifest: path not found: %s", strings.Join(missing, ", "))
	}

	if m.Archive.CentralDirOffset > 0 && m.Archive.CentralDirOffset < m.Archive.Size {
		ranges = append(ranges, fetchByteRange{m.Archive.CentralDirOffset, m.Archive.Size})
	}
	return mergeFetchRanges(ranges), nil
}

type fetchByteRange struct {
	start, end int64 // end exclusive
}

func mergeFetchRanges(in []fetchByteRange) []fetchByteRange {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool { return in[i].start < in[j].start })
	out := []fetchByteRange{in[0]}
	for i := 1; i < len(in); i++ {
		last := &out[len(out)-1]
		if in[i].start <= last.end {
			if in[i].end > last.end {
				last.end = in[i].end
			}
			continue
		}
		out = append(out, in[i])
	}
	return out
}

func rangesOverlap(aStart, aEnd, bStart, bEnd int64) bool {
	return aStart < bEnd && bStart < aEnd
}

func filterBlocksByRanges(blocks []DownloadBlock, ranges []fetchByteRange) []DownloadBlock {
	var out []DownloadBlock
	for _, b := range blocks {
		bEnd := b.Offset + b.Size
		for _, r := range ranges {
			if rangesOverlap(b.Offset, bEnd, r.start, r.end) {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// WriteManifest writes JSON manifest to path.
func WriteManifest(m *Manifest, path string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// ReadManifest loads a .nyam file.
func ReadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseManifest(data)
}

// ParseManifest decodes .nyam JSON bytes.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// ResolveManifestSources turns relative source URLs into absolute ones using baseURL
// (typically the URL of the .nyam itself). Absolute sources are left unchanged.
func ResolveManifestSources(m *Manifest, baseURL string) error {
	if m == nil {
		return fmt.Errorf("manifest: nil")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("manifest: base url: %w", err)
	}
	if len(m.Sources) == 0 && m.Archive.Name != "" {
		m.Sources = []ManifestSource{{URL: m.Archive.Name, Priority: 10}}
	}
	for i := range m.Sources {
		su, err := url.Parse(m.Sources[i].URL)
		if err != nil {
			return fmt.Errorf("manifest: source url: %w", err)
		}
		if su.IsAbs() {
			continue
		}
		m.Sources[i].URL = base.ResolveReference(su).String()
	}
	return nil
}

// ParseBlockSize accepts 4m, 8M, 4194304.
func ParseBlockSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultDownloadBlock, nil
	}
	mult := int64(1)
	switch suffix := strings.ToLower(s[len(s)-1:]); suffix {
	case "k":
		mult = 1024
		s = s[:len(s)-1]
	case "m":
		mult = 1024 * 1024
		s = s[:len(s)-1]
	case "g":
		mult = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid block size %q", s)
	}
	n *= mult
	if n < 64*1024 {
		return 0, fmt.Errorf("block size must be at least 64KB")
	}
	return n, nil
}

// DownloadState is the local resume file for nya-get.
type DownloadState struct {
	ManifestBlake3 string    `json:"manifest_blake3"`
	Output         string    `json:"output"`
	Completed      []uint32  `json:"completed"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// StatePath returns the default resume path for a manifest file.
func StatePath(manifestPath string) string {
	return manifestPath + ".state"
}
