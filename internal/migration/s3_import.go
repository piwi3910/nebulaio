// Package migration provides tools for migrating data from other S3-compatible systems.
package migration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/piwi3910/nebulaio/internal/httputil"
	"github.com/rs/zerolog/log"
)

// File permission constants.
const (
	stateDirPermissions  = 0750
	jobsFilePermissions  = 0644
	failedLogPermissions = 0600
)

// Migration operation constants.
const (
	defaultHTTPTimeout = 30 * time.Minute
	listMaxKeys        = 1000
)

// ErrMigrationCanceled is returned when a migration is canceled.
var ErrMigrationCanceled = errors.New("migration canceled")

// SourceType represents the type of source system.
type SourceType string

const (
	SourceTypeS3    SourceType = "s3"
	SourceTypeMinio SourceType = "minio"
	SourceTypeGCS   SourceType = "gcs"
	SourceTypeAzure SourceType = "azure"
	SourceTypeLocal SourceType = "local"
)

// MigrationStatus represents the status of a migration job.
type MigrationStatus string

const (
	MigrationStatusPending   MigrationStatus = "pending"
	MigrationStatusRunning   MigrationStatus = "running"
	MigrationStatusPaused    MigrationStatus = "paused"
	MigrationStatusCompleted MigrationStatus = "completed"
	MigrationStatusFailed    MigrationStatus = "failed"
	MigrationStatusCancelled MigrationStatus = "cancelled"
)

// MigrationConfig contains configuration for a migration job.
type MigrationConfig struct {
	ModifiedAfter    *time.Time `json:"modifiedAfter,omitempty"`
	ModifiedBefore   *time.Time `json:"modifiedBefore,omitempty"`
	DestPrefix       string     `json:"destPrefix"`
	SourceEndpoint   string     `json:"sourceEndpoint"`
	SourceAccessKey  string     `json:"sourceAccessKey"`
	SourceSecretKey  string     `json:"sourceSecretKey"`
	SourceRegion     string     `json:"sourceRegion"`
	SourceType       SourceType `json:"sourceType"`
	SourcePrefix     string     `json:"sourcePrefix"`
	SourceBuckets    []string   `json:"sourceBuckets"`
	DestBuckets      []string   `json:"destBuckets"`
	SourceExclude    []string   `json:"sourceExclude,omitempty"`
	ContentTypes     []string   `json:"contentTypes,omitempty"`
	MinSize          int64      `json:"minSize,omitempty"`
	MaxBandwidth     int64      `json:"maxBandwidth,omitempty"`
	MaxSize          int64      `json:"maxSize,omitempty"`
	Concurrency      int        `json:"concurrency"`
	PreserveMetadata bool       `json:"preserveMetadata"`
	PreserveTags     bool       `json:"preserveTags"`
	DryRun           bool       `json:"dryRun"`
	Overwrite        bool       `json:"overwrite"`
	SkipExisting     bool       `json:"skipExisting"`
	SourceSecure     bool       `json:"sourceSecure"`
	VerifyChecksum   bool       `json:"verifyChecksum"`
}

// MigrationJob represents a migration job.
type MigrationJob struct {
	StartTime time.Time          `json:"startTime"`
	EndTime   time.Time          `json:"endTime"`
	Config    *MigrationConfig   `json:"config"`
	Progress  *MigrationProgress `json:"progress"`
	Resume    *ResumeState       `json:"resume,omitempty"`
	ID        string             `json:"id"`
	Status    MigrationStatus    `json:"status"`
	Error     string             `json:"error,omitempty"`
}

// MigrationProgress tracks migration progress.
type MigrationProgress struct {
	StartTime              time.Time     `json:"startTime"`
	LastUpdateTime         time.Time     `json:"lastUpdateTime"`
	CurrentBucket          string        `json:"currentBucket"`
	CurrentKey             string        `json:"currentKey"`
	TotalObjects           int64         `json:"totalObjects"`
	MigratedObjects        int64         `json:"migratedObjects"`
	FailedObjects          int64         `json:"failedObjects"`
	SkippedObjects         int64         `json:"skippedObjects"`
	TotalBytes             int64         `json:"totalBytes"`
	MigratedBytes          int64         `json:"migratedBytes"`
	BytesPerSecond         float64       `json:"bytesPerSecond"`
	EstimatedTimeRemaining time.Duration `json:"estimatedTimeRemaining"`
}

