package nya

import "fmt"

// SendPackProfile picks compression settings for nya send when the user did not
// override -level. Text-heavy inputs aim at 7-Zip-style ratio (LZMA2-9 + solid);
// dense archives store; executables use LZMA2-7 with per-file BCJ when helpful.
type SendPackProfile struct {
	Level  int
	Solid  bool
	Reason string
}

// SendPackProfileFor returns level/solid for packing src. When explicitLevel is
// true, Level is explicitLevelVal and solid follows the legacy multi-file rule.
func SendPackProfileFor(src string, explicitLevel bool, explicitLevelVal int) (SendPackProfile, error) {
	textLike, dense, other, err := ScanPayloadKinds(src)
	if err != nil {
		return SendPackProfile{}, err
	}
	if explicitLevel {
		solid := textLike >= 2 && textLike >= dense
		return SendPackProfile{
			Level:  clampLevel(explicitLevelVal),
			Solid:  solid,
			Reason: fmt.Sprintf("manual level %d", clampLevel(explicitLevelVal)),
		}, nil
	}

	total := textLike + dense + other
	if total == 0 {
		return SendPackProfile{Level: LevelNormal, Reason: "empty"}, nil
	}
	if total == 1 {
		return profileSingle(ClassifyFile(src)), nil
	}
	return profileMany(textLike, dense, other), nil
}

func profileSingle(kind PayloadKind) SendPackProfile {
	switch kind {
	case PayloadTextLike:
		return SendPackProfile{
			Level:  LevelBest,
			Solid:  true,
			Reason: "text-like → LZMA2-9 + solid",
		}
	case PayloadDense:
		return SendPackProfile{
			Level:  LevelStore,
			Reason: "already compressed → store",
		}
	case PayloadBinary:
		return SendPackProfile{
			Level:  LevelGood,
			Reason: "executable/binary → LZMA2-7 + BCJ",
		}
	default:
		return SendPackProfile{
			Level:  LevelNormal,
			Reason: "unknown → LZMA2-5",
		}
	}
}

func profileMany(textLike, dense, other int) SendPackProfile {
	if dense > textLike && dense >= other {
		return SendPackProfile{
			Level:  LevelStore,
			Reason: "mostly pre-compressed → store",
		}
	}
	if textLike >= dense && textLike > 0 {
		return SendPackProfile{
			Level:  LevelBest,
			Solid:  true,
			Reason: "text-heavy → LZMA2-9 + solid",
		}
	}
	if other >= textLike && other >= dense && other > 0 {
		return SendPackProfile{
			Level:  LevelGood,
			Reason: "mostly binary → LZMA2-7 + BCJ",
		}
	}
	solid := textLike >= 2 && textLike >= dense
	return SendPackProfile{
		Level:  LevelNormal,
		Solid:  solid,
		Reason: "mixed → LZMA2-5",
	}
}

func clampLevel(level int) int {
	if level < LevelStore {
		return LevelStore
	}
	if level > LevelBest {
		return LevelBest
	}
	return level
}
