package ai

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// mockProvider implements LLMProvider for testing
type mockProvider struct {
	name      string
	response  string
	err       error
	embedding []float64
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) SupportedModels() []string {
	return []string{"mock-model", "mock-model-v2"}
}

func (m *mockProvider) Query(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	if m.err != nil {
		return nil, m.err
	}

	// Read content to simulate processing
	if req.ObjectContent != nil {
		io.ReadAll(req.ObjectContent)
	}

	return &PromptResponse{
		RequestID:    "mock-request-id",
		Bucket:       req.Bucket,
		Key:          req.Key,
		Model:        req.Model,
		Response:     m.response,
		FinishReason: "stop",
		Usage: &TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockProvider) StreamQuery(ctx context.Context, req *PromptRequest) (<-chan *StreamChunk, error) {
	chunks := make(chan *StreamChunk, 1)
	go func() {
		defer close(chunks)
		if m.err != nil {
			chunks <- &StreamChunk{Error: m.err}
			return
		}
		chunks <- &StreamChunk{
			RequestID:    "mock-stream-id",
			Index:        0,
			Content:      m.response,
			FinishReason: "stop",
		}
	}()
	return chunks, nil
}

func (m *mockProvider) Embed(ctx context.Context, content string) ([]float64, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.embedding != nil {
		return m.embedding, nil
	}
	return []float64{0.1, 0.2, 0.3, 0.4, 0.5}, nil
}

func TestCreateConfig(t *testing.T) {
	svc := NewPromptObjectService()

	cfg := &AIConfig{
		Name:       "test-config",
		Provider:   "mock",
		Model:      "mock-model",
		MaxTokens:  1000,
		EnableCache: true,
	}

	err := svc.CreateConfig(cfg)
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	if cfg.ID == "" {
		t.Error("Config ID should be auto-generated")
	}

	// Retrieve and verify
	retrieved, err := svc.GetConfig("test-config")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if retrieved.Model != "mock-model" {
		t.Errorf("Expected model 'mock-model', got '%s'", retrieved.Model)
	}
}

func TestCreateConfigValidation(t *testing.T) {
	svc := NewPromptObjectService()

	tests := []struct {
		name    string
		cfg     *AIConfig
		wantErr bool
	}{
		{
			name:    "missing name",
			cfg:     &AIConfig{Provider: "mock"},
			wantErr: true,
		},
		{
			name:    "missing provider",
			cfg:     &AIConfig{Name: "test"},
			wantErr: true,
		},
		{
			name:    "valid config",
			cfg:     &AIConfig{Name: "test", Provider: "mock"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.CreateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDuplicateConfig(t *testing.T) {
	svc := NewPromptObjectService()

	cfg := &AIConfig{
		Name:     "test-config",
		Provider: "mock",
	}

	svc.CreateConfig(cfg)
	err := svc.CreateConfig(cfg)
	if err == nil {
		t.Error("Expected error for duplicate config")
	}
}

func TestListConfigs(t *testing.T) {
	svc := NewPromptObjectService()

	for i := 0; i < 3; i++ {
		svc.CreateConfig(&AIConfig{
			Name:     "config-" + string(rune('a'+i)),
			Provider: "mock",
		})
	}

	configs := svc.ListConfigs()
	if len(configs) != 3 {
		t.Errorf("Expected 3 configs, got %d", len(configs))
	}
}

func TestDeleteConfig(t *testing.T) {
	svc := NewPromptObjectService()

	svc.CreateConfig(&AIConfig{
		Name:     "test-config",
		Provider: "mock",
	})

	err := svc.DeleteConfig("test-config")
	if err != nil {
		t.Fatalf("DeleteConfig failed: %v", err)
	}

	_, err = svc.GetConfig("test-config")
	if err == nil {
		t.Error("Expected error when getting deleted config")
	}
}

func TestPromptObject(t *testing.T) {
	svc := NewPromptObjectService()

	// Register mock provider
	mock := &mockProvider{
		name:     "mock",
		response: "This is a test response about the document.",
	}
	svc.RegisterProvider(mock)

	// Create config
	svc.CreateConfig(&AIConfig{
		Name:     "test-config",
		Provider: "mock",
		Model:    "mock-model",
	})

	ctx := context.Background()
	req := &PromptRequest{
		Bucket:            "test-bucket",
		Key:               "test-doc.txt",
		ObjectContent:     strings.NewReader("This is the document content."),
		ObjectContentType: "text/plain",
		ObjectSize:        30,
		Prompt:            "Summarize this document",
		Model:             "mock-model",
	}

	resp, err := svc.PromptObject(ctx, "test-config", req)
	if err != nil {
		t.Fatalf("PromptObject failed: %v", err)
	}

	if resp.Response != "This is a test response about the document." {
		t.Errorf("Unexpected response: %s", resp.Response)
	}

	if resp.Bucket != "test-bucket" {
		t.Errorf("Expected bucket 'test-bucket', got '%s'", resp.Bucket)
	}

	if resp.Usage == nil {
		t.Error("Usage should be set")
	}
}

func TestPromptObjectCaching(t *testing.T) {
	svc := NewPromptObjectService()

	mock := &mockProvider{
		name:     "mock",
		response: "Cached response",
	}
	svc.RegisterProvider(mock)

	svc.CreateConfig(&AIConfig{
		Name:        "cached-config",
		Provider:    "mock",
		Model:       "mock-model",
		EnableCache: true,
		CacheTTL:    time.Hour,
	})

	ctx := context.Background()
	req := &PromptRequest{
		Bucket:            "test-bucket",
		Key:               "test-doc.txt",
		ObjectContent:     strings.NewReader("Content"),
		ObjectContentType: "text/plain",
		Prompt:            "What is this?",
		Model:             "mock-model",
	}

	// First request
	resp1, _ := svc.PromptObject(ctx, "cached-config", req)
	if resp1.Cached {
		t.Error("First request should not be cached")
	}

	// Second request with same parameters
	req.ObjectContent = strings.NewReader("Content") // Reset reader
	resp2, _ := svc.PromptObject(ctx, "cached-config", req)
	if !resp2.Cached {
		t.Error("Second request should be cached")
	}
}

func TestPromptObjectBucketRestriction(t *testing.T) {
	svc := NewPromptObjectService()

	mock := &mockProvider{name: "mock", response: "ok"}
	svc.RegisterProvider(mock)

	svc.CreateConfig(&AIConfig{
		Name:           "restricted-config",
		Provider:       "mock",
		Model:          "mock-model",
		AllowedBuckets: []string{"allowed-bucket"},
	})

	ctx := context.Background()

	// Allowed bucket
	req1 := &PromptRequest{
		Bucket:        "allowed-bucket",
		Key:           "doc.txt",
		ObjectContent: strings.NewReader("content"),
		Prompt:        "test",
	}
	_, err := svc.PromptObject(ctx, "restricted-config", req1)
	if err != nil {
		t.Errorf("Should allow access to allowed-bucket: %v", err)
	}

	// Disallowed bucket
	req2 := &PromptRequest{
		Bucket:        "other-bucket",
		Key:           "doc.txt",
		ObjectContent: strings.NewReader("content"),
		Prompt:        "test",
	}
	_, err = svc.PromptObject(ctx, "restricted-config", req2)
	if err == nil {
		t.Error("Should deny access to other-bucket")
	}
}

func TestStreamPromptObject(t *testing.T) {
	svc := NewPromptObjectService()

	mock := &mockProvider{
		name:     "mock",
		response: "Streamed response",
	}
	svc.RegisterProvider(mock)

	svc.CreateConfig(&AIConfig{
		Name:     "stream-config",
		Provider: "mock",
		Model:    "mock-model",
	})

	ctx := context.Background()
	req := &PromptRequest{
		Bucket:        "test-bucket",
		Key:           "doc.txt",
		ObjectContent: strings.NewReader("content"),
		Prompt:        "Summarize",
	}

	chunks, err := svc.StreamPromptObject(ctx, "stream-config", req)
	if err != nil {
		t.Fatalf("StreamPromptObject failed: %v", err)
	}

	var response string
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		response += chunk.Content
	}

	if response != "Streamed response" {
		t.Errorf("Expected 'Streamed response', got '%s'", response)
	}
}

func TestGenerateEmbedding(t *testing.T) {
	svc := NewPromptObjectService()

	mock := &mockProvider{
		name:      "mock",
		embedding: []float64{0.1, 0.2, 0.3},
	}
	svc.RegisterProvider(mock)

	svc.CreateConfig(&AIConfig{
		Name:     "embed-config",
		Provider: "mock",
	})

	ctx := context.Background()
	embedding, err := svc.GenerateEmbedding(ctx, "embed-config", "test content")
	if err != nil {
		t.Fatalf("GenerateEmbedding failed: %v", err)
	}

	if len(embedding) != 3 {
		t.Errorf("Expected 3 dimensions, got %d", len(embedding))
	}

	if embedding[0] != 0.1 {
		t.Errorf("Expected first value 0.1, got %f", embedding[0])
	}
}

func TestGetMetrics(t *testing.T) {
	svc := NewPromptObjectService()

	mock := &mockProvider{name: "mock", response: "ok"}
	svc.RegisterProvider(mock)

	svc.CreateConfig(&AIConfig{
		Name:     "metrics-config",
		Provider: "mock",
		Model:    "mock-model",
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		req := &PromptRequest{
			Bucket:        "bucket-" + string(rune('a'+i%2)),
			Key:           "doc.txt",
			ObjectContent: strings.NewReader("content"),
			Prompt:        "test",
			Model:         "mock-model",
		}
		svc.PromptObject(ctx, "metrics-config", req)
	}

	metrics := svc.GetMetrics()

	if metrics.TotalRequests != 5 {
		t.Errorf("Expected 5 total requests, got %d", metrics.TotalRequests)
	}

	if metrics.RequestsByModel["mock-model"] != 5 {
		t.Errorf("Expected 5 requests for mock-model, got %d", metrics.RequestsByModel["mock-model"])
	}
}

func TestPromptObjectAPI(t *testing.T) {
	svc := NewPromptObjectService()

	mock := &mockProvider{
		name:     "mock",
		response: "API response",
	}
	svc.RegisterProvider(mock)

	svc.CreateConfig(&AIConfig{
		Name:     "default",
		Provider: "mock",
		Model:    "mock-model",
	})

	ctx := context.Background()
	input := &PromptObjectInput{
		Bucket: "test-bucket",
		Key:    "test.txt",
		Prompt: "What is this?",
		Model:  "mock-model",
	}

	output, err := svc.PromptObjectAPI(ctx, input, strings.NewReader("content"), "text/plain", 7)
	if err != nil {
		t.Fatalf("PromptObjectAPI failed: %v", err)
	}

	if output.Response != "API response" {
		t.Errorf("Expected 'API response', got '%s'", output.Response)
	}

	if output.RequestID == "" {
		t.Error("RequestID should be set")
	}
}

func TestContentProcessor(t *testing.T) {
	processor := &ContentProcessor{}

	tests := []struct {
		name        string
		content     string
		contentType string
		maxChars    int
		wantContain string
	}{
		{
			name:        "plain text",
			content:     "Hello world",
			contentType: "text/plain",
			maxChars:    0,
			wantContain: "Hello world",
		},
		{
			name:        "json prettified",
			content:     `{"name":"test"}`,
			contentType: "application/json",
			maxChars:    0,
			wantContain: "name",
		},
		{
			name:        "truncated content",
			content:     "This is a very long text that should be truncated",
			contentType: "text/plain",
			maxChars:    20,
			wantContain: "truncated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := processor.ProcessForLLM(strings.NewReader(tt.content), tt.contentType, tt.maxChars)
			if err != nil {
				t.Fatalf("ProcessForLLM failed: %v", err)
			}
			if !strings.Contains(result, tt.wantContain) {
				t.Errorf("Expected result to contain '%s', got '%s'", tt.wantContain, result)
			}
		})
	}
}

func TestChunkContent(t *testing.T) {
	processor := &ContentProcessor{}

	content := strings.Repeat("a", 1000)

	chunks := processor.ChunkContent(content, 300, 50)

	if len(chunks) < 3 {
		t.Errorf("Expected at least 3 chunks, got %d", len(chunks))
	}

	// Verify overlap
	if len(chunks) > 1 {
		// Check that chunks have overlapping content
		firstEnd := chunks[0][len(chunks[0])-50:]
		secondStart := chunks[1][:50]
		if firstEnd != secondStart {
			t.Error("Chunks should have overlapping content")
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	svc := NewPromptObjectService()

	cfg := &AIConfig{
		Name:     "defaults-test",
		Provider: "mock",
	}

	svc.CreateConfig(cfg)

	retrieved, _ := svc.GetConfig("defaults-test")

	if retrieved.MaxTokens != 4096 {
		t.Errorf("Expected default MaxTokens 4096, got %d", retrieved.MaxTokens)
	}

	if retrieved.Temperature != 0.7 {
		t.Errorf("Expected default Temperature 0.7, got %f", retrieved.Temperature)
	}

	if retrieved.CacheTTL != time.Hour {
		t.Errorf("Expected default CacheTTL 1h, got %v", retrieved.CacheTTL)
	}
}

func TestProviderNotRegistered(t *testing.T) {
	svc := NewPromptObjectService()

	svc.CreateConfig(&AIConfig{
		Name:     "no-provider",
		Provider: "nonexistent",
	})

	ctx := context.Background()
	req := &PromptRequest{
		Bucket:        "bucket",
		Key:           "key",
		ObjectContent: strings.NewReader("content"),
		Prompt:        "test",
	}

	_, err := svc.PromptObject(ctx, "no-provider", req)
	if err == nil {
		t.Error("Expected error for unregistered provider")
	}
}

func TestCacheExpiration(t *testing.T) {
	cache := &ResponseCache{
		entries: make(map[string]*CacheEntry),
		maxSize: 100,
	}

	resp := &PromptResponse{
		RequestID: "test",
		Response:  "cached",
	}

	// Set with very short TTL
	cache.Set("key1", resp, 1*time.Millisecond)

	// Should be cached initially
	if cache.Get("key1") == nil {
		t.Error("Entry should be cached")
	}

	// Wait for expiration
	time.Sleep(5 * time.Millisecond)

	// Should be expired
	if cache.Get("key1") != nil {
		t.Error("Entry should be expired")
	}
}

func TestMultipleProviders(t *testing.T) {
	svc := NewPromptObjectService()

	// Register multiple providers
	svc.RegisterProvider(&mockProvider{name: "provider1", response: "response1"})
	svc.RegisterProvider(&mockProvider{name: "provider2", response: "response2"})

	// Create configs for each
	svc.CreateConfig(&AIConfig{Name: "config1", Provider: "provider1", Model: "model1"})
	svc.CreateConfig(&AIConfig{Name: "config2", Provider: "provider2", Model: "model2"})

	ctx := context.Background()

	// Test provider1
	req1 := &PromptRequest{
		Bucket:        "bucket",
		Key:           "key",
		ObjectContent: strings.NewReader("content"),
		Prompt:        "test",
	}
	resp1, _ := svc.PromptObject(ctx, "config1", req1)
	if resp1.Response != "response1" {
		t.Errorf("Expected 'response1', got '%s'", resp1.Response)
	}

	// Test provider2
	req2 := &PromptRequest{
		Bucket:        "bucket",
		Key:           "key",
		ObjectContent: strings.NewReader("content"),
		Prompt:        "test",
	}
	resp2, _ := svc.PromptObject(ctx, "config2", req2)
	if resp2.Response != "response2" {
		t.Errorf("Expected 'response2', got '%s'", resp2.Response)
	}
}
