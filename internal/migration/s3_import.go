// Package migration provides tools for migrating data from other S3-compatible systems.
package migration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
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
)

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
	MigrationStatusPending    MigrationStatus = "pending"
	MigrationStatusRunning    MigrationStatus = "running"
	MigrationStatusPaused     MigrationStatus = "paused"
	MigrationStatusCompleted  MigrationStatus = "completed"
	MigrationStatusFailed     MigrationStatus = "failed"
	MigrationStatusCancelled  MigrationStatus = "cancelled"
)

// MigrationConfig contains configuration for a migration job.
type MigrationConfig struct {
	// Source configuration
	SourceType     SourceType
	SourceEndpoint string
	SourceAccessKey string
	SourceSecretKey string
	SourceRegion   string
	SourceSecure   bool

	// Source selection
	SourceBuckets  []string
	SourcePrefix   string
	SourceExclude  []string

	// Destination configuration
	DestBuckets    []string
	DestPrefix     string

	// Migration options
	Concurrency    int
	SkipExisting   bool
	Overwrite      bool
	PreserveMetadata bool
	PreserveTags   bool
	DryRun         bool

	// Bandwidth limiting
	MaxBandwidth   int64 // bytes per second, 0 = unlimited

	// Filtering
	MinSize        int64
	MaxSize        int64
	ModifiedAfter  *time.Time
	ModifiedBefore *time.Time
	ContentTypes   []string

	// Verification
	VerifyChecksum bool
}

// MigrationJob represents a migration job.
type MigrationJob struct {
	ID              string          `json:"id"`
	Config          *MigrationConfig `json:"config"`
	Status          MigrationStatus `json:"status"`
	StartTime       time.Time       `json:"startTime"`
	EndTime         time.Time       `json:"endTime,omitempty"`
	Progress        *MigrationProgress `json:"progress"`
	Error           string          `json:"error,omitempty"`
	Resume          *ResumeState    `json:"resume,omitempty"`
}

// MigrationProgress tracks migration progress.
type MigrationProgress struct {
	TotalObjects     int64 `json:"totalObjects"`
	MigratedObjects  int64 `json:"migratedObjects"`
	FailedObjects    int64 `json:"failedObjects"`
	SkippedObjects   int64 `json:"skippedObjects"`
	TotalBytes       int64 `json:"totalBytes"`
	MigratedBytes    int64 `json:"migratedBytes"`
	CurrentBucket    string `json:"currentBucket"`
	CurrentKey       string `json:"currentKey"`
	StartTime        time.Time `json:"startTime"`
	LastUpdateTime   time.Time `json:"lastUpdateTime"`
	BytesPerSecond   float64 `json:"bytesPerSecond"`
	EstimatedTimeRemaining time.Duration `json:"estimatedTimeRemaining"`
}

// ResumeState contains state for resuming a migration.
type ResumeState struct {
	LastBucket      string `json:"lastBucket"`
	LastKey         string `json:"lastKey"`
	LastVersionID   string `json:"lastVersionId,omitempty"`
	ProcessedKeys   map[string]bool `json:"processedKeys"`
}

// FailedObject represents a failed migration.
type FailedObject struct {
	Bucket    string
	Key       string
	Error     string
	Timestamp time.Time
	Retries   int
}

// MigrationManager manages migration jobs.
type MigrationManager struct {
	mu           sync.RWMutex
	jobs         map[string]*MigrationJob
	activeJob    *activeMigrationJob
	destStorage  DestinationStorage
	stateDir     string
	failedLog    *os.File
}

// activeMigrationJob represents an active migration.
type activeMigrationJob struct {
	job          *MigrationJob
	ctx          context.Context
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
	Key          string
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
}

// S3Client is a simple S3 client for migration.
type S3Client struct {
	endpoint  string
	accessKey string
	secretKey string
	region    string
	secure    bool
	client    *http.Client
}

// ListBucketResult represents the result of ListObjects.
type ListBucketResult struct {
	XMLName        xml.Name        `xml:"ListBucketResult"`
	Name           string          `xml:"Name"`
	Prefix         string          `xml:"Prefix"`
	Marker         string          `xml:"Marker"`
	NextMarker     string          `xml:"NextMarker"`
	MaxKeys        int             `xml:"MaxKeys"`
	IsTruncated    bool            `xml:"IsTruncated"`
	Contents       []S3Object      `xml:"Contents"`
}

