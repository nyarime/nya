package nya

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// SetWorkers caps parallel chunk decompression during Extract (0 = automatic).
func (r *Reader) SetWorkers(n int) { r.workers = n }

func (r *Reader) workersForExtract() int {
	if r.workers > 0 {
		return r.workers
	}
	return 4
}

// extractFilePayload decompresses all on-disk chunks for one file entry.
func (r *Reader) extractFilePayload(e *DirEntry) ([]byte, error) {
	if e.ChunkCount <= 1 || r.workersForExtract() <= 1 {
		return r.extractFilePayloadSequential(e)
	}
	return r.extractFilePayloadParallel(e)
}

func (r *Reader) extractFilePayloadSequential(e *DirEntry) ([]byte, error) {
	var fullData bytes.Buffer
	off := e.FirstDataOff
	for c := uint32(0); c < e.ChunkCount; c++ {
		raw, err := r.decompressChunkAt(e, off)
		if err != nil {
			return nil, err
		}
		fullData.Write(raw)
		ch, err := ReadChunkHeader(bytes.NewReader(r.data[off:]))
		if err != nil {
			return nil, err
		}
		off += chunkDataStride(ch)
	}
	return r.finishFilePayload(e, fullData.Bytes())
}

func (r *Reader) extractFilePayloadParallel(e *DirEntry) ([]byte, error) {
	n := int(e.ChunkCount)
	offsets := make([]uint64, n)
	off := e.FirstDataOff
	for c := 0; c < n; c++ {
		if off+ChunkHeaderSize > uint64(len(r.data)) {
			return nil, fmt.Errorf("nya: truncated archive at chunk %d of %s", c, e.Path)
		}
		offsets[c] = off
		ch, err := ReadChunkHeader(bytes.NewReader(r.data[off:]))
		if err != nil {
			return nil, err
		}
		off += chunkDataStride(ch)
	}

	parts := make([][]byte, n)
	jobs := make(chan int, n)
	errs := make(chan error, 1)

	workers := r.workersForExtract()
	if workers > n {
		workers = n
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				raw, err := r.decompressChunkAt(e, offsets[idx])
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					return
				}
				parts[idx] = raw
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errs:
		return nil, err
	default:
	}

	var fullData bytes.Buffer
	for i, p := range parts {
		if p == nil {
			return nil, fmt.Errorf("nya: missing decompressed chunk %d of %s", i, e.Path)
		}
		fullData.Write(p)
	}
	return r.finishFilePayload(e, fullData.Bytes())
}

func (r *Reader) finishFilePayload(e *DirEntry, raw []byte) ([]byte, error) {
	if e.OriginalSize > 0 && uint64(len(raw)) > e.OriginalSize {
		return nil, fmt.Errorf("bomb detected: file decompressed to %d bytes, exceeds declared %d", len(raw), e.OriginalSize)
	}
	if e.BCJFilter != BCJNone {
		if arch := BCJIDToArch(e.BCJFilter); arch != "" {
			ApplyBCJFilterArchSmart(raw, arch, false)
		}
	}
	return raw, nil
}

func (r *Reader) decompressChunkAt(e *DirEntry, off uint64) ([]byte, error) {
	if off+ChunkHeaderSize > uint64(len(r.data)) {
		return nil, fmt.Errorf("nya: truncated chunk header for %s", e.Path)
	}
	chBuf := bytes.NewReader(r.data[off:])
	ch, err := ReadChunkHeader(chBuf)
	if err != nil {
		return nil, err
	}

	compData := make([]byte, ch.CompressedSize)
	if _, err := io.ReadFull(chBuf, compData); err != nil {
		return nil, err
	}

	if len(r.Password) > 0 {
		dec, err := DecryptPayload(compData, r.Password, r.Header)
		if err != nil {
			return nil, fmt.Errorf("nya: decrypt %s: %w", e.Path, err)
		}
		compData = dec
	}

	var raw []byte
	pos := 0
	for pos+4 <= len(compData) {
		blockLen := int(binary.LittleEndian.Uint32(compData[pos : pos+4]))
		pos += 4
		if pos+blockLen > len(compData) {
			break
		}
		blockData := compData[pos : pos+blockLen]
		var block []byte
		switch {
		case e.CompressionID == CompressNone:
			block = append([]byte(nil), blockData...)
		case e.CompressionID == CompressLzma2:
			block, err = decompressLzma2Block(blockData)
		default:
			var dec io.ReadCloser
			dec, err = r.zstdReaderFor(blockData, e.CompressionID)
			if err == nil {
				block, err = io.ReadAll(dec)
				dec.Close()
			}
		}
		if err != nil {
			return nil, fmt.Errorf("nya: decompress %s: %w", e.Path, err)
		}
		raw = append(raw, block...)
		pos += blockLen
	}

	if ch.OriginalSize > 0 && uint64(len(raw)) > ch.OriginalSize {
		return nil, fmt.Errorf("bomb detected: chunk decompressed to %d bytes, exceeds declared %d", len(raw), ch.OriginalSize)
	}
	if ch.OriginalSize == 0 && ch.CompressedSize > 0 && len(raw) > 0 {
		ratio := uint64(len(raw)) / uint64(ch.CompressedSize)
		if ratio > BombRatioThreshold {
			return nil, fmt.Errorf("bomb detected: chunk ratio %d:1 exceeds threshold %d:1", ratio, BombRatioThreshold)
		}
	}
	return raw, nil
}
