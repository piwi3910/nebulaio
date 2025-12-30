package volume

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

// StorageTier represents a storage tier for tiered storage.
type StorageTier string

const (
	TierHot  StorageTier = "hot"
	TierWarm StorageTier = "warm"
	TierCold StorageTier = "cold"
)

// Device signature constants.
const (
	DeviceMagic         = "NEBULADEV"
	DeviceVersion       = uint32(1)
	DeviceSignatureSize = 512 // 512 bytes for device header
)

// Errors.
var (
	ErrInvalidTier         = errors.New("invalid storage tier")
	ErrDeviceNotFound      = errors.New("device not found")
	ErrDeviceHasFilesystem = errors.New("device has existing filesystem")
	ErrDeviceLocked        = errors.New("device is locked by another process")
	ErrInvalidSignature    = errors.New("invalid device signature")
	ErrTierNotAvailable    = errors.New("tier not available")
	ErrObjectNotInTier     = errors.New("object not found in specified tier")
)

// ParseTier parses a tier string into a StorageTier.
func ParseTier(s string) (StorageTier, error) {
	switch strings.ToLower(s) {
	case "hot":
		return TierHot, nil
	case "warm":
		return TierWarm, nil
	case "cold":
		return TierCold, nil
	default:
		return "", ErrInvalidTier
	}
}

// RawDeviceConfig holds configuration for raw block device access.
type RawDeviceConfig struct {
	Devices []DeviceConfig
	Safety  RawDeviceSafetyConfig
	Enabled bool
}

// RawDeviceSafetyConfig holds safety configuration for raw device access.
type RawDeviceSafetyConfig struct {
	// CheckFilesystem verifies device has no filesystem before use
	CheckFilesystem bool

	// RequireConfirmation requires explicit confirmation for new devices
	RequireConfirmation bool

	// WriteSignature writes NebulaIO signature to device header
	WriteSignature bool

	// ExclusiveLock uses flock() for exclusive access
	ExclusiveLock bool
}

// DeviceConfig holds configuration for a single device.
type DeviceConfig struct {
	// Path is the device path (e.g., /dev/nvme0n1)
	Path string

	// Tier is the storage tier for this device
	Tier StorageTier

	// Size is the size to use (0 = use entire device)
	Size uint64
}

// DefaultRawDeviceConfig returns the default raw device configuration.
func DefaultRawDeviceConfig() RawDeviceConfig {
	return RawDeviceConfig{
		Enabled: false,
		Safety: RawDeviceSafetyConfig{
			CheckFilesystem:     true,
			RequireConfirmation: true,
			WriteSignature:      true,
			ExclusiveLock:       true,
		},
		Devices: nil,
	}
}

// DeviceSignature is the header written to raw devices for identification.
type DeviceSignature struct {
	Magic      string
	DeviceSize uint64
	CreatedAt  int64
	Version    uint32
	Reserved   [404]byte
	DeviceID   [64]byte
	Tier       [16]byte
}

// NewDeviceSignature creates a new device signature.
func NewDeviceSignature(deviceID string, size uint64, tier StorageTier) *DeviceSignature {
	sig := &DeviceSignature{
		Magic:      DeviceMagic,
		Version:    DeviceVersion,
		DeviceSize: size,
		CreatedAt:  time.Now().Unix(),
	}
	copy(sig.DeviceID[:], deviceID)
	copy(sig.Tier[:], tier)

	return sig
}

// IsValid checks if the signature is valid.
func (s *DeviceSignature) IsValid() bool {
	return s.Magic == DeviceMagic && s.Version > 0 && s.CreatedAt > 0
}

// Serialize converts the signature to bytes.
func (s *DeviceSignature) Serialize() []byte {
	buf := make([]byte, DeviceSignatureSize)

	// Magic (9 bytes)
	copy(buf[0:9], s.Magic)

	// Version (4 bytes)
	binary.LittleEndian.PutUint32(buf[9:13], s.Version)

	// DeviceID (64 bytes)
	copy(buf[13:77], s.DeviceID[:])

	// DeviceSize (8 bytes)
	binary.LittleEndian.PutUint64(buf[77:85], s.DeviceSize)

	// Tier (16 bytes)
	copy(buf[85:101], s.Tier[:])

	// CreatedAt (8 bytes)
	//nolint:gosec // G115: CreatedAt is Unix timestamp, positive values fit in uint64
	binary.LittleEndian.PutUint64(buf[101:109], uint64(s.CreatedAt))

	// Reserved (403 bytes) - already zero

	return buf
}

