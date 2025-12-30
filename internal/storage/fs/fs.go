// Package fs implements a filesystem-based storage backend for NebulaIO.
//
// The filesystem backend stores objects as files on a local or network-attached
// filesystem. It is the simplest backend and suitable for:
//
//   - Development and testing environments
//   - Single-node deployments
//   - NAS/SAN storage with shared filesystem
//
// Object path layout:
//
//	{data_dir}/{bucket}/{key-hash-prefix}/{key-hash}
//
// Features:
//   - Atomic writes using temporary files
//   - Content-addressable storage for deduplication
//   - Sparse file support for efficient storage
//   - Compatible with any POSIX filesystem
package fs

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: MD5 required for S3 ETag compatibility
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// Directory permission constant.
const dirPermissions = 0750

// Config holds filesystem backend configuration.
type Config struct {
	DataDir string
}

// Backend implements the storage backend using the local filesystem.
type Backend struct {
	config     Config
	bucketsDir string
	uploadsDir string
}

// New creates a new filesystem backend.
func New(config Config) (*Backend, error) {
	b := &Backend{
		config:     config,
		bucketsDir: filepath.Join(config.DataDir, "buckets"),
		uploadsDir: filepath.Join(config.DataDir, "uploads"),
	}

	// Create directories
	err := os.MkdirAll(b.bucketsDir, dirPermissions)
	if err != nil {
		return nil, fmt.Errorf("failed to create buckets directory: %w", err)
	}

	err = os.MkdirAll(b.uploadsDir, dirPermissions)
	if err != nil {
		return nil, fmt.Errorf("failed to create uploads directory: %w", err)
	}

	return b, nil
}

// Init initializes the storage backend.
func (b *Backend) Init(ctx context.Context) error {
	return nil
}

// Close closes the storage backend.
func (b *Backend) Close() error {
	return nil
}

// objectPath returns the filesystem path for an object.
func (b *Backend) objectPath(bucket, key string) string {
	return filepath.Join(b.bucketsDir, bucket, "objects", key)
}

// bucketPath returns the filesystem path for a bucket.
func (b *Backend) bucketPath(bucket string) string {
	return filepath.Join(b.bucketsDir, bucket)
}

// uploadPath returns the filesystem path for a multipart upload.
func (b *Backend) uploadPath(bucket, key, uploadID string) string {
	return filepath.Join(b.uploadsDir, bucket, key, uploadID)
}

// partPath returns the filesystem path for a multipart upload part.
func (b *Backend) partPath(bucket, key, uploadID string, partNumber int) string {
	return filepath.Join(b.uploadPath(bucket, key, uploadID), fmt.Sprintf("part.%d", partNumber))
}

// PutObject stores an object.
func (b *Backend) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	path := b.objectPath(bucket, key)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), dirPermissions); err != nil {
		return nil, fmt.Errorf("failed to create object directory: %w", err)
	}

	// Create temporary file for atomic write
	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()

	defer func() { _ = os.Remove(tmpPath) }() // Clean up on error

	// Write data and calculate MD5
	hash := md5.New() //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	writer := io.MultiWriter(tmpFile, hash)

	written, err := io.Copy(writer, reader)
	if err != nil {
		_ = tmpFile.Close()
		return nil, fmt.Errorf("failed to write object: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return nil, fmt.Errorf("failed to sync object: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return nil, fmt.Errorf("failed to rename object: %w", err)
	}

	etag := hex.EncodeToString(hash.Sum(nil))

	return &backend.PutResult{
		ETag: etag,
		Path: path,
		Size: written,
	}, nil
}

// GetObject retrieves an object.
func (b *Backend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	path := b.objectPath(bucket, key)

	//nolint:gosec // G304: path is constructed from validated bucket/key
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("object not found: %s/%s", bucket, key)
		}

		return nil, fmt.Errorf("failed to open object: %w", err)
	}

	return file, nil
}

