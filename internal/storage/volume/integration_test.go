package volume

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/cache/dram"
	"github.com/piwi3910/nebulaio/internal/tiering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVolumeBackendWithDRAMCache tests the volume backend with DRAM cache layer.
func TestVolumeBackendWithDRAMCache(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create volume backend
	volumeBackend, err := NewBackend(dir)
	require.NoError(t, err)

	defer volumeBackend.Close()

	err = volumeBackend.Init(ctx)
	require.NoError(t, err)

	// Create DRAM cache
	cacheConfig := dram.Config{
		MaxSize:        100 * 1024 * 1024, // 100MB
		ShardCount:     16,
		EntryMaxSize:   10 * 1024 * 1024, // 10MB
		TTL:            time.Hour,
		EvictionPolicy: "lru",
	}

	cache := dram.New(cacheConfig)
	defer cache.Close()

	// Store object in volume backend
	bucket := "test-bucket"
	key := "test-key"
	data := []byte("Hello, Volume Backend with DRAM Cache!")

	err = volumeBackend.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	result, err := volumeBackend.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.NotEmpty(t, result.ETag)

	// Cache the object
	cacheKey := bucket + "/" + key
	err = cache.Put(ctx, cacheKey, data, "text/plain", result.ETag)
	require.NoError(t, err)

	// Read from cache (should be a hit)
	entry, ok := cache.Get(ctx, cacheKey)
	require.True(t, ok)
	assert.Equal(t, data, entry.Data)

	// Verify cache metrics
	metrics := cache.Metrics()
	assert.Equal(t, int64(1), metrics.Hits)
	assert.Equal(t, int64(0), metrics.Misses)

	// Read from volume backend directly
	reader, err := volumeBackend.GetObject(ctx, bucket, key)
	require.NoError(t, err)

	defer reader.Close()

	var buf bytes.Buffer

	_, err = buf.ReadFrom(reader)
	require.NoError(t, err)
	assert.Equal(t, data, buf.Bytes())
}

