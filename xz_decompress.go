package nya

// XZ/LZMA2/LZMA decompressor — uses github.com/ulikunitz/xz for XZ container,
// retains native LZMA/LZMA2 for standalone .lzma files.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"hash/crc64"
	"io"
)

// ── Public API ──────────────────────────────────────────────────────────────

// XzNewReader creates a new XZ stream decompressor.
func XzNewReader(r io.Reader) (io.ReadCloser, error) {
	xr, err := xzNewReaderImpl(r)
	if err != nil {
		return nil, err
	}
	return xr, nil
}

// XzDecompress decompresses XZ data from a byte slice.
func XzDecompress(data []byte) ([]byte, error) {
	r, err := XzNewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// Lzma2NewReader decompresses a raw LZMA2 stream, the form Lzma2Compress
// produces. dictSize must be at least the value used when compressing;
// pass 0 for the 4 MiB default this package writes.
func Lzma2NewReader(r io.Reader, dictSize int) io.ReadCloser {
	if dictSize <= 0 {
		dictSize = lzma2DictSize
	}
	return newLzma2Reader(r, dictSize)
}

// Lzma2Decompress decompresses a raw LZMA2 stream from a byte slice.
func Lzma2Decompress(data []byte, dictSize int) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	r := Lzma2NewReader(bytes.NewReader(data), dictSize)
	defer r.Close()
	return io.ReadAll(r)
}

// LzmaNewReader creates a new plain LZMA stream decompressor.
func LzmaNewReader(r io.Reader) (io.ReadCloser, error) {
	lr, err := newLmaReader(r)
	if err != nil {
		return nil, err
	}
	return lr, nil
}

