// Package dpu implements NVIDIA BlueField DPU (Data Processing Unit) support for NebulaIO.
//
// BlueField DPUs are SmartNICs that combine network interface capabilities with
// ARM-based processing cores. This enables offloading of storage and network
// operations from the host CPU to the DPU, providing:
// - Storage target offload: Run storage services directly on the DPU
// - Network acceleration: Hardware-accelerated packet processing
// - Crypto offload: Hardware encryption/decryption
// - Compression offload: Hardware-accelerated compression
// - RDMA acceleration: Direct memory access for lowest latency
//
// This implementation provides:
// - DPU discovery and capability detection
// - Offload service management
// - DOCA SDK integration abstraction
// - Performance monitoring and metrics
package dpu

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Common errors.
var (
	ErrDPUNotAvailable     = errors.New("dpu not available")
	ErrDPUNotInitialized   = errors.New("dpu not initialized")
	ErrOffloadNotSupported = errors.New("offload type not supported")
	ErrServiceNotFound     = errors.New("offload service not found")
	ErrServiceRunning      = errors.New("service already running")
	ErrServiceStopped      = errors.New("service not running")
	ErrAlreadyClosed       = errors.New("dpu service already closed")
	ErrInvalidConfig       = errors.New("invalid configuration")
)

// OffloadType represents a type of operation that can be offloaded to the DPU.
type OffloadType string

const (
	// OffloadStorage offloads storage target operations.
	OffloadStorage OffloadType = "storage"

	// OffloadNetwork offloads network packet processing.
	OffloadNetwork OffloadType = "network"

	// OffloadCrypto offloads encryption/decryption.
	OffloadCrypto OffloadType = "crypto"

	// OffloadCompress offloads compression/decompression.
	OffloadCompress OffloadType = "compress"

	// OffloadRDMA offloads RDMA operations.
	OffloadRDMA OffloadType = "rdma"

	// OffloadRegex offloads regex pattern matching.
	OffloadRegex OffloadType = "regex"
)

// Config holds BlueField DPU configuration.
type Config struct {
	StorageOffload      *StorageOffloadConfig  `json:"storage_offload,omitempty" yaml:"storage_offload,omitempty"`
	CryptoOffload       *CryptoOffloadConfig   `json:"crypto_offload,omitempty" yaml:"crypto_offload,omitempty"`
	CompressOffload     *CompressOffloadConfig `json:"compress_offload,omitempty" yaml:"compress_offload,omitempty"`
	NetworkOffload      *NetworkOffloadConfig  `json:"network_offload,omitempty" yaml:"network_offload,omitempty"`
	Offloads            []OffloadType          `json:"offloads" yaml:"offloads"`
	DeviceIndex         int                    `json:"device_index" yaml:"device_index"`
	HealthCheckInterval time.Duration          `json:"health_check_interval" yaml:"health_check_interval"`
	Enable              bool                   `json:"enable" yaml:"enable"`
	FallbackOnFailure   bool                   `json:"fallback_on_failure" yaml:"fallback_on_failure"`
}

// StorageOffloadConfig configures storage target offload.
type StorageOffloadConfig struct {
	// Enable enables storage offload
	Enable bool `json:"enable" yaml:"enable"`

	// NVMeTargets configures NVMe-oF target offload
	NVMeTargets bool `json:"nvme_targets" yaml:"nvme_targets"`

	// VirtIOBlk enables VirtIO-blk backend offload
	VirtIOBlk bool `json:"virtio_blk" yaml:"virtio_blk"`

	// SPDK enables SPDK-based storage offload
	SPDK bool `json:"spdk" yaml:"spdk"`

	// MaxQueues is the maximum number of I/O queues
	MaxQueues int `json:"max_queues" yaml:"max_queues"`

	// QueueDepth is the depth of each I/O queue
	QueueDepth int `json:"queue_depth" yaml:"queue_depth"`
}

