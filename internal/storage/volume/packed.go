package volume

import (
	"bytes"
	"hash/crc32"
	"io"
	"time"
)

// PackedBlock represents a block containing multiple small objects
type PackedBlock struct {
	header   BlockHeader
	data     []byte
	blockNum uint32
	dirty    bool
}

// NewPackedBlock creates a new empty packed block
func NewPackedBlock(blockType uint8, blockNum uint32) *PackedBlock {
	return &PackedBlock{
		header: BlockHeader{
			Type:        blockType,
			ObjectCount: 0,
			FreeOffset:  BlockHeaderSize,
			FreeSize:    BlockSize - BlockHeaderSize,
		},
		data:     make([]byte, BlockSize),
		blockNum: blockNum,
		dirty:    true,
	}
}

// LoadPackedBlock reads a packed block from disk
func (v *Volume) LoadPackedBlock(blockNum uint32) (*PackedBlock, error) {
	data, err := v.readBlock(blockNum)
	if err != nil {
		return nil, err
	}

	header, err := UnmarshalBlockHeader(data[:BlockHeaderSize])
	if err != nil {
		return nil, err
	}

	return &PackedBlock{
		header:   *header,
		data:     data,
		blockNum: blockNum,
		dirty:    false,
	}, nil
}

// Save writes the packed block to disk
func (pb *PackedBlock) Save(v *Volume) error {
	// Update header in data
	copy(pb.data[:BlockHeaderSize], pb.header.Marshal())

	// Calculate checksum of data portion
	pb.header.Checksum = crc32.ChecksumIEEE(pb.data[BlockHeaderSize:pb.header.FreeOffset])
	copy(pb.data[:BlockHeaderSize], pb.header.Marshal())

	if err := v.writeBlock(pb.blockNum, pb.data); err != nil {
		return err
	}

	pb.dirty = false
	return nil
}

// CanFit checks if an object of given size can fit in this block
func (pb *PackedBlock) CanFit(keyLen int, dataSize int) bool {
	needed := ObjectEntryTotalSize(keyLen, dataSize)
	return uint32(needed) <= pb.header.FreeSize
}

// FreeSpace returns the amount of free space in the block
func (pb *PackedBlock) FreeSpace() uint32 {
	return pb.header.FreeSize
}

// ObjectCount returns the number of objects in the block
func (pb *PackedBlock) ObjectCount() uint32 {
	return pb.header.ObjectCount
}

// Append adds an object to the packed block
// Returns the offset within the block where the object was stored
func (pb *PackedBlock) Append(keyHash [16]byte, key string, data []byte) (uint32, error) {
	keyLen := len(key)
	dataSize := len(data)
	totalSize := ObjectEntryTotalSize(keyLen, dataSize)

	if uint32(totalSize) > pb.header.FreeSize {
		return 0, ErrBlockFull
	}

	offset := pb.header.FreeOffset

	// Create object entry
	entry := ObjectEntry{
		KeyHash:  keyHash,
		DataSize: uint32(dataSize),
		KeyLen:   uint16(keyLen),
		Flags:    0,
		Checksum: crc32.ChecksumIEEE(data),
	}

	// Write entry header
	entryBytes := entry.Marshal()
	copy(pb.data[offset:], entryBytes)

	// Write key
	copy(pb.data[offset+ObjectEntrySize:], key)

	// Write data
	copy(pb.data[offset+ObjectEntrySize+uint32(keyLen):], data)

	// Update header
	pb.header.ObjectCount++
	pb.header.FreeOffset = offset + uint32(totalSize)
	pb.header.FreeSize -= uint32(totalSize)

	// Update key range
	if pb.header.ObjectCount == 1 {
		pb.header.FirstKey = keyHash
	}
	pb.header.LastKey = keyHash

	pb.dirty = true
	return offset, nil
}

// ReadObject reads an object from the packed block
func (pb *PackedBlock) ReadObject(offset uint32) (key string, data []byte, flags uint16, err error) {
	if offset+ObjectEntrySize > uint32(len(pb.data)) {
		return "", nil, 0, ErrInvalidObjectEntry
	}

	// Read entry header
	entry, err := UnmarshalObjectEntry(pb.data[offset : offset+ObjectEntrySize])
	if err != nil {
		return "", nil, 0, err
	}

	// Read key
	keyStart := offset + ObjectEntrySize
	keyEnd := keyStart + uint32(entry.KeyLen)
	if keyEnd > uint32(len(pb.data)) {
		return "", nil, 0, ErrInvalidObjectEntry
	}
	key = string(pb.data[keyStart:keyEnd])

	// Read data
	dataStart := keyEnd
	dataEnd := dataStart + entry.DataSize
	if dataEnd > uint32(len(pb.data)) {
		return "", nil, 0, ErrInvalidObjectEntry
	}
	data = make([]byte, entry.DataSize)
	copy(data, pb.data[dataStart:dataEnd])

	// Verify checksum
	if crc32.ChecksumIEEE(data) != entry.Checksum {
		return "", nil, 0, ErrChecksumMismatch
	}

	return key, data, entry.Flags, nil
}

