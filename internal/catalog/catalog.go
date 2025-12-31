// Package catalog implements S3 Inventory compatible catalog functionality
// for inventorying and analyzing petabyte to exabyte-scale namespaces.
package catalog

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// OutputFormat specifies the inventory output format.
type OutputFormat string

const (
	FormatCSV     OutputFormat = "CSV"
	FormatParquet OutputFormat = "Parquet"
	FormatJSON    OutputFormat = "JSON"
	FormatORC     OutputFormat = "ORC"
)

// Frequency specifies how often inventory is generated.
type Frequency string

const (
	FrequencyDaily  Frequency = "Daily"
	FrequencyWeekly Frequency = "Weekly"
)

// InventoryConfig defines the configuration for an inventory job.
type InventoryConfig struct {
	// 8-byte fields (pointers, slices)
	// IncludedFields specifies which fields to include
	IncludedFields []string `json:"included_fields"`

	// Filter optionally filters objects to include
	Filter *InventoryFilter `json:"filter,omitempty"`

	// LastRun is when inventory was last generated
	LastRun *time.Time `json:"last_run,omitempty"`

	// CreatedAt is when this config was created
	CreatedAt time.Time `json:"created_at"`

	// ID is the unique identifier for this inventory configuration
	ID string `json:"id"`

	// SourceBucket is the bucket to inventory
	SourceBucket string `json:"source_bucket"`

	// DestinationBucket is where inventory files are stored
	DestinationBucket string `json:"destination_bucket"`

	// DestinationPrefix is the prefix for inventory files
	DestinationPrefix string `json:"destination_prefix"`

	// Schedule for inventory generation (cron expression)
	Schedule string `json:"schedule,omitempty"`

	// Format is the output format (CSV, Parquet, JSON, ORC)
	Format OutputFormat `json:"format"`

	// Frequency determines how often inventory is generated
	Frequency Frequency `json:"frequency"`

	// Enabled determines if inventory generation is active
	Enabled bool `json:"enabled"`
}

// InventoryFilter defines filtering criteria for inventory.
type InventoryFilter struct {
	// Prefix filters objects by key prefix
	Prefix string `json:"prefix,omitempty"`

	// MinSize filters objects by minimum size
	MinSize *int64 `json:"min_size,omitempty"`

	// MaxSize filters objects by maximum size
	MaxSize *int64 `json:"max_size,omitempty"`

	// CreatedAfter filters by creation time
	CreatedAfter *time.Time `json:"created_after,omitempty"`

	// CreatedBefore filters by creation time
	CreatedBefore *time.Time `json:"created_before,omitempty"`

	// Tags filters by object tags
	Tags map[string]string `json:"tags,omitempty"`

	// StorageClass filters by storage class
	StorageClass string `json:"storage_class,omitempty"`
}

// InventoryField represents a field that can be included in inventory.
type InventoryField string

const (
	FieldBucket                    InventoryField = "Bucket"
	FieldKey                       InventoryField = "Key"
	FieldVersionID                 InventoryField = "VersionId"
	FieldIsLatest                  InventoryField = "IsLatest"
	FieldIsDeleteMarker            InventoryField = "IsDeleteMarker"
	FieldSize                      InventoryField = "Size"
	FieldLastModified              InventoryField = "LastModifiedDate"
	FieldETag                      InventoryField = "ETag"
	FieldStorageClass              InventoryField = "StorageClass"
	FieldIsMultipartUploaded       InventoryField = "IsMultipartUploaded"
	FieldReplicationStatus         InventoryField = "ReplicationStatus"
	FieldEncryptionStatus          InventoryField = "EncryptionStatus"
	FieldObjectLockRetainUntilDate InventoryField = "ObjectLockRetainUntilDate"
	FieldObjectLockMode            InventoryField = "ObjectLockMode"
	FieldObjectLockLegalHoldStatus InventoryField = "ObjectLockLegalHoldStatus"
	FieldIntelligentTieringAccess  InventoryField = "IntelligentTieringAccessTier"
	FieldBucketKeyStatus           InventoryField = "BucketKeyStatus"
	FieldChecksumAlgorithm         InventoryField = "ChecksumAlgorithm"
)

// AllInventoryFields returns all available inventory fields.
func AllInventoryFields() []InventoryField {
	return []InventoryField{
		FieldBucket,
		FieldKey,
		FieldVersionID,
		FieldIsLatest,
		FieldIsDeleteMarker,
		FieldSize,
		FieldLastModified,
		FieldETag,
		FieldStorageClass,
		FieldIsMultipartUploaded,
		FieldReplicationStatus,
		FieldEncryptionStatus,
		FieldObjectLockRetainUntilDate,
		FieldObjectLockMode,
		FieldObjectLockLegalHoldStatus,
		FieldIntelligentTieringAccess,
		FieldBucketKeyStatus,
		FieldChecksumAlgorithm,
	}
}

