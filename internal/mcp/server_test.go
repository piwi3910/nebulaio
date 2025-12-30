// Package mcp provides Model Context Protocol (MCP) server implementation for AI agent integration.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants.
const testContentTypeOctetStream = "application/octet-stream"

// Errors for mock service.
var (
	errBucketNotFound = errors.New("bucket not found")
	errObjectNotFound = errors.New("object not found")
)

// MockObjectStore implements the ObjectStore interface for testing.
type MockObjectStore struct {
	buckets map[string]bool
	objects map[string]map[string]*mockObject
}

type mockObject struct {
	metadata    map[string]string
	contentType string
	data        []byte
}

func NewMockObjectStore() *MockObjectStore {
	return &MockObjectStore{
		buckets: make(map[string]bool),
		objects: make(map[string]map[string]*mockObject),
	}
}

func (m *MockObjectStore) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	buckets := make([]BucketInfo, 0, len(m.buckets))
	for name := range m.buckets {
		buckets = append(buckets, BucketInfo{
			Name:         name,
			CreationDate: time.Now(),
		})
	}

	return buckets, nil
}

func (m *MockObjectStore) ListObjects(ctx context.Context, bucket, prefix string, maxKeys int) ([]ObjectInfo, error) {
	if !m.buckets[bucket] {
		return nil, errBucketNotFound
	}

	bucketObjects := m.objects[bucket]
	if bucketObjects == nil {
		return []ObjectInfo{}, nil
	}

	var objects []ObjectInfo

	for key, obj := range bucketObjects {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			objects = append(objects, ObjectInfo{
				Key:          key,
				Size:         int64(len(obj.data)),
				LastModified: time.Now(),
				ContentType:  obj.contentType,
			})
			if maxKeys > 0 && len(objects) >= maxKeys {
				break
			}
		}
	}

	return objects, nil
}

func (m *MockObjectStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, map[string]string, error) {
	if !m.buckets[bucket] {
		return nil, nil, errBucketNotFound
	}

	bucketObjects := m.objects[bucket]
	if bucketObjects == nil {
		return nil, nil, errObjectNotFound
	}

	obj, exists := bucketObjects[key]
	if !exists {
		return nil, nil, errObjectNotFound
	}

	metadata := map[string]string{
		"Content-Type": obj.contentType,
	}
	for k, v := range obj.metadata {
		metadata[k] = v
	}

	return io.NopCloser(bytes.NewReader(obj.data)), metadata, nil
}

func (m *MockObjectStore) PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, metadata map[string]string) (string, error) {
	if !m.buckets[bucket] {
		return "", errBucketNotFound
	}

	content, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}

	if m.objects[bucket] == nil {
		m.objects[bucket] = make(map[string]*mockObject)
	}

	contentType := testContentTypeOctetStream
	if ct, ok := metadata["Content-Type"]; ok {
		contentType = ct
	}

	m.objects[bucket][key] = &mockObject{
		data:        content,
		contentType: contentType,
		metadata:    metadata,
	}

	return "\"mock-etag\"", nil
}

func (m *MockObjectStore) DeleteObject(ctx context.Context, bucket, key string) error {
	if !m.buckets[bucket] {
		return errBucketNotFound
	}

	bucketObjects := m.objects[bucket]
	if bucketObjects == nil {
		return nil
	}

	delete(bucketObjects, key)

	return nil
}

func (m *MockObjectStore) HeadObject(ctx context.Context, bucket, key string) (*ObjectMetadata, error) {
	if !m.buckets[bucket] {
		return nil, errBucketNotFound
	}

	bucketObjects := m.objects[bucket]
	if bucketObjects == nil {
		return nil, errObjectNotFound
	}

	obj, exists := bucketObjects[key]
	if !exists {
		return nil, errObjectNotFound
	}

	return &ObjectMetadata{
		Size:         int64(len(obj.data)),
		ETag:         "\"mock-etag\"",
		ContentType:  obj.contentType,
		LastModified: time.Now(),
		Metadata:     obj.metadata,
	}, nil
}

