package volume

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllocateAligned(t *testing.T) {
	tests := []struct {
		size      int
		alignment int
	}{
		{1024, 512},
		{4096, 4096},
		{1024 * 1024, 4096},     // 1MB
		{4 * 1024 * 1024, 4096}, // 4MB (block size)
	}

	for _, tt := range tests {
		buf := AllocateAligned(tt.size, tt.alignment)
		assert.Len(t, buf, tt.size, "Buffer should be correct size")
		assert.True(t, IsAligned(buf, tt.alignment), "Buffer should be aligned to %d", tt.alignment)
	}
}

func TestIsAligned(t *testing.T) {
	// Aligned buffer
	aligned := AllocateAligned(4096, 4096)
	assert.True(t, IsAligned(aligned, 4096))
	assert.True(t, IsAligned(aligned, 512))

	// Empty buffer is always aligned
	assert.True(t, IsAligned([]byte{}, 4096))

	// Regular buffer may or may not be aligned
	regular := make([]byte, 100)
	_ = IsAligned(regular, 4096) // Just test it doesn't panic
}

func TestIsOffsetAligned(t *testing.T) {
	assert.True(t, IsOffsetAligned(0, 4096))
	assert.True(t, IsOffsetAligned(4096, 4096))
	assert.True(t, IsOffsetAligned(8192, 4096))
	assert.False(t, IsOffsetAligned(1000, 4096))
	assert.False(t, IsOffsetAligned(4097, 4096))
}

func TestAlignedBufferPool(t *testing.T) {
	pool := NewAlignedBufferPool(4096, BlockSize)

	// Get buffer from pool
	buf1 := pool.Get()
	assert.Len(t, buf1, BlockSize)
	assert.True(t, IsAligned(buf1, 4096))

	// Write some data
	buf1[0] = 0xFF
	buf1[1] = 0xFE

	// Return to pool
	pool.Put(buf1)

	// Get another buffer (may be recycled)
	buf2 := pool.Get()
	assert.Len(t, buf2, BlockSize)
	assert.True(t, IsAligned(buf2, 4096))

	// Buffer should be cleared if recycled
	// (this may be a different buffer if pool didn't recycle)
}

func TestDirectIOFileBasic(t *testing.T) {
	// Create a temp file
	tmpFile, err := os.CreateTemp(t.TempDir(), "directio_test_*.dat")
	require.NoError(t, err)

	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Pre-allocate some space
	require.NoError(t, tmpFile.Truncate(1024*1024)) // 1MB

	// Create DirectIOFile
	config := DefaultDirectIOConfig()
	config.Enabled = true
	dio := NewDirectIOFile(tmpFile, config)

	// Test write
	data := AllocateAligned(4096, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}

	n, err := dio.WriteAt(data, 0)
	require.NoError(t, err)
	assert.Equal(t, 4096, n)

	// Test read
	readBuf := AllocateAligned(4096, 4096)
	n, err = dio.ReadAt(readBuf, 0)
	require.NoError(t, err)
	assert.Equal(t, 4096, n)
	assert.True(t, bytes.Equal(data, readBuf))

	// Get stats
	stats := dio.Stats()
	// On macOS, directIOSupported() returns false, so we get buffered I/O
	if directIOSupported() {
		assert.Equal(t, uint64(1), stats.DirectReads)
		assert.Equal(t, uint64(1), stats.DirectWrites)
	} else {
		assert.Equal(t, uint64(1), stats.BufferedReads)
		assert.Equal(t, uint64(1), stats.BufferedWrites)
	}
}

