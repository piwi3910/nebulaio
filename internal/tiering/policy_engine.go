package tiering

import (
	"context"
	"fmt"
	"hash/fnv"
	"slices"
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

	stats := e.getObjectStats(ctx, obj)

	result := &EvaluationResult{
		Object: obj,
	}

	e.evaluatePolicies(obj, stats, policies, result)

	return result, nil
}

func (e *PolicyEngine) getObjectStats(ctx context.Context, obj ObjectMetadata) *ObjectAccessStats {
	stats, err := e.accessStats.GetStats(ctx, obj.Bucket, obj.Key)
	if err != nil {
		stats = &ObjectAccessStats{
			Bucket:      obj.Bucket,
			Key:         obj.Key,
			CurrentTier: obj.CurrentTier,
		}
	}
	return stats
}

func (e *PolicyEngine) evaluatePolicies(obj ObjectMetadata, stats *ObjectAccessStats, policies []*AdvancedPolicy, result *EvaluationResult) {
	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		if !e.shouldEvaluatePolicy(obj, policy) {
			continue
		}

		if !e.matchesSelector(obj, stats, policy.Selector) {
			continue
		}

		triggered, triggerInfo := e.checkTriggers(obj, stats, policy)
		if !triggered {
			continue
		}

		if e.isBlockedByConstraints(obj, policy, result) {
			continue
		}

		result.MatchingPolicies = append(result.MatchingPolicies, PolicyMatch{
			Policy:      policy,
			TriggerInfo: triggerInfo,
		})

		if len(policy.Actions) > 0 && policy.Actions[0].StopProcessing {
			break
		}
	}
}

func (e *PolicyEngine) shouldEvaluatePolicy(obj ObjectMetadata, policy *AdvancedPolicy) bool {
	if policy.Distributed.Enabled && policy.Distributed.ShardByBucket {
		if !e.shouldHandleBucket(obj.Bucket, policy.ID) {
			return false
		}
	}
	return true
}

func (e *PolicyEngine) isBlockedByConstraints(obj ObjectMetadata, policy *AdvancedPolicy, result *EvaluationResult) bool {
	if policy.AntiThrash.Enabled {
		if !e.antiThrash.CanTransition(obj.Bucket, obj.Key, policy) {
			result.BlockedByAntiThrash = true
			return true
		}
	}

	if policy.Schedule.Enabled {
		if !e.isInScheduleWindow(policy.Schedule) {
			result.OutsideSchedule = true
			return true
		}
	}

	if policy.RateLimit.Enabled {
		if !e.rateLimiter.Allow(policy.ID) {
			result.RateLimited = true
			return true
		}
	}

	return false
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
func (e *PolicyEngine) matchesSelector(obj ObjectMetadata, stats *ObjectAccessStats, sel PolicySelector) bool {
	return e.matchesBucketsSelector(obj, sel.Buckets) &&
		e.matchesPrefixesSelector(obj, sel.Prefixes) &&
		e.matchesSuffixesSelector(obj, sel.Suffixes) &&
		e.matchesSizeSelector(obj, sel.MinSize, sel.MaxSize) &&
		e.matchesContentTypesSelector(obj, sel.ContentTypes) &&
		e.matchesCurrentTiersSelector(obj, sel.CurrentTiers) &&
		e.matchesExcludeTiersSelector(obj, sel.ExcludeTiers) &&
		e.matchesStorageClassesSelector(obj, sel.StorageClasses) &&
		e.matchesTagsSelector(obj, sel.Tags)
}

// matchesBucketsSelector checks if object bucket matches any bucket pattern.
func (e *PolicyEngine) matchesBucketsSelector(obj ObjectMetadata, buckets []string) bool {
	if len(buckets) == 0 {
		return true
	}

	for _, pattern := range buckets {
		if matchWildcard(pattern, obj.Bucket) {
			return true
		}
	}

	return false
}

// matchesPrefixesSelector checks if object key has any specified prefix.
func (e *PolicyEngine) matchesPrefixesSelector(obj ObjectMetadata, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(obj.Key, prefix) {
			return true
		}
	}

	return false
}

