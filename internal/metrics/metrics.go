// Package metrics provides Prometheus metrics collection for NebulaIO.
//
// The package exposes metrics at /metrics (default port 9001) for monitoring:
//
// Request Metrics:
//   - nebulaio_requests_total: Total requests by operation and status
//   - nebulaio_request_duration_seconds: Request latency histogram
//   - nebulaio_request_size_bytes: Request body size histogram
//
// Storage Metrics:
//   - nebulaio_objects_total: Total objects by bucket
//   - nebulaio_bytes_stored: Total bytes stored by bucket
//   - nebulaio_storage_operations_total: Storage backend operations
//
// Cluster Metrics:
//   - nebulaio_raft_leader: Current Raft leader status
//   - nebulaio_raft_term: Current Raft term
//   - nebulaio_cluster_nodes: Number of cluster nodes
//
// Use with Prometheus and Grafana for comprehensive monitoring dashboards.
package metrics

import (
	"fmt"
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
			Help: "Current Raft state (1=leader, 0=follower/candidate)",
		},
		[]string{"shard_id"},
	)

	// RaftIsLeader indicates if this node is the Raft leader
	RaftIsLeader = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_raft_is_leader",
			Help: "Whether this node is the Raft leader",
		},
		[]string{"shard_id"},
	)

	// RaftProposalLatency tracks latency of Raft proposals
	RaftProposalLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_raft_proposal_latency_seconds",
			Help:    "Latency of Raft proposals",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 15),
		},
		[]string{"shard_id"},
	)

	// RaftProposalsTotal tracks total number of Raft proposals
	RaftProposalsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_raft_proposals_total",
			Help: "Total number of Raft proposals",
		},
		[]string{"shard_id", "status"},
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

	// PlacementGroupNodesTotal tracks number of nodes per placement group
	PlacementGroupNodesTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_placement_group_nodes_total",
			Help: "Number of nodes in each placement group",
		},
		[]string{"group_id", "datacenter", "region"},
	)

	// PlacementGroupStatus tracks placement group status (1=healthy, 0=degraded/offline)
	PlacementGroupStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_placement_group_status",
			Help: "Placement group status (1=healthy, 0=degraded/offline)",
		},
		[]string{"group_id", "status"},
	)

	// PlacementGroupShardDistribution tracks shard distribution across nodes
	PlacementGroupShardDistribution = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_placement_group_shard_distribution",
			Help: "Number of shards on each node",
		},
		[]string{"group_id", "node_id"},
	)

	// PlacementGroupInfo provides information about placement groups
	PlacementGroupInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_placement_group_info",
			Help: "Placement group information",
		},
		[]string{"group_id", "name", "datacenter", "region", "is_local"},
	)

	// ErasureEncodeOperationsTotal tracks total erasure encoding operations
	ErasureEncodeOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_erasure_encode_operations_total",
			Help: "Total number of erasure encoding operations",
		},
		[]string{"group_id", "status"},
	)

	// ErasureDecodeOperationsTotal tracks total erasure decoding operations
	ErasureDecodeOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_erasure_decode_operations_total",
			Help: "Total number of erasure decoding operations",
		},
		[]string{"group_id", "status"},
	)

	// ErasureReconstructOperationsTotal tracks total erasure reconstruction operations
	ErasureReconstructOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_erasure_reconstruct_operations_total",
			Help: "Total number of erasure reconstruction operations",
		},
		[]string{"group_id"},
	)

	// ErasureEncodeDurationSeconds tracks erasure encoding duration
	ErasureEncodeDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_erasure_encode_duration_seconds",
			Help:    "Duration of erasure encoding operations",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
		},
		[]string{"group_id"},
	)

	// ErasureHealthyObjects tracks number of healthy (fully redundant) objects
	ErasureHealthyObjects = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_erasure_healthy_objects",
			Help: "Number of healthy objects with full redundancy",
		},
		[]string{"group_id"},
	)

	// ErasureDegradedObjects tracks number of degraded objects (missing some shards)
	ErasureDegradedObjects = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_erasure_degraded_objects",
			Help: "Number of degraded objects missing some shards but still recoverable",
		},
		[]string{"group_id"},
	)

	// ErasureObjectsAtRisk tracks objects at risk (close to losing recoverability)
	ErasureObjectsAtRisk = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_erasure_objects_at_risk",
			Help: "Number of objects at risk of becoming unrecoverable",
		},
		[]string{"group_id"},
	)

	// TieringTransitionsTotal tracks tiering transitions between tiers
	TieringTransitionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_tiering_transitions_total",
			Help: "Total number of objects transitioned between tiers",
		},
		[]string{"source_tier", "destination_tier", "policy"},
	)

	// TieringObjectsPerTier tracks number of objects in each tier
	TieringObjectsPerTier = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_tiering_objects_total",
			Help: "Number of objects in each storage tier",
		},
		[]string{"tier"},
	)

	// TieringBytesPerTier tracks bytes stored in each tier
	TieringBytesPerTier = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_tiering_bytes_total",
			Help: "Bytes stored in each storage tier",
		},
		[]string{"tier"},
	)

	// CachePeerWriteFailures tracks failed attempts to cache entries from peer nodes
	CachePeerWriteFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_cache_peer_write_failures_total",
			Help: "Total failed attempts to cache entries from peer nodes",
		},
	)

	// CacheHits tracks cache hit count
	CacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_cache_hits_total",
			Help: "Total cache hits",
		},
	)

	// CacheMisses tracks cache miss count
	CacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_cache_misses_total",
			Help: "Total cache misses",
		},
	)

	// BucketRollbackFailures tracks bucket rollback failures
	BucketRollbackFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_bucket_rollback_failures_total",
			Help: "Total failed bucket rollback operations",
		},
	)

	// BucketCreationFailures tracks bucket creation failures
	BucketCreationFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_bucket_creation_failures_total",
			Help: "Total failed bucket creation operations",
		},
	)

	// MetadataAppliedIndexErrors tracks metadata applied index errors
	MetadataAppliedIndexErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_metadata_applied_index_errors_total",
			Help: "Total metadata applied index errors",
		},
	)

	// MetadataCloseErrors tracks metadata close errors
	MetadataCloseErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_metadata_close_errors_total",
			Help: "Total metadata close errors",
		},
	)

	// KeyRotationFailures tracks key rotation failures
	KeyRotationFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_key_rotation_failures_total",
			Help: "Total key rotation failures",
		},
	)

	// KeyExpiryNotificationFailures tracks key expiry notification failures
	KeyExpiryNotificationFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_key_expiry_notification_failures_total",
			Help: "Total key expiry notification failures",
		},
	)

	// MigrationJobSaveFailures tracks migration job save failures
	MigrationJobSaveFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_migration_job_save_failures_total",
			Help: "Total migration job save failures",
		},
	)

	// MigrationFailedObjectLogFailures tracks migration failed object log failures
	MigrationFailedObjectLogFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_migration_failed_object_log_failures_total",
			Help: "Total migration failed object log failures",
		},
	)

	// BackupWALSyncFailures tracks backup WAL sync failures
	BackupWALSyncFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_backup_wal_sync_failures_total",
			Help: "Total backup WAL sync failures",
		},
	)

	// LambdaCompressionOperations tracks Lambda compression/decompression operations
	LambdaCompressionOperations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_lambda_compression_operations_total",
			Help: "Total Lambda compression/decompression operations",
		},
		[]string{"algorithm", "direction", "status"},
	)

	// LambdaCompressionDuration tracks Lambda compression operation duration
	LambdaCompressionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_lambda_compression_duration_seconds",
			Help:    "Duration of Lambda compression/decompression operations",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
		},
		[]string{"algorithm", "direction"},
	)

	// LambdaCompressionRatio tracks compression effectiveness
	LambdaCompressionRatio = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_lambda_compression_ratio",
			Help:    "Compression ratio (original/compressed size)",
			Buckets: []float64{1.0, 1.5, 2.0, 3.0, 5.0, 10.0, 20.0, 50.0, 100.0},
		},
		[]string{"algorithm"},
	)

	// LambdaOperationsInFlight tracks concurrent Lambda operations
	LambdaOperationsInFlight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_lambda_operations_in_flight",
			Help: "Number of Lambda operations currently in progress",
		},
		[]string{"algorithm"},
	)

	// ObjectCreationRateLimited tracks rate-limited object creation attempts
	ObjectCreationRateLimited = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_object_creation_rate_limited_total",
			Help: "Total object creation attempts that were rate limited",
		},
		[]string{"bucket", "reason"},
	)

	// LambdaMaxTransformSize tracks the configured maximum transform size for Lambda operations
	LambdaMaxTransformSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_lambda_max_transform_size_bytes",
			Help: "Configured maximum transform size for Lambda operations in bytes",
		},
	)

	// LambdaStreamingThreshold tracks the configured streaming threshold for Lambda operations
	LambdaStreamingThreshold = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_lambda_streaming_threshold_bytes",
			Help: "Configured streaming threshold for Lambda operations in bytes",
		},
	)

	// LambdaBytesProcessed tracks total bytes processed by Lambda compression/decompression
	LambdaBytesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_lambda_compression_bytes_processed_total",
			Help: "Total bytes processed by Lambda compression/decompression operations",
		},
		[]string{"algorithm", "direction"},
	)

	// VolumeCompactionReclaimableBytes tracks bytes that can be reclaimed via compaction
	VolumeCompactionReclaimableBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_volume_compaction_reclaimable_bytes",
			Help: "Bytes that can be reclaimed via volume compaction",
		},
		[]string{"volume_id"},
	)

	// VolumeCompactionDeletedObjects tracks objects marked as deleted but not yet reclaimed
	VolumeCompactionDeletedObjects = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_volume_compaction_deleted_objects",
			Help: "Number of objects marked as deleted but not yet reclaimed",
		},
		[]string{"volume_id"},
	)

	// VolumeCompactionReplacedObjects tracks objects that were replaced (creating garbage)
	VolumeCompactionReplacedObjects = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_volume_compaction_replaced_objects_total",
			Help: "Total number of objects replaced, creating garbage for compaction",
		},
		[]string{"volume_id"},
	)

	// VolumeCompactionLastRun tracks the timestamp of the last compaction run
	VolumeCompactionLastRun = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_volume_compaction_last_run_timestamp_seconds",
			Help: "Unix timestamp of the last compaction run",
		},
		[]string{"volume_id"},
	)

	// VolumeCompactionDuration tracks the duration of compaction operations
	VolumeCompactionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_volume_compaction_duration_seconds",
			Help:    "Duration of volume compaction operations",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~102s
		},
		[]string{"volume_id"},
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

