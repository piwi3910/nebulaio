package gpudirect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.True(t, config.Enable)
	assert.Equal(t, 0, config.MaxGPUs)
	assert.Equal(t, int64(1<<30), config.BufferPoolSize)
	assert.Equal(t, int64(128<<20), config.MaxBufferSize)
	assert.Equal(t, int64(4<<10), config.MinTransferSize)
	assert.True(t, config.EnableBatchOperations)
	assert.True(t, config.EnableAsyncTransfers)
	assert.True(t, config.FallbackToStandardIO)
}

func TestNewService(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20 // 1MB for faster tests

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

	// Should use default config
	assert.True(t, service.IsAvailable())
}

func TestNewServiceWithNilBackend(t *testing.T) {
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, nil)
	require.NoError(t, err)

	require.NotNil(t, service)
	defer service.Close()

	// Should use simulated backend
	assert.True(t, service.IsAvailable())
}

func TestServiceDetectGPUs(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	gpus := service.GetGPUs()
	require.Len(t, gpus, 2)

	gpu0 := gpus[0]
	assert.Equal(t, 0, gpu0.DeviceID)
	assert.Contains(t, gpu0.Name, "Simulated")
	assert.True(t, gpu0.GDSSupported)
	assert.True(t, gpu0.P2PSupported)
	assert.Equal(t, int64(40<<30), gpu0.TotalMemory)
}

func TestServiceMaxGPUs(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxGPUs = 1
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	gpus := service.GetGPUs()
	assert.Len(t, gpus, 1)
}

func TestServiceGetBuffer(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	// Get a buffer
	buf, err := service.GetBuffer(ctx, 0, 512<<10) // 512KB
	require.NoError(t, err)
	require.NotNil(t, buf)

	assert.Equal(t, 0, buf.DeviceID)
	assert.True(t, buf.InUse)
	assert.Positive(t, buf.Size)

	// Release the buffer
	service.ReleaseBuffer(buf)
	assert.False(t, buf.InUse)
}

func TestServiceGetBufferTooLarge(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20 // 1MB max

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	// Try to get a buffer larger than max
	_, err = service.GetBuffer(ctx, 0, 2<<20) // 2MB
	assert.Error(t, err)
}

func TestServiceGetBufferInvalidGPU(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	// Try to get a buffer from non-existent GPU
	_, err = service.GetBuffer(ctx, 99, 512<<10)
	assert.Error(t, err)
}

func TestServiceRegisterFile(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	// Register a file
	handle, err := service.RegisterFile("/path/to/file")
	require.NoError(t, err)
	assert.NotZero(t, handle)

	// Unregister the file
	err = service.UnregisterFile(handle)
	assert.NoError(t, err)
}

func TestServiceRead(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000) // 1μs for faster tests

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	// Register file and get buffer
	handle, err := service.RegisterFile("/test/file")
	require.NoError(t, err)

	buf, err := service.GetBuffer(ctx, 0, 64<<10)
	require.NoError(t, err)

	defer service.ReleaseBuffer(buf)

	// Perform read
	result, err := service.Read(ctx, handle, buf, 0, 64<<10)
	require.NoError(t, err)
	assert.Equal(t, int64(64<<10), result.BytesTransferred)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestServiceWrite(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	// Register file and get buffer
	handle, err := service.RegisterFile("/test/file")
	require.NoError(t, err)

	buf, err := service.GetBuffer(ctx, 0, 64<<10)
	require.NoError(t, err)

	defer service.ReleaseBuffer(buf)

	// Perform write
	result, err := service.Write(ctx, handle, buf, 0, 64<<10)
	require.NoError(t, err)
	assert.Equal(t, int64(64<<10), result.BytesTransferred)
}

// asyncTransferOperation is a function type for async read/write operations.
type asyncTransferOperation func(handle uintptr, buf *GPUBuffer, offset, length int64, callback func(error)) error

// testAsyncTransfer is a helper for testing async read/write operations.
func testAsyncTransfer(t *testing.T, asyncOp func(*Service) asyncTransferOperation) {
	t.Helper()

	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20
	config.EnableAsyncTransfers = true

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	handle, err := service.RegisterFile("/test/file")
	require.NoError(t, err)

	buf, err := service.GetBuffer(ctx, 0, 64<<10)
	require.NoError(t, err)

	defer service.ReleaseBuffer(buf)

	var (
		wg    sync.WaitGroup
		opErr error
	)

	wg.Add(1)

	op := asyncOp(service)

	err = op(handle, buf, 0, 64<<10, func(err error) {
		opErr = err

		wg.Done()
	})
	require.NoError(t, err)

	wg.Wait()
	assert.NoError(t, opErr)
}

