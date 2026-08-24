use byteorder::{LittleEndian, ReadBytesExt};
use std::io::{self, Cursor, Read};

use crate::format::{
    CHUNK_HDR, COMPRESS_LZMA2, COMPRESS_NONE, COMPRESS_ZSTD, DIR_ENTRY_V2, ENTRY_DIR,
    ENTRY_FILE, FLAG_SOLID, GLOBAL_HEADER_SIZE, MAGIC,
};

#[derive(Debug, Clone)]
pub struct GlobalHeader {
    pub version_major: u16,
    pub version_minor: u16,
    pub flags: u32,
    pub data_area_size: u64,
    pub central_dir_offset: u64,
    pub central_dir_size: u64,
}

#[derive(Debug, Clone)]
pub struct DirEntry {
    pub path: String,
    pub entry_type: u8,
    pub mode: u32,
    pub original_size: u64,
    pub chunk_count: u32,
    pub compression_id: u16,
    pub first_data_off: u64,
    pub link_target: String,
}

#[derive(Debug)]
pub struct Archive {
    pub header: GlobalHeader,
    pub entries: Vec<DirEntry>,
    pub data: Vec<u8>,
}

fn read_len_str(r: &mut Cursor<&[u8]>) -> io::Result<String> {
    let n = r.read_u16::<LittleEndian>()? as usize;
    let mut buf = vec![0u8; n];
    r.read_exact(&mut buf)?;
    String::from_utf8(buf).map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))
}

pub fn parse_archive(raw: &[u8]) -> io::Result<Archive> {
    if raw.len() < GLOBAL_HEADER_SIZE {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "truncated global header",
        ));
    }
    let mut h = Cursor::new(&raw[..GLOBAL_HEADER_SIZE]);
    let mut magic = [0u8; 8];
    h.read_exact(&mut magic)?;
    if &magic != MAGIC {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "not a NYA archive",
        ));
    }
    let header = GlobalHeader {
        version_major: h.read_u16::<LittleEndian>()?,
        version_minor: h.read_u16::<LittleEndian>()?,
        flags: h.read_u32::<LittleEndian>()?,
        data_area_size: h.read_u64::<LittleEndian>()?,
        central_dir_offset: h.read_u64::<LittleEndian>()?,
        central_dir_size: h.read_u64::<LittleEndian>()?,
    };
    if header.version_major > 1 {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "unsupported NYA major version",
        ));
    }
    let das = header.data_area_size as usize;
    if GLOBAL_HEADER_SIZE + das > raw.len() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "truncated data area",
        ));
    }
    let data = raw[GLOBAL_HEADER_SIZE..GLOBAL_HEADER_SIZE + das].to_vec();

    let cd_start = header.central_dir_offset as usize;
    if cd_start + 8 > raw.len() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "truncated central directory",
        ));
    }
    let mut cd = Cursor::new(&raw[cd_start..]);
    let entry_count = cd.read_u64::<LittleEndian>()? as usize;
    if entry_count > 10_000_000 {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "absurd entry count",
        ));
    }

    let mut entries = Vec::with_capacity(entry_count.min(1024));
    for _ in 0..entry_count {
        entries.push(read_dir_entry(&mut cd)?);
    }

    Ok(Archive {
        header,
        entries,
        data,
    })
}

