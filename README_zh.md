# nya

[English](README.md) | 简体中文

**NYA 要解决的是：链路上坏了、传了一半，文件往往还能打开** — 而不是再做一个「略强一点的 7z / RAR」。

一次打包同时带上压缩与可配置的前向纠错（FEC），用 `nya send` / CDN / `.nyam` 发一个 URL，再用 `nya get` + `nya repair` 拉回并修复。这条故事更贴合 **游戏分包、固件镜像、CDN 大文件、不可靠隧道**，而不是「又一个压缩格式」。产品方向见 **[ROADMAP.md](ROADMAP.md)**。

本仓库是格式规范、参考实现与 **`nya` CLI**（`get` / `send` / `gui` / `sfx` …）的权威源。纯 Go、无 cgo。依赖：

- `github.com/nyarime/gofec` — RaptorQ / LDPC
- `github.com/nyarime/compress` **v0.2.7** — 家用 NYA-Zstd + NYA-LZMA2（[Apache-2.0](docs/COMPRESS-ECOSYSTEM.md)）
- `golang.org/x/sys` — xattr

**NYA-Zstd** 是家用编解码（RFC 8878）；**NYA-LZMA2** 走 `--best` — 见 [SPEC-CODECS.md](SPEC-CODECS.md)。

## 归档里有什么

| 层 | 作用 |
| --- | --- |
| 压缩 | 等级 1–4 用 Zstandard（RFC 8878）；5–9 用 LZMA2；0 为存储 |
| 预过滤 | x86 / ARM / AArch64 / MIPS 的 BCJ；delta 过滤 |
| 完整性 | 每块 BLAKE3-256（含 AVX2 / AVX-512 / SSE2 / NEON） |
| 恢复 | RaptorQ / Leopard-RS 奇偶，按载荷百分比配置 |
| 分发 | 嵌入下载索引 + `.nyam`；大文件多块 Range |
| 加密 | 可选 AES-256-GCM（压缩载荷） |
| 元数据 | Unix 权限、属主、时间戳、符号/硬链接、设备节点、FIFO、xattr |

不像 RAR 恢复记录常见的约 10% 上限，`-fec 50` 可把恢复数据做到载荷一半。见 [docs/BENCHMARK-FEC.md](docs/BENCHMARK-FEC.md)。

## 安装

### Linux / macOS

默认装到 `~/.local`（`nya` → `~/.local/bin`）。**只装一个 `nya`** — 下载用 `nya get`，不再单独装 `nya-get`。

```bash
curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.sh | bash
```

```bash
# 可选
bash install.sh --prefix /usr/local          # 自定义前缀
bash install.sh --version 0.1.19              # 固定版本
```

### Windows

默认装到 `%LOCALAPPDATA%\Programs\NYA`，写入用户 PATH，关联 `.nya` → `nya open`。**仅 `nya.exe`**，下载用 `nya get`。

```powershell
irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.ps1 | iex
```

```powershell
# 可选
install.ps1 -Prefix "D:\Tools\NYA"
install.ps1 -Version 0.1.19
install.ps1 -NoAssociate   # 不关联 .nya
install.ps1 -NoPath        # 不改 PATH
```

装完请新开终端，再运行 `nya help`。

