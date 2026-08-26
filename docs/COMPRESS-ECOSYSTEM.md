# Compression stack (`nyarime/compress`)

NYA does not embed all codecs in the GPL tree forever. **FEC** and **compression**
follow the same pattern: small Apache libraries, NYA orchestrates.

## Repository

| Repo | License | Contents |
| --- | --- | --- |
| [nyarime/compress](https://github.com/nyarime/compress) | Apache-2.0 | House zstd + LZMA2 (migrated from nya v0.1.0) |
| [nyarime/GoFEC](https://github.com/nyarime/GoFEC) | Apache-2.0 | RaptorQ, LDPC, Leopard-RS |
| [nyarime/nya](https://github.com/nyarime/nya) | GPL-3.0 | Archive, CLI, FEC wiring |

## Zstd + LZMA2 = house (`compress`)

NYA-Zstd and NYA-LZMA2 live in **`github.com/nyarime/compress`**:

- **zstd/** — in-house RFC 8878 (studied klauspost/libzstd; not a permanent wrapper)
- **lzma2/** — in-house raw LZMA2 encoder + LZMA/XZ decode helpers
- **NYA** imports compress via `codec_bridge.go`; on-disk CompressionID unchanged

Migration **v0.1.0** landed; encoder ratio work ships as **v0.2.x** tags.

### Recent LZMA2 releases (Aug 2026)

| Tag | Highlights |
| --- | --- |
| v0.2.3 | Dual-encode optimal/greedy min-size guard at level 9 |
| v0.2.4 | Cached length price tables for optimal DP |
| v0.2.5 | BT4 tree navigation; optimal all-rep scan |
| v0.2.6 | JSON/log dense match cut-offs in optimal parse |

Benchmark harness: [compress/docs/BENCHMARK-7Z.md](https://github.com/nyarime/compress/blob/main/docs/BENCHMARK-7Z.md).

## License hygiene

- `compress` is Apache-2.0; `NOTICE` must list klauspost/compress (BSD + mixed sub-licenses)
- NYA GPL binary linking compress is **allowed**; distribute NYA under GPL with third-party notices
- Do not copy klauspost **source** into lzma2; use as benchmark/reference only

## NYA dependency

```go
require (
    github.com/nyarime/compress v0.2.6
    github.com/nyarime/gofec/v2 v2.0.0
)
```

Codec files removed from `nya` root package; `codec_bridge.go` re-exports for internal callers.
