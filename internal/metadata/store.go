// Package metadata provides the distributed metadata store for NebulaIO.
//
// The metadata store is the source of truth for all bucket and object metadata,
// user accounts, policies, and cluster configuration. It uses Dragonboat (Raft)
// for distributed consensus, ensuring strong consistency across cluster nodes.
//
// Key features:
//   - Strongly consistent via Raft consensus
//   - Linearizable reads and writes
//   - Automatic leader election and failover
//   - Incremental snapshots for fast recovery
//   - Backed by BadgerDB for persistent storage
//
// The Store interface abstracts the underlying implementation, allowing for
// testing with mock stores and future backend flexibility.
package metadata

import (
	"context"
	"fmt"
	"time"

	"github.com/piwi3910/nebulaio/internal/audit"
)

// Store is the interface for the metadata store
// All metadata operations go through this interface, which is backed by Raft.
type Store interface {
	// Close shuts down the store
	Close() error

	// IsLeader returns true if this node is the Raft leader
	IsLeader() bool

	// LeaderAddress returns the address of the current leader
	LeaderAddress(ctx context.Context) (string, error)

	// Bucket operations
	CreateBucket(ctx context.Context, bucket *Bucket) error
	GetBucket(ctx context.Context, name string) (*Bucket, error)
	DeleteBucket(ctx context.Context, name string) error
	ListBuckets(ctx context.Context, owner string) ([]*Bucket, error)
	UpdateBucket(ctx context.Context, bucket *Bucket) error

	// Object metadata operations
	PutObjectMeta(ctx context.Context, meta *ObjectMeta) error
	GetObjectMeta(ctx context.Context, bucket, key string) (*ObjectMeta, error)
	DeleteObjectMeta(ctx context.Context, bucket, key string) error
	ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*ObjectListing, error)

	// Version operations
	GetObjectVersion(ctx context.Context, bucket, key, versionID string) (*ObjectMeta, error)
	ListObjectVersions(ctx context.Context, bucket, prefix, delimiter, keyMarker, versionIDMarker string, maxKeys int) (*VersionListing, error)
	DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error
	PutObjectMetaVersioned(ctx context.Context, meta *ObjectMeta, preserveOldVersions bool) error

	// Multipart upload operations
	CreateMultipartUpload(ctx context.Context, upload *MultipartUpload) error
	GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*MultipartUpload, error)
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
	AddUploadPart(ctx context.Context, bucket, key, uploadID string, part *UploadPart) error
	ListMultipartUploads(ctx context.Context, bucket string) ([]*MultipartUpload, error)

	// User operations
	CreateUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	UpdateUser(ctx context.Context, user *User) error
	DeleteUser(ctx context.Context, id string) error
	ListUsers(ctx context.Context) ([]*User, error)

	// Access key operations
	CreateAccessKey(ctx context.Context, key *AccessKey) error
	GetAccessKey(ctx context.Context, accessKeyID string) (*AccessKey, error)
	DeleteAccessKey(ctx context.Context, accessKeyID string) error
	ListAccessKeys(ctx context.Context, userID string) ([]*AccessKey, error)

	// Policy operations
	CreatePolicy(ctx context.Context, policy *Policy) error
	GetPolicy(ctx context.Context, name string) (*Policy, error)
	UpdatePolicy(ctx context.Context, policy *Policy) error
	DeletePolicy(ctx context.Context, name string) error
	ListPolicies(ctx context.Context) ([]*Policy, error)

	// Cluster operations
	GetClusterInfo(ctx context.Context) (*ClusterInfo, error)
	AddNode(ctx context.Context, node *NodeInfo) error
	RemoveNode(ctx context.Context, nodeID string) error
	ListNodes(ctx context.Context) ([]*NodeInfo, error)

	// Audit operations
	StoreAuditEvent(ctx context.Context, event *audit.AuditEvent) error
	ListAuditEvents(ctx context.Context, filter audit.AuditFilter) (*audit.AuditListResult, error)
	DeleteOldAuditEvents(ctx context.Context, before time.Time) (int, error)
}

