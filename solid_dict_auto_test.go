package nya

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSolidDictGamePack(t *testing.T) {
	corpus := gamePackCorpus(t)
	samples := collectTextLikePrefixes(corpus, solidDictSamplePrefix)
	dict := buildSolidZstdDictFromSamples(samples, DefaultSolidZstdDictMax)
	manual := trainZstdDict(t, corpus, 8<<10)
	if len(dict) == 0 {
		t.Fatal("empty auto dict")
	}

	paths, _ := filepath.Glob(filepath.Join(corpus, "locale", "*.txt"))
	sorted := sortSolidFilePaths(paths)
	var data []byte
	for _, p := range sorted {
		raw, _ := os.ReadFile(p)
		data = append(data, raw...)
	}
	for _, lvl := range []int{9, 19} {
		autoC := ZstdCompressWithDict(data, lvl, dict)
		manC := ZstdCompressWithDict(data, lvl, manual)
		if _, err := DecompressZstdWithDict(autoC, dict); err != nil {
			t.Fatalf("auto roundtrip lvl %d: %v", lvl, err)
		}
		if _, err := DecompressZstdWithDict(manC, manual); err != nil {
			t.Fatalf("manual roundtrip lvl %d: %v", lvl, err)
		}
		t.Logf("zlevel %d: plain=%d auto=%d manual=%d",
			lvl, len(ZstdCompressWithWindow(data, lvl)), len(autoC), len(manC))
	}
}

func TestSolidAutoDictWired(t *testing.T) {
	corpus := gamePackCorpus(t)
	for _, level := range []int{3, 4} {
		level := level
		t.Run(fmt.Sprintf("level_%d", level), func(t *testing.T) {
			work := t.TempDir()
			noDictPath := filepath.Join(work, "no-dict.nya")
			autoPath := filepath.Join(work, "auto.nya")

			noDictSize := writeSolidDictArchive(t, corpus, noDictPath, level, nil)
			autoSize := writeSolidAutoDictArchive(t, corpus, autoPath, level)

			t.Logf("level %d: no-dict=%d auto=%d", level, noDictSize, autoSize)
			if autoSize >= noDictSize {
				t.Fatalf("auto dict should shrink archive: no-dict=%d auto=%d", noDictSize, autoSize)
			}

			r, err := Open(autoPath)
			if err != nil {
				t.Fatal(err)
			}
			if r.Header.Flags&FlagHasZstdDict == 0 {
				t.Fatal("FlagHasZstdDict not set")
			}
			out := filepath.Join(work, "out")
			if err := r.Extract(out); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSolidAutoDictSkipsWhenNoGain(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		p := filepath.Join(dir, fmt.Sprintf("blob-%d.bin", i))
		if err := os.WriteFile(p, makeBinaryBlob(i, 8192), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	arch := filepath.Join(t.TempDir(), "bin.nya")
	f, err := os.Create(arch)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, 3, true)
	if err := w.AddFile(dir); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	r, err := Open(arch)
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.Flags&FlagHasZstdDict != 0 {
		t.Fatal("binary solid archive should not embed auto dict")
	}
}

func TestSolidAutoZstdDictEligible(t *testing.T) {
	if !solidAutoZstdDictEligible(3, 10, 0, 0) {
		t.Fatal("level 3 text-heavy should be eligible")
	}
	if solidAutoZstdDictEligible(9, 6, 0, 4) {
		t.Fatal("level 9 at 60% text should not be eligible")
	}
	if !solidAutoZstdDictEligible(9, 30, 0, 10) {
		t.Fatal("level 9 at 75% text should be eligible")
	}
}

func writeSolidAutoDictArchive(t *testing.T, srcDir, archivePath string, level int) int64 {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, level, true)
	if err := w.AddFile(srcDir); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	fi, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}
