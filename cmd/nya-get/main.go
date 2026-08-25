// Command nya-get downloads NYA archives using .nyam manifests with parallel HTTP Range.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/nyarime/nya"
)

const usage = `nya-get — resumable parallel downloader for NYA archives

Usage:
  nya-get [flags] <manifest.nyam>
  nya-get [flags] --url <URL> <manifest.nyam>

Flags:
  -o path       output .nya path (default: from manifest)
  -c N          parallel connections (default: 8)
  --resume      resume from .state file (default: true)
  --no-resume   start fresh
  -url URL      override/add download URL
  --paths list  partial fetch: comma-separated entry paths (uses manifest chunk ranges)

Examples:
  nya-get -c 16 GamePack.nyam
  nya-get --paths "data/big.bin" GamePack.nyam
  nya-get -o GamePack.nya --url https://cdn.example.com/GamePack.nya GamePack.nyam
`

func main() {
	fs := flag.NewFlagSet("nya-get", flag.ExitOnError)
	out := fs.String("o", "", "output archive path")
	concurrency := fs.Int("c", 8, "parallel download connections")
	resume := fs.Bool("resume", true, "resume incomplete download")
	url := fs.String("url", "", "download URL (overrides manifest sources)")
	paths := fs.String("paths", "", "comma-separated entry paths for partial fetch")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}

	manifestPath := fs.Arg(0)
	m, err := nya.ReadManifest(manifestPath)
	if err != nil {
		fatal(err)
	}

	if *url != "" {
		m.Sources = []nya.ManifestSource{{URL: *url, Priority: 10}}
	} else if len(m.Sources) == 0 {
		fatal(fmt.Errorf("manifest has no sources; pass -url"))
	}

	output := *out
	if output == "" {
		output = m.Archive.Name
		if !filepath.IsAbs(output) {
			output = filepath.Join(filepath.Dir(manifestPath), output)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var pathList []string
	for _, p := range strings.Split(*paths, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			pathList = append(pathList, p)
		}
	}

	statePath := nya.StatePath(manifestPath)
	res, err := nya.Download(ctx, nya.DownloadOptions{
		Manifest:    m,
		Output:      output,
		StatePath:   statePath,
		Concurrency: *concurrency,
		Resume:      *resume,
		Paths:       pathList,
		OnBlock: func(b nya.DownloadBlock, done, total int) {
			fmt.Fprintf(os.Stderr, "\rnya-get: block %d/%d (%s)", done, total, nya.HumanSize(int(b.Size)))
		},
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("%s: OK (%d blocks, %s, %s)\n",
		output,
		res.BlocksTotal,
		nya.HumanSize(int(res.BytesWritten)),
		res.Elapsed.Round(1))
	if res.Partial {
		fmt.Fprintf(os.Stderr, "nya-get: partial fetch (skipped whole-archive checksum)\n")
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "nya-get:", err)
	os.Exit(1)
}
