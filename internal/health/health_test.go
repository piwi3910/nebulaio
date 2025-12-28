package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/storage/backend"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockMetadataStore implements metadata.Store interface for testing
type MockMetadataStore struct {
	isLeader      bool
	leaderAddress string
	clusterInfo   *metadata.ClusterInfo
	clusterInfoErr error
	buckets       []*metadata.Bucket
	listBucketsErr error
}

func (m *MockMetadataStore) GetClusterInfo(ctx context.Context) (*metadata.ClusterInfo, error) {
	if m.clusterInfoErr != nil {
		return nil, m.clusterInfoErr
	}
	if m.clusterInfo == nil {
		return &metadata.ClusterInfo{
			RaftState:     "Follower",
			LeaderAddress: m.leaderAddress,
		}, nil
	}
	return m.clusterInfo, nil
}

func (m *MockMetadataStore) IsLeader() bool {
	return m.isLeader
}

func (m *MockMetadataStore) LeaderAddress() (string, bool) {
	return m.leaderAddress, m.leaderAddress != ""
}

func (m *MockMetadataStore) ListBuckets(ctx context.Context, owner string) ([]*metadata.Bucket, error) {
	if m.listBucketsErr != nil {
		return nil, m.listBucketsErr
	}
	return m.buckets, nil
}

// Implement remaining interface methods as no-ops
func (m *MockMetadataStore) GetBucket(ctx context.Context, name string) (*metadata.Bucket, error) { return nil, nil }
func (m *MockMetadataStore) CreateBucket(ctx context.Context, bucket *metadata.Bucket) error { return nil }
func (m *MockMetadataStore) UpdateBucket(ctx context.Context, bucket *metadata.Bucket) error { return nil }
func (m *MockMetadataStore) DeleteBucket(ctx context.Context, name string) error { return nil }
func (m *MockMetadataStore) GetObject(ctx context.Context, bucket, key string) (*metadata.ObjectMeta, error) { return nil, nil }
func (m *MockMetadataStore) PutObject(ctx context.Context, bucket string, obj *metadata.ObjectMeta) error { return nil }
func (m *MockMetadataStore) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (m *MockMetadataStore) ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*metadata.ObjectListing, error) { return nil, nil }
func (m *MockMetadataStore) ListObjectVersions(ctx context.Context, bucket, prefix, delimiter, keyMarker, versionMarker string, maxKeys int) (*metadata.VersionListing, error) { return nil, nil }
func (m *MockMetadataStore) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (*metadata.ObjectMeta, error) { return nil, nil }
func (m *MockMetadataStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error { return nil }
func (m *MockMetadataStore) CreateMultipartUpload(ctx context.Context, upload *metadata.MultipartUpload) error { return nil }
func (m *MockMetadataStore) GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*metadata.MultipartUpload, error) { return nil, nil }
func (m *MockMetadataStore) DeleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error { return nil }
func (m *MockMetadataStore) ListMultipartUploads(ctx context.Context, bucket, prefix, keyMarker, uploadIDMarker string, maxUploads int) (*metadata.MultipartUploadListing, error) { return nil, nil }
func (m *MockMetadataStore) AddPart(ctx context.Context, bucket, key, uploadID string, part *metadata.Part) error { return nil }
func (m *MockMetadataStore) GetParts(ctx context.Context, bucket, key, uploadID string) ([]*metadata.Part, error) { return nil, nil }
func (m *MockMetadataStore) Close() error { return nil }

// MockStorageBackend implements backend.Backend interface for testing
type MockStorageBackend struct {
	storageInfo    *backend.StorageInfo
	storageInfoErr error
}

func (m *MockStorageBackend) GetStorageInfo(ctx context.Context) (*backend.StorageInfo, error) {
	if m.storageInfoErr != nil {
		return nil, m.storageInfoErr
	}
	if m.storageInfo == nil {
		return &backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  500000000,
		}, nil
	}
	return m.storageInfo, nil
}

