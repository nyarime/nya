package nya

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSortSolidFilePaths(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	aGo := mk("a.go", strings.Repeat("package main\n", 100))
	bGo := mk("b.go", strings.Repeat("package lib\n", 200))
	zTxt := mk("z.txt", strings.Repeat("hello\n", 50))
	aTxt := mk("a.txt", strings.Repeat("world\n", 80))
	noExt := mk("README", strings.Repeat("readme\n", 10))
	jsonA := mk("data/a.json", `{"a":1}`+"\n"+strings.Repeat(`{"k":"v"}`+"\n", 50))
	jsonB := mk("data/b.json", `{"b":2}`+"\n"+strings.Repeat(`{"n":42}`+"\n", 80))

	got := sortSolidFilePaths([]string{zTxt, noExt, bGo, aGo, aTxt, jsonB, jsonA})
	want := []string{bGo, aGo, jsonB, jsonA, aTxt, zTxt, noExt}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: got %q want %q (full: %v)", i, filepath.Base(got[i]), filepath.Base(want[i]), bases(got))
		}
	}
}

func bases(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

func samePathOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSolidSortImprovesCompression(t *testing.T) {
	dir := t.TempDir()
	var goPaths, logPaths []string
	for i := 0; i < 8; i++ {
		body := strings.Repeat("func worker() { return 42 }\n", 400)
		goPath := filepath.Join(dir, "pkg", "file"+string(rune('a'+i))+".go")
		if err := os.MkdirAll(filepath.Dir(goPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goPath, []byte("package pkg\n"+body), 0o644); err != nil {
			t.Fatal(err)
		}
		goPaths = append(goPaths, goPath)

		body = strings.Repeat("log line entry\n", 400)
		logPath := filepath.Join(dir, "logs", "app"+string(rune('a'+i))+".log")
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(logPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		logPaths = append(logPaths, logPath)
	}
	// Bad order: alternate .go and .log instead of grouping by extension.
	var paths []string
	for i := 0; i < 8; i++ {
		if i%2 == 0 {
			paths = append(paths, goPaths[i], logPaths[i])
		} else {
			paths = append(paths, logPaths[i], goPaths[i])
		}
	}

	sorted := sortSolidFilePaths(append([]string(nil), paths...))
	if samePathOrder(paths, sorted) {
		t.Fatal("sort did not change order")
	}

	concat := func(order []string) []byte {
		var buf bytes.Buffer
		for _, p := range order {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			buf.Write(raw)
		}
		return buf.Bytes()
	}

	opts := Lzma2Options{DictSize: lzma2DictSize, Depth: 32, NiceLen: 64}
	walkOrder, err := Lzma2CompressOpts(concat(paths), opts)
	if err != nil {
		t.Fatal(err)
	}
	solidOrder, err := Lzma2CompressOpts(concat(sorted), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(solidOrder) >= len(walkOrder) {
		t.Fatalf("sorted solid stream should compress smaller: sorted=%d walk=%d", len(solidOrder), len(walkOrder))
	}
	t.Logf("walk-order %d bytes, extension-sorted %d bytes (%.1f%%)", len(walkOrder), len(solidOrder), float64(len(solidOrder))/float64(len(walkOrder))*100)
}

func TestSolidArchiveUsesSort(t *testing.T) {
	solidIntegrationSerial(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "tree")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		p := filepath.Join(sub, "f"+string(rune('a'+i))+".rs")
		if err := os.WriteFile(p, []byte(strings.Repeat("fn main(){}\n", 500)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		p := filepath.Join(sub, "g"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(p, []byte(strings.Repeat("line\n", 500)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	archive := filepath.Join(t.TempDir(), "solid.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, 9, true)
	if err := w.AddFile(sub); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		for _, ext := range []string{".rs", ".txt"} {
			name := "f" + string(rune('a'+i)) + ext
			if ext == ".txt" {
				name = "g" + string(rune('a'+i)) + ext
			}
			p := filepath.Join(out, filepath.Base(sub), name)
			if _, err := os.Stat(p); err != nil {
				t.Errorf("missing %s: %v", name, err)
			}
		}
	}
}
