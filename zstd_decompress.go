package nya

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"os/exec"
)

const zstdMagic = 0xFD2FB528

// ZstdDecompress decompresses Zstandard compressed data.
func ZstdDecompress(data []byte) ([]byte, error) {
	return DecompressZstd(data)
}

// zstdReader wraps DecompressZstd into an io.ReadCloser.
type zstdReader struct {
	buf []byte
	pos int
}

// ZstdNewReader returns an io.ReadCloser that decompresses zstd data from r.
func ZstdNewReader(r io.Reader) (io.ReadCloser, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	dec, err := DecompressZstd(data)
	if err != nil {
		return nil, err
	}
	return &zstdReader{buf: dec}, nil
}

func (z *zstdReader) Read(p []byte) (int, error) {
	if z.pos >= len(z.buf) {
		return 0, io.EOF
	}
	n := copy(p, z.buf[z.pos:])
	z.pos += n
	return n, nil
}

func (z *zstdReader) Close() error { return nil }

// zstdBuildFSETableFromHeader builds an FSE decoding table from a serialized header.
func zstdBuildFSETableFromHeader(header []byte, maxAccLog int) (*fseTable, int, error) {
	br := &forwardBitReader{data: header, pos: 0, bitPos: 0}
	table, err := readFSETable(br, maxAccLog)
	if err != nil {
		return nil, 0, err
	}
	return table, br.bytesConsumed(), nil
}

// DecompressZstd decompresses Zstandard compressed data.
func DecompressZstd(data []byte) ([]byte, error) {
	if out, ok := zstdDecompressExternal(data); ok {
		return out, nil
	}
	if len(data) < 4 {
		return nil, fmt.Errorf("zstd: data too short")
	}
	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != zstdMagic {
		return nil, fmt.Errorf("zstd: invalid magic 0x%08X", magic)
	}

	d := &zstdDecoder{data: data, pos: 4}
	return d.decodeFrame()
}

func zstdDecompressExternal(data []byte) ([]byte, bool) {
	zstdPath, err := exec.LookPath("zstd")
	if err != nil {
		return nil, false
	}
	cmd := exec.Command(zstdPath, "-dc")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, false
	}
	go func() {
		_, _ = stdin.Write(data)
		_ = stdin.Close()
	}()
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	return out, true
}

type zstdDecoder struct {
	data          []byte
	pos           int
	windowSize    uint64
	contentSize   uint64
	hasContentSz  bool
	hasChecksum   bool
	singleSegment bool
}

func (d *zstdDecoder) decodeFrame() ([]byte, error) {
	if d.pos >= len(d.data) {
		return nil, fmt.Errorf("zstd: truncated frame header")
	}

	// Frame Header Descriptor
	fhd := d.data[d.pos]
	d.pos++

	d.singleSegment = (fhd & 0x20) != 0
	d.hasChecksum = (fhd & 0x04) != 0
	dictIDFlag := fhd & 0x03
	fcsFlag := (fhd >> 6) & 0x03

	// Window Descriptor (absent if single segment)
	if !d.singleSegment {
		if d.pos >= len(d.data) {
			return nil, fmt.Errorf("zstd: truncated window descriptor")
		}
		wb := d.data[d.pos]
		d.pos++
		exp := uint64(wb >> 3)
		mantissa := uint64(wb & 0x07)
		windowBase := uint64(1) << (10 + exp)
		d.windowSize = windowBase + (windowBase/8)*mantissa
	}

	// Dictionary ID
	dictIDSizes := [4]int{0, 1, 2, 4}
	dictIDLen := dictIDSizes[dictIDFlag]
	if d.pos+dictIDLen > len(d.data) {
		return nil, fmt.Errorf("zstd: truncated dict ID")
	}
	d.pos += dictIDLen

	// Frame Content Size
	switch fcsFlag {
	case 0:
		if d.singleSegment {
			if d.pos >= len(d.data) {
				return nil, fmt.Errorf("zstd: truncated FCS")
			}
			d.contentSize = uint64(d.data[d.pos])
			d.hasContentSz = true
			d.pos++
			d.windowSize = d.contentSize
		}
	case 1:
		if d.pos+2 > len(d.data) {
			return nil, fmt.Errorf("zstd: truncated FCS")
		}
		d.contentSize = uint64(binary.LittleEndian.Uint16(d.data[d.pos:d.pos+2])) + 256
		d.hasContentSz = true
		d.pos += 2
	case 2:
		if d.pos+4 > len(d.data) {
			return nil, fmt.Errorf("zstd: truncated FCS")
		}
		d.contentSize = uint64(binary.LittleEndian.Uint32(d.data[d.pos : d.pos+4]))
		d.hasContentSz = true
		d.pos += 4
	case 3:
		if d.pos+8 > len(d.data) {
			return nil, fmt.Errorf("zstd: truncated FCS")
		}
		d.contentSize = binary.LittleEndian.Uint64(d.data[d.pos : d.pos+8])
		d.hasContentSz = true
		d.pos += 8
	}

	// Decode blocks
	var output []byte
	for {
		if d.pos+3 > len(d.data) {
			return nil, fmt.Errorf("zstd: truncated block header")
		}
		bhdr := uint32(d.data[d.pos]) | uint32(d.data[d.pos+1])<<8 | uint32(d.data[d.pos+2])<<16
		d.pos += 3

		lastBlock := (bhdr & 1) != 0
		blockType := (bhdr >> 1) & 3
		blockSize := int(bhdr >> 3)

		switch blockType {
		case 0: // Raw_Block
			if d.pos+blockSize > len(d.data) {
				return nil, fmt.Errorf("zstd: raw block truncated")
			}
			output = append(output, d.data[d.pos:d.pos+blockSize]...)
			d.pos += blockSize
		case 1: // RLE_Block
			if d.pos >= len(d.data) {
				return nil, fmt.Errorf("zstd: RLE block truncated")
			}
			b := d.data[d.pos]
			d.pos++
			for i := 0; i < blockSize; i++ {
				output = append(output, b)
			}
		case 2: // Compressed_Block
			if d.pos+blockSize > len(d.data) {
				return nil, fmt.Errorf("zstd: compressed block truncated")
			}
			blockData := d.data[d.pos : d.pos+blockSize]
			d.pos += blockSize
			decoded, err := d.decodeCompressedBlock(blockData, output)
			if err != nil {
				return nil, fmt.Errorf("zstd: compressed block: %w", err)
			}
			output = append(output, decoded...)
		case 3:
			return nil, fmt.Errorf("zstd: reserved block type")
		}

		if lastBlock {
			break
		}
	}

	// Skip checksum
	if d.hasChecksum {
		d.pos += 4
	}

	return output, nil
}

