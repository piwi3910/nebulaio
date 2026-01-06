package catalog

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parquetMagic is the magic bytes that identify a valid Parquet file.
const parquetMagic = "PAR1"

func TestInventoryToParquetRecord(t *testing.T) {
	now := time.Now()
	retainUntil := now.Add(24 * time.Hour)

	tests := []struct {
		name   string
		record InventoryRecord
	}{
		{
			name: "basic record without optional fields",
			record: InventoryRecord{
				Bucket:       "test-bucket",
				Key:          "test-key.txt",
				Size:         1024,
				LastModified: now,
				ETag:         "abc123",
				StorageClass: "STANDARD",
				IsLatest:     true,
			},
		},
		{
			name: "record with version info",
			record: InventoryRecord{
				Bucket:         "test-bucket",
				Key:            "versioned-key.txt",
				VersionID:      "v123",
				Size:           2048,
				LastModified:   now,
				ETag:           "def456",
				StorageClass:   "GLACIER",
				IsLatest:       true,
				IsDeleteMarker: false,
			},
		},
		{
			name: "record with object lock",
			record: InventoryRecord{
				Bucket:                    "locked-bucket",
				Key:                       "locked-key.txt",
				Size:                      512,
				LastModified:              now,
				ETag:                      "ghi789",
				StorageClass:              "STANDARD",
				ObjectLockRetainUntilDate: &retainUntil,
				ObjectLockMode:            "GOVERNANCE",
				ObjectLockLegalHoldStatus: "OFF",
			},
		},
		{
			name: "record with encryption info",
			record: InventoryRecord{
				Bucket:           "encrypted-bucket",
				Key:              "encrypted-key.txt",
				Size:             4096,
				LastModified:     now,
				ETag:             "jkl012",
				StorageClass:     "STANDARD",
				EncryptionStatus: "SSE-S3",
				BucketKeyStatus:  "ENABLED",
			},
		},
		{
			name: "record with all fields",
			record: InventoryRecord{
				Bucket:                    "full-bucket",
				Key:                       "full-key.txt",
				VersionID:                 "v999",
				Size:                      8192,
				LastModified:              now,
				ETag:                      "mno345",
				StorageClass:              "INTELLIGENT_TIERING",
				IsLatest:                  true,
				IsDeleteMarker:            false,
				IsMultipartUploaded:       true,
				ReplicationStatus:         "COMPLETED",
				EncryptionStatus:          "SSE-KMS",
				ObjectLockRetainUntilDate: &retainUntil,
				ObjectLockMode:            "COMPLIANCE",
				ObjectLockLegalHoldStatus: "ON",
				IntelligentTieringAccess:  "FREQUENT_ACCESS",
				BucketKeyStatus:           "ENABLED",
				ChecksumAlgorithm:         "SHA256",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parquetRec := inventoryToParquetRecord(&tt.record)

			// Verify basic fields
			assert.Equal(t, tt.record.Bucket, parquetRec.Bucket, "Bucket mismatch")
			assert.Equal(t, tt.record.Key, parquetRec.Key, "Key mismatch")
			assert.Equal(t, tt.record.Size, parquetRec.Size, "Size mismatch")
			assert.Equal(t, tt.record.LastModified.UnixMilli(), parquetRec.LastModified,
				"LastModified mismatch")
			assert.Equal(t, tt.record.ETag, parquetRec.ETag, "ETag mismatch")
			assert.Equal(t, tt.record.StorageClass, parquetRec.StorageClass,
				"StorageClass mismatch")

			// Verify optional timestamp field
			if tt.record.ObjectLockRetainUntilDate != nil {
				require.NotNil(t, parquetRec.ObjectLockRetainUntilDate,
					"ObjectLockRetainUntilDate should not be nil")
				expectedMillis := tt.record.ObjectLockRetainUntilDate.UnixMilli()
				assert.Equal(t, expectedMillis, *parquetRec.ObjectLockRetainUntilDate,
					"ObjectLockRetainUntilDate mismatch")
			} else {
				assert.Nil(t, parquetRec.ObjectLockRetainUntilDate,
					"ObjectLockRetainUntilDate should be nil")
			}
		})
	}
}

