package volume

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRawDeviceConfig(t *testing.T) {
	// Test default configuration
	cfg := DefaultRawDeviceConfig()
	assert.False(t, cfg.Enabled)
	assert.True(t, cfg.Safety.CheckFilesystem)
	assert.True(t, cfg.Safety.RequireConfirmation)
	assert.True(t, cfg.Safety.WriteSignature)
	assert.True(t, cfg.Safety.ExclusiveLock)
}

func TestStorageTier(t *testing.T) {
	tests := []struct {
		name     string
		tier     StorageTier
		expected string
	}{
		{"hot tier", TierHot, "hot"},
		{"warm tier", TierWarm, "warm"},
		{"cold tier", TierCold, "cold"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.tier))
		})
	}
}

func TestParseTier(t *testing.T) {
	tests := []struct {
		input    string
		expected StorageTier
		valid    bool
	}{
		{"hot", TierHot, true},
		{"warm", TierWarm, true},
		{"cold", TierCold, true},
		{"HOT", TierHot, true},
		{"WARM", TierWarm, true},
		{"COLD", TierCold, true},
		{"invalid", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tier, err := ParseTier(tt.input)
			if tt.valid {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, tier)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestDeviceSignature(t *testing.T) {
	// Test signature creation and validation
	sig := NewDeviceSignature("test-device-001", 1024*1024*1024, TierHot)

	assert.Equal(t, DeviceMagic, sig.Magic)
	assert.Equal(t, DeviceVersion, sig.Version)
	assert.Equal(t, "test-device-001", string(sig.DeviceID[:15]))
	assert.Equal(t, uint64(1024*1024*1024), sig.DeviceSize)
	assert.Equal(t, TierHot, StorageTier(sig.Tier[:3]))
	assert.Positive(t, sig.CreatedAt)

	// Validate signature
	assert.True(t, sig.IsValid())
}

func TestDeviceSignatureSerialize(t *testing.T) {
	sig := NewDeviceSignature("dev-001", 2*1024*1024*1024, TierWarm)

	// Serialize
	data := sig.Serialize()
	assert.Len(t, data, DeviceSignatureSize)

	// Deserialize
	sig2, err := ParseDeviceSignature(data)
	require.NoError(t, err)

	assert.Equal(t, sig.Magic, sig2.Magic)
	assert.Equal(t, sig.Version, sig2.Version)
	assert.Equal(t, sig.DeviceID, sig2.DeviceID)
	assert.Equal(t, sig.DeviceSize, sig2.DeviceSize)
	assert.Equal(t, sig.Tier, sig2.Tier)
	assert.Equal(t, sig.CreatedAt, sig2.CreatedAt)
}

func TestRawDeviceVolumeWithFile(t *testing.T) {
	// Test raw device volume using a regular file (simulates block device)
	dir := t.TempDir()
	devicePath := filepath.Join(dir, "test-device.raw")

	// Create a file to simulate a block device
	deviceSize := int64(64 * 1024 * 1024) // 64MB
	file, err := os.Create(devicePath)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(deviceSize))
	file.Close()

	// Configure raw device (with simulation mode for testing)
	cfg := RawDeviceConfig{
		Enabled: true,
		Safety: RawDeviceSafetyConfig{
			CheckFilesystem:     false, // Disabled for file-based testing
			RequireConfirmation: false,
			WriteSignature:      true,
			ExclusiveLock:       true,
		},
		Devices: []DeviceConfig{
			{
				Path: devicePath,
				Tier: TierHot,
				Size: 0, // Use entire "device"
			},
		},
	}

	// Create raw device volume
	vol, err := NewRawDeviceVolume(devicePath, cfg, TierHot)
	require.NoError(t, err)

	defer vol.Close()

	// Verify signature was written
	sig, err := vol.ReadSignature()
	require.NoError(t, err)
	assert.True(t, sig.IsValid())
	assert.Equal(t, TierHot, StorageTier(sig.Tier[:3]))

	// Test basic I/O operations
	testData := []byte("Hello, Raw Device!")
	bucket := "test-bucket"
	key := "test-key"

	// Write data
	err = vol.Put(bucket, key, testData)
	require.NoError(t, err)

	// Read data back
	result, err := vol.Get(bucket, key)
	require.NoError(t, err)
	assert.Equal(t, testData, result)

	// Check stats
	stats := vol.Stats()
	assert.Equal(t, TierHot, stats.Tier)
	assert.Equal(t, 1, stats.ObjectCount)
}

func TestRawDeviceVolumeBlockAlignment(t *testing.T) {
	dir := t.TempDir()
	devicePath := filepath.Join(dir, "align-test.raw")

	// Create simulated device
	deviceSize := int64(64 * 1024 * 1024)
	file, err := os.Create(devicePath)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(deviceSize))
	file.Close()

	cfg := RawDeviceConfig{
		Enabled: true,
		Safety: RawDeviceSafetyConfig{
			CheckFilesystem:     false,
			RequireConfirmation: false,
			WriteSignature:      true,
			ExclusiveLock:       true,
		},
		Devices: []DeviceConfig{
			{
				Path: devicePath,
				Tier: TierWarm,
				Size: 0,
			},
		},
	}

	vol, err := NewRawDeviceVolume(devicePath, cfg, TierWarm)
	require.NoError(t, err)

	defer vol.Close()

	// Test various data sizes to verify alignment handling
	testCases := []struct {
		name string
		size int
	}{
		{"tiny_100B", 100},
		{"small_1KB", 1024},
		{"medium_64KB", 64 * 1024},
		{"large_1MB", 1024 * 1024},
		{"block_aligned_4KB", 4096},
		{"not_aligned_4097B", 4097},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, tc.size)
			for i := range data {
				data[i] = byte(i % 256)
			}

			key := "test-" + tc.name
			err := vol.Put("bucket", key, data)
			require.NoError(t, err)

			result, err := vol.Get("bucket", key)
			require.NoError(t, err)
			assert.Equal(t, data, result, "Data mismatch for %s", tc.name)
		})
	}
}

