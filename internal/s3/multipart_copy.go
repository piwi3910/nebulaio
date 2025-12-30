// Package s3 provides S3-compatible API implementations for NebulaIO.
package s3

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: MD5 required for S3 ETag compatibility
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CopyPartResult represents the result of a copy part operation.
type CopyPartResult struct {
	XMLName      xml.Name `xml:"CopyPartResult"`
	ETag         string   `xml:"ETag"`
	LastModified string   `xml:"LastModified"`
}

// CopyObjectResult represents the result of a copy object operation.
type CopyObjectResult struct {
	XMLName      xml.Name `xml:"CopyObjectResult"`
	ETag         string   `xml:"ETag"`
	LastModified string   `xml:"LastModified"`
}

// MultipartCopyOptions holds options for multipart copy operations.
type MultipartCopyOptions struct {
	CopySourceIfModifiedSince      *time.Time
	ObjectLockRetainUntil          *time.Time
	Metadata                       map[string]string
	CopySourceIfUnmodifiedSince    *time.Time
	CopySourceSSECustomerAlgorithm string
	SSECustomerAlgorithm           string
	ObjectLockLegalHoldStatus      string
	CopySourceIfMatch              string
	CopySourceIfNoneMatch          string
	DestKey                        string
	DestBucket                     string
	SourceVersionID                string
	MetadataDirective              string
	ContentType                    string
	StorageClass                   string
	SourceKey                      string
	SSECustomerKey                 string
	SSECustomerKeyMD5              string
	SourceBucket                   string
	CopySourceSSECustomerKey       string
	CopySourceSSECustomerKeyMD5    string
	Tagging                        string
	TaggingDirective               string
	ObjectLockMode                 string
	PartSize                       int64
	MaxConcurrency                 int
}

// MultipartCopyManager handles multipart copy operations.
type MultipartCopyManager struct {
	storage      ObjectStorage
	activeCopies map[string]context.CancelFunc
	mu           sync.RWMutex
}

// ObjectStorage interface for storage operations.
type ObjectStorage interface {
	// Object operations
	GetObject(ctx context.Context, bucket, key string, opts *GetObjectOptions) (*Object, error)
	GetObjectRange(ctx context.Context, bucket, key string, start, end int64) (io.ReadCloser, error)
	PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, opts *PutObjectOptions) (*PutObjectResult, error)
	HeadObject(ctx context.Context, bucket, key string) (*ObjectMetadata, error)

	// Multipart operations
	CreateMultipartUpload(ctx context.Context, bucket, key string, opts *MultipartUploadOptions) (*MultipartUpload, error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*PartInfo, error)
	UploadPartCopy(ctx context.Context, params *UploadPartCopyParams) (*CopyPartResult, error)
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) (*CompleteMultipartUploadResult, error)
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
}

// Object represents a stored object.
type Object struct {
	LastModified time.Time
	Body         io.ReadCloser
	Metadata     map[string]string
	Key          string
	ETag         string
	ContentType  string
	Size         int64
}

// GetObjectOptions holds options for GetObject.
type GetObjectOptions struct {
	VersionID   string
	Range       string
	IfMatch     string
	IfNoneMatch string
	PartNumber  int
}

// PutObjectOptions holds options for PutObject.
type PutObjectOptions struct {
	ContentType         string
	ContentEncoding     string
	ContentDisposition  string
	CacheControl        string
	Metadata            map[string]string
	StorageClass        string
	Tagging             string
	SSEAlgorithm        string
	SSECustomerKey      string
	ObjectLockMode      string
	ObjectLockRetain    *time.Time
	ObjectLockLegalHold string
}

// PutObjectResult represents the result of a PutObject operation.
type PutObjectResult struct {
	ETag      string
	VersionID string
}

// ObjectMetadata contains object metadata.
type ObjectMetadata struct {
	LastModified              time.Time
	Metadata                  map[string]string
	ObjectLockRetainUntilDate *time.Time
	Key                       string
	ETag                      string
	ContentType               string
	StorageClass              string
	VersionID                 string
	ObjectLockMode            string
	ObjectLockLegalHoldStatus string
	Size                      int64
	PartsCount                int
}

