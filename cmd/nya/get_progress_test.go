package main

import (
	"testing"

	"github.com/nyarime/nya"
)

func TestHumanSpeed(t *testing.T) {
	tests := []struct {
		bps  float64
		want string
	}{
		{512, "512 B/s"},
		{2048, "2.0 KB/s"},
		{1536 * 1024, "1.50 MB/s"},
	}
	for _, tc := range tests {
		if got := humanSpeed(tc.bps); got != tc.want {
			t.Fatalf("humanSpeed(%v) = %q want %q", tc.bps, got, tc.want)
		}
	}
}

func TestHumanETA(t *testing.T) {
	if got := humanETA(45); got != "45s" {
		t.Fatalf("45s got %q", got)
	}
	if got := humanETA(125); got != "2m05s" {
		t.Fatalf("125s got %q", got)
	}
}

func TestGetDownloadProgressSnapshot(t *testing.T) {
	m := &nya.Manifest{
		Archive: nya.ArchiveMeta{Size: 10_000_000},
		Download: nya.DownloadIndex{
			Blocks: []nya.DownloadBlock{
				{ID: 0, Size: 4_000_000},
				{ID: 1, Size: 4_000_000},
				{ID: 2, Size: 2_000_000},
			},
		},
	}
	p := newGetDownloadProgress(m)
	p.blockStart(0)
	p.blockBytes(0, 1_000_000)
	p.blockDone(0, 4_000_000)
	p.blockStart(1)
	p.blockBytes(1, 500_000)

	got, total, done, tot := p.snapshot()
	if total != 10_000_000 || got != 4_500_000 || done != 1 || tot != 3 {
		t.Fatalf("snapshot got=%d total=%d blocks=%d/%d", got, total, done, tot)
	}
}