func TestWriteParquetRecords(t *testing.T) {
	now := time.Now()

	records := []ParquetInventoryRecord{
		{
			Bucket:       "test-bucket",
			Key:          "file1.txt",
			Size:         1024,
			LastModified: now.UnixMilli(),
			ETag:         "etag1",
			StorageClass: "STANDARD",
			IsLatest:     true,
		},
		{
			Bucket:       "test-bucket",
			Key:          "file2.txt",
			Size:         2048,
			LastModified: now.UnixMilli(),
			ETag:         "etag2",
			StorageClass: "STANDARD",
			IsLatest:     true,
		},
	}

	var buffer bytes.Buffer
	size, err := writeParquetRecords(&buffer, records)

	require.NoError(t, err, "writeParquetRecords should not fail")
	assert.Positive(t, size, "Expected positive size")
	assert.NotEmpty(t, buffer.Bytes(), "Buffer should not be empty")

	// Verify Parquet magic bytes (PAR1)
	data := buffer.Bytes()
	require.GreaterOrEqual(t, len(data), 4, "Parquet file too small")

	magic := string(data[:4])
	assert.Equal(t, parquetMagic, magic, "Invalid Parquet header magic")

	// Verify footer magic bytes
	footerMagic := string(data[len(data)-4:])
	assert.Equal(t, parquetMagic, footerMagic, "Invalid Parquet footer magic")
}

func TestWriteParquetEmptyRecords(t *testing.T) {
	svc := &CatalogService{}
	var buffer bytes.Buffer

	size, err := svc.writeParquet(&buffer, []InventoryRecord{})

	require.NoError(t, err, "writeParquet with empty records should not fail")
	assert.Equal(t, int64(0), size, "Expected size 0 for empty records")
}

func TestWriteParquetIntegration(t *testing.T) {
	now := time.Now()
	retainUntil := now.Add(30 * 24 * time.Hour)

	records := []InventoryRecord{
		{
			Bucket:       "integration-bucket",
			Key:          "path/to/file1.json",
			Size:         1234567,
			LastModified: now,
			ETag:         "abc123def456",
			StorageClass: "STANDARD",
			IsLatest:     true,
		},
		{
			Bucket:                    "integration-bucket",
			Key:                       "path/to/file2.parquet",
			VersionID:                 "v12345",
			Size:                      9876543,
			LastModified:              now.Add(-1 * time.Hour),
			ETag:                      "789ghi012jkl",
			StorageClass:              "GLACIER",
			IsLatest:                  false,
			IsDeleteMarker:            false,
			IsMultipartUploaded:       true,
			ReplicationStatus:         "REPLICA",
			EncryptionStatus:          "SSE-KMS",
			ObjectLockRetainUntilDate: &retainUntil,
			ObjectLockMode:            "COMPLIANCE",
			ObjectLockLegalHoldStatus: "OFF",
			IntelligentTieringAccess:  "ARCHIVE_ACCESS",
			BucketKeyStatus:           "ENABLED",
			ChecksumAlgorithm:         "SHA256",
		},
		{
			Bucket:         "integration-bucket",
			Key:            "deleted/object.txt",
			Size:           0,
			LastModified:   now.Add(-24 * time.Hour),
			ETag:           "",
			StorageClass:   "STANDARD",
			IsLatest:       true,
			IsDeleteMarker: true,
		},
	}

	svc := &CatalogService{}
	var buffer bytes.Buffer

	size, err := svc.writeParquet(&buffer, records)

	require.NoError(t, err, "writeParquet integration test should not fail")
	assert.Positive(t, size, "Expected positive size")

	// Verify buffer contains valid Parquet data
	data := buffer.Bytes()
	require.GreaterOrEqual(t, len(data), 8, "Parquet file too small for valid structure")

	// Check header magic
	assert.Equal(t, parquetMagic, string(data[:4]), "Invalid Parquet header magic")

	// Check footer magic
	assert.Equal(t, parquetMagic, string(data[len(data)-4:]), "Invalid Parquet footer magic")

	t.Logf("Generated Parquet file size: %d bytes for %d records", size, len(records))
}

func TestWriteParquetLargeDataset(t *testing.T) {
	now := time.Now()
	numRecords := 1000

	records := make([]InventoryRecord, numRecords)
	for idx := range numRecords {
		records[idx] = InventoryRecord{
			Bucket:       "large-bucket",
			Key:          "file" + string(rune('0'+idx%10)) + ".txt",
			Size:         int64(idx * 100),
			LastModified: now.Add(-time.Duration(idx) * time.Minute),
			ETag:         "etag" + string(rune('0'+idx%10)),
			StorageClass: "STANDARD",
			IsLatest:     idx%2 == 0,
		}
	}

	svc := &CatalogService{}
	var buffer bytes.Buffer

	size, err := svc.writeParquet(&buffer, records)

	require.NoError(t, err, "writeParquet with %d records should not fail", numRecords)
	assert.Positive(t, size, "Expected positive size for %d records", numRecords)

	// Verify Parquet structure
	data := buffer.Bytes()
	assert.Equal(t, parquetMagic, string(data[:4]), "Invalid Parquet header magic")
	assert.Equal(t, parquetMagic, string(data[len(data)-4:]), "Invalid Parquet footer magic")

	t.Logf("Generated Parquet file: %d bytes for %d records (%.2f bytes/record)",
		size, numRecords, float64(size)/float64(numRecords))
}

