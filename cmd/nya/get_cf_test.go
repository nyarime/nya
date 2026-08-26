package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseResolveIP(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		err  bool
	}{
		{"", "", false},
		{"104.21.93.152", "104.21.93.152", false},
		{"104.21.93.152:443", "104.21.93.152", false},
		{"not-an-ip", "", true},
	} {
		got, err := parseResolveIP(tc.in)
		if tc.err {
			if err == nil {
				t.Fatalf("%q want err", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%q => %q err=%v want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestCFTraceURL(t *testing.T) {
	got, err := cfTraceURL("https://foo.trycloudflare.com/file.nyam")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://foo.trycloudflare.com/cdn-cgi/trace" {
		t.Fatalf("got %q", got)
	}
}

func TestCFTraceFields(t *testing.T) {
	colo, ip, loc := cfTraceFields("colo=HKG\nip=1.2.3.4\nloc=HK\n")
	if colo != "HKG" || ip != "1.2.3.4" || loc != "HK" {
		t.Fatalf("colo=%s ip=%s loc=%s", colo, ip, loc)
	}
}

func TestPrintCFTrace(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/trace" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("colo=HKG\nip=1.2.3.4\n"))
	}))
	defer srv.Close()
	if err := printCFTrace(context.Background(), srv.Client(), srv.URL+"/file.nyam"); err != nil {
		t.Fatal(err)
	}
}

func TestHostFromHTTPSURL(t *testing.T) {
	if got := hostFromHTTPSURL("https://a.b.com/x.nyam"); got != "a.b.com" {
		t.Fatalf("got %q", got)
	}
	if got := hostFromHTTPSURL(""); got != "" {
		t.Fatalf("empty")
	}
}
