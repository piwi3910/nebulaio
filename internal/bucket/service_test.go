package bucket

import (
	"context"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockMetadataStore implements metadata.Store interface for testing
type MockMetadataStore struct {
	buckets   map[string]*metadata.Bucket
	objects   map[string]map[string]*metadata.ObjectMeta
	createErr error
	updateErr error
	deleteErr error
}

func NewMockMetadataStore() *MockMetadataStore {
	return &MockMetadataStore{
		buckets: make(map[string]*metadata.Bucket),
		objects: make(map[string]map[string]*metadata.ObjectMeta),
	}
}

func (m *MockMetadataStore) GetBucket(ctx context.Context, name string) (*metadata.Bucket, error) {
	if bucket, ok := m.buckets[name]; ok {
		return bucket, nil
	}
	return nil, assert.AnError
}

func (m *MockMetadataStore) CreateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.buckets[bucket.Name] = bucket
	m.objects[bucket.Name] = make(map[string]*metadata.ObjectMeta)
	return nil
}

func (m *MockMetadataStore) UpdateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.buckets[bucket.Name] = bucket
	return nil
}

func (m *MockMetadataStore) DeleteBucket(ctx context.Context, name string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.buckets, name)
	delete(m.objects, name)
	return nil
}

func (m *MockMetadataStore) ListBuckets(ctx context.Context, owner string) ([]*metadata.Bucket, error) {
	buckets := make([]*metadata.Bucket, 0)
	for _, b := range m.buckets {
		if owner == "" || b.Owner == owner {
			buckets = append(buckets, b)
		}
	}
	return buckets, nil
}

func (m *MockMetadataStore) ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*metadata.ObjectListing, error) {
	objs := m.objects[bucket]
	listing := &metadata.ObjectListing{
		Objects: make([]*metadata.ObjectMeta, 0),
	}
	for _, obj := range objs {
		listing.Objects = append(listing.Objects, obj)
		if len(listing.Objects) >= maxKeys {
			break
		}
	}
	return listing, nil
}

// Implement remaining interface methods as no-ops
func (m *MockMetadataStore) GetObject(ctx context.Context, bucket, key string) (*metadata.ObjectMeta, error) { return nil, nil }
func (m *MockMetadataStore) PutObject(ctx context.Context, bucket string, obj *metadata.ObjectMeta) error { return nil }
func (m *MockMetadataStore) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (m *MockMetadataStore) ListObjectVersions(ctx context.Context, bucket, prefix, delimiter, keyMarker, versionMarker string, maxKeys int) (*metadata.VersionListing, error) { return nil, nil }
func (m *MockMetadataStore) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (*metadata.ObjectMeta, error) { return nil, nil }
func (m *MockMetadataStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error { return nil }
func (m *MockMetadataStore) CreateMultipartUpload(ctx context.Context, upload *metadata.MultipartUpload) error { return nil }
func (m *MockMetadataStore) GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*metadata.MultipartUpload, error) { return nil, nil }
func (m *MockMetadataStore) DeleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error { return nil }
func (m *MockMetadataStore) ListMultipartUploads(ctx context.Context, bucket, prefix, keyMarker, uploadIDMarker string, maxUploads int) (*metadata.MultipartUploadListing, error) { return nil, nil }
func (m *MockMetadataStore) AddPart(ctx context.Context, bucket, key, uploadID string, part *metadata.Part) error { return nil }
func (m *MockMetadataStore) GetParts(ctx context.Context, bucket, key, uploadID string) ([]*metadata.Part, error) { return nil, nil }
func (m *MockMetadataStore) GetClusterInfo(ctx context.Context) (*metadata.ClusterInfo, error) { return nil, nil }
func (m *MockMetadataStore) IsLeader() bool { return true }
func (m *MockMetadataStore) LeaderAddress() (string, bool) { return "", false }
func (m *MockMetadataStore) Close() error { return nil }

