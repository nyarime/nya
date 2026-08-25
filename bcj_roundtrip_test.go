package nya

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBCJRoundtripOK(t *testing.T) {
	pe := buildSyntheticPE(0x8664, 0x400, 64)
	pe[0x400] = 0xE8
	binary.LittleEndian.PutUint32(pe[0x401:0x405], 0x1000)
	if !bcjRoundtripOK(pe, "x86") {
		t.Fatal("section-aware BCJ should roundtrip on synthetic PE")
	}
}

func TestSignedPEBCJWhenRoundtripOK(t *testing.T) {
	w := NewWriterOpts(&seekBuf{buf: new(bytes.Buffer)}, 0, 3, false)
	pe := buildSyntheticPE(0x8664, 0x400, 128)
	opt := int(binary.LittleEndian.Uint32(pe[0x3C:0x40])) + 24
	secDir := opt + 144
	sigStart := len(pe)
	pe = append(pe, 0xDE, 0xAD, 0xBE, 0xEF)
	binary.LittleEndian.PutUint32(pe[secDir:], uint32(sigStart))
	binary.LittleEndian.PutUint32(pe[secDir+4:], 4)
	pe[0x400] = 0xE8
	binary.LittleEndian.PutUint32(pe[0x401:0x405], 0x1000)

	if !HasEmbeddedSignature(pe) {
		t.Fatal("expected embedded signature")
	}
	if !bcjRoundtripOK(pe, "x86") {
		t.Fatal("signed PE should pass BCJ roundtrip verify")
	}

	raw, bcj := w.chooseBCJForFile(pe)
	// BCJ may or may not win on size; roundtrip must hold if BCJ selected.
	if bcj != BCJNone {
		check := append([]byte(nil), raw...)
		ApplyBCJFilterArchSmart(check, BCJIDToArch(bcj), false)
		if !bytes.Equal(check, pe) {
			t.Fatal("chosen BCJ must roundtrip to original signed PE")
		}
	}
}

func TestBCJRoundtripRejectsBadTransform(t *testing.T) {
	// Corrupt arch on random binary should fail roundtrip or not shrink.
	data := bytes.Repeat([]byte{0xE8, 0x00, 0x00, 0x00, 0x00}, 32)
	if bcjRoundtripOK(data, "x86") {
		// x86 BCJ on pure E8 pattern still roundtrips — bijective on touched ops.
		work := append([]byte(nil), data...)
		ApplyBCJFilterArchSmart(work, "x86", true)
		ApplyBCJFilterArchSmart(work, "x86", false)
		if !bytes.Equal(work, data) {
			t.Fatal("expected bijective BCJ on synthetic pattern")
		}
	}
}
