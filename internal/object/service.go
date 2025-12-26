package object

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// StorageBackend is the interface for object storage
type StorageBackend interface {
	backend.Backend
	backend.MultipartBackend
}

// Service handles object operations
type Service struct {
	store         metadata.Store
	storage       StorageBackend
	bucketService BucketService
}

// BucketService interface for bucket operations
type BucketService interface {
	GetBucket(ctx context.Context, name string) (*metadata.Bucket, error)
	GetVersioning(ctx context.Context, name string) (metadata.VersioningStatus, error)
}

// NewService creates a new object service
func NewService(store metadata.Store, storage StorageBackend, bucketService BucketService) *Service {
	return &Service{
		store:         store,
		storage:       storage,
		bucketService: bucketService,
	}
}

// PutObject stores an object
func (s *Service) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType, owner string, userMetadata map[string]string) (*metadata.ObjectMeta, error) {
	// Verify bucket exists
	bucketInfo, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	// Store the object data
	result, err := s.storage.PutObject(ctx, bucket, key, reader, size)
	if err != nil {
		return nil, fmt.Errorf("failed to store object: %w", err)
	}

	// Generate version ID if versioning is enabled
	versionID := ""
	if bucketInfo.Versioning == metadata.VersioningEnabled {
		versionID = generateVersionID()
	}

	// Create object metadata
	now := time.Now()
	meta := &metadata.ObjectMeta{
		Bucket:       bucket,
		Key:          key,
		VersionID:    versionID,
		Size:         result.Size,
		ETag:         fmt.Sprintf(`"%s"`, result.ETag),
		ContentType:  contentType,
		StorageClass: bucketInfo.StorageClass,
		Owner:        owner,
		CreatedAt:    now,
		ModifiedAt:   now,
		Metadata:     userMetadata,
		StorageInfo: &metadata.ObjectStorageInfo{
			Path: result.Path,
		},
	}

	// Store metadata
	if err := s.store.PutObjectMeta(ctx, meta); err != nil {
		// Rollback: delete the stored object
		s.storage.DeleteObject(ctx, bucket, key)
		return nil, fmt.Errorf("failed to store object metadata: %w", err)
	}

	return meta, nil
}

// GetObject retrieves an object
func (s *Service) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *metadata.ObjectMeta, error) {
	// Get object metadata
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}

	// Skip delete markers
	if meta.DeleteMarker {
		return nil, nil, fmt.Errorf("object not found (delete marker)")
	}

	// Get object data
	reader, err := s.storage.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}

	return reader, meta, nil
}

// HeadObject retrieves object metadata without the body
func (s *Service) HeadObject(ctx context.Context, bucket, key string) (*metadata.ObjectMeta, error) {
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	if meta.DeleteMarker {
		return nil, fmt.Errorf("object not found (delete marker)")
	}

	return meta, nil
}

// DeleteObject deletes an object
func (s *Service) DeleteObject(ctx context.Context, bucket, key string) error {
	// Check versioning status
	versioning, err := s.bucketService.GetVersioning(ctx, bucket)
	if err != nil {
		return err
	}

	if versioning == metadata.VersioningEnabled {
		// Create a delete marker instead of actually deleting
		meta := &metadata.ObjectMeta{
			Bucket:       bucket,
			Key:          key,
			VersionID:    generateVersionID(),
			DeleteMarker: true,
			ModifiedAt:   time.Now(),
		}
		return s.store.PutObjectMeta(ctx, meta)
	}

	// Delete the actual object
	if err := s.storage.DeleteObject(ctx, bucket, key); err != nil {
		return fmt.Errorf("failed to delete object data: %w", err)
	}

	if err := s.store.DeleteObjectMeta(ctx, bucket, key); err != nil {
		return fmt.Errorf("failed to delete object metadata: %w", err)
	}

	return nil
}

// ListObjects lists objects in a bucket
func (s *Service) ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*metadata.ObjectListing, error) {
	// Verify bucket exists
	if _, err := s.bucketService.GetBucket(ctx, bucket); err != nil {
		return nil, err
	}

	return s.store.ListObjects(ctx, bucket, prefix, delimiter, maxKeys, continuationToken)
}

// CopyObject copies an object
func (s *Service) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey, owner string) (*metadata.ObjectMeta, error) {
	// Get source object
	reader, srcMeta, err := s.GetObject(ctx, srcBucket, srcKey)
	if err != nil {
		return nil, fmt.Errorf("source object not found: %w", err)
	}
	defer reader.Close()

	// Copy to destination
	return s.PutObject(ctx, dstBucket, dstKey, reader, srcMeta.Size, srcMeta.ContentType, owner, srcMeta.Metadata)
}

// GetObjectVersion retrieves a specific version of an object
func (s *Service) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, *metadata.ObjectMeta, error) {
	meta, err := s.store.GetObjectVersion(ctx, bucket, key, versionID)
	if err != nil {
		return nil, nil, err
	}

	if meta.DeleteMarker {
		return nil, nil, fmt.Errorf("object not found (delete marker)")
	}

	reader, err := s.storage.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}

	return reader, meta, nil
}

// SetObjectTags sets tags on an object
func (s *Service) SetObjectTags(ctx context.Context, bucket, key string, tags map[string]string) error {
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return err
	}

	meta.Tags = tags
	return s.store.PutObjectMeta(ctx, meta)
}

// GetObjectTags returns tags for an object
func (s *Service) GetObjectTags(ctx context.Context, bucket, key string) (map[string]string, error) {
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return nil, err
	}
	return meta.Tags, nil
}

