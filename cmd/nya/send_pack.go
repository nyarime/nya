package main

import "strings"

// flagWasSet reports whether a named flag appeared in args (before "--").
func flagWasSet(args []string, name string) bool {
	long := "--" + name
	short := "-" + name
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == long || a == short {
			return true
		}
		if strings.HasPrefix(a, long+"=") || strings.HasPrefix(a, short+"=") {
			return true
		}
	}
	return false
}
