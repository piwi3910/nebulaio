package bucket

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/object"
	"github.com/piwi3910/nebulaio/internal/policy"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
	"github.com/piwi3910/nebulaio/pkg/s3types"
)

// Tag validation constants for buckets
const (
	MaxTagsPerBucket  = 50 // Buckets can have more tags than objects
	MaxTagKeyLength   = 128
	MaxTagValueLength = 256
)

// validateBucketTags validates tags according to S3 tagging rules for buckets
func validateBucketTags(tags map[string]string) error {
	if len(tags) > MaxTagsPerBucket {
		return fmt.Errorf("tag count exceeds maximum of %d", MaxTagsPerBucket)
	}

	for key, value := range tags {
		keyLen := utf8.RuneCountInString(key)
		valueLen := utf8.RuneCountInString(value)

		if keyLen == 0 {
			return fmt.Errorf("tag key cannot be empty")
		}

		if keyLen > MaxTagKeyLength {
			return fmt.Errorf("tag key '%s' exceeds maximum length of %d characters", key, MaxTagKeyLength)
		}

		if valueLen > MaxTagValueLength {
			return fmt.Errorf("tag value for key '%s' exceeds maximum length of %d characters", key, MaxTagValueLength)
		}

		// Check for reserved aws: prefix
		if strings.HasPrefix(strings.ToLower(key), "aws:") {
			return fmt.Errorf("tag key '%s' uses reserved 'aws:' prefix", key)
		}
	}

	return nil
}

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
		return nil, s3errors.ErrBucketAlreadyExists.WithResource(name)
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
		return nil, s3errors.ErrInternalError.WithMessage("failed to create bucket metadata: " + err.Error())
	}

	// Create storage
	if err := s.storage.CreateBucket(ctx, name); err != nil {
		// Rollback metadata
		s.store.DeleteBucket(ctx, name)
		return nil, s3errors.ErrInternalError.WithMessage("failed to create bucket storage: " + err.Error())
	}

	return bucket, nil
}

// GetBucket retrieves a bucket by name
func (s *Service) GetBucket(ctx context.Context, name string) (*metadata.Bucket, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return nil, s3errors.ErrNoSuchBucket.WithResource(name)
	}
	return bucket, nil
}

// DeleteBucket deletes a bucket
func (s *Service) DeleteBucket(ctx context.Context, name string) error {
	// Check if bucket exists
	if _, err := s.store.GetBucket(ctx, name); err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	// Check if bucket is empty
	listing, err := s.store.ListObjects(ctx, name, "", "", 1, "")
	if err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to check bucket contents: " + err.Error())
	}
	if len(listing.Objects) > 0 {
		return s3errors.ErrBucketNotEmpty.WithResource(name)
	}

	// Delete storage
	if err := s.storage.DeleteBucket(ctx, name); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to delete bucket storage: " + err.Error())
	}

	// Delete metadata
	if err := s.store.DeleteBucket(ctx, name); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to delete bucket metadata: " + err.Error())
	}

	return nil
}

// ListBuckets lists all buckets owned by a user
func (s *Service) ListBuckets(ctx context.Context, owner string) ([]*metadata.Bucket, error) {
	buckets, err := s.store.ListBuckets(ctx, owner)
	if err != nil {
		return nil, s3errors.ErrInternalError.WithMessage("failed to list buckets: " + err.Error())
	}
	return buckets, nil
}

// HeadBucket checks if a bucket exists
func (s *Service) HeadBucket(ctx context.Context, name string) error {
	_, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}
	return nil
}

// SetVersioning enables or disables versioning for a bucket
func (s *Service) SetVersioning(ctx context.Context, name string, status metadata.VersioningStatus) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	// Cannot disable versioning once enabled, only suspend
	if bucket.Versioning == metadata.VersioningEnabled && status == metadata.VersioningDisabled {
		return s3errors.ErrInvalidBucketState.WithMessage("versioning cannot be disabled, only suspended")
	}

	bucket.Versioning = status
	if err := s.store.UpdateBucket(ctx, bucket); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to update versioning: " + err.Error())
	}
	return nil
}

// GetVersioning returns the versioning status for a bucket
func (s *Service) GetVersioning(ctx context.Context, name string) (metadata.VersioningStatus, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return "", s3errors.ErrNoSuchBucket.WithResource(name)
	}
	return bucket.Versioning, nil
}

// SetBucketPolicy sets the bucket policy
func (s *Service) SetBucketPolicy(ctx context.Context, name, policyJSON string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	// Validate policy JSON by parsing and validating it
	parsedPolicy, err := policy.ParsePolicy(policyJSON)
	if err != nil {
		return s3errors.ErrMalformedPolicy.WithMessage("invalid policy JSON: " + err.Error())
	}

	// Validate the policy structure and content
	if err := parsedPolicy.Validate(); err != nil {
		return s3errors.ErrMalformedPolicy.WithMessage("policy validation failed: " + err.Error())
	}

	bucket.Policy = policyJSON
	if err := s.store.UpdateBucket(ctx, bucket); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to set bucket policy: " + err.Error())
	}
	return nil
}

