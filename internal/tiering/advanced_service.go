package tiering

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
	"github.com/rs/zerolog/log"
)

// AdvancedService provides comprehensive tiered storage with policy management
type AdvancedService struct {
	// Core components
	config       AdvancedServiceConfig
	policyStore  PolicyStore
	policyEngine *PolicyEngine
	accessStats  *AccessTracker

	// Storage tiers
	hotStorage    backend.Backend
	warmStorage   backend.Backend
	coldManager   *ColdStorageManager
	tierManager   *DefaultTierManager
	cache         *Cache

	// S3 compatibility
	s3Adapter *S3LifecycleAdapter

	// Background workers
	stopChan chan struct{}
	wg       sync.WaitGroup

	// State
	mu      sync.RWMutex
	running bool
}

// AdvancedServiceConfig configures the advanced tiering service
type AdvancedServiceConfig struct {
	// NodeID for distributed execution
	NodeID string

	// ClusterNodes for distributed policies
	ClusterNodes []string

	// Policy configuration
	PolicyConfigPath string

	// Cache configuration
	Cache CacheConfig

	// Access tracking configuration
	AccessTracking AccessTrackerConfig

	// Enable real-time policies
	EnableRealtime bool

	// Enable scheduled policies
	EnableScheduled bool

	// Enable threshold policies
	EnableThreshold bool

	// Threshold check interval
	ThresholdCheckInterval time.Duration
}

// DefaultAdvancedServiceConfig returns default configuration
func DefaultAdvancedServiceConfig() AdvancedServiceConfig {
	return AdvancedServiceConfig{
		NodeID:                 "node-1",
		ClusterNodes:           []string{"node-1"},
		Cache:                  DefaultCacheConfig(),
		AccessTracking:         DefaultAccessTrackerConfig(),
		EnableRealtime:         true,
		EnableScheduled:        true,
		EnableThreshold:        true,
		ThresholdCheckInterval: 1 * time.Minute,
	}
}

// NewAdvancedService creates a new advanced tiering service
func NewAdvancedService(
	config AdvancedServiceConfig,
	hotStorage, warmStorage backend.Backend,
	coldManager *ColdStorageManager,
) (*AdvancedService, error) {
	// Create policy store
	policyStore, err := NewHybridPolicyStore(HybridPolicyStoreConfig{
		ConfigPath:    config.PolicyConfigPath,
		MetadataStore: NewInMemoryMetadataStore(),
		CacheTTL:      5 * time.Minute,
		SyncInterval:  1 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create policy store: %w", err)
	}

	// Create access tracker
	accessStats := NewAccessTracker(config.AccessTracking)

	// Create cache
	var cache *Cache
	if config.Cache.MaxSize > 0 {
		cache = NewCache(config.Cache, hotStorage)
	}

	// Create tier manager
	tierManager := &DefaultTierManager{
		hot:    hotStorage,
		warm:   warmStorage,
		cold:   coldManager,
		cache:  cache,
	}

	// Create policy engine
	engineConfig := PolicyEngineConfig{
		NodeID:                 config.NodeID,
		ClusterNodes:           config.ClusterNodes,
		RealtimeWorkers:        4,
		ScheduledWorkers:       2,
		ThresholdCheckInterval: config.ThresholdCheckInterval,
		MaxPendingEvents:       10000,
	}

	policyEngine := NewPolicyEngine(engineConfig, policyStore, tierManager, accessStats)

	service := &AdvancedService{
		config:       config,
		policyStore:  policyStore,
		policyEngine: policyEngine,
		accessStats:  accessStats,
		hotStorage:   hotStorage,
		warmStorage:  warmStorage,
		coldManager:  coldManager,
		tierManager:  tierManager,
		cache:        cache,
		s3Adapter:    NewS3LifecycleAdapter(),
		stopChan:     make(chan struct{}),
	}

	return service, nil
}

// Start starts the advanced tiering service
func (s *AdvancedService) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	log.Info().Msg("Starting advanced tiering service")

	// Start policy engine
	if err := s.policyEngine.Start(); err != nil {
		return fmt.Errorf("failed to start policy engine: %w", err)
	}

	return nil
}

