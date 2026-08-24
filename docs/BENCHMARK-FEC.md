# FEC 损坏恢复对比说明

本文档说明 NYA 格式的前向纠错（FEC）能力，并与 7-Zip、WinRAR 的恢复机制对比。实测数据来自 `fec_damage_test.go`（运行 `go test -run FECMax -v ./...`）。

## 结论摘要

| 格式 | 内置恢复数据 | 典型冗余 | 损坏类型 | 事后追加恢复 |
|------|-------------|---------|---------|-------------|
| **NYA** | `-fec N%` 可选 | 0–50%+ | 擦除 + 随机损坏（BLAKE3 符号哈希辅助） | `nya augment -fec N` |
| **7-Zip (.7z)** | 无标准 FEC | — | 无；仅依赖 solid 块内 LZMA2 局部容错 | 不支持 |
| **WinRAR (.rar)** | Recovery Record | 约 1–8%，最大 ~10% | 主要面向擦除/截断 | 创建时可加 `-rrN%`，事后需重建 |

NYA 的优势在于：**恢复比例可配置**（`-fec 3` / `-fec 30` / `-fec 50`），大 payload（≥4 MiB 压缩后或 solid）自动切换 **Leopard-RS**，小文件用 **Hybrid（LDPC + RaptorQ）**；且 **`nya augment`** 可在归档创建后追加恢复数据（含 Leopard-RS 与 `-fec 0` 初始无 FEC 的归档）。

## NYA `-fec` 与可恢复比例

`-fec N` 表示恢复数据约占**压缩 payload** 的 N%（不是原始文件大小）。

### Leopard-RS（大 payload / solid）

当压缩后 payload ≥ 4 MiB，或启用 `-solid` 时，默认 hybrid 规划会优先选用 Leopard-RS（`FECType=3`）。

实测（5 MiB store solid，擦除式损坏：将压缩 payload 前 N% 字节置零）：

| 创建时 `-fec` | 归档体积增幅（约） | 可恢复擦除上限（约） |
|--------------|-------------------|---------------------|
| 3% | +~3% | ≈ 3% |
| 10% | +~10% | ≈ 8–10% |
| 30% | +~30% | ≈ 25–30% |
| 50% | +~50% | ≈ 45–50% |

Leopard-RS 在**擦除**场景下接近理论上限：丢失的分片数 ≤ 校验分片数即可重建。随机 bit 翻转会被 BLAKE3 符号哈希识别为“坏分片”，等效于擦除，因此 Hybrid/Leopard 均支持一定比例的**随机损坏**，而非仅连续截断。

### Hybrid（小 payload，< 4 MiB 压缩后）

256 KiB 量级 payload，`-fec-type hybrid`（默认）：

| `-fec` | 可恢复擦除（约） |
|--------|-----------------|
| 10% | ~8–15% |
| 30% | ~25–35% |
| 50% | ~40–55% |

RaptorQ 喷泉码在超过标称冗余时仍有一定概率恢复，因此 Hybrid 在极端情况下可能略超 `-fec` 标称百分比（取决于损坏模式与分块）。

## 与 7-Zip 对比

- **7z 格式**没有标准化的 recovery record 或 FEC 字段。
- 损坏 central directory 或压缩流通常导致**整包不可用**。
- NYA 额外提供 **global metadata FEC**（`-fec > 0` 时写入），用于修复 central directory + hash table 区域。
- 若需对比体积：同等 payload 下，7z 无 `-fec` 时体积最小；NYA `-fec 0` 与 7z 接近，`-fec 10` 约大 10%。

本地对比命令示例（需安装 `7z`）：

```bash
# 准备 5MB 随机数据
dd if=/dev/urandom of=/tmp/payload.bin bs=1M count=5

# NYA
nya create -fec 10 -level 5 /tmp/test.nya /tmp/payload.bin
ls -la /tmp/test.nya

# 7-Zip（无 recovery）
7z a -mx=5 /tmp/test.7z /tmp/payload.bin
ls -la /tmp/test.7z
```

## 与 WinRAR 对比

- **RAR5** 支持创建时添加 Recovery Record（`-rr[N]` 或 `-rr[N%]`），典型 3–10%，**事后无法**对已有归档追加（需重新打包）。
- WinRAR 对**多字节文件名**使用 UTF-8（RAR5 规范）；旧 RAR4 在部分 locale 下可能出现乱码。
- **NYA** 规范要求 central directory 路径为 **UTF-8**（见 `SPEC.md`），与 WinRAR RAR5 一致；`TestChineseUTF8Paths` 覆盖中文及 emoji 路径 roundtrip。
- **`nya augment`** 等价于 WinRAR 缺失的“事后加 recovery record”能力，且支持 Leopard-RS 全量重算 parity tail。

```bash
# WinRAR（若已安装，示例）
rar a -rr10 /tmp/test.rar /tmp/payload.bin

# NYA 事后追加
nya create -fec 5 /tmp/test.nya /tmp/payload.bin
nya augment -fec 5 /tmp/test.nya /tmp/test_more.nya   # 现为 ~10%
```

## 中文文件名

- NYA **始终**以 UTF-8 存储路径，不依赖系统 code page。
- 在 Windows 上解压时，Go 运行时与 NYA 工具链使用 UTF-8 API；与 WinRAR RAR5 行为对齐。
- 旧 ZIP 工具（GBK/OEM）仍可能乱码——这是 ZIP 生态问题，不是 NYA 格式限制。

## 运行 benchmark / 损坏 sweep

```bash
# 损坏恢复上限（较慢，完整 sweep）
go test -run 'FECMax|FECRecovery|Chinese' -v ./...

# 快速 CI（跳过 sweep）
go test -short ./...

# augment 回归
go test -run Augment -v ./...
```

## `nya augment` 支持范围（v1.1+）

| 场景 | 支持 |
|------|------|
| `-fec 0` 创建后首次加 FEC | ✅ |
| Leopard-RS（大/solid payload） | ✅ 全量重算 parity |
| Hybrid / RaptorQ / LDPC | ✅ |
| `-solid` 归档 | ✅ chunk header 在 data area offset 0 |
| 追加后 extract / verify | ✅ |

不支持或需注意：

- Central directory 因 augment 变大时需**重建归档**（当前实现保留原 CD 槽位大小）。
- 加密归档 augment 未单独测试（parity 对密文仍有效，但需相同密码 extract）。
