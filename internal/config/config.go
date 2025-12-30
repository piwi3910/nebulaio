// Package config provides configuration management for NebulaIO.
//
// Configuration is loaded from multiple sources with the following precedence:
//  1. Command-line flags (highest priority)
//  2. Environment variables (NEBULAIO_* prefix)
//  3. Configuration file (config.yaml)
//  4. Default values (lowest priority)
//
// The package uses Viper for configuration binding, supporting:
//   - YAML configuration files
//   - Environment variable overrides
//   - Type-safe configuration structs
//   - Validation and defaults
//
// Example usage:
//
//	cfg, err := config.Load("/etc/nebulaio/config.yaml")
//	if err != nil {
//	    log.Fatal(err)
//	}
package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/piwi3910/nebulaio/internal/auth"
)

// Config holds all configuration for NebulaIO.
type Config struct {
	GPUDirect   GPUDirectConfig `mapstructure:"gpudirect"`
	NodeID      string          `mapstructure:"node_id"`
	NodeName    string          `mapstructure:"node_name"`
	DataDir     string          `mapstructure:"data_dir"`
	LogLevel    string          `mapstructure:"log_level"`
	Storage     StorageConfig   `mapstructure:"storage"`
	TLS         TLSConfig       `mapstructure:"tls"`
	NIM         NIMConfig       `mapstructure:"nim"`
	Cache       CacheConfig     `mapstructure:"cache"`
	Audit       AuditConfig     `mapstructure:"audit"`
	Firewall    FirewallConfig  `mapstructure:"firewall"`
	Cluster     ClusterConfig   `mapstructure:"cluster"`
	Auth        AuthConfig      `mapstructure:"auth"`
	Iceberg     IcebergConfig   `mapstructure:"iceberg"`
	S3Express   S3ExpressConfig `mapstructure:"s3_express"`
	MCP         MCPConfig       `mapstructure:"mcp"`
	RDMA        RDMAConfig      `mapstructure:"rdma"`
	DPU         DPUConfig       `mapstructure:"dpu"`
	Lambda      LambdaConfig    `mapstructure:"lambda"`
	ConsolePort int             `mapstructure:"console_port"`
	AdminPort   int             `mapstructure:"admin_port"`
	S3Port      int             `mapstructure:"s3_port"`
}

// TLSConfig holds TLS configuration for secure communications.
type TLSConfig struct {
	CertDir           string   `mapstructure:"cert_dir"`
	CertFile          string   `mapstructure:"cert_file"`
	KeyFile           string   `mapstructure:"key_file"`
	CAFile            string   `mapstructure:"ca_file"`
	MinVersion        string   `mapstructure:"min_version"`
	Organization      string   `mapstructure:"organization"`
	DNSNames          []string `mapstructure:"dns_names"`
	IPAddresses       []string `mapstructure:"ip_addresses"`
	ValidityDays      int      `mapstructure:"validity_days"`
	Enabled           bool     `mapstructure:"enabled"`
	RequireClientCert bool     `mapstructure:"require_client_cert"`
	AutoGenerate      bool     `mapstructure:"auto_generate"`
}

// ClusterConfig holds cluster-related configuration.
type ClusterConfig struct {
	NodeRole             string        `mapstructure:"node_role"`
	AdvertiseAddress     string        `mapstructure:"advertise_address"`
	WALDir               string        `mapstructure:"wal_dir"`
	ClusterName          string        `mapstructure:"cluster_name"`
	JoinAddresses        []string      `mapstructure:"join_addresses"`
	RaftPort             int           `mapstructure:"raft_port"`
	GossipPort           int           `mapstructure:"gossip_port"`
	ExpectNodes          int           `mapstructure:"expect_nodes"`
	RetryJoinMaxAttempts int           `mapstructure:"retry_join_max_attempts"`
	RetryJoinInterval    time.Duration `mapstructure:"retry_join_interval"`
	ShardID              uint64        `mapstructure:"shard_id"`
	ReplicaID            uint64        `mapstructure:"replica_id"`
	SnapshotCount        uint64        `mapstructure:"snapshot_count"`
	CompactionOverhead   uint64        `mapstructure:"compaction_overhead"`
	Bootstrap            bool          `mapstructure:"bootstrap"`
}

// StorageConfig holds storage-related configuration.
type StorageConfig struct {
	Volume              VolumeStorageConfig   `mapstructure:"volume"`
	Backend             string                `mapstructure:"backend"`
	DefaultStorageClass string                `mapstructure:"default_storage_class"`
	PlacementGroups     PlacementGroupsConfig `mapstructure:"placement_groups"`
	DefaultRedundancy   RedundancyConfig      `mapstructure:"default_redundancy"`
	Tiering             TieringConfig         `mapstructure:"tiering"`
	MaxObjectSize       int64                 `mapstructure:"max_object_size"`
	MultipartPartSize   int64                 `mapstructure:"multipart_part_size"`
}

// TieringConfig holds configuration for tiered storage.
type TieringConfig struct {
	Tiers    map[string]TierConfig `mapstructure:"tiers"`
	Policies []TieringPolicyConfig `mapstructure:"policies"`
	Enabled  bool                  `mapstructure:"enabled"`
}

// TierConfig holds configuration for a specific storage tier.
type TierConfig struct {
	VolumeConfig      *VolumeStorageConfig  `mapstructure:"volume_config,omitempty"`
	ErasureConfig     *ErasureStorageConfig `mapstructure:"erasure_config,omitempty"`
	Backend           string                `mapstructure:"backend"`
	DataDir           string                `mapstructure:"data_dir"`
	Device            string                `mapstructure:"device"`
	Priority          int                   `mapstructure:"priority"`
	MaxCapacity       int64                 `mapstructure:"max_capacity"`
	CapacityThreshold float64               `mapstructure:"capacity_threshold"`
}

