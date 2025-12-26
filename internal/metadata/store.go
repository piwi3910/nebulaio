package metadata

import (
	"context"
	"time"

	"github.com/piwi3910/nebulaio/internal/audit"
)

// Store is the interface for the metadata store
// All metadata operations go through this interface, which is backed by Raft
type Store interface {
	// Close shuts down the store
	Close() error

	// IsLeader returns true if this node is the Raft leader
	IsLeader() bool

	// LeaderAddress returns the address of the current leader
	LeaderAddress() string

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

// Bucket represents a storage bucket
type Bucket struct {
	Name              string            `json:"name"`
	Owner             string            `json:"owner"`
	CreatedAt         time.Time         `json:"created_at"`
	Region            string            `json:"region"`
	Versioning        VersioningStatus  `json:"versioning"`
	StorageClass      string            `json:"storage_class"`
	ObjectLockEnabled bool              `json:"object_lock_enabled"`
	Tags              map[string]string `json:"tags,omitempty"`
	Policy            string            `json:"policy,omitempty"` // JSON policy document
	CORS              []CORSRule        `json:"cors,omitempty"`
	Lifecycle         []LifecycleRule   `json:"lifecycle,omitempty"`
}

// VersioningStatus represents bucket versioning state
type VersioningStatus string

const (
	VersioningDisabled  VersioningStatus = ""
	VersioningEnabled   VersioningStatus = "Enabled"
	VersioningSuspended VersioningStatus = "Suspended"
)

// CORSRule represents a CORS configuration rule
type CORSRule struct {
	AllowedOrigins []string `json:"allowed_origins"`
	AllowedMethods []string `json:"allowed_methods"`
	AllowedHeaders []string `json:"allowed_headers"`
	ExposeHeaders  []string `json:"expose_headers"`
	MaxAgeSeconds  int      `json:"max_age_seconds"`
}

// LifecycleRule represents a lifecycle management rule
type LifecycleRule struct {
	ID                              string                `json:"id"`
	Enabled                         bool                  `json:"enabled"`
	Prefix                          string                `json:"prefix"`
	ExpirationDays                  int                   `json:"expiration_days,omitempty"`
	NoncurrentVersionExpirationDays int                   `json:"noncurrent_version_expiration_days,omitempty"`
	Transitions                     []LifecycleTransition `json:"transitions,omitempty"`
}

// LifecycleTransition represents a storage class transition
type LifecycleTransition struct {
	Days         int    `json:"days"`
	StorageClass string `json:"storage_class"`
}

// ObjectMeta represents object metadata
type ObjectMeta struct {
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	VersionID    string            `json:"version_id,omitempty"`
	IsLatest     bool              `json:"is_latest,omitempty"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"content_type"`
	StorageClass string            `json:"storage_class"`
	Owner        string            `json:"owner"`
	CreatedAt    time.Time         `json:"created_at"`
	ModifiedAt   time.Time         `json:"modified_at"`
	DeleteMarker bool              `json:"delete_marker,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`

	// Storage location info (for distributed storage)
	StorageInfo *ObjectStorageInfo `json:"storage_info,omitempty"`
}

// ObjectStorageInfo contains information about where object data is stored
type ObjectStorageInfo struct {
	// For filesystem backend
	Path string `json:"path,omitempty"`

	// For distributed/erasure coded storage
	Shards []ShardInfo `json:"shards,omitempty"`
}

// ShardInfo represents a single shard location
type ShardInfo struct {
	Index    int    `json:"index"`
	NodeID   string `json:"node_id"`
	Path     string `json:"path"`
	Checksum string `json:"checksum"`
}

// ObjectListing represents a list of objects
type ObjectListing struct {
	Objects               []*ObjectMeta `json:"objects"`
	CommonPrefixes        []string      `json:"common_prefixes"`
	IsTruncated           bool          `json:"is_truncated"`
	NextContinuationToken string        `json:"next_continuation_token,omitempty"`
}

// VersionListing represents a list of object versions
type VersionListing struct {
	Versions            []*ObjectMeta `json:"versions"`
	DeleteMarkers       []*ObjectMeta `json:"delete_markers"`
	CommonPrefixes      []string      `json:"common_prefixes"`
	IsTruncated         bool          `json:"is_truncated"`
	NextKeyMarker       string        `json:"next_key_marker,omitempty"`
	NextVersionIDMarker string        `json:"next_version_id_marker,omitempty"`
}

// MultipartUpload represents an in-progress multipart upload
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

// UploadPart represents a single part of a multipart upload
type UploadPart struct {
	PartNumber   int       `json:"part_number"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag"`
	LastModified time.Time `json:"last_modified"`
	// Storage path for the part data
	Path string `json:"path"`
}

// User represents a system user
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	Email        string    `json:"email,omitempty"`
	DisplayName  string    `json:"display_name,omitempty"`
	Role         UserRole  `json:"role"`
	Policies     []string  `json:"policies"` // Policy names attached to user
	Groups       []string  `json:"groups"`   // Group IDs
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserRole represents a user's role in the system
type UserRole string

const (
	RoleSuperAdmin UserRole = "superadmin"
	RoleAdmin      UserRole = "admin"
	RoleUser       UserRole = "user"
	RoleReadOnly   UserRole = "readonly"
	RoleService    UserRole = "service"
)

// AccessKey represents an S3-compatible access key
type AccessKey struct {
	AccessKeyID     string    `json:"access_key_id"`
	SecretAccessKey string    `json:"secret_access_key"` // Stored encrypted
	UserID          string    `json:"user_id"`
	Description     string    `json:"description,omitempty"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	LastUsedAt      time.Time `json:"last_used_at,omitempty"`
}

// Policy represents an IAM policy
type Policy struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Document    string    `json:"document"` // JSON policy document
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ClusterInfo represents cluster status information
type ClusterInfo struct {
	ClusterID     string      `json:"cluster_id"`
	LeaderID      string      `json:"leader_id"`
	LeaderAddress string      `json:"leader_address"`
	Nodes         []*NodeInfo `json:"nodes"`
	RaftState     string      `json:"raft_state"`
}

// NodeInfo represents information about a cluster node
type NodeInfo struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Address       string    `json:"address"`
	Role          string    `json:"role"` // "gateway" or "storage"
	Status        string    `json:"status"`
	JoinedAt      time.Time `json:"joined_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`

	// For storage nodes
	StorageInfo *NodeStorageInfo `json:"storage_info,omitempty"`
}

// NodeStorageInfo represents storage capacity for a storage node
type NodeStorageInfo struct {
	TotalBytes     int64 `json:"total_bytes"`
	UsedBytes      int64 `json:"used_bytes"`
	AvailableBytes int64 `json:"available_bytes"`
	ObjectCount    int64 `json:"object_count"`
}
