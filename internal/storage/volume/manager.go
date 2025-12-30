package volume

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// Manager manages multiple volumes and routes object operations.
type Manager struct {
	volumes       map[string]*Volume
	objectIndex   map[string]string
	dataDir       string
	volumeList    []*Volume
	directIO      DirectIOConfig
	maxVolumeSize uint64
	mu            sync.RWMutex
	autoCreate    bool
}

// ManagerConfig holds configuration for the volume manager.
type ManagerConfig struct {
	DataDir       string         // Directory for volume files
	MaxVolumeSize uint64         // Maximum size of each volume (default: 32GB)
	AutoCreate    bool           // Automatically create new volumes when needed
	DirectIO      DirectIOConfig // Direct I/O configuration
}

// DefaultManagerConfig returns the default manager configuration.
func DefaultManagerConfig(dataDir string) ManagerConfig {
	return ManagerConfig{
		DataDir:       dataDir,
		MaxVolumeSize: DefaultVolumeSize,
		AutoCreate:    true,
		DirectIO:      DefaultDirectIOConfig(),
	}
}

// NewManager creates a new volume manager.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	// Ensure data directory exists
	volumeDir := filepath.Join(cfg.DataDir, "volumes")
	err := os.MkdirAll(volumeDir, 0750)
	if err != nil {
		return nil, fmt.Errorf("failed to create volume directory: %w", err)
	}

	m := &Manager{
		dataDir:       volumeDir,
		volumes:       make(map[string]*Volume),
		volumeList:    make([]*Volume, 0),
		objectIndex:   make(map[string]string),
		maxVolumeSize: cfg.MaxVolumeSize,
		autoCreate:    cfg.AutoCreate,
		directIO:      cfg.DirectIO,
	}

	// Load existing volumes
	err = m.loadExistingVolumes()
	if err != nil {
		return nil, err
	}

	log.Info().
		Str("data_dir", volumeDir).
		Int("volumes", len(m.volumes)).
		Msg("Volume manager initialized")

	return m, nil
}

// loadExistingVolumes loads all existing volume files.
func (m *Manager) loadExistingVolumes() error {
	entries, err := os.ReadDir(m.dataDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".neb") {
			continue
		}

		path := filepath.Join(m.dataDir, entry.Name())

		vol, err := OpenVolumeWithConfig(path, m.directIO)
		if err != nil {
			log.Warn().
				Str("path", path).
				Err(err).
				Msg("Failed to open volume, skipping")

			continue
		}

		m.volumes[vol.ID()] = vol
		m.volumeList = append(m.volumeList, vol)

		// Build object index from volume
		for _, entry := range vol.index.All() {
			m.objectIndex[entry.Key] = vol.ID()
		}
	}

	// Sort by free space (most free first)
	m.sortVolumesByFreeSpace()

	return nil
}

// sortVolumesByFreeSpace sorts volumes with most free space first.
func (m *Manager) sortVolumesByFreeSpace() {
	sort.Slice(m.volumeList, func(i, j int) bool {
		return m.volumeList[i].FreeSpace() > m.volumeList[j].FreeSpace()
	})
}

// Close closes all volumes.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error

	for _, vol := range m.volumes {
		err := vol.Close()
		if err != nil {
			lastErr = err
			log.Error().
				Str("volume_id", vol.ID()).
				Err(err).
				Msg("Failed to close volume")
		}
	}

	m.volumes = make(map[string]*Volume)
	m.volumeList = nil
	m.objectIndex = make(map[string]string)

	return lastErr
}

// CreateVolume creates a new volume.
func (m *Manager) CreateVolume() (*Volume, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.createVolumeLocked()
}

// createVolumeLocked creates a new volume (caller must hold lock).
func (m *Manager) createVolumeLocked() (*Volume, error) {
	// Generate unique filename
	filename := fmt.Sprintf("vol-%04d.neb", len(m.volumes)+1)
	path := filepath.Join(m.dataDir, filename)

	cfg := VolumeConfig{
		Size:      m.maxVolumeSize,
		BlockSize: BlockSize,
		DirectIO:  m.directIO,
	}

	vol, err := CreateVolume(path, cfg)
	if err != nil {
		return nil, err
	}

	m.volumes[vol.ID()] = vol
	m.volumeList = append(m.volumeList, vol)
	m.sortVolumesByFreeSpace()

	log.Info().
		Str("volume_id", vol.ID()).
		Str("path", path).
		Msg("Created new volume")

	return vol, nil
}

