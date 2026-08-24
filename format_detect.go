package nya

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// DetectFormatByMagic identifies an archive by file content (magic bytes).
// Extension is ignored so renamed files are still recognized.
func DetectFormatByMagic(path string) (string, error) {
	header, err := readHeader(path, 8)
	if err != nil {
		return FormatUnknown, err
	}
	if len(header) >= 8 && bytes.Equal(header[:8], MagicHeader[:]) {
		return "nya", nil
	}
	if len(header) >= 4 && header[0] == 'P' && header[1] == 'K' {
		return FormatZIP, nil
	}
	if len(header) >= 6 && bytes.HasPrefix(header, []byte("7z\xBC\xAF\x27\x1C")) {
		return FormatSevenZ, nil
	}
	if len(header) >= 6 && bytes.HasPrefix(header, []byte("Rar!\x1a\x07")) {
		return FormatRAR, nil
	}
	if len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b {
		return FormatGzip, nil
	}
	return FormatUnknown, fmt.Errorf("repair: unrecognized format (magic % x)", header)
}

func defaultRepairOutput(inputPath, out string) string {
	if out != "" {
		return out
	}
	return filepath.Join(filepath.Dir(inputPath), "fixed."+filepath.Base(inputPath))
}

func writeRepairOutput(inputPath, outputPath string, data []byte) error {
	out := defaultRepairOutput(inputPath, outputPath)
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return err
	}
	return nil
}