### 卸载

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.sh | bash
# bash uninstall.sh --prefix ~/.local
```

```powershell
# Windows
irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.ps1 | iex
# uninstall.ps1 -Prefix "$env:LOCALAPPDATA\Programs\NYA"
```

等价：`install.sh --uninstall` / `install.ps1 -Uninstall`。  
发行包：[GitHub Releases](https://github.com/nyarime/nya/releases/latest)。打包说明：[docs/RELEASE.md](docs/RELEASE.md)。

### 从源码安装

```bash
go install github.com/nyarime/nya/cmd/nya@latest
```

发行包只有 **`nya`**：归档 CLI、`nya get` / `nya send`、SFX stub（`create -sfx` / `nya sfx`）。源码里保留 `cmd/nya-get` 兼容 shim，但 `install.sh` 不会安装它。

## 命令行

```bash
nya create backup.nya ./project                    # 默认等级创建（含嵌入下载索引）
nya create -no-embed backup.nya ./project          # 不写下载索引
nya create -level 9 -solid backup.nya ./project    # 尽量小
nya create -level 1 backup.nya ./project           # 尽量快
nya create -fec 30 backup.nya ./data               # 约 30% 恢复数据
nya list backup.nya                                # 列出内容
nya extract backup.nya ./restored                  # 解压
nya open game.nya                                  # 解到旁边 .\game\（已存在则 "game 2"）
nya open -overwrite game.nya                       # 解进已有 .\game\（覆盖文件）
nya verify backup.nya                              # 校验摘要
nya info backup.nya                                # 头信息（含编解码）
nya repair damaged.nya fixed.nya                   # 用奇偶数据修复
nya convert legacy.zip repaired.nya                # zip/7z/rar/nya 互转（输出 .nya 可加 FEC）
nya convert -fec 20 old.rar backup.nya
nya convert archive.nya out.zip                    # nya → zip
nya convert -source-password secret enc.zip out.nya  # 加密输入必须带参数（不交互）
nya manifest add GamePack.nya                      # 写入/更新嵌入下载索引
nya manifest del GamePack.nya                      # 删除嵌入索引
nya manifest export -o GamePack.nyam --url https://cdn/game.nya GamePack.nya
nya sfx pack.nya -o pack.exe                       # 打成自解压（Go stub）
nya create -sfx game.bin -level 3 ./GameData/      # 创建并打包 SFX
nya get --url https://cdn.example.com/pack.nya
nya send pack.nya                                  # 本地 HTTP + TryCloudflare
nya gui pack.nya                                   # nyaFM（若已安装 nya-fm）
nya associate                                      # Windows：双击 .nya → nya open
```

### Windows 双击打开

```bat
nya associate
:: 然后双击 game.nya → 解压到 .\game\
```

详见 [examples/windows-open](examples/windows-open/README.md)。

### 发送 / 接收（TryCloudflare）

```bash
nya send novel.txt
# Direct:  https://….trycloudflare.com/novel.txt
# Get:     nya get --url https://….trycloudflare.com/novel.txt.nyam

nya send ./GameData
# Archive: https://….trycloudflare.com/GameData.nya
# Get:     nya get --url https://….trycloudflare.com/GameData.nyam

nya get --url https://xxxx.trycloudflare.com/novel.txt.nyam
```

索引 URL 跟资源名走（`name.nyam` / `name.nya`），不是固定的 `/index.nyam`。  
CLI 默认英文；`NYA_LANG=zh` 或 `LANG=zh_CN` 可切中文提示。

选项：`-no-tunnel`。未安装 cloudflared 时见 https://developers.cloudflare.com/tunnel/downloads/ 。Quick Tunnel 为临时链接（遵循 Cloudflare ToS）。

### 大包分发（`nya get`）

```bash
nya create -level 9 -solid -fec 20 GamePack.nya ./GameData/   # 默认嵌入下载索引
# 可选旁路清单：
# nya manifest export -o GamePack.nyam --url https://cdn.example.com/GamePack.nya GamePack.nya