// Bucket represents a storage bucket.
type Bucket struct {
	CreatedAt         time.Time                `json:"created_at"`
	Encryption        *EncryptionConfig        `json:"encryption,omitempty"`
	Redundancy        *RedundancyConfig        `json:"redundancy,omitempty"`
	OwnershipControls *OwnershipControlsConfig `json:"ownership_controls,omitempty"`
	PublicAccessBlock *PublicAccessBlockConfig `json:"public_access_block,omitempty"`
	ObjectLockConfig  *ObjectLockConfig        `json:"object_lock_config,omitempty"`
	Replication       *ReplicationConfig       `json:"replication,omitempty"`
	Tags              map[string]string        `json:"tags,omitempty"`
	Notification      *NotificationConfig      `json:"notification,omitempty"`
	Logging           *LoggingConfig           `json:"logging,omitempty"`
	Website           *WebsiteConfig           `json:"website,omitempty"`
	ACL               *BucketACL               `json:"acl,omitempty"`
	StorageClass      string                   `json:"storage_class"`
	Policy            string                   `json:"policy,omitempty"`
	Name              string                   `json:"name"`
	Versioning        VersioningStatus         `json:"versioning"`
	Region            string                   `json:"region"`
	Accelerate        string                   `json:"accelerate,omitempty"`
	Owner             string                   `json:"owner"`
	Lifecycle         []LifecycleRule          `json:"lifecycle,omitempty"`
	CORS              []CORSRule               `json:"cors,omitempty"`
	ObjectLockEnabled bool                     `json:"object_lock_enabled"`
}

// VersioningStatus represents bucket versioning state.
type VersioningStatus string

const (
	VersioningDisabled  VersioningStatus = ""
	VersioningEnabled   VersioningStatus = "Enabled"
	VersioningSuspended VersioningStatus = "Suspended"
)

// CORSRule represents a CORS configuration rule.
type CORSRule struct {
	AllowedOrigins []string `json:"allowed_origins"`
	AllowedMethods []string `json:"allowed_methods"`
	AllowedHeaders []string `json:"allowed_headers"`
	ExposeHeaders  []string `json:"expose_headers"`
	MaxAgeSeconds  int      `json:"max_age_seconds"`
}

// LifecycleRule represents a lifecycle management rule.
type LifecycleRule struct {
	ID                              string                `json:"id"`
	Prefix                          string                `json:"prefix"`
	Transitions                     []LifecycleTransition `json:"transitions,omitempty"`
	ExpirationDays                  int                   `json:"expiration_days,omitempty"`
	NoncurrentVersionExpirationDays int                   `json:"noncurrent_version_expiration_days,omitempty"`
	Enabled                         bool                  `json:"enabled"`
}

// LifecycleTransition represents a storage class transition.
type LifecycleTransition struct {
	StorageClass string `json:"storage_class"`
	Days         int    `json:"days"`
}

// ACLGrant represents an ACL grant.
type ACLGrant struct {
	GranteeType string `json:"grantee_type"` // "CanonicalUser", "AmazonCustomerByEmail", "Group"
	GranteeID   string `json:"grantee_id"`   // Canonical user ID or email
	GranteeURI  string `json:"grantee_uri"`  // For group grants (e.g., "http://acs.amazonaws.com/groups/global/AllUsers")
	DisplayName string `json:"display_name"`
	Permission  string `json:"permission"` // "FULL_CONTROL", "WRITE", "WRITE_ACP", "READ", "READ_ACP"
}

// BucketACL represents a bucket's access control list.
type BucketACL struct {
	OwnerID          string     `json:"owner_id"`
	OwnerDisplayName string     `json:"owner_display_name"`
	Grants           []ACLGrant `json:"grants"`
}

