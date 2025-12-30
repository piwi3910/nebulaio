package compression

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"sync"
	"testing"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

func TestCompressors(t *testing.T) {
	testData := []byte("Hello, World! This is some test data that should be compressed.")

	// Create repetitive data that compresses well
	repetitiveData := bytes.Repeat([]byte("AAAAAAAAAA"), 1000)

	tests := []struct {
		name    string
		newFunc func() (Compressor, error)
		data    []byte
	}{
		{"Zstd-small", func() (Compressor, error) { return NewZstdCompressor(LevelDefault) }, testData},
		{"Zstd-repetitive", func() (Compressor, error) { return NewZstdCompressor(LevelDefault) }, repetitiveData},
		{"LZ4-small", func() (Compressor, error) { return NewLZ4Compressor(LevelDefault) }, testData},
		{"LZ4-repetitive", func() (Compressor, error) { return NewLZ4Compressor(LevelDefault) }, repetitiveData},
		{"Gzip-small", func() (Compressor, error) { return NewGzipCompressor(LevelDefault) }, testData},
		{"Gzip-repetitive", func() (Compressor, error) { return NewGzipCompressor(LevelDefault) }, repetitiveData},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp, err := tt.newFunc()
			if err != nil {
				t.Fatalf("failed to create compressor: %v", err)
			}

			// Test compression
			compressed, err := comp.Compress(tt.data)
			if err != nil {
				t.Fatalf("compression failed: %v", err)
			}

			if len(compressed) == 0 {
				t.Error("compressed data is empty")
			}

			// Test decompression
			decompressed, err := comp.Decompress(compressed)
			if err != nil {
				t.Fatalf("decompression failed: %v", err)
			}

			if !bytes.Equal(tt.data, decompressed) {
				t.Error("decompressed data doesn't match original")
			}
		})
	}
}

func TestCompressionLevels(t *testing.T) {
	data := bytes.Repeat([]byte("Test data for compression level comparison. "), 100)

	levels := []Level{LevelFastest, LevelDefault, LevelBest}

	for _, level := range levels {
		t.Run("Level-"+string(rune('0'+level)), func(t *testing.T) {
			// Test Zstd
			zstd, err := NewZstdCompressor(level)
			if err != nil {
				t.Fatalf("failed to create zstd compressor: %v", err)
			}

			compressed, err := zstd.Compress(data)
			if err != nil {
				t.Fatalf("zstd compression failed: %v", err)
			}

			decompressed, err := zstd.Decompress(compressed)
			if err != nil {
				t.Fatalf("zstd decompression failed: %v", err)
			}

			if !bytes.Equal(data, decompressed) {
				t.Error("zstd: data mismatch")
			}

			// Test LZ4
			lz4, err := NewLZ4Compressor(level)
			if err != nil {
				t.Fatalf("failed to create lz4 compressor: %v", err)
			}

			compressed, err = lz4.Compress(data)
			if err != nil {
				t.Fatalf("lz4 compression failed: %v", err)
			}

			decompressed, err = lz4.Decompress(compressed)
			if err != nil {
				t.Fatalf("lz4 decompression failed: %v", err)
			}

			if !bytes.Equal(data, decompressed) {
				t.Error("lz4: data mismatch")
			}

			// Test Gzip
			gz, err := NewGzipCompressor(level)
			if err != nil {
				t.Fatalf("failed to create gzip compressor: %v", err)
			}

			compressed, err = gz.Compress(data)
			if err != nil {
				t.Fatalf("gzip compression failed: %v", err)
			}

			decompressed, err = gz.Decompress(compressed)
			if err != nil {
				t.Fatalf("gzip decompression failed: %v", err)
			}

			if !bytes.Equal(data, decompressed) {
				t.Error("gzip: data mismatch")
			}
		})
	}
}