// Implement remaining interface methods as no-ops
func (m *MockStorageBackend) CreateBucket(ctx context.Context, name string) error { return nil }
func (m *MockStorageBackend) DeleteBucket(ctx context.Context, name string) error { return nil }
func (m *MockStorageBackend) PutObject(ctx context.Context, bucket, key string, data []byte) error { return nil }
func (m *MockStorageBackend) GetObject(ctx context.Context, bucket, key string) ([]byte, error) { return nil, nil }
func (m *MockStorageBackend) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (m *MockStorageBackend) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) { return nil, nil }

func TestNewChecker(t *testing.T) {
	store := &MockMetadataStore{}
	storage := &MockStorageBackend{}

	checker := NewChecker(store, storage)

	require.NotNil(t, checker)
	assert.Equal(t, 5*time.Second, checker.cacheTTL)
}

func TestCheckHealthy(t *testing.T) {
	store := &MockMetadataStore{
		isLeader:      true,
		leaderAddress: "localhost:9003",
		clusterInfo: &metadata.ClusterInfo{
			RaftState:     "Leader",
			LeaderAddress: "localhost:9003",
		},
	}
	storage := &MockStorageBackend{
		storageInfo: &backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  500000000,
		},
	}

	checker := NewChecker(store, storage)
	ctx := context.Background()

	status := checker.Check(ctx)

	require.NotNil(t, status)
	assert.Equal(t, StatusHealthy, status.Status)
	assert.Contains(t, status.Checks, "raft")
	assert.Contains(t, status.Checks, "storage")
	assert.Contains(t, status.Checks, "metadata")
	assert.Equal(t, StatusHealthy, status.Checks["raft"].Status)
	assert.Equal(t, StatusHealthy, status.Checks["storage"].Status)
	assert.Equal(t, StatusHealthy, status.Checks["metadata"].Status)
}

func TestCheckDegraded(t *testing.T) {
	store := &MockMetadataStore{
		isLeader:      false,
		leaderAddress: "", // No leader
		clusterInfo: &metadata.ClusterInfo{
			RaftState:     "Candidate",
			LeaderAddress: "",
		},
	}
	storage := &MockStorageBackend{
		storageInfo: &backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  500000000,
		},
	}

	checker := NewChecker(store, storage)
	ctx := context.Background()

	status := checker.Check(ctx)

	require.NotNil(t, status)
	assert.Equal(t, StatusDegraded, status.Status)
	assert.Equal(t, StatusDegraded, status.Checks["raft"].Status)
}

func TestCheckUnhealthy(t *testing.T) {
	// Nil store should result in unhealthy
	checker := NewChecker(nil, nil)
	ctx := context.Background()

	status := checker.Check(ctx)

	require.NotNil(t, status)
	assert.Equal(t, StatusUnhealthy, status.Status)
}

func TestCheckRaft(t *testing.T) {
	tests := []struct {
		name           string
		store          *MockMetadataStore
		expectedStatus Status
	}{
		{
			name:           "nil store",
			store:          nil,
			expectedStatus: StatusUnhealthy,
		},
		{
			name: "healthy leader",
			store: &MockMetadataStore{
				isLeader:      true,
				leaderAddress: "localhost:9003",
				clusterInfo: &metadata.ClusterInfo{
					RaftState:     "Leader",
					LeaderAddress: "localhost:9003",
				},
			},
			expectedStatus: StatusHealthy,
		},
		{
			name: "healthy follower",
			store: &MockMetadataStore{
				isLeader:      false,
				leaderAddress: "localhost:9003",
				clusterInfo: &metadata.ClusterInfo{
					RaftState:     "Follower",
					LeaderAddress: "localhost:9003",
				},
			},
			expectedStatus: StatusHealthy,
		},
		{
			name: "no leader",
			store: &MockMetadataStore{
				clusterInfo: &metadata.ClusterInfo{
					RaftState:     "Candidate",
					LeaderAddress: "",
				},
			},
			expectedStatus: StatusDegraded,
		},
		{
			name: "unknown state",
			store: &MockMetadataStore{
				clusterInfo: &metadata.ClusterInfo{
					RaftState:     "",
					LeaderAddress: "localhost:9003",
				},
			},
			expectedStatus: StatusDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var checker *Checker
			if tt.store == nil {
				checker = &Checker{store: nil}
			} else {
				checker = &Checker{store: tt.store}
			}

			check := checker.CheckRaft(context.Background())
			assert.Equal(t, tt.expectedStatus, check.Status)
		})
	}
}

