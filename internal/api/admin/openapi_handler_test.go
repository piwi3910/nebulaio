package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAPIHandler(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths: {}`)

	handler := NewOpenAPIHandler(yamlSpec)

	require.NotNil(t, handler)
	assert.NotEmpty(t, handler.specJSON)
	assert.NotEmpty(t, handler.specYAML)
}

func TestNewOpenAPIHandler_InvalidYAML(t *testing.T) {
	invalidYAML := []byte(`{invalid yaml::`)

	handler := NewOpenAPIHandler(invalidYAML)

	require.NotNil(t, handler)
	// Should fall back to empty JSON
	assert.Equal(t, []byte("{}"), handler.specJSON)
}

func TestDefaultOpenAPIHandlerConfig(t *testing.T) {
	config := DefaultOpenAPIHandlerConfig()

	assert.Equal(t, 3600, config.CacheMaxAge)
	assert.Equal(t, 60, config.ConfigCacheMaxAge)
}

func TestNewOpenAPIHandlerWithConfig(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths: {}`)

	config := OpenAPIHandlerConfig{
		CacheMaxAge:       7200,
		ConfigCacheMaxAge: 120,
	}

	handler := NewOpenAPIHandlerWithConfig(yamlSpec, config)

	require.NotNil(t, handler)
	assert.Equal(t, 7200, handler.cacheMaxAge)
	assert.Equal(t, 120, handler.configCacheMaxAge)
}

func TestNewOpenAPIHandlerWithConfig_DefaultsForZeroValues(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths: {}`)

	// Zero values should use defaults
	config := OpenAPIHandlerConfig{
		CacheMaxAge:       0,
		ConfigCacheMaxAge: 0,
	}

	handler := NewOpenAPIHandlerWithConfig(yamlSpec, config)

	require.NotNil(t, handler)
	assert.Equal(t, 3600, handler.cacheMaxAge)
	assert.Equal(t, 60, handler.configCacheMaxAge)
}

func TestServeOpenAPIJSON(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"`)

	handler := NewOpenAPIHandler(yamlSpec)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()

	handler.ServeOpenAPIJSON(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Cache-Control"), "max-age=3600")

	var result map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "3.0.0", result["openapi"])
}

