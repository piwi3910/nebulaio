package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for NebulaIO
type Config struct {
	// Node identification
	NodeID   string `mapstructure:"node_id"`
	NodeName string `mapstructure:"node_name"`

	// Data storage
	DataDir string `mapstructure:"data_dir"`

	// Network ports
	S3Port      int `mapstructure:"s3_port"`
	AdminPort   int `mapstructure:"admin_port"`
	ConsolePort int `mapstructure:"console_port"`

	// Cluster configuration
	Cluster ClusterConfig `mapstructure:"cluster"`

	// Storage configuration
	Storage StorageConfig `mapstructure:"storage"`

	// Cache configuration
	Cache CacheConfig `mapstructure:"cache"`

	// Firewall configuration
	Firewall FirewallConfig `mapstructure:"firewall"`

	// Audit configuration
	Audit AuditConfig `mapstructure:"audit"`

	// Auth configuration
	Auth AuthConfig `mapstructure:"auth"`

	// S3 Express configuration (high-performance S3)
	S3Express S3ExpressConfig `mapstructure:"s3_express"`

	// Iceberg configuration (table format)
	Iceberg IcebergConfig `mapstructure:"iceberg"`

	// MCP Server configuration (AI agents)
	MCP MCPConfig `mapstructure:"mcp"`

	// GPUDirect configuration (GPU-to-storage)
	GPUDirect GPUDirectConfig `mapstructure:"gpudirect"`

	// DPU configuration (BlueField SmartNIC)
	DPU DPUConfig `mapstructure:"dpu"`

	// RDMA configuration (remote direct memory access)
	RDMA RDMAConfig `mapstructure:"rdma"`

	// NIM configuration (NVIDIA Inference Microservices)
	NIM NIMConfig `mapstructure:"nim"`

	// Logging
	LogLevel string `mapstructure:"log_level"`
}

// ClusterConfig holds cluster-related configuration
type ClusterConfig struct {
	// Bootstrap indicates if this node should bootstrap a new cluster
	Bootstrap bool `mapstructure:"bootstrap"`

	// JoinAddresses is a list of existing cluster nodes to join (gossip addresses)
	JoinAddresses []string `mapstructure:"join_addresses"`

	// AdvertiseAddress is the address advertised to other nodes
	// If empty, the system will try to detect the outbound IP
	AdvertiseAddress string `mapstructure:"advertise_address"`

	// RaftPort is the port used for Raft consensus
	RaftPort int `mapstructure:"raft_port"`

	// GossipPort is the port used for gossip-based node discovery
	GossipPort int `mapstructure:"gossip_port"`

	// NodeRole is the role of this node: "gateway" or "storage"
	// Gateway nodes handle S3 requests, storage nodes store data
	// Default is "storage" which does both
	NodeRole string `mapstructure:"node_role"`

	// ClusterName is an optional name for the cluster
	ClusterName string `mapstructure:"cluster_name"`

	// ExpectNodes is the expected number of nodes for initial cluster formation
	// Only used during bootstrap to wait for the expected number of nodes
	ExpectNodes int `mapstructure:"expect_nodes"`

	// RetryJoinMaxAttempts is the maximum number of join attempts
	RetryJoinMaxAttempts int `mapstructure:"retry_join_max_attempts"`

	// RetryJoinInterval is the interval between join attempts
	RetryJoinInterval time.Duration `mapstructure:"retry_join_interval"`
}

// StorageConfig holds storage-related configuration
type StorageConfig struct {
	// Backend type: "fs" (filesystem), "erasure" (distributed)
	Backend string `mapstructure:"backend"`

	// DefaultStorageClass for new buckets
	DefaultStorageClass string `mapstructure:"default_storage_class"`

	// MaxObjectSize in bytes (default: 5TB)
	MaxObjectSize int64 `mapstructure:"max_object_size"`

	// MultipartPartSize default size in bytes
	MultipartPartSize int64 `mapstructure:"multipart_part_size"`
}

