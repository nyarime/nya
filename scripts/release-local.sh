#!/usr/bin/env bash
# Local release packaging when GitHub Actions is unavailable.
# Usage: ./scripts/release-local.sh [VERSION]
# Example: ./scripts/release-local.sh 0.1.0
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VER="${1:-0.1.0}"
DIST="$ROOT/dist"
STAGING="$ROOT/staging"

need() { command -v "$1" >/dev/null || { echo "missing: $1" >&2; exit 1; }; }
need go
need upx
need tar
need zip

rm -rf "$STAGING"
mkdir -p "$DIST" \
  "$STAGING/linux-amd64" "$STAGING/linux-arm64" \
  "$STAGING/windows-amd64" \
  "$STAGING/darwin-arm64" "$STAGING/darwin-amd64"

build_go() {
  local goos=$1 goarch=$2 out=$3
  local ext=""
  [[ "$goos" == windows ]] && ext=.exe
  echo "==> go $goos/$goarch"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$out/nya$ext" ./cmd/nya
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$out/nya-get$ext" ./cmd/nya-get
}

build_go linux amd64 "$STAGING/linux-amd64"
build_go linux arm64 "$STAGING/linux-arm64"
build_go windows amd64 "$STAGING/windows-amd64"
build_go darwin arm64 "$STAGING/darwin-arm64"
build_go darwin amd64 "$STAGING/darwin-amd64"

echo "==> UPX (Linux + Windows CLI; skip Darwin / SFX / FM)"
upx --best --lzma \
  "$STAGING/linux-amd64/nya" "$STAGING/linux-amd64/nya-get" \
  "$STAGING/linux-arm64/nya" "$STAGING/linux-arm64/nya-get" \
  "$STAGING/windows-amd64/nya.exe" "$STAGING/windows-amd64/nya-get.exe"

LINUX_AMD64_EXTRAS=()
if command -v cargo >/dev/null; then
  echo "==> Rust linux amd64 (nya-fm + sfx stub)"
  cargo build --release -p nya-fm -p nya-sfx-stub
  cp target/release/nya-fm target/release/nya-sfx-stub "$STAGING/linux-amd64/"
  chmod +x "$STAGING/linux-amd64/nya-fm" "$STAGING/linux-amd64/nya-sfx-stub"
  LINUX_AMD64_EXTRAS+=(nya-fm nya-sfx-stub)

  if command -v x86_64-w64-mingw32-gcc >/dev/null; then
    echo "==> Rust windows amd64 SFX stub (mingw)"
    rustup target add x86_64-pc-windows-gnu >/dev/null
    cargo build --release -p nya-sfx-stub --target x86_64-pc-windows-gnu
    cp target/x86_64-pc-windows-gnu/release/nya-sfx-stub.exe "$STAGING/windows-amd64/"
  else
    echo "!! skip windows SFX stub (install gcc-mingw-w64-x86-64)"
  fi
else
  echo "!! cargo not found — skipping nya-fm / SFX stub"
fi

echo "==> pack"
tar -C "$STAGING/linux-amd64" -czf "$DIST/nya-${VER}-linux-amd64.tar.gz" \
  nya nya-get "${LINUX_AMD64_EXTRAS[@]}"
tar -C "$STAGING/linux-arm64" -czf "$DIST/nya-${VER}-linux-arm64.tar.gz" nya nya-get
tar -C "$STAGING/darwin-arm64" -czf "$DIST/nya-${VER}-darwin-arm64.tar.gz" nya nya-get
tar -C "$STAGING/darwin-amd64" -czf "$DIST/nya-${VER}-darwin-amd64.tar.gz" nya nya-get

WIN_FILES=(nya.exe nya-get.exe)
[[ -f $STAGING/windows-amd64/nya-sfx-stub.exe ]] && WIN_FILES+=(nya-sfx-stub.exe)
( cd "$STAGING/windows-amd64" && zip -q "$DIST/nya-${VER}-windows-amd64.zip" "${WIN_FILES[@]}" )

cat > "$DIST/NOTES-v${VER}.txt" << EOF
NYA ${VER} — local release (see scripts/release-local.sh)

- windows zip: CLI (+ SFX stub if mingw). No Inno setup.exe / nyaFM.exe from Linux hosts.
- linux-amd64: CLI + nyaFM + stub when cargo/GTK deps present
- linux-arm64 / darwin: CLI only
- UPX applied to Go CLI on Linux + Windows only
EOF

echo
echo "Artifacts in $DIST:"
ls -lah "$DIST"/nya-"${VER}"-*
echo
echo "Publish:"
echo "  gh release create v${VER} \$DIST/nya-${VER}-* --title \"NYA v${VER}\" --notes-file \$DIST/NOTES-v${VER}.txt"
