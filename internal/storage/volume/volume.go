package volume

import (
	"crypto/md5" //nolint:gosec // G501: MD5 required for S3 ETag compatibility
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Volume represents a single volume file containing objects.
type Volume struct {
	file            *os.File
	dio             *DirectIOFile
	super           *Superblock
	allocMap        *AllocationMap
	index           *Index
	path            string
	blockTypes      []byte
	dioConfig       DirectIOConfig
	compactionStats CompactionStats
	readBytes       uint64
	writeBytes      uint64
	mu              sync.RWMutex
	activeMedium    uint32
	activeSmall     uint32
	activeTiny      uint32
	closed          bool
	dirty           bool
}

// CompactionStats tracks statistics for volume compaction.
type CompactionStats struct {
	// DeletedObjectsCount is the number of objects marked as deleted but not yet reclaimed
	DeletedObjectsCount uint64
	// ReclaimableBytes is the total bytes that can be reclaimed via compaction
	ReclaimableBytes uint64
	// ReplacedObjectsCount is the number of objects replaced (which creates garbage)
	ReplacedObjectsCount uint64
	// LastCompactionTime is when the last compaction ran (Unix timestamp in nanoseconds)
	LastCompactionTime int64
}

// VolumeConfig holds configuration for creating a new volume.
type VolumeConfig struct {
	Size      uint64         // Volume size (default: 32GB)
	BlockSize uint32         // Block size (default: 4MB)
	DirectIO  DirectIOConfig // Direct I/O configuration
}

// DefaultVolumeConfig returns the default volume configuration.
func DefaultVolumeConfig() VolumeConfig {
	return VolumeConfig{
		Size:      DefaultVolumeSize,
		BlockSize: BlockSize,
		DirectIO:  DefaultDirectIOConfig(),
	}
}

// CreateVolume creates a new volume file at the given path.
func CreateVolume(path string, cfg VolumeConfig) (*Volume, error) {
	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		return nil, ErrVolumeExists
	}

	// Use defaults if not specified
	if cfg.Size == 0 {
		cfg.Size = DefaultVolumeSize
	}

	if cfg.BlockSize == 0 {
		cfg.BlockSize = BlockSize
	}

	// Determine data offset based on volume size
	// For small test volumes, use a smaller header
	dataOffset := uint64(DataOffset)
	if cfg.Size < DataOffset+uint64(cfg.BlockSize) {
		// Use minimal header for small volumes
		dataOffset = uint64(MinDataOffset)
	}

	// Validate minimum size
	if cfg.Size < dataOffset+uint64(cfg.BlockSize) {
		return nil, ErrInvalidVolumeSize
	}

	// Create the file
	//nolint:gosec // G304: path is validated by caller
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create volume file: %w", err)
	}

	// Pre-allocate the file
	//nolint:gosec // G115: cfg.Size is validated to reasonable range
	err = file.Truncate(int64(cfg.Size))
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)

		return nil, fmt.Errorf("failed to allocate volume: %w", err)
	}

	// Calculate number of blocks
	//nolint:gosec // G115: Block count fits in uint32 for typical volume sizes
	totalBlocks := uint32((cfg.Size - dataOffset) / uint64(cfg.BlockSize))

	// Create volume ID
	volumeID := uuid.New()

	var volumeIDBytes [16]byte
	copy(volumeIDBytes[:], volumeID[:])

	// Create superblock
	now := time.Now().UnixNano()
	super := &Superblock{
		Version:     SuperblockVersion,
		VolumeID:    volumeIDBytes,
		VolumeSize:  cfg.Size,
		BlockSize:   cfg.BlockSize,
		TotalBlocks: totalBlocks,
		FreeBlocks:  totalBlocks,
		IndexOffset: IndexOffset,
		IndexSize:   0,
		DataOffset:  dataOffset,
		Created:     now,
		Modified:    now,
		ObjectCount: 0,
	}
	copy(super.Magic[:], SuperblockMagic)

	// Write superblock
	superBytes := super.Marshal()
	_, err = file.WriteAt(superBytes, 0)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)

		return nil, fmt.Errorf("failed to write superblock: %w", err)
	}

	// Initialize allocation map (all zeros = all free)
	allocMap := NewAllocationMap(totalBlocks)
	err = allocMap.WriteTo(file, BitmapOffset)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)

		return nil, fmt.Errorf("failed to write allocation map: %w", err)
	}

	// Initialize block type map (all zeros = all free)
	blockTypes := make([]byte, totalBlocks)
	_, err = file.WriteAt(blockTypes, BlockTypeMapOffset)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)

		return nil, fmt.Errorf("failed to write block type map: %w", err)
	}

	// Sync to disk
	err = file.Sync()
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)

		return nil, fmt.Errorf("failed to sync volume: %w", err)
	}

	log.Info().
		Str("path", path).
		Str("volume_id", volumeID.String()).
		Uint64("size", cfg.Size).
		Uint32("blocks", totalBlocks).
		Msg("Created new volume")

	// Create volume object
	v := &Volume{
		path:         path,
		file:         file,
		super:        super,
		allocMap:     allocMap,
		blockTypes:   blockTypes,
		index:        NewIndex(),
		activeTiny:   0xFFFFFFFF, // Invalid = none active
		activeSmall:  0xFFFFFFFF,
		activeMedium: 0xFFFFFFFF,
		dioConfig:    cfg.DirectIO,
	}

	// Initialize direct I/O wrapper if enabled
	if cfg.DirectIO.Enabled {
		v.dio = NewDirectIOFile(file, cfg.DirectIO)
	}

	return v, nil
}

