package nya

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func benchXzBytes(data []byte, outDir, name string) (int, time.Duration, error) {
	in := filepath.Join(outDir, name)
	if err := os.WriteFile(in, data, 0o644); err != nil {
		return 0, 0, err
	}
	cmd := exec.Command("xz", "-9", "-c", in)
	start := time.Now()
	comp, err := cmd.Output()
	if err != nil {
		return 0, time.Since(start), fmt.Errorf("xz: %w", err)
	}
	return len(comp), time.Since(start), nil
}

func bench7zPath(path, outDir string, solid bool) (int, time.Duration, error) {
	out := filepath.Join(outDir, "out.7z")
	os.Remove(out)
	args := []string{"a", "-bd", "-mx=9", "-bso0", "-bsp0"}
	if solid {
		args = append(args, "-ms=on")
	}
	args = append(args, out, path)
	start := time.Now()
	cmd := exec.Command("7z", args...)
	cmd.Dir = outDir
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		return 0, time.Since(start), fmt.Errorf("7z: %s", string(outBytes))
	}
	fi, err := os.Stat(out)
	if err != nil {
		return 0, time.Since(start), err
	}
	return int(fi.Size()), time.Since(start), nil
}

func benchZstdBytes(data []byte, outDir, name string) (int, time.Duration, error) {
	in := filepath.Join(outDir, name)
	out := filepath.Join(outDir, "out.zst")
	if err := os.WriteFile(in, data, 0o644); err != nil {
		return 0, 0, err
	}
	start := time.Now()
	cmd := exec.Command("zstd", "-19", "-f", "-q", "-o", out, in)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return 0, time.Since(start), fmt.Errorf("zstd: %s", string(msg))
	}
	fi, err := os.Stat(out)
	if err != nil {
		return 0, time.Since(start), err
	}
	return int(fi.Size()), time.Since(start), nil
}

func externalToolsAvailable() bool {
	for _, tool := range []string{"xz", "7z", "zstd"} {
		if _, err := exec.LookPath(tool); err != nil {
			return false
		}
	}
	return true
}

func formatRatioPct(compSize, rawSize int) string {
	if rawSize == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", float64(compSize)/float64(rawSize)*100)
}

func formatRatioTime(compSize, rawSize int, d time.Duration) string {
	return fmt.Sprintf("%s (%s)", formatRatioPct(compSize, rawSize), d.Round(time.Millisecond))
}
