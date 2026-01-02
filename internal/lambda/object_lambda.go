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
	"errors"
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
	"github.com/piwi3910/nebulaio/internal/httputil"
	"github.com/piwi3910/nebulaio/internal/metrics"
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

// Compression algorithm constants.
const (
	compressionAlgGzip = "gzip"
	compressionAlgZstd = "zstd"
	compressionAlgAuto = "auto" // Used for auto-detection metrics label
)

// AccessPointConfig defines an Object Lambda access point configuration.
type AccessPointConfig struct {
	CreatedAt                    time.Time              `json:"created_at"`
	Name                         string                 `json:"name"`
	Arn                          string                 `json:"arn"`
	SupportingAccessPoint        string                 `json:"supporting_access_point"`
	TransformationConfigurations []TransformationConfig `json:"transformation_configurations"`
	AllowedFeatures              []string               `json:"allowed_features"`
}

// TransformationConfig defines a single transformation.
type TransformationConfig struct {
	ContentTransformation ContentTransformation `json:"content_transformation"`
	Actions               []string              `json:"actions"`
}

// ContentTransformation specifies how to transform content.
type ContentTransformation struct {
	WebhookConfig *WebhookConfig     `json:"webhook_config,omitempty"`
	BuiltInConfig *BuiltInConfig     `json:"builtin_config,omitempty"`
	WASMConfig    *WASMConfig        `json:"wasm_config,omitempty"`
	Type          TransformationType `json:"type"`
}

// WebhookConfig defines webhook-based transformation settings.
type WebhookConfig struct {
	Headers         map[string]string `json:"headers,omitempty"`
	URL             string            `json:"url"`
	Timeout         time.Duration     `json:"timeout"`
	RetryCount      int               `json:"retry_count"`
	PassThroughBody bool              `json:"pass_through_body"`
}

// BuiltInConfig defines built-in transformation settings.
type BuiltInConfig struct {
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Function   BuiltInTransform       `json:"function"`
}

// WASMConfig defines WASM-based transformation settings.
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

// ObjectLambdaEvent is sent to webhook transformations.
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

// GetObjectContext contains GetObject-specific context.
type GetObjectContext struct {
	// InputS3URL is the presigned URL to fetch the original object
	InputS3URL string `json:"inputS3Url"`

	// OutputRoute is the route token for WriteGetObjectResponse
	OutputRoute string `json:"outputRoute"`

	// OutputToken is the token for WriteGetObjectResponse
	OutputToken string `json:"outputToken"`
}

// AccessPointConfiguration contains access point info.
type AccessPointConfiguration struct {
	// AccessPointArn is the Object Lambda access point ARN
	AccessPointArn string `json:"accessPointArn"`

	// SupportingAccessPointArn is the supporting access point ARN
	SupportingAccessPointArn string `json:"supportingAccessPointArn"`

	// Payload is optional custom payload
	Payload string `json:"payload,omitempty"`
}

// UserRequest contains the original request details.
type UserRequest struct {
	Headers map[string]string `json:"headers"`
	URL     string            `json:"url"`
}

// UserIdentity contains requester identity.
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

// WriteGetObjectResponseInput is used by Lambda to write transformed response.
type WriteGetObjectResponseInput struct {
	Body               io.Reader         `json:"-"`
	Metadata           map[string]string `json:"Metadata,omitempty"`
	LastModified       *time.Time        `json:"LastModified,omitempty"`
	Expires            *time.Time        `json:"Expires,omitempty"`
	CacheControl       string            `json:"CacheControl,omitempty"`
	ErrorMessage       string            `json:"ErrorMessage,omitempty"`
	ContentType        string            `json:"ContentType,omitempty"`
	RequestRoute       string            `json:"RequestRoute"`
	ContentDisposition string            `json:"ContentDisposition,omitempty"`
	ContentEncoding    string            `json:"ContentEncoding,omitempty"`
	ContentLanguage    string            `json:"ContentLanguage,omitempty"`
	ETag               string            `json:"ETag,omitempty"`
	ErrorCode          string            `json:"ErrorCode,omitempty"`
	RequestToken       string            `json:"RequestToken"`
	ContentLength      int64             `json:"ContentLength"`
	StatusCode         int               `json:"StatusCode"`
}

