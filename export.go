package nya

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FormatNYA is the native NYA archive format (hub endpoint).
const FormatNYA = "nya"

// ExportOptions controls packing a directory tree into a foreign archive.
type ExportOptions struct {
	Password string // zip/7z/rar password when packing via 7z
}

// ExportResult summarizes a NYA → foreign (or tree → foreign) conversion.
type ExportResult struct {
	SourceFormat string
	DestFormat   string
	SourcePath   string
	OutputPath   string
	OutputSize   int64
}

// DetectHubFormat identifies any archive on the convert hub, including NYA.
func DetectHubFormat(path string) (string, error) {
	if format, err := DetectFormatByMagic(path); err == nil && format != FormatUnknown {
		return format, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".nya":
		return FormatNYA, nil
	case ".zip", ".jar", ".apk", ".cbz":
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
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tar.bz2") ||
		strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".tar.zst") {
		return FormatTar, nil
	}
	return DetectArchiveFormat(path)
}

// ConvertHub unpacks src (any supported archive) to a temp tree and repacks as dst.
// File-tree hub: zip/7z/rar/tar/nya ↔ zip/7z/rar/tar/nya.
func ConvertHub(src, dst string, inOpts ConvertOptions, outOpts ExportOptions) (*ExportResult, error) {
	if _, err := os.Stat(src); err != nil {
		return nil, fmt.Errorf("convert: input: %w", err)
	}
	srcFmt, err := DetectHubFormat(src)
	if err != nil {
		return nil, err
	}
	dstFmt, err := DetectHubFormat(dst)
	if err != nil {
		// Destination may not exist yet — fall back to extension.
		dstFmt = formatFromExt(dst)
		if dstFmt == FormatUnknown {
			return nil, fmt.Errorf("convert: unrecognized output format %q", dst)
		}
	}
	if srcFmt == dstFmt && srcFmt != FormatUnknown {
		return nil, fmt.Errorf("convert: source and destination formats are both %s", srcFmt)
	}

	// Password policy: encrypted input requires explicit -source-password (no prompt).
	if err := RequireSourcePassword(src, inOpts.SourcePassword); err != nil {
		return nil, err
	}

	// Fast path: foreign → nya (existing path with FEC / embed options).
	if dstFmt == FormatNYA && srcFmt != FormatNYA {
		res, err := ConvertArchive(src, dst, inOpts)
		if err != nil {
			return nil, MapExtractPasswordError(err, inOpts.SourcePassword != "")
		}
		return &ExportResult{
			SourceFormat: res.SourceFormat,
			DestFormat:   FormatNYA,
			SourcePath:   res.SourcePath,
			OutputPath:   res.OutputPath,
			OutputSize:   res.OutputSize,
		}, nil
	}

	tmp, err := os.MkdirTemp("", "nya-hub-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	staging := filepath.Join(tmp, "data")

	switch srcFmt {
	case FormatNYA:
		var openPW [][]byte
		if inOpts.SourcePassword != "" {
			openPW = [][]byte{[]byte(inOpts.SourcePassword)}
		}
		r, err := Open(src, openPW...)
		if err != nil {
			return nil, MapExtractPasswordError(fmt.Errorf("convert: open nya: %w", err), inOpts.SourcePassword != "")
		}
		if err := r.Extract(staging); err != nil {
			return nil, fmt.Errorf("convert: extract nya: %w", err)
		}
	default:
		if err := ExtractForeignArchive(src, staging, ImportOptions{Password: inOpts.SourcePassword}); err != nil {
			return nil, MapExtractPasswordError(err, inOpts.SourcePassword != "")
		}
	}

	if dstFmt == FormatNYA {
		f, err := os.Create(dst)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		var w *Writer
		if len(inOpts.Password) > 0 {
			w = NewWriterOpts(f, inOpts.FECPercent, inOpts.Level, inOpts.Solid, inOpts.Password)
		} else {
			w = NewWriterOpts(f, inOpts.FECPercent, inOpts.Level, inOpts.Solid)
		}
		if inOpts.Codec != "" {
			w.SetCompression(inOpts.Codec)
		}
		if inOpts.Workers > 0 {
			w.SetWorkers(inOpts.Workers)
		}
		if inOpts.FECPercent > 0 && inOpts.FECType != 0 {
			w.SetFECType(inOpts.FECType)
		}
		if err := w.AddDirectoryContents(staging); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else {
		if err := PackForeignArchive(staging, dst, dstFmt, outOpts); err != nil {
			return nil, err
		}
	}

	fi, err := os.Stat(dst)
	if err != nil {
		return nil, err
	}
	return &ExportResult{
		SourceFormat: srcFmt,
		DestFormat:   dstFmt,
		SourcePath:   src,
		OutputPath:   dst,
		OutputSize:   fi.Size(),
	}, nil
}

// ExportArchive extracts a .nya archive and packs it as zip/7z/rar/tar.
func ExportArchive(src, dst string, opts ExportOptions) (*ExportResult, error) {
	return ConvertHub(src, dst, ConvertOptions{}, opts)
}

// PackForeignArchive writes dir (contents, no parent prefix) as format into dst.
func PackForeignArchive(dir, dst, format string, opts ExportOptions) error {
	switch format {
	case FormatZIP:
		if opts.Password != "" {
			if !sevenZipAvailable() {
				return fmt.Errorf("export: encrypted zip requires 7-Zip (`7z` on PATH)")
			}
			return packVia7z(dir, dst, FormatZIP, opts.Password)
		}
		return writeZipFromDir(dst, dir)
	case FormatSevenZ, FormatRAR, FormatTar, FormatGzip:
		if !sevenZipAvailable() {
			return fmt.Errorf("export: %s requires 7-Zip (`7z` on PATH)", format)
		}
		return packVia7z(dir, dst, format, opts.Password)
	default:
		return fmt.Errorf("export: unsupported output format %q", format)
	}
}

func formatFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".nya":
		return FormatNYA
	case ".zip", ".jar", ".apk", ".cbz":
		return FormatZIP
	case ".7z":
		return FormatSevenZ
	case ".rar":
		return FormatRAR
	case ".tar":
		return FormatTar
	case ".tgz", ".gz":
		return FormatGzip
	}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tar.bz2") ||
		strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".tar.zst") {
		return FormatTar
	}
	return FormatUnknown
}

