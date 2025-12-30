// Package gpudirect provides a simulated GPUDirect backend for testing.
package gpudirect

import (
	"sync"
	"sync/atomic"
	"time"
)

// Simulated backend configuration constants.
const (
	// Simulated read latency in nanoseconds (100μs).
	simReadLatencyNs = 100000
	// Simulated write latency in nanoseconds (150μs).
	simWriteLatencyNs = 150000
	// Simulated GPU total memory: 40GB.
	simGPUTotalMemory = 40 << 30
	// Simulated GPU free memory: 38GB.
	simGPUFreeMemory = 38 << 30
	// Stream sync latency in microseconds.
	simSyncLatencyUs = 10
)

// SimulatedBackend provides a simulated GPUDirect Storage backend for testing.
// It simulates the behavior of cuFile API without requiring actual NVIDIA hardware.
type SimulatedBackend struct {
	files        map[uintptr]string
	buffers      map[uintptr]*GPUBuffer
	metrics      *simulatedMetrics
	gpus         []*GPUInfo
	nextHandle   uintptr
	nextBufferID uintptr
	mu           sync.RWMutex
	initialized  bool
}

type simulatedMetrics struct {
	ReadLatencyNs  int64
	WriteLatencyNs int64
}

// NewSimulatedBackend creates a new simulated GDS backend.
func NewSimulatedBackend() *SimulatedBackend {
	return &SimulatedBackend{
		files:   make(map[uintptr]string),
		buffers: make(map[uintptr]*GPUBuffer),
		metrics: &simulatedMetrics{
			ReadLatencyNs:  simReadLatencyNs,
			WriteLatencyNs: simWriteLatencyNs,
		},
	}
}

// Init initializes the simulated backend.
func (b *SimulatedBackend) Init() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.initialized {
		return nil
	}

	// Create simulated GPUs
	b.gpus = []*GPUInfo{
		{
			DeviceID:          0,
			Name:              "Simulated NVIDIA GPU 0",
			UUID:              "GPU-00000000-0000-0000-0000-000000000000",
			TotalMemory:       simGPUTotalMemory,
			FreeMemory:        simGPUFreeMemory,
			ComputeCapability: "8.0",
			PCIBusID:          "0000:00:00.0",
			GDSSupported:      true,
			P2PSupported:      true,
		},
		{
			DeviceID:          1,
			Name:              "Simulated NVIDIA GPU 1",
			UUID:              "GPU-11111111-1111-1111-1111-111111111111",
			TotalMemory:       simGPUTotalMemory,
			FreeMemory:        simGPUFreeMemory,
			ComputeCapability: "8.0",
			PCIBusID:          "0000:00:01.0",
			GDSSupported:      true,
			P2PSupported:      true,
		},
	}

	b.initialized = true

	return nil
}

// Close shuts down the simulated backend.
func (b *SimulatedBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.files = make(map[uintptr]string)
	b.buffers = make(map[uintptr]*GPUBuffer)
	b.initialized = false

	return nil
}

// DetectGPUs returns simulated GPU information.
func (b *SimulatedBackend) DetectGPUs() ([]*GPUInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.initialized {
		return nil, ErrNotInitialized
	}

	result := make([]*GPUInfo, len(b.gpus))
	copy(result, b.gpus)

	return result, nil
}

// AllocateBuffer simulates GPU memory allocation.
func (b *SimulatedBackend) AllocateBuffer(deviceID int, size int64) (*GPUBuffer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.initialized {
		return nil, ErrNotInitialized
	}

	if deviceID >= len(b.gpus) {
		return nil, ErrNoGPU
	}

	// Check available memory
	gpu := b.gpus[deviceID]
	if gpu.FreeMemory < size {
		return nil, ErrMemoryAllocation
	}

	// Allocate buffer
	b.nextBufferID++
	buf := &GPUBuffer{
		DeviceID: deviceID,
		Pointer:  b.nextBufferID,
		Size:     size,
		InUse:    false,
	}

	b.buffers[buf.Pointer] = buf
	gpu.FreeMemory -= size

	return buf, nil
}

// FreeBuffer simulates GPU memory deallocation.
func (b *SimulatedBackend) FreeBuffer(buf *GPUBuffer) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.initialized {
		return ErrNotInitialized
	}

	if buf == nil {
		return nil
	}

	existing, ok := b.buffers[buf.Pointer]
	if !ok {
		return nil
	}

	if buf.DeviceID < len(b.gpus) {
		b.gpus[buf.DeviceID].FreeMemory += existing.Size
	}

	delete(b.buffers, buf.Pointer)

	return nil
}

