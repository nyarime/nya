package main

import "testing"

func TestRearrangeFlagsFirst(t *testing.T) {
	known := map[string]bool{"o": true, "stub": true}
	got := rearrangeFlagsFirst([]string{`C:\a\pack.nya`, "-o", `D:\test.exe`}, known)
	want0, want1, want2 := "-o", `D:\test.exe`, `C:\a\pack.nya`
	if len(got) != 3 || got[0] != want0 || got[1] != want1 || got[2] != want2 {
		t.Fatalf("got %#v", got)
	}
	got = rearrangeFlagsFirst([]string{"-o", "out.exe", "pack.nya"}, known)
	if len(got) != 3 || got[0] != "-o" || got[2] != "pack.nya" {
		t.Fatalf("already-ordered: %#v", got)
	}
	got = rearrangeFlagsFirst([]string{"pack.nya", "-o=out.exe"}, known)
	if len(got) != 2 || got[0] != "-o=out.exe" || got[1] != "pack.nya" {
		t.Fatalf("equals form: %#v", got)
	}
}
