// Package object provides object storage operations for NebulaIO.
//
// The object service handles all object-level operations including:
//   - Object upload and download with streaming support
//   - Multipart upload for large objects
//   - Object copying and deletion
//   - Object metadata and tagging
//   - Version management (when versioning is enabled)
//   - Storage class transitions
//
// Objects are stored using pluggable storage backends (filesystem, volume,
// erasure coding) while metadata is managed through the metadata store.
//
// Example usage:
//
//	svc := object.NewService(store, backend, config)
//	err := svc.PutObject(ctx, "bucket", "key", reader, size, metadata)
//	obj, err := svc.GetObject(ctx, "bucket", "key")
package object

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: MD5 required for S3 ETag compatibility
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/storage/backend"
	versioningPkg "github.com/piwi3910/nebulaio/internal/versioning"
)

// Tag validation constants.
const (
	MaxTagsPerResource = 10
	MaxTagKeyLength    = 128
	MaxTagValueLength  = 256
)

// TagValidationError represents a tag validation error.
type TagValidationError struct {
	Message string
}

func (e *TagValidationError) Error() string {
	return e.Message
}

// ValidateTags validates tags according to S3 tagging rules.
func ValidateTags(tags map[string]string) error {
	if len(tags) > MaxTagsPerResource {
		return &TagValidationError{
			Message: fmt.Sprintf("tag count exceeds maximum of %d", MaxTagsPerResource),
		}
	}

	for key, value := range tags {
		keyLen := utf8.RuneCountInString(key)
		valueLen := utf8.RuneCountInString(value)

		if keyLen == 0 {
			return &TagValidationError{
				Message: "tag key cannot be empty",
			}
		}

		if keyLen > MaxTagKeyLength {
			return &TagValidationError{
				Message: fmt.Sprintf("tag key '%s' exceeds maximum length of %d characters", key, MaxTagKeyLength),
			}
		}

		if valueLen > MaxTagValueLength {
			return &TagValidationError{
				Message: fmt.Sprintf("tag value for key '%s' exceeds maximum length of %d characters", key, MaxTagValueLength),
			}
		}

		// Check for reserved aws: prefix
		if strings.HasPrefix(strings.ToLower(key), "aws:") {
			return &TagValidationError{
				Message: fmt.Sprintf("tag key '%s' uses reserved 'aws:' prefix", key),
			}
		}
	}

	return nil
}

// ParseTaggingHeader parses the x-amz-tagging header format (key1=value1&key2=value2).
func ParseTaggingHeader(header string) (map[string]string, error) {
	if header == "" {
		return map[string]string{}, nil
	}

	tags := make(map[string]string)

	for pair := range strings.SplitSeq(header, "&") {
		if pair == "" {
			continue
		}

		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid tag format: %s", pair)
		}

		key, err := url.QueryUnescape(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid tag key encoding: %w", err)
		}

		value, err := url.QueryUnescape(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid tag value encoding: %w", err)
		}

		// Check for duplicate keys
		if _, exists := tags[key]; exists {
			return nil, fmt.Errorf("duplicate tag key: %s", key)
		}

		tags[key] = value
	}

	err := ValidateTags(tags)
	if err != nil {
		return nil, err
	}

	return tags, nil
}

// StorageBackend is the interface for object storage.
type StorageBackend interface {
	backend.Backend
	backend.MultipartBackend
}

// Service handles object operations.
type Service struct {
	store          metadata.Store
	storage        StorageBackend
	bucketService  BucketService
	versionService *versioningPkg.Service
}

// BucketService interface for bucket operations.
type BucketService interface {
	GetBucket(ctx context.Context, name string) (*metadata.Bucket, error)
	GetVersioning(ctx context.Context, name string) (metadata.VersioningStatus, error)
}

// DeleteObjectInput represents an object to delete in a batch operation.
type DeleteObjectInput struct {
	Key       string
	VersionID string
}

// DeletedObject represents a successfully deleted object.
type DeletedObject struct {
	Key                   string
	VersionID             string
	DeleteMarkerVersionID string
	DeleteMarker          bool
}

// DeleteError represents an error deleting a specific object.
type DeleteError struct {
	Key       string
	VersionID string
	Code      string
	Message   string
}

// DeleteObjectsResult represents the result of a batch delete operation.
type DeleteObjectsResult struct {
	Deleted []DeletedObject
	Errors  []DeleteError
}

// NewService creates a new object service.
func NewService(store metadata.Store, storage StorageBackend, bucketService BucketService) *Service {
	return &Service{
		store:          store,
		storage:        storage,
		bucketService:  bucketService,
		versionService: versioningPkg.NewService(),
	}
}

// PutObjectOptions contains optional parameters for PutObject.
type PutObjectOptions struct {
	Tags map[string]string

	// Object Lock settings
	ObjectLockMode            string     // "GOVERNANCE" or "COMPLIANCE"
	ObjectLockRetainUntilDate *time.Time // Retention period end date
	ObjectLockLegalHoldStatus string     // "ON" or "OFF"
}

// PutObject stores an object.
func (s *Service) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType, owner string, userMetadata map[string]string) (*metadata.ObjectMeta, error) {
	return s.PutObjectWithOptions(ctx, bucket, key, reader, size, contentType, owner, userMetadata, nil)
}

