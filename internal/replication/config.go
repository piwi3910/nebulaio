package replication

import (
	"errors"
	"regexp"
	"time"
)

// RuleStatus represents the status of a replication rule
type RuleStatus string

const (
	RuleStatusEnabled  RuleStatus = "Enabled"
	RuleStatusDisabled RuleStatus = "Disabled"
)

// Config holds replication configuration for a bucket
type Config struct {
	// Role is the IAM role for replication
	Role string `json:"role" yaml:"role"`
	// Rules is the list of replication rules
	Rules []Rule `json:"rules" yaml:"rules"`
}

// Rule represents a single replication rule
type Rule struct {
	// ID is the unique identifier for this rule
	ID string `json:"id" yaml:"id"`
	// Priority is the rule priority (lower = higher priority)
	Priority int `json:"priority" yaml:"priority"`
	// Status is whether the rule is enabled or disabled
	Status RuleStatus `json:"status" yaml:"status"`
	// Filter restricts which objects are replicated
	Filter *Filter `json:"filter,omitempty" yaml:"filter,omitempty"`
	// Destination specifies where to replicate
	Destination Destination `json:"destination" yaml:"destination"`
	// DeleteMarkerReplication controls replication of delete markers
	DeleteMarkerReplication *DeleteMarkerReplication `json:"deleteMarkerReplication,omitempty" yaml:"deleteMarkerReplication,omitempty"`
	// ExistingObjectReplication controls replication of existing objects
	ExistingObjectReplication *ExistingObjectReplication `json:"existingObjectReplication,omitempty" yaml:"existingObjectReplication,omitempty"`
}

// Filter restricts which objects are replicated
type Filter struct {
	// Prefix restricts replication to objects with this prefix
	Prefix string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	// Tags restricts replication to objects with these tags
	Tags map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// Destination specifies the replication target
type Destination struct {
	// Bucket is the destination bucket ARN or name
	Bucket string `json:"bucket" yaml:"bucket"`
	// Endpoint is the S3 endpoint URL
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// AccessKey for destination
	AccessKey string `json:"accessKey" yaml:"accessKey"`
	// SecretKey for destination
	SecretKey string `json:"secretKey" yaml:"secretKey"`
	// Region is the destination region
	Region string `json:"region,omitempty" yaml:"region,omitempty"`
	// StorageClass for replicated objects
	StorageClass string `json:"storageClass,omitempty" yaml:"storageClass,omitempty"`
	// UseSSL enables HTTPS for destination
	UseSSL bool `json:"useSSL" yaml:"useSSL"`
	// EncryptionConfiguration for server-side encryption
	EncryptionConfiguration *EncryptionConfig `json:"encryptionConfiguration,omitempty" yaml:"encryptionConfiguration,omitempty"`
}

// EncryptionConfig holds encryption settings for replicated objects
type EncryptionConfig struct {
	// ReplicaKmsKeyID is the KMS key ID for encrypting replicas
	ReplicaKmsKeyID string `json:"replicaKmsKeyId,omitempty" yaml:"replicaKmsKeyId,omitempty"`
}

// DeleteMarkerReplication controls whether delete markers are replicated
type DeleteMarkerReplication struct {
	Status RuleStatus `json:"status" yaml:"status"`
}

// ExistingObjectReplication controls replication of existing objects
type ExistingObjectReplication struct {
	Status RuleStatus `json:"status" yaml:"status"`
}

// Validate checks if the config is valid
func (c *Config) Validate() error {
	if len(c.Rules) == 0 {
		return errors.New("at least one replication rule is required")
	}

	ids := make(map[string]bool)
	for i, rule := range c.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if ids[rule.ID] {
			return errors.New("duplicate rule ID: " + rule.ID)
		}
		ids[rule.ID] = true
		c.Rules[i] = rule
	}

	return nil
}

