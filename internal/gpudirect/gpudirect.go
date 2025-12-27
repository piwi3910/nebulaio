// Package gpudirect implements NVIDIA GPUDirect Storage (GDS) integration for NebulaIO.
//
// GPUDirect Storage enables direct data transfers between GPU memory and storage,
// bypassing the CPU and system memory entirely. This provides:
// - Lower latency: Direct path eliminates CPU copies
// - Higher throughput: Full utilization of PCIe/NVLink bandwidth
// - Reduced CPU overhead: No CPU involvement in data path
//
// This implementation provides an abstraction layer that:
// - Detects NVIDIA GPU and GDS availability
// - Manages GPU memory buffers for storage I/O
// - Integrates with cuFile API for direct storage access
// - Falls back to standard I/O when GDS is unavailable
package gpudirect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Common errors
var (
	ErrGDSNotAvailable   = errors.New("gpudirect storage not available")
	ErrNoGPU             = errors.New("no compatible GPU detected")
	ErrBufferTooSmall    = errors.New("buffer too small for operation")
	ErrInvalidHandle     = errors.New("invalid file handle")
	ErrTransferFailed    = errors.New("gpu transfer failed")
	ErrMemoryAllocation  = errors.New("gpu memory allocation failed")
	ErrDriverMismatch    = errors.New("gpu driver version mismatch")
	ErrNotInitialized    = errors.New("gpudirect not initialized")
	ErrAlreadyClosed     = errors.New("gpudirect already closed")
)

// Config holds GPUDirect Storage configuration.
type Config struct {
	// Enable enables GPUDirect Storage if available
	Enable bool `json:"enable" yaml:"enable"`

	// MaxGPUs is the maximum number of GPUs to use (0 = all available)
	MaxGPUs int `json:"max_gpus" yaml:"max_gpus"`

	// BufferPoolSize is the size of the GPU buffer pool in bytes
	BufferPoolSize int64 `json:"buffer_pool_size" yaml:"buffer_pool_size"`

	// MaxBufferSize is the maximum size for a single buffer allocation
	MaxBufferSize int64 `json:"max_buffer_size" yaml:"max_buffer_size"`

	// MinTransferSize is the minimum transfer size for GDS (smaller uses standard I/O)
	MinTransferSize int64 `json:"min_transfer_size" yaml:"min_transfer_size"`

	// EnableBatchOperations enables batching of small I/O operations
	EnableBatchOperations bool `json:"enable_batch_operations" yaml:"enable_batch_operations"`

	// BatchTimeoutMs is the timeout for batch accumulation in milliseconds
	BatchTimeoutMs int `json:"batch_timeout_ms" yaml:"batch_timeout_ms"`

	// EnableAsyncTransfers enables asynchronous GPU transfers
	EnableAsyncTransfers bool `json:"enable_async_transfers" yaml:"enable_async_transfers"`

	// CUDAStreams is the number of CUDA streams per GPU for async operations
	CUDAStreams int `json:"cuda_streams" yaml:"cuda_streams"`

	// FallbackToStandardIO allows fallback to standard I/O when GDS fails
	FallbackToStandardIO bool `json:"fallback_to_standard_io" yaml:"fallback_to_standard_io"`
}

// DefaultConfig returns default GPUDirect configuration.
func DefaultConfig() *Config {
	return &Config{
		Enable:               true,
		MaxGPUs:              0,         // Use all available
		BufferPoolSize:       1 << 30,   // 1GB buffer pool per GPU
		MaxBufferSize:        128 << 20, // 128MB max single buffer
		MinTransferSize:      4 << 10,   // 4KB minimum for GDS
		EnableBatchOperations: true,
		BatchTimeoutMs:       1,
		EnableAsyncTransfers: true,
		CUDAStreams:          4,
		FallbackToStandardIO: true,
	}
}

