use byteorder::{LittleEndian, ReadBytesExt};
use std::io::{self, Cursor, Read};

use crate::format::{SFX_FOOTER_SIZE, SFX_MAGIC};

#[derive(Debug, Clone, Copy)]
pub struct SfxFooter {
    pub archive_offset: u64,
    pub archive_size: u64,
    pub config_offset: u64,
    pub config_size: u32,
    pub flags: u32,
}

impl SfxFooter {
    pub fn parse_from_file_end(data: &[u8]) -> io::Result<Self> {
        if data.len() < SFX_FOOTER_SIZE {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "file too small for SFX footer",
            ));
        }
        let start = data.len() - SFX_FOOTER_SIZE;
        Self::parse(&data[start..])
    }

    pub fn parse(footer: &[u8]) -> io::Result<Self> {
        if footer.len() < SFX_FOOTER_SIZE {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "short SFX footer",
            ));
        }
        let mut c = Cursor::new(footer);
        let mut magic = [0u8; 8];
        c.read_exact(&mut magic)?;
        if &magic != SFX_MAGIC {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "not an NYA SFX file (bad footer magic)",
            ));
        }
        Ok(Self {
            archive_offset: c.read_u64::<LittleEndian>()?,
            archive_size: c.read_u64::<LittleEndian>()?,
            config_offset: c.read_u64::<LittleEndian>()?,
            config_size: c.read_u32::<LittleEndian>()?,
            flags: c.read_u32::<LittleEndian>()?,
        })
    }

    pub fn slice_archive<'a>(&self, file: &'a [u8]) -> io::Result<&'a [u8]> {
        let start = self.archive_offset as usize;
        let end = start
            .checked_add(self.archive_size as usize)
            .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidData, "archive overflow"))?;
        if end > file.len() {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "archive extends past file end",
            ));
        }
        if self.config_offset != 0 || self.config_size != 0 {
            let cfg_start = self.config_offset as usize;
            let cfg_end = cfg_start + self.config_size as usize;
            if cfg_end > file.len() || cfg_start < end {
                // config should follow archive; tolerate for now
            }
        }
        Ok(&file[start..end])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_footer() {
        let mut f = vec![0u8; 40];
        f[0..8].copy_from_slice(SFX_MAGIC);
        f[8..16].copy_from_slice(&100u64.to_le_bytes());
        f[16..24].copy_from_slice(&5000u64.to_le_bytes());
        let p = SfxFooter::parse(&f).unwrap();
        assert_eq!(p.archive_offset, 100);
        assert_eq!(p.archive_size, 5000);
    }
}
