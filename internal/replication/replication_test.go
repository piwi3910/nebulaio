package replication

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConfigValidation(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		cfg := &Config{
			Role: "arn:aws:iam::123456789012:role/replication-role",
			Rules: []Rule{
				{
					ID:       "rule1",
					Priority: 1,
					Status:   RuleStatusEnabled,
					Destination: Destination{
						Bucket:    "dest-bucket",
						Endpoint:  "s3.example.com",
						AccessKey: "AKIAIOSFODNN7EXAMPLE",
						SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					},
				},
			},
		}

		err := cfg.Validate()
		if err != nil {
			t.Errorf("valid config should not error: %v", err)
		}
	})

	t.Run("EmptyRules", func(t *testing.T) {
		cfg := &Config{
			Role:  "test-role",
			Rules: []Rule{},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for empty rules")
		}
	})

	t.Run("DuplicateRuleID", func(t *testing.T) {
		cfg := &Config{
			Rules: []Rule{
				{
					ID:       "rule1",
					Priority: 1,
					Status:   RuleStatusEnabled,
					Destination: Destination{
						Bucket:    "dest1",
						Endpoint:  "s3.example.com",
						AccessKey: "key",
						SecretKey: "secret",
					},
				},
				{
					ID:       "rule1", // Duplicate
					Priority: 2,
					Status:   RuleStatusEnabled,
					Destination: Destination{
						Bucket:    "dest2",
						Endpoint:  "s3.example.com",
						AccessKey: "key",
						SecretKey: "secret",
					},
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for duplicate rule ID")
		}
	})

	t.Run("InvalidRuleID", func(t *testing.T) {
		cfg := &Config{
			Rules: []Rule{
				{
					ID:       "rule with spaces",
					Priority: 1,
					Status:   RuleStatusEnabled,
					Destination: Destination{
						Bucket:    "dest",
						Endpoint:  "s3.example.com",
						AccessKey: "key",
						SecretKey: "secret",
					},
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for invalid rule ID")
		}
	})

	t.Run("MissingDestination", func(t *testing.T) {
		cfg := &Config{
			Rules: []Rule{
				{
					ID:       "rule1",
					Priority: 1,
					Status:   RuleStatusEnabled,
					Destination: Destination{
						// Missing bucket and endpoint
						AccessKey: "key",
						SecretKey: "secret",
					},
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing destination bucket")
		}
	})
}

func TestFilterMatching(t *testing.T) {
	t.Run("NilFilter", func(t *testing.T) {
		var f *Filter
		if !f.Matches("any/key", nil) {
			t.Error("nil filter should match everything")
		}
	})

	t.Run("PrefixMatch", func(t *testing.T) {
		f := &Filter{Prefix: "logs/"}

		if !f.Matches("logs/file.txt", nil) {
			t.Error("should match prefix")
		}

		if f.Matches("data/file.txt", nil) {
			t.Error("should not match different prefix")
		}
	})

	t.Run("TagMatch", func(t *testing.T) {
		f := &Filter{
			Tags: map[string]string{
				"env": "prod",
			},
		}

		if !f.Matches("any/key", map[string]string{"env": "prod"}) {
			t.Error("should match tags")
		}

		if f.Matches("any/key", map[string]string{"env": "dev"}) {
			t.Error("should not match different tag value")
		}

		if f.Matches("any/key", nil) {
			t.Error("should not match when tags missing")
		}
	})

	t.Run("PrefixAndTagMatch", func(t *testing.T) {
		f := &Filter{
			Prefix: "logs/",
			Tags:   map[string]string{"type": "access"},
		}

		if !f.Matches("logs/access.log", map[string]string{"type": "access"}) {
			t.Error("should match prefix and tags")
		}

		if f.Matches("logs/access.log", nil) {
			t.Error("should not match without tags")
		}

		if f.Matches("data/access.log", map[string]string{"type": "access"}) {
			t.Error("should not match wrong prefix")
		}
	})
}

func TestRuleHelpers(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		r := &Rule{Status: RuleStatusEnabled}
		if !r.IsEnabled() {
			t.Error("should be enabled")
		}

		r.Status = RuleStatusDisabled
		if r.IsEnabled() {
			t.Error("should be disabled")
		}
	})

	t.Run("ShouldReplicateDeleteMarkers", func(t *testing.T) {
		r := &Rule{}
		if r.ShouldReplicateDeleteMarkers() {
			t.Error("should not replicate delete markers by default")
		}

		r.DeleteMarkerReplication = &DeleteMarkerReplication{Status: RuleStatusEnabled}
		if !r.ShouldReplicateDeleteMarkers() {
			t.Error("should replicate delete markers when enabled")
		}
	})

	t.Run("ShouldReplicateExistingObjects", func(t *testing.T) {
		r := &Rule{}
		if r.ShouldReplicateExistingObjects() {
			t.Error("should not replicate existing objects by default")
		}

		r.ExistingObjectReplication = &ExistingObjectReplication{Status: RuleStatusEnabled}
		if !r.ShouldReplicateExistingObjects() {
			t.Error("should replicate existing objects when enabled")
		}
	})
}

func TestQueue(t *testing.T) {
	cfg := QueueConfig{
		MaxSize:  100,
		MaxRetry: 3,
	}
	q := NewQueue(cfg)
	ctx := context.Background()

	t.Run("EnqueueDequeue", func(t *testing.T) { testQueueEnqueueDequeue(t, ctx, q) })
	t.Run("Complete", func(t *testing.T) { testQueueComplete(t, ctx, q) })
	t.Run("Fail", func(t *testing.T) { testQueueFail(t, ctx, q) })
	t.Run("MaxRetry", func(t *testing.T) { testQueueMaxRetry(t, ctx, q) })
	t.Run("Stats", func(t *testing.T) { testQueueStats(t, q) })
	t.Run("ListByStatus", func(t *testing.T) { testQueueListByStatus(t, q) })
}

func testQueueEnqueueDequeue(t *testing.T, ctx context.Context, q *Queue) {
	item, err := q.Enqueue(ctx, "bucket", "key", "v1", "PUT", "rule1")
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	if item.Bucket != "bucket" || item.Key != "key" {
		t.Error("item data mismatch")
	}

	dequeued, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue failed: %v", err)
	}

	if dequeued.ID != item.ID {
		t.Error("dequeued wrong item")
	}
}

func testQueueComplete(t *testing.T, ctx context.Context, q *Queue) {
	item, _ := q.Enqueue(ctx, "bucket", "key2", "", "PUT", "rule1")
	_, _ = q.Dequeue(ctx)

	err := q.Complete(item.ID)
	if err != nil {
		t.Errorf("complete failed: %v", err)
	}

	_, err = q.Get(item.ID)
	if err == nil {
		t.Error("item should be removed after completion")
	}
}

func testQueueFail(t *testing.T, ctx context.Context, q *Queue) {
	item, _ := q.Enqueue(ctx, "bucket", "key3", "", "PUT", "rule1")
	_, _ = q.Dequeue(ctx)

	err := q.Fail(item.ID, io.EOF)
	if err != nil {
		t.Errorf("fail failed: %v", err)
	}

	retrieved, err := q.Get(item.ID)
	if err != nil {
		t.Errorf("should be able to get failed item: %v", err)
	}

	if retrieved.RetryCount != 1 {
		t.Errorf("retry count should be 1, got %d", retrieved.RetryCount)
	}
}

func testQueueMaxRetry(t *testing.T, ctx context.Context, q *Queue) {
	item, _ := q.Enqueue(ctx, "bucket", "key4", "", "PUT", "rule1")
	_, _ = q.Dequeue(ctx)

	// Fail until max retry
	for range 3 {
		_ = q.Fail(item.ID, io.EOF)

		time.Sleep(10 * time.Millisecond)
	}

	retrieved, _ := q.Get(item.ID)
	if retrieved.Status != QueueStatusFailed {
		t.Errorf("should be failed after max retries, got %s", retrieved.Status)
	}
}

func testQueueStats(t *testing.T, q *Queue) {
	stats := q.Stats()
	if stats.Total < 0 {
		t.Error("invalid stats")
	}
}

func testQueueListByStatus(t *testing.T, q *Queue) {
	items := q.ListByStatus(QueueStatusFailed)
	for _, item := range items {
		if item.Status != QueueStatusFailed {
			t.Error("listed item has wrong status")
		}
	}
}

// mockBackend implements ObjectBackend for testing
// Thread-safe with RWMutex protection for concurrent access.
type mockBackend struct {
	objects map[string][]byte
	mu      sync.RWMutex
}

func newMockBackend() *mockBackend {
	return &mockBackend{
		objects: make(map[string][]byte),
	}
}

func (m *mockBackend) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	k := bucket + "/" + key

	data, ok := m.objects[k]
	if !ok {
		return nil, io.EOF
	}

	return io.NopCloser(strings.NewReader(string(data))), nil
}

func (m *mockBackend) GetObjectInfo(_ context.Context, bucket, key string) (*ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	k := bucket + "/" + key

	data, ok := m.objects[k]
	if !ok {
		return nil, io.EOF
	}

	return &ObjectInfo{
		Key:          key,
		Size:         int64(len(data)),
		ContentType:  "application/octet-stream",
		UserMetadata: make(map[string]string),
	}, nil
}

func (m *mockBackend) PutObject(bucket, key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.objects[bucket+"/"+key] = data
}

// mockMetaStore implements MetadataStore for testing
// Thread-safe with RWMutex protection for concurrent access.
type mockMetaStore struct {
	configs  map[string]*Config
	statuses map[string]*ReplicationStatus
	mu       sync.RWMutex
}

func newMockMetaStore() *mockMetaStore {
	return &mockMetaStore{
		configs:  make(map[string]*Config),
		statuses: make(map[string]*ReplicationStatus),
	}
}

func (m *mockMetaStore) GetReplicationConfig(_ context.Context, bucket string) (*Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, ok := m.configs[bucket]
	if !ok {
		return nil, io.EOF
	}

	return cfg, nil
}

func (m *mockMetaStore) SetReplicationConfig(_ context.Context, bucket string, config *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.configs[bucket] = config

	return nil
}

func (m *mockMetaStore) DeleteReplicationConfig(_ context.Context, bucket string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.configs, bucket)

	return nil
}

