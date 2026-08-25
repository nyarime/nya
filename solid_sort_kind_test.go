package nya

import (
	"os"
	"testing"
)

func TestDetectContentKind(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"elf", []byte{0x7f, 'E', 'L', 'F', 2, 1}, contentKindELF},
		{"json", []byte(`{"key":"value"}`), contentKindJSON},
		{"text", []byte("package main\nfunc f() {}\n"), contentKindText},
		{"pe", []byte{'M', 'Z', 0, 0}, contentKindPE},
		{"macho", le32(0xFEEDFACF), contentKindMachO},
	}
	for _, tc := range cases {
		if got := contentKindFromBytes(tc.data); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestSolidSortGroupsByContentKindWithinExt(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, content []byte) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	elf := mk("a.bin", []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})
	rand := mk("b.bin", []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0xff, 0xfe})
	got := sortSolidFilePaths([]string{rand, elf})
	if got[0] != elf || got[1] != rand {
		t.Fatalf("ELF should precede opaque binary within .bin group: %v", got)
	}
}