// ResumeState contains state for resuming a migration.
type ResumeState struct {
	ProcessedKeys map[string]bool `json:"processedKeys"`
	LastBucket    string          `json:"lastBucket"`
	LastKey       string          `json:"lastKey"`
	LastVersionID string          `json:"lastVersionId,omitempty"`
}

// FailedObject represents a failed migration.
type FailedObject struct {
	Timestamp time.Time `json:"timestamp"`
	Bucket    string    `json:"bucket"`
	Key       string    `json:"key"`
	Error     string    `json:"error"`
	Retries   int       `json:"retries"`
}

// MigrationManager manages migration jobs.
type MigrationManager struct {
	destStorage DestinationStorage
	jobs        map[string]*MigrationJob
	activeJob   *activeMigrationJob
	failedLog   *os.File
	stateDir    string
	mu          sync.RWMutex
}

// activeMigrationJob represents an active migration.
type activeMigrationJob struct {
	job          *MigrationJob
	cancel       context.CancelFunc
	sourceClient *S3Client
	pauseChan    chan struct{}
	resumeChan   chan struct{}
	isPaused     bool
	mu           sync.Mutex
}

// DestinationStorage interface for the destination system.
type DestinationStorage interface {
	PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, opts *PutOptions) error
	HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error)
	CreateBucket(ctx context.Context, bucket string) error
	BucketExists(ctx context.Context, bucket string) (bool, error)
}

// PutOptions contains options for PutObject.
type PutOptions struct {
	ContentType  string
	Metadata     map[string]string
	Tags         map[string]string
	StorageClass string
}

// ObjectInfo contains object metadata.
type ObjectInfo struct {
	LastModified time.Time
	Metadata     map[string]string
	Key          string
	ETag         string
	ContentType  string
	Size         int64
}

// S3Client is a simple S3 client for migration.
type S3Client struct {
	client    *http.Client
	endpoint  string
	accessKey string
	secretKey string
	region    string
	secure    bool
}

// ListBucketResult represents the result of ListObjects.
type ListBucketResult struct {
	XMLName     xml.Name   `xml:"ListBucketResult"`
	Name        string     `xml:"Name"`
	Prefix      string     `xml:"Prefix"`
	Marker      string     `xml:"Marker"`
	NextMarker  string     `xml:"NextMarker"`
	Contents    []S3Object `xml:"Contents"`
	MaxKeys     int        `xml:"MaxKeys"`
	IsTruncated bool       `xml:"IsTruncated"`
}

// S3Object represents an object in an S3 bucket.
type S3Object struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	StorageClass string `xml:"StorageClass"`
	Size         int64  `xml:"Size"`
}

// NewMigrationManager creates a new migration manager.
func NewMigrationManager(destStorage DestinationStorage, stateDir string) (*MigrationManager, error) {
	mkdirErr := os.MkdirAll(stateDir, stateDirPermissions)
	if mkdirErr != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", mkdirErr)
	}

	failedLogPath := filepath.Join(stateDir, "failed.log")

	//nolint:gosec // G304: failedLogPath is constructed from trusted config
	failedLog, err := os.OpenFile(failedLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, failedLogPermissions)
	if err != nil {
		return nil, fmt.Errorf("failed to open failed log: %w", err)
	}

	mm := &MigrationManager{
		jobs:        make(map[string]*MigrationJob),
		destStorage: destStorage,
		stateDir:    stateDir,
		failedLog:   failedLog,
	}

	// Load existing jobs
	loadErr := mm.loadJobs()
	if loadErr != nil {
		return nil, fmt.Errorf("failed to load jobs: %w", loadErr)
	}

	return mm, nil
}

