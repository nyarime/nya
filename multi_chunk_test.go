package nya

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitRawChunkSizes(t *testing.T) {
	cases := []struct {
		total  int
		enable bool
		want   int
	}{
		{1024, true, 1},
		{4 * 1024 * 1024, true, 1},
		{4*1024*1024 + 1, true, 2},
		{10 * 1024 * 1024, true, 3},
		{10 * 1024 * 1024, false, 1},
	}
	for _, tc := range cases {
		got := splitRawChunkSizes(tc.total, 0, tc.enable)
		if len(got) != tc.want {
			t.Errorf("total=%d enable=%v: got %d chunks %v", tc.total, tc.enable, len(got), got)
		}
		sum := 0
		for _, s := range got {
			sum += s
		}
		if sum != tc.total {
			t.Errorf("total=%d: chunk sizes sum to %d", tc.total, sum)
		}
	}
}

func TestMultiChunkRoundtrip(t *testing.T) {
	payload := make([]byte, 5*1024*1024)
	for i := range payload {
		payload[i] = byte(i ^ (i >> 7))
	}
	src := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "multi.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 10, 7, false)
	if err := w.AddFile(src); err != nil {
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
	if r.Header.VersionMinor != VersionMinorMultiChunk {
		t.Fatalf("VersionMinor=%d want %d", r.Header.VersionMinor, VersionMinorMultiChunk)
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
	if fileEntry.ChunkCount < 2 {
		t.Fatalf("ChunkCount=%d, want >= 2 for 5MiB file", fileEntry.ChunkCount)
	}
	if !r.Verify() {
		t.Fatal("verify failed")
	}
	out := t.TempDir()
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch: got %d bytes", len(got))
	}
}

func TestMultiChunkBCJBoundary(t *testing.T) {
	payload := make([]byte, 6*1024*1024)
	for i := range payload {
		payload[i] = byte(i * 3)
	}
	src := filepath.Join(t.TempDir(), "p.bin")
	os.WriteFile(src, payload, 0644)
	archive := filepath.Join(t.TempDir(), "single.nya")
	f, _ := os.Create(archive)
	w := NewWriterOpts(f, 15, 5, false)
	w.SetMultiChunk(false)
	w.AddFile(src)
	w.Close()
	f.Close()
	r, _ := Open(archive)
	out := t.TempDir()
	r.Extract(out)
	got, _ := os.ReadFile(filepath.Join(out, "p.bin"))
	if !bytes.Equal(got, payload) {
		t.Fatal("single-chunk 6MB extract failed")
	}
}

func TestMultiChunkRoundtripWithFEC(t *testing.T) {
	payload := make([]byte, 6*1024*1024)
	for i := range payload {
		payload[i] = byte(i * 3)
	}
	archive := createMultiChunkArchive(t, payload, 15)
	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(out, "payload.bin"))
	if !bytes.Equal(got, payload) {
		t.Fatal("roundtrip with FEC failed")
	}
}

func TestMultiChunkRepairSingleChunk(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping repair test in -short mode")
	}
	payload := make([]byte, 6*1024*1024)
	for i := range payload {
		payload[i] = byte(i * 3)
	}
	archive := createMultiChunkArchive(t, payload, 15)

	damaged := filepath.Join(t.TempDir(), "damaged.nya")
	raw, _ := os.ReadFile(archive)
	if err := os.WriteFile(damaged, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	refs := r.buildFileChunkRefs()
	if len(refs) < 2 {
		t.Fatalf("expected >=2 chunk refs, got %d", len(refs))
	}
	target := refs[1]
	compStart := int(target.dataOff) + ChunkHeaderSize
	for i := 0; i < 256 && compStart+i < len(raw); i++ {
		raw[int(GlobalHeaderSize)+compStart+i] = 0
	}
	if err := os.WriteFile(damaged, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	rDamaged, err := Open(damaged)
	if err != nil {
		t.Fatal(err)
	}
	if rDamaged.Verify() {
		t.Fatal("damaged archive should not verify before repair")
	}

	repaired := damaged + ".repaired"
	res, err := Repair(damaged, repaired)
	if err != nil {
		t.Fatal(err)
	}
	if res.RepairedChunks == 0 {
		t.Fatalf("expected repair to fix a chunk, result=%+v", res)
	}
	if res.FailedChunks > 0 {
		t.Fatalf("repair failed chunks: %d", res.FailedChunks)
	}
	r2, err := Open(repaired)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Verify() {
		t.Fatal("repaired archive verify failed")
	}
	out := t.TempDir()
	if err := r2.Extract(out); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(out, "payload.bin"))
	if len(got) != len(payload) {
		t.Fatalf("size mismatch: got %d want %d", len(got), len(payload))
	}
	for i := range payload {
		if got[i] != payload[i] {
			t.Fatalf("first diff at %d: got %d want %d (repaired=%d corrupted=%d)",
				i, got[i], payload[i], res.RepairedChunks, res.CorruptedChunks)
		}
	}
}

