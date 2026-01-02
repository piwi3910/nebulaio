package volume

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateVolume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.neb")

	// Create volume with small size for testing
	cfg := VolumeConfig{
		Size:      64 * 1024 * 1024, // 64MB for testing
		BlockSize: BlockSize,
	}

	vol, err := CreateVolume(path, cfg)
	require.NoError(t, err)

	require.NotNil(t, vol)
	defer vol.Close()

	// Verify file exists
	stat, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(cfg.Size), stat.Size())

	// Verify superblock
	assert.NotEmpty(t, vol.ID())
	stats := vol.Stats()
	assert.Equal(t, cfg.Size, stats.TotalSize)
	assert.Positive(t, stats.FreeBlocks)
}

func TestOpenVolume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.neb")

	// Create volume
	cfg := VolumeConfig{Size: 64 * 1024 * 1024}
	vol, err := CreateVolume(path, cfg)
	require.NoError(t, err)

	volumeID := vol.ID()
	vol.Close()

	// Reopen volume
	vol2, err := OpenVolume(path)
	require.NoError(t, err)

	defer vol2.Close()

	assert.Equal(t, volumeID, vol2.ID())
}

func TestPutGetObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.neb")

	cfg := VolumeConfig{Size: 64 * 1024 * 1024}
	vol, err := CreateVolume(path, cfg)
	require.NoError(t, err)

	defer vol.Close()

	// Test tiny object
	t.Run("TinyObject", func(t *testing.T) {
		data := []byte("Hello, World!")
		err := vol.Put("bucket1", "tiny-key", bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		retrieved, err := vol.Get("bucket1", "tiny-key")
		require.NoError(t, err)
		assert.Equal(t, data, retrieved)
	})

	// Test small object (< 64KB)
	t.Run("SmallObject", func(t *testing.T) {
		data := make([]byte, 32*1024) // 32KB
		rand.Read(data)

		err := vol.Put("bucket1", "small-key", bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		retrieved, err := vol.Get("bucket1", "small-key")
		require.NoError(t, err)
		assert.Equal(t, data, retrieved)
	})

	// Test medium object (< 256KB)
	t.Run("MediumObject", func(t *testing.T) {
		data := make([]byte, 128*1024) // 128KB
		rand.Read(data)

		err := vol.Put("bucket1", "medium-key", bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		retrieved, err := vol.Get("bucket1", "medium-key")
		require.NoError(t, err)
		assert.Equal(t, data, retrieved)
	})

	// Test large object (> 1MB, < 4MB)
	t.Run("LargeObject", func(t *testing.T) {
		data := make([]byte, 2*1024*1024) // 2MB
		rand.Read(data)

		err := vol.Put("bucket1", "large-key", bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		retrieved, err := vol.Get("bucket1", "large-key")
		require.NoError(t, err)
		assert.Equal(t, data, retrieved)
	})
}

func TestDeleteObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.neb")

	cfg := VolumeConfig{Size: 64 * 1024 * 1024}
	vol, err := CreateVolume(path, cfg)
	require.NoError(t, err)

	defer vol.Close()

	// Store object
	data := []byte("Delete me!")
	err = vol.Put("bucket1", "delete-key", bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	// Verify it exists
	assert.True(t, vol.Exists("bucket1", "delete-key"))

	// Delete it
	err = vol.Delete("bucket1", "delete-key")
	require.NoError(t, err)

	// Verify it's gone
	assert.False(t, vol.Exists("bucket1", "delete-key"))

	// Get should return error
	_, err = vol.Get("bucket1", "delete-key")
	assert.ErrorIs(t, err, ErrObjectNotFound)
}

func TestListObjects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.neb")

	cfg := VolumeConfig{Size: 64 * 1024 * 1024}
	vol, err := CreateVolume(path, cfg)
	require.NoError(t, err)

	defer vol.Close()

	// Store multiple objects
	for i := range 10 {
		key := fmt.Sprintf("prefix/key-%d", i)
		data := fmt.Appendf(nil, "data-%d", i)
		err := vol.Put("bucket1", key, bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
	}

	// List with prefix
	objects, err := vol.List("bucket1", "prefix/", 100)
	require.NoError(t, err)
	assert.Len(t, objects, 10)

	// List with limit
	objects, err = vol.List("bucket1", "prefix/", 5)
	require.NoError(t, err)
	assert.Len(t, objects, 5)
}

func TestUpdateObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.neb")

	cfg := VolumeConfig{Size: 64 * 1024 * 1024}
	vol, err := CreateVolume(path, cfg)
	require.NoError(t, err)

	defer vol.Close()

	// Store object
	data1 := []byte("Original data")
	err = vol.Put("bucket1", "update-key", bytes.NewReader(data1), int64(len(data1)))
	require.NoError(t, err)

	// Update object
	data2 := []byte("Updated data - now longer!")
	err = vol.Put("bucket1", "update-key", bytes.NewReader(data2), int64(len(data2)))
	require.NoError(t, err)

	// Verify updated content
	retrieved, err := vol.Get("bucket1", "update-key")
	require.NoError(t, err)
	assert.Equal(t, data2, retrieved)

	// Object count should still be 1
	assert.Equal(t, uint64(1), vol.ObjectCount())
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.neb")

	// Create and populate volume
	cfg := VolumeConfig{Size: 64 * 1024 * 1024}
	vol, err := CreateVolume(path, cfg)
	require.NoError(t, err)

	testData := make(map[string][]byte)

	for i := range 5 {
		key := fmt.Sprintf("persist-key-%d", i)
		data := make([]byte, 1024+i*100)
		rand.Read(data)
		testData[key] = data

		err := vol.Put("bucket1", key, bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
	}

	// Sync and close
	require.NoError(t, vol.Sync())
	require.NoError(t, vol.Close())

	// Reopen and verify
	vol2, err := OpenVolume(path)
	require.NoError(t, err)

	defer vol2.Close()

	for key, expectedData := range testData {
		retrieved, err := vol2.Get("bucket1", key)
		require.NoError(t, err, "Failed to get key: %s", key)
		assert.Equal(t, expectedData, retrieved, "Data mismatch for key: %s", key)
	}
}

func TestManagerBasic(t *testing.T) {
	dir := t.TempDir()

	cfg := ManagerConfig{
		DataDir:       dir,
		MaxVolumeSize: 64 * 1024 * 1024,
		AutoCreate:    true,
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)

	defer mgr.Close()

	// Store object
	data := []byte("Manager test data")
	err = mgr.Put("bucket1", "mgr-key", data)
	require.NoError(t, err)

	// Retrieve
	retrieved, err := mgr.Get("bucket1", "mgr-key")
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)

	// Delete
	err = mgr.Delete("bucket1", "mgr-key")
	require.NoError(t, err)

	assert.False(t, mgr.Exists("bucket1", "mgr-key"))
}

func TestBackendInterface(t *testing.T) {
	dir := t.TempDir()

	backend, err := NewBackend(dir)
	require.NoError(t, err)

	defer backend.Close()

	ctx := context.Background()

	// Init
	err = backend.Init(ctx)
	require.NoError(t, err)

	// Create bucket
	err = backend.CreateBucket(ctx, "test-bucket")
	require.NoError(t, err)

	// Put object
	data := []byte("Backend interface test")
	result, err := backend.PutObject(ctx, "test-bucket", "test-key", bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.NotEmpty(t, result.ETag)
	assert.Equal(t, int64(len(data)), result.Size)

	// Get object
	reader, err := backend.GetObject(ctx, "test-bucket", "test-key")
	require.NoError(t, err)

	defer reader.Close()

	retrieved := make([]byte, len(data))
	n, _ := reader.Read(retrieved)
	assert.Equal(t, data, retrieved[:n])

	// Object exists
	exists, err := backend.ObjectExists(ctx, "test-bucket", "test-key")
	require.NoError(t, err)
	assert.True(t, exists)

	// Delete object
	err = backend.DeleteObject(ctx, "test-bucket", "test-key")
	require.NoError(t, err)

	// Verify deleted
	exists, err = backend.ObjectExists(ctx, "test-bucket", "test-key")
	require.NoError(t, err)
	assert.False(t, exists)

	// Storage info
	info, err := backend.GetStorageInfo(ctx)
	require.NoError(t, err)
	assert.Positive(t, info.TotalBytes)
}

func TestPackedBlockPacking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.neb")

	cfg := VolumeConfig{Size: 64 * 1024 * 1024}
	vol, err := CreateVolume(path, cfg)
	require.NoError(t, err)

	defer vol.Close()

	// Store many small objects that should pack into same block
	for i := range 100 {
		key := fmt.Sprintf("tiny-%d", i)
		data := fmt.Appendf(nil, "tiny-data-%d", i)
		err := vol.Put("bucket1", key, bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
	}

	stats := vol.Stats()
	// 100 tiny objects should use very few blocks (packed)
	usedBlocks := stats.TotalBlocks - stats.FreeBlocks
	assert.Less(t, usedBlocks, uint32(5), "Expected packed blocks, but used %d blocks", usedBlocks)

	// Verify all objects are readable
	for i := range 100 {
		key := fmt.Sprintf("tiny-%d", i)
		expectedData := fmt.Appendf(nil, "tiny-data-%d", i)
		retrieved, err := vol.Get("bucket1", key)
		require.NoError(t, err)
		assert.Equal(t, expectedData, retrieved)
	}
}

func TestIndexSerialization(t *testing.T) {
	idx := NewIndex()

	// Add entries
	for i := range 100 {
		key := fmt.Sprintf("bucket/key-%d", i)
		entry := IndexEntryFull{
			IndexEntry: IndexEntry{
				KeyHash:       HashKey("bucket", fmt.Sprintf("key-%d", i)),
				BlockNum:      uint32(i),
				BlockCount:    1,
				OffsetInBlock: uint32(i * 100),
				Size:          uint64(i * 10),
				Created:       1234567890,
				Flags:         0,
				KeyLen:        uint16(len(key)),
			},
			Key: key,
		}
		idx.Put(entry)
	}

	assert.Equal(t, 100, idx.Count())

	// Serialize
	data := idx.marshal()
	assert.NotEmpty(t, data)

	// Deserialize
	idx2, err := unmarshalIndex(data)
	require.NoError(t, err)
	assert.Equal(t, 100, idx2.Count())

	// Verify entries
	for i := range 100 {
		entry, exists := idx2.Get("bucket", fmt.Sprintf("key-%d", i))
		require.True(t, exists)
		assert.Equal(t, uint32(i), entry.BlockNum)
		assert.Equal(t, uint64(i*10), entry.Size)
	}
}

func TestAllocationMap(t *testing.T) {
	am := NewAllocationMap(100)

	assert.Equal(t, uint32(100), am.FreeCount())

	// Allocate first
	block, err := am.AllocateFirst()
	require.NoError(t, err)
	assert.Equal(t, uint32(0), block)
	assert.Equal(t, uint32(99), am.FreeCount())

	// Allocate consecutive
	start, err := am.AllocateConsecutive(5)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), start) // Next after 0
	assert.Equal(t, uint32(94), am.FreeCount())

	// Free some
	am.Free(0)
	assert.Equal(t, uint32(95), am.FreeCount())
	assert.False(t, am.IsAllocated(0))
	assert.True(t, am.IsAllocated(1))
}

func BenchmarkPutSmallObject(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "bench.neb")

	cfg := VolumeConfig{Size: 256 * 1024 * 1024}
	vol, err := CreateVolume(path, cfg)
	require.NoError(b, err)

	defer vol.Close()

	data := make([]byte, 1024) // 1KB object
	rand.Read(data)

	b.ResetTimer()

	var i int
	for b.Loop() {
		key := fmt.Sprintf("bench-key-%d", i)
		vol.Put("bucket", key, bytes.NewReader(data), int64(len(data)))
		i++
	}
}

func BenchmarkGetSmallObject(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "bench.neb")

	cfg := VolumeConfig{Size: 256 * 1024 * 1024}
	vol, err := CreateVolume(path, cfg)
	require.NoError(b, err)

	defer vol.Close()

	// Pre-populate
	data := make([]byte, 1024)
	rand.Read(data)

	for i := range 1000 {
		key := fmt.Sprintf("bench-key-%d", i)
		vol.Put("bucket", key, bytes.NewReader(data), int64(len(data)))
	}

	b.ResetTimer()

	var i int
	for b.Loop() {
		key := fmt.Sprintf("bench-key-%d", i%1000)
		vol.Get("bucket", key)
		i++
	}
}
