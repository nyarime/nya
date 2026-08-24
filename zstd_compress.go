package nya

// Pure Go Zstandard (RFC 8878) compressor.
// Produces valid zstd frames decodable by any compliant decoder.
// Strategy: raw literals + predefined FSE tables for sequences.
// Falls back to raw blocks when compression doesn't help.

import (
	"encoding/binary"
	"runtime"
	"sync"
)

// ZstdCompress compresses data using Zstandard format (RFC 8878).
// level: 1 (fastest) to 19 (best compression). Default 3 if 0.
func ZstdCompress(src []byte, level int) []byte {
	if len(src) > 512*1024 {
		return ZstdCompressWithWindow(src, level)
	}
	return ZstdCompressWithDict(src, level, nil)
}

// ZstdCompressWithDict compresses with a dictionary prefix.
// The dict bytes are prepended to the match-finding window so the encoder
// can reference them, but only src bytes appear in the output frame.
func ZstdCompressWithDict(src []byte, level int, dict []byte) []byte {
	if level <= 0 {
		level = 3
	}
	if level > 19 {
		level = 19
	}

	if len(src) == 0 {
		return zstdEmptyFrame()
	}
	if len(src) <= 16 && len(dict) == 0 {
		return zstdRawFrame(src)
	}
	if len(dict) == 0 {
		return zstdCompressFrame(src, level)
	}
	return zstdCompressFrameWithDict(src, level, dict)
}

// zstdCompressFrameWithDict compresses src as a single zstd frame, using dict
// as a prefix window for match finding. The dict is NOT included in output.
func zstdCompressFrameWithDict(src []byte, level int, dict []byte) []byte {
	out := make([]byte, 0, len(src)+64)
	out = zstdAppendU32(out, zstdMagic)

	// Window descriptor needed (not single-segment) because decoded size
	// and window size may differ when dict is used. However, for simplicity
	// and since our decoder handles it, we use single-segment with just src size.
	fhd, fcs := zstdMakeFCS(uint64(len(src)))
	fhd |= 1 << 5 // single segment
	out = append(out, fhd)
	out = append(out, fcs...)

	// Compress as single block with dict prefix
	const maxBlock = 128 * 1024
	var enc zstdEncoderState
	// Seed window with dict
	enc.seedWindow(dict)

	for i := 0; i < len(src); {
		end := i + maxBlock
		if end > len(src) {
			end = len(src)
		}
		last := end == len(src)
		block := src[i:end]

		compressed := enc.compressBlock(block, level)
		if compressed != nil && len(compressed) < len(block) {
			bh := uint32(2<<1) | (uint32(len(compressed)) << 3)
			if last {
				bh |= 1
			}
			out = append(out, byte(bh), byte(bh>>8), byte(bh>>16))
			out = append(out, compressed...)
		} else {
			bh := uint32(len(block)) << 3
			if last {
				bh |= 1
			}
			out = append(out, byte(bh), byte(bh>>8), byte(bh>>16))
			out = append(out, block...)
			// Still update window for subsequent blocks
			enc.advanceWindow(block)
		}
		i = end
	}
	return out
}

func zstdEmptyFrame() []byte {
	// Frame with single empty raw block
	var buf [13]byte
	binary.LittleEndian.PutUint32(buf[0:4], zstdMagic)
	buf[4] = 1 << 5 // single segment, FCS_Field_Size=0 → 1 byte
	buf[5] = 0      // FCS = 0
	// Last raw block, size 0: last=1, type=raw(0), size=0
	buf[6] = 1
	buf[7] = 0
	buf[8] = 0
	return buf[:9]
}

func zstdRawFrame(src []byte) []byte {
	out := make([]byte, 0, 4+2+3+len(src))
	out = zstdAppendU32(out, zstdMagic)
	fhd, fcs := zstdMakeFCS(uint64(len(src)))
	fhd |= 1 << 5 // single segment
	out = append(out, fhd)
	out = append(out, fcs...)
	bh := uint32(1) | (uint32(len(src)) << 3) // last=1, type=raw(0), size
	out = append(out, byte(bh), byte(bh>>8), byte(bh>>16))
	out = append(out, src...)
	return out
}

// ZstdCompressWithWindow compresses data using inter-block windowed matching.
// Blocks within the frame can reference data from previous blocks via a
// sliding window, improving compression for data with cross-block repetition.
func ZstdCompressWithWindow(src []byte, level int) []byte {
	if level <= 0 {
		level = 3
	}
	if level > 19 {
		level = 19
	}
	if len(src) == 0 {
		return zstdEmptyFrame()
	}
	if len(src) <= 16 {
		return zstdRawFrame(src)
	}
	return zstdCompressFrameWindowed(src, level)
}

func zstdCompressFrame(src []byte, level int) []byte {
	out := make([]byte, 0, len(src)+64)
	out = zstdAppendU32(out, zstdMagic)
	fhd, fcs := zstdMakeFCS(uint64(len(src)))
	fhd |= 1 << 5 // single segment
	out = append(out, fhd)
	out = append(out, fcs...)

	const maxBlock = 128 * 1024 // 512KB for better LZ77 match finding
	nBlocks := (len(src) + maxBlock - 1) / maxBlock

	// For large inputs, compress blocks in parallel
	if len(src) >= 256*1024 && nBlocks > 1 {
		type blockResult struct {
			compressed []byte
			isRaw      bool
		}
		results := make([]blockResult, nBlocks)
		nWorkers := runtime.NumCPU()
		if nWorkers > nBlocks {
			nWorkers = nBlocks
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, nWorkers)

		for i := 0; i < nBlocks; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				start := idx * maxBlock
				end := start + maxBlock
				if end > len(src) {
					end = len(src)
				}
				block := src[start:end]
				comp := zstdTryCompressBlock(block, level)
				if comp != nil && len(comp) < len(block) {
					results[idx] = blockResult{compressed: comp}
				} else {
					results[idx] = blockResult{compressed: block, isRaw: true}
				}
			}(i)
		}
		wg.Wait()

		for i, r := range results {
			last := i == nBlocks-1
			if r.isRaw {
				bh := uint32(len(r.compressed)) << 3
				if last {
					bh |= 1
				}
				out = append(out, byte(bh), byte(bh>>8), byte(bh>>16))
			} else {
				bh := uint32(2<<1) | (uint32(len(r.compressed)) << 3)
				if last {
					bh |= 1
				}
				out = append(out, byte(bh), byte(bh>>8), byte(bh>>16))
			}
			out = append(out, r.compressed...)
		}
	} else {
		// Single-threaded path for small inputs
		for i := 0; i < len(src); {
			end := i + maxBlock
			if end > len(src) {
				end = len(src)
			}
			last := end == len(src)
			block := src[i:end]

			compressed := zstdTryCompressBlock(block, level)
			if compressed != nil && len(compressed) < len(block) {
				bh := uint32(2<<1) | (uint32(len(compressed)) << 3)
				if last {
					bh |= 1
				}
				out = append(out, byte(bh), byte(bh>>8), byte(bh>>16))
				out = append(out, compressed...)
			} else {
				bh := uint32(len(block)) << 3
				if last {
					bh |= 1
				}
				out = append(out, byte(bh), byte(bh>>8), byte(bh>>16))
				out = append(out, block...)
			}
			i = end
		}
	}
	return out
}