// GetBucketPolicy returns the bucket policy
func (s *Service) GetBucketPolicy(ctx context.Context, name string) (string, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return "", s3errors.ErrNoSuchBucket.WithResource(name)
	}
	if bucket.Policy == "" {
		return "", s3errors.ErrNoSuchBucketPolicy.WithResource(name)
	}
	return bucket.Policy, nil
}

// DeleteBucketPolicy deletes the bucket policy
func (s *Service) DeleteBucketPolicy(ctx context.Context, name string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	bucket.Policy = ""
	if err := s.store.UpdateBucket(ctx, bucket); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to delete bucket policy: " + err.Error())
	}
	return nil
}

// PutBucketTagging sets bucket tags with validation
func (s *Service) PutBucketTagging(ctx context.Context, name string, tags map[string]string) error {
	// Validate tags
	if err := validateBucketTags(tags); err != nil {
		return err
	}

	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	bucket.Tags = tags
	if err := s.store.UpdateBucket(ctx, bucket); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to set bucket tags: " + err.Error())
	}
	return nil
}

// GetBucketTagging returns bucket tags
func (s *Service) GetBucketTagging(ctx context.Context, name string) (map[string]string, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return nil, s3errors.ErrNoSuchBucket.WithResource(name)
	}

	// Return empty map if no tags
	if bucket.Tags == nil {
		return make(map[string]string), nil
	}

	return bucket.Tags, nil
}

// DeleteBucketTagging deletes all bucket tags
func (s *Service) DeleteBucketTagging(ctx context.Context, name string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	bucket.Tags = nil
	if err := s.store.UpdateBucket(ctx, bucket); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to delete bucket tags: " + err.Error())
	}
	return nil
}

// SetBucketTags sets bucket tags (legacy method, calls PutBucketTagging)
func (s *Service) SetBucketTags(ctx context.Context, name string, tags map[string]string) error {
	return s.PutBucketTagging(ctx, name, tags)
}

// GetBucketTags returns bucket tags (legacy method, calls GetBucketTagging)
func (s *Service) GetBucketTags(ctx context.Context, name string) (map[string]string, error) {
	return s.GetBucketTagging(ctx, name)
}

// DeleteBucketTags deletes all bucket tags (legacy method, calls DeleteBucketTagging)
func (s *Service) DeleteBucketTags(ctx context.Context, name string) error {
	return s.DeleteBucketTagging(ctx, name)
}

// SetCORS sets CORS configuration for a bucket
func (s *Service) SetCORS(ctx context.Context, name string, rules []metadata.CORSRule) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	bucket.CORS = rules
	if err := s.store.UpdateBucket(ctx, bucket); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to set CORS configuration: " + err.Error())
	}
	return nil
}

// GetCORS returns CORS configuration for a bucket
func (s *Service) GetCORS(ctx context.Context, name string) ([]metadata.CORSRule, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return nil, s3errors.ErrNoSuchBucket.WithResource(name)
	}
	if len(bucket.CORS) == 0 {
		return nil, s3errors.ErrNoSuchCORSConfiguration.WithResource(name)
	}
	return bucket.CORS, nil
}

// DeleteCORS deletes CORS configuration for a bucket
func (s *Service) DeleteCORS(ctx context.Context, name string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	bucket.CORS = nil
	if err := s.store.UpdateBucket(ctx, bucket); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to delete CORS configuration: " + err.Error())
	}
	return nil
}

// SetLifecycle sets lifecycle rules for a bucket
func (s *Service) SetLifecycle(ctx context.Context, name string, rules []metadata.LifecycleRule) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	bucket.Lifecycle = rules
	if err := s.store.UpdateBucket(ctx, bucket); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to set lifecycle configuration: " + err.Error())
	}
	return nil
}

// GetLifecycle returns lifecycle rules for a bucket
func (s *Service) GetLifecycle(ctx context.Context, name string) ([]metadata.LifecycleRule, error) {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return nil, s3errors.ErrNoSuchBucket.WithResource(name)
	}
	if len(bucket.Lifecycle) == 0 {
		return nil, s3errors.ErrNoSuchLifecycleConfiguration.WithResource(name)
	}
	return bucket.Lifecycle, nil
}

// DeleteLifecycle deletes lifecycle rules for a bucket
func (s *Service) DeleteLifecycle(ctx context.Context, name string) error {
	bucket, err := s.store.GetBucket(ctx, name)
	if err != nil {
		return s3errors.ErrNoSuchBucket.WithResource(name)
	}

	bucket.Lifecycle = nil
	if err := s.store.UpdateBucket(ctx, bucket); err != nil {
		return s3errors.ErrInternalError.WithMessage("failed to delete lifecycle configuration: " + err.Error())
	}
	return nil
}