// CryptoOffloadConfig configures crypto offload.
type CryptoOffloadConfig struct {
	Algorithms  []string `json:"algorithms" yaml:"algorithms"`
	MaxSessions int      `json:"max_sessions" yaml:"max_sessions"`
	Enable      bool     `json:"enable" yaml:"enable"`
}

// CompressOffloadConfig configures compression offload.
type CompressOffloadConfig struct {
	Algorithms []string `json:"algorithms" yaml:"algorithms"`
	MinSize    int64    `json:"min_size" yaml:"min_size"`
	Enable     bool     `json:"enable" yaml:"enable"`
}

// NetworkOffloadConfig configures network offload.
type NetworkOffloadConfig struct {
	// Enable enables network offload
	Enable bool `json:"enable" yaml:"enable"`

	// FlowSteering enables hardware flow steering
	FlowSteering bool `json:"flow_steering" yaml:"flow_steering"`

	// Checksum enables hardware checksum offload
	Checksum bool `json:"checksum" yaml:"checksum"`

	// TSO enables TCP segmentation offload
	TSO bool `json:"tso" yaml:"tso"`

	// LRO enables large receive offload
	LRO bool `json:"lro" yaml:"lro"`
}

// DefaultConfig returns default DPU configuration.
func DefaultConfig() *Config {
	return &Config{
		Enable:      true,
		DeviceIndex: -1, // Auto-select
		Offloads: []OffloadType{
			OffloadStorage,
			OffloadCrypto,
			OffloadCompress,
			OffloadNetwork,
		},
		StorageOffload: &StorageOffloadConfig{
			Enable:      true,
			NVMeTargets: true,
			VirtIOBlk:   true,
			SPDK:        false,
			MaxQueues:   32,
			QueueDepth:  128,
		},
		CryptoOffload: &CryptoOffloadConfig{
			Enable:      true,
			Algorithms:  []string{"aes-gcm-128", "aes-gcm-256", "sha256", "sha512"},
			MaxSessions: 1024,
		},
		CompressOffload: &CompressOffloadConfig{
			Enable:     true,
			Algorithms: []string{"deflate", "lz4"},
			MinSize:    4096,
		},
		NetworkOffload: &NetworkOffloadConfig{
			Enable:       true,
			FlowSteering: true,
			Checksum:     true,
			TSO:          true,
			LRO:          true,
		},
		HealthCheckInterval: 30 * time.Second,
		FallbackOnFailure:   true,
	}
}

// DPUInfo contains information about a detected DPU.
type DPUInfo struct {
	Model           string        `json:"model"`
	SerialNumber    string        `json:"serial_number"`
	FirmwareVersion string        `json:"firmware_version"`
	DOCAVersion     string        `json:"doca_version"`
	Status          string        `json:"status"`
	Capabilities    []OffloadType `json:"capabilities"`
	ARMFrequencyMHz int           `json:"arm_frequency_mhz"`
	MemoryGB        int           `json:"memory_gb"`
	NetworkPorts    int           `json:"network_ports"`
	PortSpeedGbps   int           `json:"port_speed_gbps"`
	Index           int           `json:"index"`
	ARMCores        int           `json:"arm_cores"`
	Temperature     float64       `json:"temperature"`
	PowerWatts      float64       `json:"power_watts"`
}

// Metrics holds DPU performance metrics.
type Metrics struct {
	// Offload operation counts
	StorageOps  int64 `json:"storage_ops"`
	CryptoOps   int64 `json:"crypto_ops"`
	CompressOps int64 `json:"compress_ops"`
	NetworkOps  int64 `json:"network_ops"`

	// Bytes processed
	StorageBytes  int64 `json:"storage_bytes"`
	CryptoBytes   int64 `json:"crypto_bytes"`
	CompressBytes int64 `json:"compress_bytes"`
	NetworkBytes  int64 `json:"network_bytes"`

	// Latency (nanoseconds)
	AvgStorageLatency  int64 `json:"avg_storage_latency_ns"`
	AvgCryptoLatency   int64 `json:"avg_crypto_latency_ns"`
	AvgCompressLatency int64 `json:"avg_compress_latency_ns"`

	// Errors and fallbacks
	Errors      int64 `json:"errors"`
	Fallbacks   int64 `json:"fallbacks"`
	HealthFails int64 `json:"health_fails"`

	// Utilization (percentage)
	ARMUtilization     float64 `json:"arm_utilization"`
	CryptoUtilization  float64 `json:"crypto_utilization"`
	NetworkUtilization float64 `json:"network_utilization"`
}

