package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errLeaderNotAvailable is used in tests to simulate leader election errors.
var errLeaderNotAvailable = errors.New("leader not available")

func TestInit(t *testing.T) {
	nodeID := "test-node-1"
	Init(nodeID)

	// Verify NodeInfo metric is set
	// Note: We can't easily verify the exact value due to how prometheus works,
	// but we can ensure no panic occurs
}

func TestRecordRequest(t *testing.T) {
	// Reset metrics for testing
	RequestsTotal.Reset()
	RequestDuration.Reset()

	// Record a request
	RecordRequest("GET", "GetObject", 200, 100*time.Millisecond)

	// Verify counter was incremented
	count := testutil.ToFloat64(RequestsTotal.WithLabelValues("GET", "GetObject", "2xx"))
	assert.InDelta(t, float64(1), count, 0.001)

	// Record another request
	RecordRequest("PUT", "PutObject", 201, 50*time.Millisecond)

	count = testutil.ToFloat64(RequestsTotal.WithLabelValues("PUT", "PutObject", "2xx"))
	assert.InDelta(t, float64(1), count, 0.001)
}

func TestRecordS3Operation(t *testing.T) {
	S3OperationsTotal.Reset()

	RecordS3Operation("GetObject", "test-bucket")

	count := testutil.ToFloat64(S3OperationsTotal.WithLabelValues("GetObject", "test-bucket"))
	assert.InDelta(t, float64(1), count, 0.001)

	// Record multiple operations
	RecordS3Operation("GetObject", "test-bucket")
	RecordS3Operation("GetObject", "test-bucket")

	count = testutil.ToFloat64(S3OperationsTotal.WithLabelValues("GetObject", "test-bucket"))
	assert.InDelta(t, float64(3), count, 0.001)
}

func TestRecordError(t *testing.T) {
	ErrorsTotal.Reset()

	RecordError("GetObject", "NotFound")

	count := testutil.ToFloat64(ErrorsTotal.WithLabelValues("GetObject", "NotFound"))
	assert.InDelta(t, float64(1), count, 0.001)

	RecordError("GetObject", "NotFound")

	count = testutil.ToFloat64(ErrorsTotal.WithLabelValues("GetObject", "NotFound"))
	assert.InDelta(t, float64(2), count, 0.001)
}

func TestSetRaftLeader(t *testing.T) {
	RaftIsLeader.Reset()
	RaftState.Reset()

	shardID := "1"

	// Set as leader
	SetRaftLeader(shardID, true)
	assert.InDelta(t, float64(1), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardID)), 0.001)
	assert.InDelta(t, float64(1), testutil.ToFloat64(RaftState.WithLabelValues(shardID)), 0.001)

	// Set as follower
	SetRaftLeader(shardID, false)
	assert.InDelta(t, float64(0), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardID)), 0.001)
	assert.InDelta(t, float64(0), testutil.ToFloat64(RaftState.WithLabelValues(shardID)), 0.001)
}

func TestRecordRaftProposal(t *testing.T) {
	RaftProposalsTotal.Reset()
	RaftProposalLatency.Reset()

	shardID := "1"

	// Record successful proposal
	RecordRaftProposal(shardID, 10*time.Millisecond, true)
	count := testutil.ToFloat64(RaftProposalsTotal.WithLabelValues(shardID, "success"))
	assert.InDelta(t, float64(1), count, 0.001)

	// Record failed proposal
	RecordRaftProposal(shardID, 5*time.Millisecond, false)
	count = testutil.ToFloat64(RaftProposalsTotal.WithLabelValues(shardID, "failure"))
	assert.InDelta(t, float64(1), count, 0.001)
}

func TestActiveConnections(t *testing.T) {
	ActiveConnections.Set(0) // Reset

	IncrementActiveConnections()
	assert.InDelta(t, float64(1), testutil.ToFloat64(ActiveConnections), 0.001)

	IncrementActiveConnections()
	assert.InDelta(t, float64(2), testutil.ToFloat64(ActiveConnections), 0.001)

	DecrementActiveConnections()
	assert.InDelta(t, float64(1), testutil.ToFloat64(ActiveConnections), 0.001)

	DecrementActiveConnections()
	assert.InDelta(t, float64(0), testutil.ToFloat64(ActiveConnections), 0.001)
}

func TestAddBytesReceived(t *testing.T) {
	// Get initial value
	initial := testutil.ToFloat64(BytesReceived)

	AddBytesReceived(1024)
	assert.InDelta(t, initial+1024, testutil.ToFloat64(BytesReceived), 0.001)

	AddBytesReceived(2048)
	assert.InDelta(t, initial+3072, testutil.ToFloat64(BytesReceived), 0.001)
}

func TestAddBytesSent(t *testing.T) {
	// Get initial value
	initial := testutil.ToFloat64(BytesSent)

	AddBytesSent(1024)
	assert.InDelta(t, initial+1024, testutil.ToFloat64(BytesSent), 0.001)

	AddBytesSent(2048)
	assert.InDelta(t, initial+3072, testutil.ToFloat64(BytesSent), 0.001)
}

