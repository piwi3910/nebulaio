// Package mocks provides mock implementations for testing NebulaIO components.
package mocks

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockMetadataStore_ErrorInjection verifies error injection works correctly.
func TestMockMetadataStore_ErrorInjection(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("injected error")

	tests := []struct {
		name      string
		setError  func(*MockMetadataStore)
		operation func(*MockMetadataStore) error
	}{
		{
			name: "CreateBucket",
			setError: func(m *MockMetadataStore) {
				m.SetCreateBucketError(expectedErr)
			},
			operation: func(m *MockMetadataStore) error {
				return m.CreateBucket(ctx, &metadata.Bucket{Name: "test"})
			},
		},
		{
			name: "GetBucket",
			setError: func(m *MockMetadataStore) {
				m.SetGetBucketError(expectedErr)
			},
			operation: func(m *MockMetadataStore) error {
				_, err := m.GetBucket(ctx, "test")
				return err
			},
		},
		{
			name: "DeleteBucket",
			setError: func(m *MockMetadataStore) {
				m.SetDeleteBucketError(expectedErr)
			},
			operation: func(m *MockMetadataStore) error {
				return m.DeleteBucket(ctx, "test")
			},
		},
		{
			name: "ListBuckets",
			setError: func(m *MockMetadataStore) {
				m.SetListBucketsError(expectedErr)
			},
			operation: func(m *MockMetadataStore) error {
				_, err := m.ListBuckets(ctx, "")
				return err
			},
		},
		{
			name: "CreateMultipartUpload",
			setError: func(m *MockMetadataStore) {
				m.SetCreateMultipartUploadError(expectedErr)
			},
			operation: func(m *MockMetadataStore) error {
				return m.CreateMultipartUpload(ctx, &metadata.MultipartUpload{
					Bucket: "test", Key: "key", UploadID: "123",
				})
			},
		},
		{
			name: "GetMultipartUpload",
			setError: func(m *MockMetadataStore) {
				m.SetGetMultipartUploadError(expectedErr)
			},
			operation: func(m *MockMetadataStore) error {
				_, err := m.GetMultipartUpload(ctx, "test", "key", "123")
				return err
			},
		},
		{
			name: "ListMultipartUploads",
			setError: func(m *MockMetadataStore) {
				m.SetListMultipartUploadsError(expectedErr)
			},
			operation: func(m *MockMetadataStore) error {
				_, err := m.ListMultipartUploads(ctx, "test")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockMetadataStore()
			tt.setError(store)

			err := tt.operation(store)
			assert.Equal(t, expectedErr, err)
		})
	}
}

// TestMockMetadataStore_ThreadSafety verifies concurrent access is safe.
func TestMockMetadataStore_ThreadSafety(t *testing.T) {
	store := NewMockMetadataStore()
	ctx := context.Background()
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent bucket operations
	for i := 0; i < iterations; i++ {
		wg.Add(3)
		go func(n int) {
			defer wg.Done()
			bucket := &metadata.Bucket{Name: "test-bucket", Owner: "owner"}
			_ = store.CreateBucket(ctx, bucket)
		}(i)
		go func(n int) {
			defer wg.Done()
			_, _ = store.GetBucket(ctx, "test-bucket")
		}(i)
		go func(n int) {
			defer wg.Done()
			_, _ = store.ListBuckets(ctx, "")
		}(i)
	}

	wg.Wait()
	// If we get here without deadlock or race condition, the test passes
}

// TestMockMetadataStore_HelperMethods verifies helper methods work correctly.
func TestMockMetadataStore_HelperMethods(t *testing.T) {
	store := NewMockMetadataStore()

	// Test AddBucket helper
	bucket := &metadata.Bucket{Name: "test-bucket", Owner: "owner"}
	store.AddBucket(bucket)

	buckets := store.GetBuckets()
	require.Len(t, buckets, 1)
	assert.Equal(t, "test-bucket", buckets["test-bucket"].Name)

	// Test AddObject helper
	obj := &metadata.ObjectMeta{Bucket: "test-bucket", Key: "test-key", Size: 100}
	store.AddObject("test-bucket", obj)

	objects := store.GetObjects("test-bucket")
	require.Len(t, objects, 1)
	assert.Equal(t, "test-key", objects["test-key"].Key)
}

