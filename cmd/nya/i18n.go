package main

import (
	"os"
	"strings"
)

// Lightweight CLI i18n. Default is English.
// Override with NYA_LANG=zh|en, or LANG/LC_ALL starting with zh.

func init() {
	cliLang = detectCLILang()
}

var cliLang = "en"

func detectCLILang() string {
	if v := strings.TrimSpace(os.Getenv("NYA_LANG")); v != "" {
		return normalizeLang(v)
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return normalizeLang(v)
		}
	}
	return "en"
}

func normalizeLang(v string) string {
	v = strings.ToLower(v)
	switch {
	case v == "zh" || strings.HasPrefix(v, "zh_") || strings.HasPrefix(v, "zh-") || strings.HasPrefix(v, "chinese"):
		return "zh"
	default:
		return "en"
	}
}

// T returns a localized string for id. Missing keys fall back to English.
func T(id string) string {
	if m, ok := i18nCatalog[cliLang]; ok {
		if s, ok := m[id]; ok {
			return s
		}
	}
	if s, ok := i18nCatalog["en"][id]; ok {
		return s
	}
	return id
}

// Keep copy short—labels only, no marketing.
var i18nCatalog = map[string]map[string]string{
	"en": {
		"send.direct":   "Direct:",
		"send.archive":  "Archive:",
		"send.get":      "Get:",
		"send.lan":      "Local:",
		"send.file_fmt": "  file    %s",
		"send.nya_fmt":  "  archive %s",
		"send.get_fmt":  "  nya get --url %s",
		"send.stop":     "Ctrl+C to stop",
		"send.usage": `nya send — publish a file or folder

Usage:
  nya send [flags] <file|directory|archive.nya>

  file         Direct URL; <name>.nyam for nya get
  directory    <name>.nya for browsers; <name>.nyam for nya get
  archive.nya  Serve as-is with <name>.nyam

Example:
  nya get --url https://xxxx.trycloudflare.com/novel.txt.nyam

`,
	},
	"zh": {
		"send.direct":   "直链：",
		"send.archive":  "压缩包：",
		"send.get":      "获取：",
		"send.lan":      "本地：",
		"send.file_fmt": "  文件    %s",
		"send.nya_fmt":  "  压缩包  %s",
		"send.get_fmt":  "  nya get --url %s",
		"send.stop":     "Ctrl+C 结束",
		"send.usage": `nya send — 发布文件或目录

用法：
  nya send [flags] <文件|目录|archive.nya>

  文件         浏览器直链；<名>.nyam 供 nya get
  目录         <名>.nya 供浏览器；<名>.nyam 供 nya get
  archive.nya  直接托管并提供 <名>.nyam

示例：
  nya get --url https://xxxx.trycloudflare.com/novel.txt.nyam

`,
	},
}