// OffloadService represents an offload service running on the DPU.
type OffloadService struct {
	StartTime time.Time   `json:"start_time"`
	Config    interface{} `json:"config"`
	Type      OffloadType `json:"type"`
	Name      string      `json:"name"`
	Status    string      `json:"status"`
}

// Service provides BlueField DPU functionality.
type Service struct {
	backend      DPUBackend
	healthCtx    context.Context
	config       *Config
	dpu          *DPUInfo
	services     map[OffloadType]*OffloadService
	metrics      *Metrics
	healthCancel context.CancelFunc
	healthWg     sync.WaitGroup
	mu           sync.RWMutex
	closed       atomic.Bool
}

// DPUBackend defines the interface for DPU operations.
// This allows for hardware implementation with DOCA SDK or simulation for testing.
type DPUBackend interface {
	// Init initializes the DPU backend
	Init() error

	// Close shuts down the DPU backend
	Close() error

	// DetectDPUs detects available DPUs
	DetectDPUs() ([]*DPUInfo, error)

	// SelectDPU selects and initializes a specific DPU
	SelectDPU(index int) (*DPUInfo, error)

	// StartService starts an offload service
	StartService(offloadType OffloadType, config interface{}) error

	// StopService stops an offload service
	StopService(offloadType OffloadType) error

	// GetServiceStatus returns the status of an offload service
	GetServiceStatus(offloadType OffloadType) (*OffloadService, error)

	// Encrypt performs hardware-accelerated encryption
	Encrypt(ctx context.Context, algorithm string, key, plaintext []byte) ([]byte, error)

	// Decrypt performs hardware-accelerated decryption
	Decrypt(ctx context.Context, algorithm string, key, ciphertext []byte) ([]byte, error)

	// Compress performs hardware-accelerated compression
	Compress(ctx context.Context, algorithm string, data []byte) ([]byte, error)

	// Decompress performs hardware-accelerated decompression
	Decompress(ctx context.Context, algorithm string, data []byte) ([]byte, error)

	// GetMetrics returns backend-specific metrics
	GetMetrics() map[string]interface{}

	// HealthCheck performs a health check
	HealthCheck() error
}

// NewService creates a new BlueField DPU service.
func NewService(config *Config, backend DPUBackend) (*Service, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if backend == nil {
		backend = NewSimulatedBackend()
	}

	s := &Service{
		config:   config,
		services: make(map[OffloadType]*OffloadService),
		metrics:  &Metrics{},
		backend:  backend,
	}

	// Initialize the backend
	err := s.backend.Init()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize DPU backend: %w", err)
	}

	// Detect and select DPU
	dpus, err := s.backend.DetectDPUs()
	if err != nil {
		return nil, fmt.Errorf("failed to detect DPUs: %w", err)
	}

	if len(dpus) == 0 {
		return nil, ErrDPUNotAvailable
	}

	// Select DPU
	index := config.DeviceIndex
	if index < 0 {
		index = 0 // Auto-select first available
	}

	if index >= len(dpus) {
		return nil, fmt.Errorf("DPU index %d out of range (available: %d)", index, len(dpus))
	}

	dpu, err := s.backend.SelectDPU(index)
	if err != nil {
		return nil, fmt.Errorf("failed to select DPU: %w", err)
	}

	s.dpu = dpu

	// Start configured offload services
	for _, offload := range config.Offloads {
		err := s.startOffloadService(offload)
		if err != nil {
			// Log warning but continue with other offloads
			continue
		}
	}

	// Start health monitoring
	if config.HealthCheckInterval > 0 {
		s.startHealthMonitor()
	}

	return s, nil
}