// ObjectLambdaService manages Object Lambda access points.
type ObjectLambdaService struct {
	accessPoints     map[string]*AccessPointConfig
	httpClient       *http.Client
	transformers     map[BuiltInTransform]Transformer
	pendingResponses map[string]chan *WriteGetObjectResponseInput
	mu               sync.RWMutex
	pendingMu        sync.RWMutex
}

// Transformer interface for built-in transformations.
type Transformer interface {
	Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error)
}

// NewObjectLambdaService creates a new Object Lambda service.
func NewObjectLambdaService() *ObjectLambdaService {
	svc := &ObjectLambdaService{
		accessPoints:     make(map[string]*AccessPointConfig),
		pendingResponses: make(map[string]chan *WriteGetObjectResponseInput),
		httpClient:       httputil.NewClientWithTimeout(60 * time.Second),
		transformers:     make(map[BuiltInTransform]Transformer),
	}

	// Register built-in transformers
	svc.registerBuiltInTransformers()

	return svc
}

// registerBuiltInTransformers registers all built-in transformation functions.
func (s *ObjectLambdaService) registerBuiltInTransformers() {
	s.transformers[BuiltInRedact] = &RedactTransformer{}
	s.transformers[BuiltInMaskPII] = &PIIMaskTransformer{}
	s.transformers[BuiltInFilterFields] = &FilterFieldsTransformer{}
	s.transformers[BuiltInConvertJSON] = &ConvertJSONTransformer{}
	s.transformers[BuiltInCompress] = &CompressTransformer{}
	s.transformers[BuiltInDecompress] = &DecompressTransformer{}
}

// CreateAccessPoint creates an Object Lambda access point.
func (s *ObjectLambdaService) CreateAccessPoint(cfg *AccessPointConfig) error {
	if cfg.Name == "" {
		return errors.New("access point name is required")
	}

	if cfg.SupportingAccessPoint == "" {
		return errors.New("supporting access point is required")
	}

	if len(cfg.TransformationConfigurations) == 0 {
		return errors.New("at least one transformation configuration is required")
	}

	cfg.CreatedAt = time.Now()
	if cfg.Arn == "" {
		cfg.Arn = "arn:aws:s3-object-lambda:region:account:accesspoint/" + cfg.Name
	}

	s.mu.Lock()
	s.accessPoints[cfg.Name] = cfg
	s.mu.Unlock()

	return nil
}

// GetAccessPoint retrieves an access point configuration.
func (s *ObjectLambdaService) GetAccessPoint(name string) (*AccessPointConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg, ok := s.accessPoints[name]
	if !ok {
		return nil, fmt.Errorf("access point not found: %s", name)
	}

	return cfg, nil
}

// ListAccessPoints lists all Object Lambda access points.
func (s *ObjectLambdaService) ListAccessPoints() []*AccessPointConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configs := make([]*AccessPointConfig, 0, len(s.accessPoints))
	for _, cfg := range s.accessPoints {
		configs = append(configs, cfg)
	}

	return configs
}

// DeleteAccessPoint deletes an Object Lambda access point.
func (s *ObjectLambdaService) DeleteAccessPoint(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.accessPoints[name]; !ok {
		return fmt.Errorf("access point not found: %s", name)
	}

	delete(s.accessPoints, name)

	return nil
}

// TransformObject applies transformations to an object on GET.
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

// applyBuiltInTransform applies a built-in transformation.
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

// applyWebhookTransform calls a webhook for transformation.
func (s *ObjectLambdaService) applyWebhookTransform(
	ctx context.Context,
	cfg *AccessPointConfig,
	objectKey string,
	originalBody io.Reader,
	originalHeaders map[string]string,
	userIdentity UserIdentity,
	webhookConfig *WebhookConfig,
) (io.Reader, map[string]string, error) {
	// Validate webhook URL to prevent SSRF attacks
	if err := validateWebhookURL(ctx, webhookConfig.URL); err != nil {
		return nil, nil, fmt.Errorf("webhook URL validation failed: %w", err)
	}

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
			URL:     "/" + objectKey,
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
			ObjectBytes string          `json:"objectBytes"`
			Event       json.RawMessage `json:"event"`
		}{
			Event:       eventBody,
			ObjectBytes: base64.StdEncoding.EncodeToString(bodyBytes),
		}
		combinedBytes, _ := json.Marshal(combined)
		reqBody = bytes.NewReader(combinedBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookConfig.URL, reqBody)
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
		return nil, nil, errors.New("webhook transformation timed out")

	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