// PutObjectWithOptions stores an object with additional options including tags.
func (s *Service) PutObjectWithOptions(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType, owner string, userMetadata map[string]string, opts *PutObjectOptions) (*metadata.ObjectMeta, error) {
	bucketInfo, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	tags, err := s.validateAndGetTags(opts)
	if err != nil {
		return nil, err
	}

	result, err := s.storage.PutObject(ctx, bucket, key, reader, size)
	if err != nil {
		return nil, fmt.Errorf("failed to store object: %w", err)
	}

	versionID, preserveOldVersions := s.determineVersioning(bucketInfo)

	meta := s.createObjectMetadata(bucket, key, versionID, result, contentType, owner, userMetadata, tags, bucketInfo)

	if err := s.applyObjectLockSettings(ctx, bucket, key, bucketInfo, opts, meta); err != nil {
		_ = s.storage.DeleteObject(ctx, bucket, key)
		return nil, err
	}

	if err := s.storeObjectMetadata(ctx, bucket, key, meta, versionID, preserveOldVersions); err != nil {
		_ = s.storage.DeleteObject(ctx, bucket, key)
		return nil, err
	}

	return meta, nil
}

func (s *Service) validateAndGetTags(opts *PutObjectOptions) (map[string]string, error) {
	if opts != nil && opts.Tags != nil {
		if err := ValidateTags(opts.Tags); err != nil {
			return nil, fmt.Errorf("invalid tags: %w", err)
		}
		return opts.Tags, nil
	}
	return nil, nil
}

func (s *Service) determineVersioning(bucketInfo *metadata.Bucket) (string, bool) {
	if versioningPkg.IsVersioningEnabled(bucketInfo) {
		return s.versionService.GenerateVersionID(), true
	}
	if versioningPkg.IsVersioningSuspended(bucketInfo) {
		return versioningPkg.NullVersionID, false
	}
	return "", false
}

func (s *Service) createObjectMetadata(bucket, key, versionID string, result *backend.PutResult, contentType, owner string, userMetadata, tags map[string]string, bucketInfo *metadata.Bucket) *metadata.ObjectMeta {
	now := time.Now()
	return &metadata.ObjectMeta{
		Bucket:       bucket,
		Key:          key,
		VersionID:    versionID,
		IsLatest:     true,
		Size:         result.Size,
		ETag:         fmt.Sprintf(`"%s"`, result.ETag),
		ContentType:  contentType,
		StorageClass: bucketInfo.StorageClass,
		Owner:        owner,
		CreatedAt:    now,
		ModifiedAt:   now,
		Metadata:     userMetadata,
		Tags:         tags,
		StorageInfo: &metadata.ObjectStorageInfo{
			Path: result.Path,
		},
	}
}

func (s *Service) applyObjectLockSettings(ctx context.Context, bucket, key string, bucketInfo *metadata.Bucket, opts *PutObjectOptions, meta *metadata.ObjectMeta) error {
	if !bucketInfo.ObjectLockEnabled {
		return nil
	}

	if err := s.ApplyDefaultRetention(ctx, bucket, meta); err != nil {
		return fmt.Errorf("failed to apply default retention: %w", err)
	}

	if err := s.applyExplicitObjectLock(opts, meta); err != nil {
		return err
	}

	return nil
}

func (s *Service) applyExplicitObjectLock(opts *PutObjectOptions, meta *metadata.ObjectMeta) error {
	if opts == nil {
		return nil
	}

	if opts.ObjectLockMode != "" {
		if err := ValidateRetentionMode(opts.ObjectLockMode); err != nil {
			return err
		}
		meta.ObjectLockMode = opts.ObjectLockMode
	}

	if opts.ObjectLockRetainUntilDate != nil {
		if err := ValidateRetentionDate(*opts.ObjectLockRetainUntilDate); err != nil {
			return err
		}
		meta.ObjectLockRetainUntilDate = opts.ObjectLockRetainUntilDate
	}

	if opts.ObjectLockLegalHoldStatus != "" {
		if err := ValidateLegalHoldStatus(opts.ObjectLockLegalHoldStatus); err != nil {
			return err
		}
		meta.ObjectLockLegalHoldStatus = opts.ObjectLockLegalHoldStatus
	}

	return nil
}

func (s *Service) storeObjectMetadata(ctx context.Context, bucket, key string, meta *metadata.ObjectMeta, versionID string, preserveOldVersions bool) error {
	if versionID != "" {
		if err := s.store.PutObjectMetaVersioned(ctx, meta, preserveOldVersions); err != nil {
			return fmt.Errorf("failed to store object metadata: %w", err)
		}
	} else {
		if err := s.store.PutObjectMeta(ctx, meta); err != nil {
			return fmt.Errorf("failed to store object metadata: %w", err)
		}
	}
	return nil
}

// GetObject retrieves an object.
func (s *Service) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *metadata.ObjectMeta, error) {
	// Get object metadata
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}

	// Skip delete markers
	if meta.DeleteMarker {
		return nil, nil, errors.New("object not found (delete marker)")
	}

	// Get object data
	reader, err := s.storage.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}

	return reader, meta, nil
}

// HeadObject retrieves object metadata without the body.
func (s *Service) HeadObject(ctx context.Context, bucket, key string) (*metadata.ObjectMeta, error) {
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	if meta.DeleteMarker {
		return nil, errors.New("object not found (delete marker)")
	}

	return meta, nil
}

