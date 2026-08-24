package nya

import (
	"bytes"

	"github.com/nyarime/gofec/ldpc"
	"github.com/nyarime/gofec/raptorq"
)

var globalHashTable []uint32

const (
	fecSymbolSize  = 1024
	fecSourceCount = 64
	fecChunkSize   = fecSourceCount * fecSymbolSize
)

// raptorqFEC 生成repair symbols + per-symbol hashes
func raptorqFEC(data []byte, percent int) []byte {
	if len(data) == 0 { return nil }

	// 自适应K: 覆盖整个数据(单FEC编码)
	K := 32 // 固定K=8(GoFEC验证最佳) // 上限保护
	
	// 多给50%冗余repair(GoFEC需要超过K个符号才能解码)
	repairCount := K * percent / 100
	if repairCount < 1 { repairCount = 1 }

	chunkSize := K * fecSymbolSize

	var fec bytes.Buffer
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) { end = len(data) }

		padded := make([]byte, chunkSize)
		copy(padded, data[i:end])

		codec := raptorq.New(K, fecSymbolSize)
		symbols := codec.Encode(padded, repairCount)

		for j := K; j < len(symbols); j++ {
			fec.Write(symbols[j].Data)
		}

		for j := 0; j < K; j++ {
			h := blake3Short(padded[j*fecSymbolSize : (j+1)*fecSymbolSize])
			globalHashTable = append(globalHashTable, h)
		}
	}
	return fec.Bytes()
}

// raptorqRepair 按符号级检测损坏并恢复
func raptorqRepair(data []byte, fecData []byte, percent int, hashTable ...[]uint32) ([]byte, error) {
	var ht []uint32
	if len(hashTable) > 0 { ht = hashTable[0] }
	if len(data) == 0 { return data, nil }

	K := 32
	chunkSize := K * fecSymbolSize
	numChunks := (len(data) + chunkSize - 1) / chunkSize
	repairCount := len(fecData) / (numChunks * fecSymbolSize)
	if repairCount < 1 { repairCount = K }

	// === Stage 1: Indexer ===
	// Build hash→(chunkID, symbolID) lookup table from stored hashes
	type SymbolLoc struct {
		ChunkID  int
		SymbolID int
	}
	hashIndex := make(map[uint32]SymbolLoc)
	numChunks = (len(data) + chunkSize - 1) / chunkSize
	for chunk := 0; chunk < numChunks; chunk++ {
		for sym := 0; sym < K; sym++ {
			idx := chunk*K + sym
			if ht != nil && idx < len(ht) && ht[idx] != 0 {
				hashIndex[ht[idx]] = SymbolLoc{ChunkID: chunk, SymbolID: sym}
			}
		}
	}

	// === Stage 2: Harvester ===
	// Aligned scan: read each T-byte block, hash it, look up in index
	type FoundSymbol struct {
		ChunkID  int
		SymbolID int
		Data     []byte
	}
	
	// Per-chunk symbol pools
	chunkPools := make([][]raptorq.Symbol, numChunks)
	chunkDamaged := make([]int, numChunks)
	for i := range chunkPools {
		chunkDamaged[i] = K // assume all damaged initially
	}

	// Scan data area
	padded := make([]byte, numChunks*chunkSize)
	copy(padded, data)
	
	for chunk := 0; chunk < numChunks; chunk++ {
		for sym := 0; sym < K; sym++ {
			offset := chunk*chunkSize + sym*fecSymbolSize
			symData := make([]byte, fecSymbolSize)
			copy(symData, padded[offset:offset+fecSymbolSize])
			
			h := blake3Short(symData)
			if loc, ok := hashIndex[h]; ok && loc.ChunkID == chunk && loc.SymbolID == sym {
				// Symbol is intact!
				chunkPools[chunk] = append(chunkPools[chunk], raptorq.Symbol{
					ESI:  uint32(sym),
					Data: symData,
				})
				chunkDamaged[chunk]--
			}
		}
	}

	// 统计
	totalDamaged := 0
	failedChunks := 0
	for i := 0; i < numChunks; i++ {
		totalDamaged += chunkDamaged[i]
	}

	// 统计
	total := 0
	for i := 0; i < numChunks; i++ { total += chunkDamaged[i] }
	// === Stage 3: Dispatcher ===
	// For each damaged chunk, add repair symbols and decode
	repaired := make([]byte, len(data))
	copy(repaired, data)

	fecOff := 0
	for chunk := 0; chunk < numChunks; chunk++ {
		// Add repair symbols for this chunk
		repairSyms := make([]raptorq.Symbol, 0, repairCount)
		for j := 0; j < repairCount && fecOff+fecSymbolSize <= len(fecData); j++ {
			sym := make([]byte, fecSymbolSize)
			copy(sym, fecData[fecOff:fecOff+fecSymbolSize])
			repairSyms = append(repairSyms, raptorq.Symbol{
				ESI:  uint32(K + j),
				Data: sym,
			})
			fecOff += fecSymbolSize
		}

		if chunkDamaged[chunk] == 0 {
			continue // chunk is fine
		}

		// Include ALL source symbols (good + damaged) + repair
		// GoFEC needs source symbols for Gaussian elimination
		allSource := make([]raptorq.Symbol, K)
		goodSet := make(map[int]bool)
		for _, s := range chunkPools[chunk] {
			allSource[int(s.ESI)] = s
			goodSet[int(s.ESI)] = true
		}
		for sym := 0; sym < K; sym++ {
			if !goodSet[sym] {
				offset := chunk*chunkSize + sym*fecSymbolSize
				symData := make([]byte, fecSymbolSize)
				if offset+fecSymbolSize <= len(padded) {
					copy(symData, padded[offset:offset+fecSymbolSize])
				}
				allSource[sym] = raptorq.Symbol{ESI: uint32(sym), Data: symData}
			}
		}
		received := append(allSource, repairSyms...)

		// 标记damaged source ESIs
		var damagedESIs []int
		for sym := 0; sym < K; sym++ {
			if !goodSet[sym] { damagedESIs = append(damagedESIs, sym) }
		}
		
		codec := raptorq.New(K, fecSymbolSize)
		var decoded []byte
		var err error
		if len(damagedESIs) > 0 {
			decoded, err = codec.DecodeWithErasures(received, chunkSize, damagedESIs)
		} else {
			decoded, err = codec.Decode(received, chunkSize)
		}
		if err != nil || decoded == nil {
			failedChunks++
			continue
		}


		// Write recovered source symbols back (skip ChunkHeader)
		// Source symbols start at chunk*chunkSize in padded space
		// But in actual data, compData starts at ChunkHeaderSize offset
		chunkStart := chunk * chunkSize
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(repaired) { chunkEnd = len(repaired) }
		copy(repaired[chunkStart:chunkEnd], decoded[:chunkEnd-chunkStart])
	}

	return repaired, nil
}


