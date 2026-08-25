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

func TestUniqueOpenDestFinderStyle(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "game")
	if uniqueOpenDest(base) != base {
		t.Fatalf("missing path should stay unchanged")
	}
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	got := uniqueOpenDest(base)
	want := filepath.Join(dir, "game 2")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if err := os.Mkdir(want, 0o755); err != nil {
		t.Fatal(err)
	}
	got = uniqueOpenDest(base)
	want = filepath.Join(dir, "game 3")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
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

	dest := uniqueOpenDest(defaultOpenDest(archive))
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

	dest2 := uniqueOpenDest(defaultOpenDest(archive))
	want2 := filepath.Join(dir, "game 2")
	if dest2 != want2 {
		t.Fatalf("second dest %q want %q", dest2, want2)
	}
	if err := extractTo(archive, dest2, "", 0); err != nil {
		t.Fatal(err)
	}
}
