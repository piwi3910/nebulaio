package dpu

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.True(t, config.Enable)
	assert.Equal(t, -1, config.DeviceIndex)
	assert.Contains(t, config.Offloads, OffloadStorage)
	assert.Contains(t, config.Offloads, OffloadCrypto)
	assert.Contains(t, config.Offloads, OffloadCompress)
	assert.Contains(t, config.Offloads, OffloadNetwork)
	assert.NotNil(t, config.StorageOffload)
	assert.NotNil(t, config.CryptoOffload)
	assert.NotNil(t, config.CompressOffload)
	assert.NotNil(t, config.NetworkOffload)
	assert.True(t, config.FallbackOnFailure)
}

func TestNewService(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.HealthCheckInterval = 0 // Disable health checks for testing

	service, err := NewService(config, backend)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Close()

	assert.True(t, service.IsAvailable())
}

func TestNewServiceWithNilConfig(t *testing.T) {
	backend := NewSimulatedBackend()

	service, err := NewService(nil, backend)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Close()

	assert.True(t, service.IsAvailable())
}

func TestNewServiceWithNilBackend(t *testing.T) {
	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, nil)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Close()

	assert.True(t, service.IsAvailable())
}

func TestNewServiceNoDPU(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	backend.SetDPUs([]*DPUInfo{}) // No DPUs

	config := DefaultConfig()
	config.HealthCheckInterval = 0

	_, err := NewService(config, backend)
	assert.ErrorIs(t, err, ErrDPUNotAvailable)
}

func TestServiceGetDPUInfo(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	dpu := service.GetDPUInfo()
	require.NotNil(t, dpu)

	assert.Equal(t, 0, dpu.Index)
	assert.Equal(t, "BlueField-3 DPU", dpu.Model)
	assert.Equal(t, 16, dpu.ARMCores)
	assert.Equal(t, 32, dpu.MemoryGB)
	assert.Equal(t, 400, dpu.PortSpeedGbps)
	assert.Contains(t, dpu.Capabilities, OffloadCrypto)
	assert.Contains(t, dpu.Capabilities, OffloadCompress)
}

func TestServiceSelectSpecificDPU(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.DeviceIndex = 1 // Select BlueField-2
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	dpu := service.GetDPUInfo()
	require.NotNil(t, dpu)

	assert.Equal(t, 1, dpu.Index)
	assert.Equal(t, "BlueField-2 DPU", dpu.Model)
}

func TestServiceGetServices(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	services := service.GetServices()
	assert.Greater(t, len(services), 0)

	// Check that offload services are running
	hasStorage := false
	hasCrypto := false
	for _, svc := range services {
		if svc.Type == OffloadStorage {
			hasStorage = true
		}
		if svc.Type == OffloadCrypto {
			hasCrypto = true
		}
	}
	assert.True(t, hasStorage)
	assert.True(t, hasCrypto)
}

func TestServiceIsOffloadAvailable(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	assert.True(t, service.IsOffloadAvailable(OffloadCrypto))
	assert.True(t, service.IsOffloadAvailable(OffloadCompress))
	assert.True(t, service.IsOffloadAvailable(OffloadStorage))
}

func TestServiceEncryptDecrypt(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000) // 1μs for faster tests
	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	ctx := context.Background()
	key := []byte("0123456789abcdef") // 16 bytes
	plaintext := []byte("Hello, BlueField DPU!")

	// Encrypt
	ciphertext, err := service.Encrypt(ctx, "aes-gcm-128", key, plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	// Decrypt
	decrypted, err := service.Decrypt(ctx, "aes-gcm-128", key, ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestServiceCompressDecompress(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000)
	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	ctx := context.Background()

	// Create highly compressible data (more repetition = better compression)
	original := bytes.Repeat([]byte("AAAAAAAAAA"), 1000) // 10KB of repeated 'A's

	// Compress
	compressed, err := service.Compress(ctx, "deflate", original)
	require.NoError(t, err)
	assert.Less(t, len(compressed), len(original), "compressed data should be smaller than original")

	// Decompress
	decompressed, err := service.Decompress(ctx, "deflate", compressed)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

func TestServiceFallbackOnCryptoDisabled(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000)
	config := DefaultConfig()
	config.HealthCheckInterval = 0
	config.CryptoOffload.Enable = false
	config.FallbackOnFailure = true

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	ctx := context.Background()
	key := []byte("0123456789abcdef")
	plaintext := []byte("Test data")

	// Should fall back to software
	result, err := service.Encrypt(ctx, "aes-gcm-128", key, plaintext)
	require.NoError(t, err)
	assert.NotNil(t, result)

	metrics := service.GetMetrics()
	assert.Greater(t, metrics.Fallbacks, int64(0))
}

func TestServiceCompressMinSize(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000)
	config := DefaultConfig()
	config.HealthCheckInterval = 0
	config.CompressOffload.MinSize = 1000 // Require 1KB minimum

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	ctx := context.Background()

	// Small data should trigger fallback
	smallData := []byte("Small data")
	_, err = service.Compress(ctx, "deflate", smallData)
	require.NoError(t, err)

	metrics := service.GetMetrics()
	assert.Greater(t, metrics.Fallbacks, int64(0))
}

