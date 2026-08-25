package nya

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbedDownloadIndexRoundtrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("hello embed index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "pack.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, LevelFastest, false)
	if err := w.AddFile(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	res, err := EmbedDownloadIndex(archive, EmbedOptions{BlockSize: 1024, InPlace: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.BlockCount < 1 || res.FinalSize <= res.BodySize {
		t.Fatalf("unexpected embed result: %+v", res)
	}

	m, err := ManifestFromEmbeddedFile(archive, "https://example.test/pack.nya")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if m.Archive.Size != res.BodySize {
		t.Fatalf("manifest size %d want body %d", m.Archive.Size, res.BodySize)
	}

	out := filepath.Join(dir, "out")
	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello embed index\n" {
		t.Fatalf("got %q", got)
	}

	res2, err := EmbedDownloadIndex(archive, EmbedOptions{BlockSize: 1024, InPlace: true})
	if err != nil {
		t.Fatal(err)
	}
	if res2.BodySize != res.BodySize {
		t.Fatalf("re-embed body %d want %d", res2.BodySize, res.BodySize)
	}

	had, err := HasEmbeddedDownloadIndex(archive)
	if err != nil || !had {
		t.Fatalf("HasEmbedded=%v %v", had, err)
	}
	st, err := StripDownloadIndex(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !st.HadIndex || st.BodySize != res.BodySize {
		t.Fatalf("strip: %+v", st)
	}
	had, err = HasEmbeddedDownloadIndex(archive)
	if err != nil || had {
		t.Fatalf("after strip HasEmbedded=%v %v", had, err)
	}
	st2, err := StripDownloadIndex(archive)
	if err != nil || st2.HadIndex {
		t.Fatalf("idempotent strip: %+v %v", st2, err)
	}
}

func TestBootstrapManifestFromURL(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(src, []byte("bootstrap me"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "boot.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, LevelFastest, false)
	if err := w.AddFile(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := EmbedDownloadIndex(archive, EmbedOptions{BlockSize: 512, InPlace: true}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()

	url := srv.URL + "/boot.nya"
	m, err := BootstrapManifestFromURL(srv.Client(), url)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Sources) != 1 || m.Sources[0].URL != url {
		t.Fatalf("sources: %+v", m.Sources)
	}

	out := filepath.Join(dir, "downloaded.nya")
	res, err := Download(t.Context(), DownloadOptions{
		Manifest:    m,
		Output:      out,
		Concurrency: 2,
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BlocksTotal < 1 {
		t.Fatal("no blocks")
	}
	r, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	ext := filepath.Join(dir, "ext")
	if err := r.Extract(ext); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(ext, "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "bootstrap me" {
		t.Fatalf("got %q", got)
	}
}
