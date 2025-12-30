// Package lambda implements S3 Object Lambda functionality for transforming
// objects on GET requests without creating derivative copies.
package lambda

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"github.com/rs/zerolog/log"
)

// TransformationType defines the type of transformation.
type TransformationType string

const (
	TransformWebhook    TransformationType = "webhook"
	TransformBuiltIn    TransformationType = "builtin"
	TransformWASM       TransformationType = "wasm"
	TransformJavaScript TransformationType = "javascript"
)

// BuiltInTransform defines built-in transformation functions.
type BuiltInTransform string

const (
	BuiltInResize       BuiltInTransform = "resize"        // Resize images
	BuiltInThumbnail    BuiltInTransform = "thumbnail"     // Generate thumbnails
	BuiltInWatermark    BuiltInTransform = "watermark"     // Add watermark
	BuiltInRedact       BuiltInTransform = "redact"        // Redact sensitive data
	BuiltInCompress     BuiltInTransform = "compress"      // Compress content
	BuiltInDecompress   BuiltInTransform = "decompress"    // Decompress content
	BuiltInConvertJSON  BuiltInTransform = "convert_json"  // Convert to JSON
	BuiltInConvertCSV   BuiltInTransform = "convert_csv"   // Convert to CSV
	BuiltInFilterFields BuiltInTransform = "filter_fields" // Filter JSON fields
	BuiltInMaskPII      BuiltInTransform = "mask_pii"      // Mask PII data
	BuiltInEncrypt      BuiltInTransform = "encrypt"       // Client-side encryption
	BuiltInDecrypt      BuiltInTransform = "decrypt"       // Client-side decryption
)

// AccessPointConfig defines an Object Lambda access point configuration.
type AccessPointConfig struct {
	// Name is the access point name
	Name string `json:"name"`

	// Arn is the access point ARN
	Arn string `json:"arn"`

	// SupportingAccessPoint is the S3 access point that provides data
	SupportingAccessPoint string `json:"supporting_access_point"`

	// TransformationConfigurations defines the transformations
	TransformationConfigurations []TransformationConfig `json:"transformation_configurations"`

	// AllowedFeatures specifies allowed S3 features
	AllowedFeatures []string `json:"allowed_features"`

	// CreatedAt is when this config was created
	CreatedAt time.Time `json:"created_at"`
}

// TransformationConfig defines a single transformation.
type TransformationConfig struct {
	// Actions specifies which S3 actions trigger this transformation
	// Currently only GetObject is supported
	Actions []string `json:"actions"`

	// ContentTransformation defines the transformation to apply
	ContentTransformation ContentTransformation `json:"content_transformation"`
}

// ContentTransformation specifies how to transform content.
type ContentTransformation struct {
	// Type is the transformation type
	Type TransformationType `json:"type"`

	// WebhookConfig for webhook-based transformations
	WebhookConfig *WebhookConfig `json:"webhook_config,omitempty"`

	// BuiltInConfig for built-in transformations
	BuiltInConfig *BuiltInConfig `json:"builtin_config,omitempty"`

	// WASMConfig for WASM-based transformations
	WASMConfig *WASMConfig `json:"wasm_config,omitempty"`
}

// WebhookConfig defines webhook-based transformation settings.
type WebhookConfig struct {
	// URL is the webhook endpoint
	URL string `json:"url"`

	// Headers are additional headers to send
	Headers map[string]string `json:"headers,omitempty"`

	// Timeout is the request timeout
	Timeout time.Duration `json:"timeout"`

	// RetryCount is the number of retries on failure
	RetryCount int `json:"retry_count"`

	// PassThroughBody if true, sends original body to webhook
	PassThroughBody bool `json:"pass_through_body"`
}