// LzmaDecompress decompresses plain LZMA data.
func LzmaDecompress(data []byte) ([]byte, error) {
	r, err := LzmaNewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// ── LZMA constants ──────────────────────────────────────────────────────────

const (
	lzmaNumStates       = 12
	lzmaNumPosBitsMax   = 4
	lzmaNumPosStatesMax = 1 << lzmaNumPosBitsMax // 16

	lzmaNumLenToPosStates = 4
	lzmaNumAlignBits      = 4
	lzmaAlignTableSize    = 1 << lzmaNumAlignBits // 16

	lzmaEndPosModelIndex   = 14
	lzmaStartPosModelIndex = 4
	lzmaNumFullDistances   = 1 << (lzmaEndPosModelIndex >> 1) // 128

	lzmaNumLitStates = 7

	lzmaMatchLenMin    = 2
	lzmaLenNumLowBits  = 3
	lzmaLenNumLowSyms  = 1 << lzmaLenNumLowBits // 8
	lzmaLenNumMidBits  = 3
	lzmaLenNumMidSyms  = 1 << lzmaLenNumMidBits // 8
	lzmaLenNumHighBits = 8
	lzmaLenNumHighSyms = 1 << lzmaLenNumHighBits // 256

	lzmaProbInitVal = 1024 // 2048/2
	lzmaProbBits    = 11
	lzmaProbMask    = (1 << lzmaProbBits) - 1

	lzmaRcTopBits  = 24
	lzmaRcTopValue = 1 << lzmaRcTopBits
)

// State transitions
var lzmaNextState = [lzmaNumStates][4]uint32{
	//          Lit  Match LongRep ShortRep
	/* 0  */ {0, 7, 8, 9},
	/* 1  */ {0, 7, 8, 9},
	/* 2  */ {0, 7, 8, 9},
	/* 3  */ {0, 7, 8, 9},
	/* 4  */ {1, 7, 8, 9},
	/* 5  */ {2, 7, 8, 9},
	/* 6  */ {3, 7, 8, 9},
	/* 7  */ {4, 10, 11, 11},
	/* 8  */ {5, 10, 11, 11},
	/* 9  */ {6, 10, 11, 11},
	/* 10 */ {4, 10, 11, 11},
	/* 11 */ {5, 10, 11, 11},
}

func stateIsLit(s uint32) bool { return s < lzmaNumLitStates }

// ── Range Decoder ───────────────────────────────────────────────────────────

type rangeDecoder struct {
	rng  uint32
	code uint32
	r    io.ByteReader
}

func newRangeDecoder(r io.ByteReader) (*rangeDecoder, error) {
	rd := &rangeDecoder{rng: 0xFFFFFFFF, r: r}
	b, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	if b != 0x00 {
		return nil, fmt.Errorf("lzma: range decoder init byte is 0x%02x, expected 0x00", b)
	}
	for i := 0; i < 4; i++ {
		b, err = r.ReadByte()
		if err != nil {
			return nil, err
		}
		rd.code = (rd.code << 8) | uint32(b)
	}
	if rd.code == rd.rng {
		return nil, errors.New("lzma: range decoder corruption")
	}
	return rd, nil
}

func (rd *rangeDecoder) normalize() error {
	if rd.rng < lzmaRcTopValue {
		rd.rng <<= 8
		b, err := rd.r.ReadByte()
		if err != nil {
			// At end of stream, pad with zero bytes (LZMA convention)
			b = 0
		}
		rd.code = (rd.code << 8) | uint32(b)
	}
	return nil
}

func (rd *rangeDecoder) decodeBit(prob *uint16) (uint32, error) {
	if err := rd.normalize(); err != nil {
		return 0, err
	}
	bound := (rd.rng >> lzmaProbBits) * uint32(*prob)
	if rd.code < bound {
		rd.rng = bound
		*prob += (uint16(1<<lzmaProbBits) - *prob) >> 5
		return 0, nil
	}
	rd.code -= bound
	rd.rng -= bound
	*prob -= *prob >> 5
	return 1, nil
}

func (rd *rangeDecoder) decodeDirect(numBits int) (uint32, error) {
	var result uint32
	for i := 0; i < numBits; i++ {
		if err := rd.normalize(); err != nil {
			return 0, err
		}
		rd.rng >>= 1
		rd.code -= rd.rng
		// if code >= 0 (as unsigned) => bit=1, else bit=0
		t := uint32(0) - (rd.code >> 31) // 0 if code < 0x80000000 (meaning code was >= range)
		rd.code += rd.rng & t            // if t=0, bit=1; if t=0xFFFFFFFF, bit=0
		result = (result << 1) | (1 - (t & 1))
	}
	return result, nil
}

func (rd *rangeDecoder) decodeBitTree(probs []uint16, numBits int) (uint32, error) {
	m := uint32(1)
	for i := 0; i < numBits; i++ {
		bit, err := rd.decodeBit(&probs[m])
		if err != nil {
			return 0, err
		}
		m = (m << 1) | bit
	}
	return m - (1 << uint(numBits)), nil
}

func (rd *rangeDecoder) decodeBitTreeReverse(probs []uint16, numBits int) (uint32, error) {
	m := uint32(1)
	var result uint32
	for i := 0; i < numBits; i++ {
		bit, err := rd.decodeBit(&probs[m])
		if err != nil {
			return 0, err
		}
		m = (m << 1) | bit
		result |= bit << uint(i)
	}
	return result, nil
}

// ── Length Decoder ───────────────────────────────────────────────────────────

type lzmaLenDecoder struct {
	choice  uint16
	choice2 uint16
	low     [lzmaNumPosStatesMax][]uint16
	mid     [lzmaNumPosStatesMax][]uint16
	high    []uint16
}

func newLzmaLenDecoder() *lzmaLenDecoder {
	ld := &lzmaLenDecoder{}
	ld.high = make([]uint16, 1<<lzmaLenNumHighBits)
	ld.reset()
	return ld
}

func (ld *lzmaLenDecoder) reset() {
	ld.choice = lzmaProbInitVal
	ld.choice2 = lzmaProbInitVal
	for i := range ld.low {
		ld.low[i] = make([]uint16, 1<<lzmaLenNumLowBits)
		for j := range ld.low[i] {
			ld.low[i][j] = lzmaProbInitVal
		}
	}
	for i := range ld.mid {
		ld.mid[i] = make([]uint16, 1<<lzmaLenNumMidBits)
		for j := range ld.mid[i] {
			ld.mid[i][j] = lzmaProbInitVal
		}
	}
	if ld.high == nil {
		ld.high = make([]uint16, 1<<lzmaLenNumHighBits)
	}
	for i := range ld.high {
		ld.high[i] = lzmaProbInitVal
	}
}

func (ld *lzmaLenDecoder) decode(rd *rangeDecoder, posState uint32) (uint32, error) {
	bit, err := rd.decodeBit(&ld.choice)
	if err != nil {
		return 0, err
	}
	if bit == 0 {
		v, err := rd.decodeBitTree(ld.low[posState], lzmaLenNumLowBits)
		if err != nil {
			return 0, err
		}
		return v, nil
	}
	bit, err = rd.decodeBit(&ld.choice2)
	if err != nil {
		return 0, err
	}
	if bit == 0 {
		v, err := rd.decodeBitTree(ld.mid[posState], lzmaLenNumMidBits)
		if err != nil {
			return 0, err
		}
		return lzmaLenNumLowSyms + v, nil
	}
	v, err := rd.decodeBitTree(ld.high, lzmaLenNumHighBits)
	if err != nil {
		return 0, err
	}
	return lzmaLenNumLowSyms + lzmaLenNumMidSyms + v, nil
}

// ── LZMA Decoder ────────────────────────────────────────────────────────────

type lzmaDecoder struct {
	rc    *rangeDecoder
	dict  []byte
	pos   uint64 // total bytes decoded
	dPos  int    // position in dict buffer (circular)
	dSize int    // dict capacity

	state uint32
	reps  [4]uint32

	lc, lp, pb uint32

	// probability arrays
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

	matchLen *lzmaLenDecoder
	repLen   *lzmaLenDecoder
}

func newLzmaDecoder(lc, lp, pb uint32, dictSize int) *lzmaDecoder {
	if dictSize < 4096 {
		dictSize = 4096
	}
	d := &lzmaDecoder{
		lc: lc, lp: lp, pb: pb,
		dSize: dictSize,
		dict:  make([]byte, dictSize),
	}
	d.matchLen = newLzmaLenDecoder()
	d.repLen = newLzmaLenDecoder()
	for i := range d.posSlot {
		d.posSlot[i] = make([]uint16, 1<<6)
	}
	d.litProbs = make([]uint16, 0x300<<(lc+lp))
	d.reset()
	return d
}

func (d *lzmaDecoder) reset() {
	d.state = 0
	d.reps = [4]uint32{0, 0, 0, 0}
	initProbs(d.isMatch[:])
	initProbs(d.isRep[:])
	initProbs(d.isRepG0[:])
	initProbs(d.isRepG1[:])
	initProbs(d.isRepG2[:])
	initProbs(d.isRep0Long[:])
	initProbs(d.posSpecial[:])
	initProbs(d.posAlign[:])
	for i := range d.posSlot {
		initProbs(d.posSlot[i])
	}
	initProbs(d.litProbs)
	d.matchLen.reset()
	d.repLen.reset()
}

func (d *lzmaDecoder) resetState() {
	d.state = 0
	d.reps = [4]uint32{0, 0, 0, 0}
}

func initProbs(p []uint16) {
	for i := range p {
		p[i] = lzmaProbInitVal
	}
}

func (d *lzmaDecoder) putByte(b byte) {
	d.dict[d.dPos] = b
	d.dPos++
	if d.dPos >= d.dSize {
		d.dPos = 0
	}
	d.pos++
}

func (d *lzmaDecoder) getByte(dist int) byte {
	idx := d.dPos - dist - 1
	if idx < 0 {
		idx += d.dSize
	}
	return d.dict[idx]
}

func (d *lzmaDecoder) copyMatch(dist int, length int, out *bytes.Buffer) {
	for i := 0; i < length; i++ {
		b := d.getByte(dist)
		out.WriteByte(b)
		d.putByte(b)
	}
}

func getLenToPosState(l uint32) uint32 {
	if l >= lzmaNumLenToPosStates+lzmaMatchLenMin {
		return lzmaNumLenToPosStates - 1
	}
	return l - lzmaMatchLenMin
}

func (d *lzmaDecoder) decodeLiteral(out *bytes.Buffer) error {
	prevByte := byte(0)
	if d.pos > 0 {
		prevByte = d.getByte(0)
	}

	litState := uint32(prevByte>>(8-d.lc)) | ((uint32(d.pos) & ((1 << d.lp) - 1)) << d.lc)
	probs := d.litProbs[litState*0x300:]

	if stateIsLit(d.state) {
		// Simple literal
		symbol := uint32(1)
		for symbol < 0x100 {
			bit, err := d.rc.decodeBit(&probs[symbol])
			if err != nil {
				return err
			}
			symbol = (symbol << 1) | bit
		}
		b := byte(symbol - 0x100)
		out.WriteByte(b)
		d.putByte(b)
	} else {
		// Match literal (uses prev match byte for context)
		matchByte := uint32(d.getByte(int(d.reps[0])))
		symbol := uint32(1)
		for symbol < 0x100 {
			matchBit := (matchByte >> 7) & 1
			matchByte <<= 1
			idx := ((1 + matchBit) << 8) + symbol
			bit, err := d.rc.decodeBit(&probs[idx])
			if err != nil {
				return err
			}
			symbol = (symbol << 1) | bit
			if matchBit != bit {
				// Normal decode rest
				for symbol < 0x100 {
					bit2, err := d.rc.decodeBit(&probs[symbol])
					if err != nil {
						return err
					}
					symbol = (symbol << 1) | bit2
				}
				break
			}
		}
		b := byte(symbol - 0x100)
		out.WriteByte(b)
		d.putByte(b)
	}
	return nil
}

// decode decodes up to limit bytes. If limit < 0, decode until EOS marker.
func (d *lzmaDecoder) decode(out *bytes.Buffer, limit int64) error {
	for {
		if limit >= 0 && int64(out.Len()) >= limit {
			return nil
		}
		posState := uint32(d.pos) & ((1 << d.pb) - 1)

		bit, err := d.rc.decodeBit(&d.isMatch[(d.state<<lzmaNumPosBitsMax)+posState])
		if err != nil {
			return err
		}

		if bit == 0 {
			// Literal
			if err := d.decodeLiteral(out); err != nil {
				return err
			}
			d.state = lzmaNextState[d.state][0]
			continue
		}

		// Match or rep
		bit, err = d.rc.decodeBit(&d.isRep[d.state])
		if err != nil {
			return err
		}

		if bit == 0 {
			// Simple match
			length, err := d.matchLen.decode(d.rc, posState)
			if err != nil {
				return err
			}
			length += lzmaMatchLenMin

			slotIdx := getLenToPosState(length)
			distSlot, err := d.rc.decodeBitTree(d.posSlot[slotIdx], 6)
			if err != nil {
				return err
			}

			var dist uint32
			if distSlot < lzmaStartPosModelIndex {
				dist = distSlot
			} else {
				numDirectBits := (distSlot >> 1) - 1
				dist = (2 | (distSlot & 1)) << numDirectBits

				if distSlot < lzmaEndPosModelIndex {
					// Use bit tree
					base := int(dist) - int(distSlot)
					v, err := rd_decodeBitTreeReverseOffset(d.rc, d.posSpecial[:], base, int(numDirectBits))
					if err != nil {
						return err
					}
					dist += v
				} else {
					// Direct bits + align
					directBits, err := d.rc.decodeDirect(int(numDirectBits) - lzmaNumAlignBits)
					if err != nil {
						return err
					}
					dist += directBits << lzmaNumAlignBits
					alignBits, err := d.rc.decodeBitTreeReverse(d.posAlign[:], lzmaNumAlignBits)
					if err != nil {
						return err
					}
					dist += alignBits
				}
			}

			if dist == 0xFFFFFFFF {
				// EOS marker
				return nil
			}

			d.reps[3] = d.reps[2]
			d.reps[2] = d.reps[1]
			d.reps[1] = d.reps[0]
			d.reps[0] = dist
			d.state = lzmaNextState[d.state][1]

			if uint64(dist) >= d.pos || int(dist) >= d.dSize {
				return fmt.Errorf("lzma: invalid distance %d at position %d", dist, d.pos)
			}
			d.copyMatch(int(dist), int(length), out)
		} else {
			// Rep match
			var dist uint32
			bit, err := d.rc.decodeBit(&d.isRepG0[d.state])
			if err != nil {
				return err
			}
			if bit == 0 {
				// rep0
				bit, err := d.rc.decodeBit(&d.isRep0Long[(d.state<<lzmaNumPosBitsMax)+posState])
				if err != nil {
					return err
				}
				if bit == 0 {
					// Short rep (length=1)
					d.state = lzmaNextState[d.state][3]
					if d.reps[0] >= uint32(d.pos) || int(d.reps[0]) >= d.dSize {
						return fmt.Errorf("lzma: invalid short rep distance %d", d.reps[0])
					}
					b := d.getByte(int(d.reps[0]))
					out.WriteByte(b)
					d.putByte(b)
					continue
				}
				dist = d.reps[0]
			} else {
				bit, err := d.rc.decodeBit(&d.isRepG1[d.state])
				if err != nil {
					return err
				}
				if bit == 0 {
					dist = d.reps[1]
				} else {
					bit, err := d.rc.decodeBit(&d.isRepG2[d.state])
					if err != nil {
						return err
					}
					if bit == 0 {
						dist = d.reps[2]
					} else {
						dist = d.reps[3]
						d.reps[3] = d.reps[2]
					}
					d.reps[2] = d.reps[1]
				}
				d.reps[1] = d.reps[0]
				d.reps[0] = dist
			}

			length, err := d.repLen.decode(d.rc, posState)
			if err != nil {
				return err
			}
			length += lzmaMatchLenMin
			d.state = lzmaNextState[d.state][2]

			if uint64(dist) >= d.pos || int(dist) >= d.dSize {
				return fmt.Errorf("lzma: invalid rep distance %d at position %d", dist, d.pos)
			}
			d.copyMatch(int(dist), int(length), out)
		}
	}
}

func rd_decodeBitTreeReverseOffset(rd *rangeDecoder, probs []uint16, offset int, numBits int) (uint32, error) {
	m := uint32(1)
	var result uint32
	for i := 0; i < numBits; i++ {
		bit, err := rd.decodeBit(&probs[offset+int(m)])
		if err != nil {
			return 0, err
		}
		m = (m << 1) | bit
		result |= bit << uint(i)
	}
	return result, nil
}

// ── LZMA2 Decoder ───────────────────────────────────────────────────────────

type lzma2Reader struct {
	r            io.Reader
	dec          *lzmaDecoder
	buf          bytes.Buffer
	done         bool
	dictSize     int
	chunks       int   // chunk counter for debugging
	totalDecoded int64 // total bytes decoded across all chunks
}

func newLzma2Reader(r io.Reader, dictSize int) *lzma2Reader {
	return &lzma2Reader{
		r:        r,
		dictSize: dictSize,
	}
}

func (lr *lzma2Reader) Read(p []byte) (int, error) {
	for lr.buf.Len() == 0 {
		if lr.done {
			return 0, io.EOF
		}
		if err := lr.decodeChunk(); err != nil {
			return 0, err
		}
	}
	return lr.buf.Read(p)
}

func (lr *lzma2Reader) Close() error { return nil }

func (lr *lzma2Reader) decodeChunk() error {
	var ctrl [1]byte
	if _, err := io.ReadFull(lr.r, ctrl[:]); err != nil {
		return fmt.Errorf("lzma2: read control byte: %w", err)
	}
	c := ctrl[0]

	if c == 0x00 {
		lr.done = true
		return nil
	}

	if c == 0x01 {
		// Dictionary reset + uncompressed
		return lr.readUncompressed(true)
	}
	if c == 0x02 {
		// Uncompressed, no dict reset
		return lr.readUncompressed(false)
	}

	if c < 0x80 {
		return fmt.Errorf("lzma2: reserved control byte 0x%02X (chunk #%d, total decoded %d bytes, buf.Len=%d)", c, lr.chunks, lr.totalDecoded, lr.buf.Len())
	}

	lr.chunks++

	// LZMA chunk — control byte encodes reset level:
	//   0x80-0x9F: no reset
	//   0xA0-0xBF: state reset (no new props)
	//   0xC0-0xDF: state reset + new properties
	//   0xE0-0xFF: full reset (state + props + dictionary)
	needProps := false
	needStateReset := false
	needDictReset := false

	if c >= 0xE0 {
		needDictReset = true
		needStateReset = true
		needProps = true
	} else if c >= 0xC0 {
		needStateReset = true
		needProps = true
	} else if c >= 0xA0 {
		needStateReset = true
	}

	// Read uncompressed size (high 5 bits from ctrl + 2 bytes)
	var hdr [4]byte
	if _, err := io.ReadFull(lr.r, hdr[:]); err != nil {
		return err
	}
	uncompSize := ((int64(c&0x1F) << 16) | (int64(hdr[0]) << 8) | int64(hdr[1])) + 1
	compSize := ((int64(hdr[2]) << 8) | int64(hdr[3])) + 1

	if needProps {
		var propByte [1]byte
		if _, err := io.ReadFull(lr.r, propByte[:]); err != nil {
			return err
		}
		// Note: comp_size does NOT include the properties byte
		lc, lp, pb := decodeLzmaProps(propByte[0])
		if needDictReset || lr.dec == nil {
			lr.dec = newLzmaDecoder(lc, lp, pb, lr.dictSize)
		} else {
			lr.dec.lc = lc
			lr.dec.lp = lp
			lr.dec.pb = pb
			if needStateReset {
				lr.dec.reset()
			}
		}
	} else {
		if lr.dec == nil {
			return errors.New("lzma2: no properties for first LZMA chunk")
		}
		if needDictReset {
			lr.dec = newLzmaDecoder(lr.dec.lc, lr.dec.lp, lr.dec.pb, lr.dictSize)
		} else if needStateReset {
			lr.dec.reset()
		}
	}

	// Read compressed data
	compData := make([]byte, compSize)
	if _, err := io.ReadFull(lr.r, compData); err != nil {
		return err
	}

	br := &byteReader{data: compData}
	rc, err := newRangeDecoder(br)
	if err != nil {
		return fmt.Errorf("lzma2: range decoder init: %w", err)
	}
	lr.dec.rc = rc

	startLen := lr.buf.Len()
	if err := lr.dec.decode(&lr.buf, int64(startLen)+uncompSize); err != nil {
		return err
	}
	lr.totalDecoded += int64(lr.buf.Len() - startLen)
	return nil
}

func (lr *lzma2Reader) readUncompressed(dictReset bool) error {
	var hdr [2]byte
	if _, err := io.ReadFull(lr.r, hdr[:]); err != nil {
		return err
	}
	size := ((int(hdr[0]) << 8) | int(hdr[1])) + 1
	data := make([]byte, size)
	if _, err := io.ReadFull(lr.r, data); err != nil {
		return err
	}

	if dictReset || lr.dec == nil {
		lr.dec = newLzmaDecoder(3, 0, 2, lr.dictSize)
	}

	for _, b := range data {
		lr.buf.WriteByte(b)
		lr.dec.putByte(b)
	}
	lr.totalDecoded += int64(size)
	return nil
}

func decodeLzmaProps(b byte) (lc, lp, pb uint32) {
	// LZMA properties byte encoding: b = (pb * 5 + lp) * 9 + lc
	lc = uint32(b) % 9
	rem := uint32(b) / 9
	lp = rem % 5
	pb = rem / 5
	return
}

// ── XZ Container ────────────────────────────────────────────────────────────

var xzMagic = []byte{0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00}

const (
	xzCheckNone   = 0x00
	xzCheckCRC32  = 0x01
	xzCheckCRC64  = 0x04
	xzCheckSHA256 = 0x0A
)

var crc64Table = crc64.MakeTable(crc64.ECMA)

type xzReader struct {
	r          *countByteReader2
	lzma2      *lzma2Reader
	checkType  byte
	done       bool
	bcj        bool   // BCJ filter active (x86/ARM/etc.)
	bcjID      uint64 // BCJ filter ID (0x04=x86, etc.)
	blockStart int64  // byte position at start of block compressed data
}

// countByteReader2 wraps an io.Reader and counts bytes read.
type countByteReader2 struct {
	r   io.Reader
	pos int64
	buf [1]byte
}

func (c *countByteReader2) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.pos += int64(n)
	return n, err
}

