package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
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
	sendModeFile                // single file: browser direct + nyam
	sendModeDir                 // directory: browser .nya + nyam
)

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	port := fs.Int("port", 0, "local HTTP port (0 = ephemeral)")
	bind := fs.String("bind", "127.0.0.1", "local bind address")
	tlsFlag := fs.Bool("tls", false, "serve origin over HTTPS (auto self-signed cert; cloudflared uses --no-tls-verify)")
	tlsHost := fs.String("tls-host", "nya.naixi.net", "hostname/CN for auto-generated -tls certificate")
	tlsCert := fs.String("tls-cert", "", "PEM certificate for HTTPS origin (with -tls-key)")
	tlsKey := fs.String("tls-key", "", "PEM private key for HTTPS origin (with -tls-cert)")
	cloudflared := fs.String("cloudflared", "cloudflared", "cloudflared binary (PATH or absolute path)")
	noTunnel := fs.Bool("no-tunnel", false, "only serve locally (no TryCloudflare)")
	noEmbed := fs.Bool("no-embed", false, "do not upsert download index before send")
	verboseTunnel := fs.Bool("verbose-tunnel", false, "print full cloudflared logs (noisy)")
	out := fs.String("o", "", "when packing: write .nya here (default: temp, deleted on exit)")
	level := fs.Int("level", nya.LevelFast, "when packing: 0–9 (default 3=fast)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, T("send.usage"))
		fs.PrintDefaults()
	}
	if err := parseFlagSet(fs, args, map[string]bool{
		"port": true, "bind": true, "cloudflared": true, "o": true, "level": true,
		"tls-host": true, "tls-cert": true, "tls-key": true,
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
	directPath := "" // original file for browser direct link
	directName := ""
	archive := abs
	cleanup := func() {}

	switch {
	case st.IsDir():
		mode = sendModeDir
		archive, cleanup, err = packSendSource(abs, *out, *level, false)
		if err != nil {
			return err
		}
		defer cleanup()
		st, err = os.Stat(archive)
		if err != nil {
			return err
		}
	case !isNyaArchivePath(abs):
		mode = sendModeFile
		directPath = abs
		directName = filepath.Base(abs)
		archive, cleanup, err = packSendSource(abs, *out, *level, false)
		if err != nil {
			return err
		}
		defer cleanup()
		st, err = os.Stat(archive)
		if err != nil {
			return err
		}
	default:
		// .nya archive: embed below with TryCloudflare-optimal block size
	}

	tlsCfg, err := prepareSendTLS(*tlsFlag, *tlsCert, *tlsKey, *tlsHost)
	if err != nil {
		return err
	}
	defer tlsCfg.close()

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *bind, *port))
	if err != nil {
		return err
	}
	defer ln.Close()
	if tlsCfg.enabled {
		ln, err = tlsListen(ln, tlsCfg.certFile, tlsCfg.keyFile)
		if err != nil {
			return fmt.Errorf("send: tls: %w", err)
		}
	}

	archiveName, indexName := sendPublicNames(mode, abs, directName)
	sendBlockSize := nya.BlockSizeForParallel(st.Size(), nya.TryCloudflareMaxParallel)
	if !*noEmbed {
		if err := ensureSendEmbed(archive, sendBlockSize); err != nil {
			return err
		}
		st, err = os.Stat(archive)
		if err != nil {
			return err
		}
	}
	nyamJSON, err := buildSendIndex(archive, archiveName, sendBlockSize)
	if err != nil {
		return err
	}
	blockCount := sendManifestBlockCount(nyamJSON)
	if !*noEmbed && blockCount > 0 {
		fmt.Fprintf(os.Stderr, "nya send: %d transport blocks @ %s (max %d parallel)\n",
			blockCount, nya.HumanSize(int(sendBlockSize)), nya.TryCloudflareMaxParallel)
	}

	scheme := "http"
	if tlsCfg.enabled {
		scheme = "https"
	}
	baseLocal := fmt.Sprintf("%s://%s", scheme, ln.Addr().String())
	indexLocal := baseLocal + "/" + url.PathEscape(indexName)
	nyaLocal := baseLocal + "/" + url.PathEscape(archiveName)
	directLocal := ""
	if mode == sendModeFile {
		directLocal = baseLocal + "/" + url.PathEscape(directName)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/"+indexName:
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(nyamJSON)
		case p == "/"+archiveName:
			serveSendFile(w, r, archive, archiveName)
		case mode == sendModeFile && p == "/"+directName:
			serveSendFile(w, r, directPath, directName)
		default:
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
		bin, err := resolveCloudflared(*cloudflared)
		if err != nil {
			_ = srv.Shutdown(context.Background())
			return err
		}
		fmt.Fprintln(os.Stderr, T("send.tunnel.start"))
		tunnelURL := fmt.Sprintf("%s://%s", scheme, ln.Addr().String())
		tunnelArgs := []string{"tunnel", "--url", tunnelURL}
		if tlsCfg.enabled {
			tunnelArgs = append(tunnelArgs, "--no-tls-verify")
			if *tlsHost != "" {
				tunnelArgs = append(tunnelArgs, "--origin-server-name", *tlsHost)
			}
		}
		tunnelCmd = exec.CommandContext(ctx, bin, tunnelArgs...)
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
		go func() {
			sc := bufio.NewScanner(pr)
			buf := make([]byte, 0, 64*1024)
			sc.Buffer(buf, 1024*1024)
			for sc.Scan() {
				if u := tunnelSink.handleLine(sc.Text()); u != "" {
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

	indexURL := publicBase + "/" + url.PathEscape(indexName)
	nyaURL := publicBase + "/" + url.PathEscape(archiveName)
	directURL := ""
	if mode == sendModeFile {
		directURL = publicBase + "/" + url.PathEscape(directName)
	}

	fmt.Fprintf(os.Stderr, "\nnya send: %s (%s)\n", archiveName, nya.HumanSize(int(st.Size())))
	printSendLinks(mode, indexURL, nyaURL, directURL, indexLocal, nyaLocal, directLocal, !*noTunnel)

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

func printSendLinks(mode sendMode, indexURL, nyaURL, directURL, indexLocal, nyaLocal, directLocal string, public bool) {
	fmt.Fprintln(os.Stderr)
	if !public {
		indexURL, nyaURL, directURL = indexLocal, nyaLocal, directLocal
		fmt.Fprintln(os.Stderr, T("send.lan"))
	}
	switch mode {
	case sendModeFile:
		fmt.Fprintln(os.Stderr, T("send.direct"))
		fmt.Fprintf(os.Stderr, "  %s\n", directURL)
		fmt.Fprintln(os.Stderr, T("send.get"))
		fmt.Fprintf(os.Stderr, "  nya get --url %s\n", indexURL)
	case sendModeDir:
		fmt.Fprintln(os.Stderr, T("send.archive"))
		fmt.Fprintf(os.Stderr, "  %s\n", nyaURL)
		fmt.Fprintln(os.Stderr, T("send.get"))
		fmt.Fprintf(os.Stderr, "  nya get --url %s\n", indexURL)
	default:
		fmt.Fprintln(os.Stderr, T("send.archive"))
		fmt.Fprintf(os.Stderr, "  %s\n", nyaURL)
		fmt.Fprintln(os.Stderr, T("send.get"))
		fmt.Fprintf(os.Stderr, "  nya get --url %s\n", indexURL)
	}
	fmt.Fprintln(os.Stderr, T("send.stop"))
}

func buildSendIndex(archive, publicNyaName string, blockSize int64) ([]byte, error) {
	if blockSize <= 0 {
		fi, err := os.Stat(archive)
		if err != nil {
			return nil, err
		}
		blockSize = nya.BlockSizeForParallel(fi.Size(), nya.TryCloudflareMaxParallel)
	}
	m, err := nya.BuildManifest(archive, blockSize, nya.ManifestSource{URL: publicNyaName, Priority: 10})
	if err != nil {
		return nil, err
	}
	m.Archive.Name = publicNyaName
	return json.MarshalIndent(m, "", "  ")
}

func sendManifestBlockCount(nyamJSON []byte) int {
	var stub struct {
		Download struct {
			Blocks []json.RawMessage `json:"blocks"`
		} `json:"download"`
	}
	if err := json.Unmarshal(nyamJSON, &stub); err != nil {
		return 0
	}
	return len(stub.Download.Blocks)
}

// sendPublicNames picks URL basenames from the source, not temp paths.
// file novel.txt → novel.txt.nya + novel.txt.nyam
// dir  GameData  → GameData.nya + GameData.nyam
// nya  pack.nya  → pack.nya + pack.nyam
func sendPublicNames(mode sendMode, srcAbs, directName string) (nyaName, nyamName string) {
	switch mode {
	case sendModeFile:
		base := directName
		if base == "" {
			base = filepath.Base(srcAbs)
		}
		return base + ".nya", base + ".nyam"
	case sendModeDir:
		base := filepath.Base(srcAbs)
		if base == "" || base == "." || base == string(filepath.Separator) {
			base = "send"
		}
		return base + ".nya", base + ".nyam"
	default:
		base := filepath.Base(srcAbs)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if stem == "" {
			stem = base
		}
		return base, stem + ".nyam"
	}
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

// packSendSource archives a directory or file into a .nya for sending.
func packSendSource(src, out string, level int, embed bool) (archive string, cleanup func(), err error) {
	cleanup = func() {}
	if level < 0 || level > 9 {
		return "", cleanup, fmt.Errorf("level %d is out of range, want 0 to 9", level)
	}
	dest := out
	if dest == "" {
		base := filepath.Base(src)
		if base == "." || base == "/" || base == string(filepath.Separator) {
			base = "send"
		}
		f, err := os.CreateTemp("", "nya-send-"+base+"-*.nya")
		if err != nil {
			return "", cleanup, err
		}
		dest = f.Name()
		_ = f.Close()
		cleanup = func() { _ = os.Remove(dest) }
	} else {
		absOut, err := filepath.Abs(dest)
		if err != nil {
			return "", cleanup, err
		}
		dest = absOut
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", cleanup, err
		}
	}

	fmt.Fprintf(os.Stderr, "nya send: packing %s\n", dest)
	textLike, dense, _, scanErr := nya.ScanPayloadKinds(src)
	if scanErr != nil {
		cleanup()
		return "", func() {}, scanErr
	}
	solid := textLike >= 2 && textLike >= dense
	if err := writeNyaArchive(dest, src, level, solid); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if embed {
		if err := ensureSendEmbed(dest, 0); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return dest, cleanup, nil
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

func ensureSendEmbed(path string, blockSize int64) error {
	if blockSize <= 0 {
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		blockSize = nya.BlockSizeForParallel(fi.Size(), nya.TryCloudflareMaxParallel)
	}
	_, err := nya.EmbedDownloadIndex(path, nya.EmbedOptions{BlockSize: blockSize, InPlace: true})
	return err
}
