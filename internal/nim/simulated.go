// Package nim provides a simulated NIM backend for testing.
package nim

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// SimulatedBackend provides a simulated NIM backend for testing.
type SimulatedBackend struct {
	models      map[string]*ModelInfo
	latencyMs   int64
	failureRate float64
	mu          sync.RWMutex
	closed      bool
}

// NewSimulatedBackend creates a new simulated NIM backend.
func NewSimulatedBackend() *SimulatedBackend {
	b := &SimulatedBackend{
		models:    make(map[string]*ModelInfo),
		latencyMs: 50, // 50ms simulated latency
	}
	b.initModels()

	return b
}

// initModels initializes the simulated model catalog.
func (b *SimulatedBackend) initModels() {
	b.models = map[string]*ModelInfo{
		// LLM Models
		"meta/llama-3.1-70b-instruct": {
			ID:           "meta/llama-3.1-70b-instruct",
			Name:         "Llama 3.1 70B Instruct",
			Type:         ModelTypeLLM,
			Version:      "3.1",
			Description:  "Meta's Llama 3.1 70B instruction-tuned model",
			MaxTokens:    8192,
			MaxBatchSize: 32,
			Status:       "ready",
			AvgLatencyMs: 200,
		},
		"meta/llama-3.1-8b-instruct": {
			ID:           "meta/llama-3.1-8b-instruct",
			Name:         "Llama 3.1 8B Instruct",
			Type:         ModelTypeLLM,
			Version:      "3.1",
			Description:  "Meta's Llama 3.1 8B instruction-tuned model",
			MaxTokens:    8192,
			MaxBatchSize: 64,
			Status:       "ready",
			AvgLatencyMs: 50,
		},
		"nvidia/nemotron-4-340b-instruct": {
			ID:           "nvidia/nemotron-4-340b-instruct",
			Name:         "Nemotron-4 340B Instruct",
			Type:         ModelTypeLLM,
			Version:      "4.0",
			Description:  "NVIDIA's Nemotron-4 340B instruction model",
			MaxTokens:    4096,
			MaxBatchSize: 16,
			Status:       "ready",
			AvgLatencyMs: 400,
		},
		"mistralai/mixtral-8x22b-instruct-v0.1": {
			ID:           "mistralai/mixtral-8x22b-instruct-v0.1",
			Name:         "Mixtral 8x22B Instruct",
			Type:         ModelTypeLLM,
			Version:      "0.1",
			Description:  "Mistral AI's Mixtral 8x22B sparse MoE model",
			MaxTokens:    32768,
			MaxBatchSize: 32,
			Status:       "ready",
			AvgLatencyMs: 150,
		},
		// Embedding Models
		"nvidia/nv-embedqa-e5-v5": {
			ID:           "nvidia/nv-embedqa-e5-v5",
			Name:         "NV-EmbedQA E5 v5",
			Type:         ModelTypeEmbedding,
			Version:      "5.0",
			Description:  "NVIDIA embedding model optimized for Q&A retrieval",
			MaxBatchSize: 128,
			Status:       "ready",
			AvgLatencyMs: 20,
		},
		"nvidia/nv-embed-v2": {
			ID:           "nvidia/nv-embed-v2",
			Name:         "NV-Embed v2",
			Type:         ModelTypeEmbedding,
			Version:      "2.0",
			Description:  "NVIDIA general-purpose embedding model",
			MaxBatchSize: 256,
			Status:       "ready",
			AvgLatencyMs: 15,
		},
		// Vision Models
		"nvidia/grounding-dino": {
			ID:               "nvidia/grounding-dino",
			Name:             "Grounding DINO",
			Type:             ModelTypeVision,
			Version:          "1.0",
			Description:      "Open-set object detection with text prompts",
			MaxBatchSize:     16,
			SupportedFormats: []string{"image/jpeg", "image/png", "image/webp"},
			Status:           "ready",
			AvgLatencyMs:     100,
		},
		"nvidia/deplot": {
			ID:               "nvidia/deplot",
			Name:             "DePlot",
			Type:             ModelTypeVision,
			Version:          "1.0",
			Description:      "Chart and plot understanding model",
			MaxBatchSize:     8,
			SupportedFormats: []string{"image/jpeg", "image/png"},
			Status:           "ready",
			AvgLatencyMs:     150,
		},
		// Multimodal Models
		"nvidia/llama-3.2-neva-72b-preview": {
			ID:               "nvidia/llama-3.2-neva-72b-preview",
			Name:             "Llama 3.2 NeVA 72B",
			Type:             ModelTypeMultimodal,
			Version:          "3.2-preview",
			Description:      "Vision-language model for image understanding",
			MaxTokens:        4096,
			MaxBatchSize:     8,
			SupportedFormats: []string{"image/jpeg", "image/png", "image/webp"},
			Status:           "ready",
			AvgLatencyMs:     300,
		},
		"nvidia/vila-1.5": {
			ID:               "nvidia/vila-1.5",
			Name:             "VILA 1.5",
			Type:             ModelTypeMultimodal,
			Version:          "1.5",
			Description:      "Visual language model for complex reasoning",
			MaxTokens:        2048,
			MaxBatchSize:     4,
			SupportedFormats: []string{"image/jpeg", "image/png"},
			Status:           "ready",
			AvgLatencyMs:     250,
		},
		// Audio Models
		"nvidia/canary-1b-asr": {
			ID:               "nvidia/canary-1b-asr",
			Name:             "Canary 1B ASR",
			Type:             ModelTypeAudio,
			Version:          "1.0",
			Description:      "Automatic speech recognition model",
			MaxBatchSize:     16,
			SupportedFormats: []string{"audio/wav", "audio/mp3", "audio/flac"},
			Status:           "ready",
			AvgLatencyMs:     100,
		},
		"nvidia/parakeet-tdt-1.1b": {
			ID:               "nvidia/parakeet-tdt-1.1b",
			Name:             "Parakeet TDT 1.1B",
			Type:             ModelTypeAudio,
			Version:          "1.1",
			Description:      "Text-to-speech model with natural voices",
			MaxBatchSize:     8,
			SupportedFormats: []string{"text/plain"},
			Status:           "ready",
			AvgLatencyMs:     80,
		},
	}
}

