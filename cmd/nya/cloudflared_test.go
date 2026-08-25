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

func TestFetchCloudflared(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	p, err := fetchCloudflared()
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil || st.Size() < 1000 {
		t.Fatalf("bad binary %v %v", p, st)
	}
}
