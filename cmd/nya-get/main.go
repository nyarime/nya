// Command nya-get is a compatibility shim for `nya get`.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	nyaBin := resolveNya()
	args := append([]string{"get"}, os.Args[1:]...)
	cmd := exec.Command(nyaBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "nya-get:", err)
		os.Exit(1)
	}
}

func resolveNya() string {
	var names []string
	if runtime.GOOS == "windows" {
		names = []string{"nya.exe", "nya"}
	} else {
		names = []string{"nya", "nya.exe"}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, n := range names {
			p := filepath.Join(dir, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	if p, err := exec.LookPath("nya"); err == nil {
		return p
	}
	return "nya"
}
