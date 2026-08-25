# Executable-aware filters (ELF / PE / Mach-O)

NYA learns from [UPX](https://upx.github.io/) **ideas** (section-aware code
transforms), not stub packing. Archives must **extract bit-identical** bytes;
this path improves compression of executables **inside** `.nya` packs.

## Supported containers

| Platform | Format | Detection | BCJ arch from header |
| --- | --- | --- | --- |
| Linux / Android (native) | ELF | `0x7F ELF` | `e_machine` |
| Windows | PE / `.exe` | `MZ` + `PE\0\0` | COFF `Machine` |
| macOS | Mach-O (thin + universal) | `FEEDFACE/FACF`, `CAFEBABE` fat | `cputype` |

## Section-aware BCJ

Default archive path (`chooseBCJForFile`):

1. Detect executable format and instruction architecture (`DetectBCJArch`).
2. Parse **code sections only** (`CodeSectionRanges`):
   - ELF: `SHF_EXECINSTR`
   - PE: `IMAGE_SCN_MEM_EXECUTE`
   - Mach-O: `S_ATTR_PURE_INSTRUCTIONS` / `S_ATTR_SOME_INSTRUCTIONS`
3. Apply BCJ only inside those file-offset spans (PC math uses absolute offsets).
4. Keep BCJ only if blocked compressed size shrinks (same guard as before).

Decode mirrors encode via `ApplyBCJFilterArchSmart` in `finishFilePayload`.

**Signed executables:** BCJ is never applied when `HasEmbeddedSignature` detects
Authenticode (PE) or `LC_CODE_SIGNATURE` (Mach-O). See
[SIGNATURE-PRESERVE.md](SPEC-SIGNATURE-PRESERVE.md).

Whole-file BCJ is **not** used when section ranges are available — avoids
touching `.data` / resources that can false-match branch opcodes.

## What this is not

- Not a second UPX (no runtime stub, no on-disk exe shrink 50–70%).
- Not a promise to beat UPX on a single `.exe` in README tables.
- FEC + distribution remain the product wedge; this is encoder craft for
  game/firmware packs mixing `.exe`, macOS binaries, and ELF tools.

## Related

- [NOTE-UPX.md](NOTE-UPX.md) — UPX license + realistic expectations
- [ROADMAP.md](../ROADMAP.md) — executable-aware filters mid-term item
- `exec_sections.go` — parsers and `CodeSectionRanges`
