package tiering

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// PolicyEngine evaluates and executes tiering policies.
type PolicyEngine struct {
	tierManager  TierManager
	store        PolicyStore
	rateLimiter  *RateLimiter
	executor     *PolicyExecutor
	scheduler    *PolicyScheduler
	antiThrash   *AntiThrashManager
	accessStats  *AccessTracker
	stopChan     chan struct{}
	realtimeCh   chan RealtimeEvent
	scheduledCh  chan *AdvancedPolicy
	thresholdCh  chan ThresholdEvent
	nodeID       string
	clusterNodes []string
	workerWg     sync.WaitGroup
}

// TierManager interface for tier operations.
type TierManager interface {
	// GetObject retrieves an object
	GetObject(ctx context.Context, bucket, key string) ([]byte, error)

	// TransitionObject moves an object between tiers
	TransitionObject(ctx context.Context, bucket, key string, targetTier TierType) error

	// GetTierInfo returns information about a tier
	GetTierInfo(ctx context.Context, tier TierType) (*TierInfo, error)

	// GetObjectTier returns the current tier of an object
	GetObjectTier(ctx context.Context, bucket, key string) (TierType, error)

	// ListObjects lists objects in a bucket
	ListObjects(ctx context.Context, bucket, prefix string, limit int) ([]ObjectMetadata, error)

	// DeleteObject removes an object
	DeleteObject(ctx context.Context, bucket, key string) error
}

// TierInfo contains tier statistics.
type TierInfo struct {
	Tier           TierType
	TotalBytes     int64
	UsedBytes      int64
	AvailableBytes int64
	ObjectCount    int64
	UsagePercent   float64
}

// ObjectMetadata contains object information.
type ObjectMetadata struct {
	CreatedAt    time.Time
	ModifiedAt   time.Time
	Tags         map[string]string
	Bucket       string
	Key          string
	ContentType  string
	StorageClass StorageClass
	CurrentTier  TierType
	Size         int64
}

// RealtimeEvent represents an object access event for real-time policies.
type RealtimeEvent struct {
	Timestamp time.Time
	Bucket    string
	Key       string
	Operation string
	Size      int64
}

// ThresholdEvent represents a capacity threshold crossing.
type ThresholdEvent struct {
	Tier       TierType
	Crossed    string
	Usage      float64
	Threshold  float64
	BytesUsed  int64
	BytesTotal int64
}

// PolicyEngineConfig configures the policy engine.
type PolicyEngineConfig struct {
	// NodeID is this node's identifier
	NodeID string

	// ClusterNodes is a list of all cluster nodes
	ClusterNodes []string

	// RealtimeWorkers is number of real-time policy workers
	RealtimeWorkers int

	// ScheduledWorkers is number of scheduled policy workers
	ScheduledWorkers int

	// ThresholdCheckInterval is how often to check capacity thresholds
	ThresholdCheckInterval time.Duration

	// MaxPendingEvents is the max number of pending events
	MaxPendingEvents int
}

// DefaultPolicyEngineConfig returns default configuration.
func DefaultPolicyEngineConfig() PolicyEngineConfig {
	return PolicyEngineConfig{
		NodeID:                 "node-1",
		ClusterNodes:           []string{"node-1"},
		RealtimeWorkers:        4,
		ScheduledWorkers:       2,
		ThresholdCheckInterval: 1 * time.Minute,
		MaxPendingEvents:       10000,
	}
}

// NewPolicyEngine creates a new policy engine.
func NewPolicyEngine(
	cfg PolicyEngineConfig,
	store PolicyStore,
	tierManager TierManager,
	accessStats *AccessTracker,
) *PolicyEngine {
	engine := &PolicyEngine{
		store:        store,
		accessStats:  accessStats,
		tierManager:  tierManager,
		nodeID:       cfg.NodeID,
		clusterNodes: cfg.ClusterNodes,
		stopChan:     make(chan struct{}),
		realtimeCh:   make(chan RealtimeEvent, cfg.MaxPendingEvents),
		scheduledCh:  make(chan *AdvancedPolicy, 100),
		thresholdCh:  make(chan ThresholdEvent, 100),
	}

	engine.executor = NewPolicyExecutor(tierManager, store)
	engine.scheduler = NewPolicyScheduler(store)
	engine.antiThrash = NewAntiThrashManager()
	engine.rateLimiter = NewRateLimiter(DefaultRateLimiterConfig())

	return engine
}

