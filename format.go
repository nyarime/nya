// Package nar implements the NAR (Nyarime Archive) format v1.0.
//
// NAR = Zstandard compression + GoFEC recovery in one file.
// Unlike RAR (max 10% recovery), NAR uses RaptorQ for 50%+ corruption recovery.
//
// Layout:
//   [Global Header 128B]
//   [Data Area: compressed chunks + FEC parity]
//   [Central Directory: file index]
//   [Global Footer: optional]
package nya

import (
	"encoding/binary"
	"errors"
	"io"
	"fmt"
)

// Magic bytes
var (
	MagicHeader = [8]byte{'N', 'Y', 'A', 0, 'v', '0', '1', 0}
	MagicFooter = [8]byte{'N', 'Y', 'A', 'F', 'O', 'O', 'T', 0}
)

const (
	GlobalHeaderSize = 128
	ChunkHeaderSize  = 32
	FECParamsSize    = 16
)

// Header flags
const (
	FlagHasFooter      = 1 << 0
	FlagSolidCompress  = 1 << 1 // v2
	FlagEncrypted      = 1 << 2 // v2
	FlagHasGlobalFEC   = 1 << 3 // v2
)

// Compression IDs
const (
	CompressNone    uint16 = 0
	CompressZstd    uint16 = 1 // default
	CompressS2      uint16 = 2
	CompressBrotli  uint16 = 3
	CompressLZ4     uint16 = 4
	CompressZstdDict uint16 = 5 // v2
	CompressLzma2    uint16 = 6 // --best mode
)

// FEC types
const (
	FECNone    uint8 = 0
	FECRaptorQ uint8 = 1
	FECLDPC    uint8 = 2
	FECRS      uint8 = 3
)

// BCJ filter types
const (
	BCJNone  uint8 = 0
	BCJX86   uint8 = 1
	BCJARM   uint8 = 2
	BCJARM64 uint8 = 3
	BCJMIPS  uint8 = 4
)

// Entry types
const (
	EntryFile     uint8 = 0
	EntryDir      uint8 = 1
	EntrySymlink  uint8 = 2
	EntryHardlink uint8 = 3
	EntryCharDev  uint8 = 4
	EntryBlockDev uint8 = 5
	EntryFifo     uint8 = 6
)

// DirEntry format versions
const (
	DirEntryV1 uint8 = 1 // original format
	DirEntryV2 uint8 = 2 // with Unix metadata
)

var (
	ErrNotNAR       = errors.New("nar: not a NYA archive")
	ErrUnsupported  = errors.New("nar: unsupported version")
	ErrCorrupted    = errors.New("nar: data corrupted")
)

// GlobalHeader 128 bytes
type GlobalHeader struct {
	Magic            [8]byte
	VersionMajor     uint16
	VersionMinor     uint16
	Flags            uint32
	DataAreaSize     uint64
	CentralDirOffset uint64
	CentralDirSize   uint64
	CreationTime     int64  // unix nano
	TotalOrigSize    uint64
	Blake3           [32]byte // Data Area BLAKE3-256
	Reserved         [40]byte
}

func (h *GlobalHeader) Write(w io.Writer) error {
	return binary.Write(w, binary.LittleEndian, h)
}

func ReadGlobalHeader(r io.Reader) (*GlobalHeader, error) {
	h := &GlobalHeader{}
	if err := binary.Read(r, binary.LittleEndian, h); err != nil {
		return nil, err
	}
	if h.Magic != MagicHeader {
		return nil, ErrNotNAR
	}
	if h.VersionMajor > 1 {
		return nil, ErrUnsupported
	}
	return h, nil
}

// ChunkHeader 32 bytes (per data block)
type ChunkHeader struct {
	OriginalSize     uint64
	CompressedSize   uint64
	RepairCount      uint32
	SymbolSize       uint32
	Blake3Short      uint64 // first 8 bytes of BLAKE3
}

func (c *ChunkHeader) Write(w io.Writer) error {
	return binary.Write(w, binary.LittleEndian, c)
}

func ReadChunkHeader(r io.Reader) (*ChunkHeader, error) {
	c := &ChunkHeader{}
	if err := binary.Read(r, binary.LittleEndian, c); err != nil {
		return nil, err
	}
	return c, nil
}

// FECParams 16 bytes (union-style)
type FECParams struct {
	Param1   uint32 // RaptorQ: K, LDPC: M
	Param2   uint32 // RaptorQ: T, LDPC: parity
	Param3   uint32 // RaptorQ: repairCount, LDPC: density*10000
	Reserved uint32
}

// DirEntry in Central Directory
type DirEntry struct {
	Path          string
	EntryType     uint8
	Mode          uint32
	MTimeNano     int64
	OriginalSize  uint64
	ChunkCount    uint32
	CompressionID uint16
	FECType       uint8
	BCJFilter     uint8
	FECParams     FECParams
	FirstDataOff  uint64

	// V2 Unix metadata
	Uid        uint32
	Gid        uint32
	UserName   string
	GroupName  string
	LinkTarget string             // symlink target or hardlink name
	DevMajor   uint32             // for char/block devices
	DevMinor   uint32
	Xattrs     map[string][]byte  // extended attributes
}

