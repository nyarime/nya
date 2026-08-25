# NYA v1 extensions (foundation)

Version **1.0** (spec document). Companion to [SPEC.md](SPEC.md) and
[COMPATIBILITY.md](COMPATIBILITY.md).

This document defines **byte-level slots and semantics** for capabilities that
may ship after the first public tools release. Implementations MAY omit writers
for any section here; readers MUST follow the skip rules so older tools keep
working.

**Design goal:** freeze the *foundation* (where extensions live and how they
chain) before the first external adopter. Feature code iterates on top without
`VersionMajor = 2`.

## Relationship to v1 core

| Layer | Frozen in v1 | Iterated in v1.x |
| --- | --- | --- |
| Container topology | Header → Data → Central Dir → Recovery → Symbol Hash → **Tail chain** | — |
| Extension mechanism | Header reserved, flags, tail `typeId`, DirEntry v3 | — |
| Solid grouping | `SolidGroupID`, solid-group tail schema | Writer grouping heuristics |
| Content dedup | `ContentBlake3`, dedup index tail | Cross-entry sharing in writer |
| Download index | Tail type + embed flag; shared schema with `.nyam` | `nya manifest --embed` |
| NyaFS profile | Entry flags, path conventions, profile xattrs | Mount/overlay tools |
| Multi-session | Session tail + header `SessionBaseOffset` | Append tooling |

Nothing in this file introduces a new file extension. All extensions stay inside
`.nya` with `VersionMajor = 1`.

## Extended file layout

The core layout in [SPEC.md](SPEC.md) ends with the symbol hash table. v1
adds an optional **tail chain** and an optional **archive footer**:

```
+---------------------------+
| Global Header   128 bytes |
+---------------------------+
| Data Area                 |
+---------------------------+
| Central Directory         |
+---------------------------+
| Recovery Section          |
+---------------------------+
| Symbol Hash Table         |
+---------------------------+
| Tail Chain (optional)     |  at Header.TailChainOffset, TailChainSize bytes
+---------------------------+
| Archive Footer (optional) |  if FlagHasFooter
+---------------------------+
```

When `TailChainOffset` is zero or `TailChainSize` is zero, the tail chain is
absent and the file ends after the symbol hash table (current behaviour).

`TailChainOffset` MUST point at the first byte of the tail chain. The chain
MUST lie entirely after the symbol hash table and entirely before the archive
footer (if any).

## Global header reserved region

Bytes **88–127** of the file (the 40-byte `Reserved` field) are allocated as
follows. Unallocated bytes MUST be zero in new writers until assigned.

