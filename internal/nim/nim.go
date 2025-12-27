// Package nim implements NVIDIA NIM (NVIDIA Inference Microservices) integration for NebulaIO.
//
// NVIDIA NIM provides GPU-accelerated inference services for AI models. This integration enables:
// - Direct inference on objects stored in NebulaIO
// - Streaming inference results back to storage
// - Batch processing of datasets
// - Model caching and optimization
// - Multi-model orchestration
//
// Supported model types:
// - LLMs (Large Language Models) - text generation, chat, embeddings
// - Vision models - image classification, object detection, segmentation
// - Audio models - speech-to-text, text-to-speech
// - Multimodal models - vision-language models
//
// Architecture:
//
//	┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
//	│   NebulaIO S3   │───►│   NIM Client    │───►│   NIM Server    │
//	│   (Objects)     │    │   (this pkg)    │    │   (NVIDIA)      │
//	└─────────────────┘    └─────────────────┘    └─────────────────┘
//	                              │                       │
//	                              │    GPU Inference      │
//	                              │◄──────────────────────┘
//	                              │
//	                              ▼
//	                       ┌─────────────────┐
//	                       │  Results Store  │
//	                       │  (NebulaIO S3)  │
//	                       └─────────────────┘
package nim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Common errors
var (
	ErrNIMNotAvailable     = errors.New("nim service not available")
	ErrModelNotFound       = errors.New("model not found")
	ErrInferenceTimeout    = errors.New("inference timeout")
	ErrInvalidInput        = errors.New("invalid input for model")
	ErrQuotaExceeded       = errors.New("inference quota exceeded")
	ErrModelLoading        = errors.New("model is still loading")
	ErrServiceUnavailable  = errors.New("nim service unavailable")
	ErrAlreadyClosed       = errors.New("nim client already closed")
	ErrBatchTooLarge       = errors.New("batch size exceeds limit")
)

// ModelType represents the type of AI model
type ModelType string

const (
	ModelTypeLLM        ModelType = "llm"
	ModelTypeVision     ModelType = "vision"
	ModelTypeAudio      ModelType = "audio"
	ModelTypeMultimodal ModelType = "multimodal"
	ModelTypeEmbedding  ModelType = "embedding"
)

// Config holds NIM client configuration
type Config struct {
	// Endpoints are NIM server endpoints (supports multiple for HA)
	Endpoints []string `json:"endpoints" yaml:"endpoints"`

	// APIKey is the NVIDIA API key for authentication
	APIKey string `json:"api_key" yaml:"api_key"`

	// OrganizationID is the NVIDIA organization ID
	OrganizationID string `json:"organization_id" yaml:"organization_id"`

	// DefaultModel is the default model to use
	DefaultModel string `json:"default_model" yaml:"default_model"`

	// Timeout for inference requests
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// MaxRetries for failed requests
	MaxRetries int `json:"max_retries" yaml:"max_retries"`

	// MaxBatchSize for batch inference
	MaxBatchSize int `json:"max_batch_size" yaml:"max_batch_size"`

	// EnableStreaming enables streaming responses
	EnableStreaming bool `json:"enable_streaming" yaml:"enable_streaming"`

	// CacheResults enables caching of inference results
	CacheResults bool `json:"cache_results" yaml:"cache_results"`

	// CacheTTL is the TTL for cached results
	CacheTTL time.Duration `json:"cache_ttl" yaml:"cache_ttl"`

	// EnableMetrics enables detailed metrics collection
	EnableMetrics bool `json:"enable_metrics" yaml:"enable_metrics"`
}

// DefaultConfig returns default NIM configuration
func DefaultConfig() *Config {
	return &Config{
		Endpoints:       []string{"https://integrate.api.nvidia.com/v1"},
		Timeout:         60 * time.Second,
		MaxRetries:      3,
		MaxBatchSize:    100,
		EnableStreaming: true,
		CacheResults:    true,
		CacheTTL:        1 * time.Hour,
		EnableMetrics:   true,
	}
}