func TestSetStorageStats(t *testing.T) {
	SetStorageStats(500000000, 1000000000)

	assert.InDelta(t, float64(500000000), testutil.ToFloat64(StorageBytesUsed), 0.001)
	assert.InDelta(t, float64(1000000000), testutil.ToFloat64(StorageBytesTotal), 0.001)

	SetStorageStats(750000000, 2000000000)
	assert.InDelta(t, float64(750000000), testutil.ToFloat64(StorageBytesUsed), 0.001)
	assert.InDelta(t, float64(2000000000), testutil.ToFloat64(StorageBytesTotal), 0.001)
}

func TestSetBucketsTotal(t *testing.T) {
	SetBucketsTotal(5)
	assert.InDelta(t, float64(5), testutil.ToFloat64(BucketsTotal), 0.001)

	SetBucketsTotal(10)
	assert.InDelta(t, float64(10), testutil.ToFloat64(BucketsTotal), 0.001)
}

func TestSetObjectsTotal(t *testing.T) {
	ObjectsTotal.Reset()

	SetObjectsTotal("bucket1", 100)
	assert.InDelta(t, float64(100), testutil.ToFloat64(ObjectsTotal.WithLabelValues("bucket1")), 0.001)

	SetObjectsTotal("bucket2", 200)
	assert.InDelta(t, float64(200), testutil.ToFloat64(ObjectsTotal.WithLabelValues("bucket2")), 0.001)

	SetObjectsTotal("bucket1", 150)
	assert.InDelta(t, float64(150), testutil.ToFloat64(ObjectsTotal.WithLabelValues("bucket1")), 0.001)
}

func TestSetMultipartUploadsActive(t *testing.T) {
	SetMultipartUploadsActive(3)
	assert.InDelta(t, float64(3), testutil.ToFloat64(MultipartUploadsActive), 0.001)

	SetMultipartUploadsActive(5)
	assert.InDelta(t, float64(5), testutil.ToFloat64(MultipartUploadsActive), 0.001)
}

func TestSetClusterNodesTotal(t *testing.T) {
	SetClusterNodesTotal(3)
	assert.InDelta(t, float64(3), testutil.ToFloat64(ClusterNodesTotal), 0.001)

	SetClusterNodesTotal(5)
	assert.InDelta(t, float64(5), testutil.ToFloat64(ClusterNodesTotal), 0.001)
}

func TestStatusCodeToString(t *testing.T) {
	tests := []struct {
		expected string
		code     int
	}{
		{"2xx", 200},
		{"2xx", 201},
		{"2xx", 204},
		{"3xx", 301},
		{"3xx", 302},
		{"4xx", 400},
		{"4xx", 404},
		{"4xx", 403},
		{"5xx", 500},
		{"5xx", 503},
		{"unknown", 0},
		{"unknown", 100},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.code)), func(t *testing.T) {
			result := statusCodeToString(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMetricsRegistration(t *testing.T) {
	// Verify all metrics are registered properly by checking they exist
	require.NotNil(t, RequestsTotal)
	require.NotNil(t, RequestDuration)
	require.NotNil(t, ObjectsTotal)
	require.NotNil(t, BucketsTotal)
	require.NotNil(t, StorageBytesUsed)
	require.NotNil(t, StorageBytesTotal)
	require.NotNil(t, ActiveConnections)
	require.NotNil(t, RaftState)
	require.NotNil(t, RaftIsLeader)
	require.NotNil(t, RaftProposalLatency)
	require.NotNil(t, RaftProposalsTotal)
	require.NotNil(t, MultipartUploadsActive)
	require.NotNil(t, BytesReceived)
	require.NotNil(t, BytesSent)
	require.NotNil(t, ErrorsTotal)
	require.NotNil(t, S3OperationsTotal)
	require.NotNil(t, ClusterNodesTotal)
	require.NotNil(t, NodeInfo)
}

// MockNodeHost implements DragonboatNodeHost for testing.
type MockNodeHost struct {
	leaderErr error
	leaderID  uint64
	term      uint64
}

func (m *MockNodeHost) GetLeaderID(shardID uint64) (uint64, uint64, error) {
	return m.leaderID, m.term, m.leaderErr
}

func TestCollectDragonboatMetrics(t *testing.T) {
	RaftState.Reset()
	RaftIsLeader.Reset()

	shardID := uint64(1)
	replicaID := uint64(100)

	t.Run("is leader", func(t *testing.T) {
		nodeHost := &MockNodeHost{
			leaderID: replicaID, // Same as replicaID
			term:     1,
		}

		CollectDragonboatMetrics(shardID, nodeHost, replicaID)

		shardLabel := "1"
		assert.InDelta(t, float64(1), testutil.ToFloat64(RaftState.WithLabelValues(shardLabel)), 0.001)
		assert.InDelta(t, float64(1), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardLabel)), 0.001)
	})

	t.Run("is follower", func(t *testing.T) {
		nodeHost := &MockNodeHost{
			leaderID: 200, // Different from replicaID
			term:     1,
		}

		CollectDragonboatMetrics(shardID, nodeHost, replicaID)

		shardLabel := "1"
		assert.InDelta(t, float64(0), testutil.ToFloat64(RaftState.WithLabelValues(shardLabel)), 0.001)
		assert.InDelta(t, float64(0), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardLabel)), 0.001)
	})

	t.Run("error getting leader", func(t *testing.T) {
		nodeHost := &MockNodeHost{
			leaderErr: errLeaderNotAvailable,
		}

		CollectDragonboatMetrics(shardID, nodeHost, replicaID)

		shardLabel := "1"
		assert.InDelta(t, float64(0), testutil.ToFloat64(RaftState.WithLabelValues(shardLabel)), 0.001)
		assert.InDelta(t, float64(0), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardLabel)), 0.001)
	})
}

