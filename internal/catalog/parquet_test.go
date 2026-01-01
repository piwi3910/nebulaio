package catalog

import (
	"bytes"
	"testing"
	"time"
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
			pr := inventoryToParquetRecord(&tt.record)

			// Verify basic fields
			if pr.Bucket != tt.record.Bucket {
				t.Errorf("Bucket: got %q, want %q", pr.Bucket, tt.record.Bucket)
			}

			if pr.Key != tt.record.Key {
				t.Errorf("Key: got %q, want %q", pr.Key, tt.record.Key)
			}

			if pr.Size != tt.record.Size {
				t.Errorf("Size: got %d, want %d", pr.Size, tt.record.Size)
			}

			if pr.LastModified != tt.record.LastModified.UnixMilli() {
				t.Errorf("LastModified: got %d, want %d",
					pr.LastModified, tt.record.LastModified.UnixMilli())
			}

			if pr.ETag != tt.record.ETag {
				t.Errorf("ETag: got %q, want %q", pr.ETag, tt.record.ETag)
			}

			if pr.StorageClass != tt.record.StorageClass {
				t.Errorf("StorageClass: got %q, want %q", pr.StorageClass, tt.record.StorageClass)
			}

			// Verify optional timestamp field
			if tt.record.ObjectLockRetainUntilDate != nil {
				if pr.ObjectLockRetainUntilDate == nil {
					t.Error("ObjectLockRetainUntilDate: expected non-nil, got nil")
				} else {
					expectedMillis := tt.record.ObjectLockRetainUntilDate.UnixMilli()
					if *pr.ObjectLockRetainUntilDate != expectedMillis {
						t.Errorf("ObjectLockRetainUntilDate: got %d, want %d",
							*pr.ObjectLockRetainUntilDate, expectedMillis)
					}
				}
			} else if pr.ObjectLockRetainUntilDate != nil {
				t.Error("ObjectLockRetainUntilDate: expected nil, got non-nil")
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

	if err != nil {
		t.Fatalf("writeParquetRecords failed: %v", err)
	}

	if size <= 0 {
		t.Errorf("Expected positive size, got %d", size)
	}

	if buffer.Len() == 0 {
		t.Error("Buffer should not be empty")
	}

	// Verify Parquet magic bytes (PAR1)
	data := buffer.Bytes()
	if len(data) < 4 {
		t.Fatal("Parquet file too small")
	}

	magic := string(data[:4])
	if magic != parquetMagic {
		t.Errorf("Expected Parquet magic %q, got %q", parquetMagic, magic)
	}

	// Verify footer magic bytes
	if len(data) >= 4 {
		footerMagic := string(data[len(data)-4:])
		if footerMagic != parquetMagic {
			t.Errorf("Expected footer magic %q, got %q", parquetMagic, footerMagic)
		}
	}
}

func TestWriteParquetEmptyRecords(t *testing.T) {
	svc := &CatalogService{}
	var buffer bytes.Buffer

	size, err := svc.writeParquet(&buffer, []InventoryRecord{})
	if err != nil {
		t.Fatalf("writeParquet with empty records failed: %v", err)
	}

	if size != 0 {
		t.Errorf("Expected size 0 for empty records, got %d", size)
	}
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
	if err != nil {
		t.Fatalf("writeParquet integration test failed: %v", err)
	}

	if size <= 0 {
		t.Errorf("Expected positive size, got %d", size)
	}

	// Verify buffer contains valid Parquet data
	data := buffer.Bytes()
	if len(data) < 8 {
		t.Fatal("Parquet file too small for valid structure")
	}

	// Check header magic
	if string(data[:4]) != parquetMagic {
		t.Errorf("Invalid Parquet header magic: %q", string(data[:4]))
	}

	// Check footer magic
	if string(data[len(data)-4:]) != parquetMagic {
		t.Errorf("Invalid Parquet footer magic: %q", string(data[len(data)-4:]))
	}

	t.Logf("Generated Parquet file size: %d bytes for %d records", size, len(records))
}

func TestWriteParquetLargeDataset(t *testing.T) {
	now := time.Now()
	numRecords := 1000

	records := make([]InventoryRecord, numRecords)
	for i := range numRecords {
		records[i] = InventoryRecord{
			Bucket:       "large-bucket",
			Key:          "file" + string(rune('0'+i%10)) + ".txt",
			Size:         int64(i * 100),
			LastModified: now.Add(-time.Duration(i) * time.Minute),
			ETag:         "etag" + string(rune('0'+i%10)),
			StorageClass: "STANDARD",
			IsLatest:     i%2 == 0,
		}
	}

	svc := &CatalogService{}
	var buffer bytes.Buffer

	size, err := svc.writeParquet(&buffer, records)
	if err != nil {
		t.Fatalf("writeParquet with %d records failed: %v", numRecords, err)
	}

	if size <= 0 {
		t.Errorf("Expected positive size for %d records, got %d", numRecords, size)
	}

	// Verify Parquet structure
	data := buffer.Bytes()
	if string(data[:4]) != parquetMagic || string(data[len(data)-4:]) != parquetMagic {
		t.Error("Invalid Parquet file structure")
	}

	t.Logf("Generated Parquet file: %d bytes for %d records (%.2f bytes/record)",
		size, numRecords, float64(size)/float64(numRecords))
}

func TestParquetInventoryRecordSchema(t *testing.T) {
	pr := ParquetInventoryRecord{
		Bucket:       "schema-test",
		Key:          "key.txt",
		Size:         100,
		LastModified: time.Now().UnixMilli(),
		ETag:         "etag",
		StorageClass: "STANDARD",
	}

	// Verify the struct can be used with the parquet library
	// This is a basic smoke test to ensure tags are valid
	records := []ParquetInventoryRecord{pr}

	var buffer bytes.Buffer
	size, err := writeParquetRecords(&buffer, records)

	if err != nil {
		t.Fatalf("Failed to write single record: %v", err)
	}

	if size <= 0 {
		t.Errorf("Expected positive size, got %d", size)
	}
}
