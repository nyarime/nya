# NYA v1 codecs (NYA-Zstd & NYA-LZMA2)

Version **1.0** (spec document). Companion to [SPEC.md](SPEC.md).

This document defines how **self-developed compressors** fit the v1
foundation. Codec *algorithms* iterate in software releases; **CompressionID**
values and on-disk payload formats defined here stay stable for `VersionMajor =
1`.

## Design principle

| Stable (v1 foundation) | Iterated (v1.x releases) |
| --- | --- |
| `CompressionID` enum in central directory | Encoder search depth, match finder |
| Payload format: RFC 8878 zstd frames; raw LZMA2 streams | BT4 match finder, optimal parser |
| Level → codec mapping in tools (documented defaults) | SIMD match extend, solid/dedup integration |
| Minor version for zstd *container* quirks (0 vs 1) | Ratio/speed tuning within same bitstream |

Improving an encoder MUST NOT change the on-disk payload format for a given
CompressionID. If a new bitstream is ever required, assign a **new
CompressionID** (or bump `VersionMajor`).

## Codec roster

| ID | Name | Payload on disk | NYA levels | Role |
| ---: | --- | --- | ---: | --- |
| 0 | Store | raw bytes | 0 | No compression |
| 1 | **NYA-Zstd** | [RFC 8878](https://www.rfc-editor.org/rfc/rfc8878) zstd frame(s) | 1–4 (planned default: 3–5) | **House codec** — speed, embed, distribution |
| 5 | NYA-Zstd + dictionary | RFC 8878 with dictionary prefix | reserved | Solid / rootfs shared dict (future) |
| 6 | **NYA-LZMA2** | Raw LZMA2 stream (no XZ container) | 5–9 | **Best ratio** — `--best` / archival |
| 2–4 | S2, Brotli, LZ4 | — | — | Reserved IDs; not implemented |

Reference implementation package: `github.com/nyarime/nya` (pure Go, no cgo).

Third-party tools MAY decode NYA payloads with any conformant **zstd** or
**xz/lzma2** decoder when frames/streams follow the standards below.

## NYA-Zstd (CompressionID 1)

### On-disk format

Each 512 KiB logical block is stored as:

```
uint32 blockLength
blockLength bytes of one or more zstd frames
```

Frames MUST conform to **RFC 8878** when `VersionMinor >= 1`.

### Container minor versions

| VersionMinor | Zstd frames | Interop |
| ---: | --- | --- |
| 0 | Legacy NYA-specific sequence/Huffman tables | Self-consistent only; not interop |
| 1 | RFC 8878 | Decodable by official zstd, `zstd -d`, etc. |

**v1 freeze policy:** new public writers emit **minor 1** only. Minor 0 support
may remain in readers for pre-release archives; it is not part of the external
LTS promise unless explicitly published before v1 freeze (see
[COMPATIBILITY.md](COMPATIBILITY.md)).

### Reference encoder strategy (current)

The NYA-Zstd encoder is a **from-scratch** implementation tuned for:

- Valid RFC 8878 output (minor 1)
- Pure Go, no cgo, embeddable in firmware/tools
- Parallel block compression for large inputs
- Optional dictionary window (`ZstdCompressWithDict`) for future solid/dedup

It intentionally trades some ratio for simplicity versus `libzstd`: simpler
match finder, predefined FSE tables for sequences, limited custom entropy modes.

### Roadmap (same CompressionID, same RFC output)

Improvements ship in **software versions**, not format bumps:

1. Stronger match finder (lazy matching, wider window at high levels)
2. Dictionary mode wired to solid groups and CompressionID 5
3. SIMD literal/copy paths (already partially present in decompress)
4. Custom FSE tables re-enabled once cross-decoder conformance tests pass
5. **Default level migration:** move `LevelDefault` from LZMA2 (5) to NYA-Zstd
   (e.g. level 3–5) — product change, not a format change

**NYA-Zstd is the long-term default face of NYA.** LZMA2 remains the optional
deep compression lane.

## NYA-LZMA2 (CompressionID 6)

### On-disk format

Same block wrapper as zstd:

```
uint32 blockLength
blockLength bytes of raw LZMA2 stream
```

The payload is **raw LZMA2** (LZMA2 chunk stream with `0x00` end marker), **not**
wrapped in an XZ container. Decompression is compatible with the LZMA2 layer
of `xz` and 7-Zip.

XZ container output (`XzCompress`) exists for standalone benchmarking and
cross-checking against `xz -9`; NYA archives store **raw LZMA2** only.

### Reference encoder strategy (current)

NYA-LZMA2 is a **from-scratch** encoder:

- Range coder + LZMA state machine
- Hash-chain match finder with price-based parsing (not length-greedy)
- One-step lookahead (not full optimal parse)
- Parallel **segments** with dictionary continuity across LZMA2 chunks inside
  a segment

Decompression uses the in-tree pure Go decoder (`xz_decompress.go` / LZMA2
layer).

### Roadmap (same LZMA2 bitstream)

Improvements do **not** change the payload format:

1. **BT4 match finder** (largest single-file ratio gain vs hash chain)
2. **Full optimal parser** (DP over lookahead window)
3. SIMD match extension (allows deeper search at same CPU cost)
4. Solid-group integration ([SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md)) —
   grouping by extension/similarity before compressing
5. Content dedup before compression (identical files → one chunk)

Goal: narrow the gap to `xz -9` / `7z -mx9` on level 9 **without** inventing
"NYA-LZMA3". Beating 7-Zip solid+dedup is an **archive-profile** problem as
much as a codec problem.

## Level table (normative defaults)

Tools SHOULD map CLI `-level` as follows unless `-codec` overrides:

| Level | Label | CompressionID | Notes |
| ---: | --- | ---: | --- |
| 0 | store | 0 | |
| 1 | fastest | 1 | NYA-Zstd, low effort |
| 2 | fast | 1 | NYA-Zstd |
| 3 | fast | 1 | NYA-Zstd |
| 4 | normal-fast | 1 | NYA-Zstd, high effort |
| 5 | normal | 6 → **1** | **Planned:** switch default to NYA-Zstd; until then LZMA2 4 MiB dict |
| 6 | normal+ | 6 | NYA-LZMA2 8 MiB dict |
| 7 | good | 6 | NYA-LZMA2 16 MiB dict |
| 8 | good+ | 6 | NYA-LZMA2 32 MiB dict |
| 9 | best | 6 | NYA-LZMA2 64 MiB dict, deepest search |

The level 5 default migration is tracked as a **product release** change; archives
already written remain valid via per-entry `CompressionID`.

## Pre-filters (BCJ / delta)

BCJ filters (x86, ARM, AArch64, MIPS) and the delta filter apply **before**
compression. Filter ID is stored per entry (`BCJFilter` in central directory);
they are independent of CompressionID.

Solid mode may compress with and without a filter and keep the smaller result
(current behaviour).

## Verification requirements

Reference implementation MUST maintain:

| Check | Purpose |
| --- | --- |
| Round-trip tests per CompressionID | Correctness |
| `zstd` CLI decode of minor 1 payloads | NYA-Zstd interop |
| `xz` / liblzma decode of LZMA2 payloads | NYA-LZMA2 interop |
| Conformance tests (`zstd_conformance_test.go`) | RFC 8878 guardrails |

Encoder upgrades MUST NOT break these checks.

## When to add a new CompressionID

Add a new ID (e.g. 5 for zstd+dict, or 7 for a future codec) when:

- The payload bitstream is not decodable by existing ID rules, **and**
- You want old readers to reject the entry clearly rather than mis-decode.

Do **not** add a new ID for encoder-only improvements within zstd or LZMA2.

## Summary

- **NYA-Zstd:** house codec, RFC 8878, speed + embed + distribution; iterate
  encoder freely; plan to own default levels 1–5.
- **NYA-LZMA2:** standard raw LZMA2 bitstream, levels 6–9 / `--best`; iterate
  toward xz/7z ratio without changing on-disk layout.
- **Foundation:** CompressionID + block wrapper are frozen; solid groups, dedup,
  and dictionaries compose at the archive layer ([SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md)).

## Version history

### 1.0

Initial codec policy for NYA v1 foundation freeze.
