package replication

import (
	"errors"
	"regexp"
	"time"
)

// RuleStatus represents the status of a replication rule.
type RuleStatus string

const (
	RuleStatusEnabled  RuleStatus = "Enabled"
	RuleStatusDisabled RuleStatus = "Disabled"
)

// Config holds replication configuration for a bucket.
type Config struct {
	// Role is the IAM role for replication
	Role string `json:"role" yaml:"role"`
	// Rules is the list of replication rules
	Rules []Rule `json:"rules" yaml:"rules"`
}

// Rule represents a single replication rule.
type Rule struct {
	Destination               Destination                `json:"destination" yaml:"destination"`
	Filter                    *Filter                    `json:"filter,omitempty" yaml:"filter,omitempty"`
	DeleteMarkerReplication   *DeleteMarkerReplication   `json:"deleteMarkerReplication,omitempty" yaml:"deleteMarkerReplication,omitempty"`
	ExistingObjectReplication *ExistingObjectReplication `json:"existingObjectReplication,omitempty" yaml:"existingObjectReplication,omitempty"`
	ID                        string                     `json:"id" yaml:"id"`
	Status                    RuleStatus                 `json:"status" yaml:"status"`
	Priority                  int                        `json:"priority" yaml:"priority"`
}

// Filter restricts which objects are replicated.
type Filter struct {
	Tags   map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Prefix string            `json:"prefix,omitempty" yaml:"prefix,omitempty"`
}

// Destination specifies the replication target.
type Destination struct {
	EncryptionConfiguration *EncryptionConfig `json:"encryptionConfiguration,omitempty" yaml:"encryptionConfiguration,omitempty"`
	Bucket                  string            `json:"bucket" yaml:"bucket"`
	Endpoint                string            `json:"endpoint" yaml:"endpoint"`
	AccessKey               string            `json:"accessKey" yaml:"accessKey"`
	SecretKey               string            `json:"secretKey" yaml:"secretKey"`
	Region                  string            `json:"region,omitempty" yaml:"region,omitempty"`
	StorageClass            string            `json:"storageClass,omitempty" yaml:"storageClass,omitempty"`
	UseSSL                  bool              `json:"useSSL" yaml:"useSSL"`
}

// EncryptionConfig holds encryption settings for replicated objects.
type EncryptionConfig struct {
	// ReplicaKmsKeyID is the KMS key ID for encrypting replicas
	ReplicaKmsKeyID string `json:"replicaKmsKeyId,omitempty" yaml:"replicaKmsKeyId,omitempty"`
}

// DeleteMarkerReplication controls whether delete markers are replicated.
type DeleteMarkerReplication struct {
	Status RuleStatus `json:"status" yaml:"status"`
}

// ExistingObjectReplication controls replication of existing objects.
type ExistingObjectReplication struct {
	Status RuleStatus `json:"status" yaml:"status"`
}

// Validate checks if the config is valid.
func (c *Config) Validate() error {
	if len(c.Rules) == 0 {
		return errors.New("at least one replication rule is required")
	}

	ids := make(map[string]bool)

	for i, rule := range c.Rules {
		err := rule.Validate()
		if err != nil {
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

// Validate checks if the rule is valid.
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

	err := r.Destination.Validate()
	if err != nil {
		return err
	}

	return nil
}

// Validate checks if the destination is valid.
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

// Matches checks if an object matches this filter.
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

// IsEnabled returns true if the rule is enabled.
func (r *Rule) IsEnabled() bool {
	return r.Status == RuleStatusEnabled
}

// ShouldReplicateDeleteMarkers returns true if delete markers should be replicated.
func (r *Rule) ShouldReplicateDeleteMarkers() bool {
	return r.DeleteMarkerReplication != nil && r.DeleteMarkerReplication.Status == RuleStatusEnabled
}

// ShouldReplicateExistingObjects returns true if existing objects should be replicated.
func (r *Rule) ShouldReplicateExistingObjects() bool {
	return r.ExistingObjectReplication != nil && r.ExistingObjectReplication.Status == RuleStatusEnabled
}

// QueueItem represents an item in the replication queue.
type QueueItem struct {
	CreatedAt  time.Time       `json:"createdAt"`
	ID         string          `json:"id"`
	Bucket     string          `json:"bucket"`
	Key        string          `json:"key"`
	VersionID  string          `json:"versionId,omitempty"`
	Operation  string          `json:"operation"`
	RuleID     string          `json:"ruleId"`
	LastError  string          `json:"lastError,omitempty"`
	Status     QueueItemStatus `json:"status"`
	RetryCount int             `json:"retryCount"`
}

// QueueItemStatus represents the status of a queue item.
type QueueItemStatus string

const (
	QueueStatusPending   QueueItemStatus = "pending"
	QueueStatusProgress  QueueItemStatus = "in_progress"
	QueueStatusCompleted QueueItemStatus = "completed"
	QueueStatusFailed    QueueItemStatus = "failed"
)

// ReplicationStatus holds the replication status for an object.
type ReplicationStatus struct {
	Destinations map[string]DestinationStatus `json:"destinations,omitempty"`
	Status       string                       `json:"status"`
}

// DestinationStatus holds replication status for a specific destination.
type DestinationStatus struct {
	LastReplicationTime *time.Time `json:"lastReplicationTime,omitempty"`
	Status              string     `json:"status"`
	FailedAttempts      int        `json:"failedAttempts,omitempty"`
}

// Replication status values (S3 compatible).
const (
	ReplicationStatusPending       = "PENDING"
	ReplicationStatusComplete      = "COMPLETE"
	ReplicationStatusFailed        = "FAILED"
	ReplicationStatusReplica       = "REPLICA"
	ReplicationStatusNoReplication = ""
)
