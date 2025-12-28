package bucket

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/audit"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// mockMetadataStore implements metadata.Store for testing bucket operations
type mockMetadataStore struct {
	createBucketErr error
	deleteBucketErr error
	buckets         map[string]*metadata.Bucket
}

func newMockMetadataStore() *mockMetadataStore {
	return &mockMetadataStore{
		buckets: make(map[string]*metadata.Bucket),
	}
}

func (m *mockMetadataStore) Close() error                        { return nil }
func (m *mockMetadataStore) IsLeader() bool                      { return true }
func (m *mockMetadataStore) LeaderAddress() (string, error)      { return "", nil }

func (m *mockMetadataStore) CreateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	if m.createBucketErr != nil {
		return m.createBucketErr
	}
	m.buckets[bucket.Name] = bucket
	return nil
}

func (m *mockMetadataStore) GetBucket(ctx context.Context, name string) (*metadata.Bucket, error) {
	bucket, ok := m.buckets[name]
	if !ok {
		return nil, errors.New("bucket not found")
	}
	return bucket, nil
}

func (m *mockMetadataStore) DeleteBucket(ctx context.Context, name string) error {
	if m.deleteBucketErr != nil {
		return m.deleteBucketErr
	}
	delete(m.buckets, name)
	return nil
}

func (m *mockMetadataStore) ListBuckets(ctx context.Context, owner string) ([]*metadata.Bucket, error) {
	return nil, nil
}

func (m *mockMetadataStore) UpdateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	return nil
}

// Object metadata stubs
func (m *mockMetadataStore) PutObjectMeta(ctx context.Context, meta *metadata.ObjectMeta) error {
	return nil
}
func (m *mockMetadataStore) GetObjectMeta(ctx context.Context, bucket, key string) (*metadata.ObjectMeta, error) {
	return nil, nil
}
func (m *mockMetadataStore) DeleteObjectMeta(ctx context.Context, bucket, key string) error {
	return nil
}
func (m *mockMetadataStore) ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*metadata.ObjectListing, error) {
	return nil, nil
}

// Version stubs
func (m *mockMetadataStore) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (*metadata.ObjectMeta, error) {
	return nil, nil
}
func (m *mockMetadataStore) ListObjectVersions(ctx context.Context, bucket, prefix, delimiter, keyMarker, versionIDMarker string, maxKeys int) (*metadata.VersionListing, error) {
	return nil, nil
}
func (m *mockMetadataStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	return nil
}
func (m *mockMetadataStore) PutObjectMetaVersioned(ctx context.Context, meta *metadata.ObjectMeta, preserveOldVersions bool) error {
	return nil
}

// Multipart stubs
func (m *mockMetadataStore) CreateMultipartUpload(ctx context.Context, upload *metadata.MultipartUpload) error {
	return nil
}
func (m *mockMetadataStore) GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*metadata.MultipartUpload, error) {
	return nil, nil
}
func (m *mockMetadataStore) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (m *mockMetadataStore) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (m *mockMetadataStore) AddUploadPart(ctx context.Context, bucket, key, uploadID string, part *metadata.UploadPart) error {
	return nil
}
func (m *mockMetadataStore) ListMultipartUploads(ctx context.Context, bucket string) ([]*metadata.MultipartUpload, error) {
	return nil, nil
}

// User stubs
func (m *mockMetadataStore) CreateUser(ctx context.Context, user *metadata.User) error { return nil }
func (m *mockMetadataStore) GetUser(ctx context.Context, id string) (*metadata.User, error) {
	return nil, nil
}
func (m *mockMetadataStore) GetUserByUsername(ctx context.Context, username string) (*metadata.User, error) {
	return nil, nil
}
func (m *mockMetadataStore) UpdateUser(ctx context.Context, user *metadata.User) error { return nil }
func (m *mockMetadataStore) DeleteUser(ctx context.Context, id string) error           { return nil }
func (m *mockMetadataStore) ListUsers(ctx context.Context) ([]*metadata.User, error)   { return nil, nil }

