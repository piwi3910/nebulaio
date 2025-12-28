package replication

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Queue manages replication tasks
type Queue struct {
	mu       sync.RWMutex
	items    map[string]*QueueItem
	pending  chan *QueueItem
	maxSize  int
	maxRetry int
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// QueueConfig holds queue configuration
type QueueConfig struct {
	// MaxSize is the maximum number of items in the queue
	MaxSize int
	// MaxRetry is the maximum number of retries per item
	MaxRetry int
}

// DefaultQueueConfig returns sensible defaults
func DefaultQueueConfig() QueueConfig {
	return QueueConfig{
		MaxSize:  10000,
		MaxRetry: 3,
	}
}

// NewQueue creates a new replication queue
func NewQueue(cfg QueueConfig) *Queue {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 10000
	}
	if cfg.MaxRetry <= 0 {
		cfg.MaxRetry = 3
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Queue{
		items:    make(map[string]*QueueItem),
		pending:  make(chan *QueueItem, cfg.MaxSize),
		maxSize:  cfg.MaxSize,
		maxRetry: cfg.MaxRetry,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Enqueue adds a new item to the queue
func (q *Queue) Enqueue(ctx context.Context, bucket, key, versionID, operation, ruleID string) (*QueueItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) >= q.maxSize {
		return nil, errors.New("replication queue is full")
	}

	item := &QueueItem{
		ID:         uuid.New().String(),
		Bucket:     bucket,
		Key:        key,
		VersionID:  versionID,
		Operation:  operation,
		RuleID:     ruleID,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		Status:     QueueStatusPending,
	}

	q.items[item.ID] = item

	select {
	case q.pending <- item:
		return item, nil
	case <-ctx.Done():
		delete(q.items, item.ID)
		return nil, ctx.Err()
	default:
		delete(q.items, item.ID)
		return nil, errors.New("replication queue channel is full")
	}
}

// Dequeue retrieves the next pending item
func (q *Queue) Dequeue(ctx context.Context) (*QueueItem, error) {
	select {
	case item := <-q.pending:
		q.mu.Lock()
		if existingItem, ok := q.items[item.ID]; ok {
			existingItem.Status = QueueStatusProgress
		}
		q.mu.Unlock()
		return item, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Complete marks an item as completed and removes it from the queue
func (q *Queue) Complete(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	item, ok := q.items[id]
	if !ok {
		return fmt.Errorf("replication queue item not found: id=%s", id)
	}

	item.Status = QueueStatusCompleted
	delete(q.items, id)
	return nil
}

// Fail marks an item as failed and optionally requeues it
func (q *Queue) Fail(id string, err error) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	item, ok := q.items[id]
	if !ok {
		return fmt.Errorf("replication queue item not found: id=%s", id)
	}

	item.RetryCount++
	item.LastError = err.Error()

	if item.RetryCount >= q.maxRetry {
		item.Status = QueueStatusFailed
		// Keep failed items for inspection
		return nil
	}

	// Requeue with exponential backoff
	item.Status = QueueStatusPending
	retryCount := item.RetryCount // Capture before goroutine to avoid race

	q.wg.Add(1)
	go func() {
		defer q.wg.Done()

		// Exponential backoff: 1s, 2s, 4s, 8s, etc.
		backoff := time.Duration(1<<uint(retryCount-1)) * time.Second

		// Wait for backoff or queue cancellation
		select {
		case <-q.ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Try to requeue the item
		select {
		case q.pending <- item:
		case <-q.ctx.Done():
			// Queue is closing, don't requeue
		default:
			// Queue is full, item will stay in items map as pending
		}
	}()

	return nil
}

// Get retrieves an item by ID
func (q *Queue) Get(id string) (*QueueItem, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	item, ok := q.items[id]
	if !ok {
		return nil, errors.New("item not found")
	}

	return item, nil
}

// List returns all items in the queue
func (q *Queue) List() []*QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	items := make([]*QueueItem, 0, len(q.items))
	for _, item := range q.items {
		items = append(items, item)
	}

	return items
}

// ListByStatus returns items with a specific status
func (q *Queue) ListByStatus(status QueueItemStatus) []*QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var items []*QueueItem
	for _, item := range q.items {
		if item.Status == status {
			items = append(items, item)
		}
	}

	return items
}

// ListByBucket returns items for a specific bucket
func (q *Queue) ListByBucket(bucket string) []*QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var items []*QueueItem
	for _, item := range q.items {
		if item.Bucket == bucket {
			items = append(items, item)
		}
	}

	return items
}

// Size returns the number of items in the queue
func (q *Queue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.items)
}

// PendingCount returns the number of pending items
func (q *Queue) PendingCount() int {
	return len(q.pending)
}

// Clear removes all completed and failed items
func (q *Queue) Clear() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	count := 0
	for id, item := range q.items {
		if item.Status == QueueStatusCompleted || item.Status == QueueStatusFailed {
			delete(q.items, id)
			count++
		}
	}

	return count
}

// ClearFailed removes all failed items
func (q *Queue) ClearFailed() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	count := 0
	for id, item := range q.items {
		if item.Status == QueueStatusFailed {
			delete(q.items, id)
			count++
		}
	}

	return count
}

// Stats returns queue statistics
func (q *Queue) Stats() QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	stats := QueueStats{
		Total: len(q.items),
	}

	for _, item := range q.items {
		switch item.Status {
		case QueueStatusPending:
			stats.Pending++
		case QueueStatusProgress:
			stats.InProgress++
		case QueueStatusCompleted:
			stats.Completed++
		case QueueStatusFailed:
			stats.Failed++
		}
	}

	return stats
}

// QueueStats holds queue statistics
type QueueStats struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	InProgress int `json:"inProgress"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
}

// MarshalJSON implements json.Marshaler
func (s QueueStats) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Total      int `json:"total"`
		Pending    int `json:"pending"`
		InProgress int `json:"inProgress"`
		Completed  int `json:"completed"`
		Failed     int `json:"failed"`
	}{
		Total:      s.Total,
		Pending:    s.Pending,
		InProgress: s.InProgress,
		Completed:  s.Completed,
		Failed:     s.Failed,
	})
}

// Close closes the queue and waits for all pending goroutines to complete
func (q *Queue) Close() error {
	// Signal all goroutines to stop
	q.cancel()
	// Wait for all retry goroutines to complete
	q.wg.Wait()
	// Now safe to close the channel
	close(q.pending)
	return nil
}
