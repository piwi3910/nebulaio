// Package volume implements a high-performance volume-based storage engine.
// Instead of one file per object, objects are stored in large pre-allocated
// volume files with efficient block-based allocation.
package volume

import (
	"encoding/binary"
	"hash/crc32"
)

// Volume and block size constants.
const (
	// DefaultVolumeSize is the default size of a volume file (32GB).
	DefaultVolumeSize = 32 * 1024 * 1024 * 1024

	// BlockSize is the size of each data block (4MB).
	BlockSize = 4 * 1024 * 1024

	// SuperblockSize is the size of the volume superblock.
	SuperblockSize = 4096

	// BitmapOffset is where the allocation bitmap starts.
	BitmapOffset = SuperblockSize

	// BitmapSize is the size reserved for the allocation bitmap.
	BitmapSize = 4096

	// BlockTypeMapOffset is where the block type map starts.
	BlockTypeMapOffset = BitmapOffset + BitmapSize

	// BlockTypeMapSize is the size reserved for block type map.
	BlockTypeMapSize = 32 * 1024

	// IndexOffset is where the object index starts.
	IndexOffset = BlockTypeMapOffset + BlockTypeMapSize

	// MaxIndexSize is the maximum size of the object index.
	MaxIndexSize = 64 * 1024 * 1024

	// DataOffset is where data blocks start (64MB into the file).
	DataOffset = 64 * 1024 * 1024

	// MinDataOffset is a smaller header size for testing (1MB).
	MinDataOffset = 1 * 1024 * 1024

	// BlocksPerVolume is the number of data blocks in a default volume
	// (32GB - 64MB header) / 4MB = ~8176 blocks.
	BlocksPerVolume = (DefaultVolumeSize - DataOffset) / BlockSize

	// MinVolumeSize is the minimum allowed volume size for testing (8MB).
	MinVolumeSize = 8 * 1024 * 1024
)

// Block type constants.
const (
	BlockTypeFree        uint8 = 0 // Block is unallocated
	BlockTypeLarge       uint8 = 1 // Block contains a single large object
	BlockTypePackedTiny  uint8 = 2 // Block contains packed tiny objects (≤64KB)
	BlockTypePackedSmall uint8 = 3 // Block contains packed small objects (≤256KB)
	BlockTypePackedMed   uint8 = 4 // Block contains packed medium objects (≤1MB)
	BlockTypeSpanning    uint8 = 5 // Block is part of a multi-block object
)

// Size class thresholds for packed blocks.
const (
	TinyObjectMax   = 64 * 1024       // 64KB - objects ≤ this go in TINY blocks
	SmallObjectMax  = 256 * 1024      // 256KB - objects ≤ this go in SMALL blocks
	MediumObjectMax = 1 * 1024 * 1024 // 1MB - objects ≤ this go in MEDIUM blocks
	// Objects > 1MB get their own LARGE block or span multiple blocks.
)

// Header sizes.
const (
	BlockHeaderSize      = 64 // Size of packed block header
	ObjectEntrySize      = 32 // Size of object entry header (before key and data)
	IndexEntrySize       = 64 // Size of fixed part of index entry
	SuperblockMagic      = "NEBULAVL"
	SuperblockVersion    = 1
	superblockBufferSize = 128 // Size of superblock buffer for serialization
	alignTo8Mask         = 7   // Mask for 8-byte alignment
)

// Object flags.
const (
	FlagDeleted    uint16 = 1 << 0 // Object has been deleted
	FlagCompressed uint16 = 1 << 1 // Object data is compressed
)

// Superblock represents the volume header stored at offset 0.
type Superblock struct {
	DataOffset  uint64
	IndexOffset uint64
	ObjectCount uint64
	VolumeSize  uint64
	Modified    int64
	IndexSize   uint64
	Created     int64
	Version     uint32
	FreeBlocks  uint32
	TotalBlocks uint32
	BlockSize   uint32
	Checksum    uint32
	_           [20]byte
	VolumeID    [16]byte
	Magic       [8]byte
}

