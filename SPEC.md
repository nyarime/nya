# NYA archive format

Version **1.1** (major 1, minor 1). All integers are little-endian. Offsets are
byte offsets from the start of the file unless stated otherwise.

Long-term compatibility rules: **[COMPATIBILITY.md](COMPATIBILITY.md)**.

Extension foundation (tails, solid groups, dedup, profiles):
**[SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md)**. Codec roles:
**[SPEC-CODECS.md](SPEC-CODECS.md)**.

## Compatibility policy (summary)

- **`.nya` is the only archive container.** `.nyam` is an optional download
  sidecar, not a second archive format.
- **`VersionMajor = 1` is the LTS line.** Readers reject `Major > 1`.
- **Minor 0 archives remain readable forever** (legacy zstd tables).
- **New writers emit minor 1** (RFC 8878 zstd). See [Version history](#version-history).
- Breaking layout changes require **major 2**, not a new file extension.

## File layout

```
+---------------------------+
| Global Header   128 bytes |
+---------------------------+
| Data Area                 |  DataAreaSize bytes
+---------------------------+
| Central Directory         |  CentralDirSize bytes, at CentralDirOffset
+---------------------------+
| Recovery Section          |  uint32 length, then that many bytes
+---------------------------+
| Symbol Hash Table         |  uint32 count, then count * uint32
+---------------------------+
| Tail Chain (optional)     |  at `Reserved.TailChainOffset`; see SPEC-EXTENSIONS.md
+---------------------------+
| Archive Footer (optional) |  if `FlagHasFooter`
+---------------------------+
```

`CentralDirOffset` is always `128 + DataAreaSize`; readers should trust the
header field rather than recomputing it, since it is what locates the
recovery section after a truncation.

When the header reserved region records a non-zero tail chain offset, optional
extension records (download index, dedup map, solid groups, sessions) follow
the symbol hash table. Readers that do not implement an extension MUST skip
unknown tail records per [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md).

## Global header

| Offset | Size | Field | Notes |
| ---: | ---: | --- | --- |
| 0 | 8 | Magic | `4E 59 41 00 76 30 31 00` (`NYA\0v01\0`) |
| 8 | 2 | VersionMajor | 1 |
| 10 | 2 | VersionMinor | **1** for new archives; **0** = legacy zstd (still readable) |
| 12 | 4 | Flags | see below |
| 16 | 8 | DataAreaSize | |
| 24 | 8 | CentralDirOffset | |
| 32 | 8 | CentralDirSize | |
| 40 | 8 | CreationTime | Unix nanoseconds, signed |
| 48 | 8 | TotalOrigSize | sum of all entry sizes before compression |
| 56 | 32 | Blake3 | reserved for a digest over the data area; currently zero |
| 88 | 40 | Reserved | see [SPEC-EXTENSIONS.md](SPEC-EXTENSIONS.md) (tail chain, session, profile) |

A reader must reject a file whose magic does not match, and one whose
`VersionMajor` is above 1.

### Flags

| Bit | Name | Meaning |
| ---: | --- | --- |
| 0 | HasFooter | reserved |
| 1 | SolidCompress | the payload is one solid stream |
| 2 | Encrypted | reserved; see "Encryption" |
| 3 | HasGlobalFEC | reserved |
| 4 | HasDownloadIndex | reserved; embedded transport index (see SPEC-DOWNLOAD.md) |

## Data area

### Non-solid archives

Each file entry contributes one chunk:

```
+------------------------+
| Chunk Header  32 bytes |
+------------------------+
| Compressed payload     |  CompressedSize bytes
+------------------------+
```

`DirEntry.FirstDataOff` is the offset of the chunk header relative to the
start of the data area.

The payload is a sequence of independently compressed blocks, each preceded
by its length:

```
uint32 blockLength
blockLength bytes of a zstd frame or a raw LZMA2 stream
```

Blocks cover 512 KiB of input each. When the entry is encrypted, the whole
payload — every length prefix and block — is encrypted as one unit, so the
block structure is only visible after decryption.

### Solid archives

The data area holds a single chunk covering every file concatenated in
central directory order. `DirEntry.FirstDataOff` is then an offset into the
**decompressed** stream, and `OriginalSize` its length there.

### Chunk header

| Offset | Size | Field | Notes |
| ---: | ---: | --- | --- |
| 0 | 8 | OriginalSize | uncompressed bytes |
| 8 | 8 | CompressedSize | bytes that follow the header |
| 16 | 4 | RepairCount | parity symbols generated for this chunk |
| 20 | 4 | SymbolSize | parity symbol size, 1024 |
| 24 | 8 | Blake3Short | first 8 bytes of BLAKE3-256 over the payload |

`Blake3Short` covers the payload exactly as stored, that is after compression
and after encryption.

## Central directory

```
uint64 entryCount
entryCount * DirEntry
```

### Directory entry, version 2

| Size | Field | Notes |
| ---: | --- | --- |
| 1 | Version | 2 |
| 2 | PathLength | |
| n | Path | slash-separated, relative, UTF-8 |
| 1 | EntryType | see below |
| 4 | Mode | Go `os.FileMode` bits |
| 8 | MTimeNano | Unix nanoseconds, signed |
| 8 | OriginalSize | |
| 4 | ChunkCount | always 1 |
| 2 | CompressionID | see below |
| 1 | FECType | see below |
| 1 | BCJFilter | see below |
| 16 | FECParams | four uint32: K, symbol size, percentage, reserved |
| 8 | FirstDataOff | |
| 4 | Uid | |
| 4 | Gid | |
| 2+n | UserName | uint16 length prefix |
| 2+n | GroupName | uint16 length prefix |
| 2+n | LinkTarget | symlink target or hardlink path |
| 4 | DevMajor | device nodes only |
| 4 | DevMinor | device nodes only |
| 4 | XattrCount | |
| … | Xattrs | `uint16 keyLen, key, uint32 valLen, val` repeated |

Version 1 entries omit the version byte and everything from `Uid` onwards. A
reader distinguishes them by the leading byte: version 2 always starts with
`0x02`, whereas in version 1 that byte is the low half of `PathLength`.

Paths are relative and must not escape the destination. A reader must reject
absolute paths and any component that resolves outside the extraction root,
and must not follow an existing symlink when writing.

### Entry types

| Value | Type |
| ---: | --- |
| 0 | regular file |
| 1 | directory |
| 2 | symbolic link |
| 3 | hard link |
| 4 | character device |
| 5 | block device |
| 6 | FIFO |

### Compression IDs

| Value | Codec | Status |
| ---: | --- | --- |
| 0 | stored | level 0 |
| 1 | NYA-Zstd (RFC 8878) | levels 1–4; planned default — see [SPEC-CODECS.md](SPEC-CODECS.md) |
| 2 | S2 | reserved |
| 3 | Brotli | reserved |
| 4 | LZ4 | reserved |
| 5 | NYA-Zstd with dictionary | reserved |
| 6 | NYA-LZMA2 (raw stream) | levels 5–9, `--best` |

Only 0, 1 and 6 are produced and consumed today. The ID is recorded per
entry, so a reader must not assume one codec for a whole archive. Encoder
details and level defaults: [SPEC-CODECS.md](SPEC-CODECS.md).

### FEC types

| Value | Code |
| ---: | --- |
| 0 | none |
| 1 | RaptorQ |
| 2 | LDPC |
| 3 | Reed-Solomon |

The writer always records RaptorQ. When the recovery percentage is zero no
parity symbols are stored, and `RepairCount` falls back to the nominal K.

### BCJ filters

| Value | Architecture |
| ---: | --- |
| 0 | none |
| 1 | x86 (E8/E9 relative to absolute) |
| 2 | ARM (BL) |
| 3 | AArch64 (BL) |
| 4 | MIPS (JAL) |

The writer picks a filter from the ELF `e_machine` field when present, and
otherwise from instruction pattern frequencies. In solid mode it compresses
the stream both with and without the filter and keeps the smaller result.

## Recovery section

```
uint32 length
length bytes of RaptorQ parity symbols
```

Symbols are 1024 bytes. Each chunk of `K * 1024` payload bytes, with K = 32,
produces `K * percentage / 100` parity symbols, concatenated in chunk order.

## Symbol hash table

```
uint32 count
count * uint32
```

Each value is the first four bytes of the BLAKE3-256 digest of one 1024-byte
source symbol. Repair uses the table to tell intact symbols from damaged ones
before handing the survivors to the RaptorQ decoder, which is what lets
recovery work on corruption rather than only on erasures.

## Encryption

When a password is supplied the compressed payload of every chunk is sealed
with AES-256-GCM. The stored form is `nonce || ciphertext || tag` with a
12-byte nonce.

### v1.2 (minor version 2, recommended)

- `FlagEncrypted` and `FlagKDFArgon2id` are set in the global header.
- `GlobalHeader.Reserved` bytes 0–25 hold KDF parameters:
  - `[0:16]` random salt
  - `[16:20]` Argon2id memory (KiB), little-endian uint32
  - `[20:24]` Argon2id time iterations, little-endian uint32
  - `[24]` parallelism (uint8)
  - `[25]` KDF version (currently `1`)
- Key = `Argon2id(password, salt, time, memory, threads)` → 32 bytes.

Readers **must** reject opening encrypted archives when no password is supplied.

### Legacy (minor 0–1, pre-v1.2 writers)

- Key = `SHA-256(password)` with no salt.
- `FlagEncrypted` was not set; callers had to know a password might be required.
- Still supported for read when a password is supplied.

## Version history

### 1.1 (minor version 1)

Current writer output. Zstandard frames follow RFC 8878. Readers select
conformant sequence code tables when `VersionMinor >= 1`.

### 1.0 (minor version 0)

Minor version 0 zstd frames use non-conformant sequence code tables. In these
respects they differ from RFC 8878:

- Literal section headers misread `Size_Format`: `01` and `10` were swapped
  for raw and RLE literals, and for Huffman-coded literals `00` was treated
  as four streams instead of one.
- The Literals_Length and Match_Length baseline tables were missing the
  entries for codes 19 and 35, shifting every later code by one slot.
- Huffman-coded literals were written as a forward bitstream rather than the
  backward one the format requires, and canonical codes were assigned
  shortest-first instead of longest-first.
- The Huffman decoding table gave each symbol `2^nbBits` slots instead of
  `2^(tableLog-nbBits)`.
- Repeated offset updates overwrote the third slot when the second was
  selected.

Because the encoder and decoder shared those mistakes, 1.0 archives are
self-consistent; they are simply not readable by other zstd implementations.
This implementation keeps the old tables for archives that report **minor
version 0** (`zstd_legacy.go`). Repack an archive to move it to 1.1.

Initial public container layout, as shipped inside Nyarc before the standalone
`nyarime/nya` repository.
