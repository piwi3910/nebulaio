package lifecycle

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// LifecycleConfiguration represents the complete lifecycle configuration for a bucket
type LifecycleConfiguration struct {
	XMLName xml.Name        `xml:"LifecycleConfiguration" json:"-"`
	Rules   []LifecycleRule `xml:"Rule" json:"rules"`
}

// LifecycleRule represents a single lifecycle rule
type LifecycleRule struct {
	ID                             string                          `xml:"ID" json:"id"`
	Status                         string                          `xml:"Status" json:"status"` // Enabled/Disabled
	Filter                         Filter                          `xml:"Filter" json:"filter"`
	Expiration                     *Expiration                     `xml:"Expiration,omitempty" json:"expiration,omitempty"`
	Transition                     []Transition                    `xml:"Transition,omitempty" json:"transitions,omitempty"`
	NoncurrentVersionExpiration    *NoncurrentVersionExpiration    `xml:"NoncurrentVersionExpiration,omitempty" json:"noncurrentVersionExpiration,omitempty"`
	NoncurrentVersionTransition    []NoncurrentVersionTransition   `xml:"NoncurrentVersionTransition,omitempty" json:"noncurrentVersionTransitions,omitempty"`
	AbortIncompleteMultipartUpload *AbortIncompleteMultipartUpload `xml:"AbortIncompleteMultipartUpload,omitempty" json:"abortIncompleteMultipartUpload,omitempty"`
}

// Filter specifies which objects the rule applies to
type Filter struct {
	Prefix string `xml:"Prefix,omitempty" json:"prefix,omitempty"`
	Tag    *Tag   `xml:"Tag,omitempty" json:"tag,omitempty"`
	And    *And   `xml:"And,omitempty" json:"and,omitempty"`
}

// Tag represents a key-value tag filter
type Tag struct {
	Key   string `xml:"Key" json:"key"`
	Value string `xml:"Value" json:"value"`
}

// And combines multiple filter conditions
type And struct {
	Prefix string `xml:"Prefix,omitempty" json:"prefix,omitempty"`
	Tags   []Tag  `xml:"Tag,omitempty" json:"tags,omitempty"`
}

// Expiration specifies when objects should be deleted
type Expiration struct {
	Days                      int       `xml:"Days,omitempty" json:"days,omitempty"`
	Date                      time.Time `xml:"Date,omitempty" json:"date,omitempty"`
	ExpiredObjectDeleteMarker bool      `xml:"ExpiredObjectDeleteMarker,omitempty" json:"expiredObjectDeleteMarker,omitempty"`
}

// Transition specifies when objects should transition to a different storage class
type Transition struct {
	Days         int       `xml:"Days,omitempty" json:"days,omitempty"`
	Date         time.Time `xml:"Date,omitempty" json:"date,omitempty"`
	StorageClass string    `xml:"StorageClass" json:"storageClass"`
}

// NoncurrentVersionExpiration specifies when noncurrent versions should be deleted
type NoncurrentVersionExpiration struct {
	NoncurrentDays          int `xml:"NoncurrentDays,omitempty" json:"noncurrentDays,omitempty"`
	NewerNoncurrentVersions int `xml:"NewerNoncurrentVersions,omitempty" json:"newerNoncurrentVersions,omitempty"`
}

// NoncurrentVersionTransition specifies when noncurrent versions should transition
type NoncurrentVersionTransition struct {
	NoncurrentDays int    `xml:"NoncurrentDays" json:"noncurrentDays"`
	StorageClass   string `xml:"StorageClass" json:"storageClass"`
}

// AbortIncompleteMultipartUpload specifies when to abort incomplete multipart uploads
type AbortIncompleteMultipartUpload struct {
	DaysAfterInitiation int `xml:"DaysAfterInitiation" json:"daysAfterInitiation"`
}

// Action represents the action to take on an object
type Action int

const (
	// ActionNone means no action should be taken
	ActionNone Action = iota
	// ActionDelete means the object should be deleted
	ActionDelete
	// ActionTransition means the object should be transitioned to a different storage class
	ActionTransition
	// ActionDeleteMarker means a delete marker should be removed
	ActionDeleteMarker
	// ActionAbortMultipart means the multipart upload should be aborted
	ActionAbortMultipart
)

