package erasure

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: MD5 required for S3 ETag compatibility
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ShardManager handles local shard storage operations.
type ShardManager struct {
	dataDir string
}

// NewShardManager creates a new shard manager.
func NewShardManager(dataDir string) (*ShardManager, error) {
	// Create data directory if it doesn't exist
	err := os.MkdirAll(dataDir, 0750)
	if err != nil {
		return nil, fmt.Errorf("failed to create shard data directory: %w", err)
	}

	return &ShardManager{
		dataDir: dataDir,
	}, nil
}

// ShardPath returns the filesystem path for a shard.
func (m *ShardManager) ShardPath(bucket, key string, shardIndex int) string {
	// Use bucket/key hash to create a balanced directory structure
	hash := md5.Sum(fmt.Appendf(nil, "%s/%s", bucket, key)) //nolint:gosec // G401: MD5 for path distribution only
	hashHex := hex.EncodeToString(hash[:])

	// Create a 2-level directory structure for better filesystem performance
	dir := filepath.Join(m.dataDir, hashHex[:2], hashHex[2:4])

	return filepath.Join(dir, fmt.Sprintf("%s_%s_%d.shard", bucket, sanitizeKey(key), shardIndex))
}

// sanitizeKey makes a key safe for use in a filename.
func sanitizeKey(key string) string {
	// Replace path separators and other problematic characters
	result := make([]byte, 0, len(key))
	for i := range len(key) {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}

	return string(result)
}

// WriteShard writes a shard to local storage.
func (m *ShardManager) WriteShard(ctx context.Context, bucket, key string, shardIndex int, data []byte) (string, error) {
	path := m.ShardPath(bucket, key, shardIndex)

	// Ensure parent directory exists
	err := os.MkdirAll(filepath.Dir(path), 0750)
	if err != nil {
		return "", fmt.Errorf("failed to create shard directory: %w", err)
	}

	// Create temporary file for atomic write
	tmpPath := path + ".tmp"

	//nolint:gosec // G304: tmpPath is constructed from trusted config
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	defer func() { _ = os.Remove(tmpPath) }() // Clean up on error

	// Write data
	_, err = tmpFile.Write(data)
	if err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("failed to write shard: %w", err)
	}

	err = tmpFile.Sync()
	if err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("failed to sync shard: %w", err)
	}

	err = tmpFile.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close shard: %w", err)
	}

	// Atomic rename
	err = os.Rename(tmpPath, path)
	if err != nil {
		return "", fmt.Errorf("failed to rename shard: %w", err)
	}

	return path, nil
}

// ReadShard reads a shard from local storage.
func (m *ShardManager) ReadShard(ctx context.Context, bucket, key string, shardIndex int) ([]byte, error) {
	path := m.ShardPath(bucket, key, shardIndex)
	return m.ReadShardByPath(ctx, path)
}

// ReadShardByPath reads a shard from a specific path.
func (m *ShardManager) ReadShardByPath(ctx context.Context, path string) ([]byte, error) {
	//nolint:gosec // G304: path is constructed from trusted config
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("shard not found: %s", path)
		}

		return nil, fmt.Errorf("failed to read shard: %w", err)
	}

	return data, nil
}

// DeleteShard deletes a shard from local storage.
func (m *ShardManager) DeleteShard(ctx context.Context, bucket, key string, shardIndex int) error {
	path := m.ShardPath(bucket, key, shardIndex)
	return m.DeleteShardByPath(ctx, path)
}

// DeleteShardByPath deletes a shard at a specific path.
func (m *ShardManager) DeleteShardByPath(ctx context.Context, path string) error {
	err := os.Remove(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}

		return fmt.Errorf("failed to delete shard: %w", err)
	}

	// Clean up empty parent directories
	m.cleanEmptyDirs(filepath.Dir(path))

	return nil
}

// ShardExists checks if a shard exists.
func (m *ShardManager) ShardExists(ctx context.Context, bucket, key string, shardIndex int) bool {
	path := m.ShardPath(bucket, key, shardIndex)
	_, err := os.Stat(path)

	return err == nil
}

// VerifyShard verifies a shard's checksum.
func (m *ShardManager) VerifyShard(ctx context.Context, bucket, key string, shardIndex int, expectedChecksum string) (bool, error) {
	data, err := m.ReadShard(ctx, bucket, key, shardIndex)
	if err != nil {
		return false, err
	}

	hash := md5.Sum(data) //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	checksum := hex.EncodeToString(hash[:])

	return checksum == expectedChecksum, nil
}

// GetShardSize returns the size of a shard.
func (m *ShardManager) GetShardSize(ctx context.Context, bucket, key string, shardIndex int) (int64, error) {
	path := m.ShardPath(bucket, key, shardIndex)

	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("failed to stat shard: %w", err)
	}

	return info.Size(), nil
}

// GetShardReader returns a reader for a shard.
func (m *ShardManager) GetShardReader(ctx context.Context, bucket, key string, shardIndex int) (io.ReadCloser, error) {
	path := m.ShardPath(bucket, key, shardIndex)

	//nolint:gosec // G304: path is constructed from trusted config
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("shard not found: %s", path)
		}

		return nil, fmt.Errorf("failed to open shard: %w", err)
	}

	return file, nil
}

// ListShards lists all shards for a bucket.
func (m *ShardManager) ListShards(ctx context.Context, bucket string) ([]string, error) {
	var shards []string

	err := filepath.Walk(m.dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if filepath.Ext(path) == ".shard" {
			// Check if this shard belongs to the bucket
			base := filepath.Base(path)
			if len(base) > len(bucket)+1 && base[:len(bucket)] == bucket {
				shards = append(shards, path)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list shards: %w", err)
	}

	return shards, nil
}

// GetStorageInfo returns storage statistics.
func (m *ShardManager) GetStorageInfo(ctx context.Context) (totalBytes, usedBytes int64, shardCount int, err error) {
	err = filepath.Walk(m.dataDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !info.IsDir() && filepath.Ext(path) == ".shard" {
			usedBytes += info.Size()
			shardCount++
		}

		return nil
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to calculate storage info: %w", err)
	}

	// Get filesystem info for total bytes
	// This is platform-specific, so we'll return 0 and let the backend handle it
	return 0, usedBytes, shardCount, nil
}

// cleanEmptyDirs removes empty directories up to the data directory.
func (m *ShardManager) cleanEmptyDirs(dir string) {
	for dir != m.dataDir && dir != "." && dir != "/" {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}

		err = os.Remove(dir)
		if err != nil {
			break
		}

		dir = filepath.Dir(dir)
	}
}

// RepairShard writes a repaired shard to storage.
func (m *ShardManager) RepairShard(ctx context.Context, bucket, key string, shardIndex int, data []byte) (string, string, error) {
	// Calculate checksum
	hash := md5.Sum(data) //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	checksum := hex.EncodeToString(hash[:])

	// Write the shard
	path, err := m.WriteShard(ctx, bucket, key, shardIndex, data)
	if err != nil {
		return "", "", err
	}

	return path, checksum, nil
}