func TestServiceAsyncRead(t *testing.T) {
	testAsyncTransfer(t, func(s *Service) asyncTransferOperation {
		return s.ReadAsync
	})
}

func TestServiceAsyncWrite(t *testing.T) {
	testAsyncTransfer(t, func(s *Service) asyncTransferOperation {
		return s.WriteAsync
	})
}

func TestServiceMetrics(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	// Perform some operations
	handle, _ := service.RegisterFile("/test/file")

	buf, _ := service.GetBuffer(ctx, 0, 64<<10)
	defer service.ReleaseBuffer(buf)

	service.Read(ctx, handle, buf, 0, 64<<10)
	service.Write(ctx, handle, buf, 0, 64<<10)
	service.Read(ctx, handle, buf, 0, 64<<10)

	// Check metrics
	metrics := service.GetMetrics()
	assert.Equal(t, int64(2), metrics.ReadOps)
	assert.Equal(t, int64(1), metrics.WriteOps)
	assert.Equal(t, int64(128<<10), metrics.ReadBytes)
	assert.Equal(t, int64(64<<10), metrics.WriteBytes)
}

func TestServiceClose(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	// Close service
	err = service.Close()
	require.NoError(t, err)

	// Operations should fail after close
	ctx := context.Background()
	_, err = service.GetBuffer(ctx, 0, 64<<10)
	require.ErrorIs(t, err, ErrAlreadyClosed)

	_, err = service.RegisterFile("/test")
	require.ErrorIs(t, err, ErrAlreadyClosed)

	// Double close should return error
	err = service.Close()
	require.ErrorIs(t, err, ErrAlreadyClosed)
}

func TestServiceFallbackToStandardIO(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20
	config.MinTransferSize = 64 << 10 // 64KB minimum
	config.FallbackToStandardIO = true

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	handle, _ := service.RegisterFile("/test/file")

	buf, _ := service.GetBuffer(ctx, 0, 32<<10)
	defer service.ReleaseBuffer(buf)

	// Small transfer should use fallback
	result, err := service.Read(ctx, handle, buf, 0, 1<<10) // 1KB
	require.NoError(t, err)
	assert.Equal(t, int64(1<<10), result.BytesTransferred)

	// Check fallback metric
	metrics := service.GetMetrics()
	assert.Positive(t, metrics.FallbackOps)
}

// S3 Integration Tests

func TestS3Integration(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	s3 := NewS3Integration(service)

	require.NotNil(t, s3)
	defer s3.Close()
}

func TestS3IntegrationGetStatus(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	s3 := NewS3Integration(service)
	defer s3.Close()

	status := s3.GetStatus()
	assert.True(t, status["available"].(bool))
	assert.Len(t, status["gpus"], 2)
}

func TestS3IntegrationGetObject(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	s3 := NewS3Integration(service)
	defer s3.Close()

	ctx := context.Background()
	req := &GPUDirectRequest{
		Enabled:  true,
		DeviceID: 0,
	}

	resp, err := s3.GetObject(ctx, "bucket", "key", req, nil)
	require.NoError(t, err)
	assert.True(t, resp.Used)
	assert.Equal(t, 0, resp.DeviceID)
	assert.Positive(t, resp.BytesTransferred)
}

func TestS3IntegrationPutObject(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	s3 := NewS3Integration(service)
	defer s3.Close()

	ctx := context.Background()
	req := &GPUDirectRequest{
		Enabled:  true,
		DeviceID: 0,
	}

	resp, err := s3.PutObject(ctx, "bucket", "key", req, nil, 64<<10)
	require.NoError(t, err)
	assert.True(t, resp.Used)
}

func TestS3IntegrationDisabled(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	s3 := NewS3Integration(service)
	defer s3.Close()

	ctx := context.Background()
	req := &GPUDirectRequest{
		Enabled: false, // GPUDirect disabled
	}

	resp, err := s3.GetObject(ctx, "bucket", "key", req, nil)
	require.NoError(t, err)
	assert.False(t, resp.Used)
}