// ModelInfo contains information about a NIM model
type ModelInfo struct {
	// ID is the model identifier
	ID string `json:"id"`

	// Name is the human-readable model name
	Name string `json:"name"`

	// Type is the model type (llm, vision, audio, etc.)
	Type ModelType `json:"type"`

	// Version is the model version
	Version string `json:"version"`

	// Description describes the model capabilities
	Description string `json:"description"`

	// MaxTokens is the maximum tokens for LLMs
	MaxTokens int `json:"max_tokens,omitempty"`

	// MaxBatchSize is the maximum batch size
	MaxBatchSize int `json:"max_batch_size"`

	// SupportedFormats are input formats for vision/audio
	SupportedFormats []string `json:"supported_formats,omitempty"`

	// Status is the model status (ready, loading, error)
	Status string `json:"status"`

	// Latency is the average inference latency
	AvgLatencyMs int `json:"avg_latency_ms"`
}

// InferenceRequest represents an inference request
type InferenceRequest struct {
	// Model is the model ID to use
	Model string `json:"model"`

	// Input is the input data (text, image URL, audio URL, etc.)
	Input interface{} `json:"input"`

	// Parameters are model-specific parameters
	Parameters map[string]interface{} `json:"parameters,omitempty"`

	// Stream enables streaming response
	Stream bool `json:"stream,omitempty"`

	// MaxTokens limits output tokens for LLMs
	MaxTokens int `json:"max_tokens,omitempty"`

	// Temperature controls randomness for LLMs
	Temperature float64 `json:"temperature,omitempty"`

	// TopP is nucleus sampling parameter
	TopP float64 `json:"top_p,omitempty"`
}

// ChatMessage represents a chat message for LLM inference
type ChatMessage struct {
	Role    string `json:"role"`    // "system", "user", "assistant"
	Content string `json:"content"`
}

// ChatRequest represents a chat completion request
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	TopP        float64       `json:"top_p,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
}

// ChatResponse represents a chat completion response
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// EmbeddingRequest represents an embedding generation request
type EmbeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"` // "float" or "base64"
}

// EmbeddingResponse represents an embedding response
type EmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// VisionRequest represents a vision inference request
type VisionRequest struct {
	Model      string `json:"model"`
	ImageURL   string `json:"image_url,omitempty"`
	ImageBytes []byte `json:"image_bytes,omitempty"`
	Task       string `json:"task"` // "classification", "detection", "segmentation"
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// VisionResponse represents a vision inference response
type VisionResponse struct {
	Model   string `json:"model"`
	Results []struct {
		Label      string  `json:"label,omitempty"`
		Confidence float64 `json:"confidence,omitempty"`
		BoundingBox *struct {
			X1 float64 `json:"x1"`
			Y1 float64 `json:"y1"`
			X2 float64 `json:"x2"`
			Y2 float64 `json:"y2"`
		} `json:"bounding_box,omitempty"`
		Mask [][]int `json:"mask,omitempty"`
	} `json:"results"`
	Latency int `json:"latency_ms"`
}

// InferenceResponse represents a generic inference response
type InferenceResponse struct {
	// ID is the response ID
	ID string `json:"id"`

	// Model is the model used
	Model string `json:"model"`

	// Output is the inference output
	Output interface{} `json:"output"`

	// Latency is the inference latency in milliseconds
	LatencyMs int `json:"latency_ms"`

	// TokensUsed is for LLM responses
	TokensUsed int `json:"tokens_used,omitempty"`

	// Error is set if inference failed
	Error string `json:"error,omitempty"`
}

// BatchRequest represents a batch inference request
type BatchRequest struct {
	// Model is the model to use
	Model string `json:"model"`

	// Requests are individual inference requests
	Requests []InferenceRequest `json:"requests"`

	// Parallel enables parallel processing
	Parallel bool `json:"parallel"`
}

// BatchResponse represents a batch inference response
type BatchResponse struct {
	// Responses are individual inference responses
	Responses []InferenceResponse `json:"responses"`

	// TotalLatencyMs is the total processing time
	TotalLatencyMs int `json:"total_latency_ms"`

	// SuccessCount is the number of successful inferences
	SuccessCount int `json:"success_count"`

	// FailureCount is the number of failed inferences
	FailureCount int `json:"failure_count"`
}

// Metrics holds NIM client metrics
type Metrics struct {
	RequestsTotal    int64 `json:"requests_total"`
	RequestsSuccess  int64 `json:"requests_success"`
	RequestsFailed   int64 `json:"requests_failed"`
	TokensUsed       int64 `json:"tokens_used"`
	TotalLatencyMs   int64 `json:"total_latency_ms"`
	AvgLatencyMs     int64 `json:"avg_latency_ms"`
	CacheHits        int64 `json:"cache_hits"`
	CacheMisses      int64 `json:"cache_misses"`
	StreamingRequests int64 `json:"streaming_requests"`
}

// Client provides NIM inference capabilities
type Client struct {
	config     *Config
	httpClient *http.Client
	mu         sync.RWMutex
	closed     atomic.Bool
	metrics    *Metrics
	cache      map[string]*cachedResult
	models     map[string]*ModelInfo
	backend    NIMBackend
}

type cachedResult struct {
	response  *InferenceResponse
	expiresAt time.Time
}

// NIMBackend defines the interface for NIM operations
// This allows for actual API calls or simulation for testing
type NIMBackend interface {
	// ListModels returns available models
	ListModels(ctx context.Context) ([]*ModelInfo, error)

	// GetModel returns info about a specific model
	GetModel(ctx context.Context, modelID string) (*ModelInfo, error)

	// Chat performs chat completion
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// ChatStream performs streaming chat completion
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error)

	// Embed generates embeddings
	Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)

	// Vision performs vision inference
	Vision(ctx context.Context, req *VisionRequest) (*VisionResponse, error)

	// Infer performs generic inference
	Infer(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error)

	// Batch performs batch inference
	Batch(ctx context.Context, req *BatchRequest) (*BatchResponse, error)

	// HealthCheck checks NIM service health
	HealthCheck(ctx context.Context) error

	// Close closes the backend
	Close() error
}