// MultipartUploadOptions holds options for creating multipart uploads.
type MultipartUploadOptions struct {
	ContentType         string
	Metadata            map[string]string
	StorageClass        string
	SSEAlgorithm        string
	SSECustomerKey      string
	Tagging             string
	ObjectLockMode      string
	ObjectLockRetain    *time.Time
	ObjectLockLegalHold string
}

// MultipartUpload represents an active multipart upload.
type MultipartUpload struct {
	Initiated time.Time
	UploadID  string
	Bucket    string
	Key       string
}

// PartInfo contains information about an uploaded part.
type PartInfo struct {
	ETag       string
	PartNumber int
	Size       int64
}

// CompletedPart represents a completed part for CompleteMultipartUpload.
type CompletedPart struct {
	ETag       string `xml:"ETag"`
	PartNumber int    `xml:"PartNumber"`
}

// CompleteMultipartUploadResult contains the result of completing a multipart upload.
type CompleteMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

// UploadPartCopyParams holds parameters for UploadPartCopy.
type UploadPartCopyParams struct {
	CopySourceIfModifiedSince      *time.Time
	CopySourceIfUnmodifiedSince    *time.Time
	CopySourceIfMatch              string
	CopySourceIfNoneMatch          string
	SourceBucket                   string
	SourceKey                      string
	SourceVersionID                string
	CopySourceRange                string
	DestBucket                     string
	CopySourceSSECustomerKey       string
	UploadID                       string
	DestKey                        string
	SSECustomerAlgorithm           string
	SSECustomerKey                 string
	CopySourceSSECustomerAlgorithm string
	PartNumber                     int
}

// DefaultPartSize is the default part size for multipart copies (64MB).
const DefaultPartSize = 64 * 1024 * 1024

// MinPartSize is the minimum part size (5MB).
const MinPartSize = 5 * 1024 * 1024

// MaxPartSize is the maximum part size (5GB).
const MaxPartSize = 5 * 1024 * 1024 * 1024

// MaxParts is the maximum number of parts allowed.
const MaxParts = 10000

// MultipartCopyThreshold is the size above which multipart copy is used (5GB).
const MultipartCopyThreshold = 5 * 1024 * 1024 * 1024

// NewMultipartCopyManager creates a new multipart copy manager.
func NewMultipartCopyManager(storage ObjectStorage) *MultipartCopyManager {
	return &MultipartCopyManager{
		storage:      storage,
		activeCopies: make(map[string]context.CancelFunc),
	}
}

// CopyObject copies an object, using multipart copy for large objects.
func (m *MultipartCopyManager) CopyObject(ctx context.Context, opts *MultipartCopyOptions) (*CopyObjectResult, error) {
	// Get source object metadata
	_ = opts.SourceBucket + "/" + opts.SourceKey // sourcePath for logging if needed

	metadata, err := m.storage.HeadObject(ctx, opts.SourceBucket, opts.SourceKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get source object: %w", err)
	}

	// Check copy conditions
	if err := m.checkCopyConditions(metadata, opts); err != nil {
		return nil, err
	}

	// For small objects, use simple copy
	if metadata.Size < MultipartCopyThreshold {
		return m.simpleCopy(ctx, opts, metadata)
	}

	// For large objects, use multipart copy
	return m.multipartCopy(ctx, opts, metadata)
}

