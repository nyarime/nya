package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nyarime/nya"
)

func TestTryCloudflareURLRegex(t *testing.T) {
	line := `|  https://williams-coral-newton-soft.trycloudflare.com                                      |`
	got := tryCloudflareURL.FindString(line)
	want := "https://williams-coral-newton-soft.trycloudflare.com"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// must not match cloudflare.com marketing links
	bad := "https://www.cloudflare.com/website-terms/"
	if tryCloudflareURL.FindString(bad) != "" {
		t.Fatal("matched non-trycloudflare URL")
	}
}

func TestIsNyaArchivePath(t *testing.T) {
	if !isNyaArchivePath("a.nya") || !isNyaArchivePath(`C:\x\B.NYA`) {
		t.Fatal("expected .nya")
	}
	if isNyaArchivePath("a.zip") || isNyaArchivePath("dir") {
		t.Fatal("unexpected .nya")
	}
}

func TestPackSendSourceDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "GameData")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "readme.txt"), []byte("hello send dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out.nya")
	archive, cleanup, err := packSendSource(src, out, nya.LevelFastest, true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if archive != out {
		t.Fatalf("got %q want %q", archive, out)
	}
	st, err := os.Stat(archive)
	if err != nil || st.Size() < 32 {
		t.Fatalf("bad archive %v %v", archive, st)
	}
	r, err := nya.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	ents := r.List()
	if len(ents) < 1 {
		t.Fatal("expected entries")
	}
}

func TestPackSendSourceTempCleanup(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "f.txt")
	if err := os.WriteFile(src, []byte("one file"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive, cleanup, err := packSendSource(src, "", nya.LevelFastest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("temp archive should be removed: %v", err)
	}
}
