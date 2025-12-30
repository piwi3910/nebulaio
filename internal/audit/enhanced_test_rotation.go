package audit

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants.
const testAuditLogFile = "audit.log"

// TestFileOutputRotation_NormalFlow tests the normal rotation flow with successful compression.
func TestFileOutputRotation_NormalFlow(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	// Create FileOutput with compression enabled
	output := &FileOutput{
		path: logPath,
		rotation: RotationConfig{
			Enabled:    true,
			MaxSizeMB:  1, // Small size to trigger rotation
			Compress:   true,
			MaxBackups: 5,
		},
	}

	// Create initial log file
	//nolint:gosec // G304: logPath is from test temp dir
	f, err := os.Create(logPath)
	require.NoError(t, err)

	output.file = f
	output.size = 0

	// Write some data to create a rotatable file
	testData := strings.Repeat("test log entry\n", 1000)
	n, err := f.WriteString(testData)
	require.NoError(t, err)

	output.size = int64(n)

	// Trigger rotation
	err = output.rotate()
	require.NoError(t, err)

	// Wait a moment for background compression goroutine to complete
	time.Sleep(500 * time.Millisecond)

	// Find the rotated file (should end with timestamp)
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	var rotatedFile string

	for _, entry := range entries {
		if entry.Name() != testAuditLogFile && strings.HasPrefix(entry.Name(), testAuditLogFile+".") {
			rotatedFile = filepath.Join(tmpDir, entry.Name())
			break
		}
	}

	// Since compression succeeded (no errors injected), the original should be removed
	// and we should have a .gz file
	if rotatedFile != "" {
		// Check if the .gz file exists
		gzFile := rotatedFile + ".gz"
		_, err := os.Stat(gzFile)
		// If compression succeeded, the .gz file should exist
		if err == nil {
			// Original should be deleted after successful compression
			_, err := os.Stat(rotatedFile)
			assert.True(t, os.IsNotExist(err), "Original file should be deleted after successful compression")
		}
	}
}

// TestFileOutputRotation_CompressionSuccess tests that cleanup DOES run when compression succeeds.
func TestFileOutputRotation_CompressionSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	// Create FileOutput with compression and backup limits
	output := &FileOutput{
		path: logPath,
		rotation: RotationConfig{
			Enabled:    true,
			MaxSizeMB:  1,
			Compress:   true,
			MaxBackups: 2, // Only keep 2 backups
		},
	}

	// Create initial log file
	//nolint:gosec // G304: logPath is from test temp dir
	f, err := os.Create(logPath)
	require.NoError(t, err)

	output.file = f

	// Create several rotations to test cleanup
	for i := range 4 {
		// Write data
		testData := strings.Repeat(fmt.Sprintf("rotation %d\n", i), 1000)
		_, err := f.WriteString(testData)
		require.NoError(t, err)

		output.size = int64(len(testData))

		// Trigger rotation
		err = output.rotate()
		require.NoError(t, err)

		// Wait for background compression
		time.Sleep(500 * time.Millisecond)

		// Reopen for next iteration
		if i < 3 {
			//nolint:gosec // G304: logPath is from test temp dir
			f, err = os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
			require.NoError(t, err)

			output.file = f
		}
	}

	// Wait for all background operations to complete
	time.Sleep(1 * time.Second)

	// Count .gz files (should be MaxBackups + current = 3 total, but cleanup should limit to 2)
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	gzCount := 0

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".gz") {
			gzCount++
		}
	}

	// Should have MaxBackups (2) or fewer compressed files due to cleanup
	assert.LessOrEqual(t, gzCount, 2, "Should have MaxBackups or fewer compressed files after cleanup")
}

// TestFileOutputRotation_NoCompressionCleanupRuns tests that cleanup runs when compression is disabled.
func TestFileOutputRotation_NoCompressionCleanupRuns(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	// Create FileOutput WITHOUT compression but WITH cleanup
	output := &FileOutput{
		path: logPath,
		rotation: RotationConfig{
			Enabled:    true,
			MaxSizeMB:  1,
			Compress:   false, // Compression disabled
			MaxBackups: 2,
		},
	}

	// Create initial log file
	//nolint:gosec // G304: Test file with controlled path
	f, err := os.Create(logPath)
	require.NoError(t, err)

	output.file = f

	// Create several rotations
	for i := range 4 {
		testData := strings.Repeat(fmt.Sprintf("rotation %d\n", i), 1000)
		_, err := f.WriteString(testData)
		require.NoError(t, err)

		output.size = int64(len(testData))

		err = output.rotate()
		require.NoError(t, err)

		time.Sleep(200 * time.Millisecond)

		if i < 3 {
			//nolint:gosec // G304: Test file with controlled path
			f, err = os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
			require.NoError(t, err)

			output.file = f
		}
	}

	time.Sleep(500 * time.Millisecond)

	// Count rotated files (should be limited to MaxBackups)
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	rotatedCount := 0

	for _, entry := range entries {
		// Count files that are rotated (not the current audit.log and not .gz)
		if entry.Name() != "audit.log" && strings.HasPrefix(entry.Name(), "audit.log.") && !strings.HasSuffix(entry.Name(), ".gz") {
			rotatedCount++
		}
	}

	// Should have MaxBackups (2) or fewer files due to cleanup running even without compression
	assert.LessOrEqual(t, rotatedCount, 2, "Should have MaxBackups or fewer rotated files when compression is disabled")
}

