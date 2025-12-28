package bucket

import (
	"context"
	"errors"
	"testing"

	"github.com/piwi3910/nebulaio/internal/metadata"
)

// MockMetadataStore implements metadata.Store for testing
type MockMetadataStore struct {
	createBucketErr error
	deleteBucketErr error
	buckets         map[string]*metadata.Bucket
}

func (m *MockMetadataStore) CreateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	if m.createBucketErr != nil {
		return m.createBucketErr
	}
	if m.buckets == nil {
		m.buckets = make(map[string]*metadata.Bucket)
	}
	m.buckets[bucket.Name] = bucket
	return nil
}

func (m *MockMetadataStore) GetBucket(ctx context.Context, name string) (*metadata.Bucket, error) {
	if m.buckets == nil {
		return nil, errors.New("bucket not found")
	}
	bucket, ok := m.buckets[name]
	if !ok {
		return nil, errors.New("bucket not found")
	}
	return bucket, nil
}

func (m *MockMetadataStore) DeleteBucket(ctx context.Context, name string) error {
	if m.deleteBucketErr != nil {
		return m.deleteBucketErr
	}
	delete(m.buckets, name)
	return nil
}

func (m *MockMetadataStore) ListBuckets(ctx context.Context, owner string) ([]*metadata.Bucket, error) {
	return nil, nil
}

func (m *MockMetadataStore) UpdateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	return nil
}

// Implement remaining metadata.Store interface methods as stubs
func (m *MockMetadataStore) GetObject(ctx context.Context, bucket, key string) (*metadata.Object, error) {
	return nil, nil
}
func (m *MockMetadataStore) CreateObject(ctx context.Context, obj *metadata.Object) error { return nil }
func (m *MockMetadataStore) UpdateObject(ctx context.Context, obj *metadata.Object) error { return nil }
func (m *MockMetadataStore) DeleteObject(ctx context.Context, bucket, key string) error  { return nil }
func (m *MockMetadataStore) ListObjects(ctx context.Context, bucket, prefix, marker, delimiter string, maxKeys int) (*metadata.ListObjectsResult, error) {
	return nil, nil
}
func (m *MockMetadataStore) GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*metadata.MultipartUpload, error) {
	return nil, nil
}
func (m *MockMetadataStore) CreateMultipartUpload(ctx context.Context, upload *metadata.MultipartUpload) error {
	return nil
}
func (m *MockMetadataStore) DeleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (m *MockMetadataStore) ListMultipartUploads(ctx context.Context, bucket, prefix, keyMarker, uploadIDMarker string, maxUploads int) ([]*metadata.MultipartUpload, error) {
	return nil, nil
}
func (m *MockMetadataStore) GetPart(ctx context.Context, bucket, key, uploadID string, partNumber int) (*metadata.Part, error) {
	return nil, nil
}
func (m *MockMetadataStore) CreatePart(ctx context.Context, part *metadata.Part) error   { return nil }
func (m *MockMetadataStore) DeletePart(ctx context.Context, part *metadata.Part) error   { return nil }
func (m *MockMetadataStore) ListParts(ctx context.Context, bucket, key, uploadID string) ([]*metadata.Part, error) {
	return nil, nil
}
func (m *MockMetadataStore) Close() error { return nil }

// MockStorageBackend implements object.StorageBackend for testing
type MockStorageBackend struct {
	createBucketErr error
	deleteBucketErr error
}

func (m *MockStorageBackend) CreateBucket(ctx context.Context, name string) error {
	return m.createBucketErr
}

func (m *MockStorageBackend) DeleteBucket(ctx context.Context, name string) error {
	return m.deleteBucketErr
}

func (m *MockStorageBackend) BucketExists(ctx context.Context, name string) (bool, error) {
	return true, nil
}

func (m *MockStorageBackend) PutObject(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	return nil
}

func (m *MockStorageBackend) GetObject(ctx context.Context, bucket, key string) ([]byte, error) {
	return nil, nil
}

func (m *MockStorageBackend) DeleteObject(ctx context.Context, bucket, key string) error {
	return nil
}

func (m *MockStorageBackend) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	return nil, nil
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
			mockStore := &MockMetadataStore{
				deleteBucketErr: tc.metadataRollback,
			}
			mockStorage := &MockStorageBackend{
				createBucketErr: tc.storageErr,
			}

			service := NewService(mockStore, mockStorage)

			bucket, err := service.CreateBucket(context.Background(), "test-bucket", "owner", "us-east-1", "STANDARD")

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tc.errorContains != "" && err != nil {
					if !contains(err.Error(), tc.errorContains) {
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
