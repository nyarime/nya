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

var i18nCatalog = map[string]map[string]string{
	"en": {
		"send.browser_direct":  "Browser (original file):",
		"send.nya_get_file":    "nya get (compressed transfer → restore same file):",
		"send.browser_nya":     "Browser (download .nya archive):",
		"send.nya_get_dir":     "nya get (restore original folder):",
		"send.browser_nya_only": "Browser (.nya):",
		"send.nya_get":         "nya get:",
		"send.lan":             "LAN:",
		"send.browser_file":    "  browser file: %s",
		"send.browser_nya_fmt": "  browser .nya: %s",
		"send.nya_get_fmt":     "  nya get:      nya get --url %s",
		"send.payload_hint":    "  (.nya payload: %s)",
		"send.stop":            "Ctrl+C to stop.",
		"send.usage_file":      "browser direct link + index.nyam for nya get",
		"send.usage_dir":       "browser .nya archive + index.nyam for nya get",
		"send.usage_nya":       "serve existing archive + index.nyam",
		"send.links_file":      "file   → direct URL (original) + index.nyam (compressed, restore same file)",
		"send.links_folder":    "folder → .nya (browser archive) + index.nyam (restore same tree)",
		"send.links_magic":     "Packing uses content magic (not extension): text/code/logs compress well.",
		"send.receiver":        "Receiver:",
	},
	"zh": {
		"send.browser_direct":   "浏览器直链（原文件）：",
		"send.nya_get_file":     "nya get（压缩传输 → 还原同名文件）：",
		"send.browser_nya":      "浏览器（下载 .nya 压缩档）：",
		"send.nya_get_dir":      "nya get（还原为原文件夹）：",
		"send.browser_nya_only": "浏览器（.nya）：",
		"send.nya_get":          "nya get：",
		"send.lan":              "局域网：",
		"send.browser_file":     "  浏览器文件：%s",
		"send.browser_nya_fmt":  "  浏览器 .nya：%s",
		"send.nya_get_fmt":      "  nya get：     nya get --url %s",
		"send.payload_hint":     "  （.nya 载荷：%s）",
		"send.stop":             "按 Ctrl+C 停止。",
		"send.usage_file":       "浏览器直链 + index.nyam 给 nya get",
		"send.usage_dir":        "浏览器下 .nya 压缩档 + index.nyam 给 nya get",
		"send.usage_nya":        "托管已有归档 + index.nyam",
		"send.links_file":       "文件 → 原文件直链 + index.nyam（压缩传输并还原）",
		"send.links_folder":     "目录 → .nya（浏览器下压缩档）+ index.nyam（还原目录树）",
		"send.links_magic":      "按内容 magic 分类（不看扩展名）：文本/代码/日志压缩收益大。",
		"send.receiver":         "接收端：",
	},
}