// CacheConfig holds DRAM cache configuration for high-performance workloads
type CacheConfig struct {
	// Enabled enables the DRAM cache
	Enabled bool `mapstructure:"enabled"`

	// MaxSize is the maximum cache size in bytes (default: 8GB)
	MaxSize int64 `mapstructure:"max_size"`

	// ShardCount is the number of cache shards for lock reduction (default: 256)
	ShardCount int `mapstructure:"shard_count"`

	// EntryMaxSize is the maximum size for a single cache entry (default: 256MB)
	EntryMaxSize int64 `mapstructure:"entry_max_size"`

	// TTL is the default time-to-live in seconds (default: 3600)
	TTL int `mapstructure:"ttl"`

	// EvictionPolicy is the cache eviction policy: lru, lfu, arc (default: arc)
	EvictionPolicy string `mapstructure:"eviction_policy"`

	// PrefetchEnabled enables predictive prefetching for AI/ML workloads
	PrefetchEnabled bool `mapstructure:"prefetch_enabled"`

	// PrefetchThreshold is the access count before enabling prefetch
	PrefetchThreshold int `mapstructure:"prefetch_threshold"`

	// PrefetchAhead is the number of chunks to prefetch ahead
	PrefetchAhead int `mapstructure:"prefetch_ahead"`

	// ZeroCopyEnabled enables zero-copy reads where supported
	ZeroCopyEnabled bool `mapstructure:"zero_copy_enabled"`

	// DistributedMode enables distributed cache across cluster nodes
	DistributedMode bool `mapstructure:"distributed_mode"`

	// ReplicationFactor for distributed cache (default: 2)
	ReplicationFactor int `mapstructure:"replication_factor"`

	// WarmupEnabled enables cache warmup on startup
	WarmupEnabled bool `mapstructure:"warmup_enabled"`

	// WarmupKeys are the keys to pre-warm on startup
	WarmupKeys []string `mapstructure:"warmup_keys"`
}

// FirewallConfig holds data firewall configuration for QoS and rate limiting
type FirewallConfig struct {
	// Enabled enables the data firewall
	Enabled bool `mapstructure:"enabled"`

	// DefaultPolicy is the default action: allow, deny
	DefaultPolicy string `mapstructure:"default_policy"`

	// RateLimiting configures request rate limiting
	RateLimiting RateLimitingConfig `mapstructure:"rate_limiting"`

	// Bandwidth configures bandwidth throttling
	Bandwidth BandwidthConfig `mapstructure:"bandwidth"`

	// Connections configures connection limits
	Connections ConnectionsConfig `mapstructure:"connections"`

	// IPAllowlist is a list of allowed IP addresses/CIDRs
	IPAllowlist []string `mapstructure:"ip_allowlist"`

	// IPBlocklist is a list of blocked IP addresses/CIDRs
	IPBlocklist []string `mapstructure:"ip_blocklist"`

	// AuditEnabled enables firewall audit logging
	AuditEnabled bool `mapstructure:"audit_enabled"`
}

// RateLimitingConfig configures request rate limiting
type RateLimitingConfig struct {
	// Enabled enables rate limiting
	Enabled bool `mapstructure:"enabled"`

	// RequestsPerSecond is the default requests per second limit
	RequestsPerSecond int `mapstructure:"requests_per_second"`

	// BurstSize is the maximum burst size
	BurstSize int `mapstructure:"burst_size"`

	// PerUser enables per-user rate limiting
	PerUser bool `mapstructure:"per_user"`

	// PerIP enables per-IP rate limiting
	PerIP bool `mapstructure:"per_ip"`

	// PerBucket enables per-bucket rate limiting
	PerBucket bool `mapstructure:"per_bucket"`
}

// BandwidthConfig configures bandwidth throttling
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

// ConnectionsConfig configures connection limits
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

// AuditConfig holds enhanced audit logging configuration
type AuditConfig struct {
	// Enabled enables audit logging
	Enabled bool `mapstructure:"enabled"`

	// ComplianceMode sets the compliance standard (none, soc2, pci, hipaa, gdpr, fedramp)
	ComplianceMode string `mapstructure:"compliance_mode"`

	// FilePath is the path to the audit log file
	FilePath string `mapstructure:"file_path"`

	// RetentionDays is how long to keep audit logs
	RetentionDays int `mapstructure:"retention_days"`

	// BufferSize is the async buffer size
	BufferSize int `mapstructure:"buffer_size"`

	// IntegrityEnabled enables cryptographic integrity verification
	IntegrityEnabled bool `mapstructure:"integrity_enabled"`

	// IntegritySecret is the HMAC secret for integrity (auto-generated if empty)
	IntegritySecret string `mapstructure:"integrity_secret"`

	// MaskSensitiveData masks sensitive data like passwords
	MaskSensitiveData bool `mapstructure:"mask_sensitive_data"`

	// Rotation configures log rotation
	Rotation AuditRotationConfig `mapstructure:"rotation"`

	// Webhook configures webhook output
	Webhook AuditWebhookConfig `mapstructure:"webhook"`
}