func TestCheckStorage(t *testing.T) {
	tests := []struct {
		name           string
		storage        *MockStorageBackend
		expectedStatus Status
	}{
		{
			name:           "nil storage",
			storage:        nil,
			expectedStatus: StatusUnhealthy,
		},
		{
			name: "healthy storage",
			storage: &MockStorageBackend{
				storageInfo: &backend.StorageInfo{
					TotalBytes: 1000000000,
					UsedBytes:  500000000, // 50%
				},
			},
			expectedStatus: StatusHealthy,
		},
		{
			name: "storage nearly full",
			storage: &MockStorageBackend{
				storageInfo: &backend.StorageInfo{
					TotalBytes: 1000000000,
					UsedBytes:  910000000, // 91%
				},
			},
			expectedStatus: StatusDegraded,
		},
		{
			name: "storage critically full",
			storage: &MockStorageBackend{
				storageInfo: &backend.StorageInfo{
					TotalBytes: 1000000000,
					UsedBytes:  960000000, // 96%
				},
			},
			expectedStatus: StatusUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &Checker{storage: tt.storage}
			check := checker.CheckStorage(context.Background())
			assert.Equal(t, tt.expectedStatus, check.Status)
		})
	}
}

func TestCheckMetadata(t *testing.T) {
	tests := []struct {
		name           string
		store          *MockMetadataStore
		expectedStatus Status
	}{
		{
			name:           "nil store",
			store:          nil,
			expectedStatus: StatusUnhealthy,
		},
		{
			name: "healthy store",
			store: &MockMetadataStore{
				buckets: []*metadata.Bucket{},
			},
			expectedStatus: StatusHealthy,
		},
		{
			name: "store error",
			store: &MockMetadataStore{
				listBucketsErr: s3errors.ErrInternalError,
			},
			expectedStatus: StatusUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &Checker{store: tt.store}
			check := checker.CheckMetadata(context.Background())
			assert.Equal(t, tt.expectedStatus, check.Status)
		})
	}
}

