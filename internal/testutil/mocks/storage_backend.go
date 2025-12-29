package mocks

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
)

// MockStorageBackend implements backend.Backend and backend.MultipartBackend for testing.
// It provides thread-safe in-memory storage with configurable error injection.
type MockStorageBackend struct {
	mu sync.RWMutex

	// Data storage
	buckets map[string]bool
	objects map[string]map[string][]byte // bucket -> key -> content
	parts   map[string]map[int][]byte    // uploadKey -> partNumber -> content

	// Storage info
	storageInfo *backend.StorageInfo

	// Error injection
	initErr                  error
	closeErr                 error
	createBucketErr          error
	deleteBucketErr          error
	bucketExistsErr          error
	putObjectErr             error
	getObjectErr             error
	deleteObjectErr          error
	objectExistsErr          error
	getStorageInfoErr        error
	createMultipartUploadErr error
	putPartErr               error
	getPartErr               error
	completePartsErr         error
	abortMultipartUploadErr  error
}

// NewMockStorageBackend creates a new MockStorageBackend with initialized maps.
func NewMockStorageBackend() *MockStorageBackend {
	return &MockStorageBackend{
		buckets: make(map[string]bool),
		objects: make(map[string]map[string][]byte),
		parts:   make(map[string]map[int][]byte),
		storageInfo: &backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  500000000,
		},
	}
}

// SetInitError sets the error to return on Init calls.
func (m *MockStorageBackend) SetInitError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initErr = err
}

// SetCloseError sets the error to return on Close calls.
func (m *MockStorageBackend) SetCloseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeErr = err
}

// SetCreateBucketError sets the error to return on CreateBucket calls.
func (m *MockStorageBackend) SetCreateBucketError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createBucketErr = err
}

// SetDeleteBucketError sets the error to return on DeleteBucket calls.
func (m *MockStorageBackend) SetDeleteBucketError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteBucketErr = err
}

// SetPutObjectError sets the error to return on PutObject calls.
func (m *MockStorageBackend) SetPutObjectError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.putObjectErr = err
}

// SetGetObjectError sets the error to return on GetObject calls.
func (m *MockStorageBackend) SetGetObjectError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getObjectErr = err
}

// SetDeleteObjectError sets the error to return on DeleteObject calls.
func (m *MockStorageBackend) SetDeleteObjectError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteObjectErr = err
}

// SetGetStorageInfoError sets the error to return on GetStorageInfo calls.
func (m *MockStorageBackend) SetGetStorageInfoError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getStorageInfoErr = err
}

// SetBucketExistsError sets the error to return on BucketExists calls.
func (m *MockStorageBackend) SetBucketExistsError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bucketExistsErr = err
}

// SetObjectExistsError sets the error to return on ObjectExists calls.
func (m *MockStorageBackend) SetObjectExistsError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objectExistsErr = err
}

// SetCreateMultipartUploadError sets the error to return on CreateMultipartUpload calls.
func (m *MockStorageBackend) SetCreateMultipartUploadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createMultipartUploadErr = err
}

// SetPutPartError sets the error to return on PutPart calls.
func (m *MockStorageBackend) SetPutPartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.putPartErr = err
}

// SetGetPartError sets the error to return on GetPart calls.
func (m *MockStorageBackend) SetGetPartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getPartErr = err
}

// SetCompletePartsError sets the error to return on CompleteParts calls.
func (m *MockStorageBackend) SetCompletePartsError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completePartsErr = err
}

// SetAbortMultipartUploadError sets the error to return on AbortMultipartUpload calls.
func (m *MockStorageBackend) SetAbortMultipartUploadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.abortMultipartUploadErr = err
}

// SetStorageInfo sets the storage info to return.
func (m *MockStorageBackend) SetStorageInfo(info *backend.StorageInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storageInfo = info
}

// AddBucket adds a bucket directly to the mock store (for test setup).
func (m *MockStorageBackend) AddBucket(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buckets[name] = true
	if m.objects[name] == nil {
		m.objects[name] = make(map[string][]byte)
	}
}

// AddObject adds an object directly to the mock store (for test setup).
func (m *MockStorageBackend) AddObject(bucket, key string, content []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objects[bucket] == nil {
		m.objects[bucket] = make(map[string][]byte)
	}
	m.objects[bucket][key] = content
}

// GetStoredObject returns the stored object content (for test assertions).
func (m *MockStorageBackend) GetStoredObject(bucket, key string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if objs, ok := m.objects[bucket]; ok {
		if content, ok := objs[key]; ok {
			return content, true
		}
	}
	return nil, false
}

// Init implements backend.Backend interface.
func (m *MockStorageBackend) Init(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initErr
}

// Close implements backend.Backend interface.
func (m *MockStorageBackend) Close() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closeErr
}

// PutObject implements backend.Backend interface.
func (m *MockStorageBackend) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*backend.PutResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putObjectErr != nil {
		return nil, m.putObjectErr
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if m.objects[bucket] == nil {
		m.objects[bucket] = make(map[string][]byte)
	}
	m.objects[bucket][key] = content
	return &backend.PutResult{
		ETag: "mock-etag",
		Size: int64(len(content)),
	}, nil
}

