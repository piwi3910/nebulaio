// Package backup provides backup and restore functionality with point-in-time recovery.
package backup

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// BackupType represents the type of backup.
type BackupType string

const (
	BackupTypeFull        BackupType = "full"
	BackupTypeIncremental BackupType = "incremental"
	BackupTypeDifferential BackupType = "differential"
)

// BackupStatus represents the status of a backup.
type BackupStatus string

const (
	BackupStatusPending    BackupStatus = "pending"
	BackupStatusInProgress BackupStatus = "in_progress"
	BackupStatusCompleted  BackupStatus = "completed"
	BackupStatusFailed     BackupStatus = "failed"
	BackupStatusCancelled  BackupStatus = "cancelled"
)

// RestoreStatus represents the status of a restore operation.
type RestoreStatus string

const (
	RestoreStatusPending    RestoreStatus = "pending"
	RestoreStatusInProgress RestoreStatus = "in_progress"
	RestoreStatusCompleted  RestoreStatus = "completed"
	RestoreStatusFailed     RestoreStatus = "failed"
	RestoreStatusCancelled  RestoreStatus = "cancelled"
)

// BackupMetadata contains metadata about a backup.
type BackupMetadata struct {
	ID              string            `json:"id"`
	Type            BackupType        `json:"type"`
	Status          BackupStatus      `json:"status"`
	Buckets         []string          `json:"buckets"`
	ParentBackupID  string            `json:"parentBackupId,omitempty"`
	StartTime       time.Time         `json:"startTime"`
	EndTime         time.Time         `json:"endTime,omitempty"`
	PointInTime     time.Time         `json:"pointInTime"`
	ObjectCount     int64             `json:"objectCount"`
	TotalSize       int64             `json:"totalSize"`
	CompressedSize  int64             `json:"compressedSize"`
	Checksum        string            `json:"checksum"`
	Error           string            `json:"error,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
	RetentionDays   int               `json:"retentionDays"`
	EncryptionKeyID string            `json:"encryptionKeyId,omitempty"`
	Location        string            `json:"location"`
}

// RestoreMetadata contains metadata about a restore operation.
type RestoreMetadata struct {
	ID              string        `json:"id"`
	BackupID        string        `json:"backupId"`
	Status          RestoreStatus `json:"status"`
	TargetBuckets   []string      `json:"targetBuckets"`
	PointInTime     time.Time     `json:"pointInTime,omitempty"`
	StartTime       time.Time     `json:"startTime"`
	EndTime         time.Time     `json:"endTime,omitempty"`
	ObjectsRestored int64         `json:"objectsRestored"`
	BytesRestored   int64         `json:"bytesRestored"`
	Error           string        `json:"error,omitempty"`
	Overwrite       bool          `json:"overwrite"`
}

// WALEntry represents a write-ahead log entry for point-in-time recovery.
type WALEntry struct {
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Operation   string    `json:"operation"` // PUT, DELETE, COPY
	Bucket      string    `json:"bucket"`
	Key         string    `json:"key"`
	VersionID   string    `json:"versionId,omitempty"`
	Size        int64     `json:"size,omitempty"`
	ETag        string    `json:"etag,omitempty"`
	ContentType string    `json:"contentType,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	DataPath    string    `json:"dataPath,omitempty"`
}

// BackupConfig contains backup configuration.
type BackupConfig struct {
	// Backup destination
	DestinationPath string

	// Compression settings
	EnableCompression bool
	CompressionLevel  int

	// Encryption settings
	EnableEncryption bool
	EncryptionKeyID  string

	// Retention settings
	RetentionDays int
	MaxBackups    int

	// Performance settings
	Concurrency   int
	ChunkSize     int64

	// WAL settings
	WALPath       string
	WALSyncInterval time.Duration
	WALRetention  time.Duration
}