// ErasureStorageConfig holds erasure coding specific configuration.
type ErasureStorageConfig struct {
	PlacementPolicy string `mapstructure:"placement_policy"`
	DataShards      int    `mapstructure:"data_shards"`
	ParityShards    int    `mapstructure:"parity_shards"`
	ShardSize       int64  `mapstructure:"shard_size"`
}

// TieringPolicyConfig defines a policy for automatic data movement.
type TieringPolicyConfig struct {
	Condition       TieringCondition `mapstructure:"condition"`
	Name            string           `mapstructure:"name"`
	SourceTier      string           `mapstructure:"source_tier"`
	DestinationTier string           `mapstructure:"destination_tier"`
	Enabled         bool             `mapstructure:"enabled"`
}

// TieringCondition defines when a tiering policy should trigger.
type TieringCondition struct {
	Type            string  `mapstructure:"type"`
	CronSchedule    string  `mapstructure:"cron_schedule,omitempty"`
	AgeDays         int     `mapstructure:"age_days,omitempty"`
	AccessDays      int     `mapstructure:"access_days,omitempty"`
	CapacityPercent float64 `mapstructure:"capacity_percent,omitempty"`
}

// RedundancyConfig holds default redundancy configuration.
type RedundancyConfig struct {
	PlacementPolicy   string `mapstructure:"placement_policy"`
	DataShards        int    `mapstructure:"data_shards"`
	ParityShards      int    `mapstructure:"parity_shards"`
	ReplicationFactor int    `mapstructure:"replication_factor"`
	Enabled           bool   `mapstructure:"enabled"`
}

// PlacementGroupsConfig holds placement group configuration.
type PlacementGroupsConfig struct {
	LocalGroupID       string                 `mapstructure:"local_group_id"`
	Groups             []PlacementGroupConfig `mapstructure:"groups"`
	ReplicationTargets []string               `mapstructure:"replication_targets"`
	MinNodesForErasure int                    `mapstructure:"min_nodes_for_erasure"`
}

// PlacementGroupConfig defines a single placement group.
type PlacementGroupConfig struct {
	// ID is the unique identifier for this placement group
	ID string `mapstructure:"id"`

	// Name is a human-readable name
	Name string `mapstructure:"name"`

	// Datacenter is the datacenter this group is in
	Datacenter string `mapstructure:"datacenter"`

	// Region is the region this group is in
	Region string `mapstructure:"region"`

	// MinNodes is the minimum nodes required
	MinNodes int `mapstructure:"min_nodes"`

	// MaxNodes is the maximum nodes allowed
	MaxNodes int `mapstructure:"max_nodes"`
}

// VolumeStorageConfig holds configuration for the volume storage backend.
type VolumeStorageConfig struct {
	TierDirectories TierDirectoriesConfig `mapstructure:"tier_directories"`
	RawDevices      RawDevicesConfig      `mapstructure:"raw_devices"`
	DirectIO        DirectIOStorageConfig `mapstructure:"direct_io"`
	MaxVolumeSize   uint64                `mapstructure:"max_volume_size"`
	AutoCreate      bool                  `mapstructure:"auto_create"`
}

// RawDevicesConfig holds configuration for raw block device access.
type RawDevicesConfig struct {
	Devices []RawDeviceConfig     `mapstructure:"devices"`
	Safety  RawDeviceSafetyConfig `mapstructure:"safety"`
	Enabled bool                  `mapstructure:"enabled"`
}

// RawDeviceSafetyConfig holds safety configuration for raw device access.
type RawDeviceSafetyConfig struct {
	// CheckFilesystem verifies device has no existing filesystem before use
	CheckFilesystem bool `mapstructure:"check_filesystem"`

	// RequireConfirmation requires explicit confirmation for new devices
	RequireConfirmation bool `mapstructure:"require_confirmation"`

	// WriteSignature writes NebulaIO signature to device header
	WriteSignature bool `mapstructure:"write_signature"`

	// ExclusiveLock uses flock() to prevent concurrent access
	ExclusiveLock bool `mapstructure:"exclusive_lock"`
}

// RawDeviceConfig holds configuration for a single raw block device.
type RawDeviceConfig struct {
	// Path is the device path (e.g., /dev/nvme0n1, /dev/sda)
	Path string `mapstructure:"path"`

	// Tier is the storage tier for this device (hot, warm, cold)
	Tier string `mapstructure:"tier"`

	// Size is the size to use (0 = use entire device)
	Size uint64 `mapstructure:"size"`
}

// TierDirectoriesConfig holds directory paths for tiered storage.
type TierDirectoriesConfig struct {
	// Hot tier directory (mount NVMe here)
	Hot string `mapstructure:"hot"`

	// Warm tier directory (mount SSD here)
	Warm string `mapstructure:"warm"`

	// Cold tier directory (mount HDD here)
	Cold string `mapstructure:"cold"`
}

// DirectIOStorageConfig holds direct I/O configuration for volume storage.
type DirectIOStorageConfig struct {
	BlockAlignment  int  `mapstructure:"block_alignment"`
	Enabled         bool `mapstructure:"enabled"`
	UseMemoryPool   bool `mapstructure:"use_memory_pool"`
	FallbackOnError bool `mapstructure:"fallback_on_error"`
}

