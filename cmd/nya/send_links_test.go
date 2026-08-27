package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPrintSendLinksFileMode(t *testing.T) {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	printSendLinks(
		sendModeFile,
		"https://ex.trycloudflare.com/relapse3.log.nyam",
		"https://ex.trycloudflare.com/relapse3.log.nya",
		"https://ex.trycloudflare.com/relapse3.log",
		"", "", "",
		true,
	)
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "https://ex.trycloudflare.com/relapse3.log\n") {
		t.Fatalf("missing original direct URL: %s", out)
	}
	if !strings.Contains(out, "https://ex.trycloudflare.com/relapse3.log.nya\n") {
		t.Fatalf("missing .nya direct URL: %s", out)
	}
	if !strings.Contains(out, "nya get --url https://ex.trycloudflare.com/relapse3.log.nyam") {
		t.Fatalf("missing get URL: %s", out)
	}
}
