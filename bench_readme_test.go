package nya

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type readmeRow struct {
	name    string
	rawSize int
	nya     compressSample
	xz      compressSample
	sevenZ  compressSample
	zstd    compressSample
}

type compressSample struct {
	size int
	time time.Duration
	ok   bool
}

type abRow struct {
	corpus  string
	variant string
	level   int
	rawSize int
	comp    int
	elapsed time.Duration
}

func TestREADMEBenchmarkSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping README benchmark in -short mode")
	}
	if !externalToolsAvailable() {
		t.Skip("xz/7z/zstd not all on PATH")
	}

	root := t.TempDir()
	corpora, err := BuildBenchCorpora(root)
	if err != nil {
		t.Fatal(err)
	}

	var readme []readmeRow
	var ab []abRow

	for _, c := range corpora {
		if len(c.Raw) > 0 {
			row, variants, err := benchSingleBlob(t, c.Name, c.Raw, root)
			if err != nil {
				t.Fatal(err)
			}
			readme = append(readme, row)
			ab = append(ab, variants...)
			continue
		}
		if c.Dir != "" {
			row, variants, err := benchSolidTree(t, c.Name, c.Dir, root)
			if err != nil {
				t.Fatal(err)
			}
			readme = append(readme, row)
			ab = append(ab, variants...)
		}
	}

	logREADMETable(t, readme)
	logABTable(t, ab)

	if os.Getenv("NYA_BENCH_WRITE") == "1" {
		if err := writeREADMEBenchSection("README.md", readme, ab); err != nil {
			t.Fatalf("write README: %v", err)
		}
		if err := writeBenchCompressDoc(filepath.Join("docs", "BENCHMARK-COMPRESS.md"), abRowsToCompressRows(ab)); err != nil {
			t.Fatalf("write BENCHMARK-COMPRESS: %v", err)
		}
	}
}

func benchSingleBlob(t *testing.T, name string, raw []byte, root string) (readmeRow, []abRow, error) {
	t.Helper()
	work := filepath.Join(root, slug(name))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return readmeRow{}, nil, err
	}
	fname := "input.bin"
	inPath := filepath.Join(work, fname)
	if err := os.WriteFile(inPath, raw, 0o644); err != nil {
		return readmeRow{}, nil, err
	}

	greedySize, greedyTime, err := compressLZMA(raw, 9, false)
	if err != nil {
		return readmeRow{}, nil, err
	}
	optSize, optTime, err := compressLZMA(raw, 9, true)
	if err != nil {
		return readmeRow{}, nil, err
	}

	xzSize, xzTime, err := benchXzBytes(raw, work, fname)
	if err != nil {
		return readmeRow{}, nil, err
	}
	z7Size, z7Time, err := bench7zPath(inPath, work, false)
	if err != nil {
		return readmeRow{}, nil, err
	}
	zstSize, zstTime, err := benchZstdBytes(raw, work, fname)
	if err != nil {
		return readmeRow{}, nil, err
	}

	row := readmeRow{
		name: name, rawSize: len(raw),
		nya:     sample(greedySize, greedyTime),
		xz:      sample(xzSize, xzTime),
		sevenZ:  sample(z7Size, z7Time),
		zstd:    sample(zstSize, zstTime),
	}
	variants := []abRow{
		{name, "greedy", 9, len(raw), greedySize, greedyTime},
		{name, "optimal", 9, len(raw), optSize, optTime},
	}
	return row, variants, nil
}

func benchSolidTree(t *testing.T, name, dir, root string) (readmeRow, []abRow, error) {
	t.Helper()
	work := filepath.Join(root, slug(name))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return readmeRow{}, nil, err
	}

	files, err := CollectTreeFiles(dir)
	if err != nil {
		return readmeRow{}, nil, err
	}
	walkData, err := ConcatSolidFiles(files)
	if err != nil {
		return readmeRow{}, nil, err
	}
	sortedPaths := sortSolidFilePaths(append([]string(nil), files...))
	sortedData, err := ConcatSolidFiles(sortedPaths)
	if err != nil {
		return readmeRow{}, nil, err
	}

	walkSize, walkTime, err := compressLZMA(walkData, 9, false)
	if err != nil {
		return readmeRow{}, nil, err
	}
	sortedSize, sortedTime, err := compressLZMA(sortedData, 9, false)
	if err != nil {
		return readmeRow{}, nil, err
	}
	optSize, optTime, err := compressLZMA(sortedData, 9, true)
	if err != nil {
		return readmeRow{}, nil, err
	}

	archiveDir := filepath.Join(work, "nya-out")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return readmeRow{}, nil, err
	}
	nyaArchSize, nyaArchTime, err := writeSolidArchive(dir, archiveDir, 9, 0)
	if err != nil {
		return readmeRow{}, nil, err
	}

	xzSize, xzTime, err := benchXzBytes(sortedData, work, "solid-stream.bin")
	if err != nil {
		return readmeRow{}, nil, err
	}
	z7Size, z7Time, err := bench7zPath(dir, work, true)
	if err != nil {
		return readmeRow{}, nil, err
	}
	zstSize, zstTime, err := benchZstdBytes(sortedData, work, "solid-stream.bin")
	if err != nil {
		return readmeRow{}, nil, err
	}

	displayName := name + ", solid"
	row := readmeRow{
		name: displayName, rawSize: len(walkData),
		nya:    sample(nyaArchSize, nyaArchTime),
		xz:     sample(xzSize, xzTime),
		sevenZ: sample(z7Size, z7Time),
		zstd:   sample(zstSize, zstTime),
	}
	variants := []abRow{
		{displayName, "walk+greedy", 9, len(walkData), walkSize, walkTime},
		{displayName, "sorted+greedy", 9, len(walkData), sortedSize, sortedTime},
		{displayName, "sorted+optimal", 9, len(walkData), optSize, optTime},
		{displayName, "nya archive", 9, len(walkData), nyaArchSize, nyaArchTime},
	}
	return row, variants, nil
}

