package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

// AugmentResult reports how much extra repair data was written.
type AugmentResult struct {
	ExtraBytes int
	OldPercent int
	NewPercent int
}

// Augment increases repair data for an archive.
//
// extraPercent is added to the existing FEC percentage (or sets the initial
// percentage when the archive was created with -fec 0). Supports Leopard-RS,
// Hybrid, RaptorQ, and LDPC payloads.
func Augment(path, outputPath string, extraPercent int) (*AugmentResult, error) {
	if extraPercent <= 0 {
		return nil, fmt.Errorf("augment: extraPercent must be > 0")
	}
	r, err := Open(path)
	if err != nil {
		return nil, err
	}
	if r.FecLen > 0 && len(r.fecData) == 0 {
		ff, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		r.fecData = make([]byte, r.FecLen)
		_, _ = ff.ReadAt(r.fecData, r.FecOffset)
		ff.Close()
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	solid := r.Header.Flags&FlagSolidCompress != 0
	res := &AugmentResult{}
	var newFEC bytes.Buffer
	var newHashTables [][]uint32
	fecOff := 0

	if solid {
		e := firstFileEntry(r.Entries)
		if e == nil {
			return nil, fmt.Errorf("augment: no file entries")
		}
		compData, ch, err := compressedPayloadAt(r, 0)
		if err != nil {
			return nil, err
		}
		if err := augmentOne(compData, ch, e, 0, r, extraPercent, solid, res, &newFEC, &newHashTables, &fecOff); err != nil {
			return nil, err
		}
		copy(raw[int(GlobalHeaderSize):int(GlobalHeaderSize)+ChunkHeaderSize], r.data[:ChunkHeaderSize])
		for i := range r.Entries {
			r.Entries[i].FECType = e.FECType
			r.Entries[i].FECParams = e.FECParams
		}
	} else {
		for i := range r.Entries {
			if r.Entries[i].EntryType != EntryFile {
				continue
			}
			e := &r.Entries[i]
			compData, ch, err := compressedPayloadAt(r, e.FirstDataOff)
			if err != nil {
				return nil, fmt.Errorf("augment %q: %w", e.Path, err)
			}
			if err := augmentOne(compData, ch, e, e.FirstDataOff, r, extraPercent, solid, res, &newFEC, &newHashTables, &fecOff); err != nil {
				return nil, fmt.Errorf("augment %q: %w", e.Path, err)
			}
			off := int(GlobalHeaderSize) + int(e.FirstDataOff)
			copy(raw[off:off+ChunkHeaderSize], r.data[e.FirstDataOff:e.FirstDataOff+ChunkHeaderSize])
		}
	}

	if newFEC.Len() == 0 {
		return nil, fmt.Errorf("augment: no file chunks found to extend")
	}

	dirStart := int(r.Header.CentralDirOffset)
	dirEnd := dirStart + int(r.Header.CentralDirSize)
	var dirBuf bytes.Buffer
	binary.Write(&dirBuf, binary.LittleEndian, uint64(len(r.Entries)))
	for i := range r.Entries {
		WriteDirEntry(&dirBuf, &r.Entries[i])
	}
	dirBytes := dirBuf.Bytes()
	if len(dirBytes) > dirEnd-dirStart {
		return nil, fmt.Errorf("augment: central directory grew; recreate archive instead")
	}
	paddedDir := make([]byte, dirEnd-dirStart)
	copy(paddedDir, dirBytes)
	copy(raw[dirStart:dirEnd], paddedDir)

	fecLenPos := dirEnd
	out := bytes.NewBuffer(raw[:fecLenPos])
	binary.Write(out, binary.LittleEndian, uint32(newFEC.Len()))
	out.Write(newFEC.Bytes())

	var totalHashes uint32
	for _, ht := range newHashTables {
		totalHashes += uint32(len(ht))
	}
	binary.Write(out, binary.LittleEndian, totalHashes)
	for _, ht := range newHashTables {
		for _, h := range ht {
			binary.Write(out, binary.LittleEndian, h)
		}
	}

	if newFEC.Len() > 0 {
		meta := buildGlobalMetaFromDir(dirBytes, newHashTables)
		if g := encodeGlobalMetaFEC(meta); len(g) > 0 {
			binary.Write(out, binary.LittleEndian, uint32(len(g)))
			out.Write(g)
		}
	}

	target := outputPath
	if target == "" {
		target = path
	}
	if err := os.WriteFile(target, out.Bytes(), 0644); err != nil {
		return nil, err
	}
	return res, nil
}

func augmentOne(compData []byte, ch *ChunkHeader, e *DirEntry, dataOff uint64, r *Reader, extraPercent int, solid bool, res *AugmentResult, newFEC *bytes.Buffer, newHashTables *[][]uint32, fecOff *int) error {
	oldPlan := planFromParams(e.FECParams, e.FECType)
	if oldPlan.Type == 0 && e.FECType == 0 {
		oldPlan.Type = DefaultFECType
	}
	oldPercent := oldPlan.Percent
	newPercent := oldPercent + extraPercent
	if oldPercent == 0 {
		newPercent = extraPercent
	}
	if res.OldPercent == 0 {
		res.OldPercent = oldPercent
	}
	res.NewPercent = newPercent

	oldSize := fecSizeForPlan(oldPlan, len(compData))
	if *fecOff+oldSize > len(r.fecData) {
		oldSize = len(r.fecData) - *fecOff
		if oldSize < 0 {
			oldSize = 0
		}
	}
	*fecOff += oldSize

	fecType := oldPlan.Type
	if fecType == 0 {
		fecType = DefaultFECType
	}
	newPlan := planAugment(len(compData), oldPlan, newPercent, fecType, solid)
	newBytes, hashes := encodeFECPayload(compData, newPlan)
	if len(newBytes) == 0 {
		return fmt.Errorf("FEC encode produced no data")
	}
	res.ExtraBytes += len(newBytes) - oldSize
	newFEC.Write(newBytes)
	*newHashTables = append(*newHashTables, hashes)
	e.FECType = newPlan.Type
	e.FECParams = newPlan.toParams()
	return patchChunkHeaderInData(r.data, dataOff, ch, newPlan)
}

func compressedPayloadAt(r *Reader, dataOff uint64) ([]byte, *ChunkHeader, error) {
	if dataOff+ChunkHeaderSize > uint64(len(r.data)) {
		return nil, nil, fmt.Errorf("chunk header out of range")
	}
	ch, err := ReadChunkHeader(bytes.NewReader(r.data[dataOff:]))
	if err != nil {
		return nil, nil, err
	}
	compStart := dataOff + ChunkHeaderSize
	compEnd := compStart + ch.CompressedSize
	if compEnd > uint64(len(r.data)) {
		return nil, nil, fmt.Errorf("truncated payload")
	}
	return append([]byte(nil), r.data[compStart:compEnd]...), ch, nil
}

func compressedPayload(r *Reader, e *DirEntry) ([]byte, *ChunkHeader, error) {
	return compressedPayloadAt(r, e.FirstDataOff)
}

func planAugment(dataLen int, old fecPlan, newPercent int, fecType uint8, solid bool) fecPlan {
	if old.Percent == 0 {
		return planFEC(dataLen, newPercent, fecType, solid)
	}
	if old.Type == FECRS {
		p, err := planLeopard(dataLen, newPercent)
		if err == nil {
			if old.SymbolSize > 0 {
				p.SymbolSize = old.SymbolSize
				p.DataShards = old.DataShards
				p.ParityShards = old.DataShards * newPercent / 100
				if p.ParityShards < 1 {
					p.ParityShards = 1
				}
				p.K = p.DataShards
			}
			return p
		}
	}
	return planFEC(dataLen, newPercent, fecType, solid)
}

func encodeFECPayload(compData []byte, plan fecPlan) ([]byte, []uint32) {
	if plan.Type == FECRS {
		fec, hashes, err := encodeLeopard(compData, plan)
		if err != nil {
			return nil, nil
		}
		return fec, hashes
	}
	fec, hashes := encodeWithPlan(compData, plan)
	return fec, hashes
}

func fecSizeForPlan(plan fecPlan, dataLen int) int {
	if plan.Percent <= 0 {
		return 0
	}
	if plan.Type == FECRS {
		if plan.ParityShards > 0 && plan.SymbolSize > 0 {
			return plan.ParityShards * plan.SymbolSize
		}
		p, err := planLeopard(dataLen, plan.Percent)
		if err != nil {
			return 0
		}
		return p.ParityShards * p.SymbolSize
	}
	blockSize := plan.blockSize()
	if blockSize <= 0 {
		return plan.repairPerBlock() * plan.SymbolSize
	}
	blocks := (dataLen + blockSize - 1) / blockSize
	return blocks * plan.repairPerBlock() * plan.SymbolSize
}

func patchChunkHeaderInData(data []byte, dataOff uint64, ch *ChunkHeader, plan fecPlan) error {
	if dataOff+ChunkHeaderSize > uint64(len(data)) {
		return fmt.Errorf("chunk header patch out of range")
	}
	ch.RepairCount = uint32(plan.repairPerBlock())
	ch.SymbolSize = uint32(plan.SymbolSize)
	var buf bytes.Buffer
	ch.Write(&buf)
	copy(data[dataOff:dataOff+ChunkHeaderSize], buf.Bytes())
	return nil
}

func firstFileEntry(entries []DirEntry) *DirEntry {
	for i := range entries {
		if entries[i].EntryType == EntryFile {
			return &entries[i]
		}
	}
	return nil
}

func buildGlobalMetaFromDir(dirBytes []byte, hashTables [][]uint32) []byte {
	var meta bytes.Buffer
	meta.Write(dirBytes)
	var total uint32
	for _, ht := range hashTables {
		total += uint32(len(ht))
	}
	binary.Write(&meta, binary.LittleEndian, total)
	for _, ht := range hashTables {
		for _, h := range ht {
			binary.Write(&meta, binary.LittleEndian, h)
		}
	}
	return meta.Bytes()
}
