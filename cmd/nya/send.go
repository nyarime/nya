package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
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

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	port := fs.Int("port", 0, "local HTTP port (0 = ephemeral)")
	bind := fs.String("bind", "127.0.0.1", "local bind address")
	cloudflared := fs.String("cloudflared", "cloudflared", "cloudflared binary (PATH, absolute, or auto)")
	noTunnel := fs.Bool("no-tunnel", false, "only serve locally (no TryCloudflare)")
	noFetch := fs.Bool("no-fetch-cloudflared", false, "do not auto-install cloudflared when missing")
	noEmbed := fs.Bool("no-embed", false, "do not upsert download index before send")
	out := fs.String("o", "", "when sending a directory/file: write .nya here (default: temp, deleted on exit)")
	level := fs.Int("level", nya.LevelFast, "when packing a directory/file: 0–9 (default 3=fast)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `nya send — pack (optional) + serve over HTTP + Cloudflare Quick Tunnel

Usage:
  nya send [flags] <archive.nya>
  nya send [flags] <directory|file>     # packs to .nya first, then serves

If the argument is a directory (or a non-.nya file), nya creates an archive
(default: temp file; use -o to keep it), then serves that.

If cloudflared is missing, nya silently installs the official binary into
~/.local/bin (or %LocalAppData%\nya\bin on Windows), then runs --version
to verify it (unless -no-fetch-cloudflared). Use -no-tunnel for LAN-only.

Receiver:
  nya get --url https://xxxx.trycloudflare.com/archive.nya

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

	archive := abs
	cleanup := func() {}
	if st.IsDir() || !isNyaArchivePath(abs) {
		archive, cleanup, err = packSendSource(abs, *out, *level, !*noEmbed)
		if err != nil {
			return err
		}
		defer cleanup()
		st, err = os.Stat(archive)
		if err != nil {
			return err
		}
	} else if !*noEmbed {
		if err := ensureSendEmbed(archive); err != nil {
			return err
		}
		st, err = os.Stat(archive)
		if err != nil {
			return err
		}
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *bind, *port))
	if err != nil {
		return err
	}
	defer ln.Close()
	localURL := fmt.Sprintf("http://%s/%s", ln.Addr().String(), filepath.Base(archive))

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(archive)
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
		http.ServeContent(w, r, filepath.Base(archive), fi.ModTime(), f)
	})
	srv := &http.Server{Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	public := localURL
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
			public = u + "/" + filepath.Base(archive)
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

	fmt.Fprintf(os.Stderr, "\nnya send: serving %s (%s)\n", filepath.Base(archive), nya.HumanSize(int(st.Size())))
	fmt.Fprintf(os.Stderr, "nya send: local  %s\n", localURL)
	if !*noTunnel {
		fmt.Fprintf(os.Stderr, "nya send: public %s\n", public)
		fmt.Fprintf(os.Stderr, "\nReceiver:\n  nya get --url %s\n\nCtrl+C to stop.\n", public)
	} else {
		fmt.Fprintf(os.Stderr, "\nReceiver (LAN):\n  nya get --url %s\n\nCtrl+C to stop.\n", localURL)
	}

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

func isNyaArchivePath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".nya")
}

// packSendSource archives a directory or file into a .nya for sending.
// If out is empty, uses a temp file that cleanup removes.
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
	if err := writeNyaArchive(dest, src, level); err != nil {
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

func writeNyaArchive(dest, src string, level int) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	w := nya.NewWriterOpts(f, 0, level, false)
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
