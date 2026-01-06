package iam

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringOrSliceUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected StringOrSlice
	}{
		{
			name:     "single string",
			input:    `"value"`,
			expected: StringOrSlice{"value"},
		},
		{
			name:     "array of strings",
			input:    `["value1", "value2"]`,
			expected: StringOrSlice{"value1", "value2"},
		},
		{
			name:     "empty array",
			input:    `[]`,
			expected: StringOrSlice{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s StringOrSlice

			err := json.Unmarshal([]byte(tt.input), &s)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, s)
		})
	}
}

func TestStringOrSliceMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		input    StringOrSlice
	}{
		{
			name:     "single value",
			input:    StringOrSlice{"value"},
			expected: `"value"`,
		},
		{
			name:     "multiple values",
			input:    StringOrSlice{"value1", "value2"},
			expected: `["value1","value2"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := json.Marshal(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

func TestStringOrSliceContains(t *testing.T) {
	s := StringOrSlice{"a", "b", "c"}

	assert.True(t, s.Contains("a"))
	assert.True(t, s.Contains("b"))
	assert.True(t, s.Contains("c"))
	assert.False(t, s.Contains("d"))
	assert.False(t, s.Contains(""))
}

func TestNewPolicyManager(t *testing.T) {
	pm := NewPolicyManager(nil)

	require.NotNil(t, pm)
	assert.NotNil(t, pm.policies)
	assert.NotNil(t, pm.userPolicies)
	assert.NotNil(t, pm.groupPolicies)
	assert.NotNil(t, pm.bucketPolicies)
}

func TestCreatePolicy(t *testing.T) {
	ctx := context.Background()
	pm := NewPolicyManager(nil)

	t.Run("valid policy", func(t *testing.T) {
		policy := &Policy{
			Version: PolicyVersion,
			Statements: []Statement{
				{
					Effect:   EffectAllow,
					Action:   StringOrSlice{"s3:GetObject"},
					Resource: StringOrSlice{"arn:aws:s3:::test-bucket/*"},
				},
			},
		}

		err := pm.CreatePolicy(ctx, "test-policy", "Test policy", policy)
		require.NoError(t, err)

		// Verify policy was created
		retrieved, err := pm.GetPolicy(ctx, "test-policy")
		require.NoError(t, err)
		assert.Equal(t, "test-policy", retrieved.Name)
		assert.Equal(t, "Test policy", retrieved.Description)
		assert.NotEmpty(t, retrieved.ID)
		assert.False(t, retrieved.CreatedAt.IsZero())
	})

	t.Run("duplicate policy", func(t *testing.T) {
		policy := &Policy{
			Version: PolicyVersion,
			Statements: []Statement{
				{
					Effect:   EffectAllow,
					Action:   StringOrSlice{"s3:GetObject"},
					Resource: StringOrSlice{"*"},
				},
			},
		}

		err := pm.CreatePolicy(ctx, "duplicate", "First", policy)
		require.NoError(t, err)

		err = pm.CreatePolicy(ctx, "duplicate", "Second", policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("nil policy", func(t *testing.T) {
		err := pm.CreatePolicy(ctx, "nil-policy", "Nil", nil)
		assert.Error(t, err)
	})

	t.Run("policy with no statements", func(t *testing.T) {
		policy := &Policy{
			Version:    PolicyVersion,
			Statements: []Statement{},
		}

		err := pm.CreatePolicy(ctx, "empty-policy", "Empty", policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one statement")
	})
}

func TestGetPolicy(t *testing.T) {
	ctx := context.Background()
	pm := NewPolicyManager(nil)

	// Create a policy first
	policy := &Policy{
		Version: PolicyVersion,
		Statements: []Statement{
			{
				Effect:   EffectAllow,
				Action:   StringOrSlice{"s3:*"},
				Resource: StringOrSlice{"*"},
			},
		},
	}
	err := pm.CreatePolicy(ctx, "test-policy", "Test", policy)
	require.NoError(t, err)

	t.Run("existing policy", func(t *testing.T) {
		p, err := pm.GetPolicy(ctx, "test-policy")
		require.NoError(t, err)
		assert.NotNil(t, p)
		assert.Equal(t, "test-policy", p.Name)
	})

	t.Run("non-existing policy", func(t *testing.T) {
		_, err := pm.GetPolicy(ctx, "non-existing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestDeletePolicy(t *testing.T) {
	ctx := context.Background()
	pm := NewPolicyManager(nil)

	// Create a policy
	policy := &Policy{
		Version: PolicyVersion,
		Statements: []Statement{
			{
				Effect:   EffectAllow,
				Action:   StringOrSlice{"s3:*"},
				Resource: StringOrSlice{"*"},
			},
		},
	}
	err := pm.CreatePolicy(ctx, "test-policy", "Test", policy)
	require.NoError(t, err)

	t.Run("delete existing policy", func(t *testing.T) {
		err := pm.DeletePolicy(ctx, "test-policy")
		require.NoError(t, err)

		// Verify deleted
		_, err = pm.GetPolicy(ctx, "test-policy")
		assert.Error(t, err)
	})

	t.Run("delete non-existing policy", func(t *testing.T) {
		err := pm.DeletePolicy(ctx, "non-existing")
		assert.Error(t, err)
	})

	t.Run("delete attached policy", func(t *testing.T) {
		// Create and attach policy
		policy := &Policy{
			Version: PolicyVersion,
			Statements: []Statement{
				{
					Effect:   EffectAllow,
					Action:   StringOrSlice{"s3:*"},
					Resource: StringOrSlice{"*"},
				},
			},
		}
		err := pm.CreatePolicy(ctx, "attached-policy", "Attached", policy)
		require.NoError(t, err)

		err = pm.AttachUserPolicy(ctx, "user1", "attached-policy")
		require.NoError(t, err)

		// Try to delete - should fail
		err = pm.DeletePolicy(ctx, "attached-policy")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "attached to user")
	})
}

func TestListPolicies(t *testing.T) {
	ctx := context.Background()
	pm := NewPolicyManager(nil)

	// Create multiple policies
	for i := 1; i <= 3; i++ {
		policy := &Policy{
			Version: PolicyVersion,
			Statements: []Statement{
				{
					Effect:   EffectAllow,
					Action:   StringOrSlice{"s3:*"},
					Resource: StringOrSlice{"*"},
				},
			},
		}
		err := pm.CreatePolicy(ctx, "policy-"+string(rune('0'+i)), "Policy", policy)
		require.NoError(t, err)
	}

	policies := pm.ListPolicies(ctx)
	assert.Len(t, policies, 3)
}

func TestUserPolicyAttachment(t *testing.T) {
	ctx := context.Background()
	pm := NewPolicyManager(nil)

	// Create policy
	policy := &Policy{
		Version: PolicyVersion,
		Statements: []Statement{
			{
				Effect:   EffectAllow,
				Action:   StringOrSlice{"s3:GetObject"},
				Resource: StringOrSlice{"*"},
			},
		},
	}
	err := pm.CreatePolicy(ctx, "test-policy", "Test", policy)
	require.NoError(t, err)

	t.Run("attach policy", func(t *testing.T) {
		err := pm.AttachUserPolicy(ctx, "user1", "test-policy")
		require.NoError(t, err)

		policies := pm.GetUserPolicies(ctx, "user1")
		assert.Len(t, policies, 1)
		assert.Equal(t, "test-policy", policies[0].Name)
	})

	t.Run("attach same policy twice", func(t *testing.T) {
		// Should be idempotent
		err := pm.AttachUserPolicy(ctx, "user1", "test-policy")
		require.NoError(t, err)

		policies := pm.GetUserPolicies(ctx, "user1")
		assert.Len(t, policies, 1)
	})

	t.Run("attach non-existing policy", func(t *testing.T) {
		err := pm.AttachUserPolicy(ctx, "user1", "non-existing")
		assert.Error(t, err)
	})

	t.Run("detach policy", func(t *testing.T) {
		err := pm.DetachUserPolicy(ctx, "user1", "test-policy")
		require.NoError(t, err)

		policies := pm.GetUserPolicies(ctx, "user1")
		assert.Empty(t, policies)
	})

	t.Run("detach non-attached policy", func(t *testing.T) {
		err := pm.DetachUserPolicy(ctx, "user1", "test-policy")
		assert.Error(t, err)
	})
}

func TestBucketPolicy(t *testing.T) {
	ctx := context.Background()
	pm := NewPolicyManager(nil)

	t.Run("set bucket policy", func(t *testing.T) {
		policy := &Policy{
			Version: PolicyVersion,
			Statements: []Statement{
				{
					Sid:       "AllowPublicRead",
					Effect:    EffectAllow,
					Principal: &Principal{AWS: StringOrSlice{"*"}},
					Action:    StringOrSlice{"s3:GetObject"},
					Resource:  StringOrSlice{"arn:aws:s3:::my-bucket/*"},
				},
			},
		}

		err := pm.SetBucketPolicy(ctx, "my-bucket", policy)
		require.NoError(t, err)

		retrieved, err := pm.GetBucketPolicy(ctx, "my-bucket")
		require.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Len(t, retrieved.Statements, 1)
	})

	t.Run("get non-existing bucket policy", func(t *testing.T) {
		_, err := pm.GetBucketPolicy(ctx, "non-existing")
		assert.Error(t, err)
	})

	t.Run("delete bucket policy", func(t *testing.T) {
		err := pm.DeleteBucketPolicy(ctx, "my-bucket")
		require.NoError(t, err)

		_, err = pm.GetBucketPolicy(ctx, "my-bucket")
		assert.Error(t, err)
	})
}

func TestValidatePolicy(t *testing.T) {
	pm := NewPolicyManager(nil)

	tests := []struct {
		policy      *Policy
		name        string
		errorMsg    string
		expectError bool
	}{
		{
			name:        "nil policy",
			policy:      nil,
			expectError: true,
			errorMsg:    "policy is nil",
		},
		{
			name: "no statements",
			policy: &Policy{
				Version:    PolicyVersion,
				Statements: []Statement{},
			},
			expectError: true,
			errorMsg:    "at least one statement",
		},
		{
			name: "invalid effect",
			policy: &Policy{
				Version: PolicyVersion,
				Statements: []Statement{
					{
						Effect:   "Invalid",
						Action:   StringOrSlice{"s3:*"},
						Resource: StringOrSlice{"*"},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid effect",
		},
		{
			name: "no action",
			policy: &Policy{
				Version: PolicyVersion,
				Statements: []Statement{
					{
						Effect:   EffectAllow,
						Resource: StringOrSlice{"*"},
					},
				},
			},
			expectError: true,
			errorMsg:    "Action or NotAction",
		},
		{
			name: "no resource",
			policy: &Policy{
				Version: PolicyVersion,
				Statements: []Statement{
					{
						Effect: EffectAllow,
						Action: StringOrSlice{"s3:*"},
					},
				},
			},
			expectError: true,
			errorMsg:    "Resource or NotResource",
		},
		{
			name: "invalid action",
			policy: &Policy{
				Version: PolicyVersion,
				Statements: []Statement{
					{
						Effect:   EffectAllow,
						Action:   StringOrSlice{"invalid:Action"},
						Resource: StringOrSlice{"*"},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid action",
		},
		{
			name: "invalid resource format",
			policy: &Policy{
				Version: PolicyVersion,
				Statements: []Statement{
					{
						Effect:   EffectAllow,
						Action:   StringOrSlice{"s3:*"},
						Resource: StringOrSlice{"not-an-arn"},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid resource",
		},
		{
			name: "valid policy",
			policy: &Policy{
				Version: PolicyVersion,
				Statements: []Statement{
					{
						Effect:   EffectAllow,
						Action:   StringOrSlice{"s3:GetObject"},
						Resource: StringOrSlice{"arn:aws:s3:::my-bucket/*"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "wildcard action",
			policy: &Policy{
				Version: PolicyVersion,
				Statements: []Statement{
					{
						Effect:   EffectAllow,
						Action:   StringOrSlice{"*"},
						Resource: StringOrSlice{"*"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "s3 wildcard action",
			policy: &Policy{
				Version: PolicyVersion,
				Statements: []Statement{
					{
						Effect:   EffectAllow,
						Action:   StringOrSlice{"s3:*"},
						Resource: StringOrSlice{"*"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "admin action",
			policy: &Policy{
				Version: PolicyVersion,
				Statements: []Statement{
					{
						Effect:   EffectAllow,
						Action:   StringOrSlice{"admin:CreateUser"},
						Resource: StringOrSlice{"*"},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pm.validatePolicy(tt.policy)
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

func TestIsValidAction(t *testing.T) {
	pm := NewPolicyManager(nil)

	validActions := []string{
		"*",
		"s3:*",
		"s3:GetObject",
		"s3:PutObject",
		"s3:DeleteObject",
		"s3:ListBucket",
		"s3:Get*",
		"admin:*",
		"admin:CreateUser",
		"admin:DeleteUser",
	}

	for _, action := range validActions {
		t.Run("valid_"+action, func(t *testing.T) {
			assert.True(t, pm.isValidAction(action), "Action should be valid: %s", action)
		})
	}

	invalidActions := []string{
		"invalid:Action",
		"ec2:DescribeInstances",
		"s3:NonExistentAction",
	}

	for _, action := range invalidActions {
		t.Run("invalid_"+action, func(t *testing.T) {
			assert.False(t, pm.isValidAction(action), "Action should be invalid: %s", action)
		})
	}
}

func TestIsValidResource(t *testing.T) {
	pm := NewPolicyManager(nil)

	validResources := []string{
		"*",
		"arn:aws:s3:::bucket",
		"arn:aws:s3:::bucket/*",
		"arn:aws:s3:::bucket/key",
		"arn:aws:iam::123456789012:user/*",
	}

	for _, resource := range validResources {
		t.Run("valid_"+resource, func(t *testing.T) {
			assert.True(t, pm.isValidResource(resource))
		})
	}

	invalidResources := []string{
		"bucket",
		"s3://bucket/key",
		"arn:invalid",
		"arn:aws:ec2:region:account:instance/i-123",
	}

	for _, resource := range invalidResources {
		t.Run("invalid_"+resource, func(t *testing.T) {
			assert.False(t, pm.isValidResource(resource))
		})
	}
}

func TestPolicyEvaluator(t *testing.T) {
	ctx := context.Background()
	pm := NewPolicyManager(nil)
	evaluator := NewPolicyEvaluator(pm)

	// Create a test policy
	policy := &Policy{
		Version: PolicyVersion,
		Statements: []Statement{
			{
				Sid:      "AllowGetObject",
				Effect:   EffectAllow,
				Action:   StringOrSlice{"s3:GetObject"},
				Resource: StringOrSlice{"arn:aws:s3:::test-bucket/*"},
			},
		},
	}
	err := pm.CreatePolicy(ctx, "test-policy", "Test", policy)
	require.NoError(t, err)

	// Attach to user
	err = pm.AttachUserPolicy(ctx, "user1", "test-policy")
	require.NoError(t, err)

	t.Run("allow matching action", func(t *testing.T) {
		evalCtx := &EvaluationContext{
			UserID:   "user1",
			Action:   "s3:GetObject",
			Resource: "arn:aws:s3:::test-bucket/key",
		}

		result := evaluator.Evaluate(ctx, evalCtx)
		assert.Equal(t, DecisionAllow, result.Decision)
	})

	t.Run("implicit deny non-matching action", func(t *testing.T) {
		evalCtx := &EvaluationContext{
			UserID:   "user1",
			Action:   "s3:PutObject",
			Resource: "arn:aws:s3:::test-bucket/key",
		}

		result := evaluator.Evaluate(ctx, evalCtx)
		assert.Equal(t, DecisionImplicitDeny, result.Decision)
	})

	t.Run("implicit deny non-matching resource", func(t *testing.T) {
		evalCtx := &EvaluationContext{
			UserID:   "user1",
			Action:   "s3:GetObject",
			Resource: "arn:aws:s3:::other-bucket/key",
		}

		result := evaluator.Evaluate(ctx, evalCtx)
		assert.Equal(t, DecisionImplicitDeny, result.Decision)
	})
}

func TestPolicyEvaluatorExplicitDeny(t *testing.T) {
	ctx := context.Background()
	pm := NewPolicyManager(nil)
	evaluator := NewPolicyEvaluator(pm)

	// Create policy with allow
	allowPolicy := &Policy{
		Version: PolicyVersion,
		Statements: []Statement{
			{
				Effect:   EffectAllow,
				Action:   StringOrSlice{"s3:*"},
				Resource: StringOrSlice{"*"},
			},
		},
	}
	err := pm.CreatePolicy(ctx, "allow-all", "Allow All", allowPolicy)
	require.NoError(t, err)

	// Create policy with deny
	denyPolicy := &Policy{
		Version: PolicyVersion,
		Statements: []Statement{
			{
				Effect:   EffectDeny,
				Action:   StringOrSlice{"s3:DeleteObject"},
				Resource: StringOrSlice{"*"},
			},
		},
	}
	err = pm.CreatePolicy(ctx, "deny-delete", "Deny Delete", denyPolicy)
	require.NoError(t, err)

	// Attach both policies
	err = pm.AttachUserPolicy(ctx, "user1", "allow-all")
	require.NoError(t, err)
	err = pm.AttachUserPolicy(ctx, "user1", "deny-delete")
	require.NoError(t, err)

	t.Run("explicit deny overrides allow", func(t *testing.T) {
		evalCtx := &EvaluationContext{
			UserID:   "user1",
			Action:   "s3:DeleteObject",
			Resource: "arn:aws:s3:::bucket/key",
		}

		result := evaluator.Evaluate(ctx, evalCtx)
		assert.Equal(t, DecisionDeny, result.Decision)
	})

	t.Run("other actions still allowed", func(t *testing.T) {
		evalCtx := &EvaluationContext{
			UserID:   "user1",
			Action:   "s3:GetObject",
			Resource: "arn:aws:s3:::bucket/key",
		}

		result := evaluator.Evaluate(ctx, evalCtx)
		assert.Equal(t, DecisionAllow, result.Decision)
	})
}

func TestConditionMatches(t *testing.T) {
	pm := NewPolicyManager(nil)
	evaluator := NewPolicyEvaluator(pm)

	now := time.Now()

	tests := []struct {
		condition map[string]any
		evalCtx   *EvaluationContext
		name      string
		expected  bool
	}{
		{
			name: "StringEquals match",
			condition: map[string]any{
				"StringEquals": map[string]any{
					"aws:username": "user1",
				},
			},
			evalCtx: &EvaluationContext{
				UserID: "user1",
			},
			expected: true,
		},
		{
			name: "StringEquals no match",
			condition: map[string]any{
				"StringEquals": map[string]any{
					"aws:username": "user2",
				},
			},
			evalCtx: &EvaluationContext{
				UserID: "user1",
			},
			expected: false,
		},
		{
			name: "Bool SecureTransport true",
			condition: map[string]any{
				"Bool": map[string]any{
					"aws:SecureTransport": "true",
				},
			},
			evalCtx: &EvaluationContext{
				IsSecure: true,
			},
			expected: true,
		},
		{
			name: "Bool SecureTransport false",
			condition: map[string]any{
				"Bool": map[string]any{
					"aws:SecureTransport": "true",
				},
			},
			evalCtx: &EvaluationContext{
				IsSecure: false,
			},
			expected: false,
		},
		{
			name: "IpAddress match",
			condition: map[string]any{
				"IpAddress": map[string]any{
					"aws:SourceIp": "192.168.1.100",
				},
			},
			evalCtx: &EvaluationContext{
				SourceIP: "192.168.1.100",
			},
			expected: true,
		},
		{
			name: "IfExists with nil value",
			condition: map[string]any{
				"StringEqualsIfExists": map[string]any{
					"custom:key": "value",
				},
			},
			evalCtx: &EvaluationContext{
				Conditions: nil,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.evalCtx.RequestTime = now
			result := evaluator.conditionMatches(tt.condition, tt.evalCtx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWildcardMatches(t *testing.T) {
	pm := NewPolicyManager(nil)
	evaluator := NewPolicyEvaluator(pm)

	tests := []struct {
		pattern  string
		value    string
		expected bool
	}{
		{"*", "anything", true},
		{"s3:*", "s3:GetObject", true},
		{"s3:Get*", "s3:GetObject", true},
		{"s3:Get*", "s3:PutObject", false},
		{"arn:aws:s3:::bucket/*", "arn:aws:s3:::bucket/key", true},
		{"arn:aws:s3:::bucket/*", "arn:aws:s3:::other/key", false},
		{"*.example.com", "sub.example.com", true},
		{"*.example.com", "example.com", false},
		{"test?", "test1", true},
		{"test?", "test12", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.value, func(t *testing.T) {
			result := evaluator.wildcardMatches(tt.pattern, tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPredefinedPolicies(t *testing.T) {
	t.Run("ReadOnlyPolicy", func(t *testing.T) {
		assert.NotNil(t, ReadOnlyPolicy)
		assert.Equal(t, PolicyVersion, ReadOnlyPolicy.Version)
		assert.Len(t, ReadOnlyPolicy.Statements, 1)
		assert.Equal(t, EffectAllow, ReadOnlyPolicy.Statements[0].Effect)
	})

	t.Run("ReadWritePolicy", func(t *testing.T) {
		assert.NotNil(t, ReadWritePolicy)
		assert.Equal(t, PolicyVersion, ReadWritePolicy.Version)
		assert.Len(t, ReadWritePolicy.Statements, 1)
	})

	t.Run("AdminPolicy", func(t *testing.T) {
		assert.NotNil(t, AdminPolicy)
		assert.Equal(t, PolicyVersion, AdminPolicy.Version)
		assert.True(t, AdminPolicy.Statements[0].Action.Contains("s3:*"))
		assert.True(t, AdminPolicy.Statements[0].Action.Contains("admin:*"))
	})
}

func TestS3Actions(t *testing.T) {
	// Verify some critical S3 actions are defined
	expectedActions := []string{
		"s3:GetObject",
		"s3:PutObject",
		"s3:DeleteObject",
		"s3:ListBucket",
		"s3:CreateBucket",
		"s3:DeleteBucket",
		"s3:*",
	}

	for _, action := range expectedActions {
		t.Run(action, func(t *testing.T) {
			assert.True(t, S3Actions[action], "Action %s should be defined", action)
		})
	}
}

func TestAdminActions(t *testing.T) {
	// Verify some critical admin actions are defined
	expectedActions := []string{
		"admin:*",
		"admin:CreateUser",
		"admin:DeleteUser",
		"admin:ListUsers",
		"admin:CreatePolicy",
		"admin:DeletePolicy",
	}

	for _, action := range expectedActions {
		t.Run(action, func(t *testing.T) {
			assert.True(t, AdminActions[action], "Action %s should be defined", action)
		})
	}
}

func TestConditionOperators(t *testing.T) {
	expectedOperators := []string{
		"StringEquals",
		"StringNotEquals",
		"StringLike",
		"NumericEquals",
		"Bool",
		"IpAddress",
		"NotIpAddress",
	}

	for _, op := range expectedOperators {
		t.Run(op, func(t *testing.T) {
			assert.True(t, ConditionOperators[op], "Operator %s should be defined", op)
		})
	}
}

func TestConditionKeys(t *testing.T) {
	expectedKeys := []string{
		"aws:CurrentTime",
		"aws:SecureTransport",
		"aws:SourceIp",
		"aws:username",
		"s3:x-amz-acl",
		"s3:prefix",
	}

	for _, key := range expectedKeys {
		t.Run(key, func(t *testing.T) {
			assert.True(t, ConditionKeys[key], "Key %s should be defined", key)
		})
	}
}

// TestIPMatchesIPv6 tests IPv6 CIDR matching functionality.
func TestIPMatchesIPv6(t *testing.T) {
	pm := NewPolicyManager(nil)
	pe := NewPolicyEvaluator(pm)

	tests := []struct {
		name     string
		ip       string
		cidr     string
		expected bool
	}{
		// IPv6 CIDR matching
		{
			name:     "IPv6 address within CIDR range",
			ip:       "2001:db8::1",
			cidr:     "2001:db8::/32",
			expected: true,
		},
		{
			name:     "IPv6 address outside CIDR range",
			ip:       "2001:db9::1",
			cidr:     "2001:db8::/32",
			expected: false,
		},
		{
			name:     "IPv6 exact match - same address",
			ip:       "2001:db8::1",
			cidr:     "2001:db8::1",
			expected: true,
		},
		{
			name:     "IPv6 loopback",
			ip:       "::1",
			cidr:     "::1/128",
			expected: true,
		},
		{
			name:     "IPv6 any address",
			ip:       "::",
			cidr:     "::/0",
			expected: true,
		},
		// IPv4-mapped IPv6
		{
			name:     "IPv4-mapped IPv6 address",
			ip:       "::ffff:192.168.1.1",
			cidr:     "::ffff:192.168.0.0/112",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := pe.ipMatches(tc.ip, tc.cidr)
			assert.Equal(t, tc.expected, result, "ipMatches(%s, %s)", tc.ip, tc.cidr)
		})
	}
}

// TestIPMatchesIPv6SpecialAddresses tests special IPv6 address handling.
func TestIPMatchesIPv6SpecialAddresses(t *testing.T) {
	pm := NewPolicyManager(nil)
	pe := NewPolicyEvaluator(pm)

	tests := []struct {
		name     string
		ip       string
		cidr     string
		expected bool
	}{
		// Unique local addresses (fc00::/7)
		{
			name:     "ULA address within range",
			ip:       "fd00::1",
			cidr:     "fc00::/7",
			expected: true,
		},
		// Multicast addresses (ff00::/8)
		{
			name:     "Multicast address within range",
			ip:       "ff02::1",
			cidr:     "ff00::/8",
			expected: true,
		},
		// Link-local (fe80::/10)
		{
			name:     "Link-local address",
			ip:       "fe80::1",
			cidr:     "fe80::/10",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := pe.ipMatches(tc.ip, tc.cidr)
			assert.Equal(t, tc.expected, result, "ipMatches(%s, %s)", tc.ip, tc.cidr)
		})
	}
}

// TestIPMatchesIPv6Normalization tests that different representations of the same address match.
func TestIPMatchesIPv6Normalization(t *testing.T) {
	pm := NewPolicyManager(nil)
	pe := NewPolicyEvaluator(pm)

	tests := []struct {
		name     string
		ip       string
		cidr     string
		expected bool
	}{
		{
			name:     "Full form vs compressed form",
			ip:       "2001:0db8:0000:0000:0000:0000:0000:0001",
			cidr:     "2001:db8::1",
			expected: true,
		},
		{
			name:     "Mixed case in hex digits",
			ip:       "2001:DB8::1",
			cidr:     "2001:db8::1",
			expected: true,
		},
		{
			name:     "Leading zeros stripped vs present",
			ip:       "2001:db8:0:0:0:0:0:1",
			cidr:     "2001:db8::1",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := pe.ipMatches(tc.ip, tc.cidr)
			assert.Equal(t, tc.expected, result, "ipMatches(%s, %s)", tc.ip, tc.cidr)
		})
	}
}
