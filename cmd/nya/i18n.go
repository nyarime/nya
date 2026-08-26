package main

import (
	"os"
	"strings"

	"github.com/nyarime/nya"
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
		"usage.main": `nya — Nyarime Archive

Usage:
  nya create  [flags] <archive.nya> <path>   create archive (download index embedded by default)
  nya list    [flags] <archive.nya>          list contents
  nya extract [flags] <archive.nya> [dir]    extract (default: current directory)
  nya open    [flags] <archive.nya>          extract beside archive into .<basename>/
  nya verify  [flags] <archive.nya>          check BLAKE3 digests
  nya info    [flags] <archive.nya>          show header details
  nya repair  <archive> [out]                repair NYA / ZIP / RAR (magic detect)
  nya augment <archive.nya> [out.nya]        increase FEC repair data
  nya convert [flags] <in> <out>             archive hub: zip|7z|rar|tar|nya ↔ zip|7z|rar|tar|nya
  nya export  [flags] <in.nya> <out.zip|…>   alias of convert for NYA → foreign
  nya import  [flags] <in.zip|…> <out.nya>   alias of convert for foreign → NYA
  nya manifest add|del|export …              embedded download index
  nya sfx     [flags] <archive.nya> -o <out> wrap as self-extractor
  nya get     [flags] --url <URL>            download (+ progress)
  nya send    [flags] <file|dir|archive.nya> share via tunnel / LAN
  nya gui     [archive.nya]                  open nyaFM
  nya associate [-uninstall]                 Windows: .nya → nya open

Passwords (no interactive prompt):
  Encrypted input  → pass -password (extract/list/info/open/verify)
                     or -source-password (convert/import/export)
  Missing password → error (will not hang waiting for stdin)
  Wrong password   → error
  -password on convert/create encrypts the *output* .nya (or zip/7z via 7z)

Levels 0–9: 0 store; 1–4 Zstd (fast extract); 5–9 LZMA2 (size). Default create level is 5.
Run "nya <command> -h" for per-command flags.
`,
		"usage.convert": `nya convert — archive format hub (file tree in the middle)

Usage:
  nya convert [flags] <input> <output>
  nya import  …   (alias)
  nya export  …   (alias)
  nya repack  …   (alias)

Examples:
  nya convert game.zip game.nya
  nya convert -fec 20 old.rar backup.nya
  nya convert archive.nya out.zip
  nya convert -source-password secret enc.zip plain.nya
  nya convert -password newsecret plain.nya locked.nya

Password policy:
  -source-password   unlock encrypted *input* (zip/7z/rar/nya). Required if input is encrypted.
  -password          encrypt *output* (.nya, or zip/7z via 7z). Optional.
  No password prompt; omit -source-password on an encrypted input → error.

Formats: {{formats}}
`,
		"flag.source_password":      "unlock encrypted input (required if input is encrypted; no prompt)",
		"flag.password_out":         "encrypt output (.nya, or zip/7z via 7z)",
		"flag.password_in":          "archive password (encrypted archives; no prompt)",
		"err.password_hint_extract": "— use: nya extract -password '…' %s",
		"err.password_hint_convert": "— use: nya convert -source-password '…' <in> <out>",
		"convert.need_args":         "convert needs <input> <output> (zip|7z|rar|tar|nya ↔ …)",
		"convert.done":              "%s → %s  %s  [%s]",

		"send.direct":       "Direct:",
		"send.archive":      "Archive:",
		"send.get":          "Get:",
		"send.lan":          "Local:",
		"send.file_fmt":     "  file    %s",
		"send.nya_fmt":      "  archive %s",
		"send.get_fmt":      "  nya get --url %s",
		"send.stop":         "Ctrl+C to stop",
		"send.tunnel.start": "nya send: starting Cloudflare tunnel…",
		"send.tunnel.ready": "nya send: public URL %s",
		"send.tunnel.err":   "nya send: tunnel: ",
		"send.tunnel.missing":  "nya send: cloudflared not found in PATH.",
		"send.tunnel.download": "  download: %s",
		"send.tunnel.lan":      "  or: nya send -no-tunnel for LAN only",
		"send.stopped":      "nya send: stopped",
		"send.log.fmt":      "  [recv] %s /%s %s from %s in %s — %s%s\n",
		"send.log.ok":       "OK",
		"send.log.fail":     "ERR",
		"send.log.range":    "range",
		"send.log.cf":       "(Cloudflare)",
		"send.log.local":    "(local)",
		"send.log.nyaget":   "(nya-get)",
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
		"usage.main": `nya — Nyarime 归档

用法：
  nya create  [flags] <archive.nya> <path>   创建归档（默认嵌入下载索引）
  nya list    [flags] <archive.nya>          列出内容
  nya extract [flags] <archive.nya> [dir]    解压（默认当前目录）
  nya open    [flags] <archive.nya>          解压到归档旁 .<basename>/
  nya verify  [flags] <archive.nya>          校验 BLAKE3
  nya info    [flags] <archive.nya>          显示头信息
  nya repair  <archive> [out]                修复 NYA / ZIP / RAR（魔数识别）
  nya augment <archive.nya> [out.nya]        追加 FEC 恢复数据
  nya convert [flags] <in> <out>             归档互转：zip|7z|rar|tar|nya ↔ zip|7z|rar|tar|nya
  nya export  [flags] <in.nya> <out.zip|…>   convert 别名（NYA → 外部格式）
  nya import  [flags] <in.zip|…> <out.nya>   convert 别名（外部 → NYA）
  nya manifest add|del|export …              嵌入式下载索引
  nya sfx     [flags] <archive.nya> -o <out> 自解压封装
  nya get     [flags] --url <URL>            下载（含进度）
  nya send    [flags] <文件|目录|archive.nya> 隧道 / 局域网分享
  nya gui     [archive.nya]                  打开 nyaFM
  nya associate [-uninstall]                 Windows：.nya → nya open

密码（不交互询问）：
  输入已加密  → extract/list/info/open/verify 用 -password
                convert/import/export 用 -source-password
  未提供密码  → 直接报错（不会卡在 stdin 等输入）
  密码错误    → 报错
  convert/create 的 -password 只用于*加密输出* .nya（或经 7z 加密 zip/7z）

压缩等级 0–9：0 存储；1–4 Zstd（解压快）；5–9 LZMA2（体积）。create 默认 5。
运行 "nya <command> -h" 查看子命令参数。
`,
		"usage.convert": `nya convert — 归档格式互转（中间为文件树）

用法：
  nya convert [flags] <输入> <输出>
  nya import  …   （别名）
  nya export  …   （别名）
  nya repack  …   （别名）

示例：
  nya convert game.zip game.nya
  nya convert -fec 20 old.rar backup.nya
  nya convert archive.nya out.zip
  nya convert -source-password secret enc.zip plain.nya
  nya convert -password newsecret plain.nya locked.nya

密码策略：
  -source-password   解开加密*输入*（zip/7z/rar/nya）。输入已加密时必填。
  -password          加密*输出*（.nya，或经 7z 的 zip/7z）。可选。
  不交互要密码；加密输入却未给 -source-password → 报错。

格式：{{formats}}
`,
		"flag.source_password":      "解开加密输入（输入已加密时必填；不交互询问）",
		"flag.password_out":         "加密输出（.nya，或经 7z 的 zip/7z）",
		"flag.password_in":          "归档密码（加密归档；不交互询问）",
		"err.password_hint_extract": "— 请使用: nya extract -password '…' %s",
		"err.password_hint_convert": "— 请使用: nya convert -source-password '…' <in> <out>",
		"convert.need_args":         "convert 需要 <输入> <输出>（zip|7z|rar|tar|nya ↔ …）",
		"convert.done":              "%s → %s  %s  [%s]",

		"send.direct":       "直链：",
		"send.archive":      "压缩包：",
		"send.get":          "获取：",
		"send.lan":          "本地：",
		"send.file_fmt":     "  文件    %s",
		"send.nya_fmt":      "  压缩包  %s",
		"send.get_fmt":      "  nya get --url %s",
		"send.stop":         "Ctrl+C 结束",
		"send.tunnel.start": "nya send: 正在启动 Cloudflare 隧道…",
		"send.tunnel.ready": "nya send: 公网 URL %s",
		"send.tunnel.err":   "nya send: 隧道: ",
		"send.tunnel.missing":  "nya send: 未在 PATH 中找到 cloudflared。",
		"send.tunnel.download": "  下载：%s",
		"send.tunnel.lan":      "  或：nya send -no-tunnel 仅局域网",
		"send.stopped":      "nya send: 已停止",
		"send.log.fmt":      "  [接收] %s /%s %s 来自 %s 用时 %s — %s%s\n",
		"send.log.ok":       "OK",
		"send.log.fail":     "失败",
		"send.log.range":    "范围",
		"send.log.cf":       "(Cloudflare)",
		"send.log.local":    "(本地)",
		"send.log.nyaget":   "(nya-get)",
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

func convertUsageText() string {
	return strings.ReplaceAll(T("usage.convert"), "{{formats}}", nya.ListHubFormats())
}