func writeZipFromDir(zipPath, srcDir string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if info.IsDir() {
			name += "/"
		}
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(info.Mode())
		h.Flags |= 0x800 // UTF-8
		if info.IsDir() {
			_, err = w.CreateHeader(h)
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			h.Method = zip.Store
			wr, err := w.CreateHeader(h)
			if err != nil {
				return err
			}
			_, err = io.WriteString(wr, target)
			return err
		}
		wr, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(wr, in)
		in.Close()
		return copyErr
	})
}

func packVia7z(dir, dst, format, password string) error {
	bin, err := find7z()
	if err != nil {
		return err
	}
	_ = os.Remove(dst)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}

	// 7z a archive.ext * from inside the staging dir so paths have no parent prefix.
	args := []string{"a", "-bd", "-y"}
	switch format {
	case FormatZIP:
		args = append(args, "-tzip")
	case FormatSevenZ:
		args = append(args, "-t7z")
	case FormatRAR:
		// Stock 7-Zip cannot create RAR; surface a clear error if the run fails.
		args = append(args, "-trar")
	case FormatTar:
		args = append(args, "-ttar")
	case FormatGzip:
		args = append(args, "-tgzip")
	}
	if password != "" {
		args = append(args, "-p"+password)
	}
	args = append(args, absDst, ".")

	cmd := exec.Command(bin, args...)
	cmd.Dir = absDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if format == FormatRAR {
			return fmt.Errorf("export: rar create via 7z failed (%s); install a RAR-capable 7z build or use zip/7z output", msg)
		}
		return fmt.Errorf("export: 7z pack failed: %s", msg)
	}
	return nil
}

// ListHubFormats extends ListConvertFormats with outbound formats.
func ListHubFormats() string {
	s := ListConvertFormats() + "; nya (native); export: zip"
	if sevenZipAvailable() {
		s += ", 7z, tar (via 7z); rar if 7z can create it"
	}
	return s
}