// loadJobs loads existing job states.
func (mm *MigrationManager) loadJobs() error {
	jobsPath := filepath.Join(mm.stateDir, "jobs.json")

	//nolint:gosec // G304: jobsPath is constructed from trusted config
	data, err := os.ReadFile(jobsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	var jobs []*MigrationJob

	unmarshalErr := json.Unmarshal(data, &jobs)
	if unmarshalErr != nil {
		return unmarshalErr
	}

	for _, j := range jobs {
		mm.jobs[j.ID] = j
	}

	return nil
}

// saveJobs saves job states.
func (mm *MigrationManager) saveJobs() error {
	mm.mu.RLock()

	jobs := make([]*MigrationJob, 0, len(mm.jobs))
	for _, j := range mm.jobs {
		jobs = append(jobs, j)
	}

	mm.mu.RUnlock()

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}

	jobsPath := filepath.Join(mm.stateDir, "jobs.json")

	return os.WriteFile(jobsPath, data, jobsFilePermissions)
}

// CreateMigration creates a new migration job.
func (mm *MigrationManager) CreateMigration(config *MigrationConfig) (*MigrationJob, error) {
	err := mm.validateConfig(config)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	job := &MigrationJob{
		ID:        uuid.New().String(),
		Config:    config,
		Status:    MigrationStatusPending,
		StartTime: time.Now(),
		Progress:  &MigrationProgress{StartTime: time.Now()},
	}

	mm.mu.Lock()
	mm.jobs[job.ID] = job
	mm.mu.Unlock()

	err = mm.saveJobs()
	if err != nil {
		return nil, fmt.Errorf("failed to save job: %w", err)
	}

	return job, nil
}

// validateConfig validates migration configuration.
func (mm *MigrationManager) validateConfig(config *MigrationConfig) error {
	if config.SourceEndpoint == "" && config.SourceType != SourceTypeLocal {
		return errors.New("source endpoint is required")
	}

	if len(config.SourceBuckets) == 0 {
		return errors.New("at least one source bucket is required")
	}

	if config.Concurrency < 1 {
		config.Concurrency = 10
	}

	if len(config.DestBuckets) == 0 {
		config.DestBuckets = config.SourceBuckets
	} else if len(config.DestBuckets) != len(config.SourceBuckets) {
		return errors.New("destination bucket count must match source bucket count")
	}

	return nil
}

// StartMigration starts a migration job.
func (mm *MigrationManager) StartMigration(ctx context.Context, jobID string) error {
	mm.mu.Lock()

	if mm.activeJob != nil {
		mm.mu.Unlock()
		return errors.New("a migration is already running")
	}

	job, exists := mm.jobs[jobID]
	if !exists {
		mm.mu.Unlock()
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != MigrationStatusPending && job.Status != MigrationStatusPaused {
		mm.mu.Unlock()
		return fmt.Errorf("job cannot be started in status: %s", job.Status)
	}

	// Create source client
	client := NewS3Client(
		job.Config.SourceEndpoint,
		job.Config.SourceAccessKey,
		job.Config.SourceSecretKey,
		job.Config.SourceRegion,
		job.Config.SourceSecure,
	)

	jobCtx, cancel := context.WithCancel(ctx)
	activeJob := &activeMigrationJob{
		job:          job,
		cancel:       cancel,
		sourceClient: client,
		pauseChan:    make(chan struct{}),
		resumeChan:   make(chan struct{}),
	}

	mm.activeJob = activeJob
	job.Status = MigrationStatusRunning

	mm.mu.Unlock()

	// Start migration in goroutine
	go mm.runMigration(jobCtx, activeJob)

	return nil
}

// NewS3Client creates a new S3 client.
func NewS3Client(endpoint, accessKey, secretKey, region string, secure bool) *S3Client {
	return &S3Client{
		endpoint:  endpoint,
		accessKey: accessKey,
		secretKey: secretKey,
		region:    region,
		secure:    secure,
		client:    httputil.NewClientWithTimeout(defaultHTTPTimeout),
	}
}

// ListObjects lists objects in a bucket.
func (c *S3Client) ListObjects(ctx context.Context, bucket, prefix, marker string, maxKeys int) (*ListBucketResult, error) {
	scheme := "http"
	if c.secure {
		scheme = "https"
	}

	params := url.Values{}
	if prefix != "" {
		params.Set("prefix", prefix)
	}

	if marker != "" {
		params.Set("marker", marker)
	}

	params.Set("max-keys", strconv.Itoa(maxKeys))

	reqURL := fmt.Sprintf("%s://%s/%s?%s", scheme, c.endpoint, bucket, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	c.signRequest(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list objects failed: %s - %s", resp.Status, string(body))
	}

	var result ListBucketResult

	decodeErr := xml.NewDecoder(resp.Body).Decode(&result)
	if decodeErr != nil {
		return nil, fmt.Errorf("failed to decode response: %w", decodeErr)
	}

	return &result, nil
}

// GetObject gets an object from a bucket.
func (c *S3Client) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *ObjectInfo, error) {
	scheme := "http"
	if c.secure {
		scheme = "https"
	}

	reqURL := fmt.Sprintf("%s://%s/%s/%s", scheme, c.endpoint, bucket, url.PathEscape(key))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, nil, err
	}

	c.signRequest(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		return nil, nil, fmt.Errorf("get object failed: %s - %s", resp.Status, string(body))
	}

	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	lastModified, _ := time.Parse(time.RFC1123, resp.Header.Get("Last-Modified"))

	// Extract metadata
	metadata := make(map[string]string)

	for k, v := range resp.Header {
		if metaKey, ok := strings.CutPrefix(strings.ToLower(k), "x-amz-meta-"); ok {
			metadata[metaKey] = v[0]
		}
	}

	info := &ObjectInfo{
		Key:          key,
		Size:         size,
		ETag:         strings.Trim(resp.Header.Get("ETag"), "\""),
		ContentType:  resp.Header.Get("Content-Type"),
		LastModified: lastModified,
		Metadata:     metadata,
	}

	return resp.Body, info, nil
}

