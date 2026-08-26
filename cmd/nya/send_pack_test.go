package main

import "testing"

func TestFlagWasSet(t *testing.T) {
	args := []string{"-level", "9", "file.txt"}
	if !flagWasSet(args, "level") {
		t.Fatal("expected -level")
	}
	if flagWasSet(args, "o") {
		t.Fatal("unexpected -o")
	}
	args2 := []string{"-level=5", "x"}
	if !flagWasSet(args2, "level") {
		t.Fatal("expected -level=")
	}
	args3 := []string{"file.txt", "--level", "3"}
	if !flagWasSet(args3, "level") {
		t.Fatal("expected --level after positional")
	}
}
