package nya

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// noExtSortKey sorts extensionless paths after normal extensions.
const noExtSortKey = "\xff"

type solidSortEntry struct {
	path string
	ext  string
	size int64
}

// sortSolidFilePaths reorders files for solid compression. Similar extensions
// are grouped together (like 7-Zip solid archives), with larger files first
// within each group so repeated structure warms the dictionary.
func sortSolidFilePaths(files []string) []string {
	if len(files) < 2 {
		return files
	}

	entries := make([]solidSortEntry, len(files))
	for i, p := range files {
		fi, err := os.Lstat(p)
		if err != nil {
			entries[i] = solidSortEntry{path: p, ext: noExtSortKey}
			continue
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == "" {
			ext = noExtSortKey
		}
		entries[i] = solidSortEntry{path: p, ext: ext, size: fi.Size()}
	}

	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.ext != b.ext {
			return a.ext < b.ext
		}
		if a.size != b.size {
			return a.size > b.size
		}
		return a.path < b.path
	})

	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.path
	}
	return out
}
