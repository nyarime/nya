package nya

// BT4 (4-byte hash, binary tree) match finder — the structure 7-Zip uses at
// high compression levels. Compared to a singly-linked hash chain it visits
// more distinct candidates at the same depth budget, which matters because
// distance dominates encoded cost and the nearest chain entry is rarely the
// cheapest one.

const (
	lzmaHash2Bits = 10
	lzmaHash2Size = 1 << lzmaHash2Bits
	lzmaHash3Bits = 16
	lzmaHash3Size = 1 << lzmaHash3Bits
)

func (enc *lzmaEncoder) initBT4() {
	cyclic := enc.dictSize
	if cyclic > len(enc.src) {
		cyclic = len(enc.src)
	}
	if cyclic < 4096 {
		cyclic = 4096
	}
	enc.cyclicSize = cyclic + 1
	enc.son = make([]int32, enc.cyclicSize*2)
	for i := range enc.son {
		enc.son[i] = -1
	}
	enc.hash2Table = make([]int32, lzmaHash2Size)
	enc.hash3Table = make([]int32, lzmaHash3Size)
	for i := range enc.hash2Table {
		enc.hash2Table[i] = -1
	}
	for i := range enc.hash3Table {
		enc.hash3Table[i] = -1
	}
}

func (enc *lzmaEncoder) hash2(pos int) uint32 {
	if pos+2 > len(enc.src) {
		return 0
	}
	return (uint32(enc.src[pos]) | uint32(enc.src[pos+1])<<8) & (lzmaHash2Size - 1)
}

func (enc *lzmaEncoder) hash3(pos int) uint32 {
	if pos+3 > len(enc.src) {
		return 0
	}
	v := uint32(enc.src[pos]) | uint32(enc.src[pos+1])<<8 | uint32(enc.src[pos+2])<<16
	return (v * 0x9E3779B1) >> (32 - lzmaHash3Bits)
}

func (enc *lzmaEncoder) matchMinPos(pos int) int {
	min := pos - enc.dictSize
	if min < 0 {
		return 0
	}
	return min
}

// bt4Insert adds pos into the hash heads and the binary tree rooted at its
// hash4 bucket. Positions up to but excluding pos must already be indexed.
func (enc *lzmaEncoder) bt4Insert(pos int) {
	if pos+lzmaMinMatch > len(enc.src) {
		return
	}

	h2 := enc.hash2(pos)
	enc.hash2Table[h2] = int32(pos)
	h3 := enc.hash3(pos)
	enc.hash3Table[h3] = int32(pos)

	h4 := enc.hash4(pos)
	curMatch := enc.hashTable[h4]
	enc.hashTable[h4] = int32(pos)

	matchMin := enc.matchMinPos(pos)
	cycPos := pos % enc.cyclicSize
	sonLeft := &enc.son[cycPos*2]
	sonRight := &enc.son[cycPos*2+1]

	matchLen := 0
	for count := enc.depth; count > 0 && curMatch >= int32(matchMin); count-- {
		delta := pos - int(curMatch)
		if delta <= 0 || delta >= enc.cyclicSize {
			break
		}
		cpos := int(curMatch)
		src := enc.src
		if cpos+matchLen >= len(src) || pos+matchLen >= len(src) {
			break
		}
		if src[cpos+matchLen] != src[pos+matchLen] {
			if src[cpos+matchLen] < src[pos+matchLen] {
				sonLeft = &enc.son[(curMatch%int32(enc.cyclicSize))*2+1]
			} else {
				sonRight = &enc.son[(curMatch%int32(enc.cyclicSize))*2]
			}
			matchLen = 0
		} else {
			ml := zstdMatchLen(src, pos, cpos)
			if ml > lzmaMaxMatch {
				ml = lzmaMaxMatch
			}
			matchLen = ml
			if matchLen >= lzmaMaxMatch || cpos+matchLen >= len(src) || pos+matchLen >= len(src) {
				*sonLeft = curMatch
				*sonRight = curMatch
				return
			}
			if src[cpos+matchLen] > src[pos+matchLen] {
				sonRight = &enc.son[(curMatch%int32(enc.cyclicSize))*2]
				sonLeft = &enc.son[(curMatch%int32(enc.cyclicSize))*2+1]
			} else {
				sonLeft = &enc.son[(curMatch%int32(enc.cyclicSize))*2]
				sonRight = &enc.son[(curMatch%int32(enc.cyclicSize))*2+1]
			}
		}
		curMatch = *sonLeft
	}
	*sonLeft = -1
	*sonRight = -1
}

