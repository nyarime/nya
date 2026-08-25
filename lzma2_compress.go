package nya

// LZMA2/LZMA compressor — enables NYA --best mode for maximum compression.
// Implements: range encoder, LZ77 BT4 match finder, LZMA state machine, LZMA2 chunked wrapper.
// Decompression uses the existing pure-Go decoder in xz_decompress.go.

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
)

// ── Public API ──────────────────────────────────────────────────────────────

// lzma2MaxChunkSpan is the largest uncompressed span one LZMA2 chunk can
// describe: the control byte carries the top five bits of (size - 1).
const lzma2MaxChunkSpan = 1 << 21

// lzma2CompBudget closes a chunk before its compressed size overflows the
// 16-bit field, leaving headroom for the range coder flush.
const lzma2CompBudget = 65000

// lzma2MinSegment is the smallest span handed to one worker. Segments are
// independent of each other, so small ones would shrink the effective
// dictionary back towards the per-chunk behaviour this avoids.
const lzma2MinSegment = 4 << 20

// Lzma2Compress compresses src into a raw LZMA2 stream (no XZ container).
//
// Large inputs are split into segments compressed in parallel. Within a
// segment one encoder drives every chunk and only the probability models are
// reset at a chunk boundary, so the match finder — and the dictionary the
// decoder is holding — carries across and matches reach back through the
// whole segment. Compressing each 64 KiB chunk in isolation, as this used to,
// capped the effective dictionary at the chunk size whatever dictSize said.
func Lzma2Compress(src []byte, dictSize int) ([]byte, error) {
	return Lzma2CompressOpts(src, Lzma2Options{DictSize: dictSize})
}

// Lzma2CompressOpts compresses src with explicit encoder settings.
func Lzma2CompressOpts(src []byte, opts Lzma2Options) ([]byte, error) {
	if len(src) == 0 {
		return []byte{0x00}, nil // end marker only
	}
	if opts.DictSize <= 0 {
		opts.DictSize = lzma2DictSize
	}
	if opts.Depth <= 0 {
		opts.Depth = lzmaDefaultDepth
	}
	if opts.NiceLen <= 0 {
		opts.NiceLen = lzmaDefaultNiceLen
	}

	bounds := lzma2SegmentBounds(len(src))
	if len(bounds) == 1 {
		out, err := lzma2CompressSegment(src, opts)
		if err != nil {
			return nil, err
		}
		return append(out, 0x00), nil
	}

	parts := make([][]byte, len(bounds))
	errs := make([]error, len(bounds))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i, b := range bounds {
		wg.Add(1)
		go func(i, start, end int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			parts[i], errs[i] = lzma2CompressSegment(src[start:end], opts)
		}(i, b[0], b[1])
	}
	wg.Wait()

	var out []byte
	for i, part := range parts {
		if errs[i] != nil {
			return nil, fmt.Errorf("lzma2: segment %d: %w", i, errs[i])
		}
		out = append(out, part...)
	}
	return append(out, 0x00), nil
}

// lzma2SegmentBounds splits n bytes into at most one segment per CPU, never
// smaller than lzma2MinSegment.
func lzma2SegmentBounds(n int) [][2]int {
	segs := runtime.NumCPU()
	if max := n / lzma2MinSegment; segs > max {
		segs = max
	}
	if segs < 2 {
		return [][2]int{{0, n}}
	}
	size := (n + segs - 1) / segs
	var out [][2]int
	for start := 0; start < n; start += size {
		end := start + size
		if end > n {
			end = n
		}
		out = append(out, [2]int{start, end})
	}
	return out
}

