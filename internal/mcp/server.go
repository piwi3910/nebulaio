// Package mcp implements Model Context Protocol (MCP) server for AI agent workflows
// This enables AI agents to interact with object storage through a standardized protocol
package mcp

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
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// MCP Protocol Version.
const MCPVersion = "2024-11-05"

// MCP Errors.
var (
	ErrInvalidRequest   = errors.New("invalid request")
	ErrMethodNotFound   = errors.New("method not found")
	ErrResourceNotFound = errors.New("resource not found")
	ErrToolNotFound     = errors.New("tool not found")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrRateLimited      = errors.New("rate limited")
	ErrInternalError    = errors.New("internal server error")
)

// Server implements the MCP server for AI agent interactions.
type Server struct {
	store     ObjectStore
	config    *ServerConfig
	tools     map[string]*Tool
	resources map[string]*Resource
	prompts   map[string]*Prompt
	metrics   *ServerMetrics
	sessions  sync.Map
	mu        sync.RWMutex
}

// ServerConfig configures the MCP server.
type ServerConfig struct {
	// ServerName is the name of this MCP server
	ServerName string

	// ServerVersion is the version of this server
	ServerVersion string

	// MaxRequestSize is the maximum request size in bytes
	MaxRequestSize int64

	// SessionTimeout is the session timeout duration
	SessionTimeout time.Duration

	// RateLimitPerMinute is the rate limit per session
	RateLimitPerMinute int

	// EnableLogging enables request/response logging
	EnableLogging bool

	// EnableMetrics enables metrics collection
	EnableMetrics bool
}

// DefaultServerConfig returns sensible defaults.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		ServerName:         "nebulaio-mcp",
		ServerVersion:      "1.0.0",
		MaxRequestSize:     10 << 20, // 10MB
		SessionTimeout:     1 * time.Hour,
		RateLimitPerMinute: 100,
		EnableLogging:      true,
		EnableMetrics:      true,
	}
}

// ObjectStore interface for storage operations.
type ObjectStore interface {
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, map[string]string, error)
	PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, metadata map[string]string) (string, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	ListObjects(ctx context.Context, bucket, prefix string, maxKeys int) ([]ObjectInfo, error)
	ListBuckets(ctx context.Context) ([]BucketInfo, error)
	HeadObject(ctx context.Context, bucket, key string) (*ObjectMetadata, error)
}

// ObjectInfo contains object metadata.
type ObjectInfo struct {
	LastModified time.Time
	Key          string
	ETag         string
	ContentType  string
	Size         int64
}

// BucketInfo contains bucket metadata.
type BucketInfo struct {
	CreationDate time.Time
	Name         string
}

// ObjectMetadata contains detailed object metadata.
type ObjectMetadata struct {
	LastModified time.Time
	Metadata     map[string]string
	ETag         string
	ContentType  string
	Size         int64
}

