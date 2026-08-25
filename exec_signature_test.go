package nya

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestHasEmbeddedSignaturePE(t *testing.T) {
	unsigned := buildSyntheticPE(0x8664, 0x400, 64)
	if HasEmbeddedSignature(unsigned) {
		t.Fatal("unsigned PE should not report signature")
	}
	signed := append([]byte(nil), unsigned...)
	// Point security directory at trailing blob.
	opt := int(binary.LittleEndian.Uint32(signed[0x3C:0x40])) + 24
	secDir := opt + 144
	sigStart := len(signed)
	signed = append(signed, 0x30, 0x82, 0x01, 0x00) // fake PKCS#7 prefix
	binary.LittleEndian.PutUint32(signed[secDir:], uint32(sigStart))
	binary.LittleEndian.PutUint32(signed[secDir+4:], uint32(len(signed)-sigStart))
	if !HasEmbeddedSignature(signed) {
		t.Fatal("signed PE should report signature")
	}
}

func TestHasEmbeddedSignatureMachO(t *testing.T) {
	unsigned := buildSyntheticMachO64(machoCpuArm64, 0x1000, 64)
	if HasEmbeddedSignature(unsigned) {
		t.Fatal("unsigned Mach-O should not report signature")
	}
	signed := buildSyntheticMachO64WithCodeSig(machoCpuArm64, 0x1000, 64, 0x2000)
	if !HasEmbeddedSignature(signed) {
		t.Fatal("signed Mach-O should report signature")
	}
}

func TestChooseBCJSkipsSignedPE(t *testing.T) {
	w := NewWriterOpts(&seekBuf{buf: new(bytes.Buffer)}, 0, 3, false)
	pe := buildSyntheticPE(0x8664, 0x400, 128)
	opt := int(binary.LittleEndian.Uint32(pe[0x3C:0x40])) + 24
	secDir := opt + 144
	sigStart := len(pe)
	pe = append(pe, 0xDE, 0xAD, 0xBE, 0xEF)
	binary.LittleEndian.PutUint32(pe[secDir:], uint32(sigStart))
	binary.LittleEndian.PutUint32(pe[secDir+4:], 4)

	raw, bcj := w.chooseBCJForFile(pe)
	if bcj != BCJNone {
		t.Fatalf("signed PE must skip BCJ, got filter %d", bcj)
	}
	if !bytes.Equal(raw, pe) {
		t.Fatal("signed PE bytes must not be transformed")
	}
}

func buildSyntheticMachO64WithCodeSig(cpu uint32, fileOff, size, sigOff int) []byte {
	const hdrSize = 32
	const segCmdSize = 72 + 80
	const sigCmdSize = 16
	total := fileOff + size
	if sigOff+16 > total {
		total = sigOff + 16
	}
	b := make([]byte, total)
	binary.LittleEndian.PutUint32(b[0:], 0xFEEDFACF)
	binary.LittleEndian.PutUint32(b[4:], cpu)
	binary.LittleEndian.PutUint32(b[16:], 2) // two load commands
	binary.LittleEndian.PutUint32(b[20:], uint32(segCmdSize+sigCmdSize))

	cmd := hdrSize
	binary.LittleEndian.PutUint32(b[cmd:], machoLcSegment64)
	binary.LittleEndian.PutUint32(b[cmd+4:], uint32(segCmdSize))
	copy(b[cmd+8:], "__TEXT\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint32(b[cmd+64:], 1)
	sect := cmd + 72
	copy(b[sect:], "__text\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	copy(b[sect+16:], "__TEXT\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint64(b[sect+24:], uint64(size))
	binary.LittleEndian.PutUint32(b[sect+32:], uint32(fileOff))
	binary.LittleEndian.PutUint32(b[sect+48:], machoSAttrPureInstr)

	sigCmd := cmd + segCmdSize
	binary.LittleEndian.PutUint32(b[sigCmd:], machoLcCodeSignature)
	binary.LittleEndian.PutUint32(b[sigCmd+4:], uint32(sigCmdSize))
	binary.LittleEndian.PutUint32(b[sigCmd+8:], uint32(sigOff))
	binary.LittleEndian.PutUint32(b[sigCmd+12:], 8)

	for i := 0; i < size && fileOff+i < len(b); i++ {
		b[fileOff+i] = 0xCC
	}
	return b
}
