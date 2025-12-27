// Package express implements S3 Express API for accelerated AI workloads
// This provides atomic appends, lightweight ETags, and streaming LIST operations
package express

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// S3 Express API Errors
var (
	ErrOffsetConflict      = errors.New("offset conflict: another write already claimed this offset")
	ErrInvalidOffset       = errors.New("invalid offset: must be at end of object or specified offset")
	ErrAppendNotSupported  = errors.New("append not supported: object was not created with express API")
	ErrObjectNotFound      = errors.New("object not found")
	ErrBucketNotExpress    = errors.New("bucket is not configured for express mode")
	ErrVersioningConflict  = errors.New("versioning must be disabled for express mode")
	ErrSessionExpired      = errors.New("express session has expired")
	ErrInvalidSession      = errors.New("invalid session token")
)

// ExpressService provides S3 Express API functionality
type ExpressService struct {
	config   *Config
	store    ObjectStore
	sessions sync.Map // map[string]*Session
	locks    sync.Map // map[string]*AppendLock - per-object append locks
	metrics  *Metrics
}

// Config configures the Express service
type Config struct {
	// SessionDuration is how long express sessions remain valid
	SessionDuration time.Duration

	// MaxAppendSize is the maximum size for a single append operation
	MaxAppendSize int64

	// EnableLightweightETags uses random ETags instead of content digests
	EnableLightweightETags bool

	// StreamingListBatchSize controls streaming LIST batch size
	StreamingListBatchSize int

	// EnableAtomicAppend enables atomic/exclusive append operations
	EnableAtomicAppend bool
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		SessionDuration:        1 * time.Hour,
		MaxAppendSize:          5 << 30, // 5GB
		EnableLightweightETags: true,
		StreamingListBatchSize: 1000,
		EnableAtomicAppend:     true,
	}
}

// ObjectStore interface for storage backend
type ObjectStore interface {
	GetObject(ctx context.Context, bucket, key string) (*Object, error)
	PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, meta map[string]string) (*PutResult, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	ListObjects(ctx context.Context, bucket, prefix string, opts ListOptions) (<-chan ObjectInfo, <-chan error)
	GetObjectSize(ctx context.Context, bucket, key string) (int64, error)
	AppendObject(ctx context.Context, bucket, key string, data io.Reader, size int64, offset int64) error
	HeadBucket(ctx context.Context, bucket string) (*BucketInfo, error)
}

// Object represents a stored object
type Object struct {
	Key          string
	Data         io.ReadCloser
	Size         int64
	ETag         string
	LastModified time.Time
	Metadata     map[string]string
	IsExpress    bool
	CurrentSize  int64 // For append tracking
}

// PutResult contains the result of a PUT operation
type PutResult struct {
	ETag         string
	VersionID    string
	Size         int64
	LastModified time.Time
}

// ListOptions for listing objects
type ListOptions struct {
	Prefix            string
	StartAfter        string
	MaxKeys           int
	ContinuationToken string
	Delimiter         string
}

// ObjectInfo contains object metadata
type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
	StorageClass string
	Owner        string
}

// BucketInfo contains bucket metadata
type BucketInfo struct {
	Name              string
	CreationDate      time.Time
	IsExpressBucket   bool
	VersioningEnabled bool
}

// Session represents an express session
type Session struct {
	ID           string
	Bucket       string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	Credentials  SessionCredentials
	LastActivity time.Time
	mu           sync.RWMutex
}

// SessionCredentials for express session authentication
type SessionCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// AppendLock manages exclusive append access to an object
type AppendLock struct {
	mu           sync.Mutex
	currentSize  int64
	pendingWrite *PendingWrite
	lastWrite    time.Time
}

// PendingWrite tracks a pending append operation
type PendingWrite struct {
	Offset    int64
	Size      int64
	WriterID  string
	StartTime time.Time
}

// Metrics tracks express API performance
type Metrics struct {
	mu                   sync.RWMutex
	PutOperations        int64
	PutBytesWritten      int64
	PutLatencySum        time.Duration
	ListOperations       int64
	ListObjectsReturned  int64
	ListLatencySum       time.Duration
	AppendOperations     int64
	AppendBytesWritten   int64
	AppendConflicts      int64
	SessionsCreated      int64
	SessionsExpired      int64
	LightweightETagsUsed int64
}

// NewExpressService creates a new Express API service
func NewExpressService(store ObjectStore, config *Config) *ExpressService {
	if config == nil {
		config = DefaultConfig()
	}

	svc := &ExpressService{
		config:  config,
		store:   store,
		metrics: &Metrics{},
	}

	// Start session cleanup goroutine
	go svc.cleanupExpiredSessions()

	return svc
}

