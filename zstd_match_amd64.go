//go:build amd64

package nya

//go:noescape
func zstdMatchLen(src []byte, pos, matchPos int) int
