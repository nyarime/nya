package nya

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestFECAdaptiveParams(t *testing.T) {
	cases := []struct {
		size    int
		minK    int
		maxSym  int
		minSym  int
	}{
		{8 * 1024, fecKMin, 1024, fecSymbolMin},
		{200 * 1024, 32, 1024, 512},
		{2 * 1024 * 1024, 64, 4096, 2048},
	}
	for _, tc := range cases {
		plan := planFEC(tc.size, 30, FECHybrid, false)
		if plan.K < tc.minK {
			t.Errorf("size=%d: K=%d, want >= %d", tc.size, plan.K, tc.minK)
		}
		if plan.SymbolSize < tc.minSym || plan.SymbolSize > tc.maxSym {
			t.Errorf("size=%d: sym=%d, want [%d,%d]", tc.size, plan.SymbolSize, tc.minSym, tc.maxSym)
		}
		if plan.LDPCParity < 1 || plan.RQRepair < 1 {
			t.Errorf("size=%d: hybrid parity missing: ldpc=%d rq=%d", tc.size, plan.LDPCParity, plan.RQRepair)
		}
	}
}

func TestHybridFECRoundtrip(t *testing.T) {
	data := make([]byte, 256*1024)
	rand.New(rand.NewSource(42)).Read(data)

	for _, fecType := range []uint8{FECRaptorQ, FECLDPC, FECHybrid} {
		t.Run(fecTypeName(fecType), func(t *testing.T) {
			fec, hashes, plan := encodeFEC(data, 30, fecType, false)
			if len(fec) == 0 {
				t.Fatal("no fec produced")
			}
			got, err := repairWithPlan(data, fec, plan, hashes)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("roundtrip mismatch")
			}
		})
	}
}

func TestHybridFECRecovery(t *testing.T) {
	data := make([]byte, 32*1024)
	for i := range data {
		data[i] = byte(i * 13)
	}

	for _, fecType := range []uint8{FECLDPC, FECRaptorQ, FECHybrid} {
		t.Run(fecTypeName(fecType), func(t *testing.T) {
			fec, hashes, plan := encodeFEC(data, 50, fecType, false)
			damaged := append([]byte(nil), data...)

			// Corrupt one full symbol.
			symSize := plan.SymbolSize
			for j := 0; j < symSize; j++ {
				damaged[j] ^= 0xff
			}

			got, err := repairWithPlan(damaged, fec, plan, hashes)
			if err != nil {
				t.Fatalf("repair: %v (K=%d ldpc=%d rq=%d sym=%d)", err, plan.K, plan.LDPCParity, plan.RQRepair, plan.SymbolSize)
			}
			if !bytes.Equal(got, data) {
				t.Fatal("repair output mismatch")
			}
		})
	}
}

func TestArchiveFECRoundtrip(t *testing.T) {
	srcDir, want := buildTree(t)
	base := filepath.Base(srcDir)

	archive := filepath.Join(t.TempDir(), "fec.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 30, 5, false)
	if err := w.AddFile(srcDir); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Verify() {
		t.Fatal("archive verify failed")
	}
	var fileEntry *DirEntry
	for i := range r.Entries {
		if r.Entries[i].EntryType == EntryFile {
			e := r.Entries[i]
			fileEntry = &e
			break
		}
	}
	if fileEntry == nil {
		t.Fatal("no file entry")
	}
	if fileEntry.FECType != DefaultFECType {
		t.Errorf("FECType = %d, want %d", fileEntry.FECType, DefaultFECType)
	}
	if fileEntry.FECParams.Param1 < uint32(fecKMin) {
		t.Errorf("adaptive K too small: %d", fileEntry.FECParams.Param1)
	}
	if r.Header.Flags&FlagHasGlobalFEC == 0 {
		t.Error("expected global metadata FEC flag")
	}
	if len(r.globalMetaFec) == 0 {
		t.Error("expected global metadata FEC payload")
	}

	out := t.TempDir()
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(out, base, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !bytes.Equal(got, content) {
			t.Errorf("%s: content mismatch", name)
		}
	}
}

func fecTypeName(t uint8) string {
	switch t {
	case FECRaptorQ:
		return "raptorq"
	case FECLDPC:
		return "ldpc"
	case FECHybrid:
		return "hybrid"
	default:
		return "unknown"
	}
}