// lzma2CompressSegment emits the chunks for one independent segment, without
// the stream end marker. Every segment starts with a dictionary reset, since
// the worker never saw the bytes before it.
func lzma2CompressSegment(src []byte, opts Lzma2Options) ([]byte, error) {
	enc := newLzmaEncoder(src, opts.DictSize)
	enc.depth = opts.Depth
	enc.niceLen = opts.NiceLen
	enc.useOptimal = opts.OptimalParse

	var out []byte
	first := true
	for enc.pos < len(src) {
		start := enc.pos

		enc.limit = start + lzma2MaxChunkSpan
		if enc.limit > len(src) {
			enc.limit = len(src)
		}
		enc.compLimit = lzma2CompBudget
		enc.rc = newRangeEncoder()
		enc.resetState()

		enc.encode()
		enc.rc.finish()

		if enc.pos == start {
			return nil, fmt.Errorf("lzma2: no progress at offset %d", start)
		}

		out = lzma2EmitChunk(out, enc.rc.out, src[start:enc.pos], enc.pos-start, first)
		first = false
	}
	return out, nil
}

// lzma2MaxCompChunk is the maximum compressed size per LZMA2 chunk (16-bit field).
const lzma2MaxCompChunk = 1 << 16 // 65536

// lzma2MaxUncompChunk is the maximum uncompressed-mode chunk (16-bit size field).
const lzma2MaxUncompChunk = 1 << 16 // 65536

// lzma2EmitChunk appends one LZMA2 chunk (compressed or uncompressed) to out.
// If the compressed data exceeds the 16-bit size limit (65535), it falls back
// to emitting uncompressed sub-chunks.
func lzma2EmitChunk(out, comp, raw []byte, uncompSize int, first bool) []byte {
	if len(comp) >= uncompSize || len(comp) > lzma2MaxCompChunk {
		// Emit as uncompressed sub-chunks (max 65536 bytes each)
		for i := 0; i < len(raw); i += lzma2MaxUncompChunk {
			end := i + lzma2MaxUncompChunk
			if end > len(raw) {
				end = len(raw)
			}
			sub := raw[i:end]
			ctrl := byte(0x01) // dict reset + uncompressed
			if !first {
				ctrl = 0x02 // no dict reset
			}
			out = append(out, ctrl)
			sz := uint16(len(sub) - 1)
			out = append(out, byte(sz>>8), byte(sz))
			out = append(out, sub...)
			first = false
		}
	} else {
		// LZMA compressed chunk. Reset the state and send fresh properties,
		// but only reset the dictionary on the segment's first chunk so later
		// chunks can still match against earlier data.
		ctrl := byte(0x80)
		if first {
			ctrl |= 0x60 // dict reset + state reset + new props
		} else {
			ctrl |= 0x40 // state reset + new props, dictionary preserved
		}
		usm1 := uint32(uncompSize - 1)
		ctrl |= byte((usm1 >> 16) & 0x1F)

		out = append(out, ctrl)
		out = append(out, byte(usm1>>8), byte(usm1))
		csm1 := uint16(len(comp) - 1)
		out = append(out, byte(csm1>>8), byte(csm1))
		out = append(out, 0x5D) // props: lc=3, lp=0, pb=2
		out = append(out, comp...)
	}
	return out
}

// LzmaCompress compresses src using plain LZMA format (13-byte header + stream).
func LzmaCompress(src []byte) ([]byte, error) {
	dictSize := 1 << 22 // 4MB
	if len(src) < dictSize {
		// Use smallest power-of-2 >= len(src), min 4096
		dictSize = 4096
		for dictSize < len(src) {
			dictSize <<= 1
		}
	}

	comp, err := lzmaCompressBlock(src, dictSize)
	if err != nil {
		return nil, err
	}

	// LZMA header: props(1) + dictSize(4) + uncompSize(8) = 13 bytes
	var hdr [13]byte
	// Props byte: lc=3, lp=0, pb=2 → (2*5+0)*9+3 = 93
	hdr[0] = 93
	binary.LittleEndian.PutUint32(hdr[1:5], uint32(dictSize))
	binary.LittleEndian.PutUint64(hdr[5:13], uint64(len(src)))

	out := make([]byte, 0, 13+len(comp))
	out = append(out, hdr[:]...)
	out = append(out, comp...)
	return out, nil
}

// ── Range Encoder ───────────────────────────────────────────────────────────

