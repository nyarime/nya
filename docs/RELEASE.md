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
| `nya-VERSION-windows-amd64-setup.exe` | **Inno Setup installer** (legacy): may still bundle extras; target is **`nya.exe` only** + `.nya` → `nya open` |
| `nya-VERSION-windows-amd64.zip` | Portable Windows x64: **`nya.exe` only** |
| `nya-VERSION-windows-arm64.zip` | Portable Windows ARM64: **`nya.exe` only** |
| `nya-VERSION-linux-amd64.tar.gz` | **`nya` only** |
| `nya-VERSION-linux-arm64.tar.gz` | **`nya` only** |
| `nya-VERSION-darwin-amd64.tar.gz` | **`nya` only** |
| `nya-VERSION-darwin-arm64.tar.gz` | **`nya` only** |

Download: `nya get --url …` (no separate `nya-get` in release tarballs/zips).

## Linking model (vs 7-Zip)

**7-Zip does not ship glibc / musl / uClibc package variants.**

| Platform | What 7-Zip actually ships |
| --- | --- |
| Windows | Installer + “7-Zip Extra” console. Default MSVC build **statically links the CRT** into the `.exe` (not a separate msvcrt dance). |
| Linux (official tarball / GitHub) | One console build per arch (`7zz`); historically often **dynamic glibc** → “GLIBC_2.xx not found” on older distros. Distros (`apt install 7zip`) are normal dynamic packages. Fully static musl builds (`7zzs`) are community/CI extras, not a three-libc matrix. |
| uClibc | Not an official distribution target. |

**NYA CLI** avoids that matrix: Go with `CGO_ENABLED=0` is already a **fully static** binary (no glibc/musl dependency). One Linux amd64/arm64 CLI artifact runs on glibc *and* musl hosts.

| Artifact | Linkage | UPX |
| --- | --- | --- |
| `nya` | Go static (`CGO_ENABLED=0`) | Linux + Windows **amd64** yes; Darwin / Windows **arm64** no |

SFX outputs concatenate `nya` bytes + archive + footer — **never UPX** those files.

## Windows install experience

1. Download `*-windows-amd64-setup.exe` from the [GitHub Release](https://github.com/nyarime/nya/releases).
2. Run the installer (per-user under `%LOCALAPPDATA%\Programs\NYA` by default).
3. Leave **Associate .nya** checked → double-click runs `nya open`.
4. Optionally add install dir to **PATH** → `nya` in any terminal.

Prefer extract-beside on double-click? After install run `nya associate` (CLI), which overrides open → `nya open`.

Uninstall removes Start Menu entries, PATH append (when added by installer), and the `.nya` ProgID written by the installer.

## One-click install scripts (no Inno / no Actions)

When GitHub Actions is off, or you prefer curl/irm over setup.exe:

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.sh | bash
curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.sh | bash
```

```powershell
# Windows
irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.ps1 | iex
irm https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.ps1 | iex
```

Scripts download the matching Release asset (**`nya` only**), install under `~/.local` or
`%LOCALAPPDATA%\Programs\NYA`, and on Windows optionally set PATH + `.nya` → `nya open`.
They **complement** (not fully replace) a GUI setup.exe for non-terminal users.

## Manual / local packaging

When GitHub Actions is disabled, build on a Linux amd64 host:

```bash
./scripts/release-local.sh 0.1.0
gh release create v0.1.0 dist/nya-0.1.0-* --title "NYA v0.1.0" --notes-file dist/NOTES-v0.1.0.txt
```

Produces UPX’d **`nya`** for Linux/Windows amd64 and platform tarballs/zips with a single binary.
**Inno `*-setup.exe` still needs a Windows machine** if you ship a GUI installer (see below).

### Windows Inno installer (on Windows)

```bash
# Go — release ships nya.exe only
mkdir -p dist/windows-amd64
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/windows-amd64/nya.exe ./cmd/nya
upx --best --lzma dist/windows-amd64/nya.exe

# Inno Setup 6
ISCC /DMyAppVersion=0.1.0 /DStagingDir=dist\windows-amd64 packaging\windows\nya.iss
# → dist/nya-0.1.0-windows-amd64-setup.exe
```
