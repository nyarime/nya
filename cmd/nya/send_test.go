package main

import "testing"

func TestTryCloudflareURLRegex(t *testing.T) {
	line := `|  https://williams-coral-newton-soft.trycloudflare.com                                      |`
	got := tryCloudflareURL.FindString(line)
	want := "https://williams-coral-newton-soft.trycloudflare.com"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// must not match cloudflare.com marketing links
	bad := "https://www.cloudflare.com/website-terms/"
	if tryCloudflareURL.FindString(bad) != "" {
		t.Fatal("matched non-trycloudflare URL")
	}
}