// InventoryRecord represents a single object in the inventory.
type InventoryRecord struct {
	// 8-byte fields (pointers, int64)
	ObjectLockRetainUntilDate *time.Time `json:"object_lock_retain_until_date,omitempty" parquet:"name=object_lock_retain_until_date, type=INT64, convertedtype=TIMESTAMP_MILLIS"`
	Size                      int64      `json:"size"                                    parquet:"name=size, type=INT64"`
	LastModified              time.Time  `json:"last_modified"                           parquet:"name=last_modified, type=INT64, convertedtype=TIMESTAMP_MILLIS"`
	// Strings
	Bucket                    string `json:"bucket"                                  parquet:"name=bucket, type=BYTE_ARRAY, convertedtype=UTF8"`
	Key                       string `json:"key"                                     parquet:"name=key, type=BYTE_ARRAY, convertedtype=UTF8"`
	VersionID                 string `json:"version_id,omitempty"                    parquet:"name=version_id, type=BYTE_ARRAY, convertedtype=UTF8"`
	ETag                      string `json:"etag"                                    parquet:"name=etag, type=BYTE_ARRAY, convertedtype=UTF8"`
	StorageClass              string `json:"storage_class"                           parquet:"name=storage_class, type=BYTE_ARRAY, convertedtype=UTF8"`
	ReplicationStatus         string `json:"replication_status,omitempty"            parquet:"name=replication_status, type=BYTE_ARRAY, convertedtype=UTF8"`
	EncryptionStatus          string `json:"encryption_status,omitempty"             parquet:"name=encryption_status, type=BYTE_ARRAY, convertedtype=UTF8"`
	ObjectLockMode            string `json:"object_lock_mode,omitempty"              parquet:"name=object_lock_mode, type=BYTE_ARRAY, convertedtype=UTF8"`
	ObjectLockLegalHoldStatus string `json:"object_lock_legal_hold_status,omitempty" parquet:"name=object_lock_legal_hold_status, type=BYTE_ARRAY, convertedtype=UTF8"`
	IntelligentTieringAccess  string `json:"intelligent_tiering_access,omitempty"    parquet:"name=intelligent_tiering_access, type=BYTE_ARRAY, convertedtype=UTF8"`
	BucketKeyStatus           string `json:"bucket_key_status,omitempty"             parquet:"name=bucket_key_status, type=BYTE_ARRAY, convertedtype=UTF8"`
	ChecksumAlgorithm         string `json:"checksum_algorithm,omitempty"            parquet:"name=checksum_algorithm, type=BYTE_ARRAY, convertedtype=UTF8"`
	// 1-byte fields (bool)
	IsLatest            bool `json:"is_latest"             parquet:"name=is_latest, type=BOOLEAN"`
	IsDeleteMarker      bool `json:"is_delete_marker"      parquet:"name=is_delete_marker, type=BOOLEAN"`
	IsMultipartUploaded bool `json:"is_multipart_uploaded" parquet:"name=is_multipart_uploaded, type=BOOLEAN"`
}

// InventoryManifest describes the inventory output.
type InventoryManifest struct {
	// SourceBucket is the inventoried bucket
	SourceBucket string `json:"sourceBucket"`

	// DestinationBucket is where files are stored
	DestinationBucket string `json:"destinationBucket"`

	// Version is the manifest format version
	Version string `json:"version"`

	// CreationTimestamp is when the inventory was generated
	CreationTimestamp time.Time `json:"creationTimestamp"`

	// FileFormat is the format of inventory files
	FileFormat string `json:"fileFormat"`

	// FileSchema describes the fields in inventory files
	FileSchema string `json:"fileSchema"`

	// Files lists the inventory data files
	Files []ManifestFile `json:"files"`
}

// ManifestFile describes a single inventory file.
type ManifestFile struct {
	// Key is the S3 key of the file
	Key string `json:"key"`

	// Size is the file size in bytes
	Size int64 `json:"size"`

	// MD5Checksum is the file checksum
	MD5Checksum string `json:"MD5checksum"`
}

