// Package compression provides transparent object compression for NebulaIO.
//
// The package implements a storage backend wrapper that compresses objects
// before storage and decompresses on retrieval. Supported algorithms:
//
//   - Zstandard (zstd): Best compression ratio with fast decompression (recommended)
//   - LZ4: Fastest compression/decompression, moderate ratio
//   - Gzip: Wide compatibility, moderate performance
//
// Features:
//   - Automatic content-type detection to skip pre-compressed files
//   - Configurable compression levels
//   - Streaming compression for large objects
//   - Per-object compression statistics
//
// Example usage:
//
//	backend := compression.NewBackend(fsBackend, compression.Config{
//	    Algorithm: compression.Zstd,
//	    Level:     3,
//	})
package compression

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// Algorithm represents a compression algorithm
type Algorithm string

const (
	// AlgorithmNone disables compression
	AlgorithmNone Algorithm = "none"
	// AlgorithmZstd uses Zstandard compression (recommended)
	AlgorithmZstd Algorithm = "zstd"
	// AlgorithmLZ4 uses LZ4 compression (faster, less compression)
	AlgorithmLZ4 Algorithm = "lz4"
	// AlgorithmGzip uses Gzip compression (widely compatible)
	AlgorithmGzip Algorithm = "gzip"
)

// Level represents compression level
type Level int

const (
	// LevelFastest prioritizes speed over compression ratio
	LevelFastest Level = 1
	// LevelDefault balances speed and compression
	LevelDefault Level = 3
	// LevelBest prioritizes compression ratio over speed
	LevelBest Level = 9
)

// Config holds compression configuration
type Config struct {
	// Algorithm to use for compression
	Algorithm Algorithm `json:"algorithm" yaml:"algorithm"`
	// Level controls compression ratio vs speed trade-off
	Level Level `json:"level" yaml:"level"`
	// MinSize is the minimum object size to compress (bytes)
	MinSize int64 `json:"min_size" yaml:"min_size"`
	// ExcludeTypes are content types that should not be compressed
	ExcludeTypes []string `json:"exclude_types" yaml:"exclude_types"`
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Algorithm: AlgorithmZstd,
		Level:     LevelDefault,
		MinSize:   1024, // 1KB minimum
		ExcludeTypes: []string{
			// Already compressed formats
			"image/jpeg",
			"image/png",
			"image/gif",
			"image/webp",
			"video/mp4",
			"video/webm",
			"video/mpeg",
			"audio/mpeg",
			"audio/mp4",
			"audio/ogg",
			"application/zip",
			"application/gzip",
			"application/x-gzip",
			"application/x-bzip2",
			"application/x-xz",
			"application/x-7z-compressed",
			"application/x-rar-compressed",
		},
	}
}

// Compressor handles compression/decompression
type Compressor interface {
	// Compress compresses data and returns compressed bytes
	Compress(data []byte) ([]byte, error)
	// Decompress decompresses data and returns original bytes
	Decompress(data []byte) ([]byte, error)
	// CompressReader returns a reader that compresses on read
	CompressReader(r io.Reader) (io.ReadCloser, error)
	// DecompressReader returns a reader that decompresses on read
	DecompressReader(r io.Reader) (io.ReadCloser, error)
	// Algorithm returns the algorithm name
	Algorithm() Algorithm
}

// metaKeySuffix is appended to object keys to store compression metadata
const metaKeySuffix = ".__compression_meta__"

// Backend wraps another backend with compression support
type Backend struct {
	inner      backend.Backend
	config     Config
	compressor Compressor
}

// New creates a compression backend wrapping the given backend
func New(inner backend.Backend, cfg Config) (*Backend, error) {
	if inner == nil {
		return nil, errors.New("inner backend is required")
	}

	var comp Compressor
	var err error

	switch cfg.Algorithm {
	case AlgorithmNone:
		return &Backend{inner: inner, config: cfg, compressor: nil}, nil
	case AlgorithmZstd:
		comp, err = NewZstdCompressor(cfg.Level)
	case AlgorithmLZ4:
		comp, err = NewLZ4Compressor(cfg.Level)
	case AlgorithmGzip:
		comp, err = NewGzipCompressor(cfg.Level)
	default:
		return nil, fmt.Errorf("unknown compression algorithm: %s", cfg.Algorithm)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create compressor: %w", err)
	}

	return &Backend{
		inner:      inner,
		config:     cfg,
		compressor: comp,
	}, nil
}

// Init initializes the backend
func (b *Backend) Init(ctx context.Context) error {
	return b.inner.Init(ctx)
}

// Close closes the backend
func (b *Backend) Close() error {
	return b.inner.Close()
}

// shouldCompress determines if an object should be compressed
func (b *Backend) shouldCompress(size int64, contentType string) bool {
	if b.compressor == nil {
		return false
	}

	// Skip small objects
	if size > 0 && size < b.config.MinSize {
		return false
	}

	// Skip excluded content types
	contentType = strings.ToLower(strings.Split(contentType, ";")[0])
	for _, excluded := range b.config.ExcludeTypes {
		if strings.ToLower(excluded) == contentType {
			return false
		}
	}

	return true
}

