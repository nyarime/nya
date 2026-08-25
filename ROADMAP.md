# NYA product roadmap

This is the living product plan. Format details stay in `SPEC*.md` and
`COMPATIBILITY.md`; this file answers **why** and **what next**.

## Positioning (read first)

**NYA is not trying to win as “a slightly better 7z / RAR.”**

Those tools already own desktop habits, codecs, and GUI muscle memory. A
pure compression-ratio race is a losing game.

The wedge that is hard to copy and easy to explain:

> **Compress once, ship over unreliable links, survive damage — same file.**

| Story | Why it matters |
| --- | --- |
| **FEC + distribution** | Recovery percent is a free parameter (`-fec N`); Leopard-RS / Hybrid for large payloads; `nya augment` after the fact |
| **Single-URL get** | Embedded download index + `.nyam`; `nya get` / `nya send` for CDN and Quick Tunnel |
| **Multi-chunk (v1.3)** | Parallel compress/extract, per-chunk FEC, path toward partial download |

Target scenes (short-term messaging focus):

1. **Game packs / patch bundles** — large trees, mixed binary+text, CDN edges
2. **Firmware / device images** — bit-rot and flaky flash/USB links
3. **CDN / mirror large files** — byte damage and incomplete transfers
4. **Unreliable links** — mobile, tunnels, cross-border mirrors

Compression quality still matters, but it is **supporting cast**, not the
headline.

---

## Short term (do these first)

### 1. Tell the “FEC + distribution” story clearly

- Lead READMEs and release notes with **survive damage + ship once**, not
  “another archive format.”
- Keep concrete demos for game packs, firmware, CDN, unreliable links.
- Point to measured recovery: [docs/BENCHMARK-FEC.md](docs/BENCHMARK-FEC.md).
- Keep `nya convert` / `nya repair` / `nya get` / `nya send` in the same
  narrative: **in = out**, with optional FEC on the wire format.

