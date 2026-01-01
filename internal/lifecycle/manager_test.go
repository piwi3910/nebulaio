package lifecycle

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/audit"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockMetadataStore implements metadata.Store interface for lifecycle testing.
type MockMetadataStore struct {
	listBucketsErr error
	buckets        map[string]*metadata.Bucket
	objects        map[string][]*metadata.ObjectMeta
	versions       map[string]*metadata.VersionListing
	mu             sync.RWMutex
	isLeader       bool
}

func NewMockMetadataStore() *MockMetadataStore {
	return &MockMetadataStore{
		buckets:  make(map[string]*metadata.Bucket),
		objects:  make(map[string][]*metadata.ObjectMeta),
		versions: make(map[string]*metadata.VersionListing),
		isLeader: true,
	}
}

func (m *MockMetadataStore) IsLeader() bool {
	return m.isLeader
}

func (m *MockMetadataStore) ListBuckets(ctx context.Context, owner string) ([]*metadata.Bucket, error) {
	if m.listBucketsErr != nil {
		return nil, m.listBucketsErr
	}

	buckets := make([]*metadata.Bucket, 0, len(m.buckets))
	for _, b := range m.buckets {
		buckets = append(buckets, b)
	}

	return buckets, nil
}

func (m *MockMetadataStore) GetBucket(ctx context.Context, name string) (*metadata.Bucket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if b, ok := m.buckets[name]; ok {
		return b, nil
	}

	return nil, s3errors.ErrNoSuchBucket
}

func (m *MockMetadataStore) ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*metadata.ObjectListing, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	objs := m.objects[bucket]

	return &metadata.ObjectListing{
		Objects:     objs,
		IsTruncated: false,
	}, nil
}

func (m *MockMetadataStore) ListObjectVersions(ctx context.Context, bucket, prefix, delimiter, keyMarker, versionMarker string, maxKeys int) (*metadata.VersionListing, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if v, ok := m.versions[bucket]; ok {
		return v, nil
	}

	return &metadata.VersionListing{
		Versions:      []*metadata.ObjectMeta{},
		DeleteMarkers: []*metadata.ObjectMeta{},
	}, nil
}

