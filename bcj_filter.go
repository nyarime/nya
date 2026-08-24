package nya

import "encoding/binary"

// BCJFilterX86 applies x86 BCJ filter in-place.
// Converts E8 (CALL) and E9 (JMP) relative offsets to absolute (encode=true)
// or absolute back to relative (encode=false).
func BCJFilterX86(data []byte, encode bool) []byte {
	for i := 0; i+4 < len(data); {
		if data[i] == 0xE8 || data[i] == 0xE9 {
			v := int32(binary.LittleEndian.Uint32(data[i+1 : i+5]))
			if encode {
				v += int32(i) + 5
			} else {
				v -= int32(i) + 5
			}
			binary.LittleEndian.PutUint32(data[i+1:i+5], uint32(v))
			i += 5
		} else {
			i++
		}
	}
	return data
}

// BCJFilterARM applies ARM (A32) BCJ filter.
// Converts BL (Branch with Link) relative offsets to absolute.
// ARM BL encoding: cond=0xEB in byte[3], 24-bit signed offset in bytes[0:3].
func BCJFilterARM(data []byte, encode bool) []byte {
	for i := 0; i+3 < len(data); i += 4 {
		if data[i+3] == 0xEB {
			// 24-bit signed offset (little-endian ARM)
			raw := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16
			// sign-extend 24-bit to 32-bit
			offset := int32(raw)
			if raw&0x800000 != 0 {
				offset |= -1 << 24
			}
			// offset is in units of 4 bytes; PC = i + 8 in ARM
			if encode {
				offset += int32(i+8) >> 2
			} else {
				offset -= int32(i+8) >> 2
			}
			u := uint32(offset) & 0xFFFFFF
			data[i] = byte(u)
			data[i+1] = byte(u >> 8)
			data[i+2] = byte(u >> 16)
		}
	}
	return data
}

// BCJFilterARM64 applies AArch64 BCJ filter.
// Converts BL instructions: opcode bits [31:26] = 100101 → high byte 0x94..0x97.
func BCJFilterARM64(data []byte, encode bool) []byte {
	for i := 0; i+3 < len(data); i += 4 {
		inst := binary.LittleEndian.Uint32(data[i : i+4])
		// BL: bits[31:26] == 0b100101 → (inst >> 26) == 0x25
		if (inst >> 26) == 0x25 {
			imm26 := int32(inst & 0x03FFFFFF)
			// sign-extend 26-bit
			if imm26&0x02000000 != 0 {
				imm26 |= -1 << 26
			}
			if encode {
				imm26 += int32(i) >> 2
			} else {
				imm26 -= int32(i) >> 2
			}
			inst = (inst & 0xFC000000) | (uint32(imm26) & 0x03FFFFFF)
			binary.LittleEndian.PutUint32(data[i:i+4], inst)
		}
	}
	return data
}

// BCJFilterMIPS applies MIPS BCJ filter.
// Converts JAL (Jump And Link) instructions: opcode 0x0C in bits[31:26].
// MIPS is big-endian. JAL target is 26-bit word address in bits[25:0].
func BCJFilterMIPS(data []byte, encode bool) []byte {
	for i := 0; i+3 < len(data); i += 4 {
		inst := binary.BigEndian.Uint32(data[i : i+4])
		// JAL: opcode = bits[31:26] = 0b000011 = 3
		if (inst >> 26) == 0x03 {
			target := inst & 0x03FFFFFF
			if encode {
				target += uint32(i) >> 2
			} else {
				target -= uint32(i) >> 2
			}
			inst = (inst & 0xFC000000) | (target & 0x03FFFFFF)
			binary.BigEndian.PutUint32(data[i:i+4], inst)
		}
	}
	return data
}

// BCJArchToID converts arch string to BCJ filter ID.
func BCJArchToID(arch string) uint8 {
	switch arch {
	case "x86":   return BCJX86
	case "arm":   return BCJARM
	case "arm64": return BCJARM64
	case "mips":  return BCJMIPS
	default:      return BCJNone
	}
}

// BCJIDToArch converts BCJ filter ID to arch string.
func BCJIDToArch(id uint8) string {
	switch id {
	case BCJX86:   return "x86"
	case BCJARM:   return "arm"
	case BCJARM64: return "arm64"
	case BCJMIPS:  return "mips"
	default:       return ""
	}
}

// ApplyBCJFilterArch applies the named BCJ filter. arch is "x86","arm","arm64","mips".
func ApplyBCJFilterArch(data []byte, arch string, encode bool) []byte {
	switch arch {
	case "x86":
		return BCJFilterX86(data, encode)
	case "arm":
		return BCJFilterARM(data, encode)
	case "arm64":
		return BCJFilterARM64(data, encode)
	case "mips":
		return BCJFilterMIPS(data, encode)
	default:
		return data
	}
}
