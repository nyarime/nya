package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCloudflaredReleaseAsset(t *testing.T) {
	asset, archived, err := cloudflaredReleaseAsset()
	if err != nil {
		t.Fatal(err)
	}
	if asset == "" {
		t.Fatal("empty asset")
	}
	if runtime.GOOS == "darwin" && !archived {
		t.Fatal("darwin should be tgz")
	}
	t.Log(asset, archived)
}

func TestUserBinDir(t *testing.T) {
	dir := userBinDir()
	if dir == "" {
		t.Fatal("empty userBinDir")
	}
	if runtime.GOOS != "windows" && !filepath.IsAbs(dir) {
		t.Fatalf("expected abs path, got %q", dir)
	}
	t.Log(dir)
}

func TestInstallCloudflaredBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))

	src := filepath.Join(home, "cloudflared-src")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest, err := installCloudflaredBinary(src)
	if err != nil {
		t.Fatal(err)
	}
	want := installedCloudflaredPath()
	if dest != want {
		t.Fatalf("got %q want %q", dest, want)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Size() < 4 {
		t.Fatalf("bad install %v %v", dest, st)
	}
}

func TestFetchCloudflared(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "AppData", "Local"))
	// Keep PATH without cloudflared so resolve would fetch; fetch itself installs.
	t.Setenv("PATH", filepath.Join(dir, "empty-bin"))

	p, err := fetchCloudflared()
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil || st.Size() < 1000 {
		t.Fatalf("bad binary %v %v", p, st)
	}
	if p != installedCloudflaredPath() {
		t.Fatalf("expected install path %q, got %q", installedCloudflaredPath(), p)
	}
}
