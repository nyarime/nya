# NYA GitHub Releases

Tag a version to build installable artifacts (7-Zip-style):

```bash
git tag v0.1.0
git push origin v0.1.0
```

Or run **Actions → Release → Run workflow** and enter a version (creates/updates `vVERSION`).

Workflow: [`.github/workflows/release.yml`](../.github/workflows/release.yml).

## What you get

| Asset | Contents |
| --- | --- |
| `nya-VERSION-windows-amd64-setup.exe` | **Inno Setup installer**: `nya.exe`, `nya-get.exe`, `nya-fm.exe`, SFX stub; optional PATH + `.nya` → nyaFM |
| `nya-VERSION-windows-amd64.zip` | Portable Windows x64 binaries (same files, no installer) |
| `nya-VERSION-linux-amd64.tar.gz` | `nya`, `nya-get`, `nya-fm`, `nya-sfx-stub` |
| `nya-VERSION-linux-arm64.tar.gz` | CLI only (`nya`, `nya-get`) |
| `nya-VERSION-darwin-amd64.tar.gz` | macOS Intel: CLI + nyaFM + stub |
| `nya-VERSION-darwin-arm64.tar.gz` | macOS Apple Silicon: CLI + nyaFM + stub |

## Windows install experience

1. Download `*-windows-amd64-setup.exe` from the [GitHub Release](https://github.com/nyarime/nya/releases).
2. Run the installer (per-user under `%LOCALAPPDATA%\Programs\NYA` by default).
3. Leave **Associate .nya** checked → double-click opens **nyaFM**.
4. Optionally add install dir to **PATH** → `nya` / `nya-get` in any terminal.
5. Start Menu → **nyaFM**.

Prefer extract-beside on double-click? After install run `nya associate` (CLI), which overrides open → `nya open`.

Uninstall removes Start Menu entries, PATH append (when added by installer), and the `.nya` ProgID written by the installer.

## Manual / local packaging

```bash
# Go
mkdir -p dist/windows-amd64
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/windows-amd64/nya.exe ./cmd/nya
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/windows-amd64/nya-get.exe ./cmd/nya-get

# Rust (on Windows)
cargo build -p nya-fm -p nya-sfx-stub --release
cp target/release/nya-fm.exe target/release/nya-sfx-stub.exe dist/windows-amd64/

# Inno Setup 6
ISCC /DMyAppVersion=0.1.0 /DStagingDir=dist\windows-amd64 packaging\windows\nya.iss
# → dist/nya-0.1.0-windows-amd64-setup.exe
```
