package dram

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewCache(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 100 // 100MB for testing
	config.ShardCount = 16

	cache := New(config)
	defer cache.Close()

	if cache == nil {
		t.Fatal("expected cache to be created")
	}

	if len(cache.shards) != 16 {
		t.Errorf("expected 16 shards, got %d", len(cache.shards))
	}
}

func TestPutAndGet(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10 // 10MB
	config.TTL = time.Minute

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	key := "test-bucket/test-key"
	data := []byte("test data content")
	contentType := "text/plain"
	etag := "abc123"

	// Put entry
	err := cache.Put(ctx, key, data, contentType, etag)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get entry
	entry, ok := cache.Get(ctx, key)
	if !ok {
		t.Fatal("expected entry to be found")
	}

	if string(entry.Data) != string(data) {
		t.Errorf("expected data %q, got %q", data, entry.Data)
	}

	if entry.ContentType != contentType {
		t.Errorf("expected content type %q, got %q", contentType, entry.ContentType)
	}

	if entry.ETag != etag {
		t.Errorf("expected etag %q, got %q", etag, entry.ETag)
	}
}

func TestHas(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	key := "test-bucket/test-key"

	// Should not exist initially
	if cache.Has(ctx, key) {
		t.Error("expected key to not exist")
	}

	// Put and check again
	cache.Put(ctx, key, []byte("data"), "", "")

	if !cache.Has(ctx, key) {
		t.Error("expected key to exist")
	}
}

func TestDelete(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	key := "test-bucket/test-key"

	// Put entry
	cache.Put(ctx, key, []byte("data"), "", "")

	// Verify exists
	if !cache.Has(ctx, key) {
		t.Fatal("expected key to exist before delete")
	}

	// Delete
	err := cache.Delete(ctx, key)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted
	if cache.Has(ctx, key) {
		t.Error("expected key to be deleted")
	}
}

func TestExpiration(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10
	config.TTL = 50 * time.Millisecond

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	key := "test-bucket/test-key"

	// Put entry
	cache.Put(ctx, key, []byte("data"), "", "")

	// Should exist immediately
	if !cache.Has(ctx, key) {
		t.Error("expected key to exist before expiration")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	if cache.Has(ctx, key) {
		t.Error("expected key to be expired")
	}
}

func TestEviction(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1000     // Very small cache
	config.ShardCount = 1     // Single shard for predictable behavior
	config.EntryMaxSize = 500 // Allow entries up to 500 bytes

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()

	// Put entries that exceed cache size
	for i := range 10 {
		key := fmt.Sprintf("bucket/key-%d", i)
		data := make([]byte, 200) // 200 bytes each
		cache.Put(ctx, key, data, "", "")
	}

	// Cache should have evicted old entries
	metrics := cache.Metrics()
	if metrics.Size > config.MaxSize {
		t.Errorf("cache size %d exceeds max size %d", metrics.Size, config.MaxSize)
	}

	// Some evictions should have occurred
	if metrics.Evictions == 0 {
		t.Error("expected some evictions to occur")
	}
}

func TestMetrics(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10
	config.MetricsEnabled = true

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()

	// Put some entries
	for i := range 5 {
		key := fmt.Sprintf("bucket/key-%d", i)
		cache.Put(ctx, key, []byte("test data"), "", "")
	}

	// Get some entries (hits)
	for i := range 3 {
		key := fmt.Sprintf("bucket/key-%d", i)
		cache.Get(ctx, key)
	}

	// Get non-existent entries (misses)
	for i := 10; i < 13; i++ {
		key := fmt.Sprintf("bucket/key-%d", i)
		cache.Get(ctx, key)
	}

	metrics := cache.Metrics()

	if metrics.Objects != 5 {
		t.Errorf("expected 5 objects, got %d", metrics.Objects)
	}

	if metrics.Hits != 3 {
		t.Errorf("expected 3 hits, got %d", metrics.Hits)
	}

	if metrics.Misses != 3 {
		t.Errorf("expected 3 misses, got %d", metrics.Misses)
	}

	expectedHitRate := 0.5 // 3 hits / 6 total
	if metrics.HitRate != expectedHitRate {
		t.Errorf("expected hit rate %f, got %f", expectedHitRate, metrics.HitRate)
	}
}

func TestConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 100
	config.ShardCount = 64

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()

	var wg sync.WaitGroup

	numGoroutines := 100
	opsPerGoroutine := 100

	// Concurrent puts
	for i := range numGoroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := range opsPerGoroutine {
				key := fmt.Sprintf("bucket/key-%d-%d", id, j)
				cache.Put(ctx, key, []byte("test data"), "", "")
			}
		}(i)
	}

	wg.Wait()

	// Concurrent gets
	for i := range numGoroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := range opsPerGoroutine {
				key := fmt.Sprintf("bucket/key-%d-%d", id, j)
				cache.Get(ctx, key)
			}
		}(i)
	}

	wg.Wait()

	// Should complete without race conditions
	metrics := cache.Metrics()
	t.Logf("Cache metrics after concurrent access: %+v", metrics)
}