// Tool represents an MCP tool that agents can invoke.
type Tool struct {
	InputSchema map[string]interface{} `json:"inputSchema"`
	Handler     ToolHandler            `json:"-"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
}

// ToolHandler is a function that handles tool invocations.
type ToolHandler func(ctx context.Context, args map[string]interface{}) (*ToolResult, error)

// ToolResult is the result of a tool invocation.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock represents content in MCP messages.
type ContentBlock struct {
	Type     string `json:"type"` // "text", "image", "resource"
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// Resource represents an MCP resource.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Prompt represents an MCP prompt template.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument is an argument for a prompt.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Session represents an MCP session.
type Session struct {
	CreatedAt    time.Time
	LastActivity time.Time
	Capabilities *ClientCapabilities
	ID           string
	RequestCount int64
	mu           sync.RWMutex
}

// ClientCapabilities describes client capabilities.
type ClientCapabilities struct {
	Experimental map[string]interface{} `json:"experimental,omitempty"`
	Roots        *RootsCapability       `json:"roots,omitempty"`
	Sampling     map[string]interface{} `json:"sampling,omitempty"`
}

// RootsCapability for roots support.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerMetrics tracks MCP server metrics.
type ServerMetrics struct {
	mu               sync.RWMutex
	RequestsTotal    int64
	RequestsSuccess  int64
	RequestsFailed   int64
	ToolInvocations  int64
	ResourceReads    int64
	ActiveSessions   int64
	BytesTransferred int64
	LatencySum       time.Duration
	LatencyCount     int64
}

// NewServer creates a new MCP server.
func NewServer(store ObjectStore, config *ServerConfig) *Server {
	if config == nil {
		config = DefaultServerConfig()
	}

	s := &Server{
		config:    config,
		store:     store,
		tools:     make(map[string]*Tool),
		resources: make(map[string]*Resource),
		prompts:   make(map[string]*Prompt),
		metrics:   &ServerMetrics{},
	}

	// Register built-in tools
	s.registerBuiltInTools()

	// Register built-in prompts
	s.registerBuiltInPrompts()

	return s
}

// registerBuiltInTools registers the built-in S3 tools.
func (s *Server) registerBuiltInTools() {
	// List buckets tool
	s.RegisterTool(&Tool{
		Name:        "list_buckets",
		Description: "List all available S3 buckets",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: s.handleListBuckets,
	})

	// List objects tool
	s.RegisterTool(&Tool{
		Name:        "list_objects",
		Description: "List objects in a bucket with optional prefix filter",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"bucket": map[string]interface{}{
					"type":        "string",
					"description": "The bucket name",
				},
				"prefix": map[string]interface{}{
					"type":        "string",
					"description": "Optional prefix to filter objects",
				},
				"max_keys": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of objects to return (default: 1000)",
				},
			},
			"required": []string{"bucket"},
		},
		Handler: s.handleListObjects,
	})

	// Get object tool
	s.RegisterTool(&Tool{
		Name:        "get_object",
		Description: "Retrieve an object from S3 storage",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"bucket": map[string]interface{}{
					"type":        "string",
					"description": "The bucket name",
				},
				"key": map[string]interface{}{
					"type":        "string",
					"description": "The object key",
				},
			},
			"required": []string{"bucket", "key"},
		},
		Handler: s.handleGetObject,
	})

	// Put object tool
	s.RegisterTool(&Tool{
		Name:        "put_object",
		Description: "Store an object in S3 storage",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"bucket": map[string]interface{}{
					"type":        "string",
					"description": "The bucket name",
				},
				"key": map[string]interface{}{
					"type":        "string",
					"description": "The object key",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The content to store",
				},
				"content_type": map[string]interface{}{
					"type":        "string",
					"description": "The content MIME type",
				},
			},
			"required": []string{"bucket", "key", "content"},
		},
		Handler: s.handlePutObject,
	})

	// Delete object tool
	s.RegisterTool(&Tool{
		Name:        "delete_object",
		Description: "Delete an object from S3 storage",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"bucket": map[string]interface{}{
					"type":        "string",
					"description": "The bucket name",
				},
				"key": map[string]interface{}{
					"type":        "string",
					"description": "The object key",
				},
			},
			"required": []string{"bucket", "key"},
		},
		Handler: s.handleDeleteObject,
	})

	// Head object tool
	s.RegisterTool(&Tool{
		Name:        "head_object",
		Description: "Get metadata for an object without retrieving the content",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"bucket": map[string]interface{}{
					"type":        "string",
					"description": "The bucket name",
				},
				"key": map[string]interface{}{
					"type":        "string",
					"description": "The object key",
				},
			},
			"required": []string{"bucket", "key"},
		},
		Handler: s.handleHeadObject,
	})

	// Search objects tool
	s.RegisterTool(&Tool{
		Name:        "search_objects",
		Description: "Search for objects matching a pattern",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"bucket": map[string]interface{}{
					"type":        "string",
					"description": "The bucket name",
				},
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Search pattern (supports * wildcard)",
				},
			},
			"required": []string{"bucket", "pattern"},
		},
		Handler: s.handleSearchObjects,
	})

	// Copy object tool
	s.RegisterTool(&Tool{
		Name:        "copy_object",
		Description: "Copy an object within or between buckets",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_bucket": map[string]interface{}{
					"type":        "string",
					"description": "The source bucket name",
				},
				"source_key": map[string]interface{}{
					"type":        "string",
					"description": "The source object key",
				},
				"dest_bucket": map[string]interface{}{
					"type":        "string",
					"description": "The destination bucket name",
				},
				"dest_key": map[string]interface{}{
					"type":        "string",
					"description": "The destination object key",
				},
			},
			"required": []string{"source_bucket", "source_key", "dest_bucket", "dest_key"},
		},
		Handler: s.handleCopyObject,
	})
}

// registerBuiltInPrompts registers built-in prompt templates.
func (s *Server) registerBuiltInPrompts() {
	s.RegisterPrompt(&Prompt{
		Name:        "analyze_data",
		Description: "Analyze data stored in an S3 object",
		Arguments: []PromptArgument{
			{Name: "bucket", Description: "The bucket containing the data", Required: true},
			{Name: "key", Description: "The object key", Required: true},
			{Name: "analysis_type", Description: "Type of analysis: summary, statistics, or insights", Required: false},
		},
	})

	s.RegisterPrompt(&Prompt{
		Name:        "generate_report",
		Description: "Generate a report from multiple objects",
		Arguments: []PromptArgument{
			{Name: "bucket", Description: "The bucket containing the data", Required: true},
			{Name: "prefix", Description: "Prefix to filter objects", Required: false},
			{Name: "format", Description: "Output format: markdown, json, or html", Required: false},
		},
	})
}

// RegisterTool registers a new tool.
func (s *Server) RegisterTool(tool *Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tools[tool.Name] = tool
}

// RegisterResource registers a new resource.
func (s *Server) RegisterResource(resource *Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.resources[resource.URI] = resource
}

// RegisterPrompt registers a new prompt.
func (s *Server) RegisterPrompt(prompt *Prompt) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.prompts[prompt.Name] = prompt
}

// Tool handlers

func (s *Server) handleListBuckets(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	buckets, err := s.store.ListBuckets(ctx)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error listing buckets: %v", err)}},
			IsError: true,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("Available buckets:\n")

	for _, b := range buckets {
		sb.WriteString(fmt.Sprintf("- %s (created: %s)\n", b.Name, b.CreationDate.Format(time.RFC3339)))
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: sb.String()}},
	}, nil
}

func (s *Server) handleListObjects(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	bucket, ok := args["bucket"].(string)
	if !ok || bucket == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: bucket"}},
			IsError: true,
		}, nil
	}

	prefix := ""
	if p, ok := args["prefix"].(string); ok {
		prefix = p
	}

	maxKeys := 1000
	if m, ok := args["max_keys"].(float64); ok {
		maxKeys = int(m)
	}

	objects, err := s.store.ListObjects(ctx, bucket, prefix, maxKeys)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error listing objects: %v", err)}},
			IsError: true,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("Objects in " + bucket)

	if prefix != "" {
		sb.WriteString(fmt.Sprintf(" (prefix: %s)", prefix))
	}

	sb.WriteString(":\n")

	for _, obj := range objects {
		sb.WriteString(fmt.Sprintf("- %s (size: %d bytes, modified: %s)\n",
			obj.Key, obj.Size, obj.LastModified.Format(time.RFC3339)))
	}

	if len(objects) == 0 {
		sb.WriteString("(no objects found)\n")
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: sb.String()}},
	}, nil
}

func (s *Server) handleGetObject(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	bucket, ok := args["bucket"].(string)
	if !ok || bucket == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: bucket"}},
			IsError: true,
		}, nil
	}

	key, ok := args["key"].(string)
	if !ok || key == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: key"}},
			IsError: true,
		}, nil
	}

	reader, metadata, err := s.store.GetObject(ctx, bucket, key)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error getting object: %v", err)}},
			IsError: true,
		}, nil
	}

	defer func() { _ = reader.Close() }()

	// Read content
	data, err := io.ReadAll(reader)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error reading object: %v", err)}},
			IsError: true,
		}, nil
	}

	atomic.AddInt64(&s.metrics.BytesTransferred, int64(len(data)))
	atomic.AddInt64(&s.metrics.ResourceReads, 1)

	contentType := metadata["Content-Type"]
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Return as text if it's text content
	if strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		contentType == "application/xml" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: string(data)}},
		}, nil
	}

	// Return as base64 for binary content
	return &ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: fmt.Sprintf("Retrieved binary object: %s/%s (%d bytes, type: %s)", bucket, key, len(data), contentType)},
		},
	}, nil
}

func (s *Server) handlePutObject(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	bucket, ok := args["bucket"].(string)
	if !ok || bucket == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: bucket"}},
			IsError: true,
		}, nil
	}

	key, ok := args["key"].(string)
	if !ok || key == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: key"}},
			IsError: true,
		}, nil
	}

	content, ok := args["content"].(string)
	if !ok {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: content"}},
			IsError: true,
		}, nil
	}

	contentType := "text/plain"
	if ct, ok := args["content_type"].(string); ok && ct != "" {
		contentType = ct
	}

	metadata := map[string]string{
		"Content-Type": contentType,
	}

	data := []byte(content)

	etag, err := s.store.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), metadata)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error putting object: %v", err)}},
			IsError: true,
		}, nil
	}

	atomic.AddInt64(&s.metrics.BytesTransferred, int64(len(data)))

	return &ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: fmt.Sprintf("Successfully stored object: %s/%s (ETag: %s, Size: %d bytes)", bucket, key, etag, len(data))},
		},
	}, nil
}

func (s *Server) handleDeleteObject(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	bucket, ok := args["bucket"].(string)
	if !ok || bucket == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: bucket"}},
			IsError: true,
		}, nil
	}

	key, ok := args["key"].(string)
	if !ok || key == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: key"}},
			IsError: true,
		}, nil
	}

	err := s.store.DeleteObject(ctx, bucket, key)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error deleting object: %v", err)}},
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: fmt.Sprintf("Successfully deleted object: %s/%s", bucket, key)},
		},
	}, nil
}

func (s *Server) handleHeadObject(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	bucket, ok := args["bucket"].(string)
	if !ok || bucket == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: bucket"}},
			IsError: true,
		}, nil
	}

	key, ok := args["key"].(string)
	if !ok || key == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: key"}},
			IsError: true,
		}, nil
	}

	meta, err := s.store.HeadObject(ctx, bucket, key)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error getting object metadata: %v", err)}},
			IsError: true,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Object: %s/%s\n", bucket, key))
	sb.WriteString(fmt.Sprintf("Size: %d bytes\n", meta.Size))
	sb.WriteString(fmt.Sprintf("Content-Type: %s\n", meta.ContentType))
	sb.WriteString(fmt.Sprintf("ETag: %s\n", meta.ETag))
	sb.WriteString(fmt.Sprintf("Last-Modified: %s\n", meta.LastModified.Format(time.RFC3339)))

	if len(meta.Metadata) > 0 {
		sb.WriteString("Metadata:\n")

		for k, v := range meta.Metadata {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: sb.String()}},
	}, nil
}

func (s *Server) handleSearchObjects(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	bucket, ok := args["bucket"].(string)
	if !ok || bucket == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: bucket"}},
			IsError: true,
		}, nil
	}

	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: pattern"}},
			IsError: true,
		}, nil
	}

	// Convert pattern to prefix (simple implementation)
	prefix := ""
	if idx := strings.Index(pattern, "*"); idx > 0 {
		prefix = pattern[:idx]
	}

	objects, err := s.store.ListObjects(ctx, bucket, prefix, 1000)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error searching objects: %v", err)}},
			IsError: true,
		}, nil
	}

	// Filter by pattern
	var matches []ObjectInfo

	for _, obj := range objects {
		if matchPattern(obj.Key, pattern) {
			matches = append(matches, obj)
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for pattern '%s' in %s:\n", pattern, bucket))

	for _, obj := range matches {
		sb.WriteString(fmt.Sprintf("- %s (size: %d bytes)\n", obj.Key, obj.Size))
	}

	if len(matches) == 0 {
		sb.WriteString("(no matching objects found)\n")
	} else {
		sb.WriteString(fmt.Sprintf("\nTotal: %d matching objects\n", len(matches)))
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: sb.String()}},
	}, nil
}

func (s *Server) handleCopyObject(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	srcBucket, ok := args["source_bucket"].(string)
	if !ok || srcBucket == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: source_bucket"}},
			IsError: true,
		}, nil
	}

	srcKey, ok := args["source_key"].(string)
	if !ok || srcKey == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: source_key"}},
			IsError: true,
		}, nil
	}

	dstBucket, ok := args["dest_bucket"].(string)
	if !ok || dstBucket == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: dest_bucket"}},
			IsError: true,
		}, nil
	}

	dstKey, ok := args["dest_key"].(string)
	if !ok || dstKey == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required parameter: dest_key"}},
			IsError: true,
		}, nil
	}

	// Get source object
	reader, metadata, err := s.store.GetObject(ctx, srcBucket, srcKey)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error reading source object: %v", err)}},
			IsError: true,
		}, nil
	}

	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error reading source content: %v", err)}},
			IsError: true,
		}, nil
	}

	// Put to destination
	etag, err := s.store.PutObject(ctx, dstBucket, dstKey, bytes.NewReader(data), int64(len(data)), metadata)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error writing destination object: %v", err)}},
			IsError: true,
		}, nil
	}

	atomic.AddInt64(&s.metrics.BytesTransferred, int64(len(data)*2)) // Read + Write

	return &ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: fmt.Sprintf("Successfully copied %s/%s to %s/%s (ETag: %s, Size: %d bytes)", srcBucket, srcKey, dstBucket, dstKey, etag, len(data))},
		},
	}, nil
}

// MCP Protocol Messages

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	ID      interface{}   `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
	JSONRPC string        `json:"jsonrpc"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message"`
	Code    int         `json:"code"`
}