// MarkDeleted marks an object as deleted in the packed block
func (pb *PackedBlock) MarkDeleted(offset uint32) error {
	if offset+ObjectEntrySize > uint32(len(pb.data)) {
		return ErrInvalidObjectEntry
	}

	// Read current flags
	flagsOffset := offset + 22 // Offset of Flags in ObjectEntry
	flags := uint16(pb.data[flagsOffset]) | uint16(pb.data[flagsOffset+1])<<8

	// Set deleted flag
	flags |= FlagDeleted
	pb.data[flagsOffset] = byte(flags)
	pb.data[flagsOffset+1] = byte(flags >> 8)

	pb.dirty = true
	return nil
}

// Put stores an object in the volume
func (v *Volume) Put(bucket, key string, r io.Reader, size int64) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return ErrVolumeNotOpen
	}

	// Read all data
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}

	fullKey := FullKey(bucket, key)
	keyHash := HashKey(bucket, key)

	// Check if object already exists
	if existing, exists := v.index.Get(bucket, key); exists {
		// Mark old entry as deleted
		v.index.Delete(bucket, key)
		v.super.ObjectCount-- // Decrement since we're replacing

		// Track for compaction stats - the old space becomes reclaimable
		v.compactionStats.ReplacedObjectsCount++
		v.compactionStats.ReclaimableBytes += existing.Size
		v.compactionStats.DeletedObjectsCount++
	}

	var blockNum uint32
	var offsetInBlock uint32
	var blockCount uint16 = 1

	sizeClass := SizeClassForObject(size)

	switch sizeClass {
	case BlockTypePackedTiny, BlockTypePackedSmall, BlockTypePackedMed:
		// Use packed block
		blockNum, offsetInBlock = v.putPacked(sizeClass, keyHash, fullKey, data)
		if blockNum == 0xFFFFFFFF {
			return ErrVolumeFull
		}

	case BlockTypeLarge:
		// Single block for medium-large objects
		var err error
		blockNum, err = v.allocateBlock()
		if err != nil {
			return err
		}
		v.blockTypes[blockNum] = BlockTypeLarge
		if err := v.writeBlock(blockNum, data); err != nil {
			return err
		}
		offsetInBlock = 0

	case BlockTypeSpanning:
		// Multiple blocks for very large objects
		blocksNeeded := BlocksNeeded(size)
		var err error
		blockNum, err = v.allocateBlocks(blocksNeeded)
		if err != nil {
			return err
		}
		blockCount = uint16(blocksNeeded)

		// Mark blocks as spanning
		for i := 0; i < blocksNeeded; i++ {
			v.blockTypes[blockNum+uint32(i)] = BlockTypeSpanning
		}

		// Write data across blocks
		remaining := data
		for i := 0; i < blocksNeeded; i++ {
			writeSize := BlockSize
			if len(remaining) < BlockSize {
				writeSize = len(remaining)
			}
			if err := v.writeBlock(blockNum+uint32(i), remaining[:writeSize]); err != nil {
				return err
			}
			remaining = remaining[writeSize:]
		}
		offsetInBlock = 0
	}

	// Add to index
	entry := IndexEntryFull{
		IndexEntry: IndexEntry{
			KeyHash:       keyHash,
			BlockNum:      blockNum,
			BlockCount:    blockCount,
			OffsetInBlock: offsetInBlock,
			Size:          uint64(size),
			Created:       time.Now().UnixNano(),
			Flags:         0,
			KeyLen:        uint16(len(fullKey)),
		},
		Key: fullKey,
	}
	v.index.Put(entry)
	v.super.ObjectCount++
	v.dirty = true

	return nil
}

// putPacked stores an object in a packed block
func (v *Volume) putPacked(blockType uint8, keyHash [16]byte, key string, data []byte) (uint32, uint32) {
	// Get or create active block for this size class
	var activeBlock *uint32
	switch blockType {
	case BlockTypePackedTiny:
		activeBlock = &v.activeTiny
	case BlockTypePackedSmall:
		activeBlock = &v.activeSmall
	case BlockTypePackedMed:
		activeBlock = &v.activeMedium
	}

	// Try to use active block
	if *activeBlock != 0xFFFFFFFF {
		pb, err := v.LoadPackedBlock(*activeBlock)
		if err == nil && pb.CanFit(len(key), len(data)) {
			offset, err := pb.Append(keyHash, key, data)
			if err == nil {
				if err := pb.Save(v); err == nil {
					return *activeBlock, offset
				}
			}
		}
	}

	// Need a new block
	blockNum, err := v.allocateBlock()
	if err != nil {
		return 0xFFFFFFFF, 0
	}

	v.blockTypes[blockNum] = blockType
	*activeBlock = blockNum

	pb := NewPackedBlock(blockType, blockNum)
	offset, err := pb.Append(keyHash, key, data)
	if err != nil {
		return 0xFFFFFFFF, 0
	}

	if err := pb.Save(v); err != nil {
		return 0xFFFFFFFF, 0
	}

	return blockNum, offset
}

