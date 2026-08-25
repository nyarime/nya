package nya

import (
	"encoding/binary"
	"errors"
)

// ExecFormat identifies a supported native executable container.
type ExecFormat uint8

const (
	ExecUnknown ExecFormat = 0
	ExecELF     ExecFormat = 1
	ExecPE      ExecFormat = 2
	ExecMachO   ExecFormat = 3
)

// CodeRange is a file-offset span that may contain executable instructions.
type CodeRange struct {
	Offset int
	Size   int
}

// DetectExecFormat sniffs ELF, PE (MZ), or Mach-O (incl. universal fat).
func DetectExecFormat(data []byte) ExecFormat {
	if isELF(data) {
		return ExecELF
	}
	if isPE(data) {
		return ExecPE
	}
	if isMachO(data) {
		return ExecMachO
	}
	return ExecUnknown
}

func isELF(data []byte) bool {
	return len(data) >= 4 && data[0] == 0x7f && data[1] == 'E' && data[2] == 'L' && data[3] == 'F'
}

func isPE(data []byte) bool {
	return len(data) >= 0x40 && data[0] == 'M' && data[1] == 'Z'
}

func isMachO(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	m := binary.LittleEndian.Uint32(data[:4])
	switch m {
	case 0xFEEDFACE, 0xFEEDFACF, 0xCEFAEDFE, 0xCFFAEDFE:
		return true
	case 0xCAFEBABE, 0xBEBAFECA:
		return len(data) >= 12
	default:
		return false
	}
}

// CodeSectionRanges returns executable code spans for ELF / PE / Mach-O.
// ok is false when the blob is not a supported executable or has no code sections.
func CodeSectionRanges(data []byte) ([]CodeRange, bool) {
	switch DetectExecFormat(data) {
	case ExecELF:
		ranges, err := elfCodeRanges(data)
		if err != nil || len(ranges) == 0 {
			return nil, false
		}
		return clampRanges(data, ranges), true
	case ExecPE:
		ranges, err := peCodeRanges(data)
		if err != nil || len(ranges) == 0 {
			return nil, false
		}
		return clampRanges(data, ranges), true
	case ExecMachO:
		ranges, err := machOCodeRanges(data)
		if err != nil || len(ranges) == 0 {
			return nil, false
		}
		return clampRanges(data, ranges), true
	default:
		return nil, false
	}
}

func clampRanges(data []byte, ranges []CodeRange) []CodeRange {
	out := make([]CodeRange, 0, len(ranges))
	for _, r := range ranges {
		if r.Offset < 0 || r.Size <= 0 || r.Offset >= len(data) {
			continue
		}
		end := r.Offset + r.Size
		if end > len(data) {
			end = len(data)
		}
		if end <= r.Offset {
			continue
		}
		out = append(out, CodeRange{Offset: r.Offset, Size: end - r.Offset})
	}
	return out
}

// ApplyBCJFilterArchSmart applies BCJ on whole-file or section-aware spans for
// known executables. Falls back to whole-file BCJ for opaque binaries.
func ApplyBCJFilterArchSmart(data []byte, arch string, encode bool) []byte {
	if ranges, ok := CodeSectionRanges(data); ok {
		ApplyBCJFilterArchRanges(data, arch, encode, ranges)
		return data
	}
	return ApplyBCJFilterArch(data, arch, encode)
}

// ApplyBCJFilterArchRanges applies BCJ only within file-offset spans (PC-relative
// math uses absolute file offsets).
func ApplyBCJFilterArchRanges(data []byte, arch string, encode bool, ranges []CodeRange) {
	for _, r := range ranges {
		if r.Offset < 0 || r.Size <= 0 || r.Offset >= len(data) {
			continue
		}
		end := r.Offset + r.Size
		if end > len(data) {
			end = len(data)
		}
		ApplyBCJFilterArchAt(data[r.Offset:end], arch, encode, r.Offset)
	}
}

// --- ELF ---

const elfShfExecInstr = 0x4