// ListModels returns available models.
func (b *SimulatedBackend) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return nil, ErrAlreadyClosed
	}

	models := make([]*ModelInfo, 0, len(b.models))
	for _, m := range b.models {
		models = append(models, m)
	}

	return models, nil
}

// GetModel returns info about a specific model.
func (b *SimulatedBackend) GetModel(ctx context.Context, modelID string) (*ModelInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return nil, ErrAlreadyClosed
	}

	model, ok := b.models[modelID]
	if !ok {
		return nil, ErrModelNotFound
	}

	return model, nil
}

// Chat performs simulated chat completion.
func (b *SimulatedBackend) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	b.mu.RLock()

	if b.closed {
		b.mu.RUnlock()
		return nil, ErrAlreadyClosed
	}

	latency := b.latencyMs
	b.mu.RUnlock()

	// Simulate processing latency
	time.Sleep(time.Duration(latency) * time.Millisecond)

	// Check if model exists
	if _, err := b.GetModel(ctx, req.Model); err != nil {
		return nil, err
	}

	// Generate simulated response
	responseID := generateID()
	responseContent := b.generateChatResponse(req)
	promptTokens := b.estimateTokens(req.Messages)
	completionTokens := b.estimateTokens([]ChatMessage{{Content: responseContent}})

	return &ChatResponse{
		ID:      responseID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{
				Index: 0,
				Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{
					Role:    "assistant",
					Content: responseContent,
				},
				FinishReason: "stop",
			},
		},
		Usage: struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}, nil
}

