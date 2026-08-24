package nya

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

type damageMode int

const (
	damageErase damageMode = iota
	damageXOR
	damageTruncate
)

func (m damageMode) String() string {
	switch m {
	case damageErase:
		return "erase"
	case damageXOR:
		return "xor"
	case damageTruncate:
		return "tail_erase"
	default:
		return "unknown"
	}
}

// corruptArchive applies one damage pattern to a copy of archivePath written to dest.
func corruptArchive(t *testing.T, archivePath, dest string, mode damageMode, percent int) {
	t.Helper()
	raw, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	switch mode {
	case damageErase:
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		corruptCompressedPayload(t, dest, percent)
	case damageXOR:
		r, err := Open(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		dataOff := uint64(0)
		if r.Header.Flags&FlagSolidCompress == 0 {
			e := firstFileEntry(r.Entries)
			if e == nil {
				t.Fatal("no file entry")
			}
			dataOff = e.FirstDataOff
		}
		ch, err := ReadChunkHeader(bytes.NewReader(r.data[dataOff:]))
		if err != nil {
			t.Fatal(err)
		}
		compStart := int(GlobalHeaderSize) + int(dataOff) + ChunkHeaderSize
		compLen := int(ch.CompressedSize)
		flips := compLen * percent / 100
		if flips <= 0 {
			flips = 1
		}
		rng := rand.New(rand.NewSource(int64(compStart ^ percent)))
		for i := 0; i < flips; i++ {
			pos := compStart + rng.Intn(compLen)
			raw[pos] ^= byte(1 << (i % 8))
		}
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	case damageTruncate:
		r, err := Open(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		dataOff := uint64(0)
		if r.Header.Flags&FlagSolidCompress == 0 {
			e := firstFileEntry(r.Entries)
			if e == nil {
				t.Fatal("no file entry")
			}
			dataOff = e.FirstDataOff
		}
		ch, err := ReadChunkHeader(bytes.NewReader(r.data[dataOff:]))
		if err != nil {
			t.Fatal(err)
		}
		compStart := int(GlobalHeaderSize) + int(dataOff) + ChunkHeaderSize
		compLen := int(ch.CompressedSize)
		eraseBytes := compLen * percent / 100
		if eraseBytes <= 0 {
			eraseBytes = 1
		}
		for i := 0; i < eraseBytes && compStart+compLen-1-i >= compStart; i++ {
			raw[compStart+compLen-1-i] = 0
		}
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown damage mode %d", mode)
	}
}

func createMultiFileArchive(t *testing.T, fecPercent int) string {
	t.Helper()
	srcDir, _ := buildTree(t)
	archive := filepath.Join(t.TempDir(), "multi.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, fecPercent, 5, false)
	w.SetFECType(FECHybrid)
	if err := w.AddFile(srcDir); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return archive
}

func expectRecovery(t *testing.T, archive, label string, mode damageMode, fecPercent, damagePercent int, wantOK bool) {
	t.Helper()
	damaged := filepath.Join(t.TempDir(), "damaged.nya")
	corruptArchive(t, archive, damaged, mode, damagePercent)
	ok := tryRepairAndVerify(t, damaged)
	if ok != wantOK {
		t.Errorf("%s mode=%s fec=%d%% damage=%d%%: recovered=%v want=%v",
			label, mode, fecPercent, damagePercent, ok, wantOK)
	} else {
		t.Logf("%s mode=%s fec=%d%% damage=%d%%: recovered=%v (expected)",
			label, mode, fecPercent, damagePercent, ok)
	}
}

// TestFECRecoveryMatrix sweeps FEC redundancy, damage mode, and archive layout.
func TestFECRecoveryMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FEC matrix in -short mode")
	}

	payload := make([]byte, 2*1024*1024)
	for i := range payload {
		payload[i] = byte(i ^ (i >> 8))
	}

	fecLevels := []int{5, 10, 20, 30}
	modes := []damageMode{damageErase, damageXOR, damageTruncate}

	for _, fecPercent := range fecLevels {
		solidArchive := createSolidArchive(t, fecPercent, 5, payload)
		multiArchive := createMultiFileArchive(t, fecPercent)

		// Recoverable damage: ~60% of configured FEC (conservative for CI).
		recoverPct := fecPercent * 60 / 100
		if recoverPct < 2 {
			recoverPct = 2
		}

		for _, mode := range modes {
			damagePct := recoverPct
			t.Run("solid_"+mode.String()+"_fec"+itoa(fecPercent), func(t *testing.T) {
				expectRecovery(t, solidArchive, "solid", mode, fecPercent, damagePct, true)
			})
			t.Run("multi_"+mode.String()+"_fec"+itoa(fecPercent), func(t *testing.T) {
				expectRecovery(t, multiArchive, "multi", mode, fecPercent, damagePct, true)
			})
		}

	}
}
