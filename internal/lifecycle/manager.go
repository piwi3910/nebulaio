// Package lifecycle implements S3 lifecycle management for NebulaIO.
//
// The lifecycle manager automatically executes actions on objects based on
// configurable rules:
//
//   - Expiration: Delete objects after a specified age
//   - Transition: Move objects to different storage classes
//   - NoncurrentVersionExpiration: Delete old versions
//   - AbortIncompleteMultipartUpload: Clean up incomplete uploads
//
// Rules can be filtered by:
//   - Object key prefix
//   - Object tags
//   - Object size range
//
// The manager runs periodic scans (default: hourly) to evaluate objects
// against configured rules and execute matching actions.
//
// Example rule: Delete logs older than 90 days:
//
//	{
//	  "Rules": [{
//	    "ID": "delete-old-logs",
//	    "Filter": {"Prefix": "logs/"},
//	    "Status": "Enabled",
//	    "Expiration": {"Days": 90}
//	  }]
//	}
package lifecycle

import (
	"context"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/rs/zerolog/log"
)

// DefaultInterval is the default interval for lifecycle evaluation.
const DefaultInterval = time.Hour

// Processing batch sizes.
const (
	objectBatchSize  = 1000
	versionBatchSize = 10000
)

// ObjectService defines the interface for object operations needed by lifecycle.
type ObjectService interface {
	DeleteObject(ctx context.Context, bucket, key string) error
	DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error
	TransitionStorageClass(ctx context.Context, bucket, key, targetClass string) error
}

// MultipartService defines the interface for multipart operations needed by lifecycle.
type MultipartService interface {
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
	ListMultipartUploads(ctx context.Context, bucket string) ([]*metadata.MultipartUpload, error)
}

// Manager handles lifecycle policy evaluation and execution.
type Manager struct {
	store            metadata.Store
	objectService    ObjectService
	multipartService MultipartService
	stopCh           chan struct{}
	stoppedCh        chan struct{}
	interval         time.Duration
	mu               sync.RWMutex
	running          bool
}

// NewManager creates a new lifecycle manager.
func NewManager(store metadata.Store, objectService ObjectService, multipartService MultipartService) *Manager {
	return &Manager{
		store:            store,
		objectService:    objectService,
		multipartService: multipartService,
		interval:         DefaultInterval,
		stopCh:           make(chan struct{}),
		stoppedCh:        make(chan struct{}),
	}
}

// SetInterval sets the evaluation interval.
func (m *Manager) SetInterval(interval time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.interval = interval
}

// Start starts the lifecycle manager background processing.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()

	if m.running {
		m.mu.Unlock()
		return
	}

	m.running = true
	m.stopCh = make(chan struct{})
	m.stoppedCh = make(chan struct{})
	m.mu.Unlock()

	go m.run(ctx)
}

// Stop stops the lifecycle manager.
func (m *Manager) Stop() {
	m.mu.Lock()

	if !m.running {
		m.mu.Unlock()
		return
	}

	m.running = false
	close(m.stopCh)
	m.mu.Unlock()

	// Wait for the goroutine to stop
	<-m.stoppedCh
}

// run is the main loop for lifecycle processing.
func (m *Manager) run(ctx context.Context) {
	defer close(m.stoppedCh)

	log.Info().Dur("interval", m.interval).Msg("Lifecycle manager started")

	// Run immediately on start, then on interval
	m.runCycle(ctx)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Lifecycle manager stopping (context done)")
			return
		case <-m.stopCh:
			log.Info().Msg("Lifecycle manager stopping (stop signal)")
			return
		case <-ticker.C:
			m.runCycle(ctx)
		}
	}
}

