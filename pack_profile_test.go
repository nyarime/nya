package nya

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSendPackProfileSingleText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("2026 INFO line one\n2026 INFO line two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := SendPackProfileFor(path, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Level != LevelFast || p.Solid {
		t.Fatalf("time-first: got level=%d solid=%t reason=%q", p.Level, p.Solid, p.Reason)
	}
}

func TestSendPackProfileSingleDense(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.jpg")
	if err := os.WriteFile(path, []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := SendPackProfileFor(path, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Level != LevelStore || p.Solid {
		t.Fatalf("got level=%d solid=%t", p.Level, p.Solid)
	}
}

func TestSendPackProfileSingleBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app")
	head := []byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00}
	if err := os.WriteFile(path, append(head, make([]byte, 64)...), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := SendPackProfileFor(path, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Level != LevelFast || p.Solid {
		t.Fatalf("got level=%d solid=%t", p.Level, p.Solid)
	}
}

func TestSendPackProfileTextDir(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		name := filepath.Join(root, "f"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(name, []byte("line\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := SendPackProfileFor(root, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Level != LevelFast || p.Solid {
		t.Fatalf("time-first: got level=%d solid=%t", p.Level, p.Solid)
	}
}

func TestSendPackProfileExplicitLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one.log")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := SendPackProfileFor(path, true, 3)
	if err != nil {
		t.Fatal(err)
	}
	if p.Level != 3 || p.Solid {
		t.Fatalf("explicit level should not auto-solid single file: level=%d solid=%t", p.Level, p.Solid)
	}
}

func TestSendPackProfileExplicitLevel9SolidDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := SendPackProfileFor(root, true, 9)
	if err != nil {
		t.Fatal(err)
	}
	if p.Level != 9 || !p.Solid {
		t.Fatalf("explicit 9 + multi text should solid: level=%d solid=%t", p.Level, p.Solid)
	}
}