func TestTieredDeviceManager(t *testing.T) {
	dir := t.TempDir()

	// Create simulated devices for each tier
	devices := map[StorageTier]string{
		TierHot:  filepath.Join(dir, "hot.raw"),
		TierWarm: filepath.Join(dir, "warm.raw"),
		TierCold: filepath.Join(dir, "cold.raw"),
	}

	for _, path := range devices {
		file, err := os.Create(path)
		require.NoError(t, err)
		require.NoError(t, file.Truncate(64*1024*1024))
		file.Close()
	}

	// Configure tiered device manager
	cfg := TieredDeviceManagerConfig{
		RawDevices: RawDeviceConfig{
			Enabled: true,
			Safety: RawDeviceSafetyConfig{
				CheckFilesystem:     false,
				RequireConfirmation: false,
				WriteSignature:      true,
				ExclusiveLock:       true,
			},
			Devices: []DeviceConfig{
				{Path: devices[TierHot], Tier: TierHot, Size: 0},
				{Path: devices[TierWarm], Tier: TierWarm, Size: 0},
				{Path: devices[TierCold], Tier: TierCold, Size: 0},
			},
		},
	}

	mgr, err := NewTieredDeviceManager(cfg)
	require.NoError(t, err)

	defer mgr.Close()

	// Verify all tiers are available
	assert.True(t, mgr.HasTier(TierHot))
	assert.True(t, mgr.HasTier(TierWarm))
	assert.True(t, mgr.HasTier(TierCold))

	// Write to hot tier
	err = mgr.Put("bucket", "hot-key", []byte("hot data"), TierHot)
	require.NoError(t, err)

	// Write to cold tier
	err = mgr.Put("bucket", "cold-key", []byte("cold data"), TierCold)
	require.NoError(t, err)

	// Verify data location
	tier, err := mgr.GetObjectTier("bucket", "hot-key")
	require.NoError(t, err)
	assert.Equal(t, TierHot, tier)

	tier, err = mgr.GetObjectTier("bucket", "cold-key")
	require.NoError(t, err)
	assert.Equal(t, TierCold, tier)

	// Read from each tier
	hotData, err := mgr.Get("bucket", "hot-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("hot data"), hotData)

	coldData, err := mgr.Get("bucket", "cold-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("cold data"), coldData)
}

func TestTierTransition(t *testing.T) {
	dir := t.TempDir()

	// Create simulated devices
	hotPath := filepath.Join(dir, "hot.raw")
	coldPath := filepath.Join(dir, "cold.raw")

	for _, path := range []string{hotPath, coldPath} {
		file, err := os.Create(path)
		require.NoError(t, err)
		require.NoError(t, file.Truncate(64*1024*1024))
		file.Close()
	}

	cfg := TieredDeviceManagerConfig{
		RawDevices: RawDeviceConfig{
			Enabled: true,
			Safety: RawDeviceSafetyConfig{
				CheckFilesystem:     false,
				RequireConfirmation: false,
				WriteSignature:      true,
				ExclusiveLock:       true,
			},
			Devices: []DeviceConfig{
				{Path: hotPath, Tier: TierHot, Size: 0},
				{Path: coldPath, Tier: TierCold, Size: 0},
			},
		},
	}

	mgr, err := NewTieredDeviceManager(cfg)
	require.NoError(t, err)

	defer mgr.Close()

	// Write to hot tier
	testData := []byte("data to be migrated")
	err = mgr.Put("bucket", "migrate-key", testData, TierHot)
	require.NoError(t, err)

	// Verify initial tier
	tier, err := mgr.GetObjectTier("bucket", "migrate-key")
	require.NoError(t, err)
	assert.Equal(t, TierHot, tier)

	// Migrate to cold tier
	err = mgr.MigrateObject("bucket", "migrate-key", TierCold)
	require.NoError(t, err)

	// Verify new tier
	tier, err = mgr.GetObjectTier("bucket", "migrate-key")
	require.NoError(t, err)
	assert.Equal(t, TierCold, tier)

	// Verify data integrity after migration
	result, err := mgr.Get("bucket", "migrate-key")
	require.NoError(t, err)
	assert.Equal(t, testData, result)
}

func TestDeviceFreeSpaceTracking(t *testing.T) {
	dir := t.TempDir()
	devicePath := filepath.Join(dir, "space-test.raw")

	// Create 64MB simulated device
	deviceSize := int64(64 * 1024 * 1024)
	file, err := os.Create(devicePath)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(deviceSize))
	file.Close()

	cfg := RawDeviceConfig{
		Enabled: true,
		Safety: RawDeviceSafetyConfig{
			CheckFilesystem:     false,
			RequireConfirmation: false,
			WriteSignature:      true,
			ExclusiveLock:       true,
		},
		Devices: []DeviceConfig{
			{Path: devicePath, Tier: TierHot, Size: 0},
		},
	}

	vol, err := NewRawDeviceVolume(devicePath, cfg, TierHot)
	require.NoError(t, err)

	defer vol.Close()

	// Get initial free space
	initialFree := vol.FreeSpace()
	t.Logf("Initial free space: %d bytes", initialFree)

	// Write some data
	data := make([]byte, 1024*1024) // 1MB
	err = vol.Put("bucket", "key1", data)
	require.NoError(t, err)

	// Check free space decreased
	afterWrite := vol.FreeSpace()
	t.Logf("Free space after write: %d bytes", afterWrite)
	assert.Less(t, afterWrite, initialFree)

	// Delete data
	err = vol.Delete("bucket", "key1")
	require.NoError(t, err)

	// Free space should increase (or be marked for reclaim)
	afterDelete := vol.FreeSpace()
	t.Logf("Free space after delete: %d bytes", afterDelete)
	// Note: Space may not be immediately reclaimed depending on implementation
}

func BenchmarkRawDeviceWrite(b *testing.B) {
	dir := b.TempDir()
	devicePath := filepath.Join(dir, "bench.raw")

	file, err := os.Create(devicePath)
	require.NoError(b, err)
	require.NoError(b, file.Truncate(512*1024*1024)) // 512MB
	file.Close()

	cfg := RawDeviceConfig{
		Enabled: true,
		Safety: RawDeviceSafetyConfig{
			CheckFilesystem:     false,
			RequireConfirmation: false,
			WriteSignature:      true,
			ExclusiveLock:       true,
		},
		Devices: []DeviceConfig{
			{Path: devicePath, Tier: TierHot, Size: 0},
		},
	}

	vol, err := NewRawDeviceVolume(devicePath, cfg, TierHot)
	require.NoError(b, err)

	defer vol.Close()

	data := make([]byte, 4096) // 4KB writes
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.SetBytes(4096)

	for i := range b.N {
		key := "bench-key-" + string(rune(i%10000))

		err := vol.Put("bench", key, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRawDeviceRead(b *testing.B) {
	dir := b.TempDir()
	devicePath := filepath.Join(dir, "bench.raw")

	file, err := os.Create(devicePath)
	require.NoError(b, err)
	require.NoError(b, file.Truncate(512*1024*1024))
	file.Close()

	cfg := RawDeviceConfig{
		Enabled: true,
		Safety: RawDeviceSafetyConfig{
			CheckFilesystem:     false,
			RequireConfirmation: false,
			WriteSignature:      true,
			ExclusiveLock:       true,
		},
		Devices: []DeviceConfig{
			{Path: devicePath, Tier: TierHot, Size: 0},
		},
	}

	vol, err := NewRawDeviceVolume(devicePath, cfg, TierHot)
	require.NoError(b, err)

	defer vol.Close()

	// Pre-populate data
	data := make([]byte, 4096)

	for i := range 1000 {
		key := "bench-key-" + string(rune(i))
		err := vol.Put("bench", key, data)
		require.NoError(b, err)
	}

	b.ResetTimer()
	b.SetBytes(4096)

	for i := range b.N {
		key := "bench-key-" + string(rune(i%1000))

		_, err := vol.Get("bench", key)
		if err != nil {
			b.Fatal(err)
		}
	}
}