// ---- Compressed Block Decoding ----

func (d *zstdDecoder) decodeCompressedBlock(blockData []byte, prevOutput []byte) ([]byte, error) {
	r := &blockReader{data: blockData, pos: 0}

	// Literals Section
	literals, err := r.decodeLiterals()
	if err != nil {
		return nil, fmt.Errorf("literals: %w", err)
	}

	// Sequences Section
	sequences, err := r.decodeSequences()
	if err != nil {
		return nil, fmt.Errorf("sequences: %w", err)
	}

	// Execute sequences
	return executeSequences(literals, sequences, prevOutput)
}

type blockReader struct {
	data []byte
	pos  int
}

type sequence struct {
	litLen   int
	matchLen int
	offset   int
}

// ---- Literals Decoding ----

func (r *blockReader) decodeLiterals() ([]byte, error) {
	if r.pos >= len(r.data) {
		return nil, fmt.Errorf("truncated literals header")
	}

	hdr := r.data[r.pos]
	litType := hdr & 3

	switch litType {
	case 0: // Raw_Literals_Block
		sizeFormat := (hdr >> 2) & 3
		var size int
		switch {
		case sizeFormat < 2:
			size = int(hdr >> 3)
			r.pos++
		case sizeFormat == 2:
			if r.pos+1 >= len(r.data) {
				return nil, fmt.Errorf("truncated raw lit header")
			}
			size = int(hdr>>4) | int(r.data[r.pos+1])<<4
			r.pos += 2
		case sizeFormat == 3:
			if r.pos+2 >= len(r.data) {
				return nil, fmt.Errorf("truncated raw lit header")
			}
			size = int(hdr>>4) | int(r.data[r.pos+1])<<4 | int(r.data[r.pos+2])<<12
			r.pos += 3
		}
		if r.pos+size > len(r.data) {
			return nil, fmt.Errorf("raw literals truncated")
		}
		lit := make([]byte, size)
		copy(lit, r.data[r.pos:r.pos+size])
		r.pos += size
		return lit, nil

	case 1: // RLE_Literals_Block
		sizeFormat := (hdr >> 2) & 3
		var size int
		switch {
		case sizeFormat < 2:
			size = int(hdr >> 3)
			r.pos++
		case sizeFormat == 2:
			if r.pos+1 >= len(r.data) {
				return nil, fmt.Errorf("truncated RLE lit header")
			}
			size = int(hdr>>4) | int(r.data[r.pos+1])<<4
			r.pos += 2
		case sizeFormat == 3:
			if r.pos+2 >= len(r.data) {
				return nil, fmt.Errorf("truncated RLE lit header")
			}
			size = int(hdr>>4) | int(r.data[r.pos+1])<<4 | int(r.data[r.pos+2])<<12
			r.pos += 3
		}
		if r.pos >= len(r.data) {
			return nil, fmt.Errorf("RLE literals truncated")
		}
		b := r.data[r.pos]
		r.pos++
		lit := make([]byte, size)
		for i := range lit {
			lit[i] = b
		}
		return lit, nil

	case 2, 3: // Compressed / Treeless
		return r.decodeCompressedLiterals(litType)
	}

	return nil, fmt.Errorf("invalid literal type")
}

