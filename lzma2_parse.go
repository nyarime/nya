package nya

// Parsing: deciding, at each position, whether to emit a literal, a match or
// a repeated match.
//
// The choice is made on encoded cost rather than match length. Length alone
// is misleading — a two byte match at a distance of 100000 costs far more
// than the two literals it replaces — and choosing by length was what kept
// this encoder well behind the reference one.

const (
	moveLiteral = iota
	moveShortRep
	moveRep
	moveMatch
)

// lzmaMove is one candidate coding decision and what it costs.
type lzmaMove struct {
	kind   int
	repIdx int
	dist   uint32
	length int
	price  uint32
}

// step emits one coding decision at the current position.
func (enc *lzmaEncoder) step() {
	pos := enc.pos
	// Index everything before pos, but not pos itself: a match search there
	// must only see earlier positions, and finding pos in its own chain would
	// yield a zero distance and abort the search.
	enc.advanceHash(pos)

	best := enc.bestMoveAt(pos)
	if best.kind == moveLiteral {
		enc.encodeLiteral(enc.src[pos])
		return
	}

	// Lazy match: skip up to two literals when a better match follows soon.
	if best.length < enc.niceLen {
		for skip := 1; skip <= 2 && pos+skip < enc.limit; skip++ {
			if enc.shouldLazyLiteral(pos, best, skip) {
				enc.encodeLiteral(enc.src[pos])
				return
			}
		}
	}

	switch best.kind {
	case moveShortRep:
		enc.encodeRepMatch(0, 1)
	case moveRep:
		enc.encodeRepMatch(best.repIdx, best.length)
	default:
		enc.encodeMatch(best.dist, best.length)
	}
}

// bestMoveAt prices every option available at pos and returns the cheapest.
// The result is moveLiteral when no match beats simply coding the byte.
func (enc *lzmaEncoder) bestMoveAt(pos int) lzmaMove {
	posState := uint32(pos) & ((1 << enc.pb) - 1)

	best := lzmaMove{kind: moveLiteral, length: 1, price: enc.priceLiteral(pos)}
	// Compare candidates on price per byte, scaled to avoid division.
	bestNum := uint64(best.price)
	bestDen := uint64(best.length)

	consider := func(m lzmaMove) {
		num := uint64(m.price)
		den := uint64(m.length)
		if num*bestDen < bestNum*den {
			best, bestNum, bestDen = m, num, den
		}
	}

	// A single byte that continues the most recent distance is very cheap.
	if enc.repMatchesAt(pos, int(enc.reps[0]), 1) {
		consider(lzmaMove{
			kind:   moveShortRep,
			length: 1,
			price:  enc.priceShortRep(posState),
		})
	}

	if repIdx, repLen := enc.findRepMatchAt(pos, &enc.reps); repIdx >= 0 {
		// Shorter forms of the same repeat are sometimes cheaper per byte,
		// so price a few lengths rather than only the longest.
		for _, l := range candidateLengths(repLen) {
			consider(lzmaMove{
				kind:   moveRep,
				repIdx: repIdx,
				length: l,
				price:  enc.priceRep(repIdx, l, posState),
			})
		}
	}

	// Price the whole length/distance frontier. A nearby three byte match can
	// beat a distant twenty byte one once the distance code is paid for.
	for _, m := range enc.findMatchesAt(pos) {
		consider(lzmaMove{
			kind:   moveMatch,
			dist:   m.dist,
			length: m.length,
			price:  enc.priceMatch(m.dist, m.length, posState),
		})
	}

	return best
}

// shouldLazyLiteral reports whether emitting one literal at pos beats taking
// best now, assuming skip literals then a match at pos+skip.
func (enc *lzmaEncoder) shouldLazyLiteral(pos int, best lzmaMove, skip int) bool {
	if best.length >= enc.niceLen || pos+skip >= enc.limit {
		return false
	}
	savedState := enc.state
	var litPrice uint32
	state := enc.state
	for i := 0; i < skip; i++ {
		litPrice += enc.priceLiteral(pos + i)
		state = lzmaNextState[state][0]
	}
	enc.state = state
	for i := 1; i <= skip; i++ {
		enc.advanceHash(pos + i)
	}
	next := enc.bestMoveAt(pos + skip)
	enc.state = savedState
	if next.kind == moveLiteral {
		return false
	}
	lhs := (uint64(litPrice) + uint64(next.price)) * uint64(best.length)
	rhs := uint64(best.price) * uint64(skip+next.length)
	return lhs < rhs
}

// candidateLengths returns the lengths worth pricing for a match that can run
// up to maxLen bytes: the full length, plus a couple of shorter cut-offs that
// occasionally encode better.
func candidateLengths(maxLen int) []int {
	if maxLen <= lzmaMinMatch {
		return []int{maxLen}
	}
	out := []int{lzmaMinMatch}
	add := func(l int) {
		if l < lzmaMinMatch {
			l = lzmaMinMatch
		}
		if l > maxLen {
			l = maxLen
		}
		for _, x := range out {
			if x == l {
				return
			}
		}
		out = append(out, l)
	}
	add(maxLen / 2)
	add((maxLen * 3) / 4)
	add(maxLen - 1)
	add(maxLen)
	return out
}

// repMatchesAt reports whether the n bytes at pos repeat the ones at the
// given 0-based repeat distance.
func (enc *lzmaEncoder) repMatchesAt(pos, dist, n int) bool {
	src := pos - dist - 1
	if src < 0 || pos+n > enc.limit {
		return false
	}
	for i := 0; i < n; i++ {
		if enc.src[src+i] != enc.src[pos+i] {
			return false
		}
	}
	return true
}
