package catalog

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

// mockObjectLister implements ObjectLister for testing.
type mockObjectLister struct {
	objects []ObjectInfo
}

func (m *mockObjectLister) ListObjects(ctx context.Context, bucket, prefix string, recursive bool) (<-chan ObjectInfo, <-chan error) {
	objects := make(chan ObjectInfo)
	errs := make(chan error, 1)

	go func() {
		defer close(objects)
		defer close(errs)

		for _, obj := range m.objects {
			if bucket != "" && obj.Bucket != bucket {
				continue
			}

			select {
			case <-ctx.Done():
				return
			case objects <- obj:
			}
		}
	}()

	return objects, errs
}

// mockObjectWriter implements ObjectWriter for testing.
type mockObjectWriter struct {
	objects map[string][]byte
	mu      sync.Mutex
}

func newMockObjectWriter() *mockObjectWriter {
	return &mockObjectWriter{
		objects: make(map[string][]byte),
	}
}

func (m *mockObjectWriter) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.objects[bucket+"/"+key] = data
	m.mu.Unlock()

	return nil
}

func TestCreateInventoryConfig(t *testing.T) {
	lister := &mockObjectLister{}
	writer := newMockObjectWriter()
	svc := NewCatalogService(lister, writer, CatalogConfig{})

	cfg := &InventoryConfig{
		SourceBucket:      "test-bucket",
		DestinationBucket: "inventory-bucket",
		Format:            FormatCSV,
		Frequency:         FrequencyDaily,
	}

	err := svc.CreateInventoryConfig(cfg)
	if err != nil {
		t.Fatalf("CreateInventoryConfig failed: %v", err)
	}

	if cfg.ID == "" {
		t.Error("Config ID should be auto-generated")
	}

	if len(cfg.IncludedFields) == 0 {
		t.Error("Default fields should be set")
	}

	// Retrieve and verify
	retrieved, err := svc.GetInventoryConfig(cfg.ID)
	if err != nil {
		t.Fatalf("GetInventoryConfig failed: %v", err)
	}

	if retrieved.SourceBucket != "test-bucket" {
		t.Errorf("Expected source bucket 'test-bucket', got '%s'", retrieved.SourceBucket)
	}
}

func TestListInventoryConfigs(t *testing.T) {
	lister := &mockObjectLister{}
	writer := newMockObjectWriter()
	svc := NewCatalogService(lister, writer, CatalogConfig{})

	// Create multiple configs
	for i := range 3 {
		cfg := &InventoryConfig{
			SourceBucket:      "bucket-" + string(rune('a'+i)),
			DestinationBucket: "inventory-bucket",
		}
		svc.CreateInventoryConfig(cfg)
	}

	// List all
	configs := svc.ListInventoryConfigs("")
	if len(configs) != 3 {
		t.Errorf("Expected 3 configs, got %d", len(configs))
	}

	// List filtered
	configs = svc.ListInventoryConfigs("bucket-a")
	if len(configs) != 1 {
		t.Errorf("Expected 1 config for bucket-a, got %d", len(configs))
	}
}

func TestDeleteInventoryConfig(t *testing.T) {
	lister := &mockObjectLister{}
	writer := newMockObjectWriter()
	svc := NewCatalogService(lister, writer, CatalogConfig{})

	cfg := &InventoryConfig{
		SourceBucket:      "test-bucket",
		DestinationBucket: "inventory-bucket",
	}
	svc.CreateInventoryConfig(cfg)

	err := svc.DeleteInventoryConfig(cfg.ID)
	if err != nil {
		t.Fatalf("DeleteInventoryConfig failed: %v", err)
	}

	_, err = svc.GetInventoryConfig(cfg.ID)
	if err == nil {
		t.Error("Expected error when getting deleted config")
	}
}

func TestStartInventoryJob(t *testing.T) {
	now := time.Now()
	objects := []ObjectInfo{
		{
			Bucket:       "test-bucket",
			Key:          "file1.txt",
			Size:         100,
			LastModified: now,
			ETag:         "etag1",
			StorageClass: "STANDARD",
		},
		{
			Bucket:       "test-bucket",
			Key:          "file2.txt",
			Size:         200,
			LastModified: now,
			ETag:         "etag2",
			StorageClass: "STANDARD",
		},
	}

	lister := &mockObjectLister{objects: objects}
	writer := newMockObjectWriter()
	svc := NewCatalogService(lister, writer, CatalogConfig{
		Concurrency: 2,
		BatchSize:   100,
	})

	cfg := &InventoryConfig{
		SourceBucket:      "test-bucket",
		DestinationBucket: "inventory-bucket",
		Format:            FormatCSV,
	}
	svc.CreateInventoryConfig(cfg)

	ctx := context.Background()

	job, err := svc.StartInventoryJob(ctx, cfg.ID)
	if err != nil {
		t.Fatalf("StartInventoryJob failed: %v", err)
	}

	if job.ID == "" {
		t.Error("Job ID should be set")
	}

	// Wait for job to complete
	time.Sleep(500 * time.Millisecond)

	job, err = svc.GetInventoryJob(job.ID)
	if err != nil {
		t.Fatalf("GetInventoryJob failed: %v", err)
	}

	if job.Status != JobStatusCompleted {
		t.Errorf("Expected job status 'completed', got '%s'", job.Status)
	}

	if job.Progress.ProcessedObjects != 2 {
		t.Errorf("Expected 2 processed objects, got %d", job.Progress.ProcessedObjects)
	}
}

