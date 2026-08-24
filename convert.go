package nya

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// ConvertOptions mirrors create-time NYA writer settings plus source password.
type ConvertOptions struct {
	FECPercent     int
	Level          int
	Solid          bool
	Codec          string
	Password       []byte // output encryption
	FECType        uint8
	SourcePassword string
	Workers        int
}

// ConvertResult summarizes a foreign → NYA conversion.
type ConvertResult struct {
	SourceFormat string
	SourcePath   string
	OutputPath   string
	OutputSize   int64
}

// ConvertArchive unpacks zip/7z/rar (and related formats) and repacks as .nya.
// This is the supported path for "repairing" legacy archives: decompress, recompress
// with NYA codecs, and optionally add FEC parity (unlike 7z or post-hoc RAR recovery).
func ConvertArchive(src, dst string, opts ConvertOptions) (*ConvertResult, error) {
	format, err := DetectArchiveFormat(src)
	if err != nil {
		return nil, err
	}
	if format == FormatUnknown {
		if !sevenZipAvailable() {
			return nil, fmt.Errorf("convert: unknown format %q (install 7z for broader detection)", src)
		}
		format = "7z-supported"
	}

	tmp, err := os.MkdirTemp("", "nya-convert-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	staging := filepath.Join(tmp, "data")
	if err := ExtractForeignArchive(src, staging, ImportOptions{Password: opts.SourcePassword}); err != nil {
		return nil, err
	}

	f, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var w *Writer
	if len(opts.Password) > 0 {
		w = NewWriterOpts(f, opts.FECPercent, opts.Level, opts.Solid, opts.Password)
	} else {
		w = NewWriterOpts(f, opts.FECPercent, opts.Level, opts.Solid)
	}
	if opts.Codec != "" {
		w.SetCompression(opts.Codec)
	}
	if opts.Workers > 0 {
		w.SetWorkers(opts.Workers)
	}
	if opts.FECPercent > 0 && opts.FECType != 0 {
		w.SetFECType(opts.FECType)
	}

	if err := w.AddDirectoryContents(staging); err != nil {
		return nil, fmt.Errorf("convert: pack: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	fi, err := os.Stat(dst)
	if err != nil {
		return nil, err
	}
	return &ConvertResult{
		SourceFormat: format,
		SourcePath:   src,
		OutputPath:   dst,
		OutputSize:   fi.Size(),
	}, nil
}

// AddDirectoryContents archives dir using paths relative to dir itself (no extra
// parent directory prefix). Used by ConvertArchive after foreign extract.
func (nw *Writer) AddDirectoryContents(dir string) error {
	absPath, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		nw.basePath = filepath.Dir(absPath)
		return nw.addFile(absPath, info)
	}
	nw.basePath = absPath
	return nw.addDirectoryTree(absPath)
}

func (nw *Writer) addDirectoryTree(root string) error {
	var allPaths []string
	if err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p != root {
			allPaths = append(allPaths, p)
		}
		return nil
	}); err != nil {
		return err
	}

	var files []string
	for _, p := range allPaths {
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if fi.IsDir() {
			relPath, _ := filepath.Rel(nw.basePath, p)
			nw.addDirEntry(relPath, fi)
			continue
		}
		files = append(files, p)
	}

	if nw.solid {
		for _, f := range files {
			fi, _ := os.Stat(f)
			if err := nw.addFile(f, fi); err != nil {
				return err
			}
		}
		return nil
	}

	workers := runtime.NumCPU()
	if nw.workers > 0 {
		workers = nw.workers
	}
	if workers > 4 {
		workers = 4
	}
	sem := make(chan struct{}, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	for _, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(fp string) {
			defer wg.Done()
			defer func() { <-sem }()
			fi, _ := os.Stat(fp)
			mu.Lock()
			if firstErr == nil {
				firstErr = nw.addFile(fp, fi)
			}
			mu.Unlock()
		}(f)
	}
	wg.Wait()
	return firstErr
}

// Convertable reports whether src looks like a supported foreign archive.
func Convertable(src string) bool {
	format, err := DetectArchiveFormat(src)
	if err == nil && format != FormatUnknown {
		return true
	}
	return sevenZipAvailable()
}

// ListConvertFormats returns human-readable format hints for help text.
func ListConvertFormats() string {
	s := "zip (built-in)"
	if sevenZipAvailable() {
		s += ", 7z, rar, tar, tar.gz, … (via 7z)"
	} else {
		s += "; install 7z for 7z/rar/tar"
	}
	return s
}