// Validate checks if the rule is valid
func (r *Rule) Validate() error {
	if r.ID == "" {
		return errors.New("rule ID is required")
	}

	// ID must be alphanumeric with dashes and underscores
	idRegex := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	if !idRegex.MatchString(r.ID) {
		return errors.New("rule ID must be alphanumeric with dashes and underscores")
	}

	if r.Status != RuleStatusEnabled && r.Status != RuleStatusDisabled {
		return errors.New("rule status must be Enabled or Disabled")
	}

	if err := r.Destination.Validate(); err != nil {
		return err
	}

	return nil
}

// Validate checks if the destination is valid
func (d *Destination) Validate() error {
	if d.Bucket == "" {
		return errors.New("destination bucket is required")
	}

	if d.Endpoint == "" {
		return errors.New("destination endpoint is required")
	}

	if d.AccessKey == "" || d.SecretKey == "" {
		return errors.New("destination credentials are required")
	}

	return nil
}

// Matches checks if an object matches this filter
func (f *Filter) Matches(key string, tags map[string]string) bool {
	if f == nil {
		return true
	}

	// Check prefix
	if f.Prefix != "" {
		if len(key) < len(f.Prefix) || key[:len(f.Prefix)] != f.Prefix {
			return false
		}
	}

	// Check tags
	if len(f.Tags) > 0 {
		for k, v := range f.Tags {
			if tags[k] != v {
				return false
			}
		}
	}

	return true
}

// IsEnabled returns true if the rule is enabled
func (r *Rule) IsEnabled() bool {
	return r.Status == RuleStatusEnabled
}

// ShouldReplicateDeleteMarkers returns true if delete markers should be replicated
func (r *Rule) ShouldReplicateDeleteMarkers() bool {
	return r.DeleteMarkerReplication != nil && r.DeleteMarkerReplication.Status == RuleStatusEnabled
}

// ShouldReplicateExistingObjects returns true if existing objects should be replicated
func (r *Rule) ShouldReplicateExistingObjects() bool {
	return r.ExistingObjectReplication != nil && r.ExistingObjectReplication.Status == RuleStatusEnabled
}

// QueueItem represents an item in the replication queue
type QueueItem struct {
	// ID is the unique identifier for this queue item
	ID string `json:"id"`
	// Bucket is the source bucket
	Bucket string `json:"bucket"`
	// Key is the object key
	Key string `json:"key"`
	// VersionID is the object version ID
	VersionID string `json:"versionId,omitempty"`
	// Operation is the type of operation (PUT, DELETE)
	Operation string `json:"operation"`
	// RuleID is the replication rule that triggered this
	RuleID string `json:"ruleId"`
	// CreatedAt is when the item was queued
	CreatedAt time.Time `json:"createdAt"`
	// RetryCount is the number of retry attempts
	RetryCount int `json:"retryCount"`
	// LastError is the last error message
	LastError string `json:"lastError,omitempty"`
	// Status is the current status
	Status QueueItemStatus `json:"status"`
}

// QueueItemStatus represents the status of a queue item
type QueueItemStatus string

const (
	QueueStatusPending   QueueItemStatus = "pending"
	QueueStatusProgress  QueueItemStatus = "in_progress"
	QueueStatusCompleted QueueItemStatus = "completed"
	QueueStatusFailed    QueueItemStatus = "failed"
)

// ReplicationStatus holds the replication status for an object
type ReplicationStatus struct {
	// Status is the overall replication status
	Status string `json:"status"`
	// Destinations maps destination bucket to its status
	Destinations map[string]DestinationStatus `json:"destinations,omitempty"`
}

// DestinationStatus holds replication status for a specific destination
type DestinationStatus struct {
	// Status is the replication status for this destination
	Status string `json:"status"`
	// LastReplicationTime is when replication last succeeded
	LastReplicationTime *time.Time `json:"lastReplicationTime,omitempty"`
	// FailedAttempts is the number of failed replication attempts
	FailedAttempts int `json:"failedAttempts,omitempty"`
}

// Replication status values (S3 compatible)
const (
	ReplicationStatusPending   = "PENDING"
	ReplicationStatusComplete  = "COMPLETE"
	ReplicationStatusFailed    = "FAILED"
	ReplicationStatusReplica   = "REPLICA"
	ReplicationStatusNoReplication = ""
)