// EncryptionRule represents a server-side encryption rule.
type EncryptionRule struct {
	SSEAlgorithm     string `json:"sse_algorithm"` // "AES256" or "aws:kms"
	KMSMasterKeyID   string `json:"kms_master_key_id"`
	BucketKeyEnabled bool   `json:"bucket_key_enabled"`
}

// EncryptionConfig represents bucket encryption configuration.
type EncryptionConfig struct {
	Rules []EncryptionRule `json:"rules"`
}

// WebsiteRoutingRule represents a website routing rule.
type WebsiteRoutingRule struct {
	Condition struct {
		KeyPrefixEquals             string `json:"key_prefix_equals"`
		HttpErrorCodeReturnedEquals string `json:"http_error_code_returned_equals"`
	} `json:"condition"`
	Redirect struct {
		Protocol             string `json:"protocol"`
		HostName             string `json:"host_name"`
		ReplaceKeyPrefixWith string `json:"replace_key_prefix_with"`
		ReplaceKeyWith       string `json:"replace_key_with"`
		HttpRedirectCode     string `json:"http_redirect_code"`
	} `json:"redirect"`
}

// WebsiteConfig represents bucket website configuration.
type WebsiteConfig struct {
	IndexDocument         string `json:"index_document"`
	ErrorDocument         string `json:"error_document"`
	RedirectAllRequestsTo struct {
		HostName string `json:"host_name"`
		Protocol string `json:"protocol"`
	} `json:"redirect_all_requests_to"`
	RoutingRules []WebsiteRoutingRule `json:"routing_rules,omitempty"`
}

// LoggingConfig represents bucket logging configuration.
type LoggingConfig struct {
	TargetBucket string     `json:"target_bucket"`
	TargetPrefix string     `json:"target_prefix"`
	TargetGrants []ACLGrant `json:"target_grants,omitempty"`
}

// NotificationFilterRule represents a notification filter rule.
type NotificationFilterRule struct {
	Name  string `json:"name"` // "prefix" or "suffix"
	Value string `json:"value"`
}

// TopicNotification represents an SNS topic notification.
type TopicNotification struct {
	ID          string                   `json:"id"`
	TopicArn    string                   `json:"topic_arn"`
	Events      []string                 `json:"events"`
	FilterRules []NotificationFilterRule `json:"filter_rules,omitempty"`
}

// QueueNotification represents an SQS queue notification.
type QueueNotification struct {
	ID          string                   `json:"id"`
	QueueArn    string                   `json:"queue_arn"`
	Events      []string                 `json:"events"`
	FilterRules []NotificationFilterRule `json:"filter_rules,omitempty"`
}

// LambdaNotification represents a Lambda function notification.
type LambdaNotification struct {
	ID          string                   `json:"id"`
	LambdaArn   string                   `json:"lambda_arn"`
	Events      []string                 `json:"events"`
	FilterRules []NotificationFilterRule `json:"filter_rules,omitempty"`
}

// NotificationConfig represents bucket notification configuration.
type NotificationConfig struct {
	TopicConfigurations  []TopicNotification  `json:"topic_configurations,omitempty"`
	QueueConfigurations  []QueueNotification  `json:"queue_configurations,omitempty"`
	LambdaConfigurations []LambdaNotification `json:"lambda_configurations,omitempty"`
}

// ReplicationDestinationConfig represents replication destination.
type ReplicationDestinationConfig struct {
	Bucket       string `json:"bucket"`
	StorageClass string `json:"storage_class,omitempty"`
	Account      string `json:"account,omitempty"`
}

// ReplicationRuleConfig represents a replication rule.
type ReplicationRuleConfig struct {
	Destination             ReplicationDestinationConfig `json:"destination"`
	ID                      string                       `json:"id"`
	Status                  string                       `json:"status"`
	Prefix                  string                       `json:"prefix,omitempty"`
	DeleteMarkerReplication string                       `json:"delete_marker_replication,omitempty"`
	Priority                int                          `json:"priority"`
}

// ReplicationConfig represents bucket replication configuration.
type ReplicationConfig struct {
	Role  string                  `json:"role"`
	Rules []ReplicationRuleConfig `json:"rules"`
}