// startOffloadService starts a specific offload service.
func (s *Service) startOffloadService(offloadType OffloadType) error {
	var serviceConfig interface{}

	switch offloadType {
	case OffloadStorage:
		if s.config.StorageOffload == nil || !s.config.StorageOffload.Enable {
			return nil
		}

		serviceConfig = s.config.StorageOffload
	case OffloadCrypto:
		if s.config.CryptoOffload == nil || !s.config.CryptoOffload.Enable {
			return nil
		}

		serviceConfig = s.config.CryptoOffload
	case OffloadCompress:
		if s.config.CompressOffload == nil || !s.config.CompressOffload.Enable {
			return nil
		}

		serviceConfig = s.config.CompressOffload
	case OffloadNetwork:
		if s.config.NetworkOffload == nil || !s.config.NetworkOffload.Enable {
			return nil
		}

		serviceConfig = s.config.NetworkOffload
	default:
		return ErrOffloadNotSupported
	}

	err := s.backend.StartService(offloadType, serviceConfig)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.services[offloadType] = &OffloadService{
		Type:      offloadType,
		Name:      string(offloadType) + "-offload",
		Status:    "running",
		StartTime: time.Now(),
		Config:    serviceConfig,
	}
	s.mu.Unlock()

	return nil
}

// startHealthMonitor starts the health monitoring goroutine.
func (s *Service) startHealthMonitor() {
	s.healthCtx, s.healthCancel = context.WithCancel(context.Background())
	s.healthWg.Add(1)

	go func() {
		defer s.healthWg.Done()

		ticker := time.NewTicker(s.config.HealthCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-s.healthCtx.Done():
				return
			case <-ticker.C:
				err := s.backend.HealthCheck()
				if err != nil {
					atomic.AddInt64(&s.metrics.HealthFails, 1)
				}
			}
		}
	}()
}

// GetDPUInfo returns information about the selected DPU.
func (s *Service) GetDPUInfo() *DPUInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.dpu
}

// GetServices returns all running offload services.
func (s *Service) GetServices() []*OffloadService {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*OffloadService, 0, len(s.services))
	for _, svc := range s.services {
		result = append(result, svc)
	}

	return result
}

// IsOffloadAvailable checks if a specific offload type is available.
func (s *Service) IsOffloadAvailable(offloadType OffloadType) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.dpu == nil {
		return false
	}

	for _, cap := range s.dpu.Capabilities {
		if cap == offloadType {
			_, ok := s.services[offloadType]
			return ok
		}
	}

	return false
}

// cryptoOperation is a function type for encryption/decryption operations.
type cryptoOperation func(ctx context.Context, algorithm string, key, data []byte) ([]byte, error)

// performCryptoOp is a generic helper for encryption/decryption operations.
func (s *Service) performCryptoOp(
	ctx context.Context,
	algorithm string,
	key, data []byte,
	backendOp cryptoOperation,
	fallbackOp func(algorithm string, key, data []byte) ([]byte, error),
) ([]byte, error) {
	if s.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	if !s.IsOffloadAvailable(OffloadCrypto) {
		if s.config.FallbackOnFailure {
			atomic.AddInt64(&s.metrics.Fallbacks, 1)

			return fallbackOp(algorithm, key, data)
		}

		return nil, ErrOffloadNotSupported
	}

	start := time.Now()

	result, err := backendOp(ctx, algorithm, key, data)
	if err != nil {
		atomic.AddInt64(&s.metrics.Errors, 1)

		if s.config.FallbackOnFailure {
			atomic.AddInt64(&s.metrics.Fallbacks, 1)

			return fallbackOp(algorithm, key, data)
		}

		return nil, err
	}

	atomic.AddInt64(&s.metrics.CryptoOps, 1)
	atomic.AddInt64(&s.metrics.CryptoBytes, int64(len(data)))
	atomic.StoreInt64(&s.metrics.AvgCryptoLatency, time.Since(start).Nanoseconds())

	return result, nil
}