// runCycle runs a single lifecycle evaluation cycle.
func (m *Manager) runCycle(ctx context.Context) {
	// Only run on leader node
	if !m.store.IsLeader() {
		log.Debug().Msg("Skipping lifecycle cycle - not leader")
		return
	}

	log.Debug().Msg("Starting lifecycle evaluation cycle")

	startTime := time.Now()

	// Get all buckets
	buckets, err := m.store.ListBuckets(ctx, "")
	if err != nil {
		log.Error().Err(err).Msg("Failed to list buckets for lifecycle evaluation")
		return
	}

	var processed, errors int

	for _, bucket := range buckets {
		err := m.ProcessBucket(ctx, bucket.Name)
		if err != nil {
			log.Error().Err(err).Str("bucket", bucket.Name).Msg("Failed to process bucket lifecycle")

			errors++
		} else {
			processed++
		}
	}

	log.Info().
		Int("buckets_processed", processed).
		Int("errors", errors).
		Dur("duration", time.Since(startTime)).
		Msg("Lifecycle evaluation cycle completed")
}

// ProcessBucket evaluates and applies lifecycle rules for a single bucket.
func (m *Manager) ProcessBucket(ctx context.Context, bucket string) error {
	// Get bucket metadata
	bucketMeta, err := m.store.GetBucket(ctx, bucket)
	if err != nil {
		return err
	}

	// Skip if no lifecycle rules
	if len(bucketMeta.Lifecycle) == 0 {
		return nil
	}

	log.Debug().Str("bucket", bucket).Int("rules", len(bucketMeta.Lifecycle)).Msg("Processing bucket lifecycle")

	// Convert metadata lifecycle rules to our lifecycle rules
	rules := convertMetadataRules(bucketMeta.Lifecycle)

	// Process regular objects
	if err := m.processObjects(ctx, bucket, rules); err != nil {
		return err
	}

	// Process multipart uploads
	if err := m.processMultipartUploads(ctx, bucket, rules); err != nil {
		return err
	}

	// Process versioned objects if versioning is enabled
	if bucketMeta.Versioning == metadata.VersioningEnabled || bucketMeta.Versioning == metadata.VersioningSuspended {
		err := m.processVersions(ctx, bucket, rules)
		if err != nil {
			return err
		}
	}

	return nil
}

// processObjects processes current objects for expiration.
func (m *Manager) processObjects(ctx context.Context, bucket string, rules []LifecycleRule) error {
	var continuationToken string

	batchSize := objectBatchSize

	for {
		listing, err := m.store.ListObjects(ctx, bucket, "", "", batchSize, continuationToken)
		if err != nil {
			return err
		}

		for _, obj := range listing.Objects {
			// Skip delete markers
			if obj.DeleteMarker {
				continue
			}

			action := m.EvaluateObject(rules, obj)
			err := m.applyAction(ctx, bucket, obj, action)
			if err != nil {
				log.Error().Err(err).
					Str("bucket", bucket).
					Str("key", obj.Key).
					Msg("Failed to apply lifecycle action")
			}
		}

		if !listing.IsTruncated {
			break
		}

		continuationToken = listing.NextContinuationToken
	}

	return nil
}