// InventoryJob represents a running inventory job.
type InventoryJob struct {
	// 8-byte fields (pointers)
	// CompletedAt is when the job finished
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// StartedAt is when the job started
	StartedAt time.Time `json:"started_at"`

	// Progress tracks job progress
	Progress JobProgress `json:"progress"`

	// ID is the unique job identifier
	ID string `json:"id"`

	// ConfigID references the inventory configuration
	ConfigID string `json:"config_id"`

	// Error contains error details if failed
	Error string `json:"error,omitempty"`

	// ManifestKey is the S3 key of the manifest file
	ManifestKey string `json:"manifest_key,omitempty"`

	// Status is the current job status
	Status JobStatus `json:"status"`
}

// JobStatus represents the status of an inventory job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// JobProgress tracks inventory generation progress.
type JobProgress struct {
	TotalObjects     int64 `json:"total_objects"`
	ProcessedObjects int64 `json:"processed_objects"`
	TotalBytes       int64 `json:"total_bytes"`
	ProcessedBytes   int64 `json:"processed_bytes"`
	FilesGenerated   int   `json:"files_generated"`
}

// ObjectLister provides an interface to list objects in a bucket.
type ObjectLister interface {
	ListObjects(ctx context.Context, bucket, prefix string, recursive bool) (<-chan ObjectInfo, <-chan error)
}

// ObjectInfo contains metadata about an object.
type ObjectInfo struct {
	// 8-byte fields (pointers, maps, int64)
	ObjectLockRetainUntilDate *time.Time
	Tags                      map[string]string
	Size                      int64
	LastModified              time.Time
	// Strings
	Bucket                    string
	Key                       string
	VersionID                 string
	ETag                      string
	StorageClass              string
	ReplicationStatus         string
	EncryptionStatus          string
	ObjectLockMode            string
	ObjectLockLegalHoldStatus string
	IntelligentTieringAccess  string
	BucketKeyStatus           string
	ChecksumAlgorithm         string
	// 1-byte fields (bool)
	IsLatest            bool
	IsDeleteMarker      bool
	IsMultipartUploaded bool
}

// ObjectWriter provides an interface to write inventory files.
type ObjectWriter interface {
	PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType string) error
}

// CatalogService manages inventory generation.
type CatalogService struct {
	// 8-byte fields (interfaces, maps)
	lister      ObjectLister
	writer      ObjectWriter
	configs     map[string]*InventoryConfig
	jobs        map[string]*InventoryJob
	cancelFuncs map[string]context.CancelFunc
	// Mutexes and structs
	configMu sync.RWMutex
	jobMu    sync.RWMutex
	cancelMu sync.Mutex
	// 4-byte fields (int)
	concurrency int
	batchSize   int
}

// CatalogConfig holds configuration for the catalog service.
type CatalogConfig struct {
	Concurrency int
	BatchSize   int
}

// NewCatalogService creates a new catalog service.
func NewCatalogService(lister ObjectLister, writer ObjectWriter, cfg CatalogConfig) *CatalogService {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10000
	}

	return &CatalogService{
		lister:      lister,
		writer:      writer,
		configs:     make(map[string]*InventoryConfig),
		jobs:        make(map[string]*InventoryJob),
		cancelFuncs: make(map[string]context.CancelFunc),
		concurrency: cfg.Concurrency,
		batchSize:   cfg.BatchSize,
	}
}

// CreateInventoryConfig creates a new inventory configuration.
func (s *CatalogService) CreateInventoryConfig(cfg *InventoryConfig) error {
	if cfg.ID == "" {
		cfg.ID = uuid.New().String()
	}

	if cfg.SourceBucket == "" {
		return errors.New("source bucket is required")
	}

	if cfg.DestinationBucket == "" {
		return errors.New("destination bucket is required")
	}

	if cfg.Format == "" {
		cfg.Format = FormatCSV
	}

	if cfg.Frequency == "" {
		cfg.Frequency = FrequencyDaily
	}

	if len(cfg.IncludedFields) == 0 {
		cfg.IncludedFields = []string{
			string(FieldBucket),
			string(FieldKey),
			string(FieldSize),
			string(FieldLastModified),
			string(FieldETag),
			string(FieldStorageClass),
		}
	}

	cfg.CreatedAt = time.Now()

	s.configMu.Lock()
	s.configs[cfg.ID] = cfg
	s.configMu.Unlock()

	return nil
}

// GetInventoryConfig retrieves an inventory configuration.
func (s *CatalogService) GetInventoryConfig(id string) (*InventoryConfig, error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()

	cfg, ok := s.configs[id]
	if !ok {
		return nil, fmt.Errorf("inventory config not found: %s", id)
	}

	return cfg, nil
}

