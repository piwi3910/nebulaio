package events

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Queue configuration constants.
const (
	defaultQueueSize          = 10000                   // Default event queue size
	defaultMaxRetries         = 3                       // Default maximum retries for failed events
	defaultWorkers            = 4                       // Default number of worker goroutines
	eventPublishTimeout       = 30 * time.Second        // Timeout for publishing events
	retryProcessorInterval    = 500 * time.Millisecond  // Interval for retry processor
)

// QueuedEvent represents an event in the queue.
type QueuedEvent struct {
	NextRetry  time.Time
	CreatedAt  time.Time
	Event      *S3Event
	TargetName string
	Attempts   int
}

// EventQueue manages queued events for async delivery.
type EventQueue struct {
	ctx        context.Context
	events     chan *QueuedEvent
	targets    map[string]Target
	cancel     context.CancelFunc
	retryQueue []*QueuedEvent
	wg         sync.WaitGroup
	maxRetries int
	retryDelay time.Duration
	workers    int
	mu         sync.RWMutex
	closed     bool
}

// EventQueueConfig configures the event queue.
type EventQueueConfig struct {
	// QueueSize is the size of the event queue
	QueueSize int `json:"queueSize" yaml:"queueSize"`

	// MaxRetries is the maximum number of retries
	MaxRetries int `json:"maxRetries" yaml:"maxRetries"`

	// RetryDelay is the initial delay between retries
	RetryDelay time.Duration `json:"retryDelay" yaml:"retryDelay"`

	// Workers is the number of worker goroutines
	Workers int `json:"workers" yaml:"workers"`
}

// DefaultQueueConfig returns a default queue configuration.
func DefaultQueueConfig() EventQueueConfig {
	return EventQueueConfig{
		QueueSize:  defaultQueueSize,
		MaxRetries: defaultMaxRetries,
		RetryDelay: 1 * time.Second,
		Workers:    defaultWorkers,
	}
}

// NewEventQueue creates a new event queue.
func NewEventQueue(config EventQueueConfig) *EventQueue {
	if config.QueueSize == 0 {
		config.QueueSize = defaultQueueSize
	}

	if config.MaxRetries == 0 {
		config.MaxRetries = defaultMaxRetries
	}

	if config.RetryDelay == 0 {
		config.RetryDelay = 1 * time.Second
	}

	if config.Workers == 0 {
		config.Workers = defaultWorkers
	}

	ctx, cancel := context.WithCancel(context.Background())

	q := &EventQueue{
		events:     make(chan *QueuedEvent, config.QueueSize),
		retryQueue: make([]*QueuedEvent, 0),
		targets:    make(map[string]Target),
		maxRetries: config.MaxRetries,
		retryDelay: config.RetryDelay,
		workers:    config.Workers,
		ctx:        ctx,
		cancel:     cancel,
	}

	return q
}

// Start starts the queue workers.
func (q *EventQueue) Start() {
	// Start worker goroutines
	for i := range q.workers {
		q.wg.Add(1)

		go q.worker(i)
	}

	// Start retry processor
	q.wg.Add(1)

	go q.retryProcessor()

	log.Info().Int("workers", q.workers).Msg("Event queue started")
}

// Stop stops the queue workers.
func (q *EventQueue) Stop() {
	q.mu.Lock()

	if q.closed {
		q.mu.Unlock()
		return
	}

	q.closed = true
	q.mu.Unlock()

	// Cancel context first to signal workers to stop
	q.cancel()
	// Wait for workers to finish before closing the channel
	q.wg.Wait()

	log.Info().Msg("Event queue stopped")
}

// AddTarget adds a target to the queue.
func (q *EventQueue) AddTarget(target Target) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.targets[target.Name()] = target
}

// RemoveTarget removes a target from the queue.
func (q *EventQueue) RemoveTarget(name string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.targets, name)
}

// GetTarget returns a target by name.
func (q *EventQueue) GetTarget(name string) (Target, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	target, ok := q.targets[name]

	return target, ok
}