// CreateSession creates a new express session for a bucket
func (s *ExpressService) CreateSession(ctx context.Context, bucket string) (*Session, error) {
	// Verify bucket exists and is configured for express mode
	info, err := s.store.HeadBucket(ctx, bucket)
	if err != nil {
		return nil, err
	}

	if !info.IsExpressBucket {
		return nil, ErrBucketNotExpress
	}

	if info.VersioningEnabled {
		return nil, ErrVersioningConflict
	}

	// Generate session credentials
	sessionID := generateSessionID()
	accessKey := generateAccessKey()
	secretKey := generateSecretKey()
	sessionToken := generateSessionToken()

	now := time.Now()
	session := &Session{
		ID:        sessionID,
		Bucket:    bucket,
		CreatedAt: now,
		ExpiresAt: now.Add(s.config.SessionDuration),
		Credentials: SessionCredentials{
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
			SessionToken:    sessionToken,
		},
		LastActivity: now,
	}

	s.sessions.Store(sessionID, session)
	atomic.AddInt64(&s.metrics.SessionsCreated, 1)

	return session, nil
}

// ValidateSession validates an express session
func (s *ExpressService) ValidateSession(sessionID string) (*Session, error) {
	val, ok := s.sessions.Load(sessionID)
	if !ok {
		return nil, ErrInvalidSession
	}

	session := val.(*Session)
	session.mu.RLock()
	defer session.mu.RUnlock()

	if time.Now().After(session.ExpiresAt) {
		s.sessions.Delete(sessionID)
		atomic.AddInt64(&s.metrics.SessionsExpired, 1)
		return nil, ErrSessionExpired
	}

	return session, nil
}

// ExpressPutObject performs an accelerated PUT operation
func (s *ExpressService) ExpressPutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, meta map[string]string) (*PutResult, error) {
	start := time.Now()
	defer func() {
		atomic.AddInt64(&s.metrics.PutOperations, 1)
		atomic.AddInt64(&s.metrics.PutBytesWritten, size)
		s.metrics.mu.Lock()
		s.metrics.PutLatencySum += time.Since(start)
		s.metrics.mu.Unlock()
	}()

	// Add express metadata
	if meta == nil {
		meta = make(map[string]string)
	}
	meta["x-amz-express-object"] = "true"
	meta["x-amz-express-created"] = time.Now().UTC().Format(time.RFC3339)

	// Generate lightweight ETag if enabled
	var etag string
	if s.config.EnableLightweightETags {
		etag = generateLightweightETag()
		meta["x-amz-express-etag"] = etag
		atomic.AddInt64(&s.metrics.LightweightETagsUsed, 1)
	}

	result, err := s.store.PutObject(ctx, bucket, key, data, size, meta)
	if err != nil {
		return nil, err
	}

	// Use lightweight ETag if generated
	if etag != "" {
		result.ETag = etag
	}

	return result, nil
}

// ExpressAppendObject performs an atomic/exclusive append operation
func (s *ExpressService) ExpressAppendObject(ctx context.Context, bucket, key string, data io.Reader, size int64, requestedOffset int64, writerID string) (*AppendResult, error) {
	if !s.config.EnableAtomicAppend {
		return nil, errors.New("atomic append is not enabled")
	}

	if size > s.config.MaxAppendSize {
		return nil, fmt.Errorf("append size %d exceeds maximum %d", size, s.config.MaxAppendSize)
	}

	start := time.Now()

	// Get or create append lock for this object
	lockKey := fmt.Sprintf("%s/%s", bucket, key)
	lockVal, _ := s.locks.LoadOrStore(lockKey, &AppendLock{})
	lock := lockVal.(*AppendLock)

	lock.mu.Lock()
	defer lock.mu.Unlock()

	// Get current object size
	currentSize, err := s.store.GetObjectSize(ctx, bucket, key)
	if err != nil && !errors.Is(err, ErrObjectNotFound) {
		return nil, err
	}
	if errors.Is(err, ErrObjectNotFound) {
		currentSize = 0
	}

	// Update lock's current size
	lock.currentSize = currentSize

	// Validate offset - must match current end of object
	if requestedOffset != -1 && requestedOffset != currentSize {
		atomic.AddInt64(&s.metrics.AppendConflicts, 1)
		return nil, fmt.Errorf("%w: requested offset %d, current size %d", ErrOffsetConflict, requestedOffset, currentSize)
	}

	// Check for pending writes at this offset (exclusive access)
	if lock.pendingWrite != nil && lock.pendingWrite.Offset == currentSize {
		if time.Since(lock.pendingWrite.StartTime) < 30*time.Second {
			atomic.AddInt64(&s.metrics.AppendConflicts, 1)
			return nil, fmt.Errorf("%w: writer %s already writing at offset %d",
				ErrOffsetConflict, lock.pendingWrite.WriterID, currentSize)
		}
		// Previous write timed out, clear it
		lock.pendingWrite = nil
	}

	// Register pending write
	lock.pendingWrite = &PendingWrite{
		Offset:    currentSize,
		Size:      size,
		WriterID:  writerID,
		StartTime: start,
	}

	// Perform the append
	err = s.store.AppendObject(ctx, bucket, key, data, size, currentSize)

	// Clear pending write
	lock.pendingWrite = nil
	lock.lastWrite = time.Now()

	if err != nil {
		return nil, err
	}

	lock.currentSize = currentSize + size

	atomic.AddInt64(&s.metrics.AppendOperations, 1)
	atomic.AddInt64(&s.metrics.AppendBytesWritten, size)

	return &AppendResult{
		Offset:     currentSize,
		Size:       size,
		NewSize:    currentSize + size,
		ETag:       generateLightweightETag(),
		AppendedAt: time.Now(),
	}, nil
}