// ObjectLockRetention represents default retention for object lock.
type ObjectLockRetention struct {
	Mode  string `json:"mode"` // "GOVERNANCE" or "COMPLIANCE"
	Days  int    `json:"days,omitempty"`
	Years int    `json:"years,omitempty"`
}

// ObjectLockConfig represents bucket object lock configuration.
type ObjectLockConfig struct {
	DefaultRetention  *ObjectLockRetention `json:"default_retention,omitempty"`
	ObjectLockEnabled string               `json:"object_lock_enabled"`
}

// PublicAccessBlockConfig represents public access block configuration.
type PublicAccessBlockConfig struct {
	BlockPublicAcls       bool `json:"block_public_acls"`
	IgnorePublicAcls      bool `json:"ignore_public_acls"`
	BlockPublicPolicy     bool `json:"block_public_policy"`
	RestrictPublicBuckets bool `json:"restrict_public_buckets"`
}

// OwnershipControlsConfig represents ownership controls configuration.
type OwnershipControlsConfig struct {
	Rules []OwnershipControlsRule `json:"rules"`
}

// OwnershipControlsRule represents an ownership controls rule.
type OwnershipControlsRule struct {
	ObjectOwnership string `json:"object_ownership"` // "BucketOwnerPreferred", "ObjectWriter", "BucketOwnerEnforced"
}

// RedundancyConfig defines how data should be protected with erasure coding.
type RedundancyConfig struct {
	PlacementPolicy    string   `json:"placement_policy,omitempty"`
	ReplicationTargets []string `json:"replication_targets,omitempty"`
	DataShards         int      `json:"data_shards"`
	ParityShards       int      `json:"parity_shards"`
	MinAvailableShards int      `json:"min_available_shards,omitempty"`
	ReplicationFactor  int      `json:"replication_factor,omitempty"`
	Enabled            bool     `json:"enabled"`
}

// DefaultRedundancyConfig returns a sensible default redundancy configuration.
func DefaultRedundancyConfig() *RedundancyConfig {
	return &RedundancyConfig{
		Enabled:         true,
		DataShards:      10,
		ParityShards:    4,
		PlacementPolicy: "spread",
	}
}

// ValidPlacementPolicies defines the allowed placement policy values.
var ValidPlacementPolicies = map[string]bool{
	"spread":     true,
	"local":      true,
	"rack-aware": true,
	"zone-aware": true,
	"":           true, // empty is allowed, defaults to spread
}

// Validate checks if the RedundancyConfig is valid.
func (c *RedundancyConfig) Validate() error {
	if c == nil {
		return nil // nil config is valid (uses defaults)
	}

	// If not enabled, no further validation needed
	if !c.Enabled {
		return nil
	}

	// Validate DataShards bounds
	if c.DataShards < 2 {
		return fmt.Errorf("data_shards must be at least 2, got %d", c.DataShards)
	}

	if c.DataShards > 256 {
		return fmt.Errorf("data_shards must be at most 256, got %d", c.DataShards)
	}

	// Validate ParityShards bounds
	if c.ParityShards < 1 {
		return fmt.Errorf("parity_shards must be at least 1, got %d", c.ParityShards)
	}

	if c.ParityShards > 256 {
		return fmt.Errorf("parity_shards must be at most 256, got %d", c.ParityShards)
	}

	// Total shards must not exceed 256 (Reed-Solomon limit)
	if c.DataShards+c.ParityShards > 256 {
		return fmt.Errorf("total shards (data + parity) must be at most 256, got %d", c.DataShards+c.ParityShards)
	}

	// Validate MinAvailableShards if set
	if c.MinAvailableShards > 0 && c.MinAvailableShards < c.DataShards {
		return fmt.Errorf("min_available_shards (%d) cannot be less than data_shards (%d)", c.MinAvailableShards, c.DataShards)
	}

	// Validate PlacementPolicy
	if !ValidPlacementPolicies[c.PlacementPolicy] {
		return fmt.Errorf("invalid placement_policy %q, must be one of: spread, local, rack-aware, zone-aware", c.PlacementPolicy)
	}

	// Validate ReplicationFactor bounds
	if c.ReplicationFactor < 0 {
		return fmt.Errorf("replication_factor cannot be negative, got %d", c.ReplicationFactor)
	}

	if c.ReplicationFactor > 10 {
		return fmt.Errorf("replication_factor cannot exceed 10, got %d", c.ReplicationFactor)
	}

	return nil
}

