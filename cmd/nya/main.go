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
  nya repair  <archive> [out]                 repair NYA / ZIP / RAR (format detected by magic)
  nya augment <archive.nya> [out.nya]        increase FEC repair data (Leopard-RS / Hybrid / RaptorQ)
  nya convert [flags] <in.zip|7z|rar> <out.nya>  unpack legacy archive and repack as NYA (zip/7z/rar)
  nya manifest <archive.nya> -o <manifest.nyam>  build download manifest for nya-get
  nya sfx     [flags] <archive.nya> -o <out.exe> wrap archive as self-extractor (Rust stub)

Levels run 0 to 9, the way 7-Zip and WinRAR present them: 0 stores, 1 is
fastest, 5 is the default, 9 is smallest. Levels up to 4 use Zstandard for
quick extraction; 5 and above use LZMA2 for size. "-codec" overrides that
choice if you want a specific one.

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
	case "augment":
		err = cmdAugment(os.Args[2:])
	case "convert", "import", "repack":
		err = cmdConvert(os.Args[2:])
	case "manifest":
		err = cmdManifest(os.Args[2:])
	case "sfx":
		err = cmdSfx(os.Args[2:])
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
	level := fs.Int("level", nya.LevelDefault,
		"0 store, 1 fastest, 3 fast, 5 normal, 7 good, 9 best")
	fec := fs.Int("fec", 0, "recovery data as a percentage of the payload")
	fecType := fs.String("fec-type", "hybrid",
		"FEC codec when -fec > 0: hybrid (default), raptorq or ldpc")
	solid := fs.Bool("solid", false, "compress all files as one stream (better ratio, slower random access)")
	sfx := fs.Bool("sfx", false, "write a self-extracting file (requires Rust stub; see sfx/README.md)")
	sfxStub := fs.String("sfx-stub", "", "path to nya-sfx-stub binary (default: sfx/stubs/<os>_<arch>)")
	codec := fs.String("codec", "",
		"override the level's codec: lzma2, zstd or store")
	password := fs.String("password", "", "encrypt the payload with this password")
	workers := fs.Int("workers", 0, "number of compression workers (0 = automatic)")
	multiChunk := fs.Bool("multi-chunk", true, "split non-solid files > 4 MiB into multiple chunks (format 1.3)")
	chunkSize := fs.Int("chunk-size", 0, "raw chunk size for multi-chunk entries (0 = automatic)")
	fs.Parse(args)

	if *level < 0 || *level > 9 {
		return fmt.Errorf("level %d is out of range, want 0 to 9", *level)
	}
	switch *codec {
	case "", nya.CompressionLZMA2, nya.CompressionZstd, nya.CompressionStore:
	default:
		return fmt.Errorf("unknown codec %q, want %q, %q or %q",
			*codec, nya.CompressionLZMA2, nya.CompressionZstd, nya.CompressionStore)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("create needs an archive path and one input path")
	}
	archive, input := fs.Arg(0), fs.Arg(1)

	archivePath := archive
	if *sfx {
		archivePath = archive + ".part.nya"
	}

	f, err := os.Create(archivePath)
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
	if *codec != "" {
		w.SetCompression(*codec)
	}
	if *workers > 0 {
		w.SetWorkers(*workers)
	}
	w.SetMultiChunk(*multiChunk)
	if *chunkSize > 0 {
		w.SetChunkSize(*chunkSize)
	}
	if *fec > 0 {
		switch *fecType {
		case "hybrid", "":
			w.SetFECType(nya.FECHybrid)
		case "raptorq":
			w.SetFECType(nya.FECRaptorQ)
		case "ldpc":
			w.SetFECType(nya.FECLDPC)
		default:
			return fmt.Errorf("unknown fec-type %q, want hybrid, raptorq or ldpc", *fecType)
		}
	}

	start := time.Now()
	if err := w.AddFile(input); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	f.Close()

	if *sfx {
		stubPath := *sfxStub
		if stubPath == "" {
			var err error
			stubPath, err = nya.DefaultStubPath()
			if err != nil {
				return err
			}
		}
		if err := nya.BuildSFX(stubPath, archivePath, archive, nil, nya.SFXFlagConsole); err != nil {
			return err
		}
		os.Remove(archivePath)
	}

	fi, err := os.Stat(archive)
	if err != nil {
		return err
	}
	orig := inputSize(input)
	fmt.Printf("%s  %s  [%s]", archive, nya.HumanSize(int(fi.Size())), nya.LevelName(*level))
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

	r, err := openOrPasswordHint(fs.Arg(0), *password)
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

	r, err := openOrPasswordHint(fs.Arg(0), *password)
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

	r, err := openOrPasswordHint(fs.Arg(0), *password)
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
	fmt.Printf("encrypted:       %t\n", h.Flags&nya.FlagEncrypted != 0)
	if h.Flags&nya.FlagEncrypted != 0 {
		if h.Flags&nya.FlagKDFArgon2id != 0 {
			fmt.Printf("kdf:             Argon2id (v1.2)\n")
		} else {
			fmt.Printf("kdf:             SHA-256 (legacy)\n")
		}
	}
	fmt.Printf("recovery data:   %s\n", nya.HumanSize(int(r.FecLen)))
	return nil
}