func (enc *lzmaEncoder) bt4ProbeHash2(pos, maxLen, matchMin int) {
	if pos+2 > enc.limit {
		return
	}
	cur := enc.hash2Table[enc.hash2(pos)]
	if cur < int32(matchMin) || int(cur) >= pos {
		return
	}
	cpos := int(cur)
	ml := zstdMatchLen(enc.src, pos, cpos)
	if ml > maxLen {
		ml = maxLen
	}
	if ml >= lzmaMinMatch {
		enc.recordMatchFrontier(uint32(pos-cpos-1), ml)
	}
}

func (enc *lzmaEncoder) bt4ProbeHash3(pos, maxLen, matchMin int) {
	if pos+3 > enc.limit {
		return
	}
	cur := enc.hash3Table[enc.hash3(pos)]
	if cur < int32(matchMin) || int(cur) >= pos {
		return
	}
	cpos := int(cur)
	ml := zstdMatchLen(enc.src, pos, cpos)
	if ml > maxLen {
		ml = maxLen
	}
	if ml >= lzmaMinMatch {
		enc.recordMatchFrontier(uint32(pos-cpos-1), ml)
	}
}

func (enc *lzmaEncoder) appendMatchFrontier(dist uint32, length int) {
	for _, m := range enc.matches {
		if m.length == length && m.dist == dist {
			return
		}
	}
	// Keep increasing lengths; replace same length with nearer dist.
	for i, m := range enc.matches {
		if m.length == length {
			if dist < m.dist {
				enc.matches[i].dist = dist
			}
			return
		}
	}
	enc.matches = append(enc.matches, lzmaMatch{dist: dist, length: length})
}

// recordMatchFrontier adds several priced lengths for one distance.
func (enc *lzmaEncoder) recordMatchFrontier(dist uint32, length int) {
	if length < lzmaMinMatch {
		return
	}
	for _, l := range candidateLengths(length) {
		enc.appendMatchFrontier(dist, l)
	}
}

// bt4FindMatches collects match candidates at pos. Pos must not be indexed
// yet; callers should run advanceHash(pos) first.
func (enc *lzmaEncoder) bt4FindMatches(pos int) []lzmaMatch {
	enc.matches = enc.matches[:0]
	if pos+lzmaMinMatch > enc.limit {
		return enc.matches
	}

	maxLen := lzmaMaxMatch
	if rem := enc.limit - pos; rem < maxLen {
		maxLen = rem
	}
	matchMin := enc.matchMinPos(pos)

	enc.bt4ProbeHash2(pos, maxLen, matchMin)
	enc.bt4ProbeHash3(pos, maxLen, matchMin)

	h4 := enc.hash4(pos)
	curMatch := enc.hashTable[h4]

	bestLen := 1
	count := enc.depth
	for curMatch >= int32(matchMin) && count > 0 {
		count--
		delta := pos - int(curMatch)
		if delta <= 0 || delta >= enc.cyclicSize {
			break
		}
		cpos := int(curMatch)
		src := enc.src

		if bestLen > 1 && src[cpos+bestLen-1] != src[pos+bestLen-1] {
			curMatch = enc.son[(curMatch%int32(enc.cyclicSize))*2]
			continue
		}

		ml := zstdMatchLen(src, pos, cpos)
		if ml > maxLen {
			ml = maxLen
		}
		if ml > bestLen {
			bestLen = ml
			if ml >= lzmaMinMatch {
				enc.recordMatchFrontier(uint32(delta-1), ml)
			}
			if ml >= maxLen || ml >= enc.niceLen {
				break
			}
		}
		curMatch = enc.son[(curMatch%int32(enc.cyclicSize))*2]
	}

	// Sort by length ascending for the parser.
	if len(enc.matches) > 1 {
		for i := 1; i < len(enc.matches); i++ {
			for j := i; j > 0 && enc.matches[j].length < enc.matches[j-1].length; j-- {
				enc.matches[j], enc.matches[j-1] = enc.matches[j-1], enc.matches[j]
			}
		}
	}
	return enc.matches
}