// MockStorageBackend implements object.StorageBackend interface for testing
type MockStorageBackend struct {
	buckets   map[string]bool
	createErr error
	deleteErr error
}

func NewMockStorageBackend() *MockStorageBackend {
	return &MockStorageBackend{
		buckets: make(map[string]bool),
	}
}

func (m *MockStorageBackend) CreateBucket(ctx context.Context, name string) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.buckets[name] = true
	return nil
}

func (m *MockStorageBackend) DeleteBucket(ctx context.Context, name string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.buckets, name)
	return nil
}

func TestNewService(t *testing.T) {
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()

	service := NewService(store, storage)

	require.NotNil(t, service)
	assert.Equal(t, store, service.store)
	assert.Equal(t, storage, service.storage)
}

func TestValidateBucketName(t *testing.T) {
	tests := []struct {
		name        string
		bucketName  string
		expectError bool
		errorMsg    string
	}{
		{"valid simple name", "my-bucket", false, ""},
		{"valid with numbers", "bucket123", false, ""},
		{"valid with dots", "my.bucket.name", false, ""},
		{"valid exact 3 chars", "abc", false, ""},
		{"valid exact 63 chars", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0", false, ""},
		{"too short", "ab", true, "between 3 and 63"},
		{"too long", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz01", true, "between 3 and 63"},
		{"starts with dot", ".bucket", true, "cannot start or end with a period"},
		{"ends with dot", "bucket.", true, "cannot start or end with a period"},
		{"starts with hyphen", "-bucket", true, "cannot start or end with a hyphen"},
		{"ends with hyphen", "bucket-", true, "cannot start or end with a hyphen"},
		{"uppercase letters", "MyBucket", true, "lowercase letters"},
		{"IP address format", "192.168.1.1", true, "cannot be formatted as an IP address"},
		{"underscore not allowed", "my_bucket", true, "lowercase letters"},
		{"space not allowed", "my bucket", true, "lowercase letters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBucketName(tt.bucketName)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateBucketTags(t *testing.T) {
	tests := []struct {
		name        string
		tags        map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid tags",
			tags:        map[string]string{"env": "prod", "team": "platform"},
			expectError: false,
		},
		{
			name:        "empty tags",
			tags:        map[string]string{},
			expectError: false,
		},
		{
			name:        "empty key",
			tags:        map[string]string{"": "value"},
			expectError: true,
			errorMsg:    "tag key cannot be empty",
		},
		{
			name:        "aws prefix key",
			tags:        map[string]string{"aws:internal": "value"},
			expectError: true,
			errorMsg:    "reserved 'aws:' prefix",
		},
		{
			name:        "AWS uppercase prefix key",
			tags:        map[string]string{"AWS:internal": "value"},
			expectError: true,
			errorMsg:    "reserved 'aws:' prefix",
		},
		{
			name: "too many tags",
			tags: func() map[string]string {
				tags := make(map[string]string)
				for i := 0; i <= MaxTagsPerBucket; i++ {
					tags[string(rune('a'+i%26))+string(rune(i))] = "value"
				}
				return tags
			}(),
			expectError: true,
			errorMsg:    "exceeds maximum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBucketTags(tt.tags)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateBucket(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		bucket, err := service.CreateBucket(ctx, "test-bucket", "owner1", "us-east-1", "STANDARD")

		require.NoError(t, err)
		require.NotNil(t, bucket)
		assert.Equal(t, "test-bucket", bucket.Name)
		assert.Equal(t, "owner1", bucket.Owner)
		assert.Equal(t, "us-east-1", bucket.Region)
		assert.Equal(t, "STANDARD", bucket.StorageClass)
		assert.False(t, bucket.CreatedAt.IsZero())
	})

	t.Run("default values", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		bucket, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")

		require.NoError(t, err)
		assert.Equal(t, "us-east-1", bucket.Region)
		assert.Equal(t, "STANDARD", bucket.StorageClass)
	})

	t.Run("invalid name", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		_, err := service.CreateBucket(ctx, "INVALID", "owner1", "", "")

		assert.Error(t, err)
	})

	t.Run("bucket already exists", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		// Create first bucket
		_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
		require.NoError(t, err)

		// Try to create again
		_, err = service.CreateBucket(ctx, "test-bucket", "owner2", "", "")
		assert.Error(t, err)
	})
}