// Encrypt performs hardware-accelerated encryption.
func (s *Service) Encrypt(ctx context.Context, algorithm string, key, plaintext []byte) ([]byte, error) {
	return s.performCryptoOp(ctx, algorithm, key, plaintext, s.backend.Encrypt, s.softwareEncrypt)
}

// Decrypt performs hardware-accelerated decryption.
func (s *Service) Decrypt(ctx context.Context, algorithm string, key, ciphertext []byte) ([]byte, error) {
	return s.performCryptoOp(ctx, algorithm, key, ciphertext, s.backend.Decrypt, s.softwareDecrypt)
}

// Compress performs hardware-accelerated compression.
func (s *Service) Compress(ctx context.Context, algorithm string, data []byte) ([]byte, error) {
	if s.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	// Check minimum size
	if s.config.CompressOffload != nil && int64(len(data)) < s.config.CompressOffload.MinSize {
		atomic.AddInt64(&s.metrics.Fallbacks, 1)
		return s.softwareCompress(algorithm, data)
	}

	if !s.IsOffloadAvailable(OffloadCompress) {
		if s.config.FallbackOnFailure {
			atomic.AddInt64(&s.metrics.Fallbacks, 1)
			return s.softwareCompress(algorithm, data)
		}

		return nil, ErrOffloadNotSupported
	}

	start := time.Now()

	result, err := s.backend.Compress(ctx, algorithm, data)
	if err != nil {
		atomic.AddInt64(&s.metrics.Errors, 1)

		if s.config.FallbackOnFailure {
			atomic.AddInt64(&s.metrics.Fallbacks, 1)
			return s.softwareCompress(algorithm, data)
		}

		return nil, err
	}

	atomic.AddInt64(&s.metrics.CompressOps, 1)
	atomic.AddInt64(&s.metrics.CompressBytes, int64(len(data)))
	atomic.StoreInt64(&s.metrics.AvgCompressLatency, time.Since(start).Nanoseconds())

	return result, nil
}

// Decompress performs hardware-accelerated decompression.
func (s *Service) Decompress(ctx context.Context, algorithm string, data []byte) ([]byte, error) {
	if s.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	if !s.IsOffloadAvailable(OffloadCompress) {
		if s.config.FallbackOnFailure {
			atomic.AddInt64(&s.metrics.Fallbacks, 1)
			return s.softwareDecompress(algorithm, data)
		}

		return nil, ErrOffloadNotSupported
	}

	start := time.Now()

	result, err := s.backend.Decompress(ctx, algorithm, data)
	if err != nil {
		atomic.AddInt64(&s.metrics.Errors, 1)

		if s.config.FallbackOnFailure {
			atomic.AddInt64(&s.metrics.Fallbacks, 1)
			return s.softwareDecompress(algorithm, data)
		}

		return nil, err
	}

	atomic.AddInt64(&s.metrics.CompressOps, 1)
	atomic.AddInt64(&s.metrics.CompressBytes, int64(len(result)))
	atomic.StoreInt64(&s.metrics.AvgCompressLatency, time.Since(start).Nanoseconds())

	return result, nil
}

// Software fallback implementations.
func (s *Service) softwareEncrypt(algorithm string, key, plaintext []byte) ([]byte, error) {
	// Placeholder for software encryption fallback
	// In real implementation, use crypto/aes, crypto/cipher
	result := make([]byte, len(plaintext))
	copy(result, plaintext)

	return result, nil
}

