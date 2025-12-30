package volume

import "errors"

// Volume errors.
var (
	// Superblock errors.
	ErrInvalidSuperblock = errors.New("invalid superblock")
	ErrInvalidMagic      = errors.New("invalid volume magic number")
	ErrChecksumMismatch  = errors.New("checksum mismatch")
	ErrVersionMismatch   = errors.New("unsupported volume version")

	// Block errors.
	ErrInvalidBlockHeader = errors.New("invalid block header")
	ErrInvalidObjectEntry = errors.New("invalid object entry")
	ErrBlockFull          = errors.New("block is full")
	ErrInvalidBlockType   = errors.New("invalid block type")

	// Volume errors.
	ErrVolumeNotOpen     = errors.New("volume is not open")
	ErrVolumeFull        = errors.New("volume is full")
	ErrVolumeCorrupted   = errors.New("volume is corrupted")
	ErrVolumeExists      = errors.New("volume already exists")
	ErrVolumeNotFound    = errors.New("volume not found")
	ErrInvalidVolumeSize = errors.New("invalid volume size")

	// Object errors.
	ErrObjectNotFound   = errors.New("object not found")
	ErrObjectExists     = errors.New("object already exists")
	ErrObjectTooLarge   = errors.New("object too large for volume")
	ErrObjectDeleted    = errors.New("object has been deleted")
	ErrInvalidObjectKey = errors.New("invalid object key")

	// Index errors.
	ErrIndexCorrupted = errors.New("index is corrupted")
	ErrIndexFull      = errors.New("index is full")

	// Allocation errors.
	ErrNoFreeBlocks       = errors.New("no free blocks available")
	ErrNoContiguousBlocks = errors.New("no contiguous blocks available")
	ErrInvalidBlockNum    = errors.New("invalid block number")
)
