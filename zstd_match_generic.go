//go:build !amd64 && !arm64

package nya

func zstdMatchLen(src []byte, pos, matchPos int) int {
	maxL := len(src) - pos
	if r := len(src) - matchPos; r < maxL {
		maxL = r
	}
	l := 0
	for l < maxL && src[pos+l] == src[matchPos+l] {
		l++
	}
	return l
}
