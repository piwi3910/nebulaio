// Package backend defines the storage backend interface for NebulaIO.
//
// The Backend interface abstracts object storage operations, allowing for
// pluggable storage implementations. NebulaIO includes several backends:
//
//   - fs: Filesystem-based storage (default)
//   - volume: High-performance block storage with pre-allocated volumes
//   - erasure: Reed-Solomon erasure coding for distributed durability
//
// Each backend implements the same interface, enabling transparent switching
// based on workload requirements, performance needs, and durability goals.
//
// Example usage:
//
//	var backend backend.Backend = fs.NewBackend(config)
//	err := backend.Put(ctx, "bucket", "key", reader, size)
//	reader, err := backend.Get(ctx, "bucket", "key")
package backend

import (
	"context"
	"errors"
	"io"
)

// Common backend errors.
var (
	ErrObjectNotFound = errors.New("object not found")
	ErrBucketNotFound = errors.New("bucket not found")
)

// Backend is the interface for object storage backends.
type Backend interface {
	// Init initializes the storage backend
	Init(ctx context.Context) error

	// Close closes the storage backend
	Close() error

	// PutObject stores an object
	PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*PutResult, error)

	// GetObject retrieves an object
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)

	// DeleteObject deletes an object
	DeleteObject(ctx context.Context, bucket, key string) error

	// ObjectExists checks if an object exists
	ObjectExists(ctx context.Context, bucket, key string) (bool, error)

	// CreateBucket creates storage for a bucket
	CreateBucket(ctx context.Context, bucket string) error

	// DeleteBucket deletes storage for a bucket
	DeleteBucket(ctx context.Context, bucket string) error

	// BucketExists checks if bucket storage exists
	BucketExists(ctx context.Context, bucket string) (bool, error)

	// GetStorageInfo returns storage statistics
	GetStorageInfo(ctx context.Context) (*StorageInfo, error)
}

// PutResult contains the result of a put operation.
type PutResult struct {
	// ETag is the MD5/checksum of the stored object
	ETag string

	// Path is the storage path (for filesystem backend)
	Path string

	// Size is the actual size written
	Size int64
}

// StorageInfo contains storage statistics.
type StorageInfo struct {
	TotalBytes     int64
	UsedBytes      int64
	AvailableBytes int64
	ObjectCount    int64
}

// MultipartBackend extends Backend with multipart upload support.
type MultipartBackend interface {
	Backend

	// CreateMultipartUpload creates storage for a multipart upload
	CreateMultipartUpload(ctx context.Context, bucket, key, uploadID string) error

	// PutPart stores a part of a multipart upload
	PutPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*PutResult, error)

	// GetPart retrieves a part of a multipart upload
	GetPart(ctx context.Context, bucket, key, uploadID string, partNumber int) (io.ReadCloser, error)

	// CompleteParts combines parts into the final object
	CompleteParts(ctx context.Context, bucket, key, uploadID string, parts []int) (*PutResult, error)

	// AbortMultipartUpload cleans up a multipart upload
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
}
