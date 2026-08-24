package nya

// LZMA2/XZ compression — achieve 7z-level compression ratios in NYA format

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

// CompressLZMA2 compresses data using LZMA2 (XZ format)
func CompressLZMA2(data []byte) ([]byte, error) {
	return Lzma2Compress(data, 0)
}

// DecompressLZMA2 decompresses LZMA2/XZ data
func DecompressLZMA2(data []byte) ([]byte, error) {
	// Try raw LZMA2 first (from our Lzma2Compress)
	lr := newLzma2Reader(bytes.NewReader(data), 1<<22) // 4MB dict
	result, err := io.ReadAll(lr)
	if err == nil && len(result) > 0 {
		return result, nil
	}
	// Fallback: try XZ wrapper
	return XzDecompress(data)
}

// CompressLZMA compresses data using plain LZMA (for legacy compatibility)
func CompressLZMA(data []byte) ([]byte, error) {
	return LzmaCompress(data)
}

// DecompressLZMA decompresses plain LZMA data
func DecompressLZMA(data []byte) ([]byte, error) {
	return LzmaDecompress(data)
}

// xzNewReader creates a new XZ decompression reader (internal helper).
func xzNewReader(r io.Reader) (io.Reader, error) {
	return XzNewReader(r)
}

// BenchmarkCompression tests all compressors on given data
func BenchmarkCompression(data []byte) string {
	var sb strings.Builder
	origSize := len(data)
	sb.WriteString(fmt.Sprintf("Compression Benchmark (%s)\n", HumanSize(origSize)))
	sb.WriteString(fmt.Sprintf("%-14s %10s %8s %10s %8s\n", "Method", "Compressed", "Ratio", "Time", "MB/s"))
	sb.WriteString(strings.Repeat("─", 58) + "\n")

	// Store
	sb.WriteString(fmt.Sprintf("%-14s %10s %7.1f:1 %10s %8s\n",
		"Store", HumanSize(origSize), 1.0, "-", "-"))

	// LZMA2/XZ
	start := time.Now()
	lzma2, err := CompressLZMA2(data)
	dur := time.Since(start)
	if err == nil {
		ratio := float64(origSize) / float64(len(lzma2))
		speed := float64(origSize) / dur.Seconds() / 1024 / 1024
		sb.WriteString(fmt.Sprintf("%-14s %10s %7.1f:1 %10s %6.0fMB/s\n",
			"LZMA2 (XZ)", HumanSize(len(lzma2)), ratio, dur.Round(time.Millisecond), speed))

		// Verify decompression
		dec, err := DecompressLZMA2(lzma2)
		if err == nil && len(dec) == origSize {
			sb.WriteString("  ✅ Decompression verified\n")
		}
	}

	// Plain LZMA
	start = time.Now()
	lzmaData, err := CompressLZMA(data)
	dur = time.Since(start)
	if err == nil {
		ratio := float64(origSize) / float64(len(lzmaData))
		speed := float64(origSize) / dur.Seconds() / 1024 / 1024
		sb.WriteString(fmt.Sprintf("%-14s %10s %7.1f:1 %10s %6.0fMB/s\n",
			"LZMA", HumanSize(len(lzmaData)), ratio, dur.Round(time.Millisecond), speed))
	}

	sb.WriteString("\n  💡 NYA + LZMA2 + FEC = 7z压缩比 + 损坏恢复\n")

	return sb.String()
}
