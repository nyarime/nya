package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nyarime/nya"
)

// sendAccessLogger logs each HTTP fetch with peer, size, and timing.
func sendAccessLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		lw := &sendLogWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		logSendAccess(r, lw.status, lw.written, time.Since(start))
	})
}

type sendLogWriter struct {
	http.ResponseWriter
	status  int
	written int64
}

func (w *sendLogWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *sendLogWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.written += int64(n)
	return n, err
}

func logSendAccess(r *http.Request, status int, nbytes int64, dur time.Duration) {
	path := r.URL.Path
	if path == "" || path == "/" {
		return
	}
	path = strings.TrimPrefix(path, "/")
	peer := sendPeerLabel(r)
	size := nya.HumanSize(int(nbytes))
	if nbytes == 0 && status == http.StatusOK {
		size = "0 B"
	}
	extra := ""
	if rg := r.Header.Get("Range"); rg != "" {
		extra = " " + T("send.log.range") + "=" + truncateLog(rg, 48)
	}
	if ua := strings.TrimSpace(r.Header.Get("User-Agent")); ua != "" {
		extra += " ua=" + truncateLog(ua, 56)
	}
	statusWord := T("send.log.ok")
	if status >= 400 {
		statusWord = fmt.Sprintf("%s %d", T("send.log.fail"), status)
	}
	fmt.Fprintf(os.Stderr, T("send.log.fmt"),
		r.Method, path, size, peer, formatSendDuration(dur), statusWord, extra)
}

func sendPeerLabel(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); ip != "" {
		return ip + " " + T("send.log.cf")
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			xff = strings.TrimSpace(xff[:i])
		}
		if xff != "" {
			return xff
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if host == "127.0.0.1" || host == "::1" {
		return host + " " + T("send.log.local")
	}
	return host
}

func formatSendDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func truncateLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
