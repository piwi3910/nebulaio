package tiering

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdvancedPolicyValidation(t *testing.T) {
	tests := []struct {
		name        string
		policy      AdvancedPolicy
		expectError bool
	}{
		{
			name: "valid policy",
			policy: AdvancedPolicy{
				ID:      "test-policy-1",
				Name:    "Test Policy",
				Type:    PolicyTypeScheduled,
				Scope:   PolicyScopeGlobal,
				Enabled: true,
				Triggers: []PolicyTrigger{
					{
						Type: TriggerTypeAge,
						Age: &AgeTrigger{
							DaysSinceCreation: 30,
						},
					},
				},
				Actions: []PolicyAction{
					{
						Type: ActionTransition,
						Transition: &TransitionActionConfig{
							TargetTier: TierCold,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "missing ID",
			policy: AdvancedPolicy{
				Name: "Test Policy",
				Type: PolicyTypeScheduled,
				Triggers: []PolicyTrigger{
					{Type: TriggerTypeAge, Age: &AgeTrigger{DaysSinceCreation: 30}},
				},
				Actions: []PolicyAction{
					{Type: ActionTransition, Transition: &TransitionActionConfig{TargetTier: TierCold}},
				},
			},
			expectError: true,
		},
		{
			name: "missing name",
			policy: AdvancedPolicy{
				ID:   "test-1",
				Type: PolicyTypeScheduled,
				Triggers: []PolicyTrigger{
					{Type: TriggerTypeAge, Age: &AgeTrigger{DaysSinceCreation: 30}},
				},
				Actions: []PolicyAction{
					{Type: ActionTransition, Transition: &TransitionActionConfig{TargetTier: TierCold}},
				},
			},
			expectError: true,
		},
		{
			name: "missing triggers",
			policy: AdvancedPolicy{
				ID:      "test-1",
				Name:    "Test",
				Type:    PolicyTypeScheduled,
				Actions: []PolicyAction{{Type: ActionTransition, Transition: &TransitionActionConfig{TargetTier: TierCold}}},
			},
			expectError: true,
		},
		{
			name: "missing actions",
			policy: AdvancedPolicy{
				ID:       "test-1",
				Name:     "Test",
				Type:     PolicyTypeScheduled,
				Triggers: []PolicyTrigger{{Type: TriggerTypeAge, Age: &AgeTrigger{DaysSinceCreation: 30}}},
			},
			expectError: true,
		},
		{
			name: "invalid trigger - missing age config",
			policy: AdvancedPolicy{
				ID:       "test-1",
				Name:     "Test",
				Type:     PolicyTypeScheduled,
				Triggers: []PolicyTrigger{{Type: TriggerTypeAge}},
				Actions:  []PolicyAction{{Type: ActionTransition, Transition: &TransitionActionConfig{TargetTier: TierCold}}},
			},
			expectError: true,
		},
		{
			name: "invalid action - missing transition config",
			policy: AdvancedPolicy{
				ID:       "test-1",
				Name:     "Test",
				Type:     PolicyTypeScheduled,
				Triggers: []PolicyTrigger{{Type: TriggerTypeAge, Age: &AgeTrigger{DaysSinceCreation: 30}}},
				Actions:  []PolicyAction{{Type: ActionTransition}},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHybridPolicyStore(t *testing.T) {
	ctx := context.Background()

	store, err := NewHybridPolicyStore(HybridPolicyStoreConfig{
		MetadataStore: NewInMemoryMetadataStore(),
		CacheTTL:      1 * time.Minute,
		SyncInterval:  1 * time.Second,
	})
	require.NoError(t, err)

	defer store.Close()

	// Create policy
	policy := &AdvancedPolicy{
		ID:      "test-policy-1",
		Name:    "Test Policy",
		Type:    PolicyTypeScheduled,
		Scope:   PolicyScopeGlobal,
		Enabled: true,
		Triggers: []PolicyTrigger{
			{Type: TriggerTypeAge, Age: &AgeTrigger{DaysSinceCreation: 30}},
		},
		Actions: []PolicyAction{
			{Type: ActionTransition, Transition: &TransitionActionConfig{TargetTier: TierCold}},
		},
	}

	err = store.Create(ctx, policy)
	require.NoError(t, err)

	// Get policy
	retrieved, err := store.Get(ctx, "test-policy-1")
	require.NoError(t, err)
	assert.Equal(t, "Test Policy", retrieved.Name)
	assert.Equal(t, 1, retrieved.Version)

	// List policies
	policies, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, policies, 1)

	// Update policy
	policy.Name = "Updated Policy"
	err = store.Update(ctx, policy)
	require.NoError(t, err)

	retrieved, err = store.Get(ctx, "test-policy-1")
	require.NoError(t, err)
	assert.Equal(t, "Updated Policy", retrieved.Name)
	assert.Equal(t, 2, retrieved.Version)

	// Delete policy
	err = store.Delete(ctx, "test-policy-1")
	require.NoError(t, err)

	_, err = store.Get(ctx, "test-policy-1")
	assert.Error(t, err)
}

func TestPolicyStoreVersionConflict(t *testing.T) {
	ctx := context.Background()

	store, err := NewHybridPolicyStore(HybridPolicyStoreConfig{
		MetadataStore: NewInMemoryMetadataStore(),
	})
	require.NoError(t, err)

	defer store.Close()

	policy := &AdvancedPolicy{
		ID:       "test-1",
		Name:     "Test",
		Type:     PolicyTypeScheduled,
		Triggers: []PolicyTrigger{{Type: TriggerTypeAge, Age: &AgeTrigger{DaysSinceCreation: 30}}},
		Actions:  []PolicyAction{{Type: ActionTransition, Transition: &TransitionActionConfig{TargetTier: TierCold}}},
	}

	err = store.Create(ctx, policy)
	require.NoError(t, err)

	// Get the current policy to see its version
	current, err := store.Get(ctx, "test-1")
	require.NoError(t, err)
	assert.Equal(t, 1, current.Version)

	// Create a copy with wrong version
	stalePolicy := *current
	stalePolicy.Version = 0 // Wrong version
	stalePolicy.Name = "Updated Name"

	// Try to update with wrong version
	err = store.Update(ctx, &stalePolicy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version conflict")
}

func TestAccessTracker(t *testing.T) {
	tracker := NewAccessTracker(DefaultAccessTrackerConfig())
	defer tracker.Close()

	ctx := context.Background()

	// Record accesses
	tracker.RecordAccess(ctx, "bucket", "key1", "GET", 1024)
	tracker.RecordAccess(ctx, "bucket", "key1", "GET", 1024)
	tracker.RecordAccess(ctx, "bucket", "key1", "GET", 1024)

	// Get stats
	stats, err := tracker.GetStats(ctx, "bucket", "key1")
	require.NoError(t, err)
	require.NotNil(t, stats)

	assert.Equal(t, int64(3), stats.AccessCount)
	assert.Equal(t, 3, stats.AccessesLast24h)
	assert.Len(t, stats.AccessHistory, 3)
}

func TestAccessTrackerTrends(t *testing.T) {
	cfg := DefaultAccessTrackerConfig()
	cfg.MaxHistorySize = 100

	tracker := NewAccessTracker(cfg)
	defer tracker.Close()

	ctx := context.Background()

	// Simulate access pattern: more accesses recently
	now := time.Now()

	// Add old accesses (7-14 days ago) - fewer
	for range 5 {
		tracker.RecordAccess(ctx, "bucket", "trending", "GET", 1024)
	}

	// Manually adjust timestamps for testing
	tracker.statsMu.Lock()

	stats := tracker.stats["bucket/trending"]
	if stats != nil {
		for i := range len(stats.AccessHistory) {
			stats.AccessHistory[i].Timestamp = now.Add(-10 * 24 * time.Hour)
		}
	}

	tracker.statsMu.Unlock()

	// Add recent accesses (last 7 days) - more
	for range 15 {
		tracker.RecordAccess(ctx, "bucket", "trending", "GET", 1024)
	}

	stats, err := tracker.GetStats(ctx, "bucket", "trending")
	require.NoError(t, err)

	// With more recent accesses, trend should be "increasing"
	assert.Equal(t, "increasing", stats.AccessTrend)
}

func TestAntiThrashManager(t *testing.T) {
	manager := NewAntiThrashManager()

	policy := &AdvancedPolicy{
		ID:   "test-1",
		Name: "Test",
		AntiThrash: AntiThrashConfig{
			Enabled:                 true,
			CooldownAfterTransition: "1h",
			MaxTransitionsPerDay:    2,
		},
	}

	// First transition should be allowed
	assert.True(t, manager.CanTransition("bucket", "key", policy))

	// Record transition
	manager.RecordTransition("bucket", "key")

	// Second transition should be blocked (within cooldown)
	assert.False(t, manager.CanTransition("bucket", "key", policy))

	// Without cooldown, check max transitions
	policy.AntiThrash.CooldownAfterTransition = ""

	manager.RecordTransition("bucket", "key")
	assert.False(t, manager.CanTransition("bucket", "key", policy)) // 2 transitions, at max
}

func TestRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterConfig{
		DefaultObjectsPerSecond: 10,
		DefaultBurstSize:        5,
	})

	// Should allow burst
	for range 5 {
		assert.True(t, limiter.Allow("policy-1"))
	}

	// Should deny after burst exhausted
	assert.False(t, limiter.Allow("policy-1"))
}

func TestS3LifecycleAdapter(t *testing.T) {
	adapter := NewS3LifecycleAdapter()

	config := &S3LifecycleConfiguration{
		Rules: []S3LifecycleRule{
			{
				ID:     "rule-1",
				Status: "Enabled",
				Filter: &S3LifecycleFilter{
					Prefix: "logs/",
				},
				Transitions: []S3LifecycleTransition{
					{
						Days:         30,
						StorageClass: string(StorageClassStandardIA),
					},
					{
						Days:         90,
						StorageClass: string(StorageClassGlacier),
					},
				},
				Expiration: &S3LifecycleExpiration{
					Days: 365,
				},
			},
		},
	}

	// Convert to NebulaIO policies
	policies, err := adapter.ConvertFromS3("my-bucket", config)
	require.NoError(t, err)
	require.Len(t, policies, 1)

	policy := policies[0]
	assert.Equal(t, PolicyTypeS3Lifecycle, policy.Type)
	assert.Equal(t, PolicyScopeBucket, policy.Scope)
	assert.True(t, policy.Enabled)
	assert.Contains(t, policy.Selector.Buckets, "my-bucket")
	assert.Contains(t, policy.Selector.Prefixes, "logs/")

	// Should have triggers and actions for each transition + expiration
	assert.GreaterOrEqual(t, len(policy.Triggers), 3)
	assert.GreaterOrEqual(t, len(policy.Actions), 3)

	// Convert back to S3
	s3Config, err := adapter.ConvertToS3("my-bucket", policies)
	require.NoError(t, err)
	require.Len(t, s3Config.Rules, 1)
}

func TestS3LifecycleXMLParsing(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<LifecycleConfiguration>
    <Rule>
        <ID>archive-logs</ID>
        <Status>Enabled</Status>
        <Filter>
            <Prefix>logs/</Prefix>
        </Filter>
        <Transition>
            <Days>30</Days>
            <StorageClass>STANDARD_IA</StorageClass>
        </Transition>
        <Expiration>
            <Days>365</Days>
        </Expiration>
    </Rule>
</LifecycleConfiguration>`)

	config, err := ParseS3LifecycleXML(xmlData)
	require.NoError(t, err)
	require.Len(t, config.Rules, 1)

	rule := config.Rules[0]
	assert.Equal(t, "archive-logs", rule.ID)
	assert.Equal(t, "Enabled", rule.Status)
	assert.NotNil(t, rule.Filter)
	assert.Equal(t, "logs/", rule.Filter.Prefix)
	assert.Len(t, rule.Transitions, 1)
	assert.Equal(t, 30, rule.Transitions[0].Days)
	assert.NotNil(t, rule.Expiration)
	assert.Equal(t, 365, rule.Expiration.Days)

	// Convert back to XML
	outputXml, err := config.ToXML()
	require.NoError(t, err)
	assert.Contains(t, string(outputXml), "archive-logs")
}

func TestPolicySelectorMatching(t *testing.T) {
	tests := []struct {
		obj      ObjectMetadata
		stats    *ObjectAccessStats
		name     string
		selector PolicySelector
		matches  bool
	}{
		{
			name: "bucket match",
			selector: PolicySelector{
				Buckets: []string{"my-bucket"},
			},
			obj:     ObjectMetadata{Bucket: "my-bucket", Key: "test.txt"},
			matches: true,
		},
		{
			name: "bucket wildcard match",
			selector: PolicySelector{
				Buckets: []string{"my-*"},
			},
			obj:     ObjectMetadata{Bucket: "my-bucket", Key: "test.txt"},
			matches: true,
		},
		{
			name: "bucket no match",
			selector: PolicySelector{
				Buckets: []string{"other-bucket"},
			},
			obj:     ObjectMetadata{Bucket: "my-bucket", Key: "test.txt"},
			matches: false,
		},
		{
			name: "prefix match",
			selector: PolicySelector{
				Prefixes: []string{"logs/"},
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "logs/access.log"},
			matches: true,
		},
		{
			name: "prefix no match",
			selector: PolicySelector{
				Prefixes: []string{"logs/"},
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "data/file.txt"},
			matches: false,
		},
		{
			name: "suffix match",
			selector: PolicySelector{
				Suffixes: []string{".log"},
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "access.log"},
			matches: true,
		},
		{
			name: "size range match",
			selector: PolicySelector{
				MinSize: 1000,
				MaxSize: 10000,
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "file.txt", Size: 5000},
			matches: true,
		},
		{
			name: "size too small",
			selector: PolicySelector{
				MinSize: 1000,
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "file.txt", Size: 500},
			matches: false,
		},
		{
			name: "content type match",
			selector: PolicySelector{
				ContentTypes: []string{"image/*"},
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "photo.jpg", ContentType: "image/jpeg"},
			matches: true,
		},
		{
			name: "current tier match",
			selector: PolicySelector{
				CurrentTiers: []TierType{TierHot},
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "file.txt", CurrentTier: TierHot},
			matches: true,
		},
		{
			name: "exclude tier",
			selector: PolicySelector{
				ExcludeTiers: []TierType{TierCold},
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "file.txt", CurrentTier: TierCold},
			matches: false,
		},
		{
			name: "tag match",
			selector: PolicySelector{
				Tags: map[string]string{"env": "prod"},
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "file.txt", Tags: map[string]string{"env": "prod"}},
			matches: true,
		},
		{
			name: "tag mismatch",
			selector: PolicySelector{
				Tags: map[string]string{"env": "prod"},
			},
			obj:     ObjectMetadata{Bucket: "bucket", Key: "file.txt", Tags: map[string]string{"env": "dev"}},
			matches: false,
		},
	}

	// Create a minimal policy engine for testing
	store, _ := NewHybridPolicyStore(HybridPolicyStoreConfig{
		MetadataStore: NewInMemoryMetadataStore(),
	})
	defer store.Close()

	engine := &PolicyEngine{
		store: store,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := tt.stats
			if stats == nil {
				stats = &ObjectAccessStats{}
			}

			result := engine.matchesSelector(tt.obj, stats, tt.selector)
			assert.Equal(t, tt.matches, result)
		})
	}
}

func TestScheduleWindow(t *testing.T) {
	engine := &PolicyEngine{}

	// Get current time details for testing
	now := time.Now()
	currentWeekday := int(now.Weekday())
	currentHour := now.Hour()
	currentMinute := now.Minute()

	tests := []struct {
		name     string
		schedule ScheduleConfig
		expected bool
	}{
		{
			name: "no schedule",
			schedule: ScheduleConfig{
				Enabled: false,
			},
			expected: true,
		},
		{
			name: "in maintenance window",
			schedule: ScheduleConfig{
				Enabled: true,
				MaintenanceWindows: []MaintenanceWindow{
					{
						Name:       "current",
						DaysOfWeek: []int{currentWeekday},
						StartTime:  formatTime(currentHour, 0),
						EndTime:    formatTime(currentHour+1, 0),
					},
				},
			},
			expected: true,
		},
		{
			name: "outside maintenance window",
			schedule: ScheduleConfig{
				Enabled: true,
				MaintenanceWindows: []MaintenanceWindow{
					{
						Name:       "other",
						DaysOfWeek: []int{(currentWeekday + 1) % 7},
						StartTime:  "02:00",
						EndTime:    "04:00",
					},
				},
			},
			expected: false,
		},
		{
			name: "in blackout window",
			schedule: ScheduleConfig{
				Enabled: true,
				BlackoutWindows: []MaintenanceWindow{
					{
						Name:       "current",
						DaysOfWeek: []int{currentWeekday},
						StartTime:  formatTime(currentHour, 0),
						EndTime:    formatTime(currentHour+1, 0),
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.isInScheduleWindow(tt.schedule)
			assert.Equal(t, tt.expected, result, "Expected %v for schedule window check", tt.expected)
		})
	}

	_ = currentMinute // Silence unused variable warning
}

func formatTime(hour, minute int) string {
	return time.Date(2000, 1, 1, hour, minute, 0, 0, time.UTC).Format("15:04")
}

func TestAntiThrashConfig(t *testing.T) {
	config := AntiThrashConfig{
		MinTimeInTier:           "1h",
		CooldownAfterTransition: "30m",
	}

	assert.Equal(t, 1*time.Hour, config.GetMinTimeInTier())
	assert.Equal(t, 30*time.Minute, config.GetCooldownAfterTransition())

	// Invalid duration
	config.MinTimeInTier = "invalid"
	assert.Equal(t, time.Duration(0), config.GetMinTimeInTier())
}

func TestPolicyJSON(t *testing.T) {
	policy := &AdvancedPolicy{
		ID:      "test-1",
		Name:    "Test Policy",
		Type:    PolicyTypeScheduled,
		Scope:   PolicyScopeGlobal,
		Enabled: true,
		Selector: PolicySelector{
			Buckets:  []string{"my-bucket"},
			Prefixes: []string{"logs/"},
		},
		Triggers: []PolicyTrigger{
			{
				Type: TriggerTypeAge,
				Age:  &AgeTrigger{DaysSinceAccess: 30},
			},
		},
		Actions: []PolicyAction{
			{
				Type:       ActionTransition,
				Transition: &TransitionActionConfig{TargetTier: TierCold},
			},
		},
		AntiThrash: AntiThrashConfig{
			Enabled:       true,
			MinTimeInTier: "24h",
		},
	}

	// Serialize
	data, err := policy.ToJSON()
	require.NoError(t, err)

	// Deserialize
	var parsed AdvancedPolicy

	err = parsed.FromJSON(data)
	require.NoError(t, err)

	assert.Equal(t, policy.ID, parsed.ID)
	assert.Equal(t, policy.Name, parsed.Name)
	assert.Equal(t, policy.Enabled, parsed.Enabled)
	assert.Len(t, parsed.Triggers, len(policy.Triggers))
	assert.Len(t, parsed.Actions, len(policy.Actions))
}