type rangeEncoder struct {
	low       uint64
	range_    uint32
	cache     byte
	cacheSize int
	out       []byte
	firstByte bool
}

func newRangeEncoder() *rangeEncoder {
	return &rangeEncoder{
		range_:    0xFFFFFFFF,
		cacheSize: 1,
		firstByte: true,
	}
}

func (e *rangeEncoder) shiftLow() {
	lowHi := byte(e.low >> 32)
	if lowHi != 0 || e.low < 0xFF000000 {
		// Flush cache
		b := e.cache + lowHi
		if e.firstByte {
			// First byte of range encoder output is always 0x00
			e.out = append(e.out, 0x00)
			e.firstByte = false
			e.cacheSize--
		}
		if e.cacheSize > 0 {
			e.out = append(e.out, b)
			for i := 1; i < e.cacheSize; i++ {
				e.out = append(e.out, 0xFF+lowHi)
			}
		}
		e.cacheSize = 0
		e.cache = byte(e.low >> 24)
	}
	e.cacheSize++
	e.low = (e.low << 8) & 0x00FFFFFFFF
}

func (e *rangeEncoder) normalize() {
	if e.range_ < (1 << 24) {
		e.range_ <<= 8
		e.shiftLow()
	}
}

func (e *rangeEncoder) encodeBit(prob *uint16, bit int) {
	bound := (e.range_ >> 11) * uint32(*prob)
	if bit == 0 {
		e.range_ = bound
		*prob += (2048 - *prob) >> 5
	} else {
		e.low += uint64(bound)
		e.range_ -= bound
		*prob -= *prob >> 5
	}
	e.normalize()
}

func (e *rangeEncoder) encodeDirect(value uint32, numBits int) {
	for i := numBits - 1; i >= 0; i-- {
		e.range_ >>= 1
		if (value>>uint(i))&1 != 0 {
			e.low += uint64(e.range_)
		}
		e.normalize()
	}
}

func (e *rangeEncoder) encodeBitTree(probs []uint16, numBits int, value uint32) {
	m := uint32(1)
	for i := numBits - 1; i >= 0; i-- {
		bit := int((value >> uint(i)) & 1)
		e.encodeBit(&probs[m], bit)
		m = (m << 1) | uint32(bit)
	}
}

func (e *rangeEncoder) encodeBitTreeReverse(probs []uint16, numBits int, value uint32) {
	m := uint32(1)
	for i := 0; i < numBits; i++ {
		bit := int(value & 1)
		e.encodeBit(&probs[m], bit)
		m = (m << 1) | uint32(bit)
		value >>= 1
	}
}

func (e *rangeEncoder) finish() {
	for i := 0; i < 5; i++ {
		e.shiftLow()
	}
}

// ── Length Encoder ───────────────────────────────────────────────────────────

type lzmaLenEncoder struct {
	choice  uint16
	choice2 uint16
	low     [lzmaNumPosStatesMax][]uint16
	mid     [lzmaNumPosStatesMax][]uint16
	high    []uint16
}

func newLzmaLenEncoder() *lzmaLenEncoder {
	le := &lzmaLenEncoder{}
	le.high = make([]uint16, 1<<lzmaLenNumHighBits)
	le.reset()
	return le
}

func (le *lzmaLenEncoder) reset() {
	le.choice = lzmaProbInitVal
	le.choice2 = lzmaProbInitVal
	for i := range le.low {
		le.low[i] = make([]uint16, 1<<lzmaLenNumLowBits)
		initProbs(le.low[i])
	}
	for i := range le.mid {
		le.mid[i] = make([]uint16, 1<<lzmaLenNumMidBits)
		initProbs(le.mid[i])
	}
	initProbs(le.high)
}