// SetRaftLeader sets whether this node is the Raft leader for a specific shard
func SetRaftLeader(shardID string, isLeader bool) {
	if isLeader {
		RaftIsLeader.WithLabelValues(shardID).Set(1)
		RaftState.WithLabelValues(shardID).Set(1)
	} else {
		RaftIsLeader.WithLabelValues(shardID).Set(0)
		RaftState.WithLabelValues(shardID).Set(0)
	}
}

// RecordRaftProposal records a Raft proposal with its duration and status
func RecordRaftProposal(shardID string, duration time.Duration, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	RaftProposalLatency.WithLabelValues(shardID).Observe(duration.Seconds())
	RaftProposalsTotal.WithLabelValues(shardID, status).Inc()
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

// SetPlacementGroupNodes sets the number of nodes in a placement group
func SetPlacementGroupNodes(groupID, datacenter, region string, count int) {
	PlacementGroupNodesTotal.WithLabelValues(groupID, datacenter, region).Set(float64(count))
}

// SetPlacementGroupStatusMetric sets the status of a placement group
func SetPlacementGroupStatusMetric(groupID, status string) {
	// Reset all status values for this group first
	PlacementGroupStatus.WithLabelValues(groupID, "healthy").Set(0)
	PlacementGroupStatus.WithLabelValues(groupID, "degraded").Set(0)
	PlacementGroupStatus.WithLabelValues(groupID, "offline").Set(0)
	PlacementGroupStatus.WithLabelValues(groupID, "unknown").Set(0)
	// Set the current status
	PlacementGroupStatus.WithLabelValues(groupID, status).Set(1)
}

// SetPlacementGroupInfo sets placement group information
func SetPlacementGroupInfo(groupID, name, datacenter, region string, isLocal bool) {
	isLocalStr := "false"
	if isLocal {
		isLocalStr = "true"
	}
	PlacementGroupInfo.WithLabelValues(groupID, name, datacenter, region, isLocalStr).Set(1)
}

// SetPlacementGroupShardCount sets the shard count for a node in a placement group
func SetPlacementGroupShardCount(groupID, nodeID string, count int) {
	PlacementGroupShardDistribution.WithLabelValues(groupID, nodeID).Set(float64(count))
}

// RecordErasureEncode records an erasure encoding operation
func RecordErasureEncode(groupID string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "failure"
	}
	ErasureEncodeOperationsTotal.WithLabelValues(groupID, status).Inc()
	if success {
		ErasureEncodeDurationSeconds.WithLabelValues(groupID).Observe(duration.Seconds())
	}
}

