package nya

// Bit-cost estimation for the LZMA encoder.
//
// Every function here mirrors the matching encode* routine symbol for symbol.
// If one changes, the other has to change with it, otherwise the parser will
// optimise against a model that does not describe the real output.
//
// Prices are in sixteenths of a bit, the same fixed point the LZMA reference
// encoder uses.

const (
	lzmaMoveReducingBits = 4
	lzmaPriceShiftBits   = 4
	lzmaPriceOneBit      = 1 << lzmaPriceShiftBits
)

// lzmaProbPrices[p>>4] is the cost of coding a bit whose model says it has
// probability p of being zero.
var lzmaProbPrices [(1 << lzmaProbBits) >> lzmaMoveReducingBits]uint32

func init() {
	for i := range lzmaProbPrices {
		w := uint32(i<<lzmaMoveReducingBits) + (1 << (lzmaMoveReducingBits - 1))
		bitCount := uint32(0)
		for j := 0; j < lzmaPriceShiftBits; j++ {
			w = w * w
			bitCount <<= 1
			for w >= 1<<16 {
				w >>= 1
				bitCount++
			}
		}
		lzmaProbPrices[i] = (lzmaProbBits << lzmaPriceShiftBits) - 15 - bitCount
	}
}

// priceBit is the cost of coding bit under model prob.
func priceBit(prob uint16, bit int) uint32 {
	p := uint32(prob)
	if bit != 0 {
		p ^= lzmaProbMask
	}
	return lzmaProbPrices[p>>lzmaMoveReducingBits]
}

// priceBitTree mirrors rangeEncoder.encodeBitTree.
func priceBitTree(probs []uint16, numBits int, value uint32) uint32 {
	var price uint32
	m := uint32(1)
	for i := numBits - 1; i >= 0; i-- {
		bit := int((value >> uint(i)) & 1)
		price += priceBit(probs[m], bit)
		m = (m << 1) | uint32(bit)
	}
	return price
}

// priceBitTreeReverse mirrors rangeEncoder.encodeBitTreeReverse.
func priceBitTreeReverse(probs []uint16, numBits int, value uint32) uint32 {
	var price uint32
	m := uint32(1)
	for i := 0; i < numBits; i++ {
		bit := int(value & 1)
		price += priceBit(probs[m], bit)
		m = (m << 1) | uint32(bit)
		value >>= 1
	}
	return price
}

// priceBitTreeReverseOffset mirrors lzmaEncoder.encodeBitTreeReverseOffset.
func priceBitTreeReverseOffset(probs []uint16, offset, numBits int, value uint32) uint32 {
	var price uint32
	m := uint32(1)
	for i := 0; i < numBits; i++ {
		bit := int(value & 1)
		price += priceBit(probs[offset+int(m)], bit)
		m = (m << 1) | uint32(bit)
		value >>= 1
	}
	return price
}

// price mirrors lzmaLenEncoder.encode.
func (le *lzmaLenEncoder) price(length uint32, posState uint32) uint32 {
	length -= lzmaMatchLenMin
	if length < lzmaLenNumLowSyms {
		return priceBit(le.choice, 0) +
			priceBitTree(le.low[posState], lzmaLenNumLowBits, length)
	}
	price := priceBit(le.choice, 1)
	length -= lzmaLenNumLowSyms
	if length < lzmaLenNumMidSyms {
		return price + priceBit(le.choice2, 0) +
			priceBitTree(le.mid[posState], lzmaLenNumMidBits, length)
	}
	length -= lzmaLenNumMidSyms
	return price + priceBit(le.choice2, 1) +
		priceBitTree(le.high, lzmaLenNumHighBits, length)
}