func TestParquetInventoryRecordSchema(t *testing.T) {
	parquetRec := ParquetInventoryRecord{
		Bucket:       "schema-test",
		Key:          "key.txt",
		Size:         100,
		LastModified: time.Now().UnixMilli(),
		ETag:         "etag",
		StorageClass: "STANDARD",
	}

	// Verify the struct can be used with the parquet library
	// This is a basic smoke test to ensure tags are valid
	records := []ParquetInventoryRecord{parquetRec}

	var buffer bytes.Buffer
	size, err := writeParquetRecords(&buffer, records)

	require.NoError(t, err, "Failed to write single record")
	assert.Positive(t, size, "Expected positive size")
}

// TestWriteParquetCompression verifies that Snappy compression is applied
// by checking that the compressed output is smaller than uncompressed equivalent.
func TestWriteParquetCompression(t *testing.T) {
	now := time.Now()

	// Create records with repetitive data that compresses well
	numRecords := 100
	records := make([]InventoryRecord, numRecords)
	for idx := range numRecords {
		records[idx] = InventoryRecord{
			Bucket:       "compression-test-bucket",
			Key:          "repeated/path/to/file.txt",
			Size:         1024,
			LastModified: now,
			ETag:         "same-etag-for-all-records",
			StorageClass: "STANDARD",
			IsLatest:     true,
		}
	}

	svc := &CatalogService{}
	var buffer bytes.Buffer

	size, err := svc.writeParquet(&buffer, records)

	require.NoError(t, err, "writeParquet should not fail")
	assert.Positive(t, size, "Expected positive size")

	// Verify Parquet structure
	data := buffer.Bytes()
	assert.Equal(t, parquetMagic, string(data[:4]), "Invalid Parquet header magic")
	assert.Equal(t, parquetMagic, string(data[len(data)-4:]), "Invalid Parquet footer magic")

	// With Snappy compression and dictionary encoding, repetitive data should compress well.
	// Uncompressed, 100 records with ~100 bytes each would be ~10KB.
	// Compressed should be significantly smaller due to repetition.
	uncompressedEstimate := int64(numRecords * 100)
	assert.Less(t, size, uncompressedEstimate,
		"Compressed size (%d) should be less than uncompressed estimate (%d)",
		size, uncompressedEstimate)

	t.Logf("Compression test: %d records, compressed size: %d bytes (estimated uncompressed: %d)",
		numRecords, size, uncompressedEstimate)
}

// TestWriteParquetDataIntegrity verifies that written data can be read back correctly
// by checking the Parquet file structure and metadata.
func TestWriteParquetDataIntegrity(t *testing.T) {
	now := time.Now()
	retainUntil := now.Add(24 * time.Hour)

	// Create a record with all fields populated
	records := []InventoryRecord{
		{
			Bucket:                    "integrity-bucket",
			Key:                       "test/file.txt",
			VersionID:                 "v1",
			Size:                      12345,
			LastModified:              now,
			ETag:                      "abc123",
			StorageClass:              "STANDARD",
			IsLatest:                  true,
			IsDeleteMarker:            false,
			IsMultipartUploaded:       true,
			ReplicationStatus:         "COMPLETED",
			EncryptionStatus:          "SSE-S3",
			ObjectLockRetainUntilDate: &retainUntil,
			ObjectLockMode:            "GOVERNANCE",
			ObjectLockLegalHoldStatus: "OFF",
			IntelligentTieringAccess:  "FREQUENT",
			BucketKeyStatus:           "ENABLED",
			ChecksumAlgorithm:         "SHA256",
		},
	}

	svc := &CatalogService{}
	var buffer bytes.Buffer

	size, err := svc.writeParquet(&buffer, records)

	require.NoError(t, err, "writeParquet should not fail")
	assert.Positive(t, size, "Expected positive size")

	data := buffer.Bytes()

	// Verify Parquet file structure
	require.GreaterOrEqual(t, len(data), 8, "Parquet file too small")
	assert.Equal(t, parquetMagic, string(data[:4]), "Invalid header magic")
	assert.Equal(t, parquetMagic, string(data[len(data)-4:]), "Invalid footer magic")

	// Verify the file contains expected content markers
	// The bucket name should appear in the file (in the data pages)
	assert.True(t, bytes.Contains(data, []byte("integrity-bucket")),
		"Parquet file should contain bucket name")
	assert.True(t, bytes.Contains(data, []byte("test/file.txt")),
		"Parquet file should contain key")

	t.Logf("Data integrity test: file size %d bytes, contains expected data markers", size)
}

