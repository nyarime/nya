package nya

import "encoding/binary"

// HasEmbeddedSignature reports whether data carries an embedded authenticator
// that must survive bit-identical roundtrip (Authenticode on PE, code signature
// on Mach-O). When true, NYA skips BCJ and any other pre-compression transform.
func HasEmbeddedSignature(data []byte) bool {
	switch DetectExecFormat(data) {
	case ExecPE:
		return peHasAuthenticode(data)
	case ExecMachO:
		return machOHasCodeSignature(data)
	default:
		return false
	}
}

// peHasAuthenticode checks IMAGE_DIRECTORY_ENTRY_SECURITY (index 4).
func peHasAuthenticode(data []byte) bool {
	if len(data) < 0x40 {
		return false
	}
	peOff := int(binary.LittleEndian.Uint32(data[0x3C:0x40]))
	if peOff < 0 || peOff+24 > len(data) {
		return false
	}
	if data[peOff] != 'P' || data[peOff+1] != 'E' {
		return false
	}
	opt := peOff + 4 + 20 // COFF header is 20 bytes
	if opt+2 > len(data) {
		return false
	}
	magic := binary.LittleEndian.Uint16(data[opt : opt+2])
	var secDir int
	switch magic {
	case 0x10b: // PE32
		secDir = opt + 128 // DataDirectory[4]
	case 0x20b: // PE32+
		secDir = opt + 144
	default:
		return false
	}
	if secDir+8 > len(data) {
		return false
	}
	secOff := binary.LittleEndian.Uint32(data[secDir : secDir+4])
	secSize := binary.LittleEndian.Uint32(data[secDir+4 : secDir+8])
	if secSize == 0 || secOff == 0 {
		return false
	}
	end := int(secOff) + int(secSize)
	return secOff < uint32(len(data)) && end > int(secOff) && end <= len(data)
}

const machoLcCodeSignature = 0x1d

func machOHasCodeSignature(data []byte) bool {
	_, slice, err := machOImage(data)
	if err != nil || len(slice) < 8 {
		return false
	}
	magic := binary.LittleEndian.Uint32(slice[:4])
	var is64 bool
	switch magic {
	case 0xFEEDFACF, 0xCFFAEDFE:
		is64 = true
	case 0xFEEDFACE, 0xCEFAEDFE:
		is64 = false
	default:
		return false
	}
	hdrSize := 28
	if is64 {
		hdrSize = 32
	}
	if len(slice) < hdrSize {
		return false
	}
	ncmds := binary.LittleEndian.Uint32(slice[16:20])
	cmdOff := hdrSize
	for c := uint32(0); c < ncmds && cmdOff+8 <= len(slice); c++ {
		cmd := binary.LittleEndian.Uint32(slice[cmdOff : cmdOff+4])
		cmdsize := int(binary.LittleEndian.Uint32(slice[cmdOff+4 : cmdOff+8]))
		if cmdsize < 8 || cmdOff+cmdsize > len(slice) {
			break
		}
		if cmd == machoLcCodeSignature && cmdOff+16 <= len(slice) {
			datasize := binary.LittleEndian.Uint32(slice[cmdOff+12 : cmdOff+16])
			if datasize > 0 {
				return true
			}
		}
		cmdOff += cmdsize
	}
	return false
}