// BuiltInConfig defines built-in transformation settings
type BuiltInConfig struct {
	// Function is the built-in function name
	Function BuiltInTransform `json:"function"`

	// Parameters are function-specific parameters
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// WASMConfig defines WASM-based transformation settings
type WASMConfig struct {
	// ModuleBucket is the bucket containing the WASM module
	ModuleBucket string `json:"module_bucket"`

	// ModuleKey is the key of the WASM module
	ModuleKey string `json:"module_key"`

	// FunctionName is the exported function to call
	FunctionName string `json:"function_name"`

	// MemoryLimit is the memory limit in MB
	MemoryLimit int `json:"memory_limit"`
}

// ObjectLambdaEvent is sent to webhook transformations
type ObjectLambdaEvent struct {
	// RequestID is the unique request identifier
	RequestID string `json:"requestId"`

	// GetObjectContext contains information about the GetObject request
	GetObjectContext GetObjectContext `json:"getObjectContext"`

	// Configuration contains the access point configuration
	Configuration AccessPointConfiguration `json:"configuration"`

	// UserRequest contains the original user request
	UserRequest UserRequest `json:"userRequest"`

	// UserIdentity contains information about the requester
	UserIdentity UserIdentity `json:"userIdentity"`

	// ProtocolVersion is the event version
	ProtocolVersion string `json:"protocolVersion"`
}

// GetObjectContext contains GetObject-specific context
type GetObjectContext struct {
	// InputS3URL is the presigned URL to fetch the original object
	InputS3URL string `json:"inputS3Url"`

	// OutputRoute is the route token for WriteGetObjectResponse
	OutputRoute string `json:"outputRoute"`

	// OutputToken is the token for WriteGetObjectResponse
	OutputToken string `json:"outputToken"`
}

// AccessPointConfiguration contains access point info
type AccessPointConfiguration struct {
	// AccessPointArn is the Object Lambda access point ARN
	AccessPointArn string `json:"accessPointArn"`

	// SupportingAccessPointArn is the supporting access point ARN
	SupportingAccessPointArn string `json:"supportingAccessPointArn"`

	// Payload is optional custom payload
	Payload string `json:"payload,omitempty"`
}

// UserRequest contains the original request details
type UserRequest struct {
	// URL is the original request URL
	URL string `json:"url"`

	// Headers are the original request headers
	Headers map[string]string `json:"headers"`
}

// UserIdentity contains requester identity
type UserIdentity struct {
	// Type is the identity type
	Type string `json:"type"`

	// PrincipalID is the principal identifier
	PrincipalID string `json:"principalId"`

	// Arn is the identity ARN
	Arn string `json:"arn"`

	// AccountID is the AWS account ID
	AccountID string `json:"accountId"`

	// AccessKeyID is the access key used
	AccessKeyID string `json:"accessKeyId"`
}

// WriteGetObjectResponseInput is used by Lambda to write transformed response
type WriteGetObjectResponseInput struct {
	// RequestRoute is the route token
	RequestRoute string `json:"RequestRoute"`

	// RequestToken is the output token
	RequestToken string `json:"RequestToken"`

	// StatusCode is the HTTP status code
	StatusCode int `json:"StatusCode"`

	// ErrorCode is an optional error code
	ErrorCode string `json:"ErrorCode,omitempty"`

	// ErrorMessage is an optional error message
	ErrorMessage string `json:"ErrorMessage,omitempty"`

	// Body is the transformed content
	Body io.Reader `json:"-"`

	// ContentLength is the body length
	ContentLength int64 `json:"ContentLength"`

	// ContentType is the content MIME type
	ContentType string `json:"ContentType,omitempty"`

	// CacheControl is the cache-control header
	CacheControl string `json:"CacheControl,omitempty"`

	// ContentDisposition is the content-disposition header
	ContentDisposition string `json:"ContentDisposition,omitempty"`

	// ContentEncoding is the content-encoding header
	ContentEncoding string `json:"ContentEncoding,omitempty"`

	// ContentLanguage is the content-language header
	ContentLanguage string `json:"ContentLanguage,omitempty"`

	// ETag is the object ETag
	ETag string `json:"ETag,omitempty"`

	// Expires is the expiration time
	Expires *time.Time `json:"Expires,omitempty"`

	// LastModified is the last modified time
	LastModified *time.Time `json:"LastModified,omitempty"`

	// Metadata is custom metadata
	Metadata map[string]string `json:"Metadata,omitempty"`
}

// ObjectLambdaService manages Object Lambda access points
type ObjectLambdaService struct {
	accessPoints map[string]*AccessPointConfig
	mu           sync.RWMutex
	httpClient   *http.Client

	// Built-in transformers
	transformers map[BuiltInTransform]Transformer

	// Pending responses from webhook transformations
	pendingResponses map[string]chan *WriteGetObjectResponseInput
	pendingMu        sync.RWMutex
}

// Transformer interface for built-in transformations
type Transformer interface {
	Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error)
}

// NewObjectLambdaService creates a new Object Lambda service
func NewObjectLambdaService() *ObjectLambdaService {
	svc := &ObjectLambdaService{
		accessPoints:     make(map[string]*AccessPointConfig),
		pendingResponses: make(map[string]chan *WriteGetObjectResponseInput),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		transformers: make(map[BuiltInTransform]Transformer),
	}

	// Register built-in transformers
	svc.registerBuiltInTransformers()

	return svc
}

