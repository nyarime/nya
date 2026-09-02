# NYA v1 foundation freeze

**Effective:** 2026-08-27  
**Scope:** `.nya` container major version **1** (magic, header, central directory,
chunk layout, tail chain rules, CompressionID semantics)

After this announcement, byte layouts documented in the files below are
**LTS** — breaking changes require `VersionMajor = 2` and new magic, not a silent
fork.

## Frozen documents

| Document | What is frozen |
| --- | --- |
| [SPEC.md](../SPEC.md) | Global header, chunk headers, central directory, recovery section |
| [SPEC-EXTENSIONS.md](../SPEC-EXTENSIONS.md) | Tail chain, solid/dedup flags, download index tail `0x0001`, profile bits |
| [SPEC-CODECS.md](../SPEC-CODECS.md) | CompressionID 0–6 meaning; NYA-Zstd / NYA-LZMA2 on-disk bitstreams |
| [SPEC-DOWNLOAD.md](../SPEC-DOWNLOAD.md) | `.nyam` schema v1, transport blocks, resume state JSON |
| [docs/SPEC-MULTICHUNK.md](SPEC-MULTICHUNK.md) | Multi-chunk entries (minor **3**) |
| [COMPATIBILITY.md](../COMPATIBILITY.md) | Minor version policy and reader guarantees |

## What may still change in v1.x (software only)

These ship in **releases** without a new file extension or major bump:

- Encoder quality within the same CompressionID (NYA-Zstd / NYA-LZMA2)
- CLI defaults and profiles (`-profile distribute`, send auto-pack heuristics)
- `nya get` / `nya send` transport behavior (HTTP, resume, progress)
- nyaFM GUI and Rust extract stub capabilities
- Benchmarks and documentation

## What requires v2 (not planned without strong cause)

- Chunk header size changes
- Moving recovery section without dual-read migration
- Reassigning header `Reserved` bytes already used by Argon2id / tail chain
- Dropping read support for minor **0** archives

## Reader support matrix (reference implementation)

| Minor | Must read | Notes |
| ---: | --- | --- |
| 0 | Yes | Legacy zstd tables |
| 1 | Yes | RFC 8878 zstd |
| 2 | Yes | Argon2id encryption |
| 3 | Yes | Multi-chunk non-solid |

Current `nya` release line reads **1.0–1.3**.

## Adopter checklist

See [ADOPTION.md](ADOPTION.md) for install, CI template, distribute profile,
and remaining ecosystem gaps.

Questions about commercial embedding: **nyarime@naixi.net**