// signRequest signs an S3 request using AWS Signature V4.
func (c *S3Client) signRequest(req *http.Request) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("Host", c.endpoint)
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")

	// Create canonical request
	canonicalHeaders := c.getCanonicalHeaders(req)
	signedHeaders := c.getSignedHeaders(req)

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.Path,
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		"UNSIGNED-PAYLOAD",
	}, "\n")

	// Create string to sign
	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, c.region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		c.sha256Hash([]byte(canonicalRequest)),
	}, "\n")

	// Calculate signature
	signingKey := c.getSignatureKey(dateStamp)
	signature := hex.EncodeToString(c.hmacSHA256(signingKey, []byte(stringToSign)))

	// Build authorization header
	authorization := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.accessKey,
		credentialScope,
		signedHeaders,
		signature,
	)

	req.Header.Set("Authorization", authorization)
}

func (c *S3Client) getCanonicalHeaders(req *http.Request) string {
	headers := make(map[string]string)

	for k := range req.Header {
		lowerKey := strings.ToLower(k)
		if strings.HasPrefix(lowerKey, "x-amz-") || lowerKey == "host" {
			headers[lowerKey] = strings.TrimSpace(req.Header.Get(k))
		}
	}

	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var canonical strings.Builder
	for _, k := range keys {
		canonical.WriteString(k)
		canonical.WriteString(":")
		canonical.WriteString(headers[k])
		canonical.WriteString("\n")
	}

	return canonical.String()
}

func (c *S3Client) getSignedHeaders(req *http.Request) string {
	var headers []string

	for k := range req.Header {
		lowerKey := strings.ToLower(k)
		if strings.HasPrefix(lowerKey, "x-amz-") || lowerKey == "host" {
			headers = append(headers, lowerKey)
		}
	}

	sort.Strings(headers)

	return strings.Join(headers, ";")
}

func (c *S3Client) sha256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (c *S3Client) hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)

	return h.Sum(nil)
}

func (c *S3Client) getSignatureKey(dateStamp string) []byte {
	kDate := c.hmacSHA256([]byte("AWS4"+c.secretKey), []byte(dateStamp))
	kRegion := c.hmacSHA256(kDate, []byte(c.region))
	kService := c.hmacSHA256(kRegion, []byte("s3"))
	kSigning := c.hmacSHA256(kService, []byte("aws4_request"))

	return kSigning
}