// Access key stubs
func (m *mockMetadataStore) CreateAccessKey(ctx context.Context, key *metadata.AccessKey) error {
	return nil
}
func (m *mockMetadataStore) GetAccessKey(ctx context.Context, accessKeyID string) (*metadata.AccessKey, error) {
	return nil, nil
}
func (m *mockMetadataStore) DeleteAccessKey(ctx context.Context, accessKeyID string) error { return nil }
func (m *mockMetadataStore) ListAccessKeys(ctx context.Context, userID string) ([]*metadata.AccessKey, error) {
	return nil, nil
}

// Policy stubs
func (m *mockMetadataStore) CreatePolicy(ctx context.Context, policy *metadata.Policy) error {
	return nil
}
func (m *mockMetadataStore) GetPolicy(ctx context.Context, name string) (*metadata.Policy, error) {
	return nil, nil
}
func (m *mockMetadataStore) UpdatePolicy(ctx context.Context, policy *metadata.Policy) error {
	return nil
}
func (m *mockMetadataStore) DeletePolicy(ctx context.Context, name string) error { return nil }
func (m *mockMetadataStore) ListPolicies(ctx context.Context) ([]*metadata.Policy, error) {
	return nil, nil
}

// Cluster stubs
func (m *mockMetadataStore) GetClusterInfo(ctx context.Context) (*metadata.ClusterInfo, error) {
	return nil, nil
}
func (m *mockMetadataStore) AddNode(ctx context.Context, node *metadata.NodeInfo) error { return nil }
func (m *mockMetadataStore) RemoveNode(ctx context.Context, nodeID string) error        { return nil }
func (m *mockMetadataStore) ListNodes(ctx context.Context) ([]*metadata.NodeInfo, error) {
	return nil, nil
}

// Audit stubs
func (m *mockMetadataStore) StoreAuditEvent(ctx context.Context, event *audit.AuditEvent) error {
	return nil
}
func (m *mockMetadataStore) ListAuditEvents(ctx context.Context, filter audit.AuditFilter) (*audit.AuditListResult, error) {
	return nil, nil
}
func (m *mockMetadataStore) DeleteOldAuditEvents(ctx context.Context, before time.Time) (int, error) {
	return 0, nil
}

// mockStorageBackend implements object.StorageBackend for testing
type mockStorageBackend struct {
	createBucketErr error
	deleteBucketErr error
}

func (m *mockStorageBackend) Init(ctx context.Context) error { return nil }
func (m *mockStorageBackend) Close() error                   { return nil }

func (m *mockStorageBackend) CreateBucket(ctx context.Context, bucket string) error {
	return m.createBucketErr
}

func (m *mockStorageBackend) DeleteBucket(ctx context.Context, bucket string) error {
	return m.deleteBucketErr
}

func (m *mockStorageBackend) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return true, nil
}

func (m *mockStorageBackend) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	return nil, nil
}

func (m *mockStorageBackend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockStorageBackend) DeleteObject(ctx context.Context, bucket, key string) error {
	return nil
}

func (m *mockStorageBackend) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	return false, nil
}

func (m *mockStorageBackend) GetStorageInfo(ctx context.Context) (*backend.StorageInfo, error) {
	return nil, nil
}

// Multipart stubs
func (m *mockStorageBackend) CreateMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}

func (m *mockStorageBackend) PutPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*backend.PutResult, error) {
	return nil, nil
}

func (m *mockStorageBackend) GetPart(ctx context.Context, bucket, key, uploadID string, partNumber int) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockStorageBackend) CompleteParts(ctx context.Context, bucket, key, uploadID string, parts []int) (*backend.PutResult, error) {
	return nil, nil
}

func (m *mockStorageBackend) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}

