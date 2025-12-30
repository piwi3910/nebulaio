package volume

import (
	"context"
	"crypto/md5" // #nosec G501 - MD5 used for S3 ETag compatibility
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// Backend implements the backend.Backend interface using volume storage
type Backend struct {
	manager *Manager
	buckets map[string]bool // tracks created buckets
	mu      sync.RWMutex
}

// Ensure Backend implements the interface
var _ backend.Backend = (*Backend)(nil)

// NewBackend creates a new volume-based storage backend
func NewBackend(dataDir string) (*Backend, error) {
	cfg := DefaultManagerConfig(dataDir)
	mgr, err := NewManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create volume manager: %w", err)
	}

	return &Backend{
		manager: mgr,
		buckets: make(map[string]bool),
	}, nil
}

// Init initializes the storage backend
func (b *Backend) Init(ctx context.Context) error {
	// Manager is already initialized in NewBackend
	// Ensure at least one volume exists
	if b.manager.VolumeCount() == 0 {
		_, err := b.manager.CreateVolume()
		if err != nil {
			return fmt.Errorf("failed to create initial volume: %w", err)
		}
	}
	return nil
}

// Close closes the storage backend
func (b *Backend) Close() error {
	return b.manager.Close()
}

// PutObject stores an object
func (b *Backend) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	// Read all data to calculate hash and store
	data := make([]byte, size)
	n, err := io.ReadFull(reader, data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("failed to read object data: %w", err)
	}
	data = data[:n]

	// Calculate MD5 for ETag
	hash := md5.Sum(data)
	etag := hex.EncodeToString(hash[:])

	// Store in volume manager
	if err := b.manager.Put(bucket, key, data); err != nil {
		return nil, fmt.Errorf("failed to store object: %w", err)
	}

	return &backend.PutResult{
		ETag: etag,
		Size: int64(n),
	}, nil
}

// GetObject retrieves an object
func (b *Backend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	data, err := b.manager.Get(bucket, key)
	if err != nil {
		if err == ErrObjectNotFound {
			return nil, backend.ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to get object: %w", err)
	}

	return &bytesReadCloser{data: data}, nil
}

// DeleteObject deletes an object
func (b *Backend) DeleteObject(ctx context.Context, bucket, key string) error {
	err := b.manager.Delete(bucket, key)
	if err != nil {
		if err == ErrObjectNotFound {
			return nil // S3 spec: delete on non-existent is success
		}
		return fmt.Errorf("failed to delete object: %w", err)
	}
	return nil
}

// ObjectExists checks if an object exists
func (b *Backend) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	return b.manager.Exists(bucket, key), nil
}

// CreateBucket creates storage for a bucket
func (b *Backend) CreateBucket(ctx context.Context, bucket string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buckets[bucket] = true
	return nil
}

// DeleteBucket deletes storage for a bucket
func (b *Backend) DeleteBucket(ctx context.Context, bucket string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if bucket has objects
	objects, err := b.manager.List(bucket, "", 1)
	if err == nil && len(objects) > 0 {
		return fmt.Errorf("bucket is not empty")
	}

	delete(b.buckets, bucket)
	return nil
}

// BucketExists checks if bucket storage exists
func (b *Backend) BucketExists(ctx context.Context, bucket string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// If bucket is tracked, it exists
	if b.buckets[bucket] {
		return true, nil
	}

	// Also check if any objects exist in the bucket
	objects, err := b.manager.List(bucket, "", 1)
	if err != nil {
		return false, nil
	}
	return len(objects) > 0, nil
}

// GetStorageInfo returns storage statistics
func (b *Backend) GetStorageInfo(ctx context.Context) (*backend.StorageInfo, error) {
	stats := b.manager.Stats()

	return &backend.StorageInfo{
		TotalBytes:     int64(stats.TotalSize),
		UsedBytes:      int64(stats.UsedSize),
		AvailableBytes: int64(stats.FreeSize),
		ObjectCount:    int64(stats.ObjectCount),
	}, nil
}

// Sync flushes all pending writes to disk
func (b *Backend) Sync() error {
	return b.manager.Sync()
}

// Stats returns detailed volume statistics
func (b *Backend) Stats() ManagerStats {
	return b.manager.Stats()
}

// bytesReadCloser wraps a byte slice as an io.ReadCloser
type bytesReadCloser struct {
	data []byte
	pos  int
}

func (r *bytesReadCloser) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *bytesReadCloser) Close() error {
	return nil
}