// Implement remaining interface methods as no-ops.
func (m *MockMetadataStore) CreateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	return nil
}
func (m *MockMetadataStore) UpdateBucket(ctx context.Context, bucket *metadata.Bucket) error {
	return nil
}
func (m *MockMetadataStore) DeleteBucket(ctx context.Context, name string) error { return nil }
func (m *MockMetadataStore) GetObject(ctx context.Context, bucket, key string) (*metadata.ObjectMeta, error) {
	//nolint:nilnil // mock returns nil,nil for not-found case
	return nil, nil
}
func (m *MockMetadataStore) PutObject(ctx context.Context, bucket string, obj *metadata.ObjectMeta) error {
	return nil
}
func (m *MockMetadataStore) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (m *MockMetadataStore) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (*metadata.ObjectMeta, error) {
	//nolint:nilnil // mock returns nil,nil for not-found case
	return nil, nil
}
func (m *MockMetadataStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	return nil
}
func (m *MockMetadataStore) CreateMultipartUpload(ctx context.Context, upload *metadata.MultipartUpload) error {
	return nil
}
func (m *MockMetadataStore) GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*metadata.MultipartUpload, error) {
	//nolint:nilnil // mock returns nil,nil for not-found case
	return nil, nil
}
func (m *MockMetadataStore) DeleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (m *MockMetadataStore) ListMultipartUploads(ctx context.Context, bucket string) ([]*metadata.MultipartUpload, error) {
	return nil, nil
}
func (m *MockMetadataStore) AddUploadPart(ctx context.Context, bucket, key, uploadID string, part *metadata.UploadPart) error {
	return nil
}
func (m *MockMetadataStore) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (m *MockMetadataStore) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (m *MockMetadataStore) AddNode(ctx context.Context, node *metadata.NodeInfo) error {
	return nil
}
func (m *MockMetadataStore) GetParts(ctx context.Context, bucket, key, uploadID string) ([]*metadata.UploadPart, error) {
	return nil, nil
}
func (m *MockMetadataStore) GetClusterInfo(ctx context.Context) (*metadata.ClusterInfo, error) {
	//nolint:nilnil // mock returns nil,nil for not-found case
	return nil, nil
}
func (m *MockMetadataStore) LeaderAddress(ctx context.Context) (string, error) { return "", nil }
func (m *MockMetadataStore) PutObjectMeta(ctx context.Context, meta *metadata.ObjectMeta) error {
	return nil
}
func (m *MockMetadataStore) GetObjectMeta(ctx context.Context, bucket, key string) (*metadata.ObjectMeta, error) {
	//nolint:nilnil // mock returns nil,nil for not-found case
	return nil, nil
}
func (m *MockMetadataStore) DeleteObjectMeta(ctx context.Context, bucket, key string) error {
	return nil
}
func (m *MockMetadataStore) PutObjectMetaVersioned(ctx context.Context, meta *metadata.ObjectMeta, preserveOldVersions bool) error {
	return nil
}
func (m *MockMetadataStore) CreateUser(ctx context.Context, user *metadata.User) error { return nil }
func (m *MockMetadataStore) GetUser(ctx context.Context, id string) (*metadata.User, error) {
	//nolint:nilnil // mock returns nil,nil for not-found case
	return nil, nil
}
func (m *MockMetadataStore) GetUserByUsername(ctx context.Context, username string) (*metadata.User, error) {
	//nolint:nilnil // mock returns nil,nil for not-found case
	return nil, nil
}
func (m *MockMetadataStore) UpdateUser(ctx context.Context, user *metadata.User) error { return nil }
func (m *MockMetadataStore) DeleteUser(ctx context.Context, id string) error           { return nil }
func (m *MockMetadataStore) ListUsers(ctx context.Context) ([]*metadata.User, error)   { return nil, nil }
func (m *MockMetadataStore) CreateAccessKey(ctx context.Context, key *metadata.AccessKey) error {
	return nil
}
func (m *MockMetadataStore) GetAccessKey(ctx context.Context, accessKeyID string) (*metadata.AccessKey, error) {
	//nolint:nilnil // mock returns nil,nil for not-found case
	return nil, nil
}
func (m *MockMetadataStore) DeleteAccessKey(ctx context.Context, accessKeyID string) error {
	return nil
}
func (m *MockMetadataStore) ListAccessKeys(ctx context.Context, userID string) ([]*metadata.AccessKey, error) {
	return nil, nil
}
func (m *MockMetadataStore) CreatePolicy(ctx context.Context, policy *metadata.Policy) error {
	return nil
}
func (m *MockMetadataStore) GetPolicy(ctx context.Context, name string) (*metadata.Policy, error) {
	//nolint:nilnil // mock returns nil,nil for not-found case
	return nil, nil
}
func (m *MockMetadataStore) UpdatePolicy(ctx context.Context, policy *metadata.Policy) error {
	return nil
}
func (m *MockMetadataStore) DeletePolicy(ctx context.Context, name string) error { return nil }
func (m *MockMetadataStore) ListPolicies(ctx context.Context) ([]*metadata.Policy, error) {
	return nil, nil
}
func (m *MockMetadataStore) RemoveNode(ctx context.Context, nodeID string) error { return nil }
func (m *MockMetadataStore) ListNodes(ctx context.Context) ([]*metadata.NodeInfo, error) {
	return nil, nil
}
func (m *MockMetadataStore) StoreAuditEvent(ctx context.Context, event *audit.AuditEvent) error {
	return nil
}
func (m *MockMetadataStore) ListAuditEvents(ctx context.Context, filter audit.AuditFilter) (*audit.AuditListResult, error) {
	//nolint:nilnil // mock returns nil,nil for empty result
	return nil, nil
}
func (m *MockMetadataStore) DeleteOldAuditEvents(ctx context.Context, before time.Time) (int, error) {
	return 0, nil
}
func (m *MockMetadataStore) Close() error { return nil }

