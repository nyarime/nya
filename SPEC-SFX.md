# NYA self-extracting archives (SFX)

Version **1.0**. Companion to [SPEC.md](SPEC.md).

An SFX file is **not** a different archive format. It is a **native executable
stub** with a complete `.nya` archive appended and a fixed **footer** at the
very end of the file so the stub can locate the archive without scanning.

Reference stubs: Rust crate in [`sfx/`](sfx/) (`nya-sfx-stub`). The main
[`nya`](cmd/nya) tool only concatenates stub + archive + footer; extraction
logic lives in the stub.

## Goals

| Goal | Mechanism |
| --- | --- |
| 7-Zip-like UX | `nya create -sfx game.exe files/` |
| Small stub | Rust extract-only binary (~300 KB–1 MB stripped) |
| Format unchanged | Embedded bytes are a normal `.nya` v1 file |
| Interop | `nya extract game.exe` works when footer is present |

## File layout

```
+---------------------------+
| Stub executable           |  PE (Windows) or ELF (Linux), etc.
+---------------------------+
| Embedded .nya archive     |  Starts with NYA magic; valid standalone .nya
+---------------------------+
| Optional config (UTF-8)   |  Install script block (future)
+---------------------------+
| SFX footer (40 bytes)     |  Always last bytes of the file
+---------------------------+
```

## SFX footer (40 bytes, little-endian)

| Offset | Size | Field | Notes |
| ---: | ---: | --- | --- |
| 0 | 8 | Magic | ASCII `NYASFX01` |
| 8 | 8 | ArchiveOffset | Byte offset of embedded `.nya` from file start |
| 16 | 8 | ArchiveSize | Length of embedded `.nya` in bytes |
| 24 | 8 | ConfigOffset | `0` if no config block |
| 32 | 4 | ConfigSize | Length of config block |
| 36 | 4 | Flags | See below |

### Flags

| Bit | Name | Meaning |
| ---: | --- | --- |
| 0 | Console | Stub uses stderr/stdout (default) |
| 1 | GUI | Reserved: graphical progress |
| 2 | Silent | No prompts |
| 3 | AutoRun | Run `RunProgram` from config after extract (future) |
| 4–31 | _Reserved_ | zero |

## Stub behaviour (normative)

1. Resolve path to the running executable (`argv[0]` / `/proc/self/exe`).
2. Read the last **40 bytes**; verify `NYASFX01`.
3. Seek to `ArchiveOffset` and read exactly `ArchiveSize` bytes.
4. Parse as [SPEC.md](SPEC.md) `.nya` and extract entries to the output
   directory (default: current working directory).
5. Exit non-zero on magic, truncation, or decode failure.

The stub MUST NOT modify bytes before `ArchiveOffset`. The embedded archive
MAY be extracted by `nya extract` when copied out manually.

## Building SFX files

```bash
# Build reference stubs (once per platform)
cd sfx && cargo build --release

# From an existing archive
nya sfx pack.nya -o pack.exe -stub sfx/target/release/nya-sfx-stub

# Create and wrap in one step
nya create -sfx game.exe -level 3 ./GameData/
```

When `-stub` is omitted, `nya` uses embedded stubs from `sfx/stubs/` if
present (see `sfx/README.md`).

## Config block (future)

Optional UTF-8 text between archive and footer, 7-Zip-style:

```text
;!@Install@!UTF-8!
Title="My Game"
ExtractPath="."
RunProgram="bin\\start.bat"
;!@InstallEnd@!
```

`ConfigOffset` / `ConfigSize` point at this block. v1 stubs may ignore it.

## Stub variants

| Stub | Codecs | Target size (stripped) |
| --- | --- | --- |
| `nya-sfx-stub` | store, zstd, lzma2 | ~500 KB–1.5 MB (platform-dependent) |
| Future `nya-sfx-lite` | store, zstd only | smaller |

Archives using unsupported codecs MUST fail with a clear error from the stub.

## Version history

### 1.0

Initial footer layout and Rust reference stub.