// RedundancyPreset represents a named redundancy configuration preset.
type RedundancyPreset string

const (
	// RedundancyPresetMinimal uses 4 data + 2 parity (can lose 2 shards).
	RedundancyPresetMinimal RedundancyPreset = "minimal"

	// RedundancyPresetStandard uses 10 data + 4 parity (can lose 4 shards).
	RedundancyPresetStandard RedundancyPreset = "standard"

	// RedundancyPresetMaximum uses 8 data + 8 parity (can lose 8 shards).
	RedundancyPresetMaximum RedundancyPreset = "maximum"

	// RedundancyPresetNone disables erasure coding (single copy).
	RedundancyPresetNone RedundancyPreset = "none"
)

// RedundancyConfigFromPreset creates a RedundancyConfig from a preset name.
func RedundancyConfigFromPreset(preset RedundancyPreset) *RedundancyConfig {
	switch preset {
	case RedundancyPresetMinimal:
		return &RedundancyConfig{
			Enabled:         true,
			DataShards:      4,
			ParityShards:    2,
			PlacementPolicy: "spread",
		}
	case RedundancyPresetStandard:
		return &RedundancyConfig{
			Enabled:         true,
			DataShards:      10,
			ParityShards:    4,
			PlacementPolicy: "spread",
		}
	case RedundancyPresetMaximum:
		return &RedundancyConfig{
			Enabled:         true,
			DataShards:      8,
			ParityShards:    8,
			PlacementPolicy: "spread",
		}
	case RedundancyPresetNone:
		return &RedundancyConfig{
			Enabled: false,
		}
	default:
		return DefaultRedundancyConfig()
	}
}

// ObjectMeta represents object metadata.
type ObjectMeta struct {
	CreatedAt                 time.Time          `json:"created_at"`
	ModifiedAt                time.Time          `json:"modified_at"`
	Redundancy                *RedundancyConfig  `json:"redundancy,omitempty"`
	ACL                       *ObjectACL         `json:"acl,omitempty"`
	ObjectLockRetainUntilDate *time.Time         `json:"object_lock_retain_until_date,omitempty"`
	StorageInfo               *ObjectStorageInfo `json:"storage_info,omitempty"`
	Tags                      map[string]string  `json:"tags,omitempty"`
	Metadata                  map[string]string  `json:"metadata,omitempty"`
	ETag                      string             `json:"etag"`
	Owner                     string             `json:"owner"`
	StorageClass              string             `json:"storage_class"`
	ContentType               string             `json:"content_type"`
	Bucket                    string             `json:"bucket"`
	ObjectLockMode            string             `json:"object_lock_mode,omitempty"`
	ObjectLockLegalHoldStatus string             `json:"object_lock_legal_hold_status,omitempty"`
	VersionID                 string             `json:"version_id,omitempty"`
	Key                       string             `json:"key"`
	Size                      int64              `json:"size"`
	DeleteMarker              bool               `json:"delete_marker,omitempty"`
	IsLatest                  bool               `json:"is_latest,omitempty"`
}

// ObjectACL represents an object's access control list.
type ObjectACL struct {
	OwnerID          string     `json:"owner_id"`
	OwnerDisplayName string     `json:"owner_display_name"`
	Grants           []ACLGrant `json:"grants"`
}

// ObjectStorageInfo contains information about where object data is stored.
type ObjectStorageInfo struct {
	// For filesystem backend
	Path string `json:"path,omitempty"`

	// For distributed/erasure coded storage
	Shards []ShardInfo `json:"shards,omitempty"`
}

