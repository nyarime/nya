package nya

import (
	"io"

	"github.com/nyarime/compress/lzma2"
	"github.com/nyarime/compress/zstd"
)

// lzma2DictSize is the dictionary the writer asks LZMA2 for (see compress/lzma2).
const lzma2DictSize = lzma2.DefaultDictSize

// Lzma2Options tunes the house LZMA2 encoder (see compress/lzma2).
type Lzma2Options = lzma2.Lzma2Options

func ZstdCompress(src []byte, level int) []byte {
	return zstd.ZstdCompress(src, level)
}

func ZstdCompressWithDict(src []byte, level int, dict []byte) []byte {
	return zstd.ZstdCompressWithDict(src, level, dict)
}

func ZstdCompressWithWindow(src []byte, level int) []byte {
	return zstd.ZstdCompressWithWindow(src, level)
}

func ZstdDecompress(data []byte) ([]byte, error) {
	return zstd.ZstdDecompress(data)
}

func ZstdNewReader(r io.Reader) (io.ReadCloser, error) {
	return zstd.ZstdNewReader(r)
}

func ZstdNewReaderLegacy(r io.Reader) (io.ReadCloser, error) {
	return zstd.ZstdNewReaderLegacy(r)
}

func DecompressZstd(data []byte) ([]byte, error) {
	return zstd.DecompressZstd(data)
}

func DecompressZstdLegacy(data []byte) ([]byte, error) {
	return zstd.DecompressZstdLegacy(data)
}

func DecompressZstdWithDict(data, dict []byte) ([]byte, error) {
	return zstd.DecompressZstdWithDict(data, dict)
}

func Lzma2Compress(src []byte, dictSize int) ([]byte, error) {
	return lzma2.Lzma2Compress(src, dictSize)
}

func Lzma2CompressOpts(src []byte, opts Lzma2Options) ([]byte, error) {
	return lzma2.Lzma2CompressOpts(src, opts)
}

func Lzma2Decompress(data []byte, dictSize int) ([]byte, error) {
	return lzma2.Lzma2Decompress(data, dictSize)
}

func Lzma2NewReader(r io.Reader, dictSize int) io.ReadCloser {
	return lzma2.Lzma2NewReader(r, dictSize)
}

func LzmaDecompress(data []byte) ([]byte, error) {
	return lzma2.LzmaDecompress(data)
}

func LzmaNewReader(r io.Reader) (io.ReadCloser, error) {
	return lzma2.LzmaNewReader(r)
}

func XzNewReader(r io.Reader) (io.ReadCloser, error) {
	return lzma2.XzNewReader(r)
}

func XzDecompress(data []byte) ([]byte, error) {
	return lzma2.XzDecompress(data)
}

// zstdReader wraps decompressed bytes for streaming extract.
type zstdReader struct {
	buf []byte
	off int
}

func (z *zstdReader) Read(p []byte) (int, error) {
	if z.off >= len(z.buf) {
		return 0, io.EOF
	}
	n := copy(p, z.buf[z.off:])
	z.off += n
	return n, nil
}

func (z *zstdReader) Close() error { return nil }