func (le *lzmaLenEncoder) encode(rc *rangeEncoder, length uint32, posState uint32) {
	length -= lzmaMatchLenMin
	if length < lzmaLenNumLowSyms {
		rc.encodeBit(&le.choice, 0)
		rc.encodeBitTree(le.low[posState], lzmaLenNumLowBits, length)
	} else {
		rc.encodeBit(&le.choice, 1)
		length -= lzmaLenNumLowSyms
		if length < lzmaLenNumMidSyms {
			rc.encodeBit(&le.choice2, 0)
			rc.encodeBitTree(le.mid[posState], lzmaLenNumMidBits, length)
		} else {
			rc.encodeBit(&le.choice2, 1)
			length -= lzmaLenNumMidSyms
			rc.encodeBitTree(le.high, lzmaLenNumHighBits, length)
		}
	}
}

// ── LZMA Encoder ────────────────────────────────────────────────────────────

type lzmaEncoder struct {
	rc       *rangeEncoder
	src      []byte
	pos      int
	dictSize int

	state uint32
	reps  [4]uint32

	lc, lp, pb uint32

	// Probability arrays (mirror decoder)
	isMatch    [lzmaNumStates << lzmaNumPosBitsMax]uint16
	isRep      [lzmaNumStates]uint16
	isRepG0    [lzmaNumStates]uint16
	isRepG1    [lzmaNumStates]uint16
	isRepG2    [lzmaNumStates]uint16
	isRep0Long [lzmaNumStates << lzmaNumPosBitsMax]uint16

	posSlot    [lzmaNumLenToPosStates][]uint16
	posSpecial [lzmaNumFullDistances - lzmaStartPosModelIndex]uint16
	posAlign   [lzmaAlignTableSize]uint16
	litProbs   []uint16

	matchLen *lzmaLenEncoder
	repLen   *lzmaLenEncoder

	// limit bounds the region encoded into the current LZMA2 chunk. Matches
	// may reach back before the chunk start — that is what keeps the
	// dictionary live across chunks — but never past limit.
	limit int

	// compLimit closes a chunk once the range coder has produced this many
	// bytes, keeping it inside the 16-bit compressed size field.
	compLimit int

	// depth caps how far the match finder walks a hash chain, and niceLen is
	// the match length at which the parser stops looking for better.
	depth   int
	niceLen int

	// matches is scratch reused by findMatchesAt.
	matches []lzmaMatch

	// BT4 match finder (binary tree + hash2/hash3/hash4 heads)
	cyclicSize int
	son        []int32 // [cyclicPos*2] left, [cyclicPos*2+1] right
	hash2Table []int32
	hash3Table []int32
	hashTable  []int32 // hash4 → newest position
	hashPos    int     // next position not yet indexed

	// Optimal parse buffers
	useOptimal   bool
	optBuf       []lzmaOptNode
	optMoves     []lzmaMove
	optMatchCache [][]lzmaMatch
}

const (
	lzmaHashBits = 16
	lzmaHashSize = 1 << lzmaHashBits
	lzmaMinMatch = 2
	lzmaMaxMatch = 273 // 2 + 8 + 8 + 256 - 1

	lzmaDefaultDepth   = 32
	lzmaDefaultNiceLen = 64
)

// Lzma2Options tunes the encoder's effort.
type Lzma2Options struct {
	// DictSize is the match window in bytes. Zero picks the package default.
	DictSize int
	// Depth caps the hash chain walk per position. Zero picks the default.
	Depth int
	// NiceLen is the match length at which the parser stops looking for a
	// better option. Zero picks the default.
	NiceLen int
	// OptimalParse enables the DP parser (7-Zip-style). It helps on some
	// solid/repetitive corpora but is slower; off by default until tuned per level.
	OptimalParse bool
}

func newLzmaEncoder(src []byte, dictSize int) *lzmaEncoder {
	enc := &lzmaEncoder{
		rc:       newRangeEncoder(),
		src:      src,
		dictSize: dictSize,
		limit:    len(src),
		depth:    lzmaDefaultDepth,
		niceLen:  lzmaDefaultNiceLen,
		lc:       3,
		lp:       0,
		pb:       2,
	}
	enc.matchLen = newLzmaLenEncoder()
	enc.repLen = newLzmaLenEncoder()
	for i := range enc.posSlot {
		enc.posSlot[i] = make([]uint16, 1<<6)
	}
	enc.litProbs = make([]uint16, 0x300<<(enc.lc+enc.lp))
	enc.hashTable = make([]int32, lzmaHashSize)
	for i := range enc.hashTable {
		enc.hashTable[i] = -1
	}
	enc.initBT4()
	enc.optBuf = make([]lzmaOptNode, lzmaOptimumWindow+1)
	enc.resetState()
	return enc
}

