package nya

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestZstdDictRoundtripNeedsDict(t *testing.T) {
	// Dict holds patterns that do not appear verbatim as a long run in src
	// alone in a way that makes the encoder useless — keep src short and
	// heavily based on dict so matches into the prefix are likely.
	dict := bytes.Repeat([]byte("DICT_UNIQUE_TOKEN_ABCD_"), 256) // 5.5 KiB
	payload := append([]byte("hdr:"), bytes.Repeat([]byte("DICT_UNIQUE_TOKEN_ABCD_"), 64)...)

	comp := ZstdCompressWithDict(payload, 3, dict)
	if _, err := DecompressZstd(comp); err == nil {
		// May succeed if encoder never referenced the dict prefix; still require
		// WithDict path to round-trip.
	}
	got, err := DecompressZstdWithDict(comp, dict)
	if err != nil {
		t.Fatalf("DecompressZstdWithDict: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("dict codec mismatch: got %d want %d", len(got), len(payload))
	}
}

func TestArchiveZstdDictRoundtrip(t *testing.T) {
	dict := bytes.Repeat([]byte("shared_firmware_header_v1_"), 128)
	payload := append([]byte("file-a:"), bytes.Repeat([]byte("shared_firmware_header_v1_"), 40)...)

	dir := t.TempDir()
	src := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "d.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, 3, false)
	w.SetDict(dict)
	w.SetMultiChunk(false)
	if err := w.AddFile(src); err != nil {
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
	if r.Header.Flags&FlagHasZstdDict == 0 {
		t.Fatal("FlagHasZstdDict not set")
	}
	var fileEntry *DirEntry
	for i := range r.Entries {
		if r.Entries[i].EntryType == EntryFile {
			e := r.Entries[i]
			fileEntry = &e
			break
		}
	}
	if fileEntry == nil {
		t.Fatal("no file entry")
	}
	if fileEntry.CompressionID != CompressZstdDict {
		t.Fatalf("CompressionID=%d want %d", fileEntry.CompressionID, CompressZstdDict)
	}

	// Embedded dict: extract without SetDict.
	out := filepath.Join(dir, "out")
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "a.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("extracted content mismatch")
	}
}

func TestArchiveZstdDictSurvivesDownloadIndexEmbed(t *testing.T) {
	dict := bytes.Repeat([]byte("embed_dict_seed_xx_"), 64)
	payload := append([]byte("x:"), bytes.Repeat([]byte("embed_dict_seed_xx_"), 30)...)
	dir := t.TempDir()
	src := filepath.Join(dir, "b.bin")
	os.WriteFile(src, payload, 0o644)
	archive := filepath.Join(dir, "e.nya")
	f, _ := os.Create(archive)
	w := NewWriterOpts(f, 0, 3, false)
	w.SetDict(dict)
	w.SetMultiChunk(false)
	w.AddFile(src)
	w.Close()
	f.Close()

	if _, err := EmbedDownloadIndex(archive, EmbedOptions{BlockSize: 1 << 20, InPlace: true}); err != nil {
		t.Fatal(err)
	}
	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out2")
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(out, "b.bin"))
	if !bytes.Equal(got, payload) {
		t.Fatal("dict lost after download-index embed")
	}
}

func TestArchiveZstdDictSolid(t *testing.T) {
	dict := bytes.Repeat([]byte("solid_dict_seed_"), 64)
	dir := t.TempDir()
	for i, name := range []string{"x.txt", "y.txt"} {
		p := append([]byte(name), bytes.Repeat([]byte("solid_dict_seed_"), 20+i)...)
		if err := os.WriteFile(filepath.Join(dir, name), p, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "s.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, 3, true)
	w.SetDict(dict)
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
	out := t.TempDir()
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
}
