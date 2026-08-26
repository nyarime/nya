package main

import "strings"

// cliVersion is set at link time by scripts/release-local.sh / CI when available.
var cliVersion = "0.1.10"

const nyaGetUAProduct = "NyaGet"

func nyaCLIVersion() string {
	if v := strings.TrimSpace(cliVersion); v != "" {
		return strings.TrimPrefix(v, "v")
	}
	return "dev"
}

// nyaGetUserAgent identifies nya get HTTP requests (aria2-style Product/Version).
func nyaGetUserAgent() string {
	return nyaGetUAProduct + "/" + nyaCLIVersion()
}

func isNyaGetUserAgent(ua string) bool {
	ua = strings.TrimSpace(ua)
	return strings.HasPrefix(ua, nyaGetUAProduct+"/")
}
