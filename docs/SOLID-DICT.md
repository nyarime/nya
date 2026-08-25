# Solid ZSTD dictionary (`-dict`)

NYA can embed a shared **Zstandard dictionary** for solid archives when many
members share repeated text (configs, shaders, localization, scripted data).
Use `nya create -solid -dict trained.zdict …` (levels **1–4**, NYA-Zstd).

## When to use `-dict` for game packs

| Good fit | Poor fit |
| --- | --- |
| Many small text assets with shared headers/tokens | Mostly unique binary blobs (textures, audio, compiled meshes) |
| Localization trees, JSON/CSV tables, script sources | Already compressed payloads (PNG, OGG, BCn blocks) |
| Solid mode (`-solid`) so one compressed stream sees cross-file redundancy | Single-file or non-solid archives (dict rarely pays off) |
| Corpus large enough to train a dict (dozens of KiB+ of similar text) | One-off archives or highly heterogeneous content |

**Workflow (MVP):**

1. Collect representative pack files (or a staging directory) with repeated text
   headers / padding (see `solid_dict_test.go`).
2. Train offline: `zstd --train --maxdict=8192 -o pack.zdict assets/ data/ locale/`
3. Build the NYA raw dictionary from shared corpus bytes (NYA tail `0x0006` stores
   **raw** prefix bytes, not the ZSTD-trained `.zdict` file — repeat the shared
   header/token block up to `--maxdict`).
4. Create: `nya create -solid -level 3 -dict pack.rawdict game/ -o game.nya`
5. Extract: `nya extract game.nya` (embedded dict; no `-dict` needed).

Levels **3–4** balance ratio and encode time for distribute/get scenes; see
`docs/BENCHMARK-COMPRESS.md` for LZMA2 solid baselines. Dictionary wins are
most visible on text-heavy trees — see `solid_dict_test.go`.