// BackupManager manages backup and restore operations.
type BackupManager struct {
	mu            sync.RWMutex
	config        *BackupConfig
	storage       BackupStorage
	wal           *WALManager
	backups       map[string]*BackupMetadata
	restores      map[string]*RestoreMetadata
	activeBackup  *BackupJob
	activeRestore *RestoreJob
}

// BackupStorage interface for storage operations.
type BackupStorage interface {
	ListBuckets(ctx context.Context) ([]string, error)
	ListObjects(ctx context.Context, bucket, prefix string) ([]*ObjectInfo, error)
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *ObjectInfo, error)
	GetObjectVersion(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, *ObjectInfo, error)
	PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, opts *PutObjectOptions) error
	DeleteObject(ctx context.Context, bucket, key string) error
	CreateBucket(ctx context.Context, bucket string) error
	BucketExists(ctx context.Context, bucket string) (bool, error)
}

// ObjectInfo contains object metadata.
type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	VersionID    string
	Metadata     map[string]string
	IsLatest     bool
	DeleteMarker bool
}

// PutObjectOptions contains options for PutObject.
type PutObjectOptions struct {
	ContentType string
	Metadata    map[string]string
}

// WALManager manages write-ahead log for PITR.
type WALManager struct {
	mu          sync.Mutex
	path        string
	entries     []*WALEntry
	file        *os.File
	encoder     *json.Encoder
	syncTicker  *time.Ticker
	retention   time.Duration
	lastSync    time.Time
}

// BackupJob represents an active backup operation.
type BackupJob struct {
	metadata       *BackupMetadata
	ctx            context.Context
	cancel         context.CancelFunc
	_progress      int64
	_totalObjects  int64
	_errors        []error
	_mu            sync.Mutex
}

// RestoreJob represents an active restore operation.
type RestoreJob struct {
	metadata       *RestoreMetadata
	ctx            context.Context
	cancel         context.CancelFunc
	_progress      int64
	_totalObjects  int64
	_errors        []error
	_mu            sync.Mutex
}

// NewBackupManager creates a new backup manager.
func NewBackupManager(config *BackupConfig, storage BackupStorage) (*BackupManager, error) {
	// Create destination directory
	if err := os.MkdirAll(config.DestinationPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Initialize WAL manager
	wal, err := NewWALManager(config.WALPath, config.WALSyncInterval, config.WALRetention)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize WAL: %w", err)
	}

	bm := &BackupManager{
		config:   config,
		storage:  storage,
		wal:      wal,
		backups:  make(map[string]*BackupMetadata),
		restores: make(map[string]*RestoreMetadata),
	}

	// Load existing backup metadata
	if err := bm.loadMetadata(); err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	return bm, nil
}

// NewWALManager creates a new WAL manager.
func NewWALManager(path string, syncInterval, retention time.Duration) (*WALManager, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	walFile := filepath.Join(path, "wal.log")
	file, err := os.OpenFile(walFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	wm := &WALManager{
		path:       path,
		file:       file,
		encoder:    json.NewEncoder(file),
		syncTicker: time.NewTicker(syncInterval),
		retention:  retention,
		entries:    make([]*WALEntry, 0),
	}

	// Start sync goroutine
	go wm.syncLoop()

	return wm, nil
}

// WriteEntry writes an entry to the WAL.
func (wm *WALManager) WriteEntry(entry *WALEntry) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	entry.ID = uuid.New().String()
	entry.Timestamp = time.Now()

	if err := wm.encoder.Encode(entry); err != nil {
		return fmt.Errorf("failed to write WAL entry: %w", err)
	}

	wm.entries = append(wm.entries, entry)
	return nil
}

// GetEntriesSince returns all entries since the given time.
func (wm *WALManager) GetEntriesSince(t time.Time) ([]*WALEntry, error) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	var result []*WALEntry
	for _, entry := range wm.entries {
		if entry.Timestamp.After(t) || entry.Timestamp.Equal(t) {
			result = append(result, entry)
		}
	}
	return result, nil
}

