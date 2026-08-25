package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nyarime/nya"
)

func TestDefaultOpenDest(t *testing.T) {
	in := filepath.Join(t.TempDir(), "game.nya")
	got := defaultOpenDest(in)
	want := filepath.Join(filepath.Dir(in), "game")
	if got != want {
		t.Fatalf("defaultOpenDest(%q)=%q want %q", in, got, want)
	}
}

func TestOpenExtractBeside(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(srcFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "game.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := nya.NewWriterOpts(f, 0, nya.LevelFastest, false)
	if err := w.AddFile(srcFile); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	dest := defaultOpenDest(archive)
	if err := extractTo(archive, dest, "", 0); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}
