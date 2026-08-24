//go:build linux

package nya

import (
	"golang.org/x/sys/unix"
)

// listXattr reads all extended attributes from a file path.
// Returns nil map on any error (non-fatal).
func listXattr(path string) map[string][]byte {
	sz, err := unix.Llistxattr(path, nil)
	if err != nil || sz <= 0 {
		return nil
	}
	buf := make([]byte, sz)
	sz, err = unix.Llistxattr(path, buf)
	if err != nil || sz <= 0 {
		return nil
	}
	buf = buf[:sz]

	result := make(map[string][]byte)
	for len(buf) > 0 {
		i := 0
		for i < len(buf) && buf[i] != 0 {
			i++
		}
		name := string(buf[:i])
		if i < len(buf) {
			i++ // skip null
		}
		buf = buf[i:]

		val := make([]byte, 256)
		vlen, err := unix.Lgetxattr(path, name, val)
		if err != nil {
			continue
		}
		v := make([]byte, vlen)
		copy(v, val[:vlen])
		result[name] = v
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// setXattrs restores extended attributes on a path.
func setXattrs(path string, xattrs map[string][]byte) {
	for name, val := range xattrs {
		unix.Lsetxattr(path, name, val, 0)
	}
}

// mknod creates a device node.
func mknod(path string, mode uint32, major, minor uint32) error {
	dev := unix.Mkdev(major, minor)
	return unix.Mknod(path, mode, int(dev))
}
