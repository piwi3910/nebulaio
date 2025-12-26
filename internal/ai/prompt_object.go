// Package ai implements AI/LLM integration for S3 objects
// This includes the promptObject API for natural language queries
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PromptObjectService provides AI/LLM integration for querying objects
type PromptObjectService struct {
	mu        sync.RWMutex
	providers map[string]LLMProvider
	configs   map[string]*AIConfig
	cache     *ResponseCache
	metrics   *AIMetrics
}

// AIConfig stores AI configuration for a bucket or globally
type AIConfig struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Provider        string            `json:"provider"`        // openai, anthropic, bedrock, ollama, custom
	Model           string            `json:"model"`           // gpt-4, claude-3, etc.
	Endpoint        string            `json:"endpoint"`        // Custom endpoint URL
	APIKey          string            `json:"api_key"`         // API key (encrypted)
	MaxTokens       int               `json:"max_tokens"`      // Max response tokens
	Temperature     float64           `json:"temperature"`     // Model temperature
	SystemPrompt    string            `json:"system_prompt"`   // Default system prompt
	EnableCache     bool              `json:"enable_cache"`    // Cache responses
	CacheTTL        time.Duration     `json:"cache_ttl"`       // Cache TTL
	RateLimitRPM    int               `json:"rate_limit_rpm"`  // Requests per minute
	AllowedBuckets  []string          `json:"allowed_buckets"` // Restrict to buckets
	EnabledFeatures []string          `json:"enabled_features"`
	Metadata        map[string]string `json:"metadata"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// LLMProvider interface for different AI providers
type LLMProvider interface {
	// Name returns the provider name
	Name() string

	// Query sends a prompt to the LLM with object context
	Query(ctx context.Context, req *PromptRequest) (*PromptResponse, error)

	// StreamQuery sends a prompt and streams the response
	StreamQuery(ctx context.Context, req *PromptRequest) (<-chan *StreamChunk, error)

	// Embed generates embeddings for content
	Embed(ctx context.Context, content string) ([]float64, error)

	// SupportedModels returns list of supported models
	SupportedModels() []string
}

// PromptRequest represents a prompt query request
type PromptRequest struct {
	// Object context
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	VersionID string `json:"version_id,omitempty"`

	// Object content (provided by caller)
	ObjectContent     io.Reader `json:"-"`
	ObjectContentType string    `json:"object_content_type"`
	ObjectSize        int64     `json:"object_size"`

	// Prompt details
	Prompt       string            `json:"prompt"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Model        string            `json:"model,omitempty"`
	MaxTokens    int               `json:"max_tokens,omitempty"`
	Temperature  float64           `json:"temperature,omitempty"`
	Stream       bool              `json:"stream,omitempty"`
	Parameters   map[string]string `json:"parameters,omitempty"`

	// Context options
	IncludeMetadata bool     `json:"include_metadata,omitempty"`
	ChunkSize       int      `json:"chunk_size,omitempty"`
	ChunkOverlap    int      `json:"chunk_overlap,omitempty"`
	MaxChunks       int      `json:"max_chunks,omitempty"`
	SelectFields    []string `json:"select_fields,omitempty"` // For structured data
}