// CacheConfig holds DRAM cache configuration for high-performance workloads.
type CacheConfig struct {
	EvictionPolicy    string   `mapstructure:"eviction_policy"`
	WarmupKeys        []string `mapstructure:"warmup_keys"`
	ReplicationFactor int      `mapstructure:"replication_factor"`
	MaxSize           int64    `mapstructure:"max_size"`
	ShardCount        int      `mapstructure:"shard_count"`
	EntryMaxSize      int64    `mapstructure:"entry_max_size"`
	TTL               int      `mapstructure:"ttl"`
	PrefetchThreshold int      `mapstructure:"prefetch_threshold"`
	PrefetchAhead     int      `mapstructure:"prefetch_ahead"`
	PrefetchEnabled   bool     `mapstructure:"prefetch_enabled"`
	DistributedMode   bool     `mapstructure:"distributed_mode"`
	ZeroCopyEnabled   bool     `mapstructure:"zero_copy_enabled"`
	WarmupEnabled     bool     `mapstructure:"warmup_enabled"`
	Enabled           bool     `mapstructure:"enabled"`
}

// FirewallConfig holds data firewall configuration for QoS and rate limiting.
type FirewallConfig struct {
	DefaultPolicy string             `mapstructure:"default_policy"`
	IPAllowlist   []string           `mapstructure:"ip_allowlist"`
	IPBlocklist   []string           `mapstructure:"ip_blocklist"`
	Connections   ConnectionsConfig  `mapstructure:"connections"`
	RateLimiting  RateLimitingConfig `mapstructure:"rate_limiting"`
	Bandwidth     BandwidthConfig    `mapstructure:"bandwidth"`
	Enabled       bool               `mapstructure:"enabled"`
	AuditEnabled  bool               `mapstructure:"audit_enabled"`
}

// RateLimitingConfig configures request rate limiting.
type RateLimitingConfig struct {
	RequestsPerSecond int  `mapstructure:"requests_per_second"`
	BurstSize         int  `mapstructure:"burst_size"`
	Enabled           bool `mapstructure:"enabled"`
	PerUser           bool `mapstructure:"per_user"`
	PerIP             bool `mapstructure:"per_ip"`
	PerBucket         bool `mapstructure:"per_bucket"`
}

// BandwidthConfig configures bandwidth throttling.
type BandwidthConfig struct {
	// Enabled enables bandwidth throttling
	Enabled bool `mapstructure:"enabled"`

	// MaxBytesPerSecond is the global max bandwidth in bytes/second
	MaxBytesPerSecond int64 `mapstructure:"max_bytes_per_second"`

	// MaxBytesPerSecondPerUser is per-user bandwidth limit
	MaxBytesPerSecondPerUser int64 `mapstructure:"max_bytes_per_second_per_user"`

	// MaxBytesPerSecondPerBucket is per-bucket bandwidth limit
	MaxBytesPerSecondPerBucket int64 `mapstructure:"max_bytes_per_second_per_bucket"`
}

// ConnectionsConfig configures connection limits.
type ConnectionsConfig struct {
	// Enabled enables connection limiting
	Enabled bool `mapstructure:"enabled"`

	// MaxConnections is the global max concurrent connections
	MaxConnections int `mapstructure:"max_connections"`

	// MaxConnectionsPerIP is per-IP connection limit
	MaxConnectionsPerIP int `mapstructure:"max_connections_per_ip"`

	// MaxConnectionsPerUser is per-user connection limit
	MaxConnectionsPerUser int `mapstructure:"max_connections_per_user"`

	// IdleTimeoutSeconds is the idle connection timeout in seconds
	IdleTimeoutSeconds int `mapstructure:"idle_timeout_seconds"`
}

// AuditConfig holds enhanced audit logging configuration.
type AuditConfig struct {
	ComplianceMode    string              `mapstructure:"compliance_mode"`
	FilePath          string              `mapstructure:"file_path"`
	IntegritySecret   string              `mapstructure:"integrity_secret"`
	Webhook           AuditWebhookConfig  `mapstructure:"webhook"`
	Rotation          AuditRotationConfig `mapstructure:"rotation"`
	RetentionDays     int                 `mapstructure:"retention_days"`
	BufferSize        int                 `mapstructure:"buffer_size"`
	Enabled           bool                `mapstructure:"enabled"`
	IntegrityEnabled  bool                `mapstructure:"integrity_enabled"`
	MaskSensitiveData bool                `mapstructure:"mask_sensitive_data"`
}

// AuditRotationConfig configures audit log rotation.
type AuditRotationConfig struct {
	MaxSizeMB  int  `mapstructure:"max_size_mb"`
	MaxBackups int  `mapstructure:"max_backups"`
	MaxAgeDays int  `mapstructure:"max_age_days"`
	Enabled    bool `mapstructure:"enabled"`
	Compress   bool `mapstructure:"compress"`
}

// AuditWebhookConfig configures audit webhook output.
type AuditWebhookConfig struct {
	URL                  string `mapstructure:"url"`
	AuthToken            string `mapstructure:"auth_token"`
	BatchSize            int    `mapstructure:"batch_size"`
	FlushIntervalSeconds int    `mapstructure:"flush_interval_seconds"`
	Enabled              bool   `mapstructure:"enabled"`
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	// RootUser is the initial admin username
	RootUser string `mapstructure:"root_user"`

	// RootPassword is the initial admin password
	RootPassword string `mapstructure:"root_password"`

	// JWTSecret for signing tokens
	JWTSecret string `mapstructure:"jwt_secret"`

	// TokenExpiry in minutes
	TokenExpiry int `mapstructure:"token_expiry"`

	// RefreshTokenExpiry in hours
	RefreshTokenExpiry int `mapstructure:"refresh_token_expiry"`
}

// S3ExpressConfig holds S3 Express One Zone configuration.
type S3ExpressConfig struct {
	DefaultZone        string              `mapstructure:"default_zone"`
	Zones              []ExpressZoneConfig `mapstructure:"zones"`
	SessionDuration    int                 `mapstructure:"session_duration"`
	MaxAppendSize      int64               `mapstructure:"max_append_size"`
	Enabled            bool                `mapstructure:"enabled"`
	EnableAtomicAppend bool                `mapstructure:"enable_atomic_append"`
}