// registerBuiltInTransformers registers all built-in transformation functions
func (s *ObjectLambdaService) registerBuiltInTransformers() {
	s.transformers[BuiltInRedact] = &RedactTransformer{}
	s.transformers[BuiltInMaskPII] = &PIIMaskTransformer{}
	s.transformers[BuiltInFilterFields] = &FilterFieldsTransformer{}
	s.transformers[BuiltInConvertJSON] = &ConvertJSONTransformer{}
	s.transformers[BuiltInCompress] = &CompressTransformer{}
	s.transformers[BuiltInDecompress] = &DecompressTransformer{}
}

// CreateAccessPoint creates an Object Lambda access point
func (s *ObjectLambdaService) CreateAccessPoint(cfg *AccessPointConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("access point name is required")
	}
	if cfg.SupportingAccessPoint == "" {
		return fmt.Errorf("supporting access point is required")
	}
	if len(cfg.TransformationConfigurations) == 0 {
		return fmt.Errorf("at least one transformation configuration is required")
	}

	cfg.CreatedAt = time.Now()
	if cfg.Arn == "" {
		cfg.Arn = fmt.Sprintf("arn:aws:s3-object-lambda:region:account:accesspoint/%s", cfg.Name)
	}

	s.mu.Lock()
	s.accessPoints[cfg.Name] = cfg
	s.mu.Unlock()

	return nil
}

// GetAccessPoint retrieves an access point configuration
func (s *ObjectLambdaService) GetAccessPoint(name string) (*AccessPointConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg, ok := s.accessPoints[name]
	if !ok {
		return nil, fmt.Errorf("access point not found: %s", name)
	}
	return cfg, nil
}

// ListAccessPoints lists all Object Lambda access points
func (s *ObjectLambdaService) ListAccessPoints() []*AccessPointConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var configs []*AccessPointConfig
	for _, cfg := range s.accessPoints {
		configs = append(configs, cfg)
	}
	return configs
}

// DeleteAccessPoint deletes an Object Lambda access point
func (s *ObjectLambdaService) DeleteAccessPoint(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.accessPoints[name]; !ok {
		return fmt.Errorf("access point not found: %s", name)
	}

	delete(s.accessPoints, name)
	return nil
}

// TransformObject applies transformations to an object on GET
func (s *ObjectLambdaService) TransformObject(
	ctx context.Context,
	accessPointName string,
	objectKey string,
	originalBody io.Reader,
	originalHeaders map[string]string,
	userIdentity UserIdentity,
) (io.Reader, map[string]string, error) {
	cfg, err := s.GetAccessPoint(accessPointName)
	if err != nil {
		return nil, nil, err
	}

	// Find matching transformation for GetObject
	var transform *ContentTransformation
	for _, tc := range cfg.TransformationConfigurations {
		for _, action := range tc.Actions {
			if action == "GetObject" {
				transform = &tc.ContentTransformation
				break
			}
		}
		if transform != nil {
			break
		}
	}

	if transform == nil {
		// No transformation, return original
		return originalBody, originalHeaders, nil
	}

	// Apply transformation based on type
	switch transform.Type {
	case TransformBuiltIn:
		return s.applyBuiltInTransform(ctx, originalBody, transform.BuiltInConfig)

	case TransformWebhook:
		return s.applyWebhookTransform(ctx, cfg, objectKey, originalBody, originalHeaders, userIdentity, transform.WebhookConfig)

	case TransformWASM:
		return s.applyWASMTransform(ctx, originalBody, transform.WASMConfig)

	default:
		return nil, nil, fmt.Errorf("unsupported transformation type: %s", transform.Type)
	}
}

// applyBuiltInTransform applies a built-in transformation
func (s *ObjectLambdaService) applyBuiltInTransform(
	ctx context.Context,
	input io.Reader,
	config *BuiltInConfig,
) (io.Reader, map[string]string, error) {
	transformer, ok := s.transformers[config.Function]
	if !ok {
		return nil, nil, fmt.Errorf("unknown built-in function: %s", config.Function)
	}

	return transformer.Transform(ctx, input, config.Parameters)
}