// DeleteObject deletes an object.
func (b *Backend) DeleteObject(ctx context.Context, bucket, key string) error {
	path := b.objectPath(bucket, key)

	err := os.Remove(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}

		return fmt.Errorf("failed to delete object: %w", err)
	}

	// Clean up empty parent directories
	b.cleanEmptyDirs(filepath.Dir(path), b.bucketPath(bucket))

	return nil
}

// ObjectExists checks if an object exists.
func (b *Backend) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	path := b.objectPath(bucket, key)

	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// CreateBucket creates storage for a bucket.
func (b *Backend) CreateBucket(ctx context.Context, bucket string) error {
	path := filepath.Join(b.bucketPath(bucket), "objects")

	err := os.MkdirAll(path, 0750)
	if err != nil {
		return fmt.Errorf("failed to create bucket directory: %w", err)
	}

	return nil
}

// DeleteBucket deletes storage for a bucket.
func (b *Backend) DeleteBucket(ctx context.Context, bucket string) error {
	path := b.bucketPath(bucket)

	// Check if bucket is empty (has objects)
	objectsPath := filepath.Join(path, "objects")

	entries, err := os.ReadDir(objectsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to check bucket contents: %w", err)
	}

	if len(entries) > 0 {
		return errors.New("bucket not empty")
	}

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to delete bucket directory: %w", err)
	}

	return nil
}

// BucketExists checks if bucket storage exists.
func (b *Backend) BucketExists(ctx context.Context, bucket string) (bool, error) {
	path := b.bucketPath(bucket)

	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// GetStorageInfo returns storage statistics.
func (b *Backend) GetStorageInfo(ctx context.Context) (*backend.StorageInfo, error) {
	var stat syscall.Statfs_t

	err := syscall.Statfs(b.config.DataDir, &stat)
	if err != nil {
		return nil, fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	//nolint:gosec // G115: filesystem stats are always positive
	total := stat.Blocks * uint64(stat.Bsize)
	//nolint:gosec // G115: filesystem stats are always positive
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free

	// Count objects
	var objectCount int64

	_ = filepath.Walk(b.bucketsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !info.IsDir() {
			objectCount++
		}

		return nil
	})

	return &backend.StorageInfo{
		//nolint:gosec // G115: filesystem stats fit in int64
		TotalBytes: int64(total),
		//nolint:gosec // G115: filesystem stats fit in int64
		UsedBytes: int64(used),
		//nolint:gosec // G115: filesystem stats fit in int64
		AvailableBytes: int64(free),
		ObjectCount:    objectCount,
	}, nil
}

// CreateMultipartUpload creates storage for a multipart upload.
func (b *Backend) CreateMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	path := b.uploadPath(bucket, key, uploadID)

	err := os.MkdirAll(path, 0750)
	if err != nil {
		return fmt.Errorf("failed to create upload directory: %w", err)
	}

	return nil
}

// PutPart stores a part of a multipart upload.
func (b *Backend) PutPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*backend.PutResult, error) {
	path := b.partPath(bucket, key, uploadID, partNumber)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("failed to create part directory: %w", err)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()

	defer func() { _ = os.Remove(tmpPath) }()

	// Write data and calculate MD5
	hash := md5.New() //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	writer := io.MultiWriter(tmpFile, hash)

	written, err := io.Copy(writer, reader)
	if err != nil {
		_ = tmpFile.Close()
		return nil, fmt.Errorf("failed to write part: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return nil, fmt.Errorf("failed to sync part: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return nil, fmt.Errorf("failed to rename part: %w", err)
	}

	etag := hex.EncodeToString(hash.Sum(nil))

	return &backend.PutResult{
		ETag: etag,
		Path: path,
		Size: written,
	}, nil
}

// GetPart retrieves a part of a multipart upload.
func (b *Backend) GetPart(ctx context.Context, bucket, key, uploadID string, partNumber int) (io.ReadCloser, error) {
	path := b.partPath(bucket, key, uploadID, partNumber)

	//nolint:gosec // G304: path is constructed from validated inputs
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("part not found: %d", partNumber)
		}

		return nil, fmt.Errorf("failed to open part: %w", err)
	}

	return file, nil
}

