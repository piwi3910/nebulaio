// Package catalog implements S3 Inventory compatible catalog functionality.
package catalog

import (
	"bytes"
	"fmt"
	"io"

	"github.com/xitongsys/parquet-go-source/writerfile"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

// ParquetInventoryRecord is the Parquet-compatible representation of InventoryRecord.
// It uses int64 for timestamps (milliseconds since epoch) for AWS S3 Inventory compatibility.
// Optional fields use pointers to support null values in Parquet.
type ParquetInventoryRecord struct {
	// Core fields - always present
	Bucket       string `parquet:"name=bucket, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	Key          string `parquet:"name=key, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	Size         int64  `parquet:"name=size, type=INT64"`
	LastModified int64  `parquet:"name=last_modified, type=INT64, convertedtype=TIMESTAMP_MILLIS"`
	ETag         string `parquet:"name=etag, type=BYTE_ARRAY, convertedtype=UTF8"`
	StorageClass string `parquet:"name=storage_class, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`

	// Version fields
	VersionID      string `parquet:"name=version_id, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`
	IsLatest       bool   `parquet:"name=is_latest, type=BOOLEAN"`
	IsDeleteMarker bool   `parquet:"name=is_delete_marker, type=BOOLEAN"`

	// Upload/replication fields
	IsMultipartUploaded bool   `parquet:"name=is_multipart_uploaded, type=BOOLEAN"`
	ReplicationStatus   string `parquet:"name=replication_status, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`

	// Encryption fields
	EncryptionStatus string `parquet:"name=encryption_status, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`
	BucketKeyStatus  string `parquet:"name=bucket_key_status, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`

	// Object Lock fields
	ObjectLockRetainUntilDate *int64 `parquet:"name=object_lock_retain_until_date, type=INT64, convertedtype=TIMESTAMP_MILLIS, repetitiontype=OPTIONAL"`
	ObjectLockMode            string `parquet:"name=object_lock_mode, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`
	ObjectLockLegalHoldStatus string `parquet:"name=object_lock_legal_hold_status, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`

	// Tiering and checksum
	IntelligentTieringAccess string `parquet:"name=intelligent_tiering_access, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`
	ChecksumAlgorithm        string `parquet:"name=checksum_algorithm, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`
}

// DefaultParquetWriterConcurrency is the default number of parallel writers.
const DefaultParquetWriterConcurrency = 4

// writeParquet writes inventory records to a Parquet file with Snappy compression.
// This is the main implementation that replaces the placeholder.
func (s *CatalogService) writeParquet(w io.Writer, records []InventoryRecord) (int64, error) {
	if len(records) == 0 {
		return 0, nil
	}

	// Convert records to Parquet-compatible format
	parquetRecords := make([]ParquetInventoryRecord, 0, len(records))
	for idx := range records {
		parquetRecords = append(parquetRecords, inventoryToParquetRecord(&records[idx]))
	}

	// Write to buffer first to get accurate size
	var buffer bytes.Buffer
	bytesWritten, err := writeParquetRecords(&buffer, parquetRecords)

	if err != nil {
		return 0, fmt.Errorf("failed to write parquet records: %w", err)
	}

	// Copy buffer to the actual writer
	_, err = io.Copy(w, &buffer)
	if err != nil {
		return 0, fmt.Errorf("failed to copy parquet data to writer: %w", err)
	}

	return bytesWritten, nil
}

// writeParquetRecords writes the parquet records to a buffer and returns bytes written.
func writeParquetRecords(buffer *bytes.Buffer, records []ParquetInventoryRecord) (int64, error) {
	// Create a writer file from the buffer
	wf := writerfile.NewWriterFile(buffer)

	// Create parquet writer with Snappy compression
	pw, err := writer.NewParquetWriter(wf, new(ParquetInventoryRecord), DefaultParquetWriterConcurrency)
	if err != nil {
		return 0, fmt.Errorf("failed to create parquet writer: %w", err)
	}

	// Set compression to Snappy for better performance and compatibility
	pw.CompressionType = parquet.CompressionCodec_SNAPPY

	// Write all records
	for idx := range records {
		if err := pw.Write(records[idx]); err != nil {
			return 0, fmt.Errorf("failed to write parquet record: %w", err)
		}
	}

	// Close the writer to flush and finalize
	if err := pw.WriteStop(); err != nil {
		return 0, fmt.Errorf("failed to finalize parquet file: %w", err)
	}

	return int64(buffer.Len()), nil
}

// inventoryToParquetRecord converts an InventoryRecord to ParquetInventoryRecord.
func inventoryToParquetRecord(rec *InventoryRecord) ParquetInventoryRecord {
	pr := ParquetInventoryRecord{
		Bucket:                    rec.Bucket,
		Key:                       rec.Key,
		Size:                      rec.Size,
		LastModified:              rec.LastModified.UnixMilli(),
		ETag:                      rec.ETag,
		StorageClass:              rec.StorageClass,
		VersionID:                 rec.VersionID,
		IsLatest:                  rec.IsLatest,
		IsDeleteMarker:            rec.IsDeleteMarker,
		IsMultipartUploaded:       rec.IsMultipartUploaded,
		ReplicationStatus:         rec.ReplicationStatus,
		EncryptionStatus:          rec.EncryptionStatus,
		BucketKeyStatus:           rec.BucketKeyStatus,
		ObjectLockMode:            rec.ObjectLockMode,
		ObjectLockLegalHoldStatus: rec.ObjectLockLegalHoldStatus,
		IntelligentTieringAccess:  rec.IntelligentTieringAccess,
		ChecksumAlgorithm:         rec.ChecksumAlgorithm,
	}

	// Handle optional timestamp field
	if rec.ObjectLockRetainUntilDate != nil {
		millis := rec.ObjectLockRetainUntilDate.UnixMilli()
		pr.ObjectLockRetainUntilDate = &millis
	}

	return pr
}
