package nya

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// sanitizePath prevents path traversal attacks in archive entries.
// Returns cleaned path relative to destDir, or error if malicious.
func sanitizePath(destDir, entryPath string) (string, error) {
	// Clean the entry path
	cleaned := filepath.Clean(entryPath)

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("security: absolute path rejected: %s", entryPath)
	}

	// Reject path traversal (..)
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("security: path traversal rejected: %s", entryPath)
	}

	// Construct full output path
	outPath := filepath.Join(destDir, cleaned)

	// Verify it's actually inside destDir (double check)
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(destDir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absOut, absDir+string(os.PathSeparator)) && absOut != absDir {
		return "", fmt.Errorf("security: path escapes destination: %s", entryPath)
	}

	return outPath, nil
}

// checkSymlink returns error if path is a symlink (prevent symlink attacks)
func checkSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil // doesn't exist yet, ok
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("security: refusing to overwrite symlink: %s", path)
	}
	return nil
}

const maxSymlinkDepth = 40

// SafeWalk is like filepath.WalkDir but skips symlink directories to prevent
// infinite recursion from symlink loops. Regular file symlinks are visited
// but directory symlinks are skipped entirely.
func SafeWalk(root string, fn func(path string, d fs.DirEntry, err error) error) error {
	return safeWalkDir(root, 0, fn)
}

func safeWalkDir(dir string, depth int, fn func(string, fs.DirEntry, error) error) error {
	if depth > maxSymlinkDepth {
		return nil // silently stop — too deep
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// report error on the directory itself
		return fn(dir, nil, err)
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.Type()&os.ModeSymlink != 0 {
			// Resolve symlink to check if it points to a directory
			target, err := filepath.EvalSymlinks(p)
			if err != nil {
				// broken symlink — report but continue
				if err2 := fn(p, e, nil); err2 != nil {
					return err2
				}
				continue
			}
			fi, err := os.Stat(target)
			if err != nil {
				if err2 := fn(p, e, nil); err2 != nil {
					return err2
				}
				continue
			}
			if fi.IsDir() {
				// Skip symlink directories — prevent loops
				continue
			}
			// Symlink to file — visit it
			if err2 := fn(p, e, nil); err2 != nil {
				return err2
			}
			continue
		}
		if err2 := fn(p, e, nil); err2 != nil {
			if err2 == filepath.SkipDir {
				continue
			}
			return err2
		}
		if e.IsDir() {
			if err := safeWalkDir(p, depth+1, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// SafeWalkFiles is a convenience wrapper that calls fn for each regular file.
// It skips symlink directories and broken symlinks.
func SafeWalkFiles(root string, fn func(path string, info os.FileInfo) error) error {
	return SafeWalk(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		return fn(path, info)
	})
}

// SafeWalkCompat is a drop-in replacement for filepath.Walk that skips
// symlink directories. Same signature as filepath.Walk for easy migration.
func SafeWalkCompat(root string, fn filepath.WalkFunc) error {
	return SafeWalk(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fn(path, nil, err)
		}
		if d == nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return fn(path, nil, ierr)
		}
				return fn(path, info, nil)
	})
}

// CountFiles counts regular files under root using SafeWalk.
func CountFiles(root string) int {
	n := 0
	SafeWalkFiles(root, func(_ string, _ os.FileInfo) error {
		n++
		return nil
	})
	return n
}
