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
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$out/nya-get$ext" ./cmd/nya-get
  # Go SFX stub (same codecs as nya). Never UPX — stub is concatenated with archive.
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$out/nya-sfx-stub$ext" ./cmd/nya-sfx-stub
}

build_go linux amd64 "$STAGING/linux-amd64"
build_go linux arm64 "$STAGING/linux-arm64"
build_go windows amd64 "$STAGING/windows-amd64"
build_go windows arm64 "$STAGING/windows-arm64"
build_go darwin arm64 "$STAGING/darwin-arm64"
build_go darwin amd64 "$STAGING/darwin-amd64"

echo "==> UPX (CLI only; never SFX stub)"
upx --best --lzma \
  "$STAGING/linux-amd64/nya" "$STAGING/linux-amd64/nya-get" \
  "$STAGING/linux-arm64/nya" "$STAGING/linux-arm64/nya-get" \
  "$STAGING/windows-amd64/nya.exe" "$STAGING/windows-amd64/nya-get.exe"

LINUX_AMD64_EXTRAS=(nya-sfx-stub)
if command -v cargo >/dev/null; then
  echo "==> Rust linux amd64 nya-fm"
  cargo build --release -p nya-fm
  cp target/release/nya-fm "$STAGING/linux-amd64/"
  chmod +x "$STAGING/linux-amd64/nya-fm"
  LINUX_AMD64_EXTRAS+=(nya-fm)
else
  echo "!! cargo not found — skipping nya-fm"
fi

echo "==> pack"
tar -C "$STAGING/linux-amd64" -czf "$DIST/nya-${VER}-linux-amd64.tar.gz" \
  nya nya-get "${LINUX_AMD64_EXTRAS[@]}"
tar -C "$STAGING/linux-arm64" -czf "$DIST/nya-${VER}-linux-arm64.tar.gz" nya nya-get nya-sfx-stub
tar -C "$STAGING/darwin-arm64" -czf "$DIST/nya-${VER}-darwin-arm64.tar.gz" nya nya-get nya-sfx-stub
tar -C "$STAGING/darwin-amd64" -czf "$DIST/nya-${VER}-darwin-amd64.tar.gz" nya nya-get nya-sfx-stub

( cd "$STAGING/windows-amd64" && zip -q "$DIST/nya-${VER}-windows-amd64.zip" nya.exe nya-get.exe nya-sfx-stub.exe )
( cd "$STAGING/windows-arm64" && zip -q "$DIST/nya-${VER}-windows-arm64.zip" nya.exe nya-get.exe nya-sfx-stub.exe )

# Seed repo stubs layout for local nya sfx without -stub
mkdir -p sfx/stubs
cp "$STAGING/linux-amd64/nya-sfx-stub" "sfx/stubs/nya-sfx-stub_linux_amd64" 2>/dev/null || true

cat > "$DIST/NOTES-v${VER}.txt" << EOF
NYA ${VER} — local release (see scripts/release-local.sh)

- Go SFX stub (cmd/nya-sfx-stub) on all platforms — same NYA-Zstd/LZMA2 as nya
- Do not UPX the SFX stub (concatenated with archive payload)
- windows/linux/darwin archives include nya-sfx-stub
- linux-amd64 also includes nya-fm when cargo is available
EOF

echo
echo "Artifacts in $DIST:"
ls -lah "$DIST"/nya-"${VER}"-*
echo
echo "Publish:"
echo "  gh release create v${VER} \$DIST/nya-${VER}-* --title \"NYA v${VER}\" --notes-file \$DIST/NOTES-v${VER}.txt"
