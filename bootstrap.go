package nya

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

// BootstrapManifestFromURL fetches the EOF download-index footer and tail from
// a single .nya URL (HEAD + two Range GETs) and returns a Manifest ready for Download.
func BootstrapManifestFromURL(client *http.Client, url string) (*Manifest, error) {
	if client == nil {
		client = http.DefaultClient
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("bootstrap: empty url")
	}

	size, err := httpContentLength(client, url)
	if err != nil {
		return nil, err
	}
	if size < DownloadIndexFooterSize+GlobalHeaderSize {
		return nil, fmt.Errorf("bootstrap: remote file too small (%d)", size)
	}

	footer, err := httpRange(client, url, size-DownloadIndexFooterSize, DownloadIndexFooterSize)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: footer: %w", err)
	}
	foot, err := ParseDownloadIndexFooter(footer)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w — archive may lack embedded index (nya manifest --embed)", err)
	}
	if int64(foot.TailChainOffset+foot.TailChainSize+DownloadIndexFooterSize) != size {
		return nil, fmt.Errorf("bootstrap: footer bounds mismatch")
	}

	tail, err := httpRange(client, url, int64(foot.TailChainOffset), int64(foot.TailChainSize))
	if err != nil {
		return nil, fmt.Errorf("bootstrap: tail: %w", err)
	}

	// Present footer+tail as a contiguous ReaderAt via a synthetic buffer of the
	// remote size — only the tail region is populated (enough for ManifestFromEmbeddedReader).
	buf := make([]byte, size)
	copy(buf[foot.TailChainOffset:], tail)
	copy(buf[size-DownloadIndexFooterSize:], footer)

	name := path.Base(strings.Split(url, "?")[0])
	if name == "" || name == "/" || name == "." {
		name = "archive.nya"
	}
	return ManifestFromEmbeddedReader(bytesReaderAt(buf), size, name, url)
}

type bytesReaderAt []byte

func (b bytesReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func httpContentLength(client *http.Client, url string) (int64, error) {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK && resp.ContentLength > 0 {
		return resp.ContentLength, nil
	}
	// Fallback: Range first byte and parse Content-Range: bytes 0-0/TOTAL
	req, err = http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err = client.Do(req)
	if err != nil {
		return 0, err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bootstrap: size probe HTTP %d", resp.StatusCode)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		// bytes 0-0/12345
		var start, end, total int64
		if _, err := fmt.Sscanf(cr, "bytes %d-%d/%d", &start, &end, &total); err == nil && total > 0 {
			return total, nil
		}
	}
	if resp.ContentLength > 0 && resp.StatusCode == http.StatusOK {
		return resp.ContentLength, nil
	}
	return 0, fmt.Errorf("bootstrap: cannot determine remote size")
}

func httpRange(client *http.Client, url string, off, size int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+size-1))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, size+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if int64(len(body)) != size {
		return nil, fmt.Errorf("got %d bytes, want %d", len(body), size)
	}
	return body, nil
}
