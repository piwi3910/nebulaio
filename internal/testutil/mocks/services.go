package mocks

import (
	"context"
	"sync"

	"github.com/piwi3910/nebulaio/internal/metadata"
)

// MockObjectService implements object service operations for testing.
type MockObjectService struct {
	deleteObjectErr  error
	deleteVersionErr error
	transitionErr    error
	deletedObjects   []DeletedObjectRecord
	deletedVersions  []DeletedVersionRecord
	transitions      []TransitionRecord
	mu               sync.RWMutex
}

// DeletedObjectRecord records a deleted object.
type DeletedObjectRecord struct {
	Bucket string
	Key    string
}

// DeletedVersionRecord records a deleted object version.
type DeletedVersionRecord struct {
	Bucket    string
	Key       string
	VersionID string
}

// TransitionRecord records a storage class transition.
type TransitionRecord struct {
	Bucket       string
	Key          string
	StorageClass string
}

// NewMockObjectService creates a new MockObjectService.
func NewMockObjectService() *MockObjectService {
	return &MockObjectService{
		deletedObjects:  make([]DeletedObjectRecord, 0),
		deletedVersions: make([]DeletedVersionRecord, 0),
		transitions:     make([]TransitionRecord, 0),
	}
}

// SetDeleteObjectError sets the error to return on DeleteObject calls.
func (m *MockObjectService) SetDeleteObjectError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deleteObjectErr = err
}

// SetDeleteVersionError sets the error to return on DeleteObjectVersion calls.
func (m *MockObjectService) SetDeleteVersionError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deleteVersionErr = err
}

// SetTransitionError sets the error to return on TransitionStorageClass calls.
func (m *MockObjectService) SetTransitionError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.transitionErr = err
}

// DeleteObject records and optionally returns an error.
func (m *MockObjectService) DeleteObject(ctx context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.deleteObjectErr != nil {
		return m.deleteObjectErr
	}

	m.deletedObjects = append(m.deletedObjects, DeletedObjectRecord{
		Bucket: bucket,
		Key:    key,
	})

	return nil
}

// DeleteObjectVersion records and optionally returns an error.
func (m *MockObjectService) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.deleteVersionErr != nil {
		return m.deleteVersionErr
	}

	m.deletedVersions = append(m.deletedVersions, DeletedVersionRecord{
		Bucket:    bucket,
		Key:       key,
		VersionID: versionID,
	})

	return nil
}

// TransitionStorageClass records and optionally returns an error.
func (m *MockObjectService) TransitionStorageClass(ctx context.Context, bucket, key, storageClass string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.transitionErr != nil {
		return m.transitionErr
	}

	m.transitions = append(m.transitions, TransitionRecord{
		Bucket:       bucket,
		Key:          key,
		StorageClass: storageClass,
	})

	return nil
}

// GetDeletedObjects returns all deleted object records.
func (m *MockObjectService) GetDeletedObjects() []DeletedObjectRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]DeletedObjectRecord, len(m.deletedObjects))
	copy(result, m.deletedObjects)

	return result
}

// GetDeletedVersions returns all deleted version records.
func (m *MockObjectService) GetDeletedVersions() []DeletedVersionRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]DeletedVersionRecord, len(m.deletedVersions))
	copy(result, m.deletedVersions)

	return result
}

// GetTransitions returns all transition records.
func (m *MockObjectService) GetTransitions() []TransitionRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]TransitionRecord, len(m.transitions))
	copy(result, m.transitions)

	return result
}

// Reset clears all mock state for reuse between tests.
func (m *MockObjectService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deletedObjects = make([]DeletedObjectRecord, 0)
	m.deletedVersions = make([]DeletedVersionRecord, 0)
	m.transitions = make([]TransitionRecord, 0)
	m.deleteObjectErr = nil
	m.deleteVersionErr = nil
	m.transitionErr = nil
}