// resetState returns the probability models and LZMA state to their initial
// values, matching a decoder that saw a chunk flagged for a state reset. The
// match finder is deliberately untouched, so the dictionary stays live across
// chunk boundaries.
func (enc *lzmaEncoder) resetState() {
	initProbs(enc.isMatch[:])
	initProbs(enc.isRep[:])
	initProbs(enc.isRepG0[:])
	initProbs(enc.isRepG1[:])
	initProbs(enc.isRepG2[:])
	initProbs(enc.isRep0Long[:])
	initProbs(enc.posSpecial[:])
	initProbs(enc.posAlign[:])
	for i := range enc.posSlot {
		initProbs(enc.posSlot[i])
	}
	initProbs(enc.litProbs)
	enc.matchLen.reset()
	enc.repLen.reset()
	enc.state = 0
	enc.reps = [4]uint32{0, 0, 0, 0}
}

func (enc *lzmaEncoder) hash4(pos int) uint32 {
	if pos+4 > len(enc.src) {
		return 0
	}
	v := binary.LittleEndian.Uint32(enc.src[pos:])
	return (v * 0x9E3779B1) >> (32 - lzmaHashBits)
}

// advanceHash indexes every position up to but excluding end. The parser
// looks ahead of the position it has committed to, and those positions have
// to be in the chain before a match search there can see them. Positions are
// only ever inserted once; inserting one twice would make its chain entry
// point at itself.
func (enc *lzmaEncoder) advanceHash(end int) {
	if end > len(enc.src) {
		end = len(enc.src)
	}
	for p := enc.hashPos; p < end; p++ {
		enc.bt4Insert(p)
	}
	if end > enc.hashPos {
		enc.hashPos = end
	}
}

// findMatch finds the best match at current position using hash chain.
// Returns (distance, length). distance is 0-based (dist=0 means offset 1).
func (enc *lzmaEncoder) findMatch() (dist uint32, length int) {
	return enc.findMatchAt(enc.pos)
}

// lzmaMatch is one match candidate: a length and the nearest distance that
// achieves it.
type lzmaMatch struct {
	dist   uint32
	length int
}

// findMatchAt returns the longest match at pos.
func (enc *lzmaEncoder) findMatchAt(pos int) (dist uint32, length int) {
	m := enc.findMatchesAt(pos)
	if len(m) == 0 {
		return 0, 0
	}
	last := m[len(m)-1]
	return last.dist, last.length
}

// findMatchesAt collects the match candidates at pos, in increasing length,
// each at the nearest distance that reaches that length. Returning the whole
// frontier rather than only the longest lets the parser weigh a short nearby
// match against a longer distant one, which matters because distance is what
// dominates the encoded cost.
//
// The result aliases scratch space on the encoder and is valid until the next
// call. Positions up to pos must already be indexed; see advanceHash.
func (enc *lzmaEncoder) findMatchesAt(pos int) []lzmaMatch {
	return enc.bt4FindMatches(pos)
}

// findRepMatch checks if any of the 4 rep distances match at current position.
func (enc *lzmaEncoder) findRepMatch() (repIdx int, length int) {
	return enc.findRepMatchAt(enc.pos, &enc.reps)
}

func (enc *lzmaEncoder) findRepMatchAt(pos int, reps *[4]uint32) (repIdx int, length int) {
	remaining := enc.limit - pos
	if remaining < lzmaMinMatch {
		return -1, 0
	}

	bestLen := 1
	bestRep := -1
	maxLen := lzmaMaxMatch
	if remaining < maxLen {
		maxLen = remaining
	}

	for i := 0; i < 4; i++ {
		d := int(reps[i])
		if d >= pos || d < 0 {
			continue
		}
		cpos := pos - d - 1
		if cpos < 0 {
			continue
		}
		ml := zstdMatchLen(enc.src, pos, cpos)
		if ml > maxLen {
			ml = maxLen
		}
		if ml > bestLen {
			bestLen = ml
			bestRep = i
		}
	}

	if bestLen < lzmaMinMatch {
		return -1, 0
	}
	return bestRep, bestLen
}