| Offset (in Reserved) | Size | Field | Notes |
| ---: | ---: | --- | --- |
| 0 | 8 | TailChainOffset | Byte offset from file start; **0** = no tail chain |
| 8 | 8 | TailChainSize | Total size of the tail chain in bytes; **0** = absent |
| 16 | 8 | SessionBaseOffset | Byte offset where **this session's** global header starts; **0** = single-session file (header at 0) |
| 24 | 8 | ProfileFlags | See [Profile flags](#profile-flags) |
| 32 | 8 | _Reserved_ | zero |

Readers that do not implement tails MUST ignore non-zero `TailChainOffset` /
`TailChainSize` if they can still list and extract entries from the central
directory. They MUST NOT treat unknown tail payloads as corruption of the data
area or central directory.

### Profile flags

| Bit | Name | Meaning |
| ---: | --- | --- |
| 0 | ProfileArchive | General backup / game pack (default) |
| 1 | ProfileDistribution | Intended for CDN + `nya-get` |
| 2 | ProfileDatabase | e.g. Nyarc firmware analysis DB |
| 3 | ProfileRootFS | NyaFS immutable root + overlay semantics |
| 4 | ProfileOptical | Multi-session append intended |
| 5–63 | _Reserved_ | zero |

Multiple bits MAY be set when a profile combines roles (e.g. rootfs +
distribution). Tools SHOULD also record a human label in xattr
`nya.profile.name` on the root directory entry when present.

## Tail chain format

The tail chain is a concatenation of records:

```
+----------------------------------+
| uint32  typeId                   |
| uint32  payloadLen               |
| payloadLen bytes                 |
+----------------------------------+
| (next record…)                   |
+----------------------------------+
```

Rules:

- Records appear in ascending `typeId` order (recommended, not required).
- `payloadLen` MUST NOT exceed remaining bytes in the chain.
- Readers MUST skip unknown `typeId` values without failing extraction.
- Writers MUST NOT reuse a `typeId` with an incompatible payload layout; bump
  a **schema version byte** inside the payload instead.

### Tail type registry

| typeId | Name | Status | Spec section |
| ---: | --- | --- | --- |
| `0x0001` | DownloadIndex | Reserved | [Download index tail](#download-index-tail-0x0001) |
| `0x0002` | ContentAddressIndex | Reserved | [Dedup index tail](#content-address-index-tail-0x0002) |
| `0x0003` | SolidGroupTable | Reserved | [Solid group table](#solid-group-table-tail-0x0003) |
| `0x0004` | SessionDescriptor | Reserved | [Multi-session](#multi-session-tail-0x0004) |
| `0x0005` | ProfileMetadata | Reserved | [Profile metadata](#profile-metadata-tail-0x0005) |
| `0x0006` | ZstdDictionary | **Supported** | Embedded NYA-Zstd dictionary for CompressionID 5 |
| `0x0000`, `0xFFFF` | _Reserved_ | — | Must not appear |
| other | Unknown | — | Skip |

Setting global header flag bit 4 (`HasDownloadIndex`) SHOULD be done when
tail type `0x0001` is present. The flag is informative; location is always
`TailChainOffset`.

## Directory entry version 3

Version 2 entries (see [SPEC.md](SPEC.md)) remain valid forever. Version 3
adds dedup, solid-group, and profile fields **after** the xattr block:

```
… (DirEntry v2 fields and xattrs, unchanged) …
uint8   EntryVersion        MUST be 0x03 when extension present
uint16  SolidGroupID        0 = default / non-grouped
uint16  EntryFlags          see below
[32]byte ContentBlake3       BLAKE3-256 of stored chunk payload; zero = unset
```

Detection: after reading all v2 fields, if EOF has not been reached and the
next byte is `0x03`, read the v3 extension. If the next byte is not `0x03`,
the entry ends at v2 (backward compatible).

### EntryFlags (v3)

| Bit | Name | Meaning |
| ---: | --- | --- |
| 0 | Immutable | Entry must not be modified in overlay profiles (NyaFS root) |
| 1 | OverlayLayer | Entry belongs to the writable overlay layer |
| 2 | SharedChunk | `FirstDataOff` references a chunk shared with other entries |
| 3 | Whiteout | Tombstone: path hidden in overlay (deletes a lower-layer name) |
| 4–15 | _Reserved_ | zero |

### ContentBlake3 and dedup

When `EntryFlags.SharedChunk` is set:

- `ContentBlake3` is the BLAKE3-256 digest of the **stored chunk payload**
  (compressed, encrypted if applicable — the same bytes covered by the chunk
  header's `Blake3Short`, but full 256 bits).
- `FirstDataOff` points at the shared chunk header in the data area.
- Multiple directory entries MAY reference one physical chunk if
  `ContentBlake3`, `CompressedSize`, and payload bytes match.

Writers SHOULD emit tail type `0x0002` when cross-entry dedup is used (see
below). Until then, dedup MAY be implied solely from v3 entries.

## Solid archives and solid groups

Today, flag bit 1 (`SolidCompress`) means one solid stream for the whole
archive. **Solid groups** refine this:

- Entries with the same non-zero `SolidGroupID` are compressed as **one solid
  stream per group**.
- `SolidGroupID == 0` means the entry is stored in the legacy way: either
  per-file chunks (non-solid) or the single global solid stream when
  `SolidCompress` is set and no group table exists.

### Solid group table tail (`0x0003`)

```
uint8   schemaVersion     MUST be 1
uint16  groupCount
groupCount times:
    uint16  solidGroupID
    uint16  codecHint      CompressionID recommended for the group (informative)
    uint32  entryCount
    entryCount times:
        uint16  pathLen
        pathLen bytes UTF-8 path (central directory order within group)
    uint64  firstDataOff    offset of group's chunk header in data area
    uint64  decompSize      total decompressed bytes in the group stream
```

Readers use this table to map paths to offsets inside a grouped solid stream.
When the tail is absent, behaviour falls back to [SPEC.md](SPEC.md) solid
rules.

## Content-address index tail (`0x0002`)

Optional acceleration structure for deduplicated archives:

```
uint8   schemaVersion     MUST be 1
uint32  chunkCount
chunkCount times:
    [32]byte  contentBlake3
    uint64    firstDataOff
    uint64    compressedSize
    uint32    refCount         number of directory entries referencing this chunk
```

Readers MAY build an equivalent map from DirEntry v3 alone. The tail is a
hint for repair tools and fast `verify -dedup`.

## Download index tail (`0x0001`)

Embeds the same information as a [`.nyam` sidecar](SPEC-DOWNLOAD.md) in a
binary form suitable for `Range` fetch without a second file.

```
uint8   schemaVersion     MUST be 1
uint32  blockSize
uint32  blockCount
blockCount times:
    uint64  offset
    uint32  size
    [32]byte blockBlake3     BLAKE3-256 of offset..offset+size in the .nya file
[32]byte archiveBlake3       BLAKE3-256 of the entire .nya file
```

JSON field names and invariants match [SPEC-DOWNLOAD.md](SPEC-DOWNLOAD.md).
Tools MAY convert tail ↔ `.nyam` losslessly when `schemaVersion = 1`.

Phase 1 shipping path: `.nyam` sidecar only. Phase 2: `nya manifest --embed`.

## Multi-session tail (`0x0004`)

Supports optical / incremental backup profiles (`ProfileOptical`).

```
uint8   schemaVersion     MUST be 1
uint16  sessionIndex      1-based index of this session
uint16  totalSessions     total sessions described (may equal sessionIndex for append-in-progress)
uint64  previousSessionOffset   byte offset of previous session's global header; 0 for session 1
uint64  sessionDataAreaSize     size of this session's data area contribution
[32]byte sessionBlake3    BLAKE3-256 over bytes [SessionBaseOffset, TailChainOffset) optional; zero = unset
```

**Single-session files:** `SessionBaseOffset = 0`, no type `0x0004` tail.

**Multi-session files:** each append adds a new data area extent and a new
central directory that **supersedes** paths from earlier sessions (last wins).
Earlier session bytes remain for recovery and rollback. Readers:

1. Start at the **last** session's global header (highest `sessionIndex`).
2. Walk `previousSessionOffset` to read older sessions if needed.
3. Merge directory entries: later session overrides same path.

Exact append writer semantics are implementation-defined in v1.x; this tail
fixes **where sessions link**, not the CLI UX.

## Profile metadata tail (`0x0005`)

UTF-8 JSON blob (schema `"nyaprofile"`, version 1) for profile-specific data
that does not belong in per-entry xattrs:

```json
{
  "format": "nyaprofile",
  "version": 1,
  "rootfs": {
    "root_entry": "/",
    "overlay_whiteout_prefix": ".nya-whiteout/"
  },
  "optical": {
    "volume_label": "BACKUP_2026"
  }
}
```

Readers MUST accept unknown keys. Writers SHOULD prefer xattrs for per-entry
data and this tail for archive-wide profile options.

## NyaFS profile (informative)

NyaFS uses the same `.nya` container with `ProfileRootFS` and naming
conventions:

| Path / xattr | Role |
| --- | --- |
| `/` with `EntryFlags.Immutable` children | Read-only root layer |
| Entries with `EntryFlags.OverlayLayer` | Writable overlay |
| `EntryFlags.Whiteout` | Deletes a path from the merged view |
| xattr `nya.fs.layer` = `"root"` \| `"overlay"` | Redundant hint for tools |
| xattr `nya.fs.merged_path` | Optional display path |

Mount and overlay tools are **v1.x software**; this section fixes naming and
flags only.

## Reader skip rules (normative)

1. Unknown global header flag bits (except those documented as "must reject")
   MUST be ignored for extract when central directory parses.
2. Unknown tail `typeId`: skip `payloadLen` bytes, continue.
3. Unknown DirEntry v3 `EntryFlags` bits: ignore for extract unless documented
   as security-sensitive.
4. Unknown `ProfileFlags` bits: ignore.
5. If `TailChainOffset + TailChainSize` extends past EOF, reject as corrupted.

## Writer phases (informative)

| Phase | Deliverable |
| ---: | --- |
| 0 | This document + header reserved allocation (no code required) |
| 1 | Current create/list/extract/verify/repair + `.nyam` sidecar |
| 2 | DirEntry v3 + dedup writer (optional) |
| 3 | Solid group table + 7z-style grouping |
| 4 | Download index tail embed |
| 5 | NyaFS profile tools + multi-session append |

## Version history

### 1.0

Initial extension foundation: tail chain, header reserved layout, DirEntry v3,
solid groups, dedup index, download embed, NyaFS flags, multi-session link.