func TestCompressorReaders(t *testing.T) {
	data := []byte("Stream data for reader tests")

	compressors := []struct {
		newFunc func() (Compressor, error)
		name    string
	}{
		{func() (Compressor, error) { return NewZstdCompressor(LevelDefault) }, "Zstd"},
		{func() (Compressor, error) { return NewLZ4Compressor(LevelDefault) }, "LZ4"},
		{func() (Compressor, error) { return NewGzipCompressor(LevelDefault) }, "Gzip"},
	}

	for _, cc := range compressors {
		t.Run(cc.name+"-CompressReader", func(t *testing.T) {
			comp, _ := cc.newFunc()

			reader, err := comp.CompressReader(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("CompressReader failed: %v", err)
			}

			defer func() { _ = reader.Close() }()

			compressed, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("failed to read compressed data: %v", err)
			}

			if len(compressed) == 0 {
				t.Error("compressed data is empty")
			}
		})

		t.Run(cc.name+"-DecompressReader", func(t *testing.T) {
			comp, _ := cc.newFunc()

			// First compress
			compressed, _ := comp.Compress(data)

			// Then decompress using reader
			reader, err := comp.DecompressReader(bytes.NewReader(compressed))
			if err != nil {
				t.Fatalf("DecompressReader failed: %v", err)
			}

			defer func() { _ = reader.Close() }()

			var buf bytes.Buffer

			_, err = buf.ReadFrom(reader)
			if err != nil {
				t.Fatalf("failed to read decompressed data: %v", err)
			}

			if !bytes.Equal(data, buf.Bytes()) {
				t.Error("decompressed data doesn't match original")
			}
		})
	}
}

// mockBackend is a simple in-memory backend for testing
// Thread-safe with RWMutex protection for concurrent access.
type mockBackend struct {
	objects map[string]map[string][]byte
	mu      sync.RWMutex
}

func newMockBackend() *mockBackend {
	return &mockBackend{
		objects: make(map[string]map[string][]byte),
	}
}

func (m *mockBackend) Init(_ context.Context) error { return nil }
func (m *mockBackend) Close() error                 { return nil }

func (m *mockBackend) PutObject(_ context.Context, bucket, key string, reader io.Reader, _ int64) (*backend.PutResult, error) {
	// Read data before acquiring lock to minimize lock hold time.
	// This is safe because reader is not shared across goroutines.
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.objects[bucket] == nil {
		m.objects[bucket] = make(map[string][]byte)
	}

	m.objects[bucket][key] = data

	return &backend.PutResult{ETag: "mock-etag", Size: int64(len(data))}, nil
}

func (m *mockBackend) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.objects[bucket] == nil {
		return nil, io.EOF
	}

	data, ok := m.objects[bucket][key]
	if !ok {
		return nil, io.EOF
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockBackend) DeleteObject(_ context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.objects[bucket] != nil {
		delete(m.objects[bucket], key)
	}

	return nil
}

func (m *mockBackend) ObjectExists(_ context.Context, bucket, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.objects[bucket] == nil {
		return false, nil
	}

	_, ok := m.objects[bucket][key]

	return ok, nil
}

func (m *mockBackend) CreateBucket(_ context.Context, bucket string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.objects[bucket] == nil {
		m.objects[bucket] = make(map[string][]byte)
	}

	return nil
}

func (m *mockBackend) DeleteBucket(_ context.Context, bucket string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.objects, bucket)

	return nil
}

func (m *mockBackend) BucketExists(_ context.Context, bucket string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.objects[bucket]

	return ok, nil
}

func (m *mockBackend) GetStorageInfo(_ context.Context) (*backend.StorageInfo, error) {
	return &backend.StorageInfo{}, nil
}

