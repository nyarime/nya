//! NYA self-extracting archive stub (extract-only). See SPEC-SFX.md.

use nya_archive::extract;
use std::path::PathBuf;

fn main() {
    if let Err(e) = run() {
        eprintln!("nya-sfx: {e}");
        std::process::exit(1);
    }
}

fn run() -> std::io::Result<()> {
    let args: Vec<String> = std::env::args().collect();
    let mut out = PathBuf::from(".");
    let mut overwrite = false;
    let mut archive_path: Option<PathBuf> = None;

    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "-o" | "--output" => {
                i += 1;
                out = PathBuf::from(args.get(i).ok_or_else(|| {
                    std::io::Error::new(std::io::ErrorKind::InvalidInput, "missing -o path")
                })?);
            }
            "-y" | "--overwrite" => overwrite = true,
            "-h" | "--help" => {
                print_help();
                return Ok(());
            }
            other if other.starts_with('-') => {
                return Err(std::io::Error::new(
                    std::io::ErrorKind::InvalidInput,
                    format!("unknown flag {other}"),
                ));
            }
            path => {
                if archive_path.is_some() {
                    return Err(std::io::Error::new(
                        std::io::ErrorKind::InvalidInput,
                        "only one archive path allowed",
                    ));
                }
                archive_path = Some(PathBuf::from(path));
            }
        }
        i += 1;
    }

    let opts = extract::Options {
        output_dir: out,
        overwrite,
    };

    if let Some(path) = archive_path {
        let data = std::fs::read(path)?;
        return extract::extract_archive(&data, &opts);
    }

    extract::read_self_and_extract(&opts)
}

fn print_help() {
    eprintln!(
        "nya-sfx-stub — NYA self-extracting archive\n\
         \n\
         Usage:\n\
           nya-sfx-stub                    extract embedded archive to current dir\n\
           nya-sfx-stub -o DIR -y          extract to DIR, overwrite files\n\
           nya-sfx-stub -o DIR -y pack.nya extract a plain .nya (dev/test)\n"
    );
}
