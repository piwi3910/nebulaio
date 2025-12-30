package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// =============================================================================
// S3 Express One Zone Metrics
// =============================================================================

var (
	// S3ExpressEnabled indicates if S3 Express is enabled.
	S3ExpressEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_s3express_enabled",
			Help: "Whether S3 Express One Zone is enabled (1=yes, 0=no)",
		},
	)

	// S3ExpressSessionsTotal tracks total sessions created.
	S3ExpressSessionsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_s3express_sessions_total",
			Help: "Total number of S3 Express sessions created",
		},
	)

	// S3ExpressSessionsActive tracks active sessions.
	S3ExpressSessionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_s3express_sessions_active",
			Help: "Number of active S3 Express sessions",
		},
	)

	// S3ExpressOperationsTotal tracks operations by type.
	S3ExpressOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_s3express_operations_total",
			Help: "Total S3 Express operations by type",
		},
		[]string{"operation"}, // put, list, append
	)

	// S3ExpressBytesTotal tracks bytes by operation type.
	S3ExpressBytesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_s3express_bytes_total",
			Help: "Total bytes processed by S3 Express",
		},
		[]string{"operation"}, // put, append
	)

	// S3ExpressLatency tracks operation latency.
	S3ExpressLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_s3express_latency_seconds",
			Help:    "S3 Express operation latency in seconds",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		},
		[]string{"operation"},
	)

	// S3ExpressAppendConflicts tracks append conflicts.
	S3ExpressAppendConflicts = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_s3express_append_conflicts_total",
			Help: "Total number of append conflicts",
		},
	)

	// S3ExpressLightweightETags tracks lightweight ETags used.
	S3ExpressLightweightETags = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_s3express_lightweight_etags_total",
			Help: "Total lightweight ETags generated",
		},
	)
)

// =============================================================================
// Apache Iceberg Metrics
// =============================================================================

var (
	// IcebergEnabled indicates if Iceberg is enabled.
	IcebergEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_iceberg_enabled",
			Help: "Whether Apache Iceberg is enabled (1=yes, 0=no)",
		},
	)

	// IcebergNamespacesTotal tracks total namespaces.
	IcebergNamespacesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_iceberg_namespaces_total",
			Help: "Total number of Iceberg namespaces",
		},
	)

	// IcebergTablesTotal tracks total tables.
	IcebergTablesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_iceberg_tables_total",
			Help: "Total number of Iceberg tables",
		},
	)

	// IcebergOperationsTotal tracks operations by type.
	IcebergOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_iceberg_operations_total",
			Help: "Total Iceberg operations by type",
		},
		[]string{"operation"}, // create_namespace, delete_namespace, create_table, update_table, delete_table, commit, snapshot
	)

	// IcebergCommitsTotal tracks commit outcomes.
	IcebergCommitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_iceberg_commits_total",
			Help: "Total Iceberg commits by outcome",
		},
		[]string{"outcome"}, // success, failed, conflict
	)

	// IcebergCacheHits tracks cache performance.
	IcebergCacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_iceberg_cache_hits_total",
			Help: "Total Iceberg metadata cache hits",
		},
	)

	// IcebergCacheMisses tracks cache misses.
	IcebergCacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_iceberg_cache_misses_total",
			Help: "Total Iceberg metadata cache misses",
		},
	)

	// IcebergSnapshotsTotal tracks snapshots created.
	IcebergSnapshotsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_iceberg_snapshots_total",
			Help: "Total Iceberg snapshots created",
		},
	)
)

// =============================================================================
// MCP Server Metrics
// =============================================================================

var (
	// MCPEnabled indicates if MCP is enabled.
	MCPEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_mcp_enabled",
			Help: "Whether MCP Server is enabled (1=yes, 0=no)",
		},
	)

	// MCPSessionsActive tracks active MCP sessions.
	MCPSessionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_mcp_sessions_active",
			Help: "Number of active MCP sessions",
		},
	)

	// MCPRequestsTotal tracks requests by outcome.
	MCPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_mcp_requests_total",
			Help: "Total MCP requests by outcome",
		},
		[]string{"outcome"}, // success, failed
	)

	// MCPToolInvocations tracks tool invocations.
	MCPToolInvocations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_mcp_tool_invocations_total",
			Help: "Total MCP tool invocations by tool",
		},
		[]string{"tool"},
	)

	// MCPResourceReads tracks resource reads.
	MCPResourceReads = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_mcp_resource_reads_total",
			Help: "Total MCP resource reads",
		},
	)

	// MCPBytesTransferred tracks bytes transferred.
	MCPBytesTransferred = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_mcp_bytes_transferred_total",
			Help: "Total bytes transferred via MCP",
		},
	)

	// MCPLatency tracks request latency.
	MCPLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "nebulaio_mcp_latency_seconds",
			Help:    "MCP request latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)
)

// =============================================================================
// GPUDirect Storage Metrics
// =============================================================================