// PromptResponse contains the AI response
type PromptResponse struct {
	RequestID      string                 `json:"request_id"`
	Bucket         string                 `json:"bucket"`
	Key            string                 `json:"key"`
	Model          string                 `json:"model"`
	Response       string                 `json:"response"`
	FinishReason   string                 `json:"finish_reason"`
	Usage          *TokenUsage            `json:"usage,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	Citations      []Citation             `json:"citations,omitempty"`
	ProcessingTime time.Duration          `json:"processing_time"`
	Cached         bool                   `json:"cached"`
	CreatedAt      time.Time              `json:"created_at"`
}

// TokenUsage tracks token consumption
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Citation references a specific part of the object
type Citation struct {
	Text      string `json:"text"`
	StartChar int    `json:"start_char"`
	EndChar   int    `json:"end_char"`
	ChunkID   int    `json:"chunk_id,omitempty"`
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
	RequestID    string `json:"request_id"`
	Index        int    `json:"index"`
	Content      string `json:"content"`
	FinishReason string `json:"finish_reason,omitempty"`
	Error        error  `json:"-"`
}

// ResponseCache caches AI responses
type ResponseCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	maxSize int
}

// CacheEntry represents a cached response
type CacheEntry struct {
	Response  *PromptResponse
	ExpiresAt time.Time
}

// AIMetrics tracks AI usage metrics
type AIMetrics struct {
	mu               sync.RWMutex
	TotalRequests    int64
	CacheHits        int64
	CacheMisses      int64
	TotalTokens      int64
	TotalLatencyMs   int64
	ErrorCount       int64
	RequestsByModel  map[string]int64
	RequestsByBucket map[string]int64
}

// NewPromptObjectService creates a new prompt object service
func NewPromptObjectService() *PromptObjectService {
	return &PromptObjectService{
		providers: make(map[string]LLMProvider),
		configs:   make(map[string]*AIConfig),
		cache: &ResponseCache{
			entries: make(map[string]*CacheEntry),
			maxSize: 1000,
		},
		metrics: &AIMetrics{
			RequestsByModel:  make(map[string]int64),
			RequestsByBucket: make(map[string]int64),
		},
	}
}

// RegisterProvider registers an LLM provider
func (s *PromptObjectService) RegisterProvider(provider LLMProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[provider.Name()] = provider
}

// CreateConfig creates an AI configuration
func (s *PromptObjectService) CreateConfig(cfg *AIConfig) error {
	if cfg.Name == "" {
		return errors.New("config name required")
	}
	if cfg.Provider == "" {
		return errors.New("provider required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.configs[cfg.Name]; exists {
		return fmt.Errorf("config %s already exists", cfg.Name)
	}

	// Set defaults
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("ai-config-%d", time.Now().UnixNano())
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.7
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = time.Hour
	}

	cfg.CreatedAt = time.Now()
	cfg.UpdatedAt = cfg.CreatedAt
	s.configs[cfg.Name] = cfg
	return nil
}

// GetConfig retrieves an AI configuration
func (s *PromptObjectService) GetConfig(name string) (*AIConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg, exists := s.configs[name]
	if !exists {
		return nil, fmt.Errorf("config %s not found", name)
	}
	return cfg, nil
}

// ListConfigs lists all AI configurations
func (s *PromptObjectService) ListConfigs() []*AIConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configs := make([]*AIConfig, 0, len(s.configs))
	for _, cfg := range s.configs {
		configs = append(configs, cfg)
	}
	return configs
}

// DeleteConfig removes an AI configuration
func (s *PromptObjectService) DeleteConfig(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.configs[name]; !exists {
		return fmt.Errorf("config %s not found", name)
	}
	delete(s.configs, name)
	return nil
}

// PromptObject queries an object using natural language
func (s *PromptObjectService) PromptObject(ctx context.Context, configName string, req *PromptRequest) (*PromptResponse, error) {
	start := time.Now()

	// Get config
	cfg, err := s.GetConfig(configName)
	if err != nil {
		return nil, err
	}

	// Check bucket permissions
	if len(cfg.AllowedBuckets) > 0 {
		allowed := false
		for _, b := range cfg.AllowedBuckets {
			if b == req.Bucket {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("bucket %s not allowed for config %s", req.Bucket, configName)
		}
	}

	// Check cache
	cacheKey := s.generateCacheKey(req)
	if cfg.EnableCache {
		if cached := s.cache.Get(cacheKey); cached != nil {
			s.recordMetrics(req, cached, true)
			return cached, nil
		}
	}

	// Get provider
	s.mu.RLock()
	provider, exists := s.providers[cfg.Provider]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("provider %s not registered", cfg.Provider)
	}

	// Apply config defaults to request
	if req.Model == "" {
		req.Model = cfg.Model
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = cfg.MaxTokens
	}
	if req.Temperature == 0 {
		req.Temperature = cfg.Temperature
	}
	if req.SystemPrompt == "" {
		req.SystemPrompt = cfg.SystemPrompt
	}

	// Execute query
	resp, err := provider.Query(ctx, req)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.ErrorCount++
		s.metrics.mu.Unlock()
		return nil, err
	}

	resp.ProcessingTime = time.Since(start)

	// Cache response
	if cfg.EnableCache {
		s.cache.Set(cacheKey, resp, cfg.CacheTTL)
	}

	s.recordMetrics(req, resp, false)
	return resp, nil
}

// StreamPromptObject queries an object with streaming response
func (s *PromptObjectService) StreamPromptObject(ctx context.Context, configName string, req *PromptRequest) (<-chan *StreamChunk, error) {
	// Get config
	cfg, err := s.GetConfig(configName)
	if err != nil {
		return nil, err
	}

	// Get provider
	s.mu.RLock()
	provider, exists := s.providers[cfg.Provider]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("provider %s not registered", cfg.Provider)
	}

	// Apply config defaults
	if req.Model == "" {
		req.Model = cfg.Model
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = cfg.MaxTokens
	}
	if req.Temperature == 0 {
		req.Temperature = cfg.Temperature
	}
	if req.SystemPrompt == "" {
		req.SystemPrompt = cfg.SystemPrompt
	}

	req.Stream = true
	return provider.StreamQuery(ctx, req)
}

// GenerateEmbedding generates embeddings for object content
func (s *PromptObjectService) GenerateEmbedding(ctx context.Context, configName string, content string) ([]float64, error) {
	cfg, err := s.GetConfig(configName)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	provider, exists := s.providers[cfg.Provider]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("provider %s not registered", cfg.Provider)
	}

	return provider.Embed(ctx, content)
}

// GetMetrics returns AI usage metrics
func (s *PromptObjectService) GetMetrics() *AIMetrics {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	// Return a copy
	metrics := &AIMetrics{
		TotalRequests:    s.metrics.TotalRequests,
		CacheHits:        s.metrics.CacheHits,
		CacheMisses:      s.metrics.CacheMisses,
		TotalTokens:      s.metrics.TotalTokens,
		TotalLatencyMs:   s.metrics.TotalLatencyMs,
		ErrorCount:       s.metrics.ErrorCount,
		RequestsByModel:  make(map[string]int64),
		RequestsByBucket: make(map[string]int64),
	}
	for k, v := range s.metrics.RequestsByModel {
		metrics.RequestsByModel[k] = v
	}
	for k, v := range s.metrics.RequestsByBucket {
		metrics.RequestsByBucket[k] = v
	}
	return metrics
}

func (s *PromptObjectService) generateCacheKey(req *PromptRequest) string {
	return fmt.Sprintf("%s/%s/%s:%s:%s", req.Bucket, req.Key, req.VersionID, req.Model, req.Prompt)
}

func (s *PromptObjectService) recordMetrics(req *PromptRequest, resp *PromptResponse, cached bool) {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()

	s.metrics.TotalRequests++
	if cached {
		s.metrics.CacheHits++
	} else {
		s.metrics.CacheMisses++
	}
	if resp.Usage != nil {
		s.metrics.TotalTokens += int64(resp.Usage.TotalTokens)
	}
	s.metrics.TotalLatencyMs += resp.ProcessingTime.Milliseconds()
	s.metrics.RequestsByModel[resp.Model]++
	s.metrics.RequestsByBucket[req.Bucket]++
}

// Cache methods
func (c *ResponseCache) Get(key string) *PromptResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil
	}
	if time.Now().After(entry.ExpiresAt) {
		return nil
	}
	resp := *entry.Response
	resp.Cached = true
	return &resp
}

func (c *ResponseCache) Set(key string, resp *PromptResponse, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if full
	if len(c.entries) >= c.maxSize {
		// Simple eviction: remove oldest expired
		now := time.Now()
		for k, v := range c.entries {
			if now.After(v.ExpiresAt) {
				delete(c.entries, k)
			}
		}
	}

	c.entries[key] = &CacheEntry{
		Response:  resp,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// ========================================
// Built-in LLM Providers
// ========================================

// OpenAIProvider implements LLMProvider for OpenAI
type OpenAIProvider struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:   apiKey,
		endpoint: "https://api.openai.com/v1",
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) SupportedModels() []string {
	return []string{"gpt-4", "gpt-4-turbo", "gpt-3.5-turbo", "gpt-4o", "gpt-4o-mini"}
}

func (p *OpenAIProvider) Query(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	// Read object content
	var content string
	if req.ObjectContent != nil {
		data, err := io.ReadAll(req.ObjectContent)
		if err != nil {
			return nil, fmt.Errorf("failed to read object content: %w", err)
		}
		content = string(data)
	}

	// Build messages
	messages := []map[string]string{}
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}

	// Add object context
	userMessage := fmt.Sprintf("Object: s3://%s/%s\nContent-Type: %s\n\nContent:\n%s\n\nQuestion: %s",
		req.Bucket, req.Key, req.ObjectContentType, content, req.Prompt)
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": userMessage,
	})

	// Build request
	reqBody := map[string]interface{}{
		"model":       req.Model,
		"messages":    messages,
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
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

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Choices) == 0 {
		return nil, errors.New("no response from OpenAI")
	}

	return &PromptResponse{
		RequestID:    result.ID,
		Bucket:       req.Bucket,
		Key:          req.Key,
		Model:        req.Model,
		Response:     result.Choices[0].Message.Content,
		FinishReason: result.Choices[0].FinishReason,
		Usage: &TokenUsage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		},
		CreatedAt: time.Now(),
	}, nil
}

func (p *OpenAIProvider) StreamQuery(ctx context.Context, req *PromptRequest) (<-chan *StreamChunk, error) {
	chunks := make(chan *StreamChunk, 100)

	go func() {
		defer close(chunks)

		// For simplicity, use non-streaming and emit as single chunk
		resp, err := p.Query(ctx, req)
		if err != nil {
			chunks <- &StreamChunk{Error: err}
			return
		}

		chunks <- &StreamChunk{
			RequestID:    resp.RequestID,
			Index:        0,
			Content:      resp.Response,
			FinishReason: resp.FinishReason,
		}
	}()

	return chunks, nil
}

func (p *OpenAIProvider) Embed(ctx context.Context, content string) ([]float64, error) {
	reqBody := map[string]interface{}{
		"model": "text-embedding-ada-002",
		"input": content,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Data) == 0 {
		return nil, errors.New("no embeddings returned")
	}

	return result.Data[0].Embedding, nil
}

// AnthropicProvider implements LLMProvider for Anthropic Claude
type AnthropicProvider struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:   apiKey,
		endpoint: "https://api.anthropic.com/v1",
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) SupportedModels() []string {
	return []string{"claude-3-opus-20240229", "claude-3-sonnet-20240229", "claude-3-haiku-20240307", "claude-3-5-sonnet-20241022"}
}

func (p *AnthropicProvider) Query(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	// Read object content
	var content string
	if req.ObjectContent != nil {
		data, err := io.ReadAll(req.ObjectContent)
		if err != nil {
			return nil, fmt.Errorf("failed to read object content: %w", err)
		}
		content = string(data)
	}

	// Build user message with object context
	userMessage := fmt.Sprintf("Object: s3://%s/%s\nContent-Type: %s\n\nContent:\n%s\n\nQuestion: %s",
		req.Bucket, req.Key, req.ObjectContentType, content, req.Prompt)

	// Build request
	reqBody := map[string]interface{}{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": userMessage},
		},
	}

	if req.SystemPrompt != "" {
		reqBody["system"] = req.SystemPrompt
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Anthropic API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		ID      string `json:"id"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var responseText string
	for _, c := range result.Content {
		if c.Type == "text" {
			responseText += c.Text
		}
	}

	return &PromptResponse{
		RequestID:    result.ID,
		Bucket:       req.Bucket,
		Key:          req.Key,
		Model:        req.Model,
		Response:     responseText,
		FinishReason: result.StopReason,
		Usage: &TokenUsage{
			PromptTokens:     result.Usage.InputTokens,
			CompletionTokens: result.Usage.OutputTokens,
			TotalTokens:      result.Usage.InputTokens + result.Usage.OutputTokens,
		},
		CreatedAt: time.Now(),
	}, nil
}