func (m *MockObjectStore) AddBucket(name string) {
	m.buckets[name] = true
}

func (m *MockObjectStore) AddObject(bucket, key string, data []byte, contentType string) {
	if m.objects[bucket] == nil {
		m.objects[bucket] = make(map[string]*mockObject)
	}

	m.objects[bucket][key] = &mockObject{
		data:        data,
		contentType: contentType,
		metadata:    make(map[string]string),
	}
}

// createTestConfig creates a properly initialized test config.
func createTestConfig() *ServerConfig {
	config := DefaultServerConfig()
	config.ServerName = "test-mcp-server"
	config.ServerVersion = "1.0.0"

	return config
}

// Test helper to create a JSON-RPC request.
func createJSONRPCRequest(method string, params interface{}, id interface{}) []byte {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		ID:      id,
	}

	if params != nil {
		paramsJSON, _ := json.Marshal(params)
		req.Params = paramsJSON
	}

	data, _ := json.Marshal(req)

	return data
}

func TestNewServer(t *testing.T) {
	mockStore := NewMockObjectStore()
	config := createTestConfig()

	server := NewServer(mockStore, config)

	assert.NotNil(t, server)
	assert.NotNil(t, server.tools)
	assert.NotNil(t, server.prompts)
}

func TestNewServerDefaultConfig(t *testing.T) {
	mockStore := NewMockObjectStore()

	server := NewServer(mockStore, nil)

	assert.NotNil(t, server)
	assert.NotNil(t, server.config)
	assert.Equal(t, "nebulaio-mcp", server.config.ServerName)
}

func TestServerInitialize(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	reqBody := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "test-client",
			"version": "1.0.0",
		},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	resultJSON, _ := json.Marshal(resp.Result)

	var initResult InitializeResult

	err = json.Unmarshal(resultJSON, &initResult)
	require.NoError(t, err)
	assert.Equal(t, "2024-11-05", initResult.ProtocolVersion)
	assert.Equal(t, "test-mcp-server", initResult.ServerInfo.Name)
}