func (r *blockReader) decodeCompressedLiterals(litType byte) ([]byte, error) {
	if r.pos+3 > len(r.data) {
		return nil, fmt.Errorf("truncated compressed lit header")
	}

	hdr0 := r.data[r.pos]
	sizeFormat := (hdr0 >> 2) & 3

	var regeneratedSize, compressedSize int
	var numStreams int

	switch sizeFormat {
	case 0: // 4 streams, sizes use 10 bits each
		if r.pos+3 > len(r.data) {
			return nil, fmt.Errorf("truncated compressed lit header")
		}
		v := uint32(r.data[r.pos]) | uint32(r.data[r.pos+1])<<8 | uint32(r.data[r.pos+2])<<16
		numStreams = 4
		regeneratedSize = int((v >> 4) & 0x3FF)
		compressedSize = int((v >> 14) & 0x3FF)
		r.pos += 3
	case 1: // 1 stream, sizes use 10 bits each
		if r.pos+3 > len(r.data) {
			return nil, fmt.Errorf("truncated compressed lit header")
		}
		v := uint32(r.data[r.pos]) | uint32(r.data[r.pos+1])<<8 | uint32(r.data[r.pos+2])<<16
		numStreams = 1
		regeneratedSize = int((v >> 4) & 0x3FF)
		compressedSize = int((v >> 14) & 0x3FF)
		r.pos += 3
	case 2: // 4 streams, sizes use 14 bits each
		if r.pos+4 > len(r.data) {
			return nil, fmt.Errorf("truncated compressed lit header")
		}
		v := uint32(r.data[r.pos]) | uint32(r.data[r.pos+1])<<8 | uint32(r.data[r.pos+2])<<16 | uint32(r.data[r.pos+3])<<24
		numStreams = 4
		regeneratedSize = int((v >> 4) & 0x3FFF)
		compressedSize = int((v >> 18) & 0x3FFF)
		r.pos += 4
	case 3: // 4 streams, sizes use 18 bits each
		if r.pos+5 > len(r.data) {
			return nil, fmt.Errorf("truncated compressed lit header")
		}
		v := uint64(r.data[r.pos]) | uint64(r.data[r.pos+1])<<8 | uint64(r.data[r.pos+2])<<16 |
			uint64(r.data[r.pos+3])<<24 | uint64(r.data[r.pos+4])<<32
		numStreams = 4
		regeneratedSize = int((v >> 4) & 0x3FFFF)
		compressedSize = int((v >> 22) & 0x3FFFF)
		r.pos += 5
	}

	if r.pos+compressedSize > len(r.data) {
		return nil, fmt.Errorf("compressed literals truncated")
	}

	compData := r.data[r.pos : r.pos+compressedSize]
	r.pos += compressedSize

	// Decode Huffman tree from compressed data
	tree, treeSize, err := decodeHuffmanTree(compData)
	if err != nil {
		return nil, fmt.Errorf("huffman tree: %w", err)
	}

	huffData := compData[treeSize:]

	if numStreams == 1 {
		return decodeHuffmanStream(huffData, tree, regeneratedSize)
	}

	// 4 streams: first 6 bytes are 3 x uint16 jump table
	if len(huffData) < 6 {
		return nil, fmt.Errorf("4-stream jump table truncated")
	}
	s1Len := int(binary.LittleEndian.Uint16(huffData[0:2]))
	s2Len := int(binary.LittleEndian.Uint16(huffData[2:4]))
	s3Len := int(binary.LittleEndian.Uint16(huffData[4:6]))
	huffData = huffData[6:]

	s4Len := len(huffData) - s1Len - s2Len - s3Len
	if s4Len < 0 || s1Len+s2Len+s3Len > len(huffData) {
		return nil, fmt.Errorf("4-stream sizes invalid")
	}

	// Calculate regenerated sizes per stream
	segSize := regeneratedSize / 4
	lastSeg := regeneratedSize - 3*segSize

	streamSizes := [][2]int{
		{0, s1Len},
		{s1Len, s1Len + s2Len},
		{s1Len + s2Len, s1Len + s2Len + s3Len},
		{s1Len + s2Len + s3Len, len(huffData)},
	}
	regenSizes := [4]int{segSize, segSize, segSize, lastSeg}

	output := make([]byte, 0, regeneratedSize)
	for i := 0; i < 4; i++ {
		stream := huffData[streamSizes[i][0]:streamSizes[i][1]]
		decoded, err := decodeHuffmanStream(stream, tree, regenSizes[i])
		if err != nil {
			return nil, fmt.Errorf("stream %d: %w", i, err)
		}
		output = append(output, decoded...)
	}

	return output, nil
}

