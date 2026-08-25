#!/usr/bin/env bash
# Local release packaging when GitHub Actions is unavailable.
# Usage: ./scripts/release-local.sh [VERSION]
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
  "$STAGING/windows-amd64" "$STAGING/windows-arm64" \
  "$STAGING/darwin-arm64" "$STAGING/darwin-amd64"

build_go() {
  local goos=$1 goarch=$2 out=$3
  local ext=""
  [[ "$goos" == windows ]] && ext=.exe
  echo "==> go $goos/$goarch"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$out/nya$ext" ./cmd/nya
}

build_go linux amd64 "$STAGING/linux-amd64"
build_go linux arm64 "$STAGING/linux-arm64"
build_go windows amd64 "$STAGING/windows-amd64"
build_go windows arm64 "$STAGING/windows-arm64"
build_go darwin arm64 "$STAGING/darwin-arm64"
build_go darwin amd64 "$STAGING/darwin-amd64"

echo "==> UPX (nya CLI only; never SFX outputs)"
upx --best --lzma \
  "$STAGING/linux-amd64/nya" \
  "$STAGING/linux-arm64/nya" \
  "$STAGING/windows-amd64/nya.exe"

echo "==> pack (nya only — use nya get for downloads)"
tar -C "$STAGING/linux-amd64" -czf "$DIST/nya-${VER}-linux-amd64.tar.gz" nya
tar -C "$STAGING/linux-arm64" -czf "$DIST/nya-${VER}-linux-arm64.tar.gz" nya
tar -C "$STAGING/darwin-arm64" -czf "$DIST/nya-${VER}-darwin-arm64.tar.gz" nya
tar -C "$STAGING/darwin-amd64" -czf "$DIST/nya-${VER}-darwin-amd64.tar.gz" nya

( cd "$STAGING/windows-amd64" && zip -q "$DIST/nya-${VER}-windows-amd64.zip" nya.exe )
( cd "$STAGING/windows-arm64" && zip -q "$DIST/nya-${VER}-windows-arm64.zip" nya.exe )

cat > "$DIST/NOTES-v${VER}.txt" << EOF
NYA ${VER} — local release (see scripts/release-local.sh)

- Single nya binary: CLI + SFX stub + nya get (no separate nya-get in release)
- Do not UPX SFX outputs (concatenated with archive payload)
EOF

echo
echo "Artifacts in $DIST:"
ls -lah "$DIST"/nya-"${VER}"-*
echo
echo "Publish:"
echo "  gh release create v${VER} \$DIST/nya-${VER}-* --title \"NYA v${VER}\" --notes-file \$DIST/NOTES-v${VER}.txt"