// DeleteObjectResult contains information about a deleted object.
type DeleteObjectResult struct {
	VersionID             string
	DeleteMarkerVersionID string
	DeleteMarker          bool
}

// DeleteObject deletes an object (creates delete marker when versioning enabled).
func (s *Service) DeleteObject(ctx context.Context, bucket, key string) (*DeleteObjectResult, error) {
	return s.DeleteObjectVersion(ctx, bucket, key, "")
}

// DeleteObjectVersion deletes a specific version of an object or creates a delete marker.
func (s *Service) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) (*DeleteObjectResult, error) {
	return s.DeleteObjectVersionWithOptions(ctx, bucket, key, versionID, nil)
}

// DeleteObjectVersionWithOptions deletes a specific version with options for bypassing governance.
func (s *Service) DeleteObjectVersionWithOptions(ctx context.Context, bucket, key, versionID string, opts *ObjectLockCheckOptions) (*DeleteObjectResult, error) {
	bucketInfo, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, err
	}

	result := &DeleteObjectResult{}

	// If a specific version ID is provided, permanently delete that version
	if versionID != "" && versionID != "null" {
		return s.deleteVersionWithLock(ctx, bucket, key, versionID, opts, result)
	}

	// No version ID provided - behavior depends on versioning status
	if versioningPkg.IsVersioningEnabled(bucketInfo) {
		return s.createDeleteMarkerVersion(ctx, bucket, key, result)
	}

	// Versioning disabled or suspended - permanently delete the object
	return s.deleteObjectPermanentlyWithLock(ctx, bucket, key, opts, result)
}

// deleteVersionWithLock permanently deletes a specific version with lock check.
func (s *Service) deleteVersionWithLock(ctx context.Context, bucket, key, versionID string, opts *ObjectLockCheckOptions, result *DeleteObjectResult) (*DeleteObjectResult, error) {
	meta, err := s.store.GetObjectVersion(ctx, bucket, key, versionID)
	if err != nil {
		return nil, fmt.Errorf("version not found: %w", err)
	}

	if err := s.CheckObjectLock(ctx, meta, opts); err != nil {
		return nil, err
	}

	if err := s.store.DeleteObjectVersion(ctx, bucket, key, versionID); err != nil {
		return nil, fmt.Errorf("failed to delete object version: %w", err)
	}

	result.VersionID = versionID
	result.DeleteMarker = meta.DeleteMarker

	return result, nil
}

// createDeleteMarkerVersion creates a delete marker for versioned bucket.
func (s *Service) createDeleteMarkerVersion(ctx context.Context, bucket, key string, result *DeleteObjectResult) (*DeleteObjectResult, error) {
	deleteMarkerVersionID := s.versionService.GenerateVersionID()
	meta := &metadata.ObjectMeta{
		Bucket:       bucket,
		Key:          key,
		VersionID:    deleteMarkerVersionID,
		IsLatest:     true,
		DeleteMarker: true,
		ModifiedAt:   time.Now(),
	}

	if err := s.store.PutObjectMetaVersioned(ctx, meta, true); err != nil {
		return nil, fmt.Errorf("failed to create delete marker: %w", err)
	}

	result.DeleteMarker = true
	result.DeleteMarkerVersionID = deleteMarkerVersionID

	return result, nil
}

// deleteObjectPermanentlyWithLock deletes object when versioning is disabled.
func (s *Service) deleteObjectPermanentlyWithLock(ctx context.Context, bucket, key string, opts *ObjectLockCheckOptions, result *DeleteObjectResult) (*DeleteObjectResult, error) {
	// Check object lock before deletion
	if meta, metaErr := s.store.GetObjectMeta(ctx, bucket, key); metaErr == nil && meta != nil {
		if err := s.CheckObjectLock(ctx, meta, opts); err != nil {
			return nil, err
		}
	}

	if err := s.deleteObjectData(ctx, bucket, key); err != nil {
		return nil, err
	}

	if err := s.deleteObjectMetadata(ctx, bucket, key); err != nil {
		return nil, err
	}

	return result, nil
}

// deleteObjectData deletes object data from storage.
func (s *Service) deleteObjectData(ctx context.Context, bucket, key string) error {
	err := s.storage.DeleteObject(ctx, bucket, key)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return fmt.Errorf("failed to delete object data: %w", err)
	}
	return nil
}

// deleteObjectMetadata deletes object metadata.
func (s *Service) deleteObjectMetadata(ctx context.Context, bucket, key string) error {
	err := s.store.DeleteObjectMeta(ctx, bucket, key)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return fmt.Errorf("failed to delete object metadata: %w", err)
	}
	return nil
}

// ListObjects lists objects in a bucket.
func (s *Service) ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*metadata.ObjectListing, error) {
	// Verify bucket exists
	_, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, err
	}

	return s.store.ListObjects(ctx, bucket, prefix, delimiter, maxKeys, continuationToken)
}

// TaggingDirective specifies how to handle tags during copy.
type TaggingDirective string

const (
	// TaggingDirectiveCopy copies tags from source object (default).
	TaggingDirectiveCopy TaggingDirective = "COPY"
	// TaggingDirectiveReplace uses tags from request headers.
	TaggingDirectiveReplace TaggingDirective = "REPLACE"
)