// zstdCompressFrameWindowed compresses with inter-block window matching.
func zstdCompressFrameWindowed(src []byte, level int) []byte {
	out := make([]byte, 0, len(src)+64)
	out = zstdAppendU32(out, zstdMagic)
	fhd, fcs := zstdMakeFCS(uint64(len(src)))
	fhd |= 1 << 5 // single segment
	out = append(out, fhd)
	out = append(out, fcs...)

	const maxBlock = 128 * 1024
	var enc zstdEncoderState

	for i := 0; i < len(src); {
		end := i + maxBlock
		if end > len(src) {
			end = len(src)
		}
		last := end == len(src)
		block := src[i:end]

		compressed := enc.compressBlock(block, level)
		if compressed != nil && len(compressed) < len(block) {
			bh := uint32(2<<1) | (uint32(len(compressed)) << 3)
			if last {
				bh |= 1
			}
			out = append(out, byte(bh), byte(bh>>8), byte(bh>>16))
			out = append(out, compressed...)
		} else {
			bh := uint32(len(block)) << 3
			if last {
				bh |= 1
			}
			out = append(out, byte(bh), byte(bh>>8), byte(bh>>16))
			out = append(out, block...)
			// compressBlock already added block to window, don't double-add
		}
		i = end
	}
	return out
}

func zstdMakeFCS(size uint64) (fhd byte, fcs []byte) {
	switch {
	case size <= 255:
		return 0, []byte{byte(size)}
	case size <= 65535+256:
		v := uint16(size - 256)
		return 1 << 6, []byte{byte(v), byte(v >> 8)}
	case size <= 0xFFFFFFFF:
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(size))
		return 2 << 6, b
	default:
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, size)
		return 3 << 6, b
	}
}

func zstdAppendU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// ──────────────────────────── block compression ────────────────────────────

type zstdSeq struct {
	litLen   int
	offset   int // actual offset (positive)
	matchLen int
}

func zstdTryCompressBlock(src []byte, level int) []byte {
	if len(src) < 8 {
		return nil
	}
	seqs := zstdFindSeqs(src, level)
	if len(seqs) == 0 {
		return nil
	}
	result := zstdBuildBlock(src, seqs)
	if result == nil {
		return nil
	}
	// Safety: build a minimal frame and verify roundtrip
	var verify []byte
	verify = zstdAppendU32(verify, zstdMagic)
	if len(src) <= 255 {
		verify = append(verify, 0x20) // single segment, fcs=0 (1 byte)
		verify = append(verify, byte(len(src)))
	} else {
		verify = append(verify, 0x60) // single segment, fcs=1 (2 bytes)
		sz := uint16(len(src) - 256)
		verify = append(verify, byte(sz), byte(sz>>8))
	}
	bh := uint32(1) | uint32(2<<1) | (uint32(len(result)) << 3)
	verify = append(verify, byte(bh), byte(bh>>8), byte(bh>>16))
	verify = append(verify, result...)
	dec, err := ZstdDecompress(verify)
	if err != nil || !bytesEqualFast(dec, src) {
		return nil // fall back to raw block
	}
	return result
}

func bytesEqualFast(a, b []byte) bool {
	if len(a) != len(b) { return false }
	for i := range a { if a[i] != b[i] { return false } }
	return true
}

// ──────────────────────────── LZ77 match finder ────────────────────────────

const (
	zcMinMatch  = 4
	zcHashLog   = 16
	zcHashMask  = (1 << zcHashLog) - 1
	zcMaxOff    = 4 << 20 // 4MB - but limited by block size in practice
	zcMaxMatch  = 131074  // ML code 52: baseline 65539 + 16 bits (65535)
	zcMaxLitLen = 131071  // LL code 35: baseline 65536 + 16 bits (65535)
)

func zcHash4(b []byte) uint32 {
	v := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	return (v * 2654435761) >> (32 - zcHashLog)
}

func zstdFindSeqs(src []byte, level int) []zstdSeq {
	n := len(src)
	if n < zcMinMatch {
		return nil
	}

	// Simple hash table (no chaining for now — just single-entry)
	ht := make([]int32, 1<<zcHashLog)
	for i := range ht {
		ht[i] = -1
	}

	var seqs []zstdSeq
	pos := 0
	litStart := 0
	repeatOffsets := [3]int{1, 4, 8} // initial repeat offsets per Zstd spec

	for pos+zcMinMatch <= n {
		// Try repeat offsets first — they're much cheaper to encode
		repMatch := -1
		repML := 0
		for ri, roff := range repeatOffsets {
			if pos >= roff && pos-roff >= 0 {
				ml := zstdMatchLen(src, pos, pos-roff)
				if ml >= zcMinMatch && ml > repML {
					repMatch = ri
					repML = ml
				}
			}
		}

		// Check hash table match too, to compare
		h := zcHash4(src[pos:])
		cand := int(ht[h])
		ht[h] = int32(pos)
		hashML := 0
		if cand >= 0 && pos-cand <= zcMaxOff && pos-cand > 0 {
			hashML = zstdMatchLen(src, pos, cand)
			if hashML < zcMinMatch {
				hashML = 0
			}
		}

		// Prefer repeat offset if it's at least as good as hash match
		// (repeat offsets are much cheaper to encode, so bias towards them)
		if repMatch >= 0 && repML >= zcMinMatch && (hashML == 0 || repML >= hashML-2) {
			ml := repML
			if ml > zcMaxMatch {
				ml = zcMaxMatch
			}
			off := repeatOffsets[repMatch]

			seqs = append(seqs, zstdSeq{
				litLen:   pos - litStart,
				offset:   off,
				matchLen: ml,
			})

			switch repMatch {
			case 0:
				// no change
			case 1:
				repeatOffsets[0], repeatOffsets[1] = repeatOffsets[1], repeatOffsets[0]
			case 2:
				repeatOffsets[0], repeatOffsets[1], repeatOffsets[2] = repeatOffsets[2], repeatOffsets[0], repeatOffsets[1]
			}

			end := pos + ml
			pos++
			for pos < end && pos+zcMinMatch <= n {
				h := zcHash4(src[pos:])
				ht[h] = int32(pos)
				pos++
			}
			if pos < end {
				pos = end
			}
			litStart = pos
			continue
		}

		if cand < 0 || pos-cand > zcMaxOff || pos-cand <= 0 || hashML < zcMinMatch {
			pos++
			continue
		}

		ml := hashML

		// Lazy matching for level >= 3
		if level >= 3 && pos+1+zcMinMatch <= n {
			h2 := zcHash4(src[pos+1:])
			cand2 := int(ht[h2])
			ht[h2] = int32(pos + 1)
			if cand2 >= 0 && pos+1-cand2 <= zcMaxOff && pos+1-cand2 > 0 {
				ml2 := zstdMatchLen(src, pos+1, cand2)
				if ml2 > ml {
					pos++
					ml = ml2
					cand = cand2
				}
			}
		}

		// Cap match length to max encodable
		if ml > zcMaxMatch {
			ml = zcMaxMatch
		}

		off := pos - cand
		seqs = append(seqs, zstdSeq{
			litLen:   pos - litStart,
			offset:   off,
			matchLen: ml,
		})

		// Update repeat offsets: new offset
		repeatOffsets[2] = repeatOffsets[1]
		repeatOffsets[1] = repeatOffsets[0]
		repeatOffsets[0] = off

		// Advance past match, updating hash table
		end := pos + ml
		pos++
		for pos < end && pos+zcMinMatch <= n {
			h := zcHash4(src[pos:])
			ht[h] = int32(pos)
			pos++
		}
		if pos < end {
			pos = end
		}
		litStart = pos
	}
	return seqs
}