func TestServiceMetrics(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000)
	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	ctx := context.Background()
	key := []byte("0123456789abcdef")

	// Perform some operations
	service.Encrypt(ctx, "aes-gcm-128", key, []byte("Test1"))
	service.Encrypt(ctx, "aes-gcm-128", key, []byte("Test2"))
	// Use data larger than MinSize (4096) to trigger actual DPU compression
	service.Compress(ctx, "deflate", bytes.Repeat([]byte("Data"), 2000)) // 8KB

	metrics := service.GetMetrics()
	assert.Equal(t, int64(2), metrics.CryptoOps)
	assert.Equal(t, int64(1), metrics.CompressOps)
	assert.Greater(t, metrics.CryptoBytes, int64(0))
	assert.Greater(t, metrics.CompressBytes, int64(0))
}

func TestServiceGetStatus(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	status := service.GetStatus()

	assert.True(t, status["available"].(bool))
	assert.NotNil(t, status["dpu"])
	assert.NotNil(t, status["services"])
	assert.NotNil(t, status["metrics"])

	dpuInfo := status["dpu"].(map[string]interface{})
	assert.Equal(t, "BlueField-3 DPU", dpuInfo["model"])
}

func TestServiceClose(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)

	err = service.Close()
	assert.NoError(t, err)

	// Operations should fail after close
	ctx := context.Background()
	_, err = service.Encrypt(ctx, "aes-gcm", []byte("key"), []byte("data"))
	assert.ErrorIs(t, err, ErrAlreadyClosed)

	// Double close should return error
	err = service.Close()
	assert.ErrorIs(t, err, ErrAlreadyClosed)
}

func TestServiceHealthMonitor(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.HealthCheckInterval = 10 * time.Millisecond

	service, err := NewService(config, backend)
	require.NoError(t, err)

	// Wait for a health check
	time.Sleep(50 * time.Millisecond)

	metrics := service.GetMetrics()
	assert.Equal(t, int64(0), metrics.HealthFails)

	service.Close()
}

// Simulated Backend Tests

func TestSimulatedBackendInit(t *testing.T) {
	backend := NewSimulatedBackend()

	err := backend.Init()
	assert.NoError(t, err)

	// Double init should be ok
	err = backend.Init()
	assert.NoError(t, err)

	err = backend.Close()
	assert.NoError(t, err)
}

func TestSimulatedBackendDetectDPUs(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	defer backend.Close()

	dpus, err := backend.DetectDPUs()
	require.NoError(t, err)
	assert.Len(t, dpus, 2)

	// BlueField-3
	assert.Equal(t, "BlueField-3 DPU", dpus[0].Model)
	assert.Equal(t, 16, dpus[0].ARMCores)

	// BlueField-2
	assert.Equal(t, "BlueField-2 DPU", dpus[1].Model)
	assert.Equal(t, 8, dpus[1].ARMCores)
}

func TestSimulatedBackendNotInitialized(t *testing.T) {
	backend := NewSimulatedBackend()

	_, err := backend.DetectDPUs()
	assert.ErrorIs(t, err, ErrDPUNotInitialized)

	_, err = backend.SelectDPU(0)
	assert.ErrorIs(t, err, ErrDPUNotInitialized)
}

func TestSimulatedBackendSelectDPU(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	defer backend.Close()

	dpu, err := backend.SelectDPU(0)
	require.NoError(t, err)
	assert.Equal(t, "BlueField-3 DPU", dpu.Model)

	// Out of range
	_, err = backend.SelectDPU(99)
	assert.Error(t, err)
}

