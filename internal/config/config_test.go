package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piwi3910/nebulaio/internal/auth"
)

// testPassword is a valid password for testing that meets all requirements:
// - 12+ characters, uppercase, lowercase, number.
const testPassword = "TestPassword123"

// setTestPassword sets a valid password environment variable for tests
// Returns a cleanup function to restore the original value.
func setTestPassword(t *testing.T) {
	t.Helper()

	originalValue := os.Getenv("NEBULAIO_AUTH_ROOT_PASSWORD")
	os.Setenv("NEBULAIO_AUTH_ROOT_PASSWORD", testPassword)
	t.Cleanup(func() {
		if originalValue == "" {
			os.Unsetenv("NEBULAIO_AUTH_ROOT_PASSWORD")
		} else {
			os.Setenv("NEBULAIO_AUTH_ROOT_PASSWORD", originalValue)
		}
	})
}

func TestLoadDefaultConfig(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify defaults are set
	assert.Equal(t, 9000, cfg.S3Port)
	assert.Equal(t, 9001, cfg.AdminPort)
	assert.Equal(t, 9002, cfg.ConsolePort)
	assert.Equal(t, tempDir, cfg.DataDir)
	assert.Equal(t, "fs", cfg.Storage.Backend)
	assert.Equal(t, "STANDARD", cfg.Storage.DefaultStorageClass)
	assert.True(t, cfg.TLS.Enabled)
	assert.True(t, cfg.TLS.AutoGenerate)
	assert.True(t, cfg.Audit.Enabled)
}

func TestLoadWithOptions(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir:     tempDir,
		S3Port:      8000,
		AdminPort:   8001,
		ConsolePort: 8002,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Command line options should override defaults
	assert.Equal(t, 8000, cfg.S3Port)
	assert.Equal(t, 8001, cfg.AdminPort)
	assert.Equal(t, 8002, cfg.ConsolePort)
}

