// Package mocks provides mock implementations for testing NebulaIO components.
package mocks

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/audit"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/testutil"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
)

// MockMetadataStore implements metadata.Store interface for testing.
// It provides thread-safe in-memory storage with configurable error injection.
type MockMetadataStore struct {
	mu sync.RWMutex

	// Data storage
	buckets          map[string]*metadata.Bucket
	objects          map[string]map[string]*metadata.ObjectMeta
	versions         map[string]map[string][]*metadata.ObjectMeta
	multipartUploads map[string]*metadata.MultipartUpload
	uploadParts      map[string][]*metadata.UploadPart // uploadKey -> parts
	users            map[string]*metadata.User
	accessKeys       map[string]*metadata.AccessKey
	policies         map[string]*metadata.Policy
	nodes            map[string]*metadata.NodeInfo
	auditEvents      []*audit.AuditEvent

	// Cluster state
	isLeader      bool
	leaderAddress string
	clusterInfo   *metadata.ClusterInfo

	// Error injection
	createBucketErr          error
	updateBucketErr          error
	deleteBucketErr          error
	getBucketErr             error
	listBucketsErr           error
	putObjectMetaErr         error
	getObjectMetaErr         error
	deleteObjectMetaErr      error
	listObjectsErr           error
	getClusterInfoErr        error
	createMultipartUploadErr error
	getMultipartUploadErr    error
	listMultipartUploadsErr  error
	createUserErr            error
	getUserErr               error
	createAccessKeyErr       error
	getAccessKeyErr          error
	createPolicyErr          error
	getPolicyErr             error
	updateUserErr            error
	deleteUserErr            error
	deleteAccessKeyErr       error
	updatePolicyErr          error
	deletePolicyErr          error
	addUploadPartErr         error
	completeMultipartErr     error
	abortMultipartUploadErr  error
}

// NewMockMetadataStore creates a new MockMetadataStore with initialized maps.
func NewMockMetadataStore() *MockMetadataStore {
	return &MockMetadataStore{
		buckets:          make(map[string]*metadata.Bucket),
		objects:          make(map[string]map[string]*metadata.ObjectMeta),
		versions:         make(map[string]map[string][]*metadata.ObjectMeta),
		multipartUploads: make(map[string]*metadata.MultipartUpload),
		uploadParts:      make(map[string][]*metadata.UploadPart),
		users:            make(map[string]*metadata.User),
		accessKeys:       make(map[string]*metadata.AccessKey),
		policies:         make(map[string]*metadata.Policy),
		nodes:            make(map[string]*metadata.NodeInfo),
		auditEvents:      make([]*audit.AuditEvent, 0),
		isLeader:         true,
		leaderAddress:    "localhost:9003",
	}
}

// SetCreateBucketError sets the error to return on CreateBucket calls.
func (m *MockMetadataStore) SetCreateBucketError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createBucketErr = err
}

// SetUpdateBucketError sets the error to return on UpdateBucket calls.
func (m *MockMetadataStore) SetUpdateBucketError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateBucketErr = err
}

// SetDeleteBucketError sets the error to return on DeleteBucket calls.
func (m *MockMetadataStore) SetDeleteBucketError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteBucketErr = err
}

// SetGetBucketError sets the error to return on GetBucket calls.
func (m *MockMetadataStore) SetGetBucketError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getBucketErr = err
}

// SetListBucketsError sets the error to return on ListBuckets calls.
func (m *MockMetadataStore) SetListBucketsError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listBucketsErr = err
}

// SetPutObjectMetaError sets the error to return on PutObjectMeta calls.
func (m *MockMetadataStore) SetPutObjectMetaError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.putObjectMetaErr = err
}

// SetGetObjectMetaError sets the error to return on GetObjectMeta calls.
func (m *MockMetadataStore) SetGetObjectMetaError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getObjectMetaErr = err
}

// SetDeleteObjectMetaError sets the error to return on DeleteObjectMeta calls.
func (m *MockMetadataStore) SetDeleteObjectMetaError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteObjectMetaErr = err
}

