package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	// Enabled enables the enterprise DRAM cache
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

	// Cache defaults (Enterprise DRAM Cache)
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