// matchesSuffixesSelector checks if object key has any specified suffix.
func (e *PolicyEngine) matchesSuffixesSelector(obj ObjectMetadata, suffixes []string) bool {
	if len(suffixes) == 0 {
		return true
	}

	for _, suffix := range suffixes {
		if strings.HasSuffix(obj.Key, suffix) {
			return true
		}
	}

	return false
}

// matchesSizeSelector checks if object size is within the specified range.
func (e *PolicyEngine) matchesSizeSelector(obj ObjectMetadata, minSize, maxSize int64) bool {
	if minSize > 0 && obj.Size < minSize {
		return false
	}

	if maxSize > 0 && obj.Size > maxSize {
		return false
	}

	return true
}

// matchesContentTypesSelector checks if object content type matches any pattern.
func (e *PolicyEngine) matchesContentTypesSelector(obj ObjectMetadata, contentTypes []string) bool {
	if len(contentTypes) == 0 {
		return true
	}

	for _, pattern := range contentTypes {
		if matchContentType(pattern, obj.ContentType) {
			return true
		}
	}

	return false
}

// matchesCurrentTiersSelector checks if object is in any of the specified tiers.
func (e *PolicyEngine) matchesCurrentTiersSelector(obj ObjectMetadata, currentTiers []TierType) bool {
	if len(currentTiers) == 0 {
		return true
	}

	return slices.Contains(currentTiers, obj.CurrentTier)
}

// matchesExcludeTiersSelector checks if object is not in any excluded tier.
func (e *PolicyEngine) matchesExcludeTiersSelector(obj ObjectMetadata, excludeTiers []TierType) bool {
	return !slices.Contains(excludeTiers, obj.CurrentTier)
}

// matchesStorageClassesSelector checks if object storage class matches any specified class.
func (e *PolicyEngine) matchesStorageClassesSelector(obj ObjectMetadata, storageClasses []StorageClass) bool {
	if len(storageClasses) == 0 {
		return true
	}

	for _, sc := range storageClasses {
		if sc == obj.StorageClass {
			return true
		}
	}

	return false
}

// matchesTagsSelector checks if object has all required tags with matching values.
func (e *PolicyEngine) matchesTagsSelector(obj ObjectMetadata, tags map[string]string) bool {
	for key, value := range tags {
		if objValue, ok := obj.Tags[key]; !ok || objValue != value {
			return false
		}
	}

	return true
}

// checkTriggers checks if any triggers are satisfied.
func (e *PolicyEngine) checkTriggers(obj ObjectMetadata, stats *ObjectAccessStats, policy *AdvancedPolicy) (bool, string) {
	now := time.Now()

	for _, trigger := range policy.Triggers {
		if satisfied, reason := e.evaluateTrigger(trigger, obj, stats, now); satisfied {
			return true, reason
		}
	}

	return false, ""
}

// evaluateTrigger evaluates a single trigger.
func (e *PolicyEngine) evaluateTrigger(trigger PolicyTrigger, obj ObjectMetadata, stats *ObjectAccessStats, now time.Time) (bool, string) {
	switch trigger.Type {
	case TriggerTypeAge:
		return e.checkAgeTrigger(trigger, obj, stats, now)
	case TriggerTypeAccess:
		return e.checkAccessTrigger(trigger, stats)
	case TriggerTypeFrequency:
		return e.checkFrequencyTrigger(trigger, stats)
	case TriggerTypeCapacity, TriggerTypeCron:
		// Handled by threshold monitor and scheduler respectively
		return false, ""
	default:
		return false, ""
	}
}

// checkAgeTrigger checks age-based triggers.
func (e *PolicyEngine) checkAgeTrigger(trigger PolicyTrigger, obj ObjectMetadata, stats *ObjectAccessStats, now time.Time) (bool, string) {
	if trigger.Age == nil {
		return false, ""
	}

	if satisfied, reason := e.checkDaysSinceCreation(trigger.Age, obj.CreatedAt, now); satisfied {
		return true, reason
	}

	if satisfied, reason := e.checkDaysSinceModification(trigger.Age, obj.ModifiedAt, now); satisfied {
		return true, reason
	}

	if satisfied, reason := e.checkDaysSinceAccess(trigger.Age, stats, now); satisfied {
		return true, reason
	}

	if satisfied, reason := e.checkHoursSinceAccess(trigger.Age, stats, now); satisfied {
		return true, reason
	}

	return false, ""
}

