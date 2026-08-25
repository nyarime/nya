// Command nya-sfx-stub is a thin wrapper around nya.RunSFXExtract.
// SFX wrapping uses the main `nya` binary as stub; this command remains for
// tests and legacy scripts.
package main

import (
	"fmt"
	"os"

	"github.com/nyarime/nya"
)

func main() {
	if err := nya.RunSFXExtract(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "nya-sfx: %v\n", err)
		os.Exit(1)
	}
}
