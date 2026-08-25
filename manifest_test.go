package nya

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildManifestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "test.nya")

	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 9*1024*1024+123) // spans 3 x 4MB blocks
	for i := range payload {
		payload[i] = byte(i)
	}
	if _, err := f.Write(payload); err != nil {
		t.Fatal(err)
	}
	f.Close()

	m, err := BuildManifest(archive, 4*1024*1024, ManifestSource{URL: "https://example.com/test.nya", Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Download.Blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(m.Download.Blocks))
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	manPath := filepath.Join(dir, "test.nyam")
	if err := WriteManifest(m, manPath); err != nil {
		t.Fatal(err)
	}
	m2, err := ReadManifest(manPath)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Archive.Blake3 != m.Archive.Blake3 {
		t.Fatalf("blake3 mismatch")
	}

	// verify block 1 hash
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	b1 := m.Download.Blocks[1]
	slice := data[b1.Offset : b1.Offset+b1.Size]
	h := Blake3Sum256(slice)
	if hex.EncodeToString(h[:]) != b1.Blake3 {
		t.Fatal("block 1 hash mismatch")
	}
}

func TestParseBlockSize(t *testing.T) {
	n, err := ParseBlockSize("4m")
	if err != nil || n != 4*1024*1024 {
		t.Fatalf("4m = %d err=%v", n, err)
	}
}

func TestResolveManifestSourcesRelative(t *testing.T) {
	m := &Manifest{
		Format:  ManifestFormat,
		Version: ManifestVersion,
		Archive: ArchiveMeta{Name: "pack.nya", Size: 1, Blake3: strings.Repeat("a", 64)},
		Download: DownloadIndex{BlockSize: 1, Blocks: []DownloadBlock{
			{ID: 0, Offset: 0, Size: 1, Blake3: strings.Repeat("b", 64)},
		}},
		Sources: []ManifestSource{{URL: "pack.nya", Priority: 10}},
	}
	if err := ResolveManifestSources(m, "https://ex.trycloudflare.com/index.nyam"); err != nil {
		t.Fatal(err)
	}
	want := "https://ex.trycloudflare.com/pack.nya"
	if m.Sources[0].URL != want {
		t.Fatalf("got %q want %q", m.Sources[0].URL, want)
	}
}
