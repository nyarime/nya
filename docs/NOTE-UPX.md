# UPX vs NYA on executables

## Is UPX open source?

Yes. [UPX](https://upx.github.io/) / [upx/upx](https://github.com/upx/upx)
ships full source under **GPLv2+**, optionally with **special exceptions**
that allow free use of *UPX-compressed binaries* (including commercial apps).
Read their `LICENSE` and `COPYING` before copying code or bundling their
stub. Studying algorithms and reimplementing ideas under NYA’s GPL-3.0 is
the usual open-source path; do not assume “GPL project ⇒ paste freely.”

## Why our ELF rows look like 100%

On the published README corpora, **xz, 7z, and zstd also sit near 100%**.
A general compressor sees a flat byte stream. When the ELF is already dense
(stripped release binary, embedded compressed resources, crypto, etc.),
LZ family codecs cannot invent redundancy. Slight **>100%** is normal:
block headers / frame framing cost more than they save.

UPX’s 50–70% claims are measured on **normal, compressible programs**, using
a pipeline that NYA’s default archive path does not run.

## What UPX actually does (learnable layers)

Rough stack (simplified):

1. **Format parse** — ELF/PE/… headers, sections, imports, relocations
2. **Code filters** — call/jump absolute→relative style transforms (related to,
   but richer than, 7-Zip-style BCJ)
3. **Codec** — historically NRV (UCL), also LZMA; tuned for instruction streams
4. **Runtime stub** — tiny loader reconstitutes the image in memory so the
   *packed file* replaces the original executable on disk

NYA today roughly has (2) in weak form (BCJ) and (3) as Zstd/LZMA2 on the
whole file. It does **not** do (1) deeply or (4) as part of compression
(SFX stub is for self-extracting *archives*, different job).

## What is realistic to learn

| Idea | Fit for NYA | Notes |
| --- | --- | --- |
| Section-aware filter on `.text` only | High | ELF, **PE (.exe)**, **Mach-O** — see [SPEC-FILTER-EXEC.md](SPEC-FILTER-EXEC.md) |
| Skip / store dense segments | High | Aligns with `PayloadDense`; avoids >100% bloat |
| Stronger matchers on code windows | Medium | Same CompressionID; software-only |
| Ship a UPX-like pack+stub for single ELF | Low / separate product | Conflicts with “bit-identical extract”; AV/policy issues |
| Vendoring UPX/UCL as-is | Careful | License + maintenance; usually reimplement |

## Product takeaway

Beating UPX on a single ELF is **not** the FEC + distribution wedge.
Treat executable-aware filtering as a **mid-term encoder improvement** for
binaries *inside* `.nya` packs. Keep README honest: incompressible ELF ≈
parity with xz/7z/zstd; UPX is a different tool class.

See [ROADMAP.md](../ROADMAP.md).
