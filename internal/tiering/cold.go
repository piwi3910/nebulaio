package tiering

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// ColdStorageType represents the type of cold storage backend.
type ColdStorageType string

const (
	// ColdStorageS3 uses an S3-compatible backend.
	ColdStorageS3 ColdStorageType = "s3"
	// ColdStorageAzure uses Azure Blob Storage.
	ColdStorageAzure ColdStorageType = "azure"
	// ColdStorageGCS uses Google Cloud Storage.
	ColdStorageGCS ColdStorageType = "gcs"
	// ColdStorageFileSystem uses a local filesystem path.
	ColdStorageFileSystem ColdStorageType = "filesystem"
)

// ColdStorageConfig configures a cold storage backend.
type ColdStorageConfig struct {
	// Type of cold storage
	Type ColdStorageType `json:"type" yaml:"type"`

	// Name for this cold storage tier
	Name string `json:"name" yaml:"name"`

	// Enabled determines if this cold storage is active
	Enabled bool `json:"enabled" yaml:"enabled"`

	// S3 configuration
	S3 *S3ColdConfig `json:"s3,omitempty" yaml:"s3,omitempty"`

	// Azure configuration
	Azure *AzureColdConfig `json:"azure,omitempty" yaml:"azure,omitempty"`

	// GCS configuration
	GCS *GCSColdConfig `json:"gcs,omitempty" yaml:"gcs,omitempty"`

	// Filesystem configuration
	Filesystem *FilesystemColdConfig `json:"filesystem,omitempty" yaml:"filesystem,omitempty"`

	// RetentionDays is how long to keep objects in cold storage
	RetentionDays int `json:"retentionDays,omitempty" yaml:"retentionDays,omitempty"`

	// CompressionEnabled compresses objects before storing
	CompressionEnabled bool `json:"compressionEnabled,omitempty" yaml:"compressionEnabled,omitempty"`

	// EncryptionEnabled encrypts objects before storing
	EncryptionEnabled bool `json:"encryptionEnabled,omitempty" yaml:"encryptionEnabled,omitempty"`
}

// S3ColdConfig configures S3-compatible cold storage.
type S3ColdConfig struct {
	Endpoint        string `json:"endpoint"               yaml:"endpoint"`
	Region          string `json:"region"                 yaml:"region"`
	Bucket          string `json:"bucket"                 yaml:"bucket"`
	Prefix          string `json:"prefix,omitempty"       yaml:"prefix,omitempty"`
	AccessKeyID     string `json:"-"                      yaml:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"-"                      yaml:"secretAccessKey,omitempty"`
	UseSSL          bool   `json:"useSsl,omitempty"       yaml:"useSsl,omitempty"`
	StorageClass    string `json:"storageClass,omitempty" yaml:"storageClass,omitempty"`
}

// AzureColdConfig configures Azure Blob cold storage.
type AzureColdConfig struct {
	AccountName   string `json:"accountName"          yaml:"accountName"`
	AccountKey    string `json:"-"                    yaml:"accountKey,omitempty"`
	ContainerName string `json:"containerName"        yaml:"containerName"`
	Prefix        string `json:"prefix,omitempty"     yaml:"prefix,omitempty"`
	AccessTier    string `json:"accessTier,omitempty" yaml:"accessTier,omitempty"` // Hot, Cool, Cold, Archive
}

// GCSColdConfig configures Google Cloud Storage cold storage.
type GCSColdConfig struct {
	ProjectID       string `json:"projectId"              yaml:"projectId"`
	Bucket          string `json:"bucket"                 yaml:"bucket"`
	Prefix          string `json:"prefix,omitempty"       yaml:"prefix,omitempty"`
	CredentialsFile string `json:"-"                      yaml:"credentialsFile,omitempty"`
	StorageClass    string `json:"storageClass,omitempty" yaml:"storageClass,omitempty"` // STANDARD, NEARLINE, COLDLINE, ARCHIVE
}

// FilesystemColdConfig configures filesystem cold storage.
type FilesystemColdConfig struct {
	Path string `json:"path" yaml:"path"`
}

// ColdStorage represents a cold storage backend.
type ColdStorage struct {
	config  ColdStorageConfig
	backend backend.Backend
	mu      sync.RWMutex
	closed  bool
}

// NewColdStorage creates a new cold storage backend.
func NewColdStorage(config ColdStorageConfig, b backend.Backend) (*ColdStorage, error) {
	if !config.Enabled {
		return nil, errors.New("cold storage is not enabled")
	}

	return &ColdStorage{
		config:  config,
		backend: b,
	}, nil
}

// Name returns the cold storage name.
func (c *ColdStorage) Name() string {
	return c.config.Name
}

// Type returns the cold storage type.
func (c *ColdStorage) Type() ColdStorageType {
	return c.config.Type
}

