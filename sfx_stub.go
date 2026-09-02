package nya

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultStubPath returns an on-disk stub for the current OS/arch (legacy).
// Prefer SelfStubBytes / BuildSFXAuto which use the running `nya` binary.
// Search order:
//  1. <exeDir>/nya-sfx-stub[.exe]
//  2. ./nya-sfx-stub[.exe] (repo cwd)
func DefaultStubPath() (string, error) {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "nya-sfx-stub"),
			filepath.Join(dir, "nya-sfx-stub.exe"),
		)
	}
	candidates = append(candidates,
		"nya-sfx-stub",
		"nya-sfx-stub.exe",
	)
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return candidate, nil
			}
			return abs, nil
		}
	}
	return "", fmt.Errorf("sfx: stub not found; build: go build -o nya-sfx-stub ./cmd/nya-sfx-stub (or use create -sfx / nya sfx with the running nya binary)")
}