// checkCopyConditions verifies copy conditions.
func (m *MultipartCopyManager) checkCopyConditions(metadata *ObjectMetadata, opts *MultipartCopyOptions) error {
	// CopySourceIfMatch
	if opts.CopySourceIfMatch != "" {
		if metadata.ETag != opts.CopySourceIfMatch {
			return &S3Error{
				Code:       "PreconditionFailed",
				Message:    "At least one of the pre-conditions you specified did not hold",
				StatusCode: http.StatusPreconditionFailed,
			}
		}
	}

	// CopySourceIfNoneMatch
	if opts.CopySourceIfNoneMatch != "" {
		if metadata.ETag == opts.CopySourceIfNoneMatch {
			return &S3Error{
				Code:       "PreconditionFailed",
				Message:    "At least one of the pre-conditions you specified did not hold",
				StatusCode: http.StatusPreconditionFailed,
			}
		}
	}

	// CopySourceIfModifiedSince
	if opts.CopySourceIfModifiedSince != nil {
		if !metadata.LastModified.After(*opts.CopySourceIfModifiedSince) {
			return &S3Error{
				Code:       "PreconditionFailed",
				Message:    "At least one of the pre-conditions you specified did not hold",
				StatusCode: http.StatusPreconditionFailed,
			}
		}
	}

	// CopySourceIfUnmodifiedSince
	if opts.CopySourceIfUnmodifiedSince != nil {
		if metadata.LastModified.After(*opts.CopySourceIfUnmodifiedSince) {
			return &S3Error{
				Code:       "PreconditionFailed",
				Message:    "At least one of the pre-conditions you specified did not hold",
				StatusCode: http.StatusPreconditionFailed,
			}
		}
	}

	return nil
}