// MockObjectService implements ObjectService interface.
type MockObjectService struct {
	deletedObjects  map[string][]string
	deletedVersions map[string]map[string][]string
	transitions     map[string]map[string]string
	mu              sync.Mutex
}

func NewMockObjectService() *MockObjectService {
	return &MockObjectService{
		deletedObjects:  make(map[string][]string),
		deletedVersions: make(map[string]map[string][]string),
		transitions:     make(map[string]map[string]string),
	}
}

func (m *MockObjectService) DeleteObject(ctx context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deletedObjects[bucket] = append(m.deletedObjects[bucket], key)

	return nil
}

func (m *MockObjectService) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.deletedVersions[bucket] == nil {
		m.deletedVersions[bucket] = make(map[string][]string)
	}

	m.deletedVersions[bucket][key] = append(m.deletedVersions[bucket][key], versionID)

	return nil
}

func (m *MockObjectService) TransitionStorageClass(ctx context.Context, bucket, key, targetClass string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.transitions[bucket] == nil {
		m.transitions[bucket] = make(map[string]string)
	}

	m.transitions[bucket][key] = targetClass

	return nil
}

// MockMultipartService implements MultipartService interface.
type MockMultipartService struct {
	abortedUploads map[string]bool
	uploads        []*metadata.MultipartUpload
}

func NewMockMultipartService() *MockMultipartService {
	return &MockMultipartService{
		abortedUploads: make(map[string]bool),
	}
}

func (m *MockMultipartService) ListMultipartUploads(ctx context.Context, bucket string) ([]*metadata.MultipartUpload, error) {
	return m.uploads, nil
}

func (m *MockMultipartService) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	m.abortedUploads[uploadID] = true
	return nil
}

func TestNewManager(t *testing.T) {
	store := NewMockMetadataStore()
	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()

	manager := NewManager(store, objectService, multipartService)

	require.NotNil(t, manager)
	assert.Equal(t, DefaultInterval, manager.interval)
}

func TestSetInterval(t *testing.T) {
	store := NewMockMetadataStore()
	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()

	manager := NewManager(store, objectService, multipartService)
	newInterval := 30 * time.Minute

	manager.SetInterval(newInterval)

	assert.Equal(t, newInterval, manager.interval)
}

func TestStartStop(t *testing.T) {
	store := NewMockMetadataStore()
	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()

	manager := NewManager(store, objectService, multipartService)
	manager.SetInterval(100 * time.Millisecond)

	ctx := context.Background()
	manager.Start(ctx)

	// Verify running
	manager.mu.RLock()
	running := manager.running
	manager.mu.RUnlock()
	assert.True(t, running)

	// Wait a bit to allow a cycle to run
	time.Sleep(150 * time.Millisecond)

	// Stop
	manager.Stop()

	manager.mu.RLock()
	running = manager.running
	manager.mu.RUnlock()
	assert.False(t, running)
}

func TestStartIdempotent(t *testing.T) {
	store := NewMockMetadataStore()
	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()

	manager := NewManager(store, objectService, multipartService)
	manager.SetInterval(1 * time.Hour) // Long interval to prevent actual runs

	ctx := context.Background()

	// Start twice
	manager.Start(ctx)
	manager.Start(ctx)

	manager.mu.RLock()
	running := manager.running
	manager.mu.RUnlock()
	assert.True(t, running)

	manager.Stop()
}

func TestStopIdempotent(t *testing.T) {
	store := NewMockMetadataStore()
	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()

	manager := NewManager(store, objectService, multipartService)

	// Stop without starting - should not panic
	manager.Stop()
	manager.Stop()
}