// Start starts the policy engine.
func (e *PolicyEngine) Start() error {
	log.Info().Str("node_id", e.nodeID).Msg("Starting policy engine")

	// Start real-time workers
	for i := range 4 {
		e.workerWg.Add(1)

		go e.realtimeWorker(i)
	}

	// Start scheduled policy worker
	e.workerWg.Add(1)

	go e.scheduledWorker()

	// Start threshold monitor
	e.workerWg.Add(1)

	go e.thresholdMonitor()

	// Start threshold worker
	e.workerWg.Add(1)

	go e.thresholdWorker()

	// Start scheduler
	err := e.scheduler.Start(e.scheduledCh)
	if err != nil {
		return err
	}

	return nil
}

// Stop stops the policy engine.
func (e *PolicyEngine) Stop() error {
	log.Info().Msg("Stopping policy engine")

	close(e.stopChan)
	e.scheduler.Stop()
	e.workerWg.Wait()

	return nil
}

// RecordAccess records an object access for real-time policies.
func (e *PolicyEngine) RecordAccess(bucket, key, operation string, size int64) {
	event := RealtimeEvent{
		Bucket:    bucket,
		Key:       key,
		Operation: operation,
		Size:      size,
		Timestamp: time.Now(),
	}

	// Non-blocking send
	select {
	case e.realtimeCh <- event:
	default:
		// Channel full, drop event
	}
}

// EvaluateObject evaluates all policies for an object.
func (e *PolicyEngine) EvaluateObject(ctx context.Context, obj ObjectMetadata) (*EvaluationResult, error) {
	policies, err := e.store.List(ctx)
	if err != nil {
		return nil, err
	}

	// Get access stats for the object
	stats, err := e.accessStats.GetStats(ctx, obj.Bucket, obj.Key)
	if err != nil {
		stats = &ObjectAccessStats{
			Bucket:      obj.Bucket,
			Key:         obj.Key,
			CurrentTier: obj.CurrentTier,
		}
	}

	result := &EvaluationResult{
		Object: obj,
	}

	// Evaluate policies in priority order
	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		// Check if this node should handle this policy for this bucket
		if policy.Distributed.Enabled && policy.Distributed.ShardByBucket {
			if !e.shouldHandleBucket(obj.Bucket, policy.ID) {
				continue
			}
		}

		// Check selector
		if !e.matchesSelector(obj, stats, policy.Selector) {
			continue
		}

		// Check triggers
		triggered, triggerInfo := e.checkTriggers(obj, stats, policy)
		if !triggered {
			continue
		}

		// Check anti-thrash
		if policy.AntiThrash.Enabled {
			if !e.antiThrash.CanTransition(obj.Bucket, obj.Key, policy) {
				result.BlockedByAntiThrash = true
				continue
			}
		}

		// Check schedule
		if policy.Schedule.Enabled {
			if !e.isInScheduleWindow(policy.Schedule) {
				result.OutsideSchedule = true
				continue
			}
		}

		// Check rate limit
		if policy.RateLimit.Enabled {
			if !e.rateLimiter.Allow(policy.ID) {
				result.RateLimited = true
				continue
			}
		}

		// Add matching policy
		result.MatchingPolicies = append(result.MatchingPolicies, PolicyMatch{
			Policy:      policy,
			TriggerInfo: triggerInfo,
		})

		// Check if first action has stopProcessing
		if len(policy.Actions) > 0 && policy.Actions[0].StopProcessing {
			break
		}
	}

	return result, nil
}

// EvaluationResult contains the result of policy evaluation.
type EvaluationResult struct {
	Object              ObjectMetadata
	MatchingPolicies    []PolicyMatch
	BlockedByAntiThrash bool
	OutsideSchedule     bool
	RateLimited         bool
}

// PolicyMatch represents a matched policy.
type PolicyMatch struct {
	Policy      *AdvancedPolicy
	TriggerInfo string
}