// TestVolumeBackendWithTieringService tests the volume backend with tiering service.
func TestVolumeBackendWithTieringService(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create volume backend as primary storage
	volumeBackend, err := NewBackend(dir)
	require.NoError(t, err)

	defer volumeBackend.Close()

	err = volumeBackend.Init(ctx)
	require.NoError(t, err)

	// Create tiering service config
	tieringConfig := tiering.ServiceConfig{
		Enabled: true,
		Cache: tiering.CacheConfig{
			MaxSize:        50 * 1024 * 1024, // 50MB
			MaxObjects:     1000,
			TTL:            time.Hour,
			EvictionPolicy: "lru",
			WriteThrough:   true,
		},
		Policies:            tiering.DefaultPolicies(),
		ScanInterval:        time.Hour,
		TransitionBatchSize: 10,
		TransitionWorkers:   2,
	}

	// Create tiering service with volume backend as primary
	tieringService := tiering.NewService(tieringConfig, volumeBackend, nil)
	err = tieringService.Start()
	require.NoError(t, err)

	defer tieringService.Stop()

	bucket := "tiered-bucket"
	key := "tiered-key"
	data := []byte("Hello, Tiered Storage with Volume Backend!")

	// Create bucket in volume backend
	err = volumeBackend.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Store through tiering service
	result, err := tieringService.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.NotEmpty(t, result.ETag)

	// Check object exists
	exists, err := tieringService.ObjectExists(ctx, bucket, key)
	require.NoError(t, err)
	assert.True(t, exists)

	// Read through tiering service
	reader, err := tieringService.GetObject(ctx, bucket, key)
	require.NoError(t, err)

	defer reader.Close()

	var buf bytes.Buffer

	_, err = buf.ReadFrom(reader)
	require.NoError(t, err)
	assert.Equal(t, data, buf.Bytes())

	// Check tiering stats
	stats := tieringService.Stats()
	assert.NotNil(t, stats.CacheStats)

	// Delete through tiering service
	err = tieringService.DeleteObject(ctx, bucket, key)
	require.NoError(t, err)

	// Verify deletion
	exists, err = tieringService.ObjectExists(ctx, bucket, key)
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestVolumeBackendCacheIntegration tests cache behavior with volume backend.
func TestVolumeBackendCacheIntegration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create volume backend
	volumeBackend, err := NewBackend(dir)
	require.NoError(t, err)

	defer volumeBackend.Close()

	err = volumeBackend.Init(ctx)
	require.NoError(t, err)

	// Create tiering cache
	cacheConfig := tiering.CacheConfig{
		MaxSize:        10 * 1024 * 1024, // 10MB
		MaxObjects:     100,
		TTL:            time.Second * 10, // Longer TTL to avoid race conditions in CI
		EvictionPolicy: "lru",
	}

	cache := tiering.NewCache(cacheConfig, nil)
	defer cache.Close()

	// Test multiple objects
	bucket := "cache-test"
	err = volumeBackend.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Store multiple objects
	for i := range 10 {
		key := bytes.Buffer{}
		key.WriteString("object-")
		key.WriteByte('0' + byte(i))
		keyStr := key.String()

		data := []byte("Data for " + keyStr)

		// Store in volume backend
		_, err := volumeBackend.PutObject(ctx, bucket, keyStr, bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		// Cache it
		cacheKey := bucket + "/" + keyStr
		err = cache.Put(ctx, cacheKey, data, "text/plain", "")
		require.NoError(t, err)
	}

	// Verify all are cached
	for i := range 10 {
		key := bytes.Buffer{}
		key.WriteString("object-")
		key.WriteByte('0' + byte(i))
		cacheKey := bucket + "/" + key.String()

		assert.True(t, cache.Has(ctx, cacheKey))
	}

	// Check cache stats
	stats := cache.Stats()
	assert.Equal(t, 10, stats.Objects)
	assert.Positive(t, stats.Size)

	// Verify volume backend still has the data (independent of cache)
	for i := range 10 {
		key := bytes.Buffer{}
		key.WriteString("object-")
		key.WriteByte('0' + byte(i))
		keyStr := key.String()

		reader, err := volumeBackend.GetObject(ctx, bucket, keyStr)
		require.NoError(t, err)
		reader.Close()
	}
}

// TestVolumeBackendLargeObjectsWithCache tests large objects with caching.
func TestVolumeBackendLargeObjectsWithCache(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create volume backend
	volumeBackend, err := NewBackend(dir)
	require.NoError(t, err)

	defer volumeBackend.Close()

	err = volumeBackend.Init(ctx)
	require.NoError(t, err)

	// Create DRAM cache with smaller entry max size
	cacheConfig := dram.Config{
		MaxSize:        100 * 1024 * 1024, // 100MB total
		ShardCount:     16,
		EntryMaxSize:   1 * 1024 * 1024, // 1MB max entry
		TTL:            time.Hour,
		EvictionPolicy: "lru",
	}

	cache := dram.New(cacheConfig)
	defer cache.Close()

	bucket := "large-objects"
	err = volumeBackend.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Test object smaller than cache entry max
	t.Run("SmallObject", func(t *testing.T) {
		data := make([]byte, 512*1024) // 512KB
		for i := range data {
			data[i] = byte(i % 256)
		}

		_, err := volumeBackend.PutObject(ctx, bucket, "small", bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		// Should be cacheable
		err = cache.Put(ctx, bucket+"/small", data, "application/octet-stream", "")
		require.NoError(t, err)

		entry, ok := cache.Get(ctx, bucket+"/small")
		require.True(t, ok)
		assert.Equal(t, data, entry.Data)
	})

	// Test object larger than cache entry max (should not cache)
	t.Run("LargeObject", func(t *testing.T) {
		data := make([]byte, 2*1024*1024) // 2MB - larger than 1MB max
		for i := range data {
			data[i] = byte(i % 256)
		}

		_, err := volumeBackend.PutObject(ctx, bucket, "large", bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		// Put should silently skip (not error) for oversized entries
		err = cache.Put(ctx, bucket+"/large", data, "application/octet-stream", "")
		require.NoError(t, err)

		// Should not be in cache
		_, ok := cache.Get(ctx, bucket+"/large")
		assert.False(t, ok)

		// But should still be in volume backend
		reader, err := volumeBackend.GetObject(ctx, bucket, "large")
		require.NoError(t, err)

		defer reader.Close()

		var buf bytes.Buffer

		_, err = buf.ReadFrom(reader)
		require.NoError(t, err)
		assert.Equal(t, data, buf.Bytes())
	})
}

// TestVolumeBackendStorageInfo tests that GetStorageInfo works correctly with tiering.
func TestVolumeBackendStorageInfo(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create volume backend
	volumeBackend, err := NewBackend(dir)
	require.NoError(t, err)

	defer volumeBackend.Close()

	err = volumeBackend.Init(ctx)
	require.NoError(t, err)

	// Get initial storage info
	info1, err := volumeBackend.GetStorageInfo(ctx)
	require.NoError(t, err)
	assert.Positive(t, info1.TotalBytes)
	assert.Equal(t, int64(0), info1.ObjectCount)

	// Add some objects
	bucket := "storage-info-test"
	err = volumeBackend.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	for i := range 5 {
		data := make([]byte, 10*1024) // 10KB each
		key := bytes.Buffer{}
		key.WriteString("obj-")
		key.WriteByte('0' + byte(i))

		_, err := volumeBackend.PutObject(ctx, bucket, key.String(), bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
	}

	// Get storage info again
	info2, err := volumeBackend.GetStorageInfo(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), info2.ObjectCount)
	assert.Positive(t, info2.UsedBytes)
}
