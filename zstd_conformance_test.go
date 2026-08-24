package nya

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

// Archives written now must record the current format version.
func TestWriterRecordsCurrentVersion(t *testing.T) {
	if VersionMajor != 1 || VersionMinor != 0 {
		t.Fatalf("unexpected format version %d.%d", VersionMajor, VersionMinor)
	}
}

func conformanceCorpora() map[string][]byte {
	corpora := map[string][]byte{}

	corpora["exact-repeat"] = bytes.Repeat([]byte("Nyarime archive format. "), 4000)

	var structured bytes.Buffer
	for i := 0; i < 8000; i++ {
		fmt.Fprintf(&structured, "row=%d name=item-%d value=%f\n", i, i%97, float64(i)*1.5)
	}
	corpora["structured-text"] = structured.Bytes()

	rng := rand.New(rand.NewSource(7))
	var mixed bytes.Buffer
	for i := 0; i < 400; i++ {
		run := make([]byte, 20+rng.Intn(200))
		rng.Read(run)
		mixed.Write(run)
		mixed.Write(bytes.Repeat([]byte("THE_QUICK_BROWN_FOX_JUMPS_OVER_THE_LAZY_DOG_"), 2+rng.Intn(5)))
	}
	corpora["mixed"] = mixed.Bytes()

	incompressible := make([]byte, 100000)
	rand.New(rand.NewSource(11)).Read(incompressible)
	corpora["random"] = incompressible

	return corpora
}

func TestZstdRoundtripCorpora(t *testing.T) {
	for name, src := range conformanceCorpora() {
		for _, level := range []int{1, 3, 9, 19} {
			t.Run(fmt.Sprintf("%s/level%d", name, level), func(t *testing.T) {
				comp := ZstdCompress(src, level)
				got, err := DecompressZstd(comp)
				if err != nil {
					t.Fatalf("decompress: %v", err)
				}
				if !bytes.Equal(got, src) {
					t.Fatalf("roundtrip mismatch: got %d bytes, want %d", len(got), len(src))
				}
			})
		}
	}
}

// Compressed blocks used to fail the encoder's internal round-trip check and
// silently degrade to stored blocks, so every input came back at 100% of its
// original size. Guard the ratios so that cannot regress unnoticed.
func TestZstdActuallyCompresses(t *testing.T) {
	limits := map[string]float64{
		"exact-repeat":    0.05,
		"structured-text": 0.60,
		"mixed":           0.75,
	}
	for name, limit := range limits {
		src := conformanceCorpora()[name]
		comp := ZstdCompress(src, 9)
		ratio := float64(len(comp)) / float64(len(src))
		if ratio > limit {
			t.Errorf("%s: compressed to %.1f%% of input, expected at most %.1f%%",
				name, ratio*100, limit*100)
		}
	}
}

// Huffman-coded literal sections exercise the tree description, the backward
// bitstream and the canonical code assignment all at once.
func TestZstdCompressedLiterals(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&buf, "row=%d name=item-%d value=%f\n", i, i%97, float64(i)*1.5)
	}
	src := buf.Bytes()

	seqs := zstdFindSeqs(src, 9)
	if len(seqs) == 0 {
		t.Fatal("no sequences found for this input")
	}
	block := zstdBuildBlock(src, seqs)
	if block == nil {
		t.Fatal("zstdBuildBlock returned nil")
	}
	if litType := block[0] & 3; litType != 2 {
		t.Fatalf("expected a Huffman-coded literals section, got type %d", litType)
	}

	r := &blockReader{data: block}
	lits, err := r.decodeLiterals()
	if err != nil {
		t.Fatalf("decodeLiterals: %v", err)
	}
	decoded, err := r.decodeSequences()
	if err != nil {
		t.Fatalf("decodeSequences: %v", err)
	}
	out, err := executeSequences(lits, decoded, nil)
	if err != nil {
		t.Fatalf("executeSequences: %v", err)
	}
	if !bytes.Equal(out, src) {
		t.Fatalf("compressed-literals block did not round-trip (%d vs %d bytes)", len(out), len(src))
	}
}

// Repeated offsets have their own update rules, including a shifted meaning
// when the literal length is zero. Getting those wrong corrupts matches well
// into a block, so drive a payload that leans on them heavily.
func TestZstdRepeatOffsets(t *testing.T) {
	var buf bytes.Buffer
	phrases := [][]byte{
		[]byte("alpha-block-payload"),
		[]byte("beta-block-payload"),
		[]byte("gamma-block-payload"),
	}
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 3000; i++ {
		buf.Write(phrases[rng.Intn(len(phrases))])
		if rng.Intn(4) == 0 {
			buf.WriteByte(byte('a' + rng.Intn(26)))
		}
	}
	src := buf.Bytes()

	comp := ZstdCompress(src, 9)
	got, err := DecompressZstd(comp)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Fatalf("repeat-offset roundtrip mismatch: %d vs %d bytes", len(got), len(src))
	}
}
