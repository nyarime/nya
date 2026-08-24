//! NYA container constants (SPEC.md v1).

pub const MAGIC: &[u8; 8] = b"NYA\0v01\0";
pub const GLOBAL_HEADER_SIZE: usize = 128;
pub const CHUNK_HDR: usize = 32;

pub const COMPRESS_NONE: u16 = 0;
pub const COMPRESS_ZSTD: u16 = 1;
pub const COMPRESS_LZMA2: u16 = 6;

pub const FLAG_SOLID: u32 = 1 << 1;

pub const ENTRY_FILE: u8 = 0;
pub const ENTRY_DIR: u8 = 1;
pub const ENTRY_SYMLINK: u8 = 2;
pub const ENTRY_HARDLINK: u8 = 3;

pub const DIR_ENTRY_V2: u8 = 2;

pub const SFX_MAGIC: &[u8; 8] = b"NYASFX01";
pub const SFX_FOOTER_SIZE: usize = 40;