// AuditRotationConfig configures audit log rotation
type AuditRotationConfig struct {
	// Enabled enables log rotation
	Enabled bool `mapstructure:"enabled"`

	// MaxSizeMB is the max file size before rotation
	MaxSizeMB int `mapstructure:"max_size_mb"`

	// MaxBackups is the max number of old files to keep
	MaxBackups int `mapstructure:"max_backups"`

	// MaxAgeDays is the max age of old files
	MaxAgeDays int `mapstructure:"max_age_days"`

	// Compress compresses rotated files
	Compress bool `mapstructure:"compress"`
}

// AuditWebhookConfig configures audit webhook output
type AuditWebhookConfig struct {
	// Enabled enables webhook output
	Enabled bool `mapstructure:"enabled"`

	// URL is the webhook endpoint URL
	URL string `mapstructure:"url"`

	// AuthToken for authenticated webhooks
	AuthToken string `mapstructure:"auth_token"`

	// BatchSize for batched outputs
	BatchSize int `mapstructure:"batch_size"`

	// FlushIntervalSeconds is how often to flush batches
	FlushIntervalSeconds int `mapstructure:"flush_interval_seconds"`
}

// AuthConfig holds authentication configuration
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

// S3ExpressConfig holds S3 Express One Zone configuration
type S3ExpressConfig struct {
	// Enabled enables S3 Express One Zone support
	Enabled bool `mapstructure:"enabled"`

	// Zones defines the available express zones
	Zones []ExpressZoneConfig `mapstructure:"zones"`

	// DefaultZone is the default zone for new directory buckets
	DefaultZone string `mapstructure:"default_zone"`

	// SessionDuration is the session token duration in seconds
	SessionDuration int `mapstructure:"session_duration"`

	// MaxAppendSize is the maximum size for atomic append operations
	MaxAppendSize int64 `mapstructure:"max_append_size"`

	// EnableAtomicAppend enables atomic append operations
	EnableAtomicAppend bool `mapstructure:"enable_atomic_append"`
}

// ExpressZoneConfig defines an S3 Express zone
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

// IcebergConfig holds Apache Iceberg table format configuration
type IcebergConfig struct {
	// Enabled enables Iceberg table support
	Enabled bool `mapstructure:"enabled"`

	// CatalogType is the catalog implementation (rest, hive, glue)
	CatalogType string `mapstructure:"catalog_type"`

	// CatalogURI is the catalog service URI
	CatalogURI string `mapstructure:"catalog_uri"`

	// Warehouse is the default warehouse location
	Warehouse string `mapstructure:"warehouse"`

	// DefaultFileFormat is the default file format (parquet, orc, avro)
	DefaultFileFormat string `mapstructure:"default_file_format"`

	// MetadataPath is where table metadata is stored
	MetadataPath string `mapstructure:"metadata_path"`

	// SnapshotRetention is how many snapshots to retain
	SnapshotRetention int `mapstructure:"snapshot_retention"`

	// ExpireSnapshotsOlderThan in hours
	ExpireSnapshotsOlderThan int `mapstructure:"expire_snapshots_older_than"`

	// EnableACID enables ACID transaction support
	EnableACID bool `mapstructure:"enable_acid"`
}

// MCPConfig holds Model Context Protocol server configuration
type MCPConfig struct {
	// Enabled enables the MCP server
	Enabled bool `mapstructure:"enabled"`

	// Port is the MCP server port
	Port int `mapstructure:"port"`

	// MaxConnections is the maximum concurrent connections
	MaxConnections int `mapstructure:"max_connections"`

	// EnableTools enables tool execution
	EnableTools bool `mapstructure:"enable_tools"`

	// EnableResources enables resource access
	EnableResources bool `mapstructure:"enable_resources"`

	// EnablePrompts enables prompt templates
	EnablePrompts bool `mapstructure:"enable_prompts"`

	// AllowedOrigins for CORS
	AllowedOrigins []string `mapstructure:"allowed_origins"`

	// AuthRequired requires authentication for MCP access
	AuthRequired bool `mapstructure:"auth_required"`

	// RateLimitPerMinute is requests per minute limit
	RateLimitPerMinute int `mapstructure:"rate_limit_per_minute"`
}