// Enqueue adds an event to the queue.
func (q *EventQueue) Enqueue(event *S3Event, targetName string) error {
	q.mu.RLock()

	if q.closed {
		q.mu.RUnlock()
		return ErrTargetClosed
	}

	q.mu.RUnlock()

	qe := &QueuedEvent{
		Event:      event,
		TargetName: targetName,
		Attempts:   0,
		CreatedAt:  time.Now(),
	}

	select {
	case q.events <- qe:
		return nil
	default:
		return ErrPublishFailed
	}
}

// EnqueueAll enqueues an event to all targets.
func (q *EventQueue) EnqueueAll(event *S3Event) error {
	q.mu.RLock()

	targets := make([]string, 0, len(q.targets))
	for name := range q.targets {
		targets = append(targets, name)
	}

	q.mu.RUnlock()

	for _, name := range targets {
		err := q.Enqueue(event, name)
		if err != nil {
			log.Warn().Str("target", name).Err(err).Msg("Failed to enqueue event")
		}
	}

	return nil
}

// worker processes events from the queue.
func (q *EventQueue) worker(id int) {
	defer q.wg.Done()

	log.Debug().Int("worker", id).Msg("Event queue worker started")

	for {
		select {
		case <-q.ctx.Done():
			return
		case qe, ok := <-q.events:
			if !ok {
				return
			}

			q.processEvent(qe)
		}
	}
}

// processEvent processes a single queued event.
func (q *EventQueue) processEvent(qe *QueuedEvent) {
	q.mu.RLock()
	target, ok := q.targets[qe.TargetName]
	q.mu.RUnlock()

	if !ok {
		log.Warn().Str("target", qe.TargetName).Msg("Target not found, dropping event")
		return
	}

	qe.Attempts++

	ctx, cancel := context.WithTimeout(q.ctx, eventPublishTimeout)
	defer cancel()

	err := target.Publish(ctx, qe.Event)
	if err != nil {
		log.Warn().
			Str("target", qe.TargetName).
			Int("attempt", qe.Attempts).
			Err(err).
			Msg("Failed to publish event")

		// Schedule retry if within limits
		if qe.Attempts < q.maxRetries {
			//nolint:gosec // G115: Attempts is bounded by maxRetries, safe for shift
			qe.NextRetry = time.Now().Add(q.retryDelay * time.Duration(1<<uint(qe.Attempts-1)))
			q.scheduleRetry(qe)
		} else {
			log.Error().
				Str("target", qe.TargetName).
				Int("attempts", qe.Attempts).
				Msg("Max retries exceeded, dropping event")
		}

		return
	}

	log.Debug().
		Str("target", qe.TargetName).
		Str("event", qe.Event.Records[0].EventName).
		Msg("Event published successfully")
}

// scheduleRetry schedules an event for retry.
func (q *EventQueue) scheduleRetry(qe *QueuedEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.retryQueue = append(q.retryQueue, qe)
}

// retryProcessor processes the retry queue.
func (q *EventQueue) retryProcessor() {
	defer q.wg.Done()

	ticker := time.NewTicker(retryProcessorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return
		case <-ticker.C:
			q.processRetries()
		}
	}
}

// processRetries processes events ready for retry.
func (q *EventQueue) processRetries() {
	q.mu.Lock()

	if q.closed {
		q.mu.Unlock()
		return
	}

	now := time.Now()
	ready := make([]*QueuedEvent, 0)
	remaining := make([]*QueuedEvent, 0)

	for _, qe := range q.retryQueue {
		if now.After(qe.NextRetry) {
			ready = append(ready, qe)
		} else {
			remaining = append(remaining, qe)
		}
	}

	q.retryQueue = remaining
	q.mu.Unlock()

	for _, qe := range ready {
		q.mu.RLock()
		closed := q.closed
		q.mu.RUnlock()

		if closed {
			return
		}

		select {
		case q.events <- qe:
		default:
			log.Warn().Str("target", qe.TargetName).Msg("Queue full, dropping retry event")
		}
	}
}

// Stats returns queue statistics.
func (q *EventQueue) Stats() QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return QueueStats{
		QueueLength:      len(q.events),
		RetryQueueLength: len(q.retryQueue),
		TargetCount:      len(q.targets),
	}
}

// QueueStats contains queue statistics.
type QueueStats struct {
	QueueLength      int `json:"queueLength"`
	RetryQueueLength int `json:"retryQueueLength"`
	TargetCount      int `json:"targetCount"`
}
