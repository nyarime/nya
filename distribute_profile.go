package nya

// DistributeCreateOptions picks writer settings for CDN / nya get pipelines.
type DistributeCreateOptions struct {
	Level      int
	Solid      bool
	MultiChunk bool
	EmbedIndex bool
}

// DistributeCreateProfile returns the recommended create settings for
// distribution (fast zstd decode, embedded download index, multi-chunk).
func DistributeCreateProfile() DistributeCreateOptions {
	return DistributeCreateOptions{
		Level:      LevelDistribute,
		Solid:      false,
		MultiChunk: true,
		EmbedIndex: true,
	}
}
