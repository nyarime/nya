package nya

import (
	"fmt"
	"os"
	"path/filepath"
)

// SendPackProfile picks compression settings for nya send when the user did not
// override -level. Defaults favor *pack time* (Zstd level 3, no solid); use
// -level 9 for smallest archives (7z-style, much slower on multi-GB logs).
type SendPackProfile struct {
	Level  int
	Solid  bool
	Reason string
}

// sendFastLevel is the default auto level: Zstd-9, fast pack + decompress.
const sendFastLevel = LevelFast

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
		return SendPackProfile{Level: sendFastLevel, Reason: "empty"}, nil
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
			Level:  sendFastLevel,
			Reason: "text-like → Zstd fast (time-first; -level 9 for smallest)",
		}
	case PayloadDense:
		return SendPackProfile{
			Level:  LevelStore,
			Reason: "already compressed → store",
		}
	case PayloadBinary:
		return SendPackProfile{
			Level:  sendFastLevel,
			Reason: "binary → Zstd fast + BCJ when helpful",
		}
	default:
		return SendPackProfile{
			Level:  sendFastLevel,
			Reason: "unknown → Zstd fast",
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
			Level:  sendFastLevel,
			Reason: "text-heavy → Zstd fast (time-first; -level 9 for smallest)",
		}
	}
	if other >= textLike && other >= dense && other > 0 {
		return SendPackProfile{
			Level:  sendFastLevel,
			Reason: "mostly binary → Zstd fast + BCJ",
		}
	}
	return SendPackProfile{
		Level:  sendFastLevel,
		Reason: "mixed → Zstd fast",
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

// SendInputBytes returns total regular-file bytes under src (file or directory tree).
func SendInputBytes(src string) (int64, error) {
	fi, err := os.Lstat(src)
	if err != nil {
		return 0, err
	}
	if !fi.IsDir() {
		if fi.Mode().IsRegular() {
			return fi.Size(), nil
		}
		return 0, nil
	}
	var total int64
	err = filepath.Walk(src, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
