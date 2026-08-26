package nya

import (
	"hash/maphash"
	"os"
	"path/filepath"
	"sort"
)

const (
	// DefaultSolidZstdDictMax is the embedded zstd dictionary cap for solid archives.
	DefaultSolidZstdDictMax = 8 << 10 // 8 KiB per docs/SOLID-DICT.md

	solidDictMinSamples   = 2
	solidDictSamplePrefix = 4096
	solidDictMinLen       = 256
	solidDictMinWindow    = 16
	solidDictMaxWindow    = 128
	solidDictStride       = 4
)

var solidDictSeed maphash.Seed

func init() { solidDictSeed = maphash.MakeSeed() }

// mostlyTextLikeSolid reports whether a solid tree is dominated by compressible
// text-like members (by file count).
func mostlyTextLikeSolid(textLike, dense, other int) bool {
	total := textLike + dense + other
	if total == 0 {
		return false
	}
	return textLike*100/total >= 50
}

// solidAutoZstdDictEligible reports whether the writer should try an auto-built
// zstd dictionary for this solid archive.
func solidAutoZstdDictEligible(level int, textLike, dense, other int) bool {
	if !mostlyTextLikeSolid(textLike, dense, other) {
		return false
	}
	if level >= LevelFast && level <= 4 {
		return true
	}
	total := textLike + dense + other
	return textLike*100/total >= 75
}

// buildSolidZstdDictFromDir derives a raw zstd dictionary prefix from repeated
// substrings in text-like file headers under root.
func buildSolidZstdDictFromDir(root string, maxDict int) []byte {
	samples := collectTextLikePrefixes(root, solidDictSamplePrefix)
	return buildSolidZstdDictFromSamples(samples, maxDict)
}

// buildSolidZstdDictFromSamples derives a dict from in-memory text-like prefixes.
func buildSolidZstdDictFromSamples(samples [][]byte, maxDict int) []byte {
	if maxDict <= 0 {
		maxDict = DefaultSolidZstdDictMax
	}
	if len(samples) < solidDictMinSamples {
		return nil
	}
	seeds := findSolidDictSeeds(samples, maxDict)
	if len(seeds) == 0 {
		return nil
	}
	dict := assembleSolidDict(seeds, maxDict)
	if len(dict) < solidDictMinLen {
		return nil
	}
	return dict
}

func collectTextLikePrefixes(root string, maxPrefix int) [][]byte {
	var samples [][]byte
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return err
		}
		if ClassifyFile(p) != PayloadTextLike {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		buf := make([]byte, maxPrefix)
		n, _ := f.Read(buf)
		f.Close()
		if n > 0 {
			samples = append(samples, append([]byte(nil), buf[:n]...))
		}
		return nil
	})
	return samples
}

type dictSeed struct {
	bytes []byte
	hits  int
}

func findSolidDictSeeds(samples [][]byte, maxDict int) []dictSeed {
	type scored struct {
		key  uint64
		ln   int
		hits int
	}
	best := make(map[uint64]scored)
	first := make(map[uint64][]byte)

	for win := solidDictMaxWindow; win >= solidDictMinWindow; win -= 8 {
		for _, s := range samples {
			lim := len(s)
			if lim > maxDict {
				lim = maxDict
			}
			seen := make(map[uint64]struct{})
			for off := 0; off+win <= lim; off += solidDictStride {
				sub := s[off : off+win]
				key := hashSubstr(sub)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				cur := best[key]
				cur.hits++
				cur.ln = win
				cur.key = key
				best[key] = cur
				if _, ok := first[key]; !ok {
					first[key] = append([]byte(nil), sub...)
				}
			}
		}
	}

	var seeds []dictSeed
	for key, sc := range best {
		if sc.hits < solidDictMinSamples {
			continue
		}
		seeds = append(seeds, dictSeed{bytes: first[key], hits: sc.hits})
	}
	sort.Slice(seeds, func(i, j int) bool {
		a, b := seeds[i], seeds[j]
		if a.hits != b.hits {
			return a.hits > b.hits
		}
		return len(a.bytes) > len(b.bytes)
	})
	if len(seeds) == 0 {
		lcp := longestCommonPrefix(samples)
		if len(lcp) >= solidDictMinLen {
			return []dictSeed{{bytes: lcp, hits: len(samples)}}
		}
	}
	return seeds
}

func assembleSolidDict(seeds []dictSeed, maxDict int) []byte {
	seen := make(map[string]struct{})
	dict := make([]byte, 0, maxDict)
	for _, sd := range seeds {
		k := string(sd.bytes)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		if len(dict)+len(sd.bytes) > maxDict {
			remain := maxDict - len(dict)
			if remain >= solidDictMinWindow {
				dict = append(dict, sd.bytes[:remain]...)
			}
			break
		}
		dict = append(dict, sd.bytes...)
	}
	if len(dict) < solidDictMinLen && len(seeds) > 0 {
		seed := seeds[0].bytes
		for len(dict) < solidDictMinLen && len(dict) < maxDict {
			remain := maxDict - len(dict)
			if remain >= len(seed) {
				dict = append(dict, seed...)
			} else {
				dict = append(dict, seed[:remain]...)
			}
		}
	}
	return dict
}

func hashSubstr(b []byte) uint64 {
	return maphash.Bytes(solidDictSeed, b)
}

func longestCommonPrefix(samples [][]byte) []byte {
	if len(samples) == 0 {
		return nil
	}
	minLen := len(samples[0])
	for _, s := range samples[1:] {
		if len(s) < minLen {
			minLen = len(s)
		}
	}
	var out []byte
	for i := 0; i < minLen; i++ {
		b := samples[0][i]
		for _, s := range samples[1:] {
			if s[i] != b {
				return out
			}
		}
		out = append(out, b)
	}
	return out
}

func countSolidTextLike(paths []string) (textLike, dense, other int) {
	for _, p := range paths {
		switch ClassifyFile(p) {
		case PayloadTextLike:
			textLike++
		case PayloadDense:
			dense++
		default:
			other++
		}
	}
	return textLike, dense, other
}

func collectSolidTextLikePrefixes(paths []string, maxPrefix int) [][]byte {
	var samples [][]byte
	for _, p := range paths {
		if ClassifyFile(p) != PayloadTextLike {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		buf := make([]byte, maxPrefix)
		n, _ := f.Read(buf)
		f.Close()
		if n > 0 {
			samples = append(samples, append([]byte(nil), buf[:n]...))
		}
	}
	return samples
}

// solidDictHelps reports whether compressing data with dict beats plain zstd
// and round-trips cleanly.
func solidDictHelps(data, dict []byte, level int) bool {
	if len(dict) == 0 || len(data) == 0 {
		return false
	}
	with := ZstdCompressWithDict(data, level, dict)
	if len(with) == 0 {
		return false
	}
	dec, err := DecompressZstdWithDict(with, dict)
	if err != nil || len(dec) != len(data) {
		return false
	}
	plain := ZstdCompressWithWindow(data, level)
	return len(with) < len(plain)
}