// InitializeParams for initialize request.
type InitializeParams struct {
	Capabilities    *ClientCapabilities `json:"capabilities"`
	ClientInfo      *ClientInfo         `json:"clientInfo"`
	ProtocolVersion string              `json:"protocolVersion"`
}

// ClientInfo describes the client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// InitializeResult for initialize response.
type InitializeResult struct {
	Capabilities    *ServerCapabilities `json:"capabilities"`
	ServerInfo      *ServerInfo         `json:"serverInfo"`
	ProtocolVersion string              `json:"protocolVersion"`
}

// ServerCapabilities describes server capabilities.
type ServerCapabilities struct {
	Experimental map[string]interface{} `json:"experimental,omitempty"`
	Logging      map[string]interface{} `json:"logging,omitempty"`
	Prompts      *PromptsCapability     `json:"prompts,omitempty"`
	Resources    *ResourcesCapability   `json:"resources,omitempty"`
	Tools        *ToolsCapability       `json:"tools,omitempty"`
}

// PromptsCapability for prompts support.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability for resources support.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// ToolsCapability for tools support.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerInfo describes the server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// HandleHTTP handles HTTP requests for the MCP server.
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()

	atomic.AddInt64(&s.metrics.RequestsTotal, 1)

	// Read request body
	body, err := io.ReadAll(io.LimitReader(r.Body, s.config.MaxRequestSize))
	if err != nil {
		s.sendError(w, nil, -32700, "Parse error")
		atomic.AddInt64(&s.metrics.RequestsFailed, 1)

		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.sendError(w, nil, -32700, "Parse error")
		atomic.AddInt64(&s.metrics.RequestsFailed, 1)

		return
	}

	if req.JSONRPC != "2.0" {
		s.sendError(w, req.ID, -32600, "Invalid Request")
		atomic.AddInt64(&s.metrics.RequestsFailed, 1)

		return
	}

	// Handle the request
	result, err := s.handleRequest(r.Context(), &req)
	if err != nil {
		s.sendError(w, req.ID, -32603, err.Error())
		atomic.AddInt64(&s.metrics.RequestsFailed, 1)

		return
	}

	// Send response
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Response already started, log error but can't do much
		_ = err
	}

	atomic.AddInt64(&s.metrics.RequestsSuccess, 1)

	s.metrics.mu.Lock()
	s.metrics.LatencySum += time.Since(start)
	s.metrics.LatencyCount++
	s.metrics.mu.Unlock()
}