// applyWASMTransform applies a WASM-based transformation.
func (s *ObjectLambdaService) applyWASMTransform(
	ctx context.Context,
	input io.Reader,
	config *WASMConfig,
) (io.Reader, map[string]string, error) {
	// TODO: Implement WASM runtime integration
	// This would use a WASM runtime like wasmtime or wasmer
	return nil, nil, errors.New("WASM transformations not yet implemented")
}

// WriteGetObjectResponse handles the response from a Lambda function.
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
		return errors.New("response channel full")
	}
}

// generateToken generates a secure random token.
func generateToken() string {
	data := make([]byte, 32)
	hash := sha256.Sum256([]byte(uuid.New().String()))
	copy(data, hash[:])

	return base64.URLEncoding.EncodeToString(data)
}

// Built-in Transformers

// RedactTransformer redacts sensitive patterns from text.
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

// PIIMaskTransformer masks PII in JSON data.
type PIIMaskTransformer struct{}

func (t *PIIMaskTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, nil, err
	}

	var obj map[string]interface{}
	err = json.Unmarshal(data, &obj)
	if err != nil {
		// Not JSON, try text redaction
		redactor := &RedactTransformer{}
		return redactor.Transform(ctx, bytes.NewReader(data), params)
	}

	piiFields := t.getPIIFields(params)
	t.maskPIIRecursive(obj, piiFields)

	result, err := json.Marshal(obj)
	if err != nil {
		return nil, nil, err
	}

	return bytes.NewReader(result), map[string]string{"Content-Type": "application/json"}, nil
}

func (t *PIIMaskTransformer) getPIIFields(params map[string]interface{}) []string {
	piiFields := []string{"email", "phone", "ssn", "credit_card", "password", "secret", "token"}
	if customFields, ok := params["fields"].([]interface{}); ok {
		piiFields = make([]string, len(customFields))
		for i, f := range customFields {
			piiFields[i] = f.(string)
		}
	}
	return piiFields
}

func (t *PIIMaskTransformer) maskValue(v interface{}) interface{} {
	if s, ok := v.(string); ok {
		if len(s) > 4 {
			return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
		}
		return "****"
	}
	return "****"
}

func (t *PIIMaskTransformer) maskPIIRecursive(obj map[string]interface{}, piiFields []string) {
	for key, value := range obj {
		if t.shouldMaskField(key, piiFields) {
			obj[key] = t.maskValue(value)
			continue
		}

		t.maskNestedStructures(value, piiFields)
	}
}

func (t *PIIMaskTransformer) shouldMaskField(key string, piiFields []string) bool {
	keyLower := strings.ToLower(key)
	for _, piiField := range piiFields {
		if strings.Contains(keyLower, piiField) {
			return true
		}
	}
	return false
}

func (t *PIIMaskTransformer) maskNestedStructures(value interface{}, piiFields []string) {
	// Recurse into nested objects
	if nested, ok := value.(map[string]interface{}); ok {
		t.maskPIIRecursive(nested, piiFields)
	}

	// Recurse into arrays
	if arr, ok := value.([]interface{}); ok {
		t.maskArrayItems(arr, piiFields)
	}
}

func (t *PIIMaskTransformer) maskArrayItems(arr []interface{}, piiFields []string) {
	for _, item := range arr {
		if nested, ok := item.(map[string]interface{}); ok {
			t.maskPIIRecursive(nested, piiFields)
		}
	}
}

// FilterFieldsTransformer filters JSON fields.
type FilterFieldsTransformer struct{}

