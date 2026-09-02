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
	out := fs.String("o", "", "output path: .nya path when keeping file; extract dir when unpacking")
	concurrency := fs.Int("c", 0, "parallel download connections (0 = auto: one per block, max 200)")
	resume := fs.Bool("resume", true, "resume incomplete download")
	urlFlag := fs.String("url", "", ".nyam / .nya / plain file URL")
	paths := fs.String("paths", "", "comma-separated entry paths for partial fetch")
	extract := fs.Bool("extract", false, "force unpack after download (overrides delivery=file)")
	noExtract := fs.Bool("no-extract", false, "keep the .nya file only (never unpack)")
	keepNya := fs.Bool("keep-nya", false, "when unpacking: also keep the downloaded .nya")
	userAgent := fs.String("user-agent", "", "HTTP User-Agent (default: Nya/VERSION)")
	cfTrace := fs.Bool("cf-trace", false, "print Cloudflare /cdn-cgi/trace before download (HTTPS URLs)")
	resolveIP := fs.String("resolve", "", "pin HTTPS host to Cloudflare edge IP (CFST-style, e.g. 1.2.3.4 or 1.2.3.4:443)")
	authUser := fs.String("user", "", "HTTP Basic username (with -password, or userinfo in --url)")
	authPass := fs.String("password", "", "HTTP Basic password")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `nya get — download via .nyam / embedded index

Usage:
  nya get --url <name.nyam|file.nya|https://…/file>
  nya get <manifest.nyam>

Delivery:
  .nya URL  → ordinary file: write the .nya as-is
  .nyam     → follow delivery field:
                restore → unpack (send file → file, send dir → directory)
                file    → keep the .nya (send of an existing .nya)
  -no-extract  always keep .nya;  -extract  force unpack;  -keep-nya  keep archive after unpack
  -user/-password  HTTP Basic auth (or embed in URL; credentials are stripped from logs)

`)
		fs.PrintDefaults()
	}
	if err := parseFlagSet(fs, args, map[string]bool{
		"o": true, "c": true, "url": true, "paths": true, "user-agent": true, "resolve": true,
		"user": true, "password": true,
	}); err != nil {
		return err
	}

	cleanURL, getAuth, err := resolveGetAuth(*urlFlag, *authUser, *authPass)
	if err != nil {
		return err
	}
	pinned, err := parseResolveIP(*resolveIP)
	if err != nil {
		return err
	}
	pageHost := hostFromHTTPSURL(cleanURL)
	if pinned != "" && pageHost == "" && fs.NArg() == 1 {
		pageHost = ""
	}
	httpOpts := getHTTPOptions{userAgent: *userAgent, resolveIP: pinned, host: pageHost, auth: getAuth}
	client := httpClientForGetOpts(httpOpts)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if *cfTrace && cleanURL != "" {
		if err := printCFTrace(ctx, client, cleanURL); err != nil {
			fmt.Fprintf(os.Stderr, "nya get: cf-trace: %v\n", err)
		}
	}

	if fs.NArg() == 0 && cleanURL != "" {
		kind, err := classifyGetURL(client, cleanURL)
		if err != nil {
			return err
		}
		if kind == getURLPlain {
			dest := *out
			if dest == "" {
				dest = guessPlainName(cleanURL)
			}
			return downloadPlainFile(ctx, client, cleanURL, dest)
		}
		return getViaManifest(ctx, client, httpOpts, *cfTrace, cleanURL, "", *out, *concurrency, *resume, *paths, kind == getURLNyam, *extract, *noExtract, *keepNya)
	}
	if fs.NArg() == 1 {
		arg := fs.Arg(0)
		isNyam := strings.HasSuffix(strings.ToLower(arg), ".nyam")
		return getViaManifest(ctx, client, httpOpts, *cfTrace, cleanURL, arg, *out, *concurrency, *resume, *paths, isNyam, *extract, *noExtract, *keepNya)
	}
	fs.Usage()
	os.Exit(2)
	return nil
}

// resolveGetExtract: bare .nya is an ordinary file; .nyam follows delivery.
func resolveGetExtract(fromNyam bool, delivery string, forceExtract, forceNoExtract bool) bool {
	if forceNoExtract {
		return false
	}
	if forceExtract {
		return true
	}
	if !fromNyam {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(delivery)) {
	case nya.DeliveryFile, "archive", "nya":
		return false
	default:
		return true
	}
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
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("download: HTTP 401 (wrong or missing -user/-password?)")
		}
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

func getViaManifest(ctx context.Context, client *http.Client, httpOpts getHTTPOptions, cfTrace bool, urlFlag, manifestPath, out string, concurrency int, resume bool, paths string, fromNyam, forceExtract, forceNoExtract, keepNya bool) error {
	var m *nya.Manifest
	var statePath string
	var archiveOut string
	var manifestFP string

	switch {
	case manifestPath == "" && urlFlag != "":
		fmt.Fprintf(os.Stderr, "nya get: %s\n", urlFlag)
		if httpOpts.resolveIP != "" {
			fmt.Fprintf(os.Stderr, "nya get: resolve %s -> %s\n", httpOpts.host, httpOpts.resolveIP)
		}
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
		manifestFP, err = nya.ManifestFileFingerprint(manifestPath)
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
		if httpOpts.resolveIP != "" && httpOpts.host == "" {
			if h := hostFromHTTPSURL(m.Sources[0].URL); h != "" {
				httpOpts.host = h
				client = httpClientForGetOpts(httpOpts)
				fmt.Fprintf(os.Stderr, "nya get: resolve %s -> %s\n", httpOpts.host, httpOpts.resolveIP)
			}
		}
		if cfTrace {
			if tracePage := cfTraceFromManifest(client, m, urlFlag); tracePage != "" {
				if err := printCFTrace(ctx, client, tracePage); err != nil {
					fmt.Fprintf(os.Stderr, "nya get: cf-trace: %v\n", err)
				}
			}
		}
		archiveOut = m.Archive.Name
		if !filepath.IsAbs(archiveOut) {
			archiveOut = filepath.Join(filepath.Dir(manifestPath), archiveOut)
		}
		statePath = nya.StatePath(manifestPath)
	default:
		return fmt.Errorf("need --url or a .nyam path")
	}

	wantExtract := resolveGetExtract(fromNyam, m.Delivery, forceExtract, forceNoExtract)

	var pathList []string
	for _, p := range strings.Split(paths, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			pathList = append(pathList, p)
		}
	}
	// Partial fetch never auto-extracts (incomplete archive).
	if len(pathList) > 0 {
		wantExtract = false
	}

	extractDir := "."
	cleanupArchive := false

	if !wantExtract {
		if out != "" {
			archiveOut = out
		}
	} else {
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
		Manifest:            m,
		Output:              archiveOut,
		StatePath:           statePath,
		ManifestFingerprint: manifestFP,
		Concurrency:         workers,
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
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("fetch index: HTTP 401 (wrong or missing -user/-password?)")
		}
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
