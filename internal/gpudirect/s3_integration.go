// Package gpudirect provides S3 API integration for GPUDirect Storage.
package gpudirect

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
)

// Conversion constants.
const (
	// Microseconds per millisecond for time conversion.
	microsecondsPerMs = 1000.0
)

// S3Integration provides GPUDirect-aware S3 operations.
type S3Integration struct {
	service *Service
	handles map[string]uintptr
	mu      sync.RWMutex
}

// NewS3Integration creates a new S3 integration layer.
func NewS3Integration(service *Service) *S3Integration {
	return &S3Integration{
		service: service,
		handles: make(map[string]uintptr),
	}
}

// GPUDirectHeaders are HTTP headers for GPUDirect operations.
const (
	// HeaderGPUDeviceID specifies which GPU to use for the transfer.
	HeaderGPUDeviceID = "X-Gpu-Device-Id"

	// HeaderGPUDirectEnabled indicates if GPUDirect should be used.
	HeaderGPUDirectEnabled = "X-Gpu-Direct-Enabled"

	// HeaderGPUBufferSize specifies the GPU buffer size to use.
	HeaderGPUBufferSize = "X-Gpu-Buffer-Size"

	// HeaderGPUAsync indicates if async transfer is requested.
	HeaderGPUAsync = "X-Gpu-Async"

	// ResponseHeaderGPUDirectUsed indicates if GPUDirect was used.
	ResponseHeaderGPUDirectUsed = "X-Gpu-Direct-Used"

	// ResponseHeaderGPUDeviceID indicates which GPU was used.
	ResponseHeaderGPUDeviceID = "X-Gpu-Device-Id"

	// ResponseHeaderGPUTransferTime indicates the GPU transfer time in ms.
	ResponseHeaderGPUTransferTime = "X-Gpu-Transfer-Time-Ms"
)

// ParseGPUDirectRequest extracts GPUDirect parameters from HTTP request.
func ParseGPUDirectRequest(r *http.Request) *GPUDirectRequest {
	req := &GPUDirectRequest{
		Enabled:  false,
		DeviceID: 0,
		Async:    false,
	}

	// Check if GPUDirect is requested
	if enabled := r.Header.Get(HeaderGPUDirectEnabled); enabled == "true" || enabled == "1" {
		req.Enabled = true
	}

	// Parse GPU device ID
	if deviceStr := r.Header.Get(HeaderGPUDeviceID); deviceStr != "" {
		if deviceID, err := strconv.Atoi(deviceStr); err == nil {
			req.DeviceID = deviceID
		}
	}

	// Parse buffer size
	if sizeStr := r.Header.Get(HeaderGPUBufferSize); sizeStr != "" {
		if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
			req.BufferSize = size
		}
	}

	// Check async flag
	if async := r.Header.Get(HeaderGPUAsync); async == "true" || async == "1" {
		req.Async = true
	}

	return req
}

// GPUDirectRequest contains GPUDirect request parameters.
type GPUDirectRequest struct {
	DeviceID   int
	BufferSize int64
	Enabled    bool
	Async      bool
}

// GPUDirectResponse contains GPUDirect response information.
type GPUDirectResponse struct {
	Error            error
	DeviceID         int
	TransferTimeMs   float64
	BytesTransferred int64
	Used             bool
}

// SetResponseHeaders adds GPUDirect response headers.
func (resp *GPUDirectResponse) SetResponseHeaders(w http.ResponseWriter) {
	if resp.Used {
		w.Header().Set(ResponseHeaderGPUDirectUsed, "true")
		w.Header().Set(ResponseHeaderGPUDeviceID, strconv.Itoa(resp.DeviceID))
		w.Header().Set(ResponseHeaderGPUTransferTime, fmt.Sprintf("%.3f", resp.TransferTimeMs))
	} else {
		w.Header().Set(ResponseHeaderGPUDirectUsed, "false")
	}
}

// GetObject performs a GPUDirect-enabled GET operation.
func (s *S3Integration) GetObject(ctx context.Context, bucket, key string, req *GPUDirectRequest, w io.Writer) (*GPUDirectResponse, error) {
	resp := &GPUDirectResponse{
		Used: false,
	}

	// Check if GPUDirect is available and requested
	if !req.Enabled || !s.service.IsAvailable() {
		return resp, nil
	}

	// Validate GPU device
	gpus := s.service.GetGPUs()
	if req.DeviceID >= len(gpus) {
		resp.Error = fmt.Errorf("GPU device %d not found", req.DeviceID)
		return resp, nil
	}

	if !gpus[req.DeviceID].GDSSupported {
		resp.Error = fmt.Errorf("GPU device %d does not support GPUDirect", req.DeviceID)
		return resp, nil
	}

	// Get or register file handle
	objectPath := fmt.Sprintf("%s/%s", bucket, key)

	handle, err := s.getOrRegisterHandle(objectPath)
	if err != nil {
		resp.Error = err
		return resp, nil
	}

	// Get buffer size
	bufferSize := req.BufferSize
	if bufferSize == 0 {
		bufferSize = s.service.config.MaxBufferSize
	}

	// Allocate GPU buffer
	buf, err := s.service.GetBuffer(ctx, req.DeviceID, bufferSize)
	if err != nil {
		resp.Error = err
		return resp, nil
	}
	defer s.service.ReleaseBuffer(buf)

	// Perform GPUDirect read
	result, err := s.service.Read(ctx, handle, buf, 0, bufferSize)
	if err != nil {
		resp.Error = err
		return resp, nil
	}

	resp.Used = true
	resp.DeviceID = req.DeviceID
	resp.BytesTransferred = result.BytesTransferred
	resp.TransferTimeMs = float64(result.Duration.Microseconds()) / microsecondsPerMs

	return resp, nil
}