// TestWriteParquetFieldConversion verifies all field types are correctly converted.
func TestWriteParquetFieldConversion(t *testing.T) {
	now := time.Now()
	retainUntil := now.Add(48 * time.Hour)

	record := InventoryRecord{
		Bucket:                    "conversion-bucket",
		Key:                       "test-key",
		VersionID:                 "version-123",
		Size:                      999999,
		LastModified:              now,
		ETag:                      "etag-value",
		StorageClass:              "GLACIER",
		IsLatest:                  false,
		IsDeleteMarker:            true,
		IsMultipartUploaded:       true,
		ReplicationStatus:         "PENDING",
		EncryptionStatus:          "SSE-KMS",
		ObjectLockRetainUntilDate: &retainUntil,
		ObjectLockMode:            "COMPLIANCE",
		ObjectLockLegalHoldStatus: "ON",
		IntelligentTieringAccess:  "ARCHIVE",
		BucketKeyStatus:           "DISABLED",
		ChecksumAlgorithm:         "CRC32",
	}

	parquetRec := inventoryToParquetRecord(&record)

	// Verify all fields converted correctly
	assert.Equal(t, record.Bucket, parquetRec.Bucket)
	assert.Equal(t, record.Key, parquetRec.Key)
	assert.Equal(t, record.VersionID, parquetRec.VersionID)
	assert.Equal(t, record.Size, parquetRec.Size)
	assert.Equal(t, record.LastModified.UnixMilli(), parquetRec.LastModified)
	assert.Equal(t, record.ETag, parquetRec.ETag)
	assert.Equal(t, record.StorageClass, parquetRec.StorageClass)
	assert.Equal(t, record.IsLatest, parquetRec.IsLatest)
	assert.Equal(t, record.IsDeleteMarker, parquetRec.IsDeleteMarker)
	assert.Equal(t, record.IsMultipartUploaded, parquetRec.IsMultipartUploaded)
	assert.Equal(t, record.ReplicationStatus, parquetRec.ReplicationStatus)
	assert.Equal(t, record.EncryptionStatus, parquetRec.EncryptionStatus)

	// Verify optional timestamp pointer conversion
	require.NotNil(t, parquetRec.ObjectLockRetainUntilDate)
	assert.Equal(t, retainUntil.UnixMilli(), *parquetRec.ObjectLockRetainUntilDate)

	assert.Equal(t, record.ObjectLockMode, parquetRec.ObjectLockMode)
	assert.Equal(t, record.ObjectLockLegalHoldStatus, parquetRec.ObjectLockLegalHoldStatus)
	assert.Equal(t, record.IntelligentTieringAccess, parquetRec.IntelligentTieringAccess)
	assert.Equal(t, record.BucketKeyStatus, parquetRec.BucketKeyStatus)
	assert.Equal(t, record.ChecksumAlgorithm, parquetRec.ChecksumAlgorithm)
}

// TestWriteParquetNilOptionalFields verifies nil optional fields are handled correctly.
func TestWriteParquetNilOptionalFields(t *testing.T) {
	record := InventoryRecord{
		Bucket:                    "nil-test-bucket",
		Key:                       "nil-test-key",
		Size:                      100,
		LastModified:              time.Now(),
		ETag:                      "etag",
		StorageClass:              "STANDARD",
		ObjectLockRetainUntilDate: nil, // Explicitly nil
	}

	parquetRec := inventoryToParquetRecord(&record)

	// Verify nil pointer is preserved
	assert.Nil(t, parquetRec.ObjectLockRetainUntilDate,
		"Nil ObjectLockRetainUntilDate should remain nil in Parquet record")

	// Verify the record can still be written successfully
	records := []ParquetInventoryRecord{parquetRec}
	var buffer bytes.Buffer
	size, err := writeParquetRecords(&buffer, records)

	require.NoError(t, err, "Should handle nil optional fields")
	assert.Positive(t, size, "Expected positive size")
}
