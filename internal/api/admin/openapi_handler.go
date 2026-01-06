package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/metrics"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

const (
	maxBodySize     = 1024 * 1024 // 1MB max body size
	maxHeaderCount  = 50          // Max number of headers
	maxHeaderKeyLen = 256         // Max header key length
	maxHeaderValLen = 8192        // Max header value length
	maxURLLength    = 2048        // Max URL length

	// Default cache durations (can be overridden via OpenAPIHandlerConfig)
	defaultCacheMaxAge       = 3600 // Default cache max age in seconds (1 hour)
	defaultConfigCacheMaxAge = 60   // Default config cache max age in seconds (1 minute)
)

// allowedMethods defines the HTTP methods that are valid for code snippet generation.
var allowedMethods = map[string]bool{
	"GET":     true,
	"POST":    true,
	"PUT":     true,
	"DELETE":  true,
	"PATCH":   true,
	"HEAD":    true,
	"OPTIONS": true,
}

// OpenAPIHandlerConfig holds configuration options for the OpenAPI handler.
type OpenAPIHandlerConfig struct {
	// CacheMaxAge is the max-age for OpenAPI spec responses in seconds (default: 3600)
	CacheMaxAge int
	// ConfigCacheMaxAge is the max-age for API Explorer config responses in seconds (default: 60)
	ConfigCacheMaxAge int
}

// DefaultOpenAPIHandlerConfig returns the default configuration.
func DefaultOpenAPIHandlerConfig() OpenAPIHandlerConfig {
	return OpenAPIHandlerConfig{
		CacheMaxAge:       defaultCacheMaxAge,
		ConfigCacheMaxAge: defaultConfigCacheMaxAge,
	}
}

// OpenAPIHandler serves the OpenAPI specification.
// TODO: Consider adding rate limiting for the code generation endpoint to prevent abuse.
type OpenAPIHandler struct {
	specJSON          []byte
	specYAML          []byte
	cacheMaxAge       int
	configCacheMaxAge int
	mu                sync.RWMutex
}

// NewOpenAPIHandler creates a new OpenAPI handler with the embedded specification.
func NewOpenAPIHandler(yamlSpec []byte) *OpenAPIHandler {
	return NewOpenAPIHandlerWithConfig(yamlSpec, DefaultOpenAPIHandlerConfig())
}

// NewOpenAPIHandlerWithConfig creates a new OpenAPI handler with custom configuration.
func NewOpenAPIHandlerWithConfig(yamlSpec []byte, config OpenAPIHandlerConfig) *OpenAPIHandler {
	// Apply defaults for zero values
	if config.CacheMaxAge <= 0 {
		config.CacheMaxAge = defaultCacheMaxAge
	}

	if config.ConfigCacheMaxAge <= 0 {
		config.ConfigCacheMaxAge = defaultConfigCacheMaxAge
	}

	handler := &OpenAPIHandler{
		specYAML:          yamlSpec,
		cacheMaxAge:       config.CacheMaxAge,
		configCacheMaxAge: config.ConfigCacheMaxAge,
	}

	// Convert YAML to JSON
	handler.convertToJSON()

	return handler
}

// convertToJSON converts the YAML spec to JSON format.
func (h *OpenAPIHandler) convertToJSON() {
	h.mu.Lock()
	defer h.mu.Unlock()

	var spec interface{}
	if err := yaml.Unmarshal(h.specYAML, &spec); err != nil {
		log.Error().
			Err(err).
			Int("yaml_size_bytes", len(h.specYAML)).
			Msg("Failed to parse OpenAPI YAML specification")
		h.specJSON = []byte("{}")

		return
	}

	// Convert map keys to strings (required for JSON)
	spec = convertMapKeys(spec)

	jsonBytes, err := json.Marshal(spec)
	if err != nil {
		log.Error().
			Err(err).
			Int("yaml_size_bytes", len(h.specYAML)).
			Msg("Failed to marshal OpenAPI specification to JSON")
		h.specJSON = []byte("{}")

		return
	}

	h.specJSON = jsonBytes
	log.Info().Int("size_bytes", len(jsonBytes)).Msg("OpenAPI specification converted to JSON")
}

// convertMapKeys recursively converts map[interface{}]interface{} to map[string]interface{}.
func convertMapKeys(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, value := range typed {
			result[key] = convertMapKeys(value)
		}

		return result
	case map[interface{}]interface{}:
		result := make(map[string]interface{})
		for key, value := range typed {
			strKey, ok := key.(string)
			if !ok {
				continue
			}

			result[strKey] = convertMapKeys(value)
		}

		return result
	case []interface{}:
		result := make([]interface{}, len(typed))
		for idx, value := range typed {
			result[idx] = convertMapKeys(value)
		}

		return result
	default:
		return v
	}
}