// PutObject performs a GPUDirect-enabled PUT operation.
func (s *S3Integration) PutObject(ctx context.Context, bucket, key string, req *GPUDirectRequest, r io.Reader, size int64) (*GPUDirectResponse, error) {
	resp := &GPUDirectResponse{
		Used: false,
	}

	// Check if GPUDirect is available and requested
	if !req.Enabled || !s.service.IsAvailable() {
		return resp, nil
	}

	// Validate GPU device
	gpus := s.service.GetGPUs()
	if req.DeviceID >= len(gpus) {
		resp.Error = fmt.Errorf("GPU device %d not found", req.DeviceID)
		return resp, nil
	}

	if !gpus[req.DeviceID].GDSSupported {
		resp.Error = fmt.Errorf("GPU device %d does not support GPUDirect", req.DeviceID)
		return resp, nil
	}

	// Get or register file handle
	objectPath := fmt.Sprintf("%s/%s", bucket, key)

	handle, err := s.getOrRegisterHandle(objectPath)
	if err != nil {
		resp.Error = err
		return resp, nil
	}

	// Get buffer size
	bufferSize := req.BufferSize
	if bufferSize == 0 {
		bufferSize = s.service.config.MaxBufferSize
	}

	if size > 0 && size < bufferSize {
		bufferSize = size
	}

	// Allocate GPU buffer
	buf, err := s.service.GetBuffer(ctx, req.DeviceID, bufferSize)
	if err != nil {
		resp.Error = err
		return resp, nil
	}
	defer s.service.ReleaseBuffer(buf)

	// Perform GPUDirect write
	result, err := s.service.Write(ctx, handle, buf, 0, bufferSize)
	if err != nil {
		resp.Error = err
		return resp, nil
	}

	resp.Used = true
	resp.DeviceID = req.DeviceID
	resp.BytesTransferred = result.BytesTransferred
	resp.TransferTimeMs = float64(result.Duration.Microseconds()) / microsecondsPerMs

	return resp, nil
}

// getOrRegisterHandle gets an existing handle or registers a new one.
func (s *S3Integration) getOrRegisterHandle(path string) (uintptr, error) {
	s.mu.RLock()
	handle, ok := s.handles[path]
	s.mu.RUnlock()

	if ok {
		return handle, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if handle, ok = s.handles[path]; ok {
		return handle, nil
	}

	// Register new handle
	handle, err := s.service.RegisterFile(path)
	if err != nil {
		return 0, err
	}

	s.handles[path] = handle

	return handle, nil
}

// UnregisterObject removes a file handle.
func (s *S3Integration) UnregisterObject(bucket, key string) error {
	path := fmt.Sprintf("%s/%s", bucket, key)

	s.mu.Lock()
	defer s.mu.Unlock()

	handle, ok := s.handles[path]
	if !ok {
		return nil
	}

	err := s.service.UnregisterFile(handle)
	if err != nil {
		return err
	}

	delete(s.handles, path)

	return nil
}

// GetStatus returns the GPUDirect service status.
func (s *S3Integration) GetStatus() map[string]interface{} {
	gpus := s.service.GetGPUs()
	metrics := s.service.GetMetrics()

	gpuInfo := make([]map[string]interface{}, len(gpus))
	for i, gpu := range gpus {
		gpuInfo[i] = map[string]interface{}{
			"device_id":          gpu.DeviceID,
			"name":               gpu.Name,
			"uuid":               gpu.UUID,
			"total_memory":       gpu.TotalMemory,
			"free_memory":        gpu.FreeMemory,
			"compute_capability": gpu.ComputeCapability,
			"gds_supported":      gpu.GDSSupported,
			"p2p_supported":      gpu.P2PSupported,
		}
	}

	s.mu.RLock()
	registeredFiles := len(s.handles)
	s.mu.RUnlock()

	return map[string]interface{}{
		"available":        s.service.IsAvailable(),
		"gpus":             gpuInfo,
		"registered_files": registeredFiles,
		"metrics": map[string]interface{}{
			"read_bytes":         metrics.ReadBytes,
			"write_bytes":        metrics.WriteBytes,
			"read_ops":           metrics.ReadOps,
			"write_ops":          metrics.WriteOps,
			"buffer_hits":        metrics.BufferHits,
			"buffer_misses":      metrics.BufferMisses,
			"buffer_allocations": metrics.BufferAllocations,
			"fallback_ops":       metrics.FallbackOps,
			"errors":             metrics.Errors,
		},
	}
}

// Close shuts down the S3 integration.
func (s *S3Integration) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Unregister all handles
	for path, handle := range s.handles {
		_ = s.service.UnregisterFile(handle)
		delete(s.handles, path)
	}

	return nil
}