// ChatStream performs simulated streaming chat completion.
func (b *SimulatedBackend) ChatStream(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	b.mu.RLock()

	if b.closed {
		b.mu.RUnlock()
		return nil, ErrAlreadyClosed
	}

	b.mu.RUnlock()

	// Check if model exists
	if _, err := b.GetModel(ctx, req.Model); err != nil {
		return nil, err
	}

	ch := make(chan *ChatResponse, 10)

	go func() {
		defer close(ch)

		responseID := generateID()
		responseContent := b.generateChatResponse(req)
		words := strings.Split(responseContent, " ")

		for i, word := range words {
			select {
			case <-ctx.Done():
				return
			default:
				// Stream word by word
				content := word
				if i < len(words)-1 {
					content += " "
				}

				finishReason := ""
				if i == len(words)-1 {
					finishReason = "stop"
				}

				ch <- &ChatResponse{
					ID:      responseID,
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   req.Model,
					Choices: []struct {
						Index   int `json:"index"`
						Message struct {
							Role    string `json:"role"`
							Content string `json:"content"`
						} `json:"message"`
						FinishReason string `json:"finish_reason"`
					}{
						{
							Index: 0,
							Message: struct {
								Role    string `json:"role"`
								Content string `json:"content"`
							}{
								Role:    "assistant",
								Content: content,
							},
							FinishReason: finishReason,
						},
					},
				}

				// Simulate streaming delay
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()

	return ch, nil
}

// Embed generates simulated embeddings.
func (b *SimulatedBackend) Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	b.mu.RLock()

	if b.closed {
		b.mu.RUnlock()
		return nil, ErrAlreadyClosed
	}

	latency := b.latencyMs
	b.mu.RUnlock()

	// Simulate processing latency
	time.Sleep(time.Duration(latency) * time.Millisecond)

	// Check if model exists
	if _, err := b.GetModel(ctx, req.Model); err != nil {
		return nil, err
	}

	// Generate simulated embeddings
	data := make([]struct {
		Object    string    `json:"object"`
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	}, len(req.Input))

	totalTokens := 0

	for i, input := range req.Input {
		// Generate deterministic but varied embedding based on input
		embedding := b.generateEmbedding(input)
		data[i] = struct {
			Object    string    `json:"object"`
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		}{
			Object:    "embedding",
			Embedding: embedding,
			Index:     i,
		}
		totalTokens += len(strings.Split(input, " "))
	}

	return &EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  req.Model,
		Usage: struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		}{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}, nil
}

// Vision performs simulated vision inference.
func (b *SimulatedBackend) Vision(ctx context.Context, req *VisionRequest) (*VisionResponse, error) {
	b.mu.RLock()

	if b.closed {
		b.mu.RUnlock()
		return nil, ErrAlreadyClosed
	}

	latency := b.latencyMs
	b.mu.RUnlock()

	// Simulate processing latency
	time.Sleep(time.Duration(latency) * time.Millisecond)

	// Check if model exists
	model, err := b.GetModel(ctx, req.Model)
	if err != nil {
		return nil, err
	}

	if model.Type != ModelTypeVision && model.Type != ModelTypeMultimodal {
		return nil, ErrInvalidInput
	}

	// Generate simulated vision results
	results := b.generateVisionResults(req.Task)

	return &VisionResponse{
		Model:   req.Model,
		Results: results,
		Latency: int(latency),
	}, nil
}

// Infer performs simulated generic inference.
func (b *SimulatedBackend) Infer(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	b.mu.RLock()

	if b.closed {
		b.mu.RUnlock()
		return nil, ErrAlreadyClosed
	}

	latency := b.latencyMs
	b.mu.RUnlock()

	// Simulate processing latency
	time.Sleep(time.Duration(latency) * time.Millisecond)

	// Check if model exists
	model, err := b.GetModel(ctx, req.Model)
	if err != nil {
		return nil, err
	}

	// Generate response based on model type
	var (
		output     interface{}
		tokensUsed int
	)

	switch model.Type {
	case ModelTypeLLM:
		output = "This is a simulated inference response for your input."
		tokensUsed = 15
	case ModelTypeEmbedding:
		output = b.generateEmbedding(fmt.Sprintf("%v", req.Input))
		tokensUsed = 10
	case ModelTypeVision:
		output = map[string]interface{}{
			"detections": []map[string]interface{}{
				{"label": "object", "confidence": 0.95},
			},
		}
	case ModelTypeAudio:
		output = map[string]interface{}{
			"transcription": "This is a simulated transcription of the audio.",
		}
	case ModelTypeMultimodal:
		output = "Based on the image, I can see various elements that suggest..."
		tokensUsed = 20
	default:
		output = "Simulated inference result"
	}

	return &InferenceResponse{
		ID:         generateID(),
		Model:      req.Model,
		Output:     output,
		LatencyMs:  int(latency),
		TokensUsed: tokensUsed,
	}, nil
}

// Batch performs simulated batch inference.
func (b *SimulatedBackend) Batch(ctx context.Context, req *BatchRequest) (*BatchResponse, error) {
	b.mu.RLock()

	if b.closed {
		b.mu.RUnlock()
		return nil, ErrAlreadyClosed
	}

	b.mu.RUnlock()

	start := time.Now()

	responses := make([]InferenceResponse, len(req.Requests))
	successCount := 0
	failureCount := 0

	for i, inferReq := range req.Requests {
		// Use the batch model if individual request doesn't specify
		if inferReq.Model == "" {
			inferReq.Model = req.Model
		}

		resp, err := b.Infer(ctx, &inferReq)
		if err != nil {
			failureCount++
			responses[i] = InferenceResponse{
				ID:    generateID(),
				Model: inferReq.Model,
				Error: err.Error(),
			}
		} else {
			successCount++
			responses[i] = *resp
		}
	}

	return &BatchResponse{
		Responses:      responses,
		TotalLatencyMs: int(time.Since(start).Milliseconds()),
		SuccessCount:   successCount,
		FailureCount:   failureCount,
	}, nil
}

// HealthCheck checks NIM service health.
func (b *SimulatedBackend) HealthCheck(ctx context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return ErrAlreadyClosed
	}

	return nil
}