// ServeOpenAPIJSON serves the OpenAPI specification as JSON.
func (h *OpenAPIHandler) ServeOpenAPIJSON(w http.ResponseWriter, _ *http.Request) {
	metrics.RecordOpenAPISpecRequest("json")

	h.mu.RLock()
	specJSON := h.specJSON
	cacheAge := h.cacheMaxAge
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", cacheAge))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(specJSON)
}

// ServeOpenAPIYAML serves the OpenAPI specification as YAML.
func (h *OpenAPIHandler) ServeOpenAPIYAML(w http.ResponseWriter, _ *http.Request) {
	metrics.RecordOpenAPISpecRequest("yaml")

	h.mu.RLock()
	specYAML := h.specYAML
	cacheAge := h.cacheMaxAge
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", cacheAge))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(specYAML)
}

// ServeOpenAPI serves the OpenAPI spec in the requested format.
func (h *OpenAPIHandler) ServeOpenAPI(w http.ResponseWriter, r *http.Request) {
	// Check Accept header for format preference
	acceptHeader := r.Header.Get("Accept")

	// Check for format query parameter
	formatParam := r.URL.Query().Get("format")

	if formatParam == "yaml" || strings.Contains(acceptHeader, "application/x-yaml") {
		h.ServeOpenAPIYAML(w, r)

		return
	}

	// Default to JSON
	h.ServeOpenAPIJSON(w, r)
}

// APIExplorerConfig holds the configuration for the API Explorer.
type APIExplorerConfig struct {
	Enabled               bool     `json:"enabled"`
	Title                 string   `json:"title"`
	Description           string   `json:"description"`
	Version               string   `json:"version"`
	BaseURL               string   `json:"base_url"`
	SupportedLanguages    []string `json:"supported_languages"`
	MaxRequestHistorySize int      `json:"max_request_history_size"`
}

// DefaultAPIExplorerConfig returns the default configuration for the API Explorer.
func DefaultAPIExplorerConfig() *APIExplorerConfig {
	return &APIExplorerConfig{
		Enabled:               true,
		Title:                 "NebulaIO API Explorer",
		Description:           "Interactive API documentation for NebulaIO Admin and S3 APIs",
		Version:               "1.0.0",
		BaseURL:               "/api/v1",
		SupportedLanguages:    []string{"curl", "javascript", "python", "go"},
		MaxRequestHistorySize: 50,
	}
}

// ServeAPIExplorerConfig serves the API Explorer configuration.
func (h *OpenAPIHandler) ServeAPIExplorerConfig(w http.ResponseWriter, _ *http.Request) {
	metrics.RecordAPIExplorerRequest("config", true)
	config := DefaultAPIExplorerConfig()

	h.mu.RLock()
	cacheAge := h.configCacheMaxAge
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", cacheAge))
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(config)
}

// RequestHistoryEntry represents a saved API request.
type RequestHistoryEntry struct {
	Timestamp    time.Time              `json:"timestamp"`
	ID           string                 `json:"id"`
	Method       string                 `json:"method"`
	Path         string                 `json:"path"`
	Headers      map[string]string      `json:"headers"`
	Body         string                 `json:"body,omitempty"`
	StatusCode   int                    `json:"status_code"`
	ResponseTime int64                  `json:"response_time_ms"`
	Response     map[string]interface{} `json:"response,omitempty"`
}

// CodeSnippet represents a generated code snippet.
type CodeSnippet struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

// GenerateCodeSnippetsRequest is the request body for generating code snippets.
type GenerateCodeSnippetsRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body,omitempty"`
}

