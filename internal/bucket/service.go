package bucket

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/object"
)

// Service handles bucket operations
type Service struct {
	store   metadata.Store
	storage object.StorageBackend
}

// NewService creates a new bucket service
func NewService(store metadata.Store, storage object.StorageBackend) *Service {
	return &Service{
		store:   store,
		storage: storage,
	}
}

// bucketNameRegex validates S3 bucket naming rules
var bucketNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// CreateBucket creates a new bucket
func (s *Service) CreateBucket(ctx context.Context, name, owner, region, storageClass string) (*metadata.Bucket, error) {
	// Validate bucket name
	if err := validateBucketName(name); err != nil {
		return nil, err
	}

	// Check if bucket already exists
	if _, err := s.store.GetBucket(ctx, name); err == nil {
		return nil, fmt.Errorf("bucket already exists: %s", name)
	}

	// Set defaults
	if region == "" {
		region = "us-east-1"
	}
	if storageClass == "" {
		storageClass = "STANDARD"
	}

	bucket := &metadata.Bucket{
		Name:         name,
		Owner:        owner,
		CreatedAt:    time.Now(),
		Region:       region,
		StorageClass: storageClass,
	}

	// Create metadata
	if err := s.store.CreateBucket(ctx, bucket); err != nil {
		return nil, fmt.Errorf("failed to create bucket metadata: %w", err)
	}

	// Create storage
	if err := s.storage.CreateBucket(ctx, name); err != nil {
		// Rollback metadata
		s.store.DeleteBucket(ctx, name)
		return nil, fmt.Errorf("failed to create bucket storage: %w", err)
	}

	return bucket, nil
}

// GetBucket retrieves a bucket by name
func (s *Service) GetBucket(ctx context.Context, name string) (*metadata.Bucket, error) {
	return s.store.GetBucket(ctx, name)
}

// DeleteBucket deletes a bucket
func (s *Service) DeleteBucket(ctx context.Context, name string) error {
	// Check if bucket exists
	if _, err := s.store.GetBucket(ctx, name); err != nil {
		return err
	}

	// Check if bucket is empty
	listing, err := s.store.ListObjects(ctx, name, "", "", 1, "")
	if err != nil {
		return fmt.Errorf("failed to check bucket contents: %w", err)
	}
	if len(listing.Objects) > 0 {
		return fmt.Errorf("bucket not empty")
	}

	// Delete storage
	if err := s.storage.DeleteBucket(ctx, name); err != nil {
		return fmt.Errorf("failed to delete bucket storage: %w", err)
	}

	// Delete metadata
	if err := s.store.DeleteBucket(ctx, name); err != nil {
		return fmt.Errorf("failed to delete bucket metadata: %w", err)
	}

	return nil
}

// ListBuckets lists all buckets owned by a user
func (s *Service) ListBuckets(ctx context.Context, owner string) ([]*metadata.Bucket, error) {
	return s.store.ListBuckets(ctx, owner)
}

// HeadBucket checks if a bucket exists
func (s *Service) HeadBucket(ctx context.Context, name string) error {
	_, err := s.store.GetBucket(ctx, name)
	return err
}

// SetVersioning enables or disables versioning for a bucket
func (s *Service) SetVersioning(ctx context.Context, name string, status metadata.VersioningStatus) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return err
	}

	// Cannot disable versioning once enabled, only suspend
	if bucket.Versioning == metadata.VersioningEnabled && status == metadata.VersioningDisabled {
		return fmt.Errorf("versioning cannot be disabled, only suspended")
	}

	bucket.Versioning = status
	return s.store.UpdateBucket(ctx, bucket)
}

// GetVersioning returns the versioning status for a bucket
func (s *Service) GetVersioning(ctx context.Context, name string) (metadata.VersioningStatus, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return "", err
	}
	return bucket.Versioning, nil
}

// SetBucketPolicy sets the bucket policy
func (s *Service) SetBucketPolicy(ctx context.Context, name, policy string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return err
	}

	// TODO: Validate policy JSON
	bucket.Policy = policy
	return s.store.UpdateBucket(ctx, bucket)
}

// GetBucketPolicy returns the bucket policy
func (s *Service) GetBucketPolicy(ctx context.Context, name string) (string, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return "", err
	}
	if bucket.Policy == "" {
		return "", fmt.Errorf("no bucket policy")
	}
	return bucket.Policy, nil
}

// DeleteBucketPolicy deletes the bucket policy
func (s *Service) DeleteBucketPolicy(ctx context.Context, name string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return err
	}

	bucket.Policy = ""
	return s.store.UpdateBucket(ctx, bucket)
}

// SetBucketTags sets bucket tags
func (s *Service) SetBucketTags(ctx context.Context, name string, tags map[string]string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return err
	}

	bucket.Tags = tags
	return s.store.UpdateBucket(ctx, bucket)
}

// GetBucketTags returns bucket tags
func (s *Service) GetBucketTags(ctx context.Context, name string) (map[string]string, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return nil, err
	}
	return bucket.Tags, nil
}

// DeleteBucketTags deletes all bucket tags
func (s *Service) DeleteBucketTags(ctx context.Context, name string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return err
	}

	bucket.Tags = nil
	return s.store.UpdateBucket(ctx, bucket)
}

// SetCORS sets CORS configuration for a bucket
func (s *Service) SetCORS(ctx context.Context, name string, rules []metadata.CORSRule) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return err
	}

	bucket.CORS = rules
	return s.store.UpdateBucket(ctx, bucket)
}

// GetCORS returns CORS configuration for a bucket
func (s *Service) GetCORS(ctx context.Context, name string) ([]metadata.CORSRule, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(bucket.CORS) == 0 {
		return nil, fmt.Errorf("no CORS configuration")
	}
	return bucket.CORS, nil
}

// DeleteCORS deletes CORS configuration for a bucket
func (s *Service) DeleteCORS(ctx context.Context, name string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return err
	}

	bucket.CORS = nil
	return s.store.UpdateBucket(ctx, bucket)
}

// SetLifecycle sets lifecycle rules for a bucket
func (s *Service) SetLifecycle(ctx context.Context, name string, rules []metadata.LifecycleRule) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return err
	}

	bucket.Lifecycle = rules
	return s.store.UpdateBucket(ctx, bucket)
}

// GetLifecycle returns lifecycle rules for a bucket
func (s *Service) GetLifecycle(ctx context.Context, name string) ([]metadata.LifecycleRule, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(bucket.Lifecycle) == 0 {
		return nil, fmt.Errorf("no lifecycle configuration")
	}
	return bucket.Lifecycle, nil
}

// DeleteLifecycle deletes lifecycle rules for a bucket
func (s *Service) DeleteLifecycle(ctx context.Context, name string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return err
	}

	bucket.Lifecycle = nil
	return s.store.UpdateBucket(ctx, bucket)
}

// validateBucketName validates S3 bucket naming rules
func validateBucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("bucket name must be between 3 and 63 characters")
	}

	if !bucketNameRegex.MatchString(name) {
		return fmt.Errorf("bucket name can only contain lowercase letters, numbers, hyphens, and periods")
	}

	// Additional checks
	if name[0] == '.' || name[len(name)-1] == '.' {
		return fmt.Errorf("bucket name cannot start or end with a period")
	}

	if name[0] == '-' || name[len(name)-1] == '-' {
		return fmt.Errorf("bucket name cannot start or end with a hyphen")
	}

	// Cannot look like an IP address
	ipRegex := regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
	if ipRegex.MatchString(name) {
		return fmt.Errorf("bucket name cannot be formatted as an IP address")
	}

	return nil
}
