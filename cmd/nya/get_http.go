package main

import (
	"net/http"
	"time"
)

// httpClientForGet returns an HTTP client used by nya get with a stable User-Agent
// so nya send access logs can distinguish CLI fetches from browsers.
// uaOverride non-empty replaces the default (aria2 -U / user-agent=).
func httpClientForGet(uaOverride string) *http.Client {
	return httpClientForGetOpts(getHTTPOptions{userAgent: uaOverride})
}

type userAgentRoundTripper struct {
	base http.RoundTripper
	ua   string
}

func (t *userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	r2 := req.Clone(req.Context())
	r2.Header.Set("User-Agent", t.ua)
	return base.RoundTrip(r2)
}

// kept for tests that need zero timeout override
func httpClientForGetWithTimeout(uaOverride string, d time.Duration) *http.Client {
	c := httpClientForGet(uaOverride)
	c.Timeout = d
	return c
}
