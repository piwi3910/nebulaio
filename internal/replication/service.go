package replication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// Service manages bucket replication
type Service struct {
	mu          sync.RWMutex
	configs     map[string]*Config // bucket -> config
	queue       *Queue
	workerPool  *WorkerPool
	backend     ObjectBackend
	metaStore   MetadataStore
	started     bool
	metrics     *ReplicationMetrics
	metricsMu   sync.RWMutex
}

// ObjectBackend is the interface for object storage operations
type ObjectBackend interface {
	// GetObject retrieves an object
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	// GetObjectInfo retrieves object metadata
	GetObjectInfo(ctx context.Context, bucket, key string) (*ObjectInfo, error)
}

// MetadataStore is the interface for storing replication metadata
type MetadataStore interface {
	// GetReplicationConfig retrieves replication config for a bucket
	GetReplicationConfig(ctx context.Context, bucket string) (*Config, error)
	// SetReplicationConfig stores replication config for a bucket
	SetReplicationConfig(ctx context.Context, bucket string, config *Config) error
	// DeleteReplicationConfig removes replication config for a bucket
	DeleteReplicationConfig(ctx context.Context, bucket string) error
	// GetReplicationStatus retrieves replication status for an object
	GetReplicationStatus(ctx context.Context, bucket, key, versionID string) (*ReplicationStatus, error)
	// SetReplicationStatus updates replication status for an object
	SetReplicationStatus(ctx context.Context, bucket, key, versionID string, status *ReplicationStatus) error
}

// ServiceConfig holds service configuration
type ServiceConfig struct {
	QueueConfig  QueueConfig
	WorkerConfig WorkerConfig
}

// DefaultServiceConfig returns sensible defaults
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		QueueConfig:  DefaultQueueConfig(),
		WorkerConfig: DefaultWorkerConfig(),
	}
}

// NewService creates a new replication service
func NewService(backend ObjectBackend, metaStore MetadataStore, cfg ServiceConfig) *Service {
	queue := NewQueue(cfg.QueueConfig)

	svc := &Service{
		configs:   make(map[string]*Config),
		queue:     queue,
		backend:   backend,
		metaStore: metaStore,
		metrics:   &ReplicationMetrics{},
	}

	svc.workerPool = NewWorkerPool(queue, svc, cfg.WorkerConfig)

	return svc
}

// Start starts the replication service
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return errors.New("service already started")
	}

	s.workerPool.Start(ctx)
	s.started = true

	return nil
}

// Stop stops the replication service
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.workerPool.Stop()
	_ = s.queue.Close()
	s.started = false

	return nil
}

// SetConfig sets the replication configuration for a bucket
func (s *Service) SetConfig(ctx context.Context, bucket string, config *Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid replication config: %w", err)
	}

	// Sort rules by priority
	sort.Slice(config.Rules, func(i, j int) bool {
		return config.Rules[i].Priority < config.Rules[j].Priority
	})

	s.mu.Lock()
	s.configs[bucket] = config
	s.mu.Unlock()

	// Persist to metadata store
	if s.metaStore != nil {
		if err := s.metaStore.SetReplicationConfig(ctx, bucket, config); err != nil {
			return fmt.Errorf("failed to persist replication config: %w", err)
		}
	}

	return nil
}

// GetConfig gets the replication configuration for a bucket
func (s *Service) GetConfig(ctx context.Context, bucket string) (*Config, error) {
	s.mu.RLock()
	config, ok := s.configs[bucket]
	s.mu.RUnlock()

	if ok {
		return config, nil
	}

	// Try to load from metadata store
	if s.metaStore != nil {
		config, err := s.metaStore.GetReplicationConfig(ctx, bucket)
		if err != nil {
			return nil, fmt.Errorf("replication not configured for bucket: %s", bucket)
		}

		s.mu.Lock()
		s.configs[bucket] = config
		s.mu.Unlock()

		return config, nil
	}

	return nil, fmt.Errorf("replication not configured for bucket: %s", bucket)
}

// DeleteConfig removes the replication configuration for a bucket
func (s *Service) DeleteConfig(ctx context.Context, bucket string) error {
	s.mu.Lock()
	delete(s.configs, bucket)
	s.mu.Unlock()

	if s.metaStore != nil {
		return s.metaStore.DeleteReplicationConfig(ctx, bucket)
	}

	return nil
}

// OnObjectCreated is called when an object is created
func (s *Service) OnObjectCreated(ctx context.Context, bucket, key, versionID string, tags map[string]string) error {
	config, err := s.GetConfig(ctx, bucket)
	if err != nil {
		// No replication configured, skip
		return nil
	}

	// Find matching rules
	for _, rule := range config.Rules {
		if !rule.IsEnabled() {
			continue
		}

		if rule.Filter != nil && !rule.Filter.Matches(key, tags) {
			continue
		}

		// Enqueue replication
		if _, err := s.queue.Enqueue(ctx, bucket, key, versionID, "PUT", rule.ID); err != nil {
			return fmt.Errorf("failed to enqueue replication: %w", err)
		}

		// Set initial replication status
		if err := s.updateReplicationStatus(ctx, bucket, key, versionID, rule.Destination.Bucket, ReplicationStatusPending); err != nil {
			// Log but don't fail
			fmt.Printf("warning: failed to set initial replication status: %v\n", err)
		}
	}

	return nil
}