// SetListObjectsError sets the error to return on ListObjects calls.
func (m *MockMetadataStore) SetListObjectsError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listObjectsErr = err
}

// SetGetClusterInfoError sets the error to return on GetClusterInfo calls.
func (m *MockMetadataStore) SetGetClusterInfoError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getClusterInfoErr = err
}

// SetCreateMultipartUploadError sets the error to return on CreateMultipartUpload calls.
func (m *MockMetadataStore) SetCreateMultipartUploadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createMultipartUploadErr = err
}

// SetGetMultipartUploadError sets the error to return on GetMultipartUpload calls.
func (m *MockMetadataStore) SetGetMultipartUploadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getMultipartUploadErr = err
}

// SetListMultipartUploadsError sets the error to return on ListMultipartUploads calls.
func (m *MockMetadataStore) SetListMultipartUploadsError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listMultipartUploadsErr = err
}

// SetCreateUserError sets the error to return on CreateUser calls.
func (m *MockMetadataStore) SetCreateUserError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createUserErr = err
}

// SetGetUserError sets the error to return on GetUser calls.
func (m *MockMetadataStore) SetGetUserError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getUserErr = err
}

// SetCreateAccessKeyError sets the error to return on CreateAccessKey calls.
func (m *MockMetadataStore) SetCreateAccessKeyError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createAccessKeyErr = err
}

// SetGetAccessKeyError sets the error to return on GetAccessKey calls.
func (m *MockMetadataStore) SetGetAccessKeyError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getAccessKeyErr = err
}

// SetCreatePolicyError sets the error to return on CreatePolicy calls.
func (m *MockMetadataStore) SetCreatePolicyError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createPolicyErr = err
}

// SetGetPolicyError sets the error to return on GetPolicy calls.
func (m *MockMetadataStore) SetGetPolicyError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getPolicyErr = err
}

// SetUpdateUserError sets the error to return on UpdateUser calls.
func (m *MockMetadataStore) SetUpdateUserError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateUserErr = err
}

// SetDeleteUserError sets the error to return on DeleteUser calls.
func (m *MockMetadataStore) SetDeleteUserError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteUserErr = err
}

// SetDeleteAccessKeyError sets the error to return on DeleteAccessKey calls.
func (m *MockMetadataStore) SetDeleteAccessKeyError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteAccessKeyErr = err
}

// SetUpdatePolicyError sets the error to return on UpdatePolicy calls.
func (m *MockMetadataStore) SetUpdatePolicyError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updatePolicyErr = err
}

// SetDeletePolicyError sets the error to return on DeletePolicy calls.
func (m *MockMetadataStore) SetDeletePolicyError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletePolicyErr = err
}

// SetAddUploadPartError sets the error to return on AddUploadPart calls.
func (m *MockMetadataStore) SetAddUploadPartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addUploadPartErr = err
}

// SetCompleteMultipartUploadError sets the error to return on CompleteMultipartUpload calls.
func (m *MockMetadataStore) SetCompleteMultipartUploadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completeMultipartErr = err
}

// SetAbortMultipartUploadError sets the error to return on AbortMultipartUpload calls.
func (m *MockMetadataStore) SetAbortMultipartUploadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.abortMultipartUploadErr = err
}

// SetIsLeader sets whether this mock is the leader.
func (m *MockMetadataStore) SetIsLeader(isLeader bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isLeader = isLeader
}

// SetLeaderAddress sets the leader address.
func (m *MockMetadataStore) SetLeaderAddress(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaderAddress = addr
}

// SetClusterInfo sets the cluster info to return.
func (m *MockMetadataStore) SetClusterInfo(info *metadata.ClusterInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusterInfo = info
}

// AddBucket adds a bucket directly to the mock store (for test setup).
func (m *MockMetadataStore) AddBucket(bucket *metadata.Bucket) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buckets[bucket.Name] = bucket
	if m.objects[bucket.Name] == nil {
		m.objects[bucket.Name] = make(map[string]*metadata.ObjectMeta)
	}
}

