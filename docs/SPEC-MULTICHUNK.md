# Multi-chunk entries (v1.3)

This document specifies how NYA grows beyond **ChunkCount = 1** per file
entry. **Implemented** in format minor 3; readers on v1.0–1.2 continue to
work when all entries still have `ChunkCount = 1`.

Related: [SPEC.md](SPEC.md), [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md),
[COMPATIBILITY.md](COMPATIBILITY.md).

## Goals

| Goal | Why |
| --- | --- |
| Parallel compress / decompress | Large files (≥ tens of MiB) should use `-workers N` on independent chunks |
| Finer-grained FEC | Parity per chunk limits blast radius; partial repair without re-reading the whole payload |
| Random access / partial extract | Future HTTP Range + `nya-get` can fetch only the chunks needed for one path |
| Solid compatibility | Solid archives remain **one** logical stream; multi-chunk applies to **non-solid** entries first |

## Non-goals (v1.3)

- Splitting a solid stream into multiple DirEntries (still one global solid chunk)
- Changing CompressionID or block wrapper format inside each chunk
- Breaking v1.1/v1.2 archives without a minor bump

## Chunk size policy

| Condition | Default raw chunk size | Notes |
| --- | ---: | --- |
| File size ≤ 4 MiB | single chunk | Same as today |
| 4 MiB < size ≤ 64 MiB | 4 MiB raw | Matches LZMA2 dictionary growth at level 7–9 |
| size > 64 MiB | 8 MiB raw | Amortizes chunk header + FEC symbol overhead |

Writer may override with `-chunk-size N` (CLI flag on `nya create`). Sizes are **raw
(uncompressed) byte boundaries** before BCJ. The writer detects BCJ on the
whole file, applies it only when compression wins (same rule as solid mode),
then splits into chunks and compresses each chunk independently into the
existing block wrapper:

```
uint32 blockLength
block bytes…
```

(repeated inside one ChunkHeader payload, as today for large single-chunk files
that already split at 512 KiB for internal block encoding).

## On-disk layout (v1.3 proposal)

### Non-solid, multi-chunk entry

```
DirEntry.ChunkCount = N
DirEntry.FirstDataOff = offset of chunk 0 header (relative to data area)

Chunk 0: ChunkHeader + compressed payload₀
Chunk 1: ChunkHeader + compressed payload₁
…
Chunk N-1: ChunkHeader + compressed payloadₙ₋₁
```

Each chunk has its own:

- `OriginalSize` / `CompressedSize` in **ChunkHeader**
- BLAKE3 short hash
- Optional FEC parity tail referenced from the recovery section (see below)

`DirEntry.OriginalSize` remains the **sum** of chunk original sizes.
`DirEntry.FirstDataOff` points at chunk 0; chunk *k* offset is discovered by
walking prior chunks (same as reading multiple blocks inside one chunk today).

### FEC interaction

Today FEC is computed over the **entire** compressed payload of an entry.
With multi-chunk:

1. **Per-chunk FEC** — default when `ChunkCount > 1` and `-fec > 0`.
   Each chunk's compressed bytes get an independent `encodeFEC` plan.
   Symbol hash tables are concatenated in recovery section order.

2. **Recovery section layout** — unchanged outer structure; inner ordering
   becomes `[chunk0 parity][chunk0 hashes][chunk1 parity][chunk1 hashes]…`.

3. **Repair** — `nya repair` attempts each chunk independently; reports
   `FailedChunks` per chunk index.

4. **Solid + FEC** — unchanged: one compressed solid stream, one FEC pass
   (Leopard-RS when payload ≥ 4 MiB compressed).

5. **Global metadata FEC** — still covers central directory + hash tables only.

### Encryption

Each chunk's compressed payload is encrypted separately when `-password` is
set (same AES-256-GCM + nonce prefix per chunk). KDF parameters remain in the
global header (v1.2 Argon2id).

### Parallel extract

Reader with `-workers N`:

1. Parse central directory
2. For each file entry with `ChunkCount > 1`, schedule chunk decompress jobs in parallel
3. Concatenate raw chunks in order, apply BCJ post-filter if entry-level BCJ set

## Version bump

- **Writers** emit `VersionMinor = 3` when any entry has `ChunkCount > 1`.
- **Readers** on v1.2 accept v1.3 if all entries still have `ChunkCount = 1`
  (forward-compatible test archives).
- **Readers** on v1.3 must read v1.0–1.2 archives (ChunkCount always 1).

## Migration path

| Step | Action | Status |
| --- | --- | --- |
| 1 | Land chunk splitter in writer behind `-multi-chunk` flag | **Done** (default on) |
| 2 | Reader: loop `ChunkCount` in `Extract` | **Done** |
| 3 | FEC/repair per chunk | **Done** |
| 4 | `nya-get` manifest lists byte ranges per chunk | **Done** (`entries` in `.nyam`, `nya-get --paths`) |
| 5 | Enable by default for files > 4 MiB non-solid | **Done** |

## Open questions

- Should chunk boundaries align to FEC symbol size to avoid padding waste?
- Dedup (SPEC-EXTENSIONS) may reference chunk hashes — defer until dedup tail lands.
- BCJ on x86: **whole-file arch detection**, apply before chunk split, reverse
  after all chunks are concatenated on extract (not per 512 KiB block).

## Testing plan

- Roundtrip: 1 MiB, 5 MiB, 100 MiB files at levels 5/9 with `-fec 10`
- Repair: corrupt chunk 2 of 5, verify other chunks untouched
- Encrypted multi-chunk + solid archive regression in CI (`-short` skips size)

See `stress_test.go` for encrypted + high-FEC + large payload coverage.