// ──────────────────────────── windowed encoder state ────────────────────────────

const zcWindowMax = 4 * 1024 * 1024 // 4MB sliding window

// zstdEncoderState maintains a sliding window across blocks within a frame,
// allowing inter-block match references. The zstd frame format already supports
// this — the decoder maintains the full decompressed output as a window.
type zstdEncoderState struct {
	window        []byte   // sliding window of recent uncompressed data
	ht            []int32  // persistent hash table
	htReady       bool
	repeatOffsets [3]int   // repeat offsets persisted across blocks
}

// seedWindow initialises the window with dictionary/prefix data without
// producing any output. Used for external dictionary mode.
func (s *zstdEncoderState) seedWindow(dict []byte) {
	if len(dict) == 0 {
		return
	}
	s.ensureHT()
	if len(dict) > zcWindowMax {
		dict = dict[len(dict)-zcWindowMax:]
	}
	s.window = append(s.window[:0], dict...)
	// Hash the dict so matches can be found
	for i := 0; i+zcMinMatch <= len(dict); i++ {
		h := zcHash4(dict[i:])
		s.ht[h] = int32(i)
	}
}

// advanceWindow updates the window with block data without compressing.
// Used when a block falls back to raw (uncompressed) but we still want
// subsequent blocks to reference it.
func (s *zstdEncoderState) advanceWindow(block []byte) {
	s.ensureHT()
	wOff := len(s.window)
	s.window = append(s.window, block...)
	// Hash the new data
	for i := wOff; i+zcMinMatch <= len(s.window); i++ {
		h := zcHash4(s.window[i:])
		s.ht[h] = int32(i)
	}
	s.trimWindow()
}

func (s *zstdEncoderState) ensureHT() {
	if !s.htReady {
		s.ht = make([]int32, 1<<zcHashLog)
		for i := range s.ht {
			s.ht[i] = -1
		}
		s.htReady = true
	}
}

func (s *zstdEncoderState) trimWindow() {
	if len(s.window) > zcWindowMax {
		trim := len(s.window) - zcWindowMax
		copy(s.window, s.window[trim:])
		s.window = s.window[:zcWindowMax]
		// Adjust hash entries
		for i := range s.ht {
			s.ht[i] -= int32(trim)
			if s.ht[i] < 0 {
				s.ht[i] = -1
			}
		}
	}
}

// compressBlock compresses a block using the sliding window for match finding.
// Matches can reference data from previous blocks. The returned bytes are the
// compressed block content (without block header), or nil if incompressible.
func (s *zstdEncoderState) compressBlock(block []byte, level int) []byte {
	if len(block) < 8 {
		// Still need to update window for subsequent blocks
		s.advanceWindow(block)
		return nil
	}
	s.ensureHT()

	wOff := len(s.window) // offset where this block starts in window
	s.window = append(s.window, block...)

	// Hash the new block data
	for i := wOff; i+zcMinMatch <= len(s.window); i++ {
		h := zcHash4(s.window[i:])
		s.ht[h] = int32(i)
	}

	// Find sequences in the full window, but only emit for positions >= wOff
	seqs := s.findSeqsWindowed(wOff, level)

	if len(seqs) == 0 {
		// Window already has the block data and hash entries, just trim
		s.trimWindow()
		return nil
	}

	result := zstdBuildBlock(block, seqs, s.repeatOffsets)
	if result == nil {
		s.trimWindow()
		return nil
	}

	// Verify roundtrip: build a mini-frame containing window prefix (raw) + compressed block
	{
		var verify []byte
		verify = zstdAppendU32(verify, zstdMagic)
		totalSize := wOff + len(block)
		if totalSize <= 255 {
			verify = append(verify, 0x20)
			verify = append(verify, byte(totalSize))
		} else if totalSize <= 65791 {
			verify = append(verify, 0x60)
			sz := uint16(totalSize - 256)
			verify = append(verify, byte(sz), byte(sz>>8))
		} else {
			verify = append(verify, 0xA0)
			sz := uint32(totalSize)
			verify = append(verify, byte(sz), byte(sz>>8), byte(sz>>16), byte(sz>>24))
		}
		// Block 1: raw window prefix
		if wOff > 0 {
			winData := s.window[:wOff]
			for len(winData) > 0 {
				chunk := winData
				if len(chunk) > 128*1024 { chunk = winData[:128*1024] }
				bh := uint32(len(chunk)) << 3 // raw block
				verify = append(verify, byte(bh), byte(bh>>8), byte(bh>>16))
				verify = append(verify, chunk...)
				winData = winData[len(chunk):]
			}
		}
		// Block 2: our compressed block (last)
		bh := uint32(1) | uint32(2<<1) | (uint32(len(result)) << 3)
		verify = append(verify, byte(bh), byte(bh>>8), byte(bh>>16))
		verify = append(verify, result...)
		dec, err := ZstdDecompress(verify)
		expected := make([]byte, totalSize)
		copy(expected, s.window[:wOff])
		copy(expected[wOff:], block)
		if err != nil || !bytesEqualFast(dec, expected) {
			s.trimWindow()
			return nil
		}
	}

	s.trimWindow()
	return result
}

// findSeqsWindowed finds LZ77 sequences in the window starting at srcOff.
// Matches can reference any position in window[0:srcOff+blockLen].
// Emitted offsets are distances from the current position (zstd convention).
func (s *zstdEncoderState) findSeqsWindowed(srcOff int, level int) []zstdSeq {
	win := s.window
	n := len(win)
	blockEnd := n

	if srcOff+zcMinMatch > n {
		return nil
	}

	var seqs []zstdSeq
	pos := srcOff
	litStart := srcOff
	repeatOffsets := s.repeatOffsets; if repeatOffsets == ([3]int{}) { repeatOffsets = [3]int{1, 4, 8} }

	for pos+zcMinMatch <= blockEnd {
		// Try repeat offsets first
		repMatch := -1
		repML := 0
		for ri, roff := range repeatOffsets {
			if pos >= roff && pos-roff >= 0 {
				ml := zstdMatchLenWindow(win, pos, pos-roff, blockEnd)
				if ml >= zcMinMatch && ml > repML {
					repMatch = ri
					repML = ml
				}
			}
		}

		// Check hash table match
		h := zcHash4(win[pos:])
		cand := int(s.ht[h])
		s.ht[h] = int32(pos)
		hashML := 0
		if cand >= 0 && pos-cand <= zcMaxOff && pos-cand > 0 {
			hashML = zstdMatchLenWindow(win, pos, cand, blockEnd)
			if hashML < zcMinMatch {
				hashML = 0
			}
		}

		if repMatch >= 0 && repML >= zcMinMatch && (hashML == 0 || repML >= hashML-2) {
			ml := repML
			if ml > zcMaxMatch {
				ml = zcMaxMatch
			}
			off := repeatOffsets[repMatch]

			seqs = append(seqs, zstdSeq{
				litLen:   pos - litStart,
				offset:   off,
				matchLen: ml,
			})

			switch repMatch {
			case 0:
				// no change
			case 1:
				repeatOffsets[0], repeatOffsets[1] = repeatOffsets[1], repeatOffsets[0]
			case 2:
				repeatOffsets[0], repeatOffsets[1], repeatOffsets[2] = repeatOffsets[2], repeatOffsets[0], repeatOffsets[1]
			}

			mEnd := pos + ml
			pos++
			for pos < mEnd && pos+zcMinMatch <= blockEnd {
				h := zcHash4(win[pos:])
				s.ht[h] = int32(pos)
				pos++
			}
			if pos < mEnd {
				pos = mEnd
			}
			litStart = pos
			continue
		}

		if cand < 0 || pos-cand > zcMaxOff || pos-cand <= 0 || hashML < zcMinMatch {
			pos++
			continue
		}

		ml := hashML

		// Lazy matching
		if level >= 3 && pos+1+zcMinMatch <= blockEnd {
			h2 := zcHash4(win[pos+1:])
			cand2 := int(s.ht[h2])
			s.ht[h2] = int32(pos + 1)
			if cand2 >= 0 && pos+1-cand2 <= zcMaxOff && pos+1-cand2 > 0 {
				ml2 := zstdMatchLenWindow(win, pos+1, cand2, blockEnd)
				if ml2 > ml {
					pos++
					ml = ml2
					cand = cand2
				}
			}
		}

		if ml > zcMaxMatch {
			ml = zcMaxMatch
		}

		off := pos - cand
		seqs = append(seqs, zstdSeq{
			litLen:   pos - litStart,
			offset:   off,
			matchLen: ml,
		})

		// Update repeat offsets: new offset
		repeatOffsets[2] = repeatOffsets[1]
		repeatOffsets[1] = repeatOffsets[0]
		repeatOffsets[0] = off

		mEnd := pos + ml
		pos++
		for pos < mEnd && pos+zcMinMatch <= blockEnd {
			h := zcHash4(win[pos:])
			s.ht[h] = int32(pos)
			pos++
		}
		if pos < mEnd {
			pos = mEnd
		}
		litStart = pos
	}

	s.repeatOffsets = repeatOffsets
	return seqs
}

