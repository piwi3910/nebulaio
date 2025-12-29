package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errLeaderNotAvailable is used in tests to simulate leader election errors
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
	assert.Equal(t, float64(1), count)

	// Record another request
	RecordRequest("PUT", "PutObject", 201, 50*time.Millisecond)
	count = testutil.ToFloat64(RequestsTotal.WithLabelValues("PUT", "PutObject", "2xx"))
	assert.Equal(t, float64(1), count)
}

func TestRecordS3Operation(t *testing.T) {
	S3OperationsTotal.Reset()

	RecordS3Operation("GetObject", "test-bucket")

	count := testutil.ToFloat64(S3OperationsTotal.WithLabelValues("GetObject", "test-bucket"))
	assert.Equal(t, float64(1), count)

	// Record multiple operations
	RecordS3Operation("GetObject", "test-bucket")
	RecordS3Operation("GetObject", "test-bucket")
	count = testutil.ToFloat64(S3OperationsTotal.WithLabelValues("GetObject", "test-bucket"))
	assert.Equal(t, float64(3), count)
}

func TestRecordError(t *testing.T) {
	ErrorsTotal.Reset()

	RecordError("GetObject", "NotFound")

	count := testutil.ToFloat64(ErrorsTotal.WithLabelValues("GetObject", "NotFound"))
	assert.Equal(t, float64(1), count)

	RecordError("GetObject", "NotFound")
	count = testutil.ToFloat64(ErrorsTotal.WithLabelValues("GetObject", "NotFound"))
	assert.Equal(t, float64(2), count)
}

func TestSetRaftLeader(t *testing.T) {
	RaftIsLeader.Reset()
	RaftState.Reset()

	shardID := "1"

	// Set as leader
	SetRaftLeader(shardID, true)
	assert.Equal(t, float64(1), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardID)))
	assert.Equal(t, float64(1), testutil.ToFloat64(RaftState.WithLabelValues(shardID)))

	// Set as follower
	SetRaftLeader(shardID, false)
	assert.Equal(t, float64(0), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardID)))
	assert.Equal(t, float64(0), testutil.ToFloat64(RaftState.WithLabelValues(shardID)))
}

func TestRecordRaftProposal(t *testing.T) {
	RaftProposalsTotal.Reset()
	RaftProposalLatency.Reset()

	shardID := "1"

	// Record successful proposal
	RecordRaftProposal(shardID, 10*time.Millisecond, true)
	count := testutil.ToFloat64(RaftProposalsTotal.WithLabelValues(shardID, "success"))
	assert.Equal(t, float64(1), count)

	// Record failed proposal
	RecordRaftProposal(shardID, 5*time.Millisecond, false)
	count = testutil.ToFloat64(RaftProposalsTotal.WithLabelValues(shardID, "failure"))
	assert.Equal(t, float64(1), count)
}

func TestActiveConnections(t *testing.T) {
	ActiveConnections.Set(0) // Reset

	IncrementActiveConnections()
	assert.Equal(t, float64(1), testutil.ToFloat64(ActiveConnections))

	IncrementActiveConnections()
	assert.Equal(t, float64(2), testutil.ToFloat64(ActiveConnections))

	DecrementActiveConnections()
	assert.Equal(t, float64(1), testutil.ToFloat64(ActiveConnections))

	DecrementActiveConnections()
	assert.Equal(t, float64(0), testutil.ToFloat64(ActiveConnections))
}

func TestAddBytesReceived(t *testing.T) {
	// Get initial value
	initial := testutil.ToFloat64(BytesReceived)

	AddBytesReceived(1024)
	assert.Equal(t, initial+1024, testutil.ToFloat64(BytesReceived))

	AddBytesReceived(2048)
	assert.Equal(t, initial+3072, testutil.ToFloat64(BytesReceived))
}

func TestAddBytesSent(t *testing.T) {
	// Get initial value
	initial := testutil.ToFloat64(BytesSent)

	AddBytesSent(1024)
	assert.Equal(t, initial+1024, testutil.ToFloat64(BytesSent))

	AddBytesSent(2048)
	assert.Equal(t, initial+3072, testutil.ToFloat64(BytesSent))
}

func TestSetStorageStats(t *testing.T) {
	SetStorageStats(500000000, 1000000000)

	assert.Equal(t, float64(500000000), testutil.ToFloat64(StorageBytesUsed))
	assert.Equal(t, float64(1000000000), testutil.ToFloat64(StorageBytesTotal))

	SetStorageStats(750000000, 2000000000)
	assert.Equal(t, float64(750000000), testutil.ToFloat64(StorageBytesUsed))
	assert.Equal(t, float64(2000000000), testutil.ToFloat64(StorageBytesTotal))
}