// MockMultipartService implements multipart service operations for testing.
type MockMultipartService struct {
	listUploadsErr   error
	abortErr         error
	uploads          map[string]*metadata.MultipartUpload
	abortedUploadIDs []string
	mu               sync.RWMutex
}

// NewMockMultipartService creates a new MockMultipartService.
func NewMockMultipartService() *MockMultipartService {
	return &MockMultipartService{
		uploads:          make(map[string]*metadata.MultipartUpload),
		abortedUploadIDs: make([]string, 0),
	}
}

// SetListUploadsError sets the error to return on ListMultipartUploads calls.
func (m *MockMultipartService) SetListUploadsError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.listUploadsErr = err
}

// SetAbortError sets the error to return on AbortMultipartUpload calls.
func (m *MockMultipartService) SetAbortError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.abortErr = err
}

// AddUpload adds an upload for testing.
func (m *MockMultipartService) AddUpload(upload *metadata.MultipartUpload) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.uploads[upload.UploadID] = upload
}

// ListMultipartUploads returns uploads for a bucket.
func (m *MockMultipartService) ListMultipartUploads(ctx context.Context, bucket string) ([]*metadata.MultipartUpload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.listUploadsErr != nil {
		return nil, m.listUploadsErr
	}

	result := make([]*metadata.MultipartUpload, 0, len(m.uploads))
	for _, upload := range m.uploads {
		if upload.Bucket == bucket {
			result = append(result, upload)
		}
	}

	return result, nil
}

// AbortMultipartUpload aborts an upload.
func (m *MockMultipartService) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.abortErr != nil {
		return m.abortErr
	}

	m.abortedUploadIDs = append(m.abortedUploadIDs, uploadID)
	delete(m.uploads, uploadID)

	return nil
}

// GetAbortedUploadIDs returns all aborted upload IDs.
func (m *MockMultipartService) GetAbortedUploadIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, len(m.abortedUploadIDs))
	copy(result, m.abortedUploadIDs)

	return result
}

// Reset clears all mock state for reuse between tests.
func (m *MockMultipartService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.uploads = make(map[string]*metadata.MultipartUpload)
	m.abortedUploadIDs = make([]string, 0)
	m.listUploadsErr = nil
	m.abortErr = nil
}

// MockEventTarget implements event target for testing.
type MockEventTarget struct {
	publishErr error
	publishFn  func(event any) error
	name       string
	published  int
	mu         sync.RWMutex
	healthy    bool
}

// NewMockEventTarget creates a new MockEventTarget.
func NewMockEventTarget(name string) *MockEventTarget {
	return &MockEventTarget{
		name:    name,
		healthy: true,
	}
}

// SetPublishError sets the error to return on Publish calls.
func (m *MockEventTarget) SetPublishError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.publishErr = err
}

// SetHealthy sets whether the target is healthy.
func (m *MockEventTarget) SetHealthy(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.healthy = healthy
}

// SetPublishFunc sets a custom publish function.
func (m *MockEventTarget) SetPublishFunc(fn func(event any) error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.publishFn = fn
}

// Publish publishes an event.
func (m *MockEventTarget) Publish(event any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishErr != nil {
		return m.publishErr
	}

	if m.publishFn != nil {
		err := m.publishFn(event)
		if err != nil {
			return err
		}
	}

	m.published++

	return nil
}

// IsHealthy returns whether the target is healthy.
func (m *MockEventTarget) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.healthy
}

// Close closes the target.
func (m *MockEventTarget) Close() error {
	return nil
}

// Name returns the target name.
func (m *MockEventTarget) Name() string {
	return m.name
}

// GetPublishedCount returns the number of published events.
func (m *MockEventTarget) GetPublishedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.published
}

// Reset clears all mock state for reuse between tests.
func (m *MockEventTarget) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.published = 0
	m.healthy = true
	m.publishFn = nil
	m.publishErr = nil
}
