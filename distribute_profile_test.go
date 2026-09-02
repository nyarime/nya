package nya

import "testing"

func TestDistributeCreateProfile(t *testing.T) {
	p := DistributeCreateProfile()
	if p.Level != LevelDistribute {
		t.Fatalf("level=%d want %d", p.Level, LevelDistribute)
	}
	if p.Solid || !p.MultiChunk || !p.EmbedIndex {
		t.Fatalf("profile=%+v", p)
	}
}
