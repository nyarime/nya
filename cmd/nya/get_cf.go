package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nyarime/nya"
)

// cfTraceWellKnownURL is the stable /cdn-cgi/trace endpoint. TryCloudflare quick-tunnel
// hostnames do not serve this path; send uses the well-known URL for origin-side edge info.
const cfTraceWellKnownURL = "https://www.cloudflare.com/cdn-cgi/trace"

// cfTraceSendURL is the probe target for nya send auto trace (tests may override).
var cfTraceSendURL = cfTraceWellKnownURL

// getHTTPOptions configures nya get HTTP client (UA, optional CF edge IP pin, Basic auth).
type getHTTPOptions struct {
	userAgent string
	resolveIP string // connect to this IPv4/IPv6 instead of DNS (Host/SNI unchanged)
	host      string // TLS ServerName + resolve scope; set from download URL
	auth      basicAuth
}

func httpClientForGetOpts(opts getHTTPOptions) *http.Client {
	ua := nyaGetUserAgent()
	if strings.TrimSpace(opts.userAgent) != "" {
		ua = strings.TrimSpace(opts.userAgent)
	}
	base := nya.DownloadHTTPTransport()
	if ip := strings.TrimSpace(opts.resolveIP); ip != "" && strings.TrimSpace(opts.host) != "" {
		host := strings.TrimSpace(opts.host)
		clone := base.Clone()
		clone.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			d := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			return d.DialContext(ctx, network, net.JoinHostPort(ip, port))
		}
		if clone.TLSClientConfig == nil {
			clone.TLSClientConfig = &tls.Config{}
		}
		tlsCfg := clone.TLSClientConfig.Clone()
		tlsCfg.ServerName = host
		clone.TLSClientConfig = tlsCfg
		base = clone
	}
	var rt http.RoundTripper = &userAgentRoundTripper{
		base: base,
		ua:   ua,
	}
	if opts.auth.enabled() {
		rt = &basicAuthRoundTripper{user: opts.auth.user, pass: opts.auth.pass, base: rt}
	}
	return &http.Client{
		Timeout:   0,
		Transport: rt,
	}
}

func hostFromHTTPSURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	return u.Hostname()
}

func parseResolveIP(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if i := strings.LastIndex(s, ":"); i > 0 && strings.Contains(s, ".") {
		// IPv4:port
		host := s[:i]
		if net.ParseIP(host) != nil {
			return host, nil
		}
	}
	if strings.HasPrefix(s, "[") {
		// [IPv6]:port
		if h, _, err := net.SplitHostPort(s); err == nil {
			return strings.Trim(h, "[]"), nil
		}
	}
	if net.ParseIP(s) != nil {
		return s, nil
	}
	return "", fmt.Errorf("resolve: invalid IP %q (want IPv4, IPv6, or ip:443)", s)
}

func cfTraceURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("cf-trace: need https URL, got %q", u.Scheme)
	}
	u.Path = "/cdn-cgi/trace"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func cfTraceOriginURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	return u.Scheme + "://" + u.Host + "/"
}

// fetchCFTrace returns raw /cdn-cgi/trace body from any HTTPS page URL on that host.
func fetchCFTrace(ctx context.Context, client *http.Client, pageURL string) (string, error) {
	traceURL, err := cfTraceURL(pageURL)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, traceURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cf-trace: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cf-trace: HTTP %s", resp.Status)
	}
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n]), nil
}

func cfTraceFields(body string) (colo, ip, loc string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "colo="):
			colo = strings.TrimPrefix(line, "colo=")
		case strings.HasPrefix(line, "ip="):
			ip = strings.TrimPrefix(line, "ip=")
		case strings.HasPrefix(line, "loc="):
			loc = strings.TrimPrefix(line, "loc=")
		}
	}
	return colo, ip, loc
}

func httpClientForCFTrace() *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: nya.DownloadHTTPTransport(),
	}
}

// printCloudflareTrace logs edge metadata (send/get overview: which CF colo this hop uses).
func printCloudflareTrace(ctx context.Context, client *http.Client, pageURL, headerFmt string) error {
	body, err := fetchCFTrace(ctx, client, pageURL)
	if err != nil {
		return err
	}
	colo, ip, loc := cfTraceFields(body)
	fmt.Fprintf(os.Stderr, headerFmt+"\n", cfTraceOriginURL(pageURL))
	if colo != "" || ip != "" || loc != "" {
		fmt.Fprintf(os.Stderr, T("cf.trace.summary")+"\n", colo, ip, loc)
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
	return nil
}

// printCFTrace is the nya get preflight hook (--cf-trace).
func printCFTrace(ctx context.Context, client *http.Client, pageURL string) error {
	getStatusf(T("get.cf.trace")+"\n", cfTraceOriginURL(pageURL))
	body, err := fetchCFTrace(ctx, client, pageURL)
	if err != nil {
		return err
	}
	colo, ip, loc := cfTraceFields(body)
	if colo != "" || ip != "" || loc != "" {
		getStatusf(T("cf.trace.summary")+"\n", colo, ip, loc)
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		getStatusf("  %s\n", line)
	}
	return nil
}

// reportSendCloudflareTrace runs after Quick Tunnel is up (origin-side uplink view).
// Uses the well-known Cloudflare trace URL — not the ephemeral trycloudflare.com host.
func reportSendCloudflareTrace(ctx context.Context, _ string) {
	if err := printCloudflareTrace(ctx, httpClientForCFTrace(), cfTraceSendURL, T("send.cf.trace")); err != nil {
		fmt.Fprintf(os.Stderr, T("send.cf.trace.err")+"\n", err)
	}
}

func cfTraceFromManifest(_ *http.Client, m *nya.Manifest, urlFlag string) string {
	if hostFromHTTPSURL(urlFlag) != "" {
		return urlFlag
	}
	if m == nil {
		return ""
	}
	for _, s := range m.Sources {
		if hostFromHTTPSURL(s.URL) != "" {
			return s.URL
		}
	}
	return ""
}