func cmdRepair(args []string) error {
	fs := flag.NewFlagSet("repair", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return fmt.Errorf("repair needs an archive path and an optional output path")
	}
	in := fs.Arg(0)
	var out string
	if fs.NArg() == 2 {
		out = fs.Arg(1)
	}

	res, err := nya.Repair(in, out)
	if err != nil {
		return err
	}
	if res.OutputPath != "" {
		out = res.OutputPath
	}
	switch res.Format {
	case nya.FormatZIP, nya.FormatRAR:
		fmt.Printf("%s (%s): rebuilt %d/%d entries → %s\n",
			filepath.Base(in), res.Format, res.RepairedChunks, res.FilesFound, out)
		if res.RepairedChunks == 0 {
			return fmt.Errorf("no entries could be recovered")
		}
	default:
		fmt.Printf("%s (nya): %d/%d chunks repaired, %d failed → %s\n",
			filepath.Base(in), res.RepairedChunks, res.TotalChunks, res.FailedChunks, out)
		if res.FailedChunks > 0 {
			return fmt.Errorf("%w (%d/%d chunks failed)", nya.ErrFECInsufficient, res.FailedChunks, res.TotalChunks)
		}
	}
	return nil
}

func cmdAugment(args []string) error {
	fs := flag.NewFlagSet("augment", flag.ExitOnError)
	extra := fs.Int("fec", 10, "extra repair data as a percentage of payload (adds to existing -fec, or sets initial when archive had -fec 0)")
	fs.Parse(args)
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return fmt.Errorf("augment needs an archive path and an optional output path")
	}
	in := fs.Arg(0)
	out := in
	if fs.NArg() == 2 {
		out = fs.Arg(1)
	}
	res, err := nya.Augment(in, out, *extra)
	if err != nil {
		return err
	}
	fmt.Printf("%s: +%s repair data (now ~%d%% payload redundancy)\n", out, nya.HumanSize(res.ExtraBytes), res.NewPercent)
	return nil
}

