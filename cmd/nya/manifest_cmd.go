package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nyarime/nya"
)

const manifestUsage = `nya manifest — manage download index (embedded and/or .nyam sidecar)

Subcommands:
  nya manifest add  [flags] <archive.nya>   upsert embedded download index
  nya manifest del  <archive.nya>           remove embedded download index
  nya manifest export [flags] <archive.nya> write .nyam sidecar only (no embed change)

Protocol notes:
  add  — strip any existing index, then write tail 0x0001 + NYADIDX1 footer (idempotent upsert)
  del  — truncate to archive body; clear FlagHasDownloadIndex (idempotent if absent)
  export — JSON sidecar for CDN/tools; if the archive is already embedded, exports that index

Legacy (still accepted):
  nya manifest [--embed|--embed-only] [-o out.nyam] [--url URL] <archive.nya>
`

func cmdManifest(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, manifestUsage)
		return fmt.Errorf("manifest needs a subcommand: add, del, or export")
	}

	switch args[0] {
	case "add":
		return cmdManifestAdd(args[1:])
	case "del", "delete", "rm", "remove":
		return cmdManifestDel(args[1:])
	case "export", "write":
		return cmdManifestExport(args[1:])
	case "-h", "--help", "help":
		fmt.Print(manifestUsage)
		return nil
	default:
		// Legacy flat form for compatibility.
		return cmdManifestLegacy(args)
	}
}

func cmdManifestAdd(args []string) error {
	fs := flag.NewFlagSet("manifest add", flag.ExitOnError)
	blockSize := fs.String("block-size", "4m", "transport block size (e.g. 4m, 1m)")
	out := fs.String("o", "", "also write a .nyam sidecar at this path")
	url := fs.String("url", "", "source URL to record in the sidecar (-o)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: nya manifest add [flags] <archive.nya>

Upsert the embedded download index (overwrite if already present).
Optional -o writes a matching .nyam sidecar for CDN tooling.

`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("manifest add needs one archive path")
	}
	archive := fs.Arg(0)

	bs, err := nya.ParseBlockSize(*blockSize)
	if err != nil {
		return err
	}
	had, _ := nya.HasEmbeddedDownloadIndex(archive)
	res, err := nya.EmbedDownloadIndex(archive, nya.EmbedOptions{BlockSize: bs, InPlace: true})
	if err != nil {
		return err
	}
	action := "added"
	if had {
		action = "updated"
	}
	fmt.Printf("%s: download index %s (%d body blocks, tail @ %d, final %s)\n",
		res.Path, action, res.BlockCount, res.TailChainOffset, nya.HumanSize(int(res.FinalSize)))

	if *out != "" {
		return writeSidecarFromEmbedded(archive, *out, *url)
	}
	return nil
}

func cmdManifestDel(args []string) error {
	fs := flag.NewFlagSet("manifest del", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: nya manifest del <archive.nya>

Remove the embedded download index (equivalent to creating with -no-embed).
Idempotent if no index is present.

`)
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("manifest del needs one archive path")
	}
	archive := fs.Arg(0)
	res, err := nya.StripDownloadIndex(archive)
	if err != nil {
		return err
	}
	if !res.HadIndex {
		fmt.Printf("%s: no embedded download index\n", archive)
		return nil
	}
	fmt.Printf("%s: download index removed (body %s)\n", archive, nya.HumanSize(int(res.BodySize)))
	return nil
}

func cmdManifestExport(args []string) error {
	fs := flag.NewFlagSet("manifest export", flag.ExitOnError)
	out := fs.String("o", "", "output .nyam path (default: <archive>.nyam)")
	blockSize := fs.String("block-size", "4m", "block size when building from a non-embedded archive")
	url := fs.String("url", "", "download URL recorded in sources")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: nya manifest export [flags] <archive.nya>

Write a .nyam sidecar without changing the archive.
If the archive already has an embedded index, the sidecar matches that index.

`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("manifest export needs one archive path")
	}
	archive := fs.Arg(0)
	outPath := *out
	if outPath == "" {
		outPath = strings.TrimSuffix(archive, filepath.Ext(archive)) + ".nyam"
	}

	had, err := nya.HasEmbeddedDownloadIndex(archive)
	if err != nil {
		return err
	}
	if had {
		return writeSidecarFromEmbedded(archive, outPath, *url)
	}

	bs, err := nya.ParseBlockSize(*blockSize)
	if err != nil {
		return err
	}
	var sources []nya.ManifestSource
	for _, u := range strings.Split(*url, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			sources = append(sources, nya.ManifestSource{URL: u, Priority: 1})
		}
	}
	m, err := nya.BuildManifest(archive, bs, sources...)
	if err != nil {
		return err
	}
	if err := nya.WriteManifest(m, outPath); err != nil {
		return err
	}
	fmt.Printf("%s: %d blocks x %s, archive %s (%s), %d file entries\n",
		outPath,
		len(m.Download.Blocks),
		nya.HumanSize(int(m.Download.BlockSize)),
		m.Archive.Name,
		nya.HumanSize(int(m.Archive.Size)),
		len(m.Entries))
	return nil
}

func writeSidecarFromEmbedded(archive, outPath, url string) error {
	m, err := nya.ManifestFromEmbeddedFile(archive, "")
	if err != nil {
		return err
	}
	m.Sources = nil
	for _, u := range strings.Split(url, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			m.Sources = append(m.Sources, nya.ManifestSource{URL: u, Priority: 1})
		}
	}
	if err := nya.WriteManifest(m, outPath); err != nil {
		return err
	}
	fmt.Printf("%s: %d blocks x %s, archive %s (%s) (from embedded index)\n",
		outPath,
		len(m.Download.Blocks),
		nya.HumanSize(int(m.Download.BlockSize)),
		m.Archive.Name,
		nya.HumanSize(int(m.Archive.Size)))
	return nil
}

func cmdManifestLegacy(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ExitOnError)
	out := fs.String("o", "", "output .nyam path (default: archive.nyam)")
	blockSize := fs.String("block-size", "4m", "transport block size")
	url := fs.String("url", "", "download URL")
	embed := fs.Bool("embed", false, "deprecated: use 'manifest add'")
	embedOnly := fs.Bool("embed-only", false, "deprecated: use 'manifest add'")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("manifest needs an archive path (or: manifest add|del|export …)")
	}
	archive := fs.Arg(0)

	if *embedOnly {
		fmt.Fprintln(os.Stderr, "nya: warning: --embed-only is deprecated; use: nya manifest add", archive)
		return cmdManifestAdd([]string{"-block-size", *blockSize, archive})
	}
	if *embed {
		fmt.Fprintln(os.Stderr, "nya: warning: --embed is deprecated; use: nya manifest add [-o …]")
		addArgs := []string{"-block-size", *blockSize}
		if *out != "" {
			addArgs = append(addArgs, "-o", *out)
		}
		if *url != "" {
			addArgs = append(addArgs, "-url", *url)
		}
		addArgs = append(addArgs, archive)
		return cmdManifestAdd(addArgs)
	}

	fmt.Fprintln(os.Stderr, "nya: warning: prefer 'nya manifest export'; legacy form still works")
	expArgs := []string{"-block-size", *blockSize}
	if *out != "" {
		expArgs = append(expArgs, "-o", *out)
	}
	if *url != "" {
		expArgs = append(expArgs, "-url", *url)
	}
	expArgs = append(expArgs, archive)
	return cmdManifestExport(expArgs)
}