// ---- Huffman Tree ----

type huffmanTree struct {
	symbols [256]byte
	nbBits  [256]byte
	maxBits int
	// Decoding table
	table    []huffEntry
	tableLog int
}

type huffEntry struct {
	symbol byte
	nbBits byte
}

func decodeHuffmanTree(data []byte) (*huffmanTree, int, error) {
	if len(data) == 0 {
		return nil, 0, fmt.Errorf("empty huffman header")
	}

	headerByte := data[0]
	var weights [256]byte
	var numSymbols int
	var consumed int

	if headerByte < 128 {
		// FSE compressed weights
		compSize := int(headerByte)
		if 1+compSize > len(data) {
			return nil, 0, fmt.Errorf("huffman FSE data truncated")
		}
		fseData := data[1 : 1+compSize]
		w, n, err := decodeFSEWeights(fseData)
		if err != nil {
			return nil, 0, err
		}
		copy(weights[:], w)
		numSymbols = n
		consumed = 1 + compSize
	} else {
		// Direct representation
		numSymbols = int(headerByte) - 127
		numBytes := (numSymbols + 1) / 2
		if 1+numBytes > len(data) {
			return nil, 0, fmt.Errorf("huffman direct weights truncated")
		}
		for i := 0; i < numSymbols; i++ {
			if i%2 == 0 {
				weights[i] = data[1+i/2] >> 4
			} else {
				weights[i] = data[1+i/2] & 0x0F
			}
		}
		consumed = 1 + numBytes
	}

	// Build tree from weights
	tree, err := buildHuffmanTable(weights[:], numSymbols)
	if err != nil {
		return nil, 0, err
	}

	return tree, consumed, nil
}

func buildHuffmanTable(weights []byte, numSymbols int) (*huffmanTree, error) {
	tree := &huffmanTree{}

	// Find max weight
	var maxWeight byte
	var weightSum uint32
	for i := 0; i < numSymbols; i++ {
		if weights[i] > maxWeight {
			maxWeight = weights[i]
		}
		if weights[i] > 0 {
			weightSum += 1 << (weights[i] - 1)
		}
	}

	// Determine maxBits (tableLog)
	tableLog := bits.Len32(weightSum) // highest bit position
	if tableLog == 0 {
		return nil, fmt.Errorf("invalid huffman weights")
	}

	// Last symbol weight
	targetSum := uint32(1) << tableLog
	if weightSum > targetSum {
		return nil, fmt.Errorf("huffman weight sum too large")
	}
	remaining := targetSum - weightSum
	if remaining == 0 || (remaining&(remaining-1)) != 0 {
		return nil, fmt.Errorf("invalid huffman weight remainder: %d", remaining)
	}
	lastWeight := byte(bits.Len32(remaining))

	// Add last symbol
	weights[numSymbols] = lastWeight
	numSymbols++

	tree.maxBits = tableLog
	tree.tableLog = tableLog

	// Assign bit lengths: nbBits = maxBits + 1 - weight
	for i := 0; i < numSymbols; i++ {
		if weights[i] > 0 {
			tree.nbBits[i] = byte(tableLog + 1 - int(weights[i]))
		}
	}

	// Build decoding table
	tableSize := 1 << tableLog
	tree.table = make([]huffEntry, tableSize)

	// Sort symbols by weight and fill table
	var pos int
	for w := byte(1); w <= byte(tableLog)+1; w++ {
		nbBits := byte(tableLog) + 1 - w
		length := 1 << nbBits
		for sym := 0; sym < numSymbols; sym++ {
			if weights[sym] == w {
				for j := 0; j < length && pos+j < tableSize; j++ {
					tree.table[pos+j] = huffEntry{symbol: byte(sym), nbBits: nbBits}
				}
				pos += length
			}
		}
	}

	return tree, nil
}

func decodeHuffmanStream(data []byte, tree *huffmanTree, outputSize int) ([]byte, error) {
	if len(data) == 0 {
		return make([]byte, outputSize), nil
	}

	br := newReverseBitReader(data)
	output := make([]byte, 0, outputSize)

	for len(output) < outputSize {
		bits, err := br.peekBits(tree.tableLog)
		if err != nil {
			if len(output) > 0 {
				break
			}
			return nil, fmt.Errorf("huffman decode: %w", err)
		}
		entry := tree.table[bits]
		br.consumeBits(int(entry.nbBits))
		output = append(output, entry.symbol)
	}

	if len(output) > outputSize {
		output = output[:outputSize]
	}
	return output, nil
}

// ---- FSE (Finite State Entropy) ----

type fseTable struct {
	tableLog  int
	tableSize int
	entries   []fseEntry

	// Parallel arrays used by zstd_compress / zstd_fse_custom
	accuracyLog int
	stateCount  int
	symbols     []byte
	numBits     []byte
	newState    []uint16
}