func elfCodeRanges(data []byte) ([]CodeRange, error) {
	if len(data) < 64 {
		return nil, errExecTruncated
	}
	class := data[4]
	isLE := data[5] == 1
	var shoff uint64
	var shentsize uint16
	var shnum uint16
	switch class {
	case 1: // ELF32
		if len(data) < 52 {
			return nil, errExecTruncated
		}
		shoff = uint64(readU32(data, 32, isLE))
		shentsize = readU16(data, 46, isLE)
		shnum = readU16(data, 48, isLE)
	case 2: // ELF64
		if len(data) < 64 {
			return nil, errExecTruncated
		}
		shoff = readU64(data, 40, isLE)
		shentsize = readU16(data, 58, isLE)
		shnum = readU16(data, 60, isLE)
	default:
		return nil, errExecBadFormat
	}
	if shentsize < 40 || shnum == 0 || shoff == 0 {
		return nil, errExecBadFormat
	}
	var ranges []CodeRange
	for i := uint16(0); i < shnum; i++ {
		off := shoff + uint64(i)*uint64(shentsize)
		if off+uint64(shentsize) > uint64(len(data)) {
			break
		}
		var flags uint64
		var secOff, secSize uint64
		switch class {
		case 1:
			flags = uint64(readU32(data, int(off)+8, isLE))
			secOff = uint64(readU32(data, int(off)+16, isLE))
			secSize = uint64(readU32(data, int(off)+20, isLE))
		case 2:
			flags = readU64(data, int(off)+8, isLE)
			secOff = readU64(data, int(off)+24, isLE)
			secSize = readU64(data, int(off)+32, isLE)
		}
		if flags&elfShfExecInstr == 0 || secSize == 0 {
			continue
		}
		ranges = append(ranges, CodeRange{Offset: int(secOff), Size: int(secSize)})
	}
	return ranges, nil
}

// --- PE ---

const peScnMemExecute = 0x20000000

func peCodeRanges(data []byte) ([]CodeRange, error) {
	if len(data) < 0x40 {
		return nil, errExecTruncated
	}
	peOff := int(binary.LittleEndian.Uint32(data[0x3C:0x40]))
	if peOff < 0 || peOff+24 > len(data) {
		return nil, errExecBadFormat
	}
	if data[peOff] != 'P' || data[peOff+1] != 'E' || data[peOff+2] != 0 || data[peOff+3] != 0 {
		return nil, errExecBadFormat
	}
	coff := peOff + 4
	nsects := int(binary.LittleEndian.Uint16(data[coff+2 : coff+4]))
	optSize := int(binary.LittleEndian.Uint16(data[coff+16 : coff+18]))
	sectTable := coff + 20 + optSize
	if nsects == 0 || sectTable < 0 || sectTable+40*nsects > len(data) {
		return nil, errExecBadFormat
	}
	var ranges []CodeRange
	for i := 0; i < nsects; i++ {
		sh := sectTable + i*40
		rawSize := int(binary.LittleEndian.Uint32(data[sh+16 : sh+20]))
		rawOff := int(binary.LittleEndian.Uint32(data[sh+20 : sh+24]))
		chars := binary.LittleEndian.Uint32(data[sh+36 : sh+40])
		if chars&peScnMemExecute == 0 || rawSize == 0 || rawOff == 0 {
			continue
		}
		ranges = append(ranges, CodeRange{Offset: rawOff, Size: rawSize})
	}
	return ranges, nil
}

// --- Mach-O ---

const (
	machoCpuI386   = 7
	machoCpuX8664  = 0x01000007
	machoCpuArm    = 12
	machoCpuArm64  = 0x0100000c

	machoLcSegment    = 0x1
	machoLcSegment64  = 0x19

	machoSAttrPureInstr = 0x80000000
	machoSAttrSomeInstr = 0x40000000
)

