package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// SFX footer layout (SPEC-SFX.md).
const (
	SFXMagic       = "NYASFX01"
	SFXFooterSize  = 40
	SFXFlagConsole = 1 << 0
)

// SfxFooter describes the 40-byte trailer at the end of a self-extracting file.
type SfxFooter struct {
	ArchiveOffset uint64
	ArchiveSize   uint64
	ConfigOffset  uint64
	ConfigSize    uint32
	Flags         uint32
}

// ParseSfxFooter reads the footer from the last 40 bytes of data.
func ParseSfxFooter(data []byte) (*SfxFooter, error) {
	if len(data) < SFXFooterSize {
		return nil, fmt.Errorf("sfx: file too small")
	}
	tail := data[len(data)-SFXFooterSize:]
	if string(tail[:8]) != SFXMagic {
		return nil, fmt.Errorf("sfx: bad footer magic")
	}
	return &SfxFooter{
		ArchiveOffset: binary.LittleEndian.Uint64(tail[8:16]),
		ArchiveSize:   binary.LittleEndian.Uint64(tail[16:24]),
		ConfigOffset:  binary.LittleEndian.Uint64(tail[24:32]),
		ConfigSize:    binary.LittleEndian.Uint32(tail[32:36]),
		Flags:         binary.LittleEndian.Uint32(tail[36:40]),
	}, nil
}

// IsSFX reports whether path points at a file with an NYA SFX footer.
func IsSFX(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.Size() < SFXFooterSize {
		return false
	}
	_, err = f.Seek(fi.Size()-SFXFooterSize, io.SeekStart)
	if err != nil {
		return false
	}
	buf := make([]byte, 8)
	if _, err := io.ReadFull(f, buf); err != nil {
		return false
	}
	return string(buf) == SFXMagic
}

// SliceSFXArchive returns the embedded .nya bytes from a self-extracting file.
func SliceSFXArchive(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	foot, err := ParseSfxFooter(data)
	if err != nil {
		return nil, err
	}
	start := int(foot.ArchiveOffset)
	end := start + int(foot.ArchiveSize)
	if start < 0 || end > len(data) {
		return nil, fmt.Errorf("sfx: archive slice out of range")
	}
	out := make([]byte, foot.ArchiveSize)
	copy(out, data[start:end])
	return out, nil
}

// BuildSFX writes stub + archive + optional config + footer to outPath.
func BuildSFX(stubPath, archivePath, outPath string, config []byte, flags uint32) error {
	stub, err := os.ReadFile(stubPath)
	if err != nil {
		return fmt.Errorf("sfx: read stub: %w", err)
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("sfx: read archive: %w", err)
	}
	if len(archive) < GlobalHeaderSize || !bytes.Equal(archive[:8], MagicHeader[:]) {
		return fmt.Errorf("sfx: not a NYA archive")
	}

	archiveOffset := uint64(len(stub))
	configOffset := uint64(0)
	configSize := uint32(0)
	if len(config) > 0 {
		configOffset = archiveOffset + uint64(len(archive))
		configSize = uint32(len(config))
	}

	var buf bytes.Buffer
	buf.Write(stub)
	buf.Write(archive)
	if len(config) > 0 {
		buf.Write(config)
	}

	foot := make([]byte, SFXFooterSize)
	copy(foot[:8], SFXMagic)
	binary.LittleEndian.PutUint64(foot[8:16], archiveOffset)
	binary.LittleEndian.PutUint64(foot[16:24], uint64(len(archive)))
	binary.LittleEndian.PutUint64(foot[24:32], configOffset)
	binary.LittleEndian.PutUint32(foot[32:36], configSize)
	binary.LittleEndian.PutUint32(foot[36:40], flags)
	buf.Write(foot)

	if err := os.WriteFile(outPath, buf.Bytes(), 0755); err != nil {
		return fmt.Errorf("sfx: write output: %w", err)
	}
	return nil
}

// OpenAny opens a plain .nya or an SFX wrapper (extracts embedded archive to memory).
func OpenAny(path string, password ...[]byte) (*Reader, error) {
	if IsSFX(path) {
		raw, err := SliceSFXArchive(path)
		if err != nil {
			return nil, err
		}
		return OpenReaderAt(bytes.NewReader(raw), int64(len(raw)), password...)
	}
	return Open(path, password...)
}

// OpenReaderAt parses an archive from a byte stream (used for SFX slices).
func OpenReaderAt(r io.ReaderAt, size int64, password ...[]byte) (*Reader, error) {
	data := make([]byte, size)
	if _, err := r.ReadAt(data, 0); err != nil {
		return nil, err
	}
	gh, err := ReadGlobalHeader(bytes.NewReader(data[:GlobalHeaderSize]))
	if err != nil {
		return nil, err
	}
	das := int(gh.DataAreaSize)
	if GlobalHeaderSize+das > len(data) {
		return nil, fmt.Errorf("sfx: truncated embedded archive")
	}
	payload := data[GlobalHeaderSize : GlobalHeaderSize+das]

	cdStart := int(gh.CentralDirOffset)
	if cdStart+8 > len(data) {
		return nil, fmt.Errorf("sfx: truncated central directory")
	}
	cd := bytes.NewReader(data[cdStart:])
	var entryCount uint64
	binary.Read(cd, binary.LittleEndian, &entryCount)
	if entryCount > 10000000 {
		return nil, fmt.Errorf("corrupt archive: entry count %d", entryCount)
	}
	entries := make([]DirEntry, 0, entryCount)
	for i := uint64(0); i < entryCount; i++ {
		e, err := ReadDirEntry(cd)
		if err != nil {
			break
		}
		entries = append(entries, *e)
	}

	fecLenPos := int64(gh.CentralDirOffset) + int64(gh.CentralDirSize)
	if fecLenPos+4 > size {
		return nil, fmt.Errorf("sfx: missing recovery section")
	}
	var fecDataLen uint32
	binary.Read(bytes.NewReader(data[fecLenPos:]), binary.LittleEndian, &fecDataLen)
	fecOffset := fecLenPos + 4

	reader := &Reader{Header: gh, Entries: entries, data: payload, FecOffset: fecOffset, FecLen: int64(fecDataLen)}
	if len(password) > 0 {
		reader.Password = password[0]
	}
	return reader, nil
}
