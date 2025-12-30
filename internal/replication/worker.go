package replication

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Worker configuration defaults.
const (
	defaultNumWorkers      = 4
	defaultProcessInterval = 100 * time.Millisecond
)

// Worker processes replication queue items.
type Worker struct {
	queue     *Queue
	service   *Service
	clients   map[string]*minio.Client
	stopCh    chan struct{}
	wg        sync.WaitGroup
	id        int
	clientsMu sync.RWMutex
}

// WorkerConfig holds worker configuration.
type WorkerConfig struct {
	// NumWorkers is the number of concurrent workers
	NumWorkers int
	// ProcessInterval is the interval between processing attempts
	ProcessInterval time.Duration
}

// DefaultWorkerConfig returns sensible defaults.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		NumWorkers:      defaultNumWorkers,
		ProcessInterval: defaultProcessInterval,
	}
}

// newWorker creates a new replication worker.
func newWorker(id int, queue *Queue, service *Service) *Worker {
	return &Worker{
		id:      id,
		queue:   queue,
		service: service,
		clients: make(map[string]*minio.Client),
		stopCh:  make(chan struct{}),
	}
}

// Start starts the worker.
func (w *Worker) Start(ctx context.Context) {
	w.wg.Add(1)

	go w.run(ctx)
}

// Stop stops the worker.
func (w *Worker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
}

// run is the main worker loop.
func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
			item, err := w.queue.Dequeue(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				continue
			}

			if err := w.processItem(ctx, item); err != nil {
				_ = w.queue.Fail(item.ID, err)
			} else {
				_ = w.queue.Complete(item.ID)
			}
		}
	}
}

// processItem processes a single queue item.
func (w *Worker) processItem(ctx context.Context, item *QueueItem) error {
	// Get the replication config for this bucket
	config, err := w.service.GetConfig(ctx, item.Bucket)
	if err != nil {
		return fmt.Errorf("failed to get replication config: %w", err)
	}

	// Find the rule that applies to this item
	var rule *Rule

	for i := range config.Rules {
		if config.Rules[i].ID == item.RuleID {
			rule = &config.Rules[i]
			break
		}
	}

	if rule == nil {
		return fmt.Errorf("rule not found: %s", item.RuleID)
	}

	if !rule.IsEnabled() {
		return nil // Rule is disabled, skip
	}

	// Get or create client for destination
	client, err := w.getClient(rule.Destination)
	if err != nil {
		return fmt.Errorf("failed to create destination client: %w", err)
	}

	switch item.Operation {
	case "PUT":
		return w.replicateObject(ctx, client, item, rule)
	case "DELETE":
		return w.replicateDelete(ctx, client, item, rule)
	default:
		return fmt.Errorf("unknown operation: %s", item.Operation)
	}
}

// replicateObject replicates an object to the destination.
func (w *Worker) replicateObject(ctx context.Context, client *minio.Client, item *QueueItem, rule *Rule) error {
	// Get the source object from our backend
	reader, err := w.service.getSourceObject(ctx, item.Bucket, item.Key, item.VersionID)
	if err != nil {
		return fmt.Errorf("failed to get source object: %w", err)
	}

	defer func() { _ = reader.Close() }()

	// Get source object info
	info, err := w.service.getSourceObjectInfo(ctx, item.Bucket, item.Key, item.VersionID)
	if err != nil {
		return fmt.Errorf("failed to get source object info: %w", err)
	}

	// Prepare put options
	opts := minio.PutObjectOptions{
		ContentType: info.ContentType,
		UserMetadata: map[string]string{
			"x-amz-replication-status": ReplicationStatusReplica,
		},
	}

	// Copy user metadata
	for k, v := range info.UserMetadata {
		opts.UserMetadata[k] = v
	}

	// Set storage class if specified
	if rule.Destination.StorageClass != "" {
		opts.StorageClass = rule.Destination.StorageClass
	}

	// Upload to destination
	_, err = client.PutObject(ctx, rule.Destination.Bucket, item.Key, reader, info.Size, opts)
	if err != nil {
		return fmt.Errorf("failed to replicate object: %w", err)
	}

	// Update replication status on source
	if err := w.service.updateReplicationStatus(ctx, item.Bucket, item.Key, item.VersionID, rule.Destination.Bucket, ReplicationStatusComplete); err != nil {
		// Log but don't fail - the replication itself succeeded
		fmt.Printf("warning: failed to update replication status: %v\n", err)
	}

	return nil
}

// replicateDelete replicates a delete operation to the destination.
func (w *Worker) replicateDelete(ctx context.Context, client *minio.Client, item *QueueItem, rule *Rule) error {
	// Check if we should replicate delete markers
	if !rule.ShouldReplicateDeleteMarkers() {
		return nil
	}

	// Delete from destination
	opts := minio.RemoveObjectOptions{}
	if item.VersionID != "" {
		opts.VersionID = item.VersionID
	}

	err := client.RemoveObject(ctx, rule.Destination.Bucket, item.Key, opts)
	if err != nil {
		return fmt.Errorf("failed to replicate delete: %w", err)
	}

	return nil
}

// getClient gets or creates a minio client for the destination.
func (w *Worker) getClient(dest Destination) (*minio.Client, error) {
	key := fmt.Sprintf("%s:%s", dest.Endpoint, dest.Bucket)

	w.clientsMu.RLock()
	client, ok := w.clients[key]
	w.clientsMu.RUnlock()

	if ok {
		return client, nil
	}

	w.clientsMu.Lock()
	defer w.clientsMu.Unlock()

	// Double-check after acquiring write lock
	if client, ok := w.clients[key]; ok {
		return client, nil
	}

	// Create new client
	//nolint:gosec // G402: InsecureSkipVerify only true for non-SSL connections (no TLS used)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !dest.UseSSL, // Only skip for non-SSL
		},
	}

	opts := &minio.Options{
		Creds:     credentials.NewStaticV4(dest.AccessKey, dest.SecretKey, ""),
		Secure:    dest.UseSSL,
		Transport: transport,
	}

	if dest.Region != "" {
		opts.Region = dest.Region
	}

	client, err := minio.New(dest.Endpoint, opts)
	if err != nil {
		return nil, err
	}

	w.clients[key] = client

	return client, nil
}

// ObjectInfo holds object information for replication.
type ObjectInfo struct {
	UserMetadata map[string]string
	Key          string
	ContentType  string
	ETag         string
	VersionID    string
	Size         int64
}

// WorkerPool manages a pool of replication workers.
type WorkerPool struct {
	queue   *Queue
	service *Service
	workers []*Worker
	config  WorkerConfig
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(queue *Queue, service *Service, cfg WorkerConfig) *WorkerPool {
	if cfg.NumWorkers <= 0 {
		cfg.NumWorkers = 4
	}

	pool := &WorkerPool{
		workers: make([]*Worker, cfg.NumWorkers),
		queue:   queue,
		service: service,
		config:  cfg,
	}

	for i := range cfg.NumWorkers {
		pool.workers[i] = newWorker(i, queue, service)
	}

	return pool
}

// Start starts all workers in the pool.
func (p *WorkerPool) Start(ctx context.Context) {
	for _, w := range p.workers {
		w.Start(ctx)
	}
}

// Stop stops all workers in the pool.
func (p *WorkerPool) Stop() {
	for _, w := range p.workers {
		w.Stop()
	}
}

// ReplicationMetrics holds replication metrics.
type ReplicationMetrics struct {
	LastReplicationTime time.Time
	TotalReplicated     int64
	TotalFailed         int64
	BytesReplicated     int64
	AverageLatency      time.Duration
}
