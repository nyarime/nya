package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nyarime/nya"
)

func TestPackSendSourceAutoTextProfile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "big.log")
	if err := os.WriteFile(src, []byte("2026 INFO line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive, cleanup, err := packSendSource(src, "", 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	r, err := nya.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.Flags&nya.FlagSolidCompress == 0 {
		t.Fatal("auto text-like single file should use solid")
	}
}
