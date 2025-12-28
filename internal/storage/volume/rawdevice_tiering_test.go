package volume

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRawDeviceBackendWithCachingPattern tests that the raw device backend
// works correctly with the typical caching pattern used by the tiering service.
//
// The tiering service uses the backend.Backend interface and implements
// a write-through cache pattern. This test verifies that the raw device backend
// correctly implements the interface for this use case.
func TestRawDeviceBackendWithCachingPattern(t *testing.T) {
	dir := t.TempDir()
	hotPath := filepath.Join(dir, "hot.raw")

	file, err := os.Create(hotPath)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(64*1024*1024))
	file.Close()

	cfg := RawDeviceBackendConfig{
		RawDevices: RawDeviceConfig{
			Enabled: true,
			Safety: RawDeviceSafetyConfig{
				CheckFilesystem:     false,
				RequireConfirmation: false,
				WriteSignature:      true,
				ExclusiveLock:       false,
			},
			Devices: []DeviceConfig{
				{Path: hotPath, Tier: TierHot, Size: 0},
			},
		},
		DefaultTier: TierHot,
	}

	be, err := NewRawDeviceBackend(cfg)
	require.NoError(t, err)
	defer be.Close()

	ctx := context.Background()
	require.NoError(t, be.Init(ctx))

	// Simulate write-through cache pattern:
	// 1. Put to backend first
	// 2. Read back (as cache would do)
	// 3. Multiple reads (simulating cache hits)
	// 4. Delete (as cache eviction would do)

	testData := []byte("This is test data for caching pattern verification")
	bucket := "cache-test"
	key := "test-object"

	// Step 1: Write to backend
	_, err = be.PutObject(ctx, bucket, key, bytes.NewReader(testData), int64(len(testData)))
	require.NoError(t, err)

	// Step 2: First read (cache miss -> read from backend)
	reader, err := be.GetObject(ctx, bucket, key)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	assert.Equal(t, testData, data)

	// Step 3: Multiple reads (simulating what cache would do for validation)
	for i := 0; i < 5; i++ {
		reader, err = be.GetObject(ctx, bucket, key)
		require.NoError(t, err)
		data, err = io.ReadAll(reader)
		require.NoError(t, err)
		reader.Close()
		assert.Equal(t, testData, data, "Read %d failed", i)
	}

	// Step 4: Delete (cache eviction)
	err = be.DeleteObject(ctx, bucket, key)
	require.NoError(t, err)

	// Verify delete worked
	exists, err := be.ObjectExists(ctx, bucket, key)
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestRawDeviceBackendStorageInfo tests that storage info is correctly reported
// This is used by the tiering service to make decisions about tier transitions
func TestRawDeviceBackendStorageInfo(t *testing.T) {
	dir := t.TempDir()
	hotPath := filepath.Join(dir, "hot.raw")
	warmPath := filepath.Join(dir, "warm.raw")

	for _, path := range []string{hotPath, warmPath} {
		file, err := os.Create(path)
		require.NoError(t, err)
		require.NoError(t, file.Truncate(64*1024*1024)) // 64MB each
		file.Close()
	}

	cfg := RawDeviceBackendConfig{
		RawDevices: RawDeviceConfig{
			Enabled: true,
			Safety: RawDeviceSafetyConfig{
				CheckFilesystem:     false,
				RequireConfirmation: false,
				WriteSignature:      true,
				ExclusiveLock:       false,
			},
			Devices: []DeviceConfig{
				{Path: hotPath, Tier: TierHot, Size: 0},
				{Path: warmPath, Tier: TierWarm, Size: 0},
			},
		},
		DefaultTier: TierHot,
	}

	be, err := NewRawDeviceBackend(cfg)
	require.NoError(t, err)
	defer be.Close()

	ctx := context.Background()
	require.NoError(t, be.Init(ctx))

	// Get initial storage info
	info, err := be.GetStorageInfo(ctx)
	require.NoError(t, err)

	// We have 2 x 64MB = 128MB total
	assert.Greater(t, info.TotalBytes, int64(100*1024*1024))
	assert.Equal(t, int64(0), info.ObjectCount)
	assert.Greater(t, info.AvailableBytes, int64(0))

	// Add some objects
	data := make([]byte, 1024*1024) // 1MB
	for i := 0; i < 5; i++ {
		key := "test-object-" + string(rune('0'+i))
		_, err = be.PutObject(ctx, "bucket", key, bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
	}

	// Check storage info updated
	info2, err := be.GetStorageInfo(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), info2.ObjectCount)
	assert.Greater(t, info2.UsedBytes, int64(0))
	assert.Less(t, info2.AvailableBytes, info.AvailableBytes)
}

// TestRawDeviceBackendTierAwareness tests the tier-aware capabilities
// which the tiering service uses to manage data placement
func TestRawDeviceBackendTierAwareness(t *testing.T) {
	dir := t.TempDir()
	hotPath := filepath.Join(dir, "hot.raw")
	coldPath := filepath.Join(dir, "cold.raw")

	for _, path := range []string{hotPath, coldPath} {
		file, err := os.Create(path)
		require.NoError(t, err)
		require.NoError(t, file.Truncate(64*1024*1024))
		file.Close()
	}

	cfg := RawDeviceBackendConfig{
		RawDevices: RawDeviceConfig{
			Enabled: true,
			Safety: RawDeviceSafetyConfig{
				CheckFilesystem:     false,
				RequireConfirmation: false,
				WriteSignature:      true,
				ExclusiveLock:       false,
			},
			Devices: []DeviceConfig{
				{Path: hotPath, Tier: TierHot, Size: 0},
				{Path: coldPath, Tier: TierCold, Size: 0},
			},
		},
		DefaultTier: TierHot,
	}

	be, err := NewRawDeviceBackend(cfg)
	require.NoError(t, err)
	defer be.Close()

	ctx := context.Background()
	require.NoError(t, be.Init(ctx))

	// Verify tier availability
	assert.True(t, be.HasTier(TierHot))
	assert.False(t, be.HasTier(TierWarm)) // Not configured
	assert.True(t, be.HasTier(TierCold))

	// Write object (goes to default hot tier)
	testData := []byte("Tier aware test data")
	_, err = be.PutObject(ctx, "bucket", "hot-object", bytes.NewReader(testData), int64(len(testData)))
	require.NoError(t, err)

	// Verify object is in hot tier
	tier, err := be.GetObjectTier("bucket", "hot-object")
	require.NoError(t, err)
	assert.Equal(t, TierHot, tier)

	// Get tier-specific stats
	hotStats, err := be.GetTierStats(TierHot)
	require.NoError(t, err)
	assert.Equal(t, 1, hotStats.ObjectCount)

	coldStats, err := be.GetTierStats(TierCold)
	require.NoError(t, err)
	assert.Equal(t, 0, coldStats.ObjectCount)

	// Migrate to cold tier
	err = be.MigrateObject(ctx, "bucket", "hot-object", TierCold)
	require.NoError(t, err)

	// Verify migration
	tier, err = be.GetObjectTier("bucket", "hot-object")
	require.NoError(t, err)
	assert.Equal(t, TierCold, tier)

	// Verify data integrity after migration
	reader, err := be.GetObject(ctx, "bucket", "hot-object")
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	assert.Equal(t, testData, data)
}