func TestGetBucket(t *testing.T) {
	ctx := context.Background()

	t.Run("exists", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		// Create bucket first
		_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
		require.NoError(t, err)

		// Get bucket
		bucket, err := service.GetBucket(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, "test-bucket", bucket.Name)
	})

	t.Run("not exists", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		_, err := service.GetBucket(ctx, "nonexistent")
		assert.Error(t, err)
	})
}

func TestDeleteBucket(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		// Create bucket first
		_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
		require.NoError(t, err)

		// Delete bucket
		err = service.DeleteBucket(ctx, "test-bucket")
		require.NoError(t, err)

		// Verify deleted
		_, err = service.GetBucket(ctx, "test-bucket")
		assert.Error(t, err)
	})

	t.Run("not exists", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		err := service.DeleteBucket(ctx, "nonexistent")
		assert.Error(t, err)
	})

	t.Run("not empty", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		// Create bucket and add an object
		_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
		require.NoError(t, err)

		store.objects["test-bucket"]["key1"] = &metadata.ObjectMeta{Key: "key1"}

		err = service.DeleteBucket(ctx, "test-bucket")
		assert.Error(t, err)
		assert.True(t, s3errors.Is(err, s3errors.ErrBucketNotEmpty))
	})
}

func TestListBuckets(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create buckets
	_, err := service.CreateBucket(ctx, "bucket1", "owner1", "", "")
	require.NoError(t, err)
	_, err = service.CreateBucket(ctx, "bucket2", "owner1", "", "")
	require.NoError(t, err)
	_, err = service.CreateBucket(ctx, "bucket3", "owner2", "", "")
	require.NoError(t, err)

	// List all buckets
	buckets, err := service.ListBuckets(ctx, "")
	require.NoError(t, err)
	assert.Len(t, buckets, 3)

	// List owner1's buckets
	buckets, err = service.ListBuckets(ctx, "owner1")
	require.NoError(t, err)
	assert.Len(t, buckets, 2)
}

func TestHeadBucket(t *testing.T) {
	ctx := context.Background()

	t.Run("exists", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
		require.NoError(t, err)

		err = service.HeadBucket(ctx, "test-bucket")
		assert.NoError(t, err)
	})

	t.Run("not exists", func(t *testing.T) {
		store := NewMockMetadataStore()
		storage := NewMockStorageBackend()
		service := NewService(store, storage)

		err := service.HeadBucket(ctx, "nonexistent")
		assert.Error(t, err)
	})
}

func TestVersioning(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create bucket
	_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
	require.NoError(t, err)

	t.Run("get initial status", func(t *testing.T) {
		status, err := service.GetVersioning(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, metadata.VersioningStatus(""), status)
	})

	t.Run("enable versioning", func(t *testing.T) {
		err := service.SetVersioning(ctx, "test-bucket", metadata.VersioningEnabled)
		require.NoError(t, err)

		status, err := service.GetVersioning(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, metadata.VersioningEnabled, status)
	})

	t.Run("suspend versioning", func(t *testing.T) {
		err := service.SetVersioning(ctx, "test-bucket", metadata.VersioningSuspended)
		require.NoError(t, err)

		status, err := service.GetVersioning(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, metadata.VersioningSuspended, status)
	})

	t.Run("nonexistent bucket", func(t *testing.T) {
		_, err := service.GetVersioning(ctx, "nonexistent")
		assert.Error(t, err)
	})
}

