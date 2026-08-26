package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNyaGetUserAgent(t *testing.T) {
	ua := nyaGetUserAgent()
	if ua != "NyaGet/"+nyaCLIVersion() {
		t.Fatalf("ua=%q want aria2-style NyaGet/VERSION", ua)
	}
}

func TestIsNyaGetUserAgent(t *testing.T) {
	if !isNyaGetUserAgent(nyaGetUserAgent()) {
		t.Fatal("own ua")
	}
	if isNyaGetUserAgent("Mozilla/5.0") {
		t.Fatal("browser")
	}
	if isNyaGetUserAgent("aria2/1.37.0") {
		t.Fatal("aria2")
	}
}

func TestParseSendAccessLogLevel(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want sendAccessLogLevel
	}{
		{"notice", sendAccessLogNotice},
		{"normal", sendAccessLogNotice},
		{"info", sendAccessLogInfo},
		{"verbose", sendAccessLogInfo},
		{"warn", sendAccessLogWarn},
	} {
		got, err := parseSendAccessLogLevel(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("%q => %v err=%v want %v", tc.in, got, err, tc.want)
		}
	}
}

func TestShouldLogSendAccessAt(t *testing.T) {
	r := httptestNewRequest("/file.nyam", "Mozilla/5.0")
	if shouldLogSendAccessAt(sendAccessLogNotice, r, http.StatusOK) {
		t.Fatal("browser nyam hidden at notice")
	}
	if !shouldLogSendAccessAt(sendAccessLogInfo, r, http.StatusOK) {
		t.Fatal("info shows browser nyam")
	}
	if shouldLogSendAccessAt(sendAccessLogWarn, r, http.StatusOK) {
		t.Fatal("warn hides success")
	}
	if !shouldLogSendAccessAt(sendAccessLogWarn, r, http.StatusNotFound) {
		t.Fatal("warn shows errors")
	}
	r2 := httptestNewRequest("/file.nyam", nyaGetUserAgent())
	if !shouldLogSendAccessAt(sendAccessLogNotice, r2, http.StatusOK) {
		t.Fatal("nya get nyam at notice")
	}
}

func TestHttpClientForGetUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != nyaGetUserAgent() {
			t.Fatalf("default ua=%q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	resp, err := httpClientForGet("").Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "aria2/1.37.0" {
			t.Fatalf("override ua=%q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()
	resp2, err := httpClientForGet("aria2/1.37.0").Get(srv2.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
}

func httptestNewRequest(path, ua string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, path, nil)
	if ua != "" {
		r.Header.Set("User-Agent", ua)
	}
	return r
}

func TestNyaGetUserAgentNoExtraURL(t *testing.T) {
	if strings.Contains(nyaGetUserAgent(), "github.com") {
		t.Fatal("aria2 does not append project URL to UA")
	}
}