**Status:** README + ROADMAP landed (merged #28).

### 2. Real corpus benchmarks + public raw data

Expand beyond synthetic trees. Preferred corpora:

| Corpus | Why |
| --- | --- |
| **Silesia** | Standard archive benchmark set |
| **enwik9** | Large text / wiki dump |
| Large **game asset** trees (textures, packs) | Distribution story |
| **Firmware** images / rootfs tarballs | Bit-rot + FEC story |

Publish:

- Summary tables in [docs/BENCHMARK-COMPRESS.md](docs/BENCHMARK-COMPRESS.md)
  and [docs/BENCHMARK-CORPUS.md](docs/BENCHMARK-CORPUS.md)
- **Raw CSV / JSON** under `docs/bench-data/` (or release artifacts)

Harness: `scripts/bench-silesia.sh` (fetch + level sweep + CSV).

**Status:** script landed; full public CSV still TODO (run when network/time allow).

### 3. Rethink the default compression strategy

Today:

```text
LevelDefault = LevelNormal = 5 → NYA-LZMA2 (small window)
Levels 1–4 → NYA-Zstd (fast decompress)
Levels 7–9 → NYA-LZMA2 (best ratio)
```

**Direction (decided):** bias distribute/get toward **zstd 1–4**; keep LZMA2 for
`-level 7–9` / `--best`. Do **not** flip `LevelDefault` until Silesia/enwik9
decode numbers are in `docs/bench-data/`.

**Status:** direction set; code default unchanged pending benches.

### 4. Stabilize multi-chunk and document v1.3 reader break

Multi-chunk is **on by default** for non-solid files > 4 MiB
(`-multi-chunk`, format **1.3**).

**Impact (must stay loud in docs):**

- Writers emit `VersionMinor = 3` when any entry has `ChunkCount > 1`.
- **Readers built for ≤ v1.2 reject minor 3** — they cannot open those
  archives at all (not “extract without parallel”).
- Escape hatch: `nya create -multi-chunk=false` → stays on minor 1 (or 2 if
  encrypted-only) so old tools can still read.
- Encrypted + multi-chunk stays **minor 3** (Argon2id flags still set; KDF
  must not downgrade minor).
- Current reference reader reads **1.0–1.3**.

**Status:** docs + encrypted multi-chunk CI test landed.

### 5. Push dictionaries (moved up from mid-term)

- CompressionID **5** + `Writer.SetDict` / `Reader.SetDict` / CLI `-dict`
- Solid path uses dict via `compressRaw`
- Dictionary is **external** for now (same file at create and extract)
- **Next:** embed dict in archive / auto-train from solid groups

**Status:** external + **embedded** (tail `0x0006`) landed; auto-train from solid groups still TODO.

### 6. Keep grinding NYA-LZMA2 (not “done”)

BT4 + optional optimal parse exist, but the encoder is **not thoroughly
finished** vs xz/7-Zip. Tracked in [SPEC-CODECS.md](SPEC-CODECS.md). This is
ongoing craft, not a one-sprint checkbox.

---

## Mid term

| Item | Intent |
| --- | --- |
| **Dictionaries** | Embed dict in archive; auto-train from solid groups (external `-dict` MVP done) |
| **Better zstd matcher** | Close ratio gap vs libzstd without new on-disk ID |
| **Optional “best effort” optimal path** | Switchable optimal parse / deeper search — not default |
| **`nya get` resume / partial** | Harden `.nyam.state`, Range, multi-chunk partial fetch |
| **Executable-aware filters (learn from UPX)** | ELF + **PE (Windows exe)** + **Mach-O (macOS)** — see [docs/SPEC-FILTER-EXEC.md](docs/SPEC-FILTER-EXEC.md) |
| **NYA-LZMA2 depth** | SIMD match extend, optimal-window policy, solid+dedup profile |
| **Real early users** | Firmware analysis / game distribution — even internal — before v1 freeze marketing |

Codec iteration stays under [SPEC-CODECS.md](SPEC-CODECS.md): same
CompressionID, better software.

### Why ELF benches sit at ~100% (and UPX still wins elsewhere)

README tables show **nya / xz / 7z / zstd all ≈ 100%** on the sample ELF
blobs. That is **not** “NYA uniquely fails on binaries.” Those inputs are
already high-entropy (stripped, media-like, or effectively incompressible as
a flat byte stream). Archive codecs treat the file as an opaque buffer;
headers + BCJ help a little on *typical* code, and lose when there is nothing
left to model.

**[UPX](https://upx.github.io/)** is **open source** (GPLv2+, optional special
exceptions for packed commercial binaries — see its `LICENSE` / `COPYING`).
It is also a **different product**: an *executable packer*, not a general
archive compressor.

| UPX does | NYA archive does today |
| --- | --- |
| Parse ELF/PE/Mach-O layout | Optional BCJ on code sections + generic LZ |
| Section-/reloc-aware transforms | No section map |
| NRV / LZMA tuned for code + tiny runtime stub | Compress → extract to original bytes |
| On-disk shrink 50–70% on *normal* programs | Must restore bit-identical file content |

So: **you can and should study UPX** (ideas are learnable; copying code means
GPL hygiene — NYA is already GPL-3.0, still review UPX’s special exceptions
before vendoring). Expectation management:

1. **Do not** promise that `nya create` on a random ELF will match `upx --best`.
2. **Do** steal the *format-aware* lessons that fit an archive:
   - Parse ELF/PE sections; apply stronger filters only on `.text` / code
   - Detect already-packed / high-entropy segments → **store** (`PayloadDense`)
   - Optional “pack executable entries” profile is a **separate** feature
     (closer to SFX/stub territory), not the default archive path
3. Keep the product headline on **FEC + distribution**; executable packing is
   a niche enhancer for firmware/game *binaries inside* a pack, not the wedge.

Detail: [docs/NOTE-UPX.md](docs/NOTE-UPX.md).

---

## Long term

Do **not** position NYA as a desktop WinRAR/7-Zip clone.

Possible durable niches (pick with users, not speculation alone):

1. **Distribution-native archive** — FEC + manifest + single URL as first-class
2. **Pipeline / firmware / game CI** — convert → FEC → CDN → get → repair
3. **Embeddable pure-Go container** — tools that cannot ship cgo or 7z DLLs

If a feature only helps “open rar faster in a GUI,” deprioritize it unless it
also strengthens the distribute/recover story.

---

## Doc map

| Doc | Role |
| --- | --- |
| [COMPATIBILITY.md](COMPATIBILITY.md) | v1 LTS contract, minor versions |
| [docs/SPEC-MULTICHUNK.md](docs/SPEC-MULTICHUNK.md) | Multi-chunk design |
| [docs/BENCHMARK-FEC.md](docs/BENCHMARK-FEC.md) | Recovery vs 7z/RAR |
| [docs/BENCHMARK-COMPRESS.md](docs/BENCHMARK-COMPRESS.md) | Compression A/B |
| [docs/BENCHMARK-CORPUS.md](docs/BENCHMARK-CORPUS.md) | Public corpus + raw data plan |
| [SPEC-CODECS.md](SPEC-CODECS.md) | Zstd vs LZMA2 roles |
| [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md) | `.nyam` / get transport |
| [docs/NOTE-UPX.md](docs/NOTE-UPX.md) | UPX license + what we can learn for ELF |
| [docs/SPEC-SIGNATURE-PRESERVE.md](docs/SPEC-SIGNATURE-PRESERVE.md) | Bit-identical extract + BCJ skip on signed PE/Mach-O |

Update this file when priorities change; keep commits conventional
(`docs: …` / `feat: …`).
