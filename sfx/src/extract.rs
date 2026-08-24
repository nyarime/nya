use std::fs::{self, File};
use std::io::{self, Read, Write};
use std::path::{Component, Path, PathBuf};

use crate::format::{ENTRY_DIR, ENTRY_FILE, ENTRY_HARDLINK, ENTRY_SYMLINK};
use crate::nya::{
    decompress_chunk_payload, is_solid, parse_archive, read_chunk, solid_codec, Archive,
};

pub struct Options {
    pub output_dir: PathBuf,
    pub overwrite: bool,
}

pub fn extract_archive(raw: &[u8], opts: &Options) -> io::Result<()> {
    let arch = parse_archive(raw)?;
    if is_solid(&arch.header) {
        extract_solid(&arch, opts)
    } else {
        extract_non_solid(&arch, opts)
    }
}

fn sanitize_path(base: &Path, entry: &str) -> io::Result<PathBuf> {
    let p = Path::new(entry);
    if p.is_absolute() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "absolute paths not allowed",
        ));
    }
    let mut out = base.to_path_buf();
    for comp in p.components() {
        match comp {
            Component::Normal(s) => out.push(s),
            Component::ParentDir => {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidInput,
                    "parent dir escape",
                ))
            }
            Component::CurDir => {}
            _ => {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidInput,
                    "invalid path component",
                ))
            }
        }
    }
    Ok(out)
}

fn write_file(path: &Path, data: &[u8], mode: u32, overwrite: bool) -> io::Result<()> {
    if path.exists() && !overwrite {
        return Err(io::Error::new(
            io::ErrorKind::AlreadyExists,
            format!("{} exists (use -y)", path.display()),
        ));
    }
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    let mut f = File::create(path)?;
    f.write_all(data)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = fs::set_permissions(path, fs::Permissions::from_mode(mode));
    }
    Ok(())
}

fn extract_non_solid(arch: &Archive, opts: &Options) -> io::Result<()> {
    for e in &arch.entries {
        let out = sanitize_path(&opts.output_dir, &e.path)?;
        match e.entry_type {
            ENTRY_DIR => {
                fs::create_dir_all(&out)?;
            }
            ENTRY_SYMLINK => {
                #[cfg(unix)]
                {
                    let _ = fs::remove_file(&out);
                    std::os::unix::fs::symlink(&e.link_target, &out)?;
                }
                #[cfg(not(unix))]
                {
                    eprintln!("nya-sfx: skip symlink {}", e.path);
                }
            }
            ENTRY_HARDLINK => {
                #[cfg(unix)]
                {
                    let target = sanitize_path(&opts.output_dir, &e.link_target)?;
                    fs::hard_link(&target, &out)?;
                }
                #[cfg(not(unix))]
                {
                    eprintln!("nya-sfx: skip hardlink {}", e.path);
                }
            }
            ENTRY_FILE => {
                let mut off = e.first_data_off;
                let mut full = Vec::new();
                for _ in 0..e.chunk_count {
                    let (payload, next) = read_chunk(&arch.data, off)?;
                    let raw = decompress_chunk_payload(e.compression_id, &payload)?;
                    full.extend_from_slice(&raw);
                    off = next;
                }
                if e.original_size as usize <= full.len() {
                    full.truncate(e.original_size as usize);
                }
                write_file(&out, &full, e.mode, opts.overwrite)?;
            }
            _ => {
                eprintln!("nya-sfx: skip entry type {} ({})", e.entry_type, e.path);
            }
        }
    }
    Ok(())
}

fn extract_solid(arch: &Archive, opts: &Options) -> io::Result<()> {
    let (payload, _) = read_chunk(&arch.data, 0)?;
    let codec = solid_codec(&arch.entries);
    let solid = decompress_chunk_payload(codec, &payload)?;

    for e in &arch.entries {
        let out = sanitize_path(&opts.output_dir, &e.path)?;
        match e.entry_type {
            ENTRY_DIR => {
                fs::create_dir_all(&out)?;
            }
            ENTRY_SYMLINK => {
                #[cfg(unix)]
                {
                    let _ = fs::remove_file(&out);
                    std::os::unix::fs::symlink(&e.link_target, &out)?;
                }
            }
            ENTRY_FILE => {
                let start = e.first_data_off as usize;
                let end = (e.first_data_off + e.original_size) as usize;
                if end > solid.len() {
                    return Err(io::Error::new(
                        io::ErrorKind::InvalidData,
                        format!("solid slice past end: {}", e.path),
                    ));
                }
                write_file(&out, &solid[start..end], e.mode, opts.overwrite)?;
            }
            _ => {}
        }
    }
    Ok(())
}

pub fn read_self_and_extract(opts: &Options) -> io::Result<()> {
    let exe = std::env::current_exe()?;
    let mut f = File::open(&exe)?;
    let mut data = Vec::new();
    f.read_to_end(&mut data)?;

    let footer = crate::footer::SfxFooter::parse_from_file_end(&data)?;
    let archive = footer.slice_archive(&data)?;
    extract_archive(archive, opts)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitize_rejects_escape() {
        assert!(sanitize_path(Path::new("/tmp"), "../etc/passwd").is_err());
    }
}
