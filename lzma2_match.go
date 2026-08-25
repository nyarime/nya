package nya

// lzmaMatchLen extends a match at pos vs cpos up to maxLen bytes.
// Uses SIMD only when enough bytes remain — per-probe AVX2+VZEROUPPER was
// ~100× slower than scalar on 6 MiB solid LZMA2 (TestSolidIgnoresMultiChunk).
func lzmaMatchLen(src []byte, pos, cpos, maxLen int) int {
	if maxLen <= 0 || pos < 0 || cpos < 0 {
		return 0
	}
	rem := maxLen
	if r := len(src) - pos; r < rem {
		rem = r
	}
	if r := len(src) - cpos; r < rem {
		rem = r
	}
	if rem <= 0 {
		return 0
	}
	if rem < 64 {
		ml := 0
		for ml < rem && src[pos+ml] == src[cpos+ml] {
			ml++
		}
		return ml
	}
	ml := zstdMatchLen(src, pos, cpos)
	if ml > rem {
		return rem
	}
	return ml
}
