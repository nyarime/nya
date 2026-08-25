# NYA v1 compatibility policy

This document is the long-term contract for the `.nya` container format.
Read it before depending on NYA in production tools, CDNs, or firmware pipelines.

## One container, one extension

| Name | Role | Stable? |
| --- | --- | --- |
| **`.nya`** | The only binary archive container | **Yes — NYA v1 LTS** |
| **`.nyam`** | Optional JSON download manifest (sidecar) | Yes — separate schema, not a rival format |
| **`.nyam.state`** | Local resume file for `nya-get` | Not published; not part of NYA |

There is no `.nyax`, `.nyar`, or second archive extension planned. New
capabilities extend **NYA v1** through header flags, reserved bytes, and minor
version bumps. A incompatible redesign would require **`VersionMajor = 2`** and
a new magic — not a silent fork.

## Version numbering

```
VersionMajor.VersionMinor  (global header bytes 8–11)
```

| Field | Meaning |
| --- | --- |
| **Major** | Container layout generation. Readers **must reject** `Major > 1`. |
| **Minor** | Feature / codec profile within major 1. Readers **must read all minors ≤ current**. |

### Minor versions (major 1)

| Minor | Writer | Reader | Notes |
| ---: | --- | --- | --- |
| **0** | Legacy (frozen) | Required forever | Pre-RFC8878 zstd sequence tables; self-consistent old archives |
| **1** | **Current default** | Required | RFC 8878 zstd; Argon2id encryption (minor 2) |
| **2** | Encrypted output | Required | **Argon2id** KDF: 16-byte salt + memory/time/thread params in header `Reserved[0:25]`; flags `FlagEncrypted` + `FlagKDFArgon2id`. Payload remains AES-256-GCM. Legacy archives that used bare SHA-256(password) with no header flag remain readable when a password is supplied. |
| **3** | Multi-chunk non-solid | Required | `ChunkCount > 1` for files > 4 MiB (non-solid); per-chunk FEC; `-multi-chunk` / `-chunk-size` on create. **Impact:** readers that only accept minor ≤ 2 **reject the whole archive** (no partial extract). Escape: `-multi-chunk=false`. Reference reader accepts through minor 3; older archives (`ChunkCount = 1`) remain readable. |

**Policy:** bump **minor** for additive, backward-compatible changes (new flags,
optional tail sections). Bump **major** only for breaking layout changes.

## Backward compatibility guarantees

1. **Read forever:** any `.nya` with `VersionMajor = 1` that this package ever
   wrote remains extractable in future releases.
2. **Write forward:** new writers may use new minor versions and flags; old
   readers may reject unknown minors but must not mis-read known ones.
3. **Sidecars optional:** `.nyam` never replaces `.nya`; download metadata may
   live in JSON today or in an optional embedded index tomorrow — still one
   `.nya` file.
4. **Reserved space:** the 40-byte header reserved region and unset flag bits
   are for extension; do not use for unrelated formats.

## v1 foundation vs v1.x features

**Foundation (freeze before first external adopter):** container topology, tail
chain location, header reserved layout, extension skip rules, CompressionID
semantics, profile flag bits. Defined in:

- [SPEC.md](SPEC.md) — core layout
- [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md) — solid groups, dedup, NyaFS, sessions
- [SPEC-CODECS.md](SPEC-CODECS.md) — NYA-Zstd & NYA-LZMA2 roles

**v1.x (iterate after foundation is fixed):** writer implementations for tails,
solid grouping heuristics, dedup, embedded download index, NyaFS mount tools,
encoder improvements within the same CompressionID bitstreams.

No external users yet means the foundation can still be adjusted **only until
the v1 freeze announcement**; after that, byte layouts in the documents above
are LTS.

## Extension rules (Win32-style)

Allowed in **v1.x** without new file extension:

- New **flag bits** (currently bit 4 `HasDownloadIndex` is reserved)
- Populate **Blake3** global header field (currently zero)
- **Tail chain** records ([SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md))
- **DirEntry v3** (solid group, dedup, profile entry flags)
- **Multi-chunk entries** (`ChunkCount > 1`) with minor bump
- Stronger **encryption** with KDF metadata in reserved/header v2 fields
- **Embedded download index** (tail type `0x0001`, alternative to `.nyam`)
- **NYA-Zstd / NYA-LZMA2 encoder upgrades** (same CompressionID; see [SPEC-CODECS.md](SPEC-CODECS.md))

Requires **major 2** (new magic or breaking directory layout):

- Changing chunk header size
- Moving recovery section before central directory without dual-read path
- Reassigning meaning of allocated header reserved bytes
- Removing support for minor 0 archives (if ever published as LTS)

## Related files

| File | Scope |
| --- | --- |
| [SPEC.md](SPEC.md) | On-disk `.nya` layout |
| [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md) | **v1 extension foundation** (tails, profiles) |
| [docs/SPEC-MULTICHUNK.md](docs/SPEC-MULTICHUNK.md) | Multi-chunk entry design (**v1.3**, default on) |
| [SPEC-CODECS.md](SPEC-CODECS.md) | **NYA-Zstd & NYA-LZMA2** policy |
| [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md) | `.nyam` manifest (distribution only) |
| [ROADMAP.md](ROADMAP.md) | Product priorities (FEC + distribute first) |

## Application profiles (informative)

Tools may use `.nya` for different jobs without new extensions:

- **Archive profile:** `nya create` / `extract` — general backup and game packs
- **Distribution profile:** `nya manifest` + `nya-get` — CDN download
- **Database profile:** (e.g. Nyarc firmware analysis) — custom entry paths
  such as `_nyarc/...` inside the same `.nya` container

Profiles share the **same v1 container**; only entry naming and tooling differ.
Profile bits and NyaFS conventions are in [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md).

## Codec policy (summary)

| Codec | CompressionID | Role |
| --- | ---: | --- |
| **NYA-Zstd** | 1 | House codec — speed, levels 1–4, RFC 8878 (candidate for `LevelDefault`) |
| **NYA-LZMA2** | 6 | Best ratio — levels 5–9 today; 7–9 for `--best` |

CLI `LevelDefault` is still **5 (LZMA2)** until corpus decode benches land; see
[ROADMAP.md](ROADMAP.md). Encoder improvements ship in software releases without
changing IDs — [SPEC-CODECS.md](SPEC-CODECS.md).

## v1.3 multi-chunk: old-reader impact

Default `nya create` enables multi-chunk for non-solid files larger than 4 MiB,
so many real game/firmware/CDN archives will be written as **minor 3**.

| Reader | Behavior on minor 3 |
| --- | --- |
| Current `nya` (this repo) | Full extract / repair / list |
| Tool built against ≤ v1.2 only | **Hard reject** (`ErrUnsupported`) — cannot list or extract |
| Need interop with old readers | Create with `-multi-chunk=false` |

Do not ship minor-3 archives into pipelines that still pin an old binary unless
you control the reader upgrade. Product context: [ROADMAP.md](ROADMAP.md).