// checkDaysSinceCreation checks if days since creation threshold is met.
func (e *PolicyEngine) checkDaysSinceCreation(age *AgeTrigger, createdAt time.Time, now time.Time) (bool, string) {
	if age.DaysSinceCreation <= 0 {
		return false, ""
	}

	days := int(now.Sub(createdAt).Hours() / 24)
	if days >= age.DaysSinceCreation {
		return true, fmt.Sprintf("age: %d days since creation", days)
	}

	return false, ""
}

// checkDaysSinceModification checks if days since modification threshold is met.
func (e *PolicyEngine) checkDaysSinceModification(age *AgeTrigger, modifiedAt time.Time, now time.Time) (bool, string) {
	if age.DaysSinceModification <= 0 {
		return false, ""
	}

	days := int(now.Sub(modifiedAt).Hours() / 24)
	if days >= age.DaysSinceModification {
		return true, fmt.Sprintf("age: %d days since modification", days)
	}

	return false, ""
}

// checkDaysSinceAccess checks if days since access threshold is met.
func (e *PolicyEngine) checkDaysSinceAccess(age *AgeTrigger, stats *ObjectAccessStats, now time.Time) (bool, string) {
	if age.DaysSinceAccess <= 0 || stats == nil || stats.LastAccessed.IsZero() {
		return false, ""
	}

	days := int(now.Sub(stats.LastAccessed).Hours() / 24)
	if days >= age.DaysSinceAccess {
		return true, fmt.Sprintf("age: %d days since access", days)
	}

	return false, ""
}

// checkHoursSinceAccess checks if hours since access threshold is met.
func (e *PolicyEngine) checkHoursSinceAccess(age *AgeTrigger, stats *ObjectAccessStats, now time.Time) (bool, string) {
	if age.HoursSinceAccess <= 0 || stats == nil || stats.LastAccessed.IsZero() {
		return false, ""
	}

	hours := int(now.Sub(stats.LastAccessed).Hours())
	if hours >= age.HoursSinceAccess {
		return true, fmt.Sprintf("age: %d hours since access", hours)
	}

	return false, ""
}