func TestDirectIOFileWithPool(t *testing.T) {
	// Create a temp file
	tmpFile, err := os.CreateTemp(t.TempDir(), "directio_pool_test_*.dat")
	require.NoError(t, err)

	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Pre-allocate
	require.NoError(t, tmpFile.Truncate(8*1024*1024)) // 8MB

	// Create DirectIOFile with pool
	config := DirectIOConfig{
		Enabled:         true,
		BlockAlignment:  4096,
		UseMemoryPool:   true,
		PoolBlockSize:   BlockSize, // 4MB
		FallbackOnError: true,
	}
	dio := NewDirectIOFile(tmpFile, config)

	// Get aligned buffer from pool
	buf := dio.GetAlignedBuffer(BlockSize)
	assert.Len(t, buf, BlockSize)
	assert.True(t, IsAligned(buf, 4096))

	// Fill with pattern
	for i := range buf {
		buf[i] = byte(i % 256)
	}

	// Write (aligned offset)
	n, err := dio.WriteAt(buf, 0)
	require.NoError(t, err)
	assert.Equal(t, BlockSize, n)

	// Return buffer
	dio.PutAlignedBuffer(buf)

	// Get another buffer and read
	readBuf := dio.GetAlignedBuffer(BlockSize)
	n, err = dio.ReadAt(readBuf, 0)
	require.NoError(t, err)
	assert.Equal(t, BlockSize, n)

	// Verify data
	for i := range BlockSize {
		if readBuf[i] != byte(i%256) {
			t.Errorf("Data mismatch at offset %d: got %d, want %d", i, readBuf[i], byte(i%256))
			break
		}
	}

	dio.PutAlignedBuffer(readBuf)
}

func TestDirectIOFallback(t *testing.T) {
	// Create a temp file
	tmpFile, err := os.CreateTemp(t.TempDir(), "directio_fallback_test_*.dat")
	require.NoError(t, err)

	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	require.NoError(t, tmpFile.Truncate(1024*1024))

	// Create DirectIOFile with fallback enabled
	config := DirectIOConfig{
		Enabled:         true,
		BlockAlignment:  4096,
		UseMemoryPool:   false,
		FallbackOnError: true,
	}
	dio := NewDirectIOFile(tmpFile, config)

	// Write with unaligned buffer (will use fallback on supported platforms)
	data := make([]byte, 1000) // Unaligned size
	for i := range data {
		data[i] = byte(i % 256)
	}

	// This uses buffered I/O due to unaligned buffer
	n, err := dio.WriteAt(data, 0)
	require.NoError(t, err)
	assert.Equal(t, 1000, n)

	// Read with unaligned buffer
	readBuf := make([]byte, 1000)
	n, err = dio.ReadAt(readBuf, 0)
	require.NoError(t, err)
	assert.Equal(t, 1000, n)
	assert.True(t, bytes.Equal(data, readBuf))

	// These should be buffered reads/writes
	stats := dio.Stats()
	assert.Equal(t, uint64(1), stats.BufferedReads)
	assert.Equal(t, uint64(1), stats.BufferedWrites)
}

func TestDirectIOSupportedCheck(t *testing.T) {
	// This test just verifies the function doesn't panic
	supported := directIOSupported()
	t.Logf("Direct I/O supported on this platform: %v", supported)
}

