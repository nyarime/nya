package nya

// Optimal parsing for the LZMA encoder.
//
// The greedy parser in lzma2_parse.go only looks one literal ahead. Optimal
// parsing keeps a dynamic-programming table of the cheapest path to each
// offset within a lookahead window, then encodes the best end point and walks
// back. This is what 7-Zip does at high levels and is the main reason short
// nearby matches stop beating longer distant ones.

const (
	lzmaOptimumWindow    = 4096
	lzmaOptimumWindowMax = 1024 // was 512; avoid L8 dip vs depth≥64 branch
	lzmaOptimumInf       = ^uint32(0) >> 1
)

type lzmaOptNode struct {
	price uint32
	state uint32
	reps  [4]uint32
	prev  int
	move  lzmaMove
}

func (enc *lzmaEncoder) optimumWindow() int {
	w := lzmaOptimumWindow
	if enc.depth >= 256 {
		w = 2048
	} else if enc.depth >= 128 {
		w = lzmaOptimumWindowMax
	} else if enc.depth >= 64 {
		w = 1024
	}
	remain := enc.limit - enc.pos
	if remain < w {
		w = remain
	}
	return w
}

func (enc *lzmaEncoder) cacheMatchesForWindow(remain int) [][]lzmaMatch {
	if cap(enc.optMatchCache) < remain+1 {
		enc.optMatchCache = make([][]lzmaMatch, remain+1)
	}
	cache := enc.optMatchCache[:remain+1]
	for i := range cache {
		cache[i] = nil
	}
	// Index only positions before each search point. Pre-indexing the whole
	// window would insert future bytes into the hash heads and produce bogus
	// forward references with huge distances.
	for cur := 0; cur <= remain; cur++ {
		enc.advanceHash(enc.pos + cur)
		cache[cur] = append([]lzmaMatch(nil), enc.findMatchesAt(enc.pos+cur)...)
	}
	return cache
}

func repsAfterMatch(reps [4]uint32, dist uint32) [4]uint32 {
	return [4]uint32{dist, reps[0], reps[1], reps[2]}
}

func repsAfterRep(reps [4]uint32, repIdx int) [4]uint32 {
	if repIdx == 0 {
		return reps
	}
	out := reps
	dist := out[repIdx]
	for i := repIdx; i > 0; i-- {
		out[i] = out[i-1]
	}
	out[0] = dist
	return out
}

func (enc *lzmaEncoder) encodeOptimal() {
	for enc.pos < enc.limit {
		if enc.compLimit > 0 && len(enc.rc.out) >= enc.compLimit {
			return
		}
		if !enc.optimalStep() {
			enc.step()
		}
	}
}

// optimalStep parses up to lzmaOptimumWindow bytes from enc.pos, encodes the
// cheapest path found, and returns false if nothing was encoded (fallback).
func (enc *lzmaEncoder) optimalStep() bool {
	remain := enc.optimumWindow()
	if remain <= 1 {
		return false
	}

	opt := enc.optBuf
	if len(opt) < remain+1 {
		enc.optBuf = make([]lzmaOptNode, lzmaOptimumWindow+1)
		opt = enc.optBuf
	}

	for i := 0; i <= remain; i++ {
		opt[i] = lzmaOptNode{price: lzmaOptimumInf}
	}
	opt[0] = lzmaOptNode{
		price: 0,
		state: enc.state,
		reps:  enc.reps,
		prev:  -1,
	}

	matchCache := enc.cacheMatchesForWindow(remain)

	for cur := 0; cur < remain; cur++ {
		if opt[cur].price >= lzmaOptimumInf {
			continue
		}
		basePos := enc.pos + cur
		enc.state = opt[cur].state
		enc.reps = opt[cur].reps

		posState := uint32(basePos) & ((1 << enc.pb) - 1)

		// Literal → cur+1
		litPrice := opt[cur].price + enc.priceLiteral(basePos)
		if litPrice < opt[cur+1].price {
			opt[cur+1] = lzmaOptNode{
				price: litPrice,
				state: lzmaNextState[opt[cur].state][0],
				reps:  opt[cur].reps,
				prev:  cur,
				move:  lzmaMove{kind: moveLiteral, length: 1},
			}
		}

		// Short rep → cur+1
		if enc.repMatchesAt(basePos, int(enc.reps[0]), 1) {
			p := opt[cur].price + enc.priceShortRep(posState)
			if p < opt[cur+1].price {
				opt[cur+1] = lzmaOptNode{
					price: p,
					state: lzmaNextState[opt[cur].state][3],
					reps:  opt[cur].reps,
					prev:  cur,
					move:  lzmaMove{kind: moveShortRep, length: 1},
				}
			}
		}

		// Repeated matches
		if repIdx, repLen := enc.findRepMatchAt(basePos, &opt[cur].reps); repIdx >= 0 {
			for _, l := range candidateLengths(repLen) {
				if cur+l > remain {
					continue
				}
				p := opt[cur].price + enc.priceRep(repIdx, l, posState)
				if p < opt[cur+l].price {
					opt[cur+l] = lzmaOptNode{
						price: p,
						state: lzmaNextState[opt[cur].state][2],
						reps:  repsAfterRep(opt[cur].reps, repIdx),
						prev:  cur,
						move:  lzmaMove{kind: moveRep, repIdx: repIdx, length: l},
					}
				}
			}
		}

		// Normal matches (cached for this window)
		for _, m := range matchCache[cur] {
			for _, l := range candidateLengths(m.length) {
				if cur+l > remain {
					continue
				}
				p := opt[cur].price + enc.priceMatch(m.dist, l, posState)
				if p < opt[cur+l].price {
					opt[cur+l] = lzmaOptNode{
						price: p,
						state: lzmaNextState[opt[cur].state][1],
						reps:  repsAfterMatch(opt[cur].reps, m.dist),
						prev:  cur,
						move:  lzmaMove{kind: moveMatch, dist: m.dist, length: l},
					}
				}
			}
		}
	}

	bestCur := 1
	for cur := 2; cur <= remain; cur++ {
		if opt[cur].price >= lzmaOptimumInf {
			continue
		}
		// Minimize price per byte; tie-break toward longer advances.
		if opt[cur].price*uint32(bestCur) <= opt[bestCur].price*uint32(cur) {
			bestCur = cur
		}
	}
	if opt[bestCur].price >= lzmaOptimumInf {
		return false
	}

	// Collect moves by walking back from bestCur.
	moves := enc.optMoves[:0]
	for cur := bestCur; cur > 0; {
		moves = append(moves, opt[cur].move)
		cur = opt[cur].prev
	}
	for i, j := 0, len(moves)-1; i < j; i, j = i+1, j-1 {
		moves[i], moves[j] = moves[j], moves[i]
	}
	enc.optMoves = moves

	// DP left enc.state/reps at the last explored node; restore the live
	// values at enc.pos before actually encoding the chosen path.
	enc.state = opt[0].state
	enc.reps = opt[0].reps

	for _, m := range moves {
		switch m.kind {
		case moveLiteral:
			enc.encodeLiteral(enc.src[enc.pos])
		case moveShortRep:
			enc.encodeRepMatch(0, 1)
		case moveRep:
			enc.encodeRepMatch(m.repIdx, m.length)
		default:
			enc.encodeMatch(m.dist, m.length)
		}
	}
	return true
}