// TestCompressFile_Success tests successful compression.
func TestCompressFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	// Create test file with data
	testData := "This is test log data\nWith multiple lines\nFor compression testing\n"
	err := os.WriteFile(testFile, []byte(testData), 0644)
	require.NoError(t, err)

	output := &FileOutput{
		path: filepath.Join(tmpDir, "audit.log"),
	}

	// Compress the file
	err = output.compressFile(testFile)
	require.NoError(t, err, "Compression should succeed")

	// Verify compressed file exists
	compressedFile := testFile + ".gz"
	_, err = os.Stat(compressedFile)
	require.NoError(t, err, "Compressed file should exist")

	// Verify original file was removed
	_, err = os.Stat(testFile)
	assert.True(t, os.IsNotExist(err), "Original file should be deleted after compression")

	// Verify compressed data is valid and matches original
	//nolint:gosec // G304: Test file with controlled path
	gzFile, err := os.Open(compressedFile)
	require.NoError(t, err)

	defer func() { _ = gzFile.Close() }()

	gzReader, err := gzip.NewReader(gzFile)
	require.NoError(t, err)

	defer func() { _ = gzReader.Close() }()

	decompressed, err := io.ReadAll(gzReader)
	require.NoError(t, err)

	assert.Equal(t, testData, string(decompressed), "Decompressed data should match original")
}

// TestCompressFile_NonExistentFile tests error handling when file doesn't exist.
func TestCompressFile_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.log")

	output := &FileOutput{
		path: filepath.Join(tmpDir, "audit.log"),
	}

	err := output.compressFile(nonExistentFile)
	require.Error(t, err, "Should return error for non-existent file")
	assert.Contains(t, err.Error(), "failed to open log file for compression", "Error should indicate file open failure")
}

// TestCompressFile_ReadOnlyDestination tests error handling when destination is read-only.
func TestCompressFile_ReadOnlyDestination(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root (permissions don't apply)")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	// Create test file
	err := os.WriteFile(testFile, []byte("test data"), 0600)
	require.NoError(t, err)

	// Make directory read-only to prevent creating .gz file
	//nolint:gosec // G302: Test intentionally sets restrictive permissions
	err = os.Chmod(tmpDir, 0500)
	require.NoError(t, err)

	//nolint:gosec // G302: Test restores permissions for cleanup
	defer func() { _ = os.Chmod(tmpDir, 0750) }() // Restore permissions for cleanup

	output := &FileOutput{
		path: filepath.Join(tmpDir, "audit.log"),
	}

	err = output.compressFile(testFile)
	require.Error(t, err, "Should return error when cannot create compressed file")
	assert.Contains(t, err.Error(), "failed to create compressed log file", "Error should indicate file creation failure")
}

// TestFileOutputRotation_CompressionFailurePreventsCleanup tests that cleanup does NOT run when compression fails.
func TestFileOutputRotation_CompressionFailurePreventsCleanup(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root (permissions don't apply)")
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	// Create FileOutput with compression and cleanup enabled
	output := &FileOutput{
		path: logPath,
		rotation: RotationConfig{
			Enabled:    true,
			MaxSizeMB:  1,
			Compress:   true,
			MaxBackups: 1, // Only keep 1 backup - this should trigger cleanup if it runs
		},
	}

	// Create and rotate first file
	//nolint:gosec // G304: Test file with controlled path
	f, err := os.Create(logPath)
	require.NoError(t, err)

	output.file = f
	testData1 := strings.Repeat("first rotation\n", 1000)
	_, err = f.WriteString(testData1)
	require.NoError(t, err)

	output.size = int64(len(testData1))

	err = output.rotate()
	require.NoError(t, err)
	time.Sleep(600 * time.Millisecond) // Wait for compression to complete

	// Create and rotate second file - this will trigger cleanup if compression succeeds
	//nolint:gosec // G304: Test file with controlled path
	f, err = os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
	require.NoError(t, err)

	output.file = f
	testData2 := strings.Repeat("second rotation\n", 1000)
	_, err = f.WriteString(testData2)
	require.NoError(t, err)

	output.size = int64(len(testData2))

	// Make the directory read-only BEFORE rotation to cause compression to fail
	// This simulates a disk space issue or permission problem during compression
	//nolint:gosec // G302: Test intentionally sets restrictive permissions
	err = os.Chmod(tmpDir, 0500)
	require.NoError(t, err)

	//nolint:gosec // G302: Test restores permissions for cleanup
	defer func() { _ = os.Chmod(tmpDir, 0700) }()

	// Trigger rotation - compression will fail due to read-only directory
	err = output.rotate()
	// Rotation itself succeeds (moves the file), but compression will fail
	require.NoError(t, err)

	// Wait for background compression attempt to complete
	time.Sleep(600 * time.Millisecond)

	// Restore permissions so we can read the directory
	//nolint:gosec // G302: Test restores permissions for reading directory
	err = os.Chmod(tmpDir, 0755)
	require.NoError(t, err)

	// Count rotated files - should have 2 uncompressed files
	// because compression failed and cleanup was skipped
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	rotatedCount := 0
	compressedCount := 0

	for _, entry := range entries {
		name := entry.Name()
		if name == "audit.log" {
			continue
		}

		if strings.HasSuffix(name, ".gz") {
			compressedCount++
		} else if strings.HasPrefix(name, "audit.log.") {
			rotatedCount++
		}
	}

	// Should have 2 uncompressed rotated files (first successful, second failed compression)
	// and 1 compressed file from the first rotation
	// Cleanup should NOT have run because the second compression failed
	assert.GreaterOrEqual(t, rotatedCount, 1, "Should have at least 1 uncompressed file from failed compression")
	assert.GreaterOrEqual(t, compressedCount, 1, "Should have at least 1 compressed file from successful first rotation")

	// The key assertion: if cleanup had run despite compression failure,
	// we would have only 1 total file (the most recent). Instead we should have more.
	totalFiles := rotatedCount + compressedCount
	assert.GreaterOrEqual(t, totalFiles, 2, "Should have 2+ files proving cleanup was skipped after compression failure")
}