func TestBucketTags(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create bucket
	_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
	require.NoError(t, err)

	t.Run("get empty tags", func(t *testing.T) {
		tags, err := service.GetBucketTagging(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Empty(t, tags)
	})

	t.Run("set tags", func(t *testing.T) {
		tags := map[string]string{"env": "prod", "team": "platform"}
		err := service.PutBucketTagging(ctx, "test-bucket", tags)
		require.NoError(t, err)

		result, err := service.GetBucketTagging(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, tags, result)
	})

	t.Run("delete tags", func(t *testing.T) {
		err := service.DeleteBucketTagging(ctx, "test-bucket")
		require.NoError(t, err)

		tags, err := service.GetBucketTagging(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Empty(t, tags)
	})
}

func TestCORS(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create bucket
	_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
	require.NoError(t, err)

	t.Run("no CORS", func(t *testing.T) {
		_, err := service.GetCORS(ctx, "test-bucket")
		assert.Error(t, err)
	})

	t.Run("set CORS", func(t *testing.T) {
		rules := []metadata.CORSRule{
			{
				AllowedOrigins: []string{"*"},
				AllowedMethods: []string{"GET", "PUT"},
				AllowedHeaders: []string{"*"},
				MaxAgeSeconds:  3600,
			},
		}
		err := service.SetCORS(ctx, "test-bucket", rules)
		require.NoError(t, err)

		result, err := service.GetCORS(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, rules[0].AllowedOrigins, result[0].AllowedOrigins)
	})

	t.Run("delete CORS", func(t *testing.T) {
		err := service.DeleteCORS(ctx, "test-bucket")
		require.NoError(t, err)

		_, err = service.GetCORS(ctx, "test-bucket")
		assert.Error(t, err)
	})
}

func TestMatchCORSOrigin(t *testing.T) {
	tests := []struct {
		name           string
		allowedOrigins []string
		origin         string
		expectMatch    bool
		expectedReturn string
	}{
		{"wildcard", []string{"*"}, "https://example.com", true, "*"},
		{"exact match", []string{"https://example.com"}, "https://example.com", true, "https://example.com"},
		{"no match", []string{"https://other.com"}, "https://example.com", false, ""},
		{"wildcard subdomain", []string{"*.example.com"}, "https://sub.example.com", true, "https://sub.example.com"},
		{"wildcard subdomain no match", []string{"*.example.com"}, "https://other.com", false, ""},
		{"multiple origins", []string{"https://one.com", "https://two.com"}, "https://two.com", true, "https://two.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, matched := MatchCORSOrigin(tt.allowedOrigins, tt.origin)
			assert.Equal(t, tt.expectMatch, matched)
			if matched {
				assert.Equal(t, tt.expectedReturn, result)
			}
		})
	}
}

func TestLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create bucket
	_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
	require.NoError(t, err)

	t.Run("no lifecycle", func(t *testing.T) {
		_, err := service.GetLifecycle(ctx, "test-bucket")
		assert.Error(t, err)
	})

	t.Run("set lifecycle", func(t *testing.T) {
		rules := []metadata.LifecycleRule{
			{
				ID:             "expire-old",
				Enabled:        true,
				ExpirationDays: 30,
			},
		}
		err := service.SetLifecycle(ctx, "test-bucket", rules)
		require.NoError(t, err)

		result, err := service.GetLifecycle(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "expire-old", result[0].ID)
	})

	t.Run("delete lifecycle", func(t *testing.T) {
		err := service.DeleteLifecycle(ctx, "test-bucket")
		require.NoError(t, err)

		_, err = service.GetLifecycle(ctx, "test-bucket")
		assert.Error(t, err)
	})
}