func (m *mockMetaStore) GetReplicationStatus(_ context.Context, bucket, key, versionID string) (*ReplicationStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	k := bucket + "/" + key + "/" + versionID

	status, ok := m.statuses[k]
	if !ok {
		return nil, io.EOF
	}

	return status, nil
}

func (m *mockMetaStore) SetReplicationStatus(_ context.Context, bucket, key, versionID string, status *ReplicationStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := bucket + "/" + key + "/" + versionID
	m.statuses[k] = status

	return nil
}

func TestService(t *testing.T) {
	backend := newMockBackend()
	metaStore := newMockMetaStore()
	cfg := DefaultServiceConfig()

	svc := NewService(backend, metaStore, cfg)
	ctx := context.Background()

	t.Run("SetAndGetConfig", func(t *testing.T) {
		config := &Config{
			Rules: []Rule{
				{
					ID:       "rule1",
					Priority: 1,
					Status:   RuleStatusEnabled,
					Destination: Destination{
						Bucket:    "dest-bucket",
						Endpoint:  "s3.example.com",
						AccessKey: "key",
						SecretKey: "secret",
					},
				},
			},
		}

		err := svc.SetConfig(ctx, "source-bucket", config)
		if err != nil {
			t.Fatalf("set config failed: %v", err)
		}

		retrieved, err := svc.GetConfig(ctx, "source-bucket")
		if err != nil {
			t.Fatalf("get config failed: %v", err)
		}

		if len(retrieved.Rules) != 1 {
			t.Error("config mismatch")
		}
	})

	t.Run("DeleteConfig", func(t *testing.T) {
		err := svc.DeleteConfig(ctx, "source-bucket")
		if err != nil {
			t.Fatalf("delete config failed: %v", err)
		}

		_, err = svc.GetConfig(ctx, "source-bucket")
		if err == nil {
			t.Error("config should be deleted")
		}
	})

	t.Run("ListConfigs", func(t *testing.T) {
		// Set a config
		config := &Config{
			Rules: []Rule{
				{
					ID:       "rule1",
					Priority: 1,
					Status:   RuleStatusEnabled,
					Destination: Destination{
						Bucket:    "dest",
						Endpoint:  "s3.example.com",
						AccessKey: "key",
						SecretKey: "secret",
					},
				},
			},
		}
		_ = svc.SetConfig(ctx, "bucket1", config)

		configs := svc.ListConfigs()
		if len(configs) < 1 {
			t.Error("should have at least one config")
		}
	})

	t.Run("GetQueueStats", func(t *testing.T) {
		stats := svc.GetQueueStats()
		// Just ensure it doesn't panic and returns valid data
		if stats.Total < 0 {
			t.Error("invalid stats")
		}
	})
}

