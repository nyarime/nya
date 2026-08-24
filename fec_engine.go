package nya

import (
	"bytes"
	"encoding/binary"

	"github.com/nyarime/gofec/v2/ldpc"
	"github.com/nyarime/gofec/v2/raptorq"
)

// FECHybrid combines LDPC block parity with RaptorQ fountain repair.
// LDPC handles burst erasures; RaptorQ handles random corruption once
// BLAKE3 symbol hashes identify survivors.
// (constant also declared in format.go for on-disk FECType values)

const (
	fecKMin         = 8
	fecKMax         = 256
	fecSymbolMin    = 512
	fecSymbolDefault = 1024
	fecBlockTarget  = 512 * 1024
	fecMinPayload   = 1024
	fecLDPCDensity  = 0.30
)

// fecPlan holds the parameters used for one payload FEC pass.
type fecPlan struct {
	K            int
	SymbolSize   int
	LDPCParity   int
	RQRepair     int
	Type         uint8
	Percent      int
	DataShards   int // Leopard-RS
	ParityShards int // Leopard-RS
}

func (p fecPlan) blockSize() int {
	if p.Type == FECRS {
		return p.DataShards * p.SymbolSize
	}
	return p.K * p.SymbolSize
}
func (p fecPlan) repairPerBlock() int {
	if p.Type == FECRS {
		return p.ParityShards
	}
	return p.LDPCParity + p.RQRepair
}

func (p fecPlan) toParams() FECParams {
	if p.Type == FECRS {
		return FECParams{
			Param1:   uint32(p.DataShards),
			Param2:   uint32(p.SymbolSize),
			Param3:   uint32(p.Percent),
			Reserved: uint32(p.ParityShards) << 16,
		}
	}
	return FECParams{
		Param1: uint32(p.K),
		Param2: uint32(p.SymbolSize),
		Param3: uint32(p.Percent),
		Reserved: uint32(p.LDPCParity)<<16 | uint32(p.RQRepair),
	}
}

func planFromParams(p FECParams, fecType uint8) fecPlan {
	if fecType == FECRS {
		return planFromParamsLeopard(p)
	}
	plan := fecPlan{
		K:          int(p.Param1),
		SymbolSize: int(p.Param2),
		Percent:    int(p.Param3),
		Type:       fecType,
	}
	if plan.K <= 0 {
		plan.K = 32
	}
	if plan.SymbolSize <= 0 {
		plan.SymbolSize = fecSymbolDefault
	}
	if p.Reserved != 0 {
		plan.LDPCParity = int(p.Reserved >> 16)
		plan.RQRepair = int(p.Reserved & 0xffff)
	}
	return plan
}

func chooseSymbolSize(dataLen int) int {
	switch {
	case dataLen >= 4*1024*1024:
		return 4096
	case dataLen >= 512*1024:
		return 2048
	case dataLen >= 64*1024:
		return 1024
	default:
		return fecSymbolMin
	}
}

func chooseK(dataLen, symbolSize int) int {
	if dataLen <= 0 {
		return fecKMin
	}
	k := fecBlockTarget / symbolSize
	if k < fecKMin {
		k = fecKMin
	}
	if k > fecKMax {
		k = fecKMax
	}
	// Small payloads: one block, sized to the data.
	if dataLen < k*symbolSize {
		k = (dataLen + symbolSize - 1) / symbolSize
		if k < fecKMin {
			k = fecKMin
		}
	}
	return k
}

func planFEC(dataLen, percent int, fecType uint8, solid bool) fecPlan {
	if dataLen >= leopardMinPayload && (fecType == FECRS || fecType == FECHybrid || solid) {
		if plan, err := planLeopard(dataLen, percent); err == nil {
			return plan
		}
	}
	if fecType == 0 {
		fecType = FECHybrid
	}
	sym := chooseSymbolSize(dataLen)
	k := chooseK(dataLen, sym)

	plan := fecPlan{
		K:          k,
		SymbolSize: sym,
		Type:       fecType,
		Percent:    percent,
	}

	switch fecType {
	case FECLDPC:
		plan.LDPCParity = k * percent / 100
		if plan.LDPCParity < 1 {
			plan.LDPCParity = 1
		}
	case FECRaptorQ:
		plan.RQRepair = k * percent / 100
		if plan.RQRepair < 1 {
			plan.RQRepair = 1
		}
	default: // FECHybrid
		half := k * percent / 200
		if half < 1 {
			half = 1
		}
		plan.LDPCParity = half
		plan.RQRepair = half
	}
	return plan
}