// simpleCopy performs a simple server-side copy for small objects.
func (m *MultipartCopyManager) simpleCopy(ctx context.Context, opts *MultipartCopyOptions, sourceMetadata *ObjectMetadata) (*CopyObjectResult, error) {
	// Get source object data
	getOpts := &GetObjectOptions{
		VersionID: opts.SourceVersionID,
	}

	obj, err := m.storage.GetObject(ctx, opts.SourceBucket, opts.SourceKey, getOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to get source object: %w", err)
	}

	defer func() { _ = obj.Body.Close() }()

	// Determine metadata
	metadata := sourceMetadata.Metadata
	contentType := sourceMetadata.ContentType

	if opts.MetadataDirective == "REPLACE" {
		metadata = opts.Metadata
		if opts.ContentType != "" {
			contentType = opts.ContentType
		}
	}

	// Put object to destination
	putOpts := &PutObjectOptions{
		ContentType:  contentType,
		Metadata:     metadata,
		StorageClass: opts.StorageClass,
	}

	if opts.TaggingDirective == "REPLACE" && opts.Tagging != "" {
		putOpts.Tagging = opts.Tagging
	}

	if opts.ObjectLockMode != "" {
		putOpts.ObjectLockMode = opts.ObjectLockMode
		putOpts.ObjectLockRetain = opts.ObjectLockRetainUntil
	}

	if opts.ObjectLockLegalHoldStatus != "" {
		putOpts.ObjectLockLegalHold = opts.ObjectLockLegalHoldStatus
	}

	result, err := m.storage.PutObject(ctx, opts.DestBucket, opts.DestKey, obj.Body, sourceMetadata.Size, putOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to copy object: %w", err)
	}

	return &CopyObjectResult{
		ETag:         result.ETag,
		LastModified: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// multipartCopy performs a multipart copy for large objects.
func (m *MultipartCopyManager) multipartCopy(ctx context.Context, opts *MultipartCopyOptions, sourceMetadata *ObjectMetadata) (*CopyObjectResult, error) {
	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	copyID := uuid.New().String()

	m.mu.Lock()
	m.activeCopies[copyID] = cancel
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.activeCopies, copyID)
		m.mu.Unlock()
	}()

	// Determine part size
	partSize := opts.PartSize
	if partSize == 0 {
		partSize = DefaultPartSize
	}

	if partSize < MinPartSize {
		partSize = MinPartSize
	}

	if partSize > MaxPartSize {
		partSize = MaxPartSize
	}

	// Calculate number of parts
	numParts := (sourceMetadata.Size + partSize - 1) / partSize
	if numParts > MaxParts {
		// Increase part size to fit within MaxParts
		partSize = (sourceMetadata.Size + MaxParts - 1) / MaxParts
		numParts = (sourceMetadata.Size + partSize - 1) / partSize
	}

	// Determine metadata
	metadata := sourceMetadata.Metadata
	contentType := sourceMetadata.ContentType

	if opts.MetadataDirective == "REPLACE" {
		metadata = opts.Metadata
		if opts.ContentType != "" {
			contentType = opts.ContentType
		}
	}

	// Initiate multipart upload
	mpOpts := &MultipartUploadOptions{
		ContentType:  contentType,
		Metadata:     metadata,
		StorageClass: opts.StorageClass,
	}

	if opts.TaggingDirective == "REPLACE" && opts.Tagging != "" {
		mpOpts.Tagging = opts.Tagging
	}

	upload, err := m.storage.CreateMultipartUpload(ctx, opts.DestBucket, opts.DestKey, mpOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create multipart upload: %w", err)
	}

	// Channel for part results
	type partResult struct {
		err        error
		result     *CopyPartResult
		partNumber int
	}

	results := make(chan partResult, numParts)

	// Semaphore for concurrency control
	maxConcurrency := opts.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 10
	}

	sem := make(chan struct{}, maxConcurrency)

	// Copy parts concurrently
	var wg sync.WaitGroup
	for i := range numParts {
		wg.Add(1)

		go func(partNum int64) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results <- partResult{partNumber: int(partNum + 1), err: ctx.Err()}
				return
			}

			defer func() { <-sem }()

			// Calculate byte range
			start := partNum * partSize

			end := start + partSize - 1
			if end >= sourceMetadata.Size {
				end = sourceMetadata.Size - 1
			}

			// Create copy source range
			copyRange := fmt.Sprintf("bytes=%d-%d", start, end)

			// Copy part
			params := &UploadPartCopyParams{
				DestBucket:                     opts.DestBucket,
				DestKey:                        opts.DestKey,
				UploadID:                       upload.UploadID,
				PartNumber:                     int(partNum + 1),
				SourceBucket:                   opts.SourceBucket,
				SourceKey:                      opts.SourceKey,
				SourceVersionID:                opts.SourceVersionID,
				CopySourceRange:                copyRange,
				SSECustomerAlgorithm:           opts.SSECustomerAlgorithm,
				SSECustomerKey:                 opts.SSECustomerKey,
				CopySourceSSECustomerAlgorithm: opts.CopySourceSSECustomerAlgorithm,
				CopySourceSSECustomerKey:       opts.CopySourceSSECustomerKey,
			}

			result, err := m.storage.UploadPartCopy(ctx, params)
			if err != nil {
				results <- partResult{partNumber: int(partNum + 1), err: err}
				return
			}

			results <- partResult{partNumber: int(partNum + 1), result: result}
		}(i)
	}

	// Wait for all parts and close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	completedParts := make([]CompletedPart, 0, numParts)

	var copyErr error

	for result := range results {
		if result.err != nil {
			if copyErr == nil {
				copyErr = result.err

				cancel() // Cancel remaining parts
			}

			continue
		}

		completedParts = append(completedParts, CompletedPart{
			PartNumber: result.partNumber,
			ETag:       result.result.ETag,
		})
	}

	// Handle errors
	if copyErr != nil {
		// Abort multipart upload
		_ = m.storage.AbortMultipartUpload(context.Background(), opts.DestBucket, opts.DestKey, upload.UploadID)
		return nil, fmt.Errorf("multipart copy failed: %w", copyErr)
	}

	// Sort parts by part number
	sortParts(completedParts)

	// Complete multipart upload
	completeResult, err := m.storage.CompleteMultipartUpload(ctx, opts.DestBucket, opts.DestKey, upload.UploadID, completedParts)
	if err != nil {
		// Abort multipart upload
		_ = m.storage.AbortMultipartUpload(context.Background(), opts.DestBucket, opts.DestKey, upload.UploadID)
		return nil, fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	return &CopyObjectResult{
		ETag:         completeResult.ETag,
		LastModified: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// sortParts sorts completed parts by part number.
func sortParts(parts []CompletedPart) {
	// Simple insertion sort for small arrays
	for i := 1; i < len(parts); i++ {
		key := parts[i]

		j := i - 1
		for j >= 0 && parts[j].PartNumber > key.PartNumber {
			parts[j+1] = parts[j]
			j--
		}

		parts[j+1] = key
	}
}

// CancelCopy cancels an active copy operation.
func (m *MultipartCopyManager) CancelCopy(copyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, exists := m.activeCopies[copyID]; exists {
		cancel()
		delete(m.activeCopies, copyID)

		return nil
	}

	return fmt.Errorf("copy %s not found", copyID)
}

// S3Error represents an S3 API error.
type S3Error struct {
	Code       string
	Message    string
	Resource   string
	RequestID  string
	StatusCode int
}

func (e *S3Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ParseCopySource parses the X-Amz-Copy-Source header.
func ParseCopySource(copySource string) (bucket, key, versionID string, err error) {
	// Remove leading slash if present
	copySource = strings.TrimPrefix(copySource, "/")

	// URL decode
	decoded, err := url.QueryUnescape(copySource)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid copy source encoding: %w", err)
	}

	// Check for version ID
	parts := strings.SplitN(decoded, "?versionId=", 2)
	if len(parts) == 2 {
		versionID = parts[1]
	}

	// Split bucket and key
	pathParts := strings.SplitN(parts[0], "/", 2)
	if len(pathParts) != 2 {
		return "", "", "", errors.New("invalid copy source format")
	}

	return pathParts[0], pathParts[1], versionID, nil
}

// ParseCopySourceRange parses the X-Amz-Copy-Source-Range header.
func ParseCopySourceRange(rangeHeader string) (start, end int64, err error) {
	if rangeHeader == "" {
		return 0, 0, nil
	}

	// Format: bytes=start-end
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, errors.New("invalid range format")
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")

	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid range format")
	}

	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range start: %w", err)
	}

	end, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range end: %w", err)
	}

	if start > end {
		return 0, 0, errors.New("range start must be less than or equal to end")
	}

	return start, end, nil
}

