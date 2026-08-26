package nya

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrPasswordIncorrect is returned when a password was supplied but unlock failed.
var ErrPasswordIncorrect = errors.New("nya: wrong password for encrypted archive")

// ArchiveNeedsPassword reports whether path is encrypted and requires a password
// to extract. For 7z/rar this is a best-effort probe via `7z l`; if probing is
// inconclusive it returns false and callers must still map extract failures.
func ArchiveNeedsPassword(path string) (bool, error) {
	format, err := DetectHubFormat(path)
	if err != nil {
		return false, err
	}
	switch format {
	case FormatNYA:
		return nyaNeedsPassword(path)
	case FormatZIP:
		return zipNeedsPassword(path)
	case FormatSevenZ, FormatRAR, FormatTar, FormatGzip:
		return sevenZNeedsPassword(path)
	default:
		return false, nil
	}
}

func nyaNeedsPassword(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h, err := ReadGlobalHeader(f)
	if err != nil {
		return false, err
	}
	return h.Flags&FlagEncrypted != 0, nil
}

func zipNeedsPassword(path string) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		// Encrypted central directory or AES zip often fails stdlib open.
		if sevenZipAvailable() {
			return sevenZNeedsPassword(path)
		}
		return false, nil
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Flags&0x1 != 0 {
			return true, nil
		}
	}
	// AE-1/AE-2 may omit the general-purpose encryption bit; probe first data file.
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return true, nil
		}
		rc.Close()
		break
	}
	return false, nil
}

func sevenZNeedsPassword(path string) (bool, error) {
	if !sevenZipAvailable() {
		return false, nil
	}
	bin, err := find7z()
	if err != nil {
		return false, nil
	}
	// -p- disables interactive prompt; encrypted archives usually advertise it.
	cmd := exec.Command(bin, "l", "-slt", "-p-", "-bd", path)
	out, err := cmd.CombinedOutput()
	text := string(out)
	lower := strings.ToLower(text)
	if strings.Contains(lower, "encrypted = +") ||
		strings.Contains(lower, "encrypted =+") ||
		strings.Contains(lower, "enter password") ||
		strings.Contains(lower, "wrong password") {
		return true, nil
	}
	if err != nil && looksLikePasswordFailure(text) {
		return true, nil
	}
	return false, nil
}

func looksLikePasswordFailure(msg string) bool {
	lower := strings.ToLower(msg)
	for _, s := range []string{
		"wrong password",
		"enter password",
		"password is incorrect",
		"can not open encrypted",
		"cannot open encrypted",
		"data error : wrong password",
		"is encrypted",
	} {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// RequireSourcePassword returns ErrPasswordRequired with a CLI-oriented hint
// when the archive is encrypted and sourcePassword is empty.
// Policy: never prompt interactively; require an explicit flag.
func RequireSourcePassword(path, sourcePassword string) error {
	if sourcePassword != "" {
		return nil
	}
	needs, err := ArchiveNeedsPassword(path)
	if err != nil {
		return err
	}
	if !needs {
		return nil
	}
	return fmt.Errorf("%w\n  hint: pass -source-password for convert/import, or -password for extract/list/info/open", ErrPasswordRequired)
}

// MapExtractPasswordError rewrites foreign-tool failures into typed password errors.
func MapExtractPasswordError(err error, passwordProvided bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrPasswordRequired) || errors.Is(err, ErrPasswordIncorrect) {
		return err
	}
	msg := err.Error()
	if !looksLikePasswordFailure(msg) {
		return err
	}
	if passwordProvided {
		return fmt.Errorf("%w: %v", ErrPasswordIncorrect, err)
	}
	return fmt.Errorf("%w\n  hint: pass -source-password '…'", ErrPasswordRequired)
}
