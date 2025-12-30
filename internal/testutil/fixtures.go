package testutil

import (
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
)

// Test fixture constants.
const (
	// DefaultTestBucketName is the default bucket name for tests.
	DefaultTestBucketName = "test-bucket"
	// DefaultTestObjectKey is the default object key for tests.
	DefaultTestObjectKey = "test-key"
	// DefaultTestRegion is the default region for tests.
	DefaultTestRegion = "us-east-1"
	// DefaultTestOwner is the default owner ID for tests.
	DefaultTestOwner = "test-owner"
	// DefaultTestETag is a sample ETag for tests.
	DefaultTestETag = "d41d8cd98f00b204e9800998ecf8427e"
	// DefaultTestContentType is the default content type for tests.
	DefaultTestContentType = "application/octet-stream"
)

// NewTestBucket creates a bucket with test defaults.
// Override fields as needed for specific test cases.
func NewTestBucket(name string) *metadata.Bucket {
	if name == "" {
		name = DefaultTestBucketName
	}

	now := time.Now()

	return &metadata.Bucket{
		Name:       name,
		Owner:      DefaultTestOwner,
		Region:     DefaultTestRegion,
		CreatedAt:  now,
		Versioning: metadata.VersioningSuspended,
	}
}

// NewTestObjectMeta creates object metadata with test defaults.
// Override fields as needed for specific test cases.
func NewTestObjectMeta(bucket, key string, size int64) *metadata.ObjectMeta {
	if bucket == "" {
		bucket = DefaultTestBucketName
	}

	if key == "" {
		key = DefaultTestObjectKey
	}

	now := time.Now()

	return &metadata.ObjectMeta{
		Bucket:       bucket,
		Key:          key,
		Size:         size,
		ContentType:  DefaultTestContentType,
		ETag:         DefaultTestETag,
		CreatedAt:    now,
		ModifiedAt:   now,
		Owner:        DefaultTestOwner,
		StorageClass: "STANDARD",
	}
}

// NewTestUser creates a user with test defaults.
func NewTestUser(username string) *metadata.User {
	if username == "" {
		username = "test-user"
	}

	now := time.Now()

	return &metadata.User{
		ID:        username,
		Username:  username,
		Email:     username + "@example.com",
		Role:      metadata.RoleUser,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewTestAccessKey creates an access key with test defaults.
func NewTestAccessKey(userID, accessKeyID, secretKey string) *metadata.AccessKey {
	if userID == "" {
		userID = "test-user"
	}

	if accessKeyID == "" {
		accessKeyID = "AKIAIOSFODNN7EXAMPLE"
	}

	if secretKey == "" {
		secretKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	}

	now := time.Now()

	return &metadata.AccessKey{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretKey,
		UserID:          userID,
		Enabled:         true,
		CreatedAt:       now,
	}
}

// NewTestPolicy creates a policy with test defaults.
func NewTestPolicy(name string) *metadata.Policy {
	if name == "" {
		name = "test-policy"
	}

	now := time.Now()

	return &metadata.Policy{
		Name:        name,
		Description: "Test policy",
		Document:    `{"Version":"2012-10-17","Statement":[{"Sid":"AllowAll","Effect":"Allow","Action":["s3:*"],"Resource":["*"]}]}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// NewTestMultipartUpload creates a multipart upload with test defaults.
func NewTestMultipartUpload(bucket, key, uploadID string) *metadata.MultipartUpload {
	if bucket == "" {
		bucket = DefaultTestBucketName
	}

	if key == "" {
		key = DefaultTestObjectKey
	}

	if uploadID == "" {
		uploadID = "test-upload-id"
	}

	now := time.Now()

	return &metadata.MultipartUpload{
		UploadID:  uploadID,
		Bucket:    bucket,
		Key:       key,
		CreatedAt: now,
		Initiator: DefaultTestOwner,
	}
}

// TestData provides common test data slices.
var TestData = struct {
	// SmallData is a small byte slice for testing (16 bytes).
	SmallData []byte
	// MediumData is a medium byte slice for testing (1KB).
	MediumData []byte
	// LargeData is a large byte slice for testing (1MB).
	LargeData []byte
}{
	SmallData:  make([]byte, 16),
	MediumData: make([]byte, 1024),
	LargeData:  make([]byte, 1024*1024),
}

func init() {
	// Fill test data with pattern.
	for i := range TestData.SmallData {
		TestData.SmallData[i] = byte(i % 256)
	}

	for i := range TestData.MediumData {
		TestData.MediumData[i] = byte(i % 256)
	}

	for i := range TestData.LargeData {
		TestData.LargeData[i] = byte(i % 256)
	}
}