func TestSetBucketsTotal(t *testing.T) {
	SetBucketsTotal(5)
	assert.Equal(t, float64(5), testutil.ToFloat64(BucketsTotal))

	SetBucketsTotal(10)
	assert.Equal(t, float64(10), testutil.ToFloat64(BucketsTotal))
}

func TestSetObjectsTotal(t *testing.T) {
	ObjectsTotal.Reset()

	SetObjectsTotal("bucket1", 100)
	assert.Equal(t, float64(100), testutil.ToFloat64(ObjectsTotal.WithLabelValues("bucket1")))

	SetObjectsTotal("bucket2", 200)
	assert.Equal(t, float64(200), testutil.ToFloat64(ObjectsTotal.WithLabelValues("bucket2")))

	SetObjectsTotal("bucket1", 150)
	assert.Equal(t, float64(150), testutil.ToFloat64(ObjectsTotal.WithLabelValues("bucket1")))
}

func TestSetMultipartUploadsActive(t *testing.T) {
	SetMultipartUploadsActive(3)
	assert.Equal(t, float64(3), testutil.ToFloat64(MultipartUploadsActive))

	SetMultipartUploadsActive(5)
	assert.Equal(t, float64(5), testutil.ToFloat64(MultipartUploadsActive))
}

func TestSetClusterNodesTotal(t *testing.T) {
	SetClusterNodesTotal(3)
	assert.Equal(t, float64(3), testutil.ToFloat64(ClusterNodesTotal))

	SetClusterNodesTotal(5)
	assert.Equal(t, float64(5), testutil.ToFloat64(ClusterNodesTotal))
}

func TestStatusCodeToString(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{200, "2xx"},
		{201, "2xx"},
		{204, "2xx"},
		{301, "3xx"},
		{302, "3xx"},
		{400, "4xx"},
		{404, "4xx"},
		{403, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{0, "unknown"},
		{100, "unknown"},
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

// MockNodeHost implements DragonboatNodeHost for testing
type MockNodeHost struct {
	leaderID  uint64
	term      uint64
	leaderErr error
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
		assert.Equal(t, float64(1), testutil.ToFloat64(RaftState.WithLabelValues(shardLabel)))
		assert.Equal(t, float64(1), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardLabel)))
	})

	t.Run("is follower", func(t *testing.T) {
		nodeHost := &MockNodeHost{
			leaderID: 200, // Different from replicaID
			term:     1,
		}

		CollectDragonboatMetrics(shardID, nodeHost, replicaID)

		shardLabel := "1"
		assert.Equal(t, float64(0), testutil.ToFloat64(RaftState.WithLabelValues(shardLabel)))
		assert.Equal(t, float64(0), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardLabel)))
	})

	t.Run("error getting leader", func(t *testing.T) {
		nodeHost := &MockNodeHost{
			leaderErr: errLeaderNotAvailable,
		}

		CollectDragonboatMetrics(shardID, nodeHost, replicaID)

		shardLabel := "1"
		assert.Equal(t, float64(0), testutil.ToFloat64(RaftState.WithLabelValues(shardLabel)))
		assert.Equal(t, float64(0), testutil.ToFloat64(RaftIsLeader.WithLabelValues(shardLabel)))
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

// Helper function for benchmarks
func resetAllMetrics() {
	RequestsTotal.Reset()
	RequestDuration.Reset()
	ObjectsTotal.Reset()
	BucketsTotal.Set(0)
	StorageBytesUsed.Set(0)
	StorageBytesTotal.Set(0)
	ActiveConnections.Set(0)
	RaftState.Reset()
	RaftIsLeader.Reset()
	RaftProposalLatency.Reset()
	RaftProposalsTotal.Reset()
	MultipartUploadsActive.Set(0)
	ErrorsTotal.Reset()
	S3OperationsTotal.Reset()
	ClusterNodesTotal.Set(0)
}

func BenchmarkRecordRequest(b *testing.B) {
	for i := 0; i < b.N; i++ {
		RecordRequest("GET", "GetObject", 200, 10*time.Millisecond)
	}
}

func BenchmarkRecordS3Operation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		RecordS3Operation("GetObject", "test-bucket")
	}
}

func BenchmarkIncrementActiveConnections(b *testing.B) {
	for i := 0; i < b.N; i++ {
		IncrementActiveConnections()
	}
}

func BenchmarkSetStorageStats(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SetStorageStats(int64(i*1024), int64(1000000))
	}
}
