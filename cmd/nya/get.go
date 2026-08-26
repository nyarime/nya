package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nyarime/nya"
)

func cmdGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	out := fs.String("o", "", "extract/output dir or file path")
	concurrency := fs.Int("c", 0, "parallel download connections (0 = auto: one per block, max 200)")
	resume := fs.Bool("resume", true, "resume incomplete download")
	urlFlag := fs.String("url", "", ".nyam / .nya / plain file URL")
	paths := fs.String("paths", "", "comma-separated entry paths for partial fetch")
	noExtract := fs.Bool("no-extract", false, "keep the .nya only; do not restore files/dirs")
	keepNya := fs.Bool("keep-nya", false, "after extract, keep the downloaded .nya")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `nya get — download and restore

Usage:
  nya get --url <name.nyam|file.nya|https://…/file>
  nya get <manifest.nyam>

`)
		fs.PrintDefaults()
	}
	if err := parseFlagSet(fs, args, map[string]bool{"o": true, "c": true, "url": true, "paths": true}); err != nil {
		return err
	}

	client := &http.Client{Timeout: 0}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Plain URL / unknown: try nyam/.nya first, else plain download.
	if fs.NArg() == 0 && *urlFlag != "" {
		kind, err := classifyGetURL(client, *urlFlag)
		if err != nil {
			return err
		}
		if kind == getURLPlain {
			dest := *out
			if dest == "" {
				dest = guessPlainName(*urlFlag)
			}
			return downloadPlainFile(ctx, client, *urlFlag, dest)
		}
		return getViaManifest(ctx, client, *urlFlag, "", *out, *concurrency, *resume, *paths, *noExtract, *keepNya)
	}
	if fs.NArg() == 1 {
		return getViaManifest(ctx, client, *urlFlag, fs.Arg(0), *out, *concurrency, *resume, *paths, *noExtract, *keepNya)
	}
	fs.Usage()
	os.Exit(2)
	return nil
}

type getURLKind int

const (
	getURLNyam getURLKind = iota
	getURLNya
	getURLPlain
)

func classifyGetURL(client *http.Client, raw string) (getURLKind, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	if u.Scheme == "" {
		return 0, fmt.Errorf("url must include scheme (https://…)")
	}
	low := strings.ToLower(u.Path)
	switch {
	case strings.HasSuffix(low, ".nyam"):
		return getURLNyam, nil
	case strings.HasSuffix(low, ".nya"):
		return getURLNya, nil
	case u.Path == "" || u.Path == "/":
		return getURLPlain, nil
	default:
		// HEAD: if Content-Type looks like nyam JSON, treat as index; else plain.
		req, err := http.NewRequest(http.MethodHead, raw, nil)
		if err != nil {
			return getURLPlain, nil
		}
		resp, err := client.Do(req)
		if err != nil {
			return getURLPlain, nil
		}
		resp.Body.Close()
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(ct, "json") {
			return getURLNyam, nil
		}
		return getURLPlain, nil
	}
}

func guessPlainName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "download"
	}
	base := path.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		return "download"
	}
	return base
}

func downloadPlainFile(ctx context.Context, client *http.Client, raw, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("download: HTTP %s", resp.Status)
	}
	if dir := filepath.Dir(dest); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 32*1024)
	start := time.Now()
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			written += int64(n)
			if total > 0 {
				getStatusf("\rnya get: %s / %s (%d%%)",
					nya.HumanSize(int(written)), nya.HumanSize(int(total)), written*100/total)
			} else {
				getStatusf("\rnya get: %s", nya.HumanSize(int(written)))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	fmt.Fprintln(os.Stderr)
	fmt.Printf("%s: OK (%s, %s)\n", dest, nya.HumanSize(int(written)), time.Since(start).Round(time.Millisecond))
	return nil
}

