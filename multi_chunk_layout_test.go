package nya

import "testing"

func TestMultiChunkFECLayoutSum(t *testing.T) {
	payload := make([]byte, 6*1024*1024)
	for i := range payload {
		payload[i] = byte(i)
	}
	archive := createMultiChunkArchive(t, payload, 15)
	r, err := Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	refs := r.buildFileChunkRefs()
	sumFEC := 0
	sumHash := 0
	for _, ref := range refs {
		sumFEC += ref.fecLen
		sumHash += ref.hashLen
	}
	if int(r.FecLen) != sumFEC {
		t.Errorf("fec layout: sum ref.fecLen=%d actual FecLen=%d", sumFEC, r.FecLen)
	}
	if len(r.allHashWords()) != sumHash {
		t.Errorf("hash layout: sum ref.hashLen=%d actual hashes=%d", sumHash, len(r.allHashWords()))
	}
}
