package tiering

import (
	"context"
	"testing"
	"time"
)

func TestPolicyFilter(t *testing.T) {
	evaluator := NewPolicyEvaluator([]Policy{
		{
			Name:     "test-policy",
			Enabled:  true,
			Priority: 1,
			Filter: PolicyFilter{
				Buckets: []string{"test-*", "prod-bucket"},
				Prefix:  "logs/",
				Suffix:  ".log",
				MinSize: 1024,
				MaxSize: 1024 * 1024,
			},
			Rules: []TransitionRule{
				{
					Name:               "to-cold",
					TargetTier:         TierCold,
					TargetStorageClass: StorageClassStandardIA,
					Action:             TransitionMove,
					Conditions: TransitionConditions{
						DaysAfterLastAccess: 30,
					},
				},
			},
		},
	})

	tests := []struct {
		name             string
		obj              ObjectInfo
		shouldMatch      bool
		shouldTransition bool
	}{
		{
			name: "matches all criteria and should transition",
			obj: ObjectInfo{
				Bucket:         "test-bucket",
				Key:            "logs/app.log",
				Size:           2048,
				LastAccessedAt: time.Now().Add(-45 * 24 * time.Hour),
			},
			shouldMatch:      true,
			shouldTransition: true,
		},
		{
			name: "matches filter but too recent access",
			obj: ObjectInfo{
				Bucket:         "test-bucket",
				Key:            "logs/app.log",
				Size:           2048,
				LastAccessedAt: time.Now().Add(-15 * 24 * time.Hour),
			},
			shouldMatch:      true,
			shouldTransition: false,
		},
		{
			name: "bucket doesn't match",
			obj: ObjectInfo{
				Bucket:         "other-bucket",
				Key:            "logs/app.log",
				Size:           2048,
				LastAccessedAt: time.Now().Add(-45 * 24 * time.Hour),
			},
			shouldMatch:      false,
			shouldTransition: false,
		},
		{
			name: "prefix doesn't match",
			obj: ObjectInfo{
				Bucket:         "test-bucket",
				Key:            "data/app.log",
				Size:           2048,
				LastAccessedAt: time.Now().Add(-45 * 24 * time.Hour),
			},
			shouldMatch:      false,
			shouldTransition: false,
		},
		{
			name: "suffix doesn't match",
			obj: ObjectInfo{
				Bucket:         "test-bucket",
				Key:            "logs/app.txt",
				Size:           2048,
				LastAccessedAt: time.Now().Add(-45 * 24 * time.Hour),
			},
			shouldMatch:      false,
			shouldTransition: false,
		},
		{
			name: "too small",
			obj: ObjectInfo{
				Bucket:         "test-bucket",
				Key:            "logs/app.log",
				Size:           512,
				LastAccessedAt: time.Now().Add(-45 * 24 * time.Hour),
			},
			shouldMatch:      false,
			shouldTransition: false,
		},
		{
			name: "too large",
			obj: ObjectInfo{
				Bucket:         "test-bucket",
				Key:            "logs/app.log",
				Size:           2 * 1024 * 1024,
				LastAccessedAt: time.Now().Add(-45 * 24 * time.Hour),
			},
			shouldMatch:      false,
			shouldTransition: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluator.Evaluate(tt.obj)

			if tt.shouldTransition && !result.ShouldTransition {
				t.Error("expected transition but got none")
			}

			if !tt.shouldTransition && result.ShouldTransition {
				t.Error("expected no transition but got one")
			}
		})
	}
}