// zstdMatchLenWindow computes match length within window, bounded by limit.
func zstdMatchLenWindow(win []byte, pos, matchPos, limit int) int {
	maxL := limit - pos
	if r := limit - matchPos; r < maxL {
		maxL = r
	}
	l := 0
	for l < maxL && win[pos+l] == win[matchPos+l] {
		l++
	}
	return l
}



// ──────────────────────────── block building ────────────────────────────

func zstdBuildBlock(src []byte, seqs []zstdSeq, offsets ...[3]int) []byte {
	// Collect literals
	var lits []byte
	pos := 0
	for _, s := range seqs {
		lits = append(lits, src[pos:pos+s.litLen]...)
		pos += s.litLen + s.matchLen
	}
	// Trailing literals after last match
	lits = append(lits, src[pos:]...)

	// Encode sequences using predefined FSE tables (mode 0)
	// This avoids having to encode FSE table headers.

	// Process offsets through repeat-offset logic to get coded offset values
	offHist := [3]int{1, 4, 8}; if len(offsets) > 0 { offHist = offsets[0] }
	type codedSeq struct {
		llCode  int
		llExtra uint32
		llBits  int
		mlCode  int
		mlExtra uint32
		mlBits  int
		ofCode  int
		ofExtra uint32
		ofBits  int
	}
	coded := make([]codedSeq, len(seqs))

	for i, s := range seqs {
		coded[i].llCode, coded[i].llBits, coded[i].llExtra = zcLLCode(s.litLen)
		coded[i].mlCode, coded[i].mlBits, coded[i].mlExtra = zcMLCode(s.matchLen)

		// Offset coding with repeat offset detection
		off := s.offset
		repIdx := -1
		for j := 0; j < 3; j++ {
			if off == offHist[j] {
				repIdx = j
				break
			}
		}

		if repIdx >= 0 && s.litLen > 0 {
			// Repeated offset with litLen > 0: code = repIdx+1 (1,2,3)
			codedOff := repIdx + 1
			coded[i].ofCode, coded[i].ofBits, coded[i].ofExtra = zcOFCode(codedOff)
			if repIdx > 0 {
				tmp := offHist[repIdx]
				copy(offHist[1:repIdx+1], offHist[:repIdx])
				offHist[0] = tmp
			}
		} else if s.litLen == 0 {
			// litLen==0: repeat offset meanings shift per RFC 8878 §3.1.2.5
			// code 1 → offHist[1], code 2 → offHist[2], code 3 → offHist[0]-1
			if off == offHist[1] {
				coded[i].ofCode, coded[i].ofBits, coded[i].ofExtra = zcOFCode(1)
				offHist[1] = offHist[0]
				offHist[0] = off
			} else if off == offHist[2] {
				coded[i].ofCode, coded[i].ofBits, coded[i].ofExtra = zcOFCode(2)
				tmp := offHist[2]
				offHist[2] = offHist[1]
				offHist[1] = offHist[0]
				offHist[0] = tmp
			} else if off == offHist[0]-1 && offHist[0] > 1 {
				coded[i].ofCode, coded[i].ofBits, coded[i].ofExtra = zcOFCode(3)
				offHist[2] = offHist[1]
				offHist[1] = offHist[0]
				offHist[0] = off
			} else {
				codedOff := off + 3
				coded[i].ofCode, coded[i].ofBits, coded[i].ofExtra = zcOFCode(codedOff)
				offHist[2] = offHist[1]
				offHist[1] = offHist[0]
				offHist[0] = off
			}
		} else {
			// Explicit offset: coded value = offset + 3
			codedOff := off + 3
			coded[i].ofCode, coded[i].ofBits, coded[i].ofExtra = zcOFCode(codedOff)
			offHist[2] = offHist[1]
			offHist[1] = offHist[0]
			offHist[0] = off
		}
	}

	// Check if we can use RLE mode for any table
	nbSeq := len(seqs)
	llSame, ofSame, mlSame := true, true, true
	for i := 1; i < nbSeq; i++ {
		if coded[i].llCode != coded[0].llCode {
			llSame = false
		}
		if coded[i].ofCode != coded[0].ofCode {
			ofSame = false
		}
		if coded[i].mlCode != coded[0].mlCode {
			mlSame = false
		}
	}

	// Determine modes: RLE (1) if all same, try Custom FSE (2), fallback Predefined (0).
	//
	// Custom tables are disabled: the serialisation below round-trips through
	// this package's own readFSETable but is rejected by conformant zstd
	// decoders, which would make our frames unreadable elsewhere. Predefined
	// and RLE tables cost roughly 1% of ratio and interoperate. Re-enable once
	// zcBuildCustomFSEEncoder emits spec-compliant tables.
	const useCustomFSE = false

	var llMode, ofMode, mlMode byte
	var llCustomHdr, ofCustomHdr, mlCustomHdr []byte
	var llCustomTbl, ofCustomTbl, mlCustomTbl *zcFSECustomTable

	if llSame {
		llMode = 1
	} else if useCustomFSE && nbSeq >= 16 {
		// Try custom FSE table for LL
		llFreqs := make(map[byte]int)
		for _, c := range coded { llFreqs[byte(c.llCode)]++ }
		if hdr, tbl, err := zcBuildCustomFSEEncoder(llFreqs, 9, 35); err == nil && len(hdr) > 0 {
			// Verify: decoder can parse our header (maxLog=9 matches decoder)
			if _, _, parseErr := zstdBuildFSETableFromHeader(hdr, 9); parseErr == nil {
				llMode = 2
				llCustomHdr = hdr
				llCustomTbl = tbl
			}
		}
	}

	if ofSame {
		ofMode = 1
	} else if useCustomFSE && nbSeq >= 16 {
		ofFreqs := make(map[byte]int)
		for _, c := range coded { ofFreqs[byte(c.ofCode)]++ }
		if hdr, tbl, err := zcBuildCustomFSEEncoder(ofFreqs, 8, 28); err == nil && len(hdr) > 0 {
			if _, _, parseErr := zstdBuildFSETableFromHeader(hdr, 8); parseErr == nil {
				ofMode = 2
				ofCustomHdr = hdr
				ofCustomTbl = tbl
			}
		}
	}

	if mlSame {
		mlMode = 1
	} else if useCustomFSE && nbSeq >= 16 {
		mlFreqs := make(map[byte]int)
		for _, c := range coded { mlFreqs[byte(c.mlCode)]++ }
		if hdr, tbl, err := zcBuildCustomFSEEncoder(mlFreqs, 9, 52); err == nil && len(hdr) > 0 {
			if _, _, parseErr := zstdBuildFSETableFromHeader(hdr, 9); parseErr == nil {
				mlMode = 2
				mlCustomHdr = hdr
				mlCustomTbl = tbl
			}
		}
	}

	// For predefined mode we need the predefined tables
	if !llSame || !ofSame || !mlSame {
		zcInitPredTables()
	}

	// Sequence count header
	var seqHdr []byte
	if nbSeq < 128 {
		seqHdr = append(seqHdr, byte(nbSeq))
	} else if nbSeq < 0x7F00 {
		seqHdr = append(seqHdr, byte((nbSeq>>8)+128), byte(nbSeq))
	} else {
		seqHdr = append(seqHdr, 255, byte(nbSeq-0x7F00), byte((nbSeq-0x7F00)>>8))
	}

	// Mode byte
	seqHdr = append(seqHdr, (llMode<<6)|(ofMode<<4)|(mlMode<<2))

	// RLE symbol bytes or FSE table headers
	if llMode == 1 {
		seqHdr = append(seqHdr, byte(coded[0].llCode))
	} else if llMode == 2 {
		seqHdr = append(seqHdr, llCustomHdr...)
	}
	if ofMode == 1 {
		seqHdr = append(seqHdr, byte(coded[0].ofCode))
	} else if ofMode == 2 {
		seqHdr = append(seqHdr, ofCustomHdr...)
	}
	if mlMode == 1 {
		seqHdr = append(seqHdr, byte(coded[0].mlCode))
	} else if mlMode == 2 {
		seqHdr = append(seqHdr, mlCustomHdr...)
	}


	// Build bitstream using backward state computation (tANS encoding).
	//
	// FSE/tANS encoding works BACKWARDS: we process sequences from last to
	// first. For each transition, we know the TARGET state (for the next
	// symbol in decode order) and find a source state for the current symbol
	// that can reach it. The bits emitted are derived from this.
	//
	// Decoder reads (from high bit positions down):
	//   init states: LL, OF, ML
	//   for each seq i=0..n-1:
	//     extras: OF, ML, LL
	//     if not last: state updates: LL, ML, OF

	type transitionInfo struct {
		llBitsN, mlBitsN, ofBitsN int
		llBitsV, mlBitsV, ofBitsV int
	}

	transitions := make([]transitionInfo, nbSeq)

	// Backward pass: compute states from last sequence to first.
	// Start with an arbitrary state for the last symbol, then work backwards.
	// For transition i (connecting seq[i] → seq[i+1] in decode order):
	//   We know the target state (for seq[i+1]). We need to find a state for
	//   seq[i] such that newState[state] + val == targetState, with val in
	//   [0, 1<<numBits[state]).

	var llState, ofState, mlState int

	// Pick states for the last sequence
	if !llSame {
		if llMode == 2 {
			llState = zcFindStateCustom(llCustomTbl, coded[nbSeq-1].llCode)
		} else {
			llState = zcFindState(&zcPredLL, coded[nbSeq-1].llCode)
		}
	}
	if !ofSame {
		if ofMode == 2 {
			ofState = zcFindStateCustom(ofCustomTbl, coded[nbSeq-1].ofCode)
		} else {
			ofState = zcFindState(&zcPredOF, coded[nbSeq-1].ofCode)
		}
	}
	if !mlSame {
		if mlMode == 2 {
			mlState = zcFindStateCustom(mlCustomTbl, coded[nbSeq-1].mlCode)
		} else {
			mlState = zcFindState(&zcPredML, coded[nbSeq-1].mlCode)
		}
	}

	// Work backwards: for each transition i (seq[i] → seq[i+1]),
	// find state for seq[i] that reaches the current state for seq[i+1].
	for i := nbSeq - 2; i >= 0; i-- {
		var ti transitionInfo
		if !llSame {
			if llMode == 2 {
				state, nb, val := zcFindNextStateCustom(llCustomTbl, llState, coded[i].llCode)
				ti.llBitsN = nb
				ti.llBitsV = val
				llState = state
			} else {
				state, nb, val := zcEncodeTransition(&zcPredLL, coded[i].llCode, llState)
				ti.llBitsN = nb
				ti.llBitsV = val
				llState = state
			}
		}
		if !mlSame {
			if mlMode == 2 {
				state, nb, val := zcFindNextStateCustom(mlCustomTbl, mlState, coded[i].mlCode)
				ti.mlBitsN = nb
				ti.mlBitsV = val
				mlState = state
			} else {
				state, nb, val := zcEncodeTransition(&zcPredML, coded[i].mlCode, mlState)
				ti.mlBitsN = nb
				ti.mlBitsV = val
				mlState = state
			}
		}
		if !ofSame {
			if ofMode == 2 {
				state, nb, val := zcFindNextStateCustom(ofCustomTbl, ofState, coded[i].ofCode)
				ti.ofBitsN = nb
				ti.ofBitsV = val
				ofState = state
			} else {
				state, nb, val := zcEncodeTransition(&zcPredOF, coded[i].ofCode, ofState)
				ti.ofBitsN = nb
				ti.ofBitsV = val
				ofState = state
			}
		}
		transitions[i] = ti
	}

	// llState/ofState/mlState now hold the init states (for seq[0])
	initLL, initOF, initML := llState, ofState, mlState

	// Write bitstream: low positions first, high positions last.
	// Decoder reads from high to low, so last-written = first-read.
	var bw zcBitWriter

	for i := nbSeq - 1; i >= 0; i-- {
		c := coded[i]

		// State update bits (not for last sequence)
		if i < nbSeq-1 {
			ti := transitions[i]
			// Decoder reads: LL, ML, OF. Write reverse: OF, ML, LL
			if !ofSame {
				bw.addBits(uint64(ti.ofBitsV), ti.ofBitsN)
			}
			if !mlSame {
				bw.addBits(uint64(ti.mlBitsV), ti.mlBitsN)
			}
			if !llSame {
				bw.addBits(uint64(ti.llBitsV), ti.llBitsN)
			}
		}

		// Extra bits: decoder reads OF, ML, LL. Write reverse: LL, ML, OF
		if c.llBits > 0 {
			bw.addBits(uint64(c.llExtra), c.llBits)
		}
		if c.mlBits > 0 {
			bw.addBits(uint64(c.mlExtra), c.mlBits)
		}
		if c.ofBits > 0 {
			bw.addBits(uint64(c.ofExtra), c.ofBits)
		}
	}

	// Initial states: decoder reads LL, OF, ML. Write reverse: ML, OF, LL
	if !mlSame {
		if mlMode == 2 {
			bw.addBits(uint64(initML), mlCustomTbl.accuracyLog)
		} else {
			bw.addBits(uint64(initML), zcPredML.accLog)
		}
	}
	if !ofSame {
		if ofMode == 2 {
			bw.addBits(uint64(initOF), ofCustomTbl.accuracyLog)
		} else {
			bw.addBits(uint64(initOF), zcPredOF.accLog)
		}
	}
	if !llSame {
		if llMode == 2 {
			bw.addBits(uint64(initLL), llCustomTbl.accuracyLog)
		} else {
			bw.addBits(uint64(initLL), zcPredLL.accLog)
		}
	}

	stream := bw.finish()

	// Assemble: literals section + sequence header + bitstream
	litSec := zcEncodeLiterals(lits)
	out := make([]byte, 0, len(litSec)+len(seqHdr)+len(stream))
	out = append(out, litSec...)
	out = append(out, seqHdr...)
	out = append(out, stream...)
	return out
}