// NewClient creates a new NIM client
func NewClient(config *Config, backend NIMBackend) (*Client, error) {
	if config == nil {
		config = DefaultConfig()
	}
	if backend == nil {
		backend = NewSimulatedBackend()
	}

	c := &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		metrics: &Metrics{},
		cache:   make(map[string]*cachedResult),
		models:  make(map[string]*ModelInfo),
		backend: backend,
	}

	// Load available models
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := backend.ListModels(ctx)
	if err != nil {
		// Non-fatal: models can be loaded later
	} else {
		for _, m := range models {
			c.models[m.ID] = m
		}
	}

	return c, nil
}

// ListModels returns available NIM models
func (c *Client) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	if c.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	return c.backend.ListModels(ctx)
}

// GetModel returns information about a specific model
func (c *Client) GetModel(ctx context.Context, modelID string) (*ModelInfo, error) {
	if c.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	c.mu.RLock()
	if m, ok := c.models[modelID]; ok {
		c.mu.RUnlock()
		return m, nil
	}
	c.mu.RUnlock()

	return c.backend.GetModel(ctx, modelID)
}

// Chat performs a chat completion request
func (c *Client) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if c.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	start := time.Now()
	atomic.AddInt64(&c.metrics.RequestsTotal, 1)

	resp, err := c.backend.Chat(ctx, req)
	if err != nil {
		atomic.AddInt64(&c.metrics.RequestsFailed, 1)
		return nil, err
	}

	atomic.AddInt64(&c.metrics.RequestsSuccess, 1)
	latency := time.Since(start).Milliseconds()
	atomic.AddInt64(&c.metrics.TotalLatencyMs, latency)
	if resp.Usage.TotalTokens > 0 {
		atomic.AddInt64(&c.metrics.TokensUsed, int64(resp.Usage.TotalTokens))
	}

	return resp, nil
}

// ChatStream performs a streaming chat completion
func (c *Client) ChatStream(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	if c.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	atomic.AddInt64(&c.metrics.RequestsTotal, 1)
	atomic.AddInt64(&c.metrics.StreamingRequests, 1)

	return c.backend.ChatStream(ctx, req)
}

