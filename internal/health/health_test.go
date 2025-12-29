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
	"github.com/piwi3910/nebulaio/internal/testutil/mocks"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChecker(t *testing.T) {
	store := mocks.NewMockMetadataStore()
	storage := mocks.NewMockStorageBackend()

	checker := NewChecker(store, storage)

	require.NotNil(t, checker)
	assert.Equal(t, 5*time.Second, checker.cacheTTL)
}

func TestCheckHealthy(t *testing.T) {
	store := mocks.NewMockMetadataStore()
	store.SetIsLeader(true)
	store.SetLeaderAddress("localhost:9003")
	store.SetClusterInfo(&metadata.ClusterInfo{
		RaftState:     "Leader",
		LeaderAddress: "localhost:9003",
	})
	storage := mocks.NewMockStorageBackend()
	storage.SetStorageInfo(&backend.StorageInfo{
		TotalBytes: 1000000000,
		UsedBytes:  500000000,
	})

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
	store := mocks.NewMockMetadataStore()
	store.SetIsLeader(false)
	store.SetLeaderAddress("") // No leader
	store.SetClusterInfo(&metadata.ClusterInfo{
		RaftState:     "Candidate",
		LeaderAddress: "",
	})
	storage := mocks.NewMockStorageBackend()
	storage.SetStorageInfo(&backend.StorageInfo{
		TotalBytes: 1000000000,
		UsedBytes:  500000000,
	})

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
	t.Run("nil store", func(t *testing.T) {
		checker := &Checker{store: nil}
		check := checker.CheckRaft(context.Background())
		assert.Equal(t, StatusUnhealthy, check.Status)
	})

	t.Run("healthy leader", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetIsLeader(true)
		store.SetLeaderAddress("localhost:9003")
		store.SetClusterInfo(&metadata.ClusterInfo{
			RaftState:     "Leader",
			LeaderAddress: "localhost:9003",
		})
		checker := &Checker{store: store}
		check := checker.CheckRaft(context.Background())
		assert.Equal(t, StatusHealthy, check.Status)
	})

	t.Run("healthy follower", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetIsLeader(false)
		store.SetLeaderAddress("localhost:9003")
		store.SetClusterInfo(&metadata.ClusterInfo{
			RaftState:     "Follower",
			LeaderAddress: "localhost:9003",
		})
		checker := &Checker{store: store}
		check := checker.CheckRaft(context.Background())
		assert.Equal(t, StatusHealthy, check.Status)
	})

	t.Run("no leader", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetClusterInfo(&metadata.ClusterInfo{
			RaftState:     "Candidate",
			LeaderAddress: "",
		})
		checker := &Checker{store: store}
		check := checker.CheckRaft(context.Background())
		assert.Equal(t, StatusDegraded, check.Status)
	})

	t.Run("unknown state", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetClusterInfo(&metadata.ClusterInfo{
			RaftState:     "",
			LeaderAddress: "localhost:9003",
		})
		checker := &Checker{store: store}
		check := checker.CheckRaft(context.Background())
		assert.Equal(t, StatusDegraded, check.Status)
	})
}

func TestCheckStorage(t *testing.T) {
	t.Run("nil storage", func(t *testing.T) {
		checker := &Checker{storage: nil}
		check := checker.CheckStorage(context.Background())
		assert.Equal(t, StatusUnhealthy, check.Status)
	})

	t.Run("healthy storage", func(t *testing.T) {
		storage := mocks.NewMockStorageBackend()
		storage.SetStorageInfo(&backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  500000000, // 50%
		})
		checker := &Checker{storage: storage}
		check := checker.CheckStorage(context.Background())
		assert.Equal(t, StatusHealthy, check.Status)
	})

	t.Run("storage nearly full", func(t *testing.T) {
		storage := mocks.NewMockStorageBackend()
		storage.SetStorageInfo(&backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  910000000, // 91%
		})
		checker := &Checker{storage: storage}
		check := checker.CheckStorage(context.Background())
		assert.Equal(t, StatusDegraded, check.Status)
	})

	t.Run("storage critically full", func(t *testing.T) {
		storage := mocks.NewMockStorageBackend()
		storage.SetStorageInfo(&backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  960000000, // 96%
		})
		checker := &Checker{storage: storage}
		check := checker.CheckStorage(context.Background())
		assert.Equal(t, StatusUnhealthy, check.Status)
	})
}

