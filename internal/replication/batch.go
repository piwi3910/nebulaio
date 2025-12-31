package replication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Batch manager configuration defaults.
const (
	defaultMaxConcurrentJobs = 5
	defaultHistoryLimit      = 100
	retryMultiplier          = 2
	streamingBufferThreshold = 65536 // 64KB
	nanosecondsPerSecond     = 1e9
)

// BatchJobStatus represents the status of a batch replication job.
type BatchJobStatus string

const (
	BatchJobStatusPending   BatchJobStatus = "Pending"
	BatchJobStatusRunning   BatchJobStatus = "Running"
	BatchJobStatusCompleted BatchJobStatus = "Completed"
	BatchJobStatusFailed    BatchJobStatus = "Failed"
	BatchJobStatusCancelled BatchJobStatus = "Cancelled"
	BatchJobStatusPaused    BatchJobStatus = "Paused"
)

// BatchJob represents a batch replication job.
type BatchJob struct {
	CreatedAt            time.Time  `json:"createdAt"`
	StartedAt            *time.Time `json:"startedAt,omitempty"`
	CreatedBefore        *time.Time `json:"createdBefore,omitempty"`
	resumeCh             chan struct{}
	pauseCh              chan struct{}
	cancelFunc           context.CancelFunc
	Tags                 map[string]string `json:"tags,omitempty"`
	CreatedAfter         *time.Time        `json:"createdAfter,omitempty"`
	CompletedAt          *time.Time        `json:"completedAt,omitempty"`
	Description          string            `json:"description,omitempty"`
	JobID                string            `json:"jobId"`
	SourceBucket         string            `json:"sourceBucket"`
	DestinationBucket    string            `json:"destinationBucket"`
	DestinationEndpoint  string            `json:"destinationEndpoint,omitempty"`
	Prefix               string            `json:"prefix,omitempty"`
	Status               BatchJobStatus    `json:"status"`
	Error                string            `json:"error,omitempty"`
	Progress             BatchJobProgress  `json:"progress"`
	Concurrency          int               `json:"concurrency"`
	MaxRetries           int               `json:"maxRetries"`
	Priority             int               `json:"priority"`
	RateLimitBytesPerSec int64             `json:"rateLimitBytesPerSec,omitempty"`
	MaxSize              int64             `json:"maxSize,omitempty"`
	MinSize              int64             `json:"minSize,omitempty"`
	mu                   sync.RWMutex      `json:"-"` // Protects Status and Error fields
}

// GetStatus safely returns the job status.
func (job *BatchJob) GetStatus() BatchJobStatus {
	job.mu.RLock()
	defer job.mu.RUnlock()

	return job.Status
}

// SetStatus safely sets the job status.
func (job *BatchJob) SetStatus(status BatchJobStatus) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Status = status
}

// SetError safely sets the job error.
func (job *BatchJob) SetError(err string) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.Error = err
}

// BatchJobProgress tracks job progress.
type BatchJobProgress struct {
	TotalObjects           int64 `json:"totalObjects"`
	ProcessedObjects       int64 `json:"processedObjects"`
	SuccessObjects         int64 `json:"successObjects"`
	FailedObjects          int64 `json:"failedObjects"`
	SkippedObjects         int64 `json:"skippedObjects"`
	TotalBytes             int64 `json:"totalBytes"`
	ProcessedBytes         int64 `json:"processedBytes"`
	BytesPerSecond         int64 `json:"bytesPerSecond"`
	EstimatedTimeRemaining int64 `json:"estimatedTimeRemaining"` // seconds
}

// BatchManager manages batch replication jobs.
type BatchManager struct {
	lister            ObjectLister
	clientFactory     RemoteClientFactory
	jobs              map[string]*BatchJob
	service           *Service
	maxConcurrentJobs int
	historyLimit      int
	mu                sync.RWMutex
	runningJobs       int32
}

