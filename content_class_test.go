package nya

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyBytesMagicNotExtension(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want PayloadKind
	}{
		{"jpeg", []byte{0xff, 0xd8, 0xff, 0xe0}, PayloadDense},
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, PayloadDense},
		{"zip", []byte{'P', 'K', 0x03, 0x04}, PayloadDense},
		{"rar", []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00}, PayloadDense},
		{"7z", []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}, PayloadDense},
		{"pdf", []byte("%PDF-1.7"), PayloadDense},
		{"gzip", []byte{0x1f, 0x8b, 0x08}, PayloadDense},
		{"elf", []byte{0x7f, 'E', 'L', 'F', 0x02}, PayloadBinary},
		{"json", []byte("{\"title\": \"novel\"}\n"), PayloadTextLike},
		{"log", []byte("2026-08-25 INFO started server on :8080\n"), PayloadTextLike},
		{"code", []byte("package main\n\nfunc main() {}\n"), PayloadTextLike},
		{"xml", []byte("<?xml version=\"1.0\"?>\n<root/>\n"), PayloadTextLike},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyBytes(tc.head)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyFileIgnoresExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.bin")
	if err := os.WriteFile(path, []byte("hello log line\nanother line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ClassifyFile(path) != PayloadTextLike {
		t.Fatalf("expected text-like, got %v", ClassifyFile(path))
	}
	zipPath := filepath.Join(dir, "photo.txt")
	if err := os.WriteFile(zipPath, []byte{'P', 'K', 0x03, 0x04, 0, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if ClassifyFile(zipPath) != PayloadDense {
		t.Fatalf("expected dense zip magic, got %v", ClassifyFile(zipPath))
	}
}