func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest) (interface{}, error) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, req.Params)
	case "initialized":
		return nil, nil
	case "tools/list":
		return s.handleListTools(ctx)
	case "tools/call":
		return s.handleCallTool(ctx, req.Params)
	case "resources/list":
		return s.handleListResources(ctx)
	case "resources/read":
		return s.handleReadResource(ctx, req.Params)
	case "prompts/list":
		return s.handleListPrompts(ctx)
	case "prompts/get":
		return s.handleGetPrompt(ctx, req.Params)
	case "ping":
		return map[string]interface{}{}, nil
	default:
		return nil, ErrMethodNotFound
	}
}

func (s *Server) handleInitialize(ctx context.Context, params json.RawMessage) (*InitializeResult, error) {
	var initParams InitializeParams
	err := json.Unmarshal(params, &initParams)
	if err != nil {
		return nil, err
	}

	// Create new session
	sessionID := uuid.New().String()
	session := &Session{
		ID:           sessionID,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		Capabilities: initParams.Capabilities,
	}
	s.sessions.Store(sessionID, session)
	atomic.AddInt64(&s.metrics.ActiveSessions, 1)

	return &InitializeResult{
		ProtocolVersion: MCPVersion,
		Capabilities: &ServerCapabilities{
			Tools: &ToolsCapability{
				ListChanged: true,
			},
			Resources: &ResourcesCapability{
				Subscribe:   true,
				ListChanged: true,
			},
			Prompts: &PromptsCapability{
				ListChanged: true,
			},
		},
		ServerInfo: &ServerInfo{
			Name:    s.config.ServerName,
			Version: s.config.ServerVersion,
		},
	}, nil
}