// applyWebhookTransform calls a webhook for transformation
func (s *ObjectLambdaService) applyWebhookTransform(
	ctx context.Context,
	cfg *AccessPointConfig,
	objectKey string,
	originalBody io.Reader,
	originalHeaders map[string]string,
	userIdentity UserIdentity,
	webhookConfig *WebhookConfig,
) (io.Reader, map[string]string, error) {
	requestID := uuid.New().String()
	outputToken := generateToken()
	outputRoute := generateToken()

	// Create presigned URL for original object (placeholder)
	inputS3URL := fmt.Sprintf("https://s3.amazonaws.com/supporting-bucket/%s?X-Amz-Signature=placeholder", url.PathEscape(objectKey))

	// Build event
	event := ObjectLambdaEvent{
		RequestID: requestID,
		GetObjectContext: GetObjectContext{
			InputS3URL:  inputS3URL,
			OutputRoute: outputRoute,
			OutputToken: outputToken,
		},
		Configuration: AccessPointConfiguration{
			AccessPointArn:           cfg.Arn,
			SupportingAccessPointArn: cfg.SupportingAccessPoint,
		},
		UserRequest: UserRequest{
			URL:     fmt.Sprintf("/%s", objectKey),
			Headers: originalHeaders,
		},
		UserIdentity:    userIdentity,
		ProtocolVersion: "1.0",
	}

	// Create response channel
	responseChan := make(chan *WriteGetObjectResponseInput, 1)
	s.pendingMu.Lock()
	s.pendingResponses[outputToken] = responseChan
	s.pendingMu.Unlock()

	defer func() {
		s.pendingMu.Lock()
		delete(s.pendingResponses, outputToken)
		s.pendingMu.Unlock()
	}()

	// Prepare request
	eventBody, err := json.Marshal(event)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	var reqBody io.Reader = bytes.NewReader(eventBody)
	if webhookConfig.PassThroughBody {
		// Include original body in a multipart request (simplified)
		bodyBytes, _ := io.ReadAll(originalBody)
		combined := struct {
			Event       json.RawMessage `json:"event"`
			ObjectBytes string          `json:"objectBytes"`
		}{
			Event:       eventBody,
			ObjectBytes: base64.StdEncoding.EncodeToString(bodyBytes),
		}
		combinedBytes, _ := json.Marshal(combined)
		reqBody = bytes.NewReader(combinedBytes)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookConfig.URL, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range webhookConfig.Headers {
		req.Header.Set(k, v)
	}

	// Send request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("webhook request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	// Wait for WriteGetObjectResponse or timeout
	timeout := webhookConfig.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	select {
	case response := <-responseChan:
		headers := map[string]string{
			"Content-Type": response.ContentType,
		}
		if response.ETag != "" {
			headers["ETag"] = response.ETag
		}
		return response.Body, headers, nil

	case <-time.After(timeout):
		return nil, nil, fmt.Errorf("webhook transformation timed out")

	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

// applyWASMTransform applies a WASM-based transformation
func (s *ObjectLambdaService) applyWASMTransform(
	ctx context.Context,
	input io.Reader,
	config *WASMConfig,
) (io.Reader, map[string]string, error) {
	// TODO: Implement WASM runtime integration
	// This would use a WASM runtime like wasmtime or wasmer
	return nil, nil, fmt.Errorf("WASM transformations not yet implemented")
}

// WriteGetObjectResponse handles the response from a Lambda function
func (s *ObjectLambdaService) WriteGetObjectResponse(input *WriteGetObjectResponseInput) error {
	s.pendingMu.RLock()
	responseChan, ok := s.pendingResponses[input.RequestToken]
	s.pendingMu.RUnlock()

	if !ok {
		return fmt.Errorf("no pending request for token: %s", input.RequestToken)
	}

	select {
	case responseChan <- input:
		return nil
	default:
		return fmt.Errorf("response channel full")
	}
}

// generateToken generates a secure random token
func generateToken() string {
	data := make([]byte, 32)
	hash := sha256.Sum256([]byte(uuid.New().String()))
	copy(data, hash[:])
	return base64.URLEncoding.EncodeToString(data)
}

// Built-in Transformers

// RedactTransformer redacts sensitive patterns from text
type RedactTransformer struct{}

func (t *RedactTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, nil, err
	}

	// Default patterns to redact
	patterns := []string{
		`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`, // Email
		`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`,                       // Phone
		`\b\d{3}[-]?\d{2}[-]?\d{4}\b`,                         // SSN
		`\b\d{16}\b`,                                          // Credit card (simple)
	}

	// Override with custom patterns if provided
	if customPatterns, ok := params["patterns"].([]interface{}); ok {
		patterns = make([]string, len(customPatterns))
		for i, p := range customPatterns {
			patterns[i] = p.(string)
		}
	}

	replacement := "[REDACTED]"
	if r, ok := params["replacement"].(string); ok {
		replacement = r
	}

	result := string(data)
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		result = re.ReplaceAllString(result, replacement)
	}

	return strings.NewReader(result), map[string]string{"Content-Type": "text/plain"}, nil
}

// PIIMaskTransformer masks PII in JSON data
type PIIMaskTransformer struct{}

func (t *PIIMaskTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, nil, err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		// Not JSON, try text redaction
		redactor := &RedactTransformer{}
		return redactor.Transform(ctx, bytes.NewReader(data), params)
	}

	// Fields to mask
	piiFields := []string{"email", "phone", "ssn", "credit_card", "password", "secret", "token"}
	if customFields, ok := params["fields"].([]interface{}); ok {
		piiFields = make([]string, len(customFields))
		for i, f := range customFields {
			piiFields[i] = f.(string)
		}
	}

	maskValue := func(v interface{}) interface{} {
		if s, ok := v.(string); ok {
			if len(s) > 4 {
				return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
			}
			return "****"
		}
		return "****"
	}

	var maskPII func(obj map[string]interface{})
	maskPII = func(obj map[string]interface{}) {
		for key, value := range obj {
			keyLower := strings.ToLower(key)
			for _, piiField := range piiFields {
				if strings.Contains(keyLower, piiField) {
					obj[key] = maskValue(value)
					break
				}
			}

			// Recurse into nested objects
			if nested, ok := value.(map[string]interface{}); ok {
				maskPII(nested)
			}
			if arr, ok := value.([]interface{}); ok {
				for _, item := range arr {
					if nested, ok := item.(map[string]interface{}); ok {
						maskPII(nested)
					}
				}
			}
		}
	}

	maskPII(obj)

	result, err := json.Marshal(obj)
	if err != nil {
		return nil, nil, err
	}

	return bytes.NewReader(result), map[string]string{"Content-Type": "application/json"}, nil
}