// ListInventoryConfigs lists all inventory configurations.
func (s *CatalogService) ListInventoryConfigs(bucket string) []*InventoryConfig {
	s.configMu.RLock()
	defer s.configMu.RUnlock()

	var configs []*InventoryConfig
	for _, cfg := range s.configs {
		if bucket == "" || cfg.SourceBucket == bucket {
			configs = append(configs, cfg)
		}
	}

	return configs
}

// DeleteInventoryConfig deletes an inventory configuration.
func (s *CatalogService) DeleteInventoryConfig(id string) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()

	if _, ok := s.configs[id]; !ok {
		return fmt.Errorf("inventory config not found: %s", id)
	}

	delete(s.configs, id)

	return nil
}

// StartInventoryJob starts an inventory generation job.
func (s *CatalogService) StartInventoryJob(ctx context.Context, configID string) (*InventoryJob, error) {
	cfg, err := s.GetInventoryConfig(configID)
	if err != nil {
		return nil, err
	}

	job := &InventoryJob{
		ID:        uuid.New().String(),
		ConfigID:  configID,
		Status:    JobStatusPending,
		StartedAt: time.Now(),
		Progress:  JobProgress{},
	}

	s.jobMu.Lock()
	s.jobs[job.ID] = job
	s.jobMu.Unlock()

	// Create cancellable context
	jobCtx, cancel := context.WithCancel(ctx)

	s.cancelMu.Lock()
	s.cancelFuncs[job.ID] = cancel
	s.cancelMu.Unlock()

	// Start job in background
	go s.runInventoryJob(jobCtx, job, cfg)

	return job, nil
}

// GetInventoryJob retrieves job status.
func (s *CatalogService) GetInventoryJob(jobID string) (*InventoryJob, error) {
	s.jobMu.RLock()
	defer s.jobMu.RUnlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	return job, nil
}

// ListInventoryJobs lists all inventory jobs.
func (s *CatalogService) ListInventoryJobs(configID string) []*InventoryJob {
	s.jobMu.RLock()
	defer s.jobMu.RUnlock()

	var jobs []*InventoryJob
	for _, job := range s.jobs {
		if configID == "" || job.ConfigID == configID {
			jobs = append(jobs, job)
		}
	}

	return jobs
}

// CancelInventoryJob cancels a running job.
func (s *CatalogService) CancelInventoryJob(jobID string) error {
	s.cancelMu.Lock()
	cancel, ok := s.cancelFuncs[jobID]
	s.cancelMu.Unlock()

	if ok {
		cancel()
	}

	s.jobMu.Lock()

	if job, ok := s.jobs[jobID]; ok {
		job.Status = JobStatusCancelled
		now := time.Now()
		job.CompletedAt = &now
	}

	s.jobMu.Unlock()

	return nil
}

// runInventoryJob executes the inventory generation.
func (s *CatalogService) runInventoryJob(ctx context.Context, job *InventoryJob, cfg *InventoryConfig) {
	defer func() {
		s.cancelMu.Lock()
		delete(s.cancelFuncs, job.ID)
		s.cancelMu.Unlock()
	}()

	s.updateJobStatus(job, JobStatusRunning)

	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	prefix := s.getDestinationPrefix(cfg)
	inventoryDir := fmt.Sprintf("%s/%s/data", prefix, timestamp)

	files, totalObjects, totalBytes, err := s.processInventoryObjects(ctx, job, cfg, inventoryDir)
	if err != nil {
		s.updateJobError(job, err.Error())
		return
	}

	manifestKey := fmt.Sprintf("%s/%s/manifest.json", prefix, timestamp)
	if err := s.finalizeInventoryJob(ctx, job, cfg, manifestKey, files, totalObjects, totalBytes); err != nil {
		s.updateJobError(job, err.Error())
		return
	}
}

func (s *CatalogService) getDestinationPrefix(cfg *InventoryConfig) string {
	if cfg.DestinationPrefix != "" {
		return cfg.DestinationPrefix
	}
	return cfg.SourceBucket
}