// CopyObjectOptions contains optional parameters for CopyObject.
type CopyObjectOptions struct {
	Tags             map[string]string
	TaggingDirective TaggingDirective
}

// CopyObject copies an object (preserves source tags by default).
func (s *Service) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey, owner string) (*metadata.ObjectMeta, error) {
	return s.CopyObjectWithOptions(ctx, srcBucket, srcKey, dstBucket, dstKey, owner, nil)
}

// CopyObjectWithOptions copies an object with additional options including tagging directive.
func (s *Service) CopyObjectWithOptions(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey, owner string, opts *CopyObjectOptions) (*metadata.ObjectMeta, error) {
	// Get source object
	reader, srcMeta, err := s.GetObject(ctx, srcBucket, srcKey)
	if err != nil {
		return nil, fmt.Errorf("source object not found: %w", err)
	}

	defer func() { _ = reader.Close() }()

	// Determine tags based on tagging directive
	var tags map[string]string

	if opts != nil && opts.TaggingDirective == TaggingDirectiveReplace {
		// Use tags from request
		if opts.Tags != nil {
			err := ValidateTags(opts.Tags)
			if err != nil {
				return nil, fmt.Errorf("invalid tags: %w", err)
			}

			tags = opts.Tags
		}
	} else {
		// Default: copy tags from source object
		tags = srcMeta.Tags
	}

	// Copy to destination with tags
	putOpts := &PutObjectOptions{
		Tags: tags,
	}

	return s.PutObjectWithOptions(ctx, dstBucket, dstKey, reader, srcMeta.Size, srcMeta.ContentType, owner, srcMeta.Metadata, putOpts)
}

// GetObjectVersion retrieves a specific version of an object.
func (s *Service) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, *metadata.ObjectMeta, error) {
	meta, err := s.store.GetObjectVersion(ctx, bucket, key, versionID)
	if err != nil {
		return nil, nil, err
	}

	if meta.DeleteMarker {
		// Return the metadata but indicate it's a delete marker
		return nil, meta, errors.New("object is a delete marker")
	}

	// For versioned objects, we need to read from the correct storage path
	// The storage path is stored in StorageInfo.Path
	if meta.StorageInfo != nil && meta.StorageInfo.Path != "" {
		reader, err := s.storage.GetObject(ctx, bucket, key)
		if err != nil {
			return nil, nil, err
		}

		return reader, meta, nil
	}

	reader, err := s.storage.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}

	return reader, meta, nil
}

// HeadObjectVersion retrieves metadata for a specific version without the body.
func (s *Service) HeadObjectVersion(ctx context.Context, bucket, key, versionID string) (*metadata.ObjectMeta, error) {
	meta, err := s.store.GetObjectVersion(ctx, bucket, key, versionID)
	if err != nil {
		return nil, err
	}

	return meta, nil
}

// ListObjectVersions lists all versions of objects in a bucket.
func (s *Service) ListObjectVersions(ctx context.Context, bucket, prefix, delimiter, keyMarker, versionIDMarker string, maxKeys int) (*metadata.VersionListing, error) {
	// Verify bucket exists
	_, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, err
	}

	return s.store.ListObjectVersions(ctx, bucket, prefix, delimiter, keyMarker, versionIDMarker, maxKeys)
}

// PutObjectTagging sets tags on an object with validation.
func (s *Service) PutObjectTagging(ctx context.Context, bucket, key string, tags map[string]string) error {
	// Validate tags
	err := ValidateTags(tags)
	if err != nil {
		return err
	}

	// Verify bucket exists
	_, err = s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return fmt.Errorf("bucket not found: %w", err)
	}

	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return err
	}

	if meta.DeleteMarker {
		return errors.New("cannot tag a delete marker")
	}

	meta.Tags = tags

	return s.store.PutObjectMeta(ctx, meta)
}

// GetObjectTagging returns tags for an object.
func (s *Service) GetObjectTagging(ctx context.Context, bucket, key string) (map[string]string, error) {
	// Verify bucket exists
	_, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	if meta.DeleteMarker {
		return nil, errors.New("object not found (delete marker)")
	}

	// Return empty map if no tags
	if meta.Tags == nil {
		return make(map[string]string), nil
	}

	return meta.Tags, nil
}

// DeleteObjectTagging deletes all tags from an object.
func (s *Service) DeleteObjectTagging(ctx context.Context, bucket, key string) error {
	// Verify bucket exists
	_, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return fmt.Errorf("bucket not found: %w", err)
	}

	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return err
	}

	if meta.DeleteMarker {
		return errors.New("cannot delete tags from a delete marker")
	}

	meta.Tags = nil

	return s.store.PutObjectMeta(ctx, meta)
}

// SetObjectTags sets tags on an object (legacy method, calls PutObjectTagging).
func (s *Service) SetObjectTags(ctx context.Context, bucket, key string, tags map[string]string) error {
	return s.PutObjectTagging(ctx, bucket, key, tags)
}

// GetObjectTags returns tags for an object (legacy method, calls GetObjectTagging).
func (s *Service) GetObjectTags(ctx context.Context, bucket, key string) (map[string]string, error) {
	return s.GetObjectTagging(ctx, bucket, key)
}

