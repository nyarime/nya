//go:build !linux

package nya

func listXattr(_ string) map[string][]byte { return nil }
func setXattrs(_ string, _ map[string][]byte) {}
func mknod(_ string, _ uint32, _, _ uint32) error { return nil }
