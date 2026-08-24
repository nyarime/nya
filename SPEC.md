# NYA archive format

Version 1.1. All integers are little-endian. Offsets are byte offsets from the
start of the file unless stated otherwise.

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
```

`CentralDirOffset` is always `128 + DataAreaSize`; readers should trust the
header field rather than recomputing it, since it is what locates the
recovery section after a truncation.

## Global header

| Offset | Size | Field | Notes |
| ---: | ---: | --- | --- |
| 0 | 8 | Magic | `4E 59 41 00 76 30 31 00` (`NYA\0v01\0`) |
| 8 | 2 | VersionMajor | 1 |
| 10 | 2 | VersionMinor | 1 for archives written by this implementation |
| 12 | 4 | Flags | see below |
| 16 | 8 | DataAreaSize | |
| 24 | 8 | CentralDirOffset | |
| 32 | 8 | CentralDirSize | |
| 40 | 8 | CreationTime | Unix nanoseconds, signed |
| 48 | 8 | TotalOrigSize | sum of all entry sizes before compression |
| 56 | 32 | Blake3 | reserved for a digest over the data area; currently zero |
| 88 | 40 | Reserved | zero |

A reader must reject a file whose magic does not match, and one whose
`VersionMajor` is above 1.

### Flags

| Bit | Name | Meaning |
| ---: | --- | --- |
| 0 | HasFooter | reserved |
| 1 | SolidCompress | the payload is one solid stream |
| 2 | Encrypted | reserved; see "Encryption" |
| 3 | HasGlobalFEC | reserved |

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
| 0 | stored | reserved |
| 1 | Zstandard | default |
| 2 | S2 | reserved |
| 3 | Brotli | reserved |
| 4 | LZ4 | reserved |
| 5 | Zstandard with dictionary | reserved |
| 6 | LZMA2 | selected by `-best` |

Only 1 and 6 are produced and consumed today.

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
12-byte nonce, and the key is `SHA-256(password)`.

Two properties are worth stating plainly:

- There is no salt and no iterated key derivation, so the scheme gives no
  meaningful resistance to offline brute force of a weak password. The header
  has no room to record KDF parameters, so fixing this needs a format change.
- The `Encrypted` flag is not set, so an archive does not advertise that a
  password is required. A reader that is given no password will fail to
  decompress rather than report a clear error.

## Version history

### 1.1

zstd frames follow RFC 8878. Earlier revisions did not, in these respects:

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
This implementation keeps the old tables for archives that report minor
version 0.

### 1.0

Initial format, as shipped inside Nyarc.