type fseEntry struct {
	symbol   byte
	nbBits   byte
	newState uint16
}

// Predefined FSE distribution for Huffman weight decoding
func decodeFSEWeights(data []byte) ([]byte, int, error) {
	br := &forwardBitReader{data: data, pos: 0, bitPos: 0}

	// Read FSE table description
	table, err := readFSETable(br, 6) // max accuracy log 6 for huffman weights
	if err != nil {
		return nil, 0, fmt.Errorf("FSE table for weights: %w", err)
	}

	// Decode weights using FSE
	// Initialize state
	state1, err := br.readBits(table.tableLog)
	if err != nil {
		return nil, 0, err
	}
	state2, err := br.readBits(table.tableLog)
	if err != nil {
		return nil, 0, err
	}

	var weights []byte
	for {
		entry := table.entries[state1]
		weights = append(weights, entry.symbol)
		if br.finished() {
			entry2 := table.entries[state2]
			weights = append(weights, entry2.symbol)
			break
		}

		newBits, err := br.readBits(int(entry.nbBits))
		if err != nil {
			break
		}
		state1 = int(entry.newState) + newBits

		entry2 := table.entries[state2]
		weights = append(weights, entry2.symbol)
		if br.finished() {
			break
		}

		newBits2, err := br.readBits(int(entry2.nbBits))
		if err != nil {
			break
		}
		state2 = int(entry2.newState) + newBits2
	}

	return weights, len(weights), nil
}

func readFSETable(br *forwardBitReader, maxLog int) (*fseTable, error) {
	accuracyLog, err := br.readBits(4)
	if err != nil {
		return nil, err
	}
	accuracyLog += 5
	if accuracyLog > maxLog+5 {
		accuracyLog = maxLog + 5
	}

	tableSize := 1 << accuracyLog
	remaining := tableSize + 1
	var symbol int
	var normalizedCounters [256]int16

	for remaining > 1 && symbol < 256 {
		maxBitsForCount := bits.Len(uint(remaining))
		// Read value using variable bit width
		val, err := br.readBits(maxBitsForCount)
		if err != nil {
			return nil, err
		}

		lowMask := (1 << (maxBitsForCount - 1)) - 1
		threshold := (1 << maxBitsForCount) - 1 - remaining

		if (val & lowMask) < threshold {
			val = val & lowMask
		}
		// else keep val as-is

		count := val - 1
		if count < 0 {
			// probability = -1 means "less than 1"
			normalizedCounters[symbol] = -1
			remaining--
		} else {
			normalizedCounters[symbol] = int16(count)
			remaining -= int(count)
		}
		symbol++

		// Handle zeros: if remaining allows, check for repeat flag
		if normalizedCounters[symbol-1] == 0 {
			for {
				repeat, err := br.readBits(2)
				if err != nil {
					break
				}
				for i := 0; i < repeat; i++ {
					normalizedCounters[symbol] = 0
					symbol++
				}
				if repeat < 3 {
					break
				}
			}
		}
	}

	maxSymbol := symbol

	// Build decoding table
	return buildFSETable(normalizedCounters[:maxSymbol], accuracyLog)
}

func buildFSETable(normalizedCounters []int16, accuracyLog int) (*fseTable, error) {
	tableSize := 1 << accuracyLog
	table := &fseTable{
		tableLog:  accuracyLog,
		tableSize: tableSize,
		entries:   make([]fseEntry, tableSize),
	}

	// Assign symbols to table positions
	symbolNext := make([]uint16, len(normalizedCounters))
	spread := (tableSize >> 1) + (tableSize >> 3) + 3
	mask := tableSize - 1

	// First pass: handle symbols with count = -1 (low probability)
	highThreshold := tableSize - 1
	for sym, count := range normalizedCounters {
		if count == -1 {
			table.entries[highThreshold].symbol = byte(sym)
			highThreshold--
			symbolNext[sym] = 1
		} else if count > 0 {
			symbolNext[sym] = uint16(count)
		}
	}

	// Second pass: spread symbols
	pos := 0
	for sym, count := range normalizedCounters {
		if count <= 0 {
			continue
		}
		for i := int16(0); i < count; i++ {
			table.entries[pos].symbol = byte(sym)
			pos = (pos + spread) & mask
			for pos > highThreshold {
				pos = (pos + spread) & mask
			}
		}
	}

	// Third pass: build full decoding entries
	for i := 0; i < tableSize; i++ {
		sym := table.entries[i].symbol
		nextState := symbolNext[sym]
		symbolNext[sym]++
		nbBits := byte(accuracyLog) - byte(bits.Len16(nextState)-1)
		newStateBaseline := (uint16(nextState) << nbBits) - uint16(tableSize)
		table.entries[i].nbBits = nbBits
		table.entries[i].newState = newStateBaseline
	}

	// Populate parallel arrays for encoder compatibility
	table.accuracyLog = accuracyLog
	table.stateCount = tableSize
	table.symbols = make([]byte, tableSize)
	table.numBits = make([]byte, tableSize)
	table.newState = make([]uint16, tableSize)
	for i := 0; i < tableSize; i++ {
		table.symbols[i] = table.entries[i].symbol
		table.numBits[i] = table.entries[i].nbBits
		table.newState[i] = table.entries[i].newState
	}

	return table, nil
}

