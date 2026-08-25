# nya

[English](README.md) | [简体中文](README_zh.md)

NYA (Nyarime Archive) is an archive format that pairs ordinary compression
with forward error correction, so a damaged archive can often still be read.
This repository is the **canonical home** for the format specification,
reference implementation, and the `nya` CLI (`get` / `send` / `gui` / `sfx`, …). The format is
general-purpose (backups, game packs, CDN distribution); firmware or database
profiles can embed the same container without changing the on-disk layout.

Everything is pure Go with no cgo and only two dependencies. The compressors
(**NYA-Zstd**, **NYA-LZMA2**), the BLAKE3 implementation and the container are
all written from scratch; `github.com/nyarime/gofec` supplies the RaptorQ and
LDPC codes and `golang.org/x/sys` the extended attribute syscalls.

**NYA-Zstd** is the house codec (RFC 8878, speed + embed). **NYA-LZMA2** is
the `--best` lane (standard raw LZMA2 bitstream). Both iterate in software
without changing on-disk IDs — see [SPEC-CODECS.md](SPEC-CODECS.md).

## What is in an archive

| Layer | What it does |
| --- | --- |
| Compression | LZMA2 at levels 5–9, Zstandard (RFC 8878) at 1-4, stored at 0 |
| Pre-filters | BCJ branch conversion for x86, ARM, AArch64 and MIPS binaries; delta filter |
| Integrity | BLAKE3-256 over every chunk, with AVX2/AVX-512/SSE2/NEON assembly paths |
| Recovery | RaptorQ parity symbols, sized as a percentage of the payload |
| Encryption | Optional AES-256-GCM over the compressed payload |
| Metadata | Unix mode, owner, timestamps, symlinks, hardlinks, device nodes, FIFOs, xattrs |

Unlike the 10% ceiling that RAR recovery records impose, the amount of
recovery data is a free parameter: `-fec 50` stores half the payload size
again in parity symbols.

## Install

### Linux / macOS

Installs into `~/.local` by default (`nya` → `~/.local/bin`).

```bash
curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.sh | bash
```

```bash
# optional
bash install.sh --prefix /usr/local          # custom prefix
bash install.sh --version 0.1.6              # pin a release
```

### Windows

Installs into `%LOCALAPPDATA%\Programs\NYA`, adds user PATH, associates `.nya` → `nya open`.

```powershell
irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.ps1 | iex
```