// RecordErasureDecode records an erasure decoding operation
func RecordErasureDecode(groupID string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	ErasureDecodeOperationsTotal.WithLabelValues(groupID, status).Inc()
}

// RecordErasureReconstruct records an erasure reconstruction operation
func RecordErasureReconstruct(groupID string) {
	ErasureReconstructOperationsTotal.WithLabelValues(groupID).Inc()
}

// SetErasureHealthMetrics sets erasure coding health metrics
func SetErasureHealthMetrics(groupID string, healthy, degraded, atRisk int) {
	ErasureHealthyObjects.WithLabelValues(groupID).Set(float64(healthy))
	ErasureDegradedObjects.WithLabelValues(groupID).Set(float64(degraded))
	ErasureObjectsAtRisk.WithLabelValues(groupID).Set(float64(atRisk))
}

// RecordTieringTransition records a tiering transition
func RecordTieringTransition(sourceTier, destTier, policy string) {
	TieringTransitionsTotal.WithLabelValues(sourceTier, destTier, policy).Inc()
}

// SetTieringMetrics sets tiering metrics for a tier
func SetTieringMetrics(tier string, objects int64, bytes int64) {
	TieringObjectsPerTier.WithLabelValues(tier).Set(float64(objects))
	TieringBytesPerTier.WithLabelValues(tier).Set(float64(bytes))
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

// DragonboatStore is an interface to avoid circular dependencies
// This should match the relevant methods from internal/metadata.DragonboatStore
type DragonboatStore interface {
	GetNodeHost() interface{} // Returns *dragonboat.NodeHost
	IsLeader() bool
}

// DragonboatNodeHost is an interface for the Dragonboat NodeHost methods we need
type DragonboatNodeHost interface {
	GetLeaderID(shardID uint64) (uint64, uint64, error)
}

// CollectDragonboatMetrics collects and updates Raft metrics from a DragonboatStore
// This function should be called periodically to update Raft metrics
func CollectDragonboatMetrics(shardID uint64, nodeHost DragonboatNodeHost, replicaID uint64) {
	shardLabel := fmt.Sprintf("%d", shardID)

	leaderID, _, err := nodeHost.GetLeaderID(shardID)
	if err != nil {
		// If we can't get leader info, set metrics to 0
		RaftState.WithLabelValues(shardLabel).Set(0)
		RaftIsLeader.WithLabelValues(shardLabel).Set(0)
		return
	}

	isLeader := leaderID == replicaID

	if isLeader {
		RaftState.WithLabelValues(shardLabel).Set(1)
		RaftIsLeader.WithLabelValues(shardLabel).Set(1)
	} else {
		RaftState.WithLabelValues(shardLabel).Set(0)
		RaftIsLeader.WithLabelValues(shardLabel).Set(0)
	}
}

// RecordCachePeerWriteFailure increments the cache peer write failure counter
func RecordCachePeerWriteFailure() {
	CachePeerWriteFailures.Inc()
}

// RecordCacheHit increments the cache hit counter
func RecordCacheHit() {
	CacheHits.Inc()
}

// RecordCacheMiss increments the cache miss counter
func RecordCacheMiss() {
	CacheMisses.Inc()
}

// RecordBucketRollbackFailure increments the bucket rollback failure counter
func RecordBucketRollbackFailure() {
	BucketRollbackFailures.Inc()
}

// RecordBucketCreationFailure increments the bucket creation failure counter
func RecordBucketCreationFailure() {
	BucketCreationFailures.Inc()
}

// RecordMetadataAppliedIndexError increments the metadata applied index error counter
func RecordMetadataAppliedIndexError() {
	MetadataAppliedIndexErrors.Inc()
}

// RecordMetadataCloseError increments the metadata close error counter
func RecordMetadataCloseError() {
	MetadataCloseErrors.Inc()
}

// RecordKeyRotationFailure increments the key rotation failure counter
func RecordKeyRotationFailure() {
	KeyRotationFailures.Inc()
}

// RecordKeyExpiryNotificationFailure increments the key expiry notification failure counter
func RecordKeyExpiryNotificationFailure() {
	KeyExpiryNotificationFailures.Inc()
}

// RecordMigrationJobSaveFailure increments the migration job save failure counter
func RecordMigrationJobSaveFailure() {
	MigrationJobSaveFailures.Inc()
}

// RecordMigrationFailedObjectLogFailure increments the migration failed object log failure counter
func RecordMigrationFailedObjectLogFailure() {
	MigrationFailedObjectLogFailures.Inc()
}

// RecordBackupWALSyncFailure increments the backup WAL sync failure counter
func RecordBackupWALSyncFailure() {
	BackupWALSyncFailures.Inc()
}

// RecordLambdaCompression records a Lambda compression/decompression operation
func RecordLambdaCompression(algorithm, direction string, success bool, duration time.Duration, originalSize, compressedSize int64) {
	status := "success"
	if !success {
		status = "error"
	}
	LambdaCompressionOperations.WithLabelValues(algorithm, direction, status).Inc()
	if success {
		LambdaCompressionDuration.WithLabelValues(algorithm, direction).Observe(duration.Seconds())
		if direction == "compress" && compressedSize > 0 {
			ratio := float64(originalSize) / float64(compressedSize)
			LambdaCompressionRatio.WithLabelValues(algorithm).Observe(ratio)
		}
	}
}

// IncrementLambdaOperationsInFlight increments the in-flight Lambda operations gauge
func IncrementLambdaOperationsInFlight(algorithm string) {
	LambdaOperationsInFlight.WithLabelValues(algorithm).Inc()
}

// DecrementLambdaOperationsInFlight decrements the in-flight Lambda operations gauge
func DecrementLambdaOperationsInFlight(algorithm string) {
	LambdaOperationsInFlight.WithLabelValues(algorithm).Dec()
}

// RecordObjectCreationRateLimited records a rate-limited object creation attempt
func RecordObjectCreationRateLimited(bucket, reason string) {
	ObjectCreationRateLimited.WithLabelValues(bucket, reason).Inc()
}

// SetLambdaMaxTransformSize sets the configured max transform size metric
func SetLambdaMaxTransformSize(size int64) {
	LambdaMaxTransformSize.Set(float64(size))
}

// SetLambdaStreamingThreshold sets the configured streaming threshold metric
func SetLambdaStreamingThreshold(size int64) {
	LambdaStreamingThreshold.Set(float64(size))
}

// RecordLambdaBytesProcessed records bytes processed by Lambda operations
func RecordLambdaBytesProcessed(algorithm, direction string, bytes int64) {
	LambdaBytesProcessed.WithLabelValues(algorithm, direction).Add(float64(bytes))
}

// SetVolumeCompactionReclaimableBytes sets the reclaimable bytes for a volume
func SetVolumeCompactionReclaimableBytes(volumeID string, bytes uint64) {
	VolumeCompactionReclaimableBytes.WithLabelValues(volumeID).Set(float64(bytes))
}

// SetVolumeCompactionDeletedObjects sets the count of deleted objects pending compaction
func SetVolumeCompactionDeletedObjects(volumeID string, count uint64) {
	VolumeCompactionDeletedObjects.WithLabelValues(volumeID).Set(float64(count))
}

// IncrementVolumeCompactionReplacedObjects increments the replaced objects counter
func IncrementVolumeCompactionReplacedObjects(volumeID string) {
	VolumeCompactionReplacedObjects.WithLabelValues(volumeID).Inc()
}

// SetVolumeCompactionLastRun sets the timestamp of the last compaction run
func SetVolumeCompactionLastRun(volumeID string, timestamp float64) {
	VolumeCompactionLastRun.WithLabelValues(volumeID).Set(timestamp)
}

// ObserveVolumeCompactionDuration records the duration of a compaction operation
func ObserveVolumeCompactionDuration(volumeID string, duration float64) {
	VolumeCompactionDuration.WithLabelValues(volumeID).Observe(duration)
}
