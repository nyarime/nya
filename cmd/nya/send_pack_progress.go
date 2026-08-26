package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nyarime/nya"
)

type sendPackProgress struct {
	total      int64
	readDone   int64
	phase      string
	start      time.Time
	lastRender time.Time
	mu         sync.Mutex

	heartMu   sync.Mutex
	heartStop chan struct{}
}

func newSendPackProgress(total int64) *sendPackProgress {
	return &sendPackProgress{
		total: total,
		start: time.Now(),
		phase: "read",
	}
}

func (p *sendPackProgress) startHeartbeat() {
	p.heartMu.Lock()
	defer p.heartMu.Unlock()
	if p.heartStop != nil {
		return
	}
	stop := make(chan struct{})
	p.heartStop = stop
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.render(false)
			case <-stop:
				return
			}
		}
	}()
}

func (p *sendPackProgress) stopHeartbeat() {
	p.heartMu.Lock()
	defer p.heartMu.Unlock()
	if p.heartStop != nil {
		close(p.heartStop)
		p.heartStop = nil
	}
}

func (p *sendPackProgress) callback() nya.PackProgress {
	return func(done, total int64, phase, _ string) {
		p.mu.Lock()
		p.readDone = done
		if phase != "" {
			p.phase = phase
		}
		if total > 0 {
			p.total = total
		}
		p.mu.Unlock()
		p.render(false)
	}
}

func (p *sendPackProgress) finish(archiveSize int64, profile nya.SendPackProfile) {
	p.render(true)
	solid := ""
	if profile.Solid {
		solid = " solid"
	}
	fmt.Fprintf(os.Stderr, "\n%s: %s → %s (%s%s, %s)\n",
		T("send.pack.done"),
		nya.HumanSize(int(p.total)),
		nya.HumanSize(int(archiveSize)),
		nya.LevelName(profile.Level),
		solid,
		profile.Reason,
	)
	_ = os.Stderr.Sync()
}

func (p *sendPackProgress) render(force bool) {
	now := time.Now()
	if !force && now.Sub(p.lastRender) < 150*time.Millisecond {
		return
	}
	p.lastRender = now

	p.mu.Lock()
	readDone := p.readDone
	total := p.total
	phase := p.phase
	elapsed := now.Sub(p.start).Seconds()
	p.mu.Unlock()

	var line string
	switch phase {
	case "compress", "finalize":
		line = fmt.Sprintf("\r%s: %s %s, %s … %s",
			T("send.pack.compressing"),
			nya.HumanSize(int(total)),
			T("send.pack.read_done"),
			T("send.pack."+phase),
			humanETA(elapsed),
		)
	default:
		pct := int64(0)
		if total > 0 {
			pct = readDone * 100 / total
		}
		line = fmt.Sprintf("\r%s: %s / %s (%d%%)",
			T("send.pack.reading"),
			nya.HumanSize(int(readDone)),
			nya.HumanSize(int(total)),
			pct,
		)
		if elapsed > 0 && readDone >= 1024 {
			speed := float64(readDone) / elapsed
			line += fmt.Sprintf("  @ %s", humanSpeed(speed))
			if speed > 0 && readDone < total {
				line += fmt.Sprintf("  ETA %s", humanETA(float64(total-readDone)/speed))
			}
		}
	}
	if force && phase == "read" && readDone >= total && total > 0 {
		// Close() returned without hitting finalize callback (tiny archive).
		pct := int64(100)
		line = fmt.Sprintf("\r%s: %s / %s (%d%%)",
			T("send.pack.reading"),
			nya.HumanSize(int(readDone)),
			nya.HumanSize(int(total)),
			pct,
		)
	}
	sendStatusPrint(line)
}

func sendStatusPrint(s string) {
	fmt.Fprint(os.Stderr, s)
	_ = os.Stderr.Sync()
}

func sendPackProgressTotal(path string) int64 {
	n := inputSize(path)
	if n > 0 {
		return n
	}
	return 1
}

func ensureSendPackProgressMin(total int64) int64 {
	if total <= 0 {
		return 1
	}
	return total
}