// Embed generates embeddings for the given input
func (c *Client) Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	if c.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	atomic.AddInt64(&c.metrics.RequestsTotal, 1)

	resp, err := c.backend.Embed(ctx, req)
	if err != nil {
		atomic.AddInt64(&c.metrics.RequestsFailed, 1)
		return nil, err
	}

	atomic.AddInt64(&c.metrics.RequestsSuccess, 1)
	return resp, nil
}

// Vision performs vision inference
func (c *Client) Vision(ctx context.Context, req *VisionRequest) (*VisionResponse, error) {
	if c.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	atomic.AddInt64(&c.metrics.RequestsTotal, 1)

	resp, err := c.backend.Vision(ctx, req)
	if err != nil {
		atomic.AddInt64(&c.metrics.RequestsFailed, 1)
		return nil, err
	}

	atomic.AddInt64(&c.metrics.RequestsSuccess, 1)
	return resp, nil
}

// Infer performs generic inference
func (c *Client) Infer(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	if c.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	// Check cache
	if c.config.CacheResults {
		cacheKey := c.getCacheKey(req)
		c.mu.RLock()
		if cached, ok := c.cache[cacheKey]; ok && time.Now().Before(cached.expiresAt) {
			c.mu.RUnlock()
			atomic.AddInt64(&c.metrics.CacheHits, 1)
			return cached.response, nil
		}
		c.mu.RUnlock()
		atomic.AddInt64(&c.metrics.CacheMisses, 1)
	}

	atomic.AddInt64(&c.metrics.RequestsTotal, 1)

	resp, err := c.backend.Infer(ctx, req)
	if err != nil {
		atomic.AddInt64(&c.metrics.RequestsFailed, 1)
		return nil, err
	}

	atomic.AddInt64(&c.metrics.RequestsSuccess, 1)

	// Cache result
	if c.config.CacheResults {
		cacheKey := c.getCacheKey(req)
		c.mu.Lock()
		c.cache[cacheKey] = &cachedResult{
			response:  resp,
			expiresAt: time.Now().Add(c.config.CacheTTL),
		}
		c.mu.Unlock()
	}

	return resp, nil
}

// Batch performs batch inference
func (c *Client) Batch(ctx context.Context, req *BatchRequest) (*BatchResponse, error) {
	if c.closed.Load() {
		return nil, ErrAlreadyClosed
	}

	if len(req.Requests) > c.config.MaxBatchSize {
		return nil, ErrBatchTooLarge
	}

	atomic.AddInt64(&c.metrics.RequestsTotal, int64(len(req.Requests)))

	resp, err := c.backend.Batch(ctx, req)
	if err != nil {
		atomic.AddInt64(&c.metrics.RequestsFailed, int64(len(req.Requests)))
		return nil, err
	}

	atomic.AddInt64(&c.metrics.RequestsSuccess, int64(resp.SuccessCount))
	atomic.AddInt64(&c.metrics.RequestsFailed, int64(resp.FailureCount))

	return resp, nil
}

// getCacheKey generates a cache key for a request
func (c *Client) getCacheKey(req *InferenceRequest) string {
	data, _ := json.Marshal(req)
	return fmt.Sprintf("%x", data)
}

// GetMetrics returns client metrics
func (c *Client) GetMetrics() *Metrics {
	total := atomic.LoadInt64(&c.metrics.RequestsTotal)
	totalLatency := atomic.LoadInt64(&c.metrics.TotalLatencyMs)
	avgLatency := int64(0)
	if total > 0 {
		avgLatency = totalLatency / total
	}

	return &Metrics{
		RequestsTotal:    total,
		RequestsSuccess:  atomic.LoadInt64(&c.metrics.RequestsSuccess),
		RequestsFailed:   atomic.LoadInt64(&c.metrics.RequestsFailed),
		TokensUsed:       atomic.LoadInt64(&c.metrics.TokensUsed),
		TotalLatencyMs:   totalLatency,
		AvgLatencyMs:     avgLatency,
		CacheHits:        atomic.LoadInt64(&c.metrics.CacheHits),
		CacheMisses:      atomic.LoadInt64(&c.metrics.CacheMisses),
		StreamingRequests: atomic.LoadInt64(&c.metrics.StreamingRequests),
	}
}

