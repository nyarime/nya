package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func resolveCloudflared(explicit string, allowFetch bool) (string, error) {
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
	if installed := installedCloudflaredPath(); fileExecutable(installed) {
		if err := verifyCloudflared(installed); err == nil {
			return installed, nil
		}
	}
	if cached := cachedCloudflaredPath(); fileExecutable(cached) {
		if p, err := installCloudflaredBinary(cached); err == nil {
			if err := verifyCloudflared(p); err == nil {
				return p, nil
			}
		}
		if err := verifyCloudflared(cached); err == nil {
			return cached, nil
		}
	}
	if !allowFetch {
		return "", fmt.Errorf("cloudflared not found; install it or omit -no-fetch-cloudflared so nya can install one")
	}
	return fetchCloudflared()
}

func cloudflaredBinaryName() string {
	if runtime.GOOS == "windows" {
		return "cloudflared.exe"
	}
	return "cloudflared"
}

func cachedCloudflaredPath() string {
	return filepath.Join(nyaCacheDir(), cloudflaredBinaryName())
}

func installedCloudflaredPath() string {
	return filepath.Join(userBinDir(), cloudflaredBinaryName())
}

// userBinDir is a per-user directory suitable for CLI tools (~/.local/bin).
func userBinDir() string {
	if runtime.GOOS == "windows" {
		// %LocalAppData%\nya\bin — writable without admin.
		if base, err := os.UserCacheDir(); err == nil && base != "" {
			return filepath.Join(base, "nya", "bin")
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, "AppData", "Local", "nya", "bin")
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "bin")
	}
	return filepath.Join(os.TempDir(), "nya-bin")
}

func nyaCacheDir() string {
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		return filepath.Join(d, "nya")
	}
	return filepath.Join(os.TempDir(), "nya-cache")
}

func fileExecutable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func pathListContains(dir string) bool {
	dir = filepath.Clean(dir)
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == dir {
			return true
		}
	}
	return false
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

func cloudflaredReleaseAsset() (asset string, archived bool, err error) {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "cloudflared-linux-amd64", false, nil
		case "arm64":
			return "cloudflared-linux-arm64", false, nil
		case "arm":
			return "cloudflared-linux-arm", false, nil
		case "386":
			return "cloudflared-linux-386", false, nil
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "cloudflared-windows-amd64.exe", false, nil
		case "386":
			return "cloudflared-windows-386.exe", false, nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return "cloudflared-darwin-arm64.tgz", true, nil
		case "amd64":
			return "cloudflared-darwin-amd64.tgz", true, nil
		}
	}
	return "", false, fmt.Errorf("no cloudflared build for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// fetchCloudflared silently downloads and installs the official binary, then verifies it.
func fetchCloudflared() (string, error) {
	asset, archived, err := cloudflaredReleaseAsset()
	if err != nil {
		return "", err
	}
	url := "https://github.com/cloudflare/cloudflared/releases/latest/download/" + asset

	cache := cachedCloudflaredPath()
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		return "", err
	}
	tmp := cache + ".tmp"
	_ = os.Remove(tmp)

	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download cloudflared: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download cloudflared: HTTP %s", resp.Status)
	}

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	if archived {
		if err := extractCloudflaredTGZ(resp.Body, f); err != nil {
			f.Close()
			_ = os.Remove(tmp)
			return "", err
		}
	} else {
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			_ = os.Remove(tmp)
			return "", err
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, cache); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}

	bin := cache
	if installed, err := installCloudflaredBinary(cache); err == nil {
		bin = installed
	}
	if err := verifyCloudflared(bin); err != nil {
		return "", fmt.Errorf("installed cloudflared but check failed: %w", err)
	}
	return bin, nil
}

// installCloudflaredBinary silently copies src into the per-user bin dir.
func installCloudflaredBinary(src string) (string, error) {
	dest := installedCloudflaredPath()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if sameFile(src, dest) {
		return dest, nil
	}

	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()

	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}

	binDir := filepath.Dir(dest)
	if !pathListContains(binDir) {
		os.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return dest, nil
}

func sameFile(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

func extractCloudflaredTGZ(r io.Reader, out *os.File) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("cloudflared binary not found in archive")
		}
		if err != nil {
			return err
		}
		base := filepath.Base(hdr.Name)
		if hdr.Typeflag == tar.TypeReg && (base == "cloudflared" || base == "cloudflared.exe") {
			_, err := io.Copy(out, tr)
			return err
		}
	}
}