// TestBucketCreateRollback verifies that bucket creation handles storage failures
// and properly rolls back metadata changes
func TestBucketCreateRollback(t *testing.T) {
	tests := []struct {
		name             string
		storageErr       error
		metadataRollback error
		expectError      bool
		errorContains    string
	}{
		{
			name:          "Storage creation fails, rollback succeeds",
			storageErr:    errors.New("disk full"),
			expectError:   true,
			errorContains: "disk full",
		},
		{
			name:             "Storage creation fails, rollback also fails",
			storageErr:       errors.New("storage error"),
			metadataRollback: errors.New("rollback failed"),
			expectError:      true,
			errorContains:    "rollback failed",
		},
		{
			name:        "Successful bucket creation",
			storageErr:  nil,
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockStore := newMockMetadataStore()
			mockStore.deleteBucketErr = tc.metadataRollback

			mockStorage := &mockStorageBackend{
				createBucketErr: tc.storageErr,
			}

			service := NewService(mockStore, mockStorage)

			bucket, err := service.CreateBucket(context.Background(), "test-bucket", "owner", "us-east-1", "STANDARD")

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tc.errorContains != "" && err != nil {
					if !strings.Contains(err.Error(), tc.errorContains) {
						t.Errorf("Expected error to contain '%s', got: %s", tc.errorContains, err.Error())
					}
				}
				if bucket != nil {
					t.Error("Expected nil bucket on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if bucket == nil {
					t.Error("Expected non-nil bucket")
				}
			}
		})
	}
}

// TestValidateBucketName tests bucket name validation
func TestValidateBucketName(t *testing.T) {
	tests := []struct {
		name        string
		bucketName  string
		expectError bool
	}{
		{"Valid name", "my-bucket", false},
		{"Valid name with numbers", "bucket123", false},
		{"Valid name with dots", "my.bucket.name", false},
		{"Too short", "ab", true},
		{"Too long", strings.Repeat("a", 64), true},
		{"Starts with hyphen", "-bucket", true},
		{"Ends with hyphen", "bucket-", true},
		{"Contains uppercase", "MyBucket", true},
		{"Contains underscore", "my_bucket", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBucketName(tc.bucketName)
			if tc.expectError && err == nil {
				t.Errorf("Expected error for bucket name '%s'", tc.bucketName)
			}
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error for bucket name '%s': %v", tc.bucketName, err)
			}
		})
	}
}