// GetEntriesUntil returns all entries until the given time.
func (wm *WALManager) GetEntriesUntil(t time.Time) ([]*WALEntry, error) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	var result []*WALEntry
	for _, entry := range wm.entries {
		if entry.Timestamp.Before(t) || entry.Timestamp.Equal(t) {
			result = append(result, entry)
		}
	}
	return result, nil
}

// syncLoop syncs the WAL file periodically.
func (wm *WALManager) syncLoop() {
	for range wm.syncTicker.C {
		wm.mu.Lock()
		if err := wm.file.Sync(); err != nil {
			log.Error().
				Err(err).
				Msg("failed to sync WAL file - data durability may be compromised")
		}
		wm.lastSync = time.Now()

		// Cleanup old entries
		cutoff := time.Now().Add(-wm.retention)
		newEntries := make([]*WALEntry, 0)
		for _, entry := range wm.entries {
			if entry.Timestamp.After(cutoff) {
				newEntries = append(newEntries, entry)
			}
		}
		wm.entries = newEntries
		wm.mu.Unlock()
	}
}

// Close closes the WAL manager.
func (wm *WALManager) Close() error {
	wm.syncTicker.Stop()
	wm.mu.Lock()
	defer wm.mu.Unlock()
	return wm.file.Close()
}