// ──────────────────────────── literals encoding ────────────────────────────

// zcEncodeLiterals tries Huffman compression (type 2), falls back to raw (type 0).
func zcEncodeLiterals(lits []byte) []byte {
	// Too small to benefit from Huffman
	if len(lits) < 32 {
		return zcRawLiterals(lits)
	}

	// Count byte frequencies
	var freq [256]int
	for _, b := range lits {
		freq[b]++
	}

	// Count distinct symbols
	numSymbols := 0
	for _, f := range freq {
		if f > 0 {
			numSymbols++
		}
	}
	if numSymbols < 2 {
		return zcRawLiterals(lits)
	}

	// Build Huffman code lengths
	bitLens := zcBuildHuffmanLens(freq[:], 11)
	if bitLens == nil {
		return zcRawLiterals(lits)
	}

	// Estimate compressed size
	totalBits := 0
	for sym := 0; sym < 256; sym++ {
		if freq[sym] > 0 {
			totalBits += freq[sym] * bitLens[sym]
		}
	}
	compressedStreamSize := (totalBits + 7) / 8

	// Encode Huffman header (direct representation)
	huffHeader := zcEncodeHuffmanHeader(bitLens)
	if huffHeader == nil {
		return zcRawLiterals(lits)
	}

	totalCompressed := len(huffHeader) + compressedStreamSize + 1 // +1 for potential sentinel byte overhead
	overhead := 3 // compressed literals header (use 3 bytes for sizeFormat 0)
	if totalCompressed+overhead >= len(lits)+1 {
		// Not worth it
		return zcRawLiterals(lits)
	}

	// Build canonical Huffman codes from bit lengths
	codes, codeLens := zcCanonicalCodes(bitLens)

	// Compress literals into a forward bitstream
	stream := zcHuffmanCompressStream(lits, codes, codeLens)

	actualCompressed := len(huffHeader) + len(stream)

	// Final check
	if actualCompressed >= len(lits) {
		return zcRawLiterals(lits)
	}

	// Build compressed literals section
	return zcCompressedLiteralsSection(lits, huffHeader, stream, actualCompressed)
}

