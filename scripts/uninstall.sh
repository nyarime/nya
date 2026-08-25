#!/usr/bin/env bash
# NYA uninstall (Linux / macOS) — thin wrapper around install.sh --uninstall
#
#   curl -fsSL https://raw.githubusercontent.com/nyarime/nya/main/scripts/uninstall.sh | bash
#   bash uninstall.sh --prefix ~/.local
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
SCRIPT_URL="${NYA_UNINSTALL_URL:-https://raw.githubusercontent.com/nyarime/nya/main/scripts/install.sh}"

if [[ -f "$ROOT/install.sh" ]]; then
  exec bash "$ROOT/install.sh" --uninstall "$@"
fi

# Piped from curl: fetch install.sh and run uninstall mode
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
curl -fsSL "$SCRIPT_URL" -o "$tmp"
exec bash "$tmp" --uninstall "$@"