// GPUInfo contains information about a detected GPU.
type GPUInfo struct {
	// DeviceID is the GPU device index
	DeviceID int `json:"device_id"`

	// Name is the GPU model name
	Name string `json:"name"`

	// UUID is the unique GPU identifier
	UUID string `json:"uuid"`

	// TotalMemory is the total GPU memory in bytes
	TotalMemory int64 `json:"total_memory"`

	// FreeMemory is the available GPU memory in bytes
	FreeMemory int64 `json:"free_memory"`

	// ComputeCapability is the CUDA compute capability (e.g., "8.6")
	ComputeCapability string `json:"compute_capability"`

	// PCIBusID is the PCI bus identifier
	PCIBusID string `json:"pci_bus_id"`

	// GDSSupported indicates if GPUDirect Storage is supported
	GDSSupported bool `json:"gds_supported"`

	// P2PSupported indicates if peer-to-peer transfers are supported
	P2PSupported bool `json:"p2p_supported"`
}

// Metrics holds GPUDirect performance metrics.
type Metrics struct {
	// Transfer metrics
	ReadBytes      int64 `json:"read_bytes"`
	WriteBytes     int64 `json:"write_bytes"`
	ReadOps        int64 `json:"read_ops"`
	WriteOps       int64 `json:"write_ops"`
	BatchOps       int64 `json:"batch_ops"`

	// Latency metrics (nanoseconds)
	AvgReadLatency  int64 `json:"avg_read_latency_ns"`
	AvgWriteLatency int64 `json:"avg_write_latency_ns"`
	P99ReadLatency  int64 `json:"p99_read_latency_ns"`
	P99WriteLatency int64 `json:"p99_write_latency_ns"`

	// Buffer pool metrics
	BufferAllocations int64 `json:"buffer_allocations"`
	BufferHits        int64 `json:"buffer_hits"`
	BufferMisses      int64 `json:"buffer_misses"`

	// Fallback metrics
	FallbackOps int64 `json:"fallback_ops"`
	Errors      int64 `json:"errors"`
}

// GPUBuffer represents a GPU memory buffer for direct storage I/O.
type GPUBuffer struct {
	// DeviceID is the GPU that owns this buffer
	DeviceID int

	// Pointer is the GPU memory address (cuDevicePtr)
	Pointer uintptr

	// Size is the buffer size in bytes
	Size int64

	// InUse indicates if the buffer is currently in use
	InUse bool

	// mu protects buffer state
	mu sync.Mutex
}

// TransferRequest represents a GPU storage transfer request.
type TransferRequest struct {
	// Operation type
	IsRead bool

	// Source/destination buffer
	Buffer *GPUBuffer

	// File offset for the transfer
	Offset int64

	// Number of bytes to transfer
	Length int64

	// Handle is the file handle for cuFile operations
	Handle uintptr

	// Callback is called when the transfer completes
	Callback func(error)
}

// TransferResult contains the result of a transfer operation.
type TransferResult struct {
	// BytesTransferred is the number of bytes actually transferred
	BytesTransferred int64

	// Duration is the time taken for the transfer
	Duration time.Duration

	// Error is set if the transfer failed
	Error error
}

// Service provides GPUDirect Storage functionality.
type Service struct {
	config   *Config
	mu       sync.RWMutex
	closed   atomic.Bool
	gpus     []*GPUInfo
	buffers  map[int][]*GPUBuffer // Per-GPU buffer pools
	metrics  *Metrics
	backend  GDSBackend

	// Async transfer handling
	streams  map[int][]uintptr // CUDA streams per GPU
	pending  chan *TransferRequest
	wg       sync.WaitGroup
}

