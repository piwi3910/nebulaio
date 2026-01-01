// Package tiering provides intelligent data lifecycle management for NebulaIO.
//
// The tiering service automatically moves objects between storage tiers based
// on configurable policies:
//
//   - Hot tier: NVMe/SSD for active, frequently accessed data
//   - Warm tier: SSD/HDD for moderately accessed data
//   - Cold tier: HDD/NAS for infrequently accessed data
//   - Archive tier: Tape/Glacier for long-term retention
//
// Features:
//   - Age-based transitions (move after N days without access)
//   - Access-pattern transitions (move based on read frequency)
//   - Capacity-based transitions (move when tier reaches capacity)
//   - ML-based predictive tiering (optional)
//   - S3 Intelligent-Tiering compatibility
//
// The service runs periodic scans to identify objects matching transition
// policies and performs background data movement with minimal I/O impact.
package tiering

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// ServiceConfig configures the tiering service.
type ServiceConfig struct {
	Policies            []Policy      `json:"policies,omitempty" yaml:"policies,omitempty"`
	Cache               CacheConfig   `json:"cache" yaml:"cache"`
	ScanInterval        time.Duration `json:"scanInterval,omitempty" yaml:"scanInterval,omitempty"`
	TransitionBatchSize int           `json:"transitionBatchSize,omitempty" yaml:"transitionBatchSize,omitempty"`
	TransitionWorkers   int           `json:"transitionWorkers,omitempty" yaml:"transitionWorkers,omitempty"`
	Enabled             bool          `json:"enabled" yaml:"enabled"`
}

// DefaultServiceConfig returns sensible defaults.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		Enabled:             true,
		Cache:               DefaultCacheConfig(),
		Policies:            DefaultPolicies(),
		ScanInterval:        1 * time.Hour,
		TransitionBatchSize: 100,
		TransitionWorkers:   4,
	}
}

// Service provides tiered storage with hot cache and cold storage.
type Service struct {
	lastScanTime        time.Time
	primary             backend.Backend
	stopChan            chan struct{}
	coldStorage         *ColdStorageManager
	evaluator           *PolicyEvaluator
	scanTicker          *time.Ticker
	transitionCh        chan transitionJob
	cache               *Cache
	config              ServiceConfig
	workerWg            sync.WaitGroup
	objectsTransitioned int64
	bytesTransitioned   int64
	lastScanDuration    time.Duration
	lastScanErrors      int
	mu                  sync.RWMutex
}

type transitionJob struct {
	result *EvaluateResult
	info   ObjectInfo
}

// NewService creates a new tiering service.
func NewService(config ServiceConfig, primary backend.Backend, cacheBackend backend.Backend) *Service {
	// Create cache
	var cache *Cache
	if config.Cache.MaxSize > 0 {
		cache = NewCache(config.Cache, cacheBackend)
	}

	// Create policy evaluator
	var evaluator *PolicyEvaluator
	if len(config.Policies) > 0 {
		evaluator = NewPolicyEvaluator(config.Policies)
	}

	s := &Service{
		config:       config,
		cache:        cache,
		primary:      primary,
		coldStorage:  NewColdStorageManager(),
		evaluator:    evaluator,
		stopChan:     make(chan struct{}),
		transitionCh: make(chan transitionJob, config.TransitionBatchSize),
	}

	return s
}

// Start starts the tiering service background workers.
func (s *Service) Start() error {
	if !s.config.Enabled {
		return nil
	}

	// Start transition workers
	for range s.config.TransitionWorkers {
		s.workerWg.Add(1)

		go s.transitionWorker()
	}

	// Start scan ticker
	if s.config.ScanInterval > 0 {
		s.scanTicker = time.NewTicker(s.config.ScanInterval)
		s.workerWg.Add(1)

		go s.scanLoop()
	}

	return nil
}

// Stop stops the tiering service.
func (s *Service) Stop() error {
	close(s.stopChan)

	if s.scanTicker != nil {
		s.scanTicker.Stop()
	}

	close(s.transitionCh)
	s.workerWg.Wait()

	if s.cache != nil {
		err := s.cache.Close()
		if err != nil {
			return err
		}
	}

	return s.coldStorage.Close()
}

