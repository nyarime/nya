package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestProgressWriterClearsStaleSuffix(t *testing.T) {
	var buf bytes.Buffer
	pw := newProgressWriter(&buf)
	pw.print("\rlong line with @ 500.00 MB/s  ETA 1m00s")
	pw.print("\rshort")
	out := buf.String()
	if strings.Contains(out, "MB/s") && strings.HasSuffix(strings.TrimRight(out, "\033[K"), "short MB/s") {
		t.Fatalf("stale suffix remained: %q", out)
	}
	if !strings.Contains(out, "\033[K") {
		t.Fatal("expected ANSI clear-to-EOL")
	}
}