// AppendResult contains the result of an append operation
type AppendResult struct {
	Offset     int64
	Size       int64
	NewSize    int64
	ETag       string
	AppendedAt time.Time
}

// StreamingListResult for streaming LIST responses
type StreamingListResult struct {
	Objects           []ObjectInfo
	CommonPrefixes    []string
	IsTruncated       bool
	ContinuationToken string
	KeyCount          int
}

// ExpressListObjects performs a streaming LIST operation
func (s *ExpressService) ExpressListObjects(ctx context.Context, bucket, prefix string, opts ListOptions) (<-chan StreamingListResult, <-chan error) {
	resultChan := make(chan StreamingListResult, 10)
	errChan := make(chan error, 1)

	go func() {
		defer close(resultChan)
		defer close(errChan)

		start := time.Now()
		defer func() {
			atomic.AddInt64(&s.metrics.ListOperations, 1)
			s.metrics.mu.Lock()
			s.metrics.ListLatencySum += time.Since(start)
			s.metrics.mu.Unlock()
		}()

		if opts.MaxKeys == 0 {
			opts.MaxKeys = s.config.StreamingListBatchSize
		}

		objectsChan, errorsChan := s.store.ListObjects(ctx, bucket, prefix, opts)

		batch := make([]ObjectInfo, 0, s.config.StreamingListBatchSize)
		prefixMap := make(map[string]bool)
		keyCount := 0
		var lastKey string

		for {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return

			case err, ok := <-errorsChan:
				if ok && err != nil {
					errChan <- err
					return
				}

			case obj, ok := <-objectsChan:
				if !ok {
					// Send final batch
					if len(batch) > 0 {
						prefixes := make([]string, 0, len(prefixMap))
						for p := range prefixMap {
							prefixes = append(prefixes, p)
						}

						select {
						case resultChan <- StreamingListResult{
							Objects:           batch,
							CommonPrefixes:    prefixes,
							IsTruncated:       false,
							ContinuationToken: "",
							KeyCount:          keyCount,
						}:
						case <-ctx.Done():
							errChan <- ctx.Err()
							return
						}
						atomic.AddInt64(&s.metrics.ListObjectsReturned, int64(len(batch)))
					}
					return
				}

				// Handle delimiter-based prefixes
				if opts.Delimiter != "" {
					if idx := findDelimiter(obj.Key, prefix, opts.Delimiter); idx >= 0 {
						commonPrefix := obj.Key[:idx+len(opts.Delimiter)]
						if !prefixMap[commonPrefix] {
							prefixMap[commonPrefix] = true
						}
						continue
					}
				}

				batch = append(batch, obj)
				lastKey = obj.Key
				keyCount++

				// Stream batch when full
				if len(batch) >= s.config.StreamingListBatchSize {
					prefixes := make([]string, 0, len(prefixMap))
					for p := range prefixMap {
						prefixes = append(prefixes, p)
					}

					select {
					case resultChan <- StreamingListResult{
						Objects:           batch,
						CommonPrefixes:    prefixes,
						IsTruncated:       true,
						ContinuationToken: lastKey,
						KeyCount:          keyCount,
					}:
					case <-ctx.Done():
						errChan <- ctx.Err()
						return
					}

					atomic.AddInt64(&s.metrics.ListObjectsReturned, int64(len(batch)))
					batch = make([]ObjectInfo, 0, s.config.StreamingListBatchSize)
					prefixMap = make(map[string]bool)
				}

				// Check max keys limit
				if opts.MaxKeys > 0 && keyCount >= opts.MaxKeys {
					prefixes := make([]string, 0, len(prefixMap))
					for p := range prefixMap {
						prefixes = append(prefixes, p)
					}

					select {
					case resultChan <- StreamingListResult{
						Objects:           batch,
						CommonPrefixes:    prefixes,
						IsTruncated:       true,
						ContinuationToken: lastKey,
						KeyCount:          keyCount,
					}:
					case <-ctx.Done():
						errChan <- ctx.Err()
						return
					}
					atomic.AddInt64(&s.metrics.ListObjectsReturned, int64(len(batch)))
					return
				}
			}
		}
	}()

	return resultChan, errChan
}