// OpenVolume opens an existing volume file.
func OpenVolume(path string) (*Volume, error) {
	//nolint:gosec // G304: path is validated by caller
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrVolumeNotFound
		}

		return nil, fmt.Errorf("failed to open volume: %w", err)
	}

	// Read superblock
	superBytes := make([]byte, 128)
	_, err = file.ReadAt(superBytes, 0)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to read superblock: %w", err)
	}

	super, err := UnmarshalSuperblock(superBytes)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	// Validate version
	if super.Version != SuperblockVersion {
		_ = file.Close()
		return nil, ErrVersionMismatch
	}

	// Read allocation map
	allocMap, err := ReadAllocationMap(file, BitmapOffset, super.TotalBlocks)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to read allocation map: %w", err)
	}

	// Read block type map
	blockTypes := make([]byte, super.TotalBlocks)
	_, err = file.ReadAt(blockTypes, BlockTypeMapOffset)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to read block type map: %w", err)
	}

	// Read index
	//nolint:gosec // G115: IndexOffset and IndexSize are within volume bounds
	index, err := ReadIndex(file, int64(super.IndexOffset), int64(super.IndexSize))
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to read index: %w", err)
	}

	// Find active packed blocks
	activeTiny := uint32(0xFFFFFFFF)
	activeSmall := uint32(0xFFFFFFFF)
	activeMedium := uint32(0xFFFFFFFF)

	for i := range super.TotalBlocks {
		switch blockTypes[i] {
		case BlockTypePackedTiny:
			if activeTiny == 0xFFFFFFFF {
				activeTiny = i
			}
		case BlockTypePackedSmall:
			if activeSmall == 0xFFFFFFFF {
				activeSmall = i
			}
		case BlockTypePackedMed:
			if activeMedium == 0xFFFFFFFF {
				activeMedium = i
			}
		}
	}

	volumeID, _ := uuid.FromBytes(super.VolumeID[:])
	log.Info().
		Str("path", path).
		Str("volume_id", volumeID.String()).
		Uint32("total_blocks", super.TotalBlocks).
		Uint32("free_blocks", super.FreeBlocks).
		Uint64("objects", super.ObjectCount).
		Msg("Opened volume")

	return &Volume{
		path:         path,
		file:         file,
		super:        super,
		allocMap:     allocMap,
		blockTypes:   blockTypes,
		index:        index,
		activeTiny:   activeTiny,
		activeSmall:  activeSmall,
		activeMedium: activeMedium,
	}, nil
}