// Close closes the backend.
func (b *SimulatedBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrAlreadyClosed
	}

	b.closed = true

	return nil
}

// SetSimulatedLatency sets the simulated processing latency.
func (b *SimulatedBackend) SetSimulatedLatency(latencyMs int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.latencyMs = latencyMs
}

// SetFailureRate sets the simulated failure rate (0.0 to 1.0).
func (b *SimulatedBackend) SetFailureRate(rate float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failureRate = rate
}

// AddModel adds a custom model to the catalog.
func (b *SimulatedBackend) AddModel(model *ModelInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.models[model.ID] = model
}

// Helper functions

func generateID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)

	return "chatcmpl-" + hex.EncodeToString(bytes)
}

func (b *SimulatedBackend) generateChatResponse(req *ChatRequest) string {
	// Generate a contextual response based on the last message
	if len(req.Messages) == 0 {
		return "I'm ready to help. What would you like to discuss?"
	}

	lastMessage := req.Messages[len(req.Messages)-1]
	content := strings.ToLower(lastMessage.Content)

	// Simulated responses based on content
	switch {
	case strings.Contains(content, "hello") || strings.Contains(content, "hi"):
		return "Hello! I'm a simulated NIM assistant. How can I help you today?"
	case strings.Contains(content, "code") || strings.Contains(content, "programming"):
		return "I can help with coding questions. In simulated mode, I provide example responses. For production use, please configure the actual NIM API endpoint."
	case strings.Contains(content, "image"):
		return "I can analyze images when used with vision-capable models. Please provide an image URL or upload an image for analysis."
	case strings.Contains(content, "summarize"):
		return "To summarize: The key points from your input have been analyzed. This is a simulated summary response demonstrating the NIM integration capabilities."
	case strings.Contains(content, "translate"):
		return "Translation services are available through NIM. This simulated response demonstrates the translation capability."
	default:
		return fmt.Sprintf("I understand you're asking about: %s. In simulation mode, I provide generic responses. Configure the NIM API for actual inference capabilities.", content[:min(len(content), 50)])
	}
}

func (b *SimulatedBackend) estimateTokens(messages []ChatMessage) int {
	total := 0
	for _, msg := range messages {
		// Rough estimate: 1 token per 4 characters
		total += len(msg.Content) / 4
		total += 4 // Role and formatting overhead
	}

	return max(total, 1)
}

