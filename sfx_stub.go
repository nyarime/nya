package nya

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultStubPath returns the on-disk stub for the current OS/arch.
// Search order:
//  1. <exeDir>/sfx/stubs/nya-sfx-stub_<os>_<arch>[.exe]  (dev / Inno layout)
//  2. <exeDir>/nya-sfx-stub[.exe]                         (install.ps1 / zip layout)
//  3. ./sfx/stubs/nya-sfx-stub_<os>_<arch>[.exe]          (repo cwd)
func DefaultStubPath() (string, error) {
	base := fmt.Sprintf("nya-sfx-stub_%s_%s", runtime.GOOS, runtime.GOARCH)
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "sfx", "stubs", base),
			filepath.Join(dir, "sfx", "stubs", base+".exe"),
			filepath.Join(dir, "nya-sfx-stub"),
			filepath.Join(dir, "nya-sfx-stub.exe"),
		)
	}
	candidates = append(candidates,
		filepath.Join("sfx", "stubs", base),
		filepath.Join("sfx", "stubs", base+".exe"),
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
	return "", fmt.Errorf("sfx: stub not found (%s); place nya-sfx-stub next to nya.exe, or: nya sfx -stub path/to/nya-sfx-stub.exe -o out.exe pack.nya", base)
}