// GetObject implements backend.Backend interface.
func (m *MockStorageBackend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	if m == nil {
		return nil, s3errors.ErrInternalError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getObjectErr != nil {
		return nil, m.getObjectErr
	}
	if objs, ok := m.objects[bucket]; ok {
		if content, ok := objs[key]; ok {
			return io.NopCloser(bytes.NewReader(content)), nil
		}
	}
	return io.NopCloser(strings.NewReader("")), nil
}

// DeleteObject implements backend.Backend interface.
func (m *MockStorageBackend) DeleteObject(ctx context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteObjectErr != nil {
		return m.deleteObjectErr
	}
	if objs, ok := m.objects[bucket]; ok {
		delete(objs, key)
	}
	return nil
}

// ObjectExists implements backend.Backend interface.
func (m *MockStorageBackend) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.objectExistsErr != nil {
		return false, m.objectExistsErr
	}
	if objs, ok := m.objects[bucket]; ok {
		_, exists := objs[key]
		return exists, nil
	}
	return false, nil
}

// CreateBucket implements backend.Backend interface.
func (m *MockStorageBackend) CreateBucket(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createBucketErr != nil {
		return m.createBucketErr
	}
	m.buckets[name] = true
	if m.objects[name] == nil {
		m.objects[name] = make(map[string][]byte)
	}
	return nil
}

// DeleteBucket implements backend.Backend interface.
func (m *MockStorageBackend) DeleteBucket(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteBucketErr != nil {
		return m.deleteBucketErr
	}
	delete(m.buckets, name)
	delete(m.objects, name)
	return nil
}

// BucketExists implements backend.Backend interface.
func (m *MockStorageBackend) BucketExists(ctx context.Context, name string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.bucketExistsErr != nil {
		return false, m.bucketExistsErr
	}
	return m.buckets[name], nil
}

// GetStorageInfo implements backend.Backend interface.
func (m *MockStorageBackend) GetStorageInfo(ctx context.Context) (*backend.StorageInfo, error) {
	if m == nil {
		return nil, s3errors.ErrInternalError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getStorageInfoErr != nil {
		return nil, m.getStorageInfoErr
	}
	if m.storageInfo == nil {
		return &backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  500000000,
		}, nil
	}
	return m.storageInfo, nil
}

// ListBuckets implements backend.Backend interface.
func (m *MockStorageBackend) ListBuckets(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	buckets := make([]string, 0, len(m.buckets))
	for name := range m.buckets {
		buckets = append(buckets, name)
	}
	return buckets, nil
}

// CreateMultipartUpload implements backend.MultipartBackend interface.
func (m *MockStorageBackend) CreateMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createMultipartUploadErr != nil {
		return m.createMultipartUploadErr
	}
	uploadKey := bucket + "/" + key + "/" + uploadID
	m.parts[uploadKey] = make(map[int][]byte)
	return nil
}

// PutPart implements backend.MultipartBackend interface.
func (m *MockStorageBackend) PutPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*backend.PutResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putPartErr != nil {
		return nil, m.putPartErr
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	uploadKey := bucket + "/" + key + "/" + uploadID
	if m.parts[uploadKey] == nil {
		m.parts[uploadKey] = make(map[int][]byte)
	}
	m.parts[uploadKey][partNumber] = content
	return &backend.PutResult{
		ETag: "mock-part-etag",
		Size: int64(len(content)),
	}, nil
}

// GetPart implements backend.MultipartBackend interface.
func (m *MockStorageBackend) GetPart(ctx context.Context, bucket, key, uploadID string, partNumber int) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getPartErr != nil {
		return nil, m.getPartErr
	}
	uploadKey := bucket + "/" + key + "/" + uploadID
	if parts, ok := m.parts[uploadKey]; ok {
		if content, ok := parts[partNumber]; ok {
			return io.NopCloser(bytes.NewReader(content)), nil
		}
	}
	return nil, s3errors.ErrNoSuchKey
}

// CompleteParts implements backend.MultipartBackend interface.
func (m *MockStorageBackend) CompleteParts(ctx context.Context, bucket, key, uploadID string, partNumbers []int) (*backend.PutResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.completePartsErr != nil {
		return nil, m.completePartsErr
	}
	uploadKey := bucket + "/" + key + "/" + uploadID
	if parts, ok := m.parts[uploadKey]; ok {
		var combined []byte
		for _, partNum := range partNumbers {
			if content, ok := parts[partNum]; ok {
				combined = append(combined, content...)
			}
		}
		if m.objects[bucket] == nil {
			m.objects[bucket] = make(map[string][]byte)
		}
		m.objects[bucket][key] = combined
		delete(m.parts, uploadKey)
		return &backend.PutResult{
			ETag: "mock-complete-etag",
			Size: int64(len(combined)),
		}, nil
	}
	return &backend.PutResult{}, nil
}

// AbortMultipartUpload implements backend.MultipartBackend interface.
func (m *MockStorageBackend) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.abortMultipartUploadErr != nil {
		return m.abortMultipartUploadErr
	}
	uploadKey := bucket + "/" + key + "/" + uploadID
	delete(m.parts, uploadKey)
	return nil
}
