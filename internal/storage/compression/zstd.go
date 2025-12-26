package compression

import (
	"bytes"
	"io"

	"github.com/klauspost/compress/zstd"
)

// ZstdCompressor implements Zstandard compression
type ZstdCompressor struct {
	level zstd.EncoderLevel
}

// NewZstdCompressor creates a new Zstd compressor
func NewZstdCompressor(level Level) (*ZstdCompressor, error) {
	var zstdLevel zstd.EncoderLevel
	switch level {
	case LevelFastest:
		zstdLevel = zstd.SpeedFastest
	case LevelDefault:
		zstdLevel = zstd.SpeedDefault
	case LevelBest:
		zstdLevel = zstd.SpeedBestCompression
	default:
		zstdLevel = zstd.SpeedDefault
	}

	return &ZstdCompressor{level: zstdLevel}, nil
}

// Algorithm returns the algorithm name
func (c *ZstdCompressor) Algorithm() Algorithm {
	return AlgorithmZstd
}

// Compress compresses data using Zstandard
func (c *ZstdCompressor) Compress(data []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(c.level))
	if err != nil {
		return nil, err
	}
	defer func() { _ = enc.Close() }()

	return enc.EncodeAll(data, nil), nil
}

// Decompress decompresses Zstandard data
func (c *ZstdCompressor) Decompress(data []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	return dec.DecodeAll(data, nil)
}

// CompressReader returns a reader that compresses data on read
func (c *ZstdCompressor) CompressReader(r io.Reader) (io.ReadCloser, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	compressed, err := c.Compress(data)
	if err != nil {
		return nil, err
	}

	return io.NopCloser(bytes.NewReader(compressed)), nil
}

// DecompressReader returns a reader that decompresses data on read
func (c *ZstdCompressor) DecompressReader(r io.Reader) (io.ReadCloser, error) {
	dec, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &zstdReadCloser{Decoder: dec}, nil
}

// zstdReadCloser wraps a zstd.Decoder for closing
type zstdReadCloser struct {
	*zstd.Decoder
}

func (r *zstdReadCloser) Close() error {
	r.Decoder.Close()
	return nil
}

// Ensure ZstdCompressor implements Compressor
var _ Compressor = (*ZstdCompressor)(nil)