func (t *FilterFieldsTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, nil, err
	}

	var obj map[string]interface{}
	err = json.Unmarshal(data, &obj)
	if err != nil {
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

// ConvertJSONTransformer converts CSV to JSON.
type ConvertJSONTransformer struct{}

func (t *ConvertJSONTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return nil, nil, errors.New("CSV must have header and at least one data row")
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

// DefaultMaxDecompressSize is the default maximum size of decompressed output (1GB).
// This protects against decompression bomb attacks where small compressed files expand to huge sizes.
const DefaultMaxDecompressSize = 1024 * 1024 * 1024

// maxTransformSize is the configurable maximum transform size (defaults to DefaultMaxTransformSize).
// Uses atomic operations for thread-safe access.
var maxTransformSize atomic.Int64

// streamingThreshold is the configurable size threshold for using streaming mode.
// Uses atomic operations for thread-safe access.
var streamingThreshold atomic.Int64

// maxDecompressSize is the configurable maximum decompressed output size (defaults to DefaultMaxDecompressSize).
// Uses atomic operations for thread-safe access.
var maxDecompressSize atomic.Int64

func init() {
	maxTransformSize.Store(DefaultMaxTransformSize)
	streamingThreshold.Store(DefaultStreamingThreshold)
	maxDecompressSize.Store(DefaultMaxDecompressSize)
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

// SetMaxDecompressSize sets the maximum size of decompressed output.
// This function is thread-safe and can be called at any time.
// Warning: Setting this too high may allow decompression bomb attacks to succeed.
func SetMaxDecompressSize(size int64) {
	if size > 0 {
		maxDecompressSize.Store(size)
	} else {
		log.Warn().
			Int64("size", size).
			Int64("current", maxDecompressSize.Load()).
			Msg("SetMaxDecompressSize called with invalid size (<= 0), ignoring")
	}
}

// GetMaxDecompressSize returns the current maximum decompressed output size.
// This function is thread-safe.
func GetMaxDecompressSize() int64 {
	return maxDecompressSize.Load()
}

// ErrDecompressSizeLimitExceeded is returned when decompressed output exceeds the configured limit.
var ErrDecompressSizeLimitExceeded = errors.New("decompressed size exceeds maximum allowed limit (potential decompression bomb)")

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
		return compressionAlgGzip
	case len(data) >= 4 && data[0] == zstdMagic1 && data[1] == zstdMagic2 && data[2] == zstdMagic3 && data[3] == zstdMagic4:
		return compressionAlgZstd
	default:
		return ""
	}
}

func (t *CompressTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	startTime := time.Now()

	// Get compression algorithm from params (default: gzip)
	algorithm := compressionAlgGzip
	if alg, ok := params["algorithm"].(string); ok {
		algorithm = alg
	}

	// Track in-flight operations
	metrics.IncrementLambdaOperationsInFlight(algorithm)
	defer metrics.DecrementLambdaOperationsInFlight(algorithm)

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
		result, headers, err := t.transformStreaming(ctx, input, algorithm, level)
		duration := time.Since(startTime)
		success := err == nil
		// For streaming mode, byte sizes are recorded as 0 since we process data
		// incrementally without buffering. Duration and success/error counts are
		// still tracked for observability. Use LambdaBytesProcessed for throughput
		// metrics in streaming scenarios.
		metrics.RecordLambdaCompression(algorithm, "compress", success, duration, 0, 0)

		return result, headers, err
	}

	// Buffered mode for small files
	maxSize := GetMaxTransformSize()
	limitedReader := io.LimitReader(input, maxSize+1)

	data, err := io.ReadAll(limitedReader)
	if err != nil {
		duration := time.Since(startTime)
		metrics.RecordLambdaCompression(algorithm, "compress", false, duration, 0, 0)

		return nil, nil, err
	}

	originalSize := int64(len(data))

	if originalSize > maxSize {
		duration := time.Since(startTime)
		metrics.RecordLambdaCompression(algorithm, "compress", false, duration, originalSize, 0)

		return nil, nil, fmt.Errorf("object size exceeds maximum transform size of %d bytes", maxSize)
	}

	// Record bytes processed (input)
	metrics.RecordLambdaBytesProcessed(algorithm, "compress", originalSize)

	var (
		buf             bytes.Buffer
		contentEncoding string
	)

	switch strings.ToLower(algorithm) {
	case compressionAlgGzip:
		err := compressGzip(&buf, data, level)
		if err != nil {
			duration := time.Since(startTime)
			metrics.RecordLambdaCompression(algorithm, "compress", false, duration, originalSize, 0)

			return nil, nil, err
		}

		contentEncoding = compressionAlgGzip

	case compressionAlgZstd:
		err := compressZstd(&buf, data, level)
		if err != nil {
			duration := time.Since(startTime)
			metrics.RecordLambdaCompression(algorithm, "compress", false, duration, originalSize, 0)

			return nil, nil, err
		}

		contentEncoding = compressionAlgZstd

	default:
		duration := time.Since(startTime)
		metrics.RecordLambdaCompression(algorithm, "compress", false, duration, originalSize, 0)

		return nil, nil, fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}

	compressedSize := int64(buf.Len())
	duration := time.Since(startTime)
	metrics.RecordLambdaCompression(algorithm, "compress", true, duration, originalSize, compressedSize)

	return bytes.NewReader(buf.Bytes()), map[string]string{"Content-Encoding": contentEncoding}, nil
}

