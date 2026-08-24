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
| Compression | Zstandard (RFC 8878) by default, or LZMA2 for the best ratio |
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
nya create -level 9 backup.nya ./project     # create
nya create -best -fec 30 backup.nya ./data   # LZMA2 plus 30% recovery data
nya list backup.nya                          # inspect
nya extract backup.nya ./restored            # extract
nya verify backup.nya                        # check stored digests
nya info backup.nya                          # header details
nya repair damaged.nya fixed.nya             # rebuild using the parity data
```

`create` also accepts `-solid` to compress every file as a single stream,
`-password` to encrypt the payload and `-workers` to cap concurrency.

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

## Compression

Measured on this machine at level 9; percentages are compressed size relative
to the input, so lower is better. `ref zstd` is
`github.com/klauspost/compress/zstd` at its default level, included as a
yardstick.

| corpus | size | nya zstd | nya lzma2 | ref zstd |
| --- | ---: | ---: | ---: | ---: |
| structured text | 819412 | 35.7% (22ms) | 9.4% (8ms) | 11.7% (4ms) |
| markdown | 39359 | 64.2% (2ms) | 49.1% (2ms) | 54.9% (1ms) |
| ELF binary | 48072 | 44.4% (2ms) | 35.5% (2ms) | 39.5% (<1ms) |
| exact repeats | 192000 | 0.0% (1ms) | 0.2% (1ms) | 0.0% (<1ms) |
| random | 200000 | 100.0% (4ms) | 100.0% (18ms) | 100.0% (<1ms) |

The LZMA2 encoder is the strong one: it beats the reference zstd encoder on
every corpus above except pure repetition, and it is what `-best` selects.
The zstd encoder uses a simpler match finder and a smaller set of entropy
coding modes, so it trades ratio for a faster decode path. If archive size
matters more than decompression speed, use `-best`.

Frames from the zstd encoder are checked against a third-party decoder, so
`.nya` payloads are readable by any conformant zstd implementation.

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
