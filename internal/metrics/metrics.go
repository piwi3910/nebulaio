package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestsTotal counts total number of requests
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_requests_total",
			Help: "Total number of requests",
		},
		[]string{"method", "operation", "status"},
	)

	// RequestDuration tracks request duration in seconds
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "operation"},
	)

	// ObjectsTotal tracks total number of objects per bucket
	ObjectsTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_objects_total",
			Help: "Total number of objects per bucket",
		},
		[]string{"bucket"},
	)

	// BucketsTotal tracks total number of buckets
	BucketsTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_buckets_total",
			Help: "Total number of buckets",
		},
	)

	// StorageBytesUsed tracks total storage bytes used
	StorageBytesUsed = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_storage_bytes_used",
			Help: "Total storage bytes used",
		},
	)

	// StorageBytesTotal tracks total storage bytes available
	StorageBytesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_storage_bytes_total",
			Help: "Total storage bytes available",
		},
	)

	// ActiveConnections tracks number of active connections
	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_active_connections",
			Help: "Number of active connections",
		},
	)

	// RaftState tracks Raft state (1=leader, 0=follower/candidate)
	RaftState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_raft_state",
			Help: "Raft state (1=leader, 0=follower/candidate)",
		},
		[]string{"node_id", "state"},
	)

	// RaftIsLeader indicates if this node is the Raft leader
	RaftIsLeader = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_raft_is_leader",
			Help: "Whether this node is the Raft leader (1=yes, 0=no)",
		},
	)

	// MultipartUploadsActive tracks number of active multipart uploads
	MultipartUploadsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_multipart_uploads_active",
			Help: "Number of active multipart uploads",
		},
	)

	// BytesReceived tracks total bytes received
	BytesReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_bytes_received_total",
			Help: "Total bytes received",
		},
	)

	// BytesSent tracks total bytes sent
	BytesSent = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_bytes_sent_total",
			Help: "Total bytes sent",
		},
	)

	// ErrorsTotal tracks total number of errors by type
	ErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_errors_total",
			Help: "Total number of errors by type",
		},
		[]string{"operation", "error_type"},
	)

	// S3OperationsTotal tracks S3 operations by type
	S3OperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_s3_operations_total",
			Help: "Total number of S3 operations by type",
		},
		[]string{"operation", "bucket"},
	)

	// ClusterNodesTotal tracks total number of cluster nodes
	ClusterNodesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_cluster_nodes_total",
			Help: "Total number of cluster nodes",
		},
	)

	// NodeInfo provides information about this node
	NodeInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_node_info",
			Help: "Node information",
		},
		[]string{"node_id", "version"},
	)
)

// Version is set at build time
var Version = "dev"

// Init initializes the metrics system
func Init(nodeID string) {
	// Set node info
	NodeInfo.WithLabelValues(nodeID, Version).Set(1)
}

// RecordRequest records a request with its method, operation, status, and duration
func RecordRequest(method, operation string, status int, duration time.Duration) {
	statusStr := statusCodeToString(status)
	RequestsTotal.WithLabelValues(method, operation, statusStr).Inc()
	RequestDuration.WithLabelValues(method, operation).Observe(duration.Seconds())
}

// RecordS3Operation records an S3 operation
func RecordS3Operation(operation, bucket string) {
	S3OperationsTotal.WithLabelValues(operation, bucket).Inc()
}

// RecordError records an error
func RecordError(operation, errorType string) {
	ErrorsTotal.WithLabelValues(operation, errorType).Inc()
}

// SetRaftLeader sets whether this node is the Raft leader
func SetRaftLeader(isLeader bool) {
	if isLeader {
		RaftIsLeader.Set(1)
	} else {
		RaftIsLeader.Set(0)
	}
}

// SetRaftState sets the Raft state for a node
func SetRaftState(nodeID, state string) {
	// Reset all states for this node
	RaftState.Reset()
	// Set current state
	RaftState.WithLabelValues(nodeID, state).Set(1)
}

// IncrementActiveConnections increments active connections counter
func IncrementActiveConnections() {
	ActiveConnections.Inc()
}

// DecrementActiveConnections decrements active connections counter
func DecrementActiveConnections() {
	ActiveConnections.Dec()
}

// AddBytesReceived adds to bytes received counter
func AddBytesReceived(bytes int64) {
	BytesReceived.Add(float64(bytes))
}

// AddBytesSent adds to bytes sent counter
func AddBytesSent(bytes int64) {
	BytesSent.Add(float64(bytes))
}

// SetStorageStats sets storage statistics
func SetStorageStats(used, total int64) {
	StorageBytesUsed.Set(float64(used))
	StorageBytesTotal.Set(float64(total))
}

// SetBucketsTotal sets total number of buckets
func SetBucketsTotal(count int) {
	BucketsTotal.Set(float64(count))
}

// SetObjectsTotal sets total number of objects for a bucket
func SetObjectsTotal(bucket string, count int) {
	ObjectsTotal.WithLabelValues(bucket).Set(float64(count))
}

// SetMultipartUploadsActive sets number of active multipart uploads
func SetMultipartUploadsActive(count int) {
	MultipartUploadsActive.Set(float64(count))
}

// SetClusterNodesTotal sets total number of cluster nodes
func SetClusterNodesTotal(count int) {
	ClusterNodesTotal.Set(float64(count))
}

// statusCodeToString converts HTTP status code to a string category
func statusCodeToString(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500:
		return "5xx"
	default:
		return "unknown"
	}
}
