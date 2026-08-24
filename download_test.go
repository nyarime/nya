package nya

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadParallel(t *testing.T) {
	dir := t.TempDir()
	payload := make([]byte, 256*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	archive := filepath.Join(dir, "game.nya")
	if err := os.WriteFile(archive, payload, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := BuildManifest(archive, 64*1024)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			http.Error(w, "range required", http.StatusBadRequest)
			return
		}
		var start, end int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		chunk := payload[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(chunk)
	}))
	defer srv.Close()

	m.Sources = []ManifestSource{{URL: srv.URL, Priority: 1}}
	out := filepath.Join(dir, "out.nya")

	res, err := Download(context.Background(), DownloadOptions{
		Manifest:    m,
		Output:      out,
		Concurrency: 4,
		StatePath:   filepath.Join(dir, "game.nyam.state"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BlocksFetched != len(m.Download.Blocks) {
		t.Fatalf("fetched %d want %d", res.BlocksFetched, len(m.Download.Blocks))
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatal("output mismatch")
	}
}

func TestDownloadResume(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("abcdefgh01234567")
	archive := filepath.Join(dir, "small.nya")
	if err := os.WriteFile(archive, payload, 0644); err != nil {
		t.Fatal(err)
	}
	m, err := BuildManifest(archive, 8)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int64
		fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		w.WriteHeader(http.StatusPartialContent)
		w.Write(payload[start : end+1])
	}))
	defer srv.Close()
	m.Sources = []ManifestSource{{URL: srv.URL}}

	out := filepath.Join(dir, "out.nya")
	state := filepath.Join(dir, "small.nyam.state")

	// Simulate partial download: full-size file with block 0 valid, block 1 empty.
	if err := os.WriteFile(out, payload, 0644); err != nil {
		t.Fatal(err)
	}
	writeDownloadState(state, out, map[uint32]struct{}{0: {}})

	res, err := Download(context.Background(), DownloadOptions{
		Manifest:    m,
		Output:      out,
		StatePath:   state,
		Resume:      true,
		Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BlocksSkipped != 1 || res.BlocksFetched != 1 {
		t.Fatalf("skipped=%d fetched=%d", res.BlocksSkipped, res.BlocksFetched)
	}
}
