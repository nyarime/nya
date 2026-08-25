//! nya-fm — minimal NYA File Manager (open / list / extract).
//!
//! Shares the same Rust archive reader as the SFX stub (`nya_archive`).

use eframe::egui;
use nya_archive::format::{ENTRY_DIR, ENTRY_FILE, ENTRY_HARDLINK, ENTRY_SYMLINK};
use nya_archive::{extract_archive, parse_archive, Archive, ExtractOptions};
use std::collections::BTreeSet;
use std::path::PathBuf;

fn main() -> eframe::Result<()> {
    let opts = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_inner_size([920.0, 560.0])
            .with_title("nyaFM"),
        ..Default::default()
    };
    eframe::run_native(
        "nyaFM",
        opts,
        Box::new(|_cc| Ok(Box::new(NyaFmApp::new()))),
    )
}

struct Row {
    path: String,
    entry_type: u8,
    original_size: u64,
    compression_id: u16,
}

struct NyaFmApp {
    archive_path: Option<PathBuf>,
    rows: Vec<Row>,
    status: String,
    extract_dir: PathBuf,
    overwrite: bool,
    filter: String,
    selected: BTreeSet<String>,
}

impl NyaFmApp {
    fn new() -> Self {
        let mut app = Self {
            archive_path: None,
            rows: Vec::new(),
            status: "Open a .nya archive to begin.".into(),
            extract_dir: std::env::current_dir().unwrap_or_else(|_| PathBuf::from(".")),
            overwrite: true,
            filter: String::new(),
            selected: BTreeSet::new(),
        };
        if let Some(path) = std::env::args().nth(1) {
            app.open_path(PathBuf::from(path));
        }
        app
    }

    fn load_rows(arch: &Archive) -> Vec<Row> {
        arch.entries
            .iter()
            .map(|e| Row {
                path: e.path.clone(),
                entry_type: e.entry_type,
                original_size: e.original_size,
                compression_id: e.compression_id,
            })
            .collect()
    }

    fn open_path(&mut self, path: PathBuf) {
        match std::fs::read(&path) {
            Ok(raw) => match parse_archive(&raw) {
                Ok(arch) => {
                    let n = arch.entries.len();
                    self.rows = Self::load_rows(&arch);
                    self.status = format!("Opened {} ({} entries)", path.display(), n);
                    self.archive_path = Some(path);
                    self.selected.clear();
                }
                Err(e) => {
                    self.status = format!("Parse error: {e}");
                    self.rows.clear();
                    self.archive_path = None;
                }
            },
            Err(e) => {
                self.status = format!("Read error: {e}");
                self.rows.clear();
                self.archive_path = None;
            }
        }
    }

    fn open_dialog(&mut self) {
        if let Some(path) = rfd::FileDialog::new()
            .add_filter("NYA archive", &["nya"])
            .add_filter("All", &["*"])
            .pick_file()
        {
            self.open_path(path);
        }
    }

    fn pick_extract_dir(&mut self) {
        if let Some(dir) = rfd::FileDialog::new().pick_folder() {
            self.extract_dir = dir;
        }
    }

    fn extract_all(&mut self) {
        let Some(path) = self.archive_path.clone() else {
            self.status = "No archive open.".into();
            return;
        };
        let raw = match std::fs::read(&path) {
            Ok(b) => b,
            Err(e) => {
                self.status = format!("Read error: {e}");
                return;
            }
        };
        let opts = ExtractOptions {
            output_dir: self.extract_dir.clone(),
            overwrite: self.overwrite,
        };
        match extract_archive(&raw, &opts) {
            Ok(()) => self.status = format!("Extracted to {}", self.extract_dir.display()),
            Err(e) => self.status = format!("Extract failed: {e}"),
        }
    }

    fn kind_label(t: u8) -> &'static str {
        match t {
            ENTRY_FILE => "file",
            ENTRY_DIR => "dir",
            ENTRY_SYMLINK => "symlink",
            ENTRY_HARDLINK => "hardlink",
            _ => "?",
        }
    }

    fn human_size(n: u64) -> String {
        const U: [&str; 5] = ["B", "KB", "MB", "GB", "TB"];
        let mut v = n as f64;
        let mut i = 0;
        while v >= 1024.0 && i < U.len() - 1 {
            v /= 1024.0;
            i += 1;
        }
        if i == 0 {
            format!("{n} {}", U[i])
        } else {
            format!("{v:.1} {}", U[i])
        }
    }

    fn codec_label(id: u16) -> &'static str {
        match id {
            0 => "store",
            1 => "zstd",
            6 => "lzma2",
            _ => "?",
        }
    }
}

impl eframe::App for NyaFmApp {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        egui::TopBottomPanel::top("menu").show(ctx, |ui| {
            egui::menu::bar(ui, |ui| {
                ui.menu_button("File", |ui| {
                    if ui.button("Open…").clicked() {
                        self.open_dialog();
                        ui.close_menu();
                    }
                    if ui.button("Extract all").clicked() {
                        self.extract_all();
                        ui.close_menu();
                    }
                    ui.separator();
                    if ui.button("Quit").clicked() {
                        ctx.send_viewport_cmd(egui::ViewportCommand::Close);
                    }
                });
                ui.menu_button("Help", |ui| {
                    ui.label("nyaFM — Rust GUI for NYA archives");
                    ui.label("Reader shared with nya-sfx-stub");
                });
            });
        });

        egui::TopBottomPanel::bottom("status").show(ctx, |ui| {
            ui.label(&self.status);
        });

        egui::CentralPanel::default().show(ctx, |ui| {
            ui.horizontal(|ui| {
                if ui.button("Open").clicked() {
                    self.open_dialog();
                }
                if ui.button("Extract all").clicked() {
                    self.extract_all();
                }
                ui.checkbox(&mut self.overwrite, "Overwrite");
                ui.separator();
                ui.label("To:");
                ui.monospace(self.extract_dir.display().to_string());
                if ui.button("Browse…").clicked() {
                    self.pick_extract_dir();
                }
            });
            ui.horizontal(|ui| {
                ui.label("Filter:");
                ui.text_edit_singleline(&mut self.filter);
                if let Some(p) = &self.archive_path {
                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                        ui.monospace(p.display().to_string());
                    });
                }
            });
            ui.separator();

            let filter = self.filter.to_lowercase();
            let rows: Vec<&Row> = self
                .rows
                .iter()
                .filter(|r| filter.is_empty() || r.path.to_lowercase().contains(&filter))
                .collect();

            egui::ScrollArea::vertical().show(ui, |ui| {
                egui::Grid::new("entries")
                    .striped(true)
                    .num_columns(4)
                    .min_col_width(72.0)
                    .show(ui, |ui| {
                        ui.strong("Name");
                        ui.strong("Type");
                        ui.strong("Size");
                        ui.strong("Codec");
                        ui.end_row();

                        if rows.is_empty() {
                            ui.label("(no archive loaded)");
                            ui.end_row();
                            return;
                        }

                        for r in rows {
                            let mut checked = self.selected.contains(&r.path);
                            if ui.checkbox(&mut checked, &r.path).changed() {
                                if checked {
                                    self.selected.insert(r.path.clone());
                                } else {
                                    self.selected.remove(&r.path);
                                }
                            }
                            ui.label(Self::kind_label(r.entry_type));
                            ui.label(if r.entry_type == ENTRY_DIR {
                                "—".into()
                            } else {
                                Self::human_size(r.original_size)
                            });
                            ui.label(Self::codec_label(r.compression_id));
                            ui.end_row();
                        }
                    });
            });
        });
    }
}
