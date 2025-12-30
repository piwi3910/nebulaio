package volume

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"
)

// DirectIO errors.
var (
	ErrNotAligned           = errors.New("buffer not aligned for direct I/O")
	ErrOffsetNotAligned     = errors.New("offset not aligned for direct I/O")
	ErrDirectIONotSupported = errors.New("direct I/O not supported on this platform")
)

// DirectIOConfig configures direct I/O behavior.
type DirectIOConfig struct {
	BlockAlignment  int  `json:"blockAlignment" yaml:"blockAlignment"`
	PoolBlockSize   int  `json:"poolBlockSize" yaml:"poolBlockSize"`
	Enabled         bool `json:"enabled" yaml:"enabled"`
	UseMemoryPool   bool `json:"useMemoryPool" yaml:"useMemoryPool"`
	FallbackOnError bool `json:"fallbackOnError" yaml:"fallbackOnError"`
}

// DefaultDirectIOConfig returns the default direct I/O configuration.
func DefaultDirectIOConfig() DirectIOConfig {
	return DirectIOConfig{
		Enabled:         true,
		BlockAlignment:  4096, // Standard page size
		UseMemoryPool:   true,
		PoolBlockSize:   BlockSize, // 4MB
		FallbackOnError: true,
	}
}

// DirectIOFile wraps a file with direct I/O capabilities.
type DirectIOFile struct {
	file           *os.File
	pool           *AlignedBufferPool
	config         DirectIOConfig
	directReads    uint64
	directWrites   uint64
	bufferedReads  uint64
	bufferedWrites uint64
	mu             sync.RWMutex
}

// DirectIOStats contains direct I/O statistics.
type DirectIOStats struct {
	DirectReads     uint64 `json:"directReads"`
	DirectWrites    uint64 `json:"directWrites"`
	BufferedReads   uint64 `json:"bufferedReads"`
	BufferedWrites  uint64 `json:"bufferedWrites"`
	DirectIOEnabled bool   `json:"directIOEnabled"`
}

// AlignedBufferPool provides a pool of aligned buffers for direct I/O.
type AlignedBufferPool struct {
	pool      sync.Pool
	alignment int
	blockSize int
}

// NewAlignedBufferPool creates a new aligned buffer pool.
func NewAlignedBufferPool(alignment, blockSize int) *AlignedBufferPool {
	return &AlignedBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return AllocateAligned(blockSize, alignment)
			},
		},
		alignment: alignment,
		blockSize: blockSize,
	}
}

// Get retrieves an aligned buffer from the pool.
func (p *AlignedBufferPool) Get() []byte {
	return p.pool.Get().([]byte)
}

// Put returns an aligned buffer to the pool.
func (p *AlignedBufferPool) Put(buf []byte) {
	// Only return buffers of the correct size
	if len(buf) == p.blockSize {
		// Clear the buffer before returning
		for i := range buf {
			buf[i] = 0
		}
		//nolint:staticcheck // SA6002: slices are pointer-like and safe to pass directly
		p.pool.Put(buf)
	}
}

// AllocateAligned allocates a buffer aligned to the specified boundary
// This is critical for O_DIRECT which requires aligned memory.
func AllocateAligned(size, alignment int) []byte {
	// Allocate extra space for alignment
	buf := make([]byte, size+alignment)

	// Find the aligned offset
	//nolint:gosec // G103: unsafe.Pointer required for memory alignment in O_DIRECT
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	alignedPtr := (ptr + uintptr(alignment-1)) &^ uintptr(alignment-1)
	offset := alignedPtr - ptr

	// Return slice starting at aligned boundary
	return buf[offset : offset+uintptr(size)]
}

// IsAligned checks if a buffer is properly aligned for direct I/O.
func IsAligned(buf []byte, alignment int) bool {
	if len(buf) == 0 {
		return true
	}

	//nolint:gosec // G103: unsafe.Pointer required for alignment check
	ptr := uintptr(unsafe.Pointer(&buf[0]))

	return ptr%uintptr(alignment) == 0
}

// IsOffsetAligned checks if an offset is properly aligned.
func IsOffsetAligned(offset int64, alignment int) bool {
	return offset%int64(alignment) == 0
}

// NewDirectIOFile wraps a file with direct I/O support.
func NewDirectIOFile(file *os.File, config DirectIOConfig) *DirectIOFile {
	dio := &DirectIOFile{
		file:   file,
		config: config,
	}

	if config.UseMemoryPool && config.PoolBlockSize > 0 {
		dio.pool = NewAlignedBufferPool(config.BlockAlignment, config.PoolBlockSize)
	}

	return dio
}

// File returns the underlying os.File.
func (d *DirectIOFile) File() *os.File {
	return d.file
}

// GetAlignedBuffer gets an aligned buffer from the pool or allocates one.
func (d *DirectIOFile) GetAlignedBuffer(size int) []byte {
	if d.pool != nil && size == d.config.PoolBlockSize {
		return d.pool.Get()
	}

	return AllocateAligned(size, d.config.BlockAlignment)
}