// transformStreaming performs streaming compression without buffering the entire input.
func (t *CompressTransformer) transformStreaming(ctx context.Context, input io.Reader, algorithm string, level int) (io.Reader, map[string]string, error) {
	pr, pw := io.Pipe()

	contentEncoding, err := t.startCompressionStream(ctx, input, pw, algorithm, level)
	if err != nil {
		return nil, nil, err
	}

	return pr, map[string]string{"Content-Encoding": contentEncoding}, nil
}

// startCompressionStream starts a goroutine to compress input stream.
func (t *CompressTransformer) startCompressionStream(ctx context.Context, input io.Reader, pw *io.PipeWriter, algorithm string, level int) (string, error) {
	switch strings.ToLower(algorithm) {
	case compressionAlgGzip:
		t.streamGzipCompression(ctx, input, pw, level)

		return compressionAlgGzip, nil
	case compressionAlgZstd:
		t.streamZstdCompression(ctx, input, pw, level)

		return compressionAlgZstd, nil
	default:
		return "", fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}
}

// streamGzipCompression compresses input stream using gzip.
func (t *CompressTransformer) streamGzipCompression(ctx context.Context, input io.Reader, pw *io.PipeWriter, level int) {
	go func() {
		defer func() { _ = pw.Close() }()

		writer, err := t.createGzipWriter(pw, level)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to create gzip writer: %w", err))

			return
		}
		defer func() { _ = writer.Close() }()

		t.streamCompress(ctx, input, writer, pw, "gzip")
	}()
}

// streamZstdCompression compresses input stream using zstd.
func (t *CompressTransformer) streamZstdCompression(ctx context.Context, input io.Reader, pw *io.PipeWriter, level int) {
	go func() {
		defer func() { _ = pw.Close() }()

		writer, err := t.createZstdWriter(pw, level)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to create zstd writer: %w", err))

			return
		}
		defer func() { _ = writer.Close() }()

		t.streamCompress(ctx, input, writer, pw, "zstd")
	}()
}

// createGzipWriter creates a gzip writer with the specified compression level.
func (t *CompressTransformer) createGzipWriter(w io.Writer, level int) (*gzip.Writer, error) {
	gzLevel := gzip.DefaultCompression
	if level >= 0 && level <= 9 {
		gzLevel = level
	}

	return gzip.NewWriterLevel(w, gzLevel)
}

// createZstdWriter creates a zstd writer with the specified compression level.
func (t *CompressTransformer) createZstdWriter(w io.Writer, level int) (*zstd.Encoder, error) {
	if level >= 1 && level <= 22 {
		return zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
	}

	return zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault))
}

