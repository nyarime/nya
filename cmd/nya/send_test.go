package main

import (
	"os"
	"path/filepath"
	"testing"
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

func TestSendPublicNames(t *testing.T) {
	serveN, nyamN := sendPublicNames(sendModeFile, "/tmp/novel.txt", "novel.txt", "")
	if serveN != "novel.txt" || nyamN != "" {
		t.Fatalf("file: %s %s", serveN, nyamN)
	}
	serveN, nyamN = sendPublicNames(sendModeDir, "/data/GameData", "", "GameData")
	if serveN != "GameData" || nyamN != "" {
		t.Fatalf("dir: %s %s", serveN, nyamN)
	}
	serveN, nyamN = sendPublicNames(sendModeNya, "/out/pack.nya", "", "")
	if serveN != "pack.nya" || nyamN != "pack.nyam" {
		t.Fatalf("nya: %s %s", serveN, nyamN)
	}
}

func TestWalkSendFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "GameData")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "a.bin"), []byte("bin"), 0o644); err != nil {
		t.Fatal(err)
	}

	prefix, files, total, err := walkSendFiles(src)
	if err != nil {
		t.Fatal(err)
	}
	if prefix != "GameData" {
		t.Fatalf("prefix=%q", prefix)
	}
	if total != 8 {
		t.Fatalf("total=%d", total)
	}
	if len(files) != 2 {
		t.Fatalf("files=%v", files)
	}
	if files["GameData/readme.txt"] == "" || files["GameData/sub/a.bin"] == "" {
		t.Fatalf("missing keys: %v", files)
	}
}

func TestURLPathJoin(t *testing.T) {
	if got := urlPathJoin("GameData/readme.txt"); got != "GameData/readme.txt" {
		t.Fatalf("got %q", got)
	}
	if got := urlPathJoin("a b/c"); got != "a%20b/c" {
		t.Fatalf("got %q", got)
	}
}
