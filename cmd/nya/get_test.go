package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nyarime/nya"
)

func TestPrintGetManifestSummary(t *testing.T) {
	m := &nya.Manifest{
		Archive: nya.ArchiveMeta{Name: "game.bin.nya", Size: 1024},
		Download: nya.DownloadIndex{
			Blocks: []nya.DownloadBlock{{ID: 0, Size: 1024}},
		},
		Entries: []nya.ManifestEntry{
			{Path: "game.bin", OriginalSize: 2048},
		},
	}
	printGetManifestSummary(m)
}

func TestRestoreDownloadedArchiveFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "novel.txt")
	want := "once upon a time in a compressed land"
	if err := os.WriteFile(src, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "pack.nya")
	if err := writeNyaArchive(archive, src, nya.LevelFastest, false); err != nil {
		t.Fatal(err)
	}
	if err := ensureSendEmbed(archive); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(root, "out")
	names, err := restoreDownloadedArchive(archive, dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "novel.txt" {
		t.Fatalf("names=%v", names)
	}
	got, err := os.ReadFile(filepath.Join(dest, "novel.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRestoreDownloadedArchiveDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "GameData")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "g.nya")
	if err := writeNyaArchive(archive, src, nya.LevelFastest, false); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(root, "out")
	names, err := restoreDownloadedArchive(archive, dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "GameData" {
		t.Fatalf("names=%v", names)
	}
	got, err := os.ReadFile(filepath.Join(dest, "GameData", "sub", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestTopLevelEntryNames(t *testing.T) {
	got := topLevelEntryNames([]nya.DirEntry{
		{Path: "GameData/a.txt"},
		{Path: "GameData/b.txt"},
		{Path: "readme.txt"},
	})
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}