// ParseDeviceSignature parses a device signature from bytes.
func ParseDeviceSignature(data []byte) (*DeviceSignature, error) {
	if len(data) < DeviceSignatureSize {
		return nil, ErrInvalidSignature
	}

	sig := &DeviceSignature{}

	// Magic
	sig.Magic = string(data[0:9])
	if sig.Magic != DeviceMagic {
		return nil, ErrInvalidSignature
	}

	// Version
	sig.Version = binary.LittleEndian.Uint32(data[9:13])

	// DeviceID
	copy(sig.DeviceID[:], data[13:77])

	// DeviceSize
	sig.DeviceSize = binary.LittleEndian.Uint64(data[77:85])

	// Tier
	copy(sig.Tier[:], data[85:101])

	// CreatedAt
	//nolint:gosec // G115: Unix timestamp fits in int64 range
	sig.CreatedAt = int64(binary.LittleEndian.Uint64(data[101:109]))

	return sig, nil
}

// RawDeviceVolume implements a volume on a raw block device.
type RawDeviceVolume struct {
	file        *os.File
	dio         *DirectIOFile
	signature   *DeviceSignature
	index       *Index
	allocator   *BlockAllocator
	path        string
	tier        StorageTier
	config      RawDeviceConfig
	objectCount int
	usedSpace   uint64
	totalSpace  uint64
	mu          sync.RWMutex
}

// RawDeviceVolumeStats contains stats for a raw device volume.
type RawDeviceVolumeStats struct {
	Tier        StorageTier
	ObjectCount int
	UsedSpace   uint64
	TotalSpace  uint64
	FreeSpace   uint64
}

// NewRawDeviceVolume creates or opens a raw device volume.
func NewRawDeviceVolume(path string, cfg RawDeviceConfig, tier StorageTier) (*RawDeviceVolume, error) {
	// Open the device/file
	flags := os.O_RDWR
	if cfg.Safety.ExclusiveLock {
		flags |= os.O_EXCL
	}

	//nolint:gosec // G304: path is validated by caller
	file, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open device: %w", err)
	}

	// Get device size
	fi, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to stat device: %w", err)
	}

	deviceSize := uint64(fi.Size()) //nolint:gosec // G115: file size is non-negative
	if deviceSize == 0 {
		// For block devices, try to get size differently
		deviceSize, err = getBlockDeviceSize(file)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("failed to get device size: %w", err)
		}
	}

	// Try exclusive lock if requested
	if cfg.Safety.ExclusiveLock {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err != nil {
			_ = file.Close()
			return nil, ErrDeviceLocked
		}
	}

	// Create DirectIO wrapper
	dioConfig := DirectIOConfig{
		Enabled:         true,
		BlockAlignment:  4096,
		UseMemoryPool:   true,
		FallbackOnError: true,
	}
	dio := NewDirectIOFile(file, dioConfig)

	vol := &RawDeviceVolume{
		path:       path,
		file:       file,
		dio:        dio,
		tier:       tier,
		config:     cfg,
		totalSpace: deviceSize,
	}

	// Check for existing signature or initialize
	err = vol.initializeOrLoad(cfg)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	return vol, nil
}

// initializeOrLoad initializes a new device or loads existing one.
func (v *RawDeviceVolume) initializeOrLoad(cfg RawDeviceConfig) error {
	// Try to read existing signature
	sigBuf := make([]byte, DeviceSignatureSize)

	_, err := v.file.ReadAt(sigBuf, 0)
	if err == nil {
		sig, err := ParseDeviceSignature(sigBuf)
		if err == nil && sig.IsValid() {
			// Valid signature found, load existing volume
			v.signature = sig
			return v.loadExisting()
		}
	}

	// No valid signature, initialize new device
	if cfg.Safety.WriteSignature {
		// Generate device ID from path
		deviceID := fmt.Sprintf("dev-%d", time.Now().UnixNano())
		v.signature = NewDeviceSignature(deviceID, v.totalSpace, v.tier)

		// Write signature
		sigData := v.signature.Serialize()

		_, err := v.file.WriteAt(sigData, 0)
		if err != nil {
			return fmt.Errorf("failed to write signature: %w", err)
		}
	}

	// Initialize volume structures
	return v.initializeNew()
}

