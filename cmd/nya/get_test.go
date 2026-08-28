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

func TestResolveGetExtract(t *testing.T) {
	cases := []struct {
		nyam, extract, noExtract bool
		delivery                 string
		want                     bool
	}{
		{true, false, false, "", true},
		{true, false, false, nya.DeliveryRestore, true},
		{true, false, false, nya.DeliveryFile, false},
		{false, false, false, "", false},
		{false, true, false, "", true},
		{true, false, true, nya.DeliveryRestore, false},
		{true, true, true, nya.DeliveryFile, false}, // -no-extract wins
	}
	for _, tc := range cases {
		got := resolveGetExtract(tc.nyam, tc.delivery, tc.extract, tc.noExtract)
		if got != tc.want {
			t.Fatalf("nyam=%v delivery=%q extract=%v no=%v → %v want %v",
				tc.nyam, tc.delivery, tc.extract, tc.noExtract, got, tc.want)
		}
	}
}

func serveArchive(t *testing.T, archive string) *httptest.Server {
	t.Helper()
	payload, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

func writeNyamFor(t *testing.T, archive, nyam, url string) {
	t.Helper()
	m, err := nya.BuildManifest(archive, 64*1024, nya.ManifestSource{URL: url, Priority: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := nya.WriteManifest(m, nyam); err != nil {
		t.Fatal(err)
	}
}

func TestGetNyamAutoExtractsFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "novel.txt")
	want := "payload-bytes"
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
	srv := serveArchive(t, archive)
	defer srv.Close()

	nyam := filepath.Join(root, "pack.nyam")
	writeNyamFor(t, archive, nyam, srv.URL)

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
	got, err := os.ReadFile(filepath.Join(work, "novel.txt"))
	if err != nil {
		t.Fatalf("nyam get should deliver the file: %v", err)
	}
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGetNyamAutoExtractsDir(t *testing.T) {
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
	if err := ensureSendEmbed(archive, 0); err != nil {
		t.Fatal(err)
	}
	srv := serveArchive(t, archive)
	defer srv.Close()

	nyam := filepath.Join(root, "g.nyam")
	writeNyamFor(t, archive, nyam, srv.URL)

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
	got, err := os.ReadFile(filepath.Join(work, "GameData", "sub", "a.txt"))
	if err != nil {
		t.Fatalf("nyam get should deliver the directory: %v", err)
	}
	if string(got) != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestGetNyamDeliveryFileKeepsArchive(t *testing.T) {
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
	srv := serveArchive(t, archive)
	defer srv.Close()

	nyam := filepath.Join(root, "pack.nyam")
	writeNyamFor(t, archive, nyam, srv.URL)
	// Simulate send of an existing .nya: delivery=file
	m, err := nya.ReadManifest(nyam)
	if err != nil {
		t.Fatal(err)
	}
	m.Delivery = nya.DeliveryFile
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

	if err := cmdGet([]string{"-o", filepath.Join(work, "kept.nya"), nyam}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "kept.nya")); err != nil {
		t.Fatalf("delivery=file should keep .nya: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "novel.txt")); !os.IsNotExist(err) {
		t.Fatal("delivery=file must not unpack")
	}
}

func TestGetNyamNoExtractKeepsArchive(t *testing.T) {
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
	srv := serveArchive(t, archive)
	defer srv.Close()

	nyam := filepath.Join(root, "pack.nyam")
	writeNyamFor(t, archive, nyam, srv.URL)

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

	if err := cmdGet([]string{"-no-extract", "-o", filepath.Join(work, "kept.nya"), nyam}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "kept.nya")); err != nil {
		t.Fatalf("expected kept .nya: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "novel.txt")); !os.IsNotExist(err) {
		t.Fatal("-no-extract must not restore the file")
	}
}