// Stop stops the advanced tiering service
func (s *AdvancedService) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	log.Info().Msg("Stopping advanced tiering service")

	close(s.stopChan)
	s.wg.Wait()

	// Stop components
	if err := s.policyEngine.Stop(); err != nil {
		return err
	}

	if err := s.accessStats.Close(); err != nil {
		return err
	}

	if s.cache != nil {
		if err := s.cache.Close(); err != nil {
			return err
		}
	}

	return nil
}

// GetObject retrieves an object with policy-aware tiering
func (s *AdvancedService) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	// Record access for policies
	s.policyEngine.RecordAccess(bucket, key, "GET", 0)

	// Try cache first
	if s.cache != nil {
		cacheKey := bucket + "/" + key
		if entry, ok := s.cache.Get(ctx, cacheKey); ok {
			return io.NopCloser(&bytesReader{data: entry.Data}), nil
		}
	}

	// Try hot storage
	if s.hotStorage != nil {
		reader, err := s.hotStorage.GetObject(ctx, bucket, key)
		if err == nil {
			// Cache for future requests
			if s.cache != nil {
				data, readErr := io.ReadAll(reader)
				reader.Close()
				if readErr == nil {
					cacheKey := bucket + "/" + key
					_ = s.cache.Put(ctx, cacheKey, data, "", "")
					return io.NopCloser(&bytesReader{data: data}), nil
				}
			}
			return reader, nil
		}
	}

	// Try warm storage
	if s.warmStorage != nil {
		reader, err := s.warmStorage.GetObject(ctx, bucket, key)
		if err == nil {
			return reader, nil
		}
	}

	// Try cold storage
	if s.coldManager != nil {
		for _, cold := range s.coldManager.List() {
			reader, err := cold.GetObject(ctx, bucket, key)
			if err == nil {
				return reader, nil
			}
		}
	}

	return nil, backend.ErrObjectNotFound
}

// PutObject stores an object with policy-aware placement
func (s *AdvancedService) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	// Evaluate policies to determine placement
	obj := ObjectMetadata{
		Bucket:       bucket,
		Key:          key,
		Size:         size,
		CurrentTier:  TierHot,
		StorageClass: StorageClassStandard,
		CreatedAt:    time.Now(),
		ModifiedAt:   time.Now(),
	}

	result, err := s.policyEngine.EvaluateObject(ctx, obj)
	if err != nil {
		log.Warn().Err(err).Msg("Policy evaluation failed, using default tier")
	}

	// Determine target tier from policy or use default
	targetTier := TierHot
	if result != nil && len(result.MatchingPolicies) > 0 {
		match := result.MatchingPolicies[0]
		for _, action := range match.Policy.Actions {
			if action.Type == ActionTransition && action.Transition != nil {
				targetTier = action.Transition.TargetTier
				break
			}
		}
	}

	// Write-through to cache if enabled
	var data []byte
	if s.cache != nil && s.config.Cache.WriteThrough && size <= s.config.Cache.MaxSize/10 {
		data, err = io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read object data: %w", err)
		}

		cacheKey := bucket + "/" + key
		_ = s.cache.Put(ctx, cacheKey, data, "", "")
	}

	// Store in target tier
	var putReader io.Reader
	if data != nil {
		putReader = &bytesReader{data: data}
	} else {
		putReader = reader
	}

	switch targetTier {
	case TierHot:
		if s.hotStorage != nil {
			return s.hotStorage.PutObject(ctx, bucket, key, putReader, size)
		}
	case TierWarm:
		if s.warmStorage != nil {
			return s.warmStorage.PutObject(ctx, bucket, key, putReader, size)
		}
	case TierCold, TierArchive:
		if s.coldManager != nil {
			cold := s.coldManager.GetByTier(targetTier)
			if cold != nil {
				return cold.PutObject(ctx, bucket, key, putReader, size)
			}
		}
	}

	// Fallback to hot storage
	if s.hotStorage != nil {
		return s.hotStorage.PutObject(ctx, bucket, key, putReader, size)
	}

	return nil, fmt.Errorf("no storage available for tier %s", targetTier)
}

