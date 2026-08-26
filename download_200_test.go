package nya

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestDownload200Concurrent verifies TryCloudflare-scale parallelism (200 in-flight blocks).
func TestDownload200Concurrent(t *testing.T) {
	const blocks = TryCloudflareMaxParallel
	const blockSize = 64 * 1024
	payload := make([]byte, blocks*blockSize)
	for i := range payload {
		payload[i] = byte(i)
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "wide.nya")
	if err := os.WriteFile(archive, payload, 0644); err != nil {
		t.Fatal(err)
	}
	bs := BlockSizeForParallel(int64(len(payload)), TryCloudflareMaxParallel)
	m, err := BuildManifest(archive, bs)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Download.Blocks) != blocks {
		t.Fatalf("blocks=%d want %d (block_size=%d)", len(m.Download.Blocks), blocks, bs)
	}

	var peak atomic.Int32
	var active atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := active.Add(1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		defer active.Add(-1)
		<-release

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
	workers := DownloadConcurrency(m, 0)
	if workers != blocks {
		t.Fatalf("workers=%d want %d", workers, blocks)
	}

	done := make(chan struct{})
	go func() {
		res, err := Download(context.Background(), DownloadOptions{
			Manifest:    m,
			Output:      out,
			Concurrency: workers,
			StatePath:   filepath.Join(dir, "wide.nyam.state"),
			HTTPClient:  &http.Client{Transport: DownloadHTTPTransport()},
		})
		if err != nil {
			t.Error(err)
			close(release)
			return
		}
		if res.BlocksFetched != blocks {
			t.Errorf("fetched=%d want %d", res.BlocksFetched, blocks)
		}
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if int(peak.Load()) >= 50 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	<-done

	if got := peak.Load(); got < 50 {
		t.Fatalf("peak concurrent requests=%d (expected high parallelism)", got)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatal("output mismatch")
	}
}