```powershell
# optional
install.ps1 -Prefix "D:\Tools\NYA"
install.ps1 -Version 0.1.6
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

Primary CLI is **`nya`**. Download is **`nya get`** (the `nya-get` binary is only a
compatibility shim). `nya-sfx-stub` stays a separate binary for SFX wrapping.

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
nya convert legacy.zip repaired.nya                # unpack zip/7z/rar → NYA (+FEC, +download index)
nya convert -fec 20 old.rar backup.nya             # WinRAR-style recovery, but configurable
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

Options: `-no-tunnel`, `-no-fetch-cloudflared`. Quick Tunnels are ephemeral (Cloudflare ToS).

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

### Convert legacy archives (zip / 7z / rar → NYA)

Use **`nya convert`** to unpack a damaged or legacy archive and repack it as `.nya` with
optional FEC — something zip and 7z cannot do natively, and WinRAR only partially
addresses with fixed-size recovery records.

```bash
nya convert game.zip game.nya                      # zip via built-in reader
nya convert -fec 30 archive.7z archive.nya         # 7z via p7zip (7z on PATH)
nya convert -source-password secret old.rar new.nya  # encrypted RAR input
nya convert -level 9 -solid -fec 10 bundle.zip bundle.nya
```

Aliases: `nya import`, `nya repack`. Formats: **zip** (pure Go); **7z, rar, tar.\*** require
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
decompress when `ChunkCount > 1`), and **`-no-embed`** to skip the default
embedded download index (needed for single-URL `nya get`).

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


Close on **structured text** (nya greedy 3.62% vs xz 4.24% / 7z 4.18% on the
corpus above). **Solid** closes most of the gap on many-file trees (sorted
greedy 30.39% vs xz 30.42% / 7z 30.92% on the 120-file interleaved tree);
encode is still slower than 7-Zip on that workload.

**Optimal parse at level 9** is larger and slower on these corpora (e.g.
structured text 4.61% vs greedy 3.62%; solid sorted 31.52% vs 30.39%). Greedy
+ extension/content-kind sort is the current default; see the A/B table above.

Solid mode applies **extension grouping**, **content-kind sorting** (magic
bytes within each extension group), and the **greedy LZMA2 parser**.
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
implementation **writes 1.1** (or **1.2** when encrypted) and **reads 1.0 through 1.2**.

See **[COMPATIBILITY.md](COMPATIBILITY.md)** for the long-term policy (one
`.nya` container, `.nyam` sidecar only, no `.nyax`).

- **1.2** — Argon2id KDF + salt in header `Reserved`; `FlagEncrypted` +
  `FlagKDFArgon2id`. Legacy SHA-256(password) archives remain readable.
- **1.1** — zstd frames follow RFC 8878 (current writer for non-encrypted).
- **1.0** — legacy zstd sequence tables; still fully readable. Repack to upgrade.

### Upgrade notes

| From | To | Action |
| --- | --- | --- |
| 1.0 zstd tables | 1.1 | Repack with current `nya create` (automatic RFC 8878) |
| SHA-256 encryption | 1.2 Argon2id | Re-create with `-password` (old archives still extract with password) |
| Non-solid many-file | solid + sort | `nya create -solid -level 9` on directory trees |

## Known limitations

- **Multi-chunk entries** (v1.3, `VersionMinor = 3`): non-solid files larger
  than 4 MiB are split into independent chunks (default 4 MiB raw, 8 MiB above
  64 MiB). Solid archives remain one chunk per entry. Disable with
  `-multi-chunk=false`. Old readers (≤ v1.2) cannot read minor 3 archives.
  Design: [docs/SPEC-MULTICHUNK.md](docs/SPEC-MULTICHUNK.md).
- **Custom FSE tables** for zstd sequence codes remain disabled (~1% ratio).
- **Optimal parse** is not enabled by default; benchmarks show greedy + sort
  wins on mixed multi-file solid trees. Enable via library `OptimalParse` when
  tuning for repetitive corpora.
- **7-Zip solid ratio** on very large mixed trees can still win on encode speed;
  NYA closes most of the gap on ratio with sort + optimal parse (see benchmark doc).

## License

NYA is free software: you may use, modify, and redistribute it under the
terms of the [GNU General Public License v3.0](LICENSE).

**Dual licensing:** If you need to embed or ship NYA in a proprietary /
closed-source product without GPL obligations, contact
**nyarime@naixi.net** for a commercial license.

## Format

- [COMPATIBILITY.md](COMPATIBILITY.md) — **v1 LTS policy** (read before adopting)
- [SPEC.md](SPEC.md) — on-disk NYA archive layout
- [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md) — **v1 foundation** (tails, solid, dedup, NyaFS, sessions)
- [docs/SPEC-MULTICHUNK.md](docs/SPEC-MULTICHUNK.md) — **multi-chunk entries** (v1.3)
- [docs/BENCHMARK-MULTICHUNK.md](docs/BENCHMARK-MULTICHUNK.md) — **multi-chunk parallel** (compress workers, FEC repair)
- [docs/BENCHMARK-COMPRESS.md](docs/BENCHMARK-COMPRESS.md) — compression A/B measurements
- [SPEC-CODECS.md](SPEC-CODECS.md) — **NYA-Zstd & NYA-LZMA2** roles and roadmap
- [SPEC-SFX.md](SPEC-SFX.md) — **self-extracting** stub + footer (Go reference stub)
- [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md) — `.nyam` manifest and `nya get` transport blocks
- [fm/README.md](fm/README.md) — **nyaFM** Rust GUI (open / list / extract)

### Self-extracting archives (7-Zip-style)

```bash
go build -o sfx/stubs/nya-sfx-stub_$(go env GOOS)_$(go env GOARCH) ./cmd/nya-sfx-stub

nya create -sfx game.exe -level 3 ./GameData/
nya sfx pack.nya -o pack.exe
./pack.exe                        # extracts beside the executable
```

Double-click / bare run unpacks into the folder that contains the SFX file
(like macOS Archive Utility). Use `-o DIR` to override. The reference stub is
**Go** (`cmd/nya-sfx-stub`) so NYA-Zstd / solid / multi-chunk all work; `nya`
only concatenates stub + archive + footer.

```bash
cargo build -p nya-fm --release
./target/release/nya-fm pack.nya   # GUI: list + extract
```
