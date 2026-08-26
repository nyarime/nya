package main

import (
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/nyarime/nya"
)

func getStatusf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	_ = os.Stderr.Sync()
}

func getStatusPrint(s string) {
	fmt.Fprint(os.Stderr, s)
	_ = os.Stderr.Sync()
}

func printGetManifestSummary(m *nya.Manifest) {
	if m == nil {
		return
	}
	getStatusf("nya get: %s  %s  (%d blocks)\n",
		m.Archive.Name,
		nya.HumanSize(int(m.Archive.Size)),
		len(m.Download.Blocks),
	)
	for _, e := range m.Entries {
		getStatusf("  %s  %s\n", e.Path, nya.HumanSize(int(e.OriginalSize)))
	}
	if len(m.Entries) == 0 {
		getStatusf("  (no embedded file list)\n")
	}
}

// getDownloadProgress tracks aggregate byte progress across concurrent block fetches.
type getDownloadProgress struct {
	totalBytes     int64
	totalBlocks    int
	completedBytes int64
	completedBlocks int

	mu         sync.Mutex
	inFlight   map[uint32]int64
	start      time.Time
	lastRender time.Time
}

func newGetDownloadProgress(m *nya.Manifest) *getDownloadProgress {
	return &getDownloadProgress{
		totalBytes:  m.Archive.Size,
		totalBlocks: len(m.Download.Blocks),
		inFlight:    make(map[uint32]int64),
		start:       time.Now(),
	}
}

func (p *getDownloadProgress) init(completedBlocks int, completedBytes int64) {
	p.mu.Lock()
	p.completedBlocks = completedBlocks
	p.completedBytes = completedBytes
	p.mu.Unlock()
	p.render(true)
}

func (p *getDownloadProgress) blockStart(id uint32) {
	p.mu.Lock()
	p.inFlight[id] = 0
	p.mu.Unlock()
}

func (p *getDownloadProgress) blockBytes(id uint32, fetched int64) {
	p.mu.Lock()
	p.inFlight[id] = fetched
	p.mu.Unlock()
	p.render(false)
}

func (p *getDownloadProgress) blockDone(id uint32, size int64) {
	p.mu.Lock()
	delete(p.inFlight, id)
	p.completedBytes += size
	p.completedBlocks++
	p.mu.Unlock()
	p.render(true)
}

func (p *getDownloadProgress) snapshot() (downloaded, total int64, blocksDone, blocksTotal int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var inflight int64
	for _, b := range p.inFlight {
		inflight += b
	}
	return p.completedBytes + inflight, p.totalBytes, p.completedBlocks, p.totalBlocks
}

func (p *getDownloadProgress) render(force bool) {
	now := time.Now()
	if !force && now.Sub(p.lastRender) < 150*time.Millisecond {
		return
	}
	p.lastRender = now

	downloaded, total, blocksDone, blocksTotal := p.snapshot()
	elapsed := now.Sub(p.start).Seconds()
	var speed float64
	if elapsed > 0 {
		speed = float64(downloaded) / elapsed
	}

	pct := int64(0)
	if total > 0 {
		pct = downloaded * 100 / total
	}

	line := fmt.Sprintf("\rnya get: %s / %s (%d%%)",
		nya.HumanSize(int(downloaded)),
		nya.HumanSize(int(total)),
		pct,
	)
	if speed >= 1024 {
		line += fmt.Sprintf("  @ %s", humanSpeed(speed))
		if speed > 0 && downloaded < total {
			eta := float64(total-downloaded) / speed
			line += fmt.Sprintf("  ETA %s", humanETA(eta))
		}
	} else if downloaded > 0 {
		line += "  @ …"
	}
	line += fmt.Sprintf("  [%d/%d blocks]", blocksDone, blocksTotal)
	getStatusPrint(line)
}

func humanSpeed(bps float64) string {
	switch {
	case bps >= 1024*1024:
		return fmt.Sprintf("%.2f MB/s", bps/1024/1024)
	case bps >= 1024:
		return fmt.Sprintf("%.1f KB/s", bps/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

func humanETA(seconds float64) string {
	if seconds <= 0 || math.IsInf(seconds, 0) || math.IsNaN(seconds) {
		return "…"
	}
	switch {
	case seconds < 60:
		return fmt.Sprintf("%.0fs", seconds)
	case seconds < 3600:
		m := int(seconds) / 60
		s := int(seconds) % 60
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		h := int(seconds) / 3600
		m := (int(seconds) % 3600) / 60
		return fmt.Sprintf("%dh%02dm", h, m)
	}
}