// validateCodeSnippetRequest validates the request for code snippet generation.
func validateCodeSnippetRequest(req *GenerateCodeSnippetsRequest) error {
	// Validate method
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if !allowedMethods[method] {
		return &ValidationError{Field: "method", Message: "invalid HTTP method"}
	}

	req.Method = method

	// Validate URL
	if len(req.URL) > maxURLLength {
		return &ValidationError{Field: "url", Message: "URL exceeds maximum length"}
	}

	parsedURL, err := url.Parse(req.URL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return &ValidationError{Field: "url", Message: "invalid URL format"}
	}

	// Only allow http and https schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return &ValidationError{Field: "url", Message: "only http and https URLs are allowed"}
	}

	// Validate headers
	if len(req.Headers) > maxHeaderCount {
		return &ValidationError{Field: "headers", Message: "too many headers"}
	}

	for key, value := range req.Headers {
		if len(key) > maxHeaderKeyLen {
			return &ValidationError{Field: "headers", Message: "header key too long"}
		}

		if len(value) > maxHeaderValLen {
			return &ValidationError{Field: "headers", Message: "header value too long"}
		}
	}

	// Validate body size
	if len(req.Body) > maxBodySize {
		return &ValidationError{Field: "body", Message: "request body exceeds maximum size"}
	}

	return nil
}

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// GenerateCodeSnippets generates code snippets for the given request.
func (h *OpenAPIHandler) GenerateCodeSnippets(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var req GenerateCodeSnippetsRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Debug().Err(err).Msg("Failed to decode code snippet request")
		metrics.RecordAPIExplorerRequest("code-snippets", false)
		writeError(w, "Invalid request body", http.StatusBadRequest)

		return
	}

	// Validate request
	if err := validateCodeSnippetRequest(&req); err != nil {
		log.Debug().Err(err).Msg("Code snippet request validation failed")
		metrics.RecordAPIExplorerRequest("code-snippets", false)
		writeError(w, err.Error(), http.StatusBadRequest)

		return
	}

	snippets := []CodeSnippet{
		{Language: "curl", Code: generateCurlSnippet(req)},
		{Language: "javascript", Code: generateJavaScriptSnippet(req)},
		{Language: "python", Code: generatePythonSnippet(req)},
		{Language: "go", Code: generateGoSnippet(req)},
	}

	// Record metrics for successful generation
	duration := time.Since(startTime)
	metrics.RecordAPIExplorerRequest("code-snippets", true)

	for _, snippet := range snippets {
		metrics.RecordCodeSnippetGeneration(snippet.Language, true, duration)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"snippets": snippets,
	})
}

// escapeShellSingleQuote escapes single quotes for shell strings.
func escapeShellSingleQuote(s string) string {
	// Replace ' with '\'' (end quote, escaped quote, start quote)
	return strings.ReplaceAll(s, "'", "'\\''")
}

// escapeJavaScriptString escapes a string for JavaScript single-quoted strings.
func escapeJavaScriptString(s string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	)

	return replacer.Replace(s)
}

// escapePythonString escapes a string for Python single-quoted strings.
func escapePythonString(s string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	)

	return replacer.Replace(s)
}

// escapeGoString escapes a string for Go double-quoted strings.
func escapeGoString(s string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	)

	return replacer.Replace(s)
}

// escapeGoRawString escapes a string for Go raw strings (backticks).
// Returns true if the string can be used as a raw string, false if it contains backticks.
func escapeGoRawString(s string) (string, bool) {
	if strings.Contains(s, "`") {
		return "", false
	}

	return s, true
}

// sanitizeHeaderKey removes any characters that could be dangerous in header keys
// and returns the canonical HTTP header format (Title-Case).
var headerKeyRegex = regexp.MustCompile(`[^a-zA-Z0-9\-_]`)

func sanitizeHeaderKey(key string) string {
	sanitized := headerKeyRegex.ReplaceAllString(key, "")
	if sanitized == "" {
		return ""
	}
	// Apply canonical HTTP header formatting (Title-Case)
	return http.CanonicalHeaderKey(sanitized)
}

func generateCurlSnippet(req GenerateCodeSnippetsRequest) string {
	var builder strings.Builder

	builder.WriteString("curl")

	if req.Method != http.MethodGet {
		builder.WriteString(" -X ")
		builder.WriteString(req.Method)
	}

	for key, value := range req.Headers {
		safeKey := sanitizeHeaderKey(key)
		if safeKey == "" {
			continue
		}

		safeValue := escapeShellSingleQuote(value)
		builder.WriteString(" \\\n  -H '")
		builder.WriteString(safeKey)
		builder.WriteString(": ")
		builder.WriteString(safeValue)
		builder.WriteString("'")
	}

	if req.Body != "" {
		safeBody := escapeShellSingleQuote(req.Body)
		builder.WriteString(" \\\n  -d '")
		builder.WriteString(safeBody)
		builder.WriteString("'")
	}

	// URL is already validated, but we still escape for safety
	safeURL := escapeShellSingleQuote(req.URL)
	builder.WriteString(" \\\n  '")
	builder.WriteString(safeURL)
	builder.WriteString("'")

	return builder.String()
}