// ---- Sequences Decoding ----

// Predefined FSE tables per zstd spec

var predefinedLitLenTable *fseTable
var predefinedMatchLenTable *fseTable
var predefinedOffsetTable *fseTable

func init() {
	predefinedLitLenTable = buildPredefinedLitLen()
	predefinedMatchLenTable = buildPredefinedMatchLen()
	predefinedOffsetTable = buildPredefinedOffset()
}

func buildPredefinedLitLen() *fseTable {
	// Predefined distribution for literal lengths (accuracy log = 6, 64 entries)
	dist := []int16{
		4, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
		2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 2, 1, 1, 1, 1, 1,
		-1, -1, -1, -1,
	}
	t, _ := buildFSETable(dist, 6)
	return t
}

func buildPredefinedMatchLen() *fseTable {
	// Predefined distribution for match lengths (accuracy log = 6, 64 entries)
	dist := []int16{
		1, 4, 3, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, -1, -1,
		-1, -1, -1, -1, -1,
	}
	t, _ := buildFSETable(dist, 6)
	return t
}

func buildPredefinedOffset() *fseTable {
	// Predefined distribution for offsets (accuracy log = 5, 32 entries)
	dist := []int16{
		1, 1, 1, 1, 1, 1, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1, -1, -1, -1, -1, -1,
	}
	t, _ := buildFSETable(dist, 5)
	return t
}

// Baseline and extra bits tables from the zstd spec
var litLenBaseline = [36]int{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	16, 18, 20, 24, 28, 32, 40, 48, 64, 128, 256, 512, 1024, 2048, 4096, 8192,
	16384, 32768, 65536, 131072,
}
var litLenExtraBits = [36]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 2, 2, 3, 3, 4, 6, 7, 8, 9, 10, 11, 12, 13,
	14, 15, 16, 17,
}

var matchLenBaseline = [53]int{
	3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18,
	19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34,
	35, 37, 39, 43, 47, 51, 59, 67, 83, 99, 131, 259, 515, 1027, 2051, 4099,
	8195, 16387, 32771, 65539, 131075,
}
var matchLenExtraBits = [53]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 2, 2, 2, 3, 3, 4, 4, 5, 7, 8, 9, 10, 11,
	12, 13, 14, 15, 16,
}

