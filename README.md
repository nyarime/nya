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
| Compression | LZMA2 by default, or Zstandard (RFC 8878) for fast extraction |
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
nya create backup.nya ./project                    # create
nya create -codec zstd backup.nya ./project        # trade size for fast extraction
nya create -fec 30 backup.nya ./data               # add 30% recovery data
nya list backup.nya                                # inspect
nya extract backup.nya ./restored                  # extract
nya verify backup.nya                              # check stored digests
nya info backup.nya                                # header details, including codec
nya repair damaged.nya fixed.nya                   # rebuild using the parity data
```

`create` also accepts `-solid` to compress every file as a single stream,
`-password` to encrypt the payload, `-level` for the zstd level and
`-workers` to cap concurrency.

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

	// 10% recovery data, compression level 9, non-solid.
	w := nya.NewWriterOpts(f, 10, 9, false)
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

## Choosing a codec

Percentages are compressed size relative to the input, so lower is better.
Measured on one machine at level 9; treat them as ratios between the columns
rather than absolute numbers. `ref zstd` is
`github.com/klauspost/compress/zstd` at its default level, as a yardstick.

| corpus | size | nya lzma2 | nya zstd | ref zstd |
| --- | ---: | ---: | ---: | ---: |
| structured text | 819412 | **9.4%** | 35.7% | 11.7% |
| markdown | 39359 | **49.1%** | 64.2% | 54.9% |
| ELF binary | 48072 | **35.5%** | 44.4% | 39.5% |
| exact repeats | 192000 | 0.2% | 0.0% | 0.0% |
| random | 200000 | 100.0% | 100.0% | 100.0% |

LZMA2 is the default because it wins on size everywhere that matters, beating
even the reference zstd encoder on every corpus except pure repetition. What
it costs is decompression speed:

| corpus | lzma2 | zstd |
| --- | ---: | ---: |
| structured text | 59 MB/s | 275 MB/s |
| markdown | 18 MB/s | 127 MB/s |
| ELF binary | 23 MB/s | 177 MB/s |

So LZMA2 extracts roughly five to seven times slower. Pass `-codec zstd` when
an archive is read far more often than it is written, or when extraction time
is on a critical path; keep the default when size is what matters.

The zstd encoder here uses a simpler match finder and fewer entropy coding
modes than the reference implementation, which is why it trails on ratio. Its
frames are checked against a third-party decoder, so `.nya` payloads written
with `-codec zstd` are readable by any conformant zstd implementation.

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
- Builds are limited to amd64 and arm64. `github.com/nyarime/gofec` v1.3.0
  declares `xor.Bytes` both in an untagged assembly stub and in a
  `!amd64 && !arm64` fallback, so the two collide on any other architecture.
  A build tag on the stub upstream would lift the restriction.

## Format

`SPEC.md` documents the on-disk layout field by field.