func TestS3IntegrationInvalidDevice(t *testing.T) {
	backend := NewSimulatedBackend()
	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	s3 := NewS3Integration(service)
	defer s3.Close()

	ctx := context.Background()
	req := &GPUDirectRequest{
		Enabled:  true,
		DeviceID: 99, // Non-existent GPU
	}

	resp, err := s3.GetObject(ctx, "bucket", "key", req, nil)
	require.NoError(t, err)
	assert.False(t, resp.Used)
	assert.Error(t, resp.Error)
}

func TestParseGPUDirectRequest(t *testing.T) {
	tests := []struct {
		headers  map[string]string
		expected *GPUDirectRequest
		name     string
	}{
		{
			name:    "no headers",
			headers: map[string]string{},
			expected: &GPUDirectRequest{
				Enabled:  false,
				DeviceID: 0,
				Async:    false,
			},
		},
		{
			name: "enabled true",
			headers: map[string]string{
				HeaderGPUDirectEnabled: "true",
			},
			expected: &GPUDirectRequest{
				Enabled:  true,
				DeviceID: 0,
				Async:    false,
			},
		},
		{
			name: "enabled 1",
			headers: map[string]string{
				HeaderGPUDirectEnabled: "1",
			},
			expected: &GPUDirectRequest{
				Enabled:  true,
				DeviceID: 0,
				Async:    false,
			},
		},
		{
			name: "with device id",
			headers: map[string]string{
				HeaderGPUDirectEnabled: "true",
				HeaderGPUDeviceID:      "1",
			},
			expected: &GPUDirectRequest{
				Enabled:  true,
				DeviceID: 1,
				Async:    false,
			},
		},
		{
			name: "with buffer size",
			headers: map[string]string{
				HeaderGPUDirectEnabled: "true",
				HeaderGPUBufferSize:    "1048576",
			},
			expected: &GPUDirectRequest{
				Enabled:    true,
				DeviceID:   0,
				BufferSize: 1048576,
				Async:      false,
			},
		},
		{
			name: "with async",
			headers: map[string]string{
				HeaderGPUDirectEnabled: "true",
				HeaderGPUAsync:         "true",
			},
			expected: &GPUDirectRequest{
				Enabled:  true,
				DeviceID: 0,
				Async:    true,
			},
		},
		{
			name: "all options",
			headers: map[string]string{
				HeaderGPUDirectEnabled: "true",
				HeaderGPUDeviceID:      "2",
				HeaderGPUBufferSize:    "2097152",
				HeaderGPUAsync:         "1",
			},
			expected: &GPUDirectRequest{
				Enabled:    true,
				DeviceID:   2,
				BufferSize: 2097152,
				Async:      true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := ParseGPUDirectRequest(req)
			assert.Equal(t, tt.expected.Enabled, result.Enabled)
			assert.Equal(t, tt.expected.DeviceID, result.DeviceID)
			assert.Equal(t, tt.expected.BufferSize, result.BufferSize)
			assert.Equal(t, tt.expected.Async, result.Async)
		})
	}
}

func TestGPUDirectResponseHeaders(t *testing.T) {
	w := httptest.NewRecorder()

	resp := &GPUDirectResponse{
		Used:           true,
		DeviceID:       1,
		TransferTimeMs: 0.123,
	}

	resp.SetResponseHeaders(w)

	assert.Equal(t, "true", w.Header().Get(ResponseHeaderGPUDirectUsed))
	assert.Equal(t, "1", w.Header().Get(ResponseHeaderGPUDeviceID))
	assert.Equal(t, "0.123", w.Header().Get(ResponseHeaderGPUTransferTime))
}

func TestGPUDirectResponseHeadersNotUsed(t *testing.T) {
	w := httptest.NewRecorder()

	resp := &GPUDirectResponse{
		Used: false,
	}

	resp.SetResponseHeaders(w)

	assert.Equal(t, "false", w.Header().Get(ResponseHeaderGPUDirectUsed))
	assert.Empty(t, w.Header().Get(ResponseHeaderGPUDeviceID))
}

// Simulated Backend Tests

func TestSimulatedBackendInit(t *testing.T) {
	backend := NewSimulatedBackend()

	err := backend.Init()
	require.NoError(t, err)

	// Double init should be ok
	err = backend.Init()
	require.NoError(t, err)

	err = backend.Close()
	require.NoError(t, err)
}

