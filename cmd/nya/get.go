package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/nyarime/nya"
)

func cmdGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	out := fs.String("o", "", "output archive path")
	concurrency := fs.Int("c", 8, "parallel download connections")
	resume := fs.Bool("resume", true, "resume incomplete download")
	url := fs.String("url", "", "download URL (overrides manifest sources; alone enables embed bootstrap)")
	paths := fs.String("paths", "", "comma-separated entry paths for partial fetch")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `nya get — resumable parallel downloader

Usage:
  nya get [flags] <manifest.nyam>
  nya get [flags] --url <URL> <manifest.nyam>
  nya get [flags] --url <https://host/pack.nya>   # single URL (embedded index)

`)
		fs.PrintDefaults()
	}
	if err := parseFlagSet(fs, args, map[string]bool{"o": true, "c": true, "url": true, "paths": true}); err != nil {
		return err
	}

	var m *nya.Manifest
	var statePath string
	var output string
	client := &http.Client{Timeout: 0}

	switch {
	case fs.NArg() == 0 && *url != "":
		fmt.Fprintf(os.Stderr, "nya get: bootstrapping embedded index from %s\n", *url)
		var err error
		m, err = nya.BootstrapManifestFromURL(client, *url)
		if err != nil {
			return err
		}
		output = *out
		if output == "" {
			output = m.Archive.Name
		}
		statePath = nya.StatePath(output + ".nyam")
	case fs.NArg() == 1:
		manifestPath := fs.Arg(0)
		var err error
		m, err = nya.ReadManifest(manifestPath)
		if err != nil {
			return err
		}
		if *url != "" {
			m.Sources = []nya.ManifestSource{{URL: *url, Priority: 10}}
		} else if len(m.Sources) == 0 {
			return fmt.Errorf("manifest has no sources; pass -url")
		}
		output = *out
		if output == "" {
			output = m.Archive.Name
			if !filepath.IsAbs(output) {
				output = filepath.Join(filepath.Dir(manifestPath), output)
			}
		}
		statePath = nya.StatePath(manifestPath)
	default:
		fs.Usage()
		os.Exit(2)
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

	res, err := nya.Download(ctx, nya.DownloadOptions{
		Manifest:    m,
		Output:      output,
		StatePath:   statePath,
		Concurrency: *concurrency,
		Resume:      *resume,
		Paths:       pathList,
		HTTPClient:  client,
		OnBlock: func(b nya.DownloadBlock, done, total int) {
			fmt.Fprintf(os.Stderr, "\rnya get: block %d/%d (%s)", done, total, nya.HumanSize(int(b.Size)))
		},
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}

	fmt.Printf("%s: OK (%d blocks, %s, %s)\n",
		output,
		res.BlocksTotal,
		nya.HumanSize(int(res.BytesWritten)),
		res.Elapsed.Round(time.Millisecond))
	if res.Partial {
		fmt.Fprintf(os.Stderr, "nya get: partial fetch (skipped whole-archive checksum)\n")
	}
	return nil
}