func TestServerListTools(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	// First initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Now list tools
	listToolsReq := createJSONRPCRequest("tools/list", nil, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(listToolsReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)

	var toolsResult struct {
		Tools []Tool `json:"tools"`
	}

	err = json.Unmarshal(resultJSON, &toolsResult)
	require.NoError(t, err)

	// Should have built-in tools (8 tools)
	assert.GreaterOrEqual(t, len(toolsResult.Tools), 8)

	// Find list_buckets tool
	var foundListBuckets bool

	for _, tool := range toolsResult.Tools {
		if tool.Name == "list_buckets" {
			foundListBuckets = true

			assert.Contains(t, tool.Description, "bucket")
		}
	}

	assert.True(t, foundListBuckets, "list_buckets tool should be registered")
}

func TestServerListBucketsTool(t *testing.T) {
	mockStore := NewMockObjectStore()
	mockStore.AddBucket("test-bucket-1")
	mockStore.AddBucket("test-bucket-2")

	server := NewServer(mockStore, createTestConfig())

	// Initialize first
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Call list_buckets tool
	callReq := createJSONRPCRequest("tools/call", map[string]interface{}{
		"name":      "list_buckets",
		"arguments": map[string]interface{}{},
	}, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(callReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)

	var callResult ToolResult

	err = json.Unmarshal(resultJSON, &callResult)
	require.NoError(t, err)

	assert.Len(t, callResult.Content, 1)
	assert.Equal(t, "text", callResult.Content[0].Type)
	assert.Contains(t, callResult.Content[0].Text, "bucket")
}

func TestServerPutAndGetObject(t *testing.T) {
	mockStore := NewMockObjectStore()
	mockStore.AddBucket("test-bucket")

	server := NewServer(mockStore, createTestConfig())

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Put object
	testContent := "Hello, MCP World!"
	putReq := createJSONRPCRequest("tools/call", map[string]interface{}{
		"name": "put_object",
		"arguments": map[string]interface{}{
			"bucket":       "test-bucket",
			"key":          "test-file.txt",
			"content":      testContent,
			"content_type": "text/plain",
		},
	}, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(putReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Get object
	getReq := createJSONRPCRequest("tools/call", map[string]interface{}{
		"name": "get_object",
		"arguments": map[string]interface{}{
			"bucket": "test-bucket",
			"key":    "test-file.txt",
		},
	}, 3)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(getReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)

	var callResult ToolResult

	err = json.Unmarshal(resultJSON, &callResult)
	require.NoError(t, err)

	assert.Len(t, callResult.Content, 1)
	assert.Contains(t, callResult.Content[0].Text, testContent)
}

func TestServerDeleteObject(t *testing.T) {
	mockStore := NewMockObjectStore()
	mockStore.AddBucket("test-bucket")
	mockStore.AddObject("test-bucket", "to-delete.txt", []byte("delete me"), "text/plain")

	server := NewServer(mockStore, createTestConfig())

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Delete object
	deleteReq := createJSONRPCRequest("tools/call", map[string]interface{}{
		"name": "delete_object",
		"arguments": map[string]interface{}{
			"bucket": "test-bucket",
			"key":    "to-delete.txt",
		},
	}, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(deleteReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify object is deleted by trying to get it
	getReq := createJSONRPCRequest("tools/call", map[string]interface{}{
		"name": "get_object",
		"arguments": map[string]interface{}{
			"bucket": "test-bucket",
			"key":    "to-delete.txt",
		},
	}, 3)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(getReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	resultJSON, _ := json.Marshal(resp.Result)

	var callResult ToolResult

	err = json.Unmarshal(resultJSON, &callResult)
	require.NoError(t, err)
	assert.True(t, callResult.IsError)
}

func TestServerListPrompts(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// List prompts
	listPromptsReq := createJSONRPCRequest("prompts/list", nil, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(listPromptsReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)

	var promptsResult struct {
		Prompts []Prompt `json:"prompts"`
	}

	err = json.Unmarshal(resultJSON, &promptsResult)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(promptsResult.Prompts), 2)

	var foundAnalyzeData bool

	for _, prompt := range promptsResult.Prompts {
		if prompt.Name == "analyze_data" {
			foundAnalyzeData = true

			assert.NotEmpty(t, prompt.Description)
		}
	}

	assert.True(t, foundAnalyzeData, "analyze_data prompt should be registered")
}

func TestServerGetPrompt(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Get prompt
	getPromptReq := createJSONRPCRequest("prompts/get", map[string]interface{}{
		"name": "analyze_data",
		"arguments": map[string]interface{}{
			"bucket":        "my-bucket",
			"key":           "data/file.csv",
			"analysis_type": "summary",
		},
	}, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(getPromptReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)

	var promptResult map[string]interface{}

	err = json.Unmarshal(resultJSON, &promptResult)
	require.NoError(t, err)

	messages, ok := promptResult["messages"].([]interface{})
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(messages), 1)
}

func TestServerListResources(t *testing.T) {
	mockStore := NewMockObjectStore()
	mockStore.AddBucket("bucket1")
	mockStore.AddBucket("bucket2")

	server := NewServer(mockStore, createTestConfig())

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// List resources
	listResourcesReq := createJSONRPCRequest("resources/list", nil, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(listResourcesReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)

	var resourcesResult struct {
		Resources []Resource `json:"resources"`
	}

	err = json.Unmarshal(resultJSON, &resourcesResult)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(resourcesResult.Resources), 2)

	for _, resource := range resourcesResult.Resources {
		assert.True(t, strings.HasPrefix(resource.URI, "s3://"))
	}
}

func TestServerReadResource(t *testing.T) {
	mockStore := NewMockObjectStore()
	mockStore.AddBucket("test-bucket")
	mockStore.AddObject("test-bucket", "test.txt", []byte("Hello from resource!"), "text/plain")

	server := NewServer(mockStore, createTestConfig())

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Read resource
	readResourceReq := createJSONRPCRequest("resources/read", map[string]interface{}{
		"uri": "s3://test-bucket/test.txt",
	}, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(readResourceReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)

	var readResult struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"contents"`
	}

	err = json.Unmarshal(resultJSON, &readResult)
	require.NoError(t, err)

	assert.Len(t, readResult.Contents, 1)
	assert.Equal(t, "s3://test-bucket/test.txt", readResult.Contents[0].URI)
	assert.Contains(t, readResult.Contents[0].Text, "Hello from resource!")
}

func TestServerHeadObject(t *testing.T) {
	mockStore := NewMockObjectStore()
	mockStore.AddBucket("test-bucket")
	mockStore.AddObject("test-bucket", "metadata-test.txt", []byte("test content for metadata"), "text/plain")

	server := NewServer(mockStore, createTestConfig())

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Head object
	headReq := createJSONRPCRequest("tools/call", map[string]interface{}{
		"name": "head_object",
		"arguments": map[string]interface{}{
			"bucket": "test-bucket",
			"key":    "metadata-test.txt",
		},
	}, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(headReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)

	var callResult ToolResult

	err = json.Unmarshal(resultJSON, &callResult)
	require.NoError(t, err)

	assert.False(t, callResult.IsError)
	assert.Contains(t, callResult.Content[0].Text, "metadata-test.txt")
}

func TestServerInvalidMethod(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	invalidReq := createJSONRPCRequest("invalid/method", nil, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(invalidReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
	// Server returns -32603 (internal error) for unknown methods
	assert.Equal(t, -32603, resp.Error.Code)
}

func TestServerInvalidJSON(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("not valid json"))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32700, resp.Error.Code)
}

func TestServerToolNotFound(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Call non-existent tool
	callReq := createJSONRPCRequest("tools/call", map[string]interface{}{
		"name":      "nonexistent_tool",
		"arguments": map[string]interface{}{},
	}, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(callReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
}

func TestServerMetrics(t *testing.T) {
	mockStore := NewMockObjectStore()
	mockStore.AddBucket("test-bucket")

	server := NewServer(mockStore, createTestConfig())

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Make several tool calls
	for i := range 3 {
		callReq := createJSONRPCRequest("tools/call", map[string]interface{}{
			"name":      "list_buckets",
			"arguments": map[string]interface{}{},
		}, i+2)

		req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(callReq))
		req.Header.Set("Content-Type", "application/json")

		w = httptest.NewRecorder()
		server.HandleHTTP(w, req)
	}

	metrics := server.GetMetrics()
	assert.Equal(t, int64(4), metrics.RequestsTotal)
	assert.Equal(t, int64(4), metrics.RequestsSuccess)
}

func TestServerRegisterCustomTool(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	customTool := &Tool{
		Name:        "custom_tool",
		Description: "A custom tool for testing",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "A message to echo",
				},
			},
			"required": []string{"message"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
			msg, _ := args["message"].(string)

			return &ToolResult{
				Content: []ContentBlock{
					{Type: "text", Text: "Echo: " + msg},
				},
			}, nil
		},
	}

	server.RegisterTool(customTool)

	// Initialize
	initReq := createJSONRPCRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	}, 1)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initReq))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	// Call custom tool
	callReq := createJSONRPCRequest("tools/call", map[string]interface{}{
		"name": "custom_tool",
		"arguments": map[string]interface{}{
			"message": "Hello Custom!",
		},
	}, 2)

	req = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(callReq))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)

	var callResult ToolResult

	err = json.Unmarshal(resultJSON, &callResult)
	require.NoError(t, err)
	assert.Contains(t, callResult.Content[0].Text, "Echo: Hello Custom!")
}

func TestServerMethodNotAllowed(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServerInvalidJSONRPCVersion(t *testing.T) {
	mockStore := NewMockObjectStore()
	server := NewServer(mockStore, createTestConfig())

	reqData, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "1.0",
		"method":  "initialize",
		"id":      1,
		"params":  map[string]interface{}{},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(reqData))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse

	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32600, resp.Error.Code)
}