func TestCompressionBackend(t *testing.T) {
	mockBE := newMockBackend()

	ctx := context.Background()
	err := mockBE.Init(ctx)
	if err != nil {
		t.Fatalf("failed to init mock backend: %v", err)
	}

	// Create compression backend
	cfg := DefaultConfig()
	cfg.MinSize = 10 // Lower threshold for testing

	compBackend, err := New(mockBE, cfg)
	if err != nil {
		t.Fatalf("failed to create compression backend: %v", err)
	}

	// Create test bucket
	err = compBackend.CreateBucket(ctx, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	t.Run("PutAndGet-Compressible", func(t *testing.T) {
		data := bytes.Repeat([]byte("Compressible data pattern. "), 100)

		result, err := compBackend.PutObject(ctx, "test-bucket", "compressible.txt", bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("PutObject failed: %v", err)
		}

		if result.ETag == "" {
			t.Error("expected non-empty ETag")
		}

		// Get the object back
		reader, err := compBackend.GetObject(ctx, "test-bucket", "compressible.txt")
		if err != nil {
			t.Fatalf("GetObject failed: %v", err)
		}

		defer func() { _ = reader.Close() }()

		retrieved, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("failed to read object: %v", err)
		}

		if len(retrieved) != len(data) {
			t.Errorf("size mismatch: expected %d, got %d", len(data), len(retrieved))
		}

		if !bytes.Equal(data, retrieved) {
			t.Error("data mismatch")
		}
	})

	t.Run("PutAndGet-Random", func(t *testing.T) {
		// Random data doesn't compress well
		data := make([]byte, 1024)
		_, _ = rand.Read(data)

		_, err := compBackend.PutObject(ctx, "test-bucket", "random.bin", bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("PutObject failed: %v", err)
		}

		reader, err := compBackend.GetObject(ctx, "test-bucket", "random.bin")
		if err != nil {
			t.Fatalf("GetObject failed: %v", err)
		}

		defer func() { _ = reader.Close() }()

		var buf bytes.Buffer

		_, err = buf.ReadFrom(reader)
		if err != nil {
			t.Fatalf("failed to read object: %v", err)
		}

		if !bytes.Equal(data, buf.Bytes()) {
			t.Error("data mismatch for random data")
		}
	})

	t.Run("SmallObject-NoCompression", func(t *testing.T) {
		data := []byte("tiny") // Below MinSize

		_, err := compBackend.PutObject(ctx, "test-bucket", "tiny.txt", bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("PutObject failed: %v", err)
		}

		reader, err := compBackend.GetObject(ctx, "test-bucket", "tiny.txt")
		if err != nil {
			t.Fatalf("GetObject failed: %v", err)
		}

		defer func() { _ = reader.Close() }()

		var buf bytes.Buffer

		_, err = buf.ReadFrom(reader)
		if err != nil {
			t.Fatalf("failed to read object: %v", err)
		}

		if !bytes.Equal(data, buf.Bytes()) {
			t.Error("data mismatch for small object")
		}
	})

	t.Run("ObjectExists", func(t *testing.T) {
		exists, err := compBackend.ObjectExists(ctx, "test-bucket", "compressible.txt")
		if err != nil {
			t.Fatalf("ObjectExists failed: %v", err)
		}

		if !exists {
			t.Error("object should exist")
		}

		exists, err = compBackend.ObjectExists(ctx, "test-bucket", "nonexistent")
		if err != nil {
			t.Fatalf("ObjectExists failed: %v", err)
		}

		if exists {
			t.Error("object should not exist")
		}
	})

	t.Run("DeleteObject", func(t *testing.T) {
		err := compBackend.DeleteObject(ctx, "test-bucket", "compressible.txt")
		if err != nil {
			t.Fatalf("DeleteObject failed: %v", err)
		}

		exists, _ := compBackend.ObjectExists(ctx, "test-bucket", "compressible.txt")
		if exists {
			t.Error("object should be deleted")
		}
	})
}