func (r *blockReader) decodeSequences() ([]sequence, error) {
	if r.pos >= len(r.data) {
		// No sequences
		return nil, nil
	}

	// Number of sequences
	seqHeader := r.data[r.pos]
	r.pos++
	var numSequences int

	if seqHeader == 0 {
		return nil, nil
	} else if seqHeader < 128 {
		numSequences = int(seqHeader)
	} else if seqHeader < 255 {
		if r.pos >= len(r.data) {
			return nil, fmt.Errorf("truncated seq count")
		}
		numSequences = ((int(seqHeader) - 128) << 8) + int(r.data[r.pos])
		r.pos++
	} else {
		if r.pos+1 >= len(r.data) {
			return nil, fmt.Errorf("truncated seq count")
		}
		numSequences = int(r.data[r.pos]) + int(r.data[r.pos+1])<<8 + 0x7F00
		r.pos += 2
	}

	if numSequences == 0 {
		return nil, nil
	}

	// Symbol compression modes
	if r.pos >= len(r.data) {
		return nil, fmt.Errorf("truncated compression modes")
	}
	compModes := r.data[r.pos]
	r.pos++

	llMode := (compModes >> 6) & 3
	ofMode := (compModes >> 4) & 3
	mlMode := (compModes >> 2) & 3

	// Get FSE tables based on modes
	llTable, err := r.getSeqTable(llMode, predefinedLitLenTable, 9)
	if err != nil {
		return nil, fmt.Errorf("litlen table: %w", err)
	}
	ofTable, err := r.getSeqTable(ofMode, predefinedOffsetTable, 8)
	if err != nil {
		return nil, fmt.Errorf("offset table: %w", err)
	}
	mlTable, err := r.getSeqTable(mlMode, predefinedMatchLenTable, 9)
	if err != nil {
		return nil, fmt.Errorf("matchlen table: %w", err)
	}

	// Decode sequences from bitstream (read backward)
	seqData := r.data[r.pos:]
	r.pos = len(r.data)

	br := newReverseBitReader(seqData)

	// Initialize states
	llState, err := br.readBitsForward(llTable.tableLog)
	if err != nil {
		return nil, fmt.Errorf("init LL state: %w", err)
	}
	ofState, err := br.readBitsForward(ofTable.tableLog)
	if err != nil {
		return nil, fmt.Errorf("init OF state: %w", err)
	}
	mlState, err := br.readBitsForward(mlTable.tableLog)
	if err != nil {
		return nil, fmt.Errorf("init ML state: %w", err)
	}

	sequences := make([]sequence, numSequences)

	for i := 0; i < numSequences; i++ {
		// Decode values from current states
		ofEntry := ofTable.entries[ofState]
		llEntry := llTable.entries[llState]
		mlEntry := mlTable.entries[mlState]

		ofCode := int(ofEntry.symbol)
		llCode := int(llEntry.symbol)
		mlCode := int(mlEntry.symbol)

		// Offset: code gives the number of extra bits
		var offset int
		if ofCode > 0 {
			extraBits, _ := br.readBitsForward(ofCode)
			offset = (1 << ofCode) + extraBits
		} else {
			offset = 1
		}

		// Match length
		var matchLen int
		if mlCode < len(matchLenBaseline) {
			extra := matchLenExtraBits[mlCode]
			matchLen = matchLenBaseline[mlCode]
			if extra > 0 {
				extraVal, _ := br.readBitsForward(extra)
				matchLen += extraVal
			}
		}

		// Literal length
		var litLen int
		if llCode < len(litLenBaseline) {
			extra := litLenExtraBits[llCode]
			litLen = litLenBaseline[llCode]
			if extra > 0 {
				extraVal, _ := br.readBitsForward(extra)
				litLen += extraVal
			}
		}

		sequences[i] = sequence{
			litLen:   litLen,
			matchLen: matchLen,
			offset:   offset,
		}

		// Update states (except on last sequence)
		if i < numSequences-1 {
			llBits, _ := br.readBitsForward(int(llEntry.nbBits))
			llState = int(llEntry.newState) + llBits

			mlBits, _ := br.readBitsForward(int(mlEntry.nbBits))
			mlState = int(mlEntry.newState) + mlBits

			ofBits, _ := br.readBitsForward(int(ofEntry.nbBits))
			ofState = int(ofEntry.newState) + ofBits
		}
	}

	return sequences, nil
}

func (r *blockReader) getSeqTable(mode byte, predefined *fseTable, maxLog int) (*fseTable, error) {
	switch mode {
	case 0: // Predefined
		return predefined, nil
	case 1: // RLE
		if r.pos >= len(r.data) {
			return nil, fmt.Errorf("RLE mode truncated")
		}
		sym := r.data[r.pos]
		r.pos++
		// Single-symbol table
		t := &fseTable{
			tableLog:  0,
			tableSize: 1,
			entries:   []fseEntry{{symbol: sym, nbBits: 0, newState: 0}},
		}
		return t, nil
	case 2: // FSE_Compressed
		br := &forwardBitReader{data: r.data[r.pos:], pos: 0, bitPos: 0}
		table, err := readFSETable(br, maxLog)
		if err != nil {
			return nil, err
		}
		r.pos += br.bytesConsumed()
		return table, nil
	case 3: // Repeat - not supported without previous frame
		return predefined, nil // fallback
	}
	return nil, fmt.Errorf("invalid mode %d", mode)
}

// ---- Sequence Execution ----

func executeSequences(literals []byte, sequences []sequence, prevOutput []byte) ([]byte, error) {
	var output []byte
	litPos := 0

	// Offset history
	offset1, offset2, offset3 := 1, 4, 8

	for _, seq := range sequences {
		// Copy literals
		if litPos+seq.litLen > len(literals) {
			return nil, fmt.Errorf("literal overflow")
		}
		output = append(output, literals[litPos:litPos+seq.litLen]...)
		litPos += seq.litLen

		// Handle repeat offsets
		actualOffset := seq.offset
		if seq.litLen > 0 {
			switch actualOffset {
			case 1:
				actualOffset = offset1
			case 2:
				actualOffset = offset2
			case 3:
				actualOffset = offset3
			default:
				actualOffset -= 3
			}
		} else {
			switch actualOffset {
			case 1:
				actualOffset = offset2
			case 2:
				actualOffset = offset3
			case 3:
				actualOffset = offset1 - 1
				if actualOffset == 0 {
					actualOffset = 1
				}
			default:
				actualOffset -= 3
			}
		}

		// Update offset history
		if actualOffset != offset1 {
			offset3 = offset2
			offset2 = offset1
			offset1 = actualOffset
		}

		// Copy match from already decoded output + prevOutput
		allData := append(prevOutput, output...)
		matchStart := len(allData) - actualOffset
		if matchStart < 0 {
			return nil, fmt.Errorf("match offset %d exceeds available data %d", actualOffset, len(allData))
		}
		for j := 0; j < seq.matchLen; j++ {
			idx := matchStart + j
			if idx < len(allData) {
				output = append(output, allData[idx])
			} else {
				// Overlapping match — read from output that was already appended
				outIdx := idx - len(prevOutput)
				if outIdx >= 0 && outIdx < len(output) {
					output = append(output, output[outIdx])
				} else {
					output = append(output, 0)
				}
			}
		}
	}

	// Remaining literals
	if litPos < len(literals) {
		output = append(output, literals[litPos:]...)
	}

	return output, nil
}

