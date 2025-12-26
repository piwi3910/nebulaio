package config

import (
	"fmt"
	"os"
	"path/filepath"

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

	// Auth configuration
	Auth AuthConfig `mapstructure:"auth"`

	// Logging
	LogLevel string `mapstructure:"log_level"`
}

// ClusterConfig holds cluster-related configuration
type ClusterConfig struct {
	// Bootstrap indicates if this node should bootstrap a new cluster
	Bootstrap bool `mapstructure:"bootstrap"`

	// JoinAddresses is a list of existing cluster nodes to join
	JoinAddresses []string `mapstructure:"join_addresses"`

	// AdvertiseAddress is the address advertised to other nodes
	AdvertiseAddress string `mapstructure:"advertise_address"`

	// RaftPort is the port used for Raft consensus
	RaftPort int `mapstructure:"raft_port"`
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

	// Storage defaults
	v.SetDefault("storage.backend", "fs")
	v.SetDefault("storage.default_storage_class", "STANDARD")
	v.SetDefault("storage.max_object_size", 5*1024*1024*1024*1024) // 5TB
	v.SetDefault("storage.multipart_part_size", 64*1024*1024)       // 64MB

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
