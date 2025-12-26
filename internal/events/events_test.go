package events_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/events"
	_ "github.com/piwi3910/nebulaio/internal/events/targets"
)

// MockTarget is a mock event target for testing
type MockTarget struct {
	name        string
	targetType  string
	publishFunc func(ctx context.Context, event *events.S3Event) error
	healthy     bool
	closed      bool
	published   int32
}

func NewMockTarget(name string) *MockTarget {
	return &MockTarget{
		name:       name,
		targetType: "mock",
		healthy:    true,
	}
}

func (t *MockTarget) Name() string {
	return t.name
}

func (t *MockTarget) Type() string {
	return t.targetType
}

func (t *MockTarget) Publish(ctx context.Context, event *events.S3Event) error {
	atomic.AddInt32(&t.published, 1)
	if t.publishFunc != nil {
		return t.publishFunc(ctx, event)
	}
	return nil
}

func (t *MockTarget) IsHealthy(ctx context.Context) bool {
	return t.healthy && !t.closed
}

func (t *MockTarget) Close() error {
	t.closed = true
	return nil
}

func (t *MockTarget) PublishCount() int {
	return int(atomic.LoadInt32(&t.published))
}

// Ensure MockTarget implements Target
var _ events.Target = (*MockTarget)(nil)

func TestS3Event(t *testing.T) {
	t.Run("NewS3Event", func(t *testing.T) {
		event := events.NewS3Event(
			events.EventObjectCreatedPut,
			"test-bucket",
			"test-key.txt",
			1024,
			"abc123",
			"v1",
			"testuser",
		)

		if len(event.Records) != 1 {
			t.Fatalf("Expected 1 record, got %d", len(event.Records))
		}

		record := event.Records[0]
		if record.EventName != string(events.EventObjectCreatedPut) {
			t.Errorf("Expected event name %s, got %s", events.EventObjectCreatedPut, record.EventName)
		}
		if record.S3.Bucket.Name != "test-bucket" {
			t.Errorf("Expected bucket name 'test-bucket', got '%s'", record.S3.Bucket.Name)
		}
		if record.S3.Object.Key != "test-key.txt" {
			t.Errorf("Expected object key 'test-key.txt', got '%s'", record.S3.Object.Key)
		}
		if record.S3.Object.Size != 1024 {
			t.Errorf("Expected size 1024, got %d", record.S3.Object.Size)
		}
		if record.S3.Object.ETag != "abc123" {
			t.Errorf("Expected ETag 'abc123', got '%s'", record.S3.Object.ETag)
		}
		if record.S3.Object.VersionID != "v1" {
			t.Errorf("Expected version ID 'v1', got '%s'", record.S3.Object.VersionID)
		}
	})

	t.Run("ToJSON", func(t *testing.T) {
		event := events.NewS3Event(
			events.EventObjectCreatedPut,
			"test-bucket",
			"test-key.txt",
			1024,
			"abc123",
			"",
			"testuser",
		)

		data, err := event.ToJSON()
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}

		if len(data) == 0 {
			t.Error("Expected non-empty JSON")
		}
	})
}