func encodeWithPlan(data []byte, plan fecPlan) (fec []byte, hashes []uint32) {
	if len(data) == 0 || plan.repairPerBlock() == 0 {
		return nil, nil
	}
	if plan.Type == FECRS {
		fec, hashes, err := encodeLeopard(data, plan)
		if err != nil {
			return nil, nil
		}
		return fec, hashes
	}

	blockSize := plan.blockSize()
	var out bytes.Buffer

	for off := 0; off < len(data); off += blockSize {
		end := off + blockSize
		if end > len(data) {
			end = len(data)
		}
		padded := make([]byte, blockSize)
		copy(padded, data[off:end])

		blockFEC, blockHashes := encodeBlock(padded, plan)
		out.Write(blockFEC)
		hashes = append(hashes, blockHashes...)
	}
	return out.Bytes(), hashes
}

func encodeBlock(padded []byte, plan fecPlan) (fec []byte, hashes []uint32) {
	k, t := plan.K, plan.SymbolSize

	for i := 0; i < k; i++ {
		hashes = append(hashes, blake3Short(padded[i*t:(i+1)*t]))
	}

	var out bytes.Buffer

	if plan.LDPCParity > 0 {
		shards := make([][]byte, k)
		for i := 0; i < k; i++ {
			shards[i] = padded[i*t : (i+1)*t]
		}
		codec := ldpc.New(k, plan.LDPCParity, fecLDPCDensity)
		encoded := codec.Encode(shards)
		for j := k; j < len(encoded); j++ {
			out.Write(encoded[j])
		}
	}

	if plan.RQRepair > 0 {
		codec := raptorq.New(k, t)
		symbols := codec.Encode(padded, plan.RQRepair)
		for j := k; j < len(symbols); j++ {
			out.Write(symbols[j].Data)
		}
	}

	return out.Bytes(), hashes
}

func repairWithPlan(data, fecData []byte, plan fecPlan, hashes []uint32) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	if plan.Type == FECRS {
		return repairLeopard(data, fecData, plan, hashes)
	}
	blockSize := plan.blockSize()
	numBlocks := (len(data) + blockSize - 1) / blockSize
	repairPerBlock := plan.repairPerBlock()
	if repairPerBlock == 0 {
		return data, nil
	}

	repaired := make([]byte, len(data))
	copy(repaired, data)

	fecOff := 0
	for block := 0; block < numBlocks; block++ {
		blockEnd := block*blockSize + blockSize
		if blockEnd > len(data) {
			blockEnd = len(data)
		}
		blockLen := blockEnd - block*blockSize

		blockFEC := fecData[fecOff:]
		if len(blockFEC) > repairPerBlock*plan.SymbolSize {
			blockFEC = blockFEC[:repairPerBlock*plan.SymbolSize]
		}
		fecOff += repairPerBlock * plan.SymbolSize

		blockHashes := blockHashSlice(hashes, plan.K, block)
		decoded, err := repairBlock(
			repaired[block*blockSize:blockEnd],
			blockFEC,
			plan,
			blockHashes,
			blockLen,
		)
		if err != nil {
			continue
		}
		copy(repaired[block*blockSize:blockEnd], decoded)
	}
	return repaired, nil
}

func blockHashSlice(hashes []uint32, k, block int) []uint32 {
	start := block * k
	end := start + k
	if start >= len(hashes) {
		return nil
	}
	if end > len(hashes) {
		end = len(hashes)
	}
	return hashes[start:end]
}

func repairBlock(data, blockFEC []byte, plan fecPlan, hashes []uint32, dataLen int) ([]byte, error) {
	k, t := plan.K, plan.SymbolSize
	blockSize := k * t

	padded := make([]byte, blockSize)
	copy(padded, data)

	good := identifyGoodSymbols(padded, k, t, hashes)

	if plan.LDPCParity > 0 && countMissing(good, k) > 0 {
		if repaired, ok := ldpcRecoverBlock(padded, blockFEC, plan, good); ok {
			padded = repaired
			good = identifyGoodSymbols(padded, k, t, hashes)
		}
	}

	if countMissing(good, k) == 0 {
		return padded[:dataLen], nil
	}

	if plan.RQRepair > 0 {
		fecOff := plan.LDPCParity * t
		rqFEC := blockFEC[fecOff:]
		if out, err := raptorqRecoverBlock(padded, rqFEC, plan, good); err == nil {
			padded = out
			good = identifyGoodSymbols(padded, k, t, hashes)
		}
	}

	if countMissing(good, k) > 0 {
		return nil, ErrCorrupted
	}
	return padded[:dataLen], nil
}