// loadMetadata loads existing backup metadata.
func (bm *BackupManager) loadMetadata() error {
	metadataPath := filepath.Join(bm.config.DestinationPath, "metadata.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var backups []*BackupMetadata
	if err := json.Unmarshal(data, &backups); err != nil {
		return err
	}

	for _, b := range backups {
		bm.backups[b.ID] = b
	}

	return nil
}

// saveMetadata saves backup metadata.
func (bm *BackupManager) saveMetadata() error {
	bm.mu.RLock()
	backups := make([]*BackupMetadata, 0, len(bm.backups))
	for _, b := range bm.backups {
		backups = append(backups, b)
	}
	bm.mu.RUnlock()

	data, err := json.MarshalIndent(backups, "", "  ")
	if err != nil {
		return err
	}

	metadataPath := filepath.Join(bm.config.DestinationPath, "metadata.json")
	return os.WriteFile(metadataPath, data, 0644)
}

// CreateBackup creates a new backup.
func (bm *BackupManager) CreateBackup(ctx context.Context, backupType BackupType, buckets []string, tags map[string]string) (*BackupMetadata, error) {
	bm.mu.Lock()
	if bm.activeBackup != nil {
		bm.mu.Unlock()
		return nil, fmt.Errorf("backup already in progress")
	}

	metadata := &BackupMetadata{
		ID:            uuid.New().String(),
		Type:          backupType,
		Status:        BackupStatusPending,
		Buckets:       buckets,
		StartTime:     time.Now(),
		PointInTime:   time.Now(),
		Tags:          tags,
		RetentionDays: bm.config.RetentionDays,
		Location:      filepath.Join(bm.config.DestinationPath, fmt.Sprintf("backup-%s", time.Now().Format("20060102-150405"))),
	}

	// For incremental backup, find parent
	if backupType == BackupTypeIncremental {
		parent := bm.findLatestFullBackup(buckets)
		if parent == nil {
			// Fall back to full backup
			metadata.Type = BackupTypeFull
		} else {
			metadata.ParentBackupID = parent.ID
		}
	}

	jobCtx, cancel := context.WithCancel(ctx)
	job := &BackupJob{
		metadata: metadata,
		ctx:      jobCtx,
		cancel:   cancel,
	}
	bm.activeBackup = job
	bm.backups[metadata.ID] = metadata
	bm.mu.Unlock()

	// Start backup in goroutine
	go bm.runBackup(job)

	return metadata, nil
}

// findLatestFullBackup finds the latest completed full backup for the given buckets.
func (bm *BackupManager) findLatestFullBackup(buckets []string) *BackupMetadata {
	var latest *BackupMetadata
	for _, b := range bm.backups {
		if b.Type != BackupTypeFull || b.Status != BackupStatusCompleted {
			continue
		}
		if !bm.bucketsMatch(b.Buckets, buckets) {
			continue
		}
		if latest == nil || b.StartTime.After(latest.StartTime) {
			latest = b
		}
	}
	return latest
}

// bucketsMatch checks if two bucket lists match.
func (bm *BackupManager) bucketsMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runBackup runs the backup operation.
func (bm *BackupManager) runBackup(job *BackupJob) {
	defer func() {
		bm.mu.Lock()
		bm.activeBackup = nil
		bm.mu.Unlock()
		_ = bm.saveMetadata()
	}()

	job.metadata.Status = BackupStatusInProgress

	// Create backup directory
	if err := os.MkdirAll(job.metadata.Location, 0755); err != nil {
		job.metadata.Status = BackupStatusFailed
		job.metadata.Error = err.Error()
		return
	}

	var totalSize int64
	var objectCount int64
	var compressedSize int64
	hasher := sha256.New()

	// Backup each bucket
	for _, bucket := range job.metadata.Buckets {
		if err := bm.backupBucket(job, bucket, &objectCount, &totalSize, &compressedSize, hasher); err != nil {
			if job.ctx.Err() != nil {
				job.metadata.Status = BackupStatusCancelled
				return
			}
			job.metadata.Status = BackupStatusFailed
			job.metadata.Error = err.Error()
			return
		}
	}

	job.metadata.EndTime = time.Now()
	job.metadata.Status = BackupStatusCompleted
	job.metadata.ObjectCount = objectCount
	job.metadata.TotalSize = totalSize
	job.metadata.CompressedSize = compressedSize
	job.metadata.Checksum = hex.EncodeToString(hasher.Sum(nil))

	// Apply retention policy
	bm.applyRetentionPolicy()
}

// backupBucket backs up a single bucket.
func (bm *BackupManager) backupBucket(job *BackupJob, bucket string, objectCount, totalSize, compressedSize *int64, hasher io.Writer) error {
	// List all objects
	objects, err := bm.storage.ListObjects(job.ctx, bucket, "")
	if err != nil {
		return fmt.Errorf("failed to list objects in bucket %s: %w", bucket, err)
	}

	// Create bucket directory in backup
	bucketPath := filepath.Join(job.metadata.Location, bucket)
	if err := os.MkdirAll(bucketPath, 0755); err != nil {
		return fmt.Errorf("failed to create bucket directory: %w", err)
	}

	// Use semaphore for concurrency control
	sem := make(chan struct{}, bm.config.Concurrency)
	var wg sync.WaitGroup
	var backupErr error
	var errMu sync.Mutex

	for _, obj := range objects {
		select {
		case <-job.ctx.Done():
			return job.ctx.Err()
		default:
		}

		// For incremental backup, check if object changed since parent
		if job.metadata.Type == BackupTypeIncremental && job.metadata.ParentBackupID != "" {
			parent := bm.backups[job.metadata.ParentBackupID]
			if parent != nil && obj.LastModified.Before(parent.PointInTime) {
				continue
			}
		}

		wg.Add(1)
		go func(o *ObjectInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := bm.backupObject(job, bucket, o, hasher); err != nil {
				errMu.Lock()
				if backupErr == nil {
					backupErr = err
				}
				errMu.Unlock()
				return
			}

			atomic.AddInt64(objectCount, 1)
			atomic.AddInt64(totalSize, o.Size)
		}(obj)
	}

	wg.Wait()
	return backupErr
}

// backupObject backs up a single object.
func (bm *BackupManager) backupObject(job *BackupJob, bucket string, obj *ObjectInfo, hasher io.Writer) error {
	reader, _, err := bm.storage.GetObject(job.ctx, bucket, obj.Key)
	if err != nil {
		return fmt.Errorf("failed to get object %s/%s: %w", bucket, obj.Key, err)
	}
	defer func() { _ = reader.Close() }()

	// Create object path
	objectPath := filepath.Join(job.metadata.Location, bucket, obj.Key)
	objectDir := filepath.Dir(objectPath)
	if err := os.MkdirAll(objectDir, 0755); err != nil {
		return fmt.Errorf("failed to create object directory: %w", err)
	}

	// Create output file
	var file *os.File
	var writer io.Writer

	if bm.config.EnableCompression {
		objectPath += ".gz"
		file, err = os.Create(objectPath)
		if err != nil {
			return fmt.Errorf("failed to create backup file: %w", err)
		}
		defer func() { _ = file.Close() }()

		gzWriter, err := gzip.NewWriterLevel(file, bm.config.CompressionLevel)
		if err != nil {
			return fmt.Errorf("failed to create gzip writer: %w", err)
		}
		defer func() { _ = gzWriter.Close() }()
		writer = io.MultiWriter(gzWriter, hasher)
	} else {
		file, err = os.Create(objectPath)
		if err != nil {
			return fmt.Errorf("failed to create backup file: %w", err)
		}
		defer func() { _ = file.Close() }()
		writer = io.MultiWriter(file, hasher)
	}

	// Copy object data
	if _, err := io.Copy(writer, reader); err != nil {
		return fmt.Errorf("failed to copy object data: %w", err)
	}

	// Save object metadata
	metadataPath := objectPath + ".meta"
	metadataData, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := os.WriteFile(metadataPath, metadataData, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// CancelBackup cancels the active backup.
func (bm *BackupManager) CancelBackup() error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.activeBackup == nil {
		return fmt.Errorf("no active backup")
	}

	bm.activeBackup.cancel()
	return nil
}

// GetBackup returns backup metadata by ID.
func (bm *BackupManager) GetBackup(id string) (*BackupMetadata, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	backup, exists := bm.backups[id]
	if !exists {
		return nil, fmt.Errorf("backup not found: %s", id)
	}
	return backup, nil
}

// ListBackups returns all backups.
func (bm *BackupManager) ListBackups() []*BackupMetadata {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	backups := make([]*BackupMetadata, 0, len(bm.backups))
	for _, b := range bm.backups {
		backups = append(backups, b)
	}

	// Sort by start time descending
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].StartTime.After(backups[j].StartTime)
	})

	return backups
}

