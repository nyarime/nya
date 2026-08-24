// Command nya creates, inspects and extracts NYA archives.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nyarime/nya"
)

const usage = `nya — Nyarime Archive

Usage:
  nya create  [flags] <archive.nya> <path>   create an archive from a file or directory
  nya list    <archive.nya>                  list archive contents
  nya extract [flags] <archive.nya> [dir]    extract an archive (default: current directory)
  nya verify  <archive.nya>                  check stored BLAKE3 digests
  nya info    <archive.nya>                  show header details
  nya repair  <archive.nya> [out.nya]        rebuild a damaged archive using its FEC data

Archives use LZMA2 by default. Pass "-codec zstd" to create for a much
faster decompressor at the cost of a bigger archive.

Run "nya <command> -h" for the flags of a command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "create", "c":
		err = cmdCreate(os.Args[2:])
	case "list", "l":
		err = cmdList(os.Args[2:])
	case "extract", "x":
		err = cmdExtract(os.Args[2:])
	case "verify", "t":
		err = cmdVerify(os.Args[2:])
	case "info":
		err = cmdInfo(os.Args[2:])
	case "repair":
		err = cmdRepair(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "nya: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "nya:", err)
		os.Exit(1)
	}
}

func cmdCreate(args []string) error {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	level := fs.Int("level", 9, "compression level, 1 (fastest) to 19 (smallest); zstd only")
	fec := fs.Int("fec", 0, "percentage of RaptorQ recovery data to add")
	solid := fs.Bool("solid", false, "compress all files as one stream (better ratio, slower random access)")
	codec := fs.String("codec", nya.CompressionLZMA2,
		"lzma2 for the smallest archive, or zstd for much faster extraction")
	password := fs.String("password", "", "encrypt the payload with this password")
	workers := fs.Int("workers", 0, "number of compression workers (0 = automatic)")
	fs.Parse(args)

	switch *codec {
	case nya.CompressionLZMA2, nya.CompressionZstd:
	default:
		return fmt.Errorf("unknown codec %q, want %q or %q",
			*codec, nya.CompressionLZMA2, nya.CompressionZstd)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("create needs an archive path and one input path")
	}
	archive, input := fs.Arg(0), fs.Arg(1)

	f, err := os.Create(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	var w *nya.Writer
	if *password != "" {
		w = nya.NewWriterOpts(f, *fec, *level, *solid, []byte(*password))
	} else {
		w = nya.NewWriterOpts(f, *fec, *level, *solid)
	}
	w.SetCompression(*codec)
	if *workers > 0 {
		w.SetWorkers(*workers)
	}

	start := time.Now()
	if err := w.AddFile(input); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	orig := inputSize(input)
	fmt.Printf("%s  %s", archive, nya.HumanSize(int(fi.Size())))
	if orig > 0 {
		fmt.Printf("  (%.1f%% of %s)", 100*float64(fi.Size())/float64(orig), nya.HumanSize(int(orig)))
	}
	fmt.Printf("  in %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	password := fs.String("password", "", "archive password")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("list needs an archive path")
	}

	r, err := open(fs.Arg(0), *password)
	if err != nil {
		return err
	}

	entries := r.List()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	var total uint64
	for _, e := range entries {
		total += e.OriginalSize
		fmt.Printf("%-10s %10s  %s  %s%s\n",
			entryKind(e.EntryType),
			nya.HumanSize(int(e.OriginalSize)),
			time.Unix(0, e.MTimeNano).Format("2006-01-02 15:04"),
			e.Path,
			linkSuffix(e))
	}
	fmt.Printf("\n%d entries, %s uncompressed\n", len(entries), nya.HumanSize(int(total)))
	return nil
}

func cmdExtract(args []string) error {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	password := fs.String("password", "", "archive password")
	fs.Parse(args)
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return fmt.Errorf("extract needs an archive path and an optional destination")
	}
	dest := "."
	if fs.NArg() == 2 {
		dest = fs.Arg(1)
	}

	r, err := open(fs.Arg(0), *password)
	if err != nil {
		return err
	}
	return r.Extract(dest)
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	password := fs.String("password", "", "archive password")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("verify needs an archive path")
	}

	r, err := open(fs.Arg(0), *password)
	if err != nil {
		return err
	}
	if !r.Verify() {
		return fmt.Errorf("%s: checksum mismatch, the archive is damaged", fs.Arg(0))
	}
	fmt.Printf("%s: OK (%d entries)\n", fs.Arg(0), len(r.Entries))
	return nil
}

func cmdInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("info needs an archive path")
	}

	r, err := nya.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	h := r.Header

	fmt.Printf("format version:  %d.%d\n", h.VersionMajor, h.VersionMinor)
	fmt.Printf("created:         %s\n", time.Unix(0, h.CreationTime).Format(time.RFC3339))
	fmt.Printf("entries:         %d\n", len(r.Entries))
	fmt.Printf("codec:           %s\n", archiveCodec(r))
	fmt.Printf("uncompressed:    %s\n", nya.HumanSize(int(h.TotalOrigSize)))
	fmt.Printf("data area:       %s\n", nya.HumanSize(int(h.DataAreaSize)))
	fmt.Printf("solid:           %t\n", h.Flags&nya.FlagSolidCompress != 0)
	fmt.Printf("recovery data:   %s\n", nya.HumanSize(int(r.FecLen)))
	if h.VersionMinor < 1 {
		fmt.Printf("\nnote: written before format 1.1, so its zstd frames use the\n" +
			"      legacy sequence code tables and are not readable by other\n" +
			"      zstd implementations. Repack it to upgrade.\n")
	}
	return nil
}

func cmdRepair(args []string) error {
	fs := flag.NewFlagSet("repair", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return fmt.Errorf("repair needs an archive path and an optional output path")
	}
	in := fs.Arg(0)
	out := in + ".repaired"
	if fs.NArg() == 2 {
		out = fs.Arg(1)
	}

	res, err := nya.Repair(in, out)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d/%d chunks repaired, %d failed\n",
		out, res.RepairedChunks, res.TotalChunks, res.FailedChunks)
	if res.FailedChunks > 0 {
		return fmt.Errorf("%d chunks could not be recovered", res.FailedChunks)
	}
	return nil
}

// archiveCodec reports the codec used by the file entries. The format records
// it per entry, so an archive can in principle mix them.
func archiveCodec(r *nya.Reader) string {
	seen := map[string]bool{}
	for _, e := range r.Entries {
		if e.EntryType != nya.EntryFile {
			continue
		}
		switch e.CompressionID {
		case nya.CompressLzma2:
			seen["lzma2"] = true
		case nya.CompressZstd:
			seen["zstd"] = true
		default:
			seen[fmt.Sprintf("id-%d", e.CompressionID)] = true
		}
	}
	switch len(seen) {
	case 0:
		return "none"
	case 1:
		for name := range seen {
			return name
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return "mixed (" + strings.Join(names, ", ") + ")"
}

func open(path, password string) (*nya.Reader, error) {
	if password != "" {
		return nya.Open(path, []byte(password))
	}
	return nya.Open(path)
}

func entryKind(t uint8) string {
	switch t {
	case nya.EntryFile:
		return "file"
	case nya.EntryDir:
		return "dir"
	case nya.EntrySymlink:
		return "symlink"
	case nya.EntryHardlink:
		return "hardlink"
	case nya.EntryCharDev:
		return "chardev"
	case nya.EntryBlockDev:
		return "blockdev"
	case nya.EntryFifo:
		return "fifo"
	}
	return "unknown"
}

func linkSuffix(e nya.DirEntry) string {
	switch e.EntryType {
	case nya.EntrySymlink:
		return " -> " + e.LinkTarget
	case nya.EntryHardlink:
		return " => " + e.LinkTarget
	case nya.EntryCharDev, nya.EntryBlockDev:
		return fmt.Sprintf(" (%d:%d)", e.DevMajor, e.DevMinor)
	}
	return ""
}

func inputSize(path string) int64 {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	if !fi.IsDir() {
		return fi.Size()
	}
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}