func (s *Server) handleListTools(ctx context.Context) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]map[string]interface{}, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		})
	}

	return map[string]interface{}{
		"tools": tools,
	}, nil
}

func (s *Server) handleCallTool(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	var callParams struct {
		Arguments map[string]interface{} `json:"arguments"`
		Name      string                 `json:"name"`
	}

	err := json.Unmarshal(params, &callParams)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	tool, ok := s.tools[callParams.Name]
	s.mu.RUnlock()

	if !ok {
		return nil, ErrToolNotFound
	}

	atomic.AddInt64(&s.metrics.ToolInvocations, 1)

	return tool.Handler(ctx, callParams.Arguments)
}

func (s *Server) handleListResources(ctx context.Context) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resources := make([]Resource, 0, len(s.resources))
	for _, r := range s.resources {
		resources = append(resources, *r)
	}

	// Also list buckets as resources
	buckets, err := s.store.ListBuckets(ctx)
	if err == nil {
		for _, b := range buckets {
			resources = append(resources, Resource{
				URI:         "s3://" + b.Name,
				Name:        b.Name,
				Description: "S3 bucket: " + b.Name,
				MimeType:    "application/x-directory",
			})
		}
	}

	return map[string]interface{}{
		"resources": resources,
	}, nil
}

func (s *Server) handleReadResource(ctx context.Context, params json.RawMessage) (map[string]interface{}, error) {
	var readParams struct {
		URI string `json:"uri"`
	}

	if err := json.Unmarshal(params, &readParams); err != nil {
		return nil, err
	}

	// Parse S3 URI
	if !strings.HasPrefix(readParams.URI, "s3://") {
		return nil, ErrResourceNotFound
	}

	path := strings.TrimPrefix(readParams.URI, "s3://")

	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 1 {
		return nil, ErrResourceNotFound
	}

	bucket := parts[0]

	key := ""
	if len(parts) > 1 {
		key = parts[1]
	}

	if key == "" {
		// List bucket contents
		objects, err := s.store.ListObjects(ctx, bucket, "", 100)
		if err != nil {
			return nil, err
		}

		var sb strings.Builder
		for _, obj := range objects {
			sb.WriteString(fmt.Sprintf("%s\t%d\t%s\n", obj.Key, obj.Size, obj.LastModified.Format(time.RFC3339)))
		}

		return map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"uri":      readParams.URI,
					"mimeType": "text/plain",
					"text":     sb.String(),
				},
			},
		}, nil
	}

	// Read object
	reader, meta, err := s.store.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	atomic.AddInt64(&s.metrics.ResourceReads, 1)
	atomic.AddInt64(&s.metrics.BytesTransferred, int64(len(data)))

	contentType := meta["Content-Type"]
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      readParams.URI,
				"mimeType": contentType,
				"text":     string(data),
			},
		},
	}, nil
}