func (b *SimulatedBackend) generateEmbedding(input string) []float64 {
	// Generate a 1024-dimensional embedding
	// Use a simple hash-based approach for deterministic results
	embedding := make([]float64, 1024)

	// Create a simple hash of the input
	hash := 0
	for _, c := range input {
		hash = hash*31 + int(c)
	}

	// Generate embedding values
	for i := range embedding {
		// Use sine/cosine for varied but deterministic values
		embedding[i] = math.Sin(float64(hash+i*17) * 0.001)
	}

	// Normalize the embedding
	var norm float64
	for _, v := range embedding {
		norm += v * v
	}

	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range embedding {
			embedding[i] /= norm
		}
	}

	return embedding
}

func (b *SimulatedBackend) generateVisionResults(task string) []struct {
	Label       string  `json:"label,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	BoundingBox *struct {
		X1 float64 `json:"x1"`
		Y1 float64 `json:"y1"`
		X2 float64 `json:"x2"`
		Y2 float64 `json:"y2"`
	} `json:"bounding_box,omitempty"`
	Mask [][]int `json:"mask,omitempty"`
} {
	switch task {
	case "detection":
		return []struct {
			Label       string  `json:"label,omitempty"`
			Confidence  float64 `json:"confidence,omitempty"`
			BoundingBox *struct {
				X1 float64 `json:"x1"`
				Y1 float64 `json:"y1"`
				X2 float64 `json:"x2"`
				Y2 float64 `json:"y2"`
			} `json:"bounding_box,omitempty"`
			Mask [][]int `json:"mask,omitempty"`
		}{
			{
				Label:      "person",
				Confidence: 0.95,
				BoundingBox: &struct {
					X1 float64 `json:"x1"`
					Y1 float64 `json:"y1"`
					X2 float64 `json:"x2"`
					Y2 float64 `json:"y2"`
				}{
					X1: 100, Y1: 50, X2: 300, Y2: 400,
				},
			},
			{
				Label:      "car",
				Confidence: 0.88,
				BoundingBox: &struct {
					X1 float64 `json:"x1"`
					Y1 float64 `json:"y1"`
					X2 float64 `json:"x2"`
					Y2 float64 `json:"y2"`
				}{
					X1: 400, Y1: 200, X2: 600, Y2: 350,
				},
			},
		}
	case "classification":
		return []struct {
			Label       string  `json:"label,omitempty"`
			Confidence  float64 `json:"confidence,omitempty"`
			BoundingBox *struct {
				X1 float64 `json:"x1"`
				Y1 float64 `json:"y1"`
				X2 float64 `json:"x2"`
				Y2 float64 `json:"y2"`
			} `json:"bounding_box,omitempty"`
			Mask [][]int `json:"mask,omitempty"`
		}{
			{Label: "landscape", Confidence: 0.85},
			{Label: "outdoor", Confidence: 0.78},
			{Label: "nature", Confidence: 0.65},
		}
	case "segmentation":
		// Return a simple 3x3 mask for testing
		return []struct {
			Label       string  `json:"label,omitempty"`
			Confidence  float64 `json:"confidence,omitempty"`
			BoundingBox *struct {
				X1 float64 `json:"x1"`
				Y1 float64 `json:"y1"`
				X2 float64 `json:"x2"`
				Y2 float64 `json:"y2"`
			} `json:"bounding_box,omitempty"`
			Mask [][]int `json:"mask,omitempty"`
		}{
			{
				Label:      "sky",
				Confidence: 0.92,
				Mask:       [][]int{{1, 1, 1}, {0, 0, 0}, {0, 0, 0}},
			},
			{
				Label:      "ground",
				Confidence: 0.89,
				Mask:       [][]int{{0, 0, 0}, {0, 0, 0}, {1, 1, 1}},
			},
		}
	default:
		return []struct {
			Label       string  `json:"label,omitempty"`
			Confidence  float64 `json:"confidence,omitempty"`
			BoundingBox *struct {
				X1 float64 `json:"x1"`
				Y1 float64 `json:"y1"`
				X2 float64 `json:"x2"`
				Y2 float64 `json:"y2"`
			} `json:"bounding_box,omitempty"`
			Mask [][]int `json:"mask,omitempty"`
		}{
			{Label: "unknown", Confidence: 0.50},
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}

	return b
}
