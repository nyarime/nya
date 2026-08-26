package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNyaGetUserAgent(t *testing.T) {
	ua := nyaGetUserAgent()
	if !strings.HasPrefix(ua, "NyaGet/") {
		t.Fatalf("ua=%q", ua)
	}
	if !strings.Contains(ua, "github.com/nyarime/nya") {
		t.Fatalf("ua=%q", ua)
	}
}

func TestIsNyaGetUserAgent(t *testing.T) {
	if !isNyaGetUserAgent(nyaGetUserAgent()) {
		t.Fatal("own ua")
	}
	if isNyaGetUserAgent("Mozilla/5.0") {
		t.Fatal("browser")
	}
	if isNyaGetUserAgent("curl/8.0") {
		t.Fatal("curl")
	}
}

func TestShouldLogSendAccess(t *testing.T) {
	r := httptestNewRequest("/file.nyam", "Mozilla/5.0")
	if shouldLogSendAccess(r, false) {
		t.Fatal("browser nyam should be hidden by default")
	}
	if !shouldLogSendAccess(r, true) {
		t.Fatal("-log-nyam-browser should show browser nyam")
	}
	r2 := httptestNewRequest("/file.nyam", nyaGetUserAgent())
	if !shouldLogSendAccess(r2, false) {
		t.Fatal("nya get nyam should log")
	}
	r3 := httptestNewRequest("/file.nya", "Mozilla/5.0")
	if !shouldLogSendAccess(r3, false) {
		t.Fatal("browser nya direct should still log")
	}
}

func TestHttpClientForGetUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); !isNyaGetUserAgent(got) {
			t.Fatalf("ua=%q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	resp, err := httpClientForGet().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func httptestNewRequest(path, ua string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, path, nil)
	if ua != "" {
		r.Header.Set("User-Agent", ua)
	}
	return r
}
