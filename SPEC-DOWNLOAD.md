# NYA download distribution (`.nyam`)

Version 1. Companion to the NYA archive format ([SPEC.md](SPEC.md)).

Transport blocks describe **byte ranges of the on-disk `.nya` file** for HTTP
`Range` requests, resumable downloads, and parallel fetchers (`nya-get`). They
are independent of internal FEC/compression blocks inside the archive.

## Design goals

| Goal | Mechanism |
| --- | --- |
| Resume after disconnect | Per transport block completion + local state file |
| Saturate bandwidth | Parallel `Range` on distinct block IDs |
| Integrity | BLAKE3-256 per transport block + whole-file digest |
| CDN friendly | Single `.nya` URL + sidecar `.nyam` JSON |
| Backward compatible | Manifest is optional; existing `.nya` v1.0 unchanged |

## File roles

```
GamePack.nya      — archive (SPEC.md)
GamePack.nyam     — download manifest (this document)
GamePack.nyam.state — local resume state (not shipped; created by nya-get)
```

## `.nyam` manifest (JSON)

Media type: `application/vnd.nyarime.nyam+json` (conventional).

```json
{
  "format": "nyam",
  "version": 1,
  "archive": {
    "name": "GamePack.nya",
    "size": 5368709120,
    "blake3": "a1b2c3…64 hex…",
    "nya_version": "1.0",
    "fec_bytes": 104857600,
    "fec_offset": 5263851520
  },
  "download": {
    "block_size": 4194304,
    "blocks": [
      {
        "id": 0,
        "offset": 0,
        "size": 4194304,
        "blake3": "…"
      }
    ]
  },
  "sources": [
    {
      "url": "https://cdn.example.com/GamePack.nya",
      "priority": 1
    }
  ]
}
```

### Top-level fields

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `format` | string | yes | Must be `"nyam"`. |
| `version` | int | yes | Manifest schema version; currently `1`. |
| `archive` | object | yes | Describes the target `.nya` file. |
| `download` | object | yes | Transport block index. |
| `entries` | array | no | Per-file on-disk chunk byte ranges (multi-chunk v1.3). |
| `sources` | array | no | Ordered download URLs (highest `priority` first). |

### `archive` object

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | yes | Basename of the archive file. |
| `size` | int | yes | Total `.nya` size in bytes. |
| `blake3` | string | yes | Lowercase hex BLAKE3-256 of the entire file. |
| `nya_version` | string | no | `"major.minor"` from the NYA global header. |
| `central_dir_offset` | int | no | Byte offset of central directory (for partial fetch tail). |
| `fec_offset` | int | no | Byte offset of global recovery section (informative). |
| `fec_bytes` | int | no | Length of recovery section (informative). |

### `entries` array (optional)

Each element describes one `EntryFile` path and its on-disk chunks in the
data area (absolute byte offsets in the `.nya` file):

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `path` | string | yes | Archive entry path (as in central directory). |
| `original_size` | int | yes | Decompressed file size. |
| `chunk_count` | int | yes | Number of on-disk chunks. |
| `chunks` | array | yes | One object per chunk, sorted by `index`. |

Chunk object:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `index` | int | yes | Zero-based chunk index within the entry. |
| `offset` | int | yes | Start byte in `.nya` (includes `ChunkHeader`). |
| `size` | int | yes | On-disk length (`ChunkHeader` + compressed payload). |
| `original_size` | int | yes | Decompressed bytes for this chunk. |
| `blake3` | string | yes | BLAKE3-256 of `offset..offset+size` on disk. |

Populated by `nya manifest` when the archive parses successfully. Solid
archives list one shared on-disk chunk per file entry.

### `download` object

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `block_size` | int | yes | Nominal block size used when building the index. |
| `blocks` | array | yes | One entry per transport block, sorted by `id`. |

### Transport block entry

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | int | yes | Zero-based block index. |
| `offset` | int | yes | Start byte in `.nya`. |
| `size` | int | yes | Length in bytes (last block may be shorter than `block_size`). |
| `blake3` | string | yes | Lowercase hex BLAKE3-256 of `offset..offset+size`. |

**Invariants**

- Blocks cover `[0, archive.size)` without gaps or overlap.
- `offset` of block `id+1` equals `offset+size` of block `id`.
- Sum of all `size` values equals `archive.size`.

### `sources` entry

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `url` | string | yes | HTTP(S) URL to the `.nya` file. |
| `priority` | int | no | Higher tried first; default `0`. |

## HTTP download semantics

Fetcher (`nya-get`) MUST:

1. Pick a source URL (by `priority`, then failover).
2. Issue `GET` with `Range: bytes=offset-offset+size-1` per block.
3. Verify block `blake3` before marking the block complete.
4. Retry failed blocks on the same or alternate source.
5. After all blocks complete, verify `archive.blake3` over the assembled file
   (skip when `--paths` partial fetch is used).

Partial fetch (`nya-get --paths "path/to/file"`):

1. Union byte ranges: global header, each listed entry's `chunks[]`, and
   `[central_dir_offset, archive.size)`.
2. Download only transport blocks overlapping that union.
3. Verify per-block `blake3`; do not require whole-file `archive.blake3`.

Servers SHOULD support `Accept-Ranges: bytes`. When `Range` is unsupported,
fetchers MAY fall back to a full-file download and verify `archive.blake3`.

## Resume state file (`.nyam.state`)

JSON written locally by `nya-get`:

```json
{
  "manifest_blake3": "…",
  "output": "GamePack.nya",
  "completed": [0, 1, 2, 5],
  "updated_at": "2026-08-24T09:00:00Z"
}
```

| Field | Notes |
| --- | --- |
| `manifest_blake3` | BLAKE3 of the `.nyam` file; invalidates state if manifest changes. |
| `output` | Path being written. |
| `completed` | Sorted list of finished block IDs. |

## Generating manifests

Reference command:

```bash
nya manifest GamePack.nya -o GamePack.nyam \
  --url https://cdn.example.com/GamePack.nya \
  --block-size 4m
```

Implementation scans the existing `.nya` on disk; no archive rewrite required.

## Embedded download index (tail type `0x0001`)

Binary layout and tail chain placement: [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md)
§ Download index tail. Global header flag bit 4 (`HasDownloadIndex`) SHOULD be
set when the tail is present.

Phase 1: `.nyam` sidecar only (current). Phase 2: `nya manifest --embed` writes
tail type `0x0001` without changing the JSON schema fields documented here.

## Version history

### 1.0

Initial `.nyam` JSON schema and transport block conventions.
