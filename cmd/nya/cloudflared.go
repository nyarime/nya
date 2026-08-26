package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const cloudflaredDownloadURL = "https://developers.cloudflare.com/tunnel/downloads/"

func resolveCloudflared(explicit string) (string, error) {
	if explicit != "" && explicit != "cloudflared" {
		if p, err := exec.LookPath(explicit); err == nil {
			if err := verifyCloudflared(p); err != nil {
				return "", err
			}
			return p, nil
		}
		if st, err := os.Stat(explicit); err == nil && !st.IsDir() {
			if err := verifyCloudflared(explicit); err != nil {
				return "", err
			}
			return explicit, nil
		}
		return "", fmt.Errorf("cloudflared not found at %q", explicit)
	}
	if p, err := exec.LookPath("cloudflared"); err == nil {
		if err := verifyCloudflared(p); err == nil {
			return p, nil
		}
	}
	for _, p := range commonCloudflaredPaths() {
		if fileExecutable(p) {
			if err := verifyCloudflared(p); err == nil {
				return p, nil
			}
		}
	}
	return "", errCloudflaredNotFound()
}

func commonCloudflaredPaths() []string {
	name := cloudflaredBinaryName()
	var paths []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".local", "bin", name))
		if runtime.GOOS == "windows" {
			if base, err := os.UserCacheDir(); err == nil && base != "" {
				paths = append(paths, filepath.Join(base, "nya", "bin", name))
			}
			paths = append(paths, filepath.Join(home, "AppData", "Local", "nya", "bin", name))
		}
	}
	return paths
}

func cloudflaredBinaryName() string {
	if runtime.GOOS == "windows" {
		return "cloudflared.exe"
	}
	return "cloudflared"
}

func fileExecutable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func errCloudflaredNotFound() error {
	return fmt.Errorf("%s\n%s\n%s",
		T("send.tunnel.missing"),
		fmt.Sprintf(T("send.tunnel.download"), cloudflaredDownloadURL),
		T("send.tunnel.lan"),
	)
}

// verifyCloudflared runs `cloudflared --version` to confirm the binary works.
func verifyCloudflared(bin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := bytes.TrimSpace(out)
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if len(msg) > 0 {
			return fmt.Errorf("cloudflared check failed (%s): %w", msg, err)
		}
		return fmt.Errorf("cloudflared check failed: %w", err)
	}
	low := bytes.ToLower(out)
	if !bytes.Contains(low, []byte("cloudflared")) && !bytes.Contains(low, []byte("version")) {
		return fmt.Errorf("cloudflared check failed: unexpected --version output")
	}
	return nil
}
