package nya

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Supported foreign archive formats for ConvertArchive.
const (
	FormatZIP     = "zip"
	FormatSevenZ  = "7z"
	FormatRAR     = "rar"
	FormatTar     = "tar"
	FormatGzip    = "gzip"
	FormatUnknown = ""
)

// ImportOptions controls extraction of a foreign archive before NYA repack.
type ImportOptions struct {
	Password string // source archive password (zip/7z/rar)
}

// DetectArchiveFormat inspects magic bytes first, then extension as a hint.
func DetectArchiveFormat(path string) (string, error) {
	if format, err := DetectFormatByMagic(path); err == nil && format != FormatUnknown {
		if format == "nya" {
			return FormatUnknown, fmt.Errorf("import: file is NYA, not a foreign archive")
		}
		return format, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".zip", ".jar", ".apk", ".cbz", ".docx", ".xlsx", ".pptx", ".epub":
		return FormatZIP, nil
	case ".7z":
		return FormatSevenZ, nil
	case ".rar":
		return FormatRAR, nil
	case ".tar":
		return FormatTar, nil
	case ".tgz", ".gz":
		return FormatGzip, nil
	}
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tar.bz2"),
		strings.HasSuffix(lower, ".tar.xz"), strings.HasSuffix(lower, ".tar.zst"):
		return FormatTar, nil
	}

	header, err := readHeader(path, 8)
	if err != nil {
		return FormatUnknown, err
	}
	if len(header) >= 4 && header[0] == 'P' && header[1] == 'K' {
		return FormatZIP, nil
	}
	if len(header) >= 6 && bytes.HasPrefix(header, []byte("7z\xBC\xAF\x27\x1C")) {
		return FormatSevenZ, nil
	}
	if len(header) >= 6 && bytes.HasPrefix(header, []byte("Rar!\x1a\x07")) {
		return FormatRAR, nil
	}
	if len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b {
		return FormatGzip, nil
	}
	if len(header) >= 5 && string(header[:5]) == "ustar" {
		return FormatTar, nil
	}
	return FormatUnknown, fmt.Errorf("import: unrecognized archive format (extension %q)", ext)
}

// ExtractForeignArchive unpacks zip/7z/rar (and tar.* via 7z) into destDir.
func ExtractForeignArchive(path, destDir string, opts ImportOptions) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	format, err := DetectArchiveFormat(path)
	if err != nil {
		return err
	}
	switch format {
	case FormatZIP:
		if err := extractZipArchive(path, destDir, opts.Password); err != nil {
			if sevenZipAvailable() {
				return extractVia7z(path, destDir, opts.Password)
			}
			return fmt.Errorf("import: zip extract failed: %w", err)
		}
		return nil
	case FormatSevenZ, FormatRAR, FormatTar, FormatGzip:
		if !sevenZipAvailable() {
			return fmt.Errorf("import: %s requires 7-Zip (install p7zip-full / 7-Zip and ensure `7z` is on PATH)", format)
		}
		return extractVia7z(path, destDir, opts.Password)
	default:
		if sevenZipAvailable() {
			return extractVia7z(path, destDir, opts.Password)
		}
		return fmt.Errorf("import: unsupported format %q", format)
	}
}

func readHeader(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	got, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	return buf[:got], nil
}

func extractZipArchive(path, destDir, password string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()

	encrypted := false
	for _, f := range zr.File {
		if f.Flags&0x1 != 0 {
			encrypted = true
			break
		}
	}
	if encrypted {
		if password == "" {
			return ErrPasswordRequired
		}
		if !sevenZipAvailable() {
			return fmt.Errorf("import: encrypted zip requires 7-Zip (`7z` on PATH) plus -source-password")
		}
		return extractVia7z(path, destDir, password)
	}

	for _, f := range zr.File {
		name := f.Name
		if !f.NonUTF8 {
			name = filepath.FromSlash(name)
		}
		target, err := sanitizePath(destDir, name)
		if err != nil {
			return err
		}

		mode := f.Mode()
		if f.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := checkSymlink(target); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("import: open zip entry %q: %w", f.Name, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}

func sevenZipAvailable() bool {
	_, err := find7z()
	return err == nil
}

func find7z() (string, error) {
	for _, name := range []string{"7z", "7za", "7zr"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("7z not found on PATH")
}

func extractVia7z(path, destDir, password string) error {
	bin, err := find7z()
	if err != nil {
		return err
	}
	destDir, err = filepath.Abs(destDir)
	if err != nil {
		return err
	}
	args := []string{"x", "-y", "-bd", path, "-o" + destDir}
	if password != "" {
		args = append(args, "-p"+password)
	}
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		raw := fmt.Errorf("import: 7z extract failed: %s", msg)
		return MapExtractPasswordError(raw, password != "")
	}
	return nil
}
