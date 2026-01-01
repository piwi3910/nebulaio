package volume

import (
	"bytes"
	"hash/crc32"
	"io"
	"sync/atomic"
	"time"
)

// PackedBlock represents a block containing multiple small objects.
type PackedBlock struct {
	data     []byte
	header   BlockHeader
	blockNum uint32
	dirty    bool
}

// NewPackedBlock creates a new empty packed block.
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

// LoadPackedBlock reads a packed block from disk.
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

// Save writes the packed block to disk.
func (pb *PackedBlock) Save(v *Volume) error {
	// Update header in data
	copy(pb.data[:BlockHeaderSize], pb.header.Marshal())

	// Calculate checksum of data portion
	pb.header.Checksum = crc32.ChecksumIEEE(pb.data[BlockHeaderSize:pb.header.FreeOffset])
	copy(pb.data[:BlockHeaderSize], pb.header.Marshal())

	err := v.writeBlock(pb.blockNum, pb.data)
	if err != nil {
		return err
	}

	pb.dirty = false

	return nil
}

// CanFit checks if an object of given size can fit in this block.
func (pb *PackedBlock) CanFit(keyLen int, dataSize int) bool {
	needed := ObjectEntryTotalSize(keyLen, dataSize)
	//nolint:gosec // G115: needed is bounded by block size
	return uint32(needed) <= pb.header.FreeSize
}

// FreeSpace returns the amount of free space in the block.
func (pb *PackedBlock) FreeSpace() uint32 {
	return pb.header.FreeSize
}

// ObjectCount returns the number of objects in the block.
func (pb *PackedBlock) ObjectCount() uint32 {
	return pb.header.ObjectCount
}

// Append adds an object to the packed block
// Returns the offset within the block where the object was stored.
func (pb *PackedBlock) Append(keyHash [16]byte, key string, data []byte) (uint32, error) {
	keyLen := len(key)
	dataSize := len(data)
	totalSize := ObjectEntryTotalSize(keyLen, dataSize)

	//nolint:gosec // G115: totalSize is bounded by block size
	if uint32(totalSize) > pb.header.FreeSize {
		return 0, ErrBlockFull
	}

	offset := pb.header.FreeOffset

	// Create object entry
	entry := ObjectEntry{
		KeyHash:  keyHash,
		DataSize: uint32(dataSize), //nolint:gosec // G115: dataSize validated at caller
		KeyLen:   uint16(keyLen),   //nolint:gosec // G115: keyLen bounded by S3 limits
		Flags:    0,
		Checksum: crc32.ChecksumIEEE(data),
	}

	// Write entry header
	entryBytes := entry.Marshal()
	copy(pb.data[offset:], entryBytes)

	// Write key
	copy(pb.data[offset+ObjectEntrySize:], key)

	// Write data
	//nolint:gosec // G115: keyLen bounded by S3 limits
	copy(pb.data[offset+ObjectEntrySize+uint32(keyLen):], data)

	// Update header
	pb.header.ObjectCount++
	//nolint:gosec // G115: totalSize is bounded by block size
	pb.header.FreeOffset = offset + uint32(totalSize)
	//nolint:gosec // G115: totalSize is bounded by block size
	pb.header.FreeSize -= uint32(totalSize)

	// Update key range
	if pb.header.ObjectCount == 1 {
		pb.header.FirstKey = keyHash
	}

	pb.header.LastKey = keyHash

	pb.dirty = true

	return offset, nil
}

// ReadObject reads an object from the packed block.
func (pb *PackedBlock) ReadObject(offset uint32) (key string, data []byte, flags uint16, err error) {
	//nolint:gosec // G115: len(pb.data) bounded by block size
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
	//nolint:gosec // G115: len(pb.data) is bounded by block size
	if keyEnd > uint32(len(pb.data)) {
		return "", nil, 0, ErrInvalidObjectEntry
	}

	key = string(pb.data[keyStart:keyEnd])

	// Read data
	dataStart := keyEnd

	dataEnd := dataStart + entry.DataSize
	//nolint:gosec // G115: len(pb.data) is bounded by block size
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

// MarkDeleted marks an object as deleted in the packed block.
func (pb *PackedBlock) MarkDeleted(offset uint32) error {
	//nolint:gosec // G115: len(pb.data) is bounded by block size
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

// Put stores an object in the volume.
func (v *Volume) Put(bucket, key string, r io.Reader, size int64) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return ErrVolumeNotOpen
	}

	data, err := v.readObjectData(r, size)
	if err != nil {
		return err
	}

	fullKey := FullKey(bucket, key)
	keyHash := HashKey(bucket, key)

	v.replaceExistingObject(bucket, key)

	blockNum, offsetInBlock, blockCount, err := v.allocateAndWriteBlocks(fullKey, keyHash, data, size)
	if err != nil {
		return err
	}

	v.addToIndex(fullKey, keyHash, blockNum, offsetInBlock, blockCount, size)

	return nil
}

func (v *Volume) readObjectData(r io.Reader, size int64) ([]byte, error) {
	data := make([]byte, size)
	_, err := io.ReadFull(r, data)
	return data, err
}

func (v *Volume) replaceExistingObject(bucket, key string) {
	existing, exists := v.index.Get(bucket, key)
	if !exists {
		return
	}

	v.index.Delete(bucket, key)
	v.super.ObjectCount--

	atomic.AddUint64(&v.compactionStats.ReplacedObjectsCount, 1)
	atomic.AddUint64(&v.compactionStats.ReclaimableBytes, existing.Size)
	atomic.AddUint64(&v.compactionStats.DeletedObjectsCount, 1)
}