func TestClear(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()

	// Put some entries
	for i := range 10 {
		key := fmt.Sprintf("bucket/key-%d", i)
		cache.Put(ctx, key, []byte("test data"), "", "")
	}

	// Clear cache
	cache.Clear()

	// Verify all entries removed
	metrics := cache.Metrics()
	if metrics.Objects != 0 {
		t.Errorf("expected 0 objects after clear, got %d", metrics.Objects)
	}

	if metrics.Size != 0 {
		t.Errorf("expected 0 size after clear, got %d", metrics.Size)
	}
}

func TestGetReader(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	key := "test-bucket/test-key"
	data := []byte("test data for streaming")

	cache.Put(ctx, key, data, "text/plain", "abc123")

	reader, entry, ok := cache.GetReader(ctx, key)
	if !ok {
		t.Fatal("expected entry to be found")
	}

	if entry.ContentType != "text/plain" {
		t.Errorf("expected content type text/plain, got %s", entry.ContentType)
	}

	// Read data from reader
	buf := make([]byte, len(data))

	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if n != len(data) {
		t.Errorf("expected to read %d bytes, got %d", len(data), n)
	}

	if string(buf) != string(data) {
		t.Errorf("expected data %q, got %q", data, buf)
	}
}

func TestWarmCache(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10
	config.WarmupEnabled = true

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()

	// Define warmup source
	source := func(ctx context.Context, key string) ([]byte, string, string, error) {
		return []byte("warmed data for " + key), "text/plain", "etag-" + key, nil
	}

	keys := []string{"bucket/warm-1", "bucket/warm-2", "bucket/warm-3"}

	err := cache.WarmCache(ctx, keys, source)
	if err != nil {
		t.Fatalf("WarmCache failed: %v", err)
	}

	// Verify all keys are cached
	for _, key := range keys {
		if !cache.Has(ctx, key) {
			t.Errorf("expected key %s to be warmed", key)
		}
	}
}

func TestEntryMarshalUnmarshal(t *testing.T) {
	entry := &Entry{
		Key:         "test-bucket/test-key",
		Data:        []byte("test data content"),
		Size:        17,
		ContentType: "text/plain",
		ETag:        "abc123",
		Checksum:    12345,
	}

	// Marshal
	data := entry.Marshal()

	// Unmarshal
	restored, err := UnmarshalEntry(data)
	if err != nil {
		t.Fatalf("UnmarshalEntry failed: %v", err)
	}

	if restored.Key != entry.Key {
		t.Errorf("expected key %s, got %s", entry.Key, restored.Key)
	}

	if string(restored.Data) != string(entry.Data) {
		t.Errorf("expected data %s, got %s", entry.Data, restored.Data)
	}

	if restored.ContentType != entry.ContentType {
		t.Errorf("expected content type %s, got %s", entry.ContentType, restored.ContentType)
	}

	if restored.ETag != entry.ETag {
		t.Errorf("expected etag %s, got %s", entry.ETag, restored.ETag)
	}

	if restored.Checksum != entry.Checksum {
		t.Errorf("expected checksum %d, got %d", entry.Checksum, restored.Checksum)
	}
}

