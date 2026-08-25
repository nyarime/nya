package main

import (
	"flag"
	"strings"
)

// rearrangeFlagsFirst moves -flag / --flag arguments (and their values) ahead
// of positional args so Go's flag package accepts `cmd path -o out` style.
// knownWithValue lists flag names (no leading dashes) that take a following argument.
func rearrangeFlagsFirst(args []string, knownWithValue map[string]bool) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			flags = append(flags, a)
			continue
		}
		flags = append(flags, a)
		if knownWithValue[name] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, pos...)
}

func parseFlagSet(fs *flag.FlagSet, args []string, knownWithValue map[string]bool) error {
	return fs.Parse(rearrangeFlagsFirst(args, knownWithValue))
}
