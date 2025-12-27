// Package benchmark provides performance benchmarks for critical NebulaIO operations.
// Run with: go test -bench=. -benchmem ./internal/benchmark/...
package benchmark

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"io"
	"testing"

	"github.com/klauspost/compress/zstd"
	lz4 "github.com/pierrec/lz4/v4"
)

// Benchmark data sizes
const (
	KB = 1024
	MB = 1024 * KB
)

// generateTestData creates random test data of the specified size.
func generateTestData(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

// BenchmarkSHA256 benchmarks SHA256 hashing at various sizes.
func BenchmarkSHA256_1KB(b *testing.B) {
	data := generateTestData(1 * KB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		sha256.Sum256(data)
	}
}

func BenchmarkSHA256_1MB(b *testing.B) {
	data := generateTestData(1 * MB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		sha256.Sum256(data)
	}
}

func BenchmarkSHA256_10MB(b *testing.B) {
	data := generateTestData(10 * MB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		sha256.Sum256(data)
	}
}

// BenchmarkZstdCompression benchmarks Zstandard compression.
func BenchmarkZstdCompress_1KB(b *testing.B) {
	data := generateTestData(1 * KB)
	encoder, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		encoder.EncodeAll(data, nil)
	}
}

func BenchmarkZstdCompress_1MB(b *testing.B) {
	data := generateTestData(1 * MB)
	encoder, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		encoder.EncodeAll(data, nil)
	}
}

func BenchmarkZstdDecompress_1MB(b *testing.B) {
	data := generateTestData(1 * MB)
	encoder, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	compressed := encoder.EncodeAll(data, nil)
	decoder, _ := zstd.NewReader(nil)
	b.ResetTimer()
	b.SetBytes(int64(len(compressed)))
	for i := 0; i < b.N; i++ {
		decoder.DecodeAll(compressed, nil)
	}
}

// BenchmarkLZ4Compression benchmarks LZ4 compression.
func BenchmarkLZ4Compress_1KB(b *testing.B) {
	data := generateTestData(1 * KB)
	dst := make([]byte, lz4.CompressBlockBound(len(data)))
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		lz4.CompressBlock(data, dst, nil)
	}
}

func BenchmarkLZ4Compress_1MB(b *testing.B) {
	data := generateTestData(1 * MB)
	dst := make([]byte, lz4.CompressBlockBound(len(data)))
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		lz4.CompressBlock(data, dst, nil)
	}
}

func BenchmarkLZ4Decompress_1MB(b *testing.B) {
	data := generateTestData(1 * MB)
	dst := make([]byte, lz4.CompressBlockBound(len(data)))
	n, _ := lz4.CompressBlock(data, dst, nil)
	compressed := dst[:n]
	output := make([]byte, len(data))
	b.ResetTimer()
	b.SetBytes(int64(len(compressed)))
	for i := 0; i < b.N; i++ {
		lz4.UncompressBlock(compressed, output)
	}
}

// BenchmarkJSONMarshal benchmarks JSON marshaling.
func BenchmarkJSONMarshal_SmallObject(b *testing.B) {
	obj := map[string]interface{}{
		"id":         "12345",
		"name":       "test-object",
		"size":       1024,
		"created_at": "2024-01-01T00:00:00Z",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(obj)
	}
}

func BenchmarkJSONMarshal_LargeObject(b *testing.B) {
	items := make([]map[string]interface{}, 1000)
	for i := range items {
		items[i] = map[string]interface{}{
			"id":         i,
			"name":       "test-object",
			"size":       1024 * i,
			"created_at": "2024-01-01T00:00:00Z",
			"metadata": map[string]string{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
		}
	}
	obj := map[string]interface{}{
		"items": items,
		"total": len(items),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(obj)
	}
}

func BenchmarkJSONUnmarshal_SmallObject(b *testing.B) {
	data := []byte(`{"id":"12345","name":"test-object","size":1024,"created_at":"2024-01-01T00:00:00Z"}`)
	var obj map[string]interface{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Unmarshal(data, &obj)
	}
}

// BenchmarkBufferOperations benchmarks buffer operations.
func BenchmarkBufferWrite_1KB(b *testing.B) {
	data := generateTestData(1 * KB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		buf.Write(data)
	}
}

func BenchmarkBufferWrite_1MB(b *testing.B) {
	data := generateTestData(1 * MB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		buf.Write(data)
	}
}

func BenchmarkBufferPreallocatedWrite_1MB(b *testing.B) {
	data := generateTestData(1 * MB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBuffer(make([]byte, 0, len(data)))
		buf.Write(data)
	}
}

// BenchmarkCopy benchmarks io.Copy operations.
func BenchmarkIOCopy_1KB(b *testing.B) {
	data := generateTestData(1 * KB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(data)
		dst := &bytes.Buffer{}
		io.Copy(dst, src)
	}
}

func BenchmarkIOCopy_1MB(b *testing.B) {
	data := generateTestData(1 * MB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(data)
		dst := &bytes.Buffer{}
		io.Copy(dst, src)
	}
}

func BenchmarkIOCopy_10MB(b *testing.B) {
	data := generateTestData(10 * MB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(data)
		dst := &bytes.Buffer{}
		io.Copy(dst, src)
	}
}

// BenchmarkIOCopyBuffer benchmarks io.CopyBuffer with fixed buffer.
func BenchmarkIOCopyBuffer_1MB_32KB(b *testing.B) {
	data := generateTestData(1 * MB)
	buf := make([]byte, 32*KB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(data)
		dst := &bytes.Buffer{}
		io.CopyBuffer(dst, src, buf)
	}
}

func BenchmarkIOCopyBuffer_1MB_256KB(b *testing.B) {
	data := generateTestData(1 * MB)
	buf := make([]byte, 256*KB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(data)
		dst := &bytes.Buffer{}
		io.CopyBuffer(dst, src, buf)
	}
}

// Parallel benchmarks for concurrent operations.
func BenchmarkSHA256_1MB_Parallel(b *testing.B) {
	data := generateTestData(1 * MB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sha256.Sum256(data)
		}
	})
}

func BenchmarkZstdCompress_1MB_Parallel(b *testing.B) {
	data := generateTestData(1 * MB)
	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	b.RunParallel(func(pb *testing.PB) {
		encoder, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
		for pb.Next() {
			encoder.EncodeAll(data, nil)
		}
	})
}

func BenchmarkJSONMarshal_Parallel(b *testing.B) {
	obj := map[string]interface{}{
		"id":         "12345",
		"name":       "test-object",
		"size":       1024,
		"created_at": "2024-01-01T00:00:00Z",
		"metadata": map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			json.Marshal(obj)
		}
	})
}