// DeleteObject removes an object from all tiers
func (s *AdvancedService) DeleteObject(ctx context.Context, bucket, key string) error {
	// Delete from cache
	if s.cache != nil {
		cacheKey := bucket + "/" + key
		_ = s.cache.Delete(ctx, cacheKey)
	}

	var lastErr error

	// Delete from hot storage
	if s.hotStorage != nil {
		if err := s.hotStorage.DeleteObject(ctx, bucket, key); err != nil && err != backend.ErrObjectNotFound {
			lastErr = err
		}
	}

	// Delete from warm storage
	if s.warmStorage != nil {
		if err := s.warmStorage.DeleteObject(ctx, bucket, key); err != nil && err != backend.ErrObjectNotFound {
			lastErr = err
		}
	}

	// Delete from cold storage
	if s.coldManager != nil {
		for _, cold := range s.coldManager.List() {
			if err := cold.DeleteObject(ctx, bucket, key); err != nil && err != backend.ErrObjectNotFound {
				lastErr = err
			}
		}
	}

	return lastErr
}

// ObjectExists checks if an object exists in any tier
func (s *AdvancedService) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	// Check cache
	if s.cache != nil {
		cacheKey := bucket + "/" + key
		if s.cache.Has(ctx, cacheKey) {
			return true, nil
		}
	}

	// Check hot storage
	if s.hotStorage != nil {
		exists, err := s.hotStorage.ObjectExists(ctx, bucket, key)
		if err == nil && exists {
			return true, nil
		}
	}

	// Check warm storage
	if s.warmStorage != nil {
		exists, err := s.warmStorage.ObjectExists(ctx, bucket, key)
		if err == nil && exists {
			return true, nil
		}
	}

	// Check cold storage
	if s.coldManager != nil {
		for _, cold := range s.coldManager.List() {
			exists, err := cold.ObjectExists(ctx, bucket, key)
			if err == nil && exists {
				return true, nil
			}
		}
	}

	return false, nil
}

// CreatePolicy creates a new policy
func (s *AdvancedService) CreatePolicy(ctx context.Context, policy *AdvancedPolicy) error {
	return s.policyStore.Create(ctx, policy)
}

// GetPolicy retrieves a policy
func (s *AdvancedService) GetPolicy(ctx context.Context, id string) (*AdvancedPolicy, error) {
	return s.policyStore.Get(ctx, id)
}

// UpdatePolicy updates a policy
func (s *AdvancedService) UpdatePolicy(ctx context.Context, policy *AdvancedPolicy) error {
	return s.policyStore.Update(ctx, policy)
}

// DeletePolicy removes a policy
func (s *AdvancedService) DeletePolicy(ctx context.Context, id string) error {
	return s.policyStore.Delete(ctx, id)
}

// ListPolicies returns all policies
func (s *AdvancedService) ListPolicies(ctx context.Context) ([]*AdvancedPolicy, error) {
	return s.policyStore.List(ctx)
}

// ListPoliciesByType returns policies of a specific type
func (s *AdvancedService) ListPoliciesByType(ctx context.Context, policyType PolicyType) ([]*AdvancedPolicy, error) {
	return s.policyStore.ListByType(ctx, policyType)
}

// ListPoliciesByScope returns policies of a specific scope
func (s *AdvancedService) ListPoliciesByScope(ctx context.Context, scope PolicyScope) ([]*AdvancedPolicy, error) {
	all, err := s.policyStore.List(ctx)
	if err != nil {
		return nil, err
	}

	var result []*AdvancedPolicy
	for _, p := range all {
		if p.Scope == scope {
			result = append(result, p)
		}
	}
	return result, nil
}

// GetPolicyStats returns statistics for a policy
func (s *AdvancedService) GetPolicyStats(ctx context.Context, id string) (*PolicyStats, error) {
	return s.policyStore.GetStats(ctx, id)
}

// SetS3LifecycleConfiguration sets S3 lifecycle rules for a bucket
func (s *AdvancedService) SetS3LifecycleConfiguration(ctx context.Context, bucket string, config *S3LifecycleConfiguration) error {
	// Convert to NebulaIO policies
	policies, err := s.s3Adapter.ConvertFromS3(bucket, config)
	if err != nil {
		return fmt.Errorf("failed to convert lifecycle configuration: %w", err)
	}

	// Delete existing lifecycle policies for this bucket
	existing, err := s.policyStore.ListByScope(ctx, PolicyScopeBucket, bucket)
	if err != nil {
		return err
	}

	for _, policy := range existing {
		if policy.Type == PolicyTypeS3Lifecycle {
			if err := s.policyStore.Delete(ctx, policy.ID); err != nil {
				return err
			}
		}
	}

	// Create new policies
	for _, policy := range policies {
		if err := s.policyStore.Create(ctx, policy); err != nil {
			return err
		}
	}

	return nil
}

