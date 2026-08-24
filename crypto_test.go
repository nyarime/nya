package nya

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestArgon2EncryptRoundtrip(t *testing.T) {
	plain := bytes.Repeat([]byte("secret payload "), 100)
	password := []byte("correct horse battery staple")

	salt, err := NewWriterKDFSalt()
	if err != nil {
		t.Fatal(err)
	}
	p := KDFParams{
		Argon2id:  true,
		Salt:      salt,
		MemoryKiB: argon2MemoryKiB,
		Time:      argon2Time,
		Threads:   argon2Threads,
	}
	sealed, err := EncryptPayload(plain, password, p)
	if err != nil {
		t.Fatal(err)
	}

	var gh GlobalHeader
	WriteKDFParams(&gh, salt)
	got, err := DecryptPayload(sealed, password, &gh)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestLegacySHA256Decrypt(t *testing.T) {
	plain := []byte("legacy secret")
	password := []byte("pass")
	sealed, err := Encrypt(plain, password)
	if err != nil {
		t.Fatal(err)
	}
	var gh GlobalHeader
	gh.Flags = FlagEncrypted
	got, err := DecryptPayload(sealed, password, &gh)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("legacy decrypt mismatch")
	}
}

func TestEncryptedArchiveRequiresPassword(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(src, []byte("hello encrypted"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "enc.nya")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriterOpts(f, 0, 9, false, []byte("secret"))
	if err := w.AddFile(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if _, err := Open(archive); err != ErrPasswordRequired {
		t.Fatalf("Open without password: got %v want ErrPasswordRequired", err)
	}

	r, err := Open(archive, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.Flags&FlagEncrypted == 0 {
		t.Fatal("FlagEncrypted not set")
	}
	if r.Header.Flags&FlagKDFArgon2id == 0 {
		t.Fatal("FlagKDFArgon2id not set")
	}
	out := filepath.Join(dir, "out")
	if err := r.Extract(out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "plain.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello encrypted" {
		t.Fatalf("got %q", got)
	}
}
