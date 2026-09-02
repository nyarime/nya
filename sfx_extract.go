package nya

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SelfStubBytes returns bytes to use as an SFX stub: the running executable,
// or the stub prefix when the executable is already an SFX file.
func SelfStubBytes() ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return nil, err
	}
	if foot, err := ParseSfxFooter(data); err == nil {
		off := int(foot.ArchiveOffset)
		if off > 0 && off <= len(data) {
			return data[:off], nil
		}
	}
	return data, nil
}

// ResolveStubBytes loads stub bytes from stubPath when set; otherwise uses
// SelfStubBytes, then falls back to DefaultStubPath on disk (legacy layout).
func ResolveStubBytes(stubPath string) ([]byte, error) {
	if stubPath != "" {
		stub, err := os.ReadFile(stubPath)
		if err != nil {
			return nil, fmt.Errorf("sfx: read stub: %w", err)
		}
		return stub, nil
	}
	if stub, err := SelfStubBytes(); err == nil && len(stub) > 0 {
		return stub, nil
	}
	path, err := DefaultStubPath()
	if err != nil {
		return nil, err
	}
	stub, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("sfx: read stub: %w", err)
	}
	return stub, nil
}

// BuildSFXFromBytes writes stub bytes + archive + optional config + footer.
func BuildSFXFromBytes(stub []byte, archivePath, outPath string, config []byte, flags uint32) error {
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("sfx: read archive: %w", err)
	}
	if len(archive) < GlobalHeaderSize || !bytes.Equal(archive[:8], MagicHeader[:]) {
		return fmt.Errorf("sfx: not a NYA archive")
	}

	archiveOffset := uint64(len(stub))
	configOffset := uint64(0)
	configSize := uint32(0)
	if len(config) > 0 {
		configOffset = archiveOffset + uint64(len(archive))
		configSize = uint32(len(config))
	}

	var buf bytes.Buffer
	buf.Write(stub)
	buf.Write(archive)
	if len(config) > 0 {
		buf.Write(config)
	}

	foot := make([]byte, SFXFooterSize)
	copy(foot[:8], SFXMagic)
	binary.LittleEndian.PutUint64(foot[8:16], archiveOffset)
	binary.LittleEndian.PutUint64(foot[16:24], uint64(len(archive)))
	binary.LittleEndian.PutUint64(foot[24:32], configOffset)
	binary.LittleEndian.PutUint32(foot[32:36], configSize)
	binary.LittleEndian.PutUint32(foot[36:40], flags)
	buf.Write(foot)

	if err := os.WriteFile(outPath, buf.Bytes(), 0755); err != nil {
		return fmt.Errorf("sfx: write output: %w", err)
	}
	return nil
}

// BuildSFXAuto builds an SFX file using ResolveStubBytes for the stub source.
func BuildSFXAuto(stubPath, archivePath, outPath string, config []byte, flags uint32) error {
	stub, err := ResolveStubBytes(stubPath)
	if err != nil {
		return err
	}
	return BuildSFXFromBytes(stub, archivePath, outPath, config, flags)
}

// RunSFXExtract implements self-extraction (double-click / nya-sfx-stub CLI).
func RunSFXExtract(args []string) error {
	fs := flag.NewFlagSet("nya-sfx", flag.ContinueOnError)
	out := fs.String("o", "", "output directory (default: folder containing this executable)")
	overwrite := fs.Bool("y", false, "overwrite existing files (Extract always replaces files)")
	_ = overwrite
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	args = rearrangeSFXArgs(args)
	if err := fs.Parse(args); err != nil {
		return err
	}

	dest := *out
	if dest == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		dest = filepath.Dir(exe)
	}

	if fs.NArg() == 1 {
		data, err := os.ReadFile(fs.Arg(0))
		if err != nil {
			return err
		}
		return extractSFXArchive(data, dest)
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("only one archive path allowed")
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	archive, err := SliceSFXArchive(exe)
	if err != nil {
		return err
	}
	return extractSFXArchive(archive, dest)
}

func extractSFXArchive(archive []byte, dest string) error {
	r, err := OpenReaderAt(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if err := r.Extract(dest); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Extracted to %s\n", dest)
	return nil
}

// rearrangeSFXArgs allows `stub pack.nya -o dir` ordering.
func rearrangeSFXArgs(args []string) []string {
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

// ShouldRunSFXMode reports whether argv should run SFX extraction instead of
// the nya CLI. True when the running binary is an SFX file and argv does not
// start with an explicit nya subcommand, or for dev/test argv (-o / .nya).
func ShouldRunSFXMode(argv []string) bool {
	if len(argv) < 2 {
		exe, err := os.Executable()
		return err == nil && IsSFX(exe)
	}
	if isNyaCLICommand(argv[1]) {
		return false
	}
	exe, err := os.Executable()
	if err == nil && IsSFX(exe) {
		return true
	}
	return looksLikeSFXArgv(argv)
}

func isNyaCLICommand(arg string) bool {
	switch arg {
	case "create", "c", "list", "l", "extract", "x", "open", "verify", "t", "info",
		"repair", "augment", "convert", "import", "repack", "manifest", "sfx",
		"get", "send", "associate", "-h", "--help", "help":
		return true
	}
	return false
}

func looksLikeSFXArgv(argv []string) bool {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if a == "-o" || a == "-y" || strings.HasPrefix(a, "-o=") {
			return true
		}
		if strings.HasSuffix(strings.ToLower(a), ".nya") {
			return true
		}
	}
	return false
}