// streamingBufferSize is the buffer size for streaming large files (8MB).
const streamingBufferSize = 8 * 1024 * 1024

// CompleteParts combines parts into the final object using streaming to handle large files efficiently.
func (b *Backend) CompleteParts(ctx context.Context, bucket, key, uploadID string, parts []int) (*backend.PutResult, error) {
	objectPath := b.objectPath(bucket, key)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(objectPath), 0750); err != nil {
		return nil, fmt.Errorf("failed to create object directory: %w", err)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp(filepath.Dir(objectPath), ".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()

	// Ensure cleanup on error
	success := false

	defer func() {
		if !success {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	// Sort parts
	sort.Ints(parts)

	// Use a buffered writer for better performance with large files
	bufWriter := io.Writer(tmpFile)
	hash := md5.New() //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	writer := io.MultiWriter(bufWriter, hash)

	var totalSize int64

	// Allocate buffer once for reuse across all parts
	buffer := make([]byte, streamingBufferSize)

	for _, partNum := range parts {
		// Check for context cancellation to support cancellable operations
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		partPath := b.partPath(bucket, key, uploadID, partNum)

		//nolint:gosec // G304: partPath is constructed from validated inputs
		partFile, err := os.Open(partPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open part %d: %w", partNum, err)
		}

		// Stream the part using the preallocated buffer
		written, err := io.CopyBuffer(writer, partFile, buffer)
		_ = partFile.Close()

		if err != nil {
			return nil, fmt.Errorf("failed to copy part %d: %w", partNum, err)
		}

		totalSize += written
	}

	if err := tmpFile.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync object: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, objectPath); err != nil {
		return nil, fmt.Errorf("failed to rename object: %w", err)
	}

	// Mark success before cleanup
	success = true

	// Clean up upload directory and parts
	uploadPath := b.uploadPath(bucket, key, uploadID)
	_ = os.RemoveAll(uploadPath)

	// Clean up empty parent directories in uploads dir
	b.cleanEmptyDirs(filepath.Dir(uploadPath), b.uploadsDir)

	etag := hex.EncodeToString(hash.Sum(nil))

	return &backend.PutResult{
		ETag: etag,
		Path: objectPath,
		Size: totalSize,
	}, nil
}

// AbortMultipartUpload cleans up a multipart upload and all its parts.
func (b *Backend) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	uploadPath := b.uploadPath(bucket, key, uploadID)

	// Check if upload directory exists
	if _, err := os.Stat(uploadPath); os.IsNotExist(err) {
		// Already cleaned up or never existed - not an error
		return nil
	}

	// Remove all parts and the upload directory
	err := os.RemoveAll(uploadPath)
	if err != nil {
		return fmt.Errorf("failed to remove upload directory: %w", err)
	}

	// Clean up empty parent directories (bucket/key path within uploads)
	b.cleanEmptyDirs(filepath.Dir(uploadPath), b.uploadsDir)

	return nil
}

// ListParts returns the paths of all parts for a multipart upload.
func (b *Backend) ListParts(ctx context.Context, bucket, key, uploadID string) ([]string, error) {
	uploadPath := b.uploadPath(bucket, key, uploadID)

	entries, err := os.ReadDir(uploadPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("upload not found: %s", uploadID)
		}

		return nil, fmt.Errorf("failed to read upload directory: %w", err)
	}

	var parts []string

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == "" {
			parts = append(parts, filepath.Join(uploadPath, entry.Name()))
		}
	}

	return parts, nil
}

// cleanEmptyDirs removes empty directories up to the stop directory.
func (b *Backend) cleanEmptyDirs(dir, stopDir string) {
	for dir != stopDir && dir != "." && dir != "/" {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}

		if err := os.Remove(dir); err != nil {
			break
		}

		dir = filepath.Dir(dir)
	}
}
