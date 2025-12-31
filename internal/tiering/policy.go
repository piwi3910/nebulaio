package tiering

import (
	"regexp"
	"strings"
	"time"
)

// StorageClass represents an S3 storage class.
type StorageClass string

const (
	// StorageClassStandard is the default storage class.
	StorageClassStandard StorageClass = "STANDARD"
	// StorageClassStandardIA is for infrequent access.
	StorageClassStandardIA StorageClass = "STANDARD_IA"
	// StorageClassOneZoneIA is for infrequent access in one zone.
	StorageClassOneZoneIA StorageClass = "ONEZONE_IA"
	// StorageClassIntelligentTiering is for automatic tiering.
	StorageClassIntelligentTiering StorageClass = "INTELLIGENT_TIERING"
	// StorageClassGlacier is for archival storage.
	StorageClassGlacier StorageClass = "GLACIER"
	// StorageClassGlacierIR is for instant retrieval from archive.
	StorageClassGlacierIR StorageClass = "GLACIER_IR"
	// StorageClassDeepArchive is for long-term archival.
	StorageClassDeepArchive StorageClass = "DEEP_ARCHIVE"
)

// TierType represents a storage tier.
type TierType string

const (
	// TierHot is the fastest tier (SSD, local storage).
	TierHot TierType = "hot"
	// TierWarm is for moderately accessed data.
	TierWarm TierType = "warm"
	// TierCold is for infrequently accessed data.
	TierCold TierType = "cold"
	// TierArchive is for rarely accessed data.
	TierArchive TierType = "archive"
)

// TransitionAction defines what happens during a transition.
type TransitionAction string

const (
	// TransitionMove moves the object to the target tier.
	TransitionMove TransitionAction = "move"
	// TransitionCopy copies the object to the target tier.
	TransitionCopy TransitionAction = "copy"
)

// Policy defines tiering rules for objects.
type Policy struct {
	// 8-byte fields (slices)
	Rules []TransitionRule `json:"rules" yaml:"rules"`
	// Structs
	Filter PolicyFilter `json:"filter" yaml:"filter"`
	// Strings
	Name        string `json:"name"                  yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// 4-byte fields (int)
	Priority int `json:"priority" yaml:"priority"`
	// 1-byte fields (bool)
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// PolicyFilter defines criteria for selecting objects.
type PolicyFilter struct {
	Tags         map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Prefix       string            `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Suffix       string            `json:"suffix,omitempty" yaml:"suffix,omitempty"`
	Buckets      []string          `json:"buckets,omitempty" yaml:"buckets,omitempty"`
	ContentTypes []string          `json:"contentTypes,omitempty" yaml:"contentTypes,omitempty"`
	MinSize      int64             `json:"minSize,omitempty" yaml:"minSize,omitempty"`
	MaxSize      int64             `json:"maxSize,omitempty" yaml:"maxSize,omitempty"`
}

// TransitionRule defines when and where to move objects.
type TransitionRule struct {
	// Name of the transition rule
	Name string `json:"name" yaml:"name"`

	// TargetTier is the destination tier
	TargetTier TierType `json:"targetTier" yaml:"targetTier"`

	// TargetStorageClass is the S3 storage class to assign
	TargetStorageClass StorageClass `json:"targetStorageClass" yaml:"targetStorageClass"`

	// Action determines if object is moved or copied
	Action TransitionAction `json:"action" yaml:"action"`

	// Conditions that must be met for transition
	Conditions TransitionConditions `json:"conditions" yaml:"conditions"`
}

