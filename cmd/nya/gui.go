package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func cmdGUI(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Print(`nya gui — open nyaFM (graphical archive browser)

Usage:
  nya gui [archive.nya]

Looks for nya-fm next to this executable (install layout). A native Go GUI
may replace this launcher later; for now it starts the Rust nyaFM binary.
`)
		return nil
	}

	fm, err := findNyaFM()
	if err != nil {
		return err
	}
	cmd := exec.Command(fm, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func findNyaFM() (string, error) {
	names := []string{"nya-fm"}
	if runtime.GOOS == "windows" {
		names = []string{"nya-fm.exe", "nya-fm"}
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd, filepath.Join(cwd, "target", "release"))
	}
	for _, dir := range dirs {
		for _, name := range names {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath("nya-fm"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("nya-fm not found beside nya (install nya-fm or build: cargo build -p nya-fm --release)")
}
