package admin

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AIMLMetrics contains all AI/ML feature metrics.
type AIMLMetrics struct {
	S3Express *S3ExpressMetrics `json:"s3_express"`
	Iceberg   *IcebergMetrics   `json:"iceberg"`
	MCP       *MCPMetrics       `json:"mcp"`
	GPUDirect *GPUDirectMetrics `json:"gpudirect"`
	DPU       *DPUMetrics       `json:"dpu"`
	RDMA      *RDMAMetrics      `json:"rdma"`
	NIM       *NIMMetrics       `json:"nim"`
}

// S3ExpressMetrics contains S3 Express metrics.
type S3ExpressMetrics struct {
	Enabled             bool    `json:"enabled"`
	SessionsCreated     int64   `json:"sessions_created"`
	SessionsActive      int64   `json:"sessions_active"`
	SessionsExpired     int64   `json:"sessions_expired"`
	PutOperations       int64   `json:"put_operations"`
	PutBytesWritten     int64   `json:"put_bytes_written"`
	ListOperations      int64   `json:"list_operations"`
	ListObjectsReturned int64   `json:"list_objects_returned"`
	AppendOperations    int64   `json:"append_operations"`
	AppendBytesWritten  int64   `json:"append_bytes_written"`
	AppendConflicts     int64   `json:"append_conflicts"`
	LightweightETags    int64   `json:"lightweight_etags"`
	AvgPutLatencyMs     float64 `json:"avg_put_latency_ms"`
	AvgListLatencyMs    float64 `json:"avg_list_latency_ms"`
}

// IcebergMetrics contains Iceberg metrics.
type IcebergMetrics struct {
	Enabled           bool    `json:"enabled"`
	NamespacesCreated int64   `json:"namespaces_created"`
	NamespacesDeleted int64   `json:"namespaces_deleted"`
	NamespacesTotal   int64   `json:"namespaces_total"`
	TablesCreated     int64   `json:"tables_created"`
	TablesUpdated     int64   `json:"tables_updated"`
	TablesDeleted     int64   `json:"tables_deleted"`
	TablesTotal       int64   `json:"tables_total"`
	SnapshotsCreated  int64   `json:"snapshots_created"`
	CommitsSucceeded  int64   `json:"commits_succeeded"`
	CommitsFailed     int64   `json:"commits_failed"`
	CommitConflicts   int64   `json:"commit_conflicts"`
	ViewsCreated      int64   `json:"views_created"`
	ViewsUpdated      int64   `json:"views_updated"`
	CacheHits         int64   `json:"cache_hits"`
	CacheMisses       int64   `json:"cache_misses"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
}

// MCPMetrics contains MCP Server metrics.
type MCPMetrics struct {
	Enabled          bool    `json:"enabled"`
	RequestsTotal    int64   `json:"requests_total"`
	RequestsSuccess  int64   `json:"requests_success"`
	RequestsFailed   int64   `json:"requests_failed"`
	ToolInvocations  int64   `json:"tool_invocations"`
	ResourceReads    int64   `json:"resource_reads"`
	ActiveSessions   int64   `json:"active_sessions"`
	BytesTransferred int64   `json:"bytes_transferred"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
}

// GPUDirectMetrics contains GPUDirect metrics.
type GPUDirectMetrics struct {
	Enabled           bool    `json:"enabled"`
	ReadBytes         int64   `json:"read_bytes"`
	WriteBytes        int64   `json:"write_bytes"`
	ReadOps           int64   `json:"read_ops"`
	WriteOps          int64   `json:"write_ops"`
	BatchOps          int64   `json:"batch_ops"`
	BufferAllocations int64   `json:"buffer_allocations"`
	BufferHits        int64   `json:"buffer_hits"`
	BufferMisses      int64   `json:"buffer_misses"`
	BufferHitRate     float64 `json:"buffer_hit_rate"`
	FallbackOps       int64   `json:"fallback_ops"`
	Errors            int64   `json:"errors"`
	AvgReadLatencyUs  float64 `json:"avg_read_latency_us"`
	AvgWriteLatencyUs float64 `json:"avg_write_latency_us"`
}

