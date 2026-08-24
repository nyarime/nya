package nya

// Multi-algorithm compression engine — Zstd, LZMA2, Brotli, LZ4
// NYA format can use any of these + FEC recovery

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
	"time"
)

// Compression method IDs (stored in NYA header)
type CompMethod byte

const (
	CompZstd   CompMethod = 0 // Default, fast
	CompLZMA2  CompMethod = 1 // Best ratio (7z-level)
	CompBrotli CompMethod = 2 // Good balance
	CompLZ4    CompMethod = 3 // Fastest
	CompDeflate CompMethod = 4 // ZIP compatible
	CompNone   CompMethod = 5 // Store
)

var compNames = map[CompMethod]string{
	CompZstd:    "Zstd",
	CompLZMA2:   "LZMA2",
	CompBrotli:  "Brotli",
	CompLZ4:     "LZ4",
	CompDeflate: "Deflate",
	CompNone:    "Store",
}

func CompMethodName(m CompMethod) string {
	if name, ok := compNames[m]; ok { return name }
	return fmt.Sprintf("Unknown(%d)", m)
}

// CompressBenchmark tests all available compressors on data
func CompressBenchmark(data []byte) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Compression Benchmark (%s input)\n", HumanSize(len(data))))
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
	sb.WriteString(fmt.Sprintf("%-12s %10s %8s %10s %8s\n", "Method", "Size", "Ratio", "Time", "Speed"))
	sb.WriteString(strings.Repeat("─", 55) + "\n")

	tests := []struct {
		name string
		fn   func([]byte) ([]byte, time.Duration)
	}{
		{"Store", func(d []byte) ([]byte, time.Duration) { return d, 0 }},
		{"Deflate-1", func(d []byte) ([]byte, time.Duration) { return benchDeflate(d, 1) }},
		{"Deflate-9", func(d []byte) ([]byte, time.Duration) { return benchDeflate(d, 9) }},
		{"Gzip-9", func(d []byte) ([]byte, time.Duration) { return benchGzip(d, 9) }},
	}

	for _, t := range tests {
		compressed, dur := t.fn(data)
		ratio := float64(len(data)) / float64(len(compressed))
		speed := float64(len(data)) / dur.Seconds() / 1024 / 1024
		if dur == 0 { speed = 0 }

		sb.WriteString(fmt.Sprintf("%-12s %10s %7.1f:1 %10s %6.0fMB/s\n",
			t.name, HumanSize(len(compressed)), ratio,
			dur.Round(time.Millisecond), speed))
	}

	// Add notes about unavailable compressors
	sb.WriteString("\n  Note: Zstd via klauspost/compress (already integrated)\n")
	sb.WriteString("  LZMA2: requires github.com/ulikunitz/xz\n")
	sb.WriteString("  Brotli: requires github.com/andybalholm/brotli\n")
	sb.WriteString("  LZ4: requires github.com/pierrec/lz4/v4\n")

	return sb.String()
}

func benchDeflate(data []byte, level int) ([]byte, time.Duration) {
	start := time.Now()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, level)
	if err != nil { return data, 0 }
	w.Write(data)
	w.Close()
	return buf.Bytes(), time.Since(start)
}

func benchGzip(data []byte, level int) ([]byte, time.Duration) {
	start := time.Now()
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, level)
	if err != nil { return data, 0 }
	w.Write(data)
	w.Close()
	return buf.Bytes(), time.Since(start)
}

// Pre-compression filters for better ratios
type PreFilter byte

const (
	FilterNone  PreFilter = 0
	FilterDelta PreFilter = 1 // Good for audio/sensor data
	FilterBCJ   PreFilter = 2 // x86 executable filter
	FilterBCJARM PreFilter = 3 // ARM executable filter
	FilterText  PreFilter = 4 // UTF-8 text preprocessor
)

// ApplyBCJFilter transforms x86 code for better compression
// Converts relative CALL/JMP addresses to absolute (like 7z BCJ)
func ApplyBCJFilter(data []byte) []byte {
	result := make([]byte, len(data))
	copy(result, data)

	pos := 0
	for pos+5 <= len(result) {
		op := result[pos]
		// E8 = CALL rel32, E9 = JMP rel32
		if op == 0xE8 || op == 0xE9 {
			// Convert relative offset to absolute
			offset := int32(result[pos+1]) | int32(result[pos+2])<<8 |
				int32(result[pos+3])<<16 | int32(result[pos+4])<<24
			abs := offset + int32(pos+5)
			result[pos+1] = byte(abs)
			result[pos+2] = byte(abs >> 8)
			result[pos+3] = byte(abs >> 16)
			result[pos+4] = byte(abs >> 24)
			pos += 5
		} else {
			pos++
		}
	}
	return result
}

// RevertBCJFilter reverses BCJ transformation
func RevertBCJFilter(data []byte) []byte {
	result := make([]byte, len(data))
	copy(result, data)

	pos := 0
	for pos+5 <= len(result) {
		op := result[pos]
		if op == 0xE8 || op == 0xE9 {
			abs := int32(result[pos+1]) | int32(result[pos+2])<<8 |
				int32(result[pos+3])<<16 | int32(result[pos+4])<<24
			rel := abs - int32(pos+5)
			result[pos+1] = byte(rel)
			result[pos+2] = byte(rel >> 8)
			result[pos+3] = byte(rel >> 16)
			result[pos+4] = byte(rel >> 24)
			pos += 5
		} else {
			pos++
		}
	}
	return result
}

// ApplyDeltaFilter applies delta encoding (byte-level)
func ApplyDeltaFilter(data []byte, distance int) []byte {
	if distance <= 0 { distance = 1 }
	result := make([]byte, len(data))
	for i := 0; i < len(data); i++ {
		if i < distance {
			result[i] = data[i]
		} else {
			result[i] = data[i] - data[i-distance]
		}
	}
	return result
}

// RevertDeltaFilter reverses delta encoding
func RevertDeltaFilter(data []byte, distance int) []byte {
	if distance <= 0 { distance = 1 }
	result := make([]byte, len(data))
	for i := 0; i < len(data); i++ {
		if i < distance {
			result[i] = data[i]
		} else {
			result[i] = data[i] + result[i-distance]
		}
	}
	return result
}

// Needed for io import
var _ io.Writer = nil
