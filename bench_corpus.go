package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

// BenchCorpus describes one README-style benchmark input.
type BenchCorpus struct {
	Name string
	// Dir is set for multi-file solid tree corpora; Raw for single-blob corpora.
	Dir string
	Raw []byte
}

// BuildBenchCorpora returns corpora aligned with README "Where it stands" rows.
func BuildBenchCorpora(root string) ([]BenchCorpus, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	var out []BenchCorpus

	structured := buildStructuredText(61000) // ~3.3 MiB, matches README row scale
	structuredPath := filepath.Join(root, "structured-text.bin")
	if err := os.WriteFile(structuredPath, structured, 0o644); err != nil {
		return nil, err
	}
	out = append(out, BenchCorpus{Name: "structured text", Raw: structured})

	md := buildMarkdownCorpus()
	mdPath := filepath.Join(root, "sample.md")
	if err := os.WriteFile(mdPath, md, 0o644); err != nil {
		return nil, err
	}
	out = append(out, BenchCorpus{Name: "markdown", Raw: md})

	elfSmall := buildELFPayload(48000, 0x3e)
	elfSmallPath := filepath.Join(root, "small.elf")
	if err := os.WriteFile(elfSmallPath, elfSmall, 0o644); err != nil {
		return nil, err
	}
	out = append(out, BenchCorpus{Name: "ELF binary", Raw: elfSmall})

	elfLarge := buildELFPayload(17*1024*1024, 0x3e)
	elfLargePath := filepath.Join(root, "large.elf")
	if err := os.WriteFile(elfLargePath, elfLarge, 0o644); err != nil {
		return nil, err
	}
	out = append(out, BenchCorpus{Name: "17 MB ELF", Raw: elfLarge})

	treeDir, err := build120FileTree(filepath.Join(root, "tree120"))
	if err != nil {
		return nil, err
	}
	out = append(out, BenchCorpus{Name: "120-file tree", Dir: treeDir})

	return out, nil
}

func buildStructuredText(lines int) []byte {
	var buf bytes.Buffer
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&buf, "row=%d name=item-%d value=%f status=active\n", i, i%97, float64(i)*1.5)
	}
	return buf.Bytes()
}

func buildMarkdownCorpus() []byte {
	var buf bytes.Buffer
	buf.WriteString("# NYA benchmark sample\n\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&buf, "## Section %d\n\nSome **bold** and `code` with [links](https://example.com/%d).\n\n", i, i)
		fmt.Fprintf(&buf, "- bullet %d\n- item %d\n\n", i, i+1)
	}
	return buf.Bytes()
}

func buildELFPayload(size int, machine uint16) []byte {
	if size < 256 {
		size = 256
	}
	b := make([]byte, size)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})
	binary.LittleEndian.PutUint16(b[18:20], machine)
	body := bytes.Repeat([]byte("xorl %eax,%eax; nop; call target; "), 64)
	copy(b[64:], body)
	for p := 64; p+5 < len(b); p += 17 {
		b[p] = 0xE8
		binary.LittleEndian.PutUint32(b[p+1:p+5], 0x1000)
	}
	rng := rand.New(rand.NewSource(42))
	rng.Read(b[128:])
	return b
}

func build120FileTree(root string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	exts := []struct {
		ext    string
		prefix string
		body   func(i int) string
	}{
		{".go", "package main\n", func(i int) string {
			return strings.Repeat(fmt.Sprintf("func f%d() int { return %d }\n", i, i), 280)
		}},
		{".rs", "fn main() {}\n", func(i int) string {
			return strings.Repeat(fmt.Sprintf("fn worker_%d() -> i32 {{ {} }}\n", i), 280)
		}},
		{".json", "", func(i int) string {
			return strings.Repeat(fmt.Sprintf(`{"id":%d,"name":"item-%d","ok":true}`+"\n", i, i), 180)
		}},
		{".txt", "", func(i int) string {
			return strings.Repeat(fmt.Sprintf("log line %d: the quick brown fox\n", i), 350)
		}},
		{".md", "# doc\n", func(i int) string {
			return strings.Repeat(fmt.Sprintf("## heading %d\n\nparagraph text here.\n\n", i), 70)
		}},
		{".bin", "", func(i int) string {
			return string(makeBinaryBlob(i, 16384))
		}},
	}
	// Flat directory with interleaved extensions in walk (name) order.
	for n := 0; n < 120; n++ {
		spec := exts[n%len(exts)]
		name := fmt.Sprintf("f%03d%s", n, spec.ext)
		content := spec.body(n)
		if spec.prefix != "" {
			content = spec.prefix + content
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			return "", err
		}
	}
	return root, nil
}

func makeBinaryBlob(seed int, size int) []byte {
	b := make([]byte, size)
	rng := rand.New(rand.NewSource(int64(seed)))
	rng.Read(b)
	if seed%3 == 0 {
		copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})
	}
	return b
}

// ConcatSolidFiles reads files in order and returns the solid stream bytes.
func ConcatSolidFiles(paths []string) ([]byte, error) {
	var buf bytes.Buffer
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		buf.Write(raw)
	}
	return buf.Bytes(), nil
}

// CollectTreeFiles returns member file paths under dir in walk order.
func CollectTreeFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		files = append(files, p)
		return nil
	})
	return files, err
}
