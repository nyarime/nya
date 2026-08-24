package nya

import "fmt"

import ()

// zcFSECustomTable holds a custom FSE encoding table built from actual symbol frequencies.
type zcFSECustomTable struct {
	accuracyLog int
	tableSize   int
	symbols     []byte
	numBits     []byte
	newState    []uint16
	sym2states  [256][]int // symbol → list of states that emit this symbol
}

// zcBuildCustomFSEEncoder builds a custom FSE table from symbol frequencies.
// Returns: (serialized_header, encoder_table, error)
func zcBuildCustomFSEEncoder(freqs map[byte]int, maxAccLog int, maxSymbol int) ([]byte, *zcFSECustomTable, error) {
	// Step 1: Find actual max symbol
	actualMax := 0
	totalCount := 0
	for sym, cnt := range freqs {
		if int(sym) > actualMax {
			actualMax = int(sym)
		}
		totalCount += cnt
	}
	if actualMax > maxSymbol {
		actualMax = maxSymbol
	}

	// Step 2: Choose accuracy log (minimum 8 — smaller causes serialization overflow)
	accLog := 8
	if maxAccLog > accLog {
		accLog = maxAccLog
	}
	tableSize := 1 << uint(accLog)

	// Step 3: Normalize frequencies to sum = tableSize (all probs >= 1, no -1)
	probs := make([]int16, maxSymbol+1)
	// Assign proportional, minimum 1 for non-zero
	assigned := 0
	nonZeroCount := 0
	for sym := 0; sym <= actualMax; sym++ {
		cnt := freqs[byte(sym)]
		if cnt == 0 {
			probs[sym] = -1 // absent but possible → prob < 1
			continue
		}
		nonZeroCount++
		p := cnt * tableSize / totalCount
		if p < 1 {
			p = 1
		}
		probs[sym] = int16(p)
		assigned += p
	}
	// Also mark symbols beyond actualMax up to maxSymbol as -1
	for sym := actualMax + 1; sym <= maxSymbol; sym++ {
		probs[sym] = -1
	}
	// Adjust largest to make sum exact: positive_sum + neg1_count = tableSize
	neg1Count := 0
	for _, p := range probs {
		if p == -1 {
			neg1Count++
		}
	}
	target := tableSize - neg1Count
	diff := target - assigned
	bestSym := -1
	for sym := 0; sym <= actualMax; sym++ {
		if probs[sym] > 0 && (bestSym < 0 || probs[sym] > probs[bestSym]) {
			bestSym = sym
		}
	}
	if bestSym >= 0 {
		probs[bestSym] += int16(diff)
	}

	// Step 3.5: Fix probs for encoding constraints
	zcFixProbsForEncoding(probs, accLog)
	// Step 4: Serialize FSE table header
	header := zcSerializeFSEHeader(probs, accLog)

	// Step 5: Parse header with DECODER to get the exact table it will use
	// This guarantees encoder and decoder use identical tables.
	decoderTbl, hdrConsumed, parseErr := zstdBuildFSETableFromHeader(header, maxAccLog)
	if parseErr != nil {
		return nil, nil, fmt.Errorf("FSE header roundtrip failed: %w", parseErr)
	}
	if hdrConsumed != len(header) {
		return nil, nil, fmt.Errorf("FSE header consumed %d != written %d", hdrConsumed, len(header))
	}

	// Convert decoder table to encoder table format
	tbl := &zcFSECustomTable{
		accuracyLog: decoderTbl.accuracyLog,
		tableSize:   decoderTbl.stateCount,
		symbols:     decoderTbl.symbols,
		numBits:     decoderTbl.numBits,
		newState:    decoderTbl.newState,
	}
	// Build sym2states reverse mapping
	for i := 0; i < tbl.tableSize; i++ {
		sym := tbl.symbols[i]
		tbl.sym2states[sym] = append(tbl.sym2states[sym], i)
	}

	return header, tbl, nil
}

