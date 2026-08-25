package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// cmdOpen extracts an archive beside itself into <basename>/ — the behaviour
// used by Windows file association (double-click game.nya → .\game\).
func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	password := fs.String("password", "", "archive password")
	workers := fs.Int("workers", 0, "parallel chunk decompression workers (0 = automatic)")
	pause := fs.Bool("pause", runtime.GOOS == "windows", "wait for Enter before exit (default on Windows)")
	noPause := fs.Bool("no-pause", false, "never wait for Enter (useful in scripts)")
	destFlag := fs.String("o", "", "override output directory (default: <archive-dir>/<basename>/)")
	fs.Parse(args)
	doPause := *pause && !*noPause
	if fs.NArg() != 1 {
		return fmt.Errorf("open needs one archive path (e.g. game.nya)")
	}

	archive := fs.Arg(0)
	abs, err := filepath.Abs(archive)
	if err != nil {
		return err
	}
	dest := *destFlag
	if dest == "" {
		dest = defaultOpenDest(abs)
	}

	err = extractTo(abs, dest, *password, *workers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nya: %v\n", err)
		if doPause {
			waitEnter()
		}
		os.Exit(1)
	}
	fmt.Printf("Extracted %s → %s\n", filepath.Base(abs), dest)
	if doPause {
		waitEnter()
	}
	return nil
}

func defaultOpenDest(archiveAbs string) string {
	dir := filepath.Dir(archiveAbs)
	base := filepath.Base(archiveAbs)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = base
	}
	return filepath.Join(dir, name)
}

func extractTo(archive, dest, password string, workers int) error {
	r, err := openOrPasswordHint(archive, password)
	if err != nil {
		return err
	}
	if workers > 0 {
		r.SetWorkers(workers)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return r.Extract(dest)
}

func waitEnter() {
	fmt.Fprint(os.Stderr, "Press Enter to close…")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
}