func TestCheckMetadata(t *testing.T) {
	t.Run("nil store", func(t *testing.T) {
		checker := &Checker{store: nil}
		check := checker.CheckMetadata(context.Background())
		assert.Equal(t, StatusUnhealthy, check.Status)
	})

	t.Run("healthy store", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		checker := &Checker{store: store}
		check := checker.CheckMetadata(context.Background())
		assert.Equal(t, StatusHealthy, check.Status)
	})

	t.Run("store error", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetListBucketsError(s3errors.ErrInternalError)
		checker := &Checker{store: store}
		check := checker.CheckMetadata(context.Background())
		assert.Equal(t, StatusUnhealthy, check.Status)
	})
}

func TestIsReady(t *testing.T) {
	t.Run("nil store", func(t *testing.T) {
		checker := &Checker{store: nil}
		result := checker.IsReady(context.Background())
		assert.False(t, result)
	})

	t.Run("is leader", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetIsLeader(true)
		checker := &Checker{store: store}
		result := checker.IsReady(context.Background())
		assert.True(t, result)
	})

	t.Run("has leader", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetIsLeader(false)
		store.SetLeaderAddress("localhost:9003")
		checker := &Checker{store: store}
		result := checker.IsReady(context.Background())
		assert.True(t, result)
	})

	t.Run("no leader", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetIsLeader(false)
		store.SetLeaderAddress("")
		checker := &Checker{store: store}
		result := checker.IsReady(context.Background())
		assert.False(t, result)
	})
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
	store := mocks.NewMockMetadataStore()
	store.SetIsLeader(true)
	store.SetLeaderAddress("localhost:9003")
	store.SetClusterInfo(&metadata.ClusterInfo{
		RaftState:     "Leader",
		LeaderAddress: "localhost:9003",
	})
	storage := mocks.NewMockStorageBackend()

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
	store := mocks.NewMockMetadataStore()
	storage := mocks.NewMockStorageBackend()
	checker := NewChecker(store, storage)

	handler := NewHandler(checker)
	require.NotNil(t, handler)
}

func TestHealthHandler(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetIsLeader(true)
		store.SetLeaderAddress("localhost:9003")
		store.SetClusterInfo(&metadata.ClusterInfo{
			RaftState:     "Leader",
			LeaderAddress: "localhost:9003",
		})
		storage := mocks.NewMockStorageBackend()
		storage.SetStorageInfo(&backend.StorageInfo{
			TotalBytes: 1000000000,
			UsedBytes:  500000000,
		})
		checker := NewChecker(store, storage)
		handler := NewHandler(checker)

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()

		handler.HealthHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Contains(t, response, "status")
	})

	t.Run("unhealthy", func(t *testing.T) {
		checker := NewChecker(nil, nil)
		handler := NewHandler(checker)

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()

		handler.HealthHandler(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Contains(t, response, "status")
	})
}

func TestLivenessHandler(t *testing.T) {
	store := mocks.NewMockMetadataStore()
	storage := mocks.NewMockStorageBackend()
	checker := NewChecker(store, storage)
	handler := NewHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	handler.LivenessHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestReadinessHandler(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetIsLeader(true)
		storage := mocks.NewMockStorageBackend()
		checker := NewChecker(store, storage)
		handler := NewHandler(checker)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()

		handler.ReadinessHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("not ready", func(t *testing.T) {
		store := mocks.NewMockMetadataStore()
		store.SetIsLeader(false)
		store.SetLeaderAddress("")
		storage := mocks.NewMockStorageBackend()
		checker := NewChecker(store, storage)
		handler := NewHandler(checker)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()

		handler.ReadinessHandler(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestDetailedHandler(t *testing.T) {
	store := mocks.NewMockMetadataStore()
	store.SetIsLeader(true)
	store.SetLeaderAddress("localhost:9003")
	store.SetClusterInfo(&metadata.ClusterInfo{
		RaftState:     "Leader",
		LeaderAddress: "localhost:9003",
	})
	storage := mocks.NewMockStorageBackend()
	storage.SetStorageInfo(&backend.StorageInfo{
		TotalBytes: 1000000000,
		UsedBytes:  500000000,
	})

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