var (
	// GPUDirectEnabled indicates if GPUDirect is enabled.
	GPUDirectEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_gpudirect_enabled",
			Help: "Whether GPUDirect Storage is enabled (1=yes, 0=no)",
		},
	)

	// GPUDirectOperationsTotal tracks operations by type.
	GPUDirectOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_gpudirect_operations_total",
			Help: "Total GPUDirect operations by type",
		},
		[]string{"operation"}, // read, write, batch
	)

	// GPUDirectBytesTotal tracks bytes by operation.
	GPUDirectBytesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_gpudirect_bytes_total",
			Help: "Total bytes transferred via GPUDirect",
		},
		[]string{"operation"}, // read, write
	)

	// GPUDirectLatency tracks latency by operation.
	GPUDirectLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_gpudirect_latency_seconds",
			Help:    "GPUDirect operation latency in seconds",
			Buckets: []float64{0.00001, 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
		},
		[]string{"operation"},
	)

	// GPUDirectBufferHits tracks buffer pool hits.
	GPUDirectBufferHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_gpudirect_buffer_hits_total",
			Help: "Total GPUDirect buffer pool hits",
		},
	)

	// GPUDirectBufferMisses tracks buffer pool misses.
	GPUDirectBufferMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_gpudirect_buffer_misses_total",
			Help: "Total GPUDirect buffer pool misses",
		},
	)

	// GPUDirectFallbacks tracks fallback to CPU operations.
	GPUDirectFallbacks = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_gpudirect_fallbacks_total",
			Help: "Total GPUDirect fallbacks to CPU",
		},
	)

	// GPUDirectErrors tracks errors.
	GPUDirectErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_gpudirect_errors_total",
			Help: "Total GPUDirect errors",
		},
	)
)

// =============================================================================
// BlueField DPU Metrics
// =============================================================================

var (
	// DPUEnabled indicates if DPU is enabled.
	DPUEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_dpu_enabled",
			Help: "Whether BlueField DPU is enabled (1=yes, 0=no)",
		},
	)

	// DPUOperationsTotal tracks operations by type.
	DPUOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_dpu_operations_total",
			Help: "Total DPU operations by type",
		},
		[]string{"operation"}, // crypto, compress, storage, network
	)

	// DPUBytesTotal tracks bytes by operation.
	DPUBytesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_dpu_bytes_total",
			Help: "Total bytes processed by DPU",
		},
		[]string{"operation"},
	)

	// DPULatency tracks latency by operation.
	DPULatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_dpu_latency_seconds",
			Help:    "DPU operation latency in seconds",
			Buckets: []float64{0.00001, 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
		},
		[]string{"operation"},
	)

	// DPUFallbacks tracks fallbacks to software.
	DPUFallbacks = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_dpu_fallbacks_total",
			Help: "Total DPU fallbacks to software",
		},
	)

	// DPUErrors tracks errors.
	DPUErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_dpu_errors_total",
			Help: "Total DPU errors",
		},
	)

	// DPUHealthChecks tracks health check outcomes.
	DPUHealthChecks = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_dpu_health_checks_total",
			Help: "Total DPU health checks by outcome",
		},
		[]string{"outcome"}, // success, failed
	)
)

// =============================================================================
// S3 over RDMA Metrics
// =============================================================================

var (
	// RDMAEnabled indicates if RDMA is enabled.
	RDMAEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_rdma_enabled",
			Help: "Whether S3 over RDMA is enabled (1=yes, 0=no)",
		},
	)

	// RDMAConnectionsActive tracks active RDMA connections.
	RDMAConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_rdma_connections_active",
			Help: "Number of active RDMA connections",
		},
	)

	// RDMAConnectionsTotal tracks total RDMA connections.
	RDMAConnectionsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_rdma_connections_total",
			Help: "Total RDMA connections established",
		},
	)

	// RDMARequestsTotal tracks requests by outcome.
	RDMARequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_rdma_requests_total",
			Help: "Total RDMA requests by outcome",
		},
		[]string{"outcome"}, // success, failed
	)

	// RDMABytesTotal tracks bytes by direction.
	RDMABytesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_rdma_bytes_total",
			Help: "Total bytes transferred via RDMA",
		},
		[]string{"direction"}, // sent, received
	)

	// RDMALatency tracks request latency.
	RDMALatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "nebulaio_rdma_latency_seconds",
			Help:    "RDMA request latency in seconds",
			Buckets: []float64{0.000001, 0.00001, 0.0001, 0.0005, 0.001, 0.005, 0.01},
		},
	)

	// RDMAMemoryUsed tracks RDMA memory pool usage.
	RDMAMemoryUsed = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_rdma_memory_bytes_used",
			Help: "RDMA memory pool bytes used",
		},
	)

	// RDMAMemoryTotal tracks total RDMA memory pool.
	RDMAMemoryTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_rdma_memory_bytes_total",
			Help: "RDMA memory pool total bytes",
		},
	)
)

