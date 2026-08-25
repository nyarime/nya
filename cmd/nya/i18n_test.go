package main

import (
	"testing"
)

func TestDetectCLILang(t *testing.T) {
	t.Setenv("NYA_LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := detectCLILang(); got != "en" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := detectCLILang(); got != "zh" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("NYA_LANG", "en")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := detectCLILang(); got != "en" {
		t.Fatalf("NYA_LANG should win, got %q", got)
	}
}

func TestTFallback(t *testing.T) {
	cliLang = "en"
	if T("send.stop") != "Ctrl+C to stop." {
		t.Fatal(T("send.stop"))
	}
	cliLang = "zh"
	if T("send.stop") != "按 Ctrl+C 停止。" {
		t.Fatal(T("send.stop"))
	}
	cliLang = "fr" // missing catalog → en
	if T("send.stop") != "Ctrl+C to stop." {
		t.Fatal(T("send.stop"))
	}
}
