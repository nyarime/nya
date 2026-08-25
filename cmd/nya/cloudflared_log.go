package main

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// tunnelLogSink filters cloudflared stderr; only important lines reach the user.
type tunnelLogSink struct {
	verbose atomic.Bool
	quiet   atomic.Bool
}

func (s *tunnelLogSink) setVerbose(v bool) {
	s.verbose.Store(v)
}

func (s *tunnelLogSink) mute() {
	s.quiet.Store(true)
}

// handleLine parses one cloudflared log line. Returns public trycloudflare URL when found.
func (s *tunnelLogSink) handleLine(line string) (publicURL string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if u := tryCloudflareURL.FindString(line); u != "" {
		return strings.TrimRight(u, "/")
	}
	if s.quiet.Load() || !s.verbose.Load() {
		if isCloudflaredErrorLine(line) {
			fmt.Fprintln(os.Stderr, T("send.tunnel.err")+line)
		}
		return ""
	}
	if shouldShowCloudflaredLine(line) {
		fmt.Fprintln(os.Stderr, line)
	}
	return ""
}

func shouldShowCloudflaredLine(line string) bool {
	low := strings.ToLower(line)
	if tryCloudflareURL.FindString(line) != "" {
		return false // nya prints its own public URL line
	}
	if isCloudflaredErrorLine(line) {
		return true
	}
	// Startup banner table row with URL is noisy; suppress marketing + INF spam.
	if strings.Contains(low, "thank you for trying") ||
		strings.Contains(low, "cloudflare tunnel") && strings.Contains(low, "inf") {
		return false
	}
	if strings.Contains(low, "version") && strings.Contains(low, "cloudflared") {
		return false
	}
	return false
}

func isCloudflaredErrorLine(line string) bool {
	low := strings.ToLower(line)
	return strings.Contains(low, " error") ||
		strings.Contains(low, " err ") ||
		strings.Contains(low, "fatal") ||
		strings.Contains(low, "failed") ||
		strings.HasPrefix(low, "error:")
}