// runMigration runs the migration job.
func (mm *MigrationManager) runMigration(ctx context.Context, activeJob *activeMigrationJob) {
	defer func() {
		mm.mu.Lock()
		mm.activeJob = nil
		mm.mu.Unlock()

		saveErr := mm.saveJobs()
		if saveErr != nil {
			log.Error().
				Err(saveErr).
				Str("job_id", activeJob.job.ID).
				Msg("failed to save migration jobs state after migration completed - progress may be lost on restart")
		}
	}()

	job := activeJob.job

	// Ensure destination buckets exist
	for _, destBucket := range job.Config.DestBuckets {
		exists, err := mm.destStorage.BucketExists(ctx, destBucket)
		if err != nil {
			job.Status = MigrationStatusFailed
			job.Error = fmt.Sprintf("failed to check bucket %s: %v", destBucket, err)

			return
		}

		if !exists {
			err := mm.destStorage.CreateBucket(ctx, destBucket)
			if err != nil {
				job.Status = MigrationStatusFailed
				job.Error = fmt.Sprintf("failed to create bucket %s: %v", destBucket, err)

				return
			}
		}
	}

	// First pass: count objects
	err := mm.countObjects(ctx, activeJob)
	if err != nil {
		if ctx.Err() != nil {
			job.Status = MigrationStatusCancelled
			return
		}

		job.Status = MigrationStatusFailed
		job.Error = err.Error()

		return
	}

	// Second pass: migrate objects
	err = mm.migrateObjects(ctx, activeJob)
	if err != nil {
		if ctx.Err() != nil {
			job.Status = MigrationStatusCancelled
			return
		}

		job.Status = MigrationStatusFailed
		job.Error = err.Error()

		return
	}

	job.EndTime = time.Now()
	job.Status = MigrationStatusCompleted
}

// migrateObjects migrates objects from source to destination.
func (mm *MigrationManager) migrateObjects(ctx context.Context, activeJob *activeMigrationJob) error {
	job := activeJob.job

	sem := make(chan struct{}, job.Config.Concurrency)
	var (
		wg           sync.WaitGroup
		migrationErr error
		errMu        sync.Mutex
	)

	for i, sourceBucket := range job.Config.SourceBuckets {
		destBucket := job.Config.DestBuckets[i]
		if err := mm.migrateBucket(ctx, activeJob, sourceBucket, destBucket, sem, &wg, &migrationErr, &errMu); err != nil {
			return err
		}
	}

	return migrationErr
}

// shouldMigrate checks if an object should be migrated based on filters.
func (mm *MigrationManager) shouldMigrate(config *MigrationConfig, obj *S3Object) bool {
	// Check prefix exclusions
	for _, exclude := range config.SourceExclude {
		if strings.HasPrefix(obj.Key, exclude) {
			return false
		}
	}

	// Check size filters
	if config.MinSize > 0 && obj.Size < config.MinSize {
		return false
	}

	if config.MaxSize > 0 && obj.Size > config.MaxSize {
		return false
	}

	// Check time filters
	objTime, _ := time.Parse(time.RFC3339, obj.LastModified)
	if config.ModifiedAfter != nil && objTime.Before(*config.ModifiedAfter) {
		return false
	}

	if config.ModifiedBefore != nil && objTime.After(*config.ModifiedBefore) {
		return false
	}

	return true
}