func WriteDirEntry(w io.Writer, e *DirEntry) error {
	// Write version byte
	binary.Write(w, binary.LittleEndian, DirEntryV2)

	pathBytes := []byte(e.Path)
	binary.Write(w, binary.LittleEndian, uint16(len(pathBytes)))
	w.Write(pathBytes)
	binary.Write(w, binary.LittleEndian, e.EntryType)
	binary.Write(w, binary.LittleEndian, e.Mode)
	binary.Write(w, binary.LittleEndian, e.MTimeNano)
	binary.Write(w, binary.LittleEndian, e.OriginalSize)
	binary.Write(w, binary.LittleEndian, e.ChunkCount)
	binary.Write(w, binary.LittleEndian, e.CompressionID)
	binary.Write(w, binary.LittleEndian, e.FECType)
	binary.Write(w, binary.LittleEndian, e.BCJFilter)
	binary.Write(w, binary.LittleEndian, e.FECParams)
	binary.Write(w, binary.LittleEndian, e.FirstDataOff)

	// V2 fields
	binary.Write(w, binary.LittleEndian, e.Uid)
	binary.Write(w, binary.LittleEndian, e.Gid)
	writeLenStr(w, e.UserName)
	writeLenStr(w, e.GroupName)
	writeLenStr(w, e.LinkTarget)
	binary.Write(w, binary.LittleEndian, e.DevMajor)
	binary.Write(w, binary.LittleEndian, e.DevMinor)

	// Xattrs: count + (key + value) pairs
	xattrCount := uint32(len(e.Xattrs))
	binary.Write(w, binary.LittleEndian, xattrCount)
	for k, v := range e.Xattrs {
		writeLenStr(w, k)
		binary.Write(w, binary.LittleEndian, uint32(len(v)))
		w.Write(v)
	}
	return nil
}

func writeLenStr(w io.Writer, s string) {
	b := []byte(s)
	binary.Write(w, binary.LittleEndian, uint16(len(b)))
	w.Write(b)
}

func readLenStr(r io.Reader) (string, error) {
	var n uint16
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

func ReadDirEntry(r io.Reader) (*DirEntry, error) {
	// Peek version byte. V1 had no version; first byte was uint16 pathLen low byte.
	// V2 starts with version byte = 2. V1 pathLen low byte is unlikely to be exactly 1 or 2
	// for valid paths, but we use the dedicated version marker.
	var version uint8
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, err
	}

	var pathLen uint16
	if version == DirEntryV2 {
		if err := binary.Read(r, binary.LittleEndian, &pathLen); err != nil {
			return nil, err
		}
	} else {
		// V1 compatibility: version byte is actually low byte of pathLen
		var hi uint8
		if err := binary.Read(r, binary.LittleEndian, &hi); err != nil {
			return nil, err
		}
		pathLen = uint16(version) | uint16(hi)<<8
		version = DirEntryV1
	}

	pathBytes := make([]byte, pathLen)
	if _, err := io.ReadFull(r, pathBytes); err != nil {
		return nil, err
	}

	e := &DirEntry{Path: string(pathBytes)}
	binary.Read(r, binary.LittleEndian, &e.EntryType)
	binary.Read(r, binary.LittleEndian, &e.Mode)
	binary.Read(r, binary.LittleEndian, &e.MTimeNano)
	binary.Read(r, binary.LittleEndian, &e.OriginalSize)
	binary.Read(r, binary.LittleEndian, &e.ChunkCount)
	binary.Read(r, binary.LittleEndian, &e.CompressionID)
	binary.Read(r, binary.LittleEndian, &e.FECType)
	binary.Read(r, binary.LittleEndian, &e.BCJFilter)
	binary.Read(r, binary.LittleEndian, &e.FECParams)
	binary.Read(r, binary.LittleEndian, &e.FirstDataOff)

	if version >= DirEntryV2 {
		binary.Read(r, binary.LittleEndian, &e.Uid)
		binary.Read(r, binary.LittleEndian, &e.Gid)
		e.UserName, _ = readLenStr(r)
		e.GroupName, _ = readLenStr(r)
		e.LinkTarget, _ = readLenStr(r)
		binary.Read(r, binary.LittleEndian, &e.DevMajor)
		binary.Read(r, binary.LittleEndian, &e.DevMinor)

		var xattrCount uint32
		binary.Read(r, binary.LittleEndian, &xattrCount)
		if xattrCount > 0 && xattrCount < 100000 {
			e.Xattrs = make(map[string][]byte, xattrCount)
			for i := uint32(0); i < xattrCount; i++ {
				key, err := readLenStr(r)
				if err != nil { break }
				var vlen uint32
				if err := binary.Read(r, binary.LittleEndian, &vlen); err != nil { break }
				if vlen > 65536 { break }
				val := make([]byte, vlen)
				if _, err := io.ReadFull(r, val); err != nil { break }
				e.Xattrs[key] = val
			}
		}
	}
	return e, nil
}

// HumanSize formats byte count
func HumanSize(b int) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(b)/1024/1024)
	}
}