func (s *Server) handleListPrompts(ctx context.Context) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prompts := make([]Prompt, 0, len(s.prompts))
	for _, p := range s.prompts {
		prompts = append(prompts, *p)
	}

	return map[string]interface{}{
		"prompts": prompts,
	}, nil
}

func (s *Server) handleGetPrompt(ctx context.Context, params json.RawMessage) (map[string]interface{}, error) {
	var getParams struct {
		Arguments map[string]interface{} `json:"arguments"`
		Name      string                 `json:"name"`
	}

	err := json.Unmarshal(params, &getParams)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	prompt, ok := s.prompts[getParams.Name]
	s.mu.RUnlock()

	if !ok {
		return nil, errors.New("prompt not found")
	}

	// Generate prompt messages based on template
	var messages []map[string]interface{}

	switch prompt.Name {
	case "analyze_data":
		bucket := getParams.Arguments["bucket"].(string)
		key := getParams.Arguments["key"].(string)

		analysisType := "summary"
		if at, ok := getParams.Arguments["analysis_type"].(string); ok {
			analysisType = at
		}

		messages = []map[string]interface{}{
			{
				"role": "user",
				"content": map[string]interface{}{
					"type": "text",
					"text": fmt.Sprintf("Please analyze the data stored in s3://%s/%s. Provide a %s of the content.", bucket, key, analysisType),
				},
			},
		}

	case "generate_report":
		bucket := getParams.Arguments["bucket"].(string)

		prefix := ""
		if p, ok := getParams.Arguments["prefix"].(string); ok {
			prefix = p
		}

		format := "markdown"
		if f, ok := getParams.Arguments["format"].(string); ok {
			format = f
		}

		messages = []map[string]interface{}{
			{
				"role": "user",
				"content": map[string]interface{}{
					"type": "text",
					"text": fmt.Sprintf("Generate a %s report from the data in s3://%s/%s", format, bucket, prefix),
				},
			},
		}
	}

	return map[string]interface{}{
		"description": prompt.Description,
		"messages":    messages,
	}, nil
}