// migrateObject migrates a single object.
func (mm *MigrationManager) migrateObject(ctx context.Context, activeJob *activeMigrationJob, sourceBucket, destBucket string, obj *S3Object) error {
	job := activeJob.job
	config := job.Config

	// Calculate destination key
	destKey := obj.Key
	if config.SourcePrefix != "" {
		destKey = strings.TrimPrefix(destKey, config.SourcePrefix)
	}

	if config.DestPrefix != "" {
		destKey = config.DestPrefix + destKey
	}

	// Check if object exists (for skip/overwrite)
	if config.SkipExisting {
		destInfo, err := mm.destStorage.HeadObject(ctx, destBucket, destKey)
		if err == nil {
			// Object exists
			if !config.Overwrite && destInfo.ETag == strings.Trim(obj.ETag, "\"") {
				return nil // Skip identical object
			}
		}
	}

	// Dry run - don't actually migrate
	if config.DryRun {
		return nil
	}

	// Get object from source
	reader, info, err := activeJob.sourceClient.GetObject(ctx, sourceBucket, obj.Key)
	if err != nil {
		return fmt.Errorf("failed to get source object: %w", err)
	}

	defer func() { _ = reader.Close() }()

	// Apply bandwidth limit
	var limitedReader io.Reader = reader
	if config.MaxBandwidth > 0 {
		limitedReader = NewRateLimitedReader(reader, config.MaxBandwidth)
	}

	// Put object to destination
	opts := &PutOptions{
		ContentType: info.ContentType,
	}

	if config.PreserveMetadata {
		opts.Metadata = info.Metadata
	}

	putErr := mm.destStorage.PutObject(ctx, destBucket, destKey, limitedReader, info.Size, opts)
	if putErr != nil {
		return fmt.Errorf("failed to put object: %w", putErr)
	}

	// Verify checksum if enabled
	if config.VerifyChecksum {
		destInfo, err := mm.destStorage.HeadObject(ctx, destBucket, destKey)
		if err != nil {
			return fmt.Errorf("failed to verify object: %w", err)
		}

		if destInfo.ETag != strings.Trim(obj.ETag, "\"") {
			return fmt.Errorf("checksum mismatch: source=%s, dest=%s", obj.ETag, destInfo.ETag)
		}
	}

	return nil
}

// PauseMigration pauses the active migration.
func (mm *MigrationManager) PauseMigration() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if mm.activeJob == nil {
		return errors.New("no active migration")
	}

	mm.activeJob.mu.Lock()
	mm.activeJob.isPaused = true
	mm.activeJob.mu.Unlock()

	mm.activeJob.job.Status = MigrationStatusPaused

	return nil
}

// ResumeMigration resumes a paused migration.
func (mm *MigrationManager) ResumeMigration() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if mm.activeJob == nil {
		return errors.New("no active migration")
	}

	mm.activeJob.mu.Lock()

	if !mm.activeJob.isPaused {
		mm.activeJob.mu.Unlock()
		return errors.New("migration is not paused")
	}

	mm.activeJob.isPaused = false
	mm.activeJob.mu.Unlock()

	close(mm.activeJob.resumeChan)
	mm.activeJob.resumeChan = make(chan struct{})
	mm.activeJob.job.Status = MigrationStatusRunning

	return nil
}

// CancelMigration cancels the active migration.
func (mm *MigrationManager) CancelMigration() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if mm.activeJob == nil {
		return errors.New("no active migration")
	}

	mm.activeJob.cancel()

	return nil
}

// GetMigration returns a migration job by ID.
func (mm *MigrationManager) GetMigration(id string) (*MigrationJob, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	job, exists := mm.jobs[id]
	if !exists {
		return nil, fmt.Errorf("job not found: %s", id)
	}

	return job, nil
}

// ListMigrations returns all migration jobs.
func (mm *MigrationManager) ListMigrations() []*MigrationJob {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	jobs := make([]*MigrationJob, 0, len(mm.jobs))
	for _, j := range mm.jobs {
		jobs = append(jobs, j)
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].StartTime.After(jobs[j].StartTime)
	})

	return jobs
}

// GetProgress returns the current migration progress.
func (mm *MigrationManager) GetProgress() (*MigrationProgress, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if mm.activeJob == nil {
		return nil, errors.New("no active migration")
	}

	return mm.activeJob.job.Progress, nil
}

// RateLimitedReader wraps a reader with rate limiting.
type RateLimitedReader struct {
	lastTime time.Time
	reader   io.Reader
	rate     int64
	bucket   int64
}

// NewRateLimitedReader creates a new rate-limited reader.
func NewRateLimitedReader(reader io.Reader, rate int64) *RateLimitedReader {
	return &RateLimitedReader{
		reader:   reader,
		rate:     rate,
		lastTime: time.Now(),
		bucket:   rate,
	}
}

