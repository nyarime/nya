package nya

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SplitArchive splits a .nya file into volumes of maxSize bytes each.
// Output: base.nya.001, base.nya.002, ...
// Each volume is a standalone chunk with its own data.
func SplitArchive(nyaPath string, maxSize int64) ([]string, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("split size must be positive")
	}

	f, err := os.Open(nyaPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	totalSize := fi.Size()
	if totalSize <= maxSize {
		// No split needed
		return []string{nyaPath}, nil
	}

	baseName := strings.TrimSuffix(nyaPath, filepath.Ext(nyaPath))
	var volumes []string
	volNum := 1
	buf := make([]byte, 1024*1024) // 1MB read buffer

	for {
		volName := fmt.Sprintf("%s.nya.%03d", baseName, volNum)
		vf, err := os.Create(volName)
		if err != nil {
			return volumes, err
		}

		var written int64
		for written < maxSize {
			toRead := maxSize - written
			if toRead > int64(len(buf)) {
				toRead = int64(len(buf))
			}
			n, err := f.Read(buf[:toRead])
			if n > 0 {
				vf.Write(buf[:n])
				written += int64(n)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				vf.Close()
				return volumes, err
			}
		}
		vf.Close()
		volumes = append(volumes, volName)

		// Check if we're done
		pos, _ := f.Seek(0, io.SeekCurrent)
		if pos >= totalSize {
			break
		}
		volNum++
	}

	return volumes, nil
}

// JoinVolumes concatenates split volumes back into a single .nya file.
// Input: base.nya.001 → finds .002, .003, etc. automatically
func JoinVolumes(firstVolume string) (string, error) {
	// Find base name: strip .001
	ext := filepath.Ext(firstVolume) // .001
	base := strings.TrimSuffix(firstVolume, ext)

	// Determine output name
	outName := base // e.g., game.nya
	if !strings.HasSuffix(outName, ".nya") {
		outName = outName + ".nya"
	}

	out, err := os.Create(outName)
	if err != nil {
		return "", err
	}
	defer out.Close()

	volNum := 1
	for {
		volName := fmt.Sprintf("%s.%03d", base, volNum)
		vf, err := os.Open(volName)
		if err != nil {
			if volNum == 1 {
				return "", fmt.Errorf("cannot open first volume: %s", volName)
			}
			break // No more volumes
		}
		io.Copy(out, vf)
		vf.Close()
		volNum++
	}

	return outName, nil
}

// ParseSplitSize parses a human-readable size string like "1G", "500M", "100K"
func ParseSplitSize(s string) (int64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty size")
	}

	multiplier := int64(1)
	numStr := s

	last := s[len(s)-1]
	switch last {
	case 'K', 'k':
		multiplier = 1024
		numStr = s[:len(s)-1]
	case 'M', 'm':
		multiplier = 1024 * 1024
		numStr = s[:len(s)-1]
	case 'G', 'g':
		multiplier = 1024 * 1024 * 1024
		numStr = s[:len(s)-1]
	}

	var num int64
	_, err := fmt.Sscanf(numStr, "%d", &num)
	if err != nil {
		return 0, fmt.Errorf("invalid size: %s", s)
	}

	return num * multiplier, nil
}