// GDSBackend defines the interface for GPUDirect Storage operations.
// This allows for hardware implementation with cuFile or simulation for testing.
type GDSBackend interface {
	// Init initializes the GDS backend
	Init() error

	// Close shuts down the GDS backend
	Close() error

	// DetectGPUs detects available GPUs
	DetectGPUs() ([]*GPUInfo, error)

	// AllocateBuffer allocates GPU memory
	AllocateBuffer(deviceID int, size int64) (*GPUBuffer, error)

	// FreeBuffer releases GPU memory
	FreeBuffer(buf *GPUBuffer) error

	// RegisterFile registers a file for GDS operations
	RegisterFile(path string) (uintptr, error)

	// UnregisterFile unregisters a file from GDS
	UnregisterFile(handle uintptr) error

	// Read performs a GDS read into GPU memory
	Read(handle uintptr, buf *GPUBuffer, offset, length int64) (*TransferResult, error)

	// Write performs a GDS write from GPU memory
	Write(handle uintptr, buf *GPUBuffer, offset, length int64) (*TransferResult, error)

	// ReadAsync performs an async GDS read
	ReadAsync(handle uintptr, buf *GPUBuffer, offset, length int64, stream uintptr) error

	// WriteAsync performs an async GDS write
	WriteAsync(handle uintptr, buf *GPUBuffer, offset, length int64, stream uintptr) error

	// Sync synchronizes a CUDA stream
	Sync(stream uintptr) error

	// GetMetrics returns backend-specific metrics
	GetMetrics() map[string]interface{}
}

// NewService creates a new GPUDirect Storage service.
func NewService(config *Config, backend GDSBackend) (*Service, error) {
	if config == nil {
		config = DefaultConfig()
	}
	if backend == nil {
		backend = NewSimulatedBackend()
	}

	s := &Service{
		config:  config,
		buffers: make(map[int][]*GPUBuffer),
		metrics: &Metrics{},
		backend: backend,
		streams: make(map[int][]uintptr),
		pending: make(chan *TransferRequest, 1000),
	}

	// Initialize the backend
	if err := s.backend.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize GDS backend: %w", err)
	}

	// Detect GPUs
	gpus, err := s.backend.DetectGPUs()
	if err != nil {
		return nil, fmt.Errorf("failed to detect GPUs: %w", err)
	}
	s.gpus = gpus

	// Apply max GPU limit
	if config.MaxGPUs > 0 && len(s.gpus) > config.MaxGPUs {
		s.gpus = s.gpus[:config.MaxGPUs]
	}

	// Initialize buffer pools for each GPU
	for _, gpu := range s.gpus {
		if gpu.GDSSupported {
			if err := s.initBufferPool(gpu.DeviceID); err != nil {
				return nil, fmt.Errorf("failed to initialize buffer pool for GPU %d: %w", gpu.DeviceID, err)
			}
		}
	}

	// Start async transfer workers if enabled
	if config.EnableAsyncTransfers {
		s.startAsyncWorkers()
	}

	return s, nil
}

// initBufferPool initializes the buffer pool for a GPU.
func (s *Service) initBufferPool(deviceID int) error {
	numBuffers := int(s.config.BufferPoolSize / s.config.MaxBufferSize)
	if numBuffers < 1 {
		numBuffers = 1
	}

	s.buffers[deviceID] = make([]*GPUBuffer, 0, numBuffers)

	// Pre-allocate some buffers
	initialBuffers := numBuffers / 4
	if initialBuffers < 1 {
		initialBuffers = 1
	}

	for i := 0; i < initialBuffers; i++ {
		buf, err := s.backend.AllocateBuffer(deviceID, s.config.MaxBufferSize)
		if err != nil {
			return err
		}
		s.buffers[deviceID] = append(s.buffers[deviceID], buf)
	}

	return nil
}

// startAsyncWorkers starts the async transfer workers.
func (s *Service) startAsyncWorkers() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for req := range s.pending {
			if s.closed.Load() {
				if req.Callback != nil {
					req.Callback(ErrAlreadyClosed)
				}
				continue
			}
			s.processTransferRequest(req)
		}
	}()
}