// checkAccessTrigger checks access-based triggers.
func (e *PolicyEngine) checkAccessTrigger(trigger PolicyTrigger, stats *ObjectAccessStats) (bool, string) {
	if trigger.Access == nil {
		return false, ""
	}

	if trigger.Access.PromoteOnAnyRead {
		return true, "access: read promotion"
	}

	if trigger.Access.CountThreshold > 0 && trigger.Access.PeriodMinutes > 0 {
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

	return false, ""
}

// checkFrequencyTrigger checks frequency-based triggers.
func (e *PolicyEngine) checkFrequencyTrigger(trigger PolicyTrigger, stats *ObjectAccessStats) (bool, string) {
	if trigger.Frequency == nil || stats == nil {
		return false, ""
	}

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
func (e *PolicyEngine) realtimeWorker(id int) {
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
	// INTENTIONAL: Using context.Background() for background event processing.
	// Real-time events are queued internally and processed asynchronously by workers.
	// The timeout provides the cancellation mechanism for event processing.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	e.recordAccessStats(ctx, event)

	policies, err := e.store.ListByType(ctx, PolicyTypeRealtime)
	if err != nil {
		return
	}

	for _, policy := range policies {
		if policy.Enabled {
			e.processRealtimePolicy(ctx, event, policy)
		}
	}
}

func (e *PolicyEngine) recordAccessStats(ctx context.Context, event RealtimeEvent) {
	if e.accessStats != nil {
		e.accessStats.RecordAccess(ctx, event.Bucket, event.Key, event.Operation, event.Size)
	}
}

func (e *PolicyEngine) processRealtimePolicy(ctx context.Context, event RealtimeEvent, policy *AdvancedPolicy) {
	for _, trigger := range policy.Triggers {
		if e.shouldProcessRealtimeTrigger(trigger, event) {
			e.executeRealtimeActions(ctx, event, policy)
			break
		}
	}
}

func (e *PolicyEngine) shouldProcessRealtimeTrigger(trigger PolicyTrigger, event RealtimeEvent) bool {
	if trigger.Type != TriggerTypeAccess || trigger.Access == nil {
		return false
	}
	return trigger.Access.PromoteOnAnyRead && event.Operation == "GET"
}

func (e *PolicyEngine) executeRealtimeActions(ctx context.Context, event RealtimeEvent, policy *AdvancedPolicy) {
	for _, action := range policy.Actions {
		if action.Type == ActionTransition && action.Transition != nil {
			e.executeRealtimeTransition(ctx, event, action)
		}
	}
}

func (e *PolicyEngine) executeRealtimeTransition(ctx context.Context, event RealtimeEvent, action PolicyAction) {
	err := e.executor.ExecuteTransition(ctx, event.Bucket, event.Key, action.Transition)
	if err != nil {
		log.Error().Err(err).
			Str("bucket", event.Bucket).
			Str("key", event.Key).
			Msg("Failed to execute read promotion")
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
	// INTENTIONAL: Using context.Background() for scheduled policy execution.
	// Policies are triggered by an internal scheduler, not by external requests.
	// The 30-minute timeout provides the cancellation mechanism for long-running policies.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	log.Info().Str("policy_id", policy.ID).Msg("Executing scheduled policy")

	startTime := time.Now()
	stats := &PolicyStats{
		PolicyID:     policy.ID,
		LastExecuted: startTime,
	}

	e.processBucketsForPolicy(ctx, policy, stats)

	stats.LastDuration = time.Since(startTime)
	stats.TotalExecutions++

	e.finalizeScheduledPolicyExecution(ctx, policy, stats)
}

// processBucketsForPolicy processes all buckets matching the policy.
func (e *PolicyEngine) processBucketsForPolicy(ctx context.Context, policy *AdvancedPolicy, stats *PolicyStats) {
	buckets := e.getBucketsForPolicy(ctx, policy)

	for _, bucket := range buckets {
		if !e.shouldProcessBucket(bucket, policy) {
			continue
		}

		e.processObjectsInBucket(ctx, bucket, policy, stats)
	}
}

// shouldProcessBucket checks if this node should handle the bucket.
func (e *PolicyEngine) shouldProcessBucket(bucket string, policy *AdvancedPolicy) bool {
	if policy.Distributed.Enabled && policy.Distributed.ShardByBucket {
		return e.shouldHandleBucket(bucket, policy.ID)
	}
	return true
}

// processObjectsInBucket processes all objects in a bucket for the policy.
func (e *PolicyEngine) processObjectsInBucket(ctx context.Context, bucket string, policy *AdvancedPolicy, stats *PolicyStats) {
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
			e.processObject(ctx, bucket, obj, policy, stats)
		}

		marker = objects[len(objects)-1].Key
		if marker == "" {
			break
		}
	}
}

// processObject evaluates and executes policy for a single object.
func (e *PolicyEngine) processObject(ctx context.Context, bucket string, obj ObjectMetadata, policy *AdvancedPolicy, stats *PolicyStats) {
	stats.ObjectsEvaluated++

	accessStats := e.getAccessStatsForObject(ctx, bucket, obj.Key)

	if !e.shouldTransitionObject(obj, accessStats, policy) {
		return
	}

	e.executeActionsForObject(ctx, bucket, obj, policy, stats)
}

// getAccessStatsForObject retrieves access stats for an object.
func (e *PolicyEngine) getAccessStatsForObject(ctx context.Context, bucket, key string) *ObjectAccessStats {
	accessStats, _ := e.accessStats.GetStats(ctx, bucket, key)
	if accessStats == nil {
		accessStats = &ObjectAccessStats{Bucket: bucket, Key: key}
	}
	return accessStats
}

// shouldTransitionObject checks if object meets policy criteria.
func (e *PolicyEngine) shouldTransitionObject(obj ObjectMetadata, accessStats *ObjectAccessStats, policy *AdvancedPolicy) bool {
	if !e.matchesSelector(obj, accessStats, policy.Selector) {
		return false
	}

	triggered, _ := e.checkTriggers(obj, accessStats, policy)
	if !triggered {
		return false
	}

	if policy.AntiThrash.Enabled {
		return e.antiThrash.CanTransition(obj.Bucket, obj.Key, policy)
	}

	return true
}

// executeActionsForObject executes policy actions on an object.
func (e *PolicyEngine) executeActionsForObject(ctx context.Context, bucket string, obj ObjectMetadata, policy *AdvancedPolicy, stats *PolicyStats) {
	for _, action := range policy.Actions {
		err := e.executor.Execute(ctx, bucket, obj.Key, &action)
		if err != nil {
			stats.Errors++
			stats.LastError = err.Error()
		} else {
			stats.ObjectsTransitioned++
			stats.BytesTransitioned += obj.Size

			if policy.AntiThrash.Enabled {
				e.antiThrash.RecordTransition(bucket, obj.Key)
			}
		}
	}

	if policy.RateLimit.Enabled {
		e.rateLimiter.Wait(policy.ID)
	}
}

// finalizeScheduledPolicyExecution updates stats and logs completion.
func (e *PolicyEngine) finalizeScheduledPolicyExecution(ctx context.Context, policy *AdvancedPolicy, stats *PolicyStats) {
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
func (e *PolicyEngine) getBucketsForPolicy(ctx context.Context, policy *AdvancedPolicy) []string {
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
	// INTENTIONAL: Using context.Background() for periodic threshold checking.
	// This is triggered by an internal timer, not by external requests.
	// The timeout provides the cancellation mechanism for the check operation.
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
	// INTENTIONAL: Using context.Background() for background threshold event processing.
	// Threshold events are generated internally by the threshold monitor.
	// The timeout provides the cancellation mechanism for potentially long-running capacity operations.
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
	targetTier := e.findTargetTierFromPolicy(policy)
	if targetTier == "" {
		return
	}

	targetBytes := e.calculateBytesToFree(event, trigger)
	if targetBytes <= 0 {
		return
	}

	log.Info().
		Str("tier", string(event.Tier)).
		Str("target_tier", string(targetTier)).
		Int64("target_bytes", targetBytes).
		Msg("Freeing tier capacity")

	bytesFreed := e.freeCapacityByTransitioning(ctx, policy, event.Tier, targetTier, targetBytes)

	log.Info().
		Int64("bytes_freed", bytesFreed).
		Int64("target_bytes", targetBytes).
		Msg("Capacity policy completed")
}

// findTargetTierFromPolicy extracts target tier from policy actions.
func (e *PolicyEngine) findTargetTierFromPolicy(policy *AdvancedPolicy) TierType {
	for _, action := range policy.Actions {
		if action.Type == ActionTransition && action.Transition != nil {
			return action.Transition.TargetTier
		}
	}
	return ""
}

// calculateBytesToFree calculates how many bytes need to be freed.
func (e *PolicyEngine) calculateBytesToFree(event ThresholdEvent, trigger *CapacityTrigger) int64 {
	return int64(float64(event.BytesTotal) * (event.Usage - trigger.LowWatermark) / 100)
}

// freeCapacityByTransitioning transitions objects to free capacity.
func (e *PolicyEngine) freeCapacityByTransitioning(ctx context.Context, policy *AdvancedPolicy, sourceTier, targetTier TierType, targetBytes int64) int64 {
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

		bytesFreed += e.transitionOldestObjects(ctx, policy, bucket, objects, sourceTier, targetTier, targetBytes, bytesFreed)

		if bytesFreed >= targetBytes {
			break
		}
	}

	return bytesFreed
}

// transitionOldestObjects transitions objects until target is reached.
func (e *PolicyEngine) transitionOldestObjects(ctx context.Context, policy *AdvancedPolicy, bucket string, objects []ObjectMetadata, sourceTier, targetTier TierType, targetBytes, currentFreed int64) int64 {
	bytesFreed := currentFreed

	for _, obj := range objects {
		if obj.CurrentTier != sourceTier {
			continue
		}

		if !e.checkAntiThrashForObject(policy, bucket, obj.Key) {
			continue
		}

		if err := e.tierManager.TransitionObject(ctx, bucket, obj.Key, targetTier); err != nil {
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

	return bytesFreed
}

// checkAntiThrashForObject checks if object can be transitioned.
func (e *PolicyEngine) checkAntiThrashForObject(policy *AdvancedPolicy, bucket, key string) bool {
	if policy.AntiThrash.Enabled {
		return e.antiThrash.CanTransition(bucket, key, policy)
	}
	return true
}