// loadExisting loads an existing volume from device.
func (v *RawDeviceVolume) loadExisting() error {
	// For now, just initialize empty structures
	// In a full implementation, we'd read the index and allocator from disk
	v.index = NewIndex()
	v.allocator = NewBlockAllocator(v.totalSpace, BlockSize)

	return nil
}

// initializeNew initializes a new empty volume.
func (v *RawDeviceVolume) initializeNew() error {
	v.index = NewIndex()
	v.allocator = NewBlockAllocator(v.totalSpace, BlockSize)

	// Reserve first block for metadata
	v.allocator.Reserve(0)

	return nil
}

// Close closes the raw device volume.
func (v *RawDeviceVolume) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Unlock if locked
	if v.config.Safety.ExclusiveLock {
		_ = syscall.Flock(int(v.file.Fd()), syscall.LOCK_UN)
	}

	return v.file.Close()
}

// ReadSignature reads and returns the device signature.
func (v *RawDeviceVolume) ReadSignature() (*DeviceSignature, error) {
	if v.signature != nil {
		return v.signature, nil
	}

	sigBuf := make([]byte, DeviceSignatureSize)

	_, err := v.file.ReadAt(sigBuf, 0)
	if err != nil {
		return nil, err
	}

	return ParseDeviceSignature(sigBuf)
}

// Put stores an object in the volume.
func (v *RawDeviceVolume) Put(bucket, key string, data []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	fullKey := FullKey(bucket, key)
	keyHash := HashKey(bucket, key)

	// Delete existing if present
	if entry, found := v.index.Get(bucket, key); found {
		v.allocator.Free(entry.BlockNum)
		v.objectCount--
		v.usedSpace -= entry.Size
	}

	// Calculate required blocks
	dataSize := len(data)
	blocksNeeded := (dataSize + BlockSize - 1) / BlockSize

	// Allocate blocks
	blockNum, err := v.allocator.Allocate(blocksNeeded)
	if err != nil {
		return err
	}

	// Write data using direct I/O
	offset := int64(blockNum) * int64(BlockSize)

	// Align data to block boundary
	alignedSize := ((dataSize + 4095) / 4096) * 4096
	alignedBuf := AllocateAligned(alignedSize, 4096)
	copy(alignedBuf, data)

	_, err = v.dio.WriteAt(alignedBuf, offset)
	if err != nil {
		v.allocator.Free(blockNum)
		return err
	}

	// Update index - use IndexEntryFull which embeds IndexEntry
	entry := IndexEntryFull{
		IndexEntry: IndexEntry{
			KeyHash:       keyHash,
			BlockNum:      blockNum,
			BlockCount:    uint16(blocksNeeded), //nolint:gosec // G115: blocksNeeded bounded by max object size
			OffsetInBlock: 0,
			Size:          uint64(dataSize),
			Created:       time.Now().Unix(),
			Flags:         0,
			KeyLen:        uint16(len(fullKey)), //nolint:gosec // G115: key length bounded by S3 limits
		},
		Key: fullKey,
	}
	v.index.Put(entry)

	v.objectCount++
	v.usedSpace += uint64(dataSize)

	return nil
}

// Get retrieves an object from the volume.
func (v *RawDeviceVolume) Get(bucket, key string) ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	entry, found := v.index.Get(bucket, key)
	if !found {
		return nil, ErrObjectNotFound
	}

	// Read data
	offset := int64(entry.BlockNum) * int64(BlockSize)
	//nolint:gosec // G115: entry.Size is bounded by max object size
	alignedSize := ((int(entry.Size) + 4095) / 4096) * 4096
	buf := AllocateAligned(alignedSize, 4096)

	_, err := v.dio.ReadAt(buf, offset)
	if err != nil {
		return nil, err
	}

	// Return only the actual data (not padding)
	return buf[:entry.Size], nil
}

