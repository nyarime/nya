package main

import (
	"strings"
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
	if T("send.stop") != "Ctrl+C to stop" {
		t.Fatal(T("send.stop"))
	}
	cliLang = "zh"
	if T("send.stop") != "Ctrl+C 结束" {
		t.Fatal(T("send.stop"))
	}
	cliLang = "fr"
	if T("send.stop") != "Ctrl+C to stop" {
		t.Fatal(T("send.stop"))
	}
}

func TestUsageI18n(t *testing.T) {
	cliLang = "en"
	if !strings.Contains(T("usage.main"), "source-password") {
		t.Fatal("en usage.main missing password policy")
	}
	if !strings.Contains(T("usage.convert"), "-source-password") {
		t.Fatal("en usage.convert missing source-password")
	}
	cliLang = "zh"
	if !strings.Contains(T("usage.main"), "source-password") {
		t.Fatal("zh usage.main missing source-password")
	}
	if !strings.Contains(T("usage.convert"), "不交互") {
		t.Fatal("zh usage.convert missing no-prompt policy")
	}
}