// FilterFieldsTransformer filters JSON fields
type FilterFieldsTransformer struct{}

func (t *FilterFieldsTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, nil, err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, nil, fmt.Errorf("input must be JSON: %w", err)
	}

	// Fields to include (whitelist)
	if includeFields, ok := params["include"].([]interface{}); ok {
		filtered := make(map[string]interface{})
		for _, f := range includeFields {
			field := f.(string)
			if v, exists := obj[field]; exists {
				filtered[field] = v
			}
		}
		obj = filtered
	}

	// Fields to exclude (blacklist)
	if excludeFields, ok := params["exclude"].([]interface{}); ok {
		for _, f := range excludeFields {
			delete(obj, f.(string))
		}
	}

	result, err := json.Marshal(obj)
	if err != nil {
		return nil, nil, err
	}

	return bytes.NewReader(result), map[string]string{"Content-Type": "application/json"}, nil
}

// ConvertJSONTransformer converts CSV to JSON
type ConvertJSONTransformer struct{}

func (t *ConvertJSONTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return nil, nil, fmt.Errorf("CSV must have header and at least one data row")
	}

	delimiter := ","
	if d, ok := params["delimiter"].(string); ok {
		delimiter = d
	}

	headers := strings.Split(lines[0], delimiter)
	var records []map[string]string

	for i := 1; i < len(lines); i++ {
		values := strings.Split(lines[i], delimiter)
		record := make(map[string]string)
		for j, header := range headers {
			if j < len(values) {
				record[strings.TrimSpace(header)] = strings.TrimSpace(values[j])
			}
		}
		records = append(records, record)
	}

	result, err := json.Marshal(records)
	if err != nil {
		return nil, nil, err
	}

	return bytes.NewReader(result), map[string]string{"Content-Type": "application/json"}, nil
}

// CompressTransformer compresses content.
type CompressTransformer struct{}

// DefaultMaxTransformSize is the default maximum size of data that can be transformed in memory (100MB).
const DefaultMaxTransformSize = 100 * 1024 * 1024

// DefaultStreamingThreshold is the default size threshold above which streaming mode is used (10MB).
const DefaultStreamingThreshold = 10 * 1024 * 1024

// maxTransformSize is the configurable maximum transform size (defaults to DefaultMaxTransformSize).
// Uses atomic operations for thread-safe access.
var maxTransformSize atomic.Int64

// streamingThreshold is the configurable size threshold for using streaming mode.
// Uses atomic operations for thread-safe access.
var streamingThreshold atomic.Int64

func init() {
	maxTransformSize.Store(DefaultMaxTransformSize)
	streamingThreshold.Store(DefaultStreamingThreshold)
}

// SetMaxTransformSize sets the maximum size of data that can be transformed in memory.
// This function is thread-safe and can be called at any time.
// Warning: Setting this too high may cause memory exhaustion during transformation operations.
func SetMaxTransformSize(size int64) {
	if size > 0 {
		maxTransformSize.Store(size)
	} else {
		log.Warn().
			Int64("size", size).
			Int64("current", maxTransformSize.Load()).
			Msg("SetMaxTransformSize called with invalid size (<= 0), ignoring")
	}
}