// Delete removes an object from the volume.
func (v *RawDeviceVolume) Delete(bucket, key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	entry, found := v.index.Get(bucket, key)
	if !found {
		return ErrObjectNotFound
	}

	// Free blocks
	v.allocator.Free(entry.BlockNum)

	// Remove from index
	v.index.Remove(bucket, key)

	v.objectCount--
	v.usedSpace -= entry.Size

	return nil
}

// FreeSpace returns the free space in bytes.
func (v *RawDeviceVolume) FreeSpace() uint64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return v.totalSpace - v.usedSpace
}

// Stats returns volume statistics.
func (v *RawDeviceVolume) Stats() RawDeviceVolumeStats {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return RawDeviceVolumeStats{
		Tier:        v.tier,
		ObjectCount: v.objectCount,
		UsedSpace:   v.usedSpace,
		TotalSpace:  v.totalSpace,
		FreeSpace:   v.totalSpace - v.usedSpace,
	}
}

// TieredDeviceManagerConfig holds configuration for tiered device manager.
type TieredDeviceManagerConfig struct {
	RawDevices RawDeviceConfig
}

// TieredDeviceManager manages volumes across storage tiers.
type TieredDeviceManager struct {
	volumes     map[StorageTier]*RawDeviceVolume
	objectIndex map[string]StorageTier // fullKey -> tier
	mu          sync.RWMutex
}

// NewTieredDeviceManager creates a new tiered device manager.
func NewTieredDeviceManager(cfg TieredDeviceManagerConfig) (*TieredDeviceManager, error) {
	mgr := &TieredDeviceManager{
		volumes:     make(map[StorageTier]*RawDeviceVolume),
		objectIndex: make(map[string]StorageTier),
	}

	// Initialize devices for each tier
	for _, devCfg := range cfg.RawDevices.Devices {
		vol, err := NewRawDeviceVolume(devCfg.Path, cfg.RawDevices, devCfg.Tier)
		if err != nil {
			// Close already opened volumes
			_ = mgr.Close()
			return nil, fmt.Errorf("failed to open device %s: %w", devCfg.Path, err)
		}

		mgr.volumes[devCfg.Tier] = vol
	}

	return mgr, nil
}

// Close closes all volumes.
func (m *TieredDeviceManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error

	for _, vol := range m.volumes {
		err := vol.Close()
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// HasTier returns true if the tier is available.
func (m *TieredDeviceManager) HasTier(tier StorageTier) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.volumes[tier]

	return ok
}

// Put stores an object in the specified tier.
func (m *TieredDeviceManager) Put(bucket, key string, data []byte, tier StorageTier) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	vol, ok := m.volumes[tier]
	if !ok {
		return ErrTierNotAvailable
	}

	err := vol.Put(bucket, key, data)
	if err != nil {
		return err
	}

	m.objectIndex[FullKey(bucket, key)] = tier

	return nil
}

// Get retrieves an object from any tier.
func (m *TieredDeviceManager) Get(bucket, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := FullKey(bucket, key)

	tier, ok := m.objectIndex[fullKey]
	if !ok {
		return nil, ErrObjectNotFound
	}

	vol, ok := m.volumes[tier]
	if !ok {
		return nil, ErrTierNotAvailable
	}

	return vol.Get(bucket, key)
}

// GetObjectTier returns the tier where an object is stored.
func (m *TieredDeviceManager) GetObjectTier(bucket, key string) (StorageTier, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := FullKey(bucket, key)

	tier, ok := m.objectIndex[fullKey]
	if !ok {
		return "", ErrObjectNotFound
	}

	return tier, nil
}

// MigrateObject moves an object from one tier to another.
func (m *TieredDeviceManager) MigrateObject(bucket, key string, targetTier StorageTier) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := FullKey(bucket, key)

	// Find current tier
	currentTier, ok := m.objectIndex[fullKey]
	if !ok {
		return ErrObjectNotFound
	}

	if currentTier == targetTier {
		return nil // Already in target tier
	}

	// Get current volume
	sourceVol, ok := m.volumes[currentTier]
	if !ok {
		return ErrTierNotAvailable
	}

	// Get target volume
	targetVol, ok := m.volumes[targetTier]
	if !ok {
		return ErrTierNotAvailable
	}

	// Read data from source
	data, err := sourceVol.Get(bucket, key)
	if err != nil {
		return err
	}

	// Write to target
	err = targetVol.Put(bucket, key, data)
	if err != nil {
		return err
	}

	// Delete from source
	err = sourceVol.Delete(bucket, key)
	if err != nil {
		// Rollback: delete from target
		_ = targetVol.Delete(bucket, key)
		return err
	}

	// Update index
	m.objectIndex[fullKey] = targetTier

	return nil
}

