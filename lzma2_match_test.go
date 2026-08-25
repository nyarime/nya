package nya

import "testing"

func TestLzmaMatchLenThreshold(t *testing.T) {
	src := bytesRepeat('x', 64)
	if n := lzmaMatchLen(src, 0, 1, 16); n != 16 {
		t.Fatalf("short: got %d want 16", n)
	}
	dup := bytesRepeat('a', 200)
	if n := lzmaMatchLen(dup, 0, 1, 200); n != 199 {
		t.Fatalf("long: got %d want 199", n)
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
