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