// ObjectLister lists objects in a bucket.
type ObjectLister interface {
	ListObjects(ctx context.Context, bucket, prefix string, recursive bool) (<-chan ObjectListEntry, <-chan error)
}

// ObjectListEntry represents an object in the list.
type ObjectListEntry struct {
	LastModified   time.Time
	Tags           map[string]string
	Key            string
	VersionID      string
	Size           int64
	IsDeleteMarker bool
}

// RemoteClientFactory creates clients for remote endpoints.
type RemoteClientFactory interface {
	GetClient(endpoint, accessKey, secretKey string) (RemoteClient, error)
}

// RemoteClient is a client for a remote endpoint.
type RemoteClient interface {
	PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, contentType string, metadata map[string]string) error
	DeleteObject(ctx context.Context, bucket, key string) error
	Close() error
}

// BatchManagerConfig configures the batch manager.
type BatchManagerConfig struct {
	MaxConcurrentJobs int
	HistoryLimit      int
}

// DefaultBatchManagerConfig returns sensible defaults.
func DefaultBatchManagerConfig() BatchManagerConfig {
	return BatchManagerConfig{
		MaxConcurrentJobs: defaultMaxConcurrentJobs,
		HistoryLimit:      defaultHistoryLimit,
	}
}

// NewBatchManager creates a new batch manager.
func NewBatchManager(service *Service, lister ObjectLister, clientFactory RemoteClientFactory, cfg BatchManagerConfig) *BatchManager {
	return &BatchManager{
		jobs:              make(map[string]*BatchJob),
		service:           service,
		lister:            lister,
		clientFactory:     clientFactory,
		maxConcurrentJobs: cfg.MaxConcurrentJobs,
		historyLimit:      cfg.HistoryLimit,
	}
}

// CreateJob creates a new batch replication job.
func (bm *BatchManager) CreateJob(job *BatchJob) error {
	if job.JobID == "" {
		return errors.New("job ID is required")
	}

	if job.SourceBucket == "" {
		return errors.New("source bucket is required")
	}

	if job.DestinationBucket == "" {
		return errors.New("destination bucket is required")
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	if _, exists := bm.jobs[job.JobID]; exists {
		return fmt.Errorf("job %s already exists", job.JobID)
	}

	// Set defaults
	if job.Concurrency <= 0 {
		job.Concurrency = 10
	}

	if job.MaxRetries <= 0 {
		job.MaxRetries = 3
	}

	if job.Priority <= 0 {
		job.Priority = 100
	}

	job.SetStatus(BatchJobStatusPending)
	job.CreatedAt = time.Now()
	job.pauseCh = make(chan struct{})
	job.resumeCh = make(chan struct{})

	bm.jobs[job.JobID] = job

	// Clean up old completed jobs if over limit
	bm.cleanupOldJobs()

	return nil
}

// StartJob starts a batch job.
func (bm *BatchManager) StartJob(ctx context.Context, jobID string) error {
	bm.mu.Lock()

	job, exists := bm.jobs[jobID]
	if !exists {
		bm.mu.Unlock()
		return fmt.Errorf("job %s not found", jobID)
	}

	status := job.GetStatus()
	if status != BatchJobStatusPending && status != BatchJobStatusPaused {
		bm.mu.Unlock()
		//nolint:err113 // Dynamic status information is necessary for error context
		return fmt.Errorf("job %s cannot be started (status: %s)", jobID, status)
	}

	if int(atomic.LoadInt32(&bm.runningJobs)) >= bm.maxConcurrentJobs {
		bm.mu.Unlock()
		return fmt.Errorf("maximum concurrent jobs (%d) reached", bm.maxConcurrentJobs)
	}

	atomic.AddInt32(&bm.runningJobs, 1)

	jobCtx, cancelFunc := context.WithCancel(ctx)
	job.cancelFunc = cancelFunc
	job.SetStatus(BatchJobStatusRunning)
	now := time.Now()
	job.StartedAt = &now

	bm.mu.Unlock()

	// Run job in background
	go bm.runJob(jobCtx, job)

	return nil
}

// PauseJob pauses a running job.
func (bm *BatchManager) PauseJob(jobID string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	job, exists := bm.jobs[jobID]
	if !exists {
		return fmt.Errorf("job %s not found", jobID)
	}

	if job.GetStatus() != BatchJobStatusRunning {
		return fmt.Errorf("job %s is not running", jobID)
	}

	job.SetStatus(BatchJobStatusPaused)
	close(job.pauseCh)

	return nil
}

// ResumeJob resumes a paused job.
func (bm *BatchManager) ResumeJob(ctx context.Context, jobID string) error {
	bm.mu.Lock()

	job, exists := bm.jobs[jobID]
	if !exists {
		bm.mu.Unlock()
		return fmt.Errorf("job %s not found", jobID)
	}

	if job.GetStatus() != BatchJobStatusPaused {
		bm.mu.Unlock()
		return fmt.Errorf("job %s is not paused", jobID)
	}

	job.SetStatus(BatchJobStatusRunning)
	job.pauseCh = make(chan struct{})
	close(job.resumeCh)
	job.resumeCh = make(chan struct{})

	bm.mu.Unlock()

	return nil
}

// CancelJob cancels a job.
func (bm *BatchManager) CancelJob(jobID string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	job, exists := bm.jobs[jobID]
	if !exists {
		return fmt.Errorf("job %s not found", jobID)
	}

	status := job.GetStatus()
	if status == BatchJobStatusCompleted || status == BatchJobStatusFailed || status == BatchJobStatusCancelled {
		return fmt.Errorf("job %s already finished", jobID)
	}

	if job.cancelFunc != nil {
		job.cancelFunc()
	}

	job.SetStatus(BatchJobStatusCancelled)
	now := time.Now()
	job.CompletedAt = &now

	return nil
}

// GetJob returns a job by ID.
func (bm *BatchManager) GetJob(jobID string) (*BatchJob, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	job, exists := bm.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job %s not found", jobID)
	}

	return job, nil
}

