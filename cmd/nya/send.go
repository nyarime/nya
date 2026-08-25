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
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `nya send — serve a .nya over HTTP and publish via Cloudflare Quick Tunnel

Usage:
  nya send [flags] <archive.nya>

If cloudflared is missing, nya installs the official binary into ~/.local/bin
(or %LocalAppData%\nya\bin on Windows) unless -no-fetch-cloudflared.
Use -no-tunnel for LAN-only.

Receiver:
  nya get --url https://xxxx.trycloudflare.com/archive.nya

`)
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
	archive := fs.Arg(0)
	abs, err := filepath.Abs(archive)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("send needs a .nya file, not a directory")
	}

	if !*noEmbed {
		if err := ensureSendEmbed(abs); err != nil {
			return err
		}
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *bind, *port))
	if err != nil {
		return err
	}
	defer ln.Close()
	localURL := fmt.Sprintf("http://%s/%s", ln.Addr().String(), filepath.Base(abs))

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(abs)
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
		http.ServeContent(w, r, filepath.Base(abs), fi.ModTime(), f)
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
			public = u + "/" + filepath.Base(abs)
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

	fmt.Fprintf(os.Stderr, "\nnya send: serving %s (%s)\n", filepath.Base(abs), nya.HumanSize(int(st.Size())))
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

func ensureSendEmbed(path string) error {
	_, err := nya.EmbedDownloadIndex(path, nya.EmbedOptions{InPlace: true})
	return err
}
