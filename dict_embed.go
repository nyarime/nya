package nya

import (
	"encoding/binary"
	"fmt"
)

// Tail type and flag for an embedded zstd dictionary (CompressionID 5).
const (
	TailTypeZstdDictionary uint32 = 0x0006

	ZstdDictionarySchemaVersion = 1
)

// EncodeZstdDictPayload builds the type 0x0006 payload (no typeId/len wrapper).
func EncodeZstdDictPayload(dict []byte) []byte {
	buf := make([]byte, 1+4+len(dict))
	buf[0] = ZstdDictionarySchemaVersion
	binary.LittleEndian.PutUint32(buf[1:5], uint32(len(dict)))
	copy(buf[5:], dict)
	return buf
}

// DecodeZstdDictPayload parses a type 0x0006 payload.
func DecodeZstdDictPayload(payload []byte) ([]byte, error) {
	if len(payload) < 5 {
		return nil, fmt.Errorf("zstd dict tail: short payload")
	}
	if payload[0] != ZstdDictionarySchemaVersion {
		return nil, fmt.Errorf("zstd dict tail: unsupported schema %d", payload[0])
	}
	n := binary.LittleEndian.Uint32(payload[1:5])
	if int(n) != len(payload)-5 {
		return nil, fmt.Errorf("zstd dict tail: length mismatch (%d vs %d)", n, len(payload)-5)
	}
	out := make([]byte, n)
	copy(out, payload[5:])
	return out, nil
}

// TailRecord is one entry in the extension tail chain.
type TailRecord struct {
	TypeID  uint32
	Payload []byte
}

// ParseTailChain decodes consecutive WrapTailRecord blobs.
func ParseTailChain(raw []byte) ([]TailRecord, error) {
	var out []TailRecord
	off := 0
	for off < len(raw) {
		if off+8 > len(raw) {
			return nil, fmt.Errorf("tail chain: truncated header at %d", off)
		}
		typeID := binary.LittleEndian.Uint32(raw[off : off+4])
		plen := int(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
		off += 8
		if plen < 0 || off+plen > len(raw) {
			return nil, fmt.Errorf("tail chain: truncated payload type=0x%x", typeID)
		}
		p := make([]byte, plen)
		copy(p, raw[off:off+plen])
		out = append(out, TailRecord{TypeID: typeID, Payload: p})
		off += plen
	}
	return out, nil
}

// EncodeTailChain concatenates WrapTailRecord encodings.
func EncodeTailChain(recs []TailRecord) []byte {
	var out []byte
	for _, r := range recs {
		out = append(out, WrapTailRecord(r.TypeID, r.Payload)...)
	}
	return out
}

// FindTailPayload returns the first payload with typeID in a raw chain.
func FindTailPayload(raw []byte, typeID uint32) ([]byte, bool) {
	recs, err := ParseTailChain(raw)
	if err != nil {
		return nil, false
	}
	for _, r := range recs {
		if r.TypeID == typeID {
			return r.Payload, true
		}
	}
	return nil, false
}