// ListJobs returns all jobs.
func (bm *BatchManager) ListJobs() []*BatchJob {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	jobs := make([]*BatchJob, 0, len(bm.jobs))
	for _, job := range bm.jobs {
		jobs = append(jobs, job)
	}

	return jobs
}

// DeleteJob deletes a job.
func (bm *BatchManager) DeleteJob(jobID string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	job, exists := bm.jobs[jobID]
	if !exists {
		return fmt.Errorf("job %s not found", jobID)
	}

	if job.GetStatus() == BatchJobStatusRunning {
		return fmt.Errorf("cannot delete running job %s", jobID)
	}

	delete(bm.jobs, jobID)

	return nil
}

// runJob executes the batch job.
func (bm *BatchManager) runJob(ctx context.Context, job *BatchJob) {
	defer atomic.AddInt32(&bm.runningJobs, -1)

	objectsCh, errCh := bm.lister.ListObjects(ctx, job.SourceBucket, job.Prefix, true)
	workCh := make(chan ObjectListEntry, job.Concurrency*2)

	var (
		wg             sync.WaitGroup
		bytesProcessed int64
	)

	startTime := time.Now()

	bm.startReplicationWorkers(ctx, job, workCh, &wg, &bytesProcessed)
	bm.feedObjectsToWorkers(ctx, job, objectsCh, workCh)

	wg.Wait()

	if bm.checkForListingErrors(job, errCh) {
		return
	}

	bm.finalizeJob(ctx, job, startTime, bytesProcessed)
}

func (bm *BatchManager) startReplicationWorkers(ctx context.Context, job *BatchJob, workCh chan ObjectListEntry, wg *sync.WaitGroup, bytesProcessed *int64) {
	for range job.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bm.replicationWorker(ctx, job, workCh, bytesProcessed)
		}()
	}
}

