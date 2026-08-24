# nya

NYA (Nyarime Archive) is an archive format that pairs ordinary compression
with forward error correction, so a damaged archive can often still be read.
This repository holds the format specification, the reference implementation
and the `nya` command line tool.

Everything is pure Go with no cgo and only two dependencies. The compressors,
the BLAKE3 implementation and the container are all written from scratch;
`github.com/nyarime/gofec` supplies the RaptorQ and LDPC codes and
`golang.org/x/sys` the extended attribute syscalls.

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
```

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

Archives record a format version in their header. This implementation writes
version 1.1 and reads both:

- **1.1** — zstd frames follow RFC 8878.
- **1.0** — written by Nyarc before the format was split out. Its zstd
  encoder used sequence code tables that did not match RFC 8878 Tables 5 and
  6, so those frames are only readable with the legacy tables. `Open` selects
  them automatically from the header version; no action is needed to read old
  archives. Repack an archive to move it to 1.1.

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

`SPEC.md` documents the on-disk layout field by field.
