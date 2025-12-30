package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLifecycleConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<LifecycleConfiguration>
	<Rule>
		<ID>expire-logs</ID>
		<Status>Enabled</Status>
		<Filter>
			<Prefix>logs/</Prefix>
		</Filter>
		<Expiration>
			<Days>30</Days>
		</Expiration>
	</Rule>
</LifecycleConfiguration>`

		config, err := ParseLifecycleConfig([]byte(xmlData))
		require.NoError(t, err)
		require.NotNil(t, config)
		assert.Len(t, config.Rules, 1)
		assert.Equal(t, "expire-logs", config.Rules[0].ID)
		assert.Equal(t, "Enabled", config.Rules[0].Status)
		assert.Equal(t, "logs/", config.Rules[0].Filter.Prefix)
		assert.Equal(t, 30, config.Rules[0].Expiration.Days)
	})

	t.Run("invalid XML", func(t *testing.T) {
		xmlData := `not valid xml`
		_, err := ParseLifecycleConfig([]byte(xmlData))
		assert.Error(t, err)
	})
}

func TestLifecycleConfigurationValidate(t *testing.T) {
	tests := []struct {
		config      *LifecycleConfiguration
		name        string
		errorMsg    string
		expectError bool
	}{
		{
			name: "valid config",
			config: &LifecycleConfiguration{
				Rules: []LifecycleRule{
					{
						ID:     "rule1",
						Status: "Enabled",
						Expiration: &Expiration{
							Days: 30,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name:        "no rules",
			config:      &LifecycleConfiguration{Rules: []LifecycleRule{}},
			expectError: true,
			errorMsg:    "at least one rule",
		},
		{
			name: "duplicate IDs",
			config: &LifecycleConfiguration{
				Rules: []LifecycleRule{
					{ID: "same-id", Status: "Enabled", Expiration: &Expiration{Days: 30}},
					{ID: "same-id", Status: "Enabled", Expiration: &Expiration{Days: 60}},
				},
			},
			expectError: true,
			errorMsg:    "duplicate rule ID",
		},
		{
			name: "invalid rule",
			config: &LifecycleConfiguration{
				Rules: []LifecycleRule{
					{ID: "rule1", Status: "Invalid"},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLifecycleRuleValidate(t *testing.T) {
	tests := []struct {
		rule        LifecycleRule
		name        string
		errorMsg    string
		expectError bool
	}{
		{
			name: "valid rule with expiration",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				Expiration: &Expiration{
					Days: 30,
				},
			},
			expectError: false,
		},
		{
			name: "valid rule with transition",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				Transition: []Transition{
					{Days: 30, StorageClass: "STANDARD_IA"},
				},
			},
			expectError: false,
		},
		{
			name: "ID too long",
			rule: LifecycleRule{
				ID:         string(make([]byte, 300)),
				Status:     "Enabled",
				Expiration: &Expiration{Days: 30},
			},
			expectError: true,
			errorMsg:    "255 characters",
		},
		{
			name: "invalid status",
			rule: LifecycleRule{
				ID:         "rule1",
				Status:     "Invalid",
				Expiration: &Expiration{Days: 30},
			},
			expectError: true,
			errorMsg:    "'Enabled' or 'Disabled'",
		},
		{
			name: "disabled status valid",
			rule: LifecycleRule{
				ID:         "rule1",
				Status:     "Disabled",
				Expiration: &Expiration{Days: 30},
			},
			expectError: false,
		},
		{
			name: "no action",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
			},
			expectError: true,
			errorMsg:    "at least one action",
		},
		{
			name: "abort multipart only",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				AbortIncompleteMultipartUpload: &AbortIncompleteMultipartUpload{
					DaysAfterInitiation: 7,
				},
			},
			expectError: false,
		},
		{
			name: "noncurrent version expiration",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				NoncurrentVersionExpiration: &NoncurrentVersionExpiration{
					NoncurrentDays: 30,
				},
			},
			expectError: false,
		},
		{
			name: "noncurrent version transition",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				NoncurrentVersionTransition: []NoncurrentVersionTransition{
					{NoncurrentDays: 30, StorageClass: "GLACIER"},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFilterValidate(t *testing.T) {
	tests := []struct {
		filter      Filter
		name        string
		errorMsg    string
		expectError bool
	}{
		{
			name:        "empty filter",
			filter:      Filter{},
			expectError: false,
		},
		{
			name:        "prefix only",
			filter:      Filter{Prefix: "logs/"},
			expectError: false,
		},
		{
			name:        "tag only",
			filter:      Filter{Tag: &Tag{Key: "env", Value: "prod"}},
			expectError: false,
		},
		{
			name:        "tag with empty key",
			filter:      Filter{Tag: &Tag{Key: "", Value: "prod"}},
			expectError: true,
			errorMsg:    "key cannot be empty",
		},
		{
			name: "And filter",
			filter: Filter{
				And: &And{
					Prefix: "logs/",
					Tags:   []Tag{{Key: "env", Value: "prod"}},
				},
			},
			expectError: false,
		},
		{
			name: "And filter with prefix",
			filter: Filter{
				Prefix: "logs/",
				And:    &And{Tags: []Tag{{Key: "env", Value: "prod"}}},
			},
			expectError: true,
			errorMsg:    "cannot use And with Prefix or Tag",
		},
		{
			name: "And filter with tag",
			filter: Filter{
				Tag: &Tag{Key: "env", Value: "prod"},
				And: &And{Prefix: "logs/"},
			},
			expectError: true,
			errorMsg:    "cannot use And with Prefix or Tag",
		},
		{
			name: "And filter empty",
			filter: Filter{
				And: &And{},
			},
			expectError: true,
			errorMsg:    "at least a prefix or one tag",
		},
		{
			name: "And filter with empty tag key",
			filter: Filter{
				And: &And{
					Tags: []Tag{{Key: "", Value: "prod"}},
				},
			},
			expectError: true,
			errorMsg:    "empty key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestExpirationValidate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		exp         Expiration
		name        string
		errorMsg    string
		expectError bool
	}{
		{
			name:        "valid days",
			exp:         Expiration{Days: 30},
			expectError: false,
		},
		{
			name:        "valid date",
			exp:         Expiration{Date: now.Add(24 * time.Hour)},
			expectError: false,
		},
		{
			name:        "valid delete marker",
			exp:         Expiration{ExpiredObjectDeleteMarker: true},
			expectError: false,
		},
		{
			name:        "both days and date",
			exp:         Expiration{Days: 30, Date: now},
			expectError: true,
			errorMsg:    "both Days and Date",
		},
		{
			name:        "delete marker with days",
			exp:         Expiration{Days: 30, ExpiredObjectDeleteMarker: true},
			expectError: true,
			errorMsg:    "cannot be used with Days or Date",
		},
		{
			name:        "nothing set",
			exp:         Expiration{},
			expectError: true,
			errorMsg:    "must have Days, Date, or ExpiredObjectDeleteMarker",
		},
		{
			name:        "zero days",
			exp:         Expiration{Days: 0},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.exp.Validate()
			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTransitionValidate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		transition  Transition
		name        string
		errorMsg    string
		expectError bool
	}{
		{
			name:        "valid with days",
			transition:  Transition{Days: 30, StorageClass: "STANDARD_IA"},
			expectError: false,
		},
		{
			name:        "valid with date",
			transition:  Transition{Date: now, StorageClass: "GLACIER"},
			expectError: false,
		},
		{
			name:        "both days and date",
			transition:  Transition{Days: 30, Date: now, StorageClass: "STANDARD_IA"},
			expectError: true,
			errorMsg:    "exactly one of Days or Date",
		},
		{
			name:        "neither days nor date",
			transition:  Transition{StorageClass: "STANDARD_IA"},
			expectError: true,
			errorMsg:    "exactly one of Days or Date",
		},
		{
			name:        "missing storage class",
			transition:  Transition{Days: 30},
			expectError: true,
			errorMsg:    "storage class is required",
		},
		{
			name:        "invalid storage class",
			transition:  Transition{Days: 30, StorageClass: "INVALID"},
			expectError: true,
			errorMsg:    "invalid storage class",
		},
		{
			name:        "ONEZONE_IA valid",
			transition:  Transition{Days: 30, StorageClass: "ONEZONE_IA"},
			expectError: false,
		},
		{
			name:        "INTELLIGENT_TIERING valid",
			transition:  Transition{Days: 30, StorageClass: "INTELLIGENT_TIERING"},
			expectError: false,
		},
		{
			name:        "GLACIER_IR valid",
			transition:  Transition{Days: 30, StorageClass: "GLACIER_IR"},
			expectError: false,
		},
		{
			name:        "DEEP_ARCHIVE valid",
			transition:  Transition{Days: 30, StorageClass: "DEEP_ARCHIVE"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.transition.Validate()
			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNoncurrentVersionExpirationValidate(t *testing.T) {
	tests := []struct {
		name        string
		nve         NoncurrentVersionExpiration
		expectError bool
	}{
		{
			name:        "valid days",
			nve:         NoncurrentVersionExpiration{NoncurrentDays: 30},
			expectError: false,
		},
		{
			name:        "valid versions",
			nve:         NoncurrentVersionExpiration{NewerNoncurrentVersions: 3},
			expectError: false,
		},
		{
			name:        "both set",
			nve:         NoncurrentVersionExpiration{NoncurrentDays: 30, NewerNoncurrentVersions: 3},
			expectError: false,
		},
		{
			name:        "neither set",
			nve:         NoncurrentVersionExpiration{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.nve.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNoncurrentVersionTransitionValidate(t *testing.T) {
	tests := []struct {
		name        string
		nvt         NoncurrentVersionTransition
		expectError bool
	}{
		{
			name:        "valid",
			nvt:         NoncurrentVersionTransition{NoncurrentDays: 30, StorageClass: "GLACIER"},
			expectError: false,
		},
		{
			name:        "zero days valid",
			nvt:         NoncurrentVersionTransition{NoncurrentDays: 0, StorageClass: "GLACIER"},
			expectError: false,
		},
		{
			name:        "missing storage class",
			nvt:         NoncurrentVersionTransition{NoncurrentDays: 30},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.nvt.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAbortIncompleteMultipartUploadValidate(t *testing.T) {
	tests := []struct {
		name        string
		abort       AbortIncompleteMultipartUpload
		expectError bool
	}{
		{
			name:        "valid",
			abort:       AbortIncompleteMultipartUpload{DaysAfterInitiation: 7},
			expectError: false,
		},
		{
			name:        "zero days",
			abort:       AbortIncompleteMultipartUpload{DaysAfterInitiation: 0},
			expectError: true,
		},
		{
			name:        "negative days",
			abort:       AbortIncompleteMultipartUpload{DaysAfterInitiation: -1},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.abort.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFilterMatches(t *testing.T) {
	tests := []struct {
		filter   Filter
		tags     map[string]string
		name     string
		key      string
		expected bool
	}{
		{
			name:     "empty filter matches all",
			filter:   Filter{},
			key:      "any/key.txt",
			expected: true,
		},
		{
			name:     "prefix match",
			filter:   Filter{Prefix: "logs/"},
			key:      "logs/app.log",
			expected: true,
		},
		{
			name:     "prefix no match",
			filter:   Filter{Prefix: "logs/"},
			key:      "data/file.txt",
			expected: false,
		},
		{
			name:     "tag match",
			filter:   Filter{Tag: &Tag{Key: "env", Value: "prod"}},
			key:      "file.txt",
			tags:     map[string]string{"env": "prod"},
			expected: true,
		},
		{
			name:     "tag no match - different value",
			filter:   Filter{Tag: &Tag{Key: "env", Value: "prod"}},
			key:      "file.txt",
			tags:     map[string]string{"env": "dev"},
			expected: false,
		},
		{
			name:     "tag no match - missing key",
			filter:   Filter{Tag: &Tag{Key: "env", Value: "prod"}},
			key:      "file.txt",
			tags:     map[string]string{"other": "value"},
			expected: false,
		},
		{
			name:     "tag no match - nil tags",
			filter:   Filter{Tag: &Tag{Key: "env", Value: "prod"}},
			key:      "file.txt",
			tags:     nil,
			expected: false,
		},
		{
			name: "And filter match",
			filter: Filter{
				And: &And{
					Prefix: "logs/",
					Tags:   []Tag{{Key: "env", Value: "prod"}},
				},
			},
			key:      "logs/app.log",
			tags:     map[string]string{"env": "prod"},
			expected: true,
		},
		{
			name: "And filter prefix no match",
			filter: Filter{
				And: &And{
					Prefix: "logs/",
					Tags:   []Tag{{Key: "env", Value: "prod"}},
				},
			},
			key:      "data/file.txt",
			tags:     map[string]string{"env": "prod"},
			expected: false,
		},
		{
			name: "And filter tag no match",
			filter: Filter{
				And: &And{
					Prefix: "logs/",
					Tags:   []Tag{{Key: "env", Value: "prod"}},
				},
			},
			key:      "logs/app.log",
			tags:     map[string]string{"env": "dev"},
			expected: false,
		},
		{
			name: "And filter multiple tags",
			filter: Filter{
				And: &And{
					Tags: []Tag{
						{Key: "env", Value: "prod"},
						{Key: "team", Value: "platform"},
					},
				},
			},
			key:      "file.txt",
			tags:     map[string]string{"env": "prod", "team": "platform"},
			expected: true,
		},
		{
			name: "And filter multiple tags partial match",
			filter: Filter{
				And: &And{
					Tags: []Tag{
						{Key: "env", Value: "prod"},
						{Key: "team", Value: "platform"},
					},
				},
			},
			key:      "file.txt",
			tags:     map[string]string{"env": "prod"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.filter.Matches(tt.key, tt.tags)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToXML(t *testing.T) {
	config := &LifecycleConfiguration{
		Rules: []LifecycleRule{
			{
				ID:     "expire-logs",
				Status: "Enabled",
				Filter: Filter{Prefix: "logs/"},
				Expiration: &Expiration{
					Days: 30,
				},
			},
		},
	}

	xmlData, err := config.ToXML()
	require.NoError(t, err)
	assert.Contains(t, string(xmlData), "LifecycleConfiguration")
	assert.Contains(t, string(xmlData), "expire-logs")
	assert.Contains(t, string(xmlData), "logs/")
	assert.Contains(t, string(xmlData), "30")
}

func TestXMLRoundTrip(t *testing.T) {
	original := &LifecycleConfiguration{
		Rules: []LifecycleRule{
			{
				ID:     "rule1",
				Status: "Enabled",
				Filter: Filter{Prefix: "test/"},
				Expiration: &Expiration{
					Days: 30,
				},
				Transition: []Transition{
					{Days: 15, StorageClass: "STANDARD_IA"},
				},
			},
		},
	}

	// Serialize to XML
	xmlData, err := original.ToXML()
	require.NoError(t, err)

	// Parse back
	parsed, err := ParseLifecycleConfig(xmlData)
	require.NoError(t, err)

	// Verify
	require.Len(t, parsed.Rules, 1)
	assert.Equal(t, original.Rules[0].ID, parsed.Rules[0].ID)
	assert.Equal(t, original.Rules[0].Status, parsed.Rules[0].Status)
	assert.Equal(t, original.Rules[0].Filter.Prefix, parsed.Rules[0].Filter.Prefix)
	assert.Equal(t, original.Rules[0].Expiration.Days, parsed.Rules[0].Expiration.Days)
	require.Len(t, parsed.Rules[0].Transition, 1)
	assert.Equal(t, original.Rules[0].Transition[0].Days, parsed.Rules[0].Transition[0].Days)
	assert.Equal(t, original.Rules[0].Transition[0].StorageClass, parsed.Rules[0].Transition[0].StorageClass)
}

func TestRuleFilterTags(t *testing.T) {
	// Test with the Filter using Tags field (for And conditions)
	rule := LifecycleRule{
		ID:     "rule1",
		Status: "Enabled",
		Filter: Filter{
			And: &And{
				Prefix: "data/",
				Tags: []Tag{
					{Key: "type", Value: "archive"},
				},
			},
		},
		Expiration: &Expiration{Days: 90},
	}

	// Should match with correct prefix and tag
	tags := map[string]string{"type": "archive"}
	assert.True(t, rule.MatchesObject("data/old-file.txt", tags))

	// Should not match with correct prefix but wrong tag
	tags = map[string]string{"type": "active"}
	assert.False(t, rule.MatchesObject("data/old-file.txt", tags))

	// Should not match with wrong prefix but correct tag
	tags = map[string]string{"type": "archive"}
	assert.False(t, rule.MatchesObject("logs/old-file.txt", tags))
}

func TestActionConstants(t *testing.T) {
	// Verify action constants are defined correctly
	assert.Equal(t, ActionNone, Action(0))
	assert.Equal(t, ActionDelete, Action(1))
	assert.Equal(t, ActionTransition, Action(2))
	assert.Equal(t, ActionDeleteMarker, Action(3))
	assert.Equal(t, ActionAbortMultipart, Action(4))
}

func TestActionResult(t *testing.T) {
	result := ActionResult{
		Action:      ActionTransition,
		TargetClass: "GLACIER",
		RuleID:      "rule1",
	}

	assert.Equal(t, ActionTransition, result.Action)
	assert.Equal(t, "GLACIER", result.TargetClass)
	assert.Equal(t, "rule1", result.RuleID)
}

func BenchmarkFilterMatches(b *testing.B) {
	filter := Filter{
		And: &And{
			Prefix: "logs/",
			Tags:   []Tag{{Key: "env", Value: "prod"}},
		},
	}
	tags := map[string]string{"env": "prod", "team": "platform"}

	b.ResetTimer()

	for range b.N {
		filter.Matches("logs/app.log", tags)
	}
}

func BenchmarkLifecycleRuleValidate(b *testing.B) {
	rule := LifecycleRule{
		ID:     "rule1",
		Status: "Enabled",
		Filter: Filter{Prefix: "logs/"},
		Expiration: &Expiration{
			Days: 30,
		},
		Transition: []Transition{
			{Days: 15, StorageClass: "STANDARD_IA"},
		},
	}

	b.ResetTimer()

	for range b.N {
		rule.Validate()
	}
}