// TestMockMetadataStore_NilReceiverHandling verifies nil receiver behavior.
func TestMockMetadataStore_NilReceiverHandling(t *testing.T) {
	var store *MockMetadataStore = nil

	// These should not panic
	assert.False(t, store.IsLeader())

	_, err := store.LeaderAddress()
	assert.Error(t, err)

	_, err = store.GetBucket(context.Background(), "test")
	assert.Error(t, err)

	_, err = store.ListBuckets(context.Background(), "")
	assert.Error(t, err)

	_, err = store.GetClusterInfo(context.Background())
	assert.Error(t, err)
}

// TestMockStorageBackend_ErrorInjection verifies error injection works correctly.
func TestMockStorageBackend_ErrorInjection(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("injected error")

	tests := []struct {
		name      string
		setError  func(*MockStorageBackend)
		operation func(*MockStorageBackend) error
	}{
		{
			name: "Init",
			setError: func(m *MockStorageBackend) {
				m.SetInitError(expectedErr)
			},
			operation: func(m *MockStorageBackend) error {
				return m.Init(ctx)
			},
		},
		{
			name: "CreateBucket",
			setError: func(m *MockStorageBackend) {
				m.SetCreateBucketError(expectedErr)
			},
			operation: func(m *MockStorageBackend) error {
				return m.CreateBucket(ctx, "test")
			},
		},
		{
			name: "DeleteBucket",
			setError: func(m *MockStorageBackend) {
				m.SetDeleteBucketError(expectedErr)
			},
			operation: func(m *MockStorageBackend) error {
				return m.DeleteBucket(ctx, "test")
			},
		},
		{
			name: "BucketExists",
			setError: func(m *MockStorageBackend) {
				m.SetBucketExistsError(expectedErr)
			},
			operation: func(m *MockStorageBackend) error {
				_, err := m.BucketExists(ctx, "test")
				return err
			},
		},
		{
			name: "ObjectExists",
			setError: func(m *MockStorageBackend) {
				m.SetObjectExistsError(expectedErr)
			},
			operation: func(m *MockStorageBackend) error {
				_, err := m.ObjectExists(ctx, "test", "key")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := NewMockStorageBackend()
			tt.setError(backend)

			err := tt.operation(backend)
			assert.Equal(t, expectedErr, err)
		})
	}
}

// TestMockStorageBackend_ThreadSafety verifies concurrent access is safe.
func TestMockStorageBackend_ThreadSafety(t *testing.T) {
	backend := NewMockStorageBackend()
	ctx := context.Background()
	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < iterations; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = backend.CreateBucket(ctx, "test-bucket")
		}()
		go func() {
			defer wg.Done()
			_, _ = backend.BucketExists(ctx, "test-bucket")
		}()
		go func() {
			defer wg.Done()
			_, _ = backend.ListBuckets(ctx)
		}()
	}

	wg.Wait()
}

