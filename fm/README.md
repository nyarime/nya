# nyaFM (Rust)

Minimal **File Manager** GUI for `.nya` archives — same idea as 7-Zip’s `7zFM`,
but scoped to open / list / extract for now. Archive parsing and decompression
are shared with the [SFX stub](../sfx/) via the `nya_archive` library.

## Features (v0.1)

- Open `.nya` (also accepts path on the command line)
- List entries (name, type, size, codec) with filter
- Extract all to a chosen folder
- Overwrite toggle

Not yet: create/add files, FEC repair UI, encryption prompts, shell integration
(use `nya associate` on Windows for double-click extract).

## Build

From the repo root (Cargo workspace):

```bash
cargo build -p nya-fm --release
./target/release/nya-fm [archive.nya]
```

Linux runtime: needs a working display and typically `libxkbcommon-x11`.

```bash
# Debian/Ubuntu
sudo apt-get install -y libxkbcommon-x11-0
```

## Layout

```
fm/          this crate (eframe / egui UI)
sfx/         nya_archive lib + nya-sfx-stub binary
Cargo.toml   workspace members = ["sfx", "fm"]
```

## Roadmap

1. Extract selected entries only  
2. Drag-and-drop open  
3. Password prompt when `FlagEncrypted`  
4. Call out to `nya` CLI for create / convert / repair  
5. Windows installer shipping `nya-fm.exe` + file association  
