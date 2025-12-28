package volume

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRawDeviceBackendCreation(t *testing.T) {
	dir := t.TempDir()

	hotPath := filepath.Join(dir, "hot.raw")
	t.Logf("Creating device at %s", hotPath)

	file, err := os.Create(hotPath)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(64*1024*1024))
	file.Close()
	t.Log("Device file created")

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
		DefaultTier:  TierHot,
		TierSelector: DefaultTierSelector,
	}
	t.Log("Config created")

	t.Log("Creating backend...")
	be, err := NewRawDeviceBackend(cfg)
	require.NoError(t, err)
	t.Log("Backend created")
	defer be.Close()

	ctx := context.Background()

	t.Log("Init...")
	err = be.Init(ctx)
	require.NoError(t, err)
	t.Log("Initialized")
}

func TestRawDeviceBackendPutGet(t *testing.T) {
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

	t.Log("Put object...")
	testData := []byte("Hello, World!")
	result, err := be.PutObject(ctx, "bucket", "key", bytes.NewReader(testData), int64(len(testData)))
	require.NoError(t, err)
	t.Logf("Put result: %+v", result)

	t.Log("Get object...")
	t.Log("Calling be.GetObject...")
	reader, err := be.GetObject(ctx, "bucket", "key")
	t.Logf("GetObject returned, reader=%v, err=%v", reader != nil, err)
	require.NoError(t, err)
	t.Log("Reading data from reader...")
	data, err := io.ReadAll(reader)
	t.Logf("ReadAll returned %d bytes, err=%v", len(data), err)
	require.NoError(t, err)
	reader.Close()

	assert.Equal(t, testData, data)
	t.Log("Success!")
}

func TestRawDeviceBackendMultiTier(t *testing.T) {
	dir := t.TempDir()

	// Create devices for each tier
	hotPath := filepath.Join(dir, "hot.raw")
	warmPath := filepath.Join(dir, "warm.raw")

	for i, path := range []string{hotPath, warmPath} {
		t.Logf("Creating device %d at %s", i, path)
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
				{Path: warmPath, Tier: TierWarm, Size: 0},
			},
		},
		DefaultTier: TierHot,
	}

	t.Log("Creating backend with 2 tiers...")
	be, err := NewRawDeviceBackend(cfg)
	require.NoError(t, err)
	defer be.Close()
	t.Log("Backend created")

	assert.True(t, be.HasTier(TierHot))
	assert.True(t, be.HasTier(TierWarm))
	assert.False(t, be.HasTier(TierCold))

	ctx := context.Background()
	require.NoError(t, be.Init(ctx))

	// Put and verify tier
	testData := []byte("Test data")
	_, err = be.PutObject(ctx, "bucket", "key", bytes.NewReader(testData), int64(len(testData)))
	require.NoError(t, err)

	tier, err := be.GetObjectTier("bucket", "key")
	require.NoError(t, err)
	assert.Equal(t, TierHot, tier)

	// Migrate
	err = be.MigrateObject(ctx, "bucket", "key", TierWarm)
	require.NoError(t, err)

	tier, err = be.GetObjectTier("bucket", "key")
	require.NoError(t, err)
	assert.Equal(t, TierWarm, tier)

	// Verify data is still accessible
	reader, err := be.GetObject(ctx, "bucket", "key")
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	assert.Equal(t, testData, data)

	fmt.Println("Multi-tier test passed!")
}