// TestMockStorageBackend_HelperMethods verifies helper methods work correctly.
func TestMockStorageBackend_HelperMethods(t *testing.T) {
	backend := NewMockStorageBackend()

	// Test AddBucket helper
	backend.AddBucket("test-bucket")
	exists, err := backend.BucketExists(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.True(t, exists)

	// Test AddObject helper
	content := []byte("test content")
	backend.AddObject("test-bucket", "test-key", content)

	stored, ok := backend.GetStoredObject("test-bucket", "test-key")
	require.True(t, ok)
	assert.Equal(t, content, stored)
}

// TestMockObjectService_ErrorInjection verifies error injection works correctly.
func TestMockObjectService_ErrorInjection(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("injected error")

	t.Run("DeleteObject", func(t *testing.T) {
		svc := NewMockObjectService()
		svc.SetDeleteObjectError(expectedErr)
		err := svc.DeleteObject(ctx, "bucket", "key")
		assert.Equal(t, expectedErr, err)
	})

	t.Run("DeleteObjectVersion", func(t *testing.T) {
		svc := NewMockObjectService()
		svc.SetDeleteVersionError(expectedErr)
		err := svc.DeleteObjectVersion(ctx, "bucket", "key", "v1")
		assert.Equal(t, expectedErr, err)
	})

	t.Run("TransitionStorageClass", func(t *testing.T) {
		svc := NewMockObjectService()
		svc.SetTransitionError(expectedErr)
		err := svc.TransitionStorageClass(ctx, "bucket", "key", "GLACIER")
		assert.Equal(t, expectedErr, err)
	})
}

// TestMockObjectService_Tracking verifies operation tracking works correctly.
func TestMockObjectService_Tracking(t *testing.T) {
	ctx := context.Background()
	svc := NewMockObjectService()

	// Test delete tracking
	err := svc.DeleteObject(ctx, "bucket1", "key1")
	require.NoError(t, err)
	err = svc.DeleteObject(ctx, "bucket2", "key2")
	require.NoError(t, err)

	deleted := svc.GetDeletedObjects()
	require.Len(t, deleted, 2)
	assert.Equal(t, "bucket1", deleted[0].Bucket)
	assert.Equal(t, "key1", deleted[0].Key)

	// Test version delete tracking
	err = svc.DeleteObjectVersion(ctx, "bucket", "key", "v1")
	require.NoError(t, err)

	versions := svc.GetDeletedVersions()
	require.Len(t, versions, 1)
	assert.Equal(t, "v1", versions[0].VersionID)

	// Test transition tracking
	err = svc.TransitionStorageClass(ctx, "bucket", "key", "GLACIER")
	require.NoError(t, err)

	transitions := svc.GetTransitions()
	require.Len(t, transitions, 1)
	assert.Equal(t, "GLACIER", transitions[0].StorageClass)
}

// TestMockMultipartService_ErrorInjection verifies error injection works correctly.
func TestMockMultipartService_ErrorInjection(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("injected error")

	t.Run("ListUploads", func(t *testing.T) {
		svc := NewMockMultipartService()
		svc.SetListUploadsError(expectedErr)
		_, err := svc.ListMultipartUploads(ctx, "bucket")
		assert.Equal(t, expectedErr, err)
	})

	t.Run("AbortUpload", func(t *testing.T) {
		svc := NewMockMultipartService()
		svc.SetAbortError(expectedErr)
		err := svc.AbortMultipartUpload(ctx, "bucket", "key", "upload-id")
		assert.Equal(t, expectedErr, err)
	})
}

// TestMockMultipartService_Tracking verifies operation tracking works correctly.
func TestMockMultipartService_Tracking(t *testing.T) {
	ctx := context.Background()
	svc := NewMockMultipartService()

	// Add uploads
	svc.AddUpload(&metadata.MultipartUpload{
		Bucket:   "bucket",
		Key:      "key1",
		UploadID: "upload-1",
	})
	svc.AddUpload(&metadata.MultipartUpload{
		Bucket:   "bucket",
		Key:      "key2",
		UploadID: "upload-2",
	})

	// List uploads
	uploads, err := svc.ListMultipartUploads(ctx, "bucket")
	require.NoError(t, err)
	assert.Len(t, uploads, 2)

	// Abort upload
	err = svc.AbortMultipartUpload(ctx, "bucket", "key1", "upload-1")
	require.NoError(t, err)

	aborted := svc.GetAbortedUploadIDs()
	require.Len(t, aborted, 1)
	assert.Equal(t, "upload-1", aborted[0])

	// Verify upload was removed
	uploads, err = svc.ListMultipartUploads(ctx, "bucket")
	require.NoError(t, err)
	assert.Len(t, uploads, 1)
}

// TestMockEventTarget_ErrorInjection verifies error injection works correctly.
func TestMockEventTarget_ErrorInjection(t *testing.T) {
	expectedErr := errors.New("injected error")

	target := NewMockEventTarget("test-target")
	target.SetPublishError(expectedErr)

	err := target.Publish(map[string]string{"event": "test"})
	assert.Equal(t, expectedErr, err)
}

// TestMockEventTarget_Tracking verifies event publishing tracking works correctly.
func TestMockEventTarget_Tracking(t *testing.T) {
	target := NewMockEventTarget("test-target")

	// Verify initial state
	assert.Equal(t, "test-target", target.Name())
	assert.True(t, target.IsHealthy())
	assert.Equal(t, 0, target.PublishedCount())

	// Publish events
	err := target.Publish(map[string]string{"event": "1"})
	require.NoError(t, err)
	err = target.Publish(map[string]string{"event": "2"})
	require.NoError(t, err)

	assert.Equal(t, 2, target.PublishedCount())

	// Test health toggle
	target.SetHealthy(false)
	assert.False(t, target.IsHealthy())
}