func TestIsReady(t *testing.T) {
	tests := []struct {
		name     string
		store    *MockMetadataStore
		expected bool
	}{
		{
			name:     "nil store",
			store:    nil,
			expected: false,
		},
		{
			name: "is leader",
			store: &MockMetadataStore{
				isLeader: true,
			},
			expected: true,
		},
		{
			name: "has leader",
			store: &MockMetadataStore{
				isLeader:      false,
				leaderAddress: "localhost:9003",
			},
			expected: true,
		},
		{
			name: "no leader",
			store: &MockMetadataStore{
				isLeader:      false,
				leaderAddress: "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &Checker{store: tt.store}
			result := checker.IsReady(context.Background())
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsLive(t *testing.T) {
	checker := &Checker{}
	assert.True(t, checker.IsLive(context.Background()))
}

func TestDetermineOverallStatus(t *testing.T) {
	tests := []struct {
		name     string
		checks   map[string]Check
		expected Status
	}{
		{
			name: "all healthy",
			checks: map[string]Check{
				"raft":     {Status: StatusHealthy},
				"storage":  {Status: StatusHealthy},
				"metadata": {Status: StatusHealthy},
			},
			expected: StatusHealthy,
		},
		{
			name: "one degraded",
			checks: map[string]Check{
				"raft":     {Status: StatusDegraded},
				"storage":  {Status: StatusHealthy},
				"metadata": {Status: StatusHealthy},
			},
			expected: StatusDegraded,
		},
		{
			name: "one unhealthy",
			checks: map[string]Check{
				"raft":     {Status: StatusHealthy},
				"storage":  {Status: StatusUnhealthy},
				"metadata": {Status: StatusHealthy},
			},
			expected: StatusUnhealthy,
		},
		{
			name: "mixed",
			checks: map[string]Check{
				"raft":     {Status: StatusDegraded},
				"storage":  {Status: StatusUnhealthy},
				"metadata": {Status: StatusHealthy},
			},
			expected: StatusUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &Checker{}
			result := checker.determineOverallStatus(tt.checks)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCaching(t *testing.T) {
	store := &MockMetadataStore{
		isLeader:      true,
		leaderAddress: "localhost:9003",
		clusterInfo: &metadata.ClusterInfo{
			RaftState:     "Leader",
			LeaderAddress: "localhost:9003",
		},
	}
	storage := &MockStorageBackend{}

	checker := NewChecker(store, storage)
	checker.cacheTTL = 100 * time.Millisecond
	ctx := context.Background()

	// First check
	status1 := checker.Check(ctx)
	timestamp1 := status1.Timestamp

	// Immediate second check should return cached result
	status2 := checker.Check(ctx)
	assert.Equal(t, timestamp1, status2.Timestamp)

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Third check should return fresh result
	status3 := checker.Check(ctx)
	assert.NotEqual(t, timestamp1, status3.Timestamp)
}

func TestNewHandler(t *testing.T) {
	store := &MockMetadataStore{}
	storage := &MockStorageBackend{}
	checker := NewChecker(store, storage)

	handler := NewHandler(checker)
	require.NotNil(t, handler)
}

func TestHealthHandler(t *testing.T) {
	tests := []struct {
		name           string
		store          *MockMetadataStore
		storage        *MockStorageBackend
		expectedCode   int
	}{
		{
			name: "healthy",
			store: &MockMetadataStore{
				isLeader:      true,
				leaderAddress: "localhost:9003",
				clusterInfo: &metadata.ClusterInfo{
					RaftState:     "Leader",
					LeaderAddress: "localhost:9003",
				},
			},
			storage: &MockStorageBackend{
				storageInfo: &backend.StorageInfo{
					TotalBytes: 1000000000,
					UsedBytes:  500000000,
				},
			},
			expectedCode: http.StatusOK,
		},
		{
			name: "unhealthy",
			store: nil,
			storage: nil,
			expectedCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var checker *Checker
			if tt.store == nil {
				checker = NewChecker(nil, nil)
			} else {
				checker = NewChecker(tt.store, tt.storage)
			}
			handler := NewHandler(checker)

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			w := httptest.NewRecorder()

			handler.HealthHandler(w, req)

			assert.Equal(t, tt.expectedCode, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			var response map[string]string
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Contains(t, response, "status")
		})
	}
}

func TestLivenessHandler(t *testing.T) {
	store := &MockMetadataStore{}
	storage := &MockStorageBackend{}
	checker := NewChecker(store, storage)
	handler := NewHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	handler.LivenessHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestReadinessHandler(t *testing.T) {
	tests := []struct {
		name         string
		store        *MockMetadataStore
		expectedCode int
	}{
		{
			name: "ready",
			store: &MockMetadataStore{
				isLeader: true,
			},
			expectedCode: http.StatusOK,
		},
		{
			name: "not ready",
			store: &MockMetadataStore{
				isLeader:      false,
				leaderAddress: "",
			},
			expectedCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &MockStorageBackend{}
			checker := NewChecker(tt.store, storage)
			handler := NewHandler(checker)

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			w := httptest.NewRecorder()

			handler.ReadinessHandler(w, req)

			assert.Equal(t, tt.expectedCode, w.Code)
		})
	}
}

func TestDetailedHandler(t *testing.T) {
	store := &MockMetadataStore{
		isLeader:      true,
		leaderAddress: "localhost:9003",
		clusterInfo: &metadata.ClusterInfo{
			RaftState:     "Leader",
			LeaderAddress: "localhost:9003",
		},
	}
	storage := &MockStorageBackend{
		storageInfo: &backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  500000000,
		},
	}

	checker := NewChecker(store, storage)
	handler := NewHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	w := httptest.NewRecorder()

	handler.DetailedHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var status HealthStatus
	err := json.Unmarshal(w.Body.Bytes(), &status)
	require.NoError(t, err)
	assert.Equal(t, StatusHealthy, status.Status)
	assert.Contains(t, status.Checks, "raft")
	assert.Contains(t, status.Checks, "storage")
	assert.Contains(t, status.Checks, "metadata")
}
