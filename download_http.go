package nya

import (
	"net"
	"net/http"
	"time"
)

// DownloadHTTPTransport returns an http.Transport tuned for parallel Range fetches
// to a single origin (TryCloudflare quick tunnels allow ~200 in-flight requests).
func DownloadHTTPTransport() *http.Transport {
	n := TryCloudflareMaxParallel + 32
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          n,
		MaxIdleConnsPerHost:   n,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