// selectVolumeForWrite selects a volume for writing an object of given size
// NOTE: This is called with lock already held.
func (m *Manager) selectVolumeForWrite(size int64) (*Volume, error) {
	// First, try to find an existing volume with enough space
	for _, vol := range m.volumeList {
		if vol.HasSpace(size) {
			return vol, nil
		}
	}

	// No suitable volume, create a new one if auto-create is enabled
	if m.autoCreate {
		return m.createVolumeLocked()
	}

	return nil, ErrVolumeFull
}

// findObjectVolume finds which volume contains an object.
func (m *Manager) findObjectVolume(bucket, key string) *Volume {
	fullKey := FullKey(bucket, key)

	volumeID, exists := m.objectIndex[fullKey]
	if !exists {
		return nil
	}

	return m.volumes[volumeID]
}

// Put stores an object.
func (m *Manager) Put(bucket, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := FullKey(bucket, key)

	// Check if object already exists in another volume
	if existingVolID, exists := m.objectIndex[fullKey]; exists {
		if vol := m.volumes[existingVolID]; vol != nil {
			// Delete from existing volume
			err := vol.Delete(bucket, key)
			if err != nil && err != ErrObjectNotFound {
				return err
			}
		}

		delete(m.objectIndex, fullKey)
	}

	// Select volume for write
	vol, err := m.selectVolumeForWrite(int64(len(data)))
	if err != nil {
		return err
	}

	// Write to volume
	if err := vol.Put(bucket, key, newBytesReader(data), int64(len(data))); err != nil {
		return err
	}

	// Update index
	m.objectIndex[fullKey] = vol.ID()

	// Re-sort volumes by free space
	m.sortVolumesByFreeSpace()

	return nil
}

// Get retrieves an object.
func (m *Manager) Get(bucket, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	vol := m.findObjectVolume(bucket, key)
	if vol == nil {
		return nil, ErrObjectNotFound
	}

	return vol.Get(bucket, key)
}

// Delete removes an object.
func (m *Manager) Delete(bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := FullKey(bucket, key)

	vol := m.findObjectVolume(bucket, key)
	if vol == nil {
		return ErrObjectNotFound
	}

	err := vol.Delete(bucket, key)
	if err != nil {
		return err
	}

	delete(m.objectIndex, fullKey)

	return nil
}

// Exists checks if an object exists.
func (m *Manager) Exists(bucket, key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	vol := m.findObjectVolume(bucket, key)
	if vol == nil {
		return false
	}

	return vol.Exists(bucket, key)
}

// List returns objects matching a prefix.
func (m *Manager) List(bucket, prefix string, maxKeys int) ([]ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ObjectInfo, 0)

	// Collect from all volumes
	for _, vol := range m.volumeList {
		objects, err := vol.List(bucket, prefix, maxKeys-len(result))
		if err != nil {
			continue
		}

		result = append(result, objects...)
		if len(result) >= maxKeys {
			break
		}
	}

	// Sort by key
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})

	if len(result) > maxKeys {
		result = result[:maxKeys]
	}

	return result, nil
}

// Sync flushes all pending writes to disk.
func (m *Manager) Sync() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var lastErr error

	for _, vol := range m.volumes {
		err := vol.Sync()
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// Stats returns manager statistics.
func (m *Manager) Stats() ManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := ManagerStats{
		VolumeCount: len(m.volumes),
		ObjectCount: len(m.objectIndex),
		Volumes:     make([]VolumeStats, 0, len(m.volumeList)),
	}

	for _, vol := range m.volumeList {
		volStats := vol.Stats()
		stats.TotalSize += volStats.TotalSize
		stats.UsedSize += volStats.UsedSize
		stats.FreeSize += volStats.TotalSize - volStats.UsedSize
		stats.Volumes = append(stats.Volumes, volStats)
	}

	return stats
}

// ManagerStats contains manager statistics.
type ManagerStats struct {
	Volumes     []VolumeStats
	VolumeCount int
	ObjectCount int
	TotalSize   uint64
	UsedSize    uint64
	FreeSize    uint64
}

// VolumeCount returns the number of volumes.
func (m *Manager) VolumeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.volumes)
}

// Volumes returns a list of all volumes (for admin operations).
func (m *Manager) Volumes() []*Volume {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Volume, len(m.volumeList))
	copy(result, m.volumeList)

	return result
}

// Helper: bytes.Reader wrapper for io.Reader interface.
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data}
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n = copy(p, r.data[r.pos:])
	r.pos += n

	return n, nil
}