// DeleteObjectTags deletes all tags from an object
func (s *Service) DeleteObjectTags(ctx context.Context, bucket, key string) error {
	meta, err := s.store.GetObjectMeta(ctx, bucket, key)
	if err != nil {
		return err
	}

	meta.Tags = nil
	return s.store.PutObjectMeta(ctx, meta)
}

// Multipart upload operations

// CreateMultipartUpload initiates a multipart upload
func (s *Service) CreateMultipartUpload(ctx context.Context, bucket, key, contentType, owner string) (*metadata.MultipartUpload, error) {
	// Verify bucket exists
	if _, err := s.bucketService.GetBucket(ctx, bucket); err != nil {
		return nil, err
	}

	uploadID := generateUploadID()

	upload := &metadata.MultipartUpload{
		Bucket:    bucket,
		Key:       key,
		UploadID:  uploadID,
		Initiator: owner,
		CreatedAt: time.Now(),
	}

	// Create storage for the upload
	if err := s.storage.CreateMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return nil, fmt.Errorf("failed to create multipart upload storage: %w", err)
	}

	// Store metadata
	if err := s.store.CreateMultipartUpload(ctx, upload); err != nil {
		s.storage.AbortMultipartUpload(ctx, bucket, key, uploadID)
		return nil, fmt.Errorf("failed to store multipart upload metadata: %w", err)
	}

	return upload, nil
}

// UploadPart uploads a part of a multipart upload
func (s *Service) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*metadata.UploadPart, error) {
	// Verify upload exists
	upload, err := s.store.GetMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return nil, err
	}

	// Store the part
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

	// Update upload metadata
	if err := s.store.AddUploadPart(ctx, bucket, key, upload.UploadID, part); err != nil {
		return nil, fmt.Errorf("failed to update upload metadata: %w", err)
	}

	return part, nil
}

// CompleteMultipartUpload completes a multipart upload
func (s *Service) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []int) (*metadata.ObjectMeta, error) {
	// Get upload metadata
	upload, err := s.store.GetMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return nil, err
	}

	// Get bucket info
	bucketInfo, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return nil, err
	}

	// Verify all parts exist
	partMap := make(map[int]*metadata.UploadPart)
	for i := range upload.Parts {
		partMap[upload.Parts[i].PartNumber] = &upload.Parts[i]
	}

	for _, partNum := range parts {
		if _, exists := partMap[partNum]; !exists {
			return nil, fmt.Errorf("part %d not found", partNum)
		}
	}

	// Complete the upload in storage
	result, err := s.storage.CompleteParts(ctx, bucket, key, uploadID, parts)
	if err != nil {
		return nil, fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	// Calculate total size and combined ETag
	var totalSize int64
	var etags []string
	for _, partNum := range parts {
		part := partMap[partNum]
		totalSize += part.Size
		etags = append(etags, strings.Trim(part.ETag, `"`))
	}

	// Combined ETag for multipart uploads
	combinedHash := md5.New()
	for _, etag := range etags {
		hashBytes, _ := hex.DecodeString(etag)
		combinedHash.Write(hashBytes)
	}
	finalETag := fmt.Sprintf(`"%s-%d"`, hex.EncodeToString(combinedHash.Sum(nil)), len(parts))

	// Generate version ID if versioning is enabled
	versionID := ""
	if bucketInfo.Versioning == metadata.VersioningEnabled {
		versionID = generateVersionID()
	}

	// Create object metadata
	now := time.Now()
	meta := &metadata.ObjectMeta{
		Bucket:       bucket,
		Key:          key,
		VersionID:    versionID,
		Size:         totalSize,
		ETag:         finalETag,
		ContentType:  "application/octet-stream", // TODO: get from initial request
		StorageClass: bucketInfo.StorageClass,
		Owner:        upload.Initiator,
		CreatedAt:    now,
		ModifiedAt:   now,
		StorageInfo: &metadata.ObjectStorageInfo{
			Path: result.Path,
		},
	}

	// Store object metadata
	if err := s.store.PutObjectMeta(ctx, meta); err != nil {
		return nil, fmt.Errorf("failed to store object metadata: %w", err)
	}

	// Clean up upload metadata
	if err := s.store.CompleteMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		// Log error but don't fail - object is already stored
	}

	return meta, nil
}

// AbortMultipartUpload aborts a multipart upload
func (s *Service) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	// Verify upload exists
	if _, err := s.store.GetMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return err
	}

	// Abort in storage
	if err := s.storage.AbortMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return fmt.Errorf("failed to abort multipart upload storage: %w", err)
	}

	// Remove metadata
	if err := s.store.AbortMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return fmt.Errorf("failed to remove multipart upload metadata: %w", err)
	}

	return nil
}

// ListMultipartUploads lists all in-progress multipart uploads for a bucket
func (s *Service) ListMultipartUploads(ctx context.Context, bucket string) ([]*metadata.MultipartUpload, error) {
	return s.store.ListMultipartUploads(ctx, bucket)
}

// ListParts lists the parts of a multipart upload
func (s *Service) ListParts(ctx context.Context, bucket, key, uploadID string) ([]metadata.UploadPart, error) {
	upload, err := s.store.GetMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return nil, err
	}
	return upload.Parts, nil
}

// Helper functions

func generateVersionID() string {
	return uuid.New().String()
}

func generateUploadID() string {
	return uuid.New().String()
}