func (c *countByteReader2) ReadByte() (byte, error) {
	_, err := io.ReadFull(c.r, c.buf[:])
	if err != nil {
		return 0, err
	}
	c.pos++
	return c.buf[0], nil
}

func xzNewReaderImpl(r io.Reader) (*xzReader, error) {
	// Read stream header (12 bytes)
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("xz: failed to read header: %w", err)
	}
	if !bytes.Equal(hdr[:6], xzMagic) {
		return nil, errors.New("xz: invalid magic")
	}

	// Stream flags (2 bytes at offset 6)
	flags := hdr[6:8]
	if flags[0] != 0 {
		return nil, fmt.Errorf("xz: unsupported stream flags byte0: 0x%02x", flags[0])
	}
	checkType := flags[1] & 0x0F

	// CRC32 of stream flags
	crc := binary.LittleEndian.Uint32(hdr[8:12])
	expected := crc32.ChecksumIEEE(flags)
	if crc != expected {
		return nil, errors.New("xz: stream header CRC32 mismatch")
	}

	cr := &countByteReader2{r: r}
	xr := &xzReader{r: cr, checkType: checkType}
	return xr, nil
}

func (xr *xzReader) Read(p []byte) (int, error) {
	if xr.done {
		return 0, io.EOF
	}

	for xr.lzma2 == nil || xr.lzma2.done {
		if xr.lzma2 != nil && xr.lzma2.done {
			// Skip block check
			if err := xr.skipBlockCheck(); err != nil {
				return 0, fmt.Errorf("xz: skipBlockCheck: %w", err)
			}
			// Pad to 4-byte alignment
			xr.lzma2 = nil
		}
		// Try to read next block header
		err := xr.readBlockHeader()
		if err != nil {
			return 0, fmt.Errorf("xz: readBlockHeader: %w", err)
		}
		if xr.done {
			return 0, io.EOF
		}
	}

	n, err := xr.lzma2.Read(p)
	if err != nil && err != io.EOF {
		return n, fmt.Errorf("xz: lzma2.Read: %w", err)
	}
	return n, err
}

