package events

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// QueuedEvent represents an event in the queue
type QueuedEvent struct {
	// Event is the S3 event
	Event *S3Event

	// TargetName is the target to publish to
	TargetName string

	// Attempts is the number of delivery attempts
	Attempts int

	// NextRetry is when to retry next
	NextRetry time.Time

	// CreatedAt is when the event was queued
	CreatedAt time.Time
}

// EventQueue manages queued events for async delivery
type EventQueue struct {
	events     chan *QueuedEvent
	retryQueue []*QueuedEvent
	targets    map[string]Target
	maxRetries int
	retryDelay time.Duration
	workers    int
	mu         sync.RWMutex
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
}

// EventQueueConfig configures the event queue
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

// DefaultQueueConfig returns a default queue configuration
func DefaultQueueConfig() EventQueueConfig {
	return EventQueueConfig{
		QueueSize:  10000,
		MaxRetries: 3,
		RetryDelay: 1 * time.Second,
		Workers:    4,
	}
}

// NewEventQueue creates a new event queue
func NewEventQueue(config EventQueueConfig) *EventQueue {
	if config.QueueSize == 0 {
		config.QueueSize = 10000
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 1 * time.Second
	}
	if config.Workers == 0 {
		config.Workers = 4
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

// Start starts the queue workers
func (q *EventQueue) Start() {
	// Start worker goroutines
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}

	// Start retry processor
	q.wg.Add(1)
	go q.retryProcessor()

	log.Info().Int("workers", q.workers).Msg("Event queue started")
}

// Stop stops the queue workers
func (q *EventQueue) Stop() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()

	q.cancel()
	close(q.events)
	q.wg.Wait()

	log.Info().Msg("Event queue stopped")
}

// AddTarget adds a target to the queue
func (q *EventQueue) AddTarget(target Target) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.targets[target.Name()] = target
}

// RemoveTarget removes a target from the queue
func (q *EventQueue) RemoveTarget(name string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.targets, name)
}

// GetTarget returns a target by name
func (q *EventQueue) GetTarget(name string) (Target, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	target, ok := q.targets[name]
	return target, ok
}

// Enqueue adds an event to the queue
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

// EnqueueAll enqueues an event to all targets
func (q *EventQueue) EnqueueAll(event *S3Event) error {
	q.mu.RLock()
	targets := make([]string, 0, len(q.targets))
	for name := range q.targets {
		targets = append(targets, name)
	}
	q.mu.RUnlock()

	for _, name := range targets {
		if err := q.Enqueue(event, name); err != nil {
			log.Warn().Str("target", name).Err(err).Msg("Failed to enqueue event")
		}
	}

	return nil
}

// worker processes events from the queue
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

// processEvent processes a single queued event
func (q *EventQueue) processEvent(qe *QueuedEvent) {
	q.mu.RLock()
	target, ok := q.targets[qe.TargetName]
	q.mu.RUnlock()

	if !ok {
		log.Warn().Str("target", qe.TargetName).Msg("Target not found, dropping event")
		return
	}

	qe.Attempts++

	ctx, cancel := context.WithTimeout(q.ctx, 30*time.Second)
	defer cancel()

	if err := target.Publish(ctx, qe.Event); err != nil {
		log.Warn().
			Str("target", qe.TargetName).
			Int("attempt", qe.Attempts).
			Err(err).
			Msg("Failed to publish event")

		// Schedule retry if within limits
		if qe.Attempts < q.maxRetries {
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

// scheduleRetry schedules an event for retry
func (q *EventQueue) scheduleRetry(qe *QueuedEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.retryQueue = append(q.retryQueue, qe)
}

// retryProcessor processes the retry queue
func (q *EventQueue) retryProcessor() {
	defer q.wg.Done()

	ticker := time.NewTicker(500 * time.Millisecond)
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

// processRetries processes events ready for retry
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

// Stats returns queue statistics
func (q *EventQueue) Stats() QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return QueueStats{
		QueueLength:      len(q.events),
		RetryQueueLength: len(q.retryQueue),
		TargetCount:      len(q.targets),
	}
}

// QueueStats contains queue statistics
type QueueStats struct {
	QueueLength      int `json:"queueLength"`
	RetryQueueLength int `json:"retryQueueLength"`
	TargetCount      int `json:"targetCount"`
}