// zcBuildHuffmanLens builds Huffman bit lengths for symbols with given frequencies.
// Returns a [256]int array of bit lengths (0 = not present). maxBits is the limit.
func zcBuildHuffmanLens(freq []int, maxBits int) []int {
	// Collect symbols with freq > 0
	type symFreq struct {
		sym  int
		freq int
	}
	var syms []symFreq
	for i, f := range freq {
		if f > 0 {
			syms = append(syms, symFreq{i, f})
		}
	}
	if len(syms) < 2 {
		return nil
	}

	// Build Huffman tree using a simple sorted-list approach
	type node struct {
		freq  int
		sym   int  // -1 for internal nodes
		left  int
		right int
	}
	nodes := make([]node, 0, 2*len(syms))

	// Create leaf nodes sorted by frequency
	for _, s := range syms {
		nodes = append(nodes, node{freq: s.freq, sym: s.sym, left: -1, right: -1})
	}

	// Simple priority queue using sorted insertion
	queue := make([]int, len(nodes))
	for i := range queue {
		queue[i] = i
	}
	// Sort by frequency (stable)
	for i := 1; i < len(queue); i++ {
		for j := i; j > 0 && nodes[queue[j]].freq < nodes[queue[j-1]].freq; j-- {
			queue[j], queue[j-1] = queue[j-1], queue[j]
		}
	}

	for len(queue) > 1 {
		a, b := queue[0], queue[1]
		queue = queue[2:]
		newNode := node{freq: nodes[a].freq + nodes[b].freq, sym: -1, left: a, right: b}
		newIdx := len(nodes)
		nodes = append(nodes, newNode)
		// Insert into sorted queue
		inserted := false
		for i, q := range queue {
			if newNode.freq <= nodes[q].freq {
				queue = append(queue, 0)
				copy(queue[i+1:], queue[i:])
				queue[i] = newIdx
				inserted = true
				break
			}
		}
		if !inserted {
			queue = append(queue, newIdx)
		}
	}

	// Extract bit lengths via DFS
	bitLens := make([]int, 256)
	var walk func(idx, depth int)
	walk = func(idx, depth int) {
		n := &nodes[idx]
		if n.left == -1 {
			bitLens[n.sym] = depth
			return
		}
		walk(n.left, depth+1)
		walk(n.right, depth+1)
	}
	walk(queue[0], 0)

	// Clamp to maxBits using the Kraft inequality approach
	needClamp := false
	for _, bl := range bitLens {
		if bl > maxBits {
			needClamp = true
			break
		}
	}
	if needClamp {
		// Iteratively reduce oversized codes
		for {
			changed := false
			for i := range bitLens {
				if bitLens[i] > maxBits {
					bitLens[i] = maxBits
					changed = true
				}
			}
			if !changed {
				break
			}
			// Adjust to satisfy Kraft inequality: sum of 2^(-len) <= 1
			// i.e. sum of 2^(maxBits - len) <= 2^maxBits
			total := 0
			for _, bl := range bitLens {
				if bl > 0 {
					total += 1 << uint(maxBits-bl)
				}
			}
			target := 1 << uint(maxBits)
			if total > target {
				// Need to increase some bit lengths
				// Increase the shortest codes first
				for total > target {
					minBL := maxBits
					for _, bl := range bitLens {
						if bl > 0 && bl < minBL {
							minBL = bl
						}
					}
					if minBL >= maxBits {
						break
					}
					for i := range bitLens {
						if bitLens[i] == minBL && total > target {
							bitLens[i]++
							total -= 1 << uint(maxBits-minBL)
							total += 1 << uint(maxBits-minBL-1)
						}
					}
				}
			}
			// Loop continues; `if !changed { break }` above is the real exit.
		}
	}

	return bitLens
}

// zcCanonicalCodes generates canonical Huffman codes from bit lengths.
func zcCanonicalCodes(bitLens []int) (codes [256]uint32, codeLens [256]int) {
	// zstd does not use the DEFLATE convention of giving the shortest codes
	// the lowest values. Its decoding table is filled starting from weight 1,
	// i.e. the longest codes, so the longest codes take the low code values.
	//
	// A symbol of length L covers 2^(maxBL-L) slots of a 2^maxBL entry table
	// and its code is the slot index shifted down by that same amount. Walking
	// the table in order, longest codes first and symbols ascending within a
	// length, reproduces exactly the layout buildHuffmanTable expects.
	maxBL := 0
	for _, bl := range bitLens {
		if bl > maxBL {
			maxBL = bl
		}
	}
	if maxBL == 0 {
		return
	}

	pos := uint32(0)
	for bl := maxBL; bl >= 1; bl-- {
		shift := uint(maxBL - bl)
		for sym := 0; sym < 256; sym++ {
			if bitLens[sym] == bl {
				codes[sym] = pos >> shift
				codeLens[sym] = bl
				pos += 1 << shift
			}
		}
	}
	return
}