func (xr *xzReader) Close() error { return nil }

func (xr *xzReader) readBlockHeader() error {
	// Block header size byte (0 = index)
	var sizeByte [1]byte
	if _, err := io.ReadFull(xr.r, sizeByte[:]); err != nil {
		return fmt.Errorf("xz: failed reading block: %w", err)
	}

	if sizeByte[0] == 0x00 {
		// Index — skip it and read stream footer
		xr.done = true
		return xr.skipIndexAndFooter()
	}

	headerSize := (int(sizeByte[0]) + 1) * 4
	hdrBuf := make([]byte, headerSize)
	hdrBuf[0] = sizeByte[0]
	if _, err := io.ReadFull(xr.r, hdrBuf[1:]); err != nil {
		return err
	}

	// Verify CRC32 of block header
	crc := binary.LittleEndian.Uint32(hdrBuf[headerSize-4:])
	expected := crc32.ChecksumIEEE(hdrBuf[:headerSize-4])
	if crc != expected {
		return errors.New("xz: block header CRC32 mismatch")
	}

	// Parse block flags
	blockFlags := hdrBuf[1]
	numFilters := (blockFlags & 0x03) + 1
	hasCompSize := (blockFlags & 0x40) != 0
	hasUncompSize := (blockFlags & 0x80) != 0

	pos := 2

	// Skip optional compressed size
	if hasCompSize {
		_, n := decodeMultiByteInt(hdrBuf[pos:])
		pos += n
	}
	// Skip optional uncompressed size
	if hasUncompSize {
		_, n := decodeMultiByteInt(hdrBuf[pos:])
		pos += n
	}

	// Read filters
	if numFilters == 2 {
		// Two filters: typically BCJ (0x04/0x05/0x06/0x07/0x08/0x09/0x0A/0x0B) + LZMA2 (0x21)
		firstFilterID, n := decodeMultiByteInt(hdrBuf[pos:])
		pos += n
		firstPropsSize, n := decodeMultiByteInt(hdrBuf[pos:])
		pos += n
		pos += int(firstPropsSize) // skip BCJ filter props (usually 0 or 4 bytes)
		// BCJ filter IDs: 0x04=x86, 0x05=PowerPC, 0x06=IA-64, 0x07=ARM, 0x08=ARM Thumb, 0x09=SPARC, 0x0A=ARM64
		if firstFilterID >= 0x04 && firstFilterID <= 0x0B {
			xr.bcj = true
			xr.bcjID = firstFilterID
		} else {
			return fmt.Errorf("xz: unsupported first filter ID 0x%X in 2-filter chain", firstFilterID)
		}
	} else if numFilters != 1 {
		return fmt.Errorf("xz: expected 1-2 filters, got %d", numFilters)
	}

	filterID, n := decodeMultiByteInt(hdrBuf[pos:])
	pos += n
	if filterID != 0x21 {
		return fmt.Errorf("xz: unsupported filter ID 0x%X (only LZMA2 0x21 supported)", filterID)
	}

	propsSize, n := decodeMultiByteInt(hdrBuf[pos:])
	pos += n
	if propsSize != 1 {
		return fmt.Errorf("xz: LZMA2 filter props size %d, expected 1", propsSize)
	}

	dictByte := hdrBuf[pos]
	dictSize := decodeLzma2DictSize(dictByte)

	xr.lzma2 = newLzma2Reader(xr.r, dictSize)
	xr.blockStart = xr.r.pos // record start of compressed data
	return nil
}