// GetS3LifecycleConfiguration gets S3 lifecycle rules for a bucket
func (s *AdvancedService) GetS3LifecycleConfiguration(ctx context.Context, bucket string) (*S3LifecycleConfiguration, error) {
	policies, err := s.policyStore.ListByScope(ctx, PolicyScopeBucket, bucket)
	if err != nil {
		return nil, err
	}

	return s.s3Adapter.ConvertToS3(bucket, policies)
}

// GetAccessStats returns access statistics for an object
func (s *AdvancedService) GetAccessStats(ctx context.Context, bucket, key string) (*ObjectAccessStats, error) {
	return s.accessStats.GetStats(ctx, bucket, key)
}

// GetHotObjects returns the most accessed objects
func (s *AdvancedService) GetHotObjects(ctx context.Context, limit int) []*ObjectAccessStats {
	return s.accessStats.GetHotObjects(ctx, limit)
}

// GetColdObjects returns objects not accessed recently
func (s *AdvancedService) GetColdObjects(ctx context.Context, inactiveDays, limit int) []*ObjectAccessStats {
	return s.accessStats.GetColdObjects(ctx, inactiveDays, limit)
}

// TransitionObject manually transitions an object to a different tier
func (s *AdvancedService) TransitionObject(ctx context.Context, bucket, key string, targetTier TierType) error {
	return s.tierManager.TransitionObject(ctx, bucket, key, targetTier)
}

// Stats returns service statistics
type AdvancedServiceStats struct {
	CacheStats        *CacheStats        `json:"cacheStats,omitempty"`
	AccessTrackerStats AccessTrackerStats `json:"accessTrackerStats"`
	PolicyCount       int                `json:"policyCount"`
	ActivePolicies    int                `json:"activePolicies"`
}

// Stats returns service statistics
func (s *AdvancedService) Stats(ctx context.Context) (*AdvancedServiceStats, error) {
	policies, err := s.policyStore.List(ctx)
	if err != nil {
		return nil, err
	}

	activePolicies := 0
	for _, p := range policies {
		if p.Enabled {
			activePolicies++
		}
	}

	stats := &AdvancedServiceStats{
		AccessTrackerStats: s.accessStats.Stats(),
		PolicyCount:        len(policies),
		ActivePolicies:     activePolicies,
	}

	if s.cache != nil {
		cacheStats := s.cache.Stats()
		stats.CacheStats = &cacheStats
	}

	return stats, nil
}

// DefaultTierManager implements TierManager
type DefaultTierManager struct {
	hot    backend.Backend
	warm   backend.Backend
	cold   *ColdStorageManager
	cache  *Cache
	mu     sync.RWMutex
}

// GetObject retrieves an object
func (m *DefaultTierManager) GetObject(ctx context.Context, bucket, key string) ([]byte, error) {
	// Try hot
	if m.hot != nil {
		reader, err := m.hot.GetObject(ctx, bucket, key)
		if err == nil {
			data, err := io.ReadAll(reader)
			reader.Close()
			return data, err
		}
	}

	// Try warm
	if m.warm != nil {
		reader, err := m.warm.GetObject(ctx, bucket, key)
		if err == nil {
			data, err := io.ReadAll(reader)
			reader.Close()
			return data, err
		}
	}

	// Try cold
	if m.cold != nil {
		for _, cold := range m.cold.List() {
			reader, err := cold.GetObject(ctx, bucket, key)
			if err == nil {
				data, err := io.ReadAll(reader)
				reader.Close()
				return data, err
			}
		}
	}

	return nil, backend.ErrObjectNotFound
}