func (enc *lzmaEncoder) encodeLiteral(b byte) {
	prevByte := byte(0)
	if enc.pos > 0 {
		prevByte = enc.src[enc.pos-1]
	}
	posState := uint32(enc.pos) & ((1 << enc.pb) - 1)

	enc.rc.encodeBit(&enc.isMatch[(enc.state<<lzmaNumPosBitsMax)+posState], 0)

	litState := uint32(prevByte>>(8-enc.lc)) | ((uint32(enc.pos) & ((1 << enc.lp) - 1)) << enc.lc)
	probs := enc.litProbs[litState*0x300:]

	if stateIsLit(enc.state) {
		// Simple literal
		symbol := uint32(1)
		for i := 7; i >= 0; i-- {
			bit := int((uint32(b) >> uint(i)) & 1)
			enc.rc.encodeBit(&probs[symbol], bit)
			symbol = (symbol << 1) | uint32(bit)
		}
	} else {
		// Match literal
		matchByte := uint32(0)
		if int(enc.reps[0]) < enc.pos {
			matchByte = uint32(enc.src[enc.pos-int(enc.reps[0])-1])
		}
		symbol := uint32(1)
		bval := uint32(b)
		for i := 7; i >= 0; i-- {
			bit := int((bval >> uint(i)) & 1)
			matchBit := (matchByte >> uint(i)) & 1
			idx := ((1 + matchBit) << 8) + symbol
			enc.rc.encodeBit(&probs[idx], bit)
			symbol = (symbol << 1) | uint32(bit)
			if matchBit != uint32(bit) {
				// Rest as normal
				for i--; i >= 0; i-- {
					bit = int((bval >> uint(i)) & 1)
					enc.rc.encodeBit(&probs[symbol], bit)
					symbol = (symbol << 1) | uint32(bit)
				}
				break
			}
		}
	}
	enc.state = lzmaNextState[enc.state][0]
	enc.pos++
	enc.advanceHash(enc.pos)
}

func (enc *lzmaEncoder) encodeMatch(dist uint32, length int) {
	posState := uint32(enc.pos) & ((1 << enc.pb) - 1)

	enc.rc.encodeBit(&enc.isMatch[(enc.state<<lzmaNumPosBitsMax)+posState], 1)
	enc.rc.encodeBit(&enc.isRep[enc.state], 0)

	enc.matchLen.encode(enc.rc, uint32(length), posState)

	// Encode distance
	lenState := getLenToPosState(uint32(length))
	distSlot := getDistSlot(dist)
	enc.rc.encodeBitTree(enc.posSlot[lenState], 6, distSlot)

	if distSlot >= lzmaStartPosModelIndex {
		numDirectBits := (distSlot >> 1) - 1
		base := (2 | (distSlot & 1)) << numDirectBits
		reduced := dist - base

		if distSlot < lzmaEndPosModelIndex {
			enc.encodeBitTreeReverseOffset(enc.posSpecial[:], int(base)-int(distSlot), int(numDirectBits), reduced)
		} else {
			// Direct bits (excluding align bits)
			enc.rc.encodeDirect(reduced>>lzmaNumAlignBits, int(numDirectBits)-lzmaNumAlignBits)
			enc.rc.encodeBitTreeReverse(enc.posAlign[:], lzmaNumAlignBits, reduced&((1<<lzmaNumAlignBits)-1))
		}
	}

	// Update reps
	enc.reps[3] = enc.reps[2]
	enc.reps[2] = enc.reps[1]
	enc.reps[1] = enc.reps[0]
	enc.reps[0] = dist
	enc.state = lzmaNextState[enc.state][1]

	enc.pos += length
	enc.advanceHash(enc.pos)
}