// streamCompress performs the actual streaming compression loop.
func (t *CompressTransformer) streamCompress(ctx context.Context, input io.Reader, writer io.Writer, pw *io.PipeWriter, algorithm string) {
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
			if werr := t.writeCompressedData(writer, buf[:n], algorithm, pw); werr != nil {
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
}

// writeCompressedData writes compressed data and handles errors.
func (t *CompressTransformer) writeCompressedData(writer io.Writer, data []byte, algorithm string, pw *io.PipeWriter) error {
	_, err := writer.Write(data)
	if err != nil {
		pw.CloseWithError(fmt.Errorf("failed to write %s data: %w", algorithm, err))

		return err
	}

	return nil
}

// compressGzip compresses data using gzip and writes to the buffer.
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
		cerr := writer.Close()
		if cerr != nil && err == nil {
			err = fmt.Errorf("failed to close gzip writer: %w", cerr)
		}
	}()

	_, err = writer.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write gzip data: %w", err)
	}

	return nil
}

// compressZstd compresses data using zstd and writes to the buffer.
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
		cerr := writer.Close()
		if cerr != nil && err == nil {
			err = fmt.Errorf("failed to close zstd writer: %w", cerr)
		}
	}()

	_, err = writer.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write zstd data: %w", err)
	}

	return nil
}

// DecompressTransformer decompresses content.
type DecompressTransformer struct{}

func (t *DecompressTransformer) Transform(ctx context.Context, input io.Reader, params map[string]interface{}) (io.Reader, map[string]string, error) {
	startTime := time.Now()
	algorithm := t.getAlgorithm(params)

	// Track in-flight operations (use auto if algorithm not yet detected)
	// Use pointer so defer sees updated value after auto-detection
	metricsAlgorithm := algorithm
	if metricsAlgorithm == "" {
		metricsAlgorithm = compressionAlgAuto
	}
	metrics.IncrementLambdaOperationsInFlight(metricsAlgorithm)
	finalAlgorithm := &metricsAlgorithm
	defer func() { metrics.DecrementLambdaOperationsInFlight(*finalAlgorithm) }()

	useStreaming := t.shouldUseStreaming(params, algorithm)

	if useStreaming && algorithm != "" {
		result, headers, err := t.transformStreaming(ctx, input, algorithm)
		duration := time.Since(startTime)
		success := err == nil
		// For streaming mode, byte sizes are recorded as 0 since we process data
		// incrementally without buffering. Duration and success/error counts are
		// still tracked for observability.
		metrics.RecordLambdaCompression(algorithm, "decompress", success, duration, 0, 0)

		return result, headers, err
	}

	return t.transformBufferedWithMetrics(ctx, input, algorithm, startTime, finalAlgorithm)
}

func (t *DecompressTransformer) getAlgorithm(params map[string]interface{}) string {
	if alg, ok := params["algorithm"].(string); ok {
		return strings.ToLower(alg)
	}
	return ""
}

func (t *DecompressTransformer) shouldUseStreaming(params map[string]interface{}, algorithm string) bool {
	contentLength, ok := params["content_length"].(int64)
	if !ok {
		return false
	}

	streamingThreshold := GetStreamingThreshold()
	if contentLength > streamingThreshold {
		log.Debug().
			Int64("content_length", contentLength).
			Int64("streaming_threshold", streamingThreshold).
			Str("algorithm", algorithm).
			Msg("Using streaming decompression for large file")
		return true
	}

	return false
}