// CalculateMultipartETag calculates the ETag for a multipart object.
func CalculateMultipartETag(partETags []string) string {
	// Remove quotes from ETags
	var cleanETags []string
	for _, etag := range partETags {
		cleanETags = append(cleanETags, strings.Trim(etag, "\""))
	}

	// Concatenate MD5 hashes
	var hashData []byte

	for _, etag := range cleanETags {
		decoded, err := hex.DecodeString(etag)
		if err != nil {
			continue
		}

		hashData = append(hashData, decoded...)
	}

	// Calculate MD5 of concatenated hashes
	finalHash := md5.Sum(hashData) //nolint:gosec // G401: MD5 required for S3 ETag compatibility

	return fmt.Sprintf("\"%s-%d\"", hex.EncodeToString(finalHash[:]), len(partETags))
}

// CalculateMD5 calculates the MD5 hash of data.
func CalculateMD5(data []byte) string {
	hash := md5.Sum(data) //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	return hex.EncodeToString(hash[:])
}

// CalculateMD5Base64 calculates the base64-encoded MD5 hash of data.
func CalculateMD5Base64(data []byte) string {
	hash := md5.Sum(data) //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	return base64.StdEncoding.EncodeToString(hash[:])
}

// MultipartCopyHandler handles HTTP requests for multipart copy operations.
type MultipartCopyHandler struct {
	manager *MultipartCopyManager
}

// NewMultipartCopyHandler creates a new HTTP handler for multipart copies.
func NewMultipartCopyHandler(manager *MultipartCopyManager) *MultipartCopyHandler {
	return &MultipartCopyHandler{manager: manager}
}