// OpenVolumeWithConfig opens an existing volume file with custom configuration.
func OpenVolumeWithConfig(path string, dioConfig DirectIOConfig) (*Volume, error) {
	//nolint:gosec // G304: path is validated by caller
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrVolumeNotFound
		}

		return nil, fmt.Errorf("failed to open volume: %w", err)
	}

	// Read superblock
	superBytes := make([]byte, 128)
	_, err = file.ReadAt(superBytes, 0)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to read superblock: %w", err)
	}

	super, err := UnmarshalSuperblock(superBytes)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	// Validate version
	if super.Version != SuperblockVersion {
		_ = file.Close()
		return nil, ErrVersionMismatch
	}

	// Read allocation map
	allocMap, err := ReadAllocationMap(file, BitmapOffset, super.TotalBlocks)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to read allocation map: %w", err)
	}

	// Read block type map
	blockTypes := make([]byte, super.TotalBlocks)
	_, err = file.ReadAt(blockTypes, BlockTypeMapOffset)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to read block type map: %w", err)
	}

	// Read index
	//nolint:gosec // G115: IndexOffset and IndexSize are within volume bounds
	index, err := ReadIndex(file, int64(super.IndexOffset), int64(super.IndexSize))
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to read index: %w", err)
	}

	// Find active packed blocks
	activeTiny := uint32(0xFFFFFFFF)
	activeSmall := uint32(0xFFFFFFFF)
	activeMedium := uint32(0xFFFFFFFF)

	for i := range super.TotalBlocks {
		switch blockTypes[i] {
		case BlockTypePackedTiny:
			if activeTiny == 0xFFFFFFFF {
				activeTiny = i
			}
		case BlockTypePackedSmall:
			if activeSmall == 0xFFFFFFFF {
				activeSmall = i
			}
		case BlockTypePackedMed:
			if activeMedium == 0xFFFFFFFF {
				activeMedium = i
			}
		}
	}

	volumeID, _ := uuid.FromBytes(super.VolumeID[:])
	log.Info().
		Str("path", path).
		Str("volume_id", volumeID.String()).
		Uint32("total_blocks", super.TotalBlocks).
		Uint32("free_blocks", super.FreeBlocks).
		Uint64("objects", super.ObjectCount).
		Bool("direct_io", dioConfig.Enabled && directIOSupported()).
		Msg("Opened volume with config")

	v := &Volume{
		path:         path,
		file:         file,
		super:        super,
		allocMap:     allocMap,
		blockTypes:   blockTypes,
		index:        index,
		activeTiny:   activeTiny,
		activeSmall:  activeSmall,
		activeMedium: activeMedium,
		dioConfig:    dioConfig,
	}

	// Initialize direct I/O wrapper if enabled
	if dioConfig.Enabled {
		v.dio = NewDirectIOFile(file, dioConfig)
	}

	return v, nil
}

// Close closes the volume file.
func (v *Volume) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return nil
	}

	// Flush any pending changes
	err := v.flushLocked()
	if err != nil {
		return err
	}

	v.closed = true

	return v.file.Close()
}

// Sync flushes all pending changes to disk.
func (v *Volume) Sync() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.flushLocked()
}

// flushLocked writes all dirty data to disk (caller must hold lock).
func (v *Volume) flushLocked() error {
	if !v.dirty {
		return nil
	}

	// Update modified time
	v.super.Modified = time.Now().UnixNano()

	// Write allocation map
	err := v.allocMap.WriteTo(v.file, BitmapOffset)
	if err != nil {
		return fmt.Errorf("failed to write allocation map: %w", err)
	}

	// Write block type map
	_, err = v.file.WriteAt(v.blockTypes, BlockTypeMapOffset)
	if err != nil {
		return fmt.Errorf("failed to write block type map: %w", err)
	}

	// Write index
	//nolint:gosec // G115: IndexOffset is within volume bounds
	indexSize, err := v.index.WriteTo(v.file, int64(v.super.IndexOffset))
	if err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	//nolint:gosec // G115: indexSize is positive and within volume bounds
	v.super.IndexSize = uint64(indexSize)

	// Write superblock last (after updating IndexSize)
	_, err = v.file.WriteAt(v.super.Marshal(), 0)
	if err != nil {
		return fmt.Errorf("failed to write superblock: %w", err)
	}

	// Sync to disk
	err = v.file.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}

	v.dirty = false

	return nil
}

