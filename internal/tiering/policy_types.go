package tiering

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// PolicyType represents the type of policy.
type PolicyType string

const (
	// PolicyTypeScheduled runs on a cron-like schedule.
	PolicyTypeScheduled PolicyType = "scheduled"
	// PolicyTypeRealtime evaluates on every access.
	PolicyTypeRealtime PolicyType = "realtime"
	// PolicyTypeThreshold triggers based on capacity thresholds.
	PolicyTypeThreshold PolicyType = "threshold"
	// PolicyTypeS3Lifecycle is S3-compatible lifecycle policy.
	PolicyTypeS3Lifecycle PolicyType = "s3_lifecycle"
)

// PolicyScope defines where a policy applies.
type PolicyScope string

const (
	// PolicyScopeGlobal applies to all buckets.
	PolicyScopeGlobal PolicyScope = "global"
	// PolicyScopeBucket applies to specific buckets.
	PolicyScopeBucket PolicyScope = "bucket"
	// PolicyScopePrefix applies to specific prefixes.
	PolicyScopePrefix PolicyScope = "prefix"
	// PolicyScopeObject applies to specific objects.
	PolicyScopeObject PolicyScope = "object"
)

// TierDirection indicates the direction of tier movement.
type TierDirection string

const (
	// TierDirectionDown moves to colder tiers.
	TierDirectionDown TierDirection = "down"
	// TierDirectionUp moves to hotter tiers (promotion).
	TierDirectionUp TierDirection = "up"
)

// TriggerType defines what triggers a policy evaluation.
type TriggerType string

const (
	// TriggerTypeAge triggers based on object age.
	TriggerTypeAge TriggerType = "age"
	// TriggerTypeAccess triggers based on access patterns.
	TriggerTypeAccess TriggerType = "access"
	// TriggerTypeCapacity triggers based on tier capacity.
	TriggerTypeCapacity TriggerType = "capacity"
	// TriggerTypeFrequency triggers based on access frequency.
	TriggerTypeFrequency TriggerType = "frequency"
	// TriggerTypeCron triggers on a schedule.
	TriggerTypeCron TriggerType = "cron"
)

