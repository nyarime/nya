package nya

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestLzmaOptimalSmallRoundtrip(t *testing.T) {
	data := bytes.Repeat([]byte("abc"), 1000)
	opts := Lzma2Options{DictSize: 1 << 20, Depth: 128, NiceLen: 64, OptimalParse: true}
	comp, err := Lzma2CompressOpts(data, opts)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := io.ReadAll(newLzma2Reader(bytes.NewReader(comp), 1<<22))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, data) {
		t.Fatalf("mismatch dec=%d data=%d comp=%d", len(dec), len(data), len(comp))
	}
}

func TestLzmaOptimalRoundtripLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("large optimal-parse corpus")
	}
	data := buildOptimalCorpus()
	opts := Lzma2Options{DictSize: 4 << 20, Depth: 128, NiceLen: 96, OptimalParse: true}
	comp, err := Lzma2CompressOpts(data, opts)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := io.ReadAll(newLzma2Reader(bytes.NewReader(comp), 1<<22))
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if !bytes.Equal(dec, data) {
		t.Fatalf("roundtrip mismatch: dec=%d data=%d comp=%d", len(dec), len(data), len(comp))
	}
	t.Logf("optimal+BT4: %d → %d bytes (%.1f%%)", len(data), len(comp), float64(len(comp))/float64(len(data))*100)
}

func buildOptimalCorpus() []byte {
	var buf bytes.Buffer
	chunk := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 200)
	for i := 0; i < 12; i++ {
		buf.WriteString("file")
		buf.WriteByte('A' + byte(i))
		buf.WriteString(".txt\n")
		buf.WriteString(chunk)
	}
	return buf.Bytes()
}
