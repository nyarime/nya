package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/nyarime/gofec/v2/raptorq"
)

// AugmentResult reports how much extra fountain parity was appended.
type AugmentResult struct {
	ExtraSymbols int
	ExtraBytes   int
}

// Augment appends additional RaptorQ repair symbols to an archive (fountain extension).
// extraPercent is relative to the compressed payload size per chunk. Leopard (FECRS)
// archives cannot be augmented in place; use a higher -fec at create time instead.
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

	res := &AugmentResult{}
	var extraFEC bytes.Buffer
	extraFEC.Write(r.fecData)

	for _, e := range r.Entries {
		if e.EntryType != EntryFile {
			continue
		}
		if e.FECType == FECRS {
			return nil, fmt.Errorf("augment: %q uses Leopard-RS; recreate with higher -fec instead", e.Path)
		}
		plan := planFromParams(e.FECParams, e.FECType)
		if plan.RQRepair == 0 && plan.Type != FECRaptorQ && plan.Type != FECHybrid {
			continue
		}

		off := e.FirstDataOff
		if off+ChunkHeaderSize > uint64(len(r.data)) {
			continue
		}
		ch, err := ReadChunkHeader(bytes.NewReader(r.data[off:]))
		if err != nil {
			continue
		}
		compStart := off + ChunkHeaderSize
		compEnd := compStart + ch.CompressedSize
		if compEnd > uint64(len(r.data)) {
			continue
		}
		compData := r.data[compStart:compEnd]

		addRepair := plan.K * extraPercent / 100
		if addRepair < 1 {
			addRepair = 1
		}
		startESI := uint32(plan.K + plan.RQRepair)
		codec := raptorq.New(plan.K, plan.SymbolSize)
		symbols := codec.GenerateRepairFromData(compData, startESI, addRepair)
		for _, s := range symbols {
			extraFEC.Write(s.Data)
			res.ExtraBytes += len(s.Data)
		}
		res.ExtraSymbols += addRepair

		plan.RQRepair += addRepair
		e.FECParams = plan.toParams()
		ch.RepairCount = uint32(plan.repairPerBlock())
	}

	if res.ExtraSymbols == 0 {
		return nil, fmt.Errorf("augment: no RaptorQ chunks found to extend")
	}

	out := outputPath
	if out == "" {
		out = path
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Rewrite central directory with updated FEC params.
	dirStart := int(r.Header.CentralDirOffset)
	dirEnd := dirStart + int(r.Header.CentralDirSize)
	var dirBuf bytes.Buffer
	binary.Write(&dirBuf, binary.LittleEndian, uint64(len(r.Entries)))
	for i := range r.Entries {
		WriteDirEntry(&dirBuf, &r.Entries[i])
	}
	copy(raw[dirStart:dirEnd], dirBuf.Bytes())

	fecLenPos := dirEnd
	binary.LittleEndian.PutUint32(raw[fecLenPos:], uint32(extraFEC.Len()))
	copy(raw[fecLenPos+4:], extraFEC.Bytes())

	if err := os.WriteFile(out, raw, 0644); err != nil {
		return nil, err
	}
	return res, nil
}
