package nya

import (
	"fmt"
	"testing"
)

func TestLzma2CompressRoundTripAfterSIMDMatch(t *testing.T) {
	payloads := [][]byte{
		nil,
		[]byte("hello lzma2"),
		bytesRepeatPattern(256, 0x41),
		makeStructuredText(64 << 10),
	}
	for i, src := range payloads {
		if len(src) == 0 {
			out, err := Lzma2Compress(nil, 1<<20)
			if err != nil || len(out) != 1 || out[0] != 0x00 {
				t.Fatalf("empty: %v %v", out, err)
			}
			continue
		}
		for _, level := range []int{5, 7, 9} {
			opts := specForLevel(level).lzma
			out, err := Lzma2CompressOpts(src, opts)
			if err != nil {
				t.Fatalf("payload %d level %d: %v", i, level, err)
			}
			dec, err := Lzma2Decompress(out, opts.DictSize)
			if err != nil {
				t.Fatalf("decode payload %d level %d: %v", i, level, err)
			}
			if string(dec) != string(src) {
				t.Fatalf("payload %d level %d: len %d vs %d", i, level, len(dec), len(src))
			}
		}
	}
}

func bytesRepeatPattern(n int, b byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func makeStructuredText(n int) []byte {
	var b []byte
	line := []byte("func benchLZMA2() { return 42 }\n")
	for len(b) < n {
		b = append(b, line...)
	}
	return b[:n]
}

func TestLzma2GreedyLazyLookahead(t *testing.T) {
	// Pattern where one literal unlocks a longer repeat.
	src := []byte("aaaaXaaaaYaaaa")
	opts := Lzma2Options{DictSize: 1 << 20, Depth: 64, NiceLen: 64}
	greedy, err := Lzma2CompressOpts(src, opts)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Lzma2Decompress(greedy, opts.DictSize)
	if err != nil || string(dec) != string(src) {
		t.Fatalf("round trip: %q err=%v", dec, err)
	}
}

func TestLzma2MatchFrontierLengths(t *testing.T) {
	src := make([]byte, 512)
	for i := range src {
		src[i] = byte(i % 17)
	}
	opts := Lzma2Options{DictSize: 1 << 20, Depth: 128, NiceLen: 128}
	out, err := Lzma2CompressOpts(src, opts)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Lzma2Decompress(out, opts.DictSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(dec) != len(src) {
		t.Fatalf("len %d vs %d", len(dec), len(src))
	}
}

// Benchmark sanity: SIMD path should not regress vs scalar on amd64.
func BenchmarkLzma2CompressStructured(b *testing.B) {
	src := makeStructuredText(256 << 10)
	opts := specForLevel(9).lzma
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Lzma2CompressOpts(src, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func ExampleLzma2CompressOpts() {
	src := []byte("nyarime lzma2 push")
	out, err := Lzma2CompressOpts(src, specForLevel(9).lzma)
	if err != nil {
		panic(err)
	}
	fmt.Printf("compressed %d -> %d bytes\n", len(src), len(out))
}