func TestReplicationStatus(t *testing.T) {
	t.Run("CalculateOverallStatus", func(t *testing.T) {
		svc := &Service{}

		// All complete
		destinations := map[string]DestinationStatus{
			"dest1": {Status: ReplicationStatusComplete},
			"dest2": {Status: ReplicationStatusComplete},
		}
		if svc.calculateOverallStatus(destinations) != ReplicationStatusComplete {
			t.Error("should be complete when all complete")
		}

		// One pending
		destinations["dest2"] = DestinationStatus{Status: ReplicationStatusPending}
		if svc.calculateOverallStatus(destinations) != ReplicationStatusPending {
			t.Error("should be pending when any pending")
		}

		// One failed
		destinations["dest2"] = DestinationStatus{Status: ReplicationStatusFailed}
		if svc.calculateOverallStatus(destinations) != ReplicationStatusFailed {
			t.Error("should be failed when any failed")
		}

		// Empty
		if svc.calculateOverallStatus(nil) != ReplicationStatusNoReplication {
			t.Error("should be empty for no destinations")
		}
	})
}

func TestQueueStats(t *testing.T) {
	stats := QueueStats{
		Total:      10,
		Pending:    5,
		InProgress: 2,
		Completed:  2,
		Failed:     1,
	}

	data, err := stats.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("empty JSON output")
	}
}
