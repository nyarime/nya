package nya

// PackProgress reports source bytes consumed while building an archive.
// phase is "read", "compress", or "finalize"; item is a relative path or label.
type PackProgress func(done, total int64, phase, item string)

// SetPackProgress registers a callback and the total uncompressed input size.
func (nw *Writer) SetPackProgress(total int64, fn PackProgress) {
	nw.packProgressTotal = total
	nw.packProgress = fn
}

func (nw *Writer) notePackProgress(delta int64, phase, item string) {
	if nw.packProgress == nil {
		return
	}
	nw.packProgressMu.Lock()
	if delta > 0 {
		nw.packProgressDone += delta
	}
	done := nw.packProgressDone
	total := nw.packProgressTotal
	fn := nw.packProgress
	nw.packProgressMu.Unlock()
	fn(done, total, phase, item)
}