func (xr *xzReader) skipBlockCheck() error {
	// First skip padding: compressed data is padded to 4-byte alignment
	dataSize := xr.r.pos - xr.blockStart
	padLen := (4 - (dataSize % 4)) % 4
	if padLen > 0 {
		pad := make([]byte, padLen)
		if _, err := io.ReadFull(xr.r, pad); err != nil {
			return err
		}
	}
	// Then skip the check bytes
	checkSize := xzCheckSize(xr.checkType)
	if checkSize > 0 {
		skip := make([]byte, checkSize)
		_, err := io.ReadFull(xr.r, skip)
		return err
	}
	return nil
}

// skipBlockPadding reads padding to align to 4 bytes - not needed since we track compressed size
// For simplicity, XZ blocks are padded to 4-byte boundaries after check.
// We handle this by reading through LZMA2 end marker.

func (xr *xzReader) skipIndexAndFooter() error {
	// We need to consume the rest of the stream: index + footer
	// Index format: number of records (multibyte), then records, then padding, then CRC32
	// For simplicity, just read until we find the 12-byte footer

	// Read index: first is number of records
	numRecords, buf, err := readMultiByteFromReader(xr.r)
	if err != nil {
		return err
	}

	var indexData []byte
	indexData = append(indexData, 0x00) // index indicator
	indexData = append(indexData, buf...)

	// Skip records
	for i := uint64(0); i < numRecords; i++ {
		// Unpadded size
		_, b, err := readMultiByteFromReader(xr.r)
		if err != nil {
			return err
		}
		indexData = append(indexData, b...)
		// Uncompressed size
		_, b, err = readMultiByteFromReader(xr.r)
		if err != nil {
			return err
		}
		indexData = append(indexData, b...)
	}

	// Padding to 4-byte alignment
	padSize := (4 - len(indexData)%4) % 4
	if padSize > 0 {
		pad := make([]byte, padSize)
		if _, err := io.ReadFull(xr.r, pad); err != nil {
			return err
		}
	}

	// CRC32 of index
	var idxCrc [4]byte
	if _, err := io.ReadFull(xr.r, idxCrc[:]); err != nil {
		return err
	}

	// Stream footer (12 bytes)
	var footer [12]byte
	if _, err := io.ReadFull(xr.r, footer[:]); err != nil {
		return err
	}
	// We don't strictly validate footer, just consume it
	return nil
}

