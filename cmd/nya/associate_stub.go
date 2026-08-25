//go:build !windows

package main

import "fmt"

func cmdAssociate(args []string) error {
	return fmt.Errorf("associate is only supported on Windows (registers .nya double-click → nya open)")
}