func (s *CatalogService) processInventoryObjects(ctx context.Context, job *InventoryJob, cfg *InventoryConfig, inventoryDir string) ([]ManifestFile, int64, int64, error) {
	objects, errs := s.lister.ListObjects(ctx, cfg.SourceBucket, "", true)

	var (
		records      []InventoryRecord
		files        []ManifestFile
		fileIndex    int
		totalObjects int64
		totalBytes   int64
	)

	for {
		select {
		case <-ctx.Done():
			return nil, 0, 0, errors.New("job cancelled")

		case err, ok := <-errs:
			if ok && err != nil {
				return nil, 0, 0, err
			}

		case obj, ok := <-objects:
			if !ok {
				// Channel closed, write remaining records
				if len(records) > 0 {
					file, err := s.writeInventoryFile(ctx, cfg, inventoryDir, fileIndex, records)
					if err != nil {
						return nil, 0, 0, err
					}
					files = append(files, file)
				}
				return files, totalObjects, totalBytes, nil
			}

			newRecords, newFiles, newFileIndex, err := s.processInventoryObject(ctx, job, cfg, inventoryDir, obj, records, files, fileIndex, &totalObjects, &totalBytes)
			if err != nil {
				return nil, 0, 0, err
			}
			records = newRecords
			files = newFiles
			fileIndex = newFileIndex
		}
	}
}

func (s *CatalogService) processInventoryObject(ctx context.Context, job *InventoryJob, cfg *InventoryConfig, inventoryDir string, obj ObjectInfo, records []InventoryRecord, files []ManifestFile, fileIndex int, totalObjects, totalBytes *int64) ([]InventoryRecord, []ManifestFile, int, error) {
	// Apply filter
	if !s.matchesFilter(obj, cfg.Filter) {
		return records, files, fileIndex, nil
	}

	// Convert to record
	record := s.objectToRecord(obj, cfg.IncludedFields)
	records = append(records, record)

	atomic.AddInt64(totalObjects, 1)
	atomic.AddInt64(totalBytes, obj.Size)

	// Update progress
	s.updateJobProgress(job, *totalObjects, *totalBytes, len(files))

	// Write batch if full
	if len(records) >= s.batchSize {
		file, err := s.writeInventoryFile(ctx, cfg, inventoryDir, fileIndex, records)
		if err != nil {
			return nil, nil, 0, err
		}

		files = append(files, file)
		records = records[:0]
		fileIndex++
	}

	return records, files, fileIndex, nil
}

func (s *CatalogService) finalizeInventoryJob(ctx context.Context, job *InventoryJob, cfg *InventoryConfig, manifestKey string, files []ManifestFile, totalObjects, totalBytes int64) error {
	// Write manifest
	manifest := InventoryManifest{
		SourceBucket:      cfg.SourceBucket,
		DestinationBucket: cfg.DestinationBucket,
		Version:           "2016-11-30",
		CreationTimestamp: time.Now().UTC(),
		FileFormat:        string(cfg.Format),
		FileSchema:        s.buildSchema(cfg.IncludedFields),
		Files:             files,
	}

	if err := s.writeManifest(ctx, cfg.DestinationBucket, manifestKey, manifest); err != nil {
		return err
	}

	s.updateJobCompletion(job, manifestKey, totalObjects, totalBytes, len(files))
	s.updateConfigLastRun(job, cfg)

	return nil
}

func (s *CatalogService) updateJobCompletion(job *InventoryJob, manifestKey string, totalObjects, totalBytes int64, filesCount int) {
	now := time.Now()
	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	if job.Status != JobStatusCancelled {
		job.Status = JobStatusCompleted
		job.CompletedAt = &now
		job.ManifestKey = manifestKey
		job.Progress.TotalObjects = totalObjects
		job.Progress.ProcessedObjects = totalObjects
		job.Progress.TotalBytes = totalBytes
		job.Progress.ProcessedBytes = totalBytes
		job.Progress.FilesGenerated = filesCount
	}
}

func (s *CatalogService) updateConfigLastRun(job *InventoryJob, cfg *InventoryConfig) {
	if job.Status == JobStatusCompleted {
		now := time.Now()
		s.configMu.Lock()
		cfg.LastRun = &now
		s.configMu.Unlock()
	}
}

// matchesFilter checks if an object matches the filter criteria.
func (s *CatalogService) matchesFilter(obj ObjectInfo, filter *InventoryFilter) bool {
	if filter == nil {
		return true
	}

	return s.matchesPrefixFilter(obj, filter) &&
		s.matchesSizeFilter(obj, filter) &&
		s.matchesTimeFilter(obj, filter) &&
		s.matchesStorageClassFilter(obj, filter) &&
		s.matchesTagsFilter(obj, filter)
}

// matchesPrefixFilter checks if object matches prefix filter.
func (s *CatalogService) matchesPrefixFilter(obj ObjectInfo, filter *InventoryFilter) bool {
	if filter.Prefix != "" && !hasPrefix(obj.Key, filter.Prefix) {
		return false
	}
	return true
}