func TestPolicyPriority(t *testing.T) {
	evaluator := NewPolicyEvaluator([]Policy{
		{
			Name:     "high-priority",
			Enabled:  true,
			Priority: 1,
			Filter:   PolicyFilter{},
			Rules: []TransitionRule{
				{
					Name:               "to-archive",
					TargetTier:         TierArchive,
					TargetStorageClass: StorageClassGlacier,
					Action:             TransitionMove,
					Conditions: TransitionConditions{
						DaysAfterLastAccess: 90,
					},
				},
			},
		},
		{
			Name:     "low-priority",
			Enabled:  true,
			Priority: 10,
			Filter:   PolicyFilter{},
			Rules: []TransitionRule{
				{
					Name:               "to-cold",
					TargetTier:         TierCold,
					TargetStorageClass: StorageClassStandardIA,
					Action:             TransitionMove,
					Conditions: TransitionConditions{
						DaysAfterLastAccess: 30,
					},
				},
			},
		},
	})

	obj := ObjectInfo{
		Bucket:         "test-bucket",
		Key:            "data.txt",
		Size:           1024,
		LastAccessedAt: time.Now().Add(-100 * 24 * time.Hour), // 100 days ago
	}

	result := evaluator.Evaluate(obj)

	if !result.ShouldTransition {
		t.Fatal("expected transition")
	}

	if result.TargetTier != TierArchive {
		t.Errorf("expected archive tier, got %s", result.TargetTier)
	}

	if result.Policy.Name != "high-priority" {
		t.Errorf("expected high-priority policy, got %s", result.Policy.Name)
	}
}

func TestContentTypeFilter(t *testing.T) {
	tests := []struct {
		pattern     string
		contentType string
		expected    bool
	}{
		{"*", "text/plain", true},
		{"*/*", "text/plain", true},
		{"text/*", "text/plain", true},
		{"text/*", "text/html", true},
		{"text/plain", "text/plain", true},
		{"text/plain", "text/html", false},
		{"image/*", "text/plain", false},
		{"application/json", "application/json", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.contentType, func(t *testing.T) {
			result := matchContentType(tt.pattern, tt.contentType)
			if result != tt.expected {
				t.Errorf("matchContentType(%q, %q) = %v, want %v", tt.pattern, tt.contentType, result, tt.expected)
			}
		})
	}
}

func TestWildcardMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		s        string
		expected bool
	}{
		{"*", "anything", true},
		{"test-*", "test-bucket", true},
		{"test-*", "prod-bucket", false},
		{"*-bucket", "test-bucket", true},
		{"*-bucket", "test-storage", false},
		{"bucket", "bucket", true},
		{"bucket", "other", false},
		{"test-?-bucket", "test-a-bucket", true},
		{"test-?-bucket", "test-ab-bucket", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.s, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.s)
			if result != tt.expected {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v", tt.pattern, tt.s, result, tt.expected)
			}
		})
	}
}

func TestCache(t *testing.T) {
	config := CacheConfig{
		MaxSize:        1024 * 1024, // 1MB
		MaxObjects:     100,
		TTL:            time.Hour,
		EvictionPolicy: "lru",
	}

	cache := NewCache(config, nil)

	defer func() { _ = cache.Close() }()

	ctx := context.Background()

	// Test Put and Get
	t.Run("PutAndGet", func(t *testing.T) {
		data := []byte("test data")

		err := cache.Put(ctx, "key1", data, "text/plain", "etag1")
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		entry, ok := cache.Get(ctx, "key1")
		if !ok {
			t.Fatal("Get returned not found")
		}

		if string(entry.Data) != "test data" {
			t.Errorf("unexpected data: %s", string(entry.Data))
		}

		if entry.ContentType != "text/plain" {
			t.Errorf("unexpected content type: %s", entry.ContentType)
		}
	})

	// Test Has
	t.Run("Has", func(t *testing.T) {
		if !cache.Has(ctx, "key1") {
			t.Error("Has returned false for existing key")
		}

		if cache.Has(ctx, "nonexistent") {
			t.Error("Has returned true for non-existing key")
		}
	})

	// Test Delete
	t.Run("Delete", func(t *testing.T) {
		err := cache.Put(ctx, "key2", []byte("data"), "text/plain", "etag")
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err = cache.Delete(ctx, "key2")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		if cache.Has(ctx, "key2") {
			t.Error("key still exists after delete")
		}
	})

	// Test eviction
	t.Run("Eviction", func(t *testing.T) {
		smallCache := NewCache(CacheConfig{
			MaxSize:    100,
			MaxObjects: 3,
		}, nil)

		defer func() { _ = smallCache.Close() }()

		// Add 3 objects
		for i := range 3 {
			key := string(rune('a' + i))
			_ = smallCache.Put(ctx, key, []byte("data"), "text/plain", "")
		}

		// Add a 4th object - should evict the first
		_ = smallCache.Put(ctx, "d", []byte("data"), "text/plain", "")

		stats := smallCache.Stats()
		if stats.Evictions == 0 {
			t.Error("expected evictions")
		}
	})

	// Test stats
	t.Run("Stats", func(t *testing.T) {
		stats := cache.Stats()
		if stats.Objects == 0 {
			t.Error("expected some objects in cache")
		}

		if stats.Hits == 0 {
			t.Error("expected some hits")
		}
	})

	// Test Clear
	t.Run("Clear", func(t *testing.T) {
		cache.Clear()

		if cache.Count() != 0 {
			t.Errorf("expected 0 objects after clear, got %d", cache.Count())
		}
	})
}

