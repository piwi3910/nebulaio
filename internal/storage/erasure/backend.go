// Package erasure implements Reed-Solomon erasure coding storage for NebulaIO.
//
// Erasure coding provides data durability by distributing encoded data shards
// across multiple storage nodes. If some shards are lost, the original data
// can be reconstructed from the remaining shards.
//
// Configuration options:
//   - Data shards: Number of data fragments (e.g., 10)
//   - Parity shards: Number of parity fragments (e.g., 4)
//   - With 10+4, any 4 shards can fail without data loss
//
// Typical configurations:
//   - 4+2: 50% overhead, tolerates 2 failures (minimum viable)
//   - 8+4: 50% overhead, tolerates 4 failures (balanced)
//   - 10+4: 40% overhead, tolerates 4 failures (recommended production)
//
// The backend coordinates with placement groups to distribute shards across
// nodes and racks for optimal failure isolation.
package erasure

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // G501: MD5 required for S3 ETag compatibility
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/piwi3910/nebulaio/internal/cluster"
	"github.com/piwi3910/nebulaio/internal/storage/backend"
	"github.com/rs/zerolog/log"
)

// File permission constants.
const (
	dirPermissions      = 0750 // Directory permissions for shards and uploads
	metadataPermissions = 0600 // Metadata file permissions (more restrictive)
	defaultVirtualNodes = 100  // Default virtual nodes for consistent hash placement
)

// objectMetadata stores metadata for an erasure-coded object.
type objectMetadata struct {
	OriginalChecksum string `json:"original_checksum"`
	OriginalSize     int64  `json:"original_size"`
	DataShards       int    `json:"data_shards"`
	ParityShards     int    `json:"parity_shards"`
}

// Backend implements the storage backend using erasure coding.
type Backend struct {
	config            Config
	placement         PlacementStrategy
	encoder           *Encoder
	decoder           *Decoder
	shardManager      *ShardManager
	placementGroupMgr *cluster.PlacementGroupManager
	uploadsDir        string
	localNodeID       string
	nodes             []NodeInfo
	mu                sync.RWMutex
}

// New creates a new erasure coding backend.
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
	if err := os.MkdirAll(uploadsDir, dirPermissions); err != nil {
		return nil, fmt.Errorf("failed to create uploads directory: %w", err)
	}

	b := &Backend{
		config:            config,
		encoder:           encoder,
		decoder:           decoder,
		shardManager:      shardManager,
		placement:         NewConsistentHashPlacement(defaultVirtualNodes),
		nodes:             make([]NodeInfo, 0),
		uploadsDir:        uploadsDir,
		localNodeID:       localNodeID,
		placementGroupMgr: config.PlacementGroupManager,
	}

	// If placement group manager is provided, log distributed mode
	if b.placementGroupMgr != nil {
		localGroup := b.placementGroupMgr.LocalGroup()
		if localGroup != nil {
			log.Info().
				Str("placement_group", string(localGroup.ID)).
				Int("nodes_in_group", len(localGroup.Nodes)).
				Msg("Erasure backend using placement group for shard distribution")
		}
	}

	return b, nil
}

// Init initializes the storage backend.
func (b *Backend) Init(ctx context.Context) error {
	log.Info().
		Int("data_shards", b.config.DataShards).
		Int("parity_shards", b.config.ParityShards).
		Str("data_dir", b.config.DataDir).
		Msg("Initializing erasure coding backend")

	return nil
}

// Close closes the storage backend.
func (b *Backend) Close() error {
	return nil
}

// SetNodes updates the list of available storage nodes.
func (b *Backend) SetNodes(nodes []NodeInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nodes = nodes
}

// GetNodes returns the current list of storage nodes.
func (b *Backend) GetNodes() []NodeInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.nodes
}