// Read implements io.Reader with rate limiting.
func (r *RateLimitedReader) Read(p []byte) (int, error) {
	now := time.Now()
	elapsed := now.Sub(r.lastTime)
	r.lastTime = now

	// Refill bucket
	r.bucket += int64(elapsed.Seconds() * float64(r.rate))
	if r.bucket > r.rate {
		r.bucket = r.rate
	}

	// Calculate how much we can read
	toRead := int64(len(p))
	if toRead > r.bucket {
		toRead = r.bucket
	}

	if toRead == 0 {
		// Wait for bucket to refill
		time.Sleep(time.Duration(float64(time.Second) * float64(len(p)) / float64(r.rate)))
		r.bucket = int64(len(p))
		toRead = r.bucket
	}

	n, err := r.reader.Read(p[:toRead])
	r.bucket -= int64(n)

	return n, err
}

// Close closes the migration manager.
func (mm *MigrationManager) Close() error {
	return mm.failedLog.Close()
}

// migrateBucket migrates all objects from a source bucket to destination.
func (mm *MigrationManager) migrateBucket(
	ctx context.Context,
	activeJob *activeMigrationJob,
	sourceBucket, destBucket string,
	sem chan struct{},
	wg *sync.WaitGroup,
	migrationErr *error,
	errMu *sync.Mutex,
) error {
	job := activeJob.job
	job.Progress.CurrentBucket = sourceBucket

	marker := mm.getResumeMarker(job, sourceBucket)

	for {
		if err := mm.checkPauseOrCancel(ctx, activeJob); err != nil {
			return err
		}

		result, err := mm.listBucketObjects(ctx, activeJob, sourceBucket, job.Config.SourcePrefix, marker)
		if err != nil {
			return err
		}

		mm.processObjectBatch(ctx, activeJob, sourceBucket, destBucket, result.Contents, sem, wg, migrationErr, errMu)

		wg.Wait()

		if !result.IsTruncated {
			break
		}

		marker = result.NextMarker
		if marker == "" && len(result.Contents) > 0 {
			marker = result.Contents[len(result.Contents)-1].Key
		}

		// Save resume state
		job.Resume = &ResumeState{
			ProcessedKeys: make(map[string]bool),
			LastBucket:    sourceBucket,
			LastKey:       marker,
			LastVersionID: "",
		}

		saveErr := mm.saveJobs()
		if saveErr != nil {
			log.Warn().
				Err(saveErr).
				Str("job_id", job.ID).
				Str("bucket", sourceBucket).
				Str("marker", marker).
				Msg("failed to save migration resume state - migration may restart from beginning if interrupted")
		}
	}

	return nil
}

// getResumeMarker returns the marker to resume from for a bucket.
func (mm *MigrationManager) getResumeMarker(job *MigrationJob, sourceBucket string) string {
	marker := ""
	if job.Resume != nil && job.Resume.LastBucket == sourceBucket {
		marker = job.Resume.LastKey
	}
	return marker
}

// checkPauseOrCancel checks if the migration should pause or cancel.
func (mm *MigrationManager) checkPauseOrCancel(ctx context.Context, activeJob *activeMigrationJob) error {
	select {
	case <-activeJob.pauseChan:
		log.Info().
			Str("job_id", activeJob.job.ID).
			Msg("Migration paused")
		<-activeJob.pauseChan
		log.Info().
			Str("job_id", activeJob.job.ID).
			Msg("Migration resumed")
	case <-ctx.Done():
		return ErrMigrationCanceled
	default:
	}
	return nil
}

// listBucketObjects lists objects in a bucket with pagination.
func (mm *MigrationManager) listBucketObjects(
	ctx context.Context,
	activeJob *activeMigrationJob,
	sourceBucket, prefix, marker string,
) (*ListBucketResult, error) {
	result, err := activeJob.sourceClient.ListObjects(ctx, sourceBucket, prefix, marker, listMaxKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to list source objects: %w", err)
	}
	return result, nil
}

