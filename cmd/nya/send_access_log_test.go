package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPeerLabel(t *testing.T) {
	r := httptest.NewRequest("GET", "/a.nya", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	got := sendPeerLabel(r)
	if !strings.Contains(got, "127.0.0.1") {
		t.Fatalf("local: %q", got)
	}

	r.Header.Set("Cf-Connecting-Ip", "203.0.113.5")
	got = sendPeerLabel(r)
	if !strings.Contains(got, "203.0.113.5") {
		t.Fatalf("cf: %q", got)
	}

	r.Header.Del("Cf-Connecting-Ip")
	r.Header.Set("X-Forwarded-For", "198.51.100.2, 10.0.0.1")
	got = sendPeerLabel(r)
	if got != "198.51.100.2" {
		t.Fatalf("xff: %q", got)
	}
}

func TestTunnelLogSink(t *testing.T) {
	line := "|  https://foo-bar.trycloudflare.com  |"
	s := &tunnelLogSink{}
	u := s.handleLine(line)
	if u != "https://foo-bar.trycloudflare.com" {
		t.Fatalf("url=%q", u)
	}
	s.mute()
	u2 := s.handleLine("INF Thank you for trying Cloudflare Tunnel")
	if u2 != "" {
		t.Fatal("should not return url on junk")
	}
}

func TestTunnelLogSinkVerbose(t *testing.T) {
	s := &tunnelLogSink{}
	s.setVerbose(true)
	// should not panic; suppresses most lines
	s.handleLine("INF some routine cloudflared info message")
}