func identifyGoodSymbols(padded []byte, k, t int, hashes []uint32) []bool {
	good := make([]bool, k)
	if len(hashes) == 0 {
		return good
	}
	for sym := 0; sym < k && sym < len(hashes); sym++ {
		if hashes[sym] == 0 {
			continue
		}
		symData := padded[sym*t : (sym+1)*t]
		if blake3Short(symData) == hashes[sym] {
			good[sym] = true
		}
	}
	return good
}

func countMissing(good []bool, k int) int {
	n := 0
	for i := 0; i < k; i++ {
		if i >= len(good) || !good[i] {
			n++
		}
	}
	return n
}

func ldpcRecoverBlock(padded, blockFEC []byte, plan fecPlan, good []bool) ([]byte, bool) {
	k, t := plan.K, plan.SymbolSize
	total := k + plan.LDPCParity
	shards := make([][]byte, total)
	for i := 0; i < k; i++ {
		if i < len(good) && good[i] {
			shards[i] = append([]byte(nil), padded[i*t:(i+1)*t]...)
		}
	}
	fecOff := 0
	for i := 0; i < plan.LDPCParity; i++ {
		if fecOff+t > len(blockFEC) {
			return nil, false
		}
		shards[k+i] = append([]byte(nil), blockFEC[fecOff:fecOff+t]...)
		fecOff += t
	}

	codec := ldpc.New(k, plan.LDPCParity, fecLDPCDensity)
	if err := codec.Decode(shards); err != nil {
		return nil, false
	}
	out := make([]byte, k*t)
	for i := 0; i < k; i++ {
		if shards[i] == nil {
			return nil, false
		}
		copy(out[i*t:(i+1)*t], shards[i])
	}
	return out, true
}

func raptorqRecoverBlock(padded, rqFEC []byte, plan fecPlan, good []bool) ([]byte, error) {
	k, t := plan.K, plan.SymbolSize

	var damagedESIs []int
	for sym := 0; sym < k; sym++ {
		if sym >= len(good) || !good[sym] {
			damagedESIs = append(damagedESIs, sym)
		}
	}
	if len(damagedESIs) == 0 {
		return padded, nil
	}
	if len(damagedESIs) > plan.RQRepair {
		return nil, ErrCorrupted
	}

	allSource := make([]raptorq.Symbol, k)
	for sym := 0; sym < k; sym++ {
		symData := append([]byte(nil), padded[sym*t:(sym+1)*t]...)
		allSource[sym] = raptorq.Symbol{ESI: uint32(sym), Data: symData}
	}

	repairSyms := make([]raptorq.Symbol, 0, plan.RQRepair)
	for j := 0; j < plan.RQRepair; j++ {
		off := j * t
		if off+t > len(rqFEC) {
			break
		}
		repairSyms = append(repairSyms, raptorq.Symbol{
			ESI:  uint32(k + j),
			Data: append([]byte(nil), rqFEC[off:off+t]...),
		})
	}
	received := append(allSource, repairSyms...)

	codec := raptorq.New(k, t)
	return codec.DecodeWithErasures(received, k*t, damagedESIs)
}

// --- global metadata FEC (Central Directory + hash table) ---

type globalMetaHeader struct {
	OrigLen    uint32
	K          uint32
	LDPCParity uint32
	SymbolSize uint32
}

func encodeGlobalMetaFEC(meta []byte) []byte {
	if len(meta) < 64 {
		return nil
	}
	plan := planFEC(len(meta), 50, FECLDPC, false)
	fec, _ := encodeWithPlan(meta, plan)
	if len(fec) == 0 {
		return nil
	}
	hdr := globalMetaHeader{
		OrigLen:    uint32(len(meta)),
		K:          uint32(plan.K),
		LDPCParity: uint32(plan.LDPCParity),
		SymbolSize: uint32(plan.SymbolSize),
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, hdr)
	out.Write(fec)
	return out.Bytes()
}

func decodeGlobalMetaFEC(damaged, wrapped []byte) ([]byte, error) {
	if len(wrapped) < 16 {
		return nil, ErrCorrupted
	}
	var hdr globalMetaHeader
	if err := binary.Read(bytes.NewReader(wrapped[:16]), binary.LittleEndian, &hdr); err != nil {
		return nil, err
	}
	plan := fecPlan{
		K:          int(hdr.K),
		SymbolSize: int(hdr.SymbolSize),
		LDPCParity: int(hdr.LDPCParity),
		Type:       FECLDPC,
	}
	fecData := wrapped[16:]
	src := damaged
	if len(src) == 0 {
		src = make([]byte, hdr.OrigLen)
	}
	return repairWithPlan(src, fecData, plan, nil)
}