// AddObject adds an object directly to the mock store (for test setup).
func (m *MockMetadataStore) AddObject(bucket string, obj *metadata.ObjectMeta) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objects[bucket] == nil {
		m.objects[bucket] = make(map[string]*metadata.ObjectMeta)
	}
	m.objects[bucket][obj.Key] = obj
}

// GetBuckets returns a copy of the buckets map (for test assertions).
func (m *MockMetadataStore) GetBuckets() map[string]*metadata.Bucket {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*metadata.Bucket)
	for k, v := range m.buckets {
		result[k] = v
	}
	return result
}

// GetObjects returns objects for a bucket (for test assertions).
func (m *MockMetadataStore) GetObjects(bucket string) map[string]*metadata.ObjectMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*metadata.ObjectMeta)
	if objs, ok := m.objects[bucket]; ok {
		for k, v := range objs {
			result[k] = v
		}
	}
	return result
}

// Close implements metadata.Store interface.
func (m *MockMetadataStore) Close() error {
	return nil
}

// IsLeader implements metadata.Store interface.
func (m *MockMetadataStore) IsLeader() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isLeader
}

// LeaderAddress implements metadata.Store interface.
func (m *MockMetadataStore) LeaderAddress() (string, error) {
	if m == nil {
		return "", s3errors.ErrInternalError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.leaderAddress == "" {
		return "", s3errors.ErrInternalError
	}
	return m.leaderAddress, nil
}

// CreateBucket implements metadata.Store interface.
func (m *MockMetadataStore) CreateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createBucketErr != nil {
		return m.createBucketErr
	}
	if _, exists := m.buckets[bucket.Name]; exists {
		return s3errors.ErrBucketAlreadyExists
	}
	m.buckets[bucket.Name] = bucket
	m.objects[bucket.Name] = make(map[string]*metadata.ObjectMeta)
	return nil
}

// GetBucket implements metadata.Store interface.
func (m *MockMetadataStore) GetBucket(ctx context.Context, name string) (*metadata.Bucket, error) {
	if m == nil {
		return nil, s3errors.ErrInternalError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getBucketErr != nil {
		return nil, m.getBucketErr
	}
	if bucket, ok := m.buckets[name]; ok {
		return bucket, nil
	}
	return nil, s3errors.ErrNoSuchBucket
}

// DeleteBucket implements metadata.Store interface.
func (m *MockMetadataStore) DeleteBucket(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteBucketErr != nil {
		return m.deleteBucketErr
	}
	delete(m.buckets, name)
	delete(m.objects, name)
	return nil
}

// ListBuckets implements metadata.Store interface.
func (m *MockMetadataStore) ListBuckets(ctx context.Context, owner string) ([]*metadata.Bucket, error) {
	if m == nil {
		return nil, s3errors.ErrInternalError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.listBucketsErr != nil {
		return nil, m.listBucketsErr
	}
	buckets := make([]*metadata.Bucket, 0, len(m.buckets))
	for _, b := range m.buckets {
		if owner == "" || b.Owner == owner {
			buckets = append(buckets, b)
		}
	}
	return buckets, nil
}

// UpdateBucket implements metadata.Store interface.
func (m *MockMetadataStore) UpdateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateBucketErr != nil {
		return m.updateBucketErr
	}
	m.buckets[bucket.Name] = bucket
	return nil
}

// PutObjectMeta implements metadata.Store interface.
func (m *MockMetadataStore) PutObjectMeta(ctx context.Context, meta *metadata.ObjectMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putObjectMetaErr != nil {
		return m.putObjectMetaErr
	}
	if m.objects[meta.Bucket] == nil {
		m.objects[meta.Bucket] = make(map[string]*metadata.ObjectMeta)
	}
	m.objects[meta.Bucket][meta.Key] = meta
	return nil
}

// GetObjectMeta implements metadata.Store interface.
func (m *MockMetadataStore) GetObjectMeta(ctx context.Context, bucket, key string) (*metadata.ObjectMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getObjectMetaErr != nil {
		return nil, m.getObjectMetaErr
	}
	if objs, ok := m.objects[bucket]; ok {
		if obj, ok := objs[key]; ok {
			return obj, nil
		}
	}
	return nil, s3errors.ErrNoSuchKey
}

// DeleteObjectMeta implements metadata.Store interface.
func (m *MockMetadataStore) DeleteObjectMeta(ctx context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteObjectMetaErr != nil {
		return m.deleteObjectMetaErr
	}
	if objs, ok := m.objects[bucket]; ok {
		delete(objs, key)
	}
	return nil
}

// ListObjects implements metadata.Store interface.
func (m *MockMetadataStore) ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*metadata.ObjectListing, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.listObjectsErr != nil {
		return nil, m.listObjectsErr
	}
	objs := m.objects[bucket]
	listing := &metadata.ObjectListing{
		Objects: make([]*metadata.ObjectMeta, 0, len(objs)),
	}
	for _, obj := range objs {
		// Filter by prefix if specified
		if prefix != "" && !strings.HasPrefix(obj.Key, prefix) {
			continue
		}
		listing.Objects = append(listing.Objects, obj)
		if maxKeys > 0 && len(listing.Objects) >= maxKeys {
			listing.IsTruncated = true
			break
		}
	}
	return listing, nil
}