// BlockHeader is the header for packed blocks (64 bytes).
type BlockHeader struct {
	Type        uint8    // Block type (packed tiny/small/medium)
	_           [3]byte  // Padding for alignment
	ObjectCount uint32   // Number of objects in this block
	FreeOffset  uint32   // Offset where free space starts
	FreeSize    uint32   // Amount of free space remaining
	Checksum    uint32   // CRC32 of block data
	FirstKey    [16]byte // Hash of first object key (for range queries)
	LastKey     [16]byte // Hash of last object key
	_           [16]byte // Reserved
}

// ObjectEntry is the header for each object within a packed block (32 bytes).
type ObjectEntry struct {
	KeyHash  [16]byte // Hash of bucket/key for fast matching
	DataSize uint32   // Size of object data
	KeyLen   uint16   // Length of key string
	Flags    uint16   // Object flags (deleted, compressed, etc.)
	Checksum uint32   // CRC32 of object data
	_        [4]byte  // Reserved
	// Followed by: Key (KeyLen bytes), Data (DataSize bytes), padding to 8-byte alignment
}

// IndexEntry represents an object in the volume index.
type IndexEntry struct {
	Size          uint64
	Created       int64
	BlockNum      uint32
	OffsetInBlock uint32
	BlockCount    uint16
	Flags         uint16
	KeyLen        uint16
	KeyHash       [16]byte
}

// MarshalSuperblock serializes a Superblock to bytes.
func (s *Superblock) Marshal() []byte {
	buf := make([]byte, superblockBufferSize)
	copy(buf[0:8], s.Magic[:])
	binary.LittleEndian.PutUint32(buf[8:12], s.Version)
	copy(buf[12:28], s.VolumeID[:])
	binary.LittleEndian.PutUint64(buf[28:36], s.VolumeSize)
	binary.LittleEndian.PutUint32(buf[36:40], s.BlockSize)
	binary.LittleEndian.PutUint32(buf[40:44], s.TotalBlocks)
	binary.LittleEndian.PutUint32(buf[44:48], s.FreeBlocks)
	binary.LittleEndian.PutUint64(buf[48:56], s.IndexOffset)
	binary.LittleEndian.PutUint64(buf[56:64], s.IndexSize)
	binary.LittleEndian.PutUint64(buf[64:72], s.DataOffset)
	//nolint:gosec // G115: Unix timestamps are non-negative and within uint64 range
	binary.LittleEndian.PutUint64(buf[72:80], uint64(s.Created))
	//nolint:gosec // G115: Unix timestamps are non-negative and within uint64 range
	binary.LittleEndian.PutUint64(buf[80:88], uint64(s.Modified))
	binary.LittleEndian.PutUint64(buf[88:96], s.ObjectCount)
	// Calculate checksum over bytes 0-95
	checksum := crc32.ChecksumIEEE(buf[0:96])
	binary.LittleEndian.PutUint32(buf[96:100], checksum)

	return buf
}

// UnmarshalSuperblock deserializes a Superblock from bytes.
func UnmarshalSuperblock(buf []byte) (*Superblock, error) {
	if len(buf) < superblockBufferSize {
		return nil, ErrInvalidSuperblock
	}

	s := &Superblock{}
	copy(s.Magic[:], buf[0:8])
	s.Version = binary.LittleEndian.Uint32(buf[8:12])
	copy(s.VolumeID[:], buf[12:28])
	s.VolumeSize = binary.LittleEndian.Uint64(buf[28:36])
	s.BlockSize = binary.LittleEndian.Uint32(buf[36:40])
	s.TotalBlocks = binary.LittleEndian.Uint32(buf[40:44])
	s.FreeBlocks = binary.LittleEndian.Uint32(buf[44:48])
	s.IndexOffset = binary.LittleEndian.Uint64(buf[48:56])
	s.IndexSize = binary.LittleEndian.Uint64(buf[56:64])
	s.DataOffset = binary.LittleEndian.Uint64(buf[64:72])
	//nolint:gosec // G115: Unix timestamps stored as uint64, safe to cast to int64
	s.Created = int64(binary.LittleEndian.Uint64(buf[72:80]))
	//nolint:gosec // G115: Unix timestamps stored as uint64, safe to cast to int64
	s.Modified = int64(binary.LittleEndian.Uint64(buf[80:88]))
	s.ObjectCount = binary.LittleEndian.Uint64(buf[88:96])
	s.Checksum = binary.LittleEndian.Uint32(buf[96:100])

	// Verify magic
	if string(s.Magic[:]) != SuperblockMagic {
		return nil, ErrInvalidMagic
	}

	// Verify checksum
	expectedChecksum := crc32.ChecksumIEEE(buf[0:96])
	if s.Checksum != expectedChecksum {
		return nil, ErrChecksumMismatch
	}

	return s, nil
}

