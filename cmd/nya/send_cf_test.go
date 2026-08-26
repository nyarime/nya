package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReportSendCloudflareTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/trace" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("colo=SIN\nip=9.9.9.9\nloc=SG\n"))
	}))
	defer srv.Close()

	old := cfTraceSendURL
	cfTraceSendURL = srv.URL
	defer func() { cfTraceSendURL = old }()

	// Tunnel base is ignored; probe uses cfTraceSendURL (well-known in production).
	if err := printCloudflareTrace(context.Background(), srv.Client(), cfTraceSendURL, T("send.cf.trace")); err != nil {
		t.Fatal(err)
	}
}

func TestCFTraceWellKnownURL(t *testing.T) {
	got, err := cfTraceURL(cfTraceWellKnownURL)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfTraceWellKnownURL {
		t.Fatalf("got %q", got)
	}
}

func TestReportSendCloudflareTraceIgnoresTunnelHost(t *testing.T) {
	if cfTraceSendURL != cfTraceWellKnownURL {
		t.Fatalf("send trace should default to well-known URL, got %q", cfTraceSendURL)
	}
}
