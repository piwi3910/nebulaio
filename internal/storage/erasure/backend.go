package erasure

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
	"github.com/rs/zerolog/log"
)

// Backend implements the storage backend using erasure coding
type Backend struct {
	config       Config
	encoder      *Encoder
	decoder      *Decoder
	shardManager *ShardManager
	placement    PlacementStrategy
	nodes        []NodeInfo
	mu           sync.RWMutex

	// For multipart uploads
	uploadsDir string

	// Local node info
	localNodeID string
}

// New creates a new erasure coding backend
func New(config Config, localNodeID string) (*Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	encoder, err := NewEncoder(config.DataShards, config.ParityShards, config.ShardSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	decoder, err := NewDecoder(config.DataShards, config.ParityShards)
	if err != nil {
		return nil, fmt.Errorf("failed to create decoder: %w", err)
	}

	shardManager, err := NewShardManager(filepath.Join(config.DataDir, "shards"))
	if err != nil {
		return nil, fmt.Errorf("failed to create shard manager: %w", err)
	}

	uploadsDir := filepath.Join(config.DataDir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create uploads directory: %w", err)
	}

	return &Backend{
		config:       config,
		encoder:      encoder,
		decoder:      decoder,
		shardManager: shardManager,
		placement:    NewConsistentHashPlacement(100),
		nodes:        make([]NodeInfo, 0),
		uploadsDir:   uploadsDir,
		localNodeID:  localNodeID,
	}, nil
}

// Init initializes the storage backend
func (b *Backend) Init(ctx context.Context) error {
	log.Info().
		Int("data_shards", b.config.DataShards).
		Int("parity_shards", b.config.ParityShards).
		Str("data_dir", b.config.DataDir).
		Msg("Initializing erasure coding backend")

	return nil
}

// Close closes the storage backend
func (b *Backend) Close() error {
	return nil
}

// SetNodes updates the list of available storage nodes
func (b *Backend) SetNodes(nodes []NodeInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nodes = nodes
}

// GetNodes returns the current list of storage nodes
func (b *Backend) GetNodes() []NodeInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.nodes
}

// PutObject stores an object with erasure coding
func (b *Backend) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	// Encode the data
	encoded, err := b.encoder.Encode(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to encode object: %w", err)
	}

	// Get node assignments for shards
	b.mu.RLock()
	nodes := b.nodes
	b.mu.RUnlock()

	// For single-node mode, store all shards locally
	if len(nodes) == 0 {
		nodes = []NodeInfo{{
			ID:      b.localNodeID,
			Status:  "alive",
			Address: "localhost",
		}}
	}

	assignments, err := b.placement.PlaceShards(fmt.Sprintf("%s/%s", bucket, key), len(encoded.Shards), nodes)
	if err != nil {
		return nil, fmt.Errorf("failed to place shards: %w", err)
	}

	// Store shards
	var wg sync.WaitGroup
	errChan := make(chan error, len(encoded.Shards))
	pathChan := make(chan struct {
		index int
		path  string
	}, len(encoded.Shards))

	for i, shard := range encoded.Shards {
		wg.Add(1)
		go func(index int, data []byte, assignment NodeAssignment) {
			defer wg.Done()

			// For now, all shards go to local node (distributed mode would send to remote nodes)
			path, writeErr := b.shardManager.WriteShard(ctx, bucket, key, index, data)
			if writeErr != nil {
				errChan <- fmt.Errorf("failed to write shard %d: %w", index, writeErr)
				return
			}

			pathChan <- struct {
				index int
				path  string
			}{index, path}
		}(i, shard, assignments[i])
	}

	wg.Wait()
	close(errChan)
	close(pathChan)

	// Check for errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	// Allow some shard failures (up to parityShards)
	if len(errs) > b.config.ParityShards {
		// Clean up written shards on failure
		for result := range pathChan {
			_ = b.shardManager.DeleteShardByPath(ctx, result.path)
		}
		return nil, fmt.Errorf("too many shard write failures (%d): %v", len(errs), errs[0])
	}

	return &backend.PutResult{
		ETag: encoded.OriginalChecksum,
		Size: encoded.OriginalSize,
		Path: fmt.Sprintf("erasure:%s/%s", bucket, key),
	}, nil
}