// HandleUploadPartCopy handles the UploadPartCopy API request.
func (h *MultipartCopyHandler) HandleUploadPartCopy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request parameters
	bucket := r.PathValue("bucket")
	key := r.PathValue("key")
	uploadID := r.URL.Query().Get("uploadId")
	partNumberStr := r.URL.Query().Get("partNumber")

	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber < 1 || partNumber > MaxParts {
		writeS3Error(w, &S3Error{
			Code:       "InvalidPart",
			Message:    "Part number must be an integer between 1 and 10000",
			StatusCode: http.StatusBadRequest,
		})

		return
	}

	// Parse copy source
	copySource := r.Header.Get("X-Amz-Copy-Source")
	if copySource == "" {
		writeS3Error(w, &S3Error{
			Code:       "InvalidArgument",
			Message:    "Copy Source must be specified",
			StatusCode: http.StatusBadRequest,
		})

		return
	}

	sourceBucket, sourceKey, sourceVersionID, err := ParseCopySource(copySource)
	if err != nil {
		writeS3Error(w, &S3Error{
			Code:       "InvalidArgument",
			Message:    err.Error(),
			StatusCode: http.StatusBadRequest,
		})

		return
	}

	// Parse copy source range
	copyRange := r.Header.Get("X-Amz-Copy-Source-Range")

	// Build parameters
	params := &UploadPartCopyParams{
		DestBucket:      bucket,
		DestKey:         key,
		UploadID:        uploadID,
		PartNumber:      partNumber,
		SourceBucket:    sourceBucket,
		SourceKey:       sourceKey,
		SourceVersionID: sourceVersionID,
		CopySourceRange: copyRange,
	}

	// Parse conditional headers
	if h := r.Header.Get("X-Amz-Copy-Source-If-Match"); h != "" {
		params.CopySourceIfMatch = h
	}

	if h := r.Header.Get("X-Amz-Copy-Source-If-None-Match"); h != "" {
		params.CopySourceIfNoneMatch = h
	}

	if h := r.Header.Get("X-Amz-Copy-Source-If-Modified-Since"); h != "" {
		if t, err := time.Parse(time.RFC1123, h); err == nil {
			params.CopySourceIfModifiedSince = &t
		}
	}

	if h := r.Header.Get("X-Amz-Copy-Source-If-Unmodified-Since"); h != "" {
		if t, err := time.Parse(time.RFC1123, h); err == nil {
			params.CopySourceIfUnmodifiedSince = &t
		}
	}

	// Parse SSE headers
	params.SSECustomerAlgorithm = r.Header.Get("X-Amz-Server-Side-Encryption-Customer-Algorithm")
	params.SSECustomerKey = r.Header.Get("X-Amz-Server-Side-Encryption-Customer-Key")
	params.CopySourceSSECustomerAlgorithm = r.Header.Get("X-Amz-Copy-Source-Server-Side-Encryption-Customer-Algorithm")
	params.CopySourceSSECustomerKey = r.Header.Get("X-Amz-Copy-Source-Server-Side-Encryption-Customer-Key")

	// Execute copy
	result, err := h.manager.storage.UploadPartCopy(ctx, params)
	if err != nil {
		if s3Err, ok := err.(*S3Error); ok {
			writeS3Error(w, s3Err)
		} else {
			writeS3Error(w, &S3Error{
				Code:       "InternalError",
				Message:    err.Error(),
				StatusCode: http.StatusInternalServerError,
			})
		}

		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(result)
}

