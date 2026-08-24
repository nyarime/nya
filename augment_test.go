package nya

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func createSolidArchive(t *testing.T, fecPercent, level int, payload []byte) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "test.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, fecPercent, level, true)
	if err := w.AddFile(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return archive
}

func TestAugmentFromZeroFEC(t *testing.T) {
	payload := make([]byte, 5*1024*1024)
	for i := range payload {
		payload[i] = byte(i)
	}
	archive := createSolidArchive(t, 0, 0, payload)

	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	if r.FecLen != 0 {
		t.Fatalf("expected no FEC, got fecLen=%d", r.FecLen)
	}

	out := filepath.Join(t.TempDir(), "aug.nya")
	res, err := Augment(archive, out, 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewPercent != 10 {
		t.Errorf("NewPercent=%d, want 10", res.NewPercent)
	}
	if res.ExtraBytes <= 0 {
		t.Error("expected positive ExtraBytes")
	}

	r2, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if r2.FecLen == 0 {
		t.Fatal("augmented archive should have FEC data")
	}
	e := firstFileEntry(r2.Entries)
	if e == nil {
		t.Fatal("no file entry")
	}
	if e.FECType != FECRS {
		t.Errorf("FECType=%d, want Leopard FECRS (%d)", e.FECType, FECRS)
	}
	if !r2.Verify() {
		t.Error("augmented archive failed verify")
	}
}

func TestAugmentLeopardIncrease(t *testing.T) {
	payload := make([]byte, 5*1024*1024)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	archive := createSolidArchive(t, 10, 0, payload)

	out := filepath.Join(t.TempDir(), "aug.nya")
	res, err := Augment(archive, out, 5)
	if err != nil {
		t.Fatal(err)
	}
	if res.OldPercent != 10 || res.NewPercent != 15 {
		t.Errorf("percent: old=%d new=%d, want 10/15", res.OldPercent, res.NewPercent)
	}

	r, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	e := firstFileEntry(r.Entries)
	if e == nil {
		t.Fatal("no file entry")
	}
	if e.FECType != FECRS {
		t.Errorf("FECType=%d, want FECRS", e.FECType)
	}
	plan := planFromParams(e.FECParams, e.FECType)
	if plan.Percent != 15 {
		t.Errorf("stored percent=%d, want 15", plan.Percent)
	}
	if !r.Verify() {
		t.Error("verify failed after augment")
	}
}

func TestAugmentSolidExtractRoundtrip(t *testing.T) {
	payload := bytes.Repeat([]byte("solid augment roundtrip\n"), 200_000)
	archive := createSolidArchive(t, 5, 1, payload)

	out := filepath.Join(t.TempDir(), "aug.nya")
	if _, err := Augment(archive, out, 10); err != nil {
		t.Fatal(err)
	}

	extractDir := t.TempDir()
	r, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Extract(extractDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(extractDir, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("extract mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

func TestAugmentMultiFileHybrid(t *testing.T) {
	srcDir, want := buildTree(t)
	base := filepath.Base(srcDir)

	archive := filepath.Join(t.TempDir(), "multi.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 10, 5, false)
	if err := w.AddFile(srcDir); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	out := filepath.Join(t.TempDir(), "aug.nya")
	res, err := Augment(archive, out, 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewPercent != 20 {
		t.Errorf("NewPercent=%d, want 20", res.NewPercent)
	}

	extractDir := t.TempDir()
	r, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Extract(extractDir); err != nil {
		t.Fatal(err)
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(extractDir, base, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !bytes.Equal(got, content) {
			t.Errorf("%s: content mismatch", name)
		}
	}
}