// DeleteBackup deletes a backup.
func (bm *BackupManager) DeleteBackup(id string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	backup, exists := bm.backups[id]
	if !exists {
		return fmt.Errorf("backup not found: %s", id)
	}

	// Check if this is a parent of other backups
	for _, b := range bm.backups {
		if b.ParentBackupID == id {
			return fmt.Errorf("backup is parent of %s", b.ID)
		}
	}

	// Delete backup files
	if err := os.RemoveAll(backup.Location); err != nil {
		return fmt.Errorf("failed to delete backup files: %w", err)
	}

	delete(bm.backups, id)
	return bm.saveMetadata()
}

// Restore restores from a backup.
func (bm *BackupManager) Restore(ctx context.Context, backupID string, targetBuckets []string, overwrite bool) (*RestoreMetadata, error) {
	bm.mu.Lock()
	if bm.activeRestore != nil {
		bm.mu.Unlock()
		return nil, fmt.Errorf("restore already in progress")
	}

	backup, exists := bm.backups[backupID]
	if !exists {
		bm.mu.Unlock()
		return nil, fmt.Errorf("backup not found: %s", backupID)
	}

	if backup.Status != BackupStatusCompleted {
		bm.mu.Unlock()
		return nil, fmt.Errorf("backup is not completed")
	}

	metadata := &RestoreMetadata{
		ID:            uuid.New().String(),
		BackupID:      backupID,
		Status:        RestoreStatusPending,
		TargetBuckets: targetBuckets,
		StartTime:     time.Now(),
		Overwrite:     overwrite,
	}

	jobCtx, cancel := context.WithCancel(ctx)
	job := &RestoreJob{
		metadata: metadata,
		ctx:      jobCtx,
		cancel:   cancel,
	}
	bm.activeRestore = job
	bm.restores[metadata.ID] = metadata
	bm.mu.Unlock()

	// Start restore in goroutine
	go bm.runRestore(job, backup)

	return metadata, nil
}