func TestSmallFileStaysSingleChunk(t *testing.T) {
	payload := bytes.Repeat([]byte("small\n"), 1000)
	src := filepath.Join(t.TempDir(), "small.txt")
	os.WriteFile(src, payload, 0o644)

	archive := filepath.Join(t.TempDir(), "small.nya")
	f, _ := os.Create(archive)
	w := NewWriterOpts(f, 0, 5, false)
	w.AddFile(src)
	w.Close()
	f.Close()

	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.VersionMinor != VersionMinor {
		t.Errorf("minor=%d want %d for small file", r.Header.VersionMinor, VersionMinor)
	}
	for _, e := range r.Entries {
		if e.EntryType == EntryFile && e.ChunkCount != 1 {
			t.Errorf("ChunkCount=%d want 1", e.ChunkCount)
		}
	}
}

func TestMultiChunkParallelExtract(t *testing.T) {
	payload := make([]byte, 6*1024*1024)
	for i := range payload {
		payload[i] = byte(i * 3)
	}
	archive := createMultiChunkArchive(t, payload, 15)

	for _, workers := range []int{1, 4, 8} {
		r, err := Open(archive)
		if err != nil {
			t.Fatal(err)
		}
		r.SetWorkers(workers)
		out := filepath.Join(t.TempDir(), fmt.Sprintf("w%d", workers))
		if err := r.Extract(out); err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		got, err := os.ReadFile(filepath.Join(out, "payload.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("workers=%d: content mismatch", workers)
		}
	}
}

func TestSolidIgnoresMultiChunk(t *testing.T) {
	payload := make([]byte, 6*1024*1024)
	for i := range payload {
		payload[i] = byte(i)
	}
	src := filepath.Join(t.TempDir(), "big.bin")
	os.WriteFile(src, payload, 0o644)

	archive := filepath.Join(t.TempDir(), "solid.nya")
	f, _ := os.Create(archive)
	w := NewWriterOpts(f, 0, 5, true)
	w.AddFile(src)
	w.Close()
	f.Close()

	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.Flags&FlagSolidCompress == 0 {
		t.Fatal("expected solid flag")
	}
	for _, e := range r.Entries {
		if e.EntryType == EntryFile && e.ChunkCount != 1 {
			t.Errorf("solid entry ChunkCount=%d want 1", e.ChunkCount)
		}
	}
}

func createMultiChunkArchive(t *testing.T, payload []byte, fec int) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "multi.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, fec, 5, false)
	if err := w.AddFile(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return archive
}

func TestMultiChunkEncryptedRoundtrip(t *testing.T) {
	payload := make([]byte, 5*1024*1024)
	for i := range payload {
		payload[i] = byte(i*3 + 7)
	}
	src := filepath.Join(t.TempDir(), "enc.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "enc.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 15, 3, false, []byte("multi-chunk-secret"))
	if err := w.AddFile(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if _, err := Open(archive); err != ErrPasswordRequired {
		t.Fatalf("Open without password: got %v", err)
	}
	r, err := Open(archive, []byte("multi-chunk-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.VersionMinor < VersionMinorMultiChunk {
		t.Fatalf("VersionMinor=%d want >= %d", r.Header.VersionMinor, VersionMinorMultiChunk)
	}
	var fileEntry *DirEntry
	for i := range r.Entries {
		if r.Entries[i].EntryType == EntryFile {
			e := r.Entries[i]
			fileEntry = &e
			break
		}
	}
	if fileEntry == nil || fileEntry.ChunkCount < 2 {
		t.Fatalf("want multi-chunk encrypted entry, got %#v", fileEntry)
	}
	out := t.TempDir()
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "enc.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("encrypted multi-chunk content mismatch")
	}
}