// GetObject retrieves an object using erasure coding
func (b *Backend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	// Collect available shards
	totalShards := b.config.TotalShards()
	shards := make([][]byte, totalShards)
	available := 0

	for i := 0; i < totalShards; i++ {
		data, err := b.shardManager.ReadShard(ctx, bucket, key, i)
		if err != nil {
			log.Debug().
				Int("shard", i).
				Str("bucket", bucket).
				Str("key", key).
				Msg("Shard not available")
			continue
		}
		shards[i] = data
		available++
	}

	if available < b.config.DataShards {
		return nil, fmt.Errorf("not enough shards available: need %d, have %d", b.config.DataShards, available)
	}

	// Decode the data
	result, err := b.decoder.Decode(&DecodeInput{
		Shards:       shards,
		OriginalSize: 0, // Will be set from metadata
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decode object: %w", err)
	}

	return io.NopCloser(bytes.NewReader(result.Data)), nil
}

// DeleteObject deletes an object and all its shards
func (b *Backend) DeleteObject(ctx context.Context, bucket, key string) error {
	totalShards := b.config.TotalShards()

	var wg sync.WaitGroup
	errChan := make(chan error, totalShards)

	for i := 0; i < totalShards; i++ {
		wg.Add(1)
		go func(shardIndex int) {
			defer wg.Done()
			if err := b.shardManager.DeleteShard(ctx, bucket, key, shardIndex); err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	// All shards deleted or not found is success
	if len(errs) > 0 {
		log.Warn().
			Int("errors", len(errs)).
			Str("bucket", bucket).
			Str("key", key).
			Msg("Some shards could not be deleted")
	}

	return nil
}

// ObjectExists checks if an object exists (at least minimum shards)
func (b *Backend) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	available := 0
	for i := 0; i < b.config.TotalShards(); i++ {
		if b.shardManager.ShardExists(ctx, bucket, key, i) {
			available++
			if available >= b.config.DataShards {
				return true, nil
			}
		}
	}
	return false, nil
}

// CreateBucket creates storage for a bucket (no-op for erasure coding)
func (b *Backend) CreateBucket(ctx context.Context, bucket string) error {
	// Shards are stored in a flat structure, no bucket directories needed
	return nil
}

// DeleteBucket deletes storage for a bucket
func (b *Backend) DeleteBucket(ctx context.Context, bucket string) error {
	// Delete all shards for this bucket
	shards, err := b.shardManager.ListShards(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to list shards: %w", err)
	}

	for _, path := range shards {
		if err := b.shardManager.DeleteShardByPath(ctx, path); err != nil {
			log.Warn().Err(err).Str("path", path).Msg("Failed to delete shard")
		}
	}

	return nil
}

// BucketExists checks if bucket storage exists (always true for erasure coding)
func (b *Backend) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return true, nil
}

// GetStorageInfo returns storage statistics
func (b *Backend) GetStorageInfo(ctx context.Context) (*backend.StorageInfo, error) {
	_, usedBytes, shardCount, err := b.shardManager.GetStorageInfo(ctx)
	if err != nil {
		return nil, err
	}

	return &backend.StorageInfo{
		UsedBytes:   usedBytes,
		ObjectCount: int64(shardCount / b.config.TotalShards()), // Approximate
	}, nil
}

// Multipart upload operations

// uploadPath returns the path for a multipart upload
func (b *Backend) uploadPath(bucket, key, uploadID string) string {
	return filepath.Join(b.uploadsDir, bucket, key, uploadID)
}

// partPath returns the path for a part
func (b *Backend) partPath(bucket, key, uploadID string, partNumber int) string {
	return filepath.Join(b.uploadPath(bucket, key, uploadID), fmt.Sprintf("part.%d", partNumber))
}

// CreateMultipartUpload creates storage for a multipart upload
func (b *Backend) CreateMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	path := b.uploadPath(bucket, key, uploadID)
	return os.MkdirAll(path, 0755)
}

// PutPart stores a part (temporarily stored as regular file, encoded on completion)
func (b *Backend) PutPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*backend.PutResult, error) {
	path := b.partPath(bucket, key, uploadID, partNumber)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create part directory: %w", err)
	}

	// Write to temp file first
	tmpPath := path + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	hash := md5.New()
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
		return nil, fmt.Errorf("failed to close part: %w", err)
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

