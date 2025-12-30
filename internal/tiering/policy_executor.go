package tiering

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// PolicyExecutor executes policy actions.
type PolicyExecutor struct {
	tierManager TierManager
	store       PolicyStore
}

// NewPolicyExecutor creates a new policy executor.
func NewPolicyExecutor(tierManager TierManager, store PolicyStore) *PolicyExecutor {
	return &PolicyExecutor{
		tierManager: tierManager,
		store:       store,
	}
}

// Execute executes a policy action.
func (e *PolicyExecutor) Execute(ctx context.Context, bucket, key string, action *PolicyAction) error {
	switch action.Type {
	case ActionTransition:
		if action.Transition != nil {
			return e.ExecuteTransition(ctx, bucket, key, action.Transition)
		}
	case ActionDelete:
		return e.ExecuteDelete(ctx, bucket, key, action.Delete)
	case ActionReplicate:
		if action.Replicate != nil {
			return e.ExecuteReplicate(ctx, bucket, key, action.Replicate)
		}
	case ActionNotify:
		if action.Notify != nil {
			return e.ExecuteNotify(ctx, bucket, key, action.Notify)
		}
	}

	return nil
}

// ExecuteTransition moves an object to a different tier.
func (e *PolicyExecutor) ExecuteTransition(ctx context.Context, bucket, key string, config *TransitionActionConfig) error {
	log.Debug().
		Str("bucket", bucket).
		Str("key", key).
		Str("target_tier", string(config.TargetTier)).
		Msg("Executing tier transition")

	return e.tierManager.TransitionObject(ctx, bucket, key, config.TargetTier)
}

// ExecuteDelete removes an object.
func (e *PolicyExecutor) ExecuteDelete(ctx context.Context, bucket, key string, config *DeleteActionConfig) error {
	log.Debug().
		Str("bucket", bucket).
		Str("key", key).
		Msg("Executing delete")

	return e.tierManager.DeleteObject(ctx, bucket, key)
}

// ExecuteReplicate copies an object to another location.
func (e *PolicyExecutor) ExecuteReplicate(ctx context.Context, bucket, key string, config *ReplicateActionConfig) error {
	log.Debug().
		Str("bucket", bucket).
		Str("key", key).
		Str("destination", config.Destination).
		Msg("Executing replication")

	// Get object data
	data, err := e.tierManager.GetObject(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("failed to get object for replication: %w", err)
	}

	// In a real implementation, this would copy to the destination
	_ = data

	return nil
}

// ExecuteNotify sends a notification.
func (e *PolicyExecutor) ExecuteNotify(ctx context.Context, bucket, key string, config *NotifyActionConfig) error {
	log.Debug().
		Str("bucket", bucket).
		Str("key", key).
		Str("endpoint", config.Endpoint).
		Msg("Executing notification")

	// In a real implementation, this would send a webhook notification
	return nil
}

// PolicyScheduler schedules policy execution.
type PolicyScheduler struct {
	store       PolicyStore
	stopChan    chan struct{}
	schedules   map[string]*scheduleEntry
	outputCh    chan<- *AdvancedPolicy
	wg          sync.WaitGroup
	schedulesMu sync.RWMutex
}

type scheduleEntry struct {
	policy   *AdvancedPolicy
	nextRun  time.Time
	stopChan chan struct{}
}

// NewPolicyScheduler creates a new scheduler.
func NewPolicyScheduler(store PolicyStore) *PolicyScheduler {
	return &PolicyScheduler{
		store:     store,
		stopChan:  make(chan struct{}),
		schedules: make(map[string]*scheduleEntry),
	}
}

// Start starts the scheduler.
func (s *PolicyScheduler) Start(outputCh chan<- *AdvancedPolicy) error {
	s.outputCh = outputCh

	// Load scheduled policies
	ctx := context.Background()

	policies, err := s.store.ListByType(ctx, PolicyTypeScheduled)
	if err != nil {
		return err
	}

	for _, policy := range policies {
		if policy.Enabled {
			s.schedulePolicy(policy)
		}
	}

	// Watch for policy changes
	s.wg.Add(1)

	go s.watchPolicies()

	return nil
}