// GetMaxTransformSize returns the current maximum transform size.
// This function is thread-safe.
func GetMaxTransformSize() int64 {
	return maxTransformSize.Load()
}

// SetStreamingThreshold sets the size threshold above which streaming mode is used
// for compression/decompression operations instead of buffering in memory.
// This function is thread-safe and can be called at any time.
func SetStreamingThreshold(size int64) {
	if size > 0 {
		streamingThreshold.Store(size)
	} else {
		log.Warn().
			Int64("size", size).
			Int64("current", streamingThreshold.Load()).
			Msg("SetStreamingThreshold called with invalid size (<= 0), ignoring")
	}
}

// GetStreamingThreshold returns the current streaming threshold.
// This function is thread-safe.
func GetStreamingThreshold() int64 {
	return streamingThreshold.Load()
}

// Compression magic bytes for auto-detection.
const (
	gzipMagic1 = 0x1f
	gzipMagic2 = 0x8b
	zstdMagic1 = 0x28
	zstdMagic2 = 0xb5
	zstdMagic3 = 0x2f
	zstdMagic4 = 0xfd
)

// detectCompressionAlgorithm attempts to detect the compression algorithm from magic bytes.
// Returns empty string if no known compression format is detected.
func detectCompressionAlgorithm(data []byte) string {
	switch {
	case len(data) >= 2 && data[0] == gzipMagic1 && data[1] == gzipMagic2:
		return "gzip"
	case len(data) >= 4 && data[0] == zstdMagic1 && data[1] == zstdMagic2 && data[2] == zstdMagic3 && data[3] == zstdMagic4:
		return "zstd"
	default:
		return ""
	}
}