// GetObjectVersion implements metadata.Store interface.
// Returns the object version if found, or ErrNoSuchKey if not found.
func (m *MockMetadataStore) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (*metadata.ObjectMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if versions, ok := m.versions[bucket]; ok {
		if objVersions, ok := versions[key]; ok {
			for _, v := range objVersions {
				if v.VersionID == versionID {
					return v, nil
				}
			}
		}
	}
	return nil, s3errors.ErrNoSuchKey
}

// ListObjectVersions implements metadata.Store interface.
// Returns all versions for objects matching the prefix.
func (m *MockMetadataStore) ListObjectVersions(ctx context.Context, bucket, prefix, delimiter, keyMarker, versionIDMarker string, maxKeys int) (*metadata.VersionListing, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	listing := &metadata.VersionListing{
		Versions:      make([]*metadata.ObjectMeta, 0),
		DeleteMarkers: make([]*metadata.ObjectMeta, 0),
	}
	if versions, ok := m.versions[bucket]; ok {
		for key, objVersions := range versions {
			if prefix == "" || strings.HasPrefix(key, prefix) {
				listing.Versions = append(listing.Versions, objVersions...)
			}
		}
	}
	return listing, nil
}

// DeleteObjectVersion implements metadata.Store interface.
func (m *MockMetadataStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	return nil
}

// PutObjectMetaVersioned implements metadata.Store interface.
func (m *MockMetadataStore) PutObjectMetaVersioned(ctx context.Context, meta *metadata.ObjectMeta, preserveOldVersions bool) error {
	return nil
}

// CreateMultipartUpload implements metadata.Store interface.
func (m *MockMetadataStore) CreateMultipartUpload(ctx context.Context, upload *metadata.MultipartUpload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createMultipartUploadErr != nil {
		return m.createMultipartUploadErr
	}
	mpKey := testutil.MultipartUploadKey(upload.Bucket, upload.Key, upload.UploadID)
	m.multipartUploads[mpKey] = upload
	return nil
}

// GetMultipartUpload implements metadata.Store interface.
// Returns ErrNoSuchUpload if the upload is not found.
func (m *MockMetadataStore) GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*metadata.MultipartUpload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getMultipartUploadErr != nil {
		return nil, m.getMultipartUploadErr
	}
	mpKey := testutil.MultipartUploadKey(bucket, key, uploadID)
	if upload, ok := m.multipartUploads[mpKey]; ok {
		return upload, nil
	}
	return nil, s3errors.ErrNoSuchUpload
}

// AbortMultipartUpload implements metadata.Store interface.
func (m *MockMetadataStore) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.abortMultipartUploadErr != nil {
		return m.abortMultipartUploadErr
	}
	mpKey := testutil.MultipartUploadKey(bucket, key, uploadID)
	delete(m.multipartUploads, mpKey)
	return nil
}

