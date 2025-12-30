package volume

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: MD5 required for S3 ETag compatibility
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// RawDeviceBackend implements backend.Backend using raw block devices
// with tiered storage support (hot/warm/cold tiers on different devices).
type RawDeviceBackend struct {
	manager      *TieredDeviceManager
	buckets      map[string]bool
	tierSelector TierSelector
	defaultTier  StorageTier
	mu           sync.RWMutex
}

// TierSelector determines which tier an object should be stored in.
type TierSelector func(bucket, key string, size int64) StorageTier

// DefaultTierSelector returns hot tier for all objects.
func DefaultTierSelector(bucket, key string, size int64) StorageTier {
	return TierHot
}

// SizeBasedTierSelector creates a tier selector based on object size.
func SizeBasedTierSelector(hotMaxSize, warmMaxSize int64) TierSelector {
	return func(bucket, key string, size int64) StorageTier {
		if size <= hotMaxSize {
			return TierHot
		}

		if size <= warmMaxSize {
			return TierWarm
		}

		return TierCold
	}
}

// RawDeviceBackendConfig configures the raw device backend.
type RawDeviceBackendConfig struct {
	TierSelector TierSelector
	DefaultTier  StorageTier
	RawDevices   RawDeviceConfig
}

// DefaultRawDeviceBackendConfig returns a default configuration.
func DefaultRawDeviceBackendConfig() RawDeviceBackendConfig {
	return RawDeviceBackendConfig{
		RawDevices:   DefaultRawDeviceConfig(),
		DefaultTier:  TierHot,
		TierSelector: DefaultTierSelector,
	}
}

// Ensure RawDeviceBackend implements the interface.
var _ backend.Backend = (*RawDeviceBackend)(nil)

// NewRawDeviceBackend creates a new raw device backend.
func NewRawDeviceBackend(cfg RawDeviceBackendConfig) (*RawDeviceBackend, error) {
	// Create tiered device manager
	mgrCfg := TieredDeviceManagerConfig{
		RawDevices: cfg.RawDevices,
	}

	mgr, err := NewTieredDeviceManager(mgrCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create tiered device manager: %w", err)
	}

	tierSelector := cfg.TierSelector
	if tierSelector == nil {
		tierSelector = DefaultTierSelector
	}

	return &RawDeviceBackend{
		manager:      mgr,
		buckets:      make(map[string]bool),
		defaultTier:  cfg.DefaultTier,
		tierSelector: tierSelector,
	}, nil
}

// Init initializes the storage backend.
func (b *RawDeviceBackend) Init(ctx context.Context) error {
	// Manager is already initialized in NewRawDeviceBackend
	return nil
}

// Close closes the storage backend.
func (b *RawDeviceBackend) Close() error {
	return b.manager.Close()
}

// PutObject stores an object.
func (b *RawDeviceBackend) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	// Read all data to calculate hash and store
	data := make([]byte, size)

	n, err := io.ReadFull(reader, data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("failed to read object data: %w", err)
	}

	data = data[:n]

	// Calculate MD5 for ETag
	hash := md5.Sum(data) //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	etag := hex.EncodeToString(hash[:])

	// Determine target tier
	tier := b.tierSelector(bucket, key, size)

	// If target tier doesn't exist, fall back to available tier
	if !b.manager.HasTier(tier) {
		tier = b.findAvailableTier()
	}

	// Store in tiered device manager
	if err := b.manager.Put(bucket, key, data, tier); err != nil {
		return nil, fmt.Errorf("failed to store object: %w", err)
	}

	return &backend.PutResult{
		ETag: etag,
		Size: int64(n),
	}, nil
}

// findAvailableTier finds any available tier.
func (b *RawDeviceBackend) findAvailableTier() StorageTier {
	if b.manager.HasTier(TierHot) {
		return TierHot
	}

	if b.manager.HasTier(TierWarm) {
		return TierWarm
	}

	if b.manager.HasTier(TierCold) {
		return TierCold
	}

	return TierHot // fallback
}

// GetObject retrieves an object.
func (b *RawDeviceBackend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	data, err := b.manager.Get(bucket, key)
	if err != nil {
		if err == ErrObjectNotFound {
			return nil, backend.ErrObjectNotFound
		}

		return nil, err
	}

	return io.NopCloser(&bytesReader{data: data}), nil
}

// DeleteObject deletes an object.
func (b *RawDeviceBackend) DeleteObject(ctx context.Context, bucket, key string) error {
	err := b.manager.Delete(bucket, key)
	if err != nil {
		if err == ErrObjectNotFound {
			return backend.ErrObjectNotFound
		}

		return err
	}

	return nil
}

// ObjectExists checks if an object exists.
func (b *RawDeviceBackend) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := b.manager.GetObjectTier(bucket, key)
	if err != nil {
		if err == ErrObjectNotFound {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// CreateBucket creates storage for a bucket.
func (b *RawDeviceBackend) CreateBucket(ctx context.Context, bucket string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buckets[bucket] = true

	return nil
}

// DeleteBucket deletes storage for a bucket.
func (b *RawDeviceBackend) DeleteBucket(ctx context.Context, bucket string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.buckets, bucket)

	return nil
}

// BucketExists checks if bucket storage exists.
func (b *RawDeviceBackend) BucketExists(ctx context.Context, bucket string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.buckets[bucket], nil
}

// GetStorageInfo returns storage statistics.
func (b *RawDeviceBackend) GetStorageInfo(ctx context.Context) (*backend.StorageInfo, error) {
	stats := b.manager.Stats()

	var totalBytes, usedBytes, objectCount int64
	for _, s := range stats {
		totalBytes += int64(s.TotalSpace)
		usedBytes += int64(s.UsedSpace)
		objectCount += int64(s.ObjectCount)
	}

	return &backend.StorageInfo{
		TotalBytes:     totalBytes,
		UsedBytes:      usedBytes,
		AvailableBytes: totalBytes - usedBytes,
		ObjectCount:    objectCount,
	}, nil
}

// MigrateObject migrates an object to a different tier.
func (b *RawDeviceBackend) MigrateObject(ctx context.Context, bucket, key string, targetTier StorageTier) error {
	return b.manager.MigrateObject(bucket, key, targetTier)
}

// GetObjectTier returns the current tier of an object.
func (b *RawDeviceBackend) GetObjectTier(bucket, key string) (StorageTier, error) {
	return b.manager.GetObjectTier(bucket, key)
}

// GetTierStats returns statistics for a specific tier.
func (b *RawDeviceBackend) GetTierStats(tier StorageTier) (*RawDeviceVolumeStats, error) {
	stats := b.manager.Stats()
	if s, ok := stats[tier]; ok {
		return &s, nil
	}

	return nil, fmt.Errorf("tier %s not available", tier)
}

// HasTier checks if a tier is available.
func (b *RawDeviceBackend) HasTier(tier StorageTier) bool {
	return b.manager.HasTier(tier)
}
