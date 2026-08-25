package nya

import "encoding/binary"

// DetectBCJArch analyzes binary data and returns the best BCJ filter to use.
// Returns "x86", "arm", "arm64", "mips", or "" if no architecture detected.
// Supports ELF (Linux), PE (Windows exe), and Mach-O (macOS, incl. universal).
func DetectBCJArch(data []byte) string {
	switch DetectExecFormat(data) {
	case ExecELF:
		if arch := detectELFArchBCJ(data); arch != "" {
			return arch
		}
	case ExecPE:
		if arch := detectPEArchBCJ(data); arch != "" {
			return arch
		}
	case ExecMachO:
		if arch := detectMachOArchBCJ(data); arch != "" {
			return arch
		}
	}
	return detectByPatterns(data)
}

// detectELFArch checks for ELF header and reads e_machine.
func detectELFArchBCJ(data []byte) string {
	if len(data) < 20 {
		return ""
	}
	// ELF magic: 0x7f 'E' 'L' 'F'
	if data[0] != 0x7f || data[1] != 'E' || data[2] != 'L' || data[3] != 'F' {
		return ""
	}
	// e_machine is at offset 18 (2 bytes)
	// ELF class: data[4] == 1 (32-bit) or 2 (64-bit) — doesn't affect e_machine offset
	// ELF data encoding: data[5] == 1 (LE) or 2 (BE)
	isLE := data[5] == 1
	var machine uint16
	if isLE {
		machine = binary.LittleEndian.Uint16(data[18:20])
	} else {
		machine = binary.BigEndian.Uint16(data[18:20])
	}
	switch machine {
	case 0x03, 0x3E: // EM_386, EM_X86_64
		return "x86"
	case 0x28: // EM_ARM
		return "arm"
	case 0xB7: // EM_AARCH64
		return "arm64"
	case 0x08, 0x0A: // EM_MIPS, EM_MIPS_RS3_LE
		return "mips"
	}
	return ""
}

func detectPEArchBCJ(data []byte) string {
	if len(data) < 0x40 {
		return ""
	}
	peOff := int(binary.LittleEndian.Uint32(data[0x3C:0x40]))
	if peOff < 0 || peOff+6 > len(data) {
		return ""
	}
	if data[peOff] != 'P' || data[peOff+1] != 'E' {
		return ""
	}
	machine := binary.LittleEndian.Uint16(data[peOff+4 : peOff+6])
	switch machine {
	case 0x014C, 0x8664: // I386, AMD64
		return "x86"
	case 0x01C4: // ARM NT
		return "arm"
	case 0xAA64: // ARM64
		return "arm64"
	}
	return ""
}

func detectMachOArchBCJ(data []byte) string {
	slice, err := machOSlice(data)
	if err != nil || len(slice) < 8 {
		return ""
	}
	cpu := binary.LittleEndian.Uint32(slice[4:8])
	switch cpu {
	case machoCpuI386, machoCpuX8664:
		return "x86"
	case machoCpuArm:
		return "arm"
	case machoCpuArm64:
		return "arm64"
	}
	return ""
}

// detectByPatterns scans instruction byte patterns to guess architecture.
func detectByPatterns(data []byte) string {
	if len(data) < 1024 {
		return ""
	}
	// Sample first 64KB
	sample := data
	if len(sample) > 65536 {
		sample = sample[:65536]
	}

	// Count x86 E8/E9 instructions
	x86Count := 0
	for i := 0; i+4 < len(sample); i++ {
		if sample[i] == 0xE8 || sample[i] == 0xE9 {
			x86Count++
			i += 4
		}
	}

	// Count ARM BL (0xEB in byte[3] at 4-byte alignment)
	armCount := 0
	for i := 0; i+3 < len(sample); i += 4 {
		if sample[i+3] == 0xEB {
			armCount++
		}
	}

	// Count ARM64 BL (high byte 0x94..0x97 at 4-byte alignment, LE)
	arm64Count := 0
	for i := 0; i+3 < len(sample); i += 4 {
		op := sample[i+3] >> 2
		if op == 0x25 { // 0x94>>2 = 0x25
			arm64Count++
		}
	}

	// Count MIPS JAL (opcode 3 in bits[31:26], BE)
	mipsCount := 0
	for i := 0; i+3 < len(sample); i += 4 {
		if (sample[i] >> 2) == 0x03 {
			mipsCount++
		}
	}

	// Threshold: at least 0.5% of positions should match
	threshold := len(sample) / 200

	best := ""
	bestCount := threshold
	if x86Count > bestCount {
		best, bestCount = "x86", x86Count
	}
	if armCount > bestCount {
		best, bestCount = "arm", armCount
	}
	if arm64Count > bestCount {
		best, bestCount = "arm64", arm64Count
	}
	if mipsCount > bestCount {
		best = "mips"
	}
	return best
}