// DeleteObjectTags deletes all tags from an object (legacy method, calls DeleteObjectTagging).
func (s *Service) DeleteObjectTags(ctx context.Context, bucket, key string) error {
	return s.DeleteObjectTagging(ctx, bucket, key)
}

// TransitionStorageClass transitions an object to a different storage class.
func (s *Service) TransitionStorageClass(ctx context.Context, bucket, key, targetClass string) error {
	// Get current object metadata
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("failed to get object metadata: %w", err)
	}

	// Check if already at target class
	if meta.StorageClass == targetClass {
		return nil // Already at target class, nothing to do
	}

	// Update storage class in metadata
	meta.StorageClass = targetClass
	meta.ModifiedAt = time.Now()

	// Store updated metadata
	err = s.store.PutObjectMeta(ctx, meta)
	if err != nil {
		return fmt.Errorf("failed to update object storage class: %w", err)
	}

	return nil
}

// Multipart upload operations

// CreateMultipartUpload initiates a multipart upload.
func (s *Service) CreateMultipartUpload(ctx context.Context, bucket, key, contentType, owner string, userMetadata map[string]string) (*metadata.MultipartUpload, error) {
	// Verify bucket exists
	bucketInfo, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, err
	}

	uploadID := generateUploadID()

	// Default content type
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	upload := &metadata.MultipartUpload{
		Bucket:       bucket,
		Key:          key,
		UploadID:     uploadID,
		Initiator:    owner,
		ContentType:  contentType,
		StorageClass: bucketInfo.StorageClass,
		Metadata:     userMetadata,
		CreatedAt:    time.Now(),
	}

	// Create storage for the upload
	err = s.storage.CreateMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return nil, fmt.Errorf("failed to create multipart upload storage: %w", err)
	}

	// Store metadata
	err = s.store.CreateMultipartUpload(ctx, upload)
	if err != nil {
		_ = s.storage.AbortMultipartUpload(ctx, bucket, key, uploadID)
		return nil, fmt.Errorf("failed to store multipart upload metadata: %w", err)
	}

	return upload, nil
}

// MinPartSize is the minimum size for all parts except the last one (5MB).
const MinPartSize = 5 * 1024 * 1024

// MaxPartNumber is the maximum allowed part number.
const MaxPartNumber = 10000

// MaxPartsPerUpload is the maximum number of parts allowed per upload (AWS limit: 10,000).
const MaxPartsPerUpload = 10000

// UploadPart uploads a part of a multipart upload.
func (s *Service) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*metadata.UploadPart, error) {
	// Validate part number (1-10000)
	if partNumber < 1 || partNumber > MaxPartNumber {
		return nil, fmt.Errorf("invalid part number: must be between 1 and %d", MaxPartNumber)
	}

	// Verify upload exists
	upload, err := s.store.GetMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return nil, err
	}

	// Check if we already have max parts (only if this is a new part, not an overwrite)
	isOverwrite := false

	for _, existingPart := range upload.Parts {
		if existingPart.PartNumber == partNumber {
			isOverwrite = true
			break
		}
	}

	if !isOverwrite && len(upload.Parts) >= MaxPartsPerUpload {
		return nil, fmt.Errorf("maximum number of parts (%d) exceeded", MaxPartsPerUpload)
	}

	// Store the part (this will overwrite any existing part with the same number)
	result, err := s.storage.PutPart(ctx, bucket, key, uploadID, partNumber, reader, size)
	if err != nil {
		return nil, fmt.Errorf("failed to store part: %w", err)
	}

	part := &metadata.UploadPart{
		PartNumber:   partNumber,
		Size:         result.Size,
		ETag:         fmt.Sprintf(`"%s"`, result.ETag),
		LastModified: time.Now(),
		Path:         result.Path,
	}

	// Update upload metadata (this handles both new parts and overwrites)
	err = s.store.AddUploadPart(ctx, bucket, key, upload.UploadID, part)
	if err != nil {
		return nil, fmt.Errorf("failed to update upload metadata: %w", err)
	}

	return part, nil
}

// CompletePart represents a part in the complete multipart upload request.
type CompletePart struct {
	ETag       string
	PartNumber int
}

// CompleteMultipartUpload completes a multipart upload.
func (s *Service) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, requestParts []CompletePart) (*metadata.ObjectMeta, error) {
	upload, bucketInfo, err := s.getUploadAndBucketInfo(ctx, bucket, key, uploadID)
	if err != nil {
		return nil, err
	}

	partMap := s.buildPartMap(upload)

	if err := s.validateRequestParts(requestParts, partMap); err != nil {
		return nil, err
	}

	partNumbers := s.extractPartNumbers(requestParts)

	result, err := s.storage.CompleteParts(ctx, bucket, key, uploadID, partNumbers)
	if err != nil {
		return nil, fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	totalSize, finalETag := s.calculateFinalETag(partNumbers, partMap)

	meta := s.createCompletedObjectMetadata(upload, bucket, key, bucketInfo, totalSize, finalETag, result.Path)

	if err := s.storeCompletedObjectMetadata(ctx, meta, bucketInfo); err != nil {
		return nil, err
	}

	_ = s.store.CompleteMultipartUpload(ctx, bucket, key, uploadID)

	return meta, nil
}

func (s *Service) getUploadAndBucketInfo(ctx context.Context, bucket, key, uploadID string) (*metadata.MultipartUpload, *metadata.Bucket, error) {
	upload, err := s.store.GetMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return nil, nil, err
	}

	bucketInfo, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, nil, err
	}

	return upload, bucketInfo, nil
}

