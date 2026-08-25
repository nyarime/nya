# Public corpus benchmark plan

Goal: publish **reproducible** compression + FEC numbers on real corpora, with
**raw data** checked in or attached to releases — not only README tables.

Related: [BENCHMARK-COMPRESS.md](BENCHMARK-COMPRESS.md),
[BENCHMARK-FEC.md](BENCHMARK-FEC.md), [BENCHMARK-MULTICHUNK.md](BENCHMARK-MULTICHUNK.md),
[../ROADMAP.md](../ROADMAP.md).

## Target corpora

| Name | Approx size | License / source | Priority |
| --- | --- | --- | --- |
| **Silesia** | ~200 MiB set | [silesia compression corpus](http://sun.aei.polsl.pl/~sdeor/index.php?page=silesia) | P0 |
| **enwik9** | 1 GiB | Large Wikipedia dump (Hutter Prize) | P0 |
| Game asset tree | tens–hundreds of MiB | Internal / redistributable pack only | P1 |
| Firmware / rootfs image | tens–hundreds of MiB | Public device image or synthetic rootfs | P1 |

Do **not** commit multi-gigabyte blobs into git. Prefer:

1. Scripts that download known URLs + checksums
2. Raw **result** CSV/JSON under `docs/bench-data/`
3. Optional release assets for large result dumps

## Metrics to publish (every run)

For each corpus × tool/level:

| Column | Notes |
| --- | --- |
| `corpus` | id |
| `tool` | `nya`, `xz`, `7z`, `zstd`, … |
| `level` | nya `-level`, or tool equivalent |
| `mode` | solid / multi-chunk / fec% |
| `raw_bytes` | input size |
| `out_bytes` | archive or stream size |
| `ratio` | out/raw |
| `encode_s` / `decode_s` | wall time |
| `fec_recover` | optional damage matrix row id |

## Suggested commands

```bash
# Compression suite (writes docs when NYA_BENCH_WRITE=1)
NYA_BENCH_WRITE=1 go test -run TestREADMEBenchmarkSuite -timeout 60m -v ./...

# FEC recovery matrix
go test -run TestFECRecoveryMatrix -timeout 30m -v ./...

# Multi-chunk parallel
go test -run TestMultiChunkParallelReport -timeout 30m -v ./...
```

Corpus-specific harnesses (TODO):

- `scripts/bench-silesia.sh` — fetch, checksum, run nya vs xz/7z/zstd, emit CSV
- `scripts/bench-enwik9.sh` — same for enwik9 (long timeout)
- Level sweep **1–4 (zstd)** vs **5–9 (LZMA2)** decode latency (feeds default-level decision)

## Raw data layout (planned)

```text
docs/bench-data/
  README.md           # schema + how to regenerate
  silesia-YYYYMMDD.csv
  enwik9-YYYYMMDD.csv
  fec-matrix-YYYYMMDD.csv
```

Until the first public run lands, treat this file as the contract for what
“public raw data” means.

## Default-level decision input

Roadmap asks whether `LevelDefault` should move from **5 (LZMA2)** toward
**3–4 (zstd)** for distribute/get scenes. Corpus benches must report
**decode_s** on enwik9 / game / firmware-like payloads before flipping the
default in `level.go`.
