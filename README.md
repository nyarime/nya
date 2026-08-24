# nya

NYA (Nyarime Archive) is an archive format that pairs ordinary compression
with forward error correction, so a damaged archive can often still be read.
This repository is the **canonical home** for the format specification,
reference implementation, `nya` CLI, and `nya-get` downloader.

[Nyarc](https://github.com/nyarime/Nyarc) is being rebuilt as a firmware
analysis tool (BinWalk / IDA-like); it may use `.nya` as an internal analysis
database format, but all format development happens here.

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

```bash
go install github.com/nyarime/nya/cmd/nya@latest
```

## Command line

```bash
nya create backup.nya ./project                    # create at the default level
nya create -level 9 -solid backup.nya ./project    # smallest
nya create -level 1 backup.nya ./project           # fastest
nya create -fec 30 backup.nya ./data               # add 30% recovery data
nya list backup.nya                                # inspect
nya extract backup.nya ./restored                  # extract
nya verify backup.nya                              # check stored digests
nya info backup.nya                                # header details, including codec
nya repair damaged.nya fixed.nya                   # rebuild using the parity data
nya manifest GamePack.nya -o GamePack.nyam --url https://cdn/game.nya  # download index
nya sfx pack.nya -o pack.bin                  # wrap as self-extractor (Rust stub)
nya create -sfx game.bin -level 3 ./GameData/ # create + wrap in one step
```

### Large package distribution (`nya-get`)

```bash
nya create -level 9 -solid -fec 20 GamePack.nya ./GameData/
nya manifest GamePack.nya -o GamePack.nyam --url https://cdn.example.com/GamePack.nya

go install github.com/nyarime/nya/cmd/nya-get@latest
nya-get -c 16 GamePack.nyam    # parallel Range download + resume
```

See [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md) for the `.nyam` manifest schema.

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
`-codec` to override the level's choice, `-password` to encrypt the payload
and `-workers` to cap concurrency.

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
Measured on one machine against the reference archivers; treat the columns as
relative to each other rather than as absolute numbers.

| corpus | size | nya (level 9) | xz -9 | 7z -mx9 | zstd -19 |
| --- | ---: | ---: | ---: | ---: | ---: |
| structured text | 3295292 | 6.12% (558ms) | 5.51% (1386ms) | 5.61% (731ms) | 8.96% (2365ms) |
| markdown | 39359 | 52.94% | 46.28% | 46.50% | 47.93% |
| ELF binary | 48072 | 33.47% | 32.75% | 31.16% | 35.03% |
| 17 MB ELF | 17438777 | 49.38% (2318ms) | 46.99% (4518ms) | 45.67% (1799ms) | 49.41% (3415ms) |
| 120-file tree, solid | 1105916 | 24.82% (726ms) | 22.50% (316ms) | 22.26% (111ms) | 23.04% (265ms) |

Close on single files, still behind on many-file archives. Two things account
for the remainder: the parser is greedy with one position of lookahead rather
than a full optimal parse, and files are stored in directory order instead of
being grouped by similarity the way 7-Zip groups them.

Extraction speed is the other half of the trade. LZMA2 decompresses at
18–59 MB/s here against 127–275 MB/s for the zstd path, so an archive that is
read far more often than written is worth writing at level 1–4.

The zstd encoder uses a simpler match finder and fewer entropy coding modes
than the reference implementation, which is why it trails on ratio. Both
codecs are checked against third-party decoders, so `.nya` payloads are
readable by any conformant zstd or xz implementation.

## Compatibility

Archives record `VersionMajor.VersionMinor` in the global header. This
implementation **writes 1.1** and **reads 1.0 and 1.1**.

See **[COMPATIBILITY.md](COMPATIBILITY.md)** for the long-term policy (one
`.nya` container, `.nyam` sidecar only, no `.nyax`).

- **1.1** — zstd frames follow RFC 8878 (current writer).
- **1.0** — legacy zstd sequence tables; still fully readable. Repack to upgrade.

## Known limitations

- Custom (mode 2) FSE tables for sequence codes are disabled. The current
  serialisation round-trips through this package but is rejected by other
  zstd decoders, so it stays off until it is fixed; the cost is roughly 1% of
  ratio.
- Password-based encryption derives its key with a bare SHA-256 of the
  password, because the header carries no salt or KDF parameters. It resists
  casual inspection, not offline brute force. See `SPEC.md`.
- An encrypted archive is not marked as such in its header; the caller has to
  know a password is required.
- The writer emits one chunk per entry, so `ChunkCount` is always 1.
- The LZMA2 parser prices its choices but only looks one position ahead. A
  full optimal parse — dynamic programming over a lookahead window — is what
  stands between this and `xz -9`.
- Solid archives store files in directory order. Grouping by extension and
  similarity first, as 7-Zip does, is most of the remaining gap on
  many-file archives.

## Format

- [COMPATIBILITY.md](COMPATIBILITY.md) — **v1 LTS policy** (read before adopting)
- [SPEC.md](SPEC.md) — on-disk NYA archive layout
- [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md) — **v1 foundation** (tails, solid, dedup, NyaFS, sessions)
- [SPEC-CODECS.md](SPEC-CODECS.md) — **NYA-Zstd & NYA-LZMA2** roles and roadmap
- [SPEC-SFX.md](SPEC-SFX.md) — **self-extracting** stub + footer (Rust)
- [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md) — `.nyam` manifest and `nya-get` transport blocks

### Self-extracting archives (7-Zip-style)

```bash
cd sfx && cargo build --release   # once; ~580 KB stub
cp target/release/nya-sfx-stub stubs/nya-sfx-stub_linux_amd64

nya create -sfx game.bin -level 3 ./GameData/
nya sfx pack.nya -o pack.bin
./pack.bin                        # extracts to current directory
```

The stub is **Rust** (small); the main `nya` tool only concatenates stub + archive + footer.