func (bm *BatchManager) feedObjectsToWorkers(ctx context.Context, job *BatchJob, objectsCh <-chan ObjectListEntry, workCh chan ObjectListEntry) {
	go func() {
		defer close(workCh)

		for {
			select {
			case <-ctx.Done():
				return
			case obj, ok := <-objectsCh:
				if !ok {
					return
				}

				if bm.processObjectForBatch(ctx, job, obj, workCh) {
					return
				}
			}
		}
	}()
}

func (bm *BatchManager) processObjectForBatch(ctx context.Context, job *BatchJob, obj ObjectListEntry, workCh chan ObjectListEntry) bool {
	// Apply filters
	if !bm.matchesFilters(job, obj) {
		atomic.AddInt64(&job.Progress.SkippedObjects, 1)
		return false
	}

	atomic.AddInt64(&job.Progress.TotalObjects, 1)
	atomic.AddInt64(&job.Progress.TotalBytes, obj.Size)

	// Check for pause
	if bm.handlePauseResume(ctx, job) {
		return true
	}

	// Send to work channel
	select {
	case workCh <- obj:
		return false
	case <-ctx.Done():
		return true
	}
}

func (bm *BatchManager) handlePauseResume(ctx context.Context, job *BatchJob) bool {
	select {
	case <-job.pauseCh:
		// Wait for resume
		select {
		case <-job.resumeCh:
			return false
		case <-ctx.Done():
			return true
		}
	default:
		return false
	}
}

func (bm *BatchManager) checkForListingErrors(job *BatchJob, errCh <-chan error) bool {
	select {
	case err := <-errCh:
		if err != nil {
			bm.mu.Lock()
			job.SetStatus(BatchJobStatusFailed)
			job.Error = err.Error()
			bm.mu.Unlock()
			return true
		}
	default:
	}
	return false
}

func (bm *BatchManager) finalizeJob(ctx context.Context, job *BatchJob, startTime time.Time, bytesProcessed int64) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	now := time.Now()
	job.CompletedAt = &now

	bm.setFinalJobStatus(ctx, job)
	bm.calculateFinalStats(job, startTime, bytesProcessed)
}

func (bm *BatchManager) setFinalJobStatus(ctx context.Context, job *BatchJob) {
	switch {
	case ctx.Err() != nil:
		if job.GetStatus() != BatchJobStatusCancelled {
			job.SetStatus(BatchJobStatusCancelled)
		}
	case job.Progress.FailedObjects > 0 && job.Progress.SuccessObjects == 0:
		job.SetStatus(BatchJobStatusFailed)
		job.Error = fmt.Sprintf("all %d objects failed", job.Progress.FailedObjects)
	default:
		job.SetStatus(BatchJobStatusCompleted)
	}
}

func (bm *BatchManager) calculateFinalStats(job *BatchJob, startTime time.Time, bytesProcessed int64) {
	elapsed := time.Since(startTime).Seconds()
	if elapsed > 0 {
		job.Progress.BytesPerSecond = int64(float64(atomic.LoadInt64(&bytesProcessed)) / elapsed)
	}
}

// replicationWorker processes objects for replication.
func (bm *BatchManager) replicationWorker(ctx context.Context, job *BatchJob, workCh <-chan ObjectListEntry, bytesProcessed *int64) {
	for {
		select {
		case <-ctx.Done():
			return
		case obj, ok := <-workCh:
			if !ok {
				return
			}

			// Replicate with retries
			var err error
			for attempt := 0; attempt <= job.MaxRetries; attempt++ {
				err = bm.replicateObject(ctx, job, obj)
				if err == nil {
					break
				}

				// Wait before retry
				if attempt < job.MaxRetries {
					select {
					case <-time.After(time.Duration(attempt+1) * time.Second):
					case <-ctx.Done():
						return
					}
				}
			}

			if err != nil {
				atomic.AddInt64(&job.Progress.FailedObjects, 1)
			} else {
				atomic.AddInt64(&job.Progress.SuccessObjects, 1)
				atomic.AddInt64(bytesProcessed, obj.Size)
			}

			atomic.AddInt64(&job.Progress.ProcessedObjects, 1)
			atomic.AddInt64(&job.Progress.ProcessedBytes, obj.Size)

			// Update ETA
			bm.updateETA(job)
		}
	}
}