func TestEventQueue(t *testing.T) {
	t.Run("EnqueueAndProcess", func(t *testing.T) {
		queue := events.NewEventQueue(events.EventQueueConfig{
			QueueSize:  100,
			MaxRetries: 3,
			RetryDelay: 100 * time.Millisecond,
			Workers:    2,
		})

		mockTarget := NewMockTarget("test-target")
		queue.AddTarget(mockTarget)
		queue.Start()
		defer queue.Stop()

		event := events.NewS3Event(
			events.EventObjectCreatedPut,
			"test-bucket",
			"test-key.txt",
			1024,
			"abc123",
			"",
			"testuser",
		)

		err := queue.Enqueue(event, "test-target")
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		if mockTarget.PublishCount() != 1 {
			t.Errorf("Expected 1 publish, got %d", mockTarget.PublishCount())
		}
	})

	t.Run("EnqueueAll", func(t *testing.T) {
		queue := events.NewEventQueue(events.EventQueueConfig{
			QueueSize:  100,
			MaxRetries: 3,
			RetryDelay: 100 * time.Millisecond,
			Workers:    2,
		})

		target1 := NewMockTarget("target1")
		target2 := NewMockTarget("target2")
		queue.AddTarget(target1)
		queue.AddTarget(target2)
		queue.Start()
		defer queue.Stop()

		event := events.NewS3Event(
			events.EventObjectCreatedPut,
			"test-bucket",
			"test-key.txt",
			1024,
			"abc123",
			"",
			"testuser",
		)

		err := queue.EnqueueAll(event)
		if err != nil {
			t.Fatalf("EnqueueAll failed: %v", err)
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		if target1.PublishCount() != 1 {
			t.Errorf("Expected 1 publish to target1, got %d", target1.PublishCount())
		}
		if target2.PublishCount() != 1 {
			t.Errorf("Expected 1 publish to target2, got %d", target2.PublishCount())
		}
	})

	t.Run("Retry", func(t *testing.T) {
		queue := events.NewEventQueue(events.EventQueueConfig{
			QueueSize:  100,
			MaxRetries: 3,
			RetryDelay: 50 * time.Millisecond,
			Workers:    2,
		})

		attempts := int32(0)
		mockTarget := NewMockTarget("retry-target")
		mockTarget.publishFunc = func(ctx context.Context, event *events.S3Event) error {
			count := atomic.AddInt32(&attempts, 1)
			if count < 3 {
				return events.ErrPublishFailed
			}
			return nil
		}

		queue.AddTarget(mockTarget)
		queue.Start()
		defer queue.Stop()

		event := events.NewS3Event(
			events.EventObjectCreatedPut,
			"test-bucket",
			"test-key.txt",
			1024,
			"abc123",
			"",
			"testuser",
		)

		err := queue.Enqueue(event, "retry-target")
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}

		// Wait for retries - need enough time for:
		// 1st attempt (immediate), 1st retry (50ms), 2nd retry (100ms)
		time.Sleep(1 * time.Second)

		finalAttempts := atomic.LoadInt32(&attempts)
		if finalAttempts < 2 {
			t.Errorf("Expected at least 2 attempts (retries working), got %d", finalAttempts)
		}
	})

	t.Run("Stats", func(t *testing.T) {
		queue := events.NewEventQueue(events.DefaultQueueConfig())

		target := NewMockTarget("stats-target")
		queue.AddTarget(target)

		stats := queue.Stats()
		if stats.TargetCount != 1 {
			t.Errorf("Expected 1 target, got %d", stats.TargetCount)
		}
	})
}