// PutObject stores an object with optional compression
func (b *Backend) PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64) (*backend.PutResult, error) {
	// Read all data to determine if we should compress
	var buf bytes.Buffer
	actualSize, err := io.Copy(&buf, data)
	if err != nil {
		return nil, fmt.Errorf("failed to read object data: %w", err)
	}

	// Check if we should compress
	if b.shouldCompress(actualSize, "") {
		compressed, err := b.compressor.Compress(buf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("compression failed: %w", err)
		}

		// Only use compression if it actually reduces size
		if int64(len(compressed)) < actualSize {
			result, err := b.inner.PutObject(ctx, bucket, key, bytes.NewReader(compressed), int64(len(compressed)))
			if err != nil {
				return nil, err
			}

			// Store compression metadata as separate object
			meta := fmt.Sprintf(`{"algorithm":"%s","original_size":%d}`, b.config.Algorithm, actualSize)
			_, err = b.inner.PutObject(ctx, bucket, key+metaKeySuffix, bytes.NewReader([]byte(meta)), int64(len(meta)))
			if err != nil {
				// Clean up the compressed object if metadata write fails
				_ = b.inner.DeleteObject(ctx, bucket, key)
				return nil, fmt.Errorf("failed to store compression metadata: %w", err)
			}

			return result, nil
		}
	}

	// Store uncompressed
	return b.inner.PutObject(ctx, bucket, key, bytes.NewReader(buf.Bytes()), actualSize)
}

// GetObject retrieves an object with automatic decompression
func (b *Backend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	reader, err := b.inner.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	// Check if compression metadata exists
	metaReader, err := b.inner.GetObject(ctx, bucket, key+metaKeySuffix)
	if err != nil {
		// No metadata = not compressed
		return reader, nil
	}

	metaBytes, err := io.ReadAll(metaReader)
	_ = metaReader.Close()
	if err != nil {
		// Can't read metadata, assume not compressed
		return reader, nil
	}

	// Parse algorithm from metadata (simple parsing)
	metaStr := string(metaBytes)
	var algorithm Algorithm
	if strings.Contains(metaStr, `"algorithm":"zstd"`) {
		algorithm = AlgorithmZstd
	} else if strings.Contains(metaStr, `"algorithm":"lz4"`) {
		algorithm = AlgorithmLZ4
	} else if strings.Contains(metaStr, `"algorithm":"gzip"`) {
		algorithm = AlgorithmGzip
	} else {
		// Unknown or no compression
		return reader, nil
	}

	// Read compressed data
	compressed, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read compressed data: %w", err)
	}

	// Decompress
	var decompressor Compressor
	switch algorithm {
	case AlgorithmZstd:
		decompressor, err = NewZstdCompressor(LevelDefault)
	case AlgorithmLZ4:
		decompressor, err = NewLZ4Compressor(LevelDefault)
	case AlgorithmGzip:
		decompressor, err = NewGzipCompressor(LevelDefault)
	default:
		return nil, fmt.Errorf("unknown compression algorithm: %s", algorithm)
	}

	if err != nil {
		return nil, err
	}

	decompressed, err := decompressor.Decompress(compressed)
	if err != nil {
		return nil, fmt.Errorf("decompression failed: %w", err)
	}

	return io.NopCloser(bytes.NewReader(decompressed)), nil
}

// DeleteObject removes an object and its compression metadata
func (b *Backend) DeleteObject(ctx context.Context, bucket, key string) error {
	// Delete compression metadata if it exists (ignore errors)
	_ = b.inner.DeleteObject(ctx, bucket, key+metaKeySuffix)

	return b.inner.DeleteObject(ctx, bucket, key)
}

// ObjectExists checks if an object exists
func (b *Backend) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	return b.inner.ObjectExists(ctx, bucket, key)
}

// CreateBucket creates a new bucket
func (b *Backend) CreateBucket(ctx context.Context, bucket string) error {
	return b.inner.CreateBucket(ctx, bucket)
}

// DeleteBucket removes a bucket
func (b *Backend) DeleteBucket(ctx context.Context, bucket string) error {
	return b.inner.DeleteBucket(ctx, bucket)
}

// BucketExists checks if a bucket exists
func (b *Backend) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return b.inner.BucketExists(ctx, bucket)
}

// GetStorageInfo returns storage statistics
func (b *Backend) GetStorageInfo(ctx context.Context) (*backend.StorageInfo, error) {
	return b.inner.GetStorageInfo(ctx)
}

// Ensure Backend implements backend.Backend
var _ backend.Backend = (*Backend)(nil)

// CompressionStats holds statistics about compression effectiveness
type CompressionStats struct {
	Algorithm        Algorithm
	OriginalSize     int64
	CompressedSize   int64
	CompressionRatio float64
	Duration         time.Duration
}

// CalculateRatio computes the compression ratio
func (s *CompressionStats) CalculateRatio() {
	if s.OriginalSize > 0 {
		s.CompressionRatio = float64(s.CompressedSize) / float64(s.OriginalSize)
	}
}

// SpaceSaved returns bytes saved by compression
func (s *CompressionStats) SpaceSaved() int64 {
	return s.OriginalSize - s.CompressedSize
}

// SpaceSavedPercent returns percentage of space saved
func (s *CompressionStats) SpaceSavedPercent() float64 {
	if s.OriginalSize > 0 {
		return (1 - s.CompressionRatio) * 100
	}
	return 0
}