// replicateObject replicates a single object.
func (bm *BatchManager) replicateObject(ctx context.Context, job *BatchJob, obj ObjectListEntry) error {
	if obj.IsDeleteMarker {
		// Handle delete marker replication
		return bm.replicateDeleteMarker(ctx, job, obj)
	}

	// Get source object
	reader, err := bm.service.getSourceObject(ctx, job.SourceBucket, obj.Key, obj.VersionID)
	if err != nil {
		return fmt.Errorf("failed to get source object: %w", err)
	}

	defer func() { _ = reader.Close() }()

	// Get object info for metadata
	info, err := bm.service.getSourceObjectInfo(ctx, job.SourceBucket, obj.Key, obj.VersionID)
	if err != nil {
		return fmt.Errorf("failed to get object info: %w", err)
	}

	// Apply rate limiting if configured
	var wrappedReader io.Reader = reader
	if job.RateLimitBytesPerSec > 0 {
		wrappedReader = newRateLimitedReader(reader, job.RateLimitBytesPerSec)
	}

	// Replicate to destination
	if job.DestinationEndpoint != "" && bm.clientFactory != nil {
		// Cross-cluster replication
		client, err := bm.clientFactory.GetClient(job.DestinationEndpoint, "", "")
		if err != nil {
			return fmt.Errorf("failed to get remote client: %w", err)
		}

		defer func() { _ = client.Close() }()

		return client.PutObject(ctx, job.DestinationBucket, obj.Key, wrappedReader, obj.Size, info.ContentType, info.UserMetadata)
	}

	// Same-cluster replication - use the replication service queue
	_, err = bm.service.queue.Enqueue(ctx, job.SourceBucket, obj.Key, obj.VersionID, "PUT", "batch-"+job.JobID)

	return err
}

// replicateDeleteMarker replicates a delete marker.
func (bm *BatchManager) replicateDeleteMarker(ctx context.Context, job *BatchJob, obj ObjectListEntry) error {
	if job.DestinationEndpoint != "" && bm.clientFactory != nil {
		client, err := bm.clientFactory.GetClient(job.DestinationEndpoint, "", "")
		if err != nil {
			return fmt.Errorf("failed to get remote client: %w", err)
		}

		defer func() { _ = client.Close() }()

		return client.DeleteObject(ctx, job.DestinationBucket, obj.Key)
	}

	// Same-cluster - queue the delete
	_, err := bm.service.queue.Enqueue(ctx, job.SourceBucket, obj.Key, obj.VersionID, "DELETE", "batch-"+job.JobID)

	return err
}

// matchesFilters checks if an object matches job filters.
func (bm *BatchManager) matchesFilters(job *BatchJob, obj ObjectListEntry) bool {
	// Size filters
	if job.MinSize > 0 && obj.Size < job.MinSize {
		return false
	}

	if job.MaxSize > 0 && obj.Size > job.MaxSize {
		return false
	}

	// Time filters
	if job.CreatedAfter != nil && obj.LastModified.Before(*job.CreatedAfter) {
		return false
	}

	if job.CreatedBefore != nil && obj.LastModified.After(*job.CreatedBefore) {
		return false
	}

	// Tag filters
	if len(job.Tags) > 0 {
		for k, v := range job.Tags {
			if obj.Tags[k] != v {
				return false
			}
		}
	}

	return true
}