// RegisterFile simulates registering a file for GDS.
func (b *SimulatedBackend) RegisterFile(path string) (uintptr, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.initialized {
		return 0, ErrNotInitialized
	}

	b.nextHandle++
	handle := b.nextHandle
	b.files[handle] = path

	return handle, nil
}

// UnregisterFile simulates unregistering a file from GDS.
func (b *SimulatedBackend) UnregisterFile(handle uintptr) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.initialized {
		return ErrNotInitialized
	}

	delete(b.files, handle)

	return nil
}

// Read simulates a GDS read operation.
func (b *SimulatedBackend) Read(handle uintptr, buf *GPUBuffer, offset, length int64) (*TransferResult, error) {
	b.mu.RLock()

	if !b.initialized {
		b.mu.RUnlock()
		return nil, ErrNotInitialized
	}

	if _, ok := b.files[handle]; !ok {
		b.mu.RUnlock()
		return nil, ErrInvalidHandle
	}

	if _, ok := b.buffers[buf.Pointer]; !ok {
		b.mu.RUnlock()
		return nil, ErrInvalidHandle
	}

	latency := atomic.LoadInt64(&b.metrics.ReadLatencyNs)
	b.mu.RUnlock()

	// Simulate I/O latency
	start := time.Now()
	time.Sleep(time.Duration(latency))

	return &TransferResult{
		BytesTransferred: length,
		Duration:         time.Since(start),
	}, nil
}

// Write simulates a GDS write operation.
func (b *SimulatedBackend) Write(handle uintptr, buf *GPUBuffer, offset, length int64) (*TransferResult, error) {
	b.mu.RLock()

	if !b.initialized {
		b.mu.RUnlock()
		return nil, ErrNotInitialized
	}

	if _, ok := b.files[handle]; !ok {
		b.mu.RUnlock()
		return nil, ErrInvalidHandle
	}

	if _, ok := b.buffers[buf.Pointer]; !ok {
		b.mu.RUnlock()
		return nil, ErrInvalidHandle
	}

	latency := atomic.LoadInt64(&b.metrics.WriteLatencyNs)
	b.mu.RUnlock()

	// Simulate I/O latency
	start := time.Now()
	time.Sleep(time.Duration(latency))

	return &TransferResult{
		BytesTransferred: length,
		Duration:         time.Since(start),
	}, nil
}

// ReadAsync simulates an async GDS read.
func (b *SimulatedBackend) ReadAsync(handle uintptr, buf *GPUBuffer, offset, length int64, stream uintptr) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.initialized {
		return ErrNotInitialized
	}

	if _, ok := b.files[handle]; !ok {
		return ErrInvalidHandle
	}

	// In simulation, async reads complete immediately
	return nil
}

// WriteAsync simulates an async GDS write.
func (b *SimulatedBackend) WriteAsync(handle uintptr, buf *GPUBuffer, offset, length int64, stream uintptr) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.initialized {
		return ErrNotInitialized
	}

	if _, ok := b.files[handle]; !ok {
		return ErrInvalidHandle
	}

	return nil
}

// Sync simulates synchronizing a CUDA stream.
func (b *SimulatedBackend) Sync(stream uintptr) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.initialized {
		return ErrNotInitialized
	}

	// Simulate stream sync latency
	time.Sleep(time.Microsecond * simSyncLatencyUs)

	return nil
}

// GetMetrics returns simulated backend metrics.
func (b *SimulatedBackend) GetMetrics() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return map[string]interface{}{
		"simulated":         true,
		"gpu_count":         len(b.gpus),
		"registered_files":  len(b.files),
		"allocated_buffers": len(b.buffers),
		"read_latency_ns":   atomic.LoadInt64(&b.metrics.ReadLatencyNs),
		"write_latency_ns":  atomic.LoadInt64(&b.metrics.WriteLatencyNs),
	}
}

// SetSimulatedLatency sets the simulated I/O latency for testing.
func (b *SimulatedBackend) SetSimulatedLatency(readNs, writeNs int64) {
	atomic.StoreInt64(&b.metrics.ReadLatencyNs, readNs)
	atomic.StoreInt64(&b.metrics.WriteLatencyNs, writeNs)
}

// SetGPUs allows setting custom GPU configurations for testing.
func (b *SimulatedBackend) SetGPUs(gpus []*GPUInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.gpus = gpus
}