func (s *Server) sendError(w http.ResponseWriter, id interface{}, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC always returns 200
	err := json.NewEncoder(w).Encode(resp)

	if err != nil {
		// Response already started, log error but can't do much
		_ = err
	}
}

// GetMetrics returns server metrics.
func (s *Server) GetMetrics() *ServerMetrics {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	return &ServerMetrics{
		RequestsTotal:    atomic.LoadInt64(&s.metrics.RequestsTotal),
		RequestsSuccess:  atomic.LoadInt64(&s.metrics.RequestsSuccess),
		RequestsFailed:   atomic.LoadInt64(&s.metrics.RequestsFailed),
		ToolInvocations:  atomic.LoadInt64(&s.metrics.ToolInvocations),
		ResourceReads:    atomic.LoadInt64(&s.metrics.ResourceReads),
		ActiveSessions:   atomic.LoadInt64(&s.metrics.ActiveSessions),
		BytesTransferred: atomic.LoadInt64(&s.metrics.BytesTransferred),
		LatencySum:       s.metrics.LatencySum,
		LatencyCount:     s.metrics.LatencyCount,
	}
}

// AverageLatency returns average request latency.
func (m *ServerMetrics) AverageLatency() time.Duration {
	if m.LatencyCount == 0 {
		return 0
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.LatencySum / time.Duration(m.LatencyCount)
}

// matchPattern matches a key against a pattern with * wildcards.
func matchPattern(key, pattern string) bool {
	if pattern == "*" {
		return true
	}

	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return key == pattern
	}

	// Check prefix
	if len(parts[0]) > 0 && !strings.HasPrefix(key, parts[0]) {
		return false
	}

	// Check suffix
	if len(parts[len(parts)-1]) > 0 && !strings.HasSuffix(key, parts[len(parts)-1]) {
		return false
	}

	// Check middle parts
	remaining := key

	for i, part := range parts {
		if part == "" {
			continue
		}

		idx := strings.Index(remaining, part)
		if idx < 0 {
			return false
		}

		if i == 0 && idx != 0 {
			return false
		}

		remaining = remaining[idx+len(part):]
	}

	return true
}