// processObjectBatch processes a batch of objects for migration.
func (mm *MigrationManager) processObjectBatch(
	ctx context.Context,
	activeJob *activeMigrationJob,
	sourceBucket, destBucket string,
	objects []S3Object,
	sem chan struct{},
	wg *sync.WaitGroup,
	migrationErr *error,
	errMu *sync.Mutex,
) {
	for i := range objects {
		if !mm.shouldMigrate(activeJob.job.Config, &objects[i]) {
			continue
		}

		sem <- struct{}{}
		wg.Add(1)

		go mm.migrateObjectWorker(ctx, activeJob, sourceBucket, destBucket, &objects[i], sem, wg, migrationErr, errMu)
	}
}

// migrateObjectWorker is a worker goroutine that migrates a single object.
func (mm *MigrationManager) migrateObjectWorker(
	ctx context.Context,
	activeJob *activeMigrationJob,
	sourceBucket, destBucket string,
	obj *S3Object,
	sem chan struct{},
	wg *sync.WaitGroup,
	migrationErr *error,
	errMu *sync.Mutex,
) {
	defer wg.Done()
	defer func() { <-sem }()

	err := mm.migrateObject(ctx, activeJob, sourceBucket, destBucket, obj)
	if err != nil {
		log.Error().
			Err(err).
			Str("job_id", activeJob.job.ID).
			Str("bucket", sourceBucket).
			Str("key", obj.Key).
			Msg("failed to migrate object")

		errMu.Lock()
		if *migrationErr == nil {
			*migrationErr = err
		}
		errMu.Unlock()
	} else {
		mm.updateMigrationProgress(activeJob, obj.Size)
	}
}

// updateMigrationProgress updates job progress counters.
func (mm *MigrationManager) updateMigrationProgress(activeJob *activeMigrationJob, objectSize int64) {
	activeJob.mu.Lock()
	activeJob.job.Progress.MigratedObjects++
	activeJob.job.Progress.MigratedBytes += objectSize
	activeJob.mu.Unlock()

	log.Debug().
		Str("job_id", activeJob.job.ID).
		Int64("objects_migrated", activeJob.job.Progress.MigratedObjects).
		Int64("bytes_migrated", activeJob.job.Progress.MigratedBytes).
		Msg("Migration progress updated")
}

// countBucketObjects counts total objects in a bucket for migration.
func (mm *MigrationManager) countBucketObjects(ctx context.Context, activeJob *activeMigrationJob, bucket string) error {
	job := activeJob.job
	marker := ""

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled: %w", ctx.Err())
		default:
		}

		result, err := mm.listBucketForCounting(ctx, activeJob, bucket, marker)
		if err != nil {
			return err
		}

		mm.accumulateObjectCounts(job, result.Contents)

		if !result.IsTruncated {
			break
		}

		marker = mm.getNextMarker(result)
	}

	return nil
}

// listBucketForCounting lists objects in a bucket for counting purposes.
func (mm *MigrationManager) listBucketForCounting(
	ctx context.Context,
	activeJob *activeMigrationJob,
	bucket, marker string,
) (*ListBucketResult, error) {
	result, err := activeJob.sourceClient.ListObjects(
		ctx,
		bucket,
		activeJob.job.Config.SourcePrefix,
		marker,
		listMaxKeys,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects in %s: %w", bucket, err)
	}

	return result, nil
}

// accumulateObjectCounts accumulates object counts and sizes.
func (mm *MigrationManager) accumulateObjectCounts(job *MigrationJob, objects []S3Object) {
	for i := range objects {
		if mm.shouldMigrate(job.Config, &objects[i]) {
			atomic.AddInt64(&job.Progress.TotalObjects, 1)
			atomic.AddInt64(&job.Progress.TotalBytes, objects[i].Size)
		}
	}
}

// getNextMarker determines the next marker for pagination.
func (mm *MigrationManager) getNextMarker(result *ListBucketResult) string {
	marker := result.NextMarker
	if marker == "" && len(result.Contents) > 0 {
		marker = result.Contents[len(result.Contents)-1].Key
	}

	return marker
}

// countObjects counts total objects to migrate.
func (mm *MigrationManager) countObjects(ctx context.Context, activeJob *activeMigrationJob) error {
	job := activeJob.job

	for _, bucket := range job.Config.SourceBuckets {
		if err := mm.countBucketObjects(ctx, activeJob, bucket); err != nil {
			return err
		}
	}

	return nil
}