// validateBucketName validates S3 bucket naming rules
func validateBucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return s3errors.ErrInvalidBucketName.WithMessage("bucket name must be between 3 and 63 characters")
	}

	if !bucketNameRegex.MatchString(name) {
		return s3errors.ErrInvalidBucketName.WithMessage("bucket name can only contain lowercase letters, numbers, hyphens, and periods")
	}

	// Additional checks
	if name[0] == '.' || name[len(name)-1] == '.' {
		return s3errors.ErrInvalidBucketName.WithMessage("bucket name cannot start or end with a period")
	}

	if name[0] == '-' || name[len(name)-1] == '-' {
		return s3errors.ErrInvalidBucketName.WithMessage("bucket name cannot start or end with a hyphen")
	}

	// Cannot look like an IP address
	ipRegex := regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
	if ipRegex.MatchString(name) {
		return s3errors.ErrInvalidBucketName.WithMessage("bucket name cannot be formatted as an IP address")
	}

	return nil
}

// FindMatchingCORSRule finds a CORS rule that matches the given origin and method
func (s *Service) FindMatchingCORSRule(rules []metadata.CORSRule, origin, method string) *metadata.CORSRule {
	for i := range rules {
		rule := &rules[i]
		// Check if origin matches
		if _, matched := MatchCORSOrigin(rule.AllowedOrigins, origin); !matched {
			continue
		}

		// Check if method is allowed
		methodAllowed := false
		for _, allowedMethod := range rule.AllowedMethods {
			if strings.EqualFold(allowedMethod, method) {
				methodAllowed = true
				break
			}
		}
		if !methodAllowed {
			continue
		}

		return rule
	}
	return nil
}

// ParseAndValidateCORSRules converts S3 CORS rules to internal format with validation
func (s *Service) ParseAndValidateCORSRules(s3Rules []s3types.CORSRule) ([]metadata.CORSRule, error) {
	if len(s3Rules) == 0 {
		return nil, fmt.Errorf("CORS configuration must have at least one rule")
	}

	if len(s3Rules) > 100 {
		return nil, fmt.Errorf("CORS configuration cannot have more than 100 rules")
	}

	rules := make([]metadata.CORSRule, 0, len(s3Rules))
	for i, s3Rule := range s3Rules {
		// Validate required fields
		if len(s3Rule.AllowedOrigin) == 0 {
			return nil, fmt.Errorf("rule %d: AllowedOrigin is required", i+1)
		}
		if len(s3Rule.AllowedMethod) == 0 {
			return nil, fmt.Errorf("rule %d: AllowedMethod is required", i+1)
		}

		// Validate methods
		validMethods := map[string]bool{
			"GET": true, "PUT": true, "POST": true, "DELETE": true, "HEAD": true,
		}
		for _, method := range s3Rule.AllowedMethod {
			if !validMethods[strings.ToUpper(method)] {
				return nil, fmt.Errorf("rule %d: invalid method '%s'", i+1, method)
			}
		}

		// Validate MaxAgeSeconds
		if s3Rule.MaxAgeSeconds < 0 {
			return nil, fmt.Errorf("rule %d: MaxAgeSeconds cannot be negative", i+1)
		}
		if s3Rule.MaxAgeSeconds > 86400 {
			return nil, fmt.Errorf("rule %d: MaxAgeSeconds cannot exceed 86400", i+1)
		}

		rules = append(rules, metadata.CORSRule{
			AllowedOrigins: s3Rule.AllowedOrigin,
			AllowedMethods: s3Rule.AllowedMethod,
			AllowedHeaders: s3Rule.AllowedHeader,
			ExposeHeaders:  s3Rule.ExposeHeader,
			MaxAgeSeconds:  s3Rule.MaxAgeSeconds,
		})
	}

	return rules, nil
}

// MatchCORSOrigin checks if the origin matches any of the allowed origins
// Returns the origin to use in the response and whether it matched
func MatchCORSOrigin(allowedOrigins []string, origin string) (string, bool) {
	for _, allowed := range allowedOrigins {
		// Exact match or wildcard
		if allowed == "*" {
			return "*", true
		}
		if allowed == origin {
			return origin, true
		}
		// Wildcard subdomain matching (e.g., "*.example.com")
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:] // Get ".example.com"
			// Origin format: https://subdomain.example.com
			if idx := strings.Index(origin, "://"); idx != -1 {
				domain := origin[idx+3:] // Get "subdomain.example.com"
				if strings.HasSuffix(domain, suffix[1:]) {
					return origin, true
				}
			}
		}
	}
	return "", false
}
