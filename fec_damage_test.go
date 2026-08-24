package nya

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// corruptCompressedPayload zeros erasePercent% of the first file chunk's
// compressed payload in-place within raw archive bytes.
func corruptCompressedPayload(t *testing.T, archivePath string, erasePercent int) {
	t.Helper()
	raw, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
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
	eraseBytes := compLen * erasePercent / 100
	if eraseBytes <= 0 {
		eraseBytes = 1
	}
	for i := 0; i < eraseBytes && compStart+i < len(raw); i++ {
		raw[compStart+i] = 0
	}
	if err := os.WriteFile(archivePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func tryRepairAndVerify(t *testing.T, damagedPath string) bool {
	t.Helper()
	repaired := damagedPath + ".repaired"
	res, err := Repair(damagedPath, repaired)
	if err != nil {
		t.Logf("repair error: %v", err)
		return false
	}
	if res.FailedChunks > 0 {
		t.Logf("repair failed chunks: %d/%d", res.FailedChunks, res.TotalChunks)
		return false
	}
	r, err := Open(repaired)
	if err != nil {
		t.Logf("open repaired: %v", err)
		return false
	}
	return r.Verify()
}

func TestFECMaxRecoveryLeopard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping damage sweep in -short mode")
	}
	payload := make([]byte, 5*1024*1024)
	for i := range payload {
		payload[i] = byte(i ^ (i >> 8))
	}

	for _, fecPercent := range []int{10, 30, 50} {
		t.Run("leopard_fec"+itoa(fecPercent), func(t *testing.T) {
			archive := createSolidArchive(t, fecPercent, 0, payload)
			maxOK := 0
			step := 5
			if fecPercent <= 10 {
				step = 2
			}
			for pct := step; pct <= fecPercent+step; pct += step {
				damaged := filepath.Join(t.TempDir(), "damaged.nya")
				src, _ := os.ReadFile(archive)
				if err := os.WriteFile(damaged, src, 0o644); err != nil {
					t.Fatal(err)
				}
				corruptCompressedPayload(t, damaged, pct)
				if tryRepairAndVerify(t, damaged) {
					maxOK = pct
				} else {
					break
				}
			}
			t.Logf("fec=%d%%: max recoverable erasure ≈ %d%% of compressed payload", fecPercent, maxOK)
			// Leopard RS should recover up to roughly the parity ratio.
			minExpected := fecPercent * 70 / 100
			if minExpected < 1 {
				minExpected = 1
			}
			if maxOK < minExpected {
				t.Errorf("max recoverable %d%% < expected ~%d%% for fec=%d", maxOK, fecPercent, fecPercent)
			}
		})
	}
}

func TestFECMaxRecoveryHybrid(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping damage sweep in -short mode")
	}
	payload := make([]byte, 256*1024)
	rng := rand.New(rand.NewSource(99))
	rng.Read(payload)

	for _, fecPercent := range []int{10, 30, 50} {
		t.Run("hybrid_fec"+itoa(fecPercent), func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "payload.bin")
			if err := os.WriteFile(src, payload, 0o644); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(t.TempDir(), "test.nya")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			w := NewWriterOpts(f, fecPercent, 5, false)
			w.SetFECType(FECHybrid)
			if err := w.AddFile(src); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			f.Close()

			maxOK := 0
			for pct := 5; pct <= 55; pct += 5 {
				damaged := filepath.Join(t.TempDir(), "damaged.nya")
				srcRaw, _ := os.ReadFile(archive)
				if err := os.WriteFile(damaged, srcRaw, 0o644); err != nil {
					t.Fatal(err)
				}
				corruptCompressedPayload(t, damaged, pct)
				if tryRepairAndVerify(t, damaged) {
					maxOK = pct
				} else {
					break
				}
			}
			t.Logf("hybrid fec=%d%%: max recoverable erasure ≈ %d%%", fecPercent, maxOK)
		})
	}
}

func TestChineseUTF8Paths(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"中文文件名.txt",
		"文档/报告-2026.pdf",
		"emoji/测试🎉.bin",
	}
	want := map[string][]byte{}
	for _, name := range names {
		content := []byte("内容 for " + name)
		want[name] = content
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	archive := filepath.Join(t.TempDir(), "中文.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 10, 5, false)
	if err := w.AddFile(dir); err != nil {
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
	found := map[string]bool{}
	for _, e := range r.Entries {
		if e.EntryType == EntryFile {
			found[e.Path] = true
		}
	}
	base := filepath.Base(dir)
	for name := range want {
		rel := filepath.ToSlash(filepath.Join(base, name))
		if !found[rel] {
			t.Errorf("missing entry %q in central directory", rel)
		}
	}

	extractDir := t.TempDir()
	if err := r.Extract(extractDir); err != nil {
		t.Fatal(err)
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(extractDir, base, name))
		if err != nil {
			t.Errorf("extract %q: %v", name, err)
			continue
		}
		if !bytes.Equal(got, content) {
			t.Errorf("content mismatch for %q", name)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
