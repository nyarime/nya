// Command nya-sfx-stub is the reference self-extractor for NYA SFX files.
// It uses the same codecs as `nya` (NYA-Zstd / NYA-LZMA2), unlike the
// experimental Rust stub which only interoperates with store / tiny frames.
//
// Default behaviour (double-click / bare run): extract the embedded archive
// into the directory that contains this executable (beside the .exe), same
// idea as macOS Archive Utility expanding a zip next to itself.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nyarime/nya"
)

func main() {
	fs := flag.NewFlagSet("nya-sfx-stub", flag.ExitOnError)
	out := fs.String("o", "", "output directory (default: folder containing this executable)")
	overwrite := fs.Bool("y", false, "overwrite existing files (Extract always replaces files)")
	_ = overwrite // Extract overwrites; flag kept for CLI compatibility with Rust stub
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `nya-sfx-stub — NYA self-extracting archive

Usage:
  nya-sfx-stub                 extract embedded archive beside this executable
  nya-sfx-stub -o DIR          extract to DIR
  nya-sfx-stub -o DIR pack.nya extract a plain .nya (dev/test)

`)
		fs.PrintDefaults()
	}
	args := rearrange(os.Args[1:])
	_ = fs.Parse(args)

	dest := *out
	if dest == "" {
		exe, err := os.Executable()
		if err != nil {
			fatal(err)
		}
		dest = filepath.Dir(exe)
	}

	var archive []byte
	var err error
	if fs.NArg() == 1 {
		archive, err = os.ReadFile(fs.Arg(0))
		if err != nil {
			fatal(err)
		}
	} else if fs.NArg() > 1 {
		fatal(fmt.Errorf("only one archive path allowed"))
	} else {
		exe, err := os.Executable()
		if err != nil {
			fatal(err)
		}
		archive, err = nya.SliceSFXArchive(exe)
		if err != nil {
			fatal(err)
		}
	}

	r, err := nya.OpenReaderAt(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		fatal(err)
	}
	if err := r.Extract(dest); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "Extracted to %s\n", dest)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "nya-sfx: %v\n", err)
	os.Exit(1)
}

// rearrange allows `stub pack.nya -o dir` ordering.
func rearrange(args []string) []string {
	known := map[string]bool{"o": true}
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			flags = append(flags, a)
			continue
		}
		flags = append(flags, a)
		if known[name] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, pos...)
}
