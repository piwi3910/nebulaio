package dpu

// a simulated BlueField DPU backend for testing.

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"
)

// SimulatedBackend provides a simulated BlueField DPU backend for testing.
type SimulatedBackend struct {
	selectedDPU  *DPUInfo
	services     map[OffloadType]bool
	dpus         []*DPUInfo
	latencyNs    int64
	_failureRate float64
	mu           sync.RWMutex
	initialized  bool
}

// NewSimulatedBackend creates a new simulated DPU backend.
func NewSimulatedBackend() *SimulatedBackend {
	return &SimulatedBackend{
		services:  make(map[OffloadType]bool),
		latencyNs: 10000, // 10μs simulated latency
	}
}

// Init initializes the simulated backend.
func (b *SimulatedBackend) Init() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.initialized {
		return nil
	}

	// Create simulated DPUs
	b.dpus = []*DPUInfo{
		{
			Index:           0,
			Model:           "BlueField-3 DPU",
			SerialNumber:    "BF3-SIM-0001",
			FirmwareVersion: "24.10.0.1234",
			DOCAVersion:     "2.8.0",
			ARMCores:        16,
			ARMFrequencyMHz: 3000,
			MemoryGB:        32,
			NetworkPorts:    2,
			PortSpeedGbps:   400,
			Capabilities: []OffloadType{
				OffloadStorage,
				OffloadNetwork,
				OffloadCrypto,
				OffloadCompress,
				OffloadRDMA,
				OffloadRegex,
			},
			Status:      "ready",
			Temperature: 45.0,
			PowerWatts:  75.0,
		},
		{
			Index:           1,
			Model:           "BlueField-2 DPU",
			SerialNumber:    "BF2-SIM-0001",
			FirmwareVersion: "24.04.0.5678",
			DOCAVersion:     "2.6.0",
			ARMCores:        8,
			ARMFrequencyMHz: 2500,
			MemoryGB:        16,
			NetworkPorts:    2,
			PortSpeedGbps:   200,
			Capabilities: []OffloadType{
				OffloadStorage,
				OffloadNetwork,
				OffloadCrypto,
				OffloadCompress,
			},
			Status:      "ready",
			Temperature: 42.0,
			PowerWatts:  50.0,
		},
	}

	b.initialized = true

	return nil
}

// Close shuts down the simulated backend.
func (b *SimulatedBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.services = make(map[OffloadType]bool)
	b.selectedDPU = nil
	b.initialized = false

	return nil
}

// DetectDPUs returns simulated DPU information.
func (b *SimulatedBackend) DetectDPUs() ([]*DPUInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.initialized {
		return nil, ErrDPUNotInitialized
	}

	result := make([]*DPUInfo, len(b.dpus))
	copy(result, b.dpus)

	return result, nil
}

// SelectDPU selects a specific DPU.
func (b *SimulatedBackend) SelectDPU(index int) (*DPUInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.initialized {
		return nil, ErrDPUNotInitialized
	}

	if index >= len(b.dpus) {
		return nil, fmt.Errorf("DPU index %d out of range", index)
	}

	b.selectedDPU = b.dpus[index]

	return b.selectedDPU, nil
}

// StartService starts an offload service.
func (b *SimulatedBackend) StartService(offloadType OffloadType, config interface{}) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.initialized {
		return ErrDPUNotInitialized
	}

	if b.selectedDPU == nil {
		return ErrDPUNotAvailable
	}

	// Check if DPU supports this offload
	if !slices.Contains(b.selectedDPU.Capabilities, offloadType) {
		return ErrOffloadNotSupported
	}

	if b.services[offloadType] {
		return ErrServiceRunning
	}

	b.services[offloadType] = true

	return nil
}

// StopService stops an offload service.
func (b *SimulatedBackend) StopService(offloadType OffloadType) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.initialized {
		return ErrDPUNotInitialized
	}

	if !b.services[offloadType] {
		return ErrServiceStopped
	}

	delete(b.services, offloadType)

	return nil
}

// GetServiceStatus returns the status of an offload service.
func (b *SimulatedBackend) GetServiceStatus(offloadType OffloadType) (*OffloadService, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.initialized {
		return nil, ErrDPUNotInitialized
	}

	running, ok := b.services[offloadType]
	if !ok || !running {
		return nil, ErrServiceNotFound
	}

	return &OffloadService{
		Type:      offloadType,
		Name:      string(offloadType) + "-offload",
		Status:    "running",
		StartTime: time.Now().Add(-time.Hour), // Simulate started an hour ago
	}, nil
}

// Encrypt performs simulated hardware-accelerated encryption.
func (b *SimulatedBackend) Encrypt(ctx context.Context, algorithm string, key, plaintext []byte) ([]byte, error) {
	b.mu.RLock()

	if !b.initialized {
		b.mu.RUnlock()
		return nil, ErrDPUNotInitialized
	}

	if !b.services[OffloadCrypto] {
		b.mu.RUnlock()
		return nil, ErrOffloadNotSupported
	}

	latency := b.latencyNs
	b.mu.RUnlock()

	// Simulate processing latency
	time.Sleep(time.Duration(latency))

	// Perform actual encryption for realistic simulation
	return b.aesGCMEncrypt(key, plaintext)
}