// Get retrieves an object from the volume
func (v *Volume) Get(bucket, key string) ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, ErrVolumeNotOpen
	}

	entry, exists := v.index.Get(bucket, key)
	if !exists {
		return nil, ErrObjectNotFound
	}

	blockType := v.blockTypes[entry.BlockNum]

	switch blockType {
	case BlockTypePackedTiny, BlockTypePackedSmall, BlockTypePackedMed:
		// Read from packed block
		pb, err := v.LoadPackedBlock(entry.BlockNum)
		if err != nil {
			return nil, err
		}
		_, data, flags, err := pb.ReadObject(entry.OffsetInBlock)
		if err != nil {
			return nil, err
		}
		if flags&FlagDeleted != 0 {
			return nil, ErrObjectNotFound
		}
		return data, nil

	case BlockTypeLarge:
		// Read single block
		data, err := v.readBlock(entry.BlockNum)
		if err != nil {
			return nil, err
		}
		return data[:entry.Size], nil

	case BlockTypeSpanning:
		// Read multiple blocks
		data := make([]byte, entry.Size)
		remaining := data
		for i := uint16(0); i < entry.BlockCount; i++ {
			block, err := v.readBlock(entry.BlockNum + uint32(i))
			if err != nil {
				return nil, err
			}
			copySize := BlockSize
			if len(remaining) < BlockSize {
				copySize = len(remaining)
			}
			copy(remaining[:copySize], block[:copySize])
			remaining = remaining[copySize:]
		}
		return data, nil
	}

	return nil, ErrInvalidBlockType
}

// GetReader returns a reader for the object data
func (v *Volume) GetReader(bucket, key string) (io.ReadCloser, int64, error) {
	data, err := v.Get(bucket, key)
	if err != nil {
		return nil, 0, err
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

// Delete marks an object as deleted
func (v *Volume) Delete(bucket, key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return ErrVolumeNotOpen
	}

	entry, exists := v.index.Get(bucket, key)
	if !exists {
		return ErrObjectNotFound
	}

	// Mark as deleted in packed block if applicable
	blockType := v.blockTypes[entry.BlockNum]
	if blockType == BlockTypePackedTiny || blockType == BlockTypePackedSmall || blockType == BlockTypePackedMed {
		pb, err := v.LoadPackedBlock(entry.BlockNum)
		if err != nil {
			return err
		}
		if err := pb.MarkDeleted(entry.OffsetInBlock); err != nil {
			return err
		}
		if err := pb.Save(v); err != nil {
			return err
		}
	}

	// For large/spanning blocks, we just mark in index
	// Actual space reclamation happens during compaction

	// Track for compaction stats - the deleted space becomes reclaimable
	v.compactionStats.DeletedObjectsCount++
	v.compactionStats.ReclaimableBytes += entry.Size

	v.index.Delete(bucket, key)
	v.super.ObjectCount--
	v.dirty = true

	return nil
}

// Exists checks if an object exists in the volume
func (v *Volume) Exists(bucket, key string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return false
	}

	_, exists := v.index.Get(bucket, key)
	return exists
}

// List returns objects matching a prefix
func (v *Volume) List(bucket, prefix string, maxKeys int) ([]ObjectInfo, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, ErrVolumeNotOpen
	}

	entries := v.index.List(bucket, prefix, maxKeys)
	result := make([]ObjectInfo, len(entries))

	bucketPrefix := bucket + "/"
	for i, entry := range entries {
		// Extract just the key part (remove bucket prefix)
		objKey := entry.Key
		if len(objKey) > len(bucketPrefix) {
			objKey = objKey[len(bucketPrefix):]
		}

		result[i] = ObjectInfo{
			Bucket:  bucket,
			Key:     objKey,
			Size:    int64(entry.Size),
			Created: time.Unix(0, entry.Created),
			KeyHash: entry.KeyHash,
		}
	}

	return result, nil
}

// ObjectInfo contains information about an object
type ObjectInfo struct {
	Bucket  string
	Key     string
	Size    int64
	Created time.Time
	KeyHash [16]byte
}
