package nya_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nyarime/nya"
)

func TestBuildSFXRoundTrip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "test.nya")
	stubSrc := filepath.Join("sfx", "stubs", "nya-sfx-stub_linux_amd64")
	if _, err := os.Stat(stubSrc); err != nil {
		t.Skip("SFX stub not built; run: cd sfx && cargo build --release")
	}

	// Create tiny archive
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0755)
	os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hello sfx"), 0644)

	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := nya.NewWriterOpts(f, 0, nya.LevelFastest, false)
	if err := w.AddFile(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	sfxOut := filepath.Join(dir, "test.sfx.bin")
	if err := nya.BuildSFX(stubSrc, archive, sfxOut, nil, nya.SFXFlagConsole); err != nil {
		t.Fatal(err)
	}

	if !nya.IsSFX(sfxOut) {
		t.Fatal("expected SFX footer")
	}

	sliced, err := nya.SliceSFXArchive(sfxOut)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sliced[:8], []byte{'N', 'Y', 'A', 0, 'v', '0', '1', 0}) {
		t.Fatal("bad embedded magic")
	}

	r, err := nya.OpenAny(sfxOut)
	if err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := r.Extract(outDir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(outDir, "src", "hello.txt"))
	if string(got) != "hello sfx" {
		t.Fatalf("extract via OpenAny: %q", got)
	}

	// Run stub in dev mode (flags before archive path)
	out2 := filepath.Join(dir, "out2")
	cmd := exec.Command(stubSrc, "-o", out2, "-y", archive)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stub extract: %v\n%s", err, out)
	}
	got2, _ := os.ReadFile(filepath.Join(out2, "src", "hello.txt"))
	if string(got2) != "hello sfx" {
		t.Fatalf("stub extract content: %q", got2)
	}

	// Self-extract: run the SFX binary itself
	out3 := filepath.Join(dir, "out3")
	cmd2 := exec.Command(sfxOut, "-o", out3, "-y")
	if out, err := cmd2.CombinedOutput(); err != nil {
		t.Fatalf("sfx self-extract: %v\n%s", err, out)
	}
	got3, _ := os.ReadFile(filepath.Join(out3, "src", "hello.txt"))
	if string(got3) != "hello sfx" {
		t.Fatalf("sfx self-extract content: %q", got3)
	}
}

func TestParseSfxFooter(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(nya.SFXMagic)
	// offset 100, size 50
	off := make([]byte, 8)
	size := make([]byte, 8)
	for i := range off {
		off[i] = byte(100 >> (8 * i))
	}
	for i := range size {
		size[i] = byte(50 >> (8 * i))
	}
	buf.Write(off)
	buf.Write(size)
	buf.Write(make([]byte, 16)) // config + flags

	foot, err := nya.ParseSfxFooter(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if foot.ArchiveOffset != 100 || foot.ArchiveSize != 50 {
		t.Fatalf("got %+v", foot)
	}
}
