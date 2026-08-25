package main

import (
	"archive/tar"
	"compress/gzip"
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
			return p, nil
		}
		if st, err := os.Stat(explicit); err == nil && !st.IsDir() {
			return explicit, nil
		}
		return "", fmt.Errorf("cloudflared not found at %q", explicit)
	}
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, nil
	}
	if cached := cachedCloudflaredPath(); fileExecutable(cached) {
		return cached, nil
	}
	if !allowFetch {
		return "", fmt.Errorf("cloudflared not found; install it or omit -no-fetch-cloudflared so nya can download one")
	}
	fmt.Fprintln(os.Stderr, "nya send: cloudflared not on PATH — downloading official binary to cache…")
	return fetchCloudflared()
}

func cachedCloudflaredPath() string {
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name = "cloudflared.exe"
	}
	return filepath.Join(nyaCacheDir(), name)
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

func fetchCloudflared() (string, error) {
	asset, archived, err := cloudflaredReleaseAsset()
	if err != nil {
		return "", err
	}
	url := "https://github.com/cloudflare/cloudflared/releases/latest/download/" + asset
	dest := cachedCloudflaredPath()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp := dest + ".tmp"
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
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	fmt.Fprintf(os.Stderr, "nya send: cloudflared cached at %s\n", dest)
	return dest, nil
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
