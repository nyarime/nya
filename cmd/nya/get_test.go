package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
	if err := writeNyaArchive(archive, src, nya.SendPackProfile{Level: nya.LevelFastest}, nil); err != nil {
		t.Fatal(err)
	}
	if err := ensureSendEmbed(archive, 0); err != nil {
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
	if err := writeNyaArchive(archive, src, nya.SendPackProfile{Level: nya.LevelFastest}, nil); err != nil {
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

func TestGetDefaultKeepsArchive(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "novel.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "pack.nya")
	if err := writeNyaArchive(archive, src, nya.SendPackProfile{Level: nya.LevelFastest}, nil); err != nil {
		t.Fatal(err)
	}
	if err := ensureSendEmbed(archive, 0); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		chunk := payload[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
	defer srv.Close()

	nyam := filepath.Join(root, "pack.nyam")
	m, err := nya.BuildManifest(archive, 64*1024, nya.ManifestSource{
		URL: srv.URL, Priority: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := nya.WriteManifest(m, nyam); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := cmdGet([]string{nyam}); err != nil {
		t.Fatal(err)
	}
	outNya := filepath.Join(root, "pack.nya") // beside the .nyam
	if _, err := os.Stat(outNya); err != nil {
		t.Fatalf("default get should leave .nya beside manifest: %v", err)
	}
	// Source novel.txt already lives in root from setup; ensure get did not
	// also dump a sibling extract tree under work/.
	if entries, err := os.ReadDir(work); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("work dir should stay empty (no extract), got %v", entries)
	}
}
