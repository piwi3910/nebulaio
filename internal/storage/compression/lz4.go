package compression

import (
	"bytes"
	"io"

	"github.com/pierrec/lz4/v4"
)

// LZ4Compressor implements LZ4 compression
type LZ4Compressor struct {
	level lz4.CompressionLevel
}

// NewLZ4Compressor creates a new LZ4 compressor
func NewLZ4Compressor(level Level) (*LZ4Compressor, error) {
	var lz4Level lz4.CompressionLevel
	switch level {
	case LevelFastest:
		lz4Level = lz4.Fast
	case LevelDefault:
		lz4Level = lz4.Level4
	case LevelBest:
		lz4Level = lz4.Level9
	default:
		lz4Level = lz4.Level4
	}

	return &LZ4Compressor{level: lz4Level}, nil
}

// Algorithm returns the algorithm name
func (c *LZ4Compressor) Algorithm() Algorithm {
	return AlgorithmLZ4
}

// Compress compresses data using LZ4
func (c *LZ4Compressor) Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := lz4.NewWriter(&buf)
	_ = writer.Apply(lz4.CompressionLevelOption(c.level))

	_, err := writer.Write(data)
	if err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Decompress decompresses LZ4 data
func (c *LZ4Compressor) Decompress(data []byte) ([]byte, error) {
	reader := lz4.NewReader(bytes.NewReader(data))
	return io.ReadAll(reader)
}

// CompressReader returns a reader that compresses data on read
func (c *LZ4Compressor) CompressReader(r io.Reader) (io.ReadCloser, error) {
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
func (c *LZ4Compressor) DecompressReader(r io.Reader) (io.ReadCloser, error) {
	reader := lz4.NewReader(r)
	return io.NopCloser(reader), nil
}

// Ensure LZ4Compressor implements Compressor
var _ Compressor = (*LZ4Compressor)(nil)