// =============================================================================
// NVIDIA NIM Metrics
// =============================================================================

var (
	// NIMEnabled indicates if NIM is enabled.
	NIMEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_nim_enabled",
			Help: "Whether NVIDIA NIM is enabled (1=yes, 0=no)",
		},
	)

	// NIMRequestsTotal tracks requests by outcome.
	NIMRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_nim_requests_total",
			Help: "Total NIM requests by outcome",
		},
		[]string{"outcome", "model"}, // success/failed, model name
	)

	// NIMTokensTotal tracks tokens used.
	NIMTokensTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_nim_tokens_total",
			Help: "Total tokens used by NIM",
		},
		[]string{"type"}, // prompt, completion, total
	)

	// NIMLatency tracks inference latency.
	NIMLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_nim_latency_seconds",
			Help:    "NIM inference latency in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"model"},
	)

	// NIMCacheHits tracks cache hits.
	NIMCacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_nim_cache_hits_total",
			Help: "Total NIM result cache hits",
		},
	)

	// NIMCacheMisses tracks cache misses.
	NIMCacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_nim_cache_misses_total",
			Help: "Total NIM result cache misses",
		},
	)

	// NIMStreamingRequests tracks streaming requests.
	NIMStreamingRequests = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_nim_streaming_requests_total",
			Help: "Total NIM streaming requests",
		},
	)

	// NIMModelsAvailable tracks available models.
	NIMModelsAvailable = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_nim_models_available",
			Help: "Number of NIM models available",
		},
	)
)

// =============================================================================
// Feature Status Info
// =============================================================================

var (
	// AIMLFeatureInfo provides feature status information.
	AIMLFeatureInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nebulaio_aiml_feature_info",
			Help: "AI/ML feature information (1=enabled, 0=disabled)",
		},
		[]string{"feature", "version"},
	)
)

// =============================================================================
// Helper Functions
// =============================================================================

// SetS3ExpressEnabled sets the S3 Express enabled status.
func SetS3ExpressEnabled(enabled bool) {
	if enabled {
		S3ExpressEnabled.Set(1)
		AIMLFeatureInfo.WithLabelValues("s3_express", "1.0").Set(1)
	} else {
		S3ExpressEnabled.Set(0)
		AIMLFeatureInfo.WithLabelValues("s3_express", "1.0").Set(0)
	}
}

// SetIcebergEnabled sets the Iceberg enabled status.
func SetIcebergEnabled(enabled bool) {
	if enabled {
		IcebergEnabled.Set(1)
		AIMLFeatureInfo.WithLabelValues("iceberg", "1.0").Set(1)
	} else {
		IcebergEnabled.Set(0)
		AIMLFeatureInfo.WithLabelValues("iceberg", "1.0").Set(0)
	}
}

// SetMCPEnabled sets the MCP enabled status.
func SetMCPEnabled(enabled bool) {
	if enabled {
		MCPEnabled.Set(1)
		AIMLFeatureInfo.WithLabelValues("mcp", "1.0").Set(1)
	} else {
		MCPEnabled.Set(0)
		AIMLFeatureInfo.WithLabelValues("mcp", "1.0").Set(0)
	}
}

// SetGPUDirectEnabled sets the GPUDirect enabled status.
func SetGPUDirectEnabled(enabled bool) {
	if enabled {
		GPUDirectEnabled.Set(1)
		AIMLFeatureInfo.WithLabelValues("gpudirect", "1.0").Set(1)
	} else {
		GPUDirectEnabled.Set(0)
		AIMLFeatureInfo.WithLabelValues("gpudirect", "1.0").Set(0)
	}
}

// SetDPUEnabled sets the DPU enabled status.
func SetDPUEnabled(enabled bool) {
	if enabled {
		DPUEnabled.Set(1)
		AIMLFeatureInfo.WithLabelValues("dpu", "1.0").Set(1)
	} else {
		DPUEnabled.Set(0)
		AIMLFeatureInfo.WithLabelValues("dpu", "1.0").Set(0)
	}
}

// SetRDMAEnabled sets the RDMA enabled status.
func SetRDMAEnabled(enabled bool) {
	if enabled {
		RDMAEnabled.Set(1)
		AIMLFeatureInfo.WithLabelValues("rdma", "1.0").Set(1)
	} else {
		RDMAEnabled.Set(0)
		AIMLFeatureInfo.WithLabelValues("rdma", "1.0").Set(0)
	}
}

// SetNIMEnabled sets the NIM enabled status.
func SetNIMEnabled(enabled bool) {
	if enabled {
		NIMEnabled.Set(1)
		AIMLFeatureInfo.WithLabelValues("nim", "1.0").Set(1)
	} else {
		NIMEnabled.Set(0)
		AIMLFeatureInfo.WithLabelValues("nim", "1.0").Set(0)
	}
}