func TestInventoryFilter(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)

	objects := []ObjectInfo{
		{
			Bucket:       "test-bucket",
			Key:          "prefix1/file1.txt",
			Size:         100,
			LastModified: now,
			StorageClass: "STANDARD",
		},
		{
			Bucket:       "test-bucket",
			Key:          "prefix2/file2.txt",
			Size:         2000,
			LastModified: yesterday,
			StorageClass: "GLACIER",
		},
		{
			Bucket:       "test-bucket",
			Key:          "prefix1/file3.txt",
			Size:         500,
			LastModified: now,
			StorageClass: "STANDARD",
		},
	}

	lister := &mockObjectLister{objects: objects}
	writer := newMockObjectWriter()
	svc := NewCatalogService(lister, writer, CatalogConfig{})

	minSize := int64(200)
	cfg := &InventoryConfig{
		SourceBucket:      "test-bucket",
		DestinationBucket: "inventory-bucket",
		Filter: &InventoryFilter{
			Prefix:  "prefix1/",
			MinSize: &minSize,
		},
	}
	svc.CreateInventoryConfig(cfg)

	ctx := context.Background()
	job, _ := svc.StartInventoryJob(ctx, cfg.ID)

	// Wait for job
	time.Sleep(500 * time.Millisecond)

	job, _ = svc.GetInventoryJob(job.ID)

	// Only file3.txt matches (prefix1/ and size >= 200)
	if job.Progress.ProcessedObjects != 1 {
		t.Errorf("Expected 1 filtered object, got %d", job.Progress.ProcessedObjects)
	}
}

func TestCancelInventoryJob(t *testing.T) {
	// Create many objects to ensure job takes time
	const numObjects = 10000

	objects := make([]ObjectInfo, 0, numObjects)
	for i := range numObjects {
		objects = append(objects, ObjectInfo{
			Bucket:       "test-bucket",
			Key:          "file" + string(rune(i)) + ".txt",
			Size:         100,
			LastModified: time.Now(),
		})
	}

	lister := &mockObjectLister{objects: objects}
	writer := newMockObjectWriter()
	svc := NewCatalogService(lister, writer, CatalogConfig{
		BatchSize: 100,
	})

	cfg := &InventoryConfig{
		SourceBucket:      "test-bucket",
		DestinationBucket: "inventory-bucket",
	}
	svc.CreateInventoryConfig(cfg)

	ctx := context.Background()
	job, _ := svc.StartInventoryJob(ctx, cfg.ID)

	// Cancel immediately
	time.Sleep(10 * time.Millisecond)

	err := svc.CancelInventoryJob(job.ID)
	if err != nil {
		t.Fatalf("CancelInventoryJob failed: %v", err)
	}

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	job, _ = svc.GetInventoryJob(job.ID)
	if job.Status != JobStatusCancelled {
		t.Errorf("Expected job status 'cancelled', got '%s'", job.Status)
	}
}

func TestListInventoryJobs(t *testing.T) {
	lister := &mockObjectLister{objects: []ObjectInfo{
		{Bucket: "bucket-a", Key: "file.txt", Size: 100, LastModified: time.Now()},
	}}
	writer := newMockObjectWriter()
	svc := NewCatalogService(lister, writer, CatalogConfig{})

	// Create two configs
	cfg1 := &InventoryConfig{
		SourceBucket:      "bucket-a",
		DestinationBucket: "inventory",
	}
	cfg2 := &InventoryConfig{
		SourceBucket:      "bucket-b",
		DestinationBucket: "inventory",
	}

	svc.CreateInventoryConfig(cfg1)
	svc.CreateInventoryConfig(cfg2)

	ctx := context.Background()
	svc.StartInventoryJob(ctx, cfg1.ID)
	svc.StartInventoryJob(ctx, cfg1.ID)
	svc.StartInventoryJob(ctx, cfg2.ID)

	time.Sleep(200 * time.Millisecond)

	// List all jobs
	jobs := svc.ListInventoryJobs("")
	if len(jobs) != 3 {
		t.Errorf("Expected 3 jobs, got %d", len(jobs))
	}

	// List by config
	jobs = svc.ListInventoryJobs(cfg1.ID)
	if len(jobs) != 2 {
		t.Errorf("Expected 2 jobs for config1, got %d", len(jobs))
	}
}

