package nya

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectFormatByMagicIgnoresExtension(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "payload.dat")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	h := &zip.FileHeader{Name: "a.txt", Method: zip.Store}
	h.SetModTime(time.Now())
	wr, _ := w.CreateHeader(h)
	wr.Write([]byte("hello"))
	w.Close()
	f.Close()

	got, err := DetectFormatByMagic(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != FormatZIP {
		t.Fatalf("got %q, want zip", got)
	}
}

func TestRepairZipRebuildsCentralDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "broken.dat")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	h := &zip.FileHeader{Name: "readme.txt", Method: zip.Store}
	h.SetModTime(time.Now())
	wr, _ := w.CreateHeader(h)
	wr.Write([]byte("zip repair test"))
	w.Close()
	f.Close()

	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	// Wipe EOCD (last 22 bytes typical for empty comment)
	for i := len(raw) - 22; i < len(raw); i++ {
		if i >= 0 {
			raw[i] = 0
		}
	}
	if err := os.WriteFile(src, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "fixed.zip")
	res, err := Repair(src, out)
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatZIP || res.RepairedChunks != 1 {
		t.Fatalf("result: %+v", res)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "readme.txt" {
		t.Fatalf("unexpected zip contents: %+v", zr.File)
	}
}

func TestRepairRAR5Structure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "archive.bin")
	if err := writeTestRAR5(src, map[string][]byte{"data.bin": []byte("rar repair")}); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(src)
	// Corrupt end block region
	if len(raw) > 8 {
		raw[len(raw)-4] = 0
		raw[len(raw)-3] = 0
	}
	os.WriteFile(src, raw, 0o644)

	out := filepath.Join(dir, "fixed.rar")
	res, err := Repair(src, out)
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatRAR || res.RepairedChunks < 1 {
		t.Fatalf("result: %+v err=%v", res, err)
	}
	fixed, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(fixed, rarSig5) {
		t.Fatal("output missing RAR5 signature")
	}
}

func writeTestRAR5(path string, files map[string][]byte) error {
	var out bytes.Buffer
	out.Write(rarSig5)
	writeRAR5ArcBlock(&out)
	for name, data := range files {
		if err := appendRAR5StoreFile(&out, name, data); err != nil {
			return err
		}
	}
	writeRAR5EndBlock(&out)
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func appendRAR5StoreFile(out *bytes.Buffer, name string, data []byte) error {
	nameBytes := []byte(name)
	dataCRC := crc32.ChecksumIEEE(data)
	var extra bytes.Buffer
	writeVint(&extra, r5FileMtime|r5FileCRC32)
	writeVint(&extra, uint64(len(data)))
	writeVint(&extra, 0x20)
	binary.Write(&extra, binary.LittleEndian, uint32(time.Now().Unix()))
	binary.Write(&extra, binary.LittleEndian, dataCRC)
	writeVint(&extra, 0)
	writeVint(&extra, 1)
	writeVint(&extra, uint64(len(nameBytes)))
	extra.Write(nameBytes)

	var body bytes.Buffer
	writeVint(&body, r5TypeFile)
	writeVint(&body, r5FlagData)
	writeVint(&body, uint64(len(data)))
	body.Write(extra.Bytes())

	var sizeVint bytes.Buffer
	writeVint(&sizeVint, uint64(body.Len()))
	crcPayload := append(sizeVint.Bytes(), body.Bytes()...)
	binary.Write(out, binary.LittleEndian, crc32.ChecksumIEEE(crcPayload))
	out.Write(crcPayload)
	out.Write(data)
	return nil
}

const (
	r5FileMtime = 0x0002
	r5FileCRC32 = 0x0004
)