func (t *DecompressTransformer) transformBufferedWithMetrics(
	ctx context.Context,
	input io.Reader,
	algorithm string,
	startTime time.Time,
	finalAlgorithm *string,
) (io.Reader, map[string]string, error) {
	maxSize := GetMaxTransformSize()
	limitedReader := io.LimitReader(input, maxSize+1)

	data, err := io.ReadAll(limitedReader)
	if err != nil {
		duration := time.Since(startTime)
		metricsAlg := algorithm
		if metricsAlg == "" {
			metricsAlg = compressionAlgAuto
		}
		metrics.RecordLambdaCompression(metricsAlg, "decompress", false, duration, 0, 0)

		return nil, nil, err
	}

	compressedSize := int64(len(data))

	if compressedSize > maxSize {
		duration := time.Since(startTime)
		metricsAlg := algorithm
		if metricsAlg == "" {
			metricsAlg = compressionAlgAuto
		}
		metrics.RecordLambdaCompression(metricsAlg, "decompress", false, duration, compressedSize, 0)

		return nil, nil, fmt.Errorf("object size exceeds maximum transform size of %d bytes", maxSize)
	}

	// Auto-detect algorithm if not provided
	detectedAlgorithm := algorithm
	if detectedAlgorithm == "" {
		detectedAlgorithm = detectCompressionAlgorithm(data)
		if detectedAlgorithm == "" {
			// Not compressed data, return as-is (no metrics for non-compressed data)
			return bytes.NewReader(data), nil, nil
		}

		// Swap in-flight gauge from "auto" to detected algorithm and update pointer
		// so defer decrements the correct algorithm label
		metrics.DecrementLambdaOperationsInFlight(compressionAlgAuto)
		metrics.IncrementLambdaOperationsInFlight(detectedAlgorithm)
		*finalAlgorithm = detectedAlgorithm
	}

	// Record bytes processed (compressed input)
	metrics.RecordLambdaBytesProcessed(detectedAlgorithm, "decompress", compressedSize)

	result, err := t.decompressData(data, detectedAlgorithm, maxSize)
	if err != nil {
		duration := time.Since(startTime)
		metrics.RecordLambdaCompression(detectedAlgorithm, "decompress", false, duration, compressedSize, 0)

		return nil, nil, err
	}

	decompressedSize := int64(len(result))
	duration := time.Since(startTime)
	metrics.RecordLambdaCompression(detectedAlgorithm, "decompress", true, duration, decompressedSize, compressedSize)

	return bytes.NewReader(result), nil, nil
}

func (t *DecompressTransformer) decompressData(data []byte, algorithm string, maxSize int64) ([]byte, error) {
	switch algorithm {
	case compressionAlgGzip:
		return t.decompressGzip(data, maxSize)
	case compressionAlgZstd:
		return t.decompressZstd(data, maxSize)
	default:
		return nil, fmt.Errorf("unsupported decompression algorithm: %s", algorithm)
	}
}

func (t *DecompressTransformer) decompressGzip(data []byte, maxSize int64) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = reader.Close() }()

	decompressLimit := io.LimitReader(reader, maxSize+1)
	result, err := io.ReadAll(decompressLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress gzip data: %w", err)
	}

	if int64(len(result)) > maxSize {
		return nil, fmt.Errorf("decompressed size exceeds maximum transform size of %d bytes", maxSize)
	}

	return result, nil
}

func (t *DecompressTransformer) decompressZstd(data []byte, maxSize int64) ([]byte, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	defer decoder.Close()

	decompressLimit := io.LimitReader(decoder, maxSize+1)
	result, err := io.ReadAll(decompressLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress zstd data: %w", err)
	}

	if int64(len(result)) > maxSize {
		return nil, fmt.Errorf("decompressed size exceeds maximum transform size of %d bytes", maxSize)
	}

	return result, nil
}

// transformStreaming performs streaming decompression for large files.
// It enforces a maximum decompressed size limit to protect against decompression bombs.
//
//nolint:unparam // headers map[string]string is always nil for decompression (no headers to set), but kept for consistency with compression API
func (t *DecompressTransformer) transformStreaming(ctx context.Context, input io.Reader, algorithm string) (io.Reader, map[string]string, error) {
	pr, pw := io.Pipe()
	maxSize := GetMaxDecompressSize()

	if err := t.startDecompressionStream(ctx, input, pw, algorithm, maxSize); err != nil {
		return nil, nil, err
	}

	log.Debug().
		Str("algorithm", algorithm).
		Int64("max_decompress_size", maxSize).
		Msg("Started streaming decompression with size limit")

	return pr, nil, nil
}

// startDecompressionStream starts a goroutine to decompress input stream.
func (t *DecompressTransformer) startDecompressionStream(ctx context.Context, input io.Reader, pw *io.PipeWriter, algorithm string, maxSize int64) error {
	switch algorithm {
	case compressionAlgGzip:
		t.streamGzipDecompression(ctx, input, pw, maxSize)
		return nil
	case compressionAlgZstd:
		t.streamZstdDecompression(ctx, input, pw, maxSize)
		return nil
	default:
		return fmt.Errorf("unsupported decompression algorithm for streaming: %s", algorithm)
	}
}