func (t *CompressTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	// Get compression algorithm from params (default: gzip)
	algorithm := "gzip"
	if alg, ok := params["algorithm"].(string); ok {
		algorithm = alg
	}

	// Get compression level from params
	level := -1 // default level
	if lvl, ok := params["level"].(int); ok {
		level = lvl
	} else if lvl, ok := params["level"].(float64); ok {
		level = int(lvl)
	}

	// Check if streaming mode should be used
	// If content_length is provided and exceeds streaming threshold, use streaming
	useStreaming := false
	if contentLength, ok := params["content_length"].(int64); ok {
		streamingThreshold := GetStreamingThreshold()
		if contentLength > streamingThreshold {
			useStreaming = true
			log.Debug().
				Int64("content_length", contentLength).
				Int64("streaming_threshold", streamingThreshold).
				Str("algorithm", algorithm).
				Msg("Using streaming compression for large file")
		}
	}

	if useStreaming {
		return t.transformStreaming(ctx, input, algorithm, level)
	}

	// Buffered mode for small files
	maxSize := GetMaxTransformSize()
	limitedReader := io.LimitReader(input, maxSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > maxSize {
		return nil, nil, fmt.Errorf("object size exceeds maximum transform size of %d bytes", maxSize)
	}

	var buf bytes.Buffer
	var contentEncoding string

	switch strings.ToLower(algorithm) {
	case "gzip":
		if err := compressGzip(&buf, data, level); err != nil {
			return nil, nil, err
		}
		contentEncoding = "gzip"

	case "zstd":
		if err := compressZstd(&buf, data, level); err != nil {
			return nil, nil, err
		}
		contentEncoding = "zstd"

	default:
		return nil, nil, fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}

	return bytes.NewReader(buf.Bytes()), map[string]string{"Content-Encoding": contentEncoding}, nil
}

// transformStreaming performs streaming compression without buffering the entire input
func (t *CompressTransformer) transformStreaming(ctx context.Context, input io.Reader, algorithm string, level int) (io.Reader, map[string]string, error) {
	pr, pw := io.Pipe()
	var contentEncoding string

	switch strings.ToLower(algorithm) {
	case "gzip":
		contentEncoding = "gzip"
		go func() {
			defer pw.Close()
			gzLevel := gzip.DefaultCompression
			if level >= 0 && level <= 9 {
				gzLevel = level
			}
			writer, err := gzip.NewWriterLevel(pw, gzLevel)
			if err != nil {
				pw.CloseWithError(fmt.Errorf("failed to create gzip writer: %w", err))
				return
			}
			defer writer.Close()

			buf := make([]byte, 32*1024) // 32KB buffer for streaming
			for {
				select {
				case <-ctx.Done():
					pw.CloseWithError(ctx.Err())
					return
				default:
				}

				n, err := input.Read(buf)
				if n > 0 {
					if _, werr := writer.Write(buf[:n]); werr != nil {
						pw.CloseWithError(fmt.Errorf("failed to write gzip data: %w", werr))
						return
					}
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					pw.CloseWithError(fmt.Errorf("failed to read input: %w", err))
					return
				}
			}
		}()

	case "zstd":
		contentEncoding = "zstd"
		go func() {
			defer pw.Close()
			var writer *zstd.Encoder
			var err error
			if level >= 1 && level <= 22 {
				writer, err = zstd.NewWriter(pw, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
			} else {
				writer, err = zstd.NewWriter(pw, zstd.WithEncoderLevel(zstd.SpeedDefault))
			}
			if err != nil {
				pw.CloseWithError(fmt.Errorf("failed to create zstd writer: %w", err))
				return
			}
			defer writer.Close()

			buf := make([]byte, 32*1024) // 32KB buffer for streaming
			for {
				select {
				case <-ctx.Done():
					pw.CloseWithError(ctx.Err())
					return
				default:
				}

				n, err := input.Read(buf)
				if n > 0 {
					if _, werr := writer.Write(buf[:n]); werr != nil {
						pw.CloseWithError(fmt.Errorf("failed to write zstd data: %w", werr))
						return
					}
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					pw.CloseWithError(fmt.Errorf("failed to read input: %w", err))
					return
				}
			}
		}()

	default:
		return nil, nil, fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}

	return pr, map[string]string{"Content-Encoding": contentEncoding}, nil
}

// compressGzip compresses data using gzip and writes to the buffer
func compressGzip(buf *bytes.Buffer, data []byte, level int) (err error) {
	gzLevel := gzip.DefaultCompression
	if level >= 0 && level <= 9 {
		gzLevel = level
	}
	writer, err := gzip.NewWriterLevel(buf, gzLevel)
	if err != nil {
		return fmt.Errorf("failed to create gzip writer: %w", err)
	}
	defer func() {
		if cerr := writer.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close gzip writer: %w", cerr)
		}
	}()

	if _, err = writer.Write(data); err != nil {
		return fmt.Errorf("failed to write gzip data: %w", err)
	}
	return nil
}

// compressZstd compresses data using zstd and writes to the buffer
func compressZstd(buf *bytes.Buffer, data []byte, level int) (err error) {
	var writer *zstd.Encoder
	if level >= 1 && level <= 22 {
		writer, err = zstd.NewWriter(buf, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
	} else {
		writer, err = zstd.NewWriter(buf, zstd.WithEncoderLevel(zstd.SpeedDefault))
	}
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer func() {
		if cerr := writer.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close zstd writer: %w", cerr)
		}
	}()

	if _, err = writer.Write(data); err != nil {
		return fmt.Errorf("failed to write zstd data: %w", err)
	}
	return nil
}

// DecompressTransformer decompresses content
type DecompressTransformer struct{}

func (t *DecompressTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	// Get compression algorithm from params (auto-detect if not specified)
	algorithm := ""
	if alg, ok := params["algorithm"].(string); ok {
		algorithm = strings.ToLower(alg)
	}

	// Check if streaming mode should be used
	useStreaming := false
	if contentLength, ok := params["content_length"].(int64); ok {
		streamingThreshold := GetStreamingThreshold()
		if contentLength > streamingThreshold {
			useStreaming = true
			log.Debug().
				Int64("content_length", contentLength).
				Int64("streaming_threshold", streamingThreshold).
				Str("algorithm", algorithm).
				Msg("Using streaming decompression for large file")
		}
	}

	if useStreaming && algorithm != "" {
		// For streaming mode, algorithm must be specified (no auto-detection)
		return t.transformStreaming(ctx, input, algorithm)
	}

	// Buffered mode for small files or when auto-detection is needed
	maxSize := GetMaxTransformSize()
	limitedReader := io.LimitReader(input, maxSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > maxSize {
		return nil, nil, fmt.Errorf("object size exceeds maximum transform size of %d bytes", maxSize)
	}

	// Auto-detect compression based on magic bytes if algorithm not specified
	if algorithm == "" {
		algorithm = detectCompressionAlgorithm(data)
		if algorithm == "" {
			// Data doesn't appear to be compressed, return as-is
			return bytes.NewReader(data), nil, nil
		}
	}

	var result []byte

	switch algorithm {
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer func() { _ = reader.Close() }()
		// Limit decompressed size to prevent decompression bombs
		decompressLimit := io.LimitReader(reader, maxSize+1)
		result, err = io.ReadAll(decompressLimit)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decompress gzip data: %w", err)
		}
		if int64(len(result)) > maxSize {
			return nil, nil, fmt.Errorf("decompressed size exceeds maximum transform size of %d bytes", maxSize)
		}

	case "zstd":
		decoder, err := zstd.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create zstd decoder: %w", err)
		}
		defer decoder.Close()
		// Limit decompressed size to prevent decompression bombs
		decompressLimit := io.LimitReader(decoder, maxSize+1)
		result, err = io.ReadAll(decompressLimit)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decompress zstd data: %w", err)
		}
		if int64(len(result)) > maxSize {
			return nil, nil, fmt.Errorf("decompressed size exceeds maximum transform size of %d bytes", maxSize)
		}

	default:
		return nil, nil, fmt.Errorf("unsupported decompression algorithm: %s", algorithm)
	}

	return bytes.NewReader(result), nil, nil
}