nya get --url https://cdn.example.com/GamePack.nya          # 下载 GamePack.nya（不自动解压）
nya get -extract --url https://cdn.example.com/GamePack.nya # 下载后还原 GameData/
nya get -c 16 GamePack.nyam                                 # 经典 .nyam
nya get --paths "Game/Data/level1.bin" GamePack.nyam        # 部分拉取（不解压）
```

`.nyam` 结构见 [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md)。

### 统一修复（`nya repair`）

按**文件魔数**识别格式（扩展名可错可无）：

```bash
nya repair damaged.nya              # NYA FEC 修复
nya repair corrupted.dat            # 内容是 PK… 则按 ZIP 修
nya repair broken.rar fixed.rar     # RAR 结构重建（RAR4/RAR5 存储块）
```

7z 不支持修复（无恢复记录）。若 7z 仍能解开，可用 `nya convert`。

### 归档互转（zip / 7z / rar / nya ↔）

**`nya convert`** 以文件树为中枢：解开任一支持格式，再打成另一种。输出为 `.nya` 时可加 FEC。

```bash
nya convert game.zip game.nya
nya convert -fec 30 archive.7z archive.nya         # 需 PATH 上有 7z
nya convert -source-password secret old.rar new.nya  # 加密输入必填；不交互询问
nya convert archive.nya out.zip                    # nya → zip
nya convert -password lock plain.nya locked.nya    # 加密*输出* .nya
nya convert -level 9 -solid -fec 10 bundle.zip bundle.nya
```

**密码策略（不交互询问）：**

| 参数 | 含义 |
| --- | --- |
| `-source-password` | 解开加密**输入**（zip/7z/rar/nya）。输入已加密时**必填**；省略 → 报错。 |
| `-password` | 加密**输出**（`.nya`，或经 7z 的 zip/7z）。可选。 |

别名：`nya import`、`nya export`、`nya repack`。格式：**zip**（纯 Go）；**7z / rar / tar.\*** 需 [7-Zip](https://www.7-zip.org/) / `p7zip-full`。路径存 **UTF-8**（中文文件名可往返）。FEC 对比见 [docs/BENCHMARK-FEC.md](docs/BENCHMARK-FEC.md)。

等级 0–9，观感类似 7-Zip / WinRAR：

| 等级 | 名称 | 编解码 |
| ---: | --- | --- |
| 0 | store | 无 |
| 1–2 | fastest | Zstandard |
| 3–4 | fast | Zstandard |
| 5–6 | normal（默认） | LZMA2 |
| 7–8 | good | LZMA2，更大窗口与更深搜索 |
| 9 | best | LZMA2，最大窗口与搜索 |

`create` 还支持 `-solid`（整包一流）、`-codec`（覆盖等级默认编解码）、`-password`、`-workers`（创建/解压并发）、**`-no-embed`**（跳过默认可单 URL 下载的嵌入索引），以及 **`-dict`**（文本型 solid 包嵌入 zstd 字典，等级 1–4）。Solid 还会在有帮助时 **自动推导字典** — 见 [docs/SOLID-DICT.md](docs/SOLID-DICT.md)。

## 作为库使用

```go
package main

import (
	"os"

	"github.com/nyarime/nya"
)