func (s *Service) softwareDecrypt(algorithm string, key, ciphertext []byte) ([]byte, error) {
	// Placeholder for software decryption fallback
	result := make([]byte, len(ciphertext))
	copy(result, ciphertext)

	return result, nil
}

func (s *Service) softwareCompress(_algorithm string, data []byte) ([]byte, error) {
	// Placeholder for software compression fallback
	// In real implementation, use compress/flate, lz4, etc.
	result := make([]byte, len(data))
	copy(result, data)

	return result, nil
}

func (s *Service) softwareDecompress(_algorithm string, data []byte) ([]byte, error) {
	// Placeholder for software decompression fallback
	result := make([]byte, len(data))
	copy(result, data)

	return result, nil
}

// GetMetrics returns current performance metrics.
func (s *Service) GetMetrics() *Metrics {
	return &Metrics{
		StorageOps:         atomic.LoadInt64(&s.metrics.StorageOps),
		CryptoOps:          atomic.LoadInt64(&s.metrics.CryptoOps),
		CompressOps:        atomic.LoadInt64(&s.metrics.CompressOps),
		NetworkOps:         atomic.LoadInt64(&s.metrics.NetworkOps),
		StorageBytes:       atomic.LoadInt64(&s.metrics.StorageBytes),
		CryptoBytes:        atomic.LoadInt64(&s.metrics.CryptoBytes),
		CompressBytes:      atomic.LoadInt64(&s.metrics.CompressBytes),
		NetworkBytes:       atomic.LoadInt64(&s.metrics.NetworkBytes),
		AvgStorageLatency:  atomic.LoadInt64(&s.metrics.AvgStorageLatency),
		AvgCryptoLatency:   atomic.LoadInt64(&s.metrics.AvgCryptoLatency),
		AvgCompressLatency: atomic.LoadInt64(&s.metrics.AvgCompressLatency),
		Errors:             atomic.LoadInt64(&s.metrics.Errors),
		Fallbacks:          atomic.LoadInt64(&s.metrics.Fallbacks),
		HealthFails:        atomic.LoadInt64(&s.metrics.HealthFails),
	}
}

// GetStatus returns the overall DPU status.
func (s *Service) GetStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	services := make([]map[string]interface{}, 0, len(s.services))
	for _, svc := range s.services {
		services = append(services, map[string]interface{}{
			"type":       svc.Type,
			"name":       svc.Name,
			"status":     svc.Status,
			"start_time": svc.StartTime,
		})
	}

	status := map[string]interface{}{
		"available": s.dpu != nil,
		"services":  services,
		"metrics":   s.GetMetrics(),
	}

	if s.dpu != nil {
		status["dpu"] = map[string]interface{}{
			"index":            s.dpu.Index,
			"model":            s.dpu.Model,
			"firmware_version": s.dpu.FirmwareVersion,
			"doca_version":     s.dpu.DOCAVersion,
			"arm_cores":        s.dpu.ARMCores,
			"memory_gb":        s.dpu.MemoryGB,
			"capabilities":     s.dpu.Capabilities,
			"status":           s.dpu.Status,
			"temperature":      s.dpu.Temperature,
			"power_watts":      s.dpu.PowerWatts,
		}
	}

	return status
}

// IsAvailable checks if DPU is available.
func (s *Service) IsAvailable() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.dpu != nil
}

// Close shuts down the DPU service.
func (s *Service) Close() error {
	if s.closed.Swap(true) {
		return ErrAlreadyClosed
	}

	// Stop health monitor
	if s.healthCancel != nil {
		s.healthCancel()
		s.healthWg.Wait()
	}

	// Stop all services
	s.mu.Lock()

	for offloadType := range s.services {
		stopErr := s.backend.StopService(offloadType)
		if stopErr != nil {
			log.Error().
				Err(stopErr).
				Str("offload_type", string(offloadType)).
				Msg("failed to stop DPU offload service - resources may not be properly released")
		}
	}

	s.services = nil
	s.mu.Unlock()

	return s.backend.Close()
}