// RestorePointInTime restores to a specific point in time.
func (bm *BackupManager) RestorePointInTime(ctx context.Context, targetTime time.Time, buckets []string, targetBuckets []string) (*RestoreMetadata, error) {
	// Find the latest backup before target time
	var baseBackup *BackupMetadata
	for _, b := range bm.backups {
		if b.Status != BackupStatusCompleted {
			continue
		}
		if b.PointInTime.After(targetTime) {
			continue
		}
		if !bm.bucketsMatch(b.Buckets, buckets) {
			continue
		}
		if baseBackup == nil || b.PointInTime.After(baseBackup.PointInTime) {
			baseBackup = b
		}
	}

	if baseBackup == nil {
		return nil, fmt.Errorf("no backup found before %s", targetTime)
	}

	// Create restore with PITR
	metadata := &RestoreMetadata{
		ID:            uuid.New().String(),
		BackupID:      baseBackup.ID,
		Status:        RestoreStatusPending,
		TargetBuckets: targetBuckets,
		PointInTime:   targetTime,
		StartTime:     time.Now(),
	}

	bm.mu.Lock()
	if bm.activeRestore != nil {
		bm.mu.Unlock()
		return nil, fmt.Errorf("restore already in progress")
	}

	jobCtx, cancel := context.WithCancel(ctx)
	job := &RestoreJob{
		metadata: metadata,
		ctx:      jobCtx,
		cancel:   cancel,
	}
	bm.activeRestore = job
	bm.restores[metadata.ID] = metadata
	bm.mu.Unlock()

	// Start PITR restore in goroutine
	go bm.runPITRRestore(job, baseBackup)

	return metadata, nil
}

// runRestore runs the restore operation.
func (bm *BackupManager) runRestore(job *RestoreJob, backup *BackupMetadata) {
	defer func() {
		bm.mu.Lock()
		bm.activeRestore = nil
		bm.mu.Unlock()
	}()

	job.metadata.Status = RestoreStatusInProgress

	// Build chain of backups for incremental restore
	backupChain := bm.buildBackupChain(backup)

	// Ensure target buckets exist
	for _, bucket := range job.metadata.TargetBuckets {
		exists, err := bm.storage.BucketExists(job.ctx, bucket)
		if err != nil {
			job.metadata.Status = RestoreStatusFailed
			job.metadata.Error = err.Error()
			return
		}
		if !exists {
			if err := bm.storage.CreateBucket(job.ctx, bucket); err != nil {
				job.metadata.Status = RestoreStatusFailed
				job.metadata.Error = err.Error()
				return
			}
		}
	}

	// Restore from each backup in chain
	for _, b := range backupChain {
		if err := bm.restoreFromBackup(job, b); err != nil {
			if job.ctx.Err() != nil {
				job.metadata.Status = RestoreStatusCancelled
				return
			}
			job.metadata.Status = RestoreStatusFailed
			job.metadata.Error = err.Error()
			return
		}
	}

	job.metadata.EndTime = time.Now()
	job.metadata.Status = RestoreStatusCompleted
}