// MarshalBlockHeader serializes a BlockHeader to bytes.
func (h *BlockHeader) Marshal() []byte {
	buf := make([]byte, BlockHeaderSize)
	buf[0] = h.Type
	binary.LittleEndian.PutUint32(buf[4:8], h.ObjectCount)
	binary.LittleEndian.PutUint32(buf[8:12], h.FreeOffset)
	binary.LittleEndian.PutUint32(buf[12:16], h.FreeSize)
	binary.LittleEndian.PutUint32(buf[16:20], h.Checksum)
	copy(buf[20:36], h.FirstKey[:])
	copy(buf[36:52], h.LastKey[:])

	return buf
}

// UnmarshalBlockHeader deserializes a BlockHeader from bytes.
func UnmarshalBlockHeader(buf []byte) (*BlockHeader, error) {
	if len(buf) < BlockHeaderSize {
		return nil, ErrInvalidBlockHeader
	}

	h := &BlockHeader{}
	h.Type = buf[0]
	h.ObjectCount = binary.LittleEndian.Uint32(buf[4:8])
	h.FreeOffset = binary.LittleEndian.Uint32(buf[8:12])
	h.FreeSize = binary.LittleEndian.Uint32(buf[12:16])
	h.Checksum = binary.LittleEndian.Uint32(buf[16:20])
	copy(h.FirstKey[:], buf[20:36])
	copy(h.LastKey[:], buf[36:52])

	return h, nil
}

// MarshalObjectEntry serializes an ObjectEntry to bytes.
func (e *ObjectEntry) Marshal() []byte {
	buf := make([]byte, ObjectEntrySize)
	copy(buf[0:16], e.KeyHash[:])
	binary.LittleEndian.PutUint32(buf[16:20], e.DataSize)
	binary.LittleEndian.PutUint16(buf[20:22], e.KeyLen)
	binary.LittleEndian.PutUint16(buf[22:24], e.Flags)
	binary.LittleEndian.PutUint32(buf[24:28], e.Checksum)

	return buf
}

// UnmarshalObjectEntry deserializes an ObjectEntry from bytes.
func UnmarshalObjectEntry(buf []byte) (*ObjectEntry, error) {
	if len(buf) < ObjectEntrySize {
		return nil, ErrInvalidObjectEntry
	}

	e := &ObjectEntry{}
	copy(e.KeyHash[:], buf[0:16])
	e.DataSize = binary.LittleEndian.Uint32(buf[16:20])
	e.KeyLen = binary.LittleEndian.Uint16(buf[20:22])
	e.Flags = binary.LittleEndian.Uint16(buf[22:24])
	e.Checksum = binary.LittleEndian.Uint32(buf[24:28])

	return e, nil
}

// BlockOffset calculates the file offset for a given block number.
func BlockOffset(blockNum uint32) int64 {
	return DataOffset + int64(blockNum)*BlockSize
}

// AlignTo8 rounds up to the next 8-byte boundary.
func AlignTo8(n int) int {
	return (n + alignTo8Mask) &^ alignTo8Mask
}

// ObjectEntryTotalSize calculates the total size of an object entry including key and data.
func ObjectEntryTotalSize(keyLen int, dataSize int) int {
	return AlignTo8(ObjectEntrySize + keyLen + dataSize)
}

// SizeClassForObject returns the appropriate block type for an object of given size.
func SizeClassForObject(size int64) uint8 {
	switch {
	case size <= TinyObjectMax:
		return BlockTypePackedTiny
	case size <= SmallObjectMax:
		return BlockTypePackedSmall
	case size <= MediumObjectMax:
		return BlockTypePackedMed
	case size <= BlockSize:
		return BlockTypeLarge
	default:
		return BlockTypeSpanning
	}
}

// BlocksNeeded calculates how many blocks are needed for an object of given size.
func BlocksNeeded(size int64) int {
	if size <= BlockSize {
		return 1
	}

	return int((size + BlockSize - 1) / BlockSize)
}
