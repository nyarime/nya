package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReportSendCloudflareTrace(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/trace" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("colo=SIN\nip=9.9.9.9\nloc=SG\n"))
	}))
	defer srv.Close()
	reportSendCloudflareTrace(context.Background(), srv.URL)
}
