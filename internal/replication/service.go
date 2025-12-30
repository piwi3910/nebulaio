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

	"github.com/rs/zerolog/log"
)

// Service manages bucket replication.
type Service struct {
	backend    ObjectBackend
	metaStore  MetadataStore
	lister     ObjectLister
	configs    map[string]*Config
	queue      *Queue
	workerPool *WorkerPool
	metrics    *ReplicationMetrics
	mu         sync.RWMutex
	metricsMu  sync.RWMutex
	started    bool
}

// ObjectBackend is the interface for object storage operations.
type ObjectBackend interface {
	// GetObject retrieves an object
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	// GetObjectInfo retrieves object metadata
	GetObjectInfo(ctx context.Context, bucket, key string) (*ObjectInfo, error)
}

// MetadataStore is the interface for storing replication metadata.
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

// ServiceConfig holds service configuration.
type ServiceConfig struct {
	QueueConfig  QueueConfig
	WorkerConfig WorkerConfig
}

// DefaultServiceConfig returns sensible defaults.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		QueueConfig:  DefaultQueueConfig(),
		WorkerConfig: DefaultWorkerConfig(),
	}
}

// NewService creates a new replication service.
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

// SetLister sets the object lister for bucket resync operations.
func (s *Service) SetLister(lister ObjectLister) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lister = lister
}

// Start starts the replication service.
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

// Stop stops the replication service.
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.workerPool.Stop()
	closeErr := s.queue.Close()
	if closeErr != nil {
		log.Error().
			Err(closeErr).
			Msg("failed to close replication queue during service stop - some items may be lost")
	}

	s.started = false

	return nil
}

// SetConfig sets the replication configuration for a bucket.
func (s *Service) SetConfig(ctx context.Context, bucket string, config *Config) error {
	err := config.Validate()
	if err != nil {
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
		err := s.metaStore.SetReplicationConfig(ctx, bucket, config)
		if err != nil {
			return fmt.Errorf("failed to persist replication config: %w", err)
		}
	}

	return nil
}

// GetConfig gets the replication configuration for a bucket.
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

// DeleteConfig removes the replication configuration for a bucket.
func (s *Service) DeleteConfig(ctx context.Context, bucket string) error {
	s.mu.Lock()
	delete(s.configs, bucket)
	s.mu.Unlock()

	if s.metaStore != nil {
		return s.metaStore.DeleteReplicationConfig(ctx, bucket)
	}

	return nil
}

// OnObjectCreated is called when an object is created.
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
		err := s.updateReplicationStatus(ctx, bucket, key, versionID, rule.Destination.Bucket, ReplicationStatusPending)
		if err != nil {
			// Log but don't fail
			fmt.Printf("warning: failed to set initial replication status: %v\n", err)
		}
	}

	return nil
}

// OnObjectDeleted is called when an object is deleted.
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

// GetReplicationStatus gets the replication status for an object.
func (s *Service) GetReplicationStatus(ctx context.Context, bucket, key, versionID string) (*ReplicationStatus, error) {
	if s.metaStore != nil {
		return s.metaStore.GetReplicationStatus(ctx, bucket, key, versionID)
	}

	return nil, errors.New("metadata store not configured")
}

// getSourceObject retrieves an object from the source backend.
func (s *Service) getSourceObject(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, error) {
	if s.backend == nil {
		return nil, errors.New("backend not configured")
	}

	return s.backend.GetObject(ctx, bucket, key)
}

// getSourceObjectInfo retrieves object info from the source backend.
func (s *Service) getSourceObjectInfo(ctx context.Context, bucket, key, versionID string) (*ObjectInfo, error) {
	if s.backend == nil {
		return nil, errors.New("backend not configured")
	}

	return s.backend.GetObjectInfo(ctx, bucket, key)
}

// updateReplicationStatus updates the replication status for an object.
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

// calculateOverallStatus calculates the overall replication status from destination statuses.
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

// GetQueueStats returns queue statistics.
func (s *Service) GetQueueStats() QueueStats {
	return s.queue.Stats()
}

