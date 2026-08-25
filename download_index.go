package nya

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Tail / download-index constants (SPEC-EXTENSIONS.md).
const (
	TailTypeDownloadIndex uint32 = 0x0001

	DownloadIndexSchemaVersion = 1

	// DownloadIndexFooterSize is a fixed trailer at EOF so clients can locate
	// the embedded index with one Range read (works even when header Reserved
	// holds Argon2id KDF params that overlap the TailChainOffset slots).
	DownloadIndexFooterSize = 40
)

// DownloadIndexFooterMagic is ASCII "NYADIDX1".
var DownloadIndexFooterMagic = [8]byte{'N', 'Y', 'A', 'D', 'I', 'D', 'X', '1'}

// DownloadIndexFooter sits at EOF when FlagHasDownloadIndex is set.
type DownloadIndexFooter struct {
	Magic           [8]byte
	TailChainOffset uint64
	TailChainSize   uint64
	Flags           uint32
	Reserved        uint32
}

func (f *DownloadIndexFooter) Marshal() []byte {
	buf := make([]byte, DownloadIndexFooterSize)
	copy(buf[0:8], f.Magic[:])
	binary.LittleEndian.PutUint64(buf[8:16], f.TailChainOffset)
	binary.LittleEndian.PutUint64(buf[16:24], f.TailChainSize)
	binary.LittleEndian.PutUint32(buf[24:28], f.Flags)
	binary.LittleEndian.PutUint32(buf[28:32], f.Reserved)
	return buf
}

func ParseDownloadIndexFooter(b []byte) (*DownloadIndexFooter, error) {
	if len(b) < DownloadIndexFooterSize {
		return nil, fmt.Errorf("download index footer: short buffer")
	}
	var f DownloadIndexFooter
	copy(f.Magic[:], b[0:8])
	if f.Magic != DownloadIndexFooterMagic {
		return nil, fmt.Errorf("download index footer: bad magic")
	}
	f.TailChainOffset = binary.LittleEndian.Uint64(b[8:16])
	f.TailChainSize = binary.LittleEndian.Uint64(b[16:24])
	f.Flags = binary.LittleEndian.Uint32(b[24:28])
	f.Reserved = binary.LittleEndian.Uint32(b[28:32])
	return &f, nil
}

// EncodeDownloadIndexPayload builds the type 0x0001 payload (no typeId/len wrapper).
// archiveBlake3 may be zero; callers often patch it after the file is finalized.
func EncodeDownloadIndexPayload(blockSize int64, blocks []DownloadBlock, archiveBlake3 [32]byte) ([]byte, error) {
	if blockSize <= 0 || blockSize > 0xffffffff {
		return nil, fmt.Errorf("download index: invalid block size")
	}
	if len(blocks) > 0xffffffff {
		return nil, fmt.Errorf("download index: too many blocks")
	}
	// schema + blockSize + blockCount + n*(offset+size+hash) + archiveHash
	n := len(blocks)
	size := 1 + 4 + 4 + n*(8+4+32) + 32
	buf := make([]byte, size)
	buf[0] = DownloadIndexSchemaVersion
	binary.LittleEndian.PutUint32(buf[1:5], uint32(blockSize))
	binary.LittleEndian.PutUint32(buf[5:9], uint32(n))
	off := 9
	for _, b := range blocks {
		if b.Size > 0xffffffff {
			return nil, fmt.Errorf("download index: block %d too large", b.ID)
		}
		sum, err := hex.DecodeString(b.Blake3)
		if err != nil || len(sum) != 32 {
			return nil, fmt.Errorf("download index: block %d blake3", b.ID)
		}
		binary.LittleEndian.PutUint64(buf[off:off+8], uint64(b.Offset))
		binary.LittleEndian.PutUint32(buf[off+8:off+12], uint32(b.Size))
		copy(buf[off+12:off+44], sum)
		off += 44
	}
	copy(buf[off:off+32], archiveBlake3[:])
	return buf, nil
}

// DecodeDownloadIndexPayload parses a type 0x0001 payload into transport blocks.
func DecodeDownloadIndexPayload(payload []byte) (blockSize int64, blocks []DownloadBlock, archiveBlake3 [32]byte, err error) {
	if len(payload) < 1+4+4+32 {
		return 0, nil, archiveBlake3, fmt.Errorf("download index: payload too short")
	}
	if payload[0] != DownloadIndexSchemaVersion {
		return 0, nil, archiveBlake3, fmt.Errorf("download index: unsupported schema %d", payload[0])
	}
	blockSize = int64(binary.LittleEndian.Uint32(payload[1:5]))
	count := binary.LittleEndian.Uint32(payload[5:9])
	need := 9 + int(count)*44 + 32
	if len(payload) < need {
		return 0, nil, archiveBlake3, fmt.Errorf("download index: truncated payload")
	}
	off := 9
	blocks = make([]DownloadBlock, 0, count)
	for i := uint32(0); i < count; i++ {
		bo := int64(binary.LittleEndian.Uint64(payload[off : off+8]))
		bs := int64(binary.LittleEndian.Uint32(payload[off+8 : off+12]))
		var h [32]byte
		copy(h[:], payload[off+12:off+44])
		blocks = append(blocks, DownloadBlock{
			ID:     i,
			Offset: bo,
			Size:   bs,
			Blake3: hex.EncodeToString(h[:]),
		})
		off += 44
	}
	copy(archiveBlake3[:], payload[off:off+32])
	return blockSize, blocks, archiveBlake3, nil
}

// WrapTailRecord prepends typeId + payloadLen.
func WrapTailRecord(typeID uint32, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(out[0:4], typeID)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(payload)))
	copy(out[8:], payload)
	return out
}

// SetTailChainReserved writes TailChainOffset/Size into GlobalHeader.Reserved[0:16]
// when KDF is not using that region. Returns false if FlagKDFArgon2id is set
// (caller should rely on the EOF footer only).
func SetTailChainReserved(h *GlobalHeader, offset, size uint64) bool {
	if h.Flags&FlagKDFArgon2id != 0 {
		return false
	}
	binary.LittleEndian.PutUint64(h.Reserved[0:8], offset)
	binary.LittleEndian.PutUint64(h.Reserved[8:16], size)
	return true
}

// TailChainFromReserved reads TailChainOffset/Size when KDF flag is clear.
func TailChainFromReserved(h *GlobalHeader) (offset, size uint64, ok bool) {
	if h.Flags&FlagKDFArgon2id != 0 {
		return 0, 0, false
	}
	offset = binary.LittleEndian.Uint64(h.Reserved[0:8])
	size = binary.LittleEndian.Uint64(h.Reserved[8:16])
	if offset == 0 || size == 0 {
		return 0, 0, false
	}
	return offset, size, true
}

func writeAt(f *os.File, off int64, p []byte) error {
	_, err := f.WriteAt(p, off)
	return err
}

func readGlobalHeaderAt(f *os.File) (*GlobalHeader, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return ReadGlobalHeader(f)
}