func TestProcessBucketNoLifecycle(t *testing.T) {
	store := NewMockMetadataStore()
	store.buckets["test-bucket"] = &metadata.Bucket{
		Name:      "test-bucket",
		Lifecycle: nil, // No lifecycle rules
	}

	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()
	manager := NewManager(store, objectService, multipartService)

	err := manager.ProcessBucket(context.Background(), "test-bucket")
	require.NoError(t, err)

	// No objects should be deleted
	assert.Empty(t, objectService.deletedObjects)
}

func TestProcessBucketWithExpiration(t *testing.T) {
	store := NewMockMetadataStore()
	store.buckets["test-bucket"] = &metadata.Bucket{
		Name: "test-bucket",
		Lifecycle: []metadata.LifecycleRule{
			{
				ID:             "expire-old",
				Enabled:        true,
				Prefix:         "",
				ExpirationDays: 30,
			},
		},
	}

	// Add an expired object
	store.objects["test-bucket"] = []*metadata.ObjectMeta{
		{
			Key:       "old-file.txt",
			CreatedAt: time.Now().Add(-60 * 24 * time.Hour), // 60 days old
		},
		{
			Key:       "new-file.txt",
			CreatedAt: time.Now().Add(-10 * 24 * time.Hour), // 10 days old
		},
	}

	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()
	manager := NewManager(store, objectService, multipartService)

	err := manager.ProcessBucket(context.Background(), "test-bucket")
	require.NoError(t, err)

	// Only the old object should be deleted
	assert.Contains(t, objectService.deletedObjects["test-bucket"], "old-file.txt")
	assert.NotContains(t, objectService.deletedObjects["test-bucket"], "new-file.txt")
}

func TestEvaluateObject(t *testing.T) {
	store := NewMockMetadataStore()
	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()
	manager := NewManager(store, objectService, multipartService)

	now := time.Now()

	tests := []struct {
		obj            *metadata.ObjectMeta
		name           string
		rules          []LifecycleRule
		expectedAction Action
	}{
		{
			name: "no matching rule",
			rules: []LifecycleRule{
				{
					ID:     "rule1",
					Status: "Enabled",
					Filter: Filter{Prefix: "logs/"},
					Expiration: &Expiration{
						Days: 30,
					},
				},
			},
			obj: &metadata.ObjectMeta{
				Key:       "data/file.txt",
				CreatedAt: now.Add(-60 * 24 * time.Hour),
			},
			expectedAction: ActionNone,
		},
		{
			name: "matching expiration rule",
			rules: []LifecycleRule{
				{
					ID:     "rule1",
					Status: "Enabled",
					Filter: Filter{Prefix: "logs/"},
					Expiration: &Expiration{
						Days: 30,
					},
				},
			},
			obj: &metadata.ObjectMeta{
				Key:       "logs/app.log",
				CreatedAt: now.Add(-60 * 24 * time.Hour),
			},
			expectedAction: ActionDelete,
		},
		{
			name: "disabled rule",
			rules: []LifecycleRule{
				{
					ID:     "rule1",
					Status: "Disabled",
					Filter: Filter{Prefix: ""},
					Expiration: &Expiration{
						Days: 30,
					},
				},
			},
			obj: &metadata.ObjectMeta{
				Key:       "file.txt",
				CreatedAt: now.Add(-60 * 24 * time.Hour),
			},
			expectedAction: ActionNone,
		},
		{
			name: "transition rule",
			rules: []LifecycleRule{
				{
					ID:     "rule1",
					Status: "Enabled",
					Filter: Filter{},
					Transition: []Transition{
						{
							Days:         30,
							StorageClass: "STANDARD_IA",
						},
					},
				},
			},
			obj: &metadata.ObjectMeta{
				Key:          "file.txt",
				CreatedAt:    now.Add(-60 * 24 * time.Hour),
				StorageClass: "STANDARD",
			},
			expectedAction: ActionTransition,
		},
		{
			name: "already transitioned",
			rules: []LifecycleRule{
				{
					ID:     "rule1",
					Status: "Enabled",
					Filter: Filter{},
					Transition: []Transition{
						{
							Days:         30,
							StorageClass: "STANDARD_IA",
						},
					},
				},
			},
			obj: &metadata.ObjectMeta{
				Key:          "file.txt",
				CreatedAt:    now.Add(-60 * 24 * time.Hour),
				StorageClass: "STANDARD_IA",
			},
			expectedAction: ActionNone,
		},
		{
			name: "not old enough",
			rules: []LifecycleRule{
				{
					ID:     "rule1",
					Status: "Enabled",
					Filter: Filter{},
					Expiration: &Expiration{
						Days: 30,
					},
				},
			},
			obj: &metadata.ObjectMeta{
				Key:       "file.txt",
				CreatedAt: now.Add(-10 * 24 * time.Hour),
			},
			expectedAction: ActionNone,
		},
		{
			name: "expiration by date",
			rules: []LifecycleRule{
				{
					ID:     "rule1",
					Status: "Enabled",
					Filter: Filter{},
					Expiration: &Expiration{
						Date: now.Add(-24 * time.Hour), // Yesterday
					},
				},
			},
			obj: &metadata.ObjectMeta{
				Key:       "file.txt",
				CreatedAt: now.Add(-10 * 24 * time.Hour),
			},
			expectedAction: ActionDelete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.EvaluateObject(tt.rules, tt.obj)
			assert.Equal(t, tt.expectedAction, result.Action)
		})
	}
}