// Decrypt performs simulated hardware-accelerated decryption.
func (b *SimulatedBackend) Decrypt(ctx context.Context, algorithm string, key, ciphertext []byte) ([]byte, error) {
	b.mu.RLock()

	if !b.initialized {
		b.mu.RUnlock()
		return nil, ErrDPUNotInitialized
	}

	if !b.services[OffloadCrypto] {
		b.mu.RUnlock()
		return nil, ErrOffloadNotSupported
	}

	latency := b.latencyNs
	b.mu.RUnlock()

	// Simulate processing latency
	time.Sleep(time.Duration(latency))

	// Perform actual decryption
	return b.aesGCMDecrypt(key, ciphertext)
}

// aesGCMEncrypt performs AES-GCM encryption.
func (b *SimulatedBackend) aesGCMEncrypt(key, plaintext []byte) ([]byte, error) {
	// Ensure key is 16, 24, or 32 bytes
	keyLen := len(key)
	if keyLen < 16 {
		paddedKey := make([]byte, 16)
		copy(paddedKey, key)
		key = paddedKey
	} else if keyLen > 16 && keyLen < 24 {
		paddedKey := make([]byte, 24)
		copy(paddedKey, key)
		key = paddedKey
	} else if keyLen > 24 && keyLen < 32 {
		paddedKey := make([]byte, 32)
		copy(paddedKey, key)
		key = paddedKey
	} else if keyLen > 32 {
		key = key[:32]
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	return ciphertext, nil
}

// aesGCMDecrypt performs AES-GCM decryption.
func (b *SimulatedBackend) aesGCMDecrypt(key, ciphertext []byte) ([]byte, error) {
	// Ensure key is 16, 24, or 32 bytes
	keyLen := len(key)
	if keyLen < 16 {
		paddedKey := make([]byte, 16)
		copy(paddedKey, key)
		key = paddedKey
	} else if keyLen > 16 && keyLen < 24 {
		paddedKey := make([]byte, 24)
		copy(paddedKey, key)
		key = paddedKey
	} else if keyLen > 24 && keyLen < 32 {
		paddedKey := make([]byte, 32)
		copy(paddedKey, key)
		key = paddedKey
	} else if keyLen > 32 {
		key = key[:32]
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:gcm.NonceSize()]
	ciphertext = ciphertext[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// Compress performs simulated hardware-accelerated compression.
func (b *SimulatedBackend) Compress(ctx context.Context, algorithm string, data []byte) ([]byte, error) {
	b.mu.RLock()

	if !b.initialized {
		b.mu.RUnlock()
		return nil, ErrDPUNotInitialized
	}

	if !b.services[OffloadCompress] {
		b.mu.RUnlock()
		return nil, ErrOffloadNotSupported
	}

	latency := b.latencyNs
	b.mu.RUnlock()

	// Simulate processing latency
	time.Sleep(time.Duration(latency))

	// Perform actual compression using deflate
	var buf bytes.Buffer

	w, err := flate.NewWriter(&buf, flate.BestSpeed)
	if err != nil {
		return nil, err
	}

	_, err = w.Write(data)
	if err != nil {
		return nil, err
	}

	err = w.Close()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Decompress performs simulated hardware-accelerated decompression.
func (b *SimulatedBackend) Decompress(ctx context.Context, algorithm string, data []byte) ([]byte, error) {
	b.mu.RLock()

	if !b.initialized {
		b.mu.RUnlock()
		return nil, ErrDPUNotInitialized
	}

	if !b.services[OffloadCompress] {
		b.mu.RUnlock()
		return nil, ErrOffloadNotSupported
	}

	latency := b.latencyNs
	b.mu.RUnlock()

	// Simulate processing latency
	time.Sleep(time.Duration(latency))

	// Perform actual decompression
	r := flate.NewReader(bytes.NewReader(data))

	defer func() { _ = r.Close() }()

	// Limit decompression size to prevent decompression bombs (100MB max)
	const maxDecompressSize = 100 * 1024 * 1024

	var buf bytes.Buffer

	_, err := io.Copy(&buf, io.LimitReader(r, maxDecompressSize))
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GetMetrics returns simulated backend metrics.
func (b *SimulatedBackend) GetMetrics() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	activeServices := make([]string, 0, len(b.services))
	for svc := range b.services {
		activeServices = append(activeServices, string(svc))
	}

	return map[string]interface{}{
		"simulated":       true,
		"dpu_count":       len(b.dpus),
		"selected_dpu":    b.selectedDPU != nil,
		"active_services": activeServices,
		"latency_ns":      b.latencyNs,
	}
}

// HealthCheck performs a simulated health check.
func (b *SimulatedBackend) HealthCheck() error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.initialized {
		return ErrDPUNotInitialized
	}

	if b.selectedDPU == nil {
		return ErrDPUNotAvailable
	}

	return nil
}

// SetSimulatedLatency sets the simulated processing latency.
func (b *SimulatedBackend) SetSimulatedLatency(latencyNs int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.latencyNs = latencyNs
}

// SetDPUs allows setting custom DPU configurations for testing.
func (b *SimulatedBackend) SetDPUs(dpus []*DPUInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dpus = dpus
}