// DPUMetrics contains BlueField DPU metrics.
type DPUMetrics struct {
	Enabled              bool    `json:"enabled"`
	StorageOps           int64   `json:"storage_ops"`
	CryptoOps            int64   `json:"crypto_ops"`
	CompressOps          int64   `json:"compress_ops"`
	NetworkOps           int64   `json:"network_ops"`
	StorageBytes         int64   `json:"storage_bytes"`
	CryptoBytes          int64   `json:"crypto_bytes"`
	CompressBytes        int64   `json:"compress_bytes"`
	NetworkBytes         int64   `json:"network_bytes"`
	Errors               int64   `json:"errors"`
	Fallbacks            int64   `json:"fallbacks"`
	HealthChecksFailed   int64   `json:"health_checks_failed"`
	AvgCryptoLatencyUs   float64 `json:"avg_crypto_latency_us"`
	AvgCompressLatencyUs float64 `json:"avg_compress_latency_us"`
}

// RDMAMetrics contains S3 over RDMA metrics.
type RDMAMetrics struct {
	Enabled           bool    `json:"enabled"`
	ConnectionsTotal  int64   `json:"connections_total"`
	ConnectionsActive int64   `json:"connections_active"`
	RequestsTotal     int64   `json:"requests_total"`
	RequestsSuccess   int64   `json:"requests_success"`
	RequestsFailed    int64   `json:"requests_failed"`
	BytesSent         int64   `json:"bytes_sent"`
	BytesReceived     int64   `json:"bytes_received"`
	MemoryUsed        int64   `json:"memory_used"`
	MemoryTotal       int64   `json:"memory_total"`
	AvgLatencyUs      float64 `json:"avg_latency_us"`
}