// Stop stops the scheduler.
func (s *PolicyScheduler) Stop() {
	close(s.stopChan)
	s.wg.Wait()

	// Stop all scheduled policies
	s.schedulesMu.Lock()

	for _, entry := range s.schedules {
		close(entry.stopChan)
	}

	s.schedules = make(map[string]*scheduleEntry)
	s.schedulesMu.Unlock()
}

// schedulePolicy adds a policy to the schedule.
func (s *PolicyScheduler) schedulePolicy(policy *AdvancedPolicy) {
	// Find cron trigger
	var cronExpr string

	for _, trigger := range policy.Triggers {
		if trigger.Type == TriggerTypeCron && trigger.Cron != nil {
			cronExpr = trigger.Cron.Expression
			break
		}
	}

	if cronExpr == "" {
		return
	}

	s.schedulesMu.Lock()
	defer s.schedulesMu.Unlock()

	// Stop existing schedule if present
	if existing, ok := s.schedules[policy.ID]; ok {
		close(existing.stopChan)
	}

	// Calculate next run
	nextRun := s.parseAndCalculateNext(cronExpr)

	entry := &scheduleEntry{
		policy:   policy,
		nextRun:  nextRun,
		stopChan: make(chan struct{}),
	}

	s.schedules[policy.ID] = entry

	// Start goroutine for this schedule
	s.wg.Add(1)

	go s.runSchedule(entry)
}

// runSchedule runs a scheduled policy.
func (s *PolicyScheduler) runSchedule(entry *scheduleEntry) {
	defer s.wg.Done()

	for {
		waitDuration := time.Until(entry.nextRun)
		if waitDuration < 0 {
			waitDuration = time.Minute
		}

		timer := time.NewTimer(waitDuration)

		select {
		case <-s.stopChan:
			timer.Stop()
			return
		case <-entry.stopChan:
			timer.Stop()
			return
		case <-timer.C:
			// Send policy for execution
			select {
			case s.outputCh <- entry.policy:
			default:
				// Channel full
			}

			// Calculate next run
			var cronExpr string

			for _, trigger := range entry.policy.Triggers {
				if trigger.Type == TriggerTypeCron && trigger.Cron != nil {
					cronExpr = trigger.Cron.Expression
					break
				}
			}

			entry.nextRun = s.parseAndCalculateNext(cronExpr)
		}
	}
}

// parseAndCalculateNext parses cron expression and calculates next run time.
func (s *PolicyScheduler) parseAndCalculateNext(cronExpr string) time.Time {
	// Simplified cron parsing - in production use a proper cron library
	// Format: minute hour day-of-month month day-of-week
	// For now, just schedule for next hour
	now := time.Now()
	return now.Add(1 * time.Hour).Truncate(time.Hour)
}

// watchPolicies watches for policy changes.
func (s *PolicyScheduler) watchPolicies() {
	defer s.wg.Done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := s.store.Watch(ctx)
	if err != nil {
		return
	}

	for {
		select {
		case <-s.stopChan:
			return
		case event := <-events:
			switch event.Type {
			case PolicyChangeCreated, PolicyChangeUpdated:
				if event.Policy.Type == PolicyTypeScheduled && event.Policy.Enabled {
					s.schedulePolicy(event.Policy)
				}
			case PolicyChangeDeleted:
				s.unschedulePolicy(event.Policy.ID)
			}
		}
	}
}

// unschedulePolicy removes a policy from the schedule.
func (s *PolicyScheduler) unschedulePolicy(policyID string) {
	s.schedulesMu.Lock()
	defer s.schedulesMu.Unlock()

	if entry, ok := s.schedules[policyID]; ok {
		close(entry.stopChan)
		delete(s.schedules, policyID)
	}
}

// AntiThrashManager prevents tier oscillation.
type AntiThrashManager struct {
	transitions map[string]*transitionRecord
	mu          sync.RWMutex
}

type transitionRecord struct {
	lastTransition  time.Time
	transitionCount int
}

// NewAntiThrashManager creates a new anti-thrash manager.
func NewAntiThrashManager() *AntiThrashManager {
	return &AntiThrashManager{
		transitions: make(map[string]*transitionRecord),
	}
}