// S3Object represents an object in an S3 bucket.
type S3Object struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// NewMigrationManager creates a new migration manager.
func NewMigrationManager(destStorage DestinationStorage, stateDir string) (*MigrationManager, error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	failedLogPath := filepath.Join(stateDir, "failed.log")
	failedLog, err := os.OpenFile(failedLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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
	if err := mm.loadJobs(); err != nil {
		return nil, fmt.Errorf("failed to load jobs: %w", err)
	}

	return mm, nil
}

// loadJobs loads existing job states.
func (mm *MigrationManager) loadJobs() error {
	jobsPath := filepath.Join(mm.stateDir, "jobs.json")
	data, err := os.ReadFile(jobsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var jobs []*MigrationJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return err
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
	return os.WriteFile(jobsPath, data, 0644)
}

// CreateMigration creates a new migration job.
func (mm *MigrationManager) CreateMigration(config *MigrationConfig) (*MigrationJob, error) {
	if err := mm.validateConfig(config); err != nil {
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

	if err := mm.saveJobs(); err != nil {
		return nil, fmt.Errorf("failed to save job: %w", err)
	}

	return job, nil
}

// validateConfig validates migration configuration.
func (mm *MigrationManager) validateConfig(config *MigrationConfig) error {
	if config.SourceEndpoint == "" && config.SourceType != SourceTypeLocal {
		return fmt.Errorf("source endpoint is required")
	}

	if len(config.SourceBuckets) == 0 {
		return fmt.Errorf("at least one source bucket is required")
	}

	if config.Concurrency < 1 {
		config.Concurrency = 10
	}

	if len(config.DestBuckets) == 0 {
		config.DestBuckets = config.SourceBuckets
	} else if len(config.DestBuckets) != len(config.SourceBuckets) {
		return fmt.Errorf("destination bucket count must match source bucket count")
	}

	return nil
}

// StartMigration starts a migration job.
func (mm *MigrationManager) StartMigration(ctx context.Context, jobID string) error {
	mm.mu.Lock()
	if mm.activeJob != nil {
		mm.mu.Unlock()
		return fmt.Errorf("a migration is already running")
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
		ctx:          jobCtx,
		cancel:       cancel,
		sourceClient: client,
		pauseChan:    make(chan struct{}),
		resumeChan:   make(chan struct{}),
	}

	mm.activeJob = activeJob
	job.Status = MigrationStatusRunning
	mm.mu.Unlock()

	// Start migration in goroutine
	go mm.runMigration(activeJob)

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
		client:    &http.Client{Timeout: 30 * time.Minute},
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

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
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
	if err := xml.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
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

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
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
		if strings.HasPrefix(strings.ToLower(k), "x-amz-meta-") {
			metaKey := strings.TrimPrefix(strings.ToLower(k), "x-amz-meta-")
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

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("Host", c.endpoint)
	req.Header.Set("x-amz-content-sha256", "UNSIGNED-PAYLOAD")

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

	var keys []string
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
func (mm *MigrationManager) runMigration(activeJob *activeMigrationJob) {
	defer func() {
		mm.mu.Lock()
		mm.activeJob = nil
		mm.mu.Unlock()
		_ = mm.saveJobs()
	}()

	job := activeJob.job

	// Ensure destination buckets exist
	for _, destBucket := range job.Config.DestBuckets {
		exists, err := mm.destStorage.BucketExists(activeJob.ctx, destBucket)
		if err != nil {
			job.Status = MigrationStatusFailed
			job.Error = fmt.Sprintf("failed to check bucket %s: %v", destBucket, err)
			return
		}
		if !exists {
			if err := mm.destStorage.CreateBucket(activeJob.ctx, destBucket); err != nil {
				job.Status = MigrationStatusFailed
				job.Error = fmt.Sprintf("failed to create bucket %s: %v", destBucket, err)
				return
			}
		}
	}

	// First pass: count objects
	if err := mm.countObjects(activeJob); err != nil {
		if activeJob.ctx.Err() != nil {
			job.Status = MigrationStatusCancelled
			return
		}
		job.Status = MigrationStatusFailed
		job.Error = err.Error()
		return
	}

	// Second pass: migrate objects
	if err := mm.migrateObjects(activeJob); err != nil {
		if activeJob.ctx.Err() != nil {
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

// countObjects counts total objects to migrate.
func (mm *MigrationManager) countObjects(activeJob *activeMigrationJob) error {
	job := activeJob.job

	for _, bucket := range job.Config.SourceBuckets {
		marker := ""
		for {
			select {
			case <-activeJob.ctx.Done():
				return activeJob.ctx.Err()
			default:
			}

			result, err := activeJob.sourceClient.ListObjects(
				activeJob.ctx,
				bucket,
				job.Config.SourcePrefix,
				marker,
				1000,
			)
			if err != nil {
				return fmt.Errorf("failed to list objects in %s: %w", bucket, err)
			}

			for _, obj := range result.Contents {
				if mm.shouldMigrate(job.Config, &obj) {
					atomic.AddInt64(&job.Progress.TotalObjects, 1)
					atomic.AddInt64(&job.Progress.TotalBytes, obj.Size)
				}
			}

			if !result.IsTruncated {
				break
			}
			marker = result.NextMarker
			if marker == "" && len(result.Contents) > 0 {
				marker = result.Contents[len(result.Contents)-1].Key
			}
		}
	}

	return nil
}

// migrateObjects migrates objects from source to destination.
func (mm *MigrationManager) migrateObjects(activeJob *activeMigrationJob) error {
	job := activeJob.job

	// Semaphore for concurrency control
	sem := make(chan struct{}, job.Config.Concurrency)
	var wg sync.WaitGroup
	var migrationErr error
	var errMu sync.Mutex

	for i, sourceBucket := range job.Config.SourceBuckets {
		destBucket := job.Config.DestBuckets[i]
		job.Progress.CurrentBucket = sourceBucket

		marker := ""
		if job.Resume != nil && job.Resume.LastBucket == sourceBucket {
			marker = job.Resume.LastKey
		}

		for {
			select {
			case <-activeJob.ctx.Done():
				return activeJob.ctx.Err()
			default:
			}

			// Check for pause
			activeJob.mu.Lock()
			if activeJob.isPaused {
				activeJob.mu.Unlock()
				select {
				case <-activeJob.resumeChan:
				case <-activeJob.ctx.Done():
					return activeJob.ctx.Err()
				}
			} else {
				activeJob.mu.Unlock()
			}

			result, err := activeJob.sourceClient.ListObjects(
				activeJob.ctx,
				sourceBucket,
				job.Config.SourcePrefix,
				marker,
				1000,
			)
			if err != nil {
				return fmt.Errorf("failed to list objects: %w", err)
			}

			for _, obj := range result.Contents {
				if !mm.shouldMigrate(job.Config, &obj) {
					atomic.AddInt64(&job.Progress.SkippedObjects, 1)
					continue
				}

				// Skip if already processed (for resume)
				if job.Resume != nil && job.Resume.ProcessedKeys[obj.Key] {
					continue
				}

				wg.Add(1)
				go func(o S3Object) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					err := mm.migrateObject(activeJob, sourceBucket, destBucket, &o)
					if err != nil {
						errMu.Lock()
						if migrationErr == nil {
							migrationErr = err
						}
						errMu.Unlock()

						atomic.AddInt64(&job.Progress.FailedObjects, 1)
						mm.logFailedObject(sourceBucket, o.Key, err)
					} else {
						atomic.AddInt64(&job.Progress.MigratedObjects, 1)
						atomic.AddInt64(&job.Progress.MigratedBytes, o.Size)
					}

					// Update progress
					job.Progress.CurrentKey = o.Key
					job.Progress.LastUpdateTime = time.Now()

					elapsed := time.Since(job.Progress.StartTime)
					if elapsed > 0 {
						job.Progress.BytesPerSecond = float64(job.Progress.MigratedBytes) / elapsed.Seconds()
					}
					if job.Progress.BytesPerSecond > 0 {
						remaining := job.Progress.TotalBytes - job.Progress.MigratedBytes
						job.Progress.EstimatedTimeRemaining = time.Duration(float64(remaining) / job.Progress.BytesPerSecond * float64(time.Second))
					}
				}(obj)
			}

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
				LastBucket: sourceBucket,
				LastKey:    marker,
			}
			_ = mm.saveJobs()
		}
	}

	return nil
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
func (mm *MigrationManager) migrateObject(activeJob *activeMigrationJob, sourceBucket, destBucket string, obj *S3Object) error {
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
		destInfo, err := mm.destStorage.HeadObject(activeJob.ctx, destBucket, destKey)
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
	reader, info, err := activeJob.sourceClient.GetObject(activeJob.ctx, sourceBucket, obj.Key)
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

	if err := mm.destStorage.PutObject(activeJob.ctx, destBucket, destKey, limitedReader, info.Size, opts); err != nil {
		return fmt.Errorf("failed to put object: %w", err)
	}

	// Verify checksum if enabled
	if config.VerifyChecksum {
		destInfo, err := mm.destStorage.HeadObject(activeJob.ctx, destBucket, destKey)
		if err != nil {
			return fmt.Errorf("failed to verify object: %w", err)
		}
		if destInfo.ETag != strings.Trim(obj.ETag, "\"") {
			return fmt.Errorf("checksum mismatch: source=%s, dest=%s", obj.ETag, destInfo.ETag)
		}
	}

	return nil
}

// logFailedObject logs a failed object migration.
func (mm *MigrationManager) logFailedObject(bucket, key string, err error) {
	entry := FailedObject{
		Bucket:    bucket,
		Key:       key,
		Error:     err.Error(),
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(entry)
	_, _ = mm.failedLog.Write(append(data, '\n'))
}

// PauseMigration pauses the active migration.
func (mm *MigrationManager) PauseMigration() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if mm.activeJob == nil {
		return fmt.Errorf("no active migration")
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
		return fmt.Errorf("no active migration")
	}

	mm.activeJob.mu.Lock()
	if !mm.activeJob.isPaused {
		mm.activeJob.mu.Unlock()
		return fmt.Errorf("migration is not paused")
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
		return fmt.Errorf("no active migration")
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
		return nil, fmt.Errorf("no active migration")
	}

	return mm.activeJob.job.Progress, nil
}

// RateLimitedReader wraps a reader with rate limiting.
type RateLimitedReader struct {
	reader   io.Reader
	rate     int64 // bytes per second
	lastTime time.Time
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
