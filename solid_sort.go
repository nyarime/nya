package nya

import (
	"bytes"
	"encoding/binary"
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
	kind string // content fingerprint from file header
	size int64
}

// sortSolidFilePaths reorders files for solid compression. Similar extensions
// are grouped together (like 7-Zip solid archives), with content-kind as a
// secondary key within each extension group, and larger files first within
// each (ext, kind) bucket so repeated structure warms the dictionary.
func sortSolidFilePaths(files []string) []string {
	if len(files) < 2 {
		return files
	}

	entries := make([]solidSortEntry, len(files))
	for i, p := range files {
		fi, err := os.Lstat(p)
		if err != nil {
			entries[i] = solidSortEntry{path: p, ext: noExtSortKey, kind: contentKindUnknown}
			continue
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == "" {
			ext = noExtSortKey
		}
		entries[i] = solidSortEntry{
			path: p,
			ext:  ext,
			kind: detectContentKind(p),
			size: fi.Size(),
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.ext != b.ext {
			return a.ext < b.ext
		}
		if a.kind != b.kind {
			return a.kind < b.kind
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

const (
	contentKindUnknown = "z_unknown"
	contentKindText    = "a_text"
	contentKindJSON    = "b_json"
	contentKindELF     = "c_elf"
	contentKindPE      = "d_pe"
	contentKindPNG     = "e_png"
	contentKindZIP     = "f_zip"
	contentKindGzip    = "g_gzip"
	contentKindWasm    = "h_wasm"
	contentKindBinary  = "y_binary"
)

// detectContentKind reads the first bytes of path and returns a coarse label
// used to refine ordering within the same extension group.
func detectContentKind(path string) string {
	switch ClassifyFile(path) {
	case PayloadTextLike:
		if looksLikeJSONFile(path) {
			return contentKindJSON
		}
		return contentKindText
	case PayloadDense:
		return denseKindLabel(path)
	case PayloadBinary:
		return binaryKindLabel(path)
	default:
		return contentKindUnknown
	}
}

func looksLikeJSONFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [32]byte
	n, _ := f.Read(head[:])
	return looksLikeJSON(head[:n])
}

func denseKindLabel(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return contentKindBinary
	}
	defer f.Close()
	var head [32]byte
	n, _ := f.Read(head[:])
	h := head[:n]
	if len(h) >= 8 && h[0] == 0x89 && h[1] == 'P' {
		return contentKindPNG
	}
	if len(h) >= 2 && h[0] == 0x50 && h[1] == 0x4b {
		return contentKindZIP
	}
	if len(h) >= 2 && h[0] == 0x1f && h[1] == 0x8b {
		return contentKindGzip
	}
	return contentKindBinary
}

func binaryKindLabel(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return contentKindBinary
	}
	defer f.Close()
	var head [8]byte
	n, _ := f.Read(head[:])
	h := head[:n]
	if len(h) >= 4 && h[0] == 0x7f && h[1] == 'E' {
		return contentKindELF
	}
	if len(h) >= 2 && h[0] == 'M' && h[1] == 'Z' {
		return contentKindPE
	}
	if len(h) >= 4 && h[0] == 0x00 && h[1] == 'a' {
		return contentKindWasm
	}
	return contentKindBinary
}

func looksLikeJSON(h []byte) bool {
	trim := bytes.TrimSpace(h)
	if len(trim) == 0 {
		return false
	}
	switch trim[0] {
	case '{', '[', '"':
		return true
	default:
		return false
	}
}

func looksLikeText(h []byte) bool {
	if len(h) == 0 {
		return false
	}
	printable := 0
	for _, b := range h {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b < 0x7f) {
			printable++
		}
	}
	return printable*100/len(h) >= 85
}

// contentKindFromBytes classifies an in-memory prefix (tests).
func contentKindFromBytes(h []byte) string {
	if len(h) >= 4 && h[0] == 0x7f && h[1] == 'E' && h[2] == 'L' && h[3] == 'F' {
		return contentKindELF
	}
	if looksLikeJSON(h) {
		return contentKindJSON
	}
	if looksLikeText(h) {
		return contentKindText
	}
	if len(h) >= 2 && binary.LittleEndian.Uint16(h[:2]) == 0x5a4d {
		return contentKindPE
	}
	return contentKindUnknown
}