// ShardInfo represents a single shard location.
type ShardInfo struct {
	NodeID   string `json:"node_id"`
	Path     string `json:"path"`
	Checksum string `json:"checksum"`
	Index    int    `json:"index"`
}

// ObjectListing represents a list of objects.
type ObjectListing struct {
	NextContinuationToken string        `json:"next_continuation_token,omitempty"`
	Objects               []*ObjectMeta `json:"objects"`
	CommonPrefixes        []string      `json:"common_prefixes"`
	IsTruncated           bool          `json:"is_truncated"`
}

// VersionListing represents a list of object versions.
type VersionListing struct {
	NextKeyMarker       string        `json:"next_key_marker,omitempty"`
	NextVersionIDMarker string        `json:"next_version_id_marker,omitempty"`
	Versions            []*ObjectMeta `json:"versions"`
	DeleteMarkers       []*ObjectMeta `json:"delete_markers"`
	CommonPrefixes      []string      `json:"common_prefixes"`
	IsTruncated         bool          `json:"is_truncated"`
}

// MultipartUpload represents an in-progress multipart upload.
type MultipartUpload struct {
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	UploadID     string            `json:"upload_id"`
	Initiator    string            `json:"initiator"`
	ContentType  string            `json:"content_type"`
	StorageClass string            `json:"storage_class,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	Parts        []UploadPart      `json:"parts"`
}

// UploadPart represents a single part of a multipart upload.
type UploadPart struct {
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag"`
	Path         string    `json:"path"`
	PartNumber   int       `json:"part_number"`
	Size         int64     `json:"size"`
}

// User represents a system user.
type User struct {
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	Email        string    `json:"email,omitempty"`
	DisplayName  string    `json:"display_name,omitempty"`
	Role         UserRole  `json:"role"`
	Policies     []string  `json:"policies"`
	Groups       []string  `json:"groups"`
	Enabled      bool      `json:"enabled"`
}

// UserRole represents a user's role in the system.
type UserRole string

const (
	RoleSuperAdmin UserRole = "superadmin"
	RoleAdmin      UserRole = "admin"
	RoleUser       UserRole = "user"
	RoleReadOnly   UserRole = "readonly"
	RoleService    UserRole = "service"
)

// AccessKey represents an S3-compatible access key.
type AccessKey struct {
	CreatedAt       time.Time `json:"created_at"`
	LastUsedAt      time.Time `json:"last_used_at"`
	AccessKeyID     string    `json:"access_key_id"`
	SecretAccessKey string    `json:"secret_access_key"`
	UserID          string    `json:"user_id"`
	Description     string    `json:"description,omitempty"`
	Enabled         bool      `json:"enabled"`
}

// Policy represents an IAM policy.
type Policy struct {
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Document    string    `json:"document"`
}

// ClusterInfo represents cluster status information.
type ClusterInfo struct {
	ClusterID     string      `json:"cluster_id"`
	LeaderID      string      `json:"leader_id"`
	LeaderAddress string      `json:"leader_address"`
	RaftState     string      `json:"raft_state"`
	Nodes         []*NodeInfo `json:"nodes"`
}

// NodeInfo represents information about a cluster node.
type NodeInfo struct {
	JoinedAt      time.Time        `json:"joined_at"`
	LastHeartbeat time.Time        `json:"last_heartbeat"`
	StorageInfo   *NodeStorageInfo `json:"storage_info,omitempty"`
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Address       string           `json:"address"`
	Role          string           `json:"role"`
	Status        string           `json:"status"`
}

// NodeStorageInfo represents storage capacity for a storage node.
type NodeStorageInfo struct {
	TotalBytes     int64 `json:"total_bytes"`
	UsedBytes      int64 `json:"used_bytes"`
	AvailableBytes int64 `json:"available_bytes"`
	ObjectCount    int64 `json:"object_count"`
}