// ID returns the volume's unique identifier.
func (v *Volume) ID() string {
	v.mu.RLock()
	defer v.mu.RUnlock()

	id, _ := uuid.FromBytes(v.super.VolumeID[:])

	return id.String()
}

// Path returns the volume file path.
func (v *Volume) Path() string {
	return v.path
}

// Stats returns volume statistics.
func (v *Volume) Stats() VolumeStats {
	v.mu.RLock()
	defer v.mu.RUnlock()

	stats := VolumeStats{
		VolumeID:    v.ID(),
		Path:        v.path,
		TotalSize:   v.super.VolumeSize,
		UsedSize:    uint64(v.super.TotalBlocks-v.super.FreeBlocks) * uint64(v.super.BlockSize),
		TotalBlocks: v.super.TotalBlocks,
		FreeBlocks:  v.super.FreeBlocks,
		ObjectCount: v.super.ObjectCount,
		Created:     time.Unix(0, v.super.Created),
		Modified:    time.Unix(0, v.super.Modified),
		Compaction:  v.compactionStats,
	}

	// Add direct I/O stats if available
	if v.dio != nil {
		stats.DirectIO = v.dio.Stats()
	}

	return stats
}

// CompactionStats returns the current compaction statistics.
func (v *Volume) CompactionStats() CompactionStats {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return v.compactionStats
}

// VolumeStats contains volume statistics.
type VolumeStats struct {
	Created     time.Time
	Modified    time.Time
	VolumeID    string
	Path        string
	DirectIO    DirectIOStats
	Compaction  CompactionStats
	TotalSize   uint64
	UsedSize    uint64
	ObjectCount uint64
	TotalBlocks uint32
	FreeBlocks  uint32
}

// HasSpace checks if the volume has space for an object of given size.
func (v *Volume) HasSpace(size int64) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	blocksNeeded := BlocksNeeded(size)

	return int(v.super.FreeBlocks) >= blocksNeeded
}

// FreeSpace returns the amount of free space in bytes.
func (v *Volume) FreeSpace() uint64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return uint64(v.super.FreeBlocks) * uint64(v.super.BlockSize)
}

// ObjectCount returns the number of objects in the volume.
func (v *Volume) ObjectCount() uint64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return v.super.ObjectCount
}

// allocateBlocks allocates n consecutive blocks, returns starting block number.
func (v *Volume) allocateBlocks(n int) (uint32, error) {
	blockNum, err := v.allocMap.AllocateConsecutive(n)
	if err != nil {
		return 0, err
	}

	//nolint:gosec // G115: n is bounded by max object size
	v.super.FreeBlocks -= uint32(n)
	v.dirty = true

	return blockNum, nil
}

// allocateBlock allocates a single block.
func (v *Volume) allocateBlock() (uint32, error) {
	return v.allocateBlocks(1)
}

// freeBlocks marks blocks as free (used during compaction)
//
//nolint:unused // Reserved for future compaction feature
func (v *Volume) freeBlocks(start uint32, count int) {
	for i := range count {
		//nolint:gosec // G115: i is bounded by count which fits in uint32
		v.allocMap.Free(start + uint32(i))
		//nolint:gosec // G115: i is bounded by count which fits in uint32
		v.blockTypes[start+uint32(i)] = BlockTypeFree
	}

	//nolint:gosec // G115: count is bounded by volume capacity
	v.super.FreeBlocks += uint32(count)
	v.dirty = true
}