// TransitionConditions defines when a transition should occur.
type TransitionConditions struct {
	// DaysAfterCreation triggers after N days since creation
	DaysAfterCreation int `json:"daysAfterCreation,omitempty" yaml:"daysAfterCreation,omitempty"`

	// DaysAfterLastAccess triggers after N days since last access
	DaysAfterLastAccess int `json:"daysAfterLastAccess,omitempty" yaml:"daysAfterLastAccess,omitempty"`

	// DaysAfterModification triggers after N days since last modification
	DaysAfterModification int `json:"daysAfterModification,omitempty" yaml:"daysAfterModification,omitempty"`

	// AccessCountThreshold triggers when access count is below this value
	AccessCountThreshold int `json:"accessCountThreshold,omitempty" yaml:"accessCountThreshold,omitempty"`

	// AccessCountPeriodDays is the period to count accesses over
	AccessCountPeriodDays int `json:"accessCountPeriodDays,omitempty" yaml:"accessCountPeriodDays,omitempty"`
}

// ObjectInfo contains metadata about an object for policy evaluation.
type ObjectInfo struct {
	CreatedAt      time.Time
	ModifiedAt     time.Time
	LastAccessedAt time.Time
	Tags           map[string]string
	Bucket         string
	Key            string
	ContentType    string
	StorageClass   StorageClass
	CurrentTier    TierType
	Size           int64
	AccessCount    int
}

// PolicyEvaluator evaluates policies against objects.
type PolicyEvaluator struct {
	policies []Policy
}

// NewPolicyEvaluator creates a new policy evaluator.
func NewPolicyEvaluator(policies []Policy) *PolicyEvaluator {
	// Sort by priority
	sortedPolicies := make([]Policy, len(policies))
	copy(sortedPolicies, policies)

	for i := range len(sortedPolicies) - 1 {
		for j := i + 1; j < len(sortedPolicies); j++ {
			if sortedPolicies[j].Priority < sortedPolicies[i].Priority {
				sortedPolicies[i], sortedPolicies[j] = sortedPolicies[j], sortedPolicies[i]
			}
		}
	}

	return &PolicyEvaluator{policies: sortedPolicies}
}

// EvaluateResult contains the result of policy evaluation.
type EvaluateResult struct {
	Policy             *Policy
	Rule               *TransitionRule
	TargetTier         TierType
	TargetStorageClass StorageClass
	Action             TransitionAction
	ShouldTransition   bool
}

// Evaluate evaluates all policies for an object.
func (e *PolicyEvaluator) Evaluate(obj ObjectInfo) *EvaluateResult {
	for _, policy := range e.policies {
		if !policy.Enabled {
			continue
		}

		if !e.matchesFilter(obj, policy.Filter) {
			continue
		}

		for _, rule := range policy.Rules {
			if e.matchesConditions(obj, rule.Conditions) {
				return &EvaluateResult{
					ShouldTransition:   true,
					Policy:             &policy,
					Rule:               &rule,
					TargetTier:         rule.TargetTier,
					TargetStorageClass: rule.TargetStorageClass,
					Action:             rule.Action,
				}
			}
		}
	}

	return &EvaluateResult{ShouldTransition: false}
}