// matchesSizeFilter checks if object matches size filter.
func (s *CatalogService) matchesSizeFilter(obj ObjectInfo, filter *InventoryFilter) bool {
	if filter.MinSize != nil && obj.Size < *filter.MinSize {
		return false
	}
	if filter.MaxSize != nil && obj.Size > *filter.MaxSize {
		return false
	}
	return true
}

// matchesTimeFilter checks if object matches time filter.
func (s *CatalogService) matchesTimeFilter(obj ObjectInfo, filter *InventoryFilter) bool {
	if filter.CreatedAfter != nil && obj.LastModified.Before(*filter.CreatedAfter) {
		return false
	}
	if filter.CreatedBefore != nil && obj.LastModified.After(*filter.CreatedBefore) {
		return false
	}
	return true
}

// matchesStorageClassFilter checks if object matches storage class filter.
func (s *CatalogService) matchesStorageClassFilter(obj ObjectInfo, filter *InventoryFilter) bool {
	if filter.StorageClass != "" && obj.StorageClass != filter.StorageClass {
		return false
	}
	return true
}

// matchesTagsFilter checks if object matches all required tags.
func (s *CatalogService) matchesTagsFilter(obj ObjectInfo, filter *InventoryFilter) bool {
	if len(filter.Tags) > 0 {
		for k, v := range filter.Tags {
			if obj.Tags[k] != v {
				return false
			}
		}
	}
	return true
}

// objectToRecord converts ObjectInfo to InventoryRecord.
func (s *CatalogService) objectToRecord(obj ObjectInfo, fields []string) InventoryRecord {
	return InventoryRecord{
		Bucket:                    obj.Bucket,
		Key:                       obj.Key,
		VersionID:                 obj.VersionID,
		IsLatest:                  obj.IsLatest,
		IsDeleteMarker:            obj.IsDeleteMarker,
		Size:                      obj.Size,
		LastModified:              obj.LastModified,
		ETag:                      obj.ETag,
		StorageClass:              obj.StorageClass,
		IsMultipartUploaded:       obj.IsMultipartUploaded,
		ReplicationStatus:         obj.ReplicationStatus,
		EncryptionStatus:          obj.EncryptionStatus,
		ObjectLockRetainUntilDate: obj.ObjectLockRetainUntilDate,
		ObjectLockMode:            obj.ObjectLockMode,
		ObjectLockLegalHoldStatus: obj.ObjectLockLegalHoldStatus,
		IntelligentTieringAccess:  obj.IntelligentTieringAccess,
		BucketKeyStatus:           obj.BucketKeyStatus,
		ChecksumAlgorithm:         obj.ChecksumAlgorithm,
	}
}

// writeInventoryFile writes a batch of records to an inventory file.
func (s *CatalogService) writeInventoryFile(ctx context.Context, cfg *InventoryConfig, dir string, index int, records []InventoryRecord) (ManifestFile, error) {
	var ext string

	switch cfg.Format {
	case FormatCSV:
		ext = "csv.gz"
	case FormatParquet:
		ext = "parquet"
	case FormatJSON:
		ext = "json.gz"
	case FormatORC:
		ext = "orc"
	default:
		ext = "csv.gz"
	}

	filename := fmt.Sprintf("%s/%04d.%s", dir, index, ext)

	// Create temp file
	tmpFile, err := os.CreateTemp("", "inventory-*")
	if err != nil {
		return ManifestFile{}, fmt.Errorf("failed to create temp file: %w", err)
	}

	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	var size int64

	switch cfg.Format {
	case FormatCSV:
		size, err = s.writeCSV(tmpFile, records, cfg.IncludedFields)
	case FormatJSON:
		size, err = s.writeJSON(tmpFile, records)
	case FormatParquet:
		size, err = s.writeParquet(tmpFile, records)
	default:
		size, err = s.writeCSV(tmpFile, records, cfg.IncludedFields)
	}

	if err != nil {
		return ManifestFile{}, err
	}

	// Seek to beginning for upload
	_, _ = tmpFile.Seek(0, 0)

	// Upload to destination bucket
	contentType := "application/octet-stream"
	if cfg.Format == FormatCSV {
		contentType = "text/csv"
	} else if cfg.Format == FormatJSON {
		contentType = "application/json"
	}

	err = s.writer.PutObject(ctx, cfg.DestinationBucket, filename, tmpFile, size, contentType)
	if err != nil {
		return ManifestFile{}, fmt.Errorf("failed to upload inventory file: %w", err)
	}

	return ManifestFile{
		Key:  filename,
		Size: size,
	}, nil
}

