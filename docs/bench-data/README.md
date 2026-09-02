# Bench raw data

Regenerated CSV/JSON from public corpora. Schema:
[BENCHMARK-CORPUS.md](../BENCHMARK-CORPUS.md).

| File | Corpus | Notes |
| --- | --- | --- |
| [silesia-20260825-partial.csv](silesia-20260825-partial.csv) | Silesia (~212 MiB) | nya levels 1/3/4/5 — partial nightly |
| [silesia-20260827.csv](silesia-20260827.csv) | Silesia (~212 MiB) | Full re-run via `scripts/bench-silesia.sh` |
| [silesia-summary.json](silesia-summary.json) | Silesia | Derived metrics for default-level policy |

Regenerate Silesia:

```bash
scripts/bench-silesia.sh
```

Adoption context: [ADOPTION.md](../ADOPTION.md).
