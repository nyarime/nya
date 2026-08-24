package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// buildTree writes a small but varied directory: compressible text, a binary
// blob, an incompressible blob and a nested directory.
func buildTree(t *testing.T) (dir string, want map[string][]byte) {
	t.Helper()
	dir = t.TempDir()
	want = map[string][]byte{}

	var text bytes.Buffer
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&text, "line %d: the quick brown fox jumps over the lazy dog\n", i)
	}
	want["text.txt"] = text.Bytes()

	binary := make([]byte, 40000)
	for i := range binary {
		binary[i] = byte(i % 251)
	}
	want["data.bin"] = binary

	noise := make([]byte, 20000)
	rand.New(rand.NewSource(5)).Read(noise)
	want["noise.bin"] = noise

	want[filepath.Join("nested", "inner.txt")] = bytes.Repeat([]byte("nested payload\n"), 500)

	for name, content := range want {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, want
}

func TestArchiveRoundtripModes(t *testing.T) {
	srcDir, want := buildTree(t)
	base := filepath.Base(srcDir)

	cases := []struct {
		name     string
		fec      int
		solid    bool
		codec    string
		password string
	}{
		{name: "default"},
		{name: "lzma2", codec: CompressionLZMA2},
		{name: "zstd", codec: CompressionZstd},
		{name: "solid", solid: true},
		{name: "solid-zstd", solid: true, codec: CompressionZstd},
		{name: "fec", fec: 30},
		{name: "encrypted", password: "correct horse battery staple"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "test.nya")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}

			var w *Writer
			if tc.password != "" {
				w = NewWriterOpts(f, tc.fec, 9, tc.solid, []byte(tc.password))
			} else {
				w = NewWriterOpts(f, tc.fec, 9, tc.solid)
			}
			if tc.codec != "" {
				w.SetCompression(tc.codec)
			}
			if err := w.AddFile(srcDir); err != nil {
				t.Fatal("AddFile:", err)
			}
			if err := w.Close(); err != nil {
				t.Fatal("Close:", err)
			}
			f.Close()

			var r *Reader
			if tc.password != "" {
				r, err = Open(archive, []byte(tc.password))
			} else {
				r, err = Open(archive)
			}
			if err != nil {
				t.Fatal("Open:", err)
			}
			wantMinor := VersionMinor
			if tc.password != "" {
				wantMinor = 2
			}
			if got := r.Header.VersionMinor; got != wantMinor {
				t.Errorf("archive minor version = %d, want %d", got, wantMinor)
			}
			if !r.Verify() {
				t.Error("Verify reported a damaged archive")
			}

			wantCodec := uint16(CompressLzma2)
			if tc.codec == CompressionZstd {
				wantCodec = CompressZstd
			}
			for _, e := range r.Entries {
				if e.EntryType == EntryFile && e.CompressionID != wantCodec {
					t.Errorf("%s: CompressionID = %d, want %d", e.Path, e.CompressionID, wantCodec)
					break
				}
			}

			out := t.TempDir()
			if err := r.Extract(out); err != nil {
				t.Fatal("Extract:", err)
			}
			for name, content := range want {
				got, err := os.ReadFile(filepath.Join(out, base, name))
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				if !bytes.Equal(got, content) {
					t.Errorf("%s: content mismatch (%d bytes extracted, %d expected)",
						name, len(got), len(content))
				}
			}
		})
	}
}

// Compressible input must actually shrink; a codec that quietly falls back to
// stored blocks would otherwise look healthy in a round-trip test.
func TestArchiveCompressionRatio(t *testing.T) {
	srcDir, want := buildTree(t)
	var raw int
	for _, c := range want {
		raw += len(c)
	}

	for _, codec := range []string{CompressionLZMA2, CompressionZstd} {
		archive := filepath.Join(t.TempDir(), "ratio.nya")
		f, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		w := NewWriterOpts(f, 0, 9, false)
		w.SetCompression(codec)
		if err := w.AddFile(srcDir); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		fi, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		f.Close()

		ratio := float64(fi.Size()) / float64(raw)
		// The tree is ~30% incompressible noise, so anything under 0.75 shows
		// the codec is doing real work.
		if ratio > 0.75 {
			t.Errorf("%s: archive is %.1f%% of the input, expected under 75%%", codec, ratio*100)
		}
		t.Logf("%s: %d -> %d bytes (%.1f%%)", codec, raw, fi.Size(), ratio*100)
	}
}

