package main

import (
	"fmt"
	"os"

	"github.com/nyarime/nya"
)

func getStatusf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	_ = os.Stderr.Sync()
}

func printGetManifestSummary(m *nya.Manifest) {
	if m == nil {
		return
	}
	getStatusf("nya get: %s  %s  (%d blocks)\n",
		m.Archive.Name,
		nya.HumanSize(int(m.Archive.Size)),
		len(m.Download.Blocks),
	)
	for _, e := range m.Entries {
		getStatusf("  %s  %s\n", e.Path, nya.HumanSize(int(e.OriginalSize)))
	}
	if len(m.Entries) == 0 {
		getStatusf("  (no embedded file list)\n")
	}
}
