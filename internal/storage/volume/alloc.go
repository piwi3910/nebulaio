package volume

import (
	"os"
	"sync"
)

// Bitmap allocation constants.
const (
	bitsPerByte       = 8 // Number of bits per byte
	byteAlignmentMask = 7 // Mask for byte alignment (bitsPerByte - 1)
)

// AllocationMap tracks which blocks are allocated using a bitmap.
type AllocationMap struct {
	bits  []byte
	total uint32
	free  uint32
	mu    sync.RWMutex
}

// NewAllocationMap creates a new allocation map with all blocks free.
func NewAllocationMap(totalBlocks uint32) *AllocationMap {
	// Round up to bytes
	numBytes := (totalBlocks + byteAlignmentMask) / bitsPerByte

	return &AllocationMap{
		bits:  make([]byte, numBytes),
		total: totalBlocks,
		free:  totalBlocks,
	}
}

// ReadAllocationMap reads an allocation map from disk.
func ReadAllocationMap(file *os.File, offset int64, totalBlocks uint32) (*AllocationMap, error) {
	numBytes := (totalBlocks + byteAlignmentMask) / bitsPerByte
	bits := make([]byte, numBytes)

	_, readErr := file.ReadAt(bits, offset)
	if readErr != nil {
		return nil, readErr
	}

	// Count free blocks
	free := uint32(0)

	for i := range totalBlocks {
		if !isSet(bits, i) {
			free++
		}
	}

	return &AllocationMap{
		bits:  bits,
		total: totalBlocks,
		free:  free,
	}, nil
}

// WriteTo writes the allocation map to disk.
func (m *AllocationMap) WriteTo(file *os.File, offset int64) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, err := file.WriteAt(m.bits, offset)

	return err
}

// IsAllocated checks if a block is allocated.
func (m *AllocationMap) IsAllocated(blockNum uint32) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if blockNum >= m.total {
		return true // Out of range = "allocated"
	}

	return isSet(m.bits, blockNum)
}

// Allocate marks a block as allocated.
func (m *AllocationMap) Allocate(blockNum uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if blockNum >= m.total {
		return ErrInvalidBlockNum
	}

	if isSet(m.bits, blockNum) {
		return nil // Already allocated
	}

	setBit(m.bits, blockNum)
	m.free--

	return nil
}

// Free marks a block as free.
func (m *AllocationMap) Free(blockNum uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if blockNum >= m.total {
		return
	}

	if !isSet(m.bits, blockNum) {
		return // Already free
	}

	clearBit(m.bits, blockNum)
	m.free++
}

// AllocateFirst allocates the first free block and returns its number.
func (m *AllocationMap) AllocateFirst() (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.free == 0 {
		return 0, ErrNoFreeBlocks
	}

	// Find first free block
	for i := range m.total {
		if !isSet(m.bits, i) {
			setBit(m.bits, i)
			m.free--

			return i, nil
		}
	}

	return 0, ErrNoFreeBlocks
}

// AllocateConsecutive allocates n consecutive free blocks.
func (m *AllocationMap) AllocateConsecutive(n int) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if n <= 0 {
		return 0, nil
	}

	//nolint:gosec // G115: n is validated positive above
	if uint32(n) > m.free {
		return 0, ErrNoFreeBlocks
	}

	// Find n consecutive free blocks
	consecutiveStart := uint32(0)
	consecutiveCount := 0

	for i := range m.total {
		if !isSet(m.bits, i) {
			if consecutiveCount == 0 {
				consecutiveStart = i
			}

			consecutiveCount++
			if consecutiveCount == n {
				// Found enough, allocate them
				//nolint:gosec // G115: n is validated positive at function start
				for j := consecutiveStart; j < consecutiveStart+uint32(n); j++ {
					setBit(m.bits, j)
				}

				//nolint:gosec // G115: n is validated positive at function start
				m.free -= uint32(n)

				return consecutiveStart, nil
			}
		} else {
			consecutiveCount = 0
		}
	}

	return 0, ErrNoContiguousBlocks
}

// FreeCount returns the number of free blocks.
func (m *AllocationMap) FreeCount() uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.free
}

// TotalCount returns the total number of blocks.
func (m *AllocationMap) TotalCount() uint32 {
	return m.total
}

// FindFreeBlocks returns a list of free block numbers (up to limit).
func (m *AllocationMap) FindFreeBlocks(limit int) []uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]uint32, 0, limit)
	for i := uint32(0); i < m.total && len(result) < limit; i++ {
		if !isSet(m.bits, i) {
			result = append(result, i)
		}
	}

	return result
}

// Helper functions for bit manipulation.
func isSet(bits []byte, n uint32) bool {
	return bits[n/bitsPerByte]&(1<<(n%bitsPerByte)) != 0
}

func setBit(bits []byte, n uint32) {
	bits[n/bitsPerByte] |= 1 << (n % bitsPerByte)
}

func clearBit(bits []byte, n uint32) {
	bits[n/bitsPerByte] &^= 1 << (n % bitsPerByte)
}