// updateETA updates the estimated time remaining.
func (bm *BatchManager) updateETA(job *BatchJob) {
	if job.StartedAt == nil {
		return
	}

	elapsed := time.Since(*job.StartedAt).Seconds()
	processed := atomic.LoadInt64(&job.Progress.ProcessedObjects)
	total := atomic.LoadInt64(&job.Progress.TotalObjects)

	if processed > 0 && elapsed > 0 {
		remaining := total - processed

		rate := float64(processed) / elapsed
		if rate > 0 {
			atomic.StoreInt64(&job.Progress.EstimatedTimeRemaining, int64(float64(remaining)/rate))
		}
	}
}

// cleanupOldJobs removes old completed jobs if over the limit.
func (bm *BatchManager) cleanupOldJobs() {
	// Count completed jobs
	var completedJobs []*BatchJob

	for _, job := range bm.jobs {
		status := job.GetStatus()
		if status == BatchJobStatusCompleted || status == BatchJobStatusFailed || status == BatchJobStatusCancelled {
			completedJobs = append(completedJobs, job)
		}
	}

	// Remove oldest if over limit
	if len(completedJobs) > bm.historyLimit {
		// Sort by completion time
		for i := range len(completedJobs) - 1 {
			for j := i + 1; j < len(completedJobs); j++ {
				if completedJobs[i].CompletedAt != nil && completedJobs[j].CompletedAt != nil {
					if completedJobs[i].CompletedAt.After(*completedJobs[j].CompletedAt) {
						completedJobs[i], completedJobs[j] = completedJobs[j], completedJobs[i]
					}
				}
			}
		}

		// Remove oldest
		toRemove := len(completedJobs) - bm.historyLimit
		for i := range toRemove {
			delete(bm.jobs, completedJobs[i].JobID)
		}
	}
}

// MarshalJSON implements json.Marshaler.
func (job *BatchJob) MarshalJSON() ([]byte, error) {
	type Alias BatchJob

	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(job),
	})
}

// rateLimitedReader wraps a reader with rate limiting.
type rateLimitedReader struct {
	lastRead            time.Time
	reader              io.Reader
	bytesPerSec         int64
	bytesSinceLastCheck int64
}

func newRateLimitedReader(reader io.Reader, bytesPerSec int64) *rateLimitedReader {
	return &rateLimitedReader{
		reader:      reader,
		bytesPerSec: bytesPerSec,
		lastRead:    time.Now(),
	}
}

func (r *rateLimitedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.bytesSinceLastCheck += int64(n)

		// Check every 64KB
		if r.bytesSinceLastCheck >= streamingBufferThreshold {
			elapsed := time.Since(r.lastRead).Seconds()
			if elapsed > 0 {
				currentRate := float64(r.bytesSinceLastCheck) / elapsed
				if currentRate > float64(r.bytesPerSec) {
					// Sleep to slow down
					sleepTime := time.Duration(float64(r.bytesSinceLastCheck)/float64(r.bytesPerSec)*nanosecondsPerSecond) - time.Since(r.lastRead)
					if sleepTime > 0 {
						time.Sleep(sleepTime)
					}
				}
			}

			r.lastRead = time.Now()
			r.bytesSinceLastCheck = 0
		}
	}

	return n, err
}

// BatchJobSummary returns a summary suitable for listing.
type BatchJobSummary struct {
	CreatedAt         time.Time        `json:"createdAt"`
	CompletedAt       *time.Time       `json:"completedAt,omitempty"`
	JobID             string           `json:"jobId"`
	Description       string           `json:"description,omitempty"`
	SourceBucket      string           `json:"sourceBucket"`
	DestinationBucket string           `json:"destinationBucket"`
	Status            BatchJobStatus   `json:"status"`
	Progress          BatchJobProgress `json:"progress"`
}

// Summary returns a summary of the job.
func (job *BatchJob) Summary() BatchJobSummary {
	return BatchJobSummary{
		JobID:             job.JobID,
		Description:       job.Description,
		SourceBucket:      job.SourceBucket,
		DestinationBucket: job.DestinationBucket,
		Status:            job.GetStatus(),
		Progress:          job.Progress,
		CreatedAt:         job.CreatedAt,
		CompletedAt:       job.CompletedAt,
	}
}