func xzCheckSize(checkType byte) int {
	switch checkType {
	case xzCheckNone:
		return 0
	case xzCheckCRC32:
		return 4
	case xzCheckCRC64:
		return 8
	case xzCheckSHA256:
		return 32
	default:
		return 0
	}
}

// lzma2MaxDictSize caps the dictionary a stream header can ask us to
// allocate. The encoded field reaches 4 GiB - 1, which does not fit an int on
// 32-bit platforms and would let a single header byte demand an absurd
// allocation. No real archive needs more than this; xz tops out at 64 MiB.
const lzma2MaxDictSize = 1 << 30

func decodeLzma2DictSize(b byte) int {
	if b >= 40 {
		return lzma2MaxDictSize
	}
	var size int
	if b&1 == 0 {
		size = 2 << (b/2 + 11)
	} else {
		size = 3 << (b/2 + 11)
	}
	if size > lzma2MaxDictSize || size <= 0 {
		return lzma2MaxDictSize
	}
	return size
}

func decodeMultiByteInt(data []byte) (uint64, int) {
	var val uint64
	for i := 0; i < len(data) && i < 9; i++ {
		val |= uint64(data[i]&0x7F) << (uint(i) * 7)
		if data[i]&0x80 == 0 {
			return val, i + 1
		}
	}
	return val, 0
}