// GPUDirectConfig holds GPUDirect Storage configuration
type GPUDirectConfig struct {
	// Enabled enables GPUDirect Storage support
	Enabled bool `mapstructure:"enabled"`

	// Devices is a list of GPU device IDs to use
	Devices []int `mapstructure:"devices"`

	// BufferPoolSize is the GPU buffer pool size in bytes
	BufferPoolSize int64 `mapstructure:"buffer_pool_size"`

	// MaxTransferSize is the maximum single transfer size
	MaxTransferSize int64 `mapstructure:"max_transfer_size"`

	// EnableAsync enables asynchronous transfers
	EnableAsync bool `mapstructure:"enable_async"`

	// CUDAStreamCount is the number of CUDA streams per GPU
	CUDAStreamCount int `mapstructure:"cuda_stream_count"`

	// EnableP2P enables peer-to-peer GPU transfers
	EnableP2P bool `mapstructure:"enable_p2p"`

	// NVMePath is the path pattern for NVMe devices
	NVMePath string `mapstructure:"nvme_path"`
}

// DPUConfig holds BlueField DPU configuration
type DPUConfig struct {
	// Enabled enables DPU offload support
	Enabled bool `mapstructure:"enabled"`

	// DeviceIndex is the DPU device index to use
	DeviceIndex int `mapstructure:"device_index"`

	// EnableCrypto enables crypto offload
	EnableCrypto bool `mapstructure:"enable_crypto"`

	// EnableCompression enables compression offload
	EnableCompression bool `mapstructure:"enable_compression"`

	// EnableStorage enables storage offload
	EnableStorage bool `mapstructure:"enable_storage"`

	// EnableNetwork enables network offload
	EnableNetwork bool `mapstructure:"enable_network"`

	// EnableRDMA enables RDMA offload via DPU
	EnableRDMA bool `mapstructure:"enable_rdma"`

	// EnableRegex enables regex offload
	EnableRegex bool `mapstructure:"enable_regex"`

	// HealthCheckInterval in seconds
	HealthCheckInterval int `mapstructure:"health_check_interval"`

	// FallbackOnError falls back to CPU on DPU errors
	FallbackOnError bool `mapstructure:"fallback_on_error"`

	// MinSizeForOffload minimum data size to offload (bytes)
	MinSizeForOffload int `mapstructure:"min_size_for_offload"`
}

// RDMAConfig holds RDMA transport configuration
type RDMAConfig struct {
	// Enabled enables RDMA transport
	Enabled bool `mapstructure:"enabled"`

	// Port is the RDMA listener port
	Port int `mapstructure:"port"`

	// DeviceName is the RDMA device name (e.g., "mlx5_0")
	DeviceName string `mapstructure:"device_name"`

	// GIDIndex is the GID index for RoCE
	GIDIndex int `mapstructure:"gid_index"`

	// MaxSendWR is max send work requests per QP
	MaxSendWR int `mapstructure:"max_send_wr"`

	// MaxRecvWR is max receive work requests per QP
	MaxRecvWR int `mapstructure:"max_recv_wr"`

	// MaxSendSGE is max scatter/gather elements per send
	MaxSendSGE int `mapstructure:"max_send_sge"`

	// MaxRecvSGE is max scatter/gather elements per receive
	MaxRecvSGE int `mapstructure:"max_recv_sge"`

	// MaxInlineData is max inline data size
	MaxInlineData int `mapstructure:"max_inline_data"`

	// MemoryPoolSize is the registered memory pool size
	MemoryPoolSize int64 `mapstructure:"memory_pool_size"`

	// EnableZeroCopy enables zero-copy data transfers
	EnableZeroCopy bool `mapstructure:"enable_zero_copy"`

	// FallbackToTCP falls back to TCP if RDMA unavailable
	FallbackToTCP bool `mapstructure:"fallback_to_tcp"`
}

// NIMConfig holds NVIDIA NIM Microservices configuration
type NIMConfig struct {
	// Enabled enables NIM integration
	Enabled bool `mapstructure:"enabled"`

	// Endpoints are NIM server endpoints
	Endpoints []string `mapstructure:"endpoints"`

	// APIKey is the NVIDIA API key
	APIKey string `mapstructure:"api_key"`

	// OrganizationID is the NVIDIA organization ID
	OrganizationID string `mapstructure:"organization_id"`

	// DefaultModel is the default model for inference
	DefaultModel string `mapstructure:"default_model"`

	// Timeout is the inference timeout in seconds
	Timeout int `mapstructure:"timeout"`

	// MaxRetries for failed requests
	MaxRetries int `mapstructure:"max_retries"`

	// MaxBatchSize for batch inference
	MaxBatchSize int `mapstructure:"max_batch_size"`

	// EnableStreaming enables streaming responses
	EnableStreaming bool `mapstructure:"enable_streaming"`

	// CacheResults enables inference result caching
	CacheResults bool `mapstructure:"cache_results"`

	// CacheTTL is cache TTL in seconds
	CacheTTL int `mapstructure:"cache_ttl"`

	// EnableMetrics enables detailed metrics
	EnableMetrics bool `mapstructure:"enable_metrics"`

	// ProcessOnUpload triggers inference on object upload
	ProcessOnUpload bool `mapstructure:"process_on_upload"`

	// ProcessContentTypes are content types to process
	ProcessContentTypes []string `mapstructure:"process_content_types"`
}