// ExpressZoneConfig defines an S3 Express zone.
type ExpressZoneConfig struct {
	// Name is the zone identifier (e.g., "use1-az1")
	Name string `mapstructure:"name"`

	// Region is the AWS region equivalent
	Region string `mapstructure:"region"`

	// StoragePath is the local storage path for this zone
	StoragePath string `mapstructure:"storage_path"`

	// MaxIOPS is the maximum IOPS for this zone
	MaxIOPS int `mapstructure:"max_iops"`

	// MaxThroughputMBps is the maximum throughput in MB/s
	MaxThroughputMBps int `mapstructure:"max_throughput_mbps"`
}

// IcebergConfig holds Apache Iceberg table format configuration.
type IcebergConfig struct {
	CatalogType              string `mapstructure:"catalog_type"`
	CatalogURI               string `mapstructure:"catalog_uri"`
	Warehouse                string `mapstructure:"warehouse"`
	DefaultFileFormat        string `mapstructure:"default_file_format"`
	MetadataPath             string `mapstructure:"metadata_path"`
	SnapshotRetention        int    `mapstructure:"snapshot_retention"`
	ExpireSnapshotsOlderThan int    `mapstructure:"expire_snapshots_older_than"`
	Enabled                  bool   `mapstructure:"enabled"`
	EnableACID               bool   `mapstructure:"enable_acid"`
}

// MCPConfig holds Model Context Protocol server configuration.
type MCPConfig struct {
	AllowedOrigins     []string `mapstructure:"allowed_origins"`
	Port               int      `mapstructure:"port"`
	MaxConnections     int      `mapstructure:"max_connections"`
	RateLimitPerMinute int      `mapstructure:"rate_limit_per_minute"`
	Enabled            bool     `mapstructure:"enabled"`
	EnableTools        bool     `mapstructure:"enable_tools"`
	EnableResources    bool     `mapstructure:"enable_resources"`
	EnablePrompts      bool     `mapstructure:"enable_prompts"`
	AuthRequired       bool     `mapstructure:"auth_required"`
}

// GPUDirectConfig holds GPUDirect Storage configuration.
type GPUDirectConfig struct {
	NVMePath        string `mapstructure:"nvme_path"`
	Devices         []int  `mapstructure:"devices"`
	BufferPoolSize  int64  `mapstructure:"buffer_pool_size"`
	MaxTransferSize int64  `mapstructure:"max_transfer_size"`
	CUDAStreamCount int    `mapstructure:"cuda_stream_count"`
	Enabled         bool   `mapstructure:"enabled"`
	EnableAsync     bool   `mapstructure:"enable_async"`
	EnableP2P       bool   `mapstructure:"enable_p2p"`
}

// DPUConfig holds BlueField DPU configuration.
type DPUConfig struct {
	DeviceIndex         int  `mapstructure:"device_index"`
	HealthCheckInterval int  `mapstructure:"health_check_interval"`
	MinSizeForOffload   int  `mapstructure:"min_size_for_offload"`
	Enabled             bool `mapstructure:"enabled"`
	EnableCrypto        bool `mapstructure:"enable_crypto"`
	EnableCompression   bool `mapstructure:"enable_compression"`
	EnableStorage       bool `mapstructure:"enable_storage"`
	EnableNetwork       bool `mapstructure:"enable_network"`
	EnableRDMA          bool `mapstructure:"enable_rdma"`
	EnableRegex         bool `mapstructure:"enable_regex"`
	FallbackOnError     bool `mapstructure:"fallback_on_error"`
}

// RDMAConfig holds RDMA transport configuration.
type RDMAConfig struct {
	DeviceName     string `mapstructure:"device_name"`
	Port           int    `mapstructure:"port"`
	GIDIndex       int    `mapstructure:"gid_index"`
	MaxSendWR      int    `mapstructure:"max_send_wr"`
	MaxRecvWR      int    `mapstructure:"max_recv_wr"`
	MaxSendSGE     int    `mapstructure:"max_send_sge"`
	MaxRecvSGE     int    `mapstructure:"max_recv_sge"`
	MaxInlineData  int    `mapstructure:"max_inline_data"`
	MemoryPoolSize int64  `mapstructure:"memory_pool_size"`
	Enabled        bool   `mapstructure:"enabled"`
	EnableZeroCopy bool   `mapstructure:"enable_zero_copy"`
	FallbackToTCP  bool   `mapstructure:"fallback_to_tcp"`
}

// NIMConfig holds NVIDIA NIM Microservices configuration.
type NIMConfig struct {
	APIKey              string   `mapstructure:"api_key"`
	OrganizationID      string   `mapstructure:"organization_id"`
	DefaultModel        string   `mapstructure:"default_model"`
	ProcessContentTypes []string `mapstructure:"process_content_types"`
	Endpoints           []string `mapstructure:"endpoints"`
	MaxRetries          int      `mapstructure:"max_retries"`
	MaxBatchSize        int      `mapstructure:"max_batch_size"`
	CacheTTL            int      `mapstructure:"cache_ttl"`
	Timeout             int      `mapstructure:"timeout"`
	Enabled             bool     `mapstructure:"enabled"`
	EnableStreaming     bool     `mapstructure:"enable_streaming"`
	CacheResults        bool     `mapstructure:"cache_results"`
	EnableMetrics       bool     `mapstructure:"enable_metrics"`
	ProcessOnUpload     bool     `mapstructure:"process_on_upload"`
}

