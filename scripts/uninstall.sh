#!/usr/bin/env bash
# NYA uninstall (Linux / macOS)
#
#   curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.sh | bash
#   bash uninstall.sh --prefix ~/.local
#
# Removes files installed by install.sh under the same prefix:
#   $PREFIX/bin/nya  nya-get  nya-fm  nya-sfx-stub
#   $PREFIX/share/nya/
set -euo pipefail

PREFIX="${NYA_PREFIX:-$HOME/.local}"

usage() {
  cat <<'EOF'
NYA uninstall (Linux / macOS)

  curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.sh | bash
  bash uninstall.sh --prefix ~/.local

Options:
  --prefix DIR   install root used by install.sh (default: ~/.local)
  -h, --help
EOF
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) PREFIX="$2"; shift 2 ;;
    -h|--help) usage ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

BIN_DIR="$PREFIX/bin"
SHARE_DIR="$PREFIX/share/nya"

echo "Uninstalling NYA"
echo "  prefix: $PREFIX"

removed=0
for f in nya nya-get nya-fm nya-sfx-stub; do
  if [[ -e "$BIN_DIR/$f" ]]; then
    rm -f "$BIN_DIR/$f"
    echo "  removed $BIN_DIR/$f"
    removed=1
  fi
done
if [[ -d "$SHARE_DIR" ]]; then
  rm -rf "$SHARE_DIR"
  echo "  removed $SHARE_DIR"
  removed=1
fi

if [[ "$removed" -eq 0 ]]; then
  echo "  nothing found under this prefix"
else
  echo "Done."
fi