// CanTransition checks if an object can be transitioned.
func (m *AntiThrashManager) CanTransition(bucket, key string, policy *AdvancedPolicy) bool {
	if !policy.AntiThrash.Enabled {
		return true
	}

	fullKey := bucket + "/" + key

	m.mu.RLock()
	record, exists := m.transitions[fullKey]
	m.mu.RUnlock()

	if !exists {
		return true
	}

	now := time.Now()

	// Check minimum time in tier
	minTime := policy.AntiThrash.GetMinTimeInTier()
	if minTime > 0 && now.Sub(record.lastTransition) < minTime {
		return false
	}

	// Check cooldown after transition
	cooldown := policy.AntiThrash.GetCooldownAfterTransition()
	if cooldown > 0 && now.Sub(record.lastTransition) < cooldown {
		return false
	}

	// Check max transitions per day
	if policy.AntiThrash.MaxTransitionsPerDay > 0 {
		dayStart := now.Truncate(24 * time.Hour)
		if record.lastTransition.After(dayStart) && record.transitionCount >= policy.AntiThrash.MaxTransitionsPerDay {
			return false
		}
	}

	return true
}

// RecordTransition records a tier transition.
func (m *AntiThrashManager) RecordTransition(bucket, key string) {
	fullKey := bucket + "/" + key
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	record, exists := m.transitions[fullKey]
	if !exists {
		m.transitions[fullKey] = &transitionRecord{
			lastTransition:  now,
			transitionCount: 1,
		}

		return
	}

	// Reset count if new day
	dayStart := now.Truncate(24 * time.Hour)
	if record.lastTransition.Before(dayStart) {
		record.transitionCount = 0
	}

	record.lastTransition = now
	record.transitionCount++
}

// Cleanup removes old records.
func (m *AntiThrashManager) Cleanup(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)

	m.mu.Lock()
	defer m.mu.Unlock()

	for key, record := range m.transitions {
		if record.lastTransition.Before(cutoff) {
			delete(m.transitions, key)
		}
	}
}

// RateLimiter controls policy execution rate.
type RateLimiter struct {
	limiters map[string]*tokenBucket
	config   RateLimiterConfig
	mu       sync.RWMutex
}

// RateLimiterConfig configures the rate limiter.
type RateLimiterConfig struct {
	DefaultObjectsPerSecond int
	DefaultBurstSize        int
}

// DefaultRateLimiterConfig returns default configuration.
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		DefaultObjectsPerSecond: 100,
		DefaultBurstSize:        10,
	}
}

type tokenBucket struct {
	lastUpdate   time.Time
	tokens       float64
	tokensPerSec float64
	maxTokens    float64
	mu           sync.Mutex
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	return &RateLimiter{
		config:   cfg,
		limiters: make(map[string]*tokenBucket),
	}
}

// Allow checks if an operation is allowed.
func (r *RateLimiter) Allow(policyID string) bool {
	r.mu.Lock()

	bucket, exists := r.limiters[policyID]
	if !exists {
		bucket = &tokenBucket{
			tokens:       float64(r.config.DefaultBurstSize),
			lastUpdate:   time.Now(),
			tokensPerSec: float64(r.config.DefaultObjectsPerSecond),
			maxTokens:    float64(r.config.DefaultBurstSize),
		}
		r.limiters[policyID] = bucket
	}

	r.mu.Unlock()

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(bucket.lastUpdate).Seconds()

	bucket.tokens += elapsed * bucket.tokensPerSec
	if bucket.tokens > bucket.maxTokens {
		bucket.tokens = bucket.maxTokens
	}

	bucket.lastUpdate = now

	// Check if we have a token
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}

	return false
}

// Wait waits until an operation is allowed.
func (r *RateLimiter) Wait(policyID string) {
	for !r.Allow(policyID) {
		time.Sleep(10 * time.Millisecond)
	}
}

// SetRate sets the rate for a policy.
func (r *RateLimiter) SetRate(policyID string, objectsPerSecond int, burstSize int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.limiters[policyID] = &tokenBucket{
		tokens:       float64(burstSize),
		lastUpdate:   time.Now(),
		tokensPerSec: float64(objectsPerSecond),
		maxTokens:    float64(burstSize),
	}
}