func (p *AnthropicProvider) StreamQuery(ctx context.Context, req *PromptRequest) (<-chan *StreamChunk, error) {
	chunks := make(chan *StreamChunk, 100)

	go func() {
		defer close(chunks)
		resp, err := p.Query(ctx, req)
		if err != nil {
			chunks <- &StreamChunk{Error: err}
			return
		}
		chunks <- &StreamChunk{
			RequestID:    resp.RequestID,
			Index:        0,
			Content:      resp.Response,
			FinishReason: resp.FinishReason,
		}
	}()

	return chunks, nil
}

func (p *AnthropicProvider) Embed(ctx context.Context, content string) ([]float64, error) {
	// Anthropic doesn't have native embeddings, could use alternative
	return nil, errors.New("Anthropic does not support embeddings directly")
}

// OllamaProvider implements LLMProvider for local Ollama
type OllamaProvider struct {
	endpoint string
	client   *http.Client
}

// NewOllamaProvider creates a new Ollama provider for local LLMs
func NewOllamaProvider(endpoint string) *OllamaProvider {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	return &OllamaProvider{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) SupportedModels() []string {
	return []string{"llama3", "llama3:70b", "mistral", "mixtral", "codellama", "phi3"}
}

func (p *OllamaProvider) Query(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	// Read object content
	var content string
	if req.ObjectContent != nil {
		data, err := io.ReadAll(req.ObjectContent)
		if err != nil {
			return nil, fmt.Errorf("failed to read object content: %w", err)
		}
		content = string(data)
	}

	// Build prompt
	fullPrompt := fmt.Sprintf("Object: s3://%s/%s\nContent-Type: %s\n\nContent:\n%s\n\nQuestion: %s",
		req.Bucket, req.Key, req.ObjectContentType, content, req.Prompt)

	if req.SystemPrompt != "" {
		fullPrompt = req.SystemPrompt + "\n\n" + fullPrompt
	}

	reqBody := map[string]interface{}{
		"model":  req.Model,
		"prompt": fullPrompt,
		"stream": false,
		"options": map[string]interface{}{
			"temperature": req.Temperature,
			"num_predict": req.MaxTokens,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	finishReason := "length"
	if result.Done {
		finishReason = "stop"
	}

	return &PromptResponse{
		RequestID:    fmt.Sprintf("ollama-%d", time.Now().UnixNano()),
		Bucket:       req.Bucket,
		Key:          req.Key,
		Model:        req.Model,
		Response:     result.Response,
		FinishReason: finishReason,
		CreatedAt:    time.Now(),
	}, nil
}

func (p *OllamaProvider) StreamQuery(ctx context.Context, req *PromptRequest) (<-chan *StreamChunk, error) {
	chunks := make(chan *StreamChunk, 100)

	go func() {
		defer close(chunks)
		resp, err := p.Query(ctx, req)
		if err != nil {
			chunks <- &StreamChunk{Error: err}
			return
		}
		chunks <- &StreamChunk{
			RequestID:    resp.RequestID,
			Index:        0,
			Content:      resp.Response,
			FinishReason: resp.FinishReason,
		}
	}()

	return chunks, nil
}

func (p *OllamaProvider) Embed(ctx context.Context, content string) ([]float64, error) {
	reqBody := map[string]interface{}{
		"model":  "nomic-embed-text",
		"prompt": content,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Embedding []float64 `json:"embedding"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Embedding, nil
}

// ========================================
// Content Processing Utilities
// ========================================

// ContentProcessor handles different content types for AI processing
type ContentProcessor struct{}

// ProcessForLLM prepares content for LLM consumption
func (p *ContentProcessor) ProcessForLLM(content io.Reader, contentType string, maxChars int) (string, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return "", err
	}

	text := string(data)

	// Handle different content types
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		// Pretty print JSON for better readability
		var v interface{}
		if err := json.Unmarshal(data, &v); err == nil {
			pretty, _ := json.MarshalIndent(v, "", "  ")
			text = string(pretty)
		}

	case strings.HasPrefix(contentType, "text/csv"):
		// Keep as-is, CSVs are readable

	case strings.HasPrefix(contentType, "application/xml"), strings.HasPrefix(contentType, "text/xml"):
		// Keep as-is, XML is readable

	case strings.HasPrefix(contentType, "text/"):
		// Plain text, keep as-is

	default:
		// Binary or unknown - note this to the LLM
		if !isTextContent(data) {
			return fmt.Sprintf("[Binary content: %d bytes, type: %s]", len(data), contentType), nil
		}
	}

	// Truncate if needed
	if maxChars > 0 && len(text) > maxChars {
		text = text[:maxChars] + "\n...[truncated]"
	}

	return text, nil
}

// ChunkContent splits content into chunks for processing
func (p *ContentProcessor) ChunkContent(content string, chunkSize, overlap int) []string {
	if chunkSize <= 0 {
		chunkSize = 4000
	}
	if overlap < 0 {
		overlap = 200
	}

	var chunks []string
	for i := 0; i < len(content); i += chunkSize - overlap {
		end := i + chunkSize
		if end > len(content) {
			end = len(content)
		}
		chunks = append(chunks, content[i:end])
		if end >= len(content) {
			break
		}
	}
	return chunks
}

func isTextContent(data []byte) bool {
	// Simple heuristic: check if content is mostly printable
	if len(data) == 0 {
		return true
	}

	printable := 0
	for _, b := range data[:min(len(data), 1000)] {
		if b >= 32 && b < 127 || b == '\n' || b == '\r' || b == '\t' {
			printable++
		}
	}

	return float64(printable)/float64(min(len(data), 1000)) > 0.9
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ========================================
// S3 API Compatibility
// ========================================

// PromptObjectInput represents the S3-compatible API input
type PromptObjectInput struct {
	Bucket       string            `json:"Bucket"`
	Key          string            `json:"Key"`
	VersionID    string            `json:"VersionId,omitempty"`
	Prompt       string            `json:"Prompt"`
	Model        string            `json:"Model,omitempty"`
	MaxTokens    int               `json:"MaxTokens,omitempty"`
	Temperature  float64           `json:"Temperature,omitempty"`
	SystemPrompt string            `json:"SystemPrompt,omitempty"`
	Stream       bool              `json:"Stream,omitempty"`
	Config       string            `json:"Config,omitempty"` // AI config name
	Parameters   map[string]string `json:"Parameters,omitempty"`
}

// PromptObjectOutput represents the S3-compatible API output
type PromptObjectOutput struct {
	RequestID      string                 `json:"RequestId"`
	Response       string                 `json:"Response"`
	Model          string                 `json:"Model"`
	FinishReason   string                 `json:"FinishReason"`
	Usage          *TokenUsage            `json:"Usage,omitempty"`
	ProcessingTime int64                  `json:"ProcessingTimeMs"`
	Cached         bool                   `json:"Cached"`
	Metadata       map[string]interface{} `json:"Metadata,omitempty"`
}

// PromptObjectAPI provides S3-compatible API for promptObject
func (s *PromptObjectService) PromptObjectAPI(ctx context.Context, input *PromptObjectInput, objectContent io.Reader, contentType string, size int64) (*PromptObjectOutput, error) {
	configName := input.Config
	if configName == "" {
		configName = "default"
	}

	req := &PromptRequest{
		Bucket:            input.Bucket,
		Key:               input.Key,
		VersionID:         input.VersionID,
		ObjectContent:     objectContent,
		ObjectContentType: contentType,
		ObjectSize:        size,
		Prompt:            input.Prompt,
		SystemPrompt:      input.SystemPrompt,
		Model:             input.Model,
		MaxTokens:         input.MaxTokens,
		Temperature:       input.Temperature,
		Stream:            input.Stream,
		Parameters:        input.Parameters,
	}

	resp, err := s.PromptObject(ctx, configName, req)
	if err != nil {
		return nil, err
	}

	return &PromptObjectOutput{
		RequestID:      resp.RequestID,
		Response:       resp.Response,
		Model:          resp.Model,
		FinishReason:   resp.FinishReason,
		Usage:          resp.Usage,
		ProcessingTime: resp.ProcessingTime.Milliseconds(),
		Cached:         resp.Cached,
		Metadata:       resp.Metadata,
	}, nil
}