// ActionResult contains the result of evaluating a lifecycle rule
type ActionResult struct {
	Action       Action
	TargetClass  string // For transition actions
	RuleID       string
	DeleteMarker bool // Whether this is a delete marker cleanup
}

// ParseLifecycleConfig parses XML lifecycle configuration
func ParseLifecycleConfig(data []byte) (*LifecycleConfiguration, error) {
	var config LifecycleConfiguration
	if err := xml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse lifecycle configuration: %w", err)
	}
	return &config, nil
}

// Validate validates the lifecycle configuration
func (c *LifecycleConfiguration) Validate() error {
	if len(c.Rules) == 0 {
		return fmt.Errorf("lifecycle configuration must have at least one rule")
	}

	if len(c.Rules) > 1000 {
		return fmt.Errorf("lifecycle configuration cannot have more than 1000 rules")
	}

	seenIDs := make(map[string]bool)
	for i, rule := range c.Rules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("rule %d: %w", i, err)
		}

		// Check for duplicate IDs
		if rule.ID != "" {
			if seenIDs[rule.ID] {
				return fmt.Errorf("duplicate rule ID: %s", rule.ID)
			}
			seenIDs[rule.ID] = true
		}
	}

	return nil
}

// Validate validates a single lifecycle rule
func (r *LifecycleRule) Validate() error {
	// Validate ID
	if len(r.ID) > 255 {
		return fmt.Errorf("rule ID must be 255 characters or less")
	}

	// Validate status
	status := strings.ToLower(r.Status)
	if status != "enabled" && status != "disabled" {
		return fmt.Errorf("status must be 'Enabled' or 'Disabled', got '%s'", r.Status)
	}

	// Validate filter
	if err := r.Filter.Validate(); err != nil {
		return fmt.Errorf("invalid filter: %w", err)
	}

	// Must have at least one action
	hasAction := r.Expiration != nil ||
		len(r.Transition) > 0 ||
		r.NoncurrentVersionExpiration != nil ||
		len(r.NoncurrentVersionTransition) > 0 ||
		r.AbortIncompleteMultipartUpload != nil

	if !hasAction {
		return fmt.Errorf("rule must have at least one action (Expiration, Transition, NoncurrentVersionExpiration, NoncurrentVersionTransition, or AbortIncompleteMultipartUpload)")
	}

	// Validate expiration
	if r.Expiration != nil {
		if err := r.Expiration.Validate(); err != nil {
			return fmt.Errorf("invalid expiration: %w", err)
		}
	}

	// Validate transitions
	for i, t := range r.Transition {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("invalid transition %d: %w", i, err)
		}
	}

	// Validate noncurrent version expiration
	if r.NoncurrentVersionExpiration != nil {
		if err := r.NoncurrentVersionExpiration.Validate(); err != nil {
			return fmt.Errorf("invalid noncurrent version expiration: %w", err)
		}
	}

	// Validate noncurrent version transitions
	for i, t := range r.NoncurrentVersionTransition {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("invalid noncurrent version transition %d: %w", i, err)
		}
	}

	// Validate abort incomplete multipart upload
	if r.AbortIncompleteMultipartUpload != nil {
		if err := r.AbortIncompleteMultipartUpload.Validate(); err != nil {
			return fmt.Errorf("invalid abort incomplete multipart upload: %w", err)
		}
	}

	return nil
}

// Validate validates a filter
func (f *Filter) Validate() error {
	hasPrefix := f.Prefix != ""
	hasTag := f.Tag != nil
	hasAnd := f.And != nil

	// Can only have one of prefix/tag or And
	if hasAnd && (hasPrefix || hasTag) {
		return fmt.Errorf("cannot use And with Prefix or Tag at the same level")
	}

	// Validate tag
	if hasTag {
		if f.Tag.Key == "" {
			return fmt.Errorf("tag key cannot be empty")
		}
	}

	// Validate And
	if hasAnd {
		if len(f.And.Tags) == 0 && f.And.Prefix == "" {
			return fmt.Errorf("And filter must have at least a prefix or one tag")
		}
		for i, tag := range f.And.Tags {
			if tag.Key == "" {
				return fmt.Errorf("tag %d in And filter has empty key", i)
			}
		}
	}

	return nil
}

