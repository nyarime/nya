package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// progressWriter prints carriage-return status lines and clears stale suffixes
// (Windows cmd.exe does not erase the tail when a shorter \r line overwrites).
type progressWriter struct {
	w           io.Writer
	lastLineLen int
}

func newProgressWriter(w io.Writer) *progressWriter {
	return &progressWriter{w: w}
}

func (pw *progressWriter) print(line string) {
	if len(line) < pw.lastLineLen {
		line += strings.Repeat(" ", pw.lastLineLen-len(line))
	}
	pw.lastLineLen = len(line)
	fmt.Fprint(pw.w, line, "\033[K")
	if f, ok := pw.w.(*os.File); ok {
		_ = f.Sync()
	}
}

func (pw *progressWriter) reset() {
	pw.lastLineLen = 0
}