// LambdaConfig holds S3 Object Lambda configuration.
type LambdaConfig struct {
	// ObjectLambda holds Object Lambda specific settings
	ObjectLambda ObjectLambdaConfig `mapstructure:"object_lambda"`
}

// ObjectLambdaConfig holds Object Lambda transformation settings.
type ObjectLambdaConfig struct {
	// MaxTransformSize is the maximum size of data that can be transformed in memory
	// Default: 100MB (100 * 1024 * 1024 bytes)
	// Warning: Setting this too high may cause memory exhaustion during transformation operations
	MaxTransformSize int64 `mapstructure:"max_transform_size"`

	// StreamingThreshold is the size threshold above which streaming mode is used
	// for compression/decompression operations instead of buffering in memory
	// Default: 10MB (10 * 1024 * 1024 bytes)
	StreamingThreshold int64 `mapstructure:"streaming_threshold"`
}

// Options are command line overrides.
type Options struct {
	DataDir     string
	S3Port      int
	AdminPort   int
	ConsolePort int
}

// Load loads configuration from file and applies command line options.
func Load(configPath string, opts Options) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Load from config file if specified
	if configPath != "" {
		v.SetConfigFile(configPath)

		err := v.ReadInConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	} else {
		// Try to find config in standard locations
		v.SetConfigName("nebulaio")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/nebulaio")
		v.AddConfigPath("$HOME/.nebulaio")

		// Ignore error if config file not found
		_ = v.ReadInConfig()
	}

	// Environment variables override
	v.SetEnvPrefix("NEBULAIO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Apply command line options
	if opts.DataDir != "" {
		v.Set("data_dir", opts.DataDir)
	}

	if opts.S3Port != 0 {
		v.Set("s3_port", opts.S3Port)
	}

	if opts.AdminPort != 0 {
		v.Set("admin_port", opts.AdminPort)
	}

	if opts.ConsolePort != 0 {
		v.Set("console_port", opts.ConsolePort)
	}

	// Unmarshal config
	var cfg Config

	err := v.Unmarshal(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate and set derived values
	err = cfg.validate()
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Node defaults
	hostname, _ := os.Hostname()
	v.SetDefault("node_name", hostname)

	// Data directory
	v.SetDefault("data_dir", "./data")

	// Network ports
	v.SetDefault("s3_port", 9000)
	v.SetDefault("admin_port", 9001)
	v.SetDefault("console_port", 9002)

	// Cluster defaults
	v.SetDefault("cluster.bootstrap", true)
	v.SetDefault("cluster.raft_port", 9003)
	v.SetDefault("cluster.gossip_port", 9004)
	v.SetDefault("cluster.node_role", "storage")
	v.SetDefault("cluster.cluster_name", "nebulaio")
	v.SetDefault("cluster.expect_nodes", 1)
	v.SetDefault("cluster.retry_join_max_attempts", 10)
	v.SetDefault("cluster.retry_join_interval", 5*time.Second)
	// Dragonboat-specific defaults
	v.SetDefault("cluster.shard_id", uint64(1))
	v.SetDefault("cluster.replica_id", uint64(0)) // Auto-generate if 0
	v.SetDefault("cluster.wal_dir", "")           // Empty means use data_dir/wal
	v.SetDefault("cluster.snapshot_count", uint64(10000))
	v.SetDefault("cluster.compaction_overhead", uint64(5000))

	// Storage defaults
	v.SetDefault("storage.backend", "fs")
	v.SetDefault("storage.default_storage_class", "STANDARD")
	v.SetDefault("storage.max_object_size", 5*1024*1024*1024*1024) // 5TB
	v.SetDefault("storage.multipart_part_size", 64*1024*1024)      // 64MB
	// Volume backend defaults
	v.SetDefault("storage.volume.max_volume_size", 32*1024*1024*1024) // 32GB
	v.SetDefault("storage.volume.auto_create", true)
	// Direct I/O defaults
	v.SetDefault("storage.volume.direct_io.enabled", true)
	v.SetDefault("storage.volume.direct_io.block_alignment", 4096)
	v.SetDefault("storage.volume.direct_io.use_memory_pool", true)
	v.SetDefault("storage.volume.direct_io.fallback_on_error", true)
	// Raw device defaults
	v.SetDefault("storage.volume.raw_devices.enabled", false)
	v.SetDefault("storage.volume.raw_devices.safety.check_filesystem", true)
	v.SetDefault("storage.volume.raw_devices.safety.require_confirmation", true)
	v.SetDefault("storage.volume.raw_devices.safety.write_signature", true)
	v.SetDefault("storage.volume.raw_devices.safety.exclusive_lock", true)
	// Tier directories defaults (empty = disabled)
	v.SetDefault("storage.volume.tier_directories.hot", "")
	v.SetDefault("storage.volume.tier_directories.warm", "")
	v.SetDefault("storage.volume.tier_directories.cold", "")

	// Tiering configuration defaults
	v.SetDefault("storage.tiering.enabled", false)

	// Default redundancy configuration
	v.SetDefault("storage.default_redundancy.enabled", true)
	v.SetDefault("storage.default_redundancy.data_shards", 10)
	v.SetDefault("storage.default_redundancy.parity_shards", 4)
	v.SetDefault("storage.default_redundancy.placement_policy", "spread")
	v.SetDefault("storage.default_redundancy.replication_factor", 1)

	// Placement groups defaults
	v.SetDefault("storage.placement_groups.local_group_id", "default")
	v.SetDefault("storage.placement_groups.min_nodes_for_erasure", 14)

	// Cache defaults (DRAM Cache)
	v.SetDefault("cache.enabled", false)
	v.SetDefault("cache.max_size", 8*1024*1024*1024) // 8GB
	v.SetDefault("cache.shard_count", 256)
	v.SetDefault("cache.entry_max_size", 256*1024*1024) // 256MB
	v.SetDefault("cache.ttl", 3600)                     // 1 hour
	v.SetDefault("cache.eviction_policy", "arc")
	v.SetDefault("cache.prefetch_enabled", true)
	v.SetDefault("cache.prefetch_threshold", 2)
	v.SetDefault("cache.prefetch_ahead", 4)
	v.SetDefault("cache.zero_copy_enabled", true)
	v.SetDefault("cache.distributed_mode", false)
	v.SetDefault("cache.replication_factor", 2)
	v.SetDefault("cache.warmup_enabled", false)

	// Firewall defaults (Data Firewall with QoS)
	v.SetDefault("firewall.enabled", false)
	v.SetDefault("firewall.default_policy", "allow")
	v.SetDefault("firewall.audit_enabled", true)
	// Rate limiting defaults
	v.SetDefault("firewall.rate_limiting.enabled", false)
	v.SetDefault("firewall.rate_limiting.requests_per_second", 1000)
	v.SetDefault("firewall.rate_limiting.burst_size", 100)
	v.SetDefault("firewall.rate_limiting.per_user", true)
	v.SetDefault("firewall.rate_limiting.per_ip", true)
	v.SetDefault("firewall.rate_limiting.per_bucket", false)
	// Bandwidth defaults
	v.SetDefault("firewall.bandwidth.enabled", false)
	v.SetDefault("firewall.bandwidth.max_bytes_per_second", 1024*1024*1024)           // 1 GB/s
	v.SetDefault("firewall.bandwidth.max_bytes_per_second_per_user", 100*1024*1024)   // 100 MB/s
	v.SetDefault("firewall.bandwidth.max_bytes_per_second_per_bucket", 500*1024*1024) // 500 MB/s
	// Connection limits defaults
	v.SetDefault("firewall.connections.enabled", false)
	v.SetDefault("firewall.connections.max_connections", 10000)
	v.SetDefault("firewall.connections.max_connections_per_ip", 100)
	v.SetDefault("firewall.connections.max_connections_per_user", 500)
	v.SetDefault("firewall.connections.idle_timeout_seconds", 60)

	// Audit defaults (Enhanced Audit Logging)
	v.SetDefault("audit.enabled", true)
	v.SetDefault("audit.compliance_mode", "none") // none, soc2, pci, hipaa, gdpr, fedramp
	v.SetDefault("audit.file_path", "./data/audit/audit.log")
	v.SetDefault("audit.retention_days", 90)
	v.SetDefault("audit.buffer_size", 10000)
	v.SetDefault("audit.integrity_enabled", true)
	v.SetDefault("audit.mask_sensitive_data", true)
	// Audit rotation defaults
	v.SetDefault("audit.rotation.enabled", true)
	v.SetDefault("audit.rotation.max_size_mb", 100)
	v.SetDefault("audit.rotation.max_backups", 10)
	v.SetDefault("audit.rotation.max_age_days", 30)
	v.SetDefault("audit.rotation.compress", true)
	// Audit webhook defaults
	v.SetDefault("audit.webhook.enabled", false)
	v.SetDefault("audit.webhook.batch_size", 100)
	v.SetDefault("audit.webhook.flush_interval_seconds", 30)

	// Auth defaults
	// Note: root_password has no default - must be set via NEBULAIO_AUTH_ROOT_PASSWORD env var or config file
	v.SetDefault("auth.root_user", "admin")
	v.SetDefault("auth.root_password", "")         // Must be configured - no default for security
	v.SetDefault("auth.token_expiry", 60)          // 1 hour
	v.SetDefault("auth.refresh_token_expiry", 168) // 7 days

	// S3 Express defaults
	v.SetDefault("s3_express.enabled", false)
	v.SetDefault("s3_express.default_zone", "use1-az1")
	v.SetDefault("s3_express.session_duration", 3600)            // 1 hour
	v.SetDefault("s3_express.max_append_size", 5*1024*1024*1024) // 5GB
	v.SetDefault("s3_express.enable_atomic_append", true)

	// Iceberg defaults
	v.SetDefault("iceberg.enabled", false)
	v.SetDefault("iceberg.catalog_type", "rest")
	v.SetDefault("iceberg.catalog_uri", "http://localhost:8181")
	v.SetDefault("iceberg.warehouse", "s3://warehouse/")
	v.SetDefault("iceberg.default_file_format", "parquet")
	v.SetDefault("iceberg.metadata_path", "./data/iceberg")
	v.SetDefault("iceberg.snapshot_retention", 10)
	v.SetDefault("iceberg.expire_snapshots_older_than", 168) // 7 days
	v.SetDefault("iceberg.enable_acid", true)

	// MCP Server defaults
	v.SetDefault("mcp.enabled", false)
	v.SetDefault("mcp.port", 9005)
	v.SetDefault("mcp.max_connections", 100)
	v.SetDefault("mcp.enable_tools", true)
	v.SetDefault("mcp.enable_resources", true)
	v.SetDefault("mcp.enable_prompts", true)
	v.SetDefault("mcp.auth_required", true)
	v.SetDefault("mcp.rate_limit_per_minute", 60)

	// GPUDirect defaults
	v.SetDefault("gpudirect.enabled", false)
	v.SetDefault("gpudirect.buffer_pool_size", 1024*1024*1024) // 1GB
	v.SetDefault("gpudirect.max_transfer_size", 256*1024*1024) // 256MB
	v.SetDefault("gpudirect.enable_async", true)
	v.SetDefault("gpudirect.cuda_stream_count", 4)
	v.SetDefault("gpudirect.enable_p2p", true)
	v.SetDefault("gpudirect.nvme_path", "/dev/nvme*")

	// DPU (BlueField) defaults
	v.SetDefault("dpu.enabled", false)
	v.SetDefault("dpu.device_index", 0)
	v.SetDefault("dpu.enable_crypto", true)
	v.SetDefault("dpu.enable_compression", true)
	v.SetDefault("dpu.enable_storage", true)
	v.SetDefault("dpu.enable_network", true)
	v.SetDefault("dpu.enable_rdma", true)
	v.SetDefault("dpu.enable_regex", false)
	v.SetDefault("dpu.health_check_interval", 30)
	v.SetDefault("dpu.fallback_on_error", true)
	v.SetDefault("dpu.min_size_for_offload", 4096)

	// RDMA defaults
	v.SetDefault("rdma.enabled", false)
	v.SetDefault("rdma.port", 9100)
	v.SetDefault("rdma.device_name", "mlx5_0")
	v.SetDefault("rdma.gid_index", 0)
	v.SetDefault("rdma.max_send_wr", 128)
	v.SetDefault("rdma.max_recv_wr", 128)
	v.SetDefault("rdma.max_send_sge", 1)
	v.SetDefault("rdma.max_recv_sge", 1)
	v.SetDefault("rdma.max_inline_data", 64)
	v.SetDefault("rdma.memory_pool_size", 1024*1024*1024) // 1GB
	v.SetDefault("rdma.enable_zero_copy", true)
	v.SetDefault("rdma.fallback_to_tcp", true)

	// NIM defaults
	v.SetDefault("nim.enabled", false)
	v.SetDefault("nim.endpoints", []string{"https://integrate.api.nvidia.com/v1"})
	v.SetDefault("nim.default_model", "meta/llama-3.1-8b-instruct")
	v.SetDefault("nim.timeout", 60)
	v.SetDefault("nim.max_retries", 3)
	v.SetDefault("nim.max_batch_size", 100)
	v.SetDefault("nim.enable_streaming", true)
	v.SetDefault("nim.cache_results", true)
	v.SetDefault("nim.cache_ttl", 3600) // 1 hour
	v.SetDefault("nim.enable_metrics", true)
	v.SetDefault("nim.process_on_upload", false)
	v.SetDefault("nim.process_content_types", []string{"image/jpeg", "image/png", "text/plain", "application/json"})

	// TLS defaults (secure by default)
	v.SetDefault("tls.enabled", true)
	v.SetDefault("tls.cert_dir", "./data/certs")
	v.SetDefault("tls.min_version", "1.2")
	v.SetDefault("tls.require_client_cert", false)
	v.SetDefault("tls.auto_generate", true)
	v.SetDefault("tls.organization", "NebulaIO")
	v.SetDefault("tls.validity_days", 365)

	// Lambda defaults (S3 Object Lambda)
	v.SetDefault("lambda.object_lambda.max_transform_size", 100*1024*1024) // 100MB
	v.SetDefault("lambda.object_lambda.streaming_threshold", 10*1024*1024) // 10MB

	// Logging
	v.SetDefault("log_level", "info")
}

func (c *Config) validate() error {
	// Ensure data directory exists
	err := os.MkdirAll(c.DataDir, 0750)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Generate node ID if not set
	if c.NodeID == "" {
		nodeIDPath := filepath.Join(c.DataDir, "node-id")
		//nolint:gosec // G304: nodeIDPath is constructed from trusted config
		data, err := os.ReadFile(nodeIDPath)
		if err == nil {
			c.NodeID = string(data)
		} else {
			c.NodeID = generateNodeID()

			err = os.WriteFile(nodeIDPath, []byte(c.NodeID), 0644)
			if err != nil {
				return fmt.Errorf("failed to write node ID: %w", err)
			}
		}
	}

	// Generate JWT secret if not set
	if c.Auth.JWTSecret == "" {
		jwtSecretPath := filepath.Join(c.DataDir, "jwt-secret")
		//nolint:gosec // G304: jwtSecretPath is constructed from trusted config
		data, err := os.ReadFile(jwtSecretPath)
		if err == nil {
			c.Auth.JWTSecret = string(data)
		} else {
			c.Auth.JWTSecret = generateSecret(32)

			err = os.WriteFile(jwtSecretPath, []byte(c.Auth.JWTSecret), 0600)
			if err != nil {
				return fmt.Errorf("failed to write JWT secret: %w", err)
			}
		}
	}

	// Validate root password - fail fast if missing or weak
	// Password must be: min 12 chars, with uppercase, lowercase, and number
	err = auth.ValidatePasswordStrength(c.Auth.RootPassword)
	if err != nil {
		return fmt.Errorf("invalid root password: %w. Set via NEBULAIO_AUTH_ROOT_PASSWORD environment variable", err)
	}

	// Generate or load replica ID if not set
	if c.Cluster.ReplicaID == 0 {
		replicaIDPath := filepath.Join(c.DataDir, "replica-id")
		//nolint:gosec // G304: replicaIDPath is constructed from trusted config
		data, err := os.ReadFile(replicaIDPath)
		if err == nil {
			// Parse stored replica ID
			var replicaID uint64

			_, parseErr := fmt.Sscanf(string(data), "%d", &replicaID)
			if parseErr == nil && replicaID > 0 {
				c.Cluster.ReplicaID = replicaID
			}
		}
		// Generate new replica ID if still not set
		if c.Cluster.ReplicaID == 0 {
			c.Cluster.ReplicaID = generateReplicaID()

			err = os.WriteFile(replicaIDPath, []byte(strconv.FormatUint(c.Cluster.ReplicaID, 10)), 0644)
			if err != nil {
				return fmt.Errorf("failed to write replica ID: %w", err)
			}
		}
	}

	// Set WAL directory if not specified
	if c.Cluster.WALDir == "" {
		c.Cluster.WALDir = filepath.Join(c.DataDir, "wal")
	}

	// Ensure WAL directory exists
	err = os.MkdirAll(c.Cluster.WALDir, 0750)
	if err != nil {
		return fmt.Errorf("failed to create WAL directory: %w", err)
	}

	// Validate placement group configuration
	err = c.Storage.PlacementGroups.validate()
	if err != nil {
		return fmt.Errorf("invalid placement group configuration: %w", err)
	}

	// Validate redundancy settings against available nodes
	err = c.validateRedundancyVsNodes()
	if err != nil {
		return fmt.Errorf("invalid redundancy configuration: %w", err)
	}

	return nil
}

// validateRedundancyVsNodes checks that redundancy settings are compatible with placement groups.
func (c *Config) validateRedundancyVsNodes() error {
	// Skip validation if redundancy is not enabled
	if !c.Storage.DefaultRedundancy.Enabled {
		return nil
	}

	totalShards := c.Storage.DefaultRedundancy.DataShards + c.Storage.DefaultRedundancy.ParityShards

	// Check MinNodesForErasure is sufficient for the default redundancy
	if c.Storage.PlacementGroups.MinNodesForErasure > 0 {
		if c.Storage.PlacementGroups.MinNodesForErasure < totalShards {
			return fmt.Errorf(
				"min_nodes_for_erasure (%d) is less than total shards required (%d data + %d parity = %d)",
				c.Storage.PlacementGroups.MinNodesForErasure,
				c.Storage.DefaultRedundancy.DataShards,
				c.Storage.DefaultRedundancy.ParityShards,
				totalShards,
			)
		}
	}

	// Check each placement group has enough min_nodes for the default redundancy
	for _, pg := range c.Storage.PlacementGroups.Groups {
		if pg.MinNodes > 0 && pg.MinNodes < totalShards {
			return fmt.Errorf(
				"placement group %q: min_nodes (%d) is less than total shards required (%d) for default redundancy",
				pg.ID, pg.MinNodes, totalShards,
			)
		}
		// If max_nodes is set, ensure it can accommodate the shards
		if pg.MaxNodes > 0 && pg.MaxNodes < totalShards {
			return fmt.Errorf(
				"placement group %q: max_nodes (%d) is less than total shards required (%d) for default redundancy",
				pg.ID, pg.MaxNodes, totalShards,
			)
		}
	}

	// Check replication factor doesn't exceed available placement groups
	if c.Storage.DefaultRedundancy.ReplicationFactor > 0 {
		availableTargets := len(c.Storage.PlacementGroups.ReplicationTargets)
		if availableTargets > 0 && c.Storage.DefaultRedundancy.ReplicationFactor > availableTargets {
			return fmt.Errorf(
				"replication_factor (%d) exceeds number of available replication targets (%d)",
				c.Storage.DefaultRedundancy.ReplicationFactor, availableTargets,
			)
		}
	}

	return nil
}

// validate checks placement group configuration for consistency.
func (c *PlacementGroupsConfig) validate() error {
	// Build a map of known group IDs
	groupIDs := make(map[string]bool)

	for _, g := range c.Groups {
		if g.ID == "" {
			return errors.New("placement group ID cannot be empty")
		}

		if groupIDs[g.ID] {
			return fmt.Errorf("duplicate placement group ID: %s", g.ID)
		}

		groupIDs[g.ID] = true

		// Validate MinNodes and MaxNodes
		if g.MinNodes < 0 {
			return fmt.Errorf("placement group %s: min_nodes cannot be negative", g.ID)
		}

		if g.MaxNodes < 0 {
			return fmt.Errorf("placement group %s: max_nodes cannot be negative", g.ID)
		}

		if g.MaxNodes > 0 && g.MinNodes > g.MaxNodes {
			return fmt.Errorf("placement group %s: min_nodes (%d) cannot exceed max_nodes (%d)", g.ID, g.MinNodes, g.MaxNodes)
		}
	}

	// Validate LocalGroupID references a known group (if groups are defined)
	if c.LocalGroupID != "" && len(c.Groups) > 0 {
		if !groupIDs[c.LocalGroupID] {
			return fmt.Errorf("local_group_id %q references unknown placement group", c.LocalGroupID)
		}
	}

	// Validate ReplicationTargets reference known groups
	for _, targetID := range c.ReplicationTargets {
		if len(c.Groups) > 0 && !groupIDs[targetID] {
			return fmt.Errorf("replication_target %q references unknown placement group", targetID)
		}
	}

	// Validate MinNodesForErasure
	if c.MinNodesForErasure < 0 {
		return errors.New("min_nodes_for_erasure cannot be negative")
	}

	return nil
}

func generateNodeID() string {
	return "node-" + generateSecret(8)
}

func generateReplicaID() uint64 {
	// Generate a unique replica ID based on process ID and timestamp
	// This creates a reasonably unique ID for Dragonboat
	//nolint:gosec // G115: pid is always positive
	pid := uint64(os.Getpid())
	//nolint:gosec // G115: uid is always positive
	uid := uint64(os.Getuid())
	// Combine pid and uid to create a unique replica ID
	// Use simple hash to ensure it's a positive uint64
	replicaID := (pid << 32) | uid
	if replicaID == 0 {
		replicaID = 1
	}

	return replicaID
}

func generateSecret(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[int(randomByte())%len(charset)]
	}

	return string(b)
}

func randomByte() byte {
	var b [1]byte

	_, err := rand.Read(b[:])
	if err != nil {
		// This should never happen with crypto/rand, but if it does,
		// panic is appropriate since we cannot safely generate secrets
		panic(fmt.Sprintf("failed to generate random bytes: %v", err))
	}

	return b[0]
}