// OnObjectDeleted is called when an object is deleted
func (s *Service) OnObjectDeleted(ctx context.Context, bucket, key, versionID string, tags map[string]string) error {
	config, err := s.GetConfig(ctx, bucket)
	if err != nil {
		// No replication configured, skip
		return nil
	}

	// Find matching rules
	for _, rule := range config.Rules {
		if !rule.IsEnabled() {
			continue
		}

		if !rule.ShouldReplicateDeleteMarkers() {
			continue
		}

		if rule.Filter != nil && !rule.Filter.Matches(key, tags) {
			continue
		}

		// Enqueue delete replication
		if _, err := s.queue.Enqueue(ctx, bucket, key, versionID, "DELETE", rule.ID); err != nil {
			return fmt.Errorf("failed to enqueue delete replication: %w", err)
		}
	}

	return nil
}

// GetReplicationStatus gets the replication status for an object
func (s *Service) GetReplicationStatus(ctx context.Context, bucket, key, versionID string) (*ReplicationStatus, error) {
	if s.metaStore != nil {
		return s.metaStore.GetReplicationStatus(ctx, bucket, key, versionID)
	}
	return nil, errors.New("metadata store not configured")
}

// getSourceObject retrieves an object from the source backend
func (s *Service) getSourceObject(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, error) {
	if s.backend == nil {
		return nil, errors.New("backend not configured")
	}
	return s.backend.GetObject(ctx, bucket, key)
}

// getSourceObjectInfo retrieves object info from the source backend
func (s *Service) getSourceObjectInfo(ctx context.Context, bucket, key, versionID string) (*ObjectInfo, error) {
	if s.backend == nil {
		return nil, errors.New("backend not configured")
	}
	return s.backend.GetObjectInfo(ctx, bucket, key)
}

// updateReplicationStatus updates the replication status for an object
func (s *Service) updateReplicationStatus(ctx context.Context, bucket, key, versionID, destBucket, status string) error {
	if s.metaStore == nil {
		return nil
	}

	// Get current status
	currentStatus, err := s.metaStore.GetReplicationStatus(ctx, bucket, key, versionID)
	if err != nil {
		// Create new status
		currentStatus = &ReplicationStatus{
			Status:       status,
			Destinations: make(map[string]DestinationStatus),
		}
	}

	// Update destination status
	now := time.Now()
	destStatus := DestinationStatus{Status: status}
	switch status {
	case ReplicationStatusComplete:
		destStatus.LastReplicationTime = &now
	case ReplicationStatusFailed:
		if existing, ok := currentStatus.Destinations[destBucket]; ok {
			destStatus.FailedAttempts = existing.FailedAttempts + 1
		} else {
			destStatus.FailedAttempts = 1
		}
	}
	currentStatus.Destinations[destBucket] = destStatus

	// Update overall status
	currentStatus.Status = s.calculateOverallStatus(currentStatus.Destinations)

	return s.metaStore.SetReplicationStatus(ctx, bucket, key, versionID, currentStatus)
}

// calculateOverallStatus calculates the overall replication status from destination statuses
func (s *Service) calculateOverallStatus(destinations map[string]DestinationStatus) string {
	if len(destinations) == 0 {
		return ReplicationStatusNoReplication
	}

	hasPending := false
	hasFailed := false
	allComplete := true

	for _, status := range destinations {
		switch status.Status {
		case ReplicationStatusPending:
			hasPending = true
			allComplete = false
		case ReplicationStatusFailed:
			hasFailed = true
			allComplete = false
		case ReplicationStatusComplete:
			// Continue checking
		default:
			allComplete = false
		}
	}

	if allComplete {
		return ReplicationStatusComplete
	}
	if hasFailed {
		return ReplicationStatusFailed
	}
	if hasPending {
		return ReplicationStatusPending
	}

	return ReplicationStatusPending
}

// GetQueueStats returns queue statistics
func (s *Service) GetQueueStats() QueueStats {
	return s.queue.Stats()
}

// GetMetrics returns replication metrics
func (s *Service) GetMetrics() ReplicationMetrics {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	return *s.metrics
}

// ListConfigs returns all replication configurations
func (s *Service) ListConfigs() map[string]*Config {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configs := make(map[string]*Config)
	for k, v := range s.configs {
		configs[k] = v
	}
	return configs
}

// ResyncBucket triggers resync of all objects in a bucket
func (s *Service) ResyncBucket(ctx context.Context, bucket string) error {
	config, err := s.GetConfig(ctx, bucket)
	if err != nil {
		return err
	}

	// Check if any rule has existing object replication enabled
	hasExistingReplication := false
	for _, rule := range config.Rules {
		if rule.ShouldReplicateExistingObjects() {
			hasExistingReplication = true
			break
		}
	}

	if !hasExistingReplication {
		return errors.New("no rules have existing object replication enabled")
	}

	// TODO: Implement full bucket listing and queueing
	// This would require listing all objects and enqueueing them
	// For now, return an error indicating this is not implemented
	return errors.New("bucket resync not yet implemented")
}

// MarshalJSON implements json.Marshaler for Config
func (c *Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Role  string `json:"role"`
		Rules []Rule `json:"rules"`
	}{
		Role:  c.Role,
		Rules: c.Rules,
	})
}

// UnmarshalJSON implements json.Unmarshaler for Config
func (c *Config) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role  string `json:"role"`
		Rules []Rule `json:"rules"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Role = raw.Role
	c.Rules = raw.Rules
	return nil
}