func (s *Service) buildPartMap(upload *metadata.MultipartUpload) map[int]*metadata.UploadPart {
	partMap := make(map[int]*metadata.UploadPart)
	for i := range upload.Parts {
		partMap[upload.Parts[i].PartNumber] = &upload.Parts[i]
	}
	return partMap
}

func (s *Service) validateRequestParts(requestParts []CompletePart, partMap map[int]*metadata.UploadPart) error {
	if len(requestParts) == 0 {
		return errors.New("at least one part must be specified")
	}

	if err := s.verifyPartsAscending(requestParts); err != nil {
		return err
	}

	return s.verifyPartsExistAndMatch(requestParts, partMap)
}

func (s *Service) verifyPartsAscending(requestParts []CompletePart) error {
	prevPartNumber := 0
	for _, reqPart := range requestParts {
		if reqPart.PartNumber <= prevPartNumber {
			return errors.New("parts must be in ascending order")
		}
		prevPartNumber = reqPart.PartNumber
	}
	return nil
}

func (s *Service) verifyPartsExistAndMatch(requestParts []CompletePart, partMap map[int]*metadata.UploadPart) error {
	for i, reqPart := range requestParts {
		uploadedPart, exists := partMap[reqPart.PartNumber]
		if !exists {
			return fmt.Errorf("part %d not found", reqPart.PartNumber)
		}

		requestETag := strings.Trim(reqPart.ETag, `"`)
		uploadedETag := strings.Trim(uploadedPart.ETag, `"`)
		if requestETag != uploadedETag {
			return fmt.Errorf("ETag mismatch for part %d: expected %s, got %s", reqPart.PartNumber, uploadedETag, requestETag)
		}

		if i < len(requestParts)-1 && uploadedPart.Size < MinPartSize {
			return fmt.Errorf("part %d is too small (%d bytes); minimum size is %d bytes except for the last part",
				reqPart.PartNumber, uploadedPart.Size, MinPartSize)
		}
	}
	return nil
}

func (s *Service) extractPartNumbers(requestParts []CompletePart) []int {
	partNumbers := make([]int, 0, len(requestParts))
	for _, reqPart := range requestParts {
		partNumbers = append(partNumbers, reqPart.PartNumber)
	}
	return partNumbers
}

func (s *Service) calculateFinalETag(partNumbers []int, partMap map[int]*metadata.UploadPart) (int64, string) {
	var (
		totalSize int64
		etagBytes []byte
	)

	for _, partNum := range partNumbers {
		part := partMap[partNum]
		totalSize += part.Size
		etag := strings.Trim(part.ETag, `"`)
		hashBytes, _ := hex.DecodeString(etag)
		etagBytes = append(etagBytes, hashBytes...)
	}

	combinedHash := md5.Sum(etagBytes) //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	finalETag := fmt.Sprintf(`"%s-%d"`, hex.EncodeToString(combinedHash[:]), len(partNumbers))

	return totalSize, finalETag
}

func (s *Service) createCompletedObjectMetadata(upload *metadata.MultipartUpload, bucket, key string, bucketInfo *metadata.Bucket, totalSize int64, finalETag, path string) *metadata.ObjectMeta {
	versionID := ""
	if versioningPkg.IsVersioningEnabled(bucketInfo) {
		versionID = s.versionService.GenerateVersionID()
	} else if versioningPkg.IsVersioningSuspended(bucketInfo) {
		versionID = versioningPkg.NullVersionID
	}

	now := time.Now()
	return &metadata.ObjectMeta{
		Bucket:       bucket,
		Key:          key,
		VersionID:    versionID,
		IsLatest:     true,
		Size:         totalSize,
		ETag:         finalETag,
		ContentType:  upload.ContentType,
		StorageClass: upload.StorageClass,
		Owner:        upload.Initiator,
		CreatedAt:    now,
		ModifiedAt:   now,
		Metadata:     upload.Metadata,
		StorageInfo: &metadata.ObjectStorageInfo{
			Path: path,
		},
	}
}

func (s *Service) storeCompletedObjectMetadata(ctx context.Context, meta *metadata.ObjectMeta, bucketInfo *metadata.Bucket) error {
	if meta.VersionID != "" {
		preserveOldVersions := versioningPkg.IsVersioningEnabled(bucketInfo)
		err := s.store.PutObjectMetaVersioned(ctx, meta, preserveOldVersions)
		if err != nil {
			return fmt.Errorf("failed to store object metadata: %w", err)
		}
	} else {
		err := s.store.PutObjectMeta(ctx, meta)
		if err != nil {
			return fmt.Errorf("failed to store object metadata: %w", err)
		}
	}
	return nil
}

// AbortMultipartUpload aborts a multipart upload.
func (s *Service) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	// Verify upload exists
	_, err := s.store.GetMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return err
	}

	// Abort in storage
	err = s.storage.AbortMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return fmt.Errorf("failed to abort multipart upload storage: %w", err)
	}

	// Remove metadata
	err = s.store.AbortMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return fmt.Errorf("failed to remove multipart upload metadata: %w", err)
	}

	return nil
}