// readBlock reads a full block from disk.
func (v *Volume) readBlock(blockNum uint32) ([]byte, error) {
	if blockNum >= v.super.TotalBlocks {
		return nil, ErrInvalidBlockNum
	}

	offset := v.blockOffset(blockNum)

	// Use direct I/O if available
	if v.dio != nil {
		// Get an aligned buffer for direct I/O
		buf := v.dio.GetAlignedBuffer(BlockSize)

		n, err := v.dio.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			v.dio.PutAlignedBuffer(buf)
			return nil, err
		}

		v.readBytes += uint64(n) //nolint:gosec // G115: n is non-negative bytes read
		// Note: caller is responsible for the returned buffer
		// For direct I/O, we return the aligned buffer directly
		return buf, nil
	}

	// Fallback to regular I/O
	buf := make([]byte, BlockSize)

	n, err := v.file.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}

	v.readBytes += uint64(n) //nolint:gosec // G115: n is non-negative bytes read

	return buf, nil
}

// writeBlock writes a full block to disk.
func (v *Volume) writeBlock(blockNum uint32, data []byte) error {
	if blockNum >= v.super.TotalBlocks {
		return ErrInvalidBlockNum
	}

	if len(data) > BlockSize {
		return errors.New("data exceeds block size")
	}

	offset := v.blockOffset(blockNum)

	// Use direct I/O if available
	if v.dio != nil {
		// Get an aligned buffer for direct I/O
		buf := v.dio.GetAlignedBuffer(BlockSize)
		copy(buf, data)
		// Pad remaining with zeros (buf is already zeroed from pool)

		n, err := v.dio.WriteAt(buf, offset)
		v.dio.PutAlignedBuffer(buf)

		if err != nil {
			return err
		}

		v.writeBytes += uint64(n) //nolint:gosec // G115: n is non-negative bytes written
		v.dirty = true

		return nil
	}

	// Fallback to regular I/O
	// Pad to full block size if needed
	if len(data) < BlockSize {
		padded := make([]byte, BlockSize)
		copy(padded, data)
		data = padded
	}

	n, err := v.file.WriteAt(data, offset)
	if err != nil {
		return err
	}

	//nolint:gosec // G115: n is bytes written, always positive
	v.writeBytes += uint64(n)
	v.dirty = true

	return nil
}

// writeBlockAt writes data at a specific offset within a block
//
//nolint:unused // Reserved for future partial block updates
func (v *Volume) writeBlockAt(blockNum uint32, offsetInBlock int64, data []byte) error {
	if blockNum >= v.super.TotalBlocks {
		return ErrInvalidBlockNum
	}

	if offsetInBlock+int64(len(data)) > BlockSize {
		return errors.New("write exceeds block boundary")
	}

	offset := v.blockOffset(blockNum) + offsetInBlock

	n, err := v.file.WriteAt(data, offset)
	if err != nil {
		return err
	}

	//nolint:gosec // G115: n is bytes written, always positive
	v.writeBytes += uint64(n)
	v.dirty = true

	return nil
}

// readBlockAt reads data from a specific offset within a block
//
//nolint:unused // Reserved for future partial block reads
func (v *Volume) readBlockAt(blockNum uint32, offsetInBlock int64, size int) ([]byte, error) {
	if blockNum >= v.super.TotalBlocks {
		return nil, ErrInvalidBlockNum
	}

	if offsetInBlock+int64(size) > BlockSize {
		return nil, errors.New("read exceeds block boundary")
	}

	buf := make([]byte, size)
	offset := v.blockOffset(blockNum) + offsetInBlock

	n, err := v.file.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}

	//nolint:gosec // G115: n is bytes read, always positive
	v.readBytes += uint64(n)

	return buf[:n], nil
}

// blockOffset calculates the file offset for a given block number.
func (v *Volume) blockOffset(blockNum uint32) int64 {
	//nolint:gosec // G115: DataOffset is within volume bounds
	return int64(v.super.DataOffset) + int64(blockNum)*BlockSize
}

// HashKey generates a 16-byte hash from bucket and key.
func HashKey(bucket, key string) [16]byte {
	h := md5.Sum([]byte(bucket + "/" + key)) //nolint:gosec // G401: MD5 for path hashing only
	return h
}

// FullKey returns the combined bucket/key string.
func FullKey(bucket, key string) string {
	return bucket + "/" + key
}