func TestCacheTTL(t *testing.T) {
	config := CacheConfig{
		MaxSize:    1024 * 1024,
		MaxObjects: 100,
		TTL:        100 * time.Millisecond, // Very short TTL
	}

	cache := NewCache(config, nil)

	defer func() { _ = cache.Close() }()

	ctx := context.Background()

	// Put data
	err := cache.Put(ctx, "expiring-key", []byte("data"), "text/plain", "")
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Should exist immediately
	if !cache.Has(ctx, "expiring-key") {
		t.Error("key should exist immediately")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Should not exist after TTL
	if cache.Has(ctx, "expiring-key") {
		t.Error("key should have expired")
	}

	// Get should return not found
	_, ok := cache.Get(ctx, "expiring-key")
	if ok {
		t.Error("Get should return not found for expired key")
	}
}

func TestDefaultPolicies(t *testing.T) {
	policies := DefaultPolicies()

	if len(policies) == 0 {
		t.Fatal("DefaultPolicies returned empty slice")
	}

	// Check that all policies have names and rules
	for _, policy := range policies {
		if policy.Name == "" {
			t.Error("policy has empty name")
		}

		if len(policy.Rules) == 0 {
			t.Errorf("policy %s has no rules", policy.Name)
		}
	}
}

func TestTransitionConditions(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		obj        ObjectInfo
		conditions TransitionConditions
		expected   bool
	}{
		{
			name: "days after creation - should transition",
			obj: ObjectInfo{
				CreatedAt: now.Add(-10 * 24 * time.Hour),
			},
			conditions: TransitionConditions{
				DaysAfterCreation: 7,
			},
			expected: true,
		},
		{
			name: "days after creation - should not transition",
			obj: ObjectInfo{
				CreatedAt: now.Add(-5 * 24 * time.Hour),
			},
			conditions: TransitionConditions{
				DaysAfterCreation: 7,
			},
			expected: false,
		},
		{
			name: "days after last access - should transition",
			obj: ObjectInfo{
				LastAccessedAt: now.Add(-45 * 24 * time.Hour),
			},
			conditions: TransitionConditions{
				DaysAfterLastAccess: 30,
			},
			expected: true,
		},
		{
			name: "access count threshold - should transition (low access)",
			obj: ObjectInfo{
				AccessCount: 2,
			},
			conditions: TransitionConditions{
				AccessCountThreshold:  5,
				AccessCountPeriodDays: 30,
			},
			expected: true,
		},
		{
			name: "access count threshold - should not transition (high access)",
			obj: ObjectInfo{
				AccessCount: 10,
			},
			conditions: TransitionConditions{
				AccessCountThreshold:  5,
				AccessCountPeriodDays: 30,
			},
			expected: false,
		},
	}

	evaluator := &PolicyEvaluator{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluator.matchesConditions(tt.obj, tt.conditions)
			if result != tt.expected {
				t.Errorf("matchesConditions() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStorageClasses(t *testing.T) {
	// Ensure all storage classes are defined
	classes := []StorageClass{
		StorageClassStandard,
		StorageClassStandardIA,
		StorageClassOneZoneIA,
		StorageClassIntelligentTiering,
		StorageClassGlacier,
		StorageClassGlacierIR,
		StorageClassDeepArchive,
	}

	for _, class := range classes {
		if class == "" {
			t.Error("found empty storage class")
		}
	}
}

func TestTierTypes(t *testing.T) {
	// Ensure all tier types are defined
	tiers := []TierType{
		TierHot,
		TierWarm,
		TierCold,
		TierArchive,
	}

	for _, tier := range tiers {
		if tier == "" {
			t.Error("found empty tier type")
		}
	}
}