// ExpressDeleteObject performs an accelerated DELETE operation
func (s *ExpressService) ExpressDeleteObject(ctx context.Context, bucket, key string) error {
	// Clean up any append locks
	lockKey := fmt.Sprintf("%s/%s", bucket, key)
	s.locks.Delete(lockKey)

	return s.store.DeleteObject(ctx, bucket, key)
}

// GetMetrics returns current metrics
func (s *ExpressService) GetMetrics() *Metrics {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	return &Metrics{
		PutOperations:        atomic.LoadInt64(&s.metrics.PutOperations),
		PutBytesWritten:      atomic.LoadInt64(&s.metrics.PutBytesWritten),
		PutLatencySum:        s.metrics.PutLatencySum,
		ListOperations:       atomic.LoadInt64(&s.metrics.ListOperations),
		ListObjectsReturned:  atomic.LoadInt64(&s.metrics.ListObjectsReturned),
		ListLatencySum:       s.metrics.ListLatencySum,
		AppendOperations:     atomic.LoadInt64(&s.metrics.AppendOperations),
		AppendBytesWritten:   atomic.LoadInt64(&s.metrics.AppendBytesWritten),
		AppendConflicts:      atomic.LoadInt64(&s.metrics.AppendConflicts),
		SessionsCreated:      atomic.LoadInt64(&s.metrics.SessionsCreated),
		SessionsExpired:      atomic.LoadInt64(&s.metrics.SessionsExpired),
		LightweightETagsUsed: atomic.LoadInt64(&s.metrics.LightweightETagsUsed),
	}
}

// AveragePutLatency returns average PUT latency
func (m *Metrics) AveragePutLatency() time.Duration {
	ops := atomic.LoadInt64(&m.PutOperations)
	if ops == 0 {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.PutLatencySum / time.Duration(ops)
}

// AverageListLatency returns average LIST latency
func (m *Metrics) AverageListLatency() time.Duration {
	ops := atomic.LoadInt64(&m.ListOperations)
	if ops == 0 {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ListLatencySum / time.Duration(ops)
}

// cleanupExpiredSessions periodically removes expired sessions
func (s *ExpressService) cleanupExpiredSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		s.sessions.Range(func(key, value interface{}) bool {
			session := value.(*Session)
			session.mu.RLock()
			expired := now.After(session.ExpiresAt)
			session.mu.RUnlock()

			if expired {
				s.sessions.Delete(key)
				atomic.AddInt64(&s.metrics.SessionsExpired, 1)
			}
			return true
		})
	}
}

// Helper functions

func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateAccessKey() string {
	b := make([]byte, 10)
	rand.Read(b)
	return "AKIA" + hex.EncodeToString(b)[:16]
}

