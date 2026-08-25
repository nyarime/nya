package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cmdOpen extracts an archive beside itself into <basename>/ — like macOS
// Archive Utility: double-click game.nya → .\game\. If that folder already
// exists, create "game 2", "game 3", … (Finder style). Pass -overwrite to
// extract into the existing folder instead (files inside are replaced).
func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	password := fs.String("password", "", "archive password")
	workers := fs.Int("workers", 0, "parallel chunk decompression workers (0 = automatic)")
	pause := fs.Bool("pause", false, "wait for Enter before exit (off by default)")
	overwrite := fs.Bool("overwrite", false, "if dest exists, extract into it (overwrite files); default is Finder-style rename")
	destFlag := fs.String("o", "", "override output directory (default: <archive-dir>/<basename>/)")
	fs.Parse(args)
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
	if !*overwrite {
		dest = uniqueOpenDest(dest)
	}

	err = extractTo(abs, dest, *password, *workers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nya: %v\n", err)
		if *pause {
			waitEnter()
		}
		os.Exit(1)
	}
	fmt.Printf("Extracted %s → %s\n", filepath.Base(abs), dest)
	if *pause {
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

// uniqueOpenDest returns path if it does not exist, otherwise "path 2", "path 3", …
// matching macOS Finder / Archive Utility (e.g. config → "config 2").
func uniqueOpenDest(path string) string {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return path
		}
		return path
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	for n := 2; n < 10000; n++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s %d", base, n))
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s %d", base, 10000))
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