func TestLoadFromConfigFile(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nebulaio.yaml")

	// Create a test config file
	configContent := `
s3_port: 7000
admin_port: 7001
console_port: 7002
log_level: debug
storage:
  backend: volume
  default_storage_class: STANDARD_IA
tls:
  enabled: false
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load(configPath, opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Config file values should be loaded
	assert.Equal(t, 7000, cfg.S3Port)
	assert.Equal(t, 7001, cfg.AdminPort)
	assert.Equal(t, 7002, cfg.ConsolePort)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "volume", cfg.Storage.Backend)
	assert.Equal(t, "STANDARD_IA", cfg.Storage.DefaultStorageClass)
	assert.False(t, cfg.TLS.Enabled)
}

func TestLoadWithEnvironmentVariables(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()

	// Set environment variables
	os.Setenv("NEBULAIO_S3_PORT", "6000")

	os.Setenv("NEBULAIO_LOG_LEVEL", "warn")
	defer os.Unsetenv("NEBULAIO_S3_PORT")
	defer os.Unsetenv("NEBULAIO_LOG_LEVEL")

	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Environment variables should override defaults
	assert.Equal(t, 6000, cfg.S3Port)
	assert.Equal(t, "warn", cfg.LogLevel)
}

func TestLoadInvalidConfigFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml", Options{})
	assert.Error(t, err)
}

func TestValidateCreatesDataDirectory(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	newDataDir := filepath.Join(tempDir, "nested", "data")

	opts := Options{
		DataDir: newDataDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Data directory should be created
	info, err := os.Stat(newDataDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNodeIDGeneration(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Node ID should be generated
	assert.NotEmpty(t, cfg.NodeID)
	assert.Greater(t, len(cfg.NodeID), 5, "NodeID should be a reasonable length")

	// Node ID file should exist
	nodeIDPath := filepath.Join(tempDir, "node-id")
	assert.FileExists(t, nodeIDPath)

	// Loading again should return the same Node ID
	cfg2, err := Load("", opts)
	require.NoError(t, err)
	assert.Equal(t, cfg.NodeID, cfg2.NodeID)
}

func TestJWTSecretGeneration(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// JWT secret should be generated
	assert.NotEmpty(t, cfg.Auth.JWTSecret)

	// JWT secret file should exist
	jwtSecretPath := filepath.Join(tempDir, "jwt-secret")
	assert.FileExists(t, jwtSecretPath)

	// Loading again should return the same secret
	cfg2, err := Load("", opts)
	require.NoError(t, err)
	assert.Equal(t, cfg.Auth.JWTSecret, cfg2.Auth.JWTSecret)
}

func TestReplicaIDGeneration(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Replica ID should be generated
	assert.NotZero(t, cfg.Cluster.ReplicaID)

	// Replica ID file should exist
	replicaIDPath := filepath.Join(tempDir, "replica-id")
	assert.FileExists(t, replicaIDPath)

	// Loading again should return the same replica ID
	cfg2, err := Load("", opts)
	require.NoError(t, err)
	assert.Equal(t, cfg.Cluster.ReplicaID, cfg2.Cluster.ReplicaID)
}

func TestWALDirectoryCreation(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// WAL directory should be set and created
	expectedWALDir := filepath.Join(tempDir, "wal")
	assert.Equal(t, expectedWALDir, cfg.Cluster.WALDir)

	info, err := os.Stat(expectedWALDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestClusterDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify cluster defaults
	assert.True(t, cfg.Cluster.Bootstrap)
	assert.Equal(t, 9003, cfg.Cluster.RaftPort)
	assert.Equal(t, 9004, cfg.Cluster.GossipPort)
	assert.Equal(t, "storage", cfg.Cluster.NodeRole)
	assert.Equal(t, "nebulaio", cfg.Cluster.ClusterName)
	assert.Equal(t, 1, cfg.Cluster.ExpectNodes)
	assert.Equal(t, 10, cfg.Cluster.RetryJoinMaxAttempts)
	assert.Equal(t, 5*time.Second, cfg.Cluster.RetryJoinInterval)
	assert.Equal(t, uint64(1), cfg.Cluster.ShardID)
	assert.Equal(t, uint64(10000), cfg.Cluster.SnapshotCount)
	assert.Equal(t, uint64(5000), cfg.Cluster.CompactionOverhead)
}

func TestStorageDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify storage defaults
	assert.Equal(t, "fs", cfg.Storage.Backend)
	assert.Equal(t, "STANDARD", cfg.Storage.DefaultStorageClass)
	assert.Equal(t, int64(5*1024*1024*1024*1024), cfg.Storage.MaxObjectSize) // 5TB
	assert.Equal(t, int64(64*1024*1024), cfg.Storage.MultipartPartSize)      // 64MB

	// Volume defaults
	assert.Equal(t, uint64(32*1024*1024*1024), cfg.Storage.Volume.MaxVolumeSize) // 32GB
	assert.True(t, cfg.Storage.Volume.AutoCreate)

	// DirectIO defaults
	assert.True(t, cfg.Storage.Volume.DirectIO.Enabled)
	assert.Equal(t, 4096, cfg.Storage.Volume.DirectIO.BlockAlignment)
	assert.True(t, cfg.Storage.Volume.DirectIO.UseMemoryPool)
	assert.True(t, cfg.Storage.Volume.DirectIO.FallbackOnError)

	// Raw device defaults
	assert.False(t, cfg.Storage.Volume.RawDevices.Enabled)
	assert.True(t, cfg.Storage.Volume.RawDevices.Safety.CheckFilesystem)
	assert.True(t, cfg.Storage.Volume.RawDevices.Safety.RequireConfirmation)
	assert.True(t, cfg.Storage.Volume.RawDevices.Safety.WriteSignature)
	assert.True(t, cfg.Storage.Volume.RawDevices.Safety.ExclusiveLock)
}

func TestCacheDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify cache defaults
	assert.False(t, cfg.Cache.Enabled)
	assert.Equal(t, int64(8*1024*1024*1024), cfg.Cache.MaxSize) // 8GB
	assert.Equal(t, 256, cfg.Cache.ShardCount)
	assert.Equal(t, int64(256*1024*1024), cfg.Cache.EntryMaxSize) // 256MB
	assert.Equal(t, 3600, cfg.Cache.TTL)                          // 1 hour
	assert.Equal(t, "arc", cfg.Cache.EvictionPolicy)
	assert.True(t, cfg.Cache.PrefetchEnabled)
	assert.Equal(t, 2, cfg.Cache.PrefetchThreshold)
	assert.Equal(t, 4, cfg.Cache.PrefetchAhead)
	assert.True(t, cfg.Cache.ZeroCopyEnabled)
	assert.False(t, cfg.Cache.DistributedMode)
	assert.Equal(t, 2, cfg.Cache.ReplicationFactor)
}

func TestFirewallDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify firewall defaults
	assert.False(t, cfg.Firewall.Enabled)
	assert.Equal(t, "allow", cfg.Firewall.DefaultPolicy)
	assert.True(t, cfg.Firewall.AuditEnabled)

	// Rate limiting
	assert.False(t, cfg.Firewall.RateLimiting.Enabled)
	assert.Equal(t, 1000, cfg.Firewall.RateLimiting.RequestsPerSecond)
	assert.Equal(t, 100, cfg.Firewall.RateLimiting.BurstSize)
	assert.True(t, cfg.Firewall.RateLimiting.PerUser)
	assert.True(t, cfg.Firewall.RateLimiting.PerIP)
	assert.False(t, cfg.Firewall.RateLimiting.PerBucket)

	// Bandwidth
	assert.False(t, cfg.Firewall.Bandwidth.Enabled)
	assert.Equal(t, int64(1024*1024*1024), cfg.Firewall.Bandwidth.MaxBytesPerSecond)

	// Connections
	assert.False(t, cfg.Firewall.Connections.Enabled)
	assert.Equal(t, 10000, cfg.Firewall.Connections.MaxConnections)
	assert.Equal(t, 100, cfg.Firewall.Connections.MaxConnectionsPerIP)
	assert.Equal(t, 500, cfg.Firewall.Connections.MaxConnectionsPerUser)
	assert.Equal(t, 60, cfg.Firewall.Connections.IdleTimeoutSeconds)
}

func TestAuditDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify audit defaults
	assert.True(t, cfg.Audit.Enabled)
	assert.Equal(t, "none", cfg.Audit.ComplianceMode)
	assert.Equal(t, 90, cfg.Audit.RetentionDays)
	assert.Equal(t, 10000, cfg.Audit.BufferSize)
	assert.True(t, cfg.Audit.IntegrityEnabled)
	assert.True(t, cfg.Audit.MaskSensitiveData)

	// Rotation
	assert.True(t, cfg.Audit.Rotation.Enabled)
	assert.Equal(t, 100, cfg.Audit.Rotation.MaxSizeMB)
	assert.Equal(t, 10, cfg.Audit.Rotation.MaxBackups)
	assert.Equal(t, 30, cfg.Audit.Rotation.MaxAgeDays)
	assert.True(t, cfg.Audit.Rotation.Compress)

	// Webhook
	assert.False(t, cfg.Audit.Webhook.Enabled)
	assert.Equal(t, 100, cfg.Audit.Webhook.BatchSize)
	assert.Equal(t, 30, cfg.Audit.Webhook.FlushIntervalSeconds)
}

func TestAuthDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify auth defaults
	assert.Equal(t, "admin", cfg.Auth.RootUser)
	assert.Equal(t, testPassword, cfg.Auth.RootPassword) // Set via env var
	assert.Equal(t, 60, cfg.Auth.TokenExpiry)            // 1 hour
	assert.Equal(t, 168, cfg.Auth.RefreshTokenExpiry)    // 7 days
}

func TestPasswordValidation(t *testing.T) {
	tests := []struct {
		errorType   error
		name        string
		password    string
		expectError bool
	}{
		{
			name:        "empty password",
			password:    "",
			expectError: true,
			errorType:   auth.ErrPasswordEmpty,
		},
		{
			name:        "too short",
			password:    "Short1Aa",
			expectError: true,
			errorType:   auth.ErrPasswordTooShort,
		},
		{
			name:        "no uppercase",
			password:    "lowercaseonly123",
			expectError: true,
			errorType:   auth.ErrPasswordNoUpper,
		},
		{
			name:        "no lowercase",
			password:    "UPPERCASEONLY123",
			expectError: true,
			errorType:   auth.ErrPasswordNoLower,
		},
		{
			name:        "no number",
			password:    "NoNumbersHere",
			expectError: true,
			errorType:   auth.ErrPasswordNoNumber,
		},
		{
			name:        "valid password",
			password:    "ValidPassword123",
			expectError: false,
		},
		{
			name:        "valid password with special chars",
			password:    "Valid@Pass#123!",
			expectError: false,
		},
		{
			name:        "exactly 12 characters",
			password:    "Exactly12Aa1",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidatePasswordStrength(tt.password)
			if tt.expectError {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.errorType)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadFailsWithEmptyPassword(t *testing.T) {
	// Ensure no password is set
	os.Unsetenv("NEBULAIO_AUTH_ROOT_PASSWORD")

	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	_, err := Load("", opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid root password")
}

func TestLoadFailsWithWeakPassword(t *testing.T) {
	// Set a weak password
	os.Setenv("NEBULAIO_AUTH_ROOT_PASSWORD", "weak")
	defer os.Unsetenv("NEBULAIO_AUTH_ROOT_PASSWORD")

	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	_, err := Load("", opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid root password")
}

func TestEnvVarPrecedenceOverConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nebulaio.yaml")

	// Create a config file with one password
	configContent := `
auth:
  root_password: "ConfigFilePass123"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Set a different password via environment variable
	envPassword := "EnvVarPassword123"

	os.Setenv("NEBULAIO_AUTH_ROOT_PASSWORD", envPassword)
	defer os.Unsetenv("NEBULAIO_AUTH_ROOT_PASSWORD")

	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load(configPath, opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Environment variable should take precedence over config file
	assert.Equal(t, envPassword, cfg.Auth.RootPassword)
}

func TestTLSDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify TLS defaults (secure by default)
	assert.True(t, cfg.TLS.Enabled)
	assert.Equal(t, "1.2", cfg.TLS.MinVersion)
	assert.False(t, cfg.TLS.RequireClientCert)
	assert.True(t, cfg.TLS.AutoGenerate)
	assert.Equal(t, "NebulaIO", cfg.TLS.Organization)
	assert.Equal(t, 365, cfg.TLS.ValidityDays)
}

func TestFeatureFlagDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Features are disabled by default
	assert.False(t, cfg.S3Express.Enabled)
	assert.False(t, cfg.Iceberg.Enabled)
	assert.False(t, cfg.MCP.Enabled)
	assert.False(t, cfg.GPUDirect.Enabled)
	assert.False(t, cfg.DPU.Enabled)
	assert.False(t, cfg.RDMA.Enabled)
	assert.False(t, cfg.NIM.Enabled)
}

func TestS3ExpressDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// S3 Express defaults
	assert.False(t, cfg.S3Express.Enabled)
	assert.Equal(t, "use1-az1", cfg.S3Express.DefaultZone)
	assert.Equal(t, 3600, cfg.S3Express.SessionDuration)
	assert.Equal(t, int64(5*1024*1024*1024), cfg.S3Express.MaxAppendSize) // 5GB
	assert.True(t, cfg.S3Express.EnableAtomicAppend)
}

func TestIcebergDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Iceberg defaults
	assert.False(t, cfg.Iceberg.Enabled)
	assert.Equal(t, "rest", cfg.Iceberg.CatalogType)
	assert.Equal(t, "http://localhost:8181", cfg.Iceberg.CatalogURI)
	assert.Equal(t, "s3://warehouse/", cfg.Iceberg.Warehouse)
	assert.Equal(t, "parquet", cfg.Iceberg.DefaultFileFormat)
	assert.Equal(t, 10, cfg.Iceberg.SnapshotRetention)
	assert.Equal(t, 168, cfg.Iceberg.ExpireSnapshotsOlderThan) // 7 days
	assert.True(t, cfg.Iceberg.EnableACID)
}

func TestMCPDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// MCP defaults
	assert.False(t, cfg.MCP.Enabled)
	assert.Equal(t, 9005, cfg.MCP.Port)
	assert.Equal(t, 100, cfg.MCP.MaxConnections)
	assert.True(t, cfg.MCP.EnableTools)
	assert.True(t, cfg.MCP.EnableResources)
	assert.True(t, cfg.MCP.EnablePrompts)
	assert.True(t, cfg.MCP.AuthRequired)
	assert.Equal(t, 60, cfg.MCP.RateLimitPerMinute)
}

func TestGPUDirectDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// GPUDirect defaults
	assert.False(t, cfg.GPUDirect.Enabled)
	assert.Equal(t, int64(1024*1024*1024), cfg.GPUDirect.BufferPoolSize) // 1GB
	assert.Equal(t, int64(256*1024*1024), cfg.GPUDirect.MaxTransferSize) // 256MB
	assert.True(t, cfg.GPUDirect.EnableAsync)
	assert.Equal(t, 4, cfg.GPUDirect.CUDAStreamCount)
	assert.True(t, cfg.GPUDirect.EnableP2P)
	assert.Equal(t, "/dev/nvme*", cfg.GPUDirect.NVMePath)
}

func TestDPUDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// DPU defaults
	assert.False(t, cfg.DPU.Enabled)
	assert.Equal(t, 0, cfg.DPU.DeviceIndex)
	assert.True(t, cfg.DPU.EnableCrypto)
	assert.True(t, cfg.DPU.EnableCompression)
	assert.True(t, cfg.DPU.EnableStorage)
	assert.True(t, cfg.DPU.EnableNetwork)
	assert.True(t, cfg.DPU.EnableRDMA)
	assert.False(t, cfg.DPU.EnableRegex)
	assert.Equal(t, 30, cfg.DPU.HealthCheckInterval)
	assert.True(t, cfg.DPU.FallbackOnError)
	assert.Equal(t, 4096, cfg.DPU.MinSizeForOffload)
}

func TestRDMADefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// RDMA defaults
	assert.False(t, cfg.RDMA.Enabled)
	assert.Equal(t, 9100, cfg.RDMA.Port)
	assert.Equal(t, "mlx5_0", cfg.RDMA.DeviceName)
	assert.Equal(t, 0, cfg.RDMA.GIDIndex)
	assert.Equal(t, 128, cfg.RDMA.MaxSendWR)
	assert.Equal(t, 128, cfg.RDMA.MaxRecvWR)
	assert.Equal(t, 1, cfg.RDMA.MaxSendSGE)
	assert.Equal(t, 1, cfg.RDMA.MaxRecvSGE)
	assert.Equal(t, 64, cfg.RDMA.MaxInlineData)
	assert.Equal(t, int64(1024*1024*1024), cfg.RDMA.MemoryPoolSize) // 1GB
	assert.True(t, cfg.RDMA.EnableZeroCopy)
	assert.True(t, cfg.RDMA.FallbackToTCP)
}

func TestNIMDefaults(t *testing.T) {
	setTestPassword(t)
	tempDir := t.TempDir()
	opts := Options{
		DataDir: tempDir,
	}

	cfg, err := Load("", opts)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// NIM defaults
	assert.False(t, cfg.NIM.Enabled)
	assert.Contains(t, cfg.NIM.Endpoints, "https://integrate.api.nvidia.com/v1")
	assert.Equal(t, "meta/llama-3.1-8b-instruct", cfg.NIM.DefaultModel)
	assert.Equal(t, 60, cfg.NIM.Timeout)
	assert.Equal(t, 3, cfg.NIM.MaxRetries)
	assert.Equal(t, 100, cfg.NIM.MaxBatchSize)
	assert.True(t, cfg.NIM.EnableStreaming)
	assert.True(t, cfg.NIM.CacheResults)
	assert.Equal(t, 3600, cfg.NIM.CacheTTL) // 1 hour
	assert.True(t, cfg.NIM.EnableMetrics)
	assert.False(t, cfg.NIM.ProcessOnUpload)
}

func TestGenerateNodeID(t *testing.T) {
	id := generateNodeID()
	assert.NotEmpty(t, id)
	assert.Greater(t, len(id), 5, "NodeID should be a reasonable length")
	assert.Contains(t, id, "node-")
}

func TestGenerateReplicaID(t *testing.T) {
	id := generateReplicaID()
	assert.NotZero(t, id)
}

func TestGenerateSecret(t *testing.T) {
	// Generate several secrets
	secrets := make(map[string]bool)

	for range 10 {
		s := generateSecret(32)
		assert.Len(t, s, 32)
		secrets[s] = true
	}
	// While collisions are possible, they should be very rare
	// With a 32-character secret from a 62-character alphabet,
	// the probability of collision is extremely low
}
