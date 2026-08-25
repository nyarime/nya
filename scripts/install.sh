#!/usr/bin/env bash
# NYA one-click install / uninstall (Linux + macOS).
#
# Install (user, no sudo):
#   curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.sh | bash
#
# Uninstall:
#   curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.sh | bash
#   # or: bash install.sh --uninstall
#
# Options:
#   --prefix DIR     install root (default: ~/.local)
#   --version VER    e.g. 0.1.6 or v0.1.6 (default: latest release)
#   --uninstall      remove binaries installed under prefix
#   --yes            reserved (non-interactive)
set -euo pipefail

REPO="${NYA_REPO:-nyarime/nya}"
PREFIX="${NYA_PREFIX:-$HOME/.local}"
VERSION="${NYA_VERSION:-}"
UNINSTALL=0

usage() {
  cat <<'EOF'
NYA install script (Linux / macOS)

  curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.sh | bash
  bash install.sh --uninstall
  bash install.sh --prefix /usr/local --version 0.1.6
EOF
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) PREFIX="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    --yes|-y) shift ;;
    -h|--help) usage ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

BIN_DIR="$PREFIX/bin"
SHARE_DIR="$PREFIX/share/nya"

need() { command -v "$1" >/dev/null || { echo "missing dependency: $1" >&2; exit 1; }; }
need curl
need tar
need uname

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  *) echo "unsupported OS: $os (use scripts/install.ps1 on Windows)" >&2; exit 1 ;;
esac

latest_tag() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1
}

uninstall() {
  echo "Removing NYA from $BIN_DIR and $SHARE_DIR"
  rm -f "$BIN_DIR/nya" "$BIN_DIR/nya-get" "$BIN_DIR/nya-fm" "$BIN_DIR/nya-sfx-stub"
  rm -rf "$SHARE_DIR"
  echo "Done."
}

if [[ "$UNINSTALL" -eq 1 ]]; then
  uninstall
  exit 0
fi

if [[ -z "$VERSION" ]]; then
  tag=$(latest_tag)
  [[ -n "$tag" ]] || { echo "could not resolve latest release" >&2; exit 1; }
else
  tag="$VERSION"
fi
ver="${tag#v}"
asset="nya-${ver}-${os}-${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/v${ver}/${asset}"

echo "Installing NYA v${ver} (${os}/${arch})"
echo "  from: $url"
echo "  into: $BIN_DIR"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
if ! curl -fsSL "$url" -o "$tmpdir/$asset"; then
  echo "download failed: $url" >&2
  exit 1
fi

mkdir -p "$BIN_DIR" "$SHARE_DIR"
tar -C "$tmpdir" -xzf "$tmpdir/$asset"
for f in nya nya-get nya-fm nya-sfx-stub; do
  if [[ -f "$tmpdir/$f" ]]; then
    install -m 755 "$tmpdir/$f" "$BIN_DIR/$f"
  fi
done
if [[ -f "$BIN_DIR/nya-sfx-stub" ]]; then
  mkdir -p "$SHARE_DIR/sfx/stubs"
  cp "$BIN_DIR/nya-sfx-stub" "$SHARE_DIR/sfx/stubs/nya-sfx-stub_${os}_${arch}"
fi

echo
echo "Installed:"
command -v ls >/dev/null && ls -la "$BIN_DIR"/nya "$BIN_DIR"/nya-get 2>/dev/null || true
[[ -x "$BIN_DIR/nya-fm" ]] && ls -la "$BIN_DIR/nya-fm" || true

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    echo
    echo "Add to PATH:"
    echo "  export PATH=\"$BIN_DIR:\$PATH\""
    ;;
esac

echo
echo "Uninstall:"
echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/uninstall.sh | bash"
if [[ "$PREFIX" != "$HOME/.local" ]]; then
  echo "  # or: bash uninstall.sh --prefix $PREFIX"
fi