// PutObject stores an object in cold storage.
func (c *ColdStorage) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	c.mu.RLock()

	if c.closed {
		c.mu.RUnlock()
		return nil, errors.New("cold storage is closed")
	}

	c.mu.RUnlock()

	// Build the cold storage key with prefix
	coldKey := c.buildKey(bucket, key)
	coldBucket := c.getBucket()

	// Store in cold backend
	return c.backend.PutObject(ctx, coldBucket, coldKey, reader, size)
}

// GetObject retrieves an object from cold storage.
func (c *ColdStorage) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	c.mu.RLock()

	if c.closed {
		c.mu.RUnlock()
		return nil, errors.New("cold storage is closed")
	}

	c.mu.RUnlock()

	coldKey := c.buildKey(bucket, key)
	coldBucket := c.getBucket()

	return c.backend.GetObject(ctx, coldBucket, coldKey)
}

// ObjectExists checks if an object exists in cold storage.
func (c *ColdStorage) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	c.mu.RLock()

	if c.closed {
		c.mu.RUnlock()
		return false, errors.New("cold storage is closed")
	}

	c.mu.RUnlock()

	coldKey := c.buildKey(bucket, key)
	coldBucket := c.getBucket()

	return c.backend.ObjectExists(ctx, coldBucket, coldKey)
}

// DeleteObject removes an object from cold storage.
func (c *ColdStorage) DeleteObject(ctx context.Context, bucket, key string) error {
	c.mu.RLock()

	if c.closed {
		c.mu.RUnlock()
		return errors.New("cold storage is closed")
	}

	c.mu.RUnlock()

	coldKey := c.buildKey(bucket, key)
	coldBucket := c.getBucket()

	return c.backend.DeleteObject(ctx, coldBucket, coldKey)
}

// IsHealthy checks if cold storage is accessible.
func (c *ColdStorage) IsHealthy(ctx context.Context) bool {
	c.mu.RLock()

	if c.closed {
		c.mu.RUnlock()
		return false
	}

	c.mu.RUnlock()

	// Try to check bucket existence as a health check
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	coldBucket := c.getBucket()
	_, err := c.backend.BucketExists(ctx, coldBucket)

	return err == nil
}

// Close closes the cold storage connection.
func (c *ColdStorage) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	return nil
}

// buildKey constructs the cold storage key with prefix.
func (c *ColdStorage) buildKey(bucket, key string) string {
	prefix := ""

	switch c.config.Type {
	case ColdStorageS3:
		if c.config.S3 != nil {
			prefix = c.config.S3.Prefix
		}
	case ColdStorageAzure:
		if c.config.Azure != nil {
			prefix = c.config.Azure.Prefix
		}
	case ColdStorageGCS:
		if c.config.GCS != nil {
			prefix = c.config.GCS.Prefix
		}
	}

	if prefix == "" {
		return bucket + "/" + key
	}

	return prefix + "/" + bucket + "/" + key
}

// getBucket returns the bucket/container name for cold storage.
func (c *ColdStorage) getBucket() string {
	switch c.config.Type {
	case ColdStorageS3:
		if c.config.S3 != nil {
			return c.config.S3.Bucket
		}
	case ColdStorageAzure:
		if c.config.Azure != nil {
			return c.config.Azure.ContainerName
		}
	case ColdStorageGCS:
		if c.config.GCS != nil {
			return c.config.GCS.Bucket
		}
	case ColdStorageFileSystem:
		return "" // Filesystem doesn't use buckets
	}

	return ""
}

// ColdStorageManager manages multiple cold storage backends.
type ColdStorageManager struct {
	storages map[string]*ColdStorage
	mu       sync.RWMutex
}

// NewColdStorageManager creates a new cold storage manager.
func NewColdStorageManager() *ColdStorageManager {
	return &ColdStorageManager{
		storages: make(map[string]*ColdStorage),
	}
}

// Register adds a cold storage backend.
func (m *ColdStorageManager) Register(storage *ColdStorage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.storages[storage.Name()] = storage
}

// Get retrieves a cold storage backend by name.
func (m *ColdStorageManager) Get(name string) (*ColdStorage, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	storage, ok := m.storages[name]

	return storage, ok
}

// List returns all registered cold storage backends.
func (m *ColdStorageManager) List() []*ColdStorage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ColdStorage, 0, len(m.storages))
	for _, storage := range m.storages {
		result = append(result, storage)
	}

	return result
}

// GetByTier returns cold storage for a specific tier.
func (m *ColdStorageManager) GetByTier(tier TierType) *ColdStorage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Find a healthy storage for the tier
	for _, storage := range m.storages {
		if storage.IsHealthy(context.Background()) {
			return storage
		}
	}

	return nil
}

// Close closes all cold storage backends.
func (m *ColdStorageManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, storage := range m.storages {
		err := storage.Close()
		if err != nil {
			return err
		}
	}

	return nil
}
