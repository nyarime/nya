package nya

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestDetectExecFormat(t *testing.T) {
	pe := buildSyntheticPE(0x8664, 0x400, 64)
	machoFat := make([]byte, 16)
	binary.BigEndian.PutUint32(machoFat[0:], 0xCAFEBABE)
	binary.BigEndian.PutUint32(machoFat[8:], 0x1000)

	cases := []struct {
		name string
		data []byte
		want ExecFormat
	}{
		{"elf", []byte{0x7f, 'E', 'L', 'F', 2}, ExecELF},
		{"pe", pe, ExecPE},
		{"macho64", le32(0xFEEDFACF), ExecMachO},
		{"macho_fat", machoFat, ExecMachO},
		{"text", []byte("hello"), ExecUnknown},
	}
	for _, tc := range cases {
		if got := DetectExecFormat(tc.data); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestDetectBCJArchPEAndMachO(t *testing.T) {
	pe := buildSyntheticPE(0x8664, 0x400, 256)
	if arch := DetectBCJArch(pe); arch != "x86" {
		t.Fatalf("PE amd64: got %q", arch)
	}
	macho := buildSyntheticMachO64(machoCpuArm64, 0x1000, 256)
	if arch := DetectBCJArch(macho); arch != "arm64" {
		t.Fatalf("Mach-O arm64: got %q", arch)
	}
}

func TestCodeSectionRangesPE(t *testing.T) {
	pe := buildSyntheticPE(0x8664, 0x400, 256)
	ranges, ok := CodeSectionRanges(pe)
	if !ok || len(ranges) != 1 {
		t.Fatalf("expected one code range, got ok=%v ranges=%v", ok, ranges)
	}
	if ranges[0].Offset != 0x400 || ranges[0].Size != 256 {
		t.Fatalf("range: %+v", ranges[0])
	}
}

func TestCodeSectionRangesMachO64(t *testing.T) {
	macho := buildSyntheticMachO64(machoCpuX8664, 0x1000, 128)
	ranges, ok := CodeSectionRanges(macho)
	if !ok || len(ranges) != 1 {
		t.Fatalf("expected one code range, got ok=%v ranges=%v", ok, ranges)
	}
	if ranges[0].Offset != 0x1000 || ranges[0].Size != 128 {
		t.Fatalf("range: %+v", ranges[0])
	}
}

func TestApplyBCJSectionAwareRoundtrip(t *testing.T) {
	pe := buildSyntheticPE(0x8664, 0x400, 64)
	pe[0x400] = 0xE8
	binary.LittleEndian.PutUint32(pe[0x401:0x405], 0x1000)
	orig := append([]byte(nil), pe...)

	work := append([]byte(nil), pe...)
	ApplyBCJFilterArchSmart(work, "x86", true)
	if bytes.Equal(work, orig) {
		t.Fatal("BCJ encode should modify code section")
	}
	if !bytes.Equal(work[:0x400], orig[:0x400]) {
		t.Fatal("header should be untouched")
	}
	ApplyBCJFilterArchSmart(work, "x86", false)
	if !bytes.Equal(work, orig) {
		t.Fatalf("BCJ decode should restore original")
	}
}

func le32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// buildSyntheticPE builds a minimal PE with one executable section at rawOff.
func buildSyntheticPE(machine uint16, rawOff, rawSize int) []byte {
	const peOff = 0x80
	const sectTable = peOff + 4 + 20 + 0xF0 // coff + minimal PE32+ optional
	total := sectTable + 40
	if rawOff+rawSize > total {
		total = rawOff + rawSize
	}
	b := make([]byte, total)
	copy(b, []byte{'M', 'Z'})
	binary.LittleEndian.PutUint32(b[0x3C:0x40], uint32(peOff))
	copy(b[peOff:], []byte{'P', 'E', 0, 0})
	binary.LittleEndian.PutUint16(b[peOff+4:], machine)
	binary.LittleEndian.PutUint16(b[peOff+6:], 1) // one section
	binary.LittleEndian.PutUint16(b[peOff+20:], 0xF0)
	// Optional PE32+ magic at peOff+24
	binary.LittleEndian.PutUint16(b[peOff+24:], 0x20B)

	sh := sectTable
	copy(b[sh:], ".text\x00\x00\x00")
	binary.LittleEndian.PutUint32(b[sh+16:], uint32(rawSize))
	binary.LittleEndian.PutUint32(b[sh+20:], uint32(rawOff))
	binary.LittleEndian.PutUint32(b[sh+36:], peScnMemExecute)
	for i := 0; i < rawSize && rawOff+i < len(b); i++ {
		b[rawOff+i] = byte(i)
	}
	return b
}

// buildSyntheticMachO64 builds thin 64-bit Mach-O with one __TEXT __text section.
func buildSyntheticMachO64(cpu uint32, fileOff, size int) []byte {
	const hdrSize = 32
	const segCmdSize = 72 + 80 // segment_command_64 + one section_64
	total := fileOff + size
	if total < hdrSize+segCmdSize {
		total = hdrSize + segCmdSize
	}
	if fileOff+size > total {
		total = fileOff + size
	}
	b := make([]byte, total)
	binary.LittleEndian.PutUint32(b[0:], 0xFEEDFACF)
	binary.LittleEndian.PutUint32(b[4:], cpu)
	binary.LittleEndian.PutUint32(b[16:], 1) // one load cmd
	binary.LittleEndian.PutUint32(b[20:], uint32(segCmdSize))

	cmd := hdrSize
	binary.LittleEndian.PutUint32(b[cmd:], machoLcSegment64)
	binary.LittleEndian.PutUint32(b[cmd+4:], uint32(segCmdSize))
	copy(b[cmd+8:], "__TEXT\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint32(b[cmd+64:], 1) // nsects

	sect := cmd + 72
	copy(b[sect:], "__text\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	copy(b[sect+16:], "__TEXT\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint64(b[sect+24:], uint64(size))
	binary.LittleEndian.PutUint32(b[sect+32:], uint32(fileOff))
	binary.LittleEndian.PutUint32(b[sect+48:], machoSAttrPureInstr)

	for i := 0; i < size && fileOff+i < len(b); i++ {
		b[fileOff+i] = byte(0xCC)
	}
	return b
}