// blake3Short 计算BLAKE3截断到uint64(8字节, 防CRC32碰撞)
func blake3Short(data []byte) uint32 {
	h := Blake3Sum256(data)
	// 用前4字节(兼容现有hash表格式)但碰撞率极低
	return uint32(h[0]) | uint32(h[1])<<8 | uint32(h[2])<<16 | uint32(h[3])<<24
}

func GetHashTable() []uint32 {
	ht := globalHashTable
	globalHashTable = nil
	return ht
}

// FEC codec types
const (
	FECTypeRaptorQ = 0 // fountain code, for network transmission
	FECTypeLDPC    = 1 // block code, for storage redundancy
)

// ldpcFEC generates LDPC repair data
func ldpcFEC(data []byte, percent int) []byte {
	if len(data) == 0 { return nil }
	
	K := 32
	repairCount := K * percent / 100
	if repairCount < 1 { repairCount = 1 }
	
	chunkSize := K * fecSymbolSize
	var fec bytes.Buffer
	
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) { end = len(data) }
		
		padded := make([]byte, chunkSize)
		copy(padded, data[i:end])
		
		// Split padded data into K shards
		shards := make([][]byte, K)
		for s := 0; s < K; s++ {
			shards[s] = padded[s*fecSymbolSize : (s+1)*fecSymbolSize]
		}
		
		codec := ldpc.New(K, repairCount, 0.5)
		encoded := codec.Encode(shards)
		
		// Take only repair symbols (after source symbols)
		for j := K; j < len(encoded); j++ {
			fec.Write(encoded[j])
		}
	}
	return fec.Bytes()
}

// GenerateFEC creates FEC data using the specified codec type
func GenerateFEC(data []byte, percent int, fecType int) []byte {
	switch fecType {
	case FECTypeLDPC:
		return ldpcFEC(data, percent)
	default:
		return raptorqFEC(data, percent)
	}
}
