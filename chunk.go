package nya

import "bytes"

const (
	// Multi-chunk thresholds (SPEC-MULTICHUNK.md).
	multiChunkThreshold      = 4 * 1024 * 1024  // single chunk at or below
	multiChunkSizeDefault    = 4 * 1024 * 1024  // 4–64 MiB files
	multiChunkSizeLarge      = 8 * 1024 * 1024  // > 64 MiB files
	multiChunkLargeThreshold = 64 * 1024 * 1024

	VersionMinorMultiChunk uint16 = 3
)

// splitRawChunkSizes divides a file into raw chunk sizes for non-solid entries.
// Solid archives always use one logical stream (ChunkCount=1 on the solid chunk).
func splitRawChunkSizes(total int, customSize int, enable bool) []int {
	if total <= 0 {
		return nil
	}
	if !enable || total <= multiChunkThreshold {
		return []int{total}
	}
	chunkSize := multiChunkSizeDefault
	if total > multiChunkLargeThreshold {
		chunkSize = multiChunkSizeLarge
	}
	if customSize > 0 {
		chunkSize = customSize
	}
	var sizes []int
	for remaining := total; remaining > 0; {
		n := chunkSize
		if n > remaining {
			n = remaining
		}
		sizes = append(sizes, n)
		remaining -= n
	}
	return sizes
}

// fecPayloadByteLen returns the FEC blob size for a compressed chunk payload.
func fecPayloadByteLen(compLen int, plan fecPlan) int {
	if compLen < fecMinPayload || plan.repairPerBlock() == 0 {
		return 0
	}
	if plan.Type == FECRS {
		return plan.ParityShards * plan.SymbolSize
	}
	blockSize := plan.blockSize()
	numBlocks := (compLen + blockSize - 1) / blockSize
	if numBlocks < 1 {
		numBlocks = 1
	}
	return numBlocks * plan.repairPerBlock() * plan.SymbolSize
}

// fecHashCount returns the number of symbol hashes for a compressed chunk.
func fecHashCount(compLen int, plan fecPlan) int {
	if compLen < fecMinPayload || plan.repairPerBlock() == 0 {
		return 0
	}
	if plan.Type == FECRS {
		return plan.DataShards
	}
	blockSize := plan.blockSize()
	numBlocks := (compLen + blockSize - 1) / blockSize
	if numBlocks < 1 {
		numBlocks = 1
	}
	return numBlocks * plan.K
}

// chunkDataStride is bytes occupied in the data area per on-disk chunk.
func chunkDataStride(ch *ChunkHeader) uint64 {
	return ChunkHeaderSize + ch.CompressedSize
}

// fileChunkRef locates one on-disk chunk for extract/repair.
type fileChunkRef struct {
	entry    *DirEntry
	chunkIdx uint32
	dataOff  uint64
	header   ChunkHeader
	fecOff   int
	fecLen   int
	hashOff  int
	hashLen  int
}

// buildFileChunkRefs walks file entries in archive order and maps FEC/hash slices.
func (r *Reader) buildFileChunkRefs() []fileChunkRef {
	var refs []fileChunkRef
	fecCursor := 0
	hashCursor := 0
	allHashes := r.allHashWords()

	for i := range r.Entries {
		e := &r.Entries[i]
		if e.EntryType != EntryFile {
			continue
		}
		off := e.FirstDataOff
		for c := uint32(0); c < e.ChunkCount; c++ {
			if off+ChunkHeaderSize > uint64(len(r.data)) {
				break
			}
			ch, err := ReadChunkHeader(bytes.NewReader(r.data[off:]))
			if err != nil {
				break
			}
			compLen := int(ch.CompressedSize)
			percent := int(e.FECParams.Param3)
			if percent <= 0 {
				percent = 10
			}
			plan := planFEC(compLen, percent, e.FECType, false)
			fLen := fecPayloadByteLen(compLen, plan)
			hLen := fecHashCount(compLen, plan)

			ref := fileChunkRef{
				entry: e, chunkIdx: c, dataOff: off, header: *ch,
				fecOff: fecCursor, fecLen: fLen,
				hashOff: hashCursor, hashLen: hLen,
			}
			refs = append(refs, ref)
			fecCursor += fLen
			hashCursor += hLen
			_ = allHashes
			off += chunkDataStride(ch)
		}
	}
	return refs
}

func chunkHeaderPlan(ch *ChunkHeader, fecType uint8) fecPlan {
	p := planFromParams(FECParams{
		Param1: ch.RepairCount,
		Param2: ch.SymbolSize,
		Param3: 0,
	}, fecType)
	if p.K <= 0 {
		p.K = int(ch.RepairCount)
	}
	if p.SymbolSize <= 0 {
		p.SymbolSize = int(ch.SymbolSize)
	}
	return p
}

func (r *Reader) allHashWords() []uint32 {
	if len(r.HashTables) == 0 {
		return nil
	}
	return r.HashTables[0]
}
