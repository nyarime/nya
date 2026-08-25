# Digital signature preservation

NYA archives are **lossless containers**. For signed binaries the product rule is:

> **Extracted bytes must equal source bytes** — signatures, notarization, and
> Authenticode must keep validating after `nya create` → `nya extract`.

This is the opposite of UPX-style packing, which intentionally replaces the
on-disk executable.

## What NYA guarantees

| Layer | Behavior |
| --- | --- |
| **Codecs** | Zstd / LZMA2 / store are lossless; no semantic re-encoding |
| **BCJ filter** | **Skipped** when an embedded signature is detected |
| **FEC / augment** | Parity shards only; does not alter payload bytes on extract |
| **Extract** | Writes `OriginalSize` bytes; optional xattrs/mode do not change file body |

## Embedded signature detection (`HasEmbeddedSignature`)

Before any BCJ transform:

| Format | Detection |
| --- | --- |
| **PE** (`.exe`, signed `.dll`) | `IMAGE_DIRECTORY_ENTRY_SECURITY` non-empty |
| **Mach-O** (macOS app/binary) | `LC_CODE_SIGNATURE` with `datasize > 0` |

Unsigned executables may still use section-aware BCJ when it shrinks the
blocked payload (see [SPEC-FILTER-EXEC.md](SPEC-FILTER-EXEC.md)).

**Solid archives:** if any member has an embedded signature, BCJ is disabled for
the entire solid stream so signed files inside the group are never transformed.

## Formats without BCJ (whole-file lossless)

These are already stored as opaque blobs — no BCJ, only lossless compress or store:

| Format | Notes |
| --- | --- |
| **MSIX** / `.appx` | ZIP container (`PK`); often `PayloadDense` |
| **MSI** | OLE compound document; package signature covers whole file |
| **DMG** | Disk image; whole-file roundtrip preserves Apple signing on the image |

If the **entire** signed package is archived and extracted bit-identically,
platform tools (`signtool verify`, `codesign -v`, Gatekeeper) should accept it.

## What breaks signatures (not NYA archive path)

| Action | Result |
| --- | --- |
| `nya sfx` (self-extracting wrapper) | New executable — not the original signed file |
| UPX / re-packers | On-disk image changes |
| Extract then edit / re-save | Outside NYA |
| Corrupt or truncated archive | Hash mismatch; verify fails |

## Verification

Tests:

- `exec_signature_test.go` — detection + BCJ skip
- `signature_roundtrip_test.go` — signed PE through zstd and LZMA2 archives

Recommended manual check after deploy:

```bash
# Windows
signtool verify /pa extracted\setup.exe

# macOS
codesign -v --verbose=4 extracted/MyApp.app
spctl -a -vv -t install extracted.pkg
```

## Related

- [SPEC-FILTER-EXEC.md](SPEC-FILTER-EXEC.md) — section-aware BCJ (unsigned path)
- [NOTE-UPX.md](NOTE-UPX.md) — UPX changes files; NYA restores them
