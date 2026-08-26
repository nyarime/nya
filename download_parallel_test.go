package nya

import "testing"

func TestBlockSizeForParallel(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	bs := BlockSizeForParallel(gb, 20)
	want := int64((gb + 19) / 20)
	if bs != want {
		t.Fatalf("1GiB/20 = %d got %d", want, bs)
	}
	small := BlockSizeForParallel(1024*1024, 20)
	if small != minDownloadBlock {
		t.Fatalf("small file min block got %d", small)
	}
}

func TestDownloadConcurrency(t *testing.T) {
	m12 := &Manifest{Download: DownloadIndex{Blocks: make([]DownloadBlock, 12)}}
	if got := DownloadConcurrency(m12, 0); got != 12 {
		t.Fatalf("auto 12 blocks want 12 got %d", got)
	}
	m30 := &Manifest{Download: DownloadIndex{Blocks: make([]DownloadBlock, 30)}}
	if got := DownloadConcurrency(m30, 0); got != TryCloudflareMaxParallel {
		t.Fatalf("auto 30 blocks want %d got %d", TryCloudflareMaxParallel, got)
	}
	if got := DownloadConcurrency(m12, 8); got != 8 {
		t.Fatalf("explicit 8 got %d", got)
	}
	if got := DownloadConcurrency(m12, 99); got != 12 {
		t.Fatalf("cap at blocks got %d", got)
	}
}
