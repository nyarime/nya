package main

import (
	"fmt"
	"net/http"
	"strings"
)

// sendAccessLogLevel mirrors aria2-style console verbosity (notice/info/warn).
type sendAccessLogLevel int

const (
	sendAccessLogNotice sendAccessLogLevel = iota // default: transfers; hide browser .nyam
	sendAccessLogInfo                             // log every request
	sendAccessLogWarn                             // HTTP errors only
)

func parseSendAccessLogLevel(raw string) (sendAccessLogLevel, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "notice", "normal":
		return sendAccessLogNotice, nil
	case "info", "verbose", "debug":
		return sendAccessLogInfo, nil
	case "warn", "error":
		return sendAccessLogWarn, nil
	default:
		return 0, fmt.Errorf("send: unknown -access-log %q (want notice, info, warn)", raw)
	}
}

func shouldLogSendAccessAt(level sendAccessLogLevel, r *http.Request, status int) bool {
	switch level {
	case sendAccessLogInfo:
		return true
	case sendAccessLogWarn:
		return status >= 400
	default: // notice
		path := strings.ToLower(strings.TrimPrefix(r.URL.Path, "/"))
		if strings.HasSuffix(path, ".nyam") && !isNyaGetUserAgent(r.Header.Get("User-Agent")) {
			return false
		}
		return true
	}
}
