package nya

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// DownloadOptions configures parallel HTTP Range fetch.
type DownloadOptions struct {
	Manifest    *Manifest
	Output      string
	StatePath   string
	Concurrency int
	HTTPClient  *http.Client
	OnBlock     func(block DownloadBlock, done, total int)
	Resume      bool
	// Paths selects partial fetch: only transport blocks overlapping the
	// manifest entry chunk ranges for these paths (plus header and central dir).
	Paths []string
}

// DownloadResult summarizes a fetch run.
type DownloadResult struct {
	BlocksTotal   int
	BlocksFetched int
	BlocksSkipped int
	BytesWritten  int64
	Elapsed       time.Duration
	Partial       bool // true when Paths was set (no whole-file checksum)
}

// Download fetches a .nya using transport blocks from manifest.
func Download(ctx context.Context, opt DownloadOptions) (*DownloadResult, error) {
	if opt.Manifest == nil {
		return nil, fmt.Errorf("download: manifest required")
	}
	if err := opt.Manifest.Validate(); err != nil {
		return nil, err
	}
	if opt.Output == "" {
		opt.Output = opt.Manifest.Archive.Name
	}
	if opt.Concurrency <= 0 {
		opt.Concurrency = 8
	}
	if opt.HTTPClient == nil {
		opt.HTTPClient = &http.Client{Timeout: 0}
	}
	if len(opt.Manifest.Sources) == 0 {
		return nil, fmt.Errorf("download: manifest has no sources; add --url when running nya manifest")
	}
	if opt.StatePath == "" {
		opt.StatePath = StatePath(opt.Output + ".nyam")
	}

	urls := sortedSourceURLs(opt.Manifest.Sources)
	start := time.Now()

	blocks := opt.Manifest.Download.Blocks
	if len(opt.Paths) > 0 {
		ranges, err := opt.Manifest.FetchRangesForPaths(opt.Paths)
		if err != nil {
			return nil, err
		}
		blocks = filterBlocksByRanges(blocks, ranges)
		if len(blocks) == 0 {
			return nil, fmt.Errorf("download: no transport blocks overlap selected paths")
		}
	}

	out, err := os.OpenFile(opt.Output, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	keepPartial := false
	if opt.Resume {
		if fi, statErr := os.Stat(opt.Output); statErr == nil && fi.Size() == opt.Manifest.Archive.Size {
			keepPartial = true
		}
	}
	if !keepPartial {
		if err := out.Truncate(opt.Manifest.Archive.Size); err != nil {
			return nil, fmt.Errorf("download: preallocate: %w", err)
		}
	}

	done := map[uint32]struct{}{}
	if opt.Resume {
		if st, err := readDownloadState(opt.StatePath); err == nil && st.Output == opt.Output {
			for _, id := range st.Completed {
				done[id] = struct{}{}
			}
		}
	}

	total := len(blocks)
	var pending []DownloadBlock
	for _, b := range blocks {
		if _, ok := done[b.ID]; ok {
			continue
		}
		pending = append(pending, b)
	}

	res := &DownloadResult{
		BlocksTotal:   total,
		BlocksSkipped: total - len(pending),
		Partial:       len(opt.Paths) > 0,
	}

	if len(pending) == 0 {
		if !res.Partial {
			if err := verifyArchiveFile(opt.Output, opt.Manifest); err != nil {
				return res, err
			}
		}
		res.Elapsed = time.Since(start)
		return res, nil
	}

	type job struct{ block DownloadBlock }
	jobs := make(chan job, len(pending))
	var wg sync.WaitGroup
	var fail atomic.Value

	worker := func() {
		defer wg.Done()
		for j := range jobs {
			if ctx.Err() != nil {
				return
			}
			if err := fetchBlock(ctx, opt.HTTPClient, urls, opt.Output, j.block); err != nil {
				fail.Store(err)
				return
			}
			data, err := readFileRange(opt.Output, j.block.Offset, j.block.Size)
			if err != nil {
				fail.Store(err)
				return
			}
			if !blockMatchesHash(data, j.block.Blake3) {
				fail.Store(fmt.Errorf("download: block %d checksum mismatch", j.block.ID))
				return
			}
			done[j.block.ID] = struct{}{}
			res.BlocksFetched++
			res.BytesWritten += j.block.Size
			if opt.OnBlock != nil {
				opt.OnBlock(j.block, len(done), total)
			}
			writeDownloadState(opt.StatePath, opt.Output, done)
		}
	}

	for i := 0; i < opt.Concurrency; i++ {
		wg.Add(1)
		go worker()
	}
	for _, b := range pending {
		jobs <- job{block: b}
	}
	close(jobs)
	wg.Wait()

	if v := fail.Load(); v != nil {
		return res, v.(error)
	}
	if !res.Partial {
		if err := verifyArchiveFile(opt.Output, opt.Manifest); err != nil {
			return res, err
		}
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

func sortedSourceURLs(src []ManifestSource) []string {
	cp := append([]ManifestSource(nil), src...)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].Priority > cp[j].Priority })
	out := make([]string, 0, len(cp))
	for _, s := range cp {
		out = append(out, s.URL)
	}
	return out
}

func fetchBlock(ctx context.Context, client *http.Client, urls []string, output string, block DownloadBlock) error {
	var lastErr error
	for _, url := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		end := block.Offset + block.Size - 1
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", block.Offset, end))

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		if int64(len(body)) != block.Size {
			lastErr = fmt.Errorf("got %d bytes, want %d", len(body), block.Size)
			continue
		}
		if err := writeFileRange(output, block.Offset, body); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no sources")
	}
	return fmt.Errorf("block %d: %w", block.ID, lastErr)
}

func writeFileRange(path string, offset int64, data []byte) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

func readFileRange(path string, offset, size int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	_, err = io.ReadFull(f, buf)
	return buf, err
}

func blockMatchesHash(data []byte, wantHex string) bool {
	h := Blake3Sum256(data)
	return hex.EncodeToString(h[:]) == wantHex
}

func verifyArchiveFile(path string, m *Manifest) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() != m.Archive.Size {
		return fmt.Errorf("download: size %d != manifest %d", fi.Size(), m.Archive.Size)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	h := Blake3Sum256(data)
	if hex.EncodeToString(h[:]) != m.Archive.Blake3 {
		return fmt.Errorf("download: archive checksum mismatch")
	}
	return nil
}

func readDownloadState(path string) (*DownloadState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st DownloadState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func writeDownloadState(path, output string, done map[uint32]struct{}) {
	ids := make([]uint32, 0, len(done))
	for id := range done {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	st := DownloadState{
		Output:    output,
		Completed: ids,
		UpdatedAt: time.Now().UTC(),
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	b = append(b, '\n')
	_ = os.WriteFile(path, b, 0644)
}
