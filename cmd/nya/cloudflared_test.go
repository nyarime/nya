package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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

func TestResolveCloudflaredMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", filepath.Join(dir, "empty-bin"))
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	_, err := resolveCloudflared("cloudflared")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, cloudflaredDownloadURL) {
		t.Fatalf("missing download URL: %q", msg)
	}
	if !strings.Contains(msg, "-no-tunnel") {
		t.Fatalf("missing LAN hint: %q", msg)
	}
}

func TestResolveCloudflaredExplicit(t *testing.T) {
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
	got, err := resolveCloudflared(bin)
	if err != nil {
		t.Fatal(err)
	}
	if got != bin {
		t.Fatalf("got %q want %q", got, bin)
	}
}