func TestEncryption(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create bucket
	_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
	require.NoError(t, err)

	t.Run("no encryption", func(t *testing.T) {
		_, err := service.GetEncryption(ctx, "test-bucket")
		assert.Error(t, err)
	})

	t.Run("set encryption", func(t *testing.T) {
		config := &metadata.EncryptionConfig{
			SSEAlgorithm: "AES256",
		}
		err := service.SetEncryption(ctx, "test-bucket", config)
		require.NoError(t, err)

		result, err := service.GetEncryption(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, "AES256", result.SSEAlgorithm)
	})

	t.Run("delete encryption", func(t *testing.T) {
		err := service.DeleteEncryption(ctx, "test-bucket")
		require.NoError(t, err)

		_, err = service.GetEncryption(ctx, "test-bucket")
		assert.Error(t, err)
	})
}

func TestBucketACL(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create bucket
	_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
	require.NoError(t, err)

	t.Run("get default ACL", func(t *testing.T) {
		acl, err := service.GetBucketACL(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, "owner1", acl.OwnerID)
		assert.Len(t, acl.Grants, 1)
		assert.Equal(t, "FULL_CONTROL", acl.Grants[0].Permission)
	})

	t.Run("set ACL", func(t *testing.T) {
		acl := &metadata.BucketACL{
			OwnerID: "owner1",
			Grants: []metadata.ACLGrant{
				{
					GranteeType: "CanonicalUser",
					GranteeID:   "owner1",
					Permission:  "FULL_CONTROL",
				},
				{
					GranteeType: "Group",
					GranteeID:   "AllUsers",
					Permission:  "READ",
				},
			},
		}
		err := service.SetBucketACL(ctx, "test-bucket", acl)
		require.NoError(t, err)

		result, err := service.GetBucketACL(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Len(t, result.Grants, 2)
	})
}

func TestGetLocation(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create bucket
	_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "eu-west-1", "")
	require.NoError(t, err)

	location, err := service.GetLocation(ctx, "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, "eu-west-1", location)
}

func TestAccelerate(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create bucket
	_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
	require.NoError(t, err)

	t.Run("get default status", func(t *testing.T) {
		status, err := service.GetAccelerate(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Empty(t, status)
	})

	t.Run("enable accelerate", func(t *testing.T) {
		err := service.SetAccelerate(ctx, "test-bucket", "Enabled")
		require.NoError(t, err)

		status, err := service.GetAccelerate(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, "Enabled", status)
	})

	t.Run("suspend accelerate", func(t *testing.T) {
		err := service.SetAccelerate(ctx, "test-bucket", "Suspended")
		require.NoError(t, err)

		status, err := service.GetAccelerate(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, "Suspended", status)
	})

	t.Run("invalid status", func(t *testing.T) {
		err := service.SetAccelerate(ctx, "test-bucket", "Invalid")
		assert.Error(t, err)
	})
}

func TestReplication(t *testing.T) {
	ctx := context.Background()
	store := NewMockMetadataStore()
	storage := NewMockStorageBackend()
	service := NewService(store, storage)

	// Create bucket with versioning enabled
	_, err := service.CreateBucket(ctx, "test-bucket", "owner1", "", "")
	require.NoError(t, err)
	err = service.SetVersioning(ctx, "test-bucket", metadata.VersioningEnabled)
	require.NoError(t, err)

	t.Run("no replication", func(t *testing.T) {
		_, err := service.GetReplication(ctx, "test-bucket")
		assert.Error(t, err)
	})

	t.Run("set replication", func(t *testing.T) {
		config := &metadata.ReplicationConfig{
			Role: "arn:aws:iam::123456789012:role/replication",
		}
		err := service.SetReplication(ctx, "test-bucket", config)
		require.NoError(t, err)

		result, err := service.GetReplication(ctx, "test-bucket")
		require.NoError(t, err)
		assert.Equal(t, config.Role, result.Role)
	})

	t.Run("delete replication", func(t *testing.T) {
		err := service.DeleteReplication(ctx, "test-bucket")
		require.NoError(t, err)

		_, err = service.GetReplication(ctx, "test-bucket")
		assert.Error(t, err)
	})
}
