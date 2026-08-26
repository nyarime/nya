package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestWriteQuickTunnelConfig(t *testing.T) {
	path, cleanup, err := writeQuickTunnelConfig("http://127.0.0.1:54321")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "url: http://127.0.0.1:54321\n" {
		t.Fatalf("config=%q", got)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected temp config removed, stat err=%v", err)
	}
}

func TestCloudflaredTunnelEnv(t *testing.T) {
	t.Setenv("TUNNEL_ORIGIN_CERT", "")
	t.Setenv("PATH", "/usr/bin")
	env := cloudflaredTunnelEnv()
	for _, e := range env {
		if strings.HasPrefix(e, "TUNNEL_ORIGIN_CERT=") {
			t.Fatalf("empty TUNNEL_ORIGIN_CERT should be dropped, got %q", e)
		}
	}
	t.Setenv("TUNNEL_ORIGIN_CERT", "/tmp/cert.pem")
	env = cloudflaredTunnelEnv()
	found := false
	for _, e := range env {
		if e == "TUNNEL_ORIGIN_CERT=/tmp/cert.pem" {
			found = true
		}
	}
	if !found {
		t.Fatal("non-empty TUNNEL_ORIGIN_CERT should be kept")
	}
}

func TestVerifyCloudflared(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "cloudflared")
	script := "#!/bin/sh\necho 'cloudflared version 0.0.0 (test)'\n"
	if runtime.GOOS == "windows" {
		bin += ".bat"
		script = "@echo cloudflared version 0.0.0 (test)\r\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyCloudflared(bin); err != nil {
		t.Fatal(err)
	}

	bad := filepath.Join(dir, "bad")
	if runtime.GOOS == "windows" {
		bad += ".bat"
		if err := os.WriteFile(bad, []byte("@echo nope\r\n@exit /b 1\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(bad, []byte("#!/bin/sh\necho nope\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyCloudflared(bad); err == nil {
		t.Fatal("expected verify failure")
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
	if err := verifyCloudflared(p); err != nil {
		t.Fatal(err)
	}
}
