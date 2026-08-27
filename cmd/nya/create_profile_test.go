package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nyarime/nya"
)

func TestCreateProfileDistribute(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(src, []byte("nya distribute profile"), 0644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "pack.nya")
	if err := cmdCreate([]string{
		"-profile", "distribute",
		archive, src,
	}); err != nil {
		t.Fatal(err)
	}
	r, err := nya.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	if r.Header == nil || (r.Header.Flags&nya.FlagHasDownloadIndex) == 0 {
		t.Fatal("distribute profile should embed download index")
	}
	if len(r.Entries) != 1 || r.Entries[0].CompressionID != 1 {
		t.Fatalf("entries=%+v want zstd compression id 1", r.Entries)
	}
}
