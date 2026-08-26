package main

import "strings"

// cliVersion is set at link time by scripts/release-local.sh / CI when available.
var cliVersion = "0.1.15"

const nyaUAProduct = "Nya"

func nyaCLIVersion() string {
	if v := strings.TrimSpace(cliVersion); v != "" {
		return strings.TrimPrefix(v, "v")
	}
	return "dev"
}

// nyaGetUserAgent is the default HTTP User-Agent for nya get (aria2-style Product/Version).
func nyaGetUserAgent() string {
	return nyaUAProduct + "/" + nyaCLIVersion()
}

func isNyaGetUserAgent(ua string) bool {
	ua = strings.TrimSpace(ua)
	return strings.HasPrefix(ua, nyaUAProduct+"/")
}