// HealthCheck checks if NIM service is available
func (c *Client) HealthCheck(ctx context.Context) error {
	if c.closed.Load() {
		return ErrAlreadyClosed
	}

	return c.backend.HealthCheck(ctx)
}

// Close shuts down the client
func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return ErrAlreadyClosed
	}

	c.mu.Lock()
	c.cache = nil
	c.models = nil
	c.mu.Unlock()

	return c.backend.Close()
}

// ObjectInference performs inference on an object stored in NebulaIO
type ObjectInference struct {
	client *Client
}

// NewObjectInference creates an object inference helper
func NewObjectInference(client *Client) *ObjectInference {
	return &ObjectInference{client: client}
}

// InferFromObject performs inference using an S3 object as input
func (o *ObjectInference) InferFromObject(ctx context.Context, bucket, key string, model string, params map[string]interface{}) (*InferenceResponse, error) {
	req := &InferenceRequest{
		Model: model,
		Input: map[string]interface{}{
			"s3_uri": fmt.Sprintf("s3://%s/%s", bucket, key),
		},
		Parameters: params,
	}

	return o.client.Infer(ctx, req)
}

// DescribeImage uses a vision-language model to describe an image object
func (o *ObjectInference) DescribeImage(ctx context.Context, bucket, key, prompt string) (*ChatResponse, error) {
	req := &ChatRequest{
		Model: "nvidia/llama-3.2-neva-72b-preview", // Example VLM model
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: fmt.Sprintf("<image s3://%s/%s> %s", bucket, key, prompt),
			},
		},
		MaxTokens: 1024,
	}

	return o.client.Chat(ctx, req)
}

// EmbedDocument generates embeddings for a document object
func (o *ObjectInference) EmbedDocument(ctx context.Context, bucket, key string) (*EmbeddingResponse, error) {
	req := &EmbeddingRequest{
		Model: "nvidia/nv-embedqa-e5-v5",
		Input: []string{fmt.Sprintf("s3://%s/%s", bucket, key)},
	}

	return o.client.Embed(ctx, req)
}

// S3Integration provides NIM integration with S3 operations
type S3Integration struct {
	client *Client
	mu     sync.RWMutex
}

// NewS3Integration creates a new S3 integration
func NewS3Integration(client *Client) *S3Integration {
	return &S3Integration{client: client}
}

// ProcessOnUpload triggers inference when an object is uploaded
func (s *S3Integration) ProcessOnUpload(ctx context.Context, bucket, key, contentType string, body io.Reader) (*InferenceResponse, error) {
	// Read body for inference
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var req *InferenceRequest

	// Determine model based on content type
	switch {
	case contentType == "image/jpeg" || contentType == "image/png":
		req = &InferenceRequest{
			Model: "nvidia/grounding-dino",
			Input: map[string]interface{}{
				"image": data,
			},
		}
	case contentType == "text/plain" || contentType == "application/json":
		req = &InferenceRequest{
			Model: "nvidia/nv-embedqa-e5-v5",
			Input: string(data),
		}
	default:
		return nil, fmt.Errorf("unsupported content type: %s", contentType)
	}

	return s.client.Infer(ctx, req)
}

// GetStatus returns NIM integration status
func (s *S3Integration) GetStatus() map[string]interface{} {
	metrics := s.client.GetMetrics()

	return map[string]interface{}{
		"available":         true,
		"requests_total":    metrics.RequestsTotal,
		"requests_success":  metrics.RequestsSuccess,
		"requests_failed":   metrics.RequestsFailed,
		"tokens_used":       metrics.TokensUsed,
		"avg_latency_ms":    metrics.AvgLatencyMs,
		"cache_hit_rate":    s.calculateCacheHitRate(metrics),
	}
}

func (s *S3Integration) calculateCacheHitRate(m *Metrics) float64 {
	total := m.CacheHits + m.CacheMisses
	if total == 0 {
		return 0
	}
	return float64(m.CacheHits) / float64(total)
}

// Close shuts down the integration
func (s *S3Integration) Close() error {
	return nil
}

// Helper for making HTTP requests to NIM API
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	endpoint := c.config.Endpoints[0]
	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("NIM API error: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}