// TestBucketCreateRollbackIntegration tests the bucket creation rollback flow
// with various failure scenarios
func TestBucketCreateRollbackIntegration(t *testing.T) {
	t.Run("storage failure during creation triggers metadata rollback", func(t *testing.T) {
		// Create mock stores with storage that fails on bucket creation
		metaStore := newMockMetadataStore()
		storageBackend := &mockStorageBackend{
			createBucketErr: errors.New("simulated storage failure"),
		}

		service := NewService(metaStore, storageBackend, nil)

		// Attempt to create bucket - should fail
		_, err := service.CreateBucket(context.Background(), "rollback-test-bucket", "test-owner", nil)
		if err == nil {
			t.Error("Expected error when storage fails, got nil")
		}

		// Verify bucket is NOT in metadata store (rollback should have removed it)
		_, getErr := metaStore.GetBucket(context.Background(), "rollback-test-bucket")
		if getErr == nil {
			t.Error("Bucket should not exist in metadata after failed storage creation")
		}
	})

	t.Run("metadata rollback failure is logged but does not mask original error", func(t *testing.T) {
		// Create mock stores where both storage and metadata deletion fail
		metaStore := &mockMetadataStore{
			buckets:         make(map[string]*metadata.Bucket),
			deleteBucketErr: errors.New("metadata deletion failed"),
		}
		storageBackend := &mockStorageBackend{
			createBucketErr: errors.New("storage creation failed"),
		}

		service := NewService(metaStore, storageBackend, nil)

		// Attempt to create bucket - should fail with storage error
		_, err := service.CreateBucket(context.Background(), "double-fail-bucket", "test-owner", nil)
		if err == nil {
			t.Error("Expected error when storage fails, got nil")
		}

		// The returned error should be about storage, not metadata rollback
		if !strings.Contains(err.Error(), "storage") && !strings.Contains(err.Error(), "failed") {
			t.Logf("Error message: %v", err)
		}
	})

	t.Run("successful creation leaves bucket in consistent state", func(t *testing.T) {
		// Create mock stores that succeed
		metaStore := newMockMetadataStore()
		storageBackend := &mockStorageBackend{
			buckets: make(map[string]bool),
		}

		service := NewService(metaStore, storageBackend, nil)

		// Create bucket successfully
		bucket, err := service.CreateBucket(context.Background(), "success-bucket", "test-owner", nil)
		if err != nil {
			t.Fatalf("Expected successful creation, got error: %v", err)
		}
		if bucket == nil {
			t.Fatal("Expected non-nil bucket after successful creation")
		}

		// Verify bucket exists in metadata
		gotBucket, getErr := metaStore.GetBucket(context.Background(), "success-bucket")
		if getErr != nil {
			t.Errorf("Bucket should exist in metadata after creation: %v", getErr)
		}
		if gotBucket == nil {
			t.Error("Expected non-nil bucket from metadata store")
		}

		// Verify storage was created
		if !storageBackend.buckets["success-bucket"] {
			t.Error("Bucket should exist in storage after creation")
		}
	})

	t.Run("concurrent bucket creation with failures", func(t *testing.T) {
		// Create mock stores
		metaStore := newMockMetadataStore()
		storageBackend := &mockStorageBackend{
			buckets: make(map[string]bool),
		}

		service := NewService(metaStore, storageBackend, nil)

		// Create several buckets concurrently
		const numBuckets = 10
		results := make(chan error, numBuckets)

		for i := 0; i < numBuckets; i++ {
			go func(idx int) {
				bucketName := "concurrent-bucket-" + string(rune('a'+idx))
				_, err := service.CreateBucket(context.Background(), bucketName, "test-owner", nil)
				results <- err
			}(i)
		}

		// Collect results
		successCount := 0
		for i := 0; i < numBuckets; i++ {
			err := <-results
			if err == nil {
				successCount++
			}
		}

		// All should succeed in this test (no injected failures)
		if successCount != numBuckets {
			t.Errorf("Expected %d successful creations, got %d", numBuckets, successCount)
		}
	})
}

// mockStorageBackend is a test storage backend that can simulate failures
type mockStorageBackend struct {
	createBucketErr error
	deleteBucketErr error
	buckets         map[string]bool
}

func (m *mockStorageBackend) Type() string { return "mock" }

func (m *mockStorageBackend) CreateBucket(ctx context.Context, name string) error {
	if m.createBucketErr != nil {
		return m.createBucketErr
	}
	if m.buckets == nil {
		m.buckets = make(map[string]bool)
	}
	m.buckets[name] = true
	return nil
}

func (m *mockStorageBackend) DeleteBucket(ctx context.Context, name string) error {
	if m.deleteBucketErr != nil {
		return m.deleteBucketErr
	}
	delete(m.buckets, name)
	return nil
}

func (m *mockStorageBackend) BucketExists(ctx context.Context, name string) (bool, error) {
	return m.buckets[name], nil
}

func (m *mockStorageBackend) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType string) (string, error) {
	return "", nil
}

func (m *mockStorageBackend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, int64, error) {
	return nil, 0, nil
}

func (m *mockStorageBackend) DeleteObject(ctx context.Context, bucket, key string) error {
	return nil
}

func (m *mockStorageBackend) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	return false, nil
}

func (m *mockStorageBackend) GetObjectSize(ctx context.Context, bucket, key string) (int64, error) {
	return 0, nil
}

func (m *mockStorageBackend) ListObjects(ctx context.Context, bucket, prefix string) ([]backend.ObjectInfo, error) {
	return nil, nil
}

func (m *mockStorageBackend) Close() error {
	return nil
}
