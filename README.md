# nya

[English](README.md) | [简体中文](README_zh.md)

**NYA is built for shipping files that still open after the link fails** —
not for winning a pure ratio race against 7-Zip or WinRAR.

Pack once with compression **and** configurable forward error correction,
publish a single URL (`nya send` / CDN / `.nyam`), and recover with
`nya get` + `nya repair`. That story fits **game packs**, **firmware
images**, **CDN large objects**, and **unreliable tunnels** better than
“yet another archive format.” Product direction:
**[ROADMAP.md](ROADMAP.md)**.

This repository is the **canonical** format spec, reference implementation,
and `nya` CLI (`get` / `send` / `gui` / `sfx`, …). Pure Go, no cgo. Dependencies:

- `github.com/nyarime/gofec` (RaptorQ / LDPC)
- `github.com/nyarime/compress` **v0.2.7** — house NYA-Zstd + NYA-LZMA2 ([Apache-2.0](docs/COMPRESS-ECOSYSTEM.md))
- `golang.org/x/sys` (xattrs)

**NYA-Zstd** is the house codec (RFC 8878); **NYA-LZMA2** is the `--best` lane — see [SPEC-CODECS.md](SPEC-CODECS.md).

## What is in an archive

| Layer | What it does |
| --- | --- |
| Compression | Zstandard (RFC 8878) at levels 1–4; LZMA2 at 5–9; stored at 0 |
| Pre-filters | BCJ branch conversion for x86, ARM, AArch64 and MIPS binaries; delta filter |
| Integrity | BLAKE3-256 over every chunk, with AVX2/AVX-512/SSE2/NEON assembly paths |
| Recovery | RaptorQ / Leopard-RS parity, sized as a percentage of the payload |
| Distribution | Embedded download index + `.nyam`; multi-chunk ranges for large files |
| Encryption | Optional AES-256-GCM over the compressed payload |
| Metadata | Unix mode, owner, timestamps, symlinks, hardlinks, device nodes, FIFOs, xattrs |

Unlike the ~10% ceiling that RAR recovery records often impose, recovery
size is a free parameter: `-fec 50` stores half the payload again in parity.
See [docs/BENCHMARK-FEC.md](docs/BENCHMARK-FEC.md).

## Install

### Linux / macOS

Installs into `~/.local` by default (`nya` → `~/.local/bin`). **One binary only** — download via `nya get` (no separate `nya-get`).

```bash
curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.sh | bash
```

```bash
# optional
bash install.sh --prefix /usr/local          # custom prefix
bash install.sh --version 0.1.16              # pin a release
```

### Windows

Installs into `%LOCALAPPDATA%\Programs\NYA`, adds user PATH, associates `.nya` → `nya open`. **Only `nya.exe`** — use `nya get` for downloads.

```powershell
irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.ps1 | iex
```

```powershell
# optional
install.ps1 -Prefix "D:\Tools\NYA"
install.ps1 -Version 0.1.16
install.ps1 -NoAssociate   # skip .nya file association
install.ps1 -NoPath        # skip PATH change
```

Open a new terminal after install, then run `nya help`.

### Uninstall

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