func main() {
	f, err := os.Create("backup.nya")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// 10% 恢复数据，最高压缩，非 solid。
	w := nya.NewWriterOpts(f, 10, nya.LevelBest, false)
	if err := w.AddFile("./project"); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
}
```

读取：

```go
r, err := nya.Open("backup.nya")
if err != nil {
	panic(err)
}
if !r.Verify() {
	// 摘要不匹配；可尝试 nya.Repair
}
if err := r.Extract("./restored"); err != nil {
	panic(err)
}
```

包本身不写标准输出。解压进度用 `Reader.OnEntry`；`Repair` 等可用包级 `nya.Log`。

也可单独用编解码：`ZstdCompress`、`DecompressZstd`、`Lzma2Compress`、`XzCompress`、`Blake3Sum256`，以及 BCJ / delta 过滤均已导出。

## 现状与基准

百分比为相对输入的压缩体积，越低越好。下列数据来自一台机器上与参考归档工具的对比（2026-08 cloud agent；请本地重跑，数值随 CPU 变化）。宜作相对比较。

| 语料 | 大小 | nya (level 9) | xz -9 | 7z -mx9 | zstd -19 |
| --- | ---: | ---: | ---: | ---: | ---: |
| structured text | 3391192 | 3.62% (201ms) | 4.24% (1.097s) | 4.18% (716ms) | 7.49% (2.098s) |
| markdown | 20786 | 4.05% (1ms) | 3.71% (6ms) | 4.32% (7ms) | 6.69% (9ms) |
| ELF binary | 48000 | 100.01% (8ms) | 100.12% (44ms) | 100.26% (6ms) | 100.03% (4ms) |
| 17 MB ELF | 17825792 | 100.00% (761ms) | 100.01% (3.391s) | 100.01% (676ms) | 100.00% (1.59s) |
| 120-file tree, solid | 1082080 | 31.62% (911ms) | 30.42% (110ms) | 30.92% (24ms) | 30.37% (31ms) |

**Level-9 解析 / solid 排序（同语料，本地重生成：`NYA_BENCH_WRITE=1 go test -run TestREADMEBenchmarkSuite -timeout 60m ./...`）：**

| 语料 | 变体 | 比率 | 时间 |
| --- | --- | ---: | ---: |
| structured text | greedy | 3.62% | 201ms |
| structured text | optimal | 4.61% | 1.5s |
| markdown | greedy | 4.05% | 1ms |
| markdown | optimal | 9.95% | 8ms |
| ELF binary | greedy | 100.01% | 8ms |
| ELF binary | optimal | 100.01% | 20ms |
| 17 MB ELF | greedy | 100.00% | 761ms |
| 17 MB ELF | optimal | 100.01% | 1.8s |
| 120-file tree, solid | walk+greedy | 30.86% | 302ms |
| 120-file tree, solid | sorted+greedy | 30.39% | 301ms |
| 120-file tree, solid | sorted+optimal | 31.52% | 1.336s |
| 120-file tree, solid | nya archive | 31.62% | 911ms |

细节：[docs/BENCHMARK-COMPRESS.md](docs/BENCHMARK-COMPRESS.md)。

### 家用 LZMA2 vs 7-Zip（`compress` v0.2.6）

单流 level-9 optimal + dual-encode（`compress/lzma2` 测试）：

| 语料 | nya% | 7z -mx9% | gap |
| --- | ---: | ---: | ---: |
| structured text（合成） | 0.9 | 1.6 | −0.7pp |
| pseudo_enwik（合成） | 0.7 | 1.1 | −0.5pp |
| mixed JSON / log | **4.8** | 5.0 | **−0.1pp** |
| Silesia dickens（1 MiB 切片） | **30.2** | 29.6 | **+0.6pp** |

### Solid 归档 vs 7-Zip（36 文件混合语料）

`TestSolidArchiveVs7z` — NYA level-9 solid vs `7z -mx9 -m0=lzma2 -ms=on`：

| | NYA | 7z | gap |
| --- | ---: | ---: | ---: |
| 压缩/原始 | **8.75%** | 8.44% | **+0.32pp** |

Solid 写入使用 **扩展名分组**、**text-like 优先排序** 与 level-9 LZMA2；BCJ whole-stream 门控后 gap 从约 +1.79pp 改善。重跑：`go test -run TestSolidArchiveVs7z -timeout 10m -v ./...`

结构化文本上 nya greedy（3.62%）优于 xz / 7z（约 4.2%）。多文件 solid 时，排序 + greedy 可贴近 xz/7z；编码仍常慢于 7-Zip。Level 9 的 optimal parse 在这些语料上往往更大更慢；当前默认是 greedy + 扩展名/内容种类排序。

Solid 使用**扩展名分组**、**内容种类排序**（同扩展名内按 magic，text-like 优先）、**可选自动 zstd 字典**（文本型 solid、等级 3–4），以及 level-9 **greedy LZMA2**（`compress` v0.2.3+ dual-encode optimal 守卫）。重跑基准需 PATH 上有 `xz`、`7z`、`zstd`。

解压另一面：此处 LZMA2 约 18–59 MB/s，zstd 路径约 127–275 MB/s；读远多于写时，宜用等级 1–4。

zstd 编码器匹配与熵编码模式少于参考实现，故比率略逊。两种编解码均对照第三方解码器校验，`.nya` 载荷可被合规 zstd / xz 实现阅读。

## 兼容性

全局头记录 `VersionMajor.VersionMinor`。本实现**可读 1.0–1.3**。**写出**：

| 条件 | 写出的 minor |
| --- | ---: |
| 默认非 solid 且文件 ≤ 4 MiB（未加密） | **1.1** |
| 加密 | **1.2** |
| 任一多块条目（`ChunkCount > 1`；非 solid 且 &gt; 4 MiB 时默认） | **1.3** |

长期策略见 **[COMPATIBILITY.md](COMPATIBILITY.md)**、产品方向见 **[ROADMAP.md](ROADMAP.md)**。

- **1.3** — 多块非 solid + 按块 FEC。**≤ v1.2 的阅读器完全打不开**这类归档（不是「能解但不并行」）。若必须兼容旧工具，用 `-multi-chunk=false`。
- **1.2** — Argon2id KDF + 头内 salt；`FlagEncrypted` + `FlagKDFArgon2id`。旧 SHA-256(password) 仍可读。
- **1.1** — zstd 帧遵循 RFC 8878。
- **1.0** — 旧 zstd sequence 表；仍完全可读。

### 升级说明

| 从 | 到 | 操作 |
| --- | --- | --- |
| 1.0 zstd 表 | 1.1 | 用当前 `nya create` 重打包 |
| SHA-256 加密 | 1.2 Argon2id | 用 `-password` 重建（旧包仍可按密码解） |
| 需要旧（≤1.2）阅读器 | 保持 ≤1.2 | `nya create -multi-chunk=false` |
| 非 solid 多文件 | solid + 排序 | 对目录 `nya create -solid -level 9` |

**默认压缩等级仍是 5（LZMA2）**。很多分发 / `get` 场景更适合 **1–4（zstd，解压快）** — 是否改默认见 [ROADMAP.md](ROADMAP.md)，请勿假定会永远停在 5。

## 已知限制

- **多块（v1.3）默认开启**：非 solid 且 &gt; 4 MiB（raw 4 MiB；&gt; 64 MiB 时 8 MiB）。Solid 仍每条目一块。设计：[docs/SPEC-MULTICHUNK.md](docs/SPEC-MULTICHUNK.md)。
- zstd sequence 的**自定义 FSE 表**仍关闭（约 1% 比率）。
- **Optimal parse** 默认关闭；混合多文件 solid 上 greedy + 排序通常更好。库内 `OptimalParse` 可按语料打开。
- 公开 **Silesia / enwik9 / 游戏资源 / 固件** 语料与 raw 数据仍在落地 — 见 [docs/BENCHMARK-CORPUS.md](docs/BENCHMARK-CORPUS.md)。

## 许可

NYA 是自由软件：可在 [GNU GPL v3.0](LICENSE) 下使用、修改与再分发。

**双许可：** 若需在专有 / 闭源产品中嵌入或发行 NYA 且不想承担 GPL 义务，请联系 **nyarime@naixi.net** 洽谈商业许可。

## 格式文档

- [ROADMAP.md](ROADMAP.md) — **产品优先级**（先讲清 FEC + 分发）
- [COMPATIBILITY.md](COMPATIBILITY.md) — **v1 LTS 策略**（采用前请读）
- [SPEC.md](SPEC.md) — NYA 磁盘布局
- [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md) — **v1 基础**（tail、solid、去重、NyaFS、会话）
- [docs/SPEC-MULTICHUNK.md](docs/SPEC-MULTICHUNK.md) — **多块条目**（v1.3）
- [docs/BENCHMARK-MULTICHUNK.md](docs/BENCHMARK-MULTICHUNK.md) — **多块并行**（压缩 worker、FEC 修复）
- [docs/BENCHMARK-COMPRESS.md](docs/BENCHMARK-COMPRESS.md) — 压缩 A/B
- [docs/SOLID-DICT.md](docs/SOLID-DICT.md) — solid zstd 字典（`-dict` + 自动推导）
- [docs/COMPRESS-ECOSYSTEM.md](docs/COMPRESS-ECOSYSTEM.md) — `nyarime/compress` 拆库
- [docs/BENCHMARK-FEC.md](docs/BENCHMARK-FEC.md) — FEC 恢复对比
- [docs/BENCHMARK-CORPUS.md](docs/BENCHMARK-CORPUS.md) — 公开语料与 raw 数据计划
- [docs/NOTE-UPX.md](docs/NOTE-UPX.md) — UPX 与归档压缩在 ELF 上的关系
- [SPEC-CODECS.md](SPEC-CODECS.md) — **NYA-Zstd & NYA-LZMA2**
- [SPEC-SFX.md](SPEC-SFX.md) — **自解压**（`nya` 统一 stub）
- [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md) — `.nyam` 与 `nya get` 传输块
- [fm/README.md](fm/README.md) — **nyaFM** Rust GUI

### 自解压归档（类似 7-Zip）

```bash
nya create -sfx game.exe -level 3 ./GameData/
nya sfx pack.nya -o pack.exe
./pack.exe                        # 解到可执行文件旁边
```

双击 / 直接运行会解到 SFX 所在目录（类似 macOS「归档实用工具」）。`-o DIR` 可改目标。  
**`nya`** 同时承担 CLI 与 SFX stub：`create -sfx` / `nya sfx` 用当前 `nya` 作前缀，再拼接归档与 footer。请勿对 SFX 输出做 UPX。

```bash
cargo build -p nya-fm --release
./target/release/nya-fm pack.nya   # GUI：列表 + 解压
```