// GetMetrics returns replication metrics.
func (s *Service) GetMetrics() ReplicationMetrics {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	return *s.metrics
}

// ListConfigs returns all replication configurations.
func (s *Service) ListConfigs() map[string]*Config {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configs := make(map[string]*Config)
	for k, v := range s.configs {
		configs[k] = v
	}

	return configs
}

// ResyncBucket triggers resync of all objects in a bucket.
func (s *Service) ResyncBucket(ctx context.Context, bucket string) error {
	log.Info().Str("bucket", bucket).Msg("Starting bucket resync")

	config, err := s.GetConfig(ctx, bucket)
	if err != nil {
		log.Error().Err(err).Str("bucket", bucket).Msg("Failed to get replication config")
		return err
	}

	// Check if any rule has existing object replication enabled
	var applicableRules []Rule

	for _, rule := range config.Rules {
		if rule.ShouldReplicateExistingObjects() {
			applicableRules = append(applicableRules, rule)
		}
	}

	if len(applicableRules) == 0 {
		return errors.New("no rules have existing object replication enabled")
	}

	// Copy lister reference under lock to avoid race conditions
	// if SetLister is called during the resync operation
	s.mu.RLock()
	lister := s.lister
	s.mu.RUnlock()

	if lister == nil {
		return errors.New("object lister not configured")
	}

	// List all objects in the bucket and enqueue them for replication
	objectsCh, errCh := lister.ListObjects(ctx, bucket, "", true)

	// Track statistics
	var (
		enqueuedCount int64
		errorCount    int64
		listErr       error
	)

	// Process objects - drain both channels to avoid goroutine leaks
	objectsDone := false
	for !objectsDone {
		select {
		case obj, ok := <-objectsCh:
			if !ok {
				objectsDone = true
				continue
			}

			// Check which rules apply to this object
			for _, rule := range applicableRules {
				// Check if the rule's filter matches the object
				if rule.Filter != nil && !rule.Filter.Matches(obj.Key, obj.Tags) {
					continue
				}

				// Enqueue the object for replication
				_, err := s.queue.Enqueue(ctx, bucket, obj.Key, obj.VersionID, "PUT", rule.ID)
				if err != nil {
					log.Warn().
						Err(err).
						Str("bucket", bucket).
						Str("key", obj.Key).
						Str("rule_id", rule.ID).
						Msg("Failed to enqueue object for replication")

					errorCount++

					continue
				}

				enqueuedCount++
			}

		case err, ok := <-errCh:
			if ok && err != nil {
				log.Error().Err(err).Str("bucket", bucket).Msg("Error listing objects during resync")
				listErr = err
			}

		case <-ctx.Done():
			log.Warn().Str("bucket", bucket).Msg("Bucket resync cancelled")
			return ctx.Err()
		}
	}

	// Drain any remaining errors from errCh
	select {
	case err, ok := <-errCh:
		if ok && err != nil && listErr == nil {
			listErr = err
		}
	default:
	}

	// Return listing error if any
	if listErr != nil {
		return fmt.Errorf("error listing objects: %w", listErr)
	}

	// Log completion
	if errorCount > 0 {
		log.Warn().
			Str("bucket", bucket).
			Int64("enqueued", enqueuedCount).
			Int64("errors", errorCount).
			Msg("Bucket resync completed with enqueue errors")

		return fmt.Errorf("resync completed with %d enqueue errors, %d objects enqueued", errorCount, enqueuedCount)
	}

	log.Info().
		Str("bucket", bucket).
		Int64("enqueued", enqueuedCount).
		Msg("Bucket resync completed successfully")

	return nil
}

// MarshalJSON implements json.Marshaler for Config.
func (c *Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Role  string `json:"role"`
		Rules []Rule `json:"rules"`
	}{
		Role:  c.Role,
		Rules: c.Rules,
	})
}

// UnmarshalJSON implements json.Unmarshaler for Config.
func (c *Config) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role  string `json:"role"`
		Rules []Rule `json:"rules"`
	}
	err := json.Unmarshal(data, &raw)
	if err != nil {
		return err
	}

	c.Role = raw.Role
	c.Rules = raw.Rules

	return nil
}