// CompleteMultipartUpload implements metadata.Store interface.
func (m *MockMetadataStore) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.completeMultipartErr != nil {
		return m.completeMultipartErr
	}
	mpKey := testutil.MultipartUploadKey(bucket, key, uploadID)
	delete(m.multipartUploads, mpKey)
	return nil
}

// AddUploadPart implements metadata.Store interface.
func (m *MockMetadataStore) AddUploadPart(ctx context.Context, bucket, key, uploadID string, part *metadata.UploadPart) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.addUploadPartErr != nil {
		return m.addUploadPartErr
	}
	mpKey := testutil.MultipartUploadKey(bucket, key, uploadID)
	m.uploadParts[mpKey] = append(m.uploadParts[mpKey], part)
	return nil
}

// GetParts implements metadata.Store interface.
func (m *MockMetadataStore) GetParts(ctx context.Context, bucket, key, uploadID string) ([]*metadata.UploadPart, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mpKey := testutil.MultipartUploadKey(bucket, key, uploadID)
	if parts, ok := m.uploadParts[mpKey]; ok {
		return parts, nil
	}
	return nil, nil
}

// ListMultipartUploads implements metadata.Store interface.
func (m *MockMetadataStore) ListMultipartUploads(ctx context.Context, bucket string) ([]*metadata.MultipartUpload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.listMultipartUploadsErr != nil {
		return nil, m.listMultipartUploadsErr
	}
	uploads := make([]*metadata.MultipartUpload, 0, len(m.multipartUploads))
	for _, upload := range m.multipartUploads {
		if upload.Bucket == bucket {
			uploads = append(uploads, upload)
		}
	}
	return uploads, nil
}

// CreateUser implements metadata.Store interface.
func (m *MockMetadataStore) CreateUser(ctx context.Context, user *metadata.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createUserErr != nil {
		return m.createUserErr
	}
	m.users[user.ID] = user
	return nil
}

// GetUser implements metadata.Store interface.
func (m *MockMetadataStore) GetUser(ctx context.Context, id string) (*metadata.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getUserErr != nil {
		return nil, m.getUserErr
	}
	if user, ok := m.users[id]; ok {
		return user, nil
	}
	return nil, nil
}