// RegisterColdStorage adds a cold storage backend.
func (s *Service) RegisterColdStorage(storage *ColdStorage) {
	s.coldStorage.Register(storage)
}

// GetObject retrieves an object, checking cache first.
func (s *Service) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	cacheKey := bucket + "/" + key

	// Try cache first
	if s.cache != nil {
		if entry, ok := s.cache.Get(ctx, cacheKey); ok {
			return io.NopCloser(&bytesReader{data: entry.Data}), nil
		}
	}

	// Try primary storage
	reader, err := s.primary.GetObject(ctx, bucket, key)
	if err == nil {
		// Cache for future requests
		if s.cache != nil {
			data, readErr := io.ReadAll(reader)

			_ = reader.Close() // Close after reading, ignore error
			if readErr == nil {
				_ = s.cache.Put(ctx, cacheKey, data, "", "")
				return io.NopCloser(&bytesReader{data: data}), nil
			}
		}

		return reader, nil
	}

	// Try cold storage
	for _, cold := range s.coldStorage.List() {
		coldReader, coldErr := cold.GetObject(ctx, bucket, key)
		if coldErr == nil {
			return coldReader, nil
		}
	}

	return nil, err
}

// PutObject stores an object.
func (s *Service) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	// Determine target tier based on policies
	objInfo := ObjectInfo{
		Bucket:      bucket,
		Key:         key,
		Size:        size,
		CurrentTier: TierHot,
		CreatedAt:   time.Now(),
		ModifiedAt:  time.Now(),
	}

	if s.evaluator != nil {
		result := s.evaluator.Evaluate(objInfo)
		if result.ShouldTransition {
			switch result.TargetTier {
			case TierCold, TierArchive:
				if cold := s.coldStorage.GetByTier(ctx, result.TargetTier); cold != nil {
					return cold.PutObject(ctx, bucket, key, reader, size)
				}
			}
		}
	}

	// Write-through to cache if enabled
	if s.cache != nil && s.config.Cache.WriteThrough && size <= s.config.Cache.MaxSize/10 {
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read object data: %w", err)
		}

		// Store in cache
		cacheKey := bucket + "/" + key
		_ = s.cache.Put(ctx, cacheKey, data, "", "")

		// Write to backend
		return s.primary.PutObject(ctx, bucket, key, bytes.NewReader(data), size)
	}

	return s.primary.PutObject(ctx, bucket, key, reader, size)
}

// DeleteObject removes an object from all tiers.
func (s *Service) DeleteObject(ctx context.Context, bucket, key string) error {
	cacheKey := bucket + "/" + key

	// Delete from cache
	if s.cache != nil {
		_ = s.cache.Delete(ctx, cacheKey)
	}

	// Delete from primary
	err := s.primary.DeleteObject(ctx, bucket, key)

	// Delete from cold storage (best effort)
	for _, cold := range s.coldStorage.List() {
		_ = cold.DeleteObject(ctx, bucket, key)
	}

	return err
}

// ObjectExists checks if an object exists in any tier.
func (s *Service) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	// Check cache first
	if s.cache != nil {
		cacheKey := bucket + "/" + key
		if s.cache.Has(ctx, cacheKey) {
			return true, nil
		}
	}

	// Try primary
	exists, err := s.primary.ObjectExists(ctx, bucket, key)
	if err == nil && exists {
		return true, nil
	}

	// Try cold storage
	for _, cold := range s.coldStorage.List() {
		coldExists, coldErr := cold.ObjectExists(ctx, bucket, key)
		if coldErr == nil && coldExists {
			return true, nil
		}
	}

	return false, err
}

