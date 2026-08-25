package nya

import "encoding/binary"

// BCJFilterX86 applies x86 BCJ filter in-place at file offset base.
func BCJFilterX86At(data []byte, base int, encode bool) []byte {
	for i := 0; i+4 < len(data); {
		if data[i] == 0xE8 || data[i] == 0xE9 {
			pos := base + i
			v := int32(binary.LittleEndian.Uint32(data[i+1 : i+5]))
			if encode {
				v += int32(pos) + 5
			} else {
				v -= int32(pos) + 5
			}
			binary.LittleEndian.PutUint32(data[i+1:i+5], uint32(v))
			i += 5
		} else {
			i++
		}
	}
	return data
}

// BCJFilterX86 applies x86 BCJ filter in-place.
func BCJFilterX86(data []byte, encode bool) []byte {
	return BCJFilterX86At(data, 0, encode)
}

// BCJFilterARM applies ARM (A32) BCJ filter at file offset base.
func BCJFilterARMAt(data []byte, base int, encode bool) []byte {
	for i := 0; i+3 < len(data); i += 4 {
		if data[i+3] == 0xEB {
			raw := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16
			offset := int32(raw)
			if raw&0x800000 != 0 {
				offset |= -1 << 24
			}
			pos := base + i
			if encode {
				offset += int32(pos+8) >> 2
			} else {
				offset -= int32(pos+8) >> 2
			}
			u := uint32(offset) & 0xFFFFFF
			data[i] = byte(u)
			data[i+1] = byte(u >> 8)
			data[i+2] = byte(u >> 16)
		}
	}
	return data
}

// BCJFilterARM applies ARM (A32) BCJ filter.
func BCJFilterARM(data []byte, encode bool) []byte {
	return BCJFilterARMAt(data, 0, encode)
}

// BCJFilterARM64 applies AArch64 BCJ filter at file offset base.
func BCJFilterARM64At(data []byte, base int, encode bool) []byte {
	for i := 0; i+3 < len(data); i += 4 {
		inst := binary.LittleEndian.Uint32(data[i : i+4])
		if (inst >> 26) == 0x25 {
			imm26 := int32(inst & 0x03FFFFFF)
			if imm26&0x02000000 != 0 {
				imm26 |= -1 << 26
			}
			pos := base + i
			if encode {
				imm26 += int32(pos) >> 2
			} else {
				imm26 -= int32(pos) >> 2
			}
			inst = (inst & 0xFC000000) | (uint32(imm26) & 0x03FFFFFF)
			binary.LittleEndian.PutUint32(data[i:i+4], inst)
		}
	}
	return data
}

// BCJFilterARM64 applies AArch64 BCJ filter.
func BCJFilterARM64(data []byte, encode bool) []byte {
	return BCJFilterARM64At(data, 0, encode)
}

// BCJFilterMIPS applies MIPS BCJ filter at file offset base.
func BCJFilterMIPSAt(data []byte, base int, encode bool) []byte {
	for i := 0; i+3 < len(data); i += 4 {
		inst := binary.BigEndian.Uint32(data[i : i+4])
		if (inst >> 26) == 0x03 {
			target := inst & 0x03FFFFFF
			pos := base + i
			if encode {
				target += uint32(pos) >> 2
			} else {
				target -= uint32(pos) >> 2
			}
			inst = (inst & 0xFC000000) | (target & 0x03FFFFFF)
			binary.BigEndian.PutUint32(data[i:i+4], inst)
		}
	}
	return data
}

// BCJFilterMIPS applies MIPS BCJ filter.
func BCJFilterMIPS(data []byte, encode bool) []byte {
	return BCJFilterMIPSAt(data, 0, encode)
}

// BCJArchToID converts arch string to BCJ filter ID.
func BCJArchToID(arch string) uint8 {
	switch arch {
	case "x86":
		return BCJX86
	case "arm":
		return BCJARM
	case "arm64":
		return BCJARM64
	case "mips":
		return BCJMIPS
	default:
		return BCJNone
	}
}

// BCJIDToArch converts BCJ filter ID to arch string.
func BCJIDToArch(id uint8) string {
	switch id {
	case BCJX86:
		return "x86"
	case BCJARM:
		return "arm"
	case BCJARM64:
		return "arm64"
	case BCJMIPS:
		return "mips"
	default:
		return ""
	}
}

// ApplyBCJFilterArch applies the named BCJ filter. arch is "x86","arm","arm64","mips".
func ApplyBCJFilterArch(data []byte, arch string, encode bool) []byte {
	return ApplyBCJFilterArchAt(data, arch, encode, 0)
}

// ApplyBCJFilterArchAt applies BCJ with PC math relative to file offset base.
func ApplyBCJFilterArchAt(data []byte, arch string, encode bool, base int) []byte {
	switch arch {
	case "x86":
		return BCJFilterX86At(data, base, encode)
	case "arm":
		return BCJFilterARMAt(data, base, encode)
	case "arm64":
		return BCJFilterARM64At(data, base, encode)
	case "mips":
		return BCJFilterMIPSAt(data, base, encode)
	default:
		return data
	}
}