// priceLiteral mirrors lzmaEncoder.encodeLiteral for the byte at pos.
func (enc *lzmaEncoder) priceLiteral(pos int) uint32 {
	b := enc.src[pos]
	prevByte := byte(0)
	if pos > 0 {
		prevByte = enc.src[pos-1]
	}
	posState := uint32(pos) & ((1 << enc.pb) - 1)

	price := priceBit(enc.isMatch[(enc.state<<lzmaNumPosBitsMax)+posState], 0)

	litState := uint32(prevByte>>(8-enc.lc)) | ((uint32(pos) & ((1 << enc.lp) - 1)) << enc.lc)
	probs := enc.litProbs[litState*0x300:]

	if stateIsLit(enc.state) {
		symbol := uint32(1)
		for i := 7; i >= 0; i-- {
			bit := int((uint32(b) >> uint(i)) & 1)
			price += priceBit(probs[symbol], bit)
			symbol = (symbol << 1) | uint32(bit)
		}
		return price
	}

	matchByte := uint32(0)
	if int(enc.reps[0]) < pos {
		matchByte = uint32(enc.src[pos-int(enc.reps[0])-1])
	}
	symbol := uint32(1)
	bval := uint32(b)
	for i := 7; i >= 0; i-- {
		bit := int((bval >> uint(i)) & 1)
		matchBit := (matchByte >> uint(i)) & 1
		price += priceBit(probs[((1+matchBit)<<8)+symbol], bit)
		symbol = (symbol << 1) | uint32(bit)
		if matchBit != uint32(bit) {
			for i--; i >= 0; i-- {
				bit = int((bval >> uint(i)) & 1)
				price += priceBit(probs[symbol], bit)
				symbol = (symbol << 1) | uint32(bit)
			}
			break
		}
	}
	return price
}

// priceMatch mirrors lzmaEncoder.encodeMatch.
func (enc *lzmaEncoder) priceMatch(dist uint32, length int, posState uint32) uint32 {
	price := priceBit(enc.isMatch[(enc.state<<lzmaNumPosBitsMax)+posState], 1)
	price += priceBit(enc.isRep[enc.state], 0)
	price += enc.matchLen.price(uint32(length), posState)

	lenState := getLenToPosState(uint32(length))
	distSlot := getDistSlot(dist)
	price += priceBitTree(enc.posSlot[lenState], 6, distSlot)

	if distSlot >= lzmaStartPosModelIndex {
		numDirectBits := (distSlot >> 1) - 1
		base := (2 | (distSlot & 1)) << numDirectBits
		reduced := dist - base
		if distSlot < lzmaEndPosModelIndex {
			price += priceBitTreeReverseOffset(enc.posSpecial[:], int(base)-int(distSlot), int(numDirectBits), reduced)
		} else {
			// Direct bits are coded without a model, so each costs one bit.
			price += (numDirectBits - lzmaNumAlignBits) * lzmaPriceOneBit
			price += priceBitTreeReverse(enc.posAlign[:], lzmaNumAlignBits, reduced&((1<<lzmaNumAlignBits)-1))
		}
	}
	return price
}

// priceRep mirrors lzmaEncoder.encodeRepMatch for length >= 2.
func (enc *lzmaEncoder) priceRep(repIdx int, length int, posState uint32) uint32 {
	price := priceBit(enc.isMatch[(enc.state<<lzmaNumPosBitsMax)+posState], 1)
	price += priceBit(enc.isRep[enc.state], 1)
	price += enc.priceRepIndex(repIdx, posState, false)
	price += enc.repLen.price(uint32(length), posState)
	return price
}

// priceShortRep is the cost of the one-byte rep0 form.
func (enc *lzmaEncoder) priceShortRep(posState uint32) uint32 {
	price := priceBit(enc.isMatch[(enc.state<<lzmaNumPosBitsMax)+posState], 1)
	price += priceBit(enc.isRep[enc.state], 1)
	price += enc.priceRepIndex(0, posState, true)
	return price
}

// priceRepIndex costs the isRepG0/G1/G2 and isRep0Long flags that select which
// repeated distance is used.
func (enc *lzmaEncoder) priceRepIndex(repIdx int, posState uint32, short bool) uint32 {
	if repIdx == 0 {
		price := priceBit(enc.isRepG0[enc.state], 0)
		if short {
			return price + priceBit(enc.isRep0Long[(enc.state<<lzmaNumPosBitsMax)+posState], 0)
		}
		return price + priceBit(enc.isRep0Long[(enc.state<<lzmaNumPosBitsMax)+posState], 1)
	}
	price := priceBit(enc.isRepG0[enc.state], 1)
	if repIdx == 1 {
		return price + priceBit(enc.isRepG1[enc.state], 0)
	}
	price += priceBit(enc.isRepG1[enc.state], 1)
	if repIdx == 2 {
		return price + priceBit(enc.isRepG2[enc.state], 0)
	}
	return price + priceBit(enc.isRepG2[enc.state], 1)
}