// Options are command line overrides
type Options struct {
	DataDir     string
	S3Port      int
	AdminPort   int
	ConsolePort int
}

// Load loads configuration from file and applies command line options
func Load(configPath string, opts Options) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Load from config file if specified
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
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
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate and set derived values
	if err := cfg.validate(); err != nil {
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

	// Storage defaults
	v.SetDefault("storage.backend", "fs")
	v.SetDefault("storage.default_storage_class", "STANDARD")
	v.SetDefault("storage.max_object_size", 5*1024*1024*1024*1024) // 5TB
	v.SetDefault("storage.multipart_part_size", 64*1024*1024)      // 64MB

	// Cache defaults (DRAM Cache)
	v.SetDefault("cache.enabled", false)
	v.SetDefault("cache.max_size", 8*1024*1024*1024)   // 8GB
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
	v.SetDefault("firewall.bandwidth.max_bytes_per_second", 1024*1024*1024)      // 1 GB/s
	v.SetDefault("firewall.bandwidth.max_bytes_per_second_per_user", 100*1024*1024)  // 100 MB/s
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
	v.SetDefault("auth.root_user", "admin")
	v.SetDefault("auth.root_password", "admin123") // Should be changed!
	v.SetDefault("auth.token_expiry", 60)          // 1 hour
	v.SetDefault("auth.refresh_token_expiry", 168) // 7 days

	// S3 Express defaults
	v.SetDefault("s3_express.enabled", false)
	v.SetDefault("s3_express.default_zone", "use1-az1")
	v.SetDefault("s3_express.session_duration", 3600)              // 1 hour
	v.SetDefault("s3_express.max_append_size", 5*1024*1024*1024)   // 5GB
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
	v.SetDefault("gpudirect.buffer_pool_size", 1024*1024*1024)    // 1GB
	v.SetDefault("gpudirect.max_transfer_size", 256*1024*1024)    // 256MB
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
	v.SetDefault("rdma.memory_pool_size", 1024*1024*1024)  // 1GB
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
	v.SetDefault("nim.cache_ttl", 3600)  // 1 hour
	v.SetDefault("nim.enable_metrics", true)
	v.SetDefault("nim.process_on_upload", false)
	v.SetDefault("nim.process_content_types", []string{"image/jpeg", "image/png", "text/plain", "application/json"})

	// Logging
	v.SetDefault("log_level", "info")
}

func (c *Config) validate() error {
	// Ensure data directory exists
	if err := os.MkdirAll(c.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Generate node ID if not set
	if c.NodeID == "" {
		nodeIDPath := filepath.Join(c.DataDir, "node-id")
		if data, err := os.ReadFile(nodeIDPath); err == nil {
			c.NodeID = string(data)
		} else {
			c.NodeID = generateNodeID()
			if err := os.WriteFile(nodeIDPath, []byte(c.NodeID), 0644); err != nil {
				return fmt.Errorf("failed to write node ID: %w", err)
			}
		}
	}

	// Generate JWT secret if not set
	if c.Auth.JWTSecret == "" {
		jwtSecretPath := filepath.Join(c.DataDir, "jwt-secret")
		if data, err := os.ReadFile(jwtSecretPath); err == nil {
			c.Auth.JWTSecret = string(data)
		} else {
			c.Auth.JWTSecret = generateSecret(32)
			if err := os.WriteFile(jwtSecretPath, []byte(c.Auth.JWTSecret), 0600); err != nil {
				return fmt.Errorf("failed to write JWT secret: %w", err)
			}
		}
	}

	return nil
}

func generateNodeID() string {
	return fmt.Sprintf("node-%s", generateSecret(8))
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
	_, _ = os.Stdin.Read(b[:])
	// Fallback to time-based if stdin fails
	return byte(os.Getpid() ^ int(os.Getuid()))
}