func TestVolumeWithDirectIO(t *testing.T) {
	dir := t.TempDir()

	// Create volume with direct I/O enabled
	cfg := VolumeConfig{
		Size:      64 * 1024 * 1024, // 64MB test volume
		BlockSize: BlockSize,
		DirectIO: DirectIOConfig{
			Enabled:         true,
			BlockAlignment:  4096,
			UseMemoryPool:   true,
			PoolBlockSize:   BlockSize,
			FallbackOnError: true,
		},
	}

	vol, err := CreateVolume(dir+"/test.neb", cfg)
	require.NoError(t, err)

	defer vol.Close()

	// Write an object
	bucket := "test-bucket"
	key := "test-key"
	data := []byte("Hello, Direct I/O!")

	err = vol.Put(bucket, key, bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	// Read it back
	result, err := vol.Get(bucket, key)
	require.NoError(t, err)
	assert.Equal(t, data, result)

	// Check stats include DirectIO stats
	stats := vol.Stats()
	t.Logf("Volume stats: DirectIO enabled=%v, direct reads=%d, direct writes=%d",
		stats.DirectIO.DirectIOEnabled,
		stats.DirectIO.DirectReads,
		stats.DirectIO.DirectWrites)
}

func TestManagerWithDirectIO(t *testing.T) {
	dir := t.TempDir()

	// Create manager with direct I/O
	cfg := ManagerConfig{
		DataDir:       dir,
		MaxVolumeSize: 64 * 1024 * 1024,
		AutoCreate:    true,
		DirectIO: DirectIOConfig{
			Enabled:         true,
			BlockAlignment:  4096,
			UseMemoryPool:   true,
			PoolBlockSize:   BlockSize,
			FallbackOnError: true,
		},
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)

	defer mgr.Close()

	// Write multiple objects
	testData := map[string][]byte{
		"obj1": []byte("First object with direct I/O"),
		"obj2": []byte("Second object with direct I/O"),
		"obj3": bytes.Repeat([]byte("Large object data pattern "), 1000),
	}

	for key, data := range testData {
		err := mgr.Put("test-bucket", key, data)
		require.NoError(t, err)
	}

	// Read them back
	for key, expected := range testData {
		actual, err := mgr.Get("test-bucket", key)
		require.NoError(t, err)
		assert.Equal(t, expected, actual, "Data mismatch for key: %s", key)
	}

	// Check stats
	stats := mgr.Stats()
	assert.Equal(t, 3, stats.ObjectCount)
	t.Logf("Manager stats: volumes=%d, objects=%d", stats.VolumeCount, stats.ObjectCount)
}

func TestOpenVolumeWithDirectIOConfig(t *testing.T) {
	dir := t.TempDir()

	// First create a volume without direct I/O
	cfg := VolumeConfig{
		Size:      64 * 1024 * 1024,
		BlockSize: BlockSize,
	}

	vol, err := CreateVolume(dir+"/test.neb", cfg)
	require.NoError(t, err)

	// Write some data
	bucket := "test-bucket"
	key := "test-key"
	data := []byte("Persistent data")
	err = vol.Put(bucket, key, bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	vol.Close()

	// Reopen with direct I/O enabled
	dioConfig := DirectIOConfig{
		Enabled:         true,
		BlockAlignment:  4096,
		UseMemoryPool:   true,
		PoolBlockSize:   BlockSize,
		FallbackOnError: true,
	}

	vol2, err := OpenVolumeWithConfig(dir+"/test.neb", dioConfig)
	require.NoError(t, err)

	defer vol2.Close()

	// Read the data
	result, err := vol2.Get(bucket, key)
	require.NoError(t, err)
	assert.Equal(t, data, result)

	// Check direct I/O is enabled in stats
	stats := vol2.Stats()
	if directIOSupported() {
		assert.True(t, stats.DirectIO.DirectIOEnabled)
	}
}

func BenchmarkDirectIOWrite(b *testing.B) {
	dir := b.TempDir()

	cfg := VolumeConfig{
		Size:      512 * 1024 * 1024, // 512MB
		BlockSize: BlockSize,
		DirectIO: DirectIOConfig{
			Enabled:         true,
			BlockAlignment:  4096,
			UseMemoryPool:   true,
			PoolBlockSize:   BlockSize,
			FallbackOnError: true,
		},
	}

	vol, err := CreateVolume(dir+"/bench.neb", cfg)
	require.NoError(b, err)

	defer vol.Close()

	data := bytes.Repeat([]byte("Benchmark data for direct I/O testing "), 256) // ~10KB

	b.ResetTimer()

	var i int
	for b.Loop() {
		key := "key-" + string(rune(i%1000))

		err := vol.Put("bench", key, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkDirectIORead(b *testing.B) {
	dir := b.TempDir()

	cfg := VolumeConfig{
		Size:      512 * 1024 * 1024,
		BlockSize: BlockSize,
		DirectIO: DirectIOConfig{
			Enabled:         true,
			BlockAlignment:  4096,
			UseMemoryPool:   true,
			PoolBlockSize:   BlockSize,
			FallbackOnError: true,
		},
	}

	vol, err := CreateVolume(dir+"/bench.neb", cfg)
	require.NoError(b, err)

	defer vol.Close()

	// Pre-populate data
	data := bytes.Repeat([]byte("Benchmark data for direct I/O testing "), 256)

	for i := range 100 {
		key := "key-" + string(rune(i))
		err := vol.Put("bench", key, bytes.NewReader(data), int64(len(data)))
		require.NoError(b, err)
	}

	b.ResetTimer()

	var i int
	for b.Loop() {
		key := "key-" + string(rune(i%100))

		_, err := vol.Get("bench", key)
		if err != nil {
			b.Fatal(err)
		}
		i++
	}
}