// zcSerializeFSEHeader serializes probability distribution per RFC 8878 §4.1.1
func zcSerializeFSEHeader(probs []int16, accLog int) []byte {
	// C-faithful FSE_writeNCount port — matches our C-faithful decoder
	tableSize := 1 << uint(accLog)
	var buf []byte
	var bitBuf uint64
	var bitPos int
	write := func(v uint32, n int) {
		bitBuf |= uint64(v) << uint(bitPos)
		bitPos += n
		for bitPos >= 8 {
			buf = append(buf, byte(bitBuf))
			bitBuf >>= 8
			bitPos -= 8
		}
	}
	write(uint32(accLog-5), 4)

	remaining := tableSize + 1
	threshold := tableSize
	nbBits := accLog + 1
	previousIs0 := false
	sym := 0

	for sym < len(probs) && remaining > 1 {
		if previousIs0 {
			// Encode run of zeros
			start := sym
			for sym < len(probs) && probs[sym] == 0 {
				sym++
			}
			if sym >= len(probs) {
				break
			}
			// Encode skip count
			for sym >= start+24 {
				write(0xFFFF, 16)
				start += 24
			}
			for sym >= start+3 {
				write(3, 2)
				start += 3
			}
			write(uint32(sym-start), 2)
		}
		if sym >= len(probs) {
			break
		}

		count := int(probs[sym])
		max := (2*threshold - 1) - remaining
		if count < 0 {
			remaining -= -count // remaining -= abs(count)
		} else {
			remaining -= count
		}
		count++ // store as count+1
		if count >= threshold {
			count += max
		}
		if count < max {
			write(uint32(count), nbBits-1) // short form
		} else {
			write(uint32(count), nbBits) // long form
		}
		previousIs0 = (count == 1)
		sym++

		for remaining < threshold {
			threshold >>= 1
			nbBits--
		}
	}

	// Flush remaining bits
	if bitPos > 0 {
		buf = append(buf, byte(bitBuf))
	}
	return buf
}

func zcFixProbsForEncoding(probs []int16, accLog int) {
	tableSize := 1 << uint(accLog)
	remaining := tableSize + 1
	threshold := tableSize
	nbBits := accLog + 1

	for sym := 0; sym < len(probs) && remaining > 1; sym++ {
		prob := probs[sym]
		var val int
		if prob == -1 {
			val = 0
		} else {
			val = int(prob) + 1
		}

		// Max encodable value at this state
		upperBound := (1 << uint(nbBits)) - 1 - threshold + 1
		// Short max: upperBound - 1
		// Long max: lowBits_max*2 + 1 - upperBound where lowBits_max = (1<<(nbBits-1))-1
		maxLong := ((1<<uint(nbBits-1))-1)*2 + 1 - upperBound
		maxVal := maxLong
		if upperBound-1 > maxVal {
			maxVal = upperBound - 1
		}

		if val > maxVal && val > 0 {
			// Can't encode this val — clamp
			if maxVal >= 2 {
				probs[sym] = int16(maxVal - 1) // reduce prob
			} else {
				probs[sym] = -1 // force to "less than 1"
			}
		}

		// Update remaining
		p := probs[sym]
		if p == -1 {
			remaining--
		} else if p > 0 {
			remaining -= int(p)
		}

		// Skip zeros
		if p == 0 {
			for sym+1 < len(probs) && probs[sym+1] == 0 {
				sym++
			}
		}

		for remaining < threshold {
			threshold >>= 1
			nbBits--
		}
	}

	// Ensure remaining == 1 at the end (add extra to last non-zero symbol if needed)
	// This is handled implicitly by the decoder stopping when remaining <= 1
}

// zcFindStateCustom returns a state index that decodes to the given symbol in a custom table.
func zcFindStateCustom(tbl *zcFSECustomTable, sym int) int {
	if sym >= 0 && sym < 256 {
		if states := tbl.sym2states[byte(sym)]; len(states) > 0 {
			return states[0]
		}
	}
	return 0
}

// zcFindNextStateCustom finds a source state for sym that can reach targetState.
// Same logic as zcEncodeTransition but for custom tables.
func zcFindNextStateCustom(tbl *zcFSECustomTable, targetState int, sym int) (int, int, int) {
	candidates := tbl.sym2states[byte(sym)]
	for _, s := range candidates {
		nb := int(tbl.numBits[s])
		base := int(tbl.newState[s])
		maxVal := 1 << uint(nb)
		val := targetState - base
		if val >= 0 && val < maxVal {
			return s, nb, val
		}
	}
	if len(candidates) > 0 {
		s := candidates[0]
		nb := int(tbl.numBits[s])
		base := int(tbl.newState[s])
		return s, nb, targetState - base
	}
	return 0, 0, 0
}
