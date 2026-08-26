package nya

import "testing"

func TestBlockSizeForParallel(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	bs := BlockSizeForParallel(gb, 200)
	want := int64((gb + 199) / 200)
	if bs != want {
		t.Fatalf("1GiB/200 = %d got %d", want, bs)
	}
	small := BlockSizeForParallel(1024*1024, 200)
	if small != minDownloadBlock {
		t.Fatalf("small file min block got %d", small)
	}
}

func TestDownloadConcurrency(t *testing.T) {
	m12 := &Manifest{Download: DownloadIndex{Blocks: make([]DownloadBlock, 12)}}
	if got := DownloadConcurrency(m12, 0); got != 12 {
		t.Fatalf("auto 12 blocks want 12 got %d", got)
	}
	m300 := &Manifest{Download: DownloadIndex{Blocks: make([]DownloadBlock, 300)}}
	if got := DownloadConcurrency(m300, 0); got != TryCloudflareMaxParallel {
		t.Fatalf("auto 300 blocks want %d got %d", TryCloudflareMaxParallel, got)
	}
	if got := DownloadConcurrency(m12, 8); got != 8 {
		t.Fatalf("explicit 8 got %d", got)
	}
	if got := DownloadConcurrency(m12, 99); got != 12 {
		t.Fatalf("cap at blocks got %d", got)
	}
}

func TestBlockSizeForParallel200Blocks47MB(t *testing.T) {
	const size = 49874167
	bs := BlockSizeForParallel(size, TryCloudflareMaxParallel)
	if bs < minDownloadBlock {
		t.Fatalf("block too small: %d", bs)
	}
	// ceil(47.6MB / 200) ≈ 250KB → 200-ish blocks when embedded
	blocks := (size + bs - 1) / bs
	if blocks > TryCloudflareMaxParallel {
		t.Fatalf("blocks=%d want <= %d", blocks, TryCloudflareMaxParallel)
	}
}
