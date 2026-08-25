# NYA SFX stub (Rust)

Small **extract-only** self-extracting stub for `.nya` archives. See
[SPEC-SFX.md](../SPEC-SFX.md).

## Build

```bash
# from repo root (workspace)
cargo build -p nya-sfx-stub --release
cp target/release/nya-sfx-stub sfx/stubs/nya-sfx-stub_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m)

# or from sfx/
cd sfx && cargo build --release
```

Release binary size is typically **~500–600 KB** (stripped, with zstd + lzma2).

If `cargo build` fails on `jobserver` requiring Rust 1.85+:

```bash
cargo update jobserver --precise 0.1.31
```

## Usage

Wrapped by the Go `nya` tool:

```bash
nya sfx pack.nya -o pack.exe
nya create -sfx game.exe -level 3 ./GameData/
nya extract game.exe   # Go reader detects SFX footer
./game.exe             # stub extracts to current directory
```

Dev/test without wrapping:

```bash
./target/release/nya-sfx-stub ../test.nya -o /tmp/out -y
```

## Codecs

- store (0)
- zstd (1), RFC 8878
- lzma2 (6), raw LZMA2 stream

BCJ filters and encrypted archives are not supported in the stub yet; use
`nya extract` for those.