func cmdConvert(args []string) error {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	level := fs.Int("level", nya.LevelDefault,
		"0 store, 1 fastest, 3 fast, 5 normal, 7 good, 9 best")
	fec := fs.Int("fec", 10, "recovery data as a percentage of the payload (repack adds FEC unlike zip/7z)")
	fecType := fs.String("fec-type", "hybrid",
		"FEC codec when -fec > 0: hybrid (default), raptorq or ldpc")
	solid := fs.Bool("solid", false, "solid compression (recommended for many small files)")
	codec := fs.String("codec", "",
		"override the level's codec: lzma2, zstd or store")
	password := fs.String("password", "", "encrypt the output .nya with this password")
	sourcePassword := fs.String("source-password", "", "password for encrypted zip/7z/rar input")
	workers := fs.Int("workers", 0, "number of compression workers (0 = automatic)")
	fs.Parse(args)

	if fs.NArg() != 2 {
		return fmt.Errorf("convert needs an input archive and output .nya path")
	}
	src, dst := fs.Arg(0), fs.Arg(1)
	if !strings.HasSuffix(strings.ToLower(dst), ".nya") {
		return fmt.Errorf("output must be a .nya path")
	}
	if !nya.Convertable(src) {
		return fmt.Errorf("input %q is not a supported archive (supported: %s)", src, nya.ListConvertFormats())
	}

	var fecTypeVal uint8
	if *fec > 0 {
		switch *fecType {
		case "hybrid", "":
			fecTypeVal = nya.FECHybrid
		case "raptorq":
			fecTypeVal = nya.FECRaptorQ
		case "ldpc":
			fecTypeVal = nya.FECLDPC
		default:
			return fmt.Errorf("unknown fec-type %q, want hybrid, raptorq or ldpc", *fecType)
		}
	}

	opts := nya.ConvertOptions{
		FECPercent:     *fec,
		Level:          *level,
		Solid:          *solid,
		Codec:          *codec,
		SourcePassword: *sourcePassword,
		FECType:        fecTypeVal,
		Workers:        *workers,
	}
	if *password != "" {
		opts.Password = []byte(*password)
	}

	start := time.Now()
	res, err := nya.ConvertArchive(src, dst, opts)
	if err != nil {
		return err
	}
	fmt.Printf("%s → %s  %s  [%s]", filepath.Base(src), dst, nya.HumanSize(int(res.OutputSize)), nya.LevelName(*level))
	if *fec > 0 {
		fmt.Printf("  +%d%% FEC", *fec)
	}
	fmt.Printf("  (%s)  in %s\n", res.SourceFormat, time.Since(start).Round(time.Millisecond))
	return nil
}

func cmdManifest(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ExitOnError)
	out := fs.String("o", "", "output .nyam path (default: archive.nyam)")
	blockSize := fs.String("block-size", "4m", "transport block size (e.g. 4m, 8m, 4194304)")
	url := fs.String("url", "", "download URL for the archive (repeatable via --url)")
	fs.Parse(args)

	if fs.NArg() != 1 {
		return fmt.Errorf("manifest needs an archive path")
	}
	archive := fs.Arg(0)

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

	outPath := *out
	if outPath == "" {
		outPath = strings.TrimSuffix(archive, filepath.Ext(archive)) + ".nyam"
	}
	if err := nya.WriteManifest(m, outPath); err != nil {
		return err
	}

	fmt.Printf("%s: %d blocks x %s, archive %s (%s)\n",
		outPath,
		len(m.Download.Blocks),
		nya.HumanSize(int(m.Download.BlockSize)),
		m.Archive.Name,
		nya.HumanSize(int(m.Archive.Size)))
	return nil
}

// archiveCodec reports the codec used by the file entries.
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
		case nya.CompressNone:
			seen["store"] = true
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
		return nya.OpenAny(path, []byte(password))
	}
	r, err := nya.OpenAny(path)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func openOrPasswordHint(path, password string) (*nya.Reader, error) {
	r, err := open(path, password)
	if err == nil {
		return r, nil
	}
	if err == nya.ErrPasswordRequired {
		return nil, fmt.Errorf("%w — use: nya extract -password '…' %s", err, path)
	}
	return nil, err
}

func cmdSfx(args []string) error {
	fs := flag.NewFlagSet("sfx", flag.ExitOnError)
	out := fs.String("o", "", "output self-extracting file (required)")
	stub := fs.String("stub", "", "path to nya-sfx-stub (default: sfx/stubs/<os>_<arch>)")
	fs.Parse(args)
	if *out == "" {
		return fmt.Errorf("sfx requires -o output path")
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("sfx needs one archive path")
	}
	stubPath := *stub
	if stubPath == "" {
		var err error
		stubPath, err = nya.DefaultStubPath()
		if err != nil {
			return err
		}
	}
	if err := nya.BuildSFX(stubPath, fs.Arg(0), *out, nil, nya.SFXFlagConsole); err != nil {
		return err
	}
	fi, err := os.Stat(*out)
	if err != nil {
		return err
	}
	fmt.Printf("%s  %s  (SFX, stub %s)\n", *out, nya.HumanSize(int(fi.Size())), stubPath)
	return nil
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