// TransitionObject moves an object between tiers
func (m *DefaultTierManager) TransitionObject(ctx context.Context, bucket, key string, targetTier TierType) error {
	// Get object data
	data, err := m.GetObject(ctx, bucket, key)
	if err != nil {
		return err
	}

	// Store in target tier
	reader := &bytesReader{data: data}
	size := int64(len(data))

	switch targetTier {
	case TierHot:
		if m.hot != nil {
			_, err = m.hot.PutObject(ctx, bucket, key, reader, size)
			if err != nil {
				return err
			}
		}
	case TierWarm:
		if m.warm != nil {
			_, err = m.warm.PutObject(ctx, bucket, key, reader, size)
			if err != nil {
				return err
			}
		}
	case TierCold, TierArchive:
		if m.cold != nil {
			cold := m.cold.GetByTier(targetTier)
			if cold != nil {
				_, err = cold.PutObject(ctx, bucket, key, reader, size)
				if err != nil {
					return err
				}
			} else {
				return fmt.Errorf("no cold storage available for tier %s", targetTier)
			}
		}
	}

	// Delete from other tiers
	return m.deleteFromOtherTiers(ctx, bucket, key, targetTier)
}

// deleteFromOtherTiers removes object from all tiers except target
func (m *DefaultTierManager) deleteFromOtherTiers(ctx context.Context, bucket, key string, exceptTier TierType) error {
	if exceptTier != TierHot && m.hot != nil {
		_ = m.hot.DeleteObject(ctx, bucket, key)
	}
	if exceptTier != TierWarm && m.warm != nil {
		_ = m.warm.DeleteObject(ctx, bucket, key)
	}
	if exceptTier != TierCold && exceptTier != TierArchive && m.cold != nil {
		for _, cold := range m.cold.List() {
			_ = cold.DeleteObject(ctx, bucket, key)
		}
	}
	return nil
}

// GetTierInfo returns information about a tier
func (m *DefaultTierManager) GetTierInfo(ctx context.Context, tier TierType) (*TierInfo, error) {
	var backend backend.Backend

	switch tier {
	case TierHot:
		backend = m.hot
	case TierWarm:
		backend = m.warm
	default:
		return nil, fmt.Errorf("tier info not available for %s", tier)
	}

	if backend == nil {
		return nil, fmt.Errorf("tier %s not configured", tier)
	}

	info, err := backend.GetStorageInfo(ctx)
	if err != nil {
		return nil, err
	}

	usagePercent := float64(0)
	if info.TotalBytes > 0 {
		usagePercent = float64(info.UsedBytes) / float64(info.TotalBytes) * 100
	}

	return &TierInfo{
		Tier:           tier,
		TotalBytes:     info.TotalBytes,
		UsedBytes:      info.UsedBytes,
		AvailableBytes: info.AvailableBytes,
		ObjectCount:    info.ObjectCount,
		UsagePercent:   usagePercent,
	}, nil
}

// GetObjectTier returns the current tier of an object
func (m *DefaultTierManager) GetObjectTier(ctx context.Context, bucket, key string) (TierType, error) {
	// Check hot
	if m.hot != nil {
		exists, err := m.hot.ObjectExists(ctx, bucket, key)
		if err == nil && exists {
			return TierHot, nil
		}
	}

	// Check warm
	if m.warm != nil {
		exists, err := m.warm.ObjectExists(ctx, bucket, key)
		if err == nil && exists {
			return TierWarm, nil
		}
	}

	// Check cold
	if m.cold != nil {
		for _, cold := range m.cold.List() {
			exists, err := cold.ObjectExists(ctx, bucket, key)
			if err == nil && exists {
				return TierCold, nil
			}
		}
	}

	return "", backend.ErrObjectNotFound
}

// ListObjects lists objects in a bucket
func (m *DefaultTierManager) ListObjects(ctx context.Context, bucket, prefix string, limit int) ([]ObjectMetadata, error) {
	// This would need to be implemented with a proper listing mechanism
	// For now, return empty
	return []ObjectMetadata{}, nil
}

// DeleteObject removes an object
func (m *DefaultTierManager) DeleteObject(ctx context.Context, bucket, key string) error {
	var lastErr error

	if m.hot != nil {
		if err := m.hot.DeleteObject(ctx, bucket, key); err != nil && err != backend.ErrObjectNotFound {
			lastErr = err
		}
	}

	if m.warm != nil {
		if err := m.warm.DeleteObject(ctx, bucket, key); err != nil && err != backend.ErrObjectNotFound {
			lastErr = err
		}
	}

	if m.cold != nil {
		for _, cold := range m.cold.List() {
			if err := cold.DeleteObject(ctx, bucket, key); err != nil && err != backend.ErrObjectNotFound {
				lastErr = err
			}
		}
	}

	return lastErr
}
