package nya

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkSolidMixedFilesLZMA(b *testing.B) {
	src := benchMixedCorpus(b)
	opts := Lzma2Options{DictSize: lzma2DictSize, Depth: 128, NiceLen: 128}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Lzma2CompressOpts(src.sorted, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSolidMixedFilesLZMA_unsorted(b *testing.B) {
	src := benchMixedCorpus(b)
	opts := Lzma2Options{DictSize: lzma2DictSize, Depth: 128, NiceLen: 128}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Lzma2CompressOpts(src.walk, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSolidMixedFilesZstd(b *testing.B) {
	src := benchMixedCorpus(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ZstdCompressWithWindow(src.sorted, 3)
	}
}

type benchCorpus struct {
	walk   []byte
	sorted []byte
}

func benchMixedCorpus(tb testing.TB) benchCorpus {
	tb.Helper()
	dir := tb.TempDir()
	var paths []string
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, "code", "m"+string(rune('a'+i))+".go")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			tb.Fatal(err)
		}
		body := strings.Repeat("package main\nfunc f() int { return 1 }\n", 300)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			tb.Fatal(err)
		}
		paths = append(paths, p)
	}
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, "data", "d"+string(rune('a'+i))+".json")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			tb.Fatal(err)
		}
		body := strings.Repeat(`{"key":"value","n":42}`+"\n", 300)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			tb.Fatal(err)
		}
		paths = append(paths, p)
	}

	concat := func(order []string) []byte {
		var buf bytes.Buffer
		for _, p := range order {
			raw, err := os.ReadFile(p)
			if err != nil {
				tb.Fatal(err)
			}
			buf.Write(raw)
		}
		return buf.Bytes()
	}
	return benchCorpus{
		walk:   concat(paths),
		sorted: concat(sortSolidFilePaths(paths)),
	}
}