// HandleCopyObject handles the CopyObject API request.
func (h *MultipartCopyHandler) HandleCopyObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request parameters
	destBucket := r.PathValue("bucket")
	destKey := r.PathValue("key")

	// Parse copy source
	copySource := r.Header.Get("X-Amz-Copy-Source")
	if copySource == "" {
		writeS3Error(w, &S3Error{
			Code:       "InvalidArgument",
			Message:    "Copy Source must be specified",
			StatusCode: http.StatusBadRequest,
		})

		return
	}

	sourceBucket, sourceKey, sourceVersionID, err := ParseCopySource(copySource)
	if err != nil {
		writeS3Error(w, &S3Error{
			Code:       "InvalidArgument",
			Message:    err.Error(),
			StatusCode: http.StatusBadRequest,
		})

		return
	}

	// Build options
	opts := &MultipartCopyOptions{
		SourceBucket:      sourceBucket,
		SourceKey:         sourceKey,
		SourceVersionID:   sourceVersionID,
		DestBucket:        destBucket,
		DestKey:           destKey,
		MetadataDirective: r.Header.Get("X-Amz-Metadata-Directive"),
		TaggingDirective:  r.Header.Get("X-Amz-Tagging-Directive"),
		ContentType:       r.Header.Get("Content-Type"),
		StorageClass:      r.Header.Get("X-Amz-Storage-Class"),
		Tagging:           r.Header.Get("X-Amz-Tagging"),
	}

	// Parse metadata
	opts.Metadata = make(map[string]string)

	for key, values := range r.Header {
		if strings.HasPrefix(strings.ToLower(key), "x-amz-meta-") {
			metaKey := strings.TrimPrefix(strings.ToLower(key), "x-amz-meta-")
			opts.Metadata[metaKey] = values[0]
		}
	}

	// Parse conditional headers
	if h := r.Header.Get("X-Amz-Copy-Source-If-Match"); h != "" {
		opts.CopySourceIfMatch = h
	}

	if h := r.Header.Get("X-Amz-Copy-Source-If-None-Match"); h != "" {
		opts.CopySourceIfNoneMatch = h
	}

	if h := r.Header.Get("X-Amz-Copy-Source-If-Modified-Since"); h != "" {
		if t, err := time.Parse(time.RFC1123, h); err == nil {
			opts.CopySourceIfModifiedSince = &t
		}
	}

	if h := r.Header.Get("X-Amz-Copy-Source-If-Unmodified-Since"); h != "" {
		if t, err := time.Parse(time.RFC1123, h); err == nil {
			opts.CopySourceIfUnmodifiedSince = &t
		}
	}

	// Parse object lock headers
	opts.ObjectLockMode = r.Header.Get("X-Amz-Object-Lock-Mode")
	if h := r.Header.Get("X-Amz-Object-Lock-Retain-Until-Date"); h != "" {
		if t, err := time.Parse(time.RFC3339, h); err == nil {
			opts.ObjectLockRetainUntil = &t
		}
	}

	opts.ObjectLockLegalHoldStatus = r.Header.Get("X-Amz-Object-Lock-Legal-Hold")

	// Parse SSE headers
	opts.SSECustomerAlgorithm = r.Header.Get("X-Amz-Server-Side-Encryption-Customer-Algorithm")
	opts.SSECustomerKey = r.Header.Get("X-Amz-Server-Side-Encryption-Customer-Key")
	opts.SSECustomerKeyMD5 = r.Header.Get("X-Amz-Server-Side-Encryption-Customer-Key-Md5")
	opts.CopySourceSSECustomerAlgorithm = r.Header.Get("X-Amz-Copy-Source-Server-Side-Encryption-Customer-Algorithm")
	opts.CopySourceSSECustomerKey = r.Header.Get("X-Amz-Copy-Source-Server-Side-Encryption-Customer-Key")
	opts.CopySourceSSECustomerKeyMD5 = r.Header.Get("X-Amz-Copy-Source-Server-Side-Encryption-Customer-Key-Md5")

	// Execute copy
	result, err := h.manager.CopyObject(ctx, opts)
	if err != nil {
		if s3Err, ok := err.(*S3Error); ok {
			writeS3Error(w, s3Err)
		} else {
			writeS3Error(w, &S3Error{
				Code:       "InternalError",
				Message:    err.Error(),
				StatusCode: http.StatusInternalServerError,
			})
		}

		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(result)
}

// writeS3Error writes an S3 error response.
func writeS3Error(w http.ResponseWriter, s3Err *S3Error) {
	type ErrorResponse struct {
		XMLName   xml.Name `xml:"Error"`
		Code      string   `xml:"Code"`
		Message   string   `xml:"Message"`
		Resource  string   `xml:"Resource,omitempty"`
		RequestID string   `xml:"RequestId"`
	}

	resp := ErrorResponse{
		Code:      s3Err.Code,
		Message:   s3Err.Message,
		Resource:  s3Err.Resource,
		RequestID: s3Err.RequestID,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(s3Err.StatusCode)
	xml.NewEncoder(w).Encode(resp)
}