// Delete removes an object from its tier.
func (m *TieredDeviceManager) Delete(bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := FullKey(bucket, key)

	tier, ok := m.objectIndex[fullKey]
	if !ok {
		return ErrObjectNotFound
	}

	vol, ok := m.volumes[tier]
	if !ok {
		return ErrTierNotAvailable
	}

	err := vol.Delete(bucket, key)
	if err != nil {
		return err
	}

	delete(m.objectIndex, fullKey)

	return nil
}

// Stats returns statistics for all tiers.
func (m *TieredDeviceManager) Stats() map[StorageTier]RawDeviceVolumeStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[StorageTier]RawDeviceVolumeStats)
	for tier, vol := range m.volumes {
		stats[tier] = vol.Stats()
	}

	return stats
}

// Helper to get block device size.
func getBlockDeviceSize(file *os.File) (uint64, error) {
	// For regular files, use stat
	fi, err := file.Stat()
	if err != nil {
		return 0, err
	}

	if fi.Mode().IsRegular() {
		return uint64(fi.Size()), nil //nolint:gosec // G115: file size is non-negative
	}

	// For block devices on Linux, use ioctl
	// This is platform-specific and simplified here
	return 0, errors.New("cannot determine block device size")
}

// BlockAllocator manages block allocation for raw devices.
type BlockAllocator struct {
	bitmap      []uint64
	mu          sync.Mutex
	totalBlocks uint32
	blockSize   uint32
	freeBlocks  uint32
}

// NewBlockAllocator creates a new block allocator.
func NewBlockAllocator(deviceSize uint64, blockSize uint32) *BlockAllocator {
	//nolint:gosec // G115: totalBlocks is bounded by device size / block size
	totalBlocks := uint32(deviceSize / uint64(blockSize))
	bitmapSize := (totalBlocks + 63) / 64

	return &BlockAllocator{
		totalBlocks: totalBlocks,
		blockSize:   blockSize,
		bitmap:      make([]uint64, bitmapSize),
		freeBlocks:  totalBlocks,
	}
}

// Allocate allocates n contiguous blocks.
func (a *BlockAllocator) Allocate(n int) (uint32, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Simple first-fit allocation
	consecutive := 0
	startBlock := uint32(0)

	for block := range a.totalBlocks {
		if a.isBlockFree(block) {
			if consecutive == 0 {
				startBlock = block
			}

			consecutive++
			if consecutive >= n {
				// Found enough blocks, mark them allocated
				//nolint:gosec // G115: n is bounded by device capacity
				for i := range uint32(n) {
					a.setBlockUsed(startBlock + i)
				}

				//nolint:gosec // G115: n is bounded by device capacity
				a.freeBlocks -= uint32(n)

				return startBlock, nil
			}
		} else {
			consecutive = 0
		}
	}

	return 0, ErrVolumeFull
}

// Free frees a block.
func (a *BlockAllocator) Free(blockNum uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.isBlockFree(blockNum) {
		a.setBlockFree(blockNum)
		a.freeBlocks++
	}
}

// Reserve marks a block as reserved (used for metadata).
func (a *BlockAllocator) Reserve(blockNum uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.isBlockFree(blockNum) {
		a.setBlockUsed(blockNum)
		a.freeBlocks--
	}
}

func (a *BlockAllocator) isBlockFree(block uint32) bool {
	idx := block / 64
	bit := block % 64

	return (a.bitmap[idx] & (1 << bit)) == 0
}

func (a *BlockAllocator) setBlockUsed(block uint32) {
	idx := block / 64
	bit := block % 64
	a.bitmap[idx] |= (1 << bit)
}

func (a *BlockAllocator) setBlockFree(block uint32) {
	idx := block / 64
	bit := block % 64
	a.bitmap[idx] &^= (1 << bit)
}