func TestSimulatedBackendStartStopService(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	defer backend.Close()

	backend.SelectDPU(0)

	// Start service
	err := backend.StartService(OffloadCrypto, nil)
	assert.NoError(t, err)

	// Start again should fail
	err = backend.StartService(OffloadCrypto, nil)
	assert.ErrorIs(t, err, ErrServiceRunning)

	// Check status
	svc, err := backend.GetServiceStatus(OffloadCrypto)
	require.NoError(t, err)
	assert.Equal(t, OffloadCrypto, svc.Type)
	assert.Equal(t, "running", svc.Status)

	// Stop service
	err = backend.StopService(OffloadCrypto)
	assert.NoError(t, err)

	// Stop again should fail
	err = backend.StopService(OffloadCrypto)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

func TestSimulatedBackendUnsupportedOffload(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	defer backend.Close()

	// Set DPU with limited capabilities
	backend.SetDPUs([]*DPUInfo{
		{
			Index:        0,
			Model:        "Limited DPU",
			Capabilities: []OffloadType{OffloadNetwork}, // Only network
		},
	})
	backend.SelectDPU(0)

	err := backend.StartService(OffloadCrypto, nil)
	assert.ErrorIs(t, err, ErrOffloadNotSupported)
}

func TestSimulatedBackendEncryptDecrypt(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	backend.SetSimulatedLatency(1000)
	defer backend.Close()

	backend.SelectDPU(0)
	backend.StartService(OffloadCrypto, nil)

	ctx := context.Background()
	key := []byte("test-key-123456")
	plaintext := []byte("Hello, DPU!")

	// Encrypt
	ciphertext, err := backend.Encrypt(ctx, "aes-gcm-128", key, plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	// Decrypt
	decrypted, err := backend.Decrypt(ctx, "aes-gcm-128", key, ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestSimulatedBackendCompressDecompress(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	backend.SetSimulatedLatency(1000)
	defer backend.Close()

	backend.SelectDPU(0)
	backend.StartService(OffloadCompress, nil)

	ctx := context.Background()
	original := bytes.Repeat([]byte("Compressible data! "), 50)

	// Compress
	compressed, err := backend.Compress(ctx, "deflate", original)
	require.NoError(t, err)
	assert.Less(t, len(compressed), len(original))

	// Decompress
	decompressed, err := backend.Decompress(ctx, "deflate", compressed)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

func TestSimulatedBackendHealthCheck(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	defer backend.Close()

	// No DPU selected
	err := backend.HealthCheck()
	assert.ErrorIs(t, err, ErrDPUNotAvailable)

	// Select DPU
	backend.SelectDPU(0)
	err = backend.HealthCheck()
	assert.NoError(t, err)
}

func TestSimulatedBackendGetMetrics(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	backend.SetSimulatedLatency(5000)
	defer backend.Close()

	backend.SelectDPU(0)
	backend.StartService(OffloadCrypto, nil)

	metrics := backend.GetMetrics()
	assert.True(t, metrics["simulated"].(bool))
	assert.Equal(t, 2, metrics["dpu_count"])
	assert.True(t, metrics["selected_dpu"].(bool))
	assert.Equal(t, int64(5000), metrics["latency_ns"])
}

// Edge Cases

func TestServiceWithCustomDPUs(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.Init()
	backend.SetDPUs([]*DPUInfo{
		{
			Index:        0,
			Model:        "Custom BlueField",
			ARMCores:     32,
			MemoryGB:     64,
			Capabilities: []OffloadType{OffloadCrypto, OffloadCompress},
		},
	})

	config := DefaultConfig()
	config.HealthCheckInterval = 0

	service, err := NewService(config, backend)
	require.NoError(t, err)
	defer service.Close()

	dpu := service.GetDPUInfo()
	assert.Equal(t, "Custom BlueField", dpu.Model)
	assert.Equal(t, 32, dpu.ARMCores)
}

func TestServiceInvalidDPUIndex(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.DeviceIndex = 99 // Invalid index
	config.HealthCheckInterval = 0

	_, err := NewService(config, backend)
	assert.Error(t, err)
}

func TestOffloadTypes(t *testing.T) {
	tests := []struct {
		offload OffloadType
		name    string
	}{
		{OffloadStorage, "storage"},
		{OffloadNetwork, "network"},
		{OffloadCrypto, "crypto"},
		{OffloadCompress, "compress"},
		{OffloadRDMA, "rdma"},
		{OffloadRegex, "regex"},
	}

	for _, tt := range tests {
		t.Run(string(tt.offload), func(t *testing.T) {
			assert.Equal(t, tt.name, string(tt.offload))
		})
	}
}
