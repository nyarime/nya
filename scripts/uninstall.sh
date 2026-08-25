#!/usr/bin/env bash
# NYA uninstall (Linux / macOS)
#
#   curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.sh | bash
#   bash uninstall.sh --prefix ~/.local
#
# Removes binaries installed by install.sh under the same prefix
# (default: ~/.local → ~/.local/bin/nya, ~/.local/share/nya).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
SCRIPT_URL="${NYA_UNINSTALL_URL:-https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.sh}"

usage() {
  cat <<'EOF'
NYA uninstall (Linux / macOS)

  curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.sh | bash
  bash uninstall.sh --prefix ~/.local

Options:
  --prefix DIR   same prefix used at install (default: ~/.local)
  -h, --help
EOF
  exit 0
}

ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage ;;
    *) ARGS+=("$1"); shift ;;
  esac
done

if [[ -f "$ROOT/install.sh" ]]; then
  exec bash "$ROOT/install.sh" --uninstall "${ARGS[@]}"
fi

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
curl -fsSL "$SCRIPT_URL" -o "$tmp"
exec bash "$tmp" --uninstall "${ARGS[@]}"