func generateJavaScriptSnippet(req GenerateCodeSnippetsRequest) string {
	var builder strings.Builder

	safeURL := escapeJavaScriptString(req.URL)
	builder.WriteString("const response = await fetch('")
	builder.WriteString(safeURL)
	builder.WriteString("', {\n")
	builder.WriteString("  method: '")
	builder.WriteString(req.Method)
	builder.WriteString("',\n")
	builder.WriteString("  headers: {\n")

	headerCount := 0
	for key, value := range req.Headers {
		safeKey := sanitizeHeaderKey(key)
		if safeKey == "" {
			continue
		}

		if headerCount > 0 {
			builder.WriteString(",\n")
		}

		safeValue := escapeJavaScriptString(value)
		builder.WriteString("    '")
		builder.WriteString(safeKey)
		builder.WriteString("': '")
		builder.WriteString(safeValue)
		builder.WriteString("'")
		headerCount++
	}

	builder.WriteString("\n  }")

	if req.Body != "" {
		// Add request body (escaped for JavaScript string literal)
		safeBody := escapeJavaScriptString(req.Body)
		builder.WriteString(",\n  body: '")
		builder.WriteString(safeBody)
		builder.WriteString("'")
	}

	builder.WriteString("\n});\n\nconst data = await response.json();\nconsole.log(data);")

	return builder.String()
}

func generatePythonSnippet(req GenerateCodeSnippetsRequest) string {
	var builder strings.Builder

	safeURL := escapePythonString(req.URL)
	builder.WriteString("import requests\n\n")
	builder.WriteString("response = requests.")
	builder.WriteString(strings.ToLower(req.Method))
	builder.WriteString("(\n    '")
	builder.WriteString(safeURL)
	builder.WriteString("',\n    headers={\n")

	headerCount := 0
	for key, value := range req.Headers {
		safeKey := sanitizeHeaderKey(key)
		if safeKey == "" {
			continue
		}

		if headerCount > 0 {
			builder.WriteString(",\n")
		}

		safeValue := escapePythonString(value)
		builder.WriteString("        '")
		builder.WriteString(safeKey)
		builder.WriteString("': '")
		builder.WriteString(safeValue)
		builder.WriteString("'")
		headerCount++
	}

	builder.WriteString("\n    }")

	if req.Body != "" {
		// Add request body (escaped for Python string literal)
		safeBody := escapePythonString(req.Body)
		builder.WriteString(",\n    data='")
		builder.WriteString(safeBody)
		builder.WriteString("'")
	}

	builder.WriteString("\n)\n\nprint(response.json())")

	return builder.String()
}

func generateGoSnippet(req GenerateCodeSnippetsRequest) string {
	var builder strings.Builder

	safeURL := escapeGoString(req.URL)

	builder.WriteString("package main\n\n")
	builder.WriteString("import (\n")
	builder.WriteString("\t\"fmt\"\n")
	builder.WriteString("\t\"io\"\n")
	builder.WriteString("\t\"net/http\"\n")

	if req.Body != "" {
		builder.WriteString("\t\"strings\"\n")
	}

	builder.WriteString(")\n\n")
	builder.WriteString("func main() {\n")

	if req.Body != "" {
		// Try raw string first, fall back to escaped string
		if rawBody, ok := escapeGoRawString(req.Body); ok {
			builder.WriteString("\tbody := strings.NewReader(`")
			builder.WriteString(rawBody)
			builder.WriteString("`)\n")
		} else {
			safeBody := escapeGoString(req.Body)
			builder.WriteString("\tbody := strings.NewReader(\"")
			builder.WriteString(safeBody)
			builder.WriteString("\")\n")
		}

		builder.WriteString("\treq, err := http.NewRequest(\"")
		builder.WriteString(req.Method)
		builder.WriteString("\", \"")
		builder.WriteString(safeURL)
		builder.WriteString("\", body)\n")
	} else {
		builder.WriteString("\treq, err := http.NewRequest(\"")
		builder.WriteString(req.Method)
		builder.WriteString("\", \"")
		builder.WriteString(safeURL)
		builder.WriteString("\", nil)\n")
	}

	builder.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n\n")

	for key, value := range req.Headers {
		safeKey := sanitizeHeaderKey(key)
		if safeKey == "" {
			continue
		}

		safeValue := escapeGoString(value)
		builder.WriteString("\treq.Header.Set(\"")
		builder.WriteString(safeKey)
		builder.WriteString("\", \"")
		builder.WriteString(safeValue)
		builder.WriteString("\")\n")
	}

	builder.WriteString("\n\tresp, err := http.DefaultClient.Do(req)\n")
	builder.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	builder.WriteString("\tdefer resp.Body.Close()\n\n")
	builder.WriteString("\tdata, _ := io.ReadAll(resp.Body)\n")
	builder.WriteString("\tfmt.Println(string(data))\n")
	builder.WriteString("}\n")

	return builder.String()
}