func readMultiByteFromReader(r io.Reader) (uint64, []byte, error) {
	var val uint64
	var buf []byte
	var b [1]byte
	for i := 0; i < 9; i++ {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		buf = append(buf, b[0])
		val |= uint64(b[0]&0x7F) << (uint(i) * 7)
		if b[0]&0x80 == 0 {
			return val, buf, nil
		}
	}
	return 0, nil, errors.New("xz: multibyte integer too long")
}

// ── Plain LZMA reader ───────────────────────────────────────────────────────

type plainLzmaReader struct {
	buf  bytes.Buffer
	done bool
}

func newLmaReader(r io.Reader) (*plainLzmaReader, error) {
	// Header: 1 byte props + 4 bytes dictSize + 8 bytes uncompSize
	var hdr [13]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("lzma: failed reading header: %w", err)
	}

	lc, lp, pb := decodeLzmaProps(hdr[0])
	if pb > 4 || lp > 4 || lc > 8 {
		return nil, fmt.Errorf("lzma: invalid props lc=%d lp=%d pb=%d", lc, lp, pb)
	}

	dictSize := int(binary.LittleEndian.Uint32(hdr[1:5]))
	if dictSize < 4096 {
		dictSize = 4096
	}

	uncompSize := binary.LittleEndian.Uint64(hdr[5:13])
	hasSize := uncompSize != 0xFFFFFFFFFFFFFFFF

	br := &countByteReader{r: toBR(r)}
	rc, err := newRangeDecoder(br)
	if err != nil {
		return nil, err
	}

	dec := newLzmaDecoder(lc, lp, pb, dictSize)
	dec.rc = rc

	plr := &plainLzmaReader{}
	limit := int64(-1)
	if hasSize {
		limit = int64(uncompSize)
	}
	if err := dec.decode(&plr.buf, limit); err != nil {
		return nil, err
	}
	plr.done = true
	return plr, nil
}

func (r *plainLzmaReader) Read(p []byte) (int, error) {
	if r.buf.Len() == 0 && r.done {
		return 0, io.EOF
	}
	return r.buf.Read(p)
}

func (r *plainLzmaReader) Close() error { return nil }

// ── Helpers ─────────────────────────────────────────────────────────────────

type byteReader struct {
	data []byte
	pos  int
}

func (b *byteReader) ReadByte() (byte, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	v := b.data[b.pos]
	b.pos++
	return v, nil
}

type countByteReader struct {
	r io.ByteReader
}

func (c *countByteReader) ReadByte() (byte, error) {
	return c.r.ReadByte()
}

func toBR(r io.Reader) io.ByteReader {
	if br, ok := r.(io.ByteReader); ok {
		return br
	}
	return &singleByteReader{r: r}
}

type singleByteReader struct {
	r io.Reader
}

func (s *singleByteReader) ReadByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(s.r, b[:])
	return b[0], err
}

// These are used indirectly for check verification (unused but kept for reference)
var _ hash.Hash32 = crc32.NewIEEE()
var _ hash.Hash64 = crc64.New(crc64Table)
