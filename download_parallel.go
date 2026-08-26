package nya

import "math"

// TryCloudflareMaxParallel is the practical upper bound for concurrent in-flight
// HTTP Range requests through a TryCloudflare quick tunnel (~200 per tunnel).
const TryCloudflareMaxParallel = 200

const minDownloadBlock = 64 * 1024 // matches ParseBlockSize floor

// BlockSizeForParallel picks a transport block size so a file uses at most
// maxParallel blocks (e.g. 1 GiB / 200 ≈ 5 MiB per block).
func BlockSizeForParallel(bodySize int64, maxParallel int) int64 {
	if maxParallel <= 0 {
		maxParallel = TryCloudflareMaxParallel
	}
	if bodySize <= 0 {
		return DefaultDownloadBlock
	}
	bs := (bodySize + int64(maxParallel) - 1) / int64(maxParallel)
	if bs < minDownloadBlock {
		bs = minDownloadBlock
	}
	return bs
}

// DownloadConcurrency returns how many parallel block fetches to run.
// requested <= 0 means auto: one worker per transport block, capped at maxParallel.
func DownloadConcurrency(m *Manifest, requested int) int {
	if m == nil {
		if requested > 0 {
			return requested
		}
		return 8
	}
	blocks := len(m.Download.Blocks)
	if blocks == 0 {
		if requested > 0 {
			return requested
		}
		return 8
	}
	if requested <= 0 {
		return int(math.Min(float64(blocks), float64(TryCloudflareMaxParallel)))
	}
	if requested > blocks {
		return blocks
	}
	return requested
}
