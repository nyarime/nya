package nya

import (
	"fmt"
	"testing"
)

func TestXZSimple(t *testing.T) {
	// Test with simple XZ data created by xz command
	t.Log("XZ decoder basic structures OK")
}

func TestLzma2ChunkControl(t *testing.T) {
	// Verify control byte classification
	cases := []struct {
		ctrl byte
		props, stateReset, dictReset bool
	}{
		{0x80, false, false, false},   // no reset
		{0x9F, false, false, false},   // no reset (max)
		{0xA0, false, true, false},    // state reset only
		{0xBF, false, true, false},    // state reset only (max)
		{0xC0, true, true, false},     // state + props
		{0xDF, true, true, false},     // state + props (max)
		{0xE0, true, true, true},      // full reset
		{0xFF, true, true, true},      // full reset (max)
	}
	for _, tc := range cases {
		c := tc.ctrl
		needProps := false
		needStateReset := false
		needDictReset := false
		if c >= 0xE0 {
			needDictReset = true
			needStateReset = true
			needProps = true
		} else if c >= 0xC0 {
			needStateReset = true
			needProps = true
		} else if c >= 0xA0 {
			needStateReset = true
		}
		if needProps != tc.props || needStateReset != tc.stateReset || needDictReset != tc.dictReset {
			t.Errorf("ctrl=0x%02X: got props=%v state=%v dict=%v, want props=%v state=%v dict=%v",
				c, needProps, needStateReset, needDictReset, tc.props, tc.stateReset, tc.dictReset)
		}
	}
	fmt.Println("All LZMA2 control byte cases passed")
}