// streamGzipDecompression decompresses gzip stream.
func (t *DecompressTransformer) streamGzipDecompression(ctx context.Context, input io.Reader, pw *io.PipeWriter, maxSize int64) {
	go func() {
		defer func() { _ = pw.Close() }()

		reader, err := gzip.NewReader(input)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to create gzip reader: %w", err))
			return
		}
		defer func() { _ = reader.Close() }()

		t.streamDecompress(ctx, reader, pw, maxSize, "gzip")
	}()
}

// streamZstdDecompression decompresses zstd stream.
func (t *DecompressTransformer) streamZstdDecompression(ctx context.Context, input io.Reader, pw *io.PipeWriter, maxSize int64) {
	go func() {
		defer func() { _ = pw.Close() }()

		decoder, err := zstd.NewReader(input)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to create zstd decoder: %w", err))
			return
		}
		defer decoder.Close()

		t.streamDecompress(ctx, decoder, pw, maxSize, "zstd")
	}()
}

// streamDecompress performs the actual streaming decompression with size limits.
func (t *DecompressTransformer) streamDecompress(ctx context.Context, reader io.Reader, pw *io.PipeWriter, maxSize int64, algorithm string) {
	buf := make([]byte, 32*1024) // 32KB buffer for streaming
	var totalWritten int64

	for {
		select {
		case <-ctx.Done():
			pw.CloseWithError(ctx.Err())
			return
		default:
		}

		n, err := reader.Read(buf)
		if n > 0 {
			if err := t.writeDecompressedData(pw, buf[:n], &totalWritten, maxSize); err != nil {
				return
			}
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to decompress %s data: %w", algorithm, err))
			return
		}
	}
}

// writeDecompressedData writes decompressed data with size limit checking.
func (t *DecompressTransformer) writeDecompressedData(pw *io.PipeWriter, data []byte, totalWritten *int64, maxSize int64) error {
	// Check decompression bomb protection before writing
	if *totalWritten+int64(len(data)) > maxSize {
		pw.CloseWithError(fmt.Errorf("%w: limit is %d bytes", ErrDecompressSizeLimitExceeded, maxSize))
		return ErrDecompressSizeLimitExceeded
	}

	_, err := pw.Write(data)
	if err != nil {
		pw.CloseWithError(fmt.Errorf("failed to write decompressed data: %w", err))
		return err
	}

	*totalWritten += int64(len(data))
	return nil
}

// GetAccessPointForBucketConfiguration implements S3 API.
func (s *ObjectLambdaService) GetAccessPointForBucketConfiguration(accessPoint, bucket string) (*AccessPointConfig, error) {
	return s.GetAccessPoint(accessPoint)
}

// CreateAccessPointForObjectLambda implements S3 API.
func (s *ObjectLambdaService) CreateAccessPointForObjectLambda(name string, config *AccessPointConfig) error {
	config.Name = name
	return s.CreateAccessPoint(config)
}

// DeleteAccessPointForObjectLambda implements S3 API.
func (s *ObjectLambdaService) DeleteAccessPointForObjectLambda(name string) error {
	return s.DeleteAccessPoint(name)
}

// GetAccessPointConfigurationForObjectLambda implements S3 API.
func (s *ObjectLambdaService) GetAccessPointConfigurationForObjectLambda(name string) (*AccessPointConfig, error) {
	return s.GetAccessPoint(name)
}

// PutAccessPointConfigurationForObjectLambda implements S3 API.
func (s *ObjectLambdaService) PutAccessPointConfigurationForObjectLambda(name string, config *AccessPointConfig) error {
	config.Name = name
	return s.CreateAccessPoint(config)
}

// ListAccessPointsForObjectLambda implements S3 API.
func (s *ObjectLambdaService) ListAccessPointsForObjectLambda(accountID string) []*AccessPointConfig {
	return s.ListAccessPoints()
}