// zcHuffmanCompressStream encodes literals using Huffman codes into a forward bitstream
// with a sentinel high bit, compatible with zstd's 1-stream format.
func zcHuffmanCompressStream(lits []byte, codes [256]uint32, codeLens [256]int) []byte {
	// RFC 8878 4.2.2: Huffman-coded streams are read backwards. Viewing the
	// output as a little-endian bit array (bit k is bit k%8 of byte k/8), the
	// decoder starts at the highest set bit of the last byte — the sentinel —
	// and walks down to bit 0.
	//
	// So the sentinel goes at bit index totalBits, and the symbol codes fill
	// the indices below it in order, each written most-significant bit first.

	totalBits := 0
	for _, b := range lits {
		totalBits += codeLens[b]
	}

	nBytes := (totalBits + 1 + 7) / 8
	out := make([]byte, nBytes)

	pos := totalBits
	out[pos/8] |= 1 << uint(pos%8) // sentinel

	for _, b := range lits {
		code := codes[b]
		for i := codeLens[b] - 1; i >= 0; i-- {
			pos--
			if (code>>uint(i))&1 != 0 {
				out[pos/8] |= 1 << uint(pos%8)
			}
		}
	}

	return out
}

// zcEncodeHuffmanHeader encodes Huffman weights in direct representation format.
// Returns the header bytes (headerByte + packed weight nibbles).
func zcEncodeHuffmanHeader(bitLens []int) []byte {
	// Find maxBits and the highest symbol with a non-zero bit length
	maxBits := 0
	lastSym := -1
	for sym := 0; sym < 256; sym++ {
		if bitLens[sym] > 0 {
			if bitLens[sym] > maxBits {
				maxBits = bitLens[sym]
			}
			lastSym = sym
		}
	}
	if lastSym < 0 {
		return nil
	}

	// numWeights = lastSym (the last symbol's weight is implicit)
	// Symbols 0..lastSym-1 have explicit weights, symbol lastSym is implicit.
	numWeights := lastSym // number of explicit weights = lastSym
	if numWeights < 1 {
		return nil
	}
	if numWeights > 127 {
		// Direct representation supports max 127 weights (headerByte 128..255 → 1..128 weights)
		// Actually: headerByte - 127 = numWeights, max headerByte = 255 → 128 weights
		// But symbols go up to 255, so lastSym can be 255, numWeights = 255.
		// For >128 weights, we'd need FSE encoding. Fall back to raw literals.
		return nil
	}

	// Weight for symbol s: if bitLen > 0, weight = maxBits + 1 - bitLen; else weight = 0
	weights := make([]byte, numWeights)
	for sym := 0; sym < numWeights; sym++ {
		if bitLens[sym] > 0 {
			w := maxBits + 1 - bitLens[sym]
			if w < 1 || w > 13 {
				return nil // invalid
			}
			weights[sym] = byte(w)
		}
	}

	// Verify: the implicit last symbol's weight must be determinable.
	// Sum of 2^(w-1) for all explicit weights with w>0, plus 2^(lastW-1) must equal 2^maxBits
	weightSum := 0
	for _, w := range weights {
		if w > 0 {
			weightSum += 1 << (w - 1)
		}
	}
	remainder := (1 << uint(maxBits)) - weightSum
	if remainder <= 0 || (remainder&(remainder-1)) != 0 {
		// remainder must be a power of 2
		return nil
	}
	// lastWeight: 2^(lastW-1) = remainder → lastW = log2(remainder) + 1
	lastW := 0
	r := remainder
	for r > 1 {
		r >>= 1
		lastW++
	}
	lastW++ // lastW = log2(remainder) + 1
	// Verify this matches the expected bit length for lastSym
	expectedBL := maxBits + 1 - lastW
	if expectedBL != bitLens[lastSym] {
		// Mismatch — the canonical code assignment won't work with this last symbol.
		// Try to find a better lastSym or give up.
		return nil
	}

	// Encode: headerByte = numWeights + 127
	hdrByte := byte(numWeights + 127)
	needed := (numWeights + 1) / 2
	out := make([]byte, 1+needed)
	out[0] = hdrByte
	for i := 0; i < numWeights; i++ {
		if i%2 == 0 {
			out[1+i/2] |= weights[i] << 4
		} else {
			out[1+i/2] |= weights[i]
		}
	}

	return out
}

// zcCompressedLiteralsSection builds the complete compressed literals section.
func zcCompressedLiteralsSection(lits []byte, huffHeader, stream []byte, compressedSize int) []byte {
	regenSize := len(lits)

	// sizeFormat 0: single stream, both sizes fit in 10 bits (< 1024)
	// sizeFormat 2: 4 streams, regen 14 bits, comp 10 bits
	// We use single stream (sizeFormat 0) when possible
	var header []byte

	if regenSize < 1024 && compressedSize < 1024 {
		// sizeFormat 0: 3-byte header
		// byte0[1:0]=litType(2), byte0[3:2]=sizeFormat(0), byte0[7:4]=regenSize[3:0]
		// byte1[3:0]=regenSize[7:4], byte1[5:4]=regenSize[9:8], byte1[7:6]=compSize[1:0]
		// byte2=compSize[9:2]
		val := uint32(regenSize) | (uint32(compressedSize) << 10)
		b0 := byte(2) | byte(0<<2) | byte((val&0xF)<<4)      // litType=2, sf=0, regen[3:0]
		b1 := byte((val >> 4) & 0xFF)
		b2 := byte((val >> 12) & 0xFF)
		header = []byte{b0, b1, b2}
	} else if regenSize < 16384 && compressedSize < 16384 {
		// sizeFormat 2: 4-byte header, 4 streams
		// But we only do single stream, so use sizeFormat 1 if sizes fit 10 bits... they don't here.
		// Actually for single stream we must use sizeFormat 0 (10-bit) or cannot use single stream.
		// For larger sizes, fall back to raw.
		return zcRawLiterals(lits)
	} else {
		return zcRawLiterals(lits)
	}

	out := make([]byte, 0, len(header)+compressedSize)
	out = append(out, header...)
	out = append(out, huffHeader...)
	out = append(out, stream...)
	return out
}

func zcRawLiterals(lits []byte) []byte {
	n := len(lits)
	if n < 32 {
		// 1-byte header: type=0(raw), sizeFormat depends on size
		// For sizeFormat 0 or 2: Regenerated_Size = Header[0]>>3 (5 bits, max 31)
		out := make([]byte, 0, 1+n)
		out = append(out, byte(n<<3)) // type=0, sizeFormat=0, size in upper 5 bits
		out = append(out, lits...)
		return out
	}
	if n < 4096 {
		// 2-byte header, sizeFormat=1
		// byte0: type(2) | sizeFormat(2) | size_low(4)
		// byte1: size_high(8)
		// size = (byte0>>4) | (byte1<<4)
		out := make([]byte, 0, 2+n)
		b0 := byte(0 | (1 << 2) | (byte(n&0xF) << 4)) // type=0, sf=1 (2-byte header per C: 0,2→1byte; 1→2byte; 3→3byte)
		b1 := byte(n >> 4)
		out = append(out, b0, b1)
		out = append(out, lits...)
		return out
	}
	// 3-byte header, sizeFormat=3
	// byte0: type(2) | sizeFormat(2) | size[3:0](4)
	// byte1: size[11:4](8)
	// byte2: size[19:12](8)
	out := make([]byte, 0, 3+n)
	b0 := byte(0 | (3 << 2) | (byte(n&0xF) << 4))
	b1 := byte(n >> 4)
	b2 := byte(n >> 12)
	out = append(out, b0, b1, b2)
	out = append(out, lits...)
	return out
}

