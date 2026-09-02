package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuthHandler(t *testing.T) {
	auth := basicAuth{user: "alice", pass: "secret"}
	srv := httptest.NewServer(auth.wrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no auth: status=%d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.SetBasicAuth("alice", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("auth ok: status=%d body=%q", resp.StatusCode, body)
	}
}

func TestResolveGetAuthStripsUserinfo(t *testing.T) {
	clean, auth, err := resolveGetAuth("https://bob:pw@example.com/a.nyam", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if clean != "https://example.com/a.nyam" {
		t.Fatalf("clean=%q", clean)
	}
	if auth.user != "bob" || auth.pass != "pw" {
		t.Fatalf("auth=%+v", auth)
	}
}

func TestResolveGetAuthFlagsOverrideURL(t *testing.T) {
	clean, auth, err := resolveGetAuth("https://bob:pw@example.com/a.nyam", "carol", "other")
	if err != nil {
		t.Fatal(err)
	}
	if clean != "https://example.com/a.nyam" {
		t.Fatalf("clean=%q", clean)
	}
	if auth.user != "carol" || auth.pass != "other" {
		t.Fatalf("auth=%+v", auth)
	}
}

func TestParseSendBasicAuthRequiresPassword(t *testing.T) {
	if _, err := parseSendBasicAuth("user", ""); err == nil {
		t.Fatal("expected error without password")
	}
}

func TestBasicAuthRoundTripper(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &basicAuthRoundTripper{user: "u", pass: "p"},
	}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotUser != "u" || gotPass != "p" {
		t.Fatalf("got %q/%q", gotUser, gotPass)
	}
}