// matchesSelector checks if an object matches a policy selector.
func (e *PolicyEngine) matchesSelector(obj ObjectMetadata, _stats *ObjectAccessStats, sel PolicySelector) bool {
	// Check buckets
	if len(sel.Buckets) > 0 {
		matched := false

		for _, pattern := range sel.Buckets {
			if matchWildcard(pattern, obj.Bucket) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	// Check prefixes
	if len(sel.Prefixes) > 0 {
		matched := false

		for _, prefix := range sel.Prefixes {
			if strings.HasPrefix(obj.Key, prefix) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	// Check suffixes
	if len(sel.Suffixes) > 0 {
		matched := false

		for _, suffix := range sel.Suffixes {
			if strings.HasSuffix(obj.Key, suffix) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	// Check size
	if sel.MinSize > 0 && obj.Size < sel.MinSize {
		return false
	}

	if sel.MaxSize > 0 && obj.Size > sel.MaxSize {
		return false
	}

	// Check content types
	if len(sel.ContentTypes) > 0 {
		matched := false

		for _, pattern := range sel.ContentTypes {
			if matchContentType(pattern, obj.ContentType) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	// Check current tiers
	if len(sel.CurrentTiers) > 0 {
		matched := false

		for _, tier := range sel.CurrentTiers {
			if tier == obj.CurrentTier {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	// Check excluded tiers
	if len(sel.ExcludeTiers) > 0 {
		for _, tier := range sel.ExcludeTiers {
			if tier == obj.CurrentTier {
				return false
			}
		}
	}

	// Check storage classes
	if len(sel.StorageClasses) > 0 {
		matched := false

		for _, sc := range sel.StorageClasses {
			if sc == obj.StorageClass {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	// Check tags
	if len(sel.Tags) > 0 {
		for key, value := range sel.Tags {
			if objValue, ok := obj.Tags[key]; !ok || objValue != value {
				return false
			}
		}
	}

	return true
}

// checkTriggers checks if any triggers are satisfied.
func (e *PolicyEngine) checkTriggers(obj ObjectMetadata, stats *ObjectAccessStats, policy *AdvancedPolicy) (bool, string) {
	now := time.Now()

	for _, trigger := range policy.Triggers {
		switch trigger.Type {
		case TriggerTypeAge:
			if trigger.Age != nil {
				if trigger.Age.DaysSinceCreation > 0 {
					days := int(now.Sub(obj.CreatedAt).Hours() / 24)
					if days >= trigger.Age.DaysSinceCreation {
						return true, fmt.Sprintf("age: %d days since creation", days)
					}
				}

				if trigger.Age.DaysSinceModification > 0 {
					days := int(now.Sub(obj.ModifiedAt).Hours() / 24)
					if days >= trigger.Age.DaysSinceModification {
						return true, fmt.Sprintf("age: %d days since modification", days)
					}
				}

				if trigger.Age.DaysSinceAccess > 0 && !stats.LastAccessed.IsZero() {
					days := int(now.Sub(stats.LastAccessed).Hours() / 24)
					if days >= trigger.Age.DaysSinceAccess {
						return true, fmt.Sprintf("age: %d days since access", days)
					}
				}

				if trigger.Age.HoursSinceAccess > 0 && !stats.LastAccessed.IsZero() {
					hours := int(now.Sub(stats.LastAccessed).Hours())
					if hours >= trigger.Age.HoursSinceAccess {
						return true, fmt.Sprintf("age: %d hours since access", hours)
					}
				}
			}

		case TriggerTypeAccess:
			if trigger.Access != nil {
				if trigger.Access.PromoteOnAnyRead {
					// This is handled in real-time worker
					return true, "access: read promotion"
				}

				if trigger.Access.CountThreshold > 0 && trigger.Access.PeriodMinutes > 0 {
					// Count accesses in period
					count := e.countAccessesInPeriod(stats, trigger.Access.PeriodMinutes)
					if trigger.Access.Direction == TierDirectionDown && count < int64(trigger.Access.CountThreshold) {
						return true, fmt.Sprintf("access: %d accesses in %d minutes (threshold: %d)",
							count, trigger.Access.PeriodMinutes, trigger.Access.CountThreshold)
					}

					if trigger.Access.Direction == TierDirectionUp && count >= int64(trigger.Access.CountThreshold) {
						return true, fmt.Sprintf("access: %d accesses in %d minutes (threshold: %d)",
							count, trigger.Access.PeriodMinutes, trigger.Access.CountThreshold)
					}
				}
			}

		case TriggerTypeFrequency:
			if trigger.Frequency != nil {
				avgPerDay := stats.AverageAccessesDay
				if trigger.Frequency.MinAccessesPerDay > 0 && avgPerDay >= trigger.Frequency.MinAccessesPerDay {
					return true, fmt.Sprintf("frequency: %.2f accesses/day (min: %.2f)", avgPerDay, trigger.Frequency.MinAccessesPerDay)
				}

				if trigger.Frequency.MaxAccessesPerDay > 0 && avgPerDay <= trigger.Frequency.MaxAccessesPerDay {
					return true, fmt.Sprintf("frequency: %.2f accesses/day (max: %.2f)", avgPerDay, trigger.Frequency.MaxAccessesPerDay)
				}

				if trigger.Frequency.Pattern != "" && stats.AccessTrend == trigger.Frequency.Pattern {
					return true, fmt.Sprintf("frequency: pattern match '%s'", trigger.Frequency.Pattern)
				}
			}

		case TriggerTypeCapacity:
			// Capacity triggers are handled by threshold monitor
			continue

		case TriggerTypeCron:
			// Cron triggers are handled by scheduler
			continue
		}
	}

	return false, ""
}

// countAccessesInPeriod counts accesses in the last N minutes.
func (e *PolicyEngine) countAccessesInPeriod(stats *ObjectAccessStats, minutes int) int64 {
	if stats == nil {
		return 0
	}

	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	var count int64

	for _, record := range stats.AccessHistory {
		if record.Timestamp.After(cutoff) {
			count++
		}
	}

	return count
}

// shouldHandleBucket determines if this node should handle a bucket (consistent hashing).
func (e *PolicyEngine) shouldHandleBucket(bucket string, policyID string) bool {
	if len(e.clusterNodes) <= 1 {
		return true
	}

	// Use consistent hashing
	h := fnv.New32a()
	h.Write([]byte(bucket + policyID))
	hash := h.Sum32()

	// Sort nodes for consistent ordering
	nodes := make([]string, len(e.clusterNodes))
	copy(nodes, e.clusterNodes)
	sort.Strings(nodes)

	// Pick node based on hash
	nodeIndex := int(hash) % len(nodes)

	return nodes[nodeIndex] == e.nodeID
}

// isInScheduleWindow checks if current time is in a maintenance window.
func (e *PolicyEngine) isInScheduleWindow(schedule ScheduleConfig) bool {
	if !schedule.Enabled {
		return true
	}

	now := time.Now()
	if schedule.Timezone != "" {
		loc, err := time.LoadLocation(schedule.Timezone)
		if err == nil {
			now = now.In(loc)
		}
	}

	// Check blackout windows first
	for _, window := range schedule.BlackoutWindows {
		if e.isInWindow(now, window) {
			return false
		}
	}

	// If maintenance windows are defined, must be in one
	if len(schedule.MaintenanceWindows) > 0 {
		for _, window := range schedule.MaintenanceWindows {
			if e.isInWindow(now, window) {
				return true
			}
		}

		return false
	}

	return true
}

// isInWindow checks if a time is within a window.
func (e *PolicyEngine) isInWindow(t time.Time, window MaintenanceWindow) bool {
	// Check day of week
	if len(window.DaysOfWeek) > 0 {
		dayMatched := false

		weekday := int(t.Weekday())
		for _, day := range window.DaysOfWeek {
			if day == weekday {
				dayMatched = true
				break
			}
		}

		if !dayMatched {
			return false
		}
	}

	// Parse times
	startParts := strings.Split(window.StartTime, ":")
	endParts := strings.Split(window.EndTime, ":")

	if len(startParts) != 2 || len(endParts) != 2 {
		return false
	}

	startHour, _ := strconv.Atoi(startParts[0])
	startMin, _ := strconv.Atoi(startParts[1])
	endHour, _ := strconv.Atoi(endParts[0])
	endMin, _ := strconv.Atoi(endParts[1])

	currentMinutes := t.Hour()*60 + t.Minute()
	startMinutes := startHour*60 + startMin
	endMinutes := endHour*60 + endMin

	// Handle window that spans midnight
	if endMinutes < startMinutes {
		return currentMinutes >= startMinutes || currentMinutes <= endMinutes
	}

	return currentMinutes >= startMinutes && currentMinutes <= endMinutes
}

// realtimeWorker processes real-time access events.
func (e *PolicyEngine) realtimeWorker(_id int) {
	defer e.workerWg.Done()

	for {
		select {
		case <-e.stopChan:
			return
		case event := <-e.realtimeCh:
			e.handleRealtimeEvent(event)
		}
	}
}

// handleRealtimeEvent handles a single real-time event.
func (e *PolicyEngine) handleRealtimeEvent(event RealtimeEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Record access in stats
	if e.accessStats != nil {
		e.accessStats.RecordAccess(ctx, event.Bucket, event.Key, event.Operation, event.Size)
	}

	// Get real-time policies
	policies, err := e.store.ListByType(ctx, PolicyTypeRealtime)
	if err != nil {
		return
	}

	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		// Check for read promotion trigger
		for _, trigger := range policy.Triggers {
			if trigger.Type == TriggerTypeAccess && trigger.Access != nil {
				if trigger.Access.PromoteOnAnyRead && event.Operation == "GET" {
					// Execute promotion
					for _, action := range policy.Actions {
						if action.Type == ActionTransition && action.Transition != nil {
							err := e.executor.ExecuteTransition(ctx, event.Bucket, event.Key, action.Transition)
							if err != nil {
								log.Error().Err(err).
									Str("bucket", event.Bucket).
									Str("key", event.Key).
									Msg("Failed to execute read promotion")
							}
						}
					}
				}
			}
		}
	}
}

// scheduledWorker processes scheduled policies.
func (e *PolicyEngine) scheduledWorker() {
	defer e.workerWg.Done()

	for {
		select {
		case <-e.stopChan:
			return
		case policy := <-e.scheduledCh:
			e.executeScheduledPolicy(policy)
		}
	}
}

// executeScheduledPolicy executes a scheduled policy.
func (e *PolicyEngine) executeScheduledPolicy(policy *AdvancedPolicy) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	log.Info().Str("policy_id", policy.ID).Msg("Executing scheduled policy")

	startTime := time.Now()
	stats := &PolicyStats{
		PolicyID:     policy.ID,
		LastExecuted: startTime,
	}

	// Iterate through buckets that match the policy selector
	buckets := e.getBucketsForPolicy(ctx, policy)

	for _, bucket := range buckets {
		// Check if this node should handle this bucket
		if policy.Distributed.Enabled && policy.Distributed.ShardByBucket {
			if !e.shouldHandleBucket(bucket, policy.ID) {
				continue
			}
		}

		// List objects in bucket
		var marker string

		for {
			objects, err := e.tierManager.ListObjects(ctx, bucket, "", 1000)
			if err != nil {
				stats.Errors++
				stats.LastError = err.Error()

				break
			}

			if len(objects) == 0 {
				break
			}

			for _, obj := range objects {
				stats.ObjectsEvaluated++

				// Get access stats
				accessStats, _ := e.accessStats.GetStats(ctx, bucket, obj.Key)
				if accessStats == nil {
					accessStats = &ObjectAccessStats{Bucket: bucket, Key: obj.Key}
				}

				// Check selector
				if !e.matchesSelector(obj, accessStats, policy.Selector) {
					continue
				}

				// Check triggers
				triggered, _ := e.checkTriggers(obj, accessStats, policy)
				if !triggered {
					continue
				}

				// Check anti-thrash
				if policy.AntiThrash.Enabled {
					if !e.antiThrash.CanTransition(bucket, obj.Key, policy) {
						continue
					}
				}

				// Execute actions
				for _, action := range policy.Actions {
					err := e.executor.Execute(ctx, bucket, obj.Key, &action)
					if err != nil {
						stats.Errors++
						stats.LastError = err.Error()
					} else {
						stats.ObjectsTransitioned++
						stats.BytesTransitioned += obj.Size

						// Record transition in anti-thrash
						if policy.AntiThrash.Enabled {
							e.antiThrash.RecordTransition(bucket, obj.Key)
						}
					}
				}

				// Rate limiting
				if policy.RateLimit.Enabled {
					e.rateLimiter.Wait(policy.ID)
				}
			}

			marker = objects[len(objects)-1].Key
			if marker == "" {
				break
			}
		}
	}

	stats.LastDuration = time.Since(startTime)
	stats.TotalExecutions++

	// Update stats
	err := e.store.UpdateStats(ctx, stats)
	if err != nil {
		log.Error().Err(err).Str("policy_id", policy.ID).Msg("Failed to update policy stats")
	}

	log.Info().
		Str("policy_id", policy.ID).
		Dur("duration", stats.LastDuration).
		Int64("evaluated", stats.ObjectsEvaluated).
		Int64("transitioned", stats.ObjectsTransitioned).
		Msg("Scheduled policy completed")
}

// getBucketsForPolicy returns buckets that match a policy.
func (e *PolicyEngine) getBucketsForPolicy(_ctx context.Context, policy *AdvancedPolicy) []string {
	// In a real implementation, this would query the metadata store
	// For now, return the buckets from the selector
	if len(policy.Selector.Buckets) > 0 {
		return policy.Selector.Buckets
	}

	// Return all buckets if no filter
	return []string{"*"}
}

// thresholdMonitor monitors tier capacity thresholds.
func (e *PolicyEngine) thresholdMonitor() {
	defer e.workerWg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.checkThresholds()
		}
	}
}

// checkThresholds checks all tier capacity thresholds.
func (e *PolicyEngine) checkThresholds() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get threshold policies
	policies, err := e.store.List(ctx)
	if err != nil {
		return
	}

	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		for _, trigger := range policy.Triggers {
			if trigger.Type != TriggerTypeCapacity || trigger.Capacity == nil {
				continue
			}

			// Get tier info
			info, err := e.tierManager.GetTierInfo(ctx, trigger.Capacity.Tier)
			if err != nil {
				continue
			}

			// Check high watermark
			if info.UsagePercent >= trigger.Capacity.HighWatermark {
				e.thresholdCh <- ThresholdEvent{
					Tier:       trigger.Capacity.Tier,
					Usage:      info.UsagePercent,
					Threshold:  trigger.Capacity.HighWatermark,
					Crossed:    "high",
					BytesUsed:  info.UsedBytes,
					BytesTotal: info.TotalBytes,
				}
			}
		}
	}
}

// thresholdWorker handles threshold events.
func (e *PolicyEngine) thresholdWorker() {
	defer e.workerWg.Done()

	for {
		select {
		case <-e.stopChan:
			return
		case event := <-e.thresholdCh:
			e.handleThresholdEvent(event)
		}
	}
}

// handleThresholdEvent handles a threshold crossing event.
func (e *PolicyEngine) handleThresholdEvent(event ThresholdEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	log.Info().
		Str("tier", string(event.Tier)).
		Float64("usage", event.Usage).
		Float64("threshold", event.Threshold).
		Msg("Handling threshold event")

	// Get policies with capacity triggers for this tier
	policies, err := e.store.List(ctx)
	if err != nil {
		return
	}

	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		for _, trigger := range policy.Triggers {
			if trigger.Type != TriggerTypeCapacity || trigger.Capacity == nil {
				continue
			}

			if trigger.Capacity.Tier != event.Tier {
				continue
			}

			// Execute capacity-based actions
			e.executeCapacityPolicy(ctx, policy, trigger.Capacity, event)
		}
	}
}

// executeCapacityPolicy moves objects to free up tier capacity.
func (e *PolicyEngine) executeCapacityPolicy(ctx context.Context, policy *AdvancedPolicy, trigger *CapacityTrigger, event ThresholdEvent) {
	// Get target tier from actions
	var targetTier TierType

	for _, action := range policy.Actions {
		if action.Type == ActionTransition && action.Transition != nil {
			targetTier = action.Transition.TargetTier
			break
		}
	}

	if targetTier == "" {
		return
	}

	// Calculate how much to free
	targetBytes := int64(float64(event.BytesTotal) * (event.Usage - trigger.LowWatermark) / 100)
	if targetBytes <= 0 {
		return
	}

	log.Info().
		Str("tier", string(event.Tier)).
		Str("target_tier", string(targetTier)).
		Int64("target_bytes", targetBytes).
		Msg("Freeing tier capacity")

	// Move oldest objects first
	bytesFreed := int64(0)

	for _, bucket := range policy.Selector.Buckets {
		objects, err := e.tierManager.ListObjects(ctx, bucket, "", 1000)
		if err != nil {
			continue
		}

		// Sort by last access time (oldest first)
		sort.Slice(objects, func(i, j int) bool {
			return objects[i].ModifiedAt.Before(objects[j].ModifiedAt)
		})

		for _, obj := range objects {
			if obj.CurrentTier != event.Tier {
				continue
			}

			// Check anti-thrash
			if policy.AntiThrash.Enabled {
				if !e.antiThrash.CanTransition(bucket, obj.Key, policy) {
					continue
				}
			}

			// Transition object
			err := e.tierManager.TransitionObject(ctx, bucket, obj.Key, targetTier)
			if err != nil {
				continue
			}

			bytesFreed += obj.Size

			if policy.AntiThrash.Enabled {
				e.antiThrash.RecordTransition(bucket, obj.Key)
			}

			if bytesFreed >= targetBytes {
				break
			}
		}

		if bytesFreed >= targetBytes {
			break
		}
	}

	log.Info().
		Int64("bytes_freed", bytesFreed).
		Int64("target_bytes", targetBytes).
		Msg("Capacity policy completed")
}