// PutAlignedBuffer returns a buffer to the pool.
func (d *DirectIOFile) PutAlignedBuffer(buf []byte) {
	if d.pool != nil && len(buf) == d.config.PoolBlockSize {
		d.pool.Put(buf)
	}
}

// ReadAt reads from the file at the specified offset
// Uses direct I/O if enabled and requirements are met.
func (d *DirectIOFile) ReadAt(buf []byte, offset int64) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Check if we can use direct I/O
	if d.config.Enabled && directIOSupported() {
		// Check alignment requirements
		if IsAligned(buf, d.config.BlockAlignment) &&
			IsOffsetAligned(offset, d.config.BlockAlignment) &&
			len(buf)%d.config.BlockAlignment == 0 {
			n, err := d.file.ReadAt(buf, offset)
			if err == nil {
				d.directReads++
				return n, nil
			}
			// Fall through to buffered I/O if fallback is enabled
			if !d.config.FallbackOnError {
				return n, err
			}
		}
	}

	// Use buffered I/O
	d.bufferedReads++

	return d.file.ReadAt(buf, offset)
}

// WriteAt writes to the file at the specified offset
// Uses direct I/O if enabled and requirements are met.
func (d *DirectIOFile) WriteAt(buf []byte, offset int64) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Check if we can use direct I/O
	if d.config.Enabled && directIOSupported() {
		// Check alignment requirements
		if IsAligned(buf, d.config.BlockAlignment) &&
			IsOffsetAligned(offset, d.config.BlockAlignment) &&
			len(buf)%d.config.BlockAlignment == 0 {
			n, err := d.file.WriteAt(buf, offset)
			if err == nil {
				d.directWrites++
				return n, nil
			}
			// Fall through to buffered I/O if fallback is enabled
			if !d.config.FallbackOnError {
				return n, err
			}
		}
	}

	// Use buffered I/O
	d.bufferedWrites++

	return d.file.WriteAt(buf, offset)
}

// ReadAtAligned reads using an aligned buffer (for direct I/O)
// This method handles the alignment requirements internally.
func (d *DirectIOFile) ReadAtAligned(size int, offset int64) ([]byte, error) {
	// Calculate aligned offset and size
	alignment := int64(d.config.BlockAlignment)
	alignedOffset := offset &^ (alignment - 1)
	startPad := offset - alignedOffset

	// Round up size to include padding and alignment
	totalSize := int(startPad) + size
	alignedSize := (totalSize + int(alignment) - 1) &^ (int(alignment) - 1)

	// Get aligned buffer
	buf := d.GetAlignedBuffer(alignedSize)

	// Read aligned data
	n, err := d.ReadAt(buf[:alignedSize], alignedOffset)
	if err != nil {
		d.PutAlignedBuffer(buf)
		return nil, err
	}

	// Extract the actual data we need
	if int(startPad)+size > n {
		d.PutAlignedBuffer(buf)
		return nil, fmt.Errorf("short read: got %d bytes, need %d", n, int(startPad)+size)
	}

	// Copy to result (we can't return a slice of the pooled buffer)
	result := make([]byte, size)
	copy(result, buf[startPad:startPad+int64(size)])
	d.PutAlignedBuffer(buf)

	return result, nil
}

// WriteAtAligned writes using aligned buffers for direct I/O
// This method handles the alignment requirements internally.
func (d *DirectIOFile) WriteAtAligned(data []byte, offset int64) error {
	// Calculate aligned offset and size
	alignment := int64(d.config.BlockAlignment)
	alignedOffset := offset &^ (alignment - 1)
	startPad := int(offset - alignedOffset)

	// Round up size to include padding and alignment
	totalSize := startPad + len(data)
	alignedSize := (totalSize + int(alignment) - 1) &^ (int(alignment) - 1)

	// Get aligned buffer
	buf := d.GetAlignedBuffer(alignedSize)
	defer d.PutAlignedBuffer(buf)

	// If we need to preserve existing data around the write
	if startPad > 0 || alignedSize > len(data) {
		// Read existing aligned block first
		_, err := d.file.ReadAt(buf[:alignedSize], alignedOffset)
		if err != nil {
			// If read fails, zero the buffer (new data)
			for i := range buf[:alignedSize] {
				buf[i] = 0
			}
		}
	}

	// Copy data to aligned buffer
	copy(buf[startPad:], data)

	// Write aligned data
	_, err := d.WriteAt(buf[:alignedSize], alignedOffset)

	return err
}

// Sync flushes file data to disk.
func (d *DirectIOFile) Sync() error {
	return d.file.Sync()
}

// Close closes the file.
func (d *DirectIOFile) Close() error {
	return d.file.Close()
}

// Stats returns direct I/O statistics.
func (d *DirectIOFile) Stats() DirectIOStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return DirectIOStats{
		DirectReads:     d.directReads,
		DirectWrites:    d.directWrites,
		BufferedReads:   d.bufferedReads,
		BufferedWrites:  d.bufferedWrites,
		DirectIOEnabled: d.config.Enabled && directIOSupported(),
	}
}
