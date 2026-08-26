package nya

import (
	"sync"
	"testing"
)

// solidIntegrationMu serializes solid zstd-dict / LZMA2 integration tests.
// Parallel package tests can race zstd dict probe paths in compress (v0.2.x).
var solidIntegrationMu sync.Mutex

func solidIntegrationSerial(t *testing.T) {
	t.Helper()
	solidIntegrationMu.Lock()
	t.Cleanup(solidIntegrationMu.Unlock)
}