// processVersions processes object versions for noncurrent version expiration.
func (m *Manager) processVersions(ctx context.Context, bucket string, rules []LifecycleRule) error {
	listing, err := m.store.ListObjectVersions(ctx, bucket, "", "", "", "", versionBatchSize)
	if err != nil {
		return err
	}

	// Combine versions and delete markers
	allVersions := make([]*metadata.ObjectMeta, 0, len(listing.Versions)+len(listing.DeleteMarkers))
	allVersions = append(allVersions, listing.Versions...)
	allVersions = append(allVersions, listing.DeleteMarkers...)

	// Group versions by key to identify noncurrent versions
	keyVersions := make(map[string][]*metadata.ObjectMeta)
	for _, v := range allVersions {
		keyVersions[v.Key] = append(keyVersions[v.Key], v)
	}

	now := time.Now()

	for key, vers := range keyVersions {
		// Sort by modification time (newest first) - assume they come sorted
		// Process noncurrent versions (all except the first/current one)
		for i, v := range vers {
			isCurrent := i == 0 && !v.DeleteMarker

			if isCurrent {
				continue
			}

			// Evaluate noncurrent version rules
			for _, rule := range rules {
				if !rule.IsEnabled() {
					continue
				}

				if !rule.MatchesObject(key, v.Tags) {
					continue
				}

				// Check for expired delete markers
				if v.DeleteMarker && rule.Expiration != nil && rule.Expiration.ExpiredObjectDeleteMarker {
					// Delete marker is expired if it's the only version
					if len(vers) == 1 {
						err := m.objectService.DeleteObjectVersion(ctx, bucket, key, v.VersionID)
						if err != nil {
							log.Error().Err(err).
								Str("bucket", bucket).
								Str("key", key).
								Str("versionId", v.VersionID).
								Msg("Failed to delete expired delete marker")
						} else {
							log.Info().
								Str("bucket", bucket).
								Str("key", key).
								Str("versionId", v.VersionID).
								Str("rule", rule.ID).
								Msg("Deleted expired delete marker")
						}
					}

					continue
				}

				// Check noncurrent version expiration
				if rule.NoncurrentVersionExpiration != nil {
					nve := rule.NoncurrentVersionExpiration

					// Check by days
					if nve.NoncurrentDays > 0 {
						expirationTime := v.ModifiedAt.AddDate(0, 0, nve.NoncurrentDays)
						if now.After(expirationTime) {
							err := m.objectService.DeleteObjectVersion(ctx, bucket, key, v.VersionID)
							if err != nil {
								log.Error().Err(err).
									Str("bucket", bucket).
									Str("key", key).
									Str("versionId", v.VersionID).
									Msg("Failed to delete noncurrent version")
							} else {
								log.Info().
									Str("bucket", bucket).
									Str("key", key).
									Str("versionId", v.VersionID).
									Str("rule", rule.ID).
									Msg("Deleted noncurrent version")
							}
						}
					}

					// Check by count (NewerNoncurrentVersions)
					if nve.NewerNoncurrentVersions > 0 && i >= nve.NewerNoncurrentVersions {
						err := m.objectService.DeleteObjectVersion(ctx, bucket, key, v.VersionID)
						if err != nil {
							log.Error().Err(err).
								Str("bucket", bucket).
								Str("key", key).
								Str("versionId", v.VersionID).
								Msg("Failed to delete excess noncurrent version")
						} else {
							log.Info().
								Str("bucket", bucket).
								Str("key", key).
								Str("versionId", v.VersionID).
								Str("rule", rule.ID).
								Msg("Deleted excess noncurrent version")
						}
					}
				}
			}
		}
	}

	return nil
}

// processMultipartUploads aborts incomplete multipart uploads.
func (m *Manager) processMultipartUploads(ctx context.Context, bucket string, rules []LifecycleRule) error {
	uploads, err := m.multipartService.ListMultipartUploads(ctx, bucket)
	if err != nil {
		return err
	}

	now := time.Now()

	for _, upload := range uploads {
		for _, rule := range rules {
			if !rule.IsEnabled() {
				continue
			}

			if !rule.MatchesObject(upload.Key, nil) {
				continue
			}

			if rule.AbortIncompleteMultipartUpload == nil {
				continue
			}

			daysAfter := rule.AbortIncompleteMultipartUpload.DaysAfterInitiation
			expirationTime := upload.CreatedAt.AddDate(0, 0, daysAfter)

			if now.After(expirationTime) {
				err := m.multipartService.AbortMultipartUpload(ctx, bucket, upload.Key, upload.UploadID)
				if err != nil {
					log.Error().Err(err).
						Str("bucket", bucket).
						Str("key", upload.Key).
						Str("uploadId", upload.UploadID).
						Msg("Failed to abort incomplete multipart upload")
				} else {
					log.Info().
						Str("bucket", bucket).
						Str("key", upload.Key).
						Str("uploadId", upload.UploadID).
						Str("rule", rule.ID).
						Msg("Aborted incomplete multipart upload")
				}

				break // Only apply first matching rule
			}
		}
	}

	return nil
}