// ListMultipartUploads lists all in-progress multipart uploads for a bucket.
func (s *Service) ListMultipartUploads(ctx context.Context, bucket string) ([]*metadata.MultipartUpload, error) {
	return s.store.ListMultipartUploads(ctx, bucket)
}

// ListPartsResult contains the result of listing parts with pagination info.
type ListPartsResult struct {
	Initiator            string
	Parts                []metadata.UploadPart
	NextPartNumberMarker int
	IsTruncated          bool
}

// ListParts lists the parts of a multipart upload with pagination.
func (s *Service) ListParts(ctx context.Context, bucket, key, uploadID string, maxParts, partNumberMarker int) (*ListPartsResult, error) {
	upload, err := s.store.GetMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return nil, err
	}

	// Default max parts
	if maxParts <= 0 || maxParts > 1000 {
		maxParts = 1000
	}

	// Sort parts by part number
	parts := make([]metadata.UploadPart, len(upload.Parts))
	copy(parts, upload.Parts)
	sortParts(parts)

	// Filter parts after marker
	var filteredParts []metadata.UploadPart

	for _, part := range parts {
		if part.PartNumber > partNumberMarker {
			filteredParts = append(filteredParts, part)
		}
	}

	// Apply max parts limit
	result := &ListPartsResult{
		Initiator: upload.Initiator,
	}

	if len(filteredParts) > maxParts {
		result.Parts = filteredParts[:maxParts]
		result.IsTruncated = true
		result.NextPartNumberMarker = result.Parts[maxParts-1].PartNumber
	} else {
		result.Parts = filteredParts
		result.IsTruncated = false
	}

	return result, nil
}

// sortParts sorts parts by part number in ascending order.
func sortParts(parts []metadata.UploadPart) {
	for i := range len(parts) - 1 {
		for j := i + 1; j < len(parts); j++ {
			if parts[i].PartNumber > parts[j].PartNumber {
				parts[i], parts[j] = parts[j], parts[i]
			}
		}
	}
}

// DeleteObjects deletes multiple objects in a batch.
func (s *Service) DeleteObjects(ctx context.Context, bucket string, objects []DeleteObjectInput, quiet bool) (*DeleteObjectsResult, error) {
	bucketInfo, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	result := &DeleteObjectsResult{
		Deleted: make([]DeletedObject, 0),
		Errors:  make([]DeleteError, 0),
	}

	for _, obj := range objects {
		s.processObjectDeletion(ctx, bucket, bucketInfo, obj, result)
	}

	return result, nil
}

func (s *Service) processObjectDeletion(ctx context.Context, bucket string, bucketInfo *metadata.Bucket, obj DeleteObjectInput, result *DeleteObjectsResult) {
	if !s.validateObjectKey(obj, result) {
		return
	}

	if obj.VersionID != "" {
		s.deleteVersionedObject(ctx, bucket, obj, result)
		return
	}

	s.deleteCurrentVersion(ctx, bucket, bucketInfo, obj, result)
}

func (s *Service) validateObjectKey(obj DeleteObjectInput, result *DeleteObjectsResult) bool {
	if obj.Key == "" {
		result.Errors = append(result.Errors, DeleteError{
			Key:       obj.Key,
			VersionID: obj.VersionID,
			Code:      "InvalidArgument",
			Message:   "Object key cannot be empty",
		})
		return false
	}

	return true
}

func (s *Service) deleteVersionedObject(ctx context.Context, bucket string, obj DeleteObjectInput, result *DeleteObjectsResult) {
	deleted, err := s.deleteSpecificVersion(ctx, bucket, obj.Key, obj.VersionID)
	if err != nil {
		result.Errors = append(result.Errors, DeleteError{
			Key:       obj.Key,
			VersionID: obj.VersionID,
			Code:      getErrorCode(err),
			Message:   err.Error(),
		})
	} else {
		result.Deleted = append(result.Deleted, *deleted)
	}
}

func (s *Service) deleteCurrentVersion(ctx context.Context, bucket string, bucketInfo *metadata.Bucket, obj DeleteObjectInput, result *DeleteObjectsResult) {
	if versioningPkg.IsVersioningEnabled(bucketInfo) {
		s.createDeleteMarker(ctx, bucket, bucketInfo, obj, result)
	} else {
		s.permanentlyDeleteObject(ctx, bucket, obj, result)
	}
}

func (s *Service) createDeleteMarker(ctx context.Context, bucket string, bucketInfo *metadata.Bucket, obj DeleteObjectInput, result *DeleteObjectsResult) {
	deleteMarkerVersionID := s.versionService.GenerateVersionID()

	meta := &metadata.ObjectMeta{
		Bucket:       bucket,
		Key:          obj.Key,
		VersionID:    deleteMarkerVersionID,
		IsLatest:     true,
		DeleteMarker: true,
		Owner:        bucketInfo.Owner,
		ModifiedAt:   time.Now(),
	}

	err := s.store.PutObjectMetaVersioned(ctx, meta, true)
	if err != nil {
		result.Errors = append(result.Errors, DeleteError{
			Key:     obj.Key,
			Code:    "InternalError",
			Message: err.Error(),
		})
	} else {
		result.Deleted = append(result.Deleted, DeletedObject{
			Key:                   obj.Key,
			DeleteMarker:          true,
			DeleteMarkerVersionID: deleteMarkerVersionID,
		})
	}
}

