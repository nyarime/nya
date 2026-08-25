package nya

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildManifestMultiChunkEntries(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.txt")
	os.WriteFile(small, []byte("small payload\n"), 0o644)

	bigPayload := make([]byte, 6*1024*1024)
	for i := range bigPayload {
		bigPayload[i] = byte(i ^ (i >> 5))
	}
	big := filepath.Join(dir, "big.bin")
	os.WriteFile(big, bigPayload, 0o644)

	archive := filepath.Join(dir, "pack.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, 5, false)
	w.SetCompression(CompressionStore)
	if err := w.AddFile(small); err != nil {
		t.Fatal(err)
	}
	if err := w.AddFile(big); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	m, err := BuildManifest(archive, 4*1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Download.Blocks) < 4 {
		t.Fatalf("transport blocks=%d want >=4 for size %d", len(m.Download.Blocks), m.Archive.Size)
	}
	if len(m.Entries) != 2 {
		t.Fatalf("entries=%d want 2", len(m.Entries))
	}
	if m.Archive.CentralDirOffset <= 0 {
		t.Fatal("expected central_dir_offset")
	}

	var bigEnt *ManifestEntry
	for i := range m.Entries {
		if m.Entries[i].Path == "big.bin" {
			bigEnt = &m.Entries[i]
			break
		}
	}
	if bigEnt == nil {
		t.Fatal("big.bin entry missing")
	}
	if bigEnt.ChunkCount < 2 {
		t.Fatalf("big.bin chunks=%d want >=2", bigEnt.ChunkCount)
	}

	ranges, err := m.FetchRangesForPaths([]string{"small.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) < 2 {
		t.Fatalf("ranges=%d want header+chunks+tail", len(ranges))
	}

	filtered := filterBlocksByRanges(m.Download.Blocks, ranges)
	if len(filtered) >= len(m.Download.Blocks) {
		t.Fatalf("partial blocks %d should be fewer than %d (archive %d bytes, %d chunks on big.bin)",
			len(filtered), len(m.Download.Blocks), m.Archive.Size, bigEnt.ChunkCount)
	}
}