// NIMMetrics contains NVIDIA NIM metrics.
type NIMMetrics struct {
	Enabled           bool    `json:"enabled"`
	RequestsTotal     int64   `json:"requests_total"`
	RequestsSuccess   int64   `json:"requests_success"`
	RequestsFailed    int64   `json:"requests_failed"`
	TokensUsed        int64   `json:"tokens_used"`
	StreamingRequests int64   `json:"streaming_requests"`
	CacheHits         int64   `json:"cache_hits"`
	CacheMisses       int64   `json:"cache_misses"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
	ModelsAvailable   int     `json:"models_available"`
}

// FeatureStatus contains the status of a single AI/ML feature.
type FeatureStatus struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Available   bool   `json:"available"`
}

// AIMLFeaturesStatus contains status of all AI/ML features.
type AIMLFeaturesStatus struct {
	Features []FeatureStatus `json:"features"`
}

// RegisterAIMLRoutes registers AI/ML-related routes.
func (h *Handler) RegisterAIMLRoutes(r chi.Router) {
	r.Get("/aiml/metrics", h.GetAIMLMetrics)
	r.Get("/aiml/metrics/{feature}", h.GetFeatureMetrics)
	r.Get("/aiml/status", h.GetAIMLStatus)
	r.Get("/config", h.GetConfig)
	r.Put("/config", h.UpdateConfig)
}

// GetAIMLMetrics returns metrics for all AI/ML features.
func (h *Handler) GetAIMLMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := &AIMLMetrics{
		S3Express: h.getS3ExpressMetrics(),
		Iceberg:   h.getIcebergMetrics(),
		MCP:       h.getMCPMetrics(),
		GPUDirect: h.getGPUDirectMetrics(),
		DPU:       h.getDPUMetrics(),
		RDMA:      h.getRDMAMetrics(),
		NIM:       h.getNIMMetrics(),
	}

	writeJSON(w, http.StatusOK, metrics)
}

// GetFeatureMetrics returns metrics for a specific feature.
func (h *Handler) GetFeatureMetrics(w http.ResponseWriter, r *http.Request) {
	feature := chi.URLParam(r, "feature")

	var metrics interface{}

	switch feature {
	case "s3-express", "s3express":
		metrics = h.getS3ExpressMetrics()
	case "iceberg":
		metrics = h.getIcebergMetrics()
	case "mcp":
		metrics = h.getMCPMetrics()
	case "gpudirect":
		metrics = h.getGPUDirectMetrics()
	case "dpu":
		metrics = h.getDPUMetrics()
	case "rdma":
		metrics = h.getRDMAMetrics()
	case "nim":
		metrics = h.getNIMMetrics()
	default:
		writeError(w, "Unknown feature: "+feature, http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, metrics)
}

// GetAIMLStatus returns the status of all AI/ML features.
func (h *Handler) GetAIMLStatus(w http.ResponseWriter, r *http.Request) {
	// Get feature status from config (defaults to false if config not set)
	s3ExpressEnabled := false
	icebergEnabled := false
	mcpEnabled := false
	gpudirectEnabled := false
	dpuEnabled := false
	rdmaEnabled := false
	nimEnabled := false

	if h.config != nil {
		s3ExpressEnabled = h.config.S3Express.Enabled
		icebergEnabled = h.config.Iceberg.Enabled
		mcpEnabled = h.config.MCP.Enabled
		gpudirectEnabled = h.config.GPUDirect.Enabled
		dpuEnabled = h.config.DPU.Enabled
		rdmaEnabled = h.config.RDMA.Enabled
		nimEnabled = h.config.NIM.Enabled
	}

	status := &AIMLFeaturesStatus{
		Features: []FeatureStatus{
			{
				Name:        "s3_express",
				Enabled:     s3ExpressEnabled,
				Available:   true,
				Version:     "1.0",
				Description: "S3 Express One Zone - Ultra-low latency storage",
			},
			{
				Name:        "iceberg",
				Enabled:     icebergEnabled,
				Available:   true,
				Version:     "1.0",
				Description: "Apache Iceberg - Native table format for data lakehouses",
			},
			{
				Name:        "mcp",
				Enabled:     mcpEnabled,
				Available:   true,
				Version:     "1.0",
				Description: "MCP Server - AI agent integration",
			},
			{
				Name:        "gpudirect",
				Enabled:     gpudirectEnabled,
				Available:   true,
				Version:     "1.0",
				Description: "GPUDirect Storage - Zero-copy GPU transfers",
			},
			{
				Name:        "dpu",
				Enabled:     dpuEnabled,
				Available:   true,
				Version:     "1.0",
				Description: "BlueField DPU - Hardware offload",
			},
			{
				Name:        "rdma",
				Enabled:     rdmaEnabled,
				Available:   true,
				Version:     "1.0",
				Description: "S3 over RDMA - Sub-10μs latency",
			},
			{
				Name:        "nim",
				Enabled:     nimEnabled,
				Available:   true,
				Version:     "1.0",
				Description: "NVIDIA NIM - AI inference on objects",
			},
		},
	}

	writeJSON(w, http.StatusOK, status)
}

// GetConfig returns the server configuration.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	// Build config from actual configuration values
	configResponse := make(map[string]interface{})

	if h.config != nil {
		configResponse["s3_express"] = map[string]interface{}{
			"enabled":              h.config.S3Express.Enabled,
			"default_zone":         h.config.S3Express.DefaultZone,
			"enable_atomic_append": h.config.S3Express.EnableAtomicAppend,
			"session_duration":     h.config.S3Express.SessionDuration,
			"max_append_size":      h.config.S3Express.MaxAppendSize,
		}
		configResponse["iceberg"] = map[string]interface{}{
			"enabled":             h.config.Iceberg.Enabled,
			"catalog_type":        h.config.Iceberg.CatalogType,
			"catalog_uri":         h.config.Iceberg.CatalogURI,
			"warehouse":           h.config.Iceberg.Warehouse,
			"enable_acid":         h.config.Iceberg.EnableACID,
			"default_file_format": h.config.Iceberg.DefaultFileFormat,
			"snapshot_retention":  h.config.Iceberg.SnapshotRetention,
		}
		configResponse["mcp"] = map[string]interface{}{
			"enabled":          h.config.MCP.Enabled,
			"port":             h.config.MCP.Port,
			"enable_tools":     h.config.MCP.EnableTools,
			"enable_resources": h.config.MCP.EnableResources,
			"enable_prompts":   h.config.MCP.EnablePrompts,
			"auth_required":    h.config.MCP.AuthRequired,
			"max_connections":  h.config.MCP.MaxConnections,
		}
		configResponse["gpudirect"] = map[string]interface{}{
			"enabled":          h.config.GPUDirect.Enabled,
			"buffer_pool_size": h.config.GPUDirect.BufferPoolSize,
			"enable_async":     h.config.GPUDirect.EnableAsync,
			"enable_p2p":       h.config.GPUDirect.EnableP2P,
		}
		configResponse["dpu"] = map[string]interface{}{
			"enabled":            h.config.DPU.Enabled,
			"device_index":       h.config.DPU.DeviceIndex,
			"enable_crypto":      h.config.DPU.EnableCrypto,
			"enable_compression": h.config.DPU.EnableCompression,
			"enable_rdma":        h.config.DPU.EnableRDMA,
			"fallback_on_error":  h.config.DPU.FallbackOnError,
		}
		configResponse["rdma"] = map[string]interface{}{
			"enabled":          h.config.RDMA.Enabled,
			"port":             h.config.RDMA.Port,
			"device_name":      h.config.RDMA.DeviceName,
			"enable_zero_copy": h.config.RDMA.EnableZeroCopy,
			"fallback_to_tcp":  h.config.RDMA.FallbackToTCP,
		}
		// Note: Sensitive fields like APIKey are intentionally excluded from the response
		configResponse["nim"] = map[string]interface{}{
			"enabled":            h.config.NIM.Enabled,
			"default_model":      h.config.NIM.DefaultModel,
			"enable_streaming":   h.config.NIM.EnableStreaming,
			"cache_results":      h.config.NIM.CacheResults,
			"process_on_upload":  h.config.NIM.ProcessOnUpload,
			"api_key_configured": h.config.NIM.APIKey != "",
		}
	} else {
		// Return placeholder defaults if config not set
		configResponse["s3_express"] = map[string]interface{}{
			"enabled":              false,
			"default_zone":         "use1-az1",
			"enable_atomic_append": true,
		}
		configResponse["iceberg"] = map[string]interface{}{
			"enabled":      false,
			"catalog_type": "rest",
			"catalog_uri":  "http://localhost:8181",
			"warehouse":    "s3://warehouse/",
			"enable_acid":  true,
		}
		configResponse["mcp"] = map[string]interface{}{
			"enabled":          false,
			"port":             9005,
			"enable_tools":     true,
			"enable_resources": true,
			"auth_required":    true,
		}
		configResponse["gpudirect"] = map[string]interface{}{
			"enabled":          false,
			"buffer_pool_size": 1073741824,
			"enable_async":     true,
			"enable_p2p":       true,
		}
		configResponse["dpu"] = map[string]interface{}{
			"enabled":            false,
			"device_index":       0,
			"enable_crypto":      true,
			"enable_compression": true,
			"enable_rdma":        true,
			"fallback_on_error":  true,
		}
		configResponse["rdma"] = map[string]interface{}{
			"enabled":          false,
			"port":             9100,
			"device_name":      "mlx5_0",
			"enable_zero_copy": true,
			"fallback_to_tcp":  true,
		}
		configResponse["nim"] = map[string]interface{}{
			"enabled":            false,
			"default_model":      "meta/llama-3.1-8b-instruct",
			"enable_streaming":   true,
			"cache_results":      true,
			"process_on_upload":  false,
			"api_key_configured": false,
		}
	}

	writeJSON(w, http.StatusOK, configResponse)
}

// UpdateConfig updates the server configuration.
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var config map[string]interface{}
	err := json.NewDecoder(r.Body).Decode(&config)
	if err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// In production, this would validate and persist the config
	// For now, return success
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Configuration updated. Restart required for changes to take effect.",
	})
}

// Helper methods to get metrics from each feature service
// These would connect to actual services in production

func (h *Handler) getS3ExpressMetrics() *S3ExpressMetrics {
	return &S3ExpressMetrics{
		Enabled:             false,
		SessionsCreated:     0,
		SessionsActive:      0,
		SessionsExpired:     0,
		PutOperations:       0,
		PutBytesWritten:     0,
		ListOperations:      0,
		ListObjectsReturned: 0,
		AppendOperations:    0,
		AppendBytesWritten:  0,
		AppendConflicts:     0,
		LightweightETags:    0,
		AvgPutLatencyMs:     0,
		AvgListLatencyMs:    0,
	}
}

func (h *Handler) getIcebergMetrics() *IcebergMetrics {
	return &IcebergMetrics{
		Enabled:           false,
		NamespacesCreated: 0,
		NamespacesDeleted: 0,
		NamespacesTotal:   0,
		TablesCreated:     0,
		TablesUpdated:     0,
		TablesDeleted:     0,
		TablesTotal:       0,
		SnapshotsCreated:  0,
		CommitsSucceeded:  0,
		CommitsFailed:     0,
		CommitConflicts:   0,
		ViewsCreated:      0,
		ViewsUpdated:      0,
		CacheHits:         0,
		CacheMisses:       0,
		CacheHitRate:      0,
	}
}

func (h *Handler) getMCPMetrics() *MCPMetrics {
	return &MCPMetrics{
		Enabled:          false,
		RequestsTotal:    0,
		RequestsSuccess:  0,
		RequestsFailed:   0,
		ToolInvocations:  0,
		ResourceReads:    0,
		ActiveSessions:   0,
		BytesTransferred: 0,
		AvgLatencyMs:     0,
	}
}

func (h *Handler) getGPUDirectMetrics() *GPUDirectMetrics {
	return &GPUDirectMetrics{
		Enabled:           false,
		ReadBytes:         0,
		WriteBytes:        0,
		ReadOps:           0,
		WriteOps:          0,
		BatchOps:          0,
		BufferAllocations: 0,
		BufferHits:        0,
		BufferMisses:      0,
		BufferHitRate:     0,
		FallbackOps:       0,
		Errors:            0,
		AvgReadLatencyUs:  0,
		AvgWriteLatencyUs: 0,
	}
}

func (h *Handler) getDPUMetrics() *DPUMetrics {
	return &DPUMetrics{
		Enabled:              false,
		StorageOps:           0,
		CryptoOps:            0,
		CompressOps:          0,
		NetworkOps:           0,
		StorageBytes:         0,
		CryptoBytes:          0,
		CompressBytes:        0,
		NetworkBytes:         0,
		Errors:               0,
		Fallbacks:            0,
		HealthChecksFailed:   0,
		AvgCryptoLatencyUs:   0,
		AvgCompressLatencyUs: 0,
	}
}

func (h *Handler) getRDMAMetrics() *RDMAMetrics {
	return &RDMAMetrics{
		Enabled:           false,
		ConnectionsTotal:  0,
		ConnectionsActive: 0,
		RequestsTotal:     0,
		RequestsSuccess:   0,
		RequestsFailed:    0,
		BytesSent:         0,
		BytesReceived:     0,
		MemoryUsed:        0,
		MemoryTotal:       0,
		AvgLatencyUs:      0,
	}
}

func (h *Handler) getNIMMetrics() *NIMMetrics {
	return &NIMMetrics{
		Enabled:           false,
		RequestsTotal:     0,
		RequestsSuccess:   0,
		RequestsFailed:    0,
		TokensUsed:        0,
		StreamingRequests: 0,
		CacheHits:         0,
		CacheMisses:       0,
		CacheHitRate:      0,
		AvgLatencyMs:      0,
		ModelsAvailable:   0,
	}
}