func getViaManifest(ctx context.Context, client *http.Client, urlFlag, manifestPath, out string, concurrency int, resume bool, paths string, noExtract, keepNya bool) error {
	var m *nya.Manifest
	var statePath string
	var archiveOut string

	switch {
	case manifestPath == "" && urlFlag != "":
		fmt.Fprintf(os.Stderr, "nya get: %s\n", urlFlag)
		var err error
		m, err = loadTransferManifest(client, urlFlag)
		if err != nil {
			return err
		}
		printGetManifestSummary(m)
		archiveOut = m.Archive.Name
		statePath = nya.StatePath(archiveOut + ".nyam")
	case manifestPath != "":
		var err error
		m, err = nya.ReadManifest(manifestPath)
		if err != nil {
			return err
		}
		printGetManifestSummary(m)
		if urlFlag != "" {
			m.Sources = []nya.ManifestSource{{URL: urlFlag, Priority: 10}}
		} else if len(m.Sources) == 0 {
			return fmt.Errorf("manifest has no sources; pass -url")
		} else if err := nya.ResolveManifestSources(m, "file://"+filepath.ToSlash(manifestPath)); err != nil {
			return err
		}
		archiveOut = m.Archive.Name
		if !filepath.IsAbs(archiveOut) {
			archiveOut = filepath.Join(filepath.Dir(manifestPath), archiveOut)
		}
		statePath = nya.StatePath(manifestPath)
	default:
		return fmt.Errorf("need --url or a .nyam path")
	}

	var pathList []string
	for _, p := range strings.Split(paths, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			pathList = append(pathList, p)
		}
	}

	wantExtract := !noExtract && len(pathList) == 0
	extractDir := "."
	cleanupArchive := false

	if noExtract {
		if out != "" {
			archiveOut = out
		}
	} else if wantExtract {
		if out != "" {
			extractDir = out
		}
		if !keepNya {
			tmp, err := os.CreateTemp("", "nya-get-*.nya")
			if err != nil {
				return err
			}
			archiveOut = tmp.Name()
			_ = tmp.Close()
			_ = os.Remove(archiveOut)
			cleanupArchive = true
			statePath = nya.StatePath(archiveOut + ".nyam")
		}
	}

	getStatusf("nya get: downloading %d blocks…\n", len(m.Download.Blocks))
	workers := nya.DownloadConcurrency(m, concurrency)
	if concurrency <= 0 {
		getStatusf("nya get: auto concurrency %d (one worker per block, max %d)\n",
			workers, nya.TryCloudflareMaxParallel)
	}

	prog := newGetDownloadProgress(m)
	res, err := nya.Download(ctx, nya.DownloadOptions{
		Manifest:    m,
		Output:      archiveOut,
		StatePath:   statePath,
		Concurrency: workers,
		Resume:      resume,
		Paths:       pathList,
		HTTPClient:  client,
		OnInit: func(completedBlocks, _ int, completedBytes int64) {
			prog.init(completedBlocks, completedBytes)
		},
		OnBlockStart: func(b nya.DownloadBlock, _, _ int) {
			prog.blockStart(b.ID)
		},
		OnBlockBytes: func(b nya.DownloadBlock, fetched, _ int64, _, _ int) {
			prog.blockBytes(b.ID, fetched)
		},
		OnBlock: func(b nya.DownloadBlock, _, _ int) {
			prog.blockDone(b.ID, b.Size)
		},
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		if cleanupArchive {
			_ = os.Remove(archiveOut)
			_ = os.Remove(statePath)
		}
		fmt.Fprintf(os.Stderr, "nya get: if download keeps failing, delete resume state %s and retry\n", statePath)
		return err
	}

	fmt.Printf("%s: OK (%d blocks, %s, %s)\n",
		archiveOut,
		res.BlocksTotal,
		nya.HumanSize(int(res.BytesWritten)),
		res.Elapsed.Round(time.Millisecond))
	if res.Partial {
		fmt.Fprintf(os.Stderr, "nya get: partial fetch\n")
		return nil
	}
	if !wantExtract {
		return nil
	}

	restored, err := restoreDownloadedArchive(archiveOut, extractDir)
	if err != nil {
		return err
	}
	if cleanupArchive {
		_ = os.Remove(archiveOut)
		_ = os.Remove(statePath)
	}
	fmt.Printf("%s → %s\n", strings.Join(restored, ", "), extractDir)
	return nil
}

func loadTransferManifest(client *http.Client, raw string) (*nya.Manifest, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		return nil, fmt.Errorf("url must include scheme (https://…)")
	}
	low := strings.ToLower(u.Path)
	switch {
	case strings.HasSuffix(low, ".nyam"):
		return fetchManifestURL(client, u.String())
	case strings.HasSuffix(low, ".nya"):
		return nya.BootstrapManifestFromURL(client, u.String())
	case u.Path == "" || u.Path == "/":
		return nil, fmt.Errorf("pass a .nyam URL (e.g. …/name.nyam), not the site root")
	default:
		// Sibling "<path>.nyam" when given a direct file URL.
		idx := *u
		idx.Path = u.Path + ".nyam"
		if m, err := fetchManifestURL(client, idx.String()); err == nil {
			return m, nil
		}
		return nya.BootstrapManifestFromURL(client, u.String())
	}
}

func fetchManifestURL(client *http.Client, raw string) (*nya.Manifest, error) {
	resp, err := client.Get(raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch index: HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	m, err := nya.ParseManifest(body)
	if err != nil {
		return nil, err
	}
	if err := nya.ResolveManifestSources(m, raw); err != nil {
		return nil, err
	}
	return m, nil
}

func restoreDownloadedArchive(archive, destDir string) ([]string, error) {
	if destDir == "" {
		destDir = "."
	}
	r, err := nya.Open(archive)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	if err := r.Extract(destDir); err != nil {
		return nil, err
	}
	return topLevelEntryNames(r.List()), nil
}

func topLevelEntryNames(entries []nya.DirEntry) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, e := range entries {
		p := filepath.ToSlash(e.Path)
		p = strings.TrimPrefix(p, "./")
		if p == "" {
			continue
		}
		top, _, _ := strings.Cut(p, "/")
		if top == "" {
			continue
		}
		if _, ok := seen[top]; ok {
			continue
		}
		seen[top] = struct{}{}
		out = append(out, top)
	}
	return out
}
