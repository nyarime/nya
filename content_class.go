package nya

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// PayloadKind classifies file bytes for pack/send decisions.
// Detection uses magic / content sniffing — never file extension.
type PayloadKind uint8

const (
	PayloadUnknown PayloadKind = iota
	PayloadTextLike              // text, code, logs, JSON, XML, … — compresses well
	PayloadDense                 // already compressed or media — little gain from re-compress
	PayloadBinary                // opaque binary (ELF/PE/…)
)

// ClassifyFile inspects path by magic/content (extension ignored).
func ClassifyFile(path string) PayloadKind {
	f, err := os.Open(path)
	if err != nil {
		return PayloadUnknown
	}
	defer f.Close()
	var head [512]byte
	n, _ := f.Read(head[:])
	return ClassifyBytes(head[:n])
}

// ClassifyBytes classifies a file-header prefix.
func ClassifyBytes(h []byte) PayloadKind {
	if len(h) == 0 {
		return PayloadUnknown
	}
	if kind := sniffDenseMagic(h); kind != PayloadUnknown {
		return kind
	}
	if kind := sniffBinaryMagic(h); kind != PayloadUnknown {
		return kind
	}
	if looksLikeUTF8Text(h) || looksLikeText(h) || looksLikeJSON(h) || looksLikeXML(h) {
		return PayloadTextLike
	}
	if bytes.IndexByte(h, 0) >= 0 {
		return PayloadBinary
	}
	return PayloadUnknown
}

// Compressible reports whether PayloadTextLike (or unknown-as-text-leaning) benefits from NYA codecs.
func (k PayloadKind) Compressible() bool {
	return k == PayloadTextLike || k == PayloadUnknown
}

func sniffDenseMagic(h []byte) PayloadKind {
	if len(h) >= 3 && h[0] == 0xff && h[1] == 0xd8 && h[2] == 0xff {
		return PayloadDense // JPEG
	}
	if len(h) >= 8 && h[0] == 0x89 && h[1] == 'P' && h[2] == 'N' && h[3] == 'G' {
		return PayloadDense
	}
	if (len(h) >= 6 && bytes.HasPrefix(h, []byte("GIF87a"))) || (len(h) >= 6 && bytes.HasPrefix(h, []byte("GIF89a"))) {
		return PayloadDense
	}
	if len(h) >= 12 && bytes.HasPrefix(h, []byte("RIFF")) && string(h[8:12]) == "WEBP" {
		return PayloadDense
	}
	if len(h) >= 12 && string(h[4:8]) == "ftyp" { // MP4 / ISO BMFF
		return PayloadDense
	}
	if len(h) >= 4 && (bytes.HasPrefix(h, []byte("\x1aE\xdf\xa3")) || // EBML/WebM/MKV
		bytes.HasPrefix(h, []byte("OggS")) ||
		bytes.HasPrefix(h, []byte("fLaC")) ||
		bytes.HasPrefix(h, []byte("ID3"))) {
		return PayloadDense
	}
	if len(h) >= 5 && bytes.HasPrefix(h, []byte("%PDF-")) {
		return PayloadDense
	}
	if len(h) >= 2 && h[0] == 'P' && h[1] == 'K' {
		return PayloadDense // ZIP / JAR / DOCX / …
	}
	if len(h) >= 6 && bytes.HasPrefix(h, []byte("7z\xBC\xAF\x27\x1C")) {
		return PayloadDense
	}
	if len(h) >= 6 && bytes.HasPrefix(h, []byte("Rar!\x1a\x07")) {
		return PayloadDense
	}
	if len(h) >= 2 && h[0] == 0x1f && h[1] == 0x8b {
		return PayloadDense // gzip
	}
	if len(h) >= 6 && bytes.HasPrefix(h, []byte("\xFD7zXZ")) {
		return PayloadDense // xz
	}
	if len(h) >= 3 && h[0] == 'B' && h[1] == 'Z' && h[2] == 'h' {
		return PayloadDense // bzip2
	}
	if len(h) >= 4 && binary.LittleEndian.Uint32(h[:4]) == 0xFD2FB528 {
		return PayloadDense // zstd frame
	}
	if len(h) >= 8 && bytes.Equal(h[:8], MagicHeader[:]) {
		return PayloadDense // already NYA
	}
	return PayloadUnknown
}

func sniffBinaryMagic(h []byte) PayloadKind {
	if len(h) >= 4 && h[0] == 0x7f && h[1] == 'E' && h[2] == 'L' && h[3] == 'F' {
		return PayloadBinary
	}
	if len(h) >= 2 && h[0] == 'M' && h[1] == 'Z' {
		return PayloadBinary
	}
	if len(h) >= 4 && h[0] == 0x00 && h[1] == 'a' && h[2] == 's' && h[3] == 'm' {
		return PayloadBinary
	}
	return PayloadUnknown
}

func looksLikeXML(h []byte) bool {
	trim := bytes.TrimSpace(h)
	return bytes.HasPrefix(trim, []byte("<?xml")) || bytes.HasPrefix(trim, []byte("<!DOCTYPE")) ||
		(len(trim) > 0 && trim[0] == '<' && bytes.IndexByte(trim, '>') > 1)
}

func looksLikeUTF8Text(h []byte) bool {
	if !utf8.Valid(h) {
		return false
	}
	return looksLikeText(h)
}

// ScanPayloadKinds walks a file or directory tree and counts magic-based kinds.
func ScanPayloadKinds(root string) (textLike, dense, other int, err error) {
	info, err := os.Lstat(root)
	if err != nil {
		return 0, 0, 0, err
	}
	if !info.IsDir() {
		switch ClassifyFile(root) {
		case PayloadTextLike:
			return 1, 0, 0, nil
		case PayloadDense:
			return 0, 1, 0, nil
		default:
			return 0, 0, 1, nil
		}
	}
	err = filepath.Walk(root, func(p string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		switch ClassifyFile(p) {
		case PayloadTextLike:
			textLike++
		case PayloadDense:
			dense++
		default:
			other++
		}
		return nil
	})
	return textLike, dense, other, err
}

