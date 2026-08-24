package nya

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultStubPath returns the embedded or on-disk stub for the current OS/arch.
func DefaultStubPath() (string, error) {
	name := fmt.Sprintf("nya-sfx-stub_%s_%s", runtime.GOOS, runtime.GOARCH)
	// Prefer sfx/stubs next to the running binary (development layout).
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "sfx", "stubs", name)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	// Repository layout when running from source tree.
	candidate := filepath.Join("sfx", "stubs", name)
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		abs, _ := filepath.Abs(candidate)
		return abs, nil
	}
	return "", fmt.Errorf("sfx: stub not found (%s); run: cd sfx && cargo build --release && cp target/release/nya-sfx-stub stubs/%s", name, name)
}