func TestLifecycleRuleMatching(t *testing.T) {
	tests := []struct {
		rule     LifecycleRule
		tags     map[string]string
		name     string
		key      string
		expected bool
	}{
		{
			name: "empty filter matches all",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				Filter: Filter{},
			},
			key:      "any/file.txt",
			expected: true,
		},
		{
			name: "prefix match",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				Filter: Filter{Prefix: "logs/"},
			},
			key:      "logs/app.log",
			expected: true,
		},
		{
			name: "prefix no match",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				Filter: Filter{Prefix: "logs/"},
			},
			key:      "data/file.txt",
			expected: false,
		},
		{
			name: "tag match",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				Filter: Filter{
					Tag: &Tag{Key: "env", Value: "dev"},
				},
			},
			key:      "file.txt",
			tags:     map[string]string{"env": "dev"},
			expected: true,
		},
		{
			name: "tag no match",
			rule: LifecycleRule{
				ID:     "rule1",
				Status: "Enabled",
				Filter: Filter{
					Tag: &Tag{Key: "env", Value: "dev"},
				},
			},
			key:      "file.txt",
			tags:     map[string]string{"env": "prod"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.rule.MatchesObject(tt.key, tt.tags)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsEnabled(t *testing.T) {
	enabledRule := LifecycleRule{Status: "Enabled"}
	disabledRule := LifecycleRule{Status: "Disabled"}
	emptyRule := LifecycleRule{}

	assert.True(t, enabledRule.IsEnabled())
	assert.False(t, disabledRule.IsEnabled())
	assert.False(t, emptyRule.IsEnabled())
}

func TestConvertMetadataRules(t *testing.T) {
	metaRules := []metadata.LifecycleRule{
		{
			ID:                              "rule1",
			Enabled:                         true,
			Prefix:                          "logs/",
			ExpirationDays:                  30,
			NoncurrentVersionExpirationDays: 7,
			Transitions: []metadata.LifecycleTransition{
				{
					Days:         15,
					StorageClass: "STANDARD_IA",
				},
			},
		},
		{
			ID:             "rule2",
			Enabled:        false,
			Prefix:         "",
			ExpirationDays: 90,
		},
	}

	rules := convertMetadataRules(metaRules)

	require.Len(t, rules, 2)

	// First rule
	assert.Equal(t, "rule1", rules[0].ID)
	assert.Equal(t, "Enabled", rules[0].Status)
	assert.Equal(t, "logs/", rules[0].Filter.Prefix)
	require.NotNil(t, rules[0].Expiration)
	assert.Equal(t, 30, rules[0].Expiration.Days)
	require.NotNil(t, rules[0].NoncurrentVersionExpiration)
	assert.Equal(t, 7, rules[0].NoncurrentVersionExpiration.NoncurrentDays)
	require.Len(t, rules[0].Transition, 1)
	assert.Equal(t, 15, rules[0].Transition[0].Days)
	assert.Equal(t, "STANDARD_IA", rules[0].Transition[0].StorageClass)

	// Second rule
	assert.Equal(t, "rule2", rules[1].ID)
	assert.Equal(t, "Disabled", rules[1].Status)
}

func TestProcessMultipartUploads(t *testing.T) {
	store := NewMockMetadataStore()
	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()

	// Add old multipart upload
	multipartService.uploads = []*metadata.MultipartUpload{
		{
			Key:       "large-file.zip",
			UploadID:  "upload-1",
			CreatedAt: time.Now().Add(-10 * 24 * time.Hour), // 10 days old
		},
		{
			Key:       "recent-file.zip",
			UploadID:  "upload-2",
			CreatedAt: time.Now().Add(-1 * 24 * time.Hour), // 1 day old
		},
	}

	manager := NewManager(store, objectService, multipartService)

	rules := []LifecycleRule{
		{
			ID:     "abort-old",
			Status: "Enabled",
			Filter: Filter{},
			AbortIncompleteMultipartUpload: &AbortIncompleteMultipartUpload{
				DaysAfterInitiation: 7,
			},
		},
	}

	err := manager.processMultipartUploads(context.Background(), "test-bucket", rules)
	require.NoError(t, err)

	// Only old upload should be aborted
	assert.True(t, multipartService.abortedUploads["upload-1"])
	assert.False(t, multipartService.abortedUploads["upload-2"])
}

func TestSkipCycleWhenNotLeader(t *testing.T) {
	store := NewMockMetadataStore()
	store.isLeader = false
	store.buckets["test-bucket"] = &metadata.Bucket{
		Name: "test-bucket",
		Lifecycle: []metadata.LifecycleRule{
			{
				ID:             "delete-all",
				Enabled:        true,
				ExpirationDays: 1,
			},
		},
	}
	store.objects["test-bucket"] = []*metadata.ObjectMeta{
		{
			Key:       "file.txt",
			CreatedAt: time.Now().Add(-30 * 24 * time.Hour),
		},
	}

	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()
	manager := NewManager(store, objectService, multipartService)

	// Run a cycle
	manager.runCycle(context.Background())

	// No objects should be deleted since we're not leader
	assert.Empty(t, objectService.deletedObjects)
}

func TestDefaultInterval(t *testing.T) {
	assert.Equal(t, time.Hour, DefaultInterval)
}

func TestActionTypes(t *testing.T) {
	// Verify action types are defined
	assert.Equal(t, ActionNone, Action(0))
	assert.Equal(t, ActionDelete, Action(1))
	assert.Equal(t, ActionTransition, Action(2))
	assert.Equal(t, ActionDeleteMarker, Action(3))
}

func TestSkipDeleteMarkers(t *testing.T) {
	store := NewMockMetadataStore()
	store.buckets["test-bucket"] = &metadata.Bucket{
		Name: "test-bucket",
		Lifecycle: []metadata.LifecycleRule{
			{
				ID:             "delete-all",
				Enabled:        true,
				ExpirationDays: 1,
			},
		},
	}
	store.objects["test-bucket"] = []*metadata.ObjectMeta{
		{
			Key:          "file.txt",
			CreatedAt:    time.Now().Add(-30 * 24 * time.Hour),
			DeleteMarker: true, // This is a delete marker
		},
	}

	objectService := NewMockObjectService()
	multipartService := NewMockMultipartService()
	manager := NewManager(store, objectService, multipartService)

	err := manager.ProcessBucket(context.Background(), "test-bucket")
	require.NoError(t, err)

	// Delete marker should be skipped in regular processing
	assert.Empty(t, objectService.deletedObjects["test-bucket"])
}
