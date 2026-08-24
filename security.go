package nya

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sanitizePath resolves an archive entry path against destDir and rejects
// anything that would escape it.
func sanitizePath(destDir, entryPath string) (string, error) {
	cleaned := filepath.Clean(entryPath)

	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("nya: absolute path rejected: %s", entryPath)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("nya: path traversal rejected: %s", entryPath)
	}

	outPath := filepath.Join(destDir, cleaned)

	// Belt and braces: confirm the joined path really is under destDir.
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(destDir)
	if err != nil {
		return "", err
	}
	if absOut != absDir && !strings.HasPrefix(absOut, absDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("nya: path escapes destination: %s", entryPath)
	}

	return outPath, nil
}

// checkSymlink refuses to write through an existing symlink, so a crafted
// archive cannot redirect an extraction outside the destination.
func checkSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil // does not exist yet
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("nya: refusing to overwrite symlink: %s", path)
	}
	return nil
}
