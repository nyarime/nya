//go:build windows

package nya

import (
	"fmt"
	"os"
)

func mkfifo(path string, mode uint32) error {
	return fmt.Errorf("mkfifo not supported on Windows")
}

func getDevNumbers(info os.FileInfo) (major, minor uint32) {
	return 0, 0
}

func getUnixStat(info os.FileInfo) (uid, gid uint32, ok bool) {
	return 0, 0, false
}