func TestAllInventoryFields(t *testing.T) {
	fields := AllInventoryFields()

	if len(fields) < 10 {
		t.Errorf("Expected at least 10 inventory fields, got %d", len(fields))
	}

	// Verify some expected fields
	expected := map[InventoryField]bool{
		FieldBucket:       true,
		FieldKey:          true,
		FieldSize:         true,
		FieldLastModified: true,
		FieldETag:         true,
	}

	for _, f := range fields {
		delete(expected, f)
	}

	if len(expected) > 0 {
		t.Errorf("Missing expected fields: %v", expected)
	}
}

func TestS3InventoryAPIs(t *testing.T) {
	lister := &mockObjectLister{}
	writer := newMockObjectWriter()
	svc := NewCatalogService(lister, writer, CatalogConfig{})

	// PutBucketInventoryConfiguration
	cfg := &InventoryConfig{
		ID:                "my-inventory",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket",
	}

	err := svc.PutBucketInventoryConfiguration(cfg)
	if err != nil {
		t.Fatalf("PutBucketInventoryConfiguration failed: %v", err)
	}

	// GetBucketInventoryConfiguration
	retrieved, err := svc.GetBucketInventoryConfiguration("source-bucket", "my-inventory")
	if err != nil {
		t.Fatalf("GetBucketInventoryConfiguration failed: %v", err)
	}

	if retrieved.ID != "my-inventory" {
		t.Errorf("Expected ID 'my-inventory', got '%s'", retrieved.ID)
	}

	// ListBucketInventoryConfigurations
	configs := svc.ListBucketInventoryConfigurations("source-bucket")
	if len(configs) != 1 {
		t.Errorf("Expected 1 config, got %d", len(configs))
	}

	// DeleteBucketInventoryConfiguration
	err = svc.DeleteBucketInventoryConfiguration("source-bucket", "my-inventory")
	if err != nil {
		t.Fatalf("DeleteBucketInventoryConfiguration failed: %v", err)
	}

	configs = svc.ListBucketInventoryConfigurations("source-bucket")
	if len(configs) != 0 {
		t.Errorf("Expected 0 configs after delete, got %d", len(configs))
	}
}

func TestMatchesFilter(t *testing.T) {
	svc := &CatalogService{}

	tests := []struct {
		obj    ObjectInfo
		filter *InventoryFilter
		name   string
		want   bool
	}{
		{
			name:   "nil filter matches all",
			obj:    ObjectInfo{Key: "any/file.txt", Size: 100},
			filter: nil,
			want:   true,
		},
		{
			name:   "prefix match",
			obj:    ObjectInfo{Key: "prefix/file.txt"},
			filter: &InventoryFilter{Prefix: "prefix/"},
			want:   true,
		},
		{
			name:   "prefix no match",
			obj:    ObjectInfo{Key: "other/file.txt"},
			filter: &InventoryFilter{Prefix: "prefix/"},
			want:   false,
		},
		{
			name:   "min size match",
			obj:    ObjectInfo{Key: "file.txt", Size: 1000},
			filter: &InventoryFilter{MinSize: ptrInt64(500)},
			want:   true,
		},
		{
			name:   "min size no match",
			obj:    ObjectInfo{Key: "file.txt", Size: 100},
			filter: &InventoryFilter{MinSize: ptrInt64(500)},
			want:   false,
		},
		{
			name:   "storage class match",
			obj:    ObjectInfo{Key: "file.txt", StorageClass: "GLACIER"},
			filter: &InventoryFilter{StorageClass: "GLACIER"},
			want:   true,
		},
		{
			name:   "storage class no match",
			obj:    ObjectInfo{Key: "file.txt", StorageClass: "STANDARD"},
			filter: &InventoryFilter{StorageClass: "GLACIER"},
			want:   false,
		},
		{
			name:   "tags match",
			obj:    ObjectInfo{Key: "file.txt", Tags: map[string]string{"env": "prod"}},
			filter: &InventoryFilter{Tags: map[string]string{"env": "prod"}},
			want:   true,
		},
		{
			name:   "tags no match",
			obj:    ObjectInfo{Key: "file.txt", Tags: map[string]string{"env": "dev"}},
			filter: &InventoryFilter{Tags: map[string]string{"env": "prod"}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.matchesFilter(tt.obj, tt.filter)
			if got != tt.want {
				t.Errorf("matchesFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func ptrInt64(v int64) *int64 {
	return &v
}
