# NYA adoption readiness

This checklist tracks what external teams need before depending on NYA in
production (CDN pipelines, game patchers, firmware OTA, internal mirrors).

Product wedge: **compress once, ship over unreliable links, survive damage —
same file.** See [ROADMAP.md](../ROADMAP.md).

## Adoption gates

| Gate | Status | Evidence |
| --- | --- | --- |
| **Format LTS contract** | Done | [COMPATIBILITY.md](../COMPATIBILITY.md), [docs/V1-FREEZE.md](V1-FREEZE.md) |
| **Public corpus benches** | Partial | [docs/bench-data/](bench-data/) — Silesia landed; enwik9 script TODO |
| **Single-URL get/send story** | Done | [SPEC-DOWNLOAD.md](../SPEC-DOWNLOAD.md), `nya get` / `nya send` |
| **Resume + multi-source get** | Done | `.nyam.state` + `manifest_blake3`; per-block failover across `sources[]` |
| **Distribute create profile** | Done | `nya create -profile distribute` (level 3 zstd, embed index, multi-chunk) |
| **Install paths** | Done | `install.sh` / `install.ps1`, GitHub Releases |
| **Package managers** | Templates | [packaging/homebrew](../packaging/homebrew/nya.rb), [packaging/winget](../packaging/winget/nyarime.nya.yaml) |
| **CI pipeline template** | Done | [examples/ci-distribute.yml](../examples/ci-distribute.yml) |
| **GUI** | Deferred | CLI-first; `nya open` for double-click extract (no Rust GUI in tree) |
| **External production users** | Open | Need 1–2 public or named case studies |

## Recommended pipeline (B2B distribute)

```bash
# 1. Pack for CDN / get (zstd fast decode, embedded index, multi-chunk)
nya create -profile distribute -fec 10 pack.nya ./GameData/

# 2. Publish archive + optional sidecar
nya manifest export pack.nya -o pack.nyam --url https://cdn.example.com/pack.nya

# 3. Client fetch (parallel Range, resume, repair if needed)
nya get --url https://cdn.example.com/pack.nyam
nya repair pack.nya          # if transport or storage damaged parity blocks
```

Quick share (temporary tunnel, not CDN SLA):

```bash
nya send ./GameData/         # auto-pack + TryCloudflare when cloudflared is installed
```

## Compression default policy

| Command | Default level | Rationale |
| --- | ---: | --- |
| `nya create` | 5 (LZMA2) | General archive / backup — best ratio lane |
| `nya create -profile distribute` | 3 (NYA-Zstd) | Fast decompress for CDN and game clients |
| `nya send` (auto pack) | 3 (NYA-Zstd) | Time-first; `-level 9` for smallest |

Silesia corpus (212 MiB, 2026-08-25): NYA level 3 **decode ~1.4× faster** than
level 5 with ~47% larger output — acceptable for distribute-first workloads.
Raw CSV: [bench-data/silesia-20260827.csv](bench-data/silesia-20260827.csv),
summary: [bench-data/silesia-summary.json](bench-data/silesia-summary.json).

## Before you ship minor 3 archives

Multi-chunk (format **1.3**) is on by default for non-solid files > 4 MiB.
Readers built for ≤ v1.2 **reject the whole archive**. Use
`-multi-chunk=false` when an old pinned reader must open the file.

## Licensing

- CLI / format reference: [GPL-3.0](../LICENSE)
- Proprietary embed: dual license — **nyarime@naixi.net**

## Still on the roadmap (not adoption blockers)

- enwik9 + game/firmware corpus CSV in `docs/bench-data/`
- `nya get` partial fetch polish for multi-chunk entry paths (mid-term)
- Optional native GUI (out of tree; not a release dependency)
- NyaFS mount tools (v1.x software)
- Hosted relay / persistent send (optional product; not required for CDN adopters)

Update this file when a gate moves from Open → Done.
