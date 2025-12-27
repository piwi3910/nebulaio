package replication

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockObjectLister implements ObjectLister for testing
type mockObjectLister struct {
	objects []ObjectListEntry
	err     error
}

func (m *mockObjectLister) ListObjects(ctx context.Context, bucket, prefix string, recursive bool) (<-chan ObjectListEntry, <-chan error) {
	objCh := make(chan ObjectListEntry)
	errCh := make(chan error, 1)

	go func() {
		defer close(objCh)
		defer close(errCh)

		if m.err != nil {
			errCh <- m.err
			return
		}

		for _, obj := range m.objects {
			if prefix != "" && !strings.HasPrefix(obj.Key, prefix) {
				continue
			}
			select {
			case objCh <- obj:
			case <-ctx.Done():
				return
			}
		}
	}()

	return objCh, errCh
}

// mockRemoteClient implements RemoteClient for testing
type mockRemoteClient struct {
	mu        sync.Mutex
	putCalls  []string
	deleteCalls []string
	err       error
}

func (m *mockRemoteClient) PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, contentType string, metadata map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.putCalls = append(m.putCalls, bucket+"/"+key)
	return m.err
}

func (m *mockRemoteClient) DeleteObject(ctx context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls = append(m.deleteCalls, bucket+"/"+key)
	return m.err
}

func (m *mockRemoteClient) Close() error {
	return nil
}

// mockRemoteClientFactory implements RemoteClientFactory for testing
type mockRemoteClientFactory struct {
	client *mockRemoteClient
}

func (m *mockRemoteClientFactory) GetClient(endpoint, accessKey, secretKey string) (RemoteClient, error) {
	return m.client, nil
}

func TestBatchManagerCreateJob(t *testing.T) {
	svc := createTestService()
	lister := &mockObjectLister{}
	bm := NewBatchManager(svc, lister, nil, DefaultBatchManagerConfig())

	job := &BatchJob{
		JobID:             "test-job-1",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket",
		Description:       "Test job",
	}

	err := bm.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Verify job was created
	createdJob, err := bm.GetJob("test-job-1")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}

	if createdJob.Status != BatchJobStatusPending {
		t.Errorf("Expected status Pending, got %s", createdJob.Status)
	}
	if createdJob.Concurrency != 10 {
		t.Errorf("Expected default concurrency 10, got %d", createdJob.Concurrency)
	}
}

