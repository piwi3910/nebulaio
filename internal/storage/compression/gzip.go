package compression

import (
	"bytes"
	"compress/gzip"
	"io"
)

// GzipCompressor implements Gzip compression.
type GzipCompressor struct {
	level int
}

// NewGzipCompressor creates a new Gzip compressor.
func NewGzipCompressor(level Level) (*GzipCompressor, error) {
	var gzipLevel int

	switch level {
	case LevelFastest:
		gzipLevel = gzip.BestSpeed
	case LevelDefault:
		gzipLevel = gzip.DefaultCompression
	case LevelBest:
		gzipLevel = gzip.BestCompression
	default:
		gzipLevel = gzip.DefaultCompression
	}

	return &GzipCompressor{level: gzipLevel}, nil
}

// Algorithm returns the algorithm name.
func (c *GzipCompressor) Algorithm() Algorithm {
	return AlgorithmGzip
}

// Compress compresses data using Gzip.
func (c *GzipCompressor) Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer

	writer, err := gzip.NewWriterLevel(&buf, c.level)
	if err != nil {
		return nil, err
	}

	_, err = writer.Write(data)
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Decompress decompresses Gzip data.
func (c *GzipCompressor) Decompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	defer func() { _ = reader.Close() }()

	return io.ReadAll(reader)
}

// CompressReader returns a reader that compresses data on read.
func (c *GzipCompressor) CompressReader(r io.Reader) (io.ReadCloser, error) {
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

// DecompressReader returns a reader that decompresses data on read.
func (c *GzipCompressor) DecompressReader(r io.Reader) (io.ReadCloser, error) {
	reader, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	return reader, nil
}

// Ensure GzipCompressor implements Compressor.
var _ Compressor = (*GzipCompressor)(nil)