func machOCodeRanges(data []byte) ([]CodeRange, error) {
	imageOff, slice, err := machOImage(data)
	if err != nil {
		return nil, err
	}
	if len(slice) < 8 {
		return nil, errExecTruncated
	}
	magic := binary.LittleEndian.Uint32(slice[:4])
	var is64 bool
	switch magic {
	case 0xFEEDFACF, 0xCFFAEDFE:
		is64 = true
	case 0xFEEDFACE, 0xCEFAEDFE:
		is64 = false
	default:
		return nil, errExecBadFormat
	}
	hdrSize := 28
	if is64 {
		hdrSize = 32
	}
	if len(slice) < hdrSize {
		return nil, errExecTruncated
	}
	ncmds := binary.LittleEndian.Uint32(slice[16:20])
	cmdOff := hdrSize
	var ranges []CodeRange
	for c := uint32(0); c < ncmds && cmdOff+8 <= len(slice); c++ {
		cmd := binary.LittleEndian.Uint32(slice[cmdOff : cmdOff+4])
		cmdsize := int(binary.LittleEndian.Uint32(slice[cmdOff+4 : cmdOff+8]))
		if cmdsize < 8 || cmdOff+cmdsize > len(slice) {
			break
		}
		switch cmd {
		case machoLcSegment64:
			ranges = append(ranges, machOParseSegment64(slice[cmdOff:cmdOff+cmdsize], imageOff)...)
		case machoLcSegment:
			if !is64 {
				ranges = append(ranges, machOParseSegment32(slice[cmdOff:cmdOff+cmdsize], imageOff)...)
			}
		}
		cmdOff += cmdsize
	}
	return ranges, nil
}

func machOImage(data []byte) (imageOff int, slice []byte, err error) {
	if len(data) < 4 {
		return 0, nil, errExecTruncated
	}
	m := binary.LittleEndian.Uint32(data[:4])
	if m == 0xCAFEBABE || m == 0xBEBAFECA {
		if len(data) < 12 {
			return 0, nil, errExecTruncated
		}
		imageOff = int(binary.BigEndian.Uint32(data[8:12]))
		if imageOff < 0 || imageOff >= len(data) {
			return 0, nil, errExecBadFormat
		}
		return imageOff, data[imageOff:], nil
	}
	return 0, data, nil
}

func machOSlice(data []byte) ([]byte, error) {
	_, slice, err := machOImage(data)
	return slice, err
}

func machOParseSegment64(seg []byte, imageOff int) []CodeRange {
	if len(seg) < 72 {
		return nil
	}
	nsects := int(binary.LittleEndian.Uint32(seg[64:68]))
	sectOff := 72
	var ranges []CodeRange
	for i := 0; i < nsects; i++ {
		sh := sectOff + i*80
		if sh+80 > len(seg) {
			break
		}
		flags := binary.LittleEndian.Uint32(seg[sh+48 : sh+52])
		off := int(binary.LittleEndian.Uint32(seg[sh+32 : sh+36]))
		size := int(binary.LittleEndian.Uint64(seg[sh+24 : sh+32]))
		if !machOIsCode(flags) || size == 0 || off == 0 {
			continue
		}
		ranges = append(ranges, CodeRange{Offset: imageOff + off, Size: size})
	}
	return ranges
}

func machOParseSegment32(seg []byte, imageOff int) []CodeRange {
	if len(seg) < 56 {
		return nil
	}
	nsects := int(binary.LittleEndian.Uint32(seg[48:52]))
	sectOff := 56
	var ranges []CodeRange
	for i := 0; i < nsects; i++ {
		sh := sectOff + i*68
		if sh+68 > len(seg) {
			break
		}
		flags := binary.LittleEndian.Uint32(seg[sh+40 : sh+44])
		off := int(binary.LittleEndian.Uint32(seg[sh+24 : sh+28]))
		size := int(binary.LittleEndian.Uint32(seg[sh+20 : sh+24]))
		if !machOIsCode(flags) || size == 0 || off == 0 {
			continue
		}
		ranges = append(ranges, CodeRange{Offset: imageOff + off, Size: size})
	}
	return ranges
}

func machOIsCode(flags uint32) bool {
	return flags&machoSAttrPureInstr != 0 || flags&machoSAttrSomeInstr != 0
}

// --- helpers ---

var (
	errExecTruncated = errors.New("exec: truncated")
	errExecBadFormat = errors.New("exec: bad format")
)

func readU16(b []byte, off int, le bool) uint16 {
	if le {
		return binary.LittleEndian.Uint16(b[off : off+2])
	}
	return binary.BigEndian.Uint16(b[off : off+2])
}

func readU32(b []byte, off int, le bool) uint32 {
	if le {
		return binary.LittleEndian.Uint32(b[off : off+4])
	}
	return binary.BigEndian.Uint32(b[off : off+4])
}

func readU64(b []byte, off int, le bool) uint64 {
	if le {
		return binary.LittleEndian.Uint64(b[off : off+8])
	}
	return binary.BigEndian.Uint64(b[off : off+8])
}