func TestSimulatedBackendDetectGPUs(t *testing.T) {
	backend := NewSimulatedBackend()

	backend.Init()
	defer backend.Close()

	gpus, err := backend.DetectGPUs()
	require.NoError(t, err)
	assert.Len(t, gpus, 2)
}

func TestSimulatedBackendNotInitialized(t *testing.T) {
	backend := NewSimulatedBackend()

	_, err := backend.DetectGPUs()
	require.ErrorIs(t, err, ErrNotInitialized)

	_, err = backend.AllocateBuffer(0, 1024)
	require.ErrorIs(t, err, ErrNotInitialized)
}

func TestSimulatedBackendAllocateBuffer(t *testing.T) {
	backend := NewSimulatedBackend()

	backend.Init()
	defer backend.Close()

	buf, err := backend.AllocateBuffer(0, 1<<20)
	require.NoError(t, err)
	require.NotNil(t, buf)

	assert.Equal(t, 0, buf.DeviceID)
	assert.Equal(t, int64(1<<20), buf.Size)
	assert.NotZero(t, buf.Pointer)

	err = backend.FreeBuffer(buf)
	assert.NoError(t, err)
}

func TestSimulatedBackendSetLatency(t *testing.T) {
	backend := NewSimulatedBackend()

	backend.Init()
	defer backend.Close()

	backend.SetSimulatedLatency(1000000, 2000000) // 1ms read, 2ms write

	metrics := backend.GetMetrics()
	assert.Equal(t, int64(1000000), metrics["read_latency_ns"])
	assert.Equal(t, int64(2000000), metrics["write_latency_ns"])
}

func TestSimulatedBackendCustomGPUs(t *testing.T) {
	backend := NewSimulatedBackend()

	backend.Init()
	defer backend.Close()

	customGPUs := []*GPUInfo{
		{
			DeviceID:     0,
			Name:         "Custom GPU",
			UUID:         "custom-uuid",
			TotalMemory:  80 << 30,
			FreeMemory:   75 << 30,
			GDSSupported: true,
		},
	}

	backend.SetGPUs(customGPUs)

	gpus, _ := backend.DetectGPUs()
	assert.Len(t, gpus, 1)
	assert.Equal(t, "Custom GPU", gpus[0].Name)
}

// Buffer Pool Tests

func TestBufferPoolReuse(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1, 1)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	// Get and release a buffer
	buf1, err := service.GetBuffer(ctx, 0, 512<<10)
	require.NoError(t, err)

	ptr1 := buf1.Pointer
	service.ReleaseBuffer(buf1)

	// Get another buffer - should reuse
	buf2, err := service.GetBuffer(ctx, 0, 256<<10)
	require.NoError(t, err)

	ptr2 := buf2.Pointer
	service.ReleaseBuffer(buf2)

	// Should be the same buffer (reused)
	assert.Equal(t, ptr1, ptr2)

	// Check metrics
	metrics := service.GetMetrics()
	assert.Positive(t, metrics.BufferHits)
}

// Reader/Writer Tests

func TestStorageReader(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	handle, _ := service.RegisterFile("/test")

	buf, _ := service.GetBuffer(ctx, 0, 64<<10)
	defer service.ReleaseBuffer(buf)

	reader := service.NewStorageReader(handle, buf, 1000)
	require.NotNil(t, reader)

	// Read some data
	p := make([]byte, 500)
	n, err := reader.Read(p)
	require.NoError(t, err)
	assert.Equal(t, 500, n)
}

func TestStorageWriter(t *testing.T) {
	backend := NewSimulatedBackend()
	backend.SetSimulatedLatency(1000, 1000)

	config := DefaultConfig()
	config.MaxBufferSize = 1 << 20

	service, err := NewService(config, backend)
	require.NoError(t, err)

	defer service.Close()

	ctx := context.Background()

	handle, _ := service.RegisterFile("/test")

	buf, _ := service.GetBuffer(ctx, 0, 64<<10)
	defer service.ReleaseBuffer(buf)

	writer := service.NewStorageWriter(handle, buf)
	require.NotNil(t, writer)

	// Write some data
	data := make([]byte, 500)
	n, err := writer.Write(data)
	require.NoError(t, err)
	assert.Equal(t, 500, n)
}