// writeCSV writes records in CSV format.
func (s *CatalogService) writeCSV(w io.Writer, records []InventoryRecord, fields []string) (int64, error) {
	cw := csv.NewWriter(w)

	// Write header
	err := cw.Write(fields)
	if err != nil {
		return 0, err
	}

	// Write records
	for _, rec := range records {
		row := s.recordToRow(rec, fields)

		err := cw.Write(row)
		if err != nil {
			return 0, err
		}
	}

	cw.Flush()

	err = cw.Error()
	if err != nil {
		return 0, err
	}

	// Get file size (approximate)
	if f, ok := w.(*os.File); ok {
		info, _ := f.Stat()
		return info.Size(), nil
	}

	return 0, nil
}

// writeJSON writes records in JSON format.
func (s *CatalogService) writeJSON(w io.Writer, records []InventoryRecord) (int64, error) {
	encoder := json.NewEncoder(w)
	for _, rec := range records {
		err := encoder.Encode(rec)
		if err != nil {
			return 0, err
		}
	}

	if f, ok := w.(*os.File); ok {
		info, _ := f.Stat()
		return info.Size(), nil
	}

	return 0, nil
}

// writeParquet writes records in Parquet format (placeholder - needs parquet library).
func (s *CatalogService) writeParquet(w io.Writer, records []InventoryRecord) (int64, error) {
	// TODO: Implement parquet writing using xitongsys/parquet-go
	// For now, fall back to JSON
	return s.writeJSON(w, records)
}

// recordToRow converts a record to a CSV row.
func (s *CatalogService) recordToRow(rec InventoryRecord, fields []string) []string {
	type fieldConverter func(InventoryRecord) string

	converters := map[InventoryField]fieldConverter{
		FieldBucket:                      func(r InventoryRecord) string { return r.Bucket },
		FieldKey:                         func(r InventoryRecord) string { return r.Key },
		FieldVersionID:                   func(r InventoryRecord) string { return r.VersionID },
		FieldIsLatest:                    func(r InventoryRecord) string { return strconv.FormatBool(r.IsLatest) },
		FieldIsDeleteMarker:              func(r InventoryRecord) string { return strconv.FormatBool(r.IsDeleteMarker) },
		FieldSize:                        func(r InventoryRecord) string { return strconv.FormatInt(r.Size, 10) },
		FieldLastModified:                func(r InventoryRecord) string { return r.LastModified.UTC().Format(time.RFC3339) },
		FieldETag:                        func(r InventoryRecord) string { return r.ETag },
		FieldStorageClass:                func(r InventoryRecord) string { return r.StorageClass },
		FieldIsMultipartUploaded:         func(r InventoryRecord) string { return strconv.FormatBool(r.IsMultipartUploaded) },
		FieldReplicationStatus:           func(r InventoryRecord) string { return r.ReplicationStatus },
		FieldEncryptionStatus:            func(r InventoryRecord) string { return r.EncryptionStatus },
		FieldObjectLockRetainUntilDate: func(r InventoryRecord) string {
			if r.ObjectLockRetainUntilDate != nil {
				return r.ObjectLockRetainUntilDate.UTC().Format(time.RFC3339)
			}
			return ""
		},
		FieldObjectLockMode:              func(r InventoryRecord) string { return r.ObjectLockMode },
		FieldObjectLockLegalHoldStatus:   func(r InventoryRecord) string { return r.ObjectLockLegalHoldStatus },
		FieldIntelligentTieringAccess:    func(r InventoryRecord) string { return r.IntelligentTieringAccess },
		FieldBucketKeyStatus:             func(r InventoryRecord) string { return r.BucketKeyStatus },
		FieldChecksumAlgorithm:           func(r InventoryRecord) string { return r.ChecksumAlgorithm },
	}

	row := make([]string, len(fields))
	for i, field := range fields {
		if converter, ok := converters[InventoryField(field)]; ok {
			row[i] = converter(rec)
		}
	}

	return row
}

// buildSchema builds the schema string for the manifest.
func (s *CatalogService) buildSchema(fields []string) string {
	return fmt.Sprintf("%v", fields)
}

// writeManifest writes the inventory manifest file.
func (s *CatalogService) writeManifest(ctx context.Context, bucket, key string, manifest InventoryManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "manifest-*")
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	_, err = tmpFile.Write(data)
	if err != nil {
		return err
	}

	_, _ = tmpFile.Seek(0, 0)

	return s.writer.PutObject(ctx, bucket, key, tmpFile, int64(len(data)), "application/json")
}

