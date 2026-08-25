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
	cloudflared := fs.String("cloudflared", "cloudflared", "cloudflared binary (PATH, absolute, or auto)")
	noTunnel := fs.Bool("no-tunnel", false, "only serve locally (no TryCloudflare)")
	noFetch := fs.Bool("no-fetch-cloudflared", false, "do not auto-install cloudflared when missing")
	noEmbed := fs.Bool("no-embed", false, "do not upsert download index before send")
	out := fs.String("o", "", "when packing: write .nya here (default: temp, deleted on exit)")
	level := fs.Int("level", nya.LevelFast, "when packing: 0–9 (default 3=fast)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `nya send — share a file or folder over HTTP + Cloudflare Quick Tunnel

Usage:
  nya send [flags] <file>           # browser direct link + index.nyam for nya get
  nya send [flags] <directory>      # browser .nya archive + index.nyam for nya get
  nya send [flags] <archive.nya>    # serve existing archive + index.nyam

Links:
  file      → 直链 (original) + index.nyam (compressed transfer, restore same file)
  folder    → .nya (browser downloads archive) + index.nyam (restore same tree)
  Packing uses content magic (not extension): text/code/logs compress well.

Receiver:
  nya get --url https://xxxx.trycloudflare.com/index.nyam

`)
		fs.PrintDefaults()
	}
	if err := parseFlagSet(fs, args, map[string]bool{
		"port": true, "bind": true, "cloudflared": true, "o": true, "level": true,
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
	directPath := "" // original file for browser 直链
	directName := ""
	archive := abs
	cleanup := func() {}

	switch {
	case st.IsDir():
		mode = sendModeDir
		archive, cleanup, err = packSendSource(abs, *out, *level, !*noEmbed)
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
		archive, cleanup, err = packSendSource(abs, *out, *level, !*noEmbed)
		if err != nil {
			return err
		}
		defer cleanup()
		st, err = os.Stat(archive)
		if err != nil {
			return err
		}
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
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *bind, *port))
	if err != nil {
		return err
	}
	defer ln.Close()

	archiveName := filepath.Base(archive)
	indexName := "index.nyam"
	nyamJSON, err := buildSendIndex(archive, archiveName)
	if err != nil {
		return err
	}

	baseLocal := fmt.Sprintf("http://%s", ln.Addr().String())
	indexLocal := baseLocal + "/" + indexName
	nyaLocal := baseLocal + "/" + archiveName
	directLocal := ""
	if mode == sendModeFile {
		directLocal = baseLocal + "/" + url.PathEscape(directName)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/" || p == "/"+indexName:
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
	srv := &http.Server{Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	publicBase := baseLocal
	var tunnelCmd *exec.Cmd
	if !*noTunnel {
		bin, err := resolveCloudflared(*cloudflared, !*noFetch)
		if err != nil {
			_ = srv.Shutdown(context.Background())
			return fmt.Errorf("%w\n  tip: install cloudflared, or nya send -no-tunnel for LAN only", err)
		}
		tunnelURL := fmt.Sprintf("http://%s", ln.Addr().String())
		tunnelCmd = exec.CommandContext(ctx, bin, "tunnel", "--url", tunnelURL)
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
				line := sc.Text()
				fmt.Fprintln(os.Stderr, line)
				if m := tryCloudflareURL.FindString(line); m != "" {
					select {
					case found <- m:
					default:
					}
				}
			}
		}()

		select {
		case u := <-found:
			publicBase = strings.TrimRight(u, "/")
		case <-time.After(45 * time.Second):
			_ = srv.Shutdown(context.Background())
			if tunnelCmd.Process != nil {
				_ = tunnelCmd.Process.Kill()
			}
			return fmt.Errorf("timed out waiting for trycloudflare URL from cloudflared")
		case <-ctx.Done():
			_ = srv.Shutdown(context.Background())
			return nil
		}
	}

	indexURL := publicBase + "/" + indexName
	nyaURL := publicBase + "/" + archiveName
	directURL := ""
	if mode == sendModeFile {
		directURL = publicBase + "/" + url.PathEscape(directName)
	}

	fmt.Fprintf(os.Stderr, "\nnya send: packed %s (%s)\n", archiveName, nya.HumanSize(int(st.Size())))
	printSendLinks(mode, indexURL, nyaURL, directURL, indexLocal, nyaLocal, directLocal, !*noTunnel)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		if tunnelCmd != nil && tunnelCmd.Process != nil {
			_ = tunnelCmd.Process.Kill()
		}
		fmt.Fprintln(os.Stderr, "nya send: stopped")
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
	if public {
		switch mode {
		case sendModeFile:
			fmt.Fprintln(os.Stderr, "Browser 直链 (原文件):")
			fmt.Fprintf(os.Stderr, "  %s\n", directURL)
			fmt.Fprintln(os.Stderr, "nya get (压缩传输 → 还原同名文件):")
			fmt.Fprintf(os.Stderr, "  nya get --url %s\n", indexURL)
			fmt.Fprintf(os.Stderr, "  (.nya payload: %s)\n", nyaURL)
		case sendModeDir:
			fmt.Fprintln(os.Stderr, "Browser (下载 .nya 压缩档):")
			fmt.Fprintf(os.Stderr, "  %s\n", nyaURL)
			fmt.Fprintln(os.Stderr, "nya get (还原为原文件夹):")
			fmt.Fprintf(os.Stderr, "  nya get --url %s\n", indexURL)
		default:
			fmt.Fprintln(os.Stderr, "Browser (.nya):")
			fmt.Fprintf(os.Stderr, "  %s\n", nyaURL)
			fmt.Fprintln(os.Stderr, "nya get:")
			fmt.Fprintf(os.Stderr, "  nya get --url %s\n", indexURL)
		}
	} else {
		fmt.Fprintln(os.Stderr, "LAN:")
		if directLocal != "" {
			fmt.Fprintf(os.Stderr, "  browser file: %s\n", directLocal)
		}
		fmt.Fprintf(os.Stderr, "  browser .nya: %s\n", nyaLocal)
		fmt.Fprintf(os.Stderr, "  nya get:      nya get --url %s\n", indexLocal)
	}
	fmt.Fprintln(os.Stderr, "\nCtrl+C to stop.")
}

func buildSendIndex(archive, archiveName string) ([]byte, error) {
	m, err := nya.BuildManifest(archive, 0, nya.ManifestSource{URL: archiveName, Priority: 10})
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(m, "", "  ")
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

	fmt.Fprintf(os.Stderr, "nya send: packing %s → %s\n", src, dest)
	textLike, dense, other, scanErr := nya.ScanPayloadKinds(src)
	if scanErr != nil {
		cleanup()
		return "", func() {}, scanErr
	}
	solid := textLike >= 2 && textLike >= dense
	if textLike+dense+other > 0 {
		fmt.Fprintf(os.Stderr, "nya send: content magic — text/code/log=%d dense=%d other=%d", textLike, dense, other)
		if solid {
			fmt.Fprint(os.Stderr, " (solid)")
		}
		fmt.Fprintln(os.Stderr)
	}
	if err := writeNyaArchive(dest, src, level, solid); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if embed {
		if err := ensureSendEmbed(dest); err != nil {
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

func ensureSendEmbed(path string) error {
	_, err := nya.EmbedDownloadIndex(path, nya.EmbedOptions{InPlace: true})
	return err
}
