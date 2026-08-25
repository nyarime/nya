#!/usr/bin/env bash
# Fetch Silesia corpus (when possible) and emit raw CSV under docs/bench-data/.
# Does not run in CI (-short). Requires: curl, sha256sum, go, optional xz/7z/zstd.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${NYA_BENCH_CACHE:-${TMPDIR:-/tmp}/nya-bench-cache}"
OUT_DIR="${ROOT}/docs/bench-data"
STAMP="$(date -u +%Y%m%d)"
CSV="${OUT_DIR}/silesia-${STAMP}.csv"

mkdir -p "$CACHE" "$OUT_DIR"

# Canonical mirror (SF / original host rotate; override with NYA_SILESIA_URL).
SILESIA_URL="${NYA_SILESIA_URL:-https://sun.aei.polsl.pl/~sdeor/corpus/silesia.zip}"
ZIP="$CACHE/silesia.zip"
TREE="$CACHE/silesia"

if [[ ! -d "$TREE" ]]; then
  if [[ ! -f "$ZIP" ]]; then
    echo "downloading Silesia → $ZIP" >&2
    curl -fL --retry 3 -o "$ZIP" "$SILESIA_URL" || {
      echo "download failed; set NYA_SILESIA_URL or place silesia.zip at $ZIP" >&2
      exit 1
    }
  fi
  mkdir -p "$TREE"
  unzip -qo "$ZIP" -d "$TREE"
fi

# Optional checksum pin (update when URL changes).
if [[ -n "${NYA_SILESIA_SHA256:-}" ]]; then
  echo "$NYA_SILESIA_SHA256  $ZIP" | sha256sum -c -
fi

echo "corpus,tool,level,mode,raw_bytes,out_bytes,ratio,encode_s,decode_s,notes" >"$CSV"

raw_total=0
while IFS= read -r -d '' f; do
  raw_total=$((raw_total + $(wc -c <"$f")))
done < <(find "$TREE" -type f -print0)

run_nya() {
  local level="$1" mode="$2" extra=("${@:3}")
  local archive outdir t0 t1 enc_s dec_s out_bytes
  archive="$(mktemp "$CACHE/nya-XXXXXX.nya")"
  outdir="$(mktemp -d "$CACHE/out-XXXXXX")"
  t0=$(date +%s.%N)
  (cd "$ROOT" && go run ./cmd/nya create -level "$level" -multi-chunk=false "${extra[@]}" "$archive" "$TREE")
  t1=$(date +%s.%N)
  enc_s=$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.3f", b-a}')
  out_bytes=$(wc -c <"$archive")
  t0=$(date +%s.%N)
  (cd "$ROOT" && go run ./cmd/nya extract "$archive" "$outdir")
  t1=$(date +%s.%N)
  dec_s=$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.3f", b-a}')
  ratio=$(awk -v o="$out_bytes" -v r="$raw_total" 'BEGIN{printf "%.4f", o/r}')
  echo "silesia,nya,$level,$mode,$raw_total,$out_bytes,$ratio,$enc_s,$dec_s," >>"$CSV"
  rm -rf "$archive" "$outdir"
}

echo "raw_total=$raw_total bytes" >&2

# Level sweep focused on default-strategy decision (zstd 1–4 vs LZMA2 5).
run_nya 1 zstd
run_nya 3 zstd
run_nya 4 zstd
run_nya 5 lzma2

if command -v zstd >/dev/null; then
  tar_c="$(mktemp "$CACHE/silesia-XXXXXX.tar")"
  tar -C "$TREE" -cf "$tar_c" .
  for lv in 1 3 19; do
    t0=$(date +%s.%N)
    zstd -"$lv" -f -o "${tar_c}.zst" "$tar_c"
    t1=$(date +%s.%N)
    enc_s=$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.3f", b-a}')
    out_bytes=$(wc -c <"${tar_c}.zst")
    t0=$(date +%s.%N)
    zstd -d -f -o "${tar_c}.out" "${tar_c}.zst"
    t1=$(date +%s.%N)
    dec_s=$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.3f", b-a}')
    ratio=$(awk -v o="$out_bytes" -v r="$raw_total" 'BEGIN{printf "%.4f", o/r}')
    echo "silesia,zstd,$lv,tar,$raw_total,$out_bytes,$ratio,$enc_s,$dec_s,tar+zstd" >>"$CSV"
  done
  rm -f "$tar_c" "${tar_c}.zst" "${tar_c}.out"
fi

echo "wrote $CSV" >&2