func (enc *lzmaEncoder) encodeRepMatch(repIdx int, length int) {
	posState := uint32(enc.pos) & ((1 << enc.pb) - 1)

	enc.rc.encodeBit(&enc.isMatch[(enc.state<<lzmaNumPosBitsMax)+posState], 1)
	enc.rc.encodeBit(&enc.isRep[enc.state], 1)

	if repIdx == 0 {
		enc.rc.encodeBit(&enc.isRepG0[enc.state], 0)
		if length == 1 {
			enc.rc.encodeBit(&enc.isRep0Long[(enc.state<<lzmaNumPosBitsMax)+posState], 0)
			enc.state = lzmaNextState[enc.state][3]
			enc.pos++
			enc.advanceHash(enc.pos)
			return
		}
		enc.rc.encodeBit(&enc.isRep0Long[(enc.state<<lzmaNumPosBitsMax)+posState], 1)
	} else {
		enc.rc.encodeBit(&enc.isRepG0[enc.state], 1)
		if repIdx == 1 {
			enc.rc.encodeBit(&enc.isRepG1[enc.state], 0)
		} else {
			enc.rc.encodeBit(&enc.isRepG1[enc.state], 1)
			if repIdx == 2 {
				enc.rc.encodeBit(&enc.isRepG2[enc.state], 0)
			} else {
				enc.rc.encodeBit(&enc.isRepG2[enc.state], 1)
			}
		}
		// Promote rep to front
		dist := enc.reps[repIdx]
		for i := repIdx; i > 0; i-- {
			enc.reps[i] = enc.reps[i-1]
		}
		enc.reps[0] = dist
	}

	enc.repLen.encode(enc.rc, uint32(length), posState)
	enc.state = lzmaNextState[enc.state][2]

	enc.pos += length
	enc.advanceHash(enc.pos)
}

func (enc *lzmaEncoder) encodeBitTreeReverseOffset(probs []uint16, offset int, numBits int, value uint32) {
	m := uint32(1)
	for i := 0; i < numBits; i++ {
		bit := int(value & 1)
		enc.rc.encodeBit(&probs[offset+int(m)], bit)
		m = (m << 1) | uint32(bit)
		value >>= 1
	}
}

// getDistSlot returns the distance slot for a given distance.
func getDistSlot(dist uint32) uint32 {
	if dist < 4 {
		return dist
	}
	// log2(dist) * 2 + bit
	nbits := uint32(31 - clz32(dist))
	return nbits*2 + ((dist >> (nbits - 1)) & 1)
}

// clz32 returns the number of leading zeros in a uint32.
func clz32(x uint32) int {
	if x == 0 {
		return 32
	}
	n := 0
	if x <= 0x0000FFFF {
		n += 16
		x <<= 16
	}
	if x <= 0x00FFFFFF {
		n += 8
		x <<= 8
	}
	if x <= 0x0FFFFFFF {
		n += 4
		x <<= 4
	}
	if x <= 0x3FFFFFFF {
		n += 2
		x <<= 2
	}
	if x <= 0x7FFFFFFF {
		n++
	}
	return n
}

func (enc *lzmaEncoder) encode() {
	if enc.useOptimal {
		enc.encodeOptimal()
		return
	}
	for enc.pos < enc.limit {
		if enc.compLimit > 0 && len(enc.rc.out) >= enc.compLimit {
			return
		}
		enc.step()
	}
}

// lzmaCompressBlock compresses data into raw LZMA stream (range coded).
// Output starts with the range encoder init byte (0x00) + encoded data.
func lzmaCompressBlock(data []byte, dictSize int) ([]byte, error) {
	if len(data) == 0 {
		// Empty: just range encoder init + EOS marker
		enc := newRangeEncoder()
		enc.finish()
		return enc.out, nil
	}

	lenc := newLzmaEncoder(data, dictSize)
	lenc.encode()
	lenc.rc.finish()
	return lenc.rc.out, nil
}