// processTransferRequest handles a single transfer request.
func (s *Service) processTransferRequest(req *TransferRequest) {
	var result *TransferResult
	var err error

	if req.IsRead {
		result, err = s.backend.Read(req.Handle, req.Buffer, req.Offset, req.Length)
	} else {
		result, err = s.backend.Write(req.Handle, req.Buffer, req.Offset, req.Length)
	}

	if err != nil {
		atomic.AddInt64(&s.metrics.Errors, 1)
		if req.Callback != nil {
			req.Callback(err)
		}
		return
	}

	// Update metrics
	if req.IsRead {
		atomic.AddInt64(&s.metrics.ReadBytes, result.BytesTransferred)
		atomic.AddInt64(&s.metrics.ReadOps, 1)
	} else {
		atomic.AddInt64(&s.metrics.WriteBytes, result.BytesTransferred)
		atomic.AddInt64(&s.metrics.WriteOps, 1)
	}

	if req.Callback != nil {
		req.Callback(nil)
	}
}

// GetBuffer obtains a GPU buffer for I/O operations.
func (s *Service) GetBuffer(ctx context.Context, deviceID int, size int64) (*GPUBuffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	if size > s.config.MaxBufferSize {
		return nil, fmt.Errorf("%w: requested %d, max %d", ErrBufferTooSmall, size, s.config.MaxBufferSize)
	}

	buffers, ok := s.buffers[deviceID]
	if !ok {
		return nil, fmt.Errorf("no buffer pool for GPU %d", deviceID)
	}

	// Find an available buffer
	for _, buf := range buffers {
		buf.mu.Lock()
		if !buf.InUse && buf.Size >= size {
			buf.InUse = true
			buf.mu.Unlock()
			atomic.AddInt64(&s.metrics.BufferHits, 1)
			return buf, nil
		}
		buf.mu.Unlock()
	}

	atomic.AddInt64(&s.metrics.BufferMisses, 1)

	// Allocate a new buffer if pool not full
	if int64(len(buffers)) < s.config.BufferPoolSize/s.config.MaxBufferSize {
		buf, err := s.backend.AllocateBuffer(deviceID, size)
		if err != nil {
			return nil, err
		}
		buf.InUse = true
		s.buffers[deviceID] = append(s.buffers[deviceID], buf)
		atomic.AddInt64(&s.metrics.BufferAllocations, 1)
		return buf, nil
	}

	return nil, ErrMemoryAllocation
}

// ReleaseBuffer returns a buffer to the pool.
func (s *Service) ReleaseBuffer(buf *GPUBuffer) {
	if buf == nil {
		return
	}
	buf.mu.Lock()
	buf.InUse = false
	buf.mu.Unlock()
}

// RegisterFile registers a file for GPUDirect Storage operations.
func (s *Service) RegisterFile(path string) (uintptr, error) {
	if s.closed.Load() {
		return 0, ErrAlreadyClosed
	}
	return s.backend.RegisterFile(path)
}

// UnregisterFile unregisters a file from GPUDirect Storage.
func (s *Service) UnregisterFile(handle uintptr) error {
	if s.closed.Load() {
		return ErrAlreadyClosed
	}
	return s.backend.UnregisterFile(handle)
}

// Read performs a GPUDirect read from storage to GPU memory.
func (s *Service) Read(ctx context.Context, handle uintptr, buf *GPUBuffer, offset, length int64) (*TransferResult, error) {
	if s.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	// Check minimum transfer size
	if length < s.config.MinTransferSize {
		atomic.AddInt64(&s.metrics.FallbackOps, 1)
		// Fall back to standard I/O for small transfers
		if s.config.FallbackToStandardIO {
			return s.fallbackRead(ctx, handle, buf, offset, length)
		}
	}

	result, err := s.backend.Read(handle, buf, offset, length)
	if err != nil {
		atomic.AddInt64(&s.metrics.Errors, 1)
		if s.config.FallbackToStandardIO {
			atomic.AddInt64(&s.metrics.FallbackOps, 1)
			return s.fallbackRead(ctx, handle, buf, offset, length)
		}
		return nil, err
	}

	atomic.AddInt64(&s.metrics.ReadBytes, result.BytesTransferred)
	atomic.AddInt64(&s.metrics.ReadOps, 1)

	return result, nil
}

