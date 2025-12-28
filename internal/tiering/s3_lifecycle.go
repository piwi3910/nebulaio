package tiering

import (
	"encoding/xml"
	"fmt"
	"time"
)

// S3LifecycleAdapter converts S3 Lifecycle policies to NebulaIO policies
type S3LifecycleAdapter struct{}

// NewS3LifecycleAdapter creates a new S3 lifecycle adapter
func NewS3LifecycleAdapter() *S3LifecycleAdapter {
	return &S3LifecycleAdapter{}
}

// S3LifecycleConfiguration represents an S3 lifecycle configuration
type S3LifecycleConfiguration struct {
	XMLName xml.Name          `xml:"LifecycleConfiguration"`
	Rules   []S3LifecycleRule `xml:"Rule"`
}

// S3LifecycleRule represents an S3 lifecycle rule
type S3LifecycleRule struct {
	ID                             string                          `xml:"ID"`
	Status                         string                          `xml:"Status"` // Enabled or Disabled
	Filter                         *S3LifecycleFilter              `xml:"Filter,omitempty"`
	Prefix                         string                          `xml:"Prefix,omitempty"` // Deprecated, use Filter
	Transitions                    []S3LifecycleTransition         `xml:"Transition,omitempty"`
	Expiration                     *S3LifecycleExpiration          `xml:"Expiration,omitempty"`
	NoncurrentVersionTransition    []S3NoncurrentVersionTransition `xml:"NoncurrentVersionTransition,omitempty"`
	NoncurrentVersionExpiration    *S3NoncurrentVersionExpiration  `xml:"NoncurrentVersionExpiration,omitempty"`
	AbortIncompleteMultipartUpload *S3AbortIncompleteUpload        `xml:"AbortIncompleteMultipartUpload,omitempty"`
}

// S3LifecycleFilter defines the filter for a lifecycle rule
type S3LifecycleFilter struct {
	Prefix string       `xml:"Prefix,omitempty"`
	Tag    *S3Tag       `xml:"Tag,omitempty"`
	And    *S3FilterAnd `xml:"And,omitempty"`
}

// S3Tag represents an S3 tag
type S3Tag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

// S3FilterAnd combines multiple filter conditions
type S3FilterAnd struct {
	Prefix string  `xml:"Prefix,omitempty"`
	Tags   []S3Tag `xml:"Tag,omitempty"`
}

// S3LifecycleTransition defines a transition rule
type S3LifecycleTransition struct {
	Days         int    `xml:"Days,omitempty"`
	Date         string `xml:"Date,omitempty"` // ISO 8601 format
	StorageClass string `xml:"StorageClass"`
}

// S3LifecycleExpiration defines an expiration rule
type S3LifecycleExpiration struct {
	Days                      int    `xml:"Days,omitempty"`
	Date                      string `xml:"Date,omitempty"`
	ExpiredObjectDeleteMarker bool   `xml:"ExpiredObjectDeleteMarker,omitempty"`
}

// S3NoncurrentVersionTransition defines transition for non-current versions
type S3NoncurrentVersionTransition struct {
	NoncurrentDays          int    `xml:"NoncurrentDays"`
	NewerNoncurrentVersions int    `xml:"NewerNoncurrentVersions,omitempty"`
	StorageClass            string `xml:"StorageClass"`
}

// S3NoncurrentVersionExpiration defines expiration for non-current versions
type S3NoncurrentVersionExpiration struct {
	NoncurrentDays          int `xml:"NoncurrentDays"`
	NewerNoncurrentVersions int `xml:"NewerNoncurrentVersions,omitempty"`
}

// S3AbortIncompleteUpload defines abort rules for incomplete multipart uploads
type S3AbortIncompleteUpload struct {
	DaysAfterInitiation int `xml:"DaysAfterInitiation"`
}

// ConvertFromS3 converts an S3 lifecycle configuration to NebulaIO policies
func (a *S3LifecycleAdapter) ConvertFromS3(bucket string, config *S3LifecycleConfiguration) ([]*AdvancedPolicy, error) {
	var policies []*AdvancedPolicy

	for _, rule := range config.Rules {
		policy, err := a.convertRule(bucket, &rule)
		if err != nil {
			return nil, fmt.Errorf("failed to convert rule %s: %w", rule.ID, err)
		}
		policies = append(policies, policy)
	}

	return policies, nil
}