func sample(size int, d time.Duration) compressSample {
	return compressSample{size: size, time: d, ok: true}
}

func slug(s string) string {
	return strings.NewReplacer(" ", "-", ",", "", "/", "-").Replace(strings.ToLower(s))
}

func logREADMETable(t *testing.T, rows []readmeRow) {
	t.Helper()
	var b strings.Builder
	b.WriteString("\nREADME benchmark (nya level 9 greedy / solid writer):\n")
	b.WriteString("| corpus | size | nya (level 9) | xz -9 | 7z -mx9 | zstd -19 |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s |\n",
			r.name, r.rawSize,
			formatRatioTime(r.nya.size, r.rawSize, r.nya.time),
			formatRatioTime(r.xz.size, r.rawSize, r.xz.time),
			formatRatioTime(r.sevenZ.size, r.rawSize, r.sevenZ.time),
			formatRatioTime(r.zstd.size, r.rawSize, r.zstd.time),
		)
	}
	t.Log(b.String())
}

func logABTable(t *testing.T, rows []abRow) {
	t.Helper()
	var b strings.Builder
	b.WriteString("\nA/B variants (level 9):\n")
	b.WriteString("| corpus | variant | ratio | time |\n")
	b.WriteString("| --- | --- | ---: | ---: |\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
			r.corpus, r.variant, formatRatioPct(r.comp, r.rawSize), r.elapsed.Round(time.Millisecond))
	}
	t.Log(b.String())
}

func abRowsToCompressRows(ab []abRow) []compressRow {
	out := make([]compressRow, len(ab))
	for i, r := range ab {
		out[i] = compressRow{
			corpus: r.corpus, variant: r.variant, level: r.level,
			rawSize: r.rawSize, compSize: r.comp, elapsed: r.elapsed,
		}
	}
	return out
}

func writeREADMEBenchSection(readmePath string, rows []readmeRow, ab []abRow) error {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}
	content := string(data)

	var table strings.Builder
	table.WriteString("| corpus | size | nya (level 9) | xz -9 | 7z -mx9 | zstd -19 |\n")
	table.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, r := range rows {
		fmt.Fprintf(&table, "| %s | %d | %s | %s | %s | %s |\n",
			r.name, r.rawSize,
			formatRatioTime(r.nya.size, r.rawSize, r.nya.time),
			formatRatioTime(r.xz.size, r.rawSize, r.xz.time),
			formatRatioTime(r.sevenZ.size, r.rawSize, r.sevenZ.time),
			formatRatioTime(r.zstd.size, r.rawSize, r.zstd.time),
		)
	}

	start := strings.Index(content, "| corpus | size | nya (level 9) |")
	if start < 0 {
		return fmt.Errorf("README benchmark table not found")
	}
	end := strings.Index(content[start:], "\n\nClose on single files")
	if end < 0 {
		return fmt.Errorf("README benchmark table end not found")
	}
	end = start + end

	var abNote strings.Builder
	abNote.WriteString("\n\n**Level-9 parser / solid order (same corpora, ")
	abNote.WriteString("regenerate with `NYA_BENCH_WRITE=1 go test -run TestREADMEBenchmarkSuite -timeout 60m ./...`):**\n\n")
	abNote.WriteString("| corpus | variant | ratio | time |\n")
	abNote.WriteString("| --- | --- | ---: | ---: |\n")
	for _, r := range ab {
		fmt.Fprintf(&abNote, "| %s | %s | %s | %s |\n",
			r.corpus, r.variant, formatRatioPct(r.comp, r.rawSize), r.elapsed.Round(time.Millisecond))
	}
	abNote.WriteString("\nDetails: [docs/BENCHMARK-COMPRESS.md](docs/BENCHMARK-COMPRESS.md).\n")

	newContent := content[:start] + table.String() + abNote.String() + content[end:]
	return os.WriteFile(readmePath, []byte(newContent), 0o644)
}
