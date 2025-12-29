// Package events provides S3 event notification support for NebulaIO.
//
// The package implements event emission for S3 operations, allowing users to
// receive notifications when objects are created, deleted, or modified.
// Events can be delivered to:
//
//   - Kafka topics
//   - Webhook endpoints
//   - Amazon SNS (via HTTP)
//   - Redis pub/sub
//
// Events follow the S3 event notification format, making them compatible
// with existing S3 event consumers and Lambda functions.
//
// Example event types:
//   - s3:ObjectCreated:Put
//   - s3:ObjectCreated:Copy
//   - s3:ObjectRemoved:Delete
//   - s3:ObjectRestore:Completed
package events

import (
	"context"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// Emitter handles event emission and routing
type Emitter struct {
	queue         *EventQueue
	configs       map[string]*NotificationConfiguration // bucket -> config
	mu            sync.RWMutex
	enabled       bool
}

// EmitterConfig configures the emitter
type EmitterConfig struct {
	// Enabled indicates if event emission is enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// QueueConfig configures the event queue
	QueueConfig EventQueueConfig `json:"queue" yaml:"queue"`
}

// DefaultEmitterConfig returns a default emitter configuration
func DefaultEmitterConfig() EmitterConfig {
	return EmitterConfig{
		Enabled:     true,
		QueueConfig: DefaultQueueConfig(),
	}
}

// NewEmitter creates a new event emitter
func NewEmitter(config EmitterConfig) *Emitter {
	e := &Emitter{
		queue:   NewEventQueue(config.QueueConfig),
		configs: make(map[string]*NotificationConfiguration),
		enabled: config.Enabled,
	}

	return e
}

// Start starts the emitter
func (e *Emitter) Start() {
	if !e.enabled {
		log.Info().Msg("Event emitter disabled")
		return
	}

	e.queue.Start()
	log.Info().Msg("Event emitter started")
}

// Stop stops the emitter
func (e *Emitter) Stop() {
	e.queue.Stop()
	log.Info().Msg("Event emitter stopped")
}

// AddTarget adds a target to the emitter
func (e *Emitter) AddTarget(target Target) {
	e.queue.AddTarget(target)
	log.Info().Str("target", target.Name()).Str("type", target.Type()).Msg("Target added")
}

// RemoveTarget removes a target from the emitter
func (e *Emitter) RemoveTarget(name string) {
	e.queue.RemoveTarget(name)
	log.Info().Str("target", name).Msg("Target removed")
}

// SetBucketConfig sets the notification configuration for a bucket
func (e *Emitter) SetBucketConfig(bucket string, config *NotificationConfiguration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if config == nil {
		delete(e.configs, bucket)
	} else {
		e.configs[bucket] = config
	}
}

// GetBucketConfig gets the notification configuration for a bucket
func (e *Emitter) GetBucketConfig(bucket string) *NotificationConfiguration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.configs[bucket]
}

// Emit emits an S3 event
func (e *Emitter) Emit(ctx context.Context, event *S3Event) error {
	if !e.enabled || len(event.Records) == 0 {
		return nil
	}

	record := &event.Records[0]
	bucket := record.S3.Bucket.Name
	key := record.S3.Object.Key
	eventName := record.EventName

	e.mu.RLock()
	config := e.configs[bucket]
	e.mu.RUnlock()

	if config == nil {
		// No notification config for this bucket
		return nil
	}

	// Find matching targets
	targets := e.findMatchingTargets(config, eventName, key)

	if len(targets) == 0 {
		return nil
	}

	log.Debug().
		Str("bucket", bucket).
		Str("key", key).
		Str("event", eventName).
		Int("targets", len(targets)).
		Msg("Emitting event")

	// Enqueue for each target
	for _, targetName := range targets {
		if err := e.queue.Enqueue(event, targetName); err != nil {
			log.Warn().
				Str("target", targetName).
				Err(err).
				Msg("Failed to enqueue event")
		}
	}

	return nil
}

// EmitObjectCreated emits an object created event
func (e *Emitter) EmitObjectCreated(ctx context.Context, eventType EventType, bucket, key string, size int64, etag, versionID, accessKey string) error {
	event := NewS3Event(eventType, bucket, key, size, etag, versionID, accessKey)
	return e.Emit(ctx, event)
}

// EmitObjectRemoved emits an object removed event
func (e *Emitter) EmitObjectRemoved(ctx context.Context, eventType EventType, bucket, key string, versionID, accessKey string) error {
	event := NewS3Event(eventType, bucket, key, 0, "", versionID, accessKey)
	return e.Emit(ctx, event)
}

// findMatchingTargets finds targets that match the event
func (e *Emitter) findMatchingTargets(config *NotificationConfiguration, eventName, key string) []string {
	var targets []string

	// Check topic configurations
	for _, tc := range config.TopicConfigurations {
		if e.matchesConfiguration(tc.Events, tc.Filter, eventName, key) {
			targets = append(targets, tc.TopicARN)
		}
	}

	// Check queue configurations
	for _, qc := range config.QueueConfigurations {
		if e.matchesConfiguration(qc.Events, qc.Filter, eventName, key) {
			targets = append(targets, qc.QueueARN)
		}
	}

	// Check lambda/webhook configurations
	for _, lc := range config.LambdaFunctionConfigurations {
		if e.matchesConfiguration(lc.Events, lc.Filter, eventName, key) {
			targets = append(targets, lc.LambdaFunctionARN)
		}
	}

	return targets
}

// matchesConfiguration checks if an event matches a notification configuration
func (e *Emitter) matchesConfiguration(events []EventType, filter *NotificationFilter, eventName, key string) bool {
	// Check event type match
	eventMatches := false
	for _, et := range events {
		if e.matchesEventType(string(et), eventName) {
			eventMatches = true
			break
		}
	}

	if !eventMatches {
		return false
	}

	// Check filter match
	if filter != nil && filter.Key != nil {
		return e.matchesKeyFilter(filter.Key, key)
	}

	return true
}

// matchesEventType checks if an event matches an event type pattern
func (e *Emitter) matchesEventType(pattern, eventName string) bool {
	if pattern == eventName {
		return true
	}

	// Handle wildcard patterns
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(eventName, prefix)
	}

	return false
}

// matchesKeyFilter checks if a key matches a key filter
func (e *Emitter) matchesKeyFilter(filter *KeyFilter, key string) bool {
	for _, rule := range filter.FilterRules {
		switch strings.ToLower(rule.Name) {
		case "prefix":
			if !strings.HasPrefix(key, rule.Value) {
				return false
			}
		case "suffix":
			if !strings.HasSuffix(key, rule.Value) {
				return false
			}
		}
	}
	return true
}

// Stats returns emitter statistics
func (e *Emitter) Stats() EmitterStats {
	e.mu.RLock()
	bucketCount := len(e.configs)
	e.mu.RUnlock()

	queueStats := e.queue.Stats()

	return EmitterStats{
		Enabled:           e.enabled,
		BucketConfigCount: bucketCount,
		QueueStats:        queueStats,
	}
}

// EmitterStats contains emitter statistics
type EmitterStats struct {
	Enabled           bool       `json:"enabled"`
	BucketConfigCount int        `json:"bucketConfigCount"`
	QueueStats        QueueStats `json:"queueStats"`
}
