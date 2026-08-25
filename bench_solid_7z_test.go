package nya

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildSolidBenchDir lays out a mixed solid corpus (text, json, small binary)
// similar to compress/lzma2/bench_corpus.go and bench_corpus.go tree patterns.
func buildSolidBenchDir(root string) (int, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return 0, err
	}

	type fileSpec struct {
		name string
		body func(i int) []byte
	}

	specs := []fileSpec{
		{
			name: "log-%02d.txt",
			body: func(i int) []byte {
				var b strings.Builder
				for j := 0; j < 400; j++ {
					fmt.Fprintf(&b, "%s %s component=worker-%d user=u%d action=%s duration_ms=%d\n",
						fmt.Sprintf("2024-06-%02dT12:%02d:%02dZ", (j%28)+1, j%60, j%60),
						[]string{"INFO", "WARN", "ERROR"}[j%3],
						(i+j)%11, (i*17+j)%1000,
						[]string{"login", "fetch", "write", "sync"}[j%4],
						j%500,
					)
				}
				return []byte(b.String())
			},
		},
		{
			name: "events-%02d.json",
			body: func(i int) []byte {
				var b strings.Builder
				for j := 0; j < 300; j++ {
					rec := map[string]any{
						"ts":   fmt.Sprintf("2024-06-%02dT12:%02d:%02dZ", (j%28)+1, j%60, j%60),
						"lvl":  []string{"info", "warn", "error"}[j%3],
						"svc":  fmt.Sprintf("svc-%d", (i+j)%17),
						"msg":  "request handled",
						"code": 200 + (j % 5),
						"ms":   j % 400,
					}
					enc, _ := json.Marshal(rec)
					b.Write(enc)
					b.WriteByte('\n')
				}
				return []byte(b.String())
			},
		},
		{
			name: "data-%02d.bin",
			body: func(i int) []byte {
				return makeBinaryBlob(i+7, 4096+i%2048)
			},
		},
		{
			name: "readme-%02d.md",
			body: func(i int) []byte {
				var b strings.Builder
				b.WriteString("# benchmark doc\n\n")
				for j := 0; j < 80; j++ {
					fmt.Fprintf(&b, "## section %d\n\nSome **bold** and `code` with [links](https://example.com/%d).\n\n", j, j+i)
				}
				return []byte(b.String())
			},
		},
	}

	rawTotal := 0
	for n := 0; n < 36; n++ {
		spec := specs[n%len(specs)]
		name := fmt.Sprintf(spec.name, n)
		data := spec.body(n)
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			return 0, err
		}
		rawTotal += len(data)
	}
	return rawTotal, nil
}

func bench7zSolidLZMA2(srcDir, outDir string) (int, error) {
	out := filepath.Join(outDir, "ref.7z")
	_ = os.Remove(out)
	cmd := exec.Command("7z", "a", "-bd", "-mx9", "-m0=lzma2", "-ms=on", "-bso0", "-bsp0", out, srcDir)
	cmd.Dir = outDir
	if msg, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("7z: %w: %s", err, msg)
	}
	fi, err := os.Stat(out)
	if err != nil {
		return 0, err
	}
	return int(fi.Size()), nil
}

// TestSolidArchiveVs7z compares a NYA solid level-9 archive against
// 7z a -mx9 -m0=lzma2 -ms=on on the same mixed-file directory.
func TestSolidArchiveVs7z(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping solid vs 7z benchmark in -short mode")
	}
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z not installed")
	}

	root := t.TempDir()
	srcDir := filepath.Join(root, "corpus")
	rawTotal, err := buildSolidBenchDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}

	nyaDir := filepath.Join(root, "nya-out")
	if err := os.MkdirAll(nyaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nyaSize, _, err := writeSolidArchive(srcDir, nyaDir, 9, 0)
	if err != nil {
		t.Fatal(err)
	}

	z7Size, err := bench7zSolidLZMA2(srcDir, root)
	if err != nil {
		t.Fatal(err)
	}

	nyaPct := float64(nyaSize) / float64(rawTotal) * 100
	sevenPct := float64(z7Size) / float64(rawTotal) * 100
	gap := nyaPct - sevenPct

	t.Log("solid archive vs 7z -mx9 -m0=lzma2 -ms=on:")
	t.Logf("raw_total=%d nya_size=%d (%.2f%%) 7z_size=%d (%.2f%%) gap=%+.2fpp",
		rawTotal, nyaSize, nyaPct, z7Size, sevenPct, gap)
}