// GetPart retrieves a part
func (b *Backend) GetPart(ctx context.Context, bucket, key, uploadID string, partNumber int) (io.ReadCloser, error) {
	path := b.partPath(bucket, key, uploadID, partNumber)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("part not found: %d", partNumber)
		}
		return nil, fmt.Errorf("failed to open part: %w", err)
	}
	return file, nil
}

// CompleteParts combines parts and encodes with erasure coding
func (b *Backend) CompleteParts(ctx context.Context, bucket, key, uploadID string, parts []int) (*backend.PutResult, error) {
	sort.Ints(parts)

	// First, combine all parts into a single buffer
	var combined bytes.Buffer
	originalHash := md5.New()
	writer := io.MultiWriter(&combined, originalHash)

	for _, partNum := range parts {
		path := b.partPath(bucket, key, uploadID, partNum)
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open part %d: %w", partNum, err)
		}

		if _, err := io.Copy(writer, file); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("failed to read part %d: %w", partNum, err)
		}
		_ = file.Close()
	}

	// Now encode the combined data with erasure coding
	result, err := b.PutObject(ctx, bucket, key, &combined, int64(combined.Len()))
	if err != nil {
		return nil, fmt.Errorf("failed to encode completed upload: %w", err)
	}

	// Clean up upload directory
	uploadPath := b.uploadPath(bucket, key, uploadID)
	_ = os.RemoveAll(uploadPath)
	b.cleanEmptyDirs(filepath.Dir(uploadPath))

	// Override ETag with multipart format
	etag := hex.EncodeToString(originalHash.Sum(nil))

	return &backend.PutResult{
		ETag: etag,
		Path: result.Path,
		Size: int64(combined.Len()),
	}, nil
}

// AbortMultipartUpload cleans up a multipart upload
func (b *Backend) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	uploadPath := b.uploadPath(bucket, key, uploadID)

	if _, err := os.Stat(uploadPath); os.IsNotExist(err) {
		return nil
	}

	if err := os.RemoveAll(uploadPath); err != nil {
		return fmt.Errorf("failed to remove upload: %w", err)
	}

	b.cleanEmptyDirs(filepath.Dir(uploadPath))
	return nil
}

// cleanEmptyDirs removes empty directories
func (b *Backend) cleanEmptyDirs(dir string) {
	for dir != b.uploadsDir && dir != "." && dir != "/" {
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

// RepairObject checks and repairs an object's shards
func (b *Backend) RepairObject(ctx context.Context, bucket, key string, checksums []string) ([]int, error) {
	totalShards := b.config.TotalShards()
	shards := make([][]byte, totalShards)

	// Read available shards
	for i := 0; i < totalShards; i++ {
		data, err := b.shardManager.ReadShard(ctx, bucket, key, i)
		if err != nil {
			continue
		}
		shards[i] = data
	}

	// Verify and repair
	repaired, err := b.decoder.VerifyAndRepair(shards, checksums)
	if err != nil {
		return nil, err
	}

	// Write repaired shards
	for _, index := range repaired {
		if shards[index] != nil {
			_, _, err := b.shardManager.RepairShard(ctx, bucket, key, index, shards[index])
			if err != nil {
				log.Warn().
					Err(err).
					Int("shard", index).
					Str("bucket", bucket).
					Str("key", key).
					Msg("Failed to write repaired shard")
			}
		}
	}

	return repaired, nil
}

// GetShardInfo returns information about shards for an object
func (b *Backend) GetShardInfo(ctx context.Context, bucket, key string) ([]ShardStatus, error) {
	totalShards := b.config.TotalShards()
	status := make([]ShardStatus, totalShards)

	for i := 0; i < totalShards; i++ {
		status[i].Index = i
		status[i].IsData = i < b.config.DataShards

		if b.shardManager.ShardExists(ctx, bucket, key, i) {
			status[i].Exists = true
			size, err := b.shardManager.GetShardSize(ctx, bucket, key, i)
			if err == nil {
				status[i].Size = size
			}
		}
	}

	return status, nil
}

// ShardStatus represents the status of a single shard
type ShardStatus struct {
	Index    int
	IsData   bool
	Exists   bool
	Size     int64
	NodeID   string
	Checksum string
}