// GetPlacementNodesForObject returns the nodes that should store shards for an object.
// If a placement group manager is configured, uses hash-based distribution across
// the placement group nodes. Otherwise, falls back to the local consistent hash placement.
func (b *Backend) GetPlacementNodesForObject(bucket, key string) ([]string, error) {
	if b.placementGroupMgr != nil {
		totalShards := b.config.DataShards + b.config.ParityShards
		return b.placementGroupMgr.GetShardPlacementNodesForObject(bucket, key, totalShards)
	}
	// Fallback to local placement (single-node mode)
	return nil, nil
}

// HasPlacementGroup returns true if this backend is configured with a placement group manager.
func (b *Backend) HasPlacementGroup() bool {
	return b.placementGroupMgr != nil
}

// GetPlacementGroupID returns the local placement group ID if configured.
func (b *Backend) GetPlacementGroupID() string {
	if b.placementGroupMgr == nil {
		return ""
	}

	localGroup := b.placementGroupMgr.LocalGroup()
	if localGroup == nil {
		return ""
	}

	return string(localGroup.ID)
}

// metadataPath returns the filesystem path for object metadata.
func (b *Backend) metadataPath(bucket, key string) string {
	hash := md5.Sum([]byte(fmt.Sprintf("%s/%s", bucket, key))) //nolint:gosec // G401: MD5 for path distribution only
	hashHex := hex.EncodeToString(hash[:])
	dir := filepath.Join(b.config.DataDir, "metadata", hashHex[:2], hashHex[2:4])

	return filepath.Join(dir, fmt.Sprintf("%s_%s.meta", bucket, sanitizeKey(key)))
}

// writeObjectMetadata stores object metadata to a file.
func (b *Backend) writeObjectMetadata(bucket, key string, meta *objectMetadata) error {
	path := b.metadataPath(bucket, key)
	if err := os.MkdirAll(filepath.Dir(path), dirPermissions); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	tmpPath := path + ".tmp"
	// Use metadataPermissions to protect metadata from unauthorized access
	if err := os.WriteFile(tmpPath, data, metadataPermissions); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) // Best effort cleanup
		return fmt.Errorf("failed to rename metadata: %w", err)
	}

	return nil
}

// readObjectMetadata reads object metadata from a file.
func (b *Backend) readObjectMetadata(bucket, key string) (*objectMetadata, error) {
	path := b.metadataPath(bucket, key)

	//nolint:gosec // G304: Path constructed from internal metadata path function
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("metadata not found for %s/%s", bucket, key)
		}

		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var meta objectMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &meta, nil
}

// deleteObjectMetadata deletes object metadata.
func (b *Backend) deleteObjectMetadata(bucket, key string) error {
	path := b.metadataPath(bucket, key)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata: %w", err)
	}

	return nil
}