func (v *Volume) allocateAndWriteBlocks(fullKey string, keyHash [16]byte, data []byte, size int64) (uint32, uint32, uint16, error) {
	sizeClass := SizeClassForObject(size)

	switch sizeClass {
	case BlockTypePackedTiny, BlockTypePackedSmall, BlockTypePackedMed:
		return v.allocatePackedBlock(sizeClass, keyHash, fullKey, data)
	case BlockTypeLarge:
		return v.allocateLargeBlock(data)
	case BlockTypeSpanning:
		return v.allocateSpanningBlocks(data, size)
	default:
		return 0, 0, 0, ErrVolumeFull
	}
}

func (v *Volume) allocatePackedBlock(sizeClass uint8, keyHash [16]byte, fullKey string, data []byte) (uint32, uint32, uint16, error) {
	blockNum, offsetInBlock := v.putPacked(sizeClass, keyHash, fullKey, data)
	if blockNum == 0xFFFFFFFF {
		return 0, 0, 0, ErrVolumeFull
	}

	return blockNum, offsetInBlock, 1, nil
}

func (v *Volume) allocateLargeBlock(data []byte) (uint32, uint32, uint16, error) {
	blockNum, err := v.allocateBlock()
	if err != nil {
		return 0, 0, 0, err
	}

	v.blockTypes[blockNum] = BlockTypeLarge
	err = v.writeBlock(blockNum, data)
	if err != nil {
		return 0, 0, 0, err
	}

	return blockNum, 0, 1, nil
}

func (v *Volume) allocateSpanningBlocks(data []byte, size int64) (uint32, uint32, uint16, error) {
	blocksNeeded := BlocksNeeded(size)

	blockNum, err := v.allocateBlocks(blocksNeeded)
	if err != nil {
		return 0, 0, 0, err
	}

	v.markSpanningBlocks(blockNum, blocksNeeded)

	if err := v.writeSpanningData(blockNum, data, blocksNeeded); err != nil {
		return 0, 0, 0, err
	}

	return blockNum, 0, uint16(blocksNeeded), nil //nolint:gosec // G115: blocksNeeded bounded by max object size
}

func (v *Volume) markSpanningBlocks(blockNum uint32, blocksNeeded int) {
	for i := range blocksNeeded {
		//nolint:gosec // G115: i is bounded by blocksNeeded which fits in uint32
		v.blockTypes[blockNum+uint32(i)] = BlockTypeSpanning
	}
}

func (v *Volume) writeSpanningData(blockNum uint32, data []byte, blocksNeeded int) error {
	remaining := data

	for i := range blocksNeeded {
		writeSize := BlockSize
		if len(remaining) < BlockSize {
			writeSize = len(remaining)
		}

		//nolint:gosec // G115: i is bounded by blocksNeeded which fits in uint32
		err := v.writeBlock(blockNum+uint32(i), remaining[:writeSize])
		if err != nil {
			return err
		}

		remaining = remaining[writeSize:]
	}

	return nil
}

func (v *Volume) addToIndex(fullKey string, keyHash [16]byte, blockNum, offsetInBlock uint32, blockCount uint16, size int64) {
	entry := IndexEntryFull{
		IndexEntry: IndexEntry{
			KeyHash:       keyHash,
			BlockNum:      blockNum,
			BlockCount:    blockCount,
			OffsetInBlock: offsetInBlock,
			Size:          uint64(size), //nolint:gosec // G115: size is validated positive
			Created:       time.Now().UnixNano(),
			Flags:         0,
			KeyLen:        uint16(len(fullKey)), //nolint:gosec // G115: key length bounded by S3 limits
		},
		Key: fullKey,
	}
	v.index.Put(entry)
	v.super.ObjectCount++
	v.dirty = true
}

// putPacked stores an object in a packed block.
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
				err := pb.Save(v)
				if err == nil {
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

	err = pb.Save(v)
	if err != nil {
		return 0xFFFFFFFF, 0
	}

	return blockNum, offset
}

// Get retrieves an object from the volume.
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

		for i := range entry.BlockCount {
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

// GetReader returns a reader for the object data.
func (v *Volume) GetReader(bucket, key string) (io.ReadCloser, int64, error) {
	data, err := v.Get(bucket, key)
	if err != nil {
		return nil, 0, err
	}

	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

// Delete marks an object as deleted.
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

		err = pb.MarkDeleted(entry.OffsetInBlock)
		if err != nil {
			return err
		}

		err = pb.Save(v)
		if err != nil {
			return err
		}
	}

	// For large/spanning blocks, we just mark in index
	// Actual space reclamation happens during compaction

	// Track for compaction stats - the deleted space becomes reclaimable
	atomic.AddUint64(&v.compactionStats.DeletedObjectsCount, 1)
	atomic.AddUint64(&v.compactionStats.ReclaimableBytes, entry.Size)

	v.index.Delete(bucket, key)
	v.super.ObjectCount--
	v.dirty = true

	return nil
}

// Exists checks if an object exists in the volume.
func (v *Volume) Exists(bucket, key string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return false
	}

	_, exists := v.index.Get(bucket, key)

	return exists
}

// List returns objects matching a prefix.
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
			Size:    int64(entry.Size), //nolint:gosec // G115: entry.Size is uint64 from storage
			Created: time.Unix(0, entry.Created),
			KeyHash: entry.KeyHash,
		}
	}

	return result, nil
}

// ObjectInfo contains information about an object.
type ObjectInfo struct {
	Created time.Time
	Bucket  string
	Key     string
	Size    int64
	KeyHash [16]byte
}
