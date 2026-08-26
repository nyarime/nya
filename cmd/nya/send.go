package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nyarime/nya"
)

var tryCloudflareURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

type sendMode int

const (
	sendModeNya sendMode = iota // already a .nya archive
	sendModeFile                // single file: direct HTTP
	sendModeDir                 // directory: serve files by relative path
)

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	port := fs.Int("port", 0, "local HTTP port (0 = ephemeral)")
	bind := fs.String("bind", "127.0.0.1", "local bind address")
	cloudflared := fs.String("cloudflared", "cloudflared", "cloudflared binary (PATH, absolute, or auto)")
	noTunnel := fs.Bool("no-tunnel", false, "only serve locally (no TryCloudflare)")
	noFetch := fs.Bool("no-fetch-cloudflared", false, "do not auto-install cloudflared when missing")
	noEmbed := fs.Bool("no-embed", false, "for .nya only: do not upsert embedded download index")
	verboseTunnel := fs.Bool("verbose-tunnel", false, "print full cloudflared logs (noisy)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, T("send.usage"))
		fs.PrintDefaults()
	}
	if err := parseFlagSet(fs, args, map[string]bool{
		"port": true, "bind": true, "cloudflared": true,
	}); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	src := fs.Arg(0)
	abs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return err
	}

	mode := sendModeNya
	directPath := ""
	directName := ""
	archive := abs
	dirFiles := map[string]string{}
	displayName := filepath.Base(abs)
	var totalSize int64

	switch {
	case st.IsDir():
		mode = sendModeDir
		prefix, files, size, err := walkSendFiles(abs)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf("directory is empty: %s", abs)
		}
		dirFiles = files
		displayName = prefix
		totalSize = size
	case !isNyaArchivePath(abs):
		mode = sendModeFile
		directPath = abs
		directName = filepath.Base(abs)
		displayName = directName
		totalSize = st.Size()
	default:
		if !*noEmbed {
			if err := ensureSendEmbed(archive); err != nil {
				return err
			}
			st, err = os.Stat(archive)
			if err != nil {
				return err
			}
		}
		totalSize = st.Size()
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *bind, *port))
	if err != nil {
		return err
	}
	defer ln.Close()

	archiveName, indexName := sendPublicNames(mode, abs, directName, displayName)
	var nyamJSON []byte
	if mode == sendModeNya {
		nyamJSON, err = buildSendIndex(archive, archiveName)
		if err != nil {
			return err
		}
	}

	baseLocal := fmt.Sprintf("http://%s", ln.Addr().String())
	indexLocal := baseLocal + "/" + urlPathJoin(indexName)
	nyaLocal := baseLocal + "/" + urlPathJoin(archiveName)
	directLocal := ""
	dirBaseLocal := baseLocal + "/"
	if mode == sendModeFile {
		directLocal = baseLocal + "/" + urlPathJoin(directName)
	}
	if mode == sendModeDir {
		dirBaseLocal = baseLocal + "/" + urlPathJoin(displayName) + "/"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		p, err := url.PathUnescape(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch mode {
		case sendModeNya:
			switch p {
			case indexName:
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write(nyamJSON)
			case archiveName:
				serveSendFile(w, r, archive, archiveName)
			default:
				http.NotFound(w, r)
			}
		case sendModeFile:
			if p == directName {
				serveSendFile(w, r, directPath, directName)
				return
			}
			http.NotFound(w, r)
		case sendModeDir:
			if absPath, ok := dirFiles[p]; ok {
				serveSendFile(w, r, absPath, filepath.Base(absPath))
				return
			}
			http.NotFound(w, r)
		}
	})
	srv := &http.Server{Handler: sendAccessLogger(mux)}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	publicBase := baseLocal
	var tunnelCmd *exec.Cmd
	var tunnelSink tunnelLogSink
	if *verboseTunnel {
		tunnelSink.setVerbose(true)
	}
	if !*noTunnel {
		bin, err := resolveCloudflared(*cloudflared, !*noFetch)
		if err != nil {
			_ = srv.Shutdown(context.Background())
			return fmt.Errorf("%w\n  tip: install cloudflared, or nya send -no-tunnel for LAN only", err)
		}
		fmt.Fprintln(os.Stderr, T("send.tunnel.start"))
		tunnelURL := fmt.Sprintf("http://%s", ln.Addr().String())
		configPath, configCleanup, err := writeQuickTunnelConfig(tunnelURL)
		if err != nil {
			return fmt.Errorf("quick tunnel config: %w", err)
		}
		defer configCleanup()
		tunnelCmd = exec.CommandContext(ctx, bin, "tunnel", "--no-autoupdate", "--config", configPath, "--url", tunnelURL)
		tunnelCmd.Env = cloudflaredTunnelEnv()
		pr, pw, err := os.Pipe()
		if err != nil {
			return err
		}
		tunnelCmd.Stdout = pw
		tunnelCmd.Stderr = pw
		if err := tunnelCmd.Start(); err != nil {
			_ = pw.Close()
			_ = pr.Close()
			return fmt.Errorf("start cloudflared: %w", err)
		}
		go func() {
			_ = tunnelCmd.Wait()
			_ = pw.Close()
		}()

		found := make(chan string, 1)
		fatalTunnel := make(chan string, 1)
		go func() {
			sc := bufio.NewScanner(pr)
			buf := make([]byte, 0, 64*1024)
			sc.Buffer(buf, 1024*1024)
			for sc.Scan() {
				line := sc.Text()
				if isOriginCertTunnelError(line) {
					select {
					case fatalTunnel <- line:
					default:
					}
				}
				if u := tunnelSink.handleLine(line); u != "" {
					select {
					case found <- u:
					default:
					}
				}
			}
		}()

		select {
		case u := <-found:
			publicBase = strings.TrimRight(u, "/")
			tunnelSink.mute()
			fmt.Fprintf(os.Stderr, T("send.tunnel.ready")+"\n", publicBase)
		case msg := <-fatalTunnel:
			tunnelSink.mute()
			_ = srv.Shutdown(context.Background())
			if tunnelCmd.Process != nil {
				_ = tunnelCmd.Process.Kill()
			}
			return fmt.Errorf("%s\n%s", T("send.tunnel.origincert"), originCertTunnelHint(msg))
		case <-time.After(45 * time.Second):
			tunnelSink.mute()
			_ = srv.Shutdown(context.Background())
			if tunnelCmd.Process != nil {
				_ = tunnelCmd.Process.Kill()
			}
			return fmt.Errorf("timed out waiting for trycloudflare URL from cloudflared")
		case <-ctx.Done():
			tunnelSink.mute()
			_ = srv.Shutdown(context.Background())
			return nil
		}
	}

	indexURL := publicBase + "/" + urlPathJoin(indexName)
	nyaURL := publicBase + "/" + urlPathJoin(archiveName)
	directURL := ""
	dirBaseURL := publicBase + "/"
	if mode == sendModeFile {
		directURL = publicBase + "/" + urlPathJoin(directName)
	}
	if mode == sendModeDir {
		dirBaseURL = publicBase + "/" + urlPathJoin(displayName) + "/"
	}

	fmt.Fprintf(os.Stderr, "\nnya send: %s (%s)\n", displayName, nya.HumanSize(int(totalSize)))
	printSendLinks(mode, indexURL, nyaURL, directURL, dirBaseURL, indexLocal, nyaLocal, directLocal, dirBaseLocal, !*noTunnel)

	select {
	case <-ctx.Done():
		tunnelSink.mute()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		if tunnelCmd != nil && tunnelCmd.Process != nil {
			_ = tunnelCmd.Process.Kill()
		}
		fmt.Fprintln(os.Stderr, T("send.stopped"))
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func printSendLinks(mode sendMode, indexURL, nyaURL, directURL, dirBaseURL, indexLocal, nyaLocal, directLocal, dirBaseLocal string, public bool) {
	fmt.Fprintln(os.Stderr)
	if !public {
		indexURL, nyaURL, directURL = indexLocal, nyaLocal, directLocal
		dirBaseURL = dirBaseLocal
		fmt.Fprintln(os.Stderr, T("send.lan"))
	}
	switch mode {
	case sendModeFile:
		fmt.Fprintln(os.Stderr, T("send.direct"))
		fmt.Fprintf(os.Stderr, "  %s\n", directURL)
		fmt.Fprintln(os.Stderr, T("send.get"))
		fmt.Fprintf(os.Stderr, "  nya get --url %s\n", directURL)
	case sendModeDir:
		fmt.Fprintln(os.Stderr, T("send.dir"))
		fmt.Fprintf(os.Stderr, "  %s\n", dirBaseURL)
		fmt.Fprintln(os.Stderr, T("send.get"))
		fmt.Fprintf(os.Stderr, "  nya get --url %s<path>\n", dirBaseURL)
	default:
		fmt.Fprintln(os.Stderr, T("send.archive"))
		fmt.Fprintf(os.Stderr, "  %s\n", nyaURL)
		fmt.Fprintln(os.Stderr, T("send.get"))
		fmt.Fprintf(os.Stderr, "  nya get --url %s\n", indexURL)
	}
	fmt.Fprintln(os.Stderr, T("send.stop"))
}

func buildSendIndex(archive, publicNyaName string) ([]byte, error) {
	m, err := nya.BuildManifest(archive, 0, nya.ManifestSource{URL: publicNyaName, Priority: 10})
	if err != nil {
		return nil, err
	}
	m.Archive.Name = publicNyaName
	return json.MarshalIndent(m, "", "  ")
}

// sendPublicNames picks URL basenames from the source, not temp paths.
func sendPublicNames(mode sendMode, srcAbs, directName, dirPrefix string) (serveName, indexName string) {
	switch mode {
	case sendModeFile:
		base := directName
		if base == "" {
			base = filepath.Base(srcAbs)
		}
		return base, ""
	case sendModeDir:
		base := dirPrefix
		if base == "" {
			base = filepath.Base(srcAbs)
		}
		if base == "" || base == "." || base == string(filepath.Separator) {
			base = "send"
		}
		return base, ""
	default:
		base := filepath.Base(srcAbs)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if stem == "" {
			stem = base
		}
		return base, stem + ".nyam"
	}
}

func urlPathJoin(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// walkSendFiles maps URL paths (dirName/rel/path) to absolute file paths.
func walkSendFiles(root string) (prefix string, files map[string]string, totalSize int64, err error) {
	prefix = filepath.Base(root)
	if prefix == "" || prefix == "." || prefix == string(filepath.Separator) {
		prefix = "send"
	}
	files = make(map[string]string)
	rootClean := filepath.Clean(root)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootClean, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, "..") {
			return fmt.Errorf("path escapes root: %s", rel)
		}
		key := prefix + "/" + filepath.ToSlash(rel)
		files[key] = path
		info, err := d.Info()
		if err != nil {
			return err
		}
		totalSize += info.Size()
		return nil
	})
	return prefix, files, totalSize, err
}

func serveSendFile(w http.ResponseWriter, r *http.Request, path, name string) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

func isNyaArchivePath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".nya")
}

func writeNyaArchive(dest, src string, level int, solid bool) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	w := nya.NewWriterOpts(f, 0, level, solid)
	if err := w.AddFile(src); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return f.Close()
}

func ensureSendEmbed(path string) error {
	_, err := nya.EmbedDownloadIndex(path, nya.EmbedOptions{InPlace: true})
	return err
}
