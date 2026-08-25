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
| **BCJ filter** | Applied only when encode→decode is **byte-identical** and payload shrinks |
| **FEC / augment** | Parity shards only; does not alter payload bytes on extract |
| **Extract** | Writes `OriginalSize` bytes; optional xattrs/mode do not change file body |

## Embedded signature detection (`HasEmbeddedSignature`)

Before any BCJ transform, `bcjRoundtripOK` encodes then decodes in memory and
compares to the source. **Signed PE/Mach-O use BCJ only when this passes** —
signatures survive because extract still matches the original file bytes.

| Format | Signature detection | BCJ policy |
| --- | --- | --- |
| **PE** (`.exe`, `.dll`) | `IMAGE_DIRECTORY_ENTRY_SECURITY` | Roundtrip verify, then optional BCJ |
| **Mach-O** | `LC_CODE_SIGNATURE` | Same |

`HasEmbeddedSignature` is used for classification/docs; it does **not** hard-block BCJ.

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