fn read_dir_entry(r: &mut Cursor<&[u8]>) -> io::Result<DirEntry> {
    let version = r.read_u8()?;
    if version != DIR_ENTRY_V2 {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            format!("unsupported dir entry version {version}"),
        ));
    }
    let path = read_len_str(r)?;
    let entry_type = r.read_u8()?;
    let mode = r.read_u32::<LittleEndian>()?;
    let _mtime = r.read_i64::<LittleEndian>()?;
    let original_size = r.read_u64::<LittleEndian>()?;
    let chunk_count = r.read_u32::<LittleEndian>()?;
    let compression_id = r.read_u16::<LittleEndian>()?;
    let _fec_type = r.read_u8()?;
    let _bcj = r.read_u8()?;
    let mut _fec = [0u8; 16];
    r.read_exact(&mut _fec)?;
    let first_data_off = r.read_u64::<LittleEndian>()?;
    let _uid = r.read_u32::<LittleEndian>()?;
    let _gid = r.read_u32::<LittleEndian>()?;
    let _user = read_len_str(r)?;
    let _group = read_len_str(r)?;
    let link_target = read_len_str(r)?;
    let _dev_major = r.read_u32::<LittleEndian>()?;
    let _dev_minor = r.read_u32::<LittleEndian>()?;
    let xattr_count = r.read_u32::<LittleEndian>()?;
    for _ in 0..xattr_count {
        let _key = read_len_str(r)?;
        let vlen = r.read_u32::<LittleEndian>()? as usize;
        if vlen > 65536 {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "xattr too large",
            ));
        }
        let mut skip = vec![0u8; vlen];
        r.read_exact(&mut skip)?;
    }
    Ok(DirEntry {
        path,
        entry_type,
        mode,
        original_size,
        chunk_count,
        compression_id,
        first_data_off,
        link_target,
    })
}

pub fn decompress_chunk_payload(comp_id: u16, comp_data: &[u8]) -> io::Result<Vec<u8>> {
    let mut out = Vec::new();
    let mut pos = 0usize;
    while pos + 4 <= comp_data.len() {
        let block_len = u32::from_le_bytes(comp_data[pos..pos + 4].try_into().unwrap()) as usize;
        pos += 4;
        if pos + block_len > comp_data.len() {
            break;
        }
        let block = &comp_data[pos..pos + block_len];
        pos += block_len;
        let raw = match comp_id {
            COMPRESS_NONE => block.to_vec(),
            COMPRESS_ZSTD => zstd::decode_all(block).map_err(|e| {
                io::Error::new(io::ErrorKind::InvalidData, format!("zstd: {e}"))
            })?,
            COMPRESS_LZMA2 => {
                let mut decoder = io::Cursor::new(block);
                let mut buf = Vec::new();
                lzma_rs::lzma2_decompress(&mut decoder, &mut buf).map_err(|e| {
                    io::Error::new(io::ErrorKind::InvalidData, format!("lzma2: {e}"))
                })?;
                buf
            }
            _ => {
                return Err(io::Error::new(
                    io::ErrorKind::Unsupported,
                    format!("unsupported compression id {comp_id}"),
                ))
            }
        };
        out.extend_from_slice(&raw);
    }
    Ok(out)
}

pub fn read_chunk(data: &[u8], off: u64) -> io::Result<(Vec<u8>, u64)> {
    let o = off as usize;
    if o + CHUNK_HDR > data.len() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "chunk header past data area",
        ));
    }
    let mut c = Cursor::new(&data[o..]);
    let _orig = c.read_u64::<LittleEndian>()?;
    let comp_size = c.read_u64::<LittleEndian>()?;
    let repair_count = c.read_u32::<LittleEndian>()?;
    let symbol_size = c.read_u32::<LittleEndian>()?;
    let _blake = c.read_u64::<LittleEndian>()?;
    let comp_size = comp_size as usize;
    let payload_start = o + CHUNK_HDR;
    let payload_end = payload_start + comp_size;
    if payload_end > data.len() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "chunk payload truncated",
        ));
    }
    let payload = data[payload_start..payload_end].to_vec();
    let next = off
        + CHUNK_HDR as u64
        + comp_size as u64
        + (repair_count as u64 * symbol_size as u64);
    Ok((payload, next))
}

pub fn is_solid(header: &GlobalHeader) -> bool {
    header.flags & FLAG_SOLID != 0
}

pub fn solid_codec(entries: &[DirEntry]) -> u16 {
    entries
        .iter()
        .find(|e| e.entry_type == ENTRY_FILE)
        .map(|e| e.compression_id)
        .unwrap_or(COMPRESS_ZSTD)
}