// ──────────────────────────── code tables ────────────────────────────

func zcLLCode(litLen int) (code, extraBits int, extra uint32) {
	if litLen < 16 {
		return litLen, 0, 0
	}
	for c := 16; c < 36; c++ {
		if c == 35 || litLen < zstdLLBaseline[c+1] {
			return c, zstdLLBits[c], uint32(litLen - zstdLLBaseline[c])
		}
	}
	return 35, zstdLLBits[35], uint32(litLen - zstdLLBaseline[35])
}

func zcMLCode(matchLen int) (code, extraBits int, extra uint32) {
	if matchLen < 3 {
		matchLen = 3
	}
	for c := 0; c < 53; c++ {
		if c == 52 || matchLen < zstdMLBaseline[c+1] {
			return c, zstdMLBits[c], uint32(matchLen - zstdMLBaseline[c])
		}
	}
	return 52, zstdMLBits[52], uint32(matchLen - zstdMLBaseline[52])
}

func zcOFCode(offset int) (code, extraBits int, extra uint32) {
	if offset < 1 {
		offset = 1
	}
	code = 0
	v := offset
	for v > 1 {
		v >>= 1
		code++
	}
	extra = uint32(offset - (1 << uint(code)))
	extraBits = code
	return
}

// ──────────────────────────── predefined FSE tables ────────────────────────────

type zcPredTable struct {
	symbols   []byte
	numBits   []byte
	newState  []uint16
	sym2state [][]int // symbol → list of valid states
	accLog    int
}

var (
	zcPredLL    zcPredTable
	zcPredOF    zcPredTable
	zcPredML    zcPredTable
	zcPredReady bool
)

func zcInitPredTables() {
	if zcPredReady {
		return
	}
	zcBuildPred(&zcPredLL, zstdLLDefaultProbs, 6)
	zcBuildPred(&zcPredOF, zstdOFDefaultProbs, 5)
	zcBuildPred(&zcPredML, zstdMLDefaultProbs, 6)
	zcPredReady = true
}

func zcBuildPred(tbl *zcPredTable, probs []int16, accLog int) {
	tableSize := 1 << uint(accLog)
	tbl.accLog = accLog
	tbl.symbols = make([]byte, tableSize)
	tbl.numBits = make([]byte, tableSize)
	tbl.newState = make([]uint16, tableSize)

	highThreshold := tableSize - 1
	for sym, p := range probs {
		if p == -1 {
			tbl.symbols[highThreshold] = byte(sym)
			highThreshold--
		}
	}

	step := (tableSize >> 1) + (tableSize >> 3) + 3
	mask := tableSize - 1
	pos := 0
	for sym, p := range probs {
		if p <= 0 {
			continue
		}
		for i := int16(0); i < p; i++ {
			tbl.symbols[pos] = byte(sym)
			pos = (pos + step) & mask
			for pos > highThreshold {
				pos = (pos + step) & mask
			}
		}
	}

	symNext := make([]uint16, len(probs))
	for sym, p := range probs {
		if p == -1 {
			symNext[sym] = 1
		} else if p > 0 {
			symNext[sym] = uint16(p)
		}
	}
	for i := 0; i < tableSize; i++ {
		sym := tbl.symbols[i]
		nb := byte(accLog) - zstdHighBit(uint32(symNext[sym]))
		tbl.numBits[i] = nb
		tbl.newState[i] = (symNext[sym] << nb) - uint16(tableSize)
		symNext[sym]++
	}

	// Build sym2state map
	tbl.sym2state = make([][]int, 256)
	for i := 0; i < tableSize; i++ {
		s := tbl.symbols[i]
		tbl.sym2state[s] = append(tbl.sym2state[s], i)
	}
}

// zcFindState returns a state index that decodes to the given symbol.
func zcFindState(tbl *zcPredTable, sym int) int {
	if sym < 256 && len(tbl.sym2state[sym]) > 0 {
		return tbl.sym2state[sym][0]
	}
	return 0
}

// zcEncodeTransition finds a state for sym such that from that state,
// the decoder can reach targetState. This is the core of backward tANS encoding.
// Returns (sourceState, bitsToEmit, bitsValue).
//
// Decoder does: nextState = newState[curState] + readBits(numBits[curState])
// So we need: targetState = newState[sourceState] + val
//           → val = targetState - newState[sourceState]
//           → val must be in [0, 1<<numBits[sourceState])
func zcEncodeTransition(tbl *zcPredTable, sym int, targetState int) (int, int, int) {
	candidates := tbl.sym2state[byte(sym)]
	for _, s := range candidates {
		nb := int(tbl.numBits[s])
		base := int(tbl.newState[s])
		maxVal := 1 << uint(nb)
		val := targetState - base
		if val >= 0 && val < maxVal {
			return s, nb, val
		}
	}
	// Shouldn't happen with correct predefined tables.
	// Fall back to first candidate (will likely produce wrong decode).
	if len(candidates) > 0 {
		s := candidates[0]
		nb := int(tbl.numBits[s])
		base := int(tbl.newState[s])
		return s, nb, targetState - base
	}
	return 0, 0, 0
}

// ──────────────────────────── bitstream writer ────────────────────────────

// Zstd bitstreams are stored with the most recently written bits at the highest
// bit positions. The decoder reads from high bits to low bits.
// We accumulate bits from the bottom up: first bits written go to lowest positions,
// last bits written go to highest positions.
// Then we add a sentinel 1-bit at the top.
//
// The decoder's initReverse() finds the sentinel, then readBits() consumes
// from high positions downward: it peeks bits at [bitOff-n .. bitOff-1]
// where bit 0 = LSB of byte 0.

type zcBitWriter struct {
	bits    []byte // byte buffer
	nbits   int    // total bits written
}

// addBits writes nbBits of val, LSB first, at the current position.
// These bits will be at a LOWER position than bits written later.
// Since the decoder reads from high to low, bits written LAST are read FIRST.
func (w *zcBitWriter) addBits(val uint64, nbBits int) {
	for i := 0; i < nbBits; i++ {
		byteIdx := w.nbits / 8
		bitIdx := uint(w.nbits % 8)
		for byteIdx >= len(w.bits) {
			w.bits = append(w.bits, 0)
		}
		if (val>>uint(i))&1 != 0 {
			w.bits[byteIdx] |= 1 << bitIdx
		}
		w.nbits++
	}
}

// finish adds the sentinel 1-bit at the top and returns the byte buffer.
func (w *zcBitWriter) finish() []byte {
	// Add sentinel 1-bit
	byteIdx := w.nbits / 8
	bitIdx := uint(w.nbits % 8)
	for byteIdx >= len(w.bits) {
		w.bits = append(w.bits, 0)
	}
	w.bits[byteIdx] |= 1 << bitIdx
	w.nbits++

	nBytes := (w.nbits + 7) / 8
	if nBytes > len(w.bits) {
		nBytes = len(w.bits)
	}
	return w.bits[:nBytes]
}