func TestNoCompression(t *testing.T) {
	mockBE := newMockBackend()

	ctx := context.Background()
	_ = mockBE.Init(ctx)

	cfg := Config{Algorithm: AlgorithmNone}

	compBackend, err := New(mockBE, cfg)
	if err != nil {
		t.Fatalf("failed to create compression backend: %v", err)
	}

	err = compBackend.CreateBucket(ctx, "test")
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	data := bytes.Repeat([]byte("test"), 100)

	_, err = compBackend.PutObject(ctx, "test", "key", bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	reader, err := compBackend.GetObject(ctx, "test", "key")
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}

	defer func() { _ = reader.Close() }()

	var buf bytes.Buffer

	_, _ = buf.ReadFrom(reader)

	if !bytes.Equal(data, buf.Bytes()) {
		t.Error("data mismatch")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Algorithm != AlgorithmZstd {
		t.Errorf("expected zstd algorithm, got %s", cfg.Algorithm)
	}

	if cfg.Level != LevelDefault {
		t.Errorf("expected default level, got %d", cfg.Level)
	}

	if cfg.MinSize != 1024 {
		t.Errorf("expected 1024 min size, got %d", cfg.MinSize)
	}

	if len(cfg.ExcludeTypes) == 0 {
		t.Error("expected excluded types")
	}
}

func TestCompressionStats(t *testing.T) {
	stats := &CompressionStats{
		Algorithm:      AlgorithmZstd,
		OriginalSize:   1000,
		CompressedSize: 250,
	}

	stats.CalculateRatio()

	if stats.CompressionRatio != 0.25 {
		t.Errorf("expected ratio 0.25, got %f", stats.CompressionRatio)
	}

	if stats.SpaceSaved() != 750 {
		t.Errorf("expected 750 bytes saved, got %d", stats.SpaceSaved())
	}

	if stats.SpaceSavedPercent() != 75 {
		t.Errorf("expected 75%% saved, got %f%%", stats.SpaceSavedPercent())
	}
}

func TestShouldCompress(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSize = 100

	mockBE := newMockBackend()
	b, _ := New(mockBE, cfg)

	// Small data should not compress
	if b.shouldCompress(50, "") {
		t.Error("small data should not be compressed")
	}

	// Large data should compress
	if !b.shouldCompress(1000, "") {
		t.Error("large data should be compressed")
	}

	// Excluded content type should not compress
	if b.shouldCompress(1000, "image/jpeg") {
		t.Error("jpeg should not be compressed")
	}

	// Text content type should compress
	if !b.shouldCompress(1000, "text/plain") {
		t.Error("text/plain should be compressed")
	}
}

func BenchmarkCompression(b *testing.B) {
	data := make([]byte, 1024*1024) // 1MB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.Run("Zstd-Fastest", func(b *testing.B) {
		comp, _ := NewZstdCompressor(LevelFastest)

		b.ResetTimer()

		for range b.N {
			_, _ = comp.Compress(data)
		}
	})

	b.Run("Zstd-Default", func(b *testing.B) {
		comp, _ := NewZstdCompressor(LevelDefault)

		b.ResetTimer()

		for range b.N {
			_, _ = comp.Compress(data)
		}
	})

	b.Run("LZ4-Default", func(b *testing.B) {
		comp, _ := NewLZ4Compressor(LevelDefault)

		b.ResetTimer()

		for range b.N {
			_, _ = comp.Compress(data)
		}
	})

	b.Run("Gzip-Default", func(b *testing.B) {
		comp, _ := NewGzipCompressor(LevelDefault)

		b.ResetTimer()

		for range b.N {
			_, _ = comp.Compress(data)
		}
	})
}

func BenchmarkDecompression(b *testing.B) {
	data := make([]byte, 1024*1024) // 1MB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.Run("Zstd", func(b *testing.B) {
		comp, _ := NewZstdCompressor(LevelDefault)
		compressed, _ := comp.Compress(data)

		b.ResetTimer()

		for range b.N {
			_, _ = comp.Decompress(compressed)
		}
	})

	b.Run("LZ4", func(b *testing.B) {
		comp, _ := NewLZ4Compressor(LevelDefault)
		compressed, _ := comp.Compress(data)

		b.ResetTimer()

		for range b.N {
			_, _ = comp.Decompress(compressed)
		}
	})

	b.Run("Gzip", func(b *testing.B) {
		comp, _ := NewGzipCompressor(LevelDefault)
		compressed, _ := comp.Compress(data)

		b.ResetTimer()

		for range b.N {
			_, _ = comp.Decompress(compressed)
		}
	})
}