func TestServeOpenAPIYAML(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"`)

	handler := NewOpenAPIHandler(yamlSpec)

	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rec := httptest.NewRecorder()

	handler.ServeOpenAPIYAML(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-yaml", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "openapi:")
}

func TestServeOpenAPI_FormatParam(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"`)

	handler := NewOpenAPIHandler(yamlSpec)

	tests := []struct {
		name        string
		format      string
		accept      string
		contentType string
	}{
		{"default to JSON", "", "", "application/json"},
		{"explicit JSON", "json", "", "application/json"},
		{"explicit YAML", "yaml", "", "application/x-yaml"},
		{"accept header YAML", "", "application/x-yaml", "application/x-yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/openapi"
			if tt.format != "" {
				url += "?format=" + tt.format
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			rec := httptest.NewRecorder()

			handler.ServeOpenAPI(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, tt.contentType, rec.Header().Get("Content-Type"))
		})
	}
}

func TestServeAPIExplorerConfig(t *testing.T) {
	handler := NewOpenAPIHandler([]byte("openapi: '3.0.0'"))

	req := httptest.NewRequest(http.MethodGet, "/api-explorer/config", nil)
	rec := httptest.NewRecorder()

	handler.ServeAPIExplorerConfig(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var config APIExplorerConfig
	err := json.Unmarshal(rec.Body.Bytes(), &config)
	require.NoError(t, err)
	assert.True(t, config.Enabled)
	assert.Equal(t, "NebulaIO API Explorer", config.Title)
	assert.Contains(t, config.SupportedLanguages, "curl")
	assert.Contains(t, config.SupportedLanguages, "javascript")
	assert.Contains(t, config.SupportedLanguages, "python")
	assert.Contains(t, config.SupportedLanguages, "go")
}

func TestGenerateCodeSnippets_ValidRequest(t *testing.T) {
	handler := NewOpenAPIHandler([]byte("openapi: '3.0.0'"))

	reqBody := GenerateCodeSnippetsRequest{
		Method: "GET",
		URL:    "https://api.example.com/users",
		Headers: map[string]string{
			"Authorization": "Bearer token123",
			"Content-Type":  "application/json",
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api-explorer/code-snippets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.GenerateCodeSnippets(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)

	snippets, ok := result["snippets"].([]interface{})
	require.True(t, ok)
	assert.Len(t, snippets, 4) // curl, javascript, python, go
}

func TestGenerateCodeSnippets_InvalidMethod(t *testing.T) {
	handler := NewOpenAPIHandler([]byte("openapi: '3.0.0'"))

	reqBody := GenerateCodeSnippetsRequest{
		Method: "INVALID",
		URL:    "https://api.example.com/users",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api-explorer/code-snippets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.GenerateCodeSnippets(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid HTTP method")
}

func TestGenerateCodeSnippets_InvalidURL(t *testing.T) {
	handler := NewOpenAPIHandler([]byte("openapi: '3.0.0'"))

	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{"empty URL", "", "invalid URL format"},
		{"no scheme", "api.example.com/users", "invalid URL format"},
		{"invalid scheme", "ftp://api.example.com/users", "only http and https URLs are allowed"},
		{"too long", "https://example.com/" + strings.Repeat("a", 2100), "URL exceeds maximum length"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := GenerateCodeSnippetsRequest{
				Method: "GET",
				URL:    tt.url,
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest(http.MethodPost, "/api-explorer/code-snippets", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.GenerateCodeSnippets(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantErr)
		})
	}
}

func TestGenerateCodeSnippets_InvalidBody(t *testing.T) {
	handler := NewOpenAPIHandler([]byte("openapi: '3.0.0'"))

	req := httptest.NewRequest(http.MethodPost, "/api-explorer/code-snippets", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.GenerateCodeSnippets(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestEscapeShellSingleQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello'world", "hello'\\''world"},
		{"it's a test", "it'\\''s a test"},
		{"'quoted'", "'\\''quoted'\\''"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeShellSingleQuote(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEscapeJavaScriptString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello'world", "hello\\'world"},
		{"line1\nline2", "line1\\nline2"},
		{"tab\there", "tab\\there"},
		{"back\\slash", "back\\\\slash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeJavaScriptString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEscapePythonString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello'world", "hello\\'world"},
		{"line1\nline2", "line1\\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapePythonString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEscapeGoString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{`hello"world`, `hello\"world`},
		{"line1\nline2", "line1\\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeGoString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEscapeGoRawString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"hello", "hello", true},
		{"hello`world", "", false},
		{"multi\nline", "multi\nline", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := escapeGoRawString(tt.input)
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestSanitizeHeaderKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Canonical header keys are Title-Case
		{"Content-Type", "Content-Type"},
		{"X-Custom-Header", "X-Custom-Header"},
		{"Authorization", "Authorization"},
		// Invalid characters are removed, then canonicalized
		{"invalid<key>", "Invalidkey"},
		{"header:with:colons", "Headerwithcolons"},
		// Underscores are preserved, then canonicalized
		{"X_Custom_Header", "X_custom_header"},
		// Lowercase inputs get canonicalized to Title-Case
		{"content-type", "Content-Type"},
		{"x-api-key", "X-Api-Key"},
		{"authorization", "Authorization"},
		// Empty input after sanitization
		{"<>:", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeHeaderKey(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCanonicalHeaderFormatInSnippets(t *testing.T) {
	// Test that generated code snippets use canonical header format
	req := GenerateCodeSnippetsRequest{
		Method: "GET",
		URL:    "https://api.example.com/users",
		Headers: map[string]string{
			"content-type":    "application/json",
			"x-api-key":       "test-key",
			"authorization":   "Bearer token",
			"x-custom-header": "value",
		},
	}

	// Test curl snippet
	curlResult := generateCurlSnippet(req)
	assert.Contains(t, curlResult, "Content-Type:")
	assert.Contains(t, curlResult, "X-Api-Key:")
	assert.Contains(t, curlResult, "Authorization:")
	assert.Contains(t, curlResult, "X-Custom-Header:")
	// Should not contain lowercase versions
	assert.NotContains(t, curlResult, "content-type:")
	assert.NotContains(t, curlResult, "x-api-key:")

	// Test JavaScript snippet
	jsResult := generateJavaScriptSnippet(req)
	assert.Contains(t, jsResult, "'Content-Type':")
	assert.Contains(t, jsResult, "'X-Api-Key':")
	assert.Contains(t, jsResult, "'Authorization':")

	// Test Python snippet
	pyResult := generatePythonSnippet(req)
	assert.Contains(t, pyResult, "'Content-Type':")
	assert.Contains(t, pyResult, "'X-Api-Key':")
	assert.Contains(t, pyResult, "'Authorization':")

	// Test Go snippet
	goResult := generateGoSnippet(req)
	assert.Contains(t, goResult, `"Content-Type"`)
	assert.Contains(t, goResult, `"X-Api-Key"`)
	assert.Contains(t, goResult, `"Authorization"`)
}

func TestGenerateCurlSnippet(t *testing.T) {
	req := GenerateCodeSnippetsRequest{
		Method: "POST",
		URL:    "https://api.example.com/users",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: `{"name": "test"}`,
	}

	result := generateCurlSnippet(req)

	assert.Contains(t, result, "curl")
	assert.Contains(t, result, "-X POST")
	assert.Contains(t, result, "Content-Type: application/json")
	assert.Contains(t, result, `{"name": "test"}`)
	assert.Contains(t, result, "https://api.example.com/users")
}

func TestGenerateCurlSnippet_EscapesInjection(t *testing.T) {
	req := GenerateCodeSnippetsRequest{
		Method: "GET",
		URL:    "https://api.example.com/users",
		Headers: map[string]string{
			"X-Malicious": "'; rm -rf /; echo '",
		},
	}

	result := generateCurlSnippet(req)

	// Should escape single quotes properly, preventing shell injection
	// The escaping turns ' into '\'' which safely quotes the dangerous content
	assert.Contains(t, result, "'\\''")
	// Verify the header is properly formatted with escaping
	assert.Contains(t, result, "X-Malicious:")
}

func TestGenerateJavaScriptSnippet(t *testing.T) {
	req := GenerateCodeSnippetsRequest{
		Method: "GET",
		URL:    "https://api.example.com/users",
		Headers: map[string]string{
			"Authorization": "Bearer token",
		},
	}

	result := generateJavaScriptSnippet(req)

	assert.Contains(t, result, "await fetch")
	assert.Contains(t, result, "method: 'GET'")
	assert.Contains(t, result, "Authorization")
	assert.Contains(t, result, "Bearer token")
}

func TestGeneratePythonSnippet(t *testing.T) {
	req := GenerateCodeSnippetsRequest{
		Method: "POST",
		URL:    "https://api.example.com/users",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: `{"name": "test"}`,
	}

	result := generatePythonSnippet(req)

	assert.Contains(t, result, "import requests")
	assert.Contains(t, result, "requests.post")
	assert.Contains(t, result, "Content-Type")
	assert.Contains(t, result, "response.json()")
}

func TestGenerateGoSnippet(t *testing.T) {
	req := GenerateCodeSnippetsRequest{
		Method: "DELETE",
		URL:    "https://api.example.com/users/123",
		Headers: map[string]string{
			"Authorization": "Bearer token",
		},
	}

	result := generateGoSnippet(req)

	assert.Contains(t, result, "package main")
	assert.Contains(t, result, "http.NewRequest")
	assert.Contains(t, result, `"DELETE"`)
	assert.Contains(t, result, "Authorization")
}

func TestGenerateGoSnippet_WithBody(t *testing.T) {
	req := GenerateCodeSnippetsRequest{
		Method: "POST",
		URL:    "https://api.example.com/users",
		Body:   `{"name": "test"}`,
	}

	result := generateGoSnippet(req)

	assert.Contains(t, result, "strings.NewReader")
	assert.Contains(t, result, `{"name": "test"}`)
}

func TestGenerateGoSnippet_BodyWithBackticks(t *testing.T) {
	req := GenerateCodeSnippetsRequest{
		Method: "POST",
		URL:    "https://api.example.com/users",
		Body:   "body with `backticks`",
	}

	result := generateGoSnippet(req)

	// Should use escaped string instead of raw string
	assert.Contains(t, result, `strings.NewReader("`)
	assert.NotContains(t, result, "strings.NewReader(`")
}

func TestValidateCodeSnippetRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     GenerateCodeSnippetsRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid request",
			req: GenerateCodeSnippetsRequest{
				Method: "GET",
				URL:    "https://api.example.com/users",
			},
			wantErr: false,
		},
		{
			name: "lowercase method normalized",
			req: GenerateCodeSnippetsRequest{
				Method: "get",
				URL:    "https://api.example.com/users",
			},
			wantErr: false,
		},
		{
			name: "invalid method",
			req: GenerateCodeSnippetsRequest{
				Method: "INVALID",
				URL:    "https://api.example.com/users",
			},
			wantErr: true,
			errMsg:  "invalid HTTP method",
		},
		{
			name: "too many headers",
			req: GenerateCodeSnippetsRequest{
				Method:  "GET",
				URL:     "https://api.example.com/users",
				Headers: makeHeaders(51),
			},
			wantErr: true,
			errMsg:  "too many headers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCodeSnippetRequest(&tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConvertMapKeys(t *testing.T) {
	// Test with map[interface{}]interface{} (common from YAML)
	input := map[interface{}]interface{}{
		"key1": "value1",
		"key2": map[interface{}]interface{}{
			"nested": "value",
		},
		"key3": []interface{}{
			map[interface{}]interface{}{
				"array_item": "value",
			},
		},
	}

	result := convertMapKeys(input)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value1", resultMap["key1"])

	nested, ok := resultMap["key2"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value", nested["nested"])
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{
		Field:   "url",
		Message: "invalid format",
	}

	assert.Equal(t, "url: invalid format", err.Error())
}

// Helper function to create a map with n headers.
func makeHeaders(n int) map[string]string {
	headers := make(map[string]string)
	for i := range n {
		headers["X-Header-"+string(rune('A'+i%26))+string(rune('0'+i/26))] = "value"
	}

	return headers
}