// PutObject stores an object with erasure coding.
func (b *Backend) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	// Check for context cancellation before starting expensive operation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Encode the data
	encoded, err := b.encoder.Encode(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to encode object: %w", err)
	}

	// Check for context cancellation after encoding
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
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

	// Store shards concurrently
	// Use a struct to track results for proper cleanup on failure
	type shardResult struct {
		err   error
		path  string
		index int
	}

	var wg sync.WaitGroup

	results := make([]shardResult, len(encoded.Shards))

	var resultsMu sync.Mutex

	for i, shard := range encoded.Shards {
		wg.Add(1)

		go func(index int, data []byte, assignment NodeAssignment) {
			defer wg.Done()

			// Check for context cancellation before writing
			select {
			case <-ctx.Done():
				resultsMu.Lock()

				results[index] = shardResult{index: index, err: ctx.Err()}

				resultsMu.Unlock()

				return
			default:
			}

			// For now, all shards go to local node (distributed mode would send to remote nodes)
			path, writeErr := b.shardManager.WriteShard(ctx, bucket, key, index, data)

			resultsMu.Lock()

			if writeErr != nil {
				results[index] = shardResult{index: index, err: fmt.Errorf("failed to write shard %d: %w", index, writeErr)}
			} else {
				results[index] = shardResult{index: index, path: path}
			}

			resultsMu.Unlock()
		}(i, shard, assignments[i])
	}

	wg.Wait()

	// Count errors and collect successful paths for potential cleanup
	var (
		errs         []error
		successPaths []string
	)

	for _, result := range results {
		if result.err != nil {
			errs = append(errs, result.err)
		} else if result.path != "" {
			successPaths = append(successPaths, result.path)
		}
	}

	// Allow some shard failures (up to parityShards)
	if len(errs) > b.config.ParityShards {
		// Clean up written shards on failure
		for _, path := range successPaths {
			_ = b.shardManager.DeleteShardByPath(ctx, path)
		}

		return nil, fmt.Errorf("too many shard write failures (%d): %v", len(errs), errs[0])
	}

	// Store object metadata for later retrieval
	objMeta := &objectMetadata{
		OriginalSize:     encoded.OriginalSize,
		OriginalChecksum: encoded.OriginalChecksum,
		DataShards:       b.config.DataShards,
		ParityShards:     b.config.ParityShards,
	}
	if err := b.writeObjectMetadata(bucket, key, objMeta); err != nil {
		log.Warn().
			Err(err).
			Str("bucket", bucket).
			Str("key", key).
			Msg("Failed to write object metadata (shards stored successfully)")
	}

	return &backend.PutResult{
		ETag: encoded.OriginalChecksum,
		Size: encoded.OriginalSize,
		Path: fmt.Sprintf("erasure:%s/%s", bucket, key),
	}, nil
}

// GetObject retrieves an object using erasure coding.
func (b *Backend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	// Read object metadata for original size - this is required for correct data reconstruction
	objMeta, err := b.readObjectMetadata(bucket, key)
	if err != nil {
		// Without metadata, we cannot properly trim padding from reconstructed data
		// This would result in data corruption, so we must fail
		return nil, fmt.Errorf("failed to read object metadata for %s/%s: %w", bucket, key, err)
	}

	// Collect available shards
	totalShards := b.config.TotalShards()
	shards := make([][]byte, totalShards)
	available := 0

	for i := range totalShards {
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

	// Decode the data with original size for proper trimming
	result, err := b.decoder.Decode(&DecodeInput{
		Shards:           shards,
		OriginalSize:     objMeta.OriginalSize,
		ExpectedChecksum: objMeta.OriginalChecksum,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decode object: %w", err)
	}

	log.Debug().
		Str("bucket", bucket).
		Str("key", key).
		Int("shards_available", available).
		Int64("original_size", objMeta.OriginalSize).
		Int("data_len", len(result.Data)).
		Msg("Object decoded successfully")

	return io.NopCloser(bytes.NewReader(result.Data)), nil
}

// DeleteObject deletes an object and all its shards.
func (b *Backend) DeleteObject(ctx context.Context, bucket, key string) error {
	totalShards := b.config.TotalShards()

	var wg sync.WaitGroup

	errChan := make(chan error, totalShards)

	for i := range totalShards {
		wg.Add(1)

		go func(shardIndex int) {
			defer wg.Done()
			err := b.shardManager.DeleteShard(ctx, bucket, key, shardIndex)

			if err != nil {
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

	// Delete object metadata
	err := b.deleteObjectMetadata(bucket, key)
	if err != nil {
		log.Warn().
			Err(err).
			Str("bucket", bucket).
			Str("key", key).
			Msg("Failed to delete object metadata")
	}

	return nil
}

// ObjectExists checks if an object exists (at least minimum shards).
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

// CreateBucket creates storage for a bucket (no-op for erasure coding).
func (b *Backend) CreateBucket(ctx context.Context, bucket string) error {
	// Shards are stored in a flat structure, no bucket directories needed
	return nil
}

// DeleteBucket deletes storage for a bucket.
func (b *Backend) DeleteBucket(ctx context.Context, bucket string) error {
	// Delete all shards for this bucket
	shards, err := b.shardManager.ListShards(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to list shards: %w", err)
	}

	for _, path := range shards {
		err := b.shardManager.DeleteShardByPath(ctx, path)
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("Failed to delete shard")
		}
	}

	return nil
}

// BucketExists checks if bucket storage exists (always true for erasure coding).
func (b *Backend) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return true, nil
}

// GetStorageInfo returns storage statistics.
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

// uploadPath returns the path for a multipart upload.
func (b *Backend) uploadPath(bucket, key, uploadID string) string {
	return filepath.Join(b.uploadsDir, bucket, key, uploadID)
}

// partPath returns the path for a part.
func (b *Backend) partPath(bucket, key, uploadID string, partNumber int) string {
	return filepath.Join(b.uploadPath(bucket, key, uploadID), fmt.Sprintf("part.%d", partNumber))
}

// CreateMultipartUpload creates storage for a multipart upload.
func (b *Backend) CreateMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	path := b.uploadPath(bucket, key, uploadID)

	return os.MkdirAll(path, dirPermissions)
}

// PutPart stores a part (temporarily stored as regular file, encoded on completion).
func (b *Backend) PutPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*backend.PutResult, error) {
	path := b.partPath(bucket, key, uploadID, partNumber)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), dirPermissions); err != nil {
		return nil, fmt.Errorf("failed to create part directory: %w", err)
	}

	// Write to temp file first
	tmpPath := path + ".tmp"

	//nolint:gosec // G304: Path constructed from internal part path function
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	defer func() { _ = os.Remove(tmpPath) }()

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