func TestRequestDurationHistogram(t *testing.T) {
	RequestDuration.Reset()

	// Record multiple requests with different durations
	durations := []time.Duration{
		1 * time.Millisecond,
		10 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		500 * time.Millisecond,
	}

	for _, d := range durations {
		RecordRequest("GET", "GetObject", 200, d)
	}

	// Verify the histogram was populated
	// We can't easily verify individual bucket values, but we can check the metric exists
	histogram, err := RequestDuration.GetMetricWithLabelValues("GET", "GetObject")
	require.NoError(t, err)
	require.NotNil(t, histogram)
}

func TestVersionVariable(t *testing.T) {
	// Version should have a default value
	assert.NotEmpty(t, Version)
	assert.Equal(t, "dev", Version)
}

func BenchmarkRecordRequest(b *testing.B) {
	for b.Loop() {
		RecordRequest("GET", "GetObject", 200, 10*time.Millisecond)
	}
}

func BenchmarkRecordS3Operation(b *testing.B) {
	for b.Loop() {
		RecordS3Operation("GetObject", "test-bucket")
	}
}

func BenchmarkIncrementActiveConnections(b *testing.B) {
	for b.Loop() {
		IncrementActiveConnections()
	}
}

func BenchmarkSetStorageStats(b *testing.B) {
	for i := range b.N {
		SetStorageStats(int64(i*1024), int64(1000000))
	}
}

func TestRecordRateLimitRequest(t *testing.T) {
	RateLimitRequestsTotal.Reset()

	// Test allowed request
	RecordRateLimitRequest("/api/buckets", true)
	count := testutil.ToFloat64(RateLimitRequestsTotal.WithLabelValues("/api/buckets", "allowed"))
	assert.InDelta(t, float64(1), count, 0.001)

	// Test denied request
	RecordRateLimitRequest("/api/buckets", false)
	countDenied := testutil.ToFloat64(RateLimitRequestsTotal.WithLabelValues("/api/buckets", "denied"))
	assert.InDelta(t, float64(1), countDenied, 0.001)

	// Test multiple requests
	RecordRateLimitRequest("/api/objects", true)
	RecordRateLimitRequest("/api/objects", true)
	RecordRateLimitRequest("/api/objects", false)

	countObjects := testutil.ToFloat64(RateLimitRequestsTotal.WithLabelValues("/api/objects", "allowed"))
	assert.InDelta(t, float64(2), countObjects, 0.001)
	countObjectsDenied := testutil.ToFloat64(RateLimitRequestsTotal.WithLabelValues("/api/objects", "denied"))
	assert.InDelta(t, float64(1), countObjectsDenied, 0.001)
}

func TestRateLimitActiveIPs(t *testing.T) {
	RateLimitActiveIPs.Set(0) // Reset

	IncrementRateLimitActiveIPs()
	assert.InDelta(t, float64(1), testutil.ToFloat64(RateLimitActiveIPs), 0.001)

	IncrementRateLimitActiveIPs()
	assert.InDelta(t, float64(2), testutil.ToFloat64(RateLimitActiveIPs), 0.001)

	DecrementRateLimitActiveIPs()
	assert.InDelta(t, float64(1), testutil.ToFloat64(RateLimitActiveIPs), 0.001)

	DecrementRateLimitActiveIPs()
	assert.InDelta(t, float64(0), testutil.ToFloat64(RateLimitActiveIPs), 0.001)
}

func TestRateLimitMetricsRegistration(t *testing.T) {
	// Verify rate limit metrics are registered properly
	require.NotNil(t, RateLimitRequestsTotal)
	require.NotNil(t, RateLimitActiveIPs)
}

func BenchmarkRecordRateLimitRequest(b *testing.B) {
	for b.Loop() {
		RecordRateLimitRequest("/api/buckets", true)
	}
}

func BenchmarkRateLimitActiveIPs(b *testing.B) {
	for b.Loop() {
		IncrementRateLimitActiveIPs()
	}
}