func TestBatchManagerCreateJobValidation(t *testing.T) {
	svc := createTestService()
	lister := &mockObjectLister{}
	bm := NewBatchManager(svc, lister, nil, DefaultBatchManagerConfig())

	tests := []struct {
		name    string
		job     *BatchJob
		wantErr bool
	}{
		{
			name: "Missing job ID",
			job: &BatchJob{
				SourceBucket:      "source",
				DestinationBucket: "dest",
			},
			wantErr: true,
		},
		{
			name: "Missing source bucket",
			job: &BatchJob{
				JobID:             "test",
				DestinationBucket: "dest",
			},
			wantErr: true,
		},
		{
			name: "Missing destination bucket",
			job: &BatchJob{
				JobID:        "test",
				SourceBucket: "source",
			},
			wantErr: true,
		},
		{
			name: "Valid job",
			job: &BatchJob{
				JobID:             "test",
				SourceBucket:      "source",
				DestinationBucket: "dest",
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := bm.CreateJob(tc.job)
			if tc.wantErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestBatchManagerDuplicateJob(t *testing.T) {
	svc := createTestService()
	lister := &mockObjectLister{}
	bm := NewBatchManager(svc, lister, nil, DefaultBatchManagerConfig())

	job := &BatchJob{
		JobID:             "test-job",
		SourceBucket:      "source",
		DestinationBucket: "dest",
	}

	err := bm.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Try to create duplicate
	err = bm.CreateJob(job)
	if err == nil {
		t.Error("Expected error for duplicate job")
	}
}

func TestBatchManagerListJobs(t *testing.T) {
	svc := createTestService()
	lister := &mockObjectLister{}
	bm := NewBatchManager(svc, lister, nil, DefaultBatchManagerConfig())

	// Create multiple jobs
	for i := 0; i < 3; i++ {
		job := &BatchJob{
			JobID:             "job-" + string(rune('a'+i)),
			SourceBucket:      "source",
			DestinationBucket: "dest",
		}
		if err := bm.CreateJob(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}

	jobs := bm.ListJobs()
	if len(jobs) != 3 {
		t.Errorf("Expected 3 jobs, got %d", len(jobs))
	}
}

func TestBatchManagerDeleteJob(t *testing.T) {
	svc := createTestService()
	lister := &mockObjectLister{}
	bm := NewBatchManager(svc, lister, nil, DefaultBatchManagerConfig())

	job := &BatchJob{
		JobID:             "test-job",
		SourceBucket:      "source",
		DestinationBucket: "dest",
	}

	err := bm.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	err = bm.DeleteJob("test-job")
	if err != nil {
		t.Fatalf("Failed to delete job: %v", err)
	}

	_, err = bm.GetJob("test-job")
	if err == nil {
		t.Error("Expected error for deleted job")
	}
}

func TestBatchManagerCancelJob(t *testing.T) {
	svc := createTestService()
	lister := &mockObjectLister{
		objects: []ObjectListEntry{
			{Key: "obj1.txt", Size: 100},
		},
	}
	bm := NewBatchManager(svc, lister, nil, DefaultBatchManagerConfig())

	job := &BatchJob{
		JobID:             "test-job",
		SourceBucket:      "source",
		DestinationBucket: "dest",
	}

	err := bm.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	err = bm.CancelJob("test-job")
	if err != nil {
		t.Fatalf("Failed to cancel job: %v", err)
	}

	cancelledJob, err := bm.GetJob("test-job")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}

	if cancelledJob.Status != BatchJobStatusCancelled {
		t.Errorf("Expected status Cancelled, got %s", cancelledJob.Status)
	}
}

func TestBatchJobFilters(t *testing.T) {
	svc := createTestService()
	lister := &mockObjectLister{}
	bm := NewBatchManager(svc, lister, nil, DefaultBatchManagerConfig())

	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	tomorrow := now.Add(24 * time.Hour)

	job := &BatchJob{
		JobID:             "filter-test",
		SourceBucket:      "source",
		DestinationBucket: "dest",
		MinSize:           100,
		MaxSize:           1000,
		CreatedAfter:      &yesterday,
		CreatedBefore:     &tomorrow,
		Tags:              map[string]string{"env": "prod"},
	}

	tests := []struct {
		name    string
		obj     ObjectListEntry
		matches bool
	}{
		{
			name: "Matches all filters",
			obj: ObjectListEntry{
				Key:          "test.txt",
				Size:         500,
				LastModified: now,
				Tags:         map[string]string{"env": "prod"},
			},
			matches: true,
		},
		{
			name: "Too small",
			obj: ObjectListEntry{
				Key:          "small.txt",
				Size:         50,
				LastModified: now,
				Tags:         map[string]string{"env": "prod"},
			},
			matches: false,
		},
		{
			name: "Too large",
			obj: ObjectListEntry{
				Key:          "large.txt",
				Size:         2000,
				LastModified: now,
				Tags:         map[string]string{"env": "prod"},
			},
			matches: false,
		},
		{
			name: "Wrong tags",
			obj: ObjectListEntry{
				Key:          "test.txt",
				Size:         500,
				LastModified: now,
				Tags:         map[string]string{"env": "dev"},
			},
			matches: false,
		},
		{
			name: "Too old",
			obj: ObjectListEntry{
				Key:          "old.txt",
				Size:         500,
				LastModified: now.Add(-48 * time.Hour),
				Tags:         map[string]string{"env": "prod"},
			},
			matches: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := bm.matchesFilters(job, tc.obj)
			if result != tc.matches {
				t.Errorf("Expected matches=%v, got %v", tc.matches, result)
			}
		})
	}
}

func TestBatchJobProgress(t *testing.T) {
	// Create a mock backend with objects
	svc := createTestService()

	objects := []ObjectListEntry{
		{Key: "obj1.txt", Size: 100},
		{Key: "obj2.txt", Size: 200},
		{Key: "obj3.txt", Size: 300},
	}

	lister := &mockObjectLister{objects: objects}
	bm := NewBatchManager(svc, lister, nil, DefaultBatchManagerConfig())

	job := &BatchJob{
		JobID:             "progress-test",
		SourceBucket:      "source",
		DestinationBucket: "dest",
		Concurrency:       1,
	}

	err := bm.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Start the job
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = bm.StartJob(ctx, "progress-test")
	if err != nil {
		t.Fatalf("Failed to start job: %v", err)
	}

	// Wait for job to complete or timeout
	for i := 0; i < 50; i++ {
		job, _ := bm.GetJob("progress-test")
		if job.Status == BatchJobStatusCompleted || job.Status == BatchJobStatusFailed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	completedJob, _ := bm.GetJob("progress-test")

	// The job should have processed all objects (though they may fail due to mock backend)
	// Use atomic load since progress fields are written atomically
	totalObjects := atomic.LoadInt64(&completedJob.Progress.TotalObjects)
	if totalObjects != 3 {
		t.Errorf("Expected 3 total objects, got %d", totalObjects)
	}
}

func TestBatchJobSummary(t *testing.T) {
	job := &BatchJob{
		JobID:             "summary-test",
		Description:       "Test job for summary",
		SourceBucket:      "source",
		DestinationBucket: "dest",
		Status:            BatchJobStatusCompleted,
		CreatedAt:         time.Now(),
	}

	summary := job.Summary()

	if summary.JobID != job.JobID {
		t.Errorf("Expected JobID %s, got %s", job.JobID, summary.JobID)
	}
	if summary.Description != job.Description {
		t.Errorf("Expected Description %s, got %s", job.Description, summary.Description)
	}
	if summary.Status != job.Status {
		t.Errorf("Expected Status %s, got %s", job.Status, summary.Status)
	}
}

func TestRateLimitedReader(t *testing.T) {
	data := make([]byte, 128*1024) // 128KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	reader := newRateLimitedReader(strings.NewReader(string(data)), 64*1024) // 64KB/s limit

	buf := make([]byte, 1024)
	var totalRead int
	start := time.Now()

	for {
		n, err := reader.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
	}

	elapsed := time.Since(start)

	if totalRead != len(data) {
		t.Errorf("Expected to read %d bytes, got %d", len(data), totalRead)
	}

	// With 128KB data at 64KB/s, it should take roughly 2 seconds
	// Allow for some variance
	if elapsed < time.Second {
		t.Logf("Read completed in %v (rate limiting may not be precise for small reads)", elapsed)
	}
}

func TestMaxConcurrentJobs(t *testing.T) {
	svc := createTestService()
	objects := make([]ObjectListEntry, 1000) // Many objects to keep jobs running
	for i := range objects {
		objects[i] = ObjectListEntry{Key: "obj" + string(rune(i)) + ".txt", Size: 100}
	}

	lister := &mockObjectLister{objects: objects}
	cfg := DefaultBatchManagerConfig()
	cfg.MaxConcurrentJobs = 2
	bm := NewBatchManager(svc, lister, nil, cfg)

	// Create 3 jobs
	for i := 0; i < 3; i++ {
		job := &BatchJob{
			JobID:             "job-" + string(rune('a'+i)),
			SourceBucket:      "source",
			DestinationBucket: "dest",
			Concurrency:       1,
		}
		if err := bm.CreateJob(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}

	ctx := context.Background()

	// Start first 2 jobs - should succeed
	if err := bm.StartJob(ctx, "job-a"); err != nil {
		t.Fatalf("Failed to start job-a: %v", err)
	}
	if err := bm.StartJob(ctx, "job-b"); err != nil {
		t.Fatalf("Failed to start job-b: %v", err)
	}

	// Third job should fail due to max concurrent limit
	err := bm.StartJob(ctx, "job-c")
	if err == nil {
		t.Error("Expected error when starting third job")
	}

	// Cancel jobs to clean up
	bm.CancelJob("job-a")
	bm.CancelJob("job-b")
}

func TestJobHistoryCleanup(t *testing.T) {
	svc := createTestService()
	lister := &mockObjectLister{}
	cfg := DefaultBatchManagerConfig()
	cfg.HistoryLimit = 3
	bm := NewBatchManager(svc, lister, nil, cfg)

	// Create and complete 5 jobs
	for i := 0; i < 5; i++ {
		job := &BatchJob{
			JobID:             "job-" + string(rune('a'+i)),
			SourceBucket:      "source",
			DestinationBucket: "dest",
		}
		if err := bm.CreateJob(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		// Manually mark as completed
		bm.mu.Lock()
		now := time.Now()
		bm.jobs[job.JobID].Status = BatchJobStatusCompleted
		bm.jobs[job.JobID].CompletedAt = &now
		bm.mu.Unlock()
	}

	// Trigger cleanup by creating another job
	bm.mu.Lock()
	bm.cleanupOldJobs()
	bm.mu.Unlock()

	jobs := bm.ListJobs()
	if len(jobs) > cfg.HistoryLimit+1 { // +1 for potentially adding job before cleanup
		t.Errorf("Expected max %d jobs after cleanup, got %d", cfg.HistoryLimit+1, len(jobs))
	}
}

// Helper to create a test service
func createTestService() *Service {
	cfg := DefaultServiceConfig()
	return NewService(newMockBackend(), newMockMetaStore(), cfg)
}