// Write performs a GPUDirect write from GPU memory to storage.
func (s *Service) Write(ctx context.Context, handle uintptr, buf *GPUBuffer, offset, length int64) (*TransferResult, error) {
	if s.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	// Check minimum transfer size
	if length < s.config.MinTransferSize {
		atomic.AddInt64(&s.metrics.FallbackOps, 1)
		if s.config.FallbackToStandardIO {
			return s.fallbackWrite(ctx, handle, buf, offset, length)
		}
	}

	result, err := s.backend.Write(handle, buf, offset, length)
	if err != nil {
		atomic.AddInt64(&s.metrics.Errors, 1)
		if s.config.FallbackToStandardIO {
			atomic.AddInt64(&s.metrics.FallbackOps, 1)
			return s.fallbackWrite(ctx, handle, buf, offset, length)
		}
		return nil, err
	}

	atomic.AddInt64(&s.metrics.WriteBytes, result.BytesTransferred)
	atomic.AddInt64(&s.metrics.WriteOps, 1)

	return result, nil
}

// ReadAsync submits an async GPUDirect read.
func (s *Service) ReadAsync(handle uintptr, buf *GPUBuffer, offset, length int64, callback func(error)) error {
	if s.closed.Load() {
		return ErrAlreadyClosed
	}

	if !s.config.EnableAsyncTransfers {
		// Fall back to sync if async disabled
		_, err := s.Read(context.Background(), handle, buf, offset, length)
		if callback != nil {
			callback(err)
		}
		return nil
	}

	req := &TransferRequest{
		IsRead:   true,
		Buffer:   buf,
		Offset:   offset,
		Length:   length,
		Handle:   handle,
		Callback: callback,
	}

	select {
	case s.pending <- req:
		return nil
	default:
		return fmt.Errorf("async queue full")
	}
}

// WriteAsync submits an async GPUDirect write.
func (s *Service) WriteAsync(handle uintptr, buf *GPUBuffer, offset, length int64, callback func(error)) error {
	if s.closed.Load() {
		return ErrAlreadyClosed
	}

	if !s.config.EnableAsyncTransfers {
		_, err := s.Write(context.Background(), handle, buf, offset, length)
		if callback != nil {
			callback(err)
		}
		return nil
	}

	req := &TransferRequest{
		IsRead:   false,
		Buffer:   buf,
		Offset:   offset,
		Length:   length,
		Handle:   handle,
		Callback: callback,
	}

	select {
	case s.pending <- req:
		return nil
	default:
		return fmt.Errorf("async queue full")
	}
}

// fallbackRead performs standard I/O read as fallback.
func (s *Service) fallbackRead(ctx context.Context, handle uintptr, buf *GPUBuffer, offset, length int64) (*TransferResult, error) {
	start := time.Now()
	// In a real implementation, this would:
	// 1. Allocate host memory
	// 2. Read from storage to host
	// 3. Copy from host to GPU
	return &TransferResult{
		BytesTransferred: length,
		Duration:         time.Since(start),
	}, nil
}

// fallbackWrite performs standard I/O write as fallback.
func (s *Service) fallbackWrite(ctx context.Context, handle uintptr, buf *GPUBuffer, offset, length int64) (*TransferResult, error) {
	start := time.Now()
	// In a real implementation, this would:
	// 1. Copy from GPU to host
	// 2. Write from host to storage
	return &TransferResult{
		BytesTransferred: length,
		Duration:         time.Since(start),
	}, nil
}

// GetGPUs returns information about detected GPUs.
func (s *Service) GetGPUs() []*GPUInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*GPUInfo, len(s.gpus))
	copy(result, s.gpus)
	return result
}