// GetUserByUsername implements metadata.Store interface.
func (m *MockMetadataStore) GetUserByUsername(ctx context.Context, username string) (*metadata.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, user := range m.users {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, nil
}

// UpdateUser implements metadata.Store interface.
func (m *MockMetadataStore) UpdateUser(ctx context.Context, user *metadata.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateUserErr != nil {
		return m.updateUserErr
	}
	m.users[user.ID] = user
	return nil
}

// DeleteUser implements metadata.Store interface.
func (m *MockMetadataStore) DeleteUser(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteUserErr != nil {
		return m.deleteUserErr
	}
	delete(m.users, id)
	return nil
}

// ListUsers implements metadata.Store interface.
func (m *MockMetadataStore) ListUsers(ctx context.Context) ([]*metadata.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	users := make([]*metadata.User, 0, len(m.users))
	for _, user := range m.users {
		users = append(users, user)
	}
	return users, nil
}

// CreateAccessKey implements metadata.Store interface.
func (m *MockMetadataStore) CreateAccessKey(ctx context.Context, key *metadata.AccessKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createAccessKeyErr != nil {
		return m.createAccessKeyErr
	}
	m.accessKeys[key.AccessKeyID] = key
	return nil
}

// GetAccessKey implements metadata.Store interface.
func (m *MockMetadataStore) GetAccessKey(ctx context.Context, accessKeyID string) (*metadata.AccessKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getAccessKeyErr != nil {
		return nil, m.getAccessKeyErr
	}
	if key, ok := m.accessKeys[accessKeyID]; ok {
		return key, nil
	}
	return nil, nil
}

// DeleteAccessKey implements metadata.Store interface.
func (m *MockMetadataStore) DeleteAccessKey(ctx context.Context, accessKeyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteAccessKeyErr != nil {
		return m.deleteAccessKeyErr
	}
	delete(m.accessKeys, accessKeyID)
	return nil
}

// ListAccessKeys implements metadata.Store interface.
func (m *MockMetadataStore) ListAccessKeys(ctx context.Context, userID string) ([]*metadata.AccessKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]*metadata.AccessKey, 0, len(m.accessKeys))
	for _, key := range m.accessKeys {
		if key.UserID == userID {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// CreatePolicy implements metadata.Store interface.
func (m *MockMetadataStore) CreatePolicy(ctx context.Context, policy *metadata.Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createPolicyErr != nil {
		return m.createPolicyErr
	}
	m.policies[policy.Name] = policy
	return nil
}

// GetPolicy implements metadata.Store interface.
func (m *MockMetadataStore) GetPolicy(ctx context.Context, name string) (*metadata.Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getPolicyErr != nil {
		return nil, m.getPolicyErr
	}
	if policy, ok := m.policies[name]; ok {
		return policy, nil
	}
	return nil, nil
}

// UpdatePolicy implements metadata.Store interface.
func (m *MockMetadataStore) UpdatePolicy(ctx context.Context, policy *metadata.Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updatePolicyErr != nil {
		return m.updatePolicyErr
	}
	m.policies[policy.Name] = policy
	return nil
}

// DeletePolicy implements metadata.Store interface.
func (m *MockMetadataStore) DeletePolicy(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deletePolicyErr != nil {
		return m.deletePolicyErr
	}
	delete(m.policies, name)
	return nil
}

// ListPolicies implements metadata.Store interface.
func (m *MockMetadataStore) ListPolicies(ctx context.Context) ([]*metadata.Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	policies := make([]*metadata.Policy, 0, len(m.policies))
	for _, policy := range m.policies {
		policies = append(policies, policy)
	}
	return policies, nil
}

// GetClusterInfo implements metadata.Store interface.
func (m *MockMetadataStore) GetClusterInfo(ctx context.Context) (*metadata.ClusterInfo, error) {
	if m == nil {
		return nil, s3errors.ErrInternalError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getClusterInfoErr != nil {
		return nil, m.getClusterInfoErr
	}
	if m.clusterInfo != nil {
		return m.clusterInfo, nil
	}
	state := "Follower"
	if m.isLeader {
		state = "Leader"
	}
	return &metadata.ClusterInfo{
		RaftState:     state,
		LeaderAddress: m.leaderAddress,
	}, nil
}

// AddNode implements metadata.Store interface.
func (m *MockMetadataStore) AddNode(ctx context.Context, node *metadata.NodeInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[node.ID] = node
	return nil
}

// RemoveNode implements metadata.Store interface.
func (m *MockMetadataStore) RemoveNode(ctx context.Context, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodes, nodeID)
	return nil
}

// ListNodes implements metadata.Store interface.
func (m *MockMetadataStore) ListNodes(ctx context.Context) ([]*metadata.NodeInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	nodes := make([]*metadata.NodeInfo, 0, len(m.nodes))
	for _, node := range m.nodes {
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// StoreAuditEvent implements metadata.Store interface.
func (m *MockMetadataStore) StoreAuditEvent(ctx context.Context, event *audit.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditEvents = append(m.auditEvents, event)
	return nil
}

// ListAuditEvents implements metadata.Store interface.
func (m *MockMetadataStore) ListAuditEvents(ctx context.Context, filter audit.AuditFilter) (*audit.AuditListResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Convert []*audit.AuditEvent to []audit.AuditEvent
	events := make([]audit.AuditEvent, 0, len(m.auditEvents))
	for _, e := range m.auditEvents {
		if e != nil {
			events = append(events, *e)
		}
	}

	return &audit.AuditListResult{
		Events: events,
	}, nil
}

// DeleteOldAuditEvents implements metadata.Store interface.
func (m *MockMetadataStore) DeleteOldAuditEvents(ctx context.Context, before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	newEvents := make([]*audit.AuditEvent, 0)
	for _, event := range m.auditEvents {
		if event.Timestamp.Before(before) {
			count++
		} else {
			newEvents = append(newEvents, event)
		}
	}
	m.auditEvents = newEvents
	return count, nil
}