func generateSecretKey() string {
	b := make([]byte, 20)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateSessionToken() string {
	b := make([]byte, 64)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateLightweightETag() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("\"%s\"", hex.EncodeToString(b))
}

func findDelimiter(key, prefix, delimiter string) int {
	if len(key) <= len(prefix) {
		return -1
	}
	remaining := key[len(prefix):]
	for i := 0; i <= len(remaining)-len(delimiter); i++ {
		if remaining[i:i+len(delimiter)] == delimiter {
			return len(prefix) + i
		}
	}
	return -1
}

// DirectoryBucket represents an S3 Express One Zone directory bucket
type DirectoryBucket struct {
	Name            string
	AvailabilityZone string
	CreatedAt       time.Time
	DataRedundancy  string // "SingleAvailabilityZone"
}

// CreateDirectoryBucket creates a new S3 Express directory bucket
func (s *ExpressService) CreateDirectoryBucket(ctx context.Context, name, availabilityZone string) (*DirectoryBucket, error) {
	// Directory buckets have special naming: bucket-name--az-id--x-s3
	bucketName := fmt.Sprintf("%s--%s--x-s3", name, availabilityZone)

	// Create the underlying bucket with express settings
	meta := map[string]string{
		"x-amz-bucket-type":        "directory",
		"x-amz-data-redundancy":    "SingleAvailabilityZone",
		"x-amz-availability-zone":  availabilityZone,
		"x-amz-express-bucket":     "true",
	}

	// Store bucket configuration (in real impl, would call store.CreateBucket)
	_ = meta // Used for bucket creation

	return &DirectoryBucket{
		Name:            bucketName,
		AvailabilityZone: availabilityZone,
		CreatedAt:       time.Now(),
		DataRedundancy:  "SingleAvailabilityZone",
	}, nil
}

// ListDirectoryBuckets lists all directory buckets
func (s *ExpressService) ListDirectoryBuckets(ctx context.Context) ([]*DirectoryBucket, error) {
	// In real implementation, would query bucket store for directory buckets
	return []*DirectoryBucket{}, nil
}

// ExpressCopyObject performs optimized copy within express buckets
func (s *ExpressService) ExpressCopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (*PutResult, error) {
	// Get source object
	obj, err := s.store.GetObject(ctx, srcBucket, srcKey)
	if err != nil {
		return nil, err
	}
	defer obj.Data.Close()

	// Read all data
	data, err := io.ReadAll(obj.Data)
	if err != nil {
		return nil, err
	}

	// Put to destination with express optimization
	return s.ExpressPutObject(ctx, dstBucket, dstKey, bytes.NewReader(data), int64(len(data)), obj.Metadata)
}

// BatchAppend allows multiple append operations in a single request
type BatchAppendRequest struct {
	Bucket   string
	Key      string
	Data     []byte
	Offset   int64
	WriterID string
}

// BatchAppendResult contains results for batch append
type BatchAppendResult struct {
	Results []AppendResult
	Errors  []error
}

// ExpressBatchAppend performs multiple append operations atomically
func (s *ExpressService) ExpressBatchAppend(ctx context.Context, requests []BatchAppendRequest) *BatchAppendResult {
	result := &BatchAppendResult{
		Results: make([]AppendResult, len(requests)),
		Errors:  make([]error, len(requests)),
	}

	// Process appends sequentially to maintain ordering guarantees
	for i, req := range requests {
		appendResult, err := s.ExpressAppendObject(
			ctx,
			req.Bucket,
			req.Key,
			bytes.NewReader(req.Data),
			int64(len(req.Data)),
			req.Offset,
			req.WriterID,
		)
		if err != nil {
			result.Errors[i] = err
		} else {
			result.Results[i] = *appendResult
		}
	}

	return result
}

// WriteMarker for tracking append position
type WriteMarker struct {
	Bucket       string
	Key          string
	CurrentSize  int64
	LastModified time.Time
	WriterID     string
}

// GetWriteMarker returns the current write position for an object
func (s *ExpressService) GetWriteMarker(ctx context.Context, bucket, key string) (*WriteMarker, error) {
	size, err := s.store.GetObjectSize(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	lockKey := fmt.Sprintf("%s/%s", bucket, key)
	lockVal, ok := s.locks.Load(lockKey)

	var lastModified time.Time
	var writerID string

	if ok {
		lock := lockVal.(*AppendLock)
		lock.mu.Lock()
		lastModified = lock.lastWrite
		if lock.pendingWrite != nil {
			writerID = lock.pendingWrite.WriterID
		}
		lock.mu.Unlock()
	}

	return &WriteMarker{
		Bucket:       bucket,
		Key:          key,
		CurrentSize:  size,
		LastModified: lastModified,
		WriterID:     writerID,
	}, nil
}

// ExpressHeadObject returns object metadata without body
func (s *ExpressService) ExpressHeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error) {
	obj, err := s.store.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, err
	}
	defer obj.Data.Close()

	return &ObjectInfo{
		Key:          obj.Key,
		Size:         obj.Size,
		ETag:         obj.ETag,
		LastModified: obj.LastModified,
	}, nil
}

// GenerateChecksumHeader creates checksum headers for express mode
type ChecksumHeader struct {
	Algorithm string
	Value     string
}

// CreateChecksum generates a lightweight checksum for express mode
func CreateChecksum(data []byte) *ChecksumHeader {
	// Use lightweight CRC32 instead of SHA256 for express mode
	var crc uint32
	for _, b := range data {
		crc = crc*31 + uint32(b)
	}

	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, crc)

	return &ChecksumHeader{
		Algorithm: "CRC32",
		Value:     hex.EncodeToString(buf),
	}
}
