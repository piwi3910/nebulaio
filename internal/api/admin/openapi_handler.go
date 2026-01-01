package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// OpenAPIHandler serves the OpenAPI specification.
type OpenAPIHandler struct {
	specJSON []byte
	specYAML []byte
	mu       sync.RWMutex
}

// NewOpenAPIHandler creates a new OpenAPI handler with the embedded specification.
func NewOpenAPIHandler(yamlSpec []byte) *OpenAPIHandler {
	handler := &OpenAPIHandler{
		specYAML: yamlSpec,
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
		// If conversion fails, use empty spec
		h.specJSON = []byte("{}")

		return
	}

	// Convert map keys to strings (required for JSON)
	spec = convertMapKeys(spec)

	jsonBytes, err := json.Marshal(spec)
	if err != nil {
		h.specJSON = []byte("{}")

		return
	}

	h.specJSON = jsonBytes
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
func (h *OpenAPIHandler) ServeOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	specJSON := h.specJSON
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(specJSON)
}

// ServeOpenAPIYAML serves the OpenAPI specification as YAML.
func (h *OpenAPIHandler) ServeOpenAPIYAML(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	specYAML := h.specYAML
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
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
func (h *OpenAPIHandler) ServeAPIExplorerConfig(w http.ResponseWriter, r *http.Request) {
	config := DefaultAPIExplorerConfig()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(config)
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

// GenerateCodeSnippets generates code snippets for the given request.
func (h *OpenAPIHandler) GenerateCodeSnippets(w http.ResponseWriter, r *http.Request) {
	var req GenerateCodeSnippetsRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)

		return
	}

	snippets := []CodeSnippet{
		{Language: "curl", Code: generateCurlSnippet(req)},
		{Language: "javascript", Code: generateJavaScriptSnippet(req)},
		{Language: "python", Code: generatePythonSnippet(req)},
		{Language: "go", Code: generateGoSnippet(req)},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"snippets": snippets,
	})
}

func generateCurlSnippet(req GenerateCodeSnippetsRequest) string {
	var builder strings.Builder

	builder.WriteString("curl")

	if req.Method != "GET" {
		builder.WriteString(" -X ")
		builder.WriteString(req.Method)
	}

	for key, value := range req.Headers {
		builder.WriteString(" \\\n  -H '")
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(value)
		builder.WriteString("'")
	}

	if req.Body != "" {
		builder.WriteString(" \\\n  -d '")
		builder.WriteString(req.Body)
		builder.WriteString("'")
	}

	builder.WriteString(" \\\n  '")
	builder.WriteString(req.URL)
	builder.WriteString("'")

	return builder.String()
}

func generateJavaScriptSnippet(req GenerateCodeSnippetsRequest) string {
	var builder strings.Builder

	builder.WriteString("const response = await fetch('")
	builder.WriteString(req.URL)
	builder.WriteString("', {\n")
	builder.WriteString("  method: '")
	builder.WriteString(req.Method)
	builder.WriteString("',\n")
	builder.WriteString("  headers: {\n")

	headerCount := 0
	for key, value := range req.Headers {
		if headerCount > 0 {
			builder.WriteString(",\n")
		}

		builder.WriteString("    '")
		builder.WriteString(key)
		builder.WriteString("': '")
		builder.WriteString(value)
		builder.WriteString("'")
		headerCount++
	}

	builder.WriteString("\n  }")

	if req.Body != "" {
		builder.WriteString(",\n  body: JSON.stringify(")
		builder.WriteString(req.Body)
		builder.WriteString(")")
	}

	builder.WriteString("\n});\n\nconst data = await response.json();\nconsole.log(data);")

	return builder.String()
}

func generatePythonSnippet(req GenerateCodeSnippetsRequest) string {
	var builder strings.Builder

	builder.WriteString("import requests\n\n")
	builder.WriteString("response = requests.")
	builder.WriteString(strings.ToLower(req.Method))
	builder.WriteString("(\n    '")
	builder.WriteString(req.URL)
	builder.WriteString("',\n    headers={\n")

	headerCount := 0
	for key, value := range req.Headers {
		if headerCount > 0 {
			builder.WriteString(",\n")
		}

		builder.WriteString("        '")
		builder.WriteString(key)
		builder.WriteString("': '")
		builder.WriteString(value)
		builder.WriteString("'")
		headerCount++
	}

	builder.WriteString("\n    }")

	if req.Body != "" {
		builder.WriteString(",\n    json=")
		builder.WriteString(req.Body)
	}

	builder.WriteString("\n)\n\nprint(response.json())")

	return builder.String()
}

func generateGoSnippet(req GenerateCodeSnippetsRequest) string {
	var builder strings.Builder

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
		builder.WriteString("\tbody := strings.NewReader(`")
		builder.WriteString(req.Body)
		builder.WriteString("`)\n")
		builder.WriteString("\treq, err := http.NewRequest(\"")
		builder.WriteString(req.Method)
		builder.WriteString("\", \"")
		builder.WriteString(req.URL)
		builder.WriteString("\", body)\n")
	} else {
		builder.WriteString("\treq, err := http.NewRequest(\"")
		builder.WriteString(req.Method)
		builder.WriteString("\", \"")
		builder.WriteString(req.URL)
		builder.WriteString("\", nil)\n")
	}

	builder.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n\n")

	for key, value := range req.Headers {
		builder.WriteString("\treq.Header.Set(\"")
		builder.WriteString(key)
		builder.WriteString("\", \"")
		builder.WriteString(value)
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
