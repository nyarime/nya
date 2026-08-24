mod extract;
mod footer;
mod format;
mod nya;

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
                let data = std::fs::read(path)?;
                return extract::extract_archive(
                    &data,
                    &extract::Options {
                        output_dir: out,
                        overwrite,
                    },
                );
            }
        }
        i += 1;
    }

    extract::read_self_and_extract(&extract::Options {
        output_dir: out,
        overwrite,
    })
}

fn print_help() {
    eprintln!(
        "nya-sfx-stub — NYA self-extracting archive\n\
         \n\
         Usage:\n\
           nya-sfx-stub              extract embedded archive to current dir\n\
           nya-sfx-stub -o DIR       extract to DIR\n\
           nya-sfx-stub -y           overwrite existing files\n\
           nya-sfx-stub pack.nya     extract a plain .nya (dev/test)\n"
    );
}