// TransitionObject moves an object to a different tier.
func (s *Service) TransitionObject(ctx context.Context, bucket, key string, targetTier TierType) error {
	// Get from current location
	reader, err := s.GetObject(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("failed to get object: %w", err)
	}

	defer func() { _ = reader.Close() }()

	// Read data
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read object: %w", err)
	}

	// Store in target tier
	switch targetTier {
	case TierHot:
		// Move to cache
		if s.cache != nil {
			cacheKey := bucket + "/" + key
			return s.cache.Put(ctx, cacheKey, data, "", "")
		}
	case TierWarm:
		// Move to primary storage
		_, err = s.primary.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)))
		return err
	case TierCold, TierArchive:
		// Move to cold storage
		cold := s.coldStorage.GetByTier(ctx, targetTier)
		if cold == nil {
			return fmt.Errorf("no cold storage available for tier %s", targetTier)
		}

		_, err = cold.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return err
		}
		// Remove from primary
		return s.primary.DeleteObject(ctx, bucket, key)
	}

	return nil
}

// scanLoop periodically scans for tiering candidates.
func (s *Service) scanLoop() {
	defer s.workerWg.Done()

	for {
		select {
		case <-s.stopChan:
			return
		case <-s.scanTicker.C:
			// INTENTIONAL: Using context.Background() for background periodic scan.
			// This is a fire-and-forget operation triggered by a timer, not a request.
			// The scan should complete even if no request context is available.
			s.runScan(context.Background())
		}
	}
}

// runScan performs a tiering scan.
func (s *Service) runScan(ctx context.Context) {
	if s.evaluator == nil {
		return
	}

	startTime := time.Now()
	errorCount := 0

	// Get storage info for scan
	info, err := s.primary.GetStorageInfo(ctx)
	if err != nil {
		errorCount++
		s.updateScanStats(startTime, errorCount)

		return
	}

	// This is a simplified scan - in production you'd iterate through
	// all objects with proper pagination
	_ = info // Would use object count for progress

	s.updateScanStats(startTime, errorCount)
}

// transitionWorker processes transition jobs.
func (s *Service) transitionWorker() {
	defer s.workerWg.Done()

	for job := range s.transitionCh {
		// INTENTIONAL: Using context.Background() for background worker.
		// Transition jobs are queued internally and processed asynchronously.
		// The timeout provides the cancellation mechanism for long-running operations.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		err := s.TransitionObject(ctx, job.info.Bucket, job.info.Key, job.result.TargetTier)

		cancel()

		if err == nil {
			s.mu.Lock()
			s.objectsTransitioned++
			s.bytesTransitioned += job.info.Size
			s.mu.Unlock()
		}
	}
}

// updateScanStats updates scan statistics.
func (s *Service) updateScanStats(startTime time.Time, errorCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastScanTime = startTime
	s.lastScanDuration = time.Since(startTime)
	s.lastScanErrors = errorCount
}

// ServiceStats returns tiering service statistics.
type ServiceStats struct {
	LastScanTime        time.Time   `json:"lastScanTime"`
	CacheStats          *CacheStats `json:"cacheStats,omitempty"`
	LastScanDuration    string      `json:"lastScanDuration"`
	ObjectsTransitioned int64       `json:"objectsTransitioned"`
	BytesTransitioned   int64       `json:"bytesTransitioned"`
	LastScanErrors      int         `json:"lastScanErrors"`
	ColdStorageCount    int         `json:"coldStorageCount"`
}

// Stats returns service statistics.
func (s *Service) Stats() ServiceStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := ServiceStats{
		ObjectsTransitioned: s.objectsTransitioned,
		BytesTransitioned:   s.bytesTransitioned,
		LastScanTime:        s.lastScanTime,
		LastScanDuration:    s.lastScanDuration.String(),
		LastScanErrors:      s.lastScanErrors,
		ColdStorageCount:    len(s.coldStorage.List()),
	}

	if s.cache != nil {
		cacheStats := s.cache.Stats()
		stats.CacheStats = &cacheStats
	}

	return stats
}

// Cache returns the hot cache (for direct access).
func (s *Service) Cache() *Cache {
	return s.cache
}

// ColdStorage returns the cold storage manager.
func (s *Service) ColdStorage() *ColdStorageManager {
	return s.coldStorage
}

// UpdatePolicies updates the tiering policies.
func (s *Service) UpdatePolicies(policies []Policy) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.Policies = policies
	s.evaluator = NewPolicyEvaluator(policies)
}

// GetPolicies returns the current policies.
func (s *Service) GetPolicies() []Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.config.Policies
}