Same effect: `install.sh --uninstall` / `install.ps1 -Uninstall`.  
Releases: [GitHub Releases](https://github.com/nyarime/nya/releases/latest). Packaging notes: [docs/RELEASE.md](docs/RELEASE.md).

### From source

```bash
go install github.com/nyarime/nya/cmd/nya@latest
```

**`nya`** is the only release binary: archive CLI, `nya get` / `nya send`, and SFX stub (`create -sfx` / `nya sfx`). The `cmd/nya-get` shim remains in source for compatibility but is not installed by `install.sh`.

## Command line

```bash
nya create backup.nya ./project                    # create at the default level (+ embedded download index)
nya create -no-embed backup.nya ./project          # skip download index
nya create -level 9 -solid backup.nya ./project    # smallest
nya create -level 1 backup.nya ./project           # fastest
nya create -fec 30 backup.nya ./data               # add 30% recovery data
nya list backup.nya                                # inspect
nya extract backup.nya ./restored                  # extract
nya open game.nya                                  # extract beside → .\game\ (or "game 2" if exists)
nya open -overwrite game.nya                       # extract into existing .\game\ (overwrite files)
nya verify backup.nya                              # check stored digests
nya info backup.nya                                # header details, including codec
nya repair damaged.nya fixed.nya                   # rebuild using the parity data
nya convert legacy.zip repaired.nya                # zip/7z/rar/nya ↔ (+FEC on .nya out)
nya convert -fec 20 old.rar backup.nya             # WinRAR-style recovery, but configurable
nya convert archive.nya out.zip                    # nya → zip
nya convert -source-password secret enc.zip out.nya  # encrypted input requires flag (no prompt)
nya manifest add GamePack.nya                      # upsert embedded download index
nya manifest del GamePack.nya                      # remove embedded download index
nya manifest export -o GamePack.nyam --url https://cdn/game.nya GamePack.nya
nya sfx pack.nya -o pack.exe                  # wrap as self-extractor (Go stub; -o anywhere)
nya create -sfx game.bin -level 3 ./GameData/ # create + wrap in one step
nya get --url https://cdn.example.com/pack.nya
nya send pack.nya                             # local HTTP + TryCloudflare → share URL
nya gui pack.nya                              # nyaFM GUI (launches nya-fm if installed)
nya associate                                 # Windows: .nya double-click → nya open
```

### Windows double-click

```bat
nya associate
:: then double-click game.nya → extracts to .\game\
```

See [examples/windows-open](examples/windows-open/README.md).

### Send + get (TryCloudflare)

```bash
nya send novel.txt
# Direct:  https://….trycloudflare.com/novel.txt
# Get:     nya get --url https://….trycloudflare.com/novel.txt.nyam

nya send ./GameData
# Archive: https://….trycloudflare.com/GameData.nya
# Get:     nya get --url https://….trycloudflare.com/GameData.nyam

nya get --url https://xxxx.trycloudflare.com/novel.txt.nyam
```

Index URLs are named after the source (`name.nyam` / `name.nya`), not a fixed `/index.nyam`.
CLI defaults to English (`NYA_LANG=zh` or `LANG=zh_CN` for Chinese).

Options: `-no-tunnel`. If cloudflared is missing, install from https://developers.cloudflare.com/tunnel/downloads/ . Quick Tunnels are ephemeral (Cloudflare ToS).

### Large package distribution (`nya get`)

```bash
nya create -level 9 -solid -fec 20 GamePack.nya ./GameData/   # download index embedded by default
# optional sidecar:
# nya manifest export -o GamePack.nyam --url https://cdn.example.com/GamePack.nya GamePack.nya

nya get --url https://cdn.example.com/GamePack.nya          # download + restore GameData/
nya get -no-extract --url https://cdn.example.com/GamePack.nya  # keep .nya only
nya get -c 16 GamePack.nyam                                 # classic sidecar
nya get --paths "Game/Data/level1.bin" GamePack.nyam        # partial fetch (no auto-extract)
```

See [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md) for the `.nyam` manifest schema.

### Unified repair (`nya repair`)

One command repairs by **magic bytes** (extension can be wrong or missing):

```bash
nya repair damaged.nya              # NYA FEC repair
nya repair corrupted.dat            # ZIP if content is PK… (even with wrong ext)
nya repair broken.rar fixed.rar     # RAR structure rebuild (RAR4/RAR5 store blocks)
```

7z is not supported for repair (no recovery record). Use `nya convert` if 7z can still extract.

### Convert archives (zip / 7z / rar / nya ↔)

**`nya convert`** is a file-tree hub: unpack any supported archive, repack as another.
Optional FEC when the output is `.nya`.

```bash
nya convert game.zip game.nya                      # zip → nya
nya convert -fec 30 archive.7z archive.nya         # 7z via p7zip (7z on PATH)
nya convert -source-password secret old.rar new.nya  # encrypted input (required; no prompt)
nya convert archive.nya out.zip                    # nya → zip
nya convert -password lock plain.nya locked.nya    # encrypt *output* .nya
nya convert -level 9 -solid -fec 10 bundle.zip bundle.nya
```

**Password policy (no interactive prompt):**

| Flag | Meaning |
| --- | --- |
| `-source-password` | Unlock encrypted **input** (zip/7z/rar/nya). **Required** if input is encrypted; omit → error. |
| `-password` | Encrypt **output** (`.nya`, or zip/7z via 7z). Optional. |

Aliases: `nya import`, `nya export`, `nya repack`. Formats: **zip** (pure Go); **7z, rar, tar.\*** require
[7-Zip](https://www.7-zip.org/) / `p7zip-full`. Paths are stored as **UTF-8** (中文 filenames
roundtrip correctly). See [docs/BENCHMARK-FEC.md](docs/BENCHMARK-FEC.md) for FEC vs WinRAR/7z.

Levels run 0 to 9, the way 7-Zip and WinRAR present them:

| level | name | codec |
| ---: | --- | --- |
| 0 | store | none |
| 1–2 | fastest | Zstandard |
| 3–4 | fast | Zstandard |
| 5–6 | normal (default) | LZMA2 |
| 7–8 | good | LZMA2, larger window and deeper search |
| 9 | best | LZMA2, maximum window and search |

`create` also accepts `-solid` to compress every file as a single stream,
`-codec` to override the level's choice, `-password` to encrypt the payload,
`-workers` to cap concurrency on **create** and **extract** (parallel per-chunk
decompress when `ChunkCount > 1`), **`-no-embed`** to skip the default
embedded download index (needed for single-URL `nya get`), and **`-dict`**
to embed a zstd dictionary for text-heavy solid packs (levels 1–4).
Solid mode also **auto-derives** a dictionary when it helps — see [docs/SOLID-DICT.md](docs/SOLID-DICT.md).

## Library

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

	// 10% recovery data, best compression, non-solid.
	w := nya.NewWriterOpts(f, 10, nya.LevelBest, false)
	if err := w.AddFile("./project"); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
}
```

Reading back:

```go
r, err := nya.Open("backup.nya")
if err != nil {
	panic(err)
}
if !r.Verify() {
	// the payload no longer matches its digests; try nya.Repair
}
if err := r.Extract("./restored"); err != nil {
	panic(err)
}
```

The package never writes to standard output. Set `Reader.OnEntry` for
extraction progress, or the package level `nya.Log` for messages from
`Repair` and the recovery volume helpers.

The codecs are usable on their own: `ZstdCompress`, `DecompressZstd`,
`Lzma2Compress`, `XzCompress`, `Blake3Sum256`, and the BCJ and delta filters
are all exported.

## Where it stands

Percentages are compressed size relative to the input, so lower is better.
Measured on one machine against the reference archivers (Aug 2026 cloud agent;
regenerate locally — numbers vary by CPU). Treat columns as relative, not absolute.

| corpus | size | nya (level 9) | xz -9 | 7z -mx9 | zstd -19 |
| --- | ---: | ---: | ---: | ---: | ---: |
| structured text | 3391192 | 3.62% (201ms) | 4.24% (1.097s) | 4.18% (716ms) | 7.49% (2.098s) |
| markdown | 20786 | 4.05% (1ms) | 3.71% (6ms) | 4.32% (7ms) | 6.69% (9ms) |
| ELF binary | 48000 | 100.01% (8ms) | 100.12% (44ms) | 100.26% (6ms) | 100.03% (4ms) |
| 17 MB ELF | 17825792 | 100.00% (761ms) | 100.01% (3.391s) | 100.01% (676ms) | 100.00% (1.59s) |
| 120-file tree, solid | 1082080 | 31.62% (911ms) | 30.42% (110ms) | 30.92% (24ms) | 30.37% (31ms) |


**Level-9 parser / solid order (same corpora, regenerate with `NYA_BENCH_WRITE=1 go test -run TestREADMEBenchmarkSuite -timeout 60m ./...`):**

| corpus | variant | ratio | time |
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

Details: [docs/BENCHMARK-COMPRESS.md](docs/BENCHMARK-COMPRESS.md).

### House LZMA2 vs 7-Zip (`github.com/nyarime/compress` v0.2.6)

Single-stream level-9 optimal + dual-encode guard (`compress/lzma2` harness):

| corpus | nya% | 7z -mx9% | gap |
| --- | ---: | ---: | ---: |
| structured text (synthetic) | 0.9 | 1.6 | −0.7pp |
| pseudo_enwik (synthetic) | 0.7 | 1.1 | −0.5pp |
| mixed JSON / log lines | **4.8** | 5.0 | **−0.1pp** |
| Silesia dickens (1 MiB slice) | **30.2** | 29.6 | **+0.6pp** |

Regenerate: `go test -C $(go env GOPATH)/pkg/mod/github.com/nyarime/compress@v0.2.6/lzma2 -run TestBenchVs7zCorpora -timeout 15m` or clone [nyarime/compress](https://github.com/nyarime/compress).

### Solid archive vs 7-Zip (mixed 36-file corpus)

`TestSolidArchiveVs7z` — NYA level-9 solid vs `7z -mx9 -m0=lzma2 -ms=on`:

| | NYA | 7z | gap |
| --- | ---: | ---: | ---: |
| compressed / raw | **8.75%** | 8.44% | **+0.32pp** |

Solid writer applies **extension grouping**, **text-like-first sorting**, and
level-9 LZMA2. Gap improved from ~+1.79pp (text sort only) with BCJ
whole-stream gate tuning. Regenerate: `go test -run TestSolidArchiveVs7z -timeout 10m -v ./...`


Close on **structured text** (nya greedy 3.62% vs xz 4.24% / 7z 4.18% on the
corpus above). **Solid** closes most of the gap on many-file trees (sorted
greedy 30.39% vs xz 30.42% / 7z 30.92% on the 120-file interleaved tree);
encode is still slower than 7-Zip on that workload.

**Optimal parse at level 9** is larger and slower on these corpora (e.g.
structured text 4.61% vs greedy 3.62%; solid sorted 31.52% vs 30.39%). Greedy
+ extension/content-kind sort is the current default; see the A/B table above.

Solid mode applies **extension grouping**, **content-kind sorting** (magic
bytes within each extension group, text-like members first), **optional auto
zstd dictionary** (levels 3–4 on text-heavy packs), and the **greedy LZMA2
parser** at level 9 (with dual-encode optimal guard from `compress` v0.2.3+).
Regenerate numbers: `NYA_BENCH_WRITE=1 go test -run TestREADMEBenchmarkSuite -timeout 60m ./...`
(requires `xz`, `7z`, `zstd` on PATH). Details: [docs/BENCHMARK-COMPRESS.md](docs/BENCHMARK-COMPRESS.md).

Extraction speed is the other half of the trade. LZMA2 decompresses at
18–59 MB/s here against 127–275 MB/s for the zstd path, so an archive that is
read far more often than written is worth writing at level 1–4.

The zstd encoder uses a simpler match finder and fewer entropy coding modes
than the reference implementation, which is why it trails on ratio. Both
codecs are checked against third-party decoders, so `.nya` payloads are
readable by any conformant zstd or xz implementation.

## Compatibility

Archives record `VersionMajor.VersionMinor` in the global header. This
implementation **reads 1.0–1.3**. It **writes**:

| Condition | Minor written |
| --- | ---: |
| Default non-solid file ≤ 4 MiB (no encryption) | **1.1** |
| Encrypted | **1.2** |
| Any multi-chunk entry (`ChunkCount > 1`, default for non-solid &gt; 4 MiB) | **1.3** |

See **[COMPATIBILITY.md](COMPATIBILITY.md)** and **[ROADMAP.md](ROADMAP.md)**.

- **1.3** — multi-chunk non-solid + per-chunk FEC. **Readers ≤ v1.2 cannot open
  these archives at all.** Use `-multi-chunk=false` when you must interop with
  old tools.
- **1.2** — Argon2id KDF + salt in header `Reserved`; `FlagEncrypted` +
  `FlagKDFArgon2id`. Legacy SHA-256(password) archives remain readable.
- **1.1** — zstd frames follow RFC 8878.
- **1.0** — legacy zstd sequence tables; still fully readable.

### Upgrade notes

| From | To | Action |
| --- | --- | --- |
| 1.0 zstd tables | 1.1 | Repack with current `nya create` (automatic RFC 8878) |
| SHA-256 encryption | 1.2 Argon2id | Re-create with `-password` (old archives still extract with password) |
| Need old (≤1.2) readers | stay ≤1.2 | `nya create -multi-chunk=false` (and avoid features that bump minor) |
| Non-solid many-file | solid + sort | `nya create -solid -level 9` on directory trees |

**Default level** is still **5 (LZMA2)**. Many distribute/get scenes are a
better fit for **levels 1–4 (zstd, fast extract)** — decision tracked in
[ROADMAP.md](ROADMAP.md); do not assume the default will stay at 5 forever.

## Known limitations

- **Multi-chunk (v1.3)** is **on by default** for non-solid files &gt; 4 MiB
  (4 MiB raw chunks; 8 MiB above 64 MiB). Solid stays one chunk per entry.
  Design: [docs/SPEC-MULTICHUNK.md](docs/SPEC-MULTICHUNK.md).
- **Custom FSE tables** for zstd sequence codes remain disabled (~1% ratio).
- **Optimal parse** is not enabled by default; benchmarks show greedy + sort
  wins on mixed multi-file solid trees. Enable via library `OptimalParse` when
  tuning for repetitive corpora.
- Public **Silesia / enwik9 / game / firmware** corpus raw data is still
  landing — see [docs/BENCHMARK-CORPUS.md](docs/BENCHMARK-CORPUS.md).

## License

NYA is free software: you may use, modify, and redistribute it under the
terms of the [GNU General Public License v3.0](LICENSE).

**Dual licensing:** If you need to embed or ship NYA in a proprietary /
closed-source product without GPL obligations, contact
**nyarime@naixi.net** for a commercial license.

## Format

- [ROADMAP.md](ROADMAP.md) — **product priorities** (FEC + distribute first)
- [COMPATIBILITY.md](COMPATIBILITY.md) — **v1 LTS policy** (read before adopting)
- [SPEC.md](SPEC.md) — on-disk NYA archive layout
- [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md) — **v1 foundation** (tails, solid, dedup, NyaFS, sessions)
- [docs/SPEC-MULTICHUNK.md](docs/SPEC-MULTICHUNK.md) — **multi-chunk entries** (v1.3)
- [docs/BENCHMARK-MULTICHUNK.md](docs/BENCHMARK-MULTICHUNK.md) — **multi-chunk parallel** (compress workers, FEC repair)
- [docs/BENCHMARK-COMPRESS.md](docs/BENCHMARK-COMPRESS.md) — compression A/B measurements
- [docs/SOLID-DICT.md](docs/SOLID-DICT.md) — solid zstd dictionary (`-dict` + auto-train)
- [docs/COMPRESS-ECOSYSTEM.md](docs/COMPRESS-ECOSYSTEM.md) — `nyarime/compress` split library
- [docs/BENCHMARK-FEC.md](docs/BENCHMARK-FEC.md) — FEC recovery vs 7z/RAR
- [docs/BENCHMARK-CORPUS.md](docs/BENCHMARK-CORPUS.md) — public corpus + raw data plan
- [docs/NOTE-UPX.md](docs/NOTE-UPX.md) — UPX vs archive compression on ELF
- [SPEC-CODECS.md](SPEC-CODECS.md) — **NYA-Zstd & NYA-LZMA2** roles and roadmap
- [SPEC-SFX.md](SPEC-SFX.md) — **self-extracting** archives (`nya` as unified stub)
- [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md) — `.nyam` manifest and `nya get` transport blocks
- [fm/README.md](fm/README.md) — **nyaFM** Rust GUI (open / list / extract)

### Self-extracting archives (7-Zip-style)

```bash
nya create -sfx game.exe -level 3 ./GameData/
nya sfx pack.nya -o pack.exe
./pack.exe                        # extracts beside the executable
```

Double-click / bare run unpacks into the folder that contains the SFX file
(like macOS Archive Utility). Use `-o DIR` to override. **`nya`** is both CLI and
SFX stub: `create -sfx` / `nya sfx` prepend the running `nya` binary, then
append the archive and footer. Do not UPX SFX outputs.

```bash
cargo build -p nya-fm --release
./target/release/nya-fm pack.nya   # GUI: list + extract
```