// AdvancedPolicy is the comprehensive policy model.
type AdvancedPolicy struct {
	// 8-byte fields (pointers, slices)
	// Triggers define when the policy should evaluate
	Triggers []PolicyTrigger `json:"triggers" yaml:"triggers"`

	// Actions define what happens when conditions are met
	Actions []PolicyAction `json:"actions" yaml:"actions"`

	LastRunAt *time.Time `json:"lastRunAt,omitempty" yaml:"lastRunAt,omitempty"`

	// Metadata for tracking
	CreatedAt time.Time `json:"createdAt" yaml:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" yaml:"updatedAt"`

	// Structs
	// Selector defines which objects this policy applies to
	Selector PolicySelector `json:"selector" yaml:"selector"`

	// AntiThrash prevents oscillation between tiers
	AntiThrash AntiThrashConfig `json:"antiThrash,omitempty" yaml:"antiThrash,omitempty"`

	// Schedule defines when the policy can execute
	Schedule ScheduleConfig `json:"schedule,omitempty" yaml:"schedule,omitempty"`

	// RateLimit controls execution rate
	RateLimit RateLimitConfig `json:"rateLimit,omitempty" yaml:"rateLimit,omitempty"`

	// Distributed execution configuration
	Distributed DistributedConfig `json:"distributed,omitempty" yaml:"distributed,omitempty"`

	// Scope defines where the policy applies
	Scope PolicyScope `json:"scope" yaml:"scope"`

	// Strings
	// ID is the unique identifier
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable name
	Name string `json:"name" yaml:"name"`

	// Description explains the policy purpose
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	LastRunNode string `json:"lastRunNode,omitempty" yaml:"lastRunNode,omitempty"`

	// Type of policy (scheduled, realtime, threshold, s3_lifecycle)
	Type PolicyType `json:"type" yaml:"type"`

	// 4-byte fields (int)
	// Priority determines evaluation order (lower = higher priority)
	Priority int `json:"priority" yaml:"priority"`

	// Version for optimistic locking
	Version int `json:"version" yaml:"version"`

	// 1-byte fields (bool)
	// Enabled determines if the policy is active
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// PolicySelector defines criteria for selecting objects.
type PolicySelector struct {
	// 8-byte fields (maps, slices, int64)
	// Tags to match (all must match)
	Tags map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`

	// Buckets to match (supports wildcards)
	Buckets []string `json:"buckets,omitempty" yaml:"buckets,omitempty"`

	// Prefix patterns to match
	Prefixes []string `json:"prefixes,omitempty" yaml:"prefixes,omitempty"`

	// Suffix patterns to match
	Suffixes []string `json:"suffixes,omitempty" yaml:"suffixes,omitempty"`

	// Content types (supports wildcards)
	ContentTypes []string `json:"contentTypes,omitempty" yaml:"contentTypes,omitempty"`

	// CurrentTiers filters objects already in certain tiers
	CurrentTiers []TierType `json:"currentTiers,omitempty" yaml:"currentTiers,omitempty"`

	// ExcludeTiers excludes objects in certain tiers
	ExcludeTiers []TierType `json:"excludeTiers,omitempty" yaml:"excludeTiers,omitempty"`

	// StorageClasses filters by S3 storage class
	StorageClasses []StorageClass `json:"storageClasses,omitempty" yaml:"storageClasses,omitempty"`

	// Size constraints
	MinSize int64 `json:"minSize,omitempty" yaml:"minSize,omitempty"`
	MaxSize int64 `json:"maxSize,omitempty" yaml:"maxSize,omitempty"`
}

// PolicyTrigger defines when a policy evaluates.
type PolicyTrigger struct {
	// 8-byte fields (pointers)
	// Age-based trigger configuration
	Age *AgeTrigger `json:"age,omitempty" yaml:"age,omitempty"`

	// Access-based trigger configuration
	Access *AccessTrigger `json:"access,omitempty" yaml:"access,omitempty"`

	// Capacity-based trigger configuration
	Capacity *CapacityTrigger `json:"capacity,omitempty" yaml:"capacity,omitempty"`

	// Frequency-based trigger configuration
	Frequency *FrequencyTrigger `json:"frequency,omitempty" yaml:"frequency,omitempty"`

	// Cron-based trigger configuration
	Cron *CronTrigger `json:"cron,omitempty" yaml:"cron,omitempty"`

	// Type of trigger
	Type TriggerType `json:"type" yaml:"type"`
}

// AgeTrigger triggers based on object age.
type AgeTrigger struct {
	// DaysSinceCreation triggers after N days since creation
	DaysSinceCreation int `json:"daysSinceCreation,omitempty" yaml:"daysSinceCreation,omitempty"`

	// DaysSinceModification triggers after N days since modification
	DaysSinceModification int `json:"daysSinceModification,omitempty" yaml:"daysSinceModification,omitempty"`

	// DaysSinceAccess triggers after N days since last access
	DaysSinceAccess int `json:"daysSinceAccess,omitempty" yaml:"daysSinceAccess,omitempty"`

	// HoursSinceAccess triggers after N hours since last access (for real-time)
	HoursSinceAccess int `json:"hoursSinceAccess,omitempty" yaml:"hoursSinceAccess,omitempty"`
}

// AccessTrigger triggers based on access patterns.
type AccessTrigger struct {
	// Operation type to track (GET, HEAD, etc.)
	Operation string `json:"operation,omitempty" yaml:"operation,omitempty"`

	// Direction for movement (up = promote on access, down = demote on lack of access)
	Direction TierDirection `json:"direction" yaml:"direction"`

	// CountThreshold is the access count threshold
	CountThreshold int `json:"countThreshold,omitempty" yaml:"countThreshold,omitempty"`

	// PeriodMinutes is the period to count accesses over
	PeriodMinutes int `json:"periodMinutes,omitempty" yaml:"periodMinutes,omitempty"`

	// PromoteOnAnyRead promotes to hot tier on any read
	PromoteOnAnyRead bool `json:"promoteOnAnyRead,omitempty" yaml:"promoteOnAnyRead,omitempty"`

	// PromoteToCache loads into cache on read
	PromoteToCache bool `json:"promoteToCache,omitempty" yaml:"promoteToCache,omitempty"`
}

// CapacityTrigger triggers based on tier capacity.
type CapacityTrigger struct {
	// Tier to monitor
	Tier TierType `json:"tier" yaml:"tier"`

	// HighWatermark percentage (e.g., 80 = 80%)
	HighWatermark float64 `json:"highWatermark" yaml:"highWatermark"`

	// LowWatermark percentage for stop condition
	LowWatermark float64 `json:"lowWatermark" yaml:"lowWatermark"`

	// BytesThreshold absolute bytes threshold
	BytesThreshold int64 `json:"bytesThreshold,omitempty" yaml:"bytesThreshold,omitempty"`

	// ObjectCountThreshold absolute object count threshold
	ObjectCountThreshold int64 `json:"objectCountThreshold,omitempty" yaml:"objectCountThreshold,omitempty"`
}

// FrequencyTrigger triggers based on access frequency patterns.
type FrequencyTrigger struct {
	// 8-byte fields (float64)
	// MinAccessesPerDay for hot classification
	MinAccessesPerDay float64 `json:"minAccessesPerDay,omitempty" yaml:"minAccessesPerDay,omitempty"`

	// MaxAccessesPerDay for cold classification
	MaxAccessesPerDay float64 `json:"maxAccessesPerDay,omitempty" yaml:"maxAccessesPerDay,omitempty"`

	// Strings
	// Pattern detection (e.g., "declining", "increasing", "stable")
	Pattern string `json:"pattern,omitempty" yaml:"pattern,omitempty"`

	// 4-byte fields (int)
	// SlidingWindowDays for frequency calculation
	SlidingWindowDays int `json:"slidingWindowDays,omitempty" yaml:"slidingWindowDays,omitempty"`
}

// CronTrigger triggers on a cron schedule.
type CronTrigger struct {
	// Expression is a cron expression (e.g., "0 2 * * *" for 2 AM daily)
	Expression string `json:"expression" yaml:"expression"`

	// Timezone for schedule evaluation
	Timezone string `json:"timezone,omitempty" yaml:"timezone,omitempty"`
}

// PolicyAction defines what happens when conditions are met.
type PolicyAction struct {
	// 8-byte fields (pointers)
	// Transition configuration
	Transition *TransitionActionConfig `json:"transition,omitempty" yaml:"transition,omitempty"`

	// Delete configuration
	Delete *DeleteActionConfig `json:"delete,omitempty" yaml:"delete,omitempty"`

	// Replicate configuration
	Replicate *ReplicateActionConfig `json:"replicate,omitempty" yaml:"replicate,omitempty"`

	// Notify configuration
	Notify *NotifyActionConfig `json:"notify,omitempty" yaml:"notify,omitempty"`

	// Type of action
	Type PolicyActionType `json:"type" yaml:"type"`

	// 1-byte fields (bool)
	// StopProcessing prevents other policies from executing
	StopProcessing bool `json:"stopProcessing,omitempty" yaml:"stopProcessing,omitempty"`
}

// PolicyActionType defines types of policy actions.
type PolicyActionType string

const (
	// ActionTransition moves object to a different tier.
	ActionTransition PolicyActionType = "transition"
	// ActionDelete removes the object.
	ActionDelete PolicyActionType = "delete"
	// ActionReplicate copies to another location.
	ActionReplicate PolicyActionType = "replicate"
	// ActionNotify sends a notification.
	ActionNotify PolicyActionType = "notify"
)

// TransitionActionConfig configures tier transitions.
type TransitionActionConfig struct {
	// TargetTier is the destination tier
	TargetTier TierType `json:"targetTier" yaml:"targetTier"`

	// TargetStorageClass is the S3 storage class
	TargetStorageClass StorageClass `json:"targetStorageClass,omitempty" yaml:"targetStorageClass,omitempty"`

	// PreserveCopy keeps original after transition
	PreserveCopy bool `json:"preserveCopy,omitempty" yaml:"preserveCopy,omitempty"`

	// Compression enables compression during transition
	Compression bool `json:"compression,omitempty" yaml:"compression,omitempty"`

	// CompressionAlgorithm specifies compression type
	CompressionAlgorithm string `json:"compressionAlgorithm,omitempty" yaml:"compressionAlgorithm,omitempty"`
}

// DeleteActionConfig configures object deletion.
type DeleteActionConfig struct {
	// DaysAfterTransition deletes N days after transitioning to a tier
	DaysAfterTransition int `json:"daysAfterTransition,omitempty" yaml:"daysAfterTransition,omitempty"`

	// ExpireDeleteMarkers for versioned objects
	ExpireDeleteMarkers bool `json:"expireDeleteMarkers,omitempty" yaml:"expireDeleteMarkers,omitempty"`

	// NonCurrentVersions configuration
	NonCurrentVersionDays int `json:"nonCurrentVersionDays,omitempty" yaml:"nonCurrentVersionDays,omitempty"`
}

// ReplicateActionConfig configures replication.
type ReplicateActionConfig struct {
	// Destination for replication
	Destination string `json:"destination" yaml:"destination"`

	// StorageClass at destination
	StorageClass StorageClass `json:"storageClass,omitempty" yaml:"storageClass,omitempty"`
}

// NotifyActionConfig configures notifications.
type NotifyActionConfig struct {
	// Endpoint URL for webhook
	Endpoint string `json:"endpoint" yaml:"endpoint"`

	// Events to notify on
	Events []string `json:"events,omitempty" yaml:"events,omitempty"`
}

// AntiThrashConfig prevents tier oscillation.
type AntiThrashConfig struct {
	// Enabled turns on anti-thrash protection
	Enabled bool `json:"enabled" yaml:"enabled"`

	// MinTimeInTier before allowing transition (duration string)
	MinTimeInTier string `json:"minTimeInTier,omitempty" yaml:"minTimeInTier,omitempty"`

	// CooldownAfterTransition before next transition (duration string)
	CooldownAfterTransition string `json:"cooldownAfterTransition,omitempty" yaml:"cooldownAfterTransition,omitempty"`

	// MaxTransitionsPerDay limits daily transitions per object
	MaxTransitionsPerDay int `json:"maxTransitionsPerDay,omitempty" yaml:"maxTransitionsPerDay,omitempty"`

	// StickinessWeight increases resistance to movement
	StickinessWeight float64 `json:"stickinessWeight,omitempty" yaml:"stickinessWeight,omitempty"`

	// RequireConsecutiveEvaluations before acting
	RequireConsecutiveEvaluations int `json:"requireConsecutiveEvaluations,omitempty" yaml:"requireConsecutiveEvaluations,omitempty"`
}

// GetMinTimeInTier returns the parsed duration.
func (c *AntiThrashConfig) GetMinTimeInTier() time.Duration {
	if c.MinTimeInTier == "" {
		return 0
	}

	d, err := time.ParseDuration(c.MinTimeInTier)
	if err != nil {
		return 0
	}

	return d
}

// GetCooldownAfterTransition returns the parsed duration.
func (c *AntiThrashConfig) GetCooldownAfterTransition() time.Duration {
	if c.CooldownAfterTransition == "" {
		return 0
	}

	d, err := time.ParseDuration(c.CooldownAfterTransition)
	if err != nil {
		return 0
	}

	return d
}

// ScheduleConfig defines when policies can execute.
type ScheduleConfig struct {
	// Enabled turns on scheduling
	Enabled bool `json:"enabled" yaml:"enabled"`

	// MaintenanceWindows define allowed execution times
	MaintenanceWindows []MaintenanceWindow `json:"maintenanceWindows,omitempty" yaml:"maintenanceWindows,omitempty"`

	// BlackoutWindows define forbidden execution times
	BlackoutWindows []MaintenanceWindow `json:"blackoutWindows,omitempty" yaml:"blackoutWindows,omitempty"`

	// Timezone for window evaluation
	Timezone string `json:"timezone,omitempty" yaml:"timezone,omitempty"`
}

// MaintenanceWindow defines a time window.
type MaintenanceWindow struct {
	// Name of the window
	Name string `json:"name" yaml:"name"`

	// DaysOfWeek (0=Sunday, 6=Saturday)
	DaysOfWeek []int `json:"daysOfWeek,omitempty" yaml:"daysOfWeek,omitempty"`

	// StartTime in HH:MM format
	StartTime string `json:"startTime" yaml:"startTime"`

	// EndTime in HH:MM format
	EndTime string `json:"endTime" yaml:"endTime"`
}

// RateLimitConfig controls execution rate.
type RateLimitConfig struct {
	// Enabled turns on rate limiting
	Enabled bool `json:"enabled" yaml:"enabled"`

	// MaxObjectsPerSecond limits throughput
	MaxObjectsPerSecond int `json:"maxObjectsPerSecond,omitempty" yaml:"maxObjectsPerSecond,omitempty"`

	// MaxBytesPerSecond limits bandwidth
	MaxBytesPerSecond int64 `json:"maxBytesPerSecond,omitempty" yaml:"maxBytesPerSecond,omitempty"`

	// MaxConcurrent limits parallel operations
	MaxConcurrent int `json:"maxConcurrent,omitempty" yaml:"maxConcurrent,omitempty"`

	// BurstSize allows short bursts
	BurstSize int `json:"burstSize,omitempty" yaml:"burstSize,omitempty"`
}

// DistributedConfig configures distributed execution.
type DistributedConfig struct {
	// Enabled turns on distributed execution
	Enabled bool `json:"enabled" yaml:"enabled"`

	// ShardByBucket uses consistent hashing per bucket
	ShardByBucket bool `json:"shardByBucket,omitempty" yaml:"shardByBucket,omitempty"`

	// RequireLeaderElection for certain policies
	RequireLeaderElection bool `json:"requireLeaderElection,omitempty" yaml:"requireLeaderElection,omitempty"`

	// CoordinationKey for distributed locking
	CoordinationKey string `json:"coordinationKey,omitempty" yaml:"coordinationKey,omitempty"`
}

// PolicyStats tracks policy execution statistics.
type PolicyStats struct {
	PolicyID               string        `json:"policyId"`
	LastExecuted           time.Time     `json:"lastExecuted"`
	LastDuration           time.Duration `json:"lastDuration"`
	ObjectsEvaluated       int64         `json:"objectsEvaluated"`
	ObjectsTransitioned    int64         `json:"objectsTransitioned"`
	BytesTransitioned      int64         `json:"bytesTransitioned"`
	Errors                 int64         `json:"errors"`
	LastError              string        `json:"lastError,omitempty"`
	ConsecutiveSuccesses   int           `json:"consecutiveSuccesses"`
	ConsecutiveFailures    int           `json:"consecutiveFailures"`
	AverageExecutionTimeMs float64       `json:"averageExecutionTimeMs"`
	TotalExecutions        int64         `json:"totalExecutions"`
}

// ObjectAccessStats tracks object access patterns.
type ObjectAccessStats struct {
	Bucket       string    `json:"bucket"`
	Key          string    `json:"key"`
	CurrentTier  TierType  `json:"currentTier"`
	LastAccessed time.Time `json:"lastAccessed"`
	LastModified time.Time `json:"lastModified"`
	AccessCount  int64     `json:"accessCount"`

	// Time-series access data
	AccessHistory []AccessRecord `json:"accessHistory,omitempty"`

	// Computed metrics
	AccessesLast24h    int     `json:"accessesLast24h"`
	AccessesLast7d     int     `json:"accessesLast7d"`
	AccessesLast30d    int     `json:"accessesLast30d"`
	AverageAccessesDay float64 `json:"averageAccessesDay"`
	AccessTrend        string  `json:"accessTrend"` // "increasing", "declining", "stable"

	// Transition tracking
	LastTransitionAt   *time.Time `json:"lastTransitionAt,omitempty"`
	LastTransitionFrom TierType   `json:"lastTransitionFrom,omitempty"`
	TransitionCount    int        `json:"transitionCount"`

	// Anti-thrash state
	ConsecutiveEvaluations int `json:"consecutiveEvaluations"`
}

// AccessRecord represents a single access event.
type AccessRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Operation string    `json:"operation"` // GET, HEAD, etc.
	BytesRead int64     `json:"bytesRead,omitempty"`
}

// Validate validates the policy configuration.
func (p *AdvancedPolicy) Validate() error {
	if p.ID == "" {
		return errors.New("policy ID is required")
	}

	if p.Name == "" {
		return errors.New("policy name is required")
	}

	if p.Type == "" {
		return errors.New("policy type is required")
	}

	// Validate triggers
	if len(p.Triggers) == 0 {
		return errors.New("at least one trigger is required")
	}

	for i, trigger := range p.Triggers {
		err := trigger.Validate()
		if err != nil {
			return fmt.Errorf("trigger %d: %w", i, err)
		}
	}

	// Validate actions
	if len(p.Actions) == 0 {
		return errors.New("at least one action is required")
	}

	for i, action := range p.Actions {
		err := action.Validate()
		if err != nil {
			return fmt.Errorf("action %d: %w", i, err)
		}
	}

	return nil
}

// Validate validates a trigger.
func (t *PolicyTrigger) Validate() error {
	if t.Type == "" {
		return errors.New("trigger type is required")
	}

	switch t.Type {
	case TriggerTypeAge:
		if t.Age == nil {
			return errors.New("age trigger requires age configuration")
		}
	case TriggerTypeAccess:
		if t.Access == nil {
			return errors.New("access trigger requires access configuration")
		}
	case TriggerTypeCapacity:
		if t.Capacity == nil {
			return errors.New("capacity trigger requires capacity configuration")
		}
	case TriggerTypeFrequency:
		if t.Frequency == nil {
			return errors.New("frequency trigger requires frequency configuration")
		}
	case TriggerTypeCron:
		if t.Cron == nil {
			return errors.New("cron trigger requires cron configuration")
		}
	}

	return nil
}

// Validate validates an action.
func (a *PolicyAction) Validate() error {
	if a.Type == "" {
		return errors.New("action type is required")
	}

	switch a.Type {
	case ActionTransition:
		if a.Transition == nil {
			return errors.New("transition action requires transition configuration")
		}

		if a.Transition.TargetTier == "" {
			return errors.New("transition target tier is required")
		}
	case ActionDelete:
		// Delete action doesn't require additional configuration
	case ActionReplicate:
		if a.Replicate == nil || a.Replicate.Destination == "" {
			return errors.New("replicate action requires destination")
		}
	case ActionNotify:
		if a.Notify == nil || a.Notify.Endpoint == "" {
			return errors.New("notify action requires endpoint")
		}
	}

	return nil
}

// ToJSON serializes policy to JSON.
func (p *AdvancedPolicy) ToJSON() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

// FromJSON deserializes policy from JSON.
func (p *AdvancedPolicy) FromJSON(data []byte) error {
	return json.Unmarshal(data, p)
}
