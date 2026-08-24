//go:build !windows

package nya

import (
	"os"
	"syscall"
)

func mkfifo(path string, mode uint32) error {
	return syscall.Mkfifo(path, mode)
}

func getDevNumbers(info os.FileInfo) (major, minor uint32) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		major = unix_major(uint64(stat.Rdev))
		minor = unix_minor(uint64(stat.Rdev))
	}
	return
}

func getUnixStat(info os.FileInfo) (uid, gid uint32, ok bool) {
	if stat, okk := info.Sys().(*syscall.Stat_t); okk {
		return stat.Uid, stat.Gid, true
	}
	return 0, 0, false
}