// updateJobStatus updates the job status.
func (s *CatalogService) updateJobStatus(job *InventoryJob, status JobStatus) {
	s.jobMu.Lock()

	job.Status = status

	s.jobMu.Unlock()
}

// updateJobProgress updates the job progress.
func (s *CatalogService) updateJobProgress(job *InventoryJob, objects, bytes int64, files int) {
	s.jobMu.Lock()

	job.Progress.ProcessedObjects = objects
	job.Progress.ProcessedBytes = bytes
	job.Progress.FilesGenerated = files

	s.jobMu.Unlock()
}

// updateJobError sets the job to failed with an error
// If the job was already cancelled, it preserves the cancelled status.
func (s *CatalogService) updateJobError(job *InventoryJob, errMsg string) {
	s.jobMu.Lock()
	// Don't overwrite cancelled status - CancelInventoryJob already set it
	if job.Status != JobStatusCancelled {
		job.Status = JobStatusFailed
		job.Error = errMsg
	}

	now := time.Now()
	job.CompletedAt = &now

	s.jobMu.Unlock()
}

// hasPrefix checks if a string has a given prefix.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// GetInventoryManifest retrieves and parses an inventory manifest.
func (s *CatalogService) GetInventoryManifest(ctx context.Context, bucket, key string) (*InventoryManifest, error) {
	// This would need to read from object storage
	// For now, return an error indicating implementation needed
	return nil, errors.New("not implemented: requires object reader interface")
}

// QueryInventory queries inventory data using SQL (for S3 Select integration).
func (s *CatalogService) QueryInventory(ctx context.Context, manifestKey string, query string) (<-chan InventoryRecord, <-chan error) {
	records := make(chan InventoryRecord)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		errs <- errors.New("not implemented: requires S3 Select integration")
	}()

	return records, errs
}

// AnalyzeInventory provides analytics on inventory data.
type InventoryAnalytics struct {
	// 8-byte fields (maps, int64)
	ByStorageClass   map[string]int64 `json:"by_storage_class"`
	BySizeRange      map[string]int64 `json:"by_size_range"`
	ByLastModified   map[string]int64 `json:"by_last_modified"`
	TotalObjects     int64            `json:"total_objects"`
	TotalSize        int64            `json:"total_size"`
	EncryptedObjects int64            `json:"encrypted_objects"`
	VersionedObjects int64            `json:"versioned_objects"`
	DeleteMarkers    int64            `json:"delete_markers"`
}

// AnalyzeInventory generates analytics from inventory files.
func (s *CatalogService) AnalyzeInventory(ctx context.Context, bucket, manifestKey string) (*InventoryAnalytics, error) {
	analytics := &InventoryAnalytics{
		ByStorageClass: make(map[string]int64),
		BySizeRange:    make(map[string]int64),
		ByLastModified: make(map[string]int64),
	}

	// This would read and analyze inventory files
	// Placeholder for now
	return analytics, errors.New("not implemented: requires inventory file reading")
}

// GetBucketInventoryConfiguration implements S3 API GetBucketInventoryConfiguration.
func (s *CatalogService) GetBucketInventoryConfiguration(bucket, id string) (*InventoryConfig, error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()

	for _, cfg := range s.configs {
		if cfg.SourceBucket == bucket && cfg.ID == id {
			return cfg, nil
		}
	}

	return nil, errors.New("inventory configuration not found")
}

// PutBucketInventoryConfiguration implements S3 API PutBucketInventoryConfiguration.
func (s *CatalogService) PutBucketInventoryConfiguration(cfg *InventoryConfig) error {
	return s.CreateInventoryConfig(cfg)
}

// DeleteBucketInventoryConfiguration implements S3 API DeleteBucketInventoryConfiguration.
func (s *CatalogService) DeleteBucketInventoryConfiguration(bucket, id string) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()

	for cfgID, cfg := range s.configs {
		if cfg.SourceBucket == bucket && cfg.ID == id {
			delete(s.configs, cfgID)
			return nil
		}
	}

	return errors.New("inventory configuration not found")
}

// ListBucketInventoryConfigurations implements S3 API ListBucketInventoryConfigurations.
func (s *CatalogService) ListBucketInventoryConfigurations(bucket string) []*InventoryConfig {
	return s.ListInventoryConfigs(bucket)
}

// _ensureDir ensures directory exists with secure permissions (reserved for future use).
func _ensureDir(dir string) error {
	return os.MkdirAll(dir, 0750)
}

// GetTempDir returns a temp directory for inventory operations.
func GetTempDir() string {
	return filepath.Join(os.TempDir(), "nebulaio-inventory")
}