// EvaluateObject evaluates lifecycle rules against an object and returns the action.
func (m *Manager) EvaluateObject(rules []LifecycleRule, obj *metadata.ObjectMeta) ActionResult {
	now := time.Now()

	for _, rule := range rules {
		if !rule.IsEnabled() {
			continue
		}

		if !rule.MatchesObject(obj.Key, obj.Tags) {
			continue
		}

		// Check expiration
		if rule.Expiration != nil {
			// Check by days
			if rule.Expiration.Days > 0 {
				expirationTime := obj.CreatedAt.AddDate(0, 0, rule.Expiration.Days)
				if now.After(expirationTime) {
					return ActionResult{
						Action: ActionDelete,
						RuleID: rule.ID,
					}
				}
			}

			// Check by date
			if !rule.Expiration.Date.IsZero() {
				if now.After(rule.Expiration.Date) {
					return ActionResult{
						Action: ActionDelete,
						RuleID: rule.ID,
					}
				}
			}
		}

		// Check transitions (find earliest applicable)
		for _, transition := range rule.Transition {
			var transitionTime time.Time

			if transition.Days > 0 {
				transitionTime = obj.CreatedAt.AddDate(0, 0, transition.Days)
			} else if !transition.Date.IsZero() {
				transitionTime = transition.Date
			}

			if now.After(transitionTime) && obj.StorageClass != transition.StorageClass {
				return ActionResult{
					Action:      ActionTransition,
					TargetClass: transition.StorageClass,
					RuleID:      rule.ID,
				}
			}
		}
	}

	return ActionResult{Action: ActionNone}
}

// applyAction applies the lifecycle action to an object.
func (m *Manager) applyAction(ctx context.Context, bucket string, obj *metadata.ObjectMeta, action ActionResult) error {
	switch action.Action {
	case ActionNone:
		return nil

	case ActionDelete:
		log.Info().
			Str("bucket", bucket).
			Str("key", obj.Key).
			Str("rule", action.RuleID).
			Msg("Deleting expired object")

		return m.objectService.DeleteObject(ctx, bucket, obj.Key)

	case ActionTransition:
		log.Info().
			Str("bucket", bucket).
			Str("key", obj.Key).
			Str("rule", action.RuleID).
			Str("currentClass", obj.StorageClass).
			Str("targetClass", action.TargetClass).
			Msg("Transitioning object to new storage class")

		return m.objectService.TransitionStorageClass(ctx, bucket, obj.Key, action.TargetClass)

	case ActionDeleteMarker:
		log.Info().
			Str("bucket", bucket).
			Str("key", obj.Key).
			Str("versionId", obj.VersionID).
			Str("rule", action.RuleID).
			Msg("Deleting expired delete marker")

		return m.objectService.DeleteObjectVersion(ctx, bucket, obj.Key, obj.VersionID)

	default:
		return nil
	}
}

// convertMetadataRules converts metadata.LifecycleRule to lifecycle.LifecycleRule.
func convertMetadataRules(metaRules []metadata.LifecycleRule) []LifecycleRule {
	rules := make([]LifecycleRule, len(metaRules))

	for i, mr := range metaRules {
		status := "Disabled"
		if mr.Enabled {
			status = "Enabled"
		}

		rule := LifecycleRule{
			ID:     mr.ID,
			Status: status,
			Filter: Filter{
				Prefix: mr.Prefix,
			},
		}

		// Convert expiration
		if mr.ExpirationDays > 0 {
			rule.Expiration = &Expiration{
				Days: mr.ExpirationDays,
			}
		}

		// Convert noncurrent version expiration
		if mr.NoncurrentVersionExpirationDays > 0 {
			rule.NoncurrentVersionExpiration = &NoncurrentVersionExpiration{
				NoncurrentDays: mr.NoncurrentVersionExpirationDays,
			}
		}

		// Convert transitions
		for _, mt := range mr.Transitions {
			rule.Transition = append(rule.Transition, Transition{
				Days:         mt.Days,
				StorageClass: mt.StorageClass,
			})
		}

		rules[i] = rule
	}

	return rules
}
