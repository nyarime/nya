package nya

import (
	"bytes"
	"fmt"

	"github.com/nyarime/gofec/v2/leopard"
)

const leopardMinPayload = 4 * 1024 * 1024 // 4 MiB compressed → Leopard-RS

func planLeopard(dataLen, percent int) (fecPlan, error) {
	c, shardSize, err := leopard.NewCodecForPayload(dataLen, percent, leopard.DefaultShardSize)
	if err != nil {
		return fecPlan{}, err
	}
	return fecPlan{
		Type:         FECRS,
		K:            c.DataShards(),
		SymbolSize:   shardSize,
		LDPCParity:   0,
		RQRepair:     0,
		Percent:      percent,
		DataShards:   c.DataShards(),
		ParityShards: c.ParityShards(),
	}, nil
}

func encodeLeopard(data []byte, plan fecPlan) (fec []byte, hashes []uint32, err error) {
	c, _, err := leopard.NewCodecForPayload(len(data), plan.Percent, plan.SymbolSize)
	if err != nil {
		return nil, nil, err
	}
	parity, err := c.EncodeParity(data)
	if err != nil {
		return nil, nil, err
	}
	var out bytes.Buffer
	for _, p := range parity {
		out.Write(p)
	}
	// Symbol hashes over data shards (even-sized).
	shardSize := plan.SymbolSize
	paddedLen := plan.DataShards * shardSize
	padded := make([]byte, paddedLen)
	copy(padded, data)
	for i := 0; i < plan.DataShards; i++ {
		hashes = append(hashes, blake3Short(padded[i*shardSize:(i+1)*shardSize]))
	}
	return out.Bytes(), hashes, nil
}

func repairLeopard(data, fecData []byte, plan fecPlan, hashes []uint32) ([]byte, error) {
	total := plan.DataShards + plan.ParityShards
	shardSize := plan.SymbolSize
	paddedLen := plan.DataShards * shardSize
	padded := make([]byte, paddedLen)
	copy(padded, data)

	shards := make([][]byte, total)
	present := make([]bool, total)

	for i := 0; i < plan.DataShards; i++ {
		shards[i] = append([]byte(nil), padded[i*shardSize:(i+1)*shardSize]...)
		if i < len(hashes) && hashes[i] != 0 && blake3Short(shards[i]) == hashes[i] {
			present[i] = true
		} else if i < len(hashes) && hashes[i] == 0 {
			present[i] = true
		} else {
			shards[i] = nil
			present[i] = false
		}
	}
	off := 0
	for i := 0; i < plan.ParityShards; i++ {
		if off+shardSize > len(fecData) {
			break
		}
		shards[plan.DataShards+i] = append([]byte(nil), fecData[off:off+shardSize]...)
		present[plan.DataShards+i] = true
		off += shardSize
	}

	codec, err := leopard.NewCodec(plan.DataShards, plan.ParityShards)
	if err != nil {
		return nil, err
	}
	got, err := codec.Decode(shards, present, len(data))
	if err != nil {
		return nil, fmt.Errorf("leopard: %w", err)
	}
	return got, nil
}

func (p fecPlan) leopardParityBytes() int {
	if p.Type != FECRS {
		return 0
	}
	return p.ParityShards * p.SymbolSize
}

func planFromParamsLeopard(p FECParams) fecPlan {
	plan := fecPlan{
		Type:         FECRS,
		K:            int(p.Param1),
		SymbolSize:   int(p.Param2),
		Percent:      int(p.Param3),
		DataShards:   int(p.Param1),
		ParityShards: int(p.Reserved >> 16),
	}
	if plan.ParityShards == 0 {
		plan.ParityShards = int(p.Reserved & 0xffff)
	}
	return plan
}