func TestExtractBucketPrefix(t *testing.T) {
	tests := []struct {
		key            string
		expectedBucket string
		expectedPrefix string
	}{
		{"mybucket/myprefix/object.txt", "mybucket", "myprefix"},
		{"mybucket/object.txt", "mybucket", "object.txt"},
		{"mybucket/a/b/c.txt", "mybucket", "a"},
		{"nobucket", "nobucket", ""},
	}

	for _, tt := range tests {
		bucket, prefix := extractBucketPrefix(tt.key)
		if bucket != tt.expectedBucket {
			t.Errorf("extractBucketPrefix(%s): expected bucket %s, got %s", tt.key, tt.expectedBucket, bucket)
		}

		if prefix != tt.expectedPrefix {
			t.Errorf("extractBucketPrefix(%s): expected prefix %s, got %s", tt.key, tt.expectedPrefix, prefix)
		}
	}
}

func TestExtractNumericSuffix(t *testing.T) {
	tests := []struct {
		s        string
		expected int64
	}{
		{"chunk-0", 0},
		{"chunk-1", 1},
		{"chunk-123", 123},
		{"data-part-42", 42},
		{"nodigits", 0},
		{"123", 123},
		{"prefix123suffix", 0}, // No trailing digits
		{"test-99-more", 0},    // No trailing digits
	}

	for _, tt := range tests {
		result := extractNumericSuffix(tt.s)
		if result != tt.expected {
			t.Errorf("extractNumericSuffix(%s): expected %d, got %d", tt.s, tt.expected, result)
		}
	}
}

func TestSharding(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 100
	config.ShardCount = 64

	cache := New(config)
	defer cache.Close()

	// Different keys should potentially map to different shards
	keys := []string{
		"bucket1/key1",
		"bucket2/key2",
		"bucket3/key3",
		"bucket4/key4",
	}

	shardIndices := make(map[int]bool)

	for _, key := range keys {
		shard := cache.getShard(key)
		for i, s := range cache.shards {
			if s == shard {
				shardIndices[i] = true
				break
			}
		}
	}

	// Not all keys will necessarily map to different shards, but we should have some distribution
	t.Logf("Keys mapped to %d unique shards", len(shardIndices))
}

func TestOversizedEntry(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10
	config.EntryMaxSize = 1000 // 1KB max entry

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	key := "bucket/large-object"
	data := make([]byte, 2000) // 2KB, exceeds max entry size

	// Should not error but should not cache
	err := cache.Put(ctx, key, data, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not be cached
	if cache.Has(ctx, key) {
		t.Error("oversized entry should not be cached")
	}
}

func TestAccessCountIncrement(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 10

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	key := "bucket/key"
	cache.Put(ctx, key, []byte("data"), "", "")

	// Access multiple times
	for range 5 {
		cache.Get(ctx, key)
	}

	entry, ok := cache.Get(ctx, key)
	if !ok {
		t.Fatal("expected entry to exist")
	}

	// Access count should be at least 6 (1 from put + 6 gets including this one)
	if entry.AccessCount < 6 {
		t.Errorf("expected access count >= 6, got %d", entry.AccessCount)
	}
}

func BenchmarkPut(b *testing.B) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 1024 // 1GB
	config.ShardCount = 256

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	data := make([]byte, 1024) // 1KB entries

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bucket/key-%d", i)
			cache.Put(ctx, key, data, "", "")

			i++
		}
	})
}

func BenchmarkGet(b *testing.B) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 1024 // 1GB
	config.ShardCount = 256

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	data := make([]byte, 1024) // 1KB entries

	// Pre-populate cache
	for i := range 10000 {
		key := fmt.Sprintf("bucket/key-%d", i)
		cache.Put(ctx, key, data, "", "")
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bucket/key-%d", i%10000)
			cache.Get(ctx, key)

			i++
		}
	})
}

func BenchmarkMixed(b *testing.B) {
	config := DefaultConfig()
	config.MaxSize = 1024 * 1024 * 1024 // 1GB
	config.ShardCount = 256

	cache := New(config)
	defer cache.Close()

	ctx := context.Background()
	data := make([]byte, 1024) // 1KB entries

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bucket/key-%d", i%1000)
			if i%10 == 0 {
				// 10% writes
				cache.Put(ctx, key, data, "", "")
			} else {
				// 90% reads
				cache.Get(ctx, key)
			}

			i++
		}
	})
}