// runPITRRestore runs a point-in-time restore.
func (bm *BackupManager) runPITRRestore(job *RestoreJob, baseBackup *BackupMetadata) {
	defer func() {
		bm.mu.Lock()
		bm.activeRestore = nil
		bm.mu.Unlock()
	}()

	job.metadata.Status = RestoreStatusInProgress

	// First, restore from base backup
	if err := bm.restoreFromBackup(job, baseBackup); err != nil {
		job.metadata.Status = RestoreStatusFailed
		job.metadata.Error = err.Error()
		return
	}

	// Then apply WAL entries up to point in time
	entries, err := bm.wal.GetEntriesUntil(job.metadata.PointInTime)
	if err != nil {
		job.metadata.Status = RestoreStatusFailed
		job.metadata.Error = err.Error()
		return
	}

	// Filter entries for relevant buckets and after base backup
	for _, entry := range entries {
		if entry.Timestamp.Before(baseBackup.PointInTime) {
			continue
		}

		targetBucket := bm.getTargetBucket(entry.Bucket, baseBackup.Buckets, job.metadata.TargetBuckets)
		if targetBucket == "" {
			continue
		}

		if err := bm.applyWALEntry(job.ctx, entry, targetBucket); err != nil {
			job.metadata.Status = RestoreStatusFailed
			job.metadata.Error = err.Error()
			return
		}

		atomic.AddInt64(&job.metadata.ObjectsRestored, 1)
	}

	job.metadata.EndTime = time.Now()
	job.metadata.Status = RestoreStatusCompleted
}

// buildBackupChain builds the chain of backups from full to current.
func (bm *BackupManager) buildBackupChain(backup *BackupMetadata) []*BackupMetadata {
	chain := []*BackupMetadata{backup}

	current := backup
	for current.ParentBackupID != "" {
		parent, exists := bm.backups[current.ParentBackupID]
		if !exists {
			break
		}
		chain = append([]*BackupMetadata{parent}, chain...)
		current = parent
	}

	return chain
}

// restoreFromBackup restores objects from a backup.
func (bm *BackupManager) restoreFromBackup(job *RestoreJob, backup *BackupMetadata) error {
	for i, bucket := range backup.Buckets {
		targetBucket := bucket
		if i < len(job.metadata.TargetBuckets) {
			targetBucket = job.metadata.TargetBuckets[i]
		}

		bucketPath := filepath.Join(backup.Location, bucket)
		if err := bm.restoreBucket(job, bucketPath, targetBucket); err != nil {
			return err
		}
	}
	return nil
}

// restoreBucket restores a single bucket.
func (bm *BackupManager) restoreBucket(job *RestoreJob, bucketPath, targetBucket string) error {
	return filepath.Walk(bucketPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Skip metadata files
		if strings.HasSuffix(path, ".meta") {
			return nil
		}

		select {
		case <-job.ctx.Done():
			return job.ctx.Err()
		default:
		}

		// Calculate object key
		relPath, _ := filepath.Rel(bucketPath, path)
		key := strings.TrimSuffix(relPath, ".gz")

		// Load metadata
		var metaPath string
		if !strings.HasSuffix(path, ".gz") {
			metaPath = path + ".meta"
		} else {
			metaPath = strings.TrimSuffix(path, ".gz") + ".gz.meta"
		}

		var objMeta ObjectInfo
		metaData, err := os.ReadFile(metaPath)
		if err == nil {
			_ = json.Unmarshal(metaData, &objMeta)
		}

		// Open backup file
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open backup file: %w", err)
		}
		defer func() { _ = file.Close() }()

		var reader io.Reader = file
		size := info.Size()

		// Decompress if needed
		if strings.HasSuffix(path, ".gz") {
			gzReader, err := gzip.NewReader(file)
			if err != nil {
				return fmt.Errorf("failed to create gzip reader: %w", err)
			}
			defer func() { _ = gzReader.Close() }()
			reader = gzReader
			size = objMeta.Size
		}

		// Restore object
		opts := &PutObjectOptions{
			ContentType: objMeta.ContentType,
			Metadata:    objMeta.Metadata,
		}

		if err := bm.storage.PutObject(job.ctx, targetBucket, key, reader, size, opts); err != nil {
			return fmt.Errorf("failed to restore object: %w", err)
		}

		atomic.AddInt64(&job.metadata.ObjectsRestored, 1)
		atomic.AddInt64(&job.metadata.BytesRestored, size)

		return nil
	})
}