func (s *Service) permanentlyDeleteObject(ctx context.Context, bucket string, obj DeleteObjectInput, result *DeleteObjectsResult) {
	err := s.deleteObjectPermanently(ctx, bucket, obj.Key)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			result.Errors = append(result.Errors, DeleteError{
				Key:     obj.Key,
				Code:    getErrorCode(err),
				Message: err.Error(),
			})
		} else {
			result.Deleted = append(result.Deleted, DeletedObject{
				Key: obj.Key,
			})
		}
	} else {
		result.Deleted = append(result.Deleted, DeletedObject{
			Key: obj.Key,
		})
	}
}

// deleteSpecificVersion permanently deletes a specific object version.
func (s *Service) deleteSpecificVersion(ctx context.Context, bucket, key, versionID string) (*DeletedObject, error) {
	// Get the specific version to check if it exists and if it's a delete marker
	meta, err := s.store.GetObjectVersion(ctx, bucket, key, versionID)
	if err != nil {
		return nil, err
	}

	result := &DeletedObject{
		Key:       key,
		VersionID: versionID,
	}

	// If this is a delete marker, we're removing the delete marker
	if meta.DeleteMarker {
		result.DeleteMarker = true
	} else if meta.StorageInfo != nil {
		// Delete the actual object data from storage
		err = s.storage.DeleteObject(ctx, bucket, key)
		if err != nil {
			// Log but continue - metadata will still be cleaned up
		}
	}

	// Delete the version metadata
	err = s.store.DeleteObjectMeta(ctx, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("failed to delete object version: %w", err)
	}

	return result, nil
}

// deleteObjectPermanently deletes an object and its metadata.
func (s *Service) deleteObjectPermanently(ctx context.Context, bucket, key string) error {
	// Get object metadata first to check if it exists
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return err
	}

	// Delete the actual object data from storage (skip if it's a delete marker)
	if !meta.DeleteMarker && meta.StorageInfo != nil {
		err = s.storage.DeleteObject(ctx, bucket, key)
		if err != nil {
			return fmt.Errorf("failed to delete object data: %w", err)
		}
	}

	// Delete the metadata
	err = s.store.DeleteObjectMeta(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("failed to delete object metadata: %w", err)
	}

	return nil
}

// getErrorCode returns an S3 error code based on the error.
func getErrorCode(err error) string {
	errStr := err.Error()
	if strings.Contains(errStr, "not found") {
		return "NoSuchKey"
	}

	if strings.Contains(errStr, "access denied") {
		return "AccessDenied"
	}

	return "InternalError"
}

// Helper functions

func _generateVersionID() string {
	return uuid.New().String()
}

func generateUploadID() string {
	return uuid.New().String()
}

// SetObjectACL sets the ACL for an object.
func (s *Service) SetObjectACL(ctx context.Context, bucket, key string, acl *metadata.ObjectACL) error {
	// Get the object metadata
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("object not found: %w", err)
	}

	// Update the ACL
	meta.ACL = acl

	// Save the updated metadata using PutObjectMeta
	err = s.store.PutObjectMeta(ctx, meta)
	if err != nil {
		return fmt.Errorf("failed to update object ACL: %w", err)
	}

	return nil
}

// SetObjectRetention sets the retention configuration for an object.
func (s *Service) SetObjectRetention(ctx context.Context, bucket, key, versionID, mode string, retainUntilDate time.Time) error {
	var (
		meta *metadata.ObjectMeta
		err  error
	)

	if versionID != "" {
		meta, err = s.store.GetObjectVersion(ctx, bucket, key, versionID)
	} else {
		meta, err = s.store.GetObjectMeta(ctx, bucket, key)
	}

	if err != nil {
		return fmt.Errorf("object not found: %w", err)
	}

	// Validate mode
	if mode != "GOVERNANCE" && mode != "COMPLIANCE" {
		return fmt.Errorf("invalid retention mode: %s", mode)
	}

	// Validate retain until date
	if retainUntilDate.Before(time.Now()) {
		return errors.New("retain until date must be in the future")
	}

	// Update retention settings
	meta.ObjectLockMode = mode
	meta.ObjectLockRetainUntilDate = &retainUntilDate

	// Save the updated metadata using PutObjectMeta
	err = s.store.PutObjectMeta(ctx, meta)
	if err != nil {
		return fmt.Errorf("failed to update object retention: %w", err)
	}

	return nil
}

// SetObjectLegalHold sets the legal hold status for an object.
func (s *Service) SetObjectLegalHold(ctx context.Context, bucket, key, versionID, status string) error {
	var (
		meta *metadata.ObjectMeta
		err  error
	)

	if versionID != "" {
		meta, err = s.store.GetObjectVersion(ctx, bucket, key, versionID)
	} else {
		meta, err = s.store.GetObjectMeta(ctx, bucket, key)
	}

	if err != nil {
		return fmt.Errorf("object not found: %w", err)
	}

	// Update legal hold status
	meta.ObjectLockLegalHoldStatus = status

	// Save the updated metadata using PutObjectMeta
	err = s.store.PutObjectMeta(ctx, meta)
	if err != nil {
		return fmt.Errorf("failed to update object legal hold: %w", err)
	}

	return nil
}