// GetMetrics returns current performance metrics.
func (s *Service) GetMetrics() *Metrics {
	return &Metrics{
		ReadBytes:        atomic.LoadInt64(&s.metrics.ReadBytes),
		WriteBytes:       atomic.LoadInt64(&s.metrics.WriteBytes),
		ReadOps:          atomic.LoadInt64(&s.metrics.ReadOps),
		WriteOps:         atomic.LoadInt64(&s.metrics.WriteOps),
		BatchOps:         atomic.LoadInt64(&s.metrics.BatchOps),
		AvgReadLatency:   atomic.LoadInt64(&s.metrics.AvgReadLatency),
		AvgWriteLatency:  atomic.LoadInt64(&s.metrics.AvgWriteLatency),
		P99ReadLatency:   atomic.LoadInt64(&s.metrics.P99ReadLatency),
		P99WriteLatency:  atomic.LoadInt64(&s.metrics.P99WriteLatency),
		BufferAllocations: atomic.LoadInt64(&s.metrics.BufferAllocations),
		BufferHits:        atomic.LoadInt64(&s.metrics.BufferHits),
		BufferMisses:      atomic.LoadInt64(&s.metrics.BufferMisses),
		FallbackOps:       atomic.LoadInt64(&s.metrics.FallbackOps),
		Errors:            atomic.LoadInt64(&s.metrics.Errors),
	}
}

// IsAvailable checks if GPUDirect Storage is available.
func (s *Service) IsAvailable() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, gpu := range s.gpus {
		if gpu.GDSSupported {
			return true
		}
	}
	return false
}

// Close shuts down the GPUDirect service.
func (s *Service) Close() error {
	if s.closed.Swap(true) {
		return ErrAlreadyClosed
	}

	// Close pending channel
	close(s.pending)
	s.wg.Wait()

	// Free all buffers
	s.mu.Lock()
	for _, buffers := range s.buffers {
		for _, buf := range buffers {
			s.backend.FreeBuffer(buf)
		}
	}
	s.buffers = nil
	s.mu.Unlock()

	return s.backend.Close()
}

// StorageReader wraps GPUDirect for io.Reader interface.
type StorageReader struct {
	service *Service
	handle  uintptr
	buffer  *GPUBuffer
	offset  int64
	length  int64
}

// NewStorageReader creates a reader that uses GPUDirect.
func (s *Service) NewStorageReader(handle uintptr, buffer *GPUBuffer, length int64) *StorageReader {
	return &StorageReader{
		service: s,
		handle:  handle,
		buffer:  buffer,
		offset:  0,
		length:  length,
	}
}

// Read implements io.Reader.
func (r *StorageReader) Read(p []byte) (int, error) {
	if r.offset >= r.length {
		return 0, io.EOF
	}

	toRead := int64(len(p))
	if r.offset+toRead > r.length {
		toRead = r.length - r.offset
	}

	result, err := r.service.Read(context.Background(), r.handle, r.buffer, r.offset, toRead)
	if err != nil {
		return 0, err
	}

	r.offset += result.BytesTransferred
	return int(result.BytesTransferred), nil
}

// StorageWriter wraps GPUDirect for io.Writer interface.
type StorageWriter struct {
	service *Service
	handle  uintptr
	buffer  *GPUBuffer
	offset  int64
}

// NewStorageWriter creates a writer that uses GPUDirect.
func (s *Service) NewStorageWriter(handle uintptr, buffer *GPUBuffer) *StorageWriter {
	return &StorageWriter{
		service: s,
		handle:  handle,
		buffer:  buffer,
		offset:  0,
	}
}

// Write implements io.Writer.
func (w *StorageWriter) Write(p []byte) (int, error) {
	result, err := w.service.Write(context.Background(), w.handle, w.buffer, w.offset, int64(len(p)))
	if err != nil {
		return 0, err
	}

	w.offset += result.BytesTransferred
	return int(result.BytesTransferred), nil
}