// convertRule converts a single S3 lifecycle rule to a NebulaIO policy
func (a *S3LifecycleAdapter) convertRule(bucket string, rule *S3LifecycleRule) (*AdvancedPolicy, error) {
	policy := &AdvancedPolicy{
		ID:          fmt.Sprintf("s3-lifecycle-%s-%s", bucket, rule.ID),
		Name:        fmt.Sprintf("S3 Lifecycle: %s", rule.ID),
		Description: fmt.Sprintf("Converted from S3 lifecycle rule %s for bucket %s", rule.ID, bucket),
		Type:        PolicyTypeS3Lifecycle,
		Scope:       PolicyScopeBucket,
		Enabled:     rule.Status == "Enabled",
		Priority:    100,
		Selector:    a.convertFilter(bucket, rule),
		Triggers:    []PolicyTrigger{},
		Actions:     []PolicyAction{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Version:     1,
	}

	// Convert transitions
	for _, transition := range rule.Transitions {
		trigger, action := a.convertTransition(&transition)
		policy.Triggers = append(policy.Triggers, trigger)
		policy.Actions = append(policy.Actions, action)
	}

	// Convert expiration
	if rule.Expiration != nil {
		trigger, action := a.convertExpiration(rule.Expiration)
		policy.Triggers = append(policy.Triggers, trigger)
		policy.Actions = append(policy.Actions, action)
	}

	// Convert non-current version transition
	for _, ncTransition := range rule.NoncurrentVersionTransition {
		trigger, action := a.convertNoncurrentTransition(&ncTransition)
		policy.Triggers = append(policy.Triggers, trigger)
		policy.Actions = append(policy.Actions, action)
	}

	// Convert non-current version expiration
	if rule.NoncurrentVersionExpiration != nil {
		trigger, action := a.convertNoncurrentExpiration(rule.NoncurrentVersionExpiration)
		policy.Triggers = append(policy.Triggers, trigger)
		policy.Actions = append(policy.Actions, action)
	}

	// If no triggers/actions, add a default cron trigger
	if len(policy.Triggers) == 0 {
		policy.Triggers = append(policy.Triggers, PolicyTrigger{
			Type: TriggerTypeCron,
			Cron: &CronTrigger{
				Expression: "0 2 * * *", // 2 AM daily
			},
		})
	}

	return policy, nil
}

// convertFilter converts S3 filter to NebulaIO selector
func (a *S3LifecycleAdapter) convertFilter(bucket string, rule *S3LifecycleRule) PolicySelector {
	selector := PolicySelector{
		Buckets: []string{bucket},
	}

	// Handle deprecated prefix
	if rule.Prefix != "" {
		selector.Prefixes = []string{rule.Prefix}
	}

	// Handle new filter
	if rule.Filter != nil {
		if rule.Filter.Prefix != "" {
			selector.Prefixes = []string{rule.Filter.Prefix}
		}

		if rule.Filter.Tag != nil {
			selector.Tags = map[string]string{
				rule.Filter.Tag.Key: rule.Filter.Tag.Value,
			}
		}

		if rule.Filter.And != nil {
			if rule.Filter.And.Prefix != "" {
				selector.Prefixes = []string{rule.Filter.And.Prefix}
			}
			if len(rule.Filter.And.Tags) > 0 {
				selector.Tags = make(map[string]string)
				for _, tag := range rule.Filter.And.Tags {
					selector.Tags[tag.Key] = tag.Value
				}
			}
		}
	}

	return selector
}

// convertTransition converts S3 transition to NebulaIO trigger and action
func (a *S3LifecycleAdapter) convertTransition(transition *S3LifecycleTransition) (PolicyTrigger, PolicyAction) {
	trigger := PolicyTrigger{
		Type: TriggerTypeAge,
		Age: &AgeTrigger{
			DaysSinceCreation: transition.Days,
		},
	}

	// Map S3 storage class to NebulaIO tier
	targetTier := a.mapStorageClassToTier(transition.StorageClass)

	action := PolicyAction{
		Type: ActionTransition,
		Transition: &TransitionActionConfig{
			TargetTier:         targetTier,
			TargetStorageClass: StorageClass(transition.StorageClass),
		},
	}

	return trigger, action
}

// convertExpiration converts S3 expiration to NebulaIO trigger and action
func (a *S3LifecycleAdapter) convertExpiration(expiration *S3LifecycleExpiration) (PolicyTrigger, PolicyAction) {
	trigger := PolicyTrigger{
		Type: TriggerTypeAge,
		Age: &AgeTrigger{
			DaysSinceCreation: expiration.Days,
		},
	}

	action := PolicyAction{
		Type: ActionDelete,
		Delete: &DeleteActionConfig{
			ExpireDeleteMarkers: expiration.ExpiredObjectDeleteMarker,
		},
	}

	return trigger, action
}

// convertNoncurrentTransition converts non-current version transition
func (a *S3LifecycleAdapter) convertNoncurrentTransition(transition *S3NoncurrentVersionTransition) (PolicyTrigger, PolicyAction) {
	trigger := PolicyTrigger{
		Type: TriggerTypeAge,
		Age: &AgeTrigger{
			DaysSinceCreation: transition.NoncurrentDays,
		},
	}

	targetTier := a.mapStorageClassToTier(transition.StorageClass)

	action := PolicyAction{
		Type: ActionTransition,
		Transition: &TransitionActionConfig{
			TargetTier:         targetTier,
			TargetStorageClass: StorageClass(transition.StorageClass),
		},
	}

	return trigger, action
}

// convertNoncurrentExpiration converts non-current version expiration
func (a *S3LifecycleAdapter) convertNoncurrentExpiration(expiration *S3NoncurrentVersionExpiration) (PolicyTrigger, PolicyAction) {
	trigger := PolicyTrigger{
		Type: TriggerTypeAge,
		Age: &AgeTrigger{
			DaysSinceCreation: expiration.NoncurrentDays,
		},
	}

	action := PolicyAction{
		Type: ActionDelete,
		Delete: &DeleteActionConfig{
			NonCurrentVersionDays: expiration.NoncurrentDays,
		},
	}

	return trigger, action
}

// mapStorageClassToTier maps S3 storage class to NebulaIO tier
func (a *S3LifecycleAdapter) mapStorageClassToTier(storageClass string) TierType {
	switch StorageClass(storageClass) {
	case StorageClassStandard:
		return TierHot
	case StorageClassStandardIA, StorageClassOneZoneIA:
		return TierWarm
	case StorageClassGlacier, StorageClassGlacierIR:
		return TierCold
	case StorageClassDeepArchive:
		return TierArchive
	case StorageClassIntelligentTiering:
		return TierHot // Will be managed by NebulaIO policies
	default:
		return TierWarm
	}
}

// ConvertToS3 converts NebulaIO policies to S3 lifecycle configuration
func (a *S3LifecycleAdapter) ConvertToS3(bucket string, policies []*AdvancedPolicy) (*S3LifecycleConfiguration, error) {
	config := &S3LifecycleConfiguration{
		Rules: make([]S3LifecycleRule, 0),
	}

	for _, policy := range policies {
		// Only convert bucket-scope policies that match this bucket
		if policy.Scope != PolicyScopeBucket {
			continue
		}

		bucketMatch := false
		for _, b := range policy.Selector.Buckets {
			if b == bucket || b == "*" {
				bucketMatch = true
				break
			}
		}
		if !bucketMatch {
			continue
		}

		rule := a.convertPolicyToRule(policy)
		config.Rules = append(config.Rules, rule)
	}

	return config, nil
}

// convertPolicyToRule converts a NebulaIO policy to S3 lifecycle rule
func (a *S3LifecycleAdapter) convertPolicyToRule(policy *AdvancedPolicy) S3LifecycleRule {
	rule := S3LifecycleRule{
		ID:     policy.ID,
		Status: "Disabled",
	}

	if policy.Enabled {
		rule.Status = "Enabled"
	}

	// Convert selector to filter
	rule.Filter = a.convertSelectorToFilter(policy.Selector)

	// Convert triggers and actions
	for i, trigger := range policy.Triggers {
		if i >= len(policy.Actions) {
			break
		}
		action := policy.Actions[i]

		if trigger.Type == TriggerTypeAge && trigger.Age != nil {
			if action.Type == ActionTransition && action.Transition != nil {
				rule.Transitions = append(rule.Transitions, S3LifecycleTransition{
					Days:         trigger.Age.DaysSinceCreation,
					StorageClass: string(action.Transition.TargetStorageClass),
				})
			} else if action.Type == ActionDelete {
				rule.Expiration = &S3LifecycleExpiration{
					Days: trigger.Age.DaysSinceCreation,
				}
				if action.Delete != nil {
					rule.Expiration.ExpiredObjectDeleteMarker = action.Delete.ExpireDeleteMarkers
				}
			}
		}
	}

	return rule
}

// convertSelectorToFilter converts NebulaIO selector to S3 filter
func (a *S3LifecycleAdapter) convertSelectorToFilter(selector PolicySelector) *S3LifecycleFilter {
	filter := &S3LifecycleFilter{}

	// Handle prefix
	if len(selector.Prefixes) > 0 {
		filter.Prefix = selector.Prefixes[0]
	}

	// Handle tags
	if len(selector.Tags) > 0 {
		if len(selector.Tags) == 1 && filter.Prefix == "" {
			// Single tag without prefix
			for k, v := range selector.Tags {
				filter.Tag = &S3Tag{Key: k, Value: v}
				break
			}
		} else {
			// Multiple conditions, use And
			filter.And = &S3FilterAnd{
				Prefix: filter.Prefix,
				Tags:   make([]S3Tag, 0, len(selector.Tags)),
			}
			for k, v := range selector.Tags {
				filter.And.Tags = append(filter.And.Tags, S3Tag{Key: k, Value: v})
			}
			filter.Prefix = ""
		}
	}

	return filter
}

// ParseS3LifecycleXML parses S3 lifecycle XML configuration
func ParseS3LifecycleXML(data []byte) (*S3LifecycleConfiguration, error) {
	var config S3LifecycleConfiguration
	if err := xml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse lifecycle configuration: %w", err)
	}
	return &config, nil
}

// ToS3LifecycleXML converts configuration to S3 XML format
func (config *S3LifecycleConfiguration) ToXML() ([]byte, error) {
	data, err := xml.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal lifecycle configuration: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}