// matchesFilter checks if an object matches the policy filter.
func (e *PolicyEvaluator) matchesFilter(obj ObjectInfo, filter PolicyFilter) bool {
	// Check buckets
	if len(filter.Buckets) > 0 {
		matched := false

		for _, pattern := range filter.Buckets {
			if matchWildcard(pattern, obj.Bucket) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	// Check prefix
	if filter.Prefix != "" && !strings.HasPrefix(obj.Key, filter.Prefix) {
		return false
	}

	// Check suffix
	if filter.Suffix != "" && !strings.HasSuffix(obj.Key, filter.Suffix) {
		return false
	}

	// Check size constraints
	if filter.MinSize > 0 && obj.Size < filter.MinSize {
		return false
	}

	if filter.MaxSize > 0 && obj.Size > filter.MaxSize {
		return false
	}

	// Check content types
	if len(filter.ContentTypes) > 0 {
		matched := false

		for _, pattern := range filter.ContentTypes {
			if matchContentType(pattern, obj.ContentType) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	// Check tags
	if len(filter.Tags) > 0 {
		for key, value := range filter.Tags {
			if objValue, ok := obj.Tags[key]; !ok || objValue != value {
				return false
			}
		}
	}

	return true
}

// matchesConditions checks if an object matches the transition conditions.
func (e *PolicyEvaluator) matchesConditions(obj ObjectInfo, conditions TransitionConditions) bool {
	now := time.Now()

	// Check days after creation
	if conditions.DaysAfterCreation > 0 {
		daysSinceCreation := int(now.Sub(obj.CreatedAt).Hours() / 24)
		if daysSinceCreation < conditions.DaysAfterCreation {
			return false
		}
	}

	// Check days after last access
	if conditions.DaysAfterLastAccess > 0 {
		daysSinceAccess := int(now.Sub(obj.LastAccessedAt).Hours() / 24)
		if daysSinceAccess < conditions.DaysAfterLastAccess {
			return false
		}
	}

	// Check days after modification
	if conditions.DaysAfterModification > 0 {
		daysSinceModification := int(now.Sub(obj.ModifiedAt).Hours() / 24)
		if daysSinceModification < conditions.DaysAfterModification {
			return false
		}
	}

	// Check access count threshold
	if conditions.AccessCountThreshold > 0 && conditions.AccessCountPeriodDays > 0 {
		if obj.AccessCount >= conditions.AccessCountThreshold {
			return false
		}
	}

	return true
}

// matchWildcard matches a string against a pattern with wildcards.
func matchWildcard(pattern, s string) bool {
	if pattern == "*" {
		return true
	}

	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, "?") {
		return pattern == s
	}

	// Convert wildcard to regex
	regexPattern := "^" + regexp.QuoteMeta(pattern) + "$"
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, ".*")
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, ".")

	matched, _ := regexp.MatchString(regexPattern, s)

	return matched
}

// matchContentType matches content type with wildcards (e.g., "image/*").
func matchContentType(pattern, contentType string) bool {
	if pattern == "*" || pattern == "*/*" {
		return true
	}

	patternParts := strings.Split(pattern, "/")
	typeParts := strings.Split(contentType, "/")

	if len(patternParts) != 2 || len(typeParts) != 2 {
		return pattern == contentType
	}

	// Check main type
	if patternParts[0] != "*" && patternParts[0] != typeParts[0] {
		return false
	}

	// Check subtype
	if patternParts[1] != "*" && patternParts[1] != typeParts[1] {
		return false
	}

	return true
}

// DefaultPolicies returns a set of common tiering policies.
func DefaultPolicies() []Policy {
	return []Policy{
		{
			Name:        "archive-old-objects",
			Description: "Move objects not accessed for 90 days to archive tier",
			Enabled:     true,
			Priority:    100,
			Filter:      PolicyFilter{},
			Rules: []TransitionRule{
				{
					Name:               "to-glacier",
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
			Name:        "infrequent-access",
			Description: "Move objects not accessed for 30 days to cold tier",
			Enabled:     true,
			Priority:    50,
			Filter:      PolicyFilter{},
			Rules: []TransitionRule{
				{
					Name:               "to-standard-ia",
					TargetTier:         TierCold,
					TargetStorageClass: StorageClassStandardIA,
					Action:             TransitionMove,
					Conditions: TransitionConditions{
						DaysAfterLastAccess: 30,
					},
				},
			},
		},
		{
			Name:        "log-archival",
			Description: "Archive log files after 7 days",
			Enabled:     true,
			Priority:    25,
			Filter: PolicyFilter{
				Suffix: ".log",
			},
			Rules: []TransitionRule{
				{
					Name:               "archive-logs",
					TargetTier:         TierArchive,
					TargetStorageClass: StorageClassGlacier,
					Action:             TransitionMove,
					Conditions: TransitionConditions{
						DaysAfterCreation: 7,
					},
				},
			},
		},
	}
}
