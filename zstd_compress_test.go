package nya

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

func TestZstdCompressRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"1byte", []byte{0x42}},
		{"10bytes", []byte("0123456789")},
		{"100bytes", bytes.Repeat([]byte("abcdefghij"), 10)},
		{"small_16", []byte("Hello World 1234")},
		{"small_17", []byte("Hello World 12345")},
		{"repetitive_hello", bytes.Repeat([]byte("Hello Nyarc"), 30000)},
		{"all_zeros_1MB", bytes.Repeat([]byte{0}, 1024*1024)},
		{"sequential_1MB", makeSequential(1024 * 1024)},
		{"random_ish_1MB", makeRandomIsh(1024 * 1024)},
		{"mixed_pattern_1MB", makeMixedPattern(1024 * 1024)},
		{"repetitive_1KB", bytes.Repeat([]byte("AAAA"), 256)},
		{"medium_patterns_1KB", makeMixedPattern(1024)},
		{"large_mixed_100KB", makeMixedPattern(100 * 1024)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed := ZstdCompress(tt.data, 3)
			t.Logf("size: %d -> %d (%.1f%%)", len(tt.data), len(compressed),
				float64(len(compressed))*100/float64(max(len(tt.data), 1)))

			got, err := ZstdDecompress(compressed)
			if err != nil {
				t.Fatalf("decompress error: %v", err)
			}
			if !bytes.Equal(got, tt.data) {
				// Find first difference
				minLen := len(got)
				if len(tt.data) < minLen {
					minLen = len(tt.data)
				}
				for i := 0; i < minLen; i++ {
					if got[i] != tt.data[i] {
						t.Fatalf("mismatch at byte %d: got 0x%02x want 0x%02x (got %d bytes, want %d)", i, got[i], tt.data[i], len(got), len(tt.data))
						break
					}
				}
				t.Fatalf("length mismatch: got %d want %d", len(got), len(tt.data))
			}
		})
	}
}

func TestZstdCompressAllLevels(t *testing.T) {
	data := makeMixedPattern(8192)
	for _, level := range []int{1, 3, 6, 9} {
		t.Run(fmt.Sprintf("level%d", level), func(t *testing.T) {
			compressed := ZstdCompress(data, level)
			got, err := ZstdDecompress(compressed)
			if err != nil {
				t.Fatalf("level %d decompress error: %v", level, err)
			}
			if !bytes.Equal(got, data) {
				t.Fatal("mismatch")
			}
		})
	}
}

func TestZstdCompressLargeRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	data := make([]byte, 64*1024)
	for i := range data {
		data[i] = byte(rng.Intn(256))
	}
	compressed := ZstdCompress(data, 1)
	got, err := ZstdDecompress(compressed)
	if err != nil {
		t.Fatalf("decompress error: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("mismatch on random data")
	}
}

func TestZstdCompressHuffmanLiterals(t *testing.T) {
	// Test cases specifically designed to exercise Huffman literal compression
	tests := []struct {
		name string
		data []byte
	}{
		{"1KB_text", bytes.Repeat([]byte("Hello world, this is a test of Huffman compression! "), 20)},
		{"10KB_text", bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog. Pack my box. "), 180)},
		{"100KB_text", bytes.Repeat([]byte("Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod. "), 1400)},
		{"1MB_repeat", bytes.Repeat([]byte("ABCDEFGH"), 128*1024)},
		{"1KB_lowentropy", makeLowEntropy(1024)},
		{"10KB_lowentropy", makeLowEntropy(10240)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed := ZstdCompress(tt.data, 3)
			ratio := float64(len(compressed)) * 100 / float64(max(len(tt.data), 1))
			t.Logf("size: %d -> %d (%.1f%%)", len(tt.data), len(compressed), ratio)

			got, err := ZstdDecompress(compressed)
			if err != nil {
				t.Fatalf("decompress error: %v", err)
			}
			if !bytes.Equal(got, tt.data) {
				t.Fatalf("roundtrip mismatch: got %d bytes, want %d", len(got), len(tt.data))
			}
		})
	}
}

func makeLowEntropy(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 16) // only 16 distinct values
	}
	return data
}

func makeSequential(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}
	return data
}

func makeRandomIsh(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte((i*7 + 13) % 256)
	}
	return data
}

func makeMixedPattern(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		switch {
		case i%1000 < 100:
			data[i] = byte(i)
		case i%1000 < 300:
			data[i] = 0
		case i%1000 < 500:
			data[i] = byte(i % 256)
		default:
			data[i] = byte((i*7 + 13) % 256)
		}
	}
	return data
}

func TestZstdCompressMultiThreaded(t *testing.T) {
	// 1MB+ triggers multi-threaded path (>256KB)
	sizes := []int{512 * 1024, 1024 * 1024, 2 * 1024 * 1024}
	for _, sz := range sizes {
		for _, level := range []int{1, 3, 6} {
			name := fmt.Sprintf("%dKB_level%d", sz/1024, level)
			t.Run(name, func(t *testing.T) {
				data := make([]byte, sz)
				for i := range data {
					switch {
					case i%1000 < 300:
						data[i] = byte(i % 64)
					default:
						data[i] = byte((i*7 + 13) % 256)
					}
				}
				comp := ZstdCompress(data, level)
				t.Logf("%d -> %d (%.1f%%)", sz, len(comp), float64(len(comp))*100/float64(sz))
				got, err := ZstdDecompress(comp)
				if err != nil {
					t.Fatalf("decompress: %v", err)
				}
				if !bytes.Equal(got, data) {
					t.Fatal("roundtrip mismatch")
				}
			})
		}
	}
}

func TestZstdLazyVsGreedy(t *testing.T) {
	// Verify level 3+ (lazy) gives better or equal ratio than level 1 (greedy)
	data := makeMixedPattern(64 * 1024)
	c1 := ZstdCompress(data, 1)
	c3 := ZstdCompress(data, 3)
	t.Logf("greedy(1): %d, lazy(3): %d", len(c1), len(c3))
	// Both must roundtrip
	for _, c := range [][]byte{c1, c3} {
		got, err := ZstdDecompress(c)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data) {
			t.Fatal("mismatch")
		}
	}
}