// ---- Bit Readers ----

// reverseBitReader reads bits from a backward bitstream (MSB first, last byte first)
type reverseBitReader struct {
	data         []byte
	bytePos      int
	bitsAvail    int
	bitContainer uint64
}

func newReverseBitReader(data []byte) *reverseBitReader {
	if len(data) == 0 {
		return &reverseBitReader{}
	}

	// Find the sentinel bit in the last byte
	lastByte := data[len(data)-1]
	if lastByte == 0 {
		// scan backward for non-zero
		for len(data) > 0 && data[len(data)-1] == 0 {
			data = data[:len(data)-1]
		}
		if len(data) == 0 {
			return &reverseBitReader{}
		}
		lastByte = data[len(data)-1]
	}

	highBit := bits.Len8(lastByte) - 1 // position of sentinel
	bitsInLast := highBit              // actual data bits (excluding sentinel)

	br := &reverseBitReader{
		data:    data,
		bytePos: len(data) - 2,
	}

	// Load initial bits
	br.bitContainer = uint64(lastByte)
	br.bitsAvail = bitsInLast

	// Remove sentinel
	br.bitContainer &= (1 << uint(bitsInLast)) - 1

	// Fill more bytes
	br.refill()

	return br
}

func (br *reverseBitReader) refill() {
	for br.bitsAvail <= 56 && br.bytePos >= 0 {
		br.bitContainer = (br.bitContainer << 8) | uint64(br.data[br.bytePos])
		br.bytePos--
		br.bitsAvail += 8
	}
}

func (br *reverseBitReader) peekBits(n int) (int, error) {
	if n > br.bitsAvail {
		return 0, fmt.Errorf("not enough bits: need %d have %d", n, br.bitsAvail)
	}
	return int((br.bitContainer >> uint(br.bitsAvail-n)) & ((1 << uint(n)) - 1)), nil
}

func (br *reverseBitReader) consumeBits(n int) {
	br.bitsAvail -= n
}

func (br *reverseBitReader) readBitsForward(n int) (int, error) {
	if n == 0 {
		return 0, nil
	}
	v, err := br.peekBits(n)
	if err != nil {
		return 0, err
	}
	br.consumeBits(n)
	return v, nil
}

// forwardBitReader reads bits forward from a byte stream
type forwardBitReader struct {
	data   []byte
	pos    int
	bitPos int
}

func (br *forwardBitReader) readBits(n int) (int, error) {
	if n == 0 {
		return 0, nil
	}

	var result int
	bitsRead := 0

	for bitsRead < n {
		if br.pos >= len(br.data) {
			return result, fmt.Errorf("forward bit reader: out of data")
		}

		availBits := 8 - br.bitPos
		needBits := n - bitsRead
		takeBits := availBits
		if takeBits > needBits {
			takeBits = needBits
		}

		bits := int((br.data[br.pos] >> uint(br.bitPos)) & ((1 << uint(takeBits)) - 1))
		result |= bits << uint(bitsRead)
		bitsRead += takeBits
		br.bitPos += takeBits

		if br.bitPos >= 8 {
			br.bitPos = 0
			br.pos++
		}
	}

	return result, nil
}

func (br *forwardBitReader) finished() bool {
	return br.pos >= len(br.data)
}

func (br *forwardBitReader) bytesConsumed() int {
	consumed := br.pos
	if br.bitPos > 0 {
		consumed++
	}
	return consumed
}

// Aliases used by zstd_compress.go
var zstdLLBaseline = litLenBaseline[:]
var zstdLLBits = litLenExtraBits[:]
var zstdMLBaseline = matchLenBaseline[:]
var zstdMLBits = matchLenExtraBits[:]

// Default probability distributions per zstd spec
var zstdLLDefaultProbs = []int16{
	4, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 2, 1, 1, 1, 1, 1,
	-1, -1, -1, -1,
}

var zstdOFDefaultProbs = []int16{
	1, 1, 1, 1, 1, 1, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, -1, -1, -1, -1, -1,
}

var zstdMLDefaultProbs = []int16{
	1, 4, 3, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, -1, -1,
	-1, -1, -1, -1, -1,
}

// zstdHighBit returns the position of the highest set bit (0-indexed).
func zstdHighBit(v uint32) byte {
	if v == 0 {
		return 0
	}
	return byte(bits.Len32(v) - 1)
}
