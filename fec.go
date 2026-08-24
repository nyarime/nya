package nya

// raptorqFEC generates RaptorQ repair symbols and per-symbol hashes.
func raptorqFEC(data []byte, percent int) []byte {
	fec, _, _ := encodeFEC(data, percent, FECRaptorQ, false)
	return fec
}

func blake3Short(data []byte) uint32 {
	h := Blake3Sum256(data)
	return uint32(h[0]) | uint32(h[1])<<8 | uint32(h[2])<<16 | uint32(h[3])<<24
}

func GetHashTable() []uint32 {
	ht := globalHashTable
	globalHashTable = nil
	return ht
}

var globalHashTable []uint32

// encodeFEC compresses payload redundancy using the requested codec.
func encodeFEC(data []byte, percent int, fecType uint8, solid bool) (fec []byte, hashes []uint32, plan fecPlan) {
	if len(data) < fecMinPayload || percent <= 0 {
		return nil, nil, fecPlan{K: 32, SymbolSize: fecSymbolDefault}
	}
	plan = planFEC(len(data), percent, fecType, solid)
	fec, hashes = encodeWithPlan(data, plan)
	globalHashTable = hashes
	return fec, hashes, plan
}

// repairFEC restores a damaged payload using stored parity and symbol hashes.
func repairFEC(data, fecData []byte, params FECParams, fecType uint8, hashes []uint32) ([]byte, error) {
	plan := planFromParams(params, fecType)
	return repairWithPlan(data, fecData, plan, hashes)
}

// FEC codec types for GenerateFEC.
const (
	FECTypeRaptorQ = 0
	FECTypeLDPC    = 1
)

func ldpcFEC(data []byte, percent int) []byte {
	fec, _, _ := encodeFEC(data, percent, FECLDPC, false)
	return fec
}

func GenerateFEC(data []byte, percent int, fecType int) []byte {
	switch fecType {
	case FECTypeLDPC:
		return ldpcFEC(data, percent)
	default:
		return raptorqFEC(data, percent)
	}
}

// DefaultFECType is what new archives use when -fec is set.
const DefaultFECType = FECHybrid
