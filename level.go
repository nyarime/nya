package nya

// Compression levels, in the style of 7-Zip's -mx and WinRAR's compression
// menu. A level picks a codec and how hard it works; everything else about
// the archive is unaffected.
const (
	LevelStore   = 0 // no compression
	LevelFastest = 1 // Zstandard, minimum effort
	LevelFast    = 3 // Zstandard
	LevelNormal  = 5 // LZMA2, small window
	LevelGood    = 7 // LZMA2, larger window and deeper search
	LevelBest    = 9 // LZMA2, maximum window and search

	// LevelDefault is still Normal (LZMA2). Many distribute/get scenes are a
	// better fit for levels 1–4 (NYA-Zstd, fast decompress); flipping the
	// default waits on public corpus decode benches — see ROADMAP.md.
	LevelDefault = LevelNormal
)

// levelSpec is what a level resolves to.
type levelSpec struct {
	codec     string
	zstdLevel int
	lzma      Lzma2Options
}

// levelTable is indexed by level, 0 through 9. Levels between the named ones
// interpolate in effort rather than doing anything surprising.
var levelTable = [10]levelSpec{
	0: {codec: CompressionStore},
	1: {codec: CompressionZstd, zstdLevel: 1},
	2: {codec: CompressionZstd, zstdLevel: 5},
	3: {codec: CompressionZstd, zstdLevel: 9},
	4: {codec: CompressionZstd, zstdLevel: 19},
	5: {codec: CompressionLZMA2, lzma: Lzma2Options{DictSize: 4 << 20, Depth: 16, NiceLen: 32}},
	6: {codec: CompressionLZMA2, lzma: Lzma2Options{DictSize: 8 << 20, Depth: 32, NiceLen: 48}},
	7: {codec: CompressionLZMA2, lzma: Lzma2Options{DictSize: 16 << 20, Depth: 64, NiceLen: 64}},
	8: {codec: CompressionLZMA2, lzma: Lzma2Options{DictSize: 32 << 20, Depth: 128, NiceLen: 96, OptimalParse: true}},
	9: {codec: CompressionLZMA2, lzma: Lzma2Options{DictSize: 64 << 20, Depth: 256, NiceLen: 128, OptimalParse: true}},
}

// LevelName gives the human label for a level, matching the wording archivers
// conventionally use.
func LevelName(level int) string {
	switch {
	case level <= 0:
		return "store"
	case level <= 2:
		return "fastest"
	case level <= 4:
		return "fast"
	case level <= 6:
		return "normal"
	case level <= 8:
		return "good"
	default:
		return "best"
	}
}

func specForLevel(level int) levelSpec {
	if level < 0 {
		level = 0
	}
	if level > 9 {
		level = 9
	}
	return levelTable[level]
}