// GetPart retrieves a part.
func (b *Backend) GetPart(ctx context.Context, bucket, key, uploadID string, partNumber int) (io.ReadCloser, error) {
	path := b.partPath(bucket, key, uploadID, partNumber)

	//nolint:gosec // G304: Path constructed from internal part path function
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("part not found: %d", partNumber)
		}

		return nil, fmt.Errorf("failed to open part: %w", err)
	}

	return file, nil
}

// CompleteParts combines parts and encodes with erasure coding.
func (b *Backend) CompleteParts(ctx context.Context, bucket, key, uploadID string, parts []int) (*backend.PutResult, error) {
	sort.Ints(parts)

	// First, combine all parts into a single buffer
	var combined bytes.Buffer

	originalHash := md5.New() //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	writer := io.MultiWriter(&combined, originalHash)

	for _, partNum := range parts {
		path := b.partPath(bucket, key, uploadID, partNum)

		//nolint:gosec // G304: path is constructed from trusted config
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

// AbortMultipartUpload cleans up a multipart upload.
func (b *Backend) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	uploadPath := b.uploadPath(bucket, key, uploadID)

	if _, err := os.Stat(uploadPath); os.IsNotExist(err) {
		return nil
	}

	err := os.RemoveAll(uploadPath)
	if err != nil {
		return fmt.Errorf("failed to remove upload: %w", err)
	}

	b.cleanEmptyDirs(filepath.Dir(uploadPath))

	return nil
}

// cleanEmptyDirs removes empty directories.
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

// RepairObject checks and repairs an object's shards.
func (b *Backend) RepairObject(ctx context.Context, bucket, key string, checksums []string) ([]int, error) {
	totalShards := b.config.TotalShards()
	shards := make([][]byte, totalShards)

	// Read available shards
	for i := range totalShards {
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

// GetShardInfo returns information about shards for an object.
func (b *Backend) GetShardInfo(ctx context.Context, bucket, key string) ([]ShardStatus, error) {
	totalShards := b.config.TotalShards()
	status := make([]ShardStatus, totalShards)

	for i := range totalShards {
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

// ShardStatus represents the status of a single shard.
type ShardStatus struct {
	NodeID   string
	Checksum string
	Index    int
	Size     int64
	IsData   bool
	Exists   bool
}