// applyWALEntry applies a WAL entry during PITR.
func (bm *BackupManager) applyWALEntry(ctx context.Context, entry *WALEntry, targetBucket string) error {
	switch entry.Operation {
	case "PUT":
		// For PUT operations, we need the data from WAL data path
		if entry.DataPath == "" {
			return nil // Skip if no data path
		}
		file, err := os.Open(entry.DataPath)
		if err != nil {
			return fmt.Errorf("failed to open WAL data: %w", err)
		}
		defer func() { _ = file.Close() }()

		opts := &PutObjectOptions{
			ContentType: entry.ContentType,
			Metadata:    entry.Metadata,
		}

		return bm.storage.PutObject(ctx, targetBucket, entry.Key, file, entry.Size, opts)

	case "DELETE":
		return bm.storage.DeleteObject(ctx, targetBucket, entry.Key)

	default:
		return nil
	}
}

// getTargetBucket maps source bucket to target bucket.
func (bm *BackupManager) getTargetBucket(sourceBucket string, sourceBuckets, targetBuckets []string) string {
	for i, b := range sourceBuckets {
		if b == sourceBucket && i < len(targetBuckets) {
			return targetBuckets[i]
		}
	}
	return ""
}

// CancelRestore cancels the active restore.
func (bm *BackupManager) CancelRestore() error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.activeRestore == nil {
		return fmt.Errorf("no active restore")
	}

	bm.activeRestore.cancel()
	return nil
}

// GetRestore returns restore metadata by ID.
func (bm *BackupManager) GetRestore(id string) (*RestoreMetadata, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	restore, exists := bm.restores[id]
	if !exists {
		return nil, fmt.Errorf("restore not found: %s", id)
	}
	return restore, nil
}

// applyRetentionPolicy removes old backups based on retention settings.
func (bm *BackupManager) applyRetentionPolicy() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -bm.config.RetentionDays)

	var toDelete []string
	for id, backup := range bm.backups {
		if backup.Status != BackupStatusCompleted {
			continue
		}
		if backup.StartTime.Before(cutoff) {
			// Check if this is a parent of another backup
			isParent := false
			for _, b := range bm.backups {
				if b.ParentBackupID == id {
					isParent = true
					break
				}
			}
			if !isParent {
				toDelete = append(toDelete, id)
			}
		}
	}

	// Also apply max backups limit
	if bm.config.MaxBackups > 0 && len(bm.backups)-len(toDelete) > bm.config.MaxBackups {
		// Sort by age and mark oldest for deletion
		var backups []*BackupMetadata
		for _, b := range bm.backups {
			backups = append(backups, b)
		}
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].StartTime.Before(backups[j].StartTime)
		})

		for _, b := range backups {
			if len(bm.backups)-len(toDelete) <= bm.config.MaxBackups {
				break
			}
			if b.Status != BackupStatusCompleted {
				continue
			}
			// Check if already marked or is parent
			found := false
			for _, id := range toDelete {
				if id == b.ID {
					found = true
					break
				}
			}
			if found {
				continue
			}
			isParent := false
			for _, other := range bm.backups {
				if other.ParentBackupID == b.ID {
					isParent = true
					break
				}
			}
			if !isParent {
				toDelete = append(toDelete, b.ID)
			}
		}
	}

	// Delete marked backups
	for _, id := range toDelete {
		backup := bm.backups[id]
		_ = os.RemoveAll(backup.Location)
		delete(bm.backups, id)
	}
}

// RecordOperation records an operation in the WAL.
func (bm *BackupManager) RecordOperation(operation, bucket, key string, info *ObjectInfo) error {
	entry := &WALEntry{
		Operation: operation,
		Bucket:    bucket,
		Key:       key,
	}

	if info != nil {
		entry.VersionID = info.VersionID
		entry.Size = info.Size
		entry.ETag = info.ETag
		entry.ContentType = info.ContentType
		entry.Metadata = info.Metadata
	}

	return bm.wal.WriteEntry(entry)
}

// Close closes the backup manager.
func (bm *BackupManager) Close() error {
	return bm.wal.Close()
}