// transformStreaming performs streaming decompression for large files
func (t *DecompressTransformer) transformStreaming(ctx context.Context, input io.Reader, algorithm string) (io.Reader, map[string]string, error) {
	pr, pw := io.Pipe()

	switch algorithm {
	case "gzip":
		go func() {
			defer pw.Close()
			reader, err := gzip.NewReader(input)
			if err != nil {
				pw.CloseWithError(fmt.Errorf("failed to create gzip reader: %w", err))
				return
			}
			defer func() { _ = reader.Close() }()

			buf := make([]byte, 32*1024) // 32KB buffer for streaming
			for {
				select {
				case <-ctx.Done():
					pw.CloseWithError(ctx.Err())
					return
				default:
				}

				n, err := reader.Read(buf)
				if n > 0 {
					if _, werr := pw.Write(buf[:n]); werr != nil {
						pw.CloseWithError(fmt.Errorf("failed to write decompressed data: %w", werr))
						return
					}
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					pw.CloseWithError(fmt.Errorf("failed to decompress gzip data: %w", err))
					return
				}
			}
		}()

	case "zstd":
		go func() {
			defer pw.Close()
			decoder, err := zstd.NewReader(input)
			if err != nil {
				pw.CloseWithError(fmt.Errorf("failed to create zstd decoder: %w", err))
				return
			}
			defer decoder.Close()

			buf := make([]byte, 32*1024) // 32KB buffer for streaming
			for {
				select {
				case <-ctx.Done():
					pw.CloseWithError(ctx.Err())
					return
				default:
				}

				n, err := decoder.Read(buf)
				if n > 0 {
					if _, werr := pw.Write(buf[:n]); werr != nil {
						pw.CloseWithError(fmt.Errorf("failed to write decompressed data: %w", werr))
						return
					}
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					pw.CloseWithError(fmt.Errorf("failed to decompress zstd data: %w", err))
					return
				}
			}
		}()

	default:
		return nil, nil, fmt.Errorf("unsupported decompression algorithm for streaming: %s", algorithm)
	}

	log.Debug().
		Str("algorithm", algorithm).
		Msg("Started streaming decompression")

	return pr, nil, nil
}

// GetAccessPointForBucketConfiguration implements S3 API
func (s *ObjectLambdaService) GetAccessPointForBucketConfiguration(accessPoint, bucket string) (*AccessPointConfig, error) {
	return s.GetAccessPoint(accessPoint)
}

// CreateAccessPointForObjectLambda implements S3 API
func (s *ObjectLambdaService) CreateAccessPointForObjectLambda(name string, config *AccessPointConfig) error {
	config.Name = name
	return s.CreateAccessPoint(config)
}

// DeleteAccessPointForObjectLambda implements S3 API
func (s *ObjectLambdaService) DeleteAccessPointForObjectLambda(name string) error {
	return s.DeleteAccessPoint(name)
}

// GetAccessPointConfigurationForObjectLambda implements S3 API
func (s *ObjectLambdaService) GetAccessPointConfigurationForObjectLambda(name string) (*AccessPointConfig, error) {
	return s.GetAccessPoint(name)
}

// PutAccessPointConfigurationForObjectLambda implements S3 API
func (s *ObjectLambdaService) PutAccessPointConfigurationForObjectLambda(name string, config *AccessPointConfig) error {
	config.Name = name
	return s.CreateAccessPoint(config)
}

// ListAccessPointsForObjectLambda implements S3 API
func (s *ObjectLambdaService) ListAccessPointsForObjectLambda(accountID string) []*AccessPointConfig {
	return s.ListAccessPoints()
}