func TestEmitter(t *testing.T) {
	t.Run("EmitWithConfig", func(t *testing.T) {
		emitter := events.NewEmitter(events.EmitterConfig{
			Enabled:     true,
			QueueConfig: events.DefaultQueueConfig(),
		})
		emitter.Start()
		defer emitter.Stop()

		mockTarget := NewMockTarget("webhook1")
		emitter.AddTarget(mockTarget)

		// Set bucket notification config
		config := &events.NotificationConfiguration{
			LambdaFunctionConfigurations: []events.LambdaFunctionConfiguration{
				{
					ID:                "config1",
					LambdaFunctionARN: "webhook1",
					Events:            []events.EventType{events.EventObjectCreatedPut},
				},
			},
		}
		emitter.SetBucketConfig("test-bucket", config)

		ctx := context.Background()
		err := emitter.EmitObjectCreated(
			ctx,
			events.EventObjectCreatedPut,
			"test-bucket",
			"test-key.txt",
			1024,
			"abc123",
			"",
			"testuser",
		)
		if err != nil {
			t.Fatalf("EmitObjectCreated failed: %v", err)
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		if mockTarget.PublishCount() != 1 {
			t.Errorf("Expected 1 publish, got %d", mockTarget.PublishCount())
		}
	})

	t.Run("EmitWithFilter", func(t *testing.T) {
		emitter := events.NewEmitter(events.EmitterConfig{
			Enabled:     true,
			QueueConfig: events.DefaultQueueConfig(),
		})
		emitter.Start()
		defer emitter.Stop()

		mockTarget := NewMockTarget("filtered-webhook")
		emitter.AddTarget(mockTarget)

		// Set bucket notification config with prefix filter
		config := &events.NotificationConfiguration{
			LambdaFunctionConfigurations: []events.LambdaFunctionConfiguration{
				{
					ID:                "filtered-config",
					LambdaFunctionARN: "filtered-webhook",
					Events:            []events.EventType{events.EventObjectCreated},
					Filter: &events.NotificationFilter{
						Key: &events.KeyFilter{
							FilterRules: []events.FilterRule{
								{Name: "prefix", Value: "logs/"},
							},
						},
					},
				},
			},
		}
		emitter.SetBucketConfig("test-bucket", config)

		ctx := context.Background()

		// This should NOT match (different prefix)
		err := emitter.EmitObjectCreated(
			ctx,
			events.EventObjectCreatedPut,
			"test-bucket",
			"data/file.txt",
			1024,
			"abc123",
			"",
			"testuser",
		)
		if err != nil {
			t.Fatalf("EmitObjectCreated failed: %v", err)
		}

		// This SHOULD match
		err = emitter.EmitObjectCreated(
			ctx,
			events.EventObjectCreatedPut,
			"test-bucket",
			"logs/file.txt",
			1024,
			"abc123",
			"",
			"testuser",
		)
		if err != nil {
			t.Fatalf("EmitObjectCreated failed: %v", err)
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		if mockTarget.PublishCount() != 1 {
			t.Errorf("Expected 1 publish (only logs/ prefix), got %d", mockTarget.PublishCount())
		}
	})

	t.Run("EmitWithSuffixFilter", func(t *testing.T) {
		emitter := events.NewEmitter(events.EmitterConfig{
			Enabled:     true,
			QueueConfig: events.DefaultQueueConfig(),
		})
		emitter.Start()
		defer emitter.Stop()

		mockTarget := NewMockTarget("suffix-webhook")
		emitter.AddTarget(mockTarget)

		config := &events.NotificationConfiguration{
			LambdaFunctionConfigurations: []events.LambdaFunctionConfiguration{
				{
					ID:                "suffix-config",
					LambdaFunctionARN: "suffix-webhook",
					Events:            []events.EventType{events.EventObjectCreated},
					Filter: &events.NotificationFilter{
						Key: &events.KeyFilter{
							FilterRules: []events.FilterRule{
								{Name: "suffix", Value: ".jpg"},
							},
						},
					},
				},
			},
		}
		emitter.SetBucketConfig("test-bucket", config)

		ctx := context.Background()

		// This should NOT match
		err := emitter.EmitObjectCreated(ctx, events.EventObjectCreatedPut, "test-bucket", "file.txt", 1024, "", "", "user")
		if err != nil {
			t.Fatalf("Emit failed: %v", err)
		}

		// This SHOULD match
		err = emitter.EmitObjectCreated(ctx, events.EventObjectCreatedPut, "test-bucket", "image.jpg", 1024, "", "", "user")
		if err != nil {
			t.Fatalf("Emit failed: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		if mockTarget.PublishCount() != 1 {
			t.Errorf("Expected 1 publish (only .jpg suffix), got %d", mockTarget.PublishCount())
		}
	})

	t.Run("EmitWithWildcardEvent", func(t *testing.T) {
		emitter := events.NewEmitter(events.EmitterConfig{
			Enabled:     true,
			QueueConfig: events.DefaultQueueConfig(),
		})
		emitter.Start()
		defer emitter.Stop()

		mockTarget := NewMockTarget("wildcard-webhook")
		emitter.AddTarget(mockTarget)

		config := &events.NotificationConfiguration{
			LambdaFunctionConfigurations: []events.LambdaFunctionConfiguration{
				{
					ID:                "wildcard-config",
					LambdaFunctionARN: "wildcard-webhook",
					Events:            []events.EventType{events.EventObjectCreated}, // Wildcard: s3:ObjectCreated:*
				},
			},
		}
		emitter.SetBucketConfig("test-bucket", config)

		ctx := context.Background()

		// All of these should match the wildcard
		_ = emitter.EmitObjectCreated(ctx, events.EventObjectCreatedPut, "test-bucket", "file1.txt", 100, "", "", "user")
		_ = emitter.EmitObjectCreated(ctx, events.EventObjectCreatedCopy, "test-bucket", "file2.txt", 100, "", "", "user")
		_ = emitter.EmitObjectCreated(ctx, events.EventObjectCreatedCompleteMultipartUpload, "test-bucket", "file3.txt", 100, "", "", "user")

		time.Sleep(100 * time.Millisecond)

		if mockTarget.PublishCount() != 3 {
			t.Errorf("Expected 3 publishes (wildcard match), got %d", mockTarget.PublishCount())
		}
	})

	t.Run("EmitNoBucketConfig", func(t *testing.T) {
		emitter := events.NewEmitter(events.EmitterConfig{
			Enabled:     true,
			QueueConfig: events.DefaultQueueConfig(),
		})
		emitter.Start()
		defer emitter.Stop()

		mockTarget := NewMockTarget("no-config-webhook")
		emitter.AddTarget(mockTarget)

		ctx := context.Background()

		// No config for this bucket, should not emit
		err := emitter.EmitObjectCreated(ctx, events.EventObjectCreatedPut, "unconfigured-bucket", "file.txt", 100, "", "", "user")
		if err != nil {
			t.Fatalf("Emit failed: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		if mockTarget.PublishCount() != 0 {
			t.Errorf("Expected 0 publishes (no bucket config), got %d", mockTarget.PublishCount())
		}
	})

	t.Run("Stats", func(t *testing.T) {
		emitter := events.NewEmitter(events.EmitterConfig{
			Enabled:     true,
			QueueConfig: events.DefaultQueueConfig(),
		})

		emitter.SetBucketConfig("bucket1", &events.NotificationConfiguration{})
		emitter.SetBucketConfig("bucket2", &events.NotificationConfiguration{})

		stats := emitter.Stats()
		if !stats.Enabled {
			t.Error("Expected emitter to be enabled")
		}
		if stats.BucketConfigCount != 2 {
			t.Errorf("Expected 2 bucket configs, got %d", stats.BucketConfigCount)
		}
	})
}

func TestNotificationConfiguration(t *testing.T) {
	t.Run("TopicConfiguration", func(t *testing.T) {
		config := &events.NotificationConfiguration{
			TopicConfigurations: []events.TopicConfiguration{
				{
					ID:       "topic-config-1",
					TopicARN: "arn:aws:sns:us-east-1:123456789:my-topic",
					Events:   []events.EventType{events.EventObjectCreatedPut},
				},
			},
		}

		if len(config.TopicConfigurations) != 1 {
			t.Errorf("Expected 1 topic configuration")
		}
	})

	t.Run("QueueConfiguration", func(t *testing.T) {
		config := &events.NotificationConfiguration{
			QueueConfigurations: []events.QueueConfiguration{
				{
					ID:       "queue-config-1",
					QueueARN: "arn:aws:sqs:us-east-1:123456789:my-queue",
					Events:   []events.EventType{events.EventObjectRemoved},
				},
			},
		}

		if len(config.QueueConfigurations) != 1 {
			t.Errorf("Expected 1 queue configuration")
		}
	})
}

func TestTargetRegistry(t *testing.T) {
	t.Run("GetRegisteredTypes", func(t *testing.T) {
		types := events.GetRegisteredTargetTypes()
		if len(types) == 0 {
			t.Error("Expected at least one registered target type")
		}

		// Check for expected types
		hasWebhook := false
		hasKafka := false
		for _, tt := range types {
			if tt == "webhook" {
				hasWebhook = true
			}
			if tt == "kafka" {
				hasKafka = true
			}
		}

		if !hasWebhook {
			t.Error("Expected 'webhook' target type to be registered")
		}
		if !hasKafka {
			t.Error("Expected 'kafka' target type to be registered")
		}
	})

	t.Run("CreateWebhookTarget", func(t *testing.T) {
		config := map[string]interface{}{
			"name": "test-webhook",
			"url":  "http://localhost:8080/webhook",
		}

		target, err := events.CreateTarget("webhook", config)
		if err != nil {
			t.Fatalf("CreateTarget failed: %v", err)
		}

		if target.Name() != "test-webhook" {
			t.Errorf("Expected name 'test-webhook', got '%s'", target.Name())
		}
		if target.Type() != "webhook" {
			t.Errorf("Expected type 'webhook', got '%s'", target.Type())
		}

		_ = target.Close()
	})

	t.Run("CreateUnknownTarget", func(t *testing.T) {
		config := map[string]interface{}{}
		_, err := events.CreateTarget("unknown-type", config)
		if err == nil {
			t.Error("Expected error for unknown target type")
		}
	})
}