// Solid archives run the BCJ filter over the whole concatenated stream. The
// reader used to undo it one file at a time, restarting the position counter
// at each slice, which silently corrupted every converted branch. Only the
// files after the first showed it, so drive several executable-looking
// members through a solid archive.
func TestSolidBCJRoundtrip(t *testing.T) {
	dir := t.TempDir()
	want := map[string][]byte{}

	// x86-ish payload: an ELF header so the architecture is read from
	// e_machine, over a compressible body, with CALL instructions whose
	// relative targets all resolve to the same absolute address. That is
	// exactly the shape BCJ exists for, so the writer will choose it.
	const absTarget = 0x4000
	for i := 0; i < 4; i++ {
		b := bytes.Repeat([]byte("xorl %eax,%eax; nop; "), 1500)
		copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})
		b[18], b[19] = 0x3e, 0x00 // e_machine = EM_X86_64
		for p := 64; p+5 < len(b); p += 11 {
			b[p] = 0xE8
			rel := uint32(absTarget - (p + 5))
			binary.LittleEndian.PutUint32(b[p+1:p+5], rel)
		}
		name := fmt.Sprintf("bin%d.elf", i)
		want[name] = b
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	archive := filepath.Join(t.TempDir(), "solid.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, 9, true)
	if err := w.AddFile(dir); err != nil {
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
	var filtered bool
	for _, e := range r.Entries {
		if e.EntryType == EntryFile && e.BCJFilter != BCJNone {
			filtered = true
			break
		}
	}
	if !filtered {
		t.Skip("the writer did not pick a BCJ filter for this payload")
	}

	out := t.TempDir()
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(out, filepath.Base(dir), name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !bytes.Equal(got, content) {
			t.Errorf("%s: content mismatch after solid BCJ roundtrip", name)
		}
	}
}

// Lzma2Compress had no exported counterpart, which matters now that LZMA2 is
// the default codec.
func TestLzma2RoundtripPublicAPI(t *testing.T) {
	inputs := map[string][]byte{
		"empty": {},
		"short": []byte("nya"),
		"text":  bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog\n"), 500),
		"binary": func() []byte {
			b := make([]byte, 70000)
			for i := range b {
				b[i] = byte(i * 7 % 251)
			}
			return b
		}(),
	}
	for name, src := range inputs {
		t.Run(name, func(t *testing.T) {
			comp, err := Lzma2Compress(src, 0)
			if err != nil {
				t.Fatalf("Lzma2Compress: %v", err)
			}
			got, err := Lzma2Decompress(comp, 0)
			if err != nil {
				t.Fatalf("Lzma2Decompress: %v", err)
			}
			if len(src) == 0 && len(got) == 0 {
				return
			}
			if !bytes.Equal(got, src) {
				t.Fatalf("roundtrip mismatch: got %d bytes, want %d", len(got), len(src))
			}
		})
	}
}

// Every level must round-trip, and a higher level must never produce a bigger
// archive than a lower one. A larger dictionary used to make things worse,
// because the match finder only offered its longest match and the parser had
// no way to prefer a nearer, shorter one.
func TestLevelsRoundtripAndImprove(t *testing.T) {
	srcDir, want := buildTree(t)
	base := filepath.Base(srcDir)

	sizes := map[int]int64{}
	for _, level := range []int{LevelStore, LevelFastest, LevelFast, LevelNormal, LevelGood, LevelBest} {
		archive := filepath.Join(t.TempDir(), "lvl.nya")
		f, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		w := NewWriterOpts(f, 0, level, false)
		if err := w.AddFile(srcDir); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		fi, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		sizes[level] = fi.Size()

		r, err := Open(archive)
		if err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
		out := t.TempDir()
		if err := r.Extract(out); err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
		for name, content := range want {
			got, err := os.ReadFile(filepath.Join(out, base, name))
			if err != nil {
				t.Errorf("level %d, %s: %v", level, name, err)
				continue
			}
			if !bytes.Equal(got, content) {
				t.Errorf("level %d, %s: content mismatch", level, name)
			}
		}
		t.Logf("level %d (%s): %d bytes", level, LevelName(level), fi.Size())
	}

	if sizes[LevelFastest] >= sizes[LevelStore] {
		t.Errorf("fastest (%d) did not beat store (%d)", sizes[LevelFastest], sizes[LevelStore])
	}
	ordered := []int{LevelFast, LevelNormal, LevelGood, LevelBest}
	for i := 1; i < len(ordered); i++ {
		lo, hi := ordered[i-1], ordered[i]
		if sizes[hi] > sizes[lo] {
			t.Errorf("level %d produced %d bytes, worse than level %d at %d",
				hi, sizes[hi], lo, sizes[lo])
		}
	}
}

func TestSanitizePathRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{
		"../escape",
		"../../etc/passwd",
		"nested/../../escape",
		"/absolute/path",
	} {
		if got, err := sanitizePath(dir, bad); err == nil {
			t.Errorf("sanitizePath(%q) = %q, want an error", bad, got)
		}
	}
	for _, ok := range []string{
		"file.txt",
		"nested/file.txt",
		"./nested/file.txt",
		"nested/../file.txt",
	} {
		if _, err := sanitizePath(dir, ok); err != nil {
			t.Errorf("sanitizePath(%q) returned %v, want success", ok, err)
		}
	}
}