// Validate validates an expiration configuration
func (e *Expiration) Validate() error {
	hasDays := e.Days > 0
	hasDate := !e.Date.IsZero()
	hasDeleteMarker := e.ExpiredObjectDeleteMarker

	// Days and Date are mutually exclusive
	if hasDays && hasDate {
		return fmt.Errorf("cannot specify both Days and Date")
	}

	// ExpiredObjectDeleteMarker is mutually exclusive with Days and Date
	if hasDeleteMarker && (hasDays || hasDate) {
		return fmt.Errorf("ExpiredObjectDeleteMarker cannot be used with Days or Date")
	}

	// Must have at least one
	if !hasDays && !hasDate && !hasDeleteMarker {
		return fmt.Errorf("expiration must have Days, Date, or ExpiredObjectDeleteMarker")
	}

	// Validate days
	if hasDays && e.Days < 1 {
		return fmt.Errorf("days must be a positive integer")
	}

	return nil
}

// Validate validates a transition configuration
func (t *Transition) Validate() error {
	hasDays := t.Days > 0
	hasDate := !t.Date.IsZero()

	// Must have exactly one of Days or Date
	if hasDays == hasDate {
		return fmt.Errorf("transition must have exactly one of Days or Date")
	}

	// Validate days
	if hasDays && t.Days < 0 {
		return fmt.Errorf("days must be a positive integer")
	}

	// Must have storage class
	if t.StorageClass == "" {
		return fmt.Errorf("storage class is required")
	}

	// Validate storage class
	validClasses := map[string]bool{
		"STANDARD":            true,
		"STANDARD_IA":         true,
		"ONEZONE_IA":          true,
		"INTELLIGENT_TIERING": true,
		"GLACIER":             true,
		"GLACIER_IR":          true,
		"DEEP_ARCHIVE":        true,
	}
	if !validClasses[t.StorageClass] {
		return fmt.Errorf("invalid storage class: %s", t.StorageClass)
	}

	return nil
}

// Validate validates a noncurrent version expiration configuration
func (n *NoncurrentVersionExpiration) Validate() error {
	if n.NoncurrentDays < 1 && n.NewerNoncurrentVersions < 1 {
		return fmt.Errorf("must specify NoncurrentDays or NewerNoncurrentVersions")
	}
	return nil
}

// Validate validates a noncurrent version transition configuration
func (n *NoncurrentVersionTransition) Validate() error {
	if n.NoncurrentDays < 0 {
		return fmt.Errorf("noncurrent days must be a positive integer")
	}

	if n.StorageClass == "" {
		return fmt.Errorf("storage class is required")
	}

	return nil
}

// Validate validates an abort incomplete multipart upload configuration
func (a *AbortIncompleteMultipartUpload) Validate() error {
	if a.DaysAfterInitiation < 1 {
		return fmt.Errorf("days after initiation must be a positive integer")
	}
	return nil
}

// IsEnabled returns true if the rule is enabled
func (r *LifecycleRule) IsEnabled() bool {
	return strings.EqualFold(r.Status, "Enabled")
}

// MatchesObject returns true if the rule's filter matches the given object
func (r *LifecycleRule) MatchesObject(key string, tags map[string]string) bool {
	return r.Filter.Matches(key, tags)
}

// Matches returns true if the filter matches the given object key and tags
func (f *Filter) Matches(key string, tags map[string]string) bool {
	// Check prefix
	if f.Prefix != "" && !strings.HasPrefix(key, f.Prefix) {
		return false
	}

	// Check single tag
	if f.Tag != nil {
		if tags == nil {
			return false
		}
		if val, ok := tags[f.Tag.Key]; !ok || val != f.Tag.Value {
			return false
		}
	}

	// Check And conditions
	if f.And != nil {
		// Check And prefix
		if f.And.Prefix != "" && !strings.HasPrefix(key, f.And.Prefix) {
			return false
		}

		// Check And tags
		for _, tag := range f.And.Tags {
			if tags == nil {
				return false
			}
			if val, ok := tags[tag.Key]; !ok || val != tag.Value {
				return false
			}
		}
	}

	return true
}

// ToXML serializes the configuration to XML
func (c *LifecycleConfiguration) ToXML() ([]byte, error) {
	return xml.MarshalIndent(c, "", "  ")
}
