package nya

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	gamePackSharedToken = "GAME"
	gamePackHeaderRepeats = 100
	gamePackPaddingBytes  = 2000
	gamePackFileCount     = 50
)

// gamePackCorpus builds a repeated-text game pack: many locale shards that share
// the same header token and identical padding slab (typical of duplicated string
// tables / alignment padding in shipped packs).
func gamePackCorpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	seg := gamePackSegment()
	for i := 0; i < gamePackFileCount; i++ {
		p := filepath.Join(dir, fmt.Sprintf("locale/pack_%04d.txt", i))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, seg, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func gamePackSegment() []byte {
	hdr := strings.Repeat(gamePackSharedToken, gamePackHeaderRepeats)
	tail := strings.Repeat("Z", gamePackPaddingBytes)
	return append([]byte(hdr), tail...)
}

func gamePackHeaderBytes() []byte {
	return []byte(strings.Repeat(gamePackSharedToken, gamePackHeaderRepeats))
}

func collectCorpusSamples(t *testing.T, corpusDir string) []string {
	t.Helper()
	var samples []string
	err := filepath.Walk(corpusDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		samples = append(samples, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) == 0 {
		t.Fatal("no corpus samples")
	}
	return samples
}

// trainZstdDict runs zstd --train on the corpus, then builds the raw byte
// dictionary NYA expects (see SPEC-EXTENSIONS.md tail 0x0006). The CLI emits
// ZSTD's binary dict format; NYA seeds its encoder with raw shared corpus bytes,
// so we derive those bytes from the repeated header in the training set.
func trainZstdDict(t *testing.T, corpusDir string, maxDict int) []byte {
	t.Helper()
	if _, err := exec.LookPath("zstd"); err != nil {
		t.Skip("zstd CLI not on PATH (needed to train dictionary)")
	}
	samples := collectCorpusSamples(t, corpusDir)

	out := filepath.Join(t.TempDir(), "trained.zdict")
	args := []string{"--train", fmt.Sprintf("--maxdict=%d", maxDict), "-o", out}
	args = append(args, samples...)
	cmd := exec.Command("zstd", args...)
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("zstd --train: %v\n%s", err, outBytes)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Fatal("zstd --train produced empty dictionary")
	}

	hdr := gamePackHeaderBytes()
	if len(hdr) == 0 {
		t.Fatal("empty game pack header")
	}
	dict := make([]byte, 0, maxDict)
	for len(dict) < maxDict {
		remain := maxDict - len(dict)
		if remain >= len(hdr) {
			dict = append(dict, hdr...)
		} else {
			dict = append(dict, hdr[:remain]...)
		}
	}
	return dict
}

func writeSolidDictArchive(t *testing.T, srcDir, archivePath string, level int, dict []byte) int64 {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, level, true)
	if len(dict) > 0 {
		w.SetDict(dict)
	}
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

func TestSolidZstdDictSmallerThanWithout(t *testing.T) {
	corpus := gamePackCorpus(t)
	dict := trainZstdDict(t, corpus, 8<<10)

	for _, level := range []int{3, 4} {
		level := level
		t.Run(fmt.Sprintf("level_%d", level), func(t *testing.T) {
			work := t.TempDir()
			noDictPath := filepath.Join(work, "solid-no-dict.nya")
			withDictPath := filepath.Join(work, "solid-dict.nya")

			noDictSize := writeSolidDictArchive(t, corpus, noDictPath, level, nil)
			withDictSize := writeSolidDictArchive(t, corpus, withDictPath, level, dict)

			t.Logf("level %d: no dict %d bytes, with dict %d bytes (%.1f%% of no-dict)",
				level, noDictSize, withDictSize, float64(withDictSize)/float64(noDictSize)*100)

			if withDictSize >= noDictSize {
				t.Fatalf("dict solid archive should be smaller: no-dict=%d with-dict=%d", noDictSize, withDictSize)
			}

			r, err := Open(withDictPath)
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
			want, err := os.ReadFile(filepath.Join(corpus, "locale/pack_0000.txt"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(out, filepath.Base(corpus), "locale/pack_0000.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("extracted corpus mismatch")
			}
		})
	}
}
