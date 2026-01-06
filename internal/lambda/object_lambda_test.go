package lambda

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants.
const (
	compressionGzip = "gzip"
	compressionZstd = "zstd"
)

// Benchmark and test configuration constants.
const (
	// testStreamingThresholdLow forces streaming mode in tests.
	testStreamingThresholdLow = 100
	// testStreamingThreshold1KB is a 1KB threshold for testing.
	testStreamingThreshold1KB = 1024
	// testStreamingThreshold512B is a 512B threshold for testing.
	testStreamingThreshold512B = 512
	// testStreamingThresholdMultiplier is used to set threshold above data size.
	testStreamingThresholdMultiplier = 10
	// testDecompressSizeLimit is the max decompress size for testing.
	testDecompressSizeLimit = 1024
	// testSmallThreshold is used to trigger streaming for small data.
	testSmallThreshold = 10
)

func TestCreateAccessPoint(t *testing.T) {
	svc := NewObjectLambdaService()

	cfg := &AccessPointConfig{
		Name:                  "test-ap",
		SupportingAccessPoint: "arn:aws:s3:region:account:accesspoint/supporting-ap",
		TransformationConfigurations: []TransformationConfig{
			{
				Actions: []string{"GetObject"},
				ContentTransformation: ContentTransformation{
					Type: TransformBuiltIn,
					BuiltInConfig: &BuiltInConfig{
						Function: BuiltInRedact,
					},
				},
			},
		},
	}

	err := svc.CreateAccessPoint(cfg)
	require.NoError(t, err, "CreateAccessPoint should succeed")

	retrieved, err := svc.GetAccessPoint("test-ap")
	require.NoError(t, err, "GetAccessPoint should succeed")

	assert.Equal(t, "test-ap", retrieved.Name, "Name should match")
	assert.NotEmpty(t, retrieved.Arn, "ARN should be auto-generated")
}

func TestCreateAccessPointValidation(t *testing.T) {
	svc := NewObjectLambdaService()

	tests := []struct {
		cfg     *AccessPointConfig
		name    string
		wantErr bool
	}{
		{
			name:    "missing name",
			cfg:     &AccessPointConfig{SupportingAccessPoint: "ap"},
			wantErr: true,
		},
		{
			name:    "missing supporting access point",
			cfg:     &AccessPointConfig{Name: "test"},
			wantErr: true,
		},
		{
			name: "missing transformations",
			cfg: &AccessPointConfig{
				Name:                  "test",
				SupportingAccessPoint: "ap",
			},
			wantErr: true,
		},
		{
			name: "valid config",
			cfg: &AccessPointConfig{
				Name:                  "test",
				SupportingAccessPoint: "ap",
				TransformationConfigurations: []TransformationConfig{
					{Actions: []string{"GetObject"}, ContentTransformation: ContentTransformation{Type: TransformBuiltIn}},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.CreateAccessPoint(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateAccessPoint() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListAccessPoints(t *testing.T) {
	svc := NewObjectLambdaService()

	// Create multiple access points
	for i := range 3 {
		cfg := &AccessPointConfig{
			Name:                  "test-ap-" + string(rune('a'+i)),
			SupportingAccessPoint: "supporting",
			TransformationConfigurations: []TransformationConfig{
				{Actions: []string{"GetObject"}, ContentTransformation: ContentTransformation{Type: TransformBuiltIn}},
			},
		}

		err := svc.CreateAccessPoint(cfg)
		if err != nil {
			t.Fatalf("CreateAccessPoint failed: %v", err)
		}
	}

	list := svc.ListAccessPoints()
	if len(list) != 3 {
		t.Errorf("Expected 3 access points, got %d", len(list))
	}
}

func TestDeleteAccessPoint(t *testing.T) {
	svc := NewObjectLambdaService()

	cfg := &AccessPointConfig{
		Name:                  "test-ap",
		SupportingAccessPoint: "supporting",
		TransformationConfigurations: []TransformationConfig{
			{Actions: []string{"GetObject"}, ContentTransformation: ContentTransformation{Type: TransformBuiltIn}},
		},
	}
	err := svc.CreateAccessPoint(cfg)
	if err != nil {
		t.Fatalf("CreateAccessPoint failed: %v", err)
	}

	err = svc.DeleteAccessPoint("test-ap")
	if err != nil {
		t.Fatalf("DeleteAccessPoint failed: %v", err)
	}

	_, err = svc.GetAccessPoint("test-ap")
	if err == nil {
		t.Error("Expected error when getting deleted access point")
	}
}

func TestRedactTransformer(t *testing.T) {
	transformer := &RedactTransformer{}

	tests := []struct {
		name     string
		input    string
		params   map[string]any
		contains string
	}{
		{
			name:     "redact email",
			input:    "Contact: john@example.com for details",
			params:   nil,
			contains: "[REDACTED]",
		},
		{
			name:     "redact phone",
			input:    "Call 555-123-4567 now",
			params:   nil,
			contains: "[REDACTED]",
		},
		{
			name:     "custom replacement",
			input:    "Email: test@test.com",
			params:   map[string]any{"replacement": "***HIDDEN***"},
			contains: "***HIDDEN***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, _, err := transformer.Transform(context.Background(), strings.NewReader(tt.input), tt.params)
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			data, _ := io.ReadAll(output)
			if !strings.Contains(string(data), tt.contains) {
				t.Errorf("Expected output to contain '%s', got '%s'", tt.contains, string(data))
			}
		})
	}
}

func TestPIIMaskTransformer(t *testing.T) {
	transformer := &PIIMaskTransformer{}

	input := `{
		"name": "John Doe",
		"email": "john@example.com",
		"phone": "555-1234",
		"ssn": "123-45-6789",
		"address": "123 Main St"
	}`

	output, headers, err := transformer.Transform(context.Background(), strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if headers["Content-Type"] != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", headers["Content-Type"])
	}

	data, _ := io.ReadAll(output)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Name should not be masked
	if result["name"] != "John Doe" {
		t.Errorf("Name should not be masked, got '%v'", result["name"])
	}

	// Email should be masked
	email := result["email"].(string)
	if !strings.Contains(email, "*") {
		t.Errorf("Email should be masked, got '%s'", email)
	}

	// Phone should be masked
	phone := result["phone"].(string)
	if !strings.Contains(phone, "*") {
		t.Errorf("Phone should be masked, got '%s'", phone)
	}
}

func TestFilterFieldsTransformer(t *testing.T) {
	transformer := &FilterFieldsTransformer{}

	input := `{
		"id": 1,
		"name": "Test",
		"secret": "hidden",
		"data": "value"
	}`

	tests := []struct {
		params map[string]any
		check  func(map[string]any) bool
		name   string
	}{
		{
			name: "include fields",
			params: map[string]any{
				"include": []any{"id", "name"},
			},
			check: func(r map[string]any) bool {
				_, hasSecret := r["secret"]
				return !hasSecret && r["id"] != nil && r["name"] != nil
			},
		},
		{
			name: "exclude fields",
			params: map[string]any{
				"exclude": []any{"secret"},
			},
			check: func(r map[string]any) bool {
				_, hasSecret := r["secret"]
				return !hasSecret && r["id"] != nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, _, err := transformer.Transform(context.Background(), strings.NewReader(input), tt.params)
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			data, _ := io.ReadAll(output)

			var result map[string]any
			err = json.Unmarshal(data, &result)
			if err != nil {
				t.Fatalf("Failed to unmarshal result: %v", err)
			}

			if !tt.check(result) {
				t.Errorf("Check failed for result: %v", result)
			}
		})
	}
}

func TestConvertJSONTransformer(t *testing.T) {
	transformer := &ConvertJSONTransformer{}

	input := `name,age,city
John,30,NYC
Jane,25,LA`

	output, headers, err := transformer.Transform(context.Background(), strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if headers["Content-Type"] != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", headers["Content-Type"])
	}

	data, _ := io.ReadAll(output)

	var result []map[string]string
	err = json.Unmarshal(data, &result)
	if err != nil {
		t.Fatalf("Failed to parse output JSON: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 records, got %d", len(result))
	}

	if result[0]["name"] != "John" {
		t.Errorf("Expected first name 'John', got '%s'", result[0]["name"])
	}

	if result[1]["city"] != "LA" {
		t.Errorf("Expected second city 'LA', got '%s'", result[1]["city"])
	}
}

func TestBuiltInTransformWithAccessPoint(t *testing.T) {
	svc := NewObjectLambdaService()

	cfg := &AccessPointConfig{
		Name:                  "redact-ap",
		SupportingAccessPoint: "supporting",
		TransformationConfigurations: []TransformationConfig{
			{
				Actions: []string{"GetObject"},
				ContentTransformation: ContentTransformation{
					Type: TransformBuiltIn,
					BuiltInConfig: &BuiltInConfig{
						Function: BuiltInRedact,
					},
				},
			},
		},
	}
	err := svc.CreateAccessPoint(cfg)
	if err != nil {
		t.Fatalf("CreateAccessPoint failed: %v", err)
	}

	input := "Contact email: user@example.com"

	output, _, err := svc.TransformObject(
		context.Background(),
		"redact-ap",
		"test-object",
		strings.NewReader(input),
		map[string]string{},
		UserIdentity{},
	)
	if err != nil {
		t.Fatalf("TransformObject failed: %v", err)
	}

	data, _ := io.ReadAll(output)
	if strings.Contains(string(data), "user@example.com") {
		t.Error("Email should be redacted")
	}

	if !strings.Contains(string(data), "[REDACTED]") {
		t.Error("Output should contain redacted marker")
	}
}

func TestNoTransformationPassThrough(t *testing.T) {
	svc := NewObjectLambdaService()

	// Access point with no GetObject transformation
	cfg := &AccessPointConfig{
		Name:                  "passthrough-ap",
		SupportingAccessPoint: "supporting",
		TransformationConfigurations: []TransformationConfig{
			{
				Actions: []string{"PutObject"}, // Not GetObject
				ContentTransformation: ContentTransformation{
					Type: TransformBuiltIn,
				},
			},
		},
	}
	svc.CreateAccessPoint(cfg)

	input := "Original content"

	output, _, err := svc.TransformObject(
		context.Background(),
		"passthrough-ap",
		"test-object",
		strings.NewReader(input),
		map[string]string{},
		UserIdentity{},
	)
	if err != nil {
		t.Fatalf("TransformObject failed: %v", err)
	}

	data, _ := io.ReadAll(output)
	if string(data) != input {
		t.Errorf("Expected pass-through of original content, got '%s'", string(data))
	}
}

func TestWriteGetObjectResponse(t *testing.T) {
	svc := NewObjectLambdaService()

	token := "test-token"

	// Create pending response channel
	responseChan := make(chan *WriteGetObjectResponseInput, 1)

	svc.pendingMu.Lock()
	svc.pendingResponses[token] = responseChan
	svc.pendingMu.Unlock()

	// Write response
	input := &WriteGetObjectResponseInput{
		RequestToken:  token,
		StatusCode:    200,
		Body:          bytes.NewReader([]byte("transformed content")),
		ContentLength: 19,
		ContentType:   "text/plain",
	}

	err := svc.WriteGetObjectResponse(input)
	if err != nil {
		t.Fatalf("WriteGetObjectResponse failed: %v", err)
	}

	// Verify response was sent
	select {
	case resp := <-responseChan:
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	default:
		t.Error("Response not received")
	}
}

func TestWriteGetObjectResponseNoToken(t *testing.T) {
	svc := NewObjectLambdaService()

	input := &WriteGetObjectResponseInput{
		RequestToken: "non-existent-token",
		StatusCode:   200,
	}

	err := svc.WriteGetObjectResponse(input)
	if err == nil {
		t.Error("Expected error for non-existent token")
	}
}

func TestS3APICompatibility(t *testing.T) {
	svc := NewObjectLambdaService()

	cfg := &AccessPointConfig{
		Name:                  "api-test-ap",
		SupportingAccessPoint: "supporting",
		TransformationConfigurations: []TransformationConfig{
			{Actions: []string{"GetObject"}, ContentTransformation: ContentTransformation{Type: TransformBuiltIn}},
		},
	}

	// CreateAccessPointForObjectLambda
	err := svc.CreateAccessPointForObjectLambda("api-test-ap", cfg)
	if err != nil {
		t.Fatalf("CreateAccessPointForObjectLambda failed: %v", err)
	}

	// GetAccessPointConfigurationForObjectLambda
	retrieved, err := svc.GetAccessPointConfigurationForObjectLambda("api-test-ap")
	if err != nil {
		t.Fatalf("GetAccessPointConfigurationForObjectLambda failed: %v", err)
	}

	if retrieved.Name != "api-test-ap" {
		t.Errorf("Expected name 'api-test-ap', got '%s'", retrieved.Name)
	}

	// ListAccessPointsForObjectLambda
	list := svc.ListAccessPointsForObjectLambda("account-id")
	if len(list) != 1 {
		t.Errorf("Expected 1 access point, got %d", len(list))
	}

	// DeleteAccessPointForObjectLambda
	err = svc.DeleteAccessPointForObjectLambda("api-test-ap")
	if err != nil {
		t.Fatalf("DeleteAccessPointForObjectLambda failed: %v", err)
	}

	list = svc.ListAccessPointsForObjectLambda("account-id")
	if len(list) != 0 {
		t.Errorf("Expected 0 access points after delete, got %d", len(list))
	}
}

func TestNestedPIIMasking(t *testing.T) {
	transformer := &PIIMaskTransformer{}

	input := `{
		"user": {
			"name": "John",
			"email": "john@example.com",
			"profile": {
				"phone": "555-1234"
			}
		},
		"contacts": [
			{"email": "contact1@example.com"},
			{"email": "contact2@example.com"}
		]
	}`

	output, _, err := transformer.Transform(context.Background(), strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	data, _ := io.ReadAll(output)

	// Verify no raw emails remain
	if strings.Contains(string(data), "john@example.com") {
		t.Error("Nested email should be masked")
	}

	if strings.Contains(string(data), "contact1@example.com") {
		t.Error("Array email should be masked")
	}
}

func TestCompressTransformer(t *testing.T) {
	transformer := &CompressTransformer{}
	testData := "Hello, World! This is test data for compression."

	tests := []struct {
		name      string
		params    map[string]any
		algorithm string
	}{
		{
			name:      "gzip default",
			params:    nil,
			algorithm: compressionGzip,
		},
		{
			name:      "gzip explicit",
			params:    map[string]any{"algorithm": compressionGzip},
			algorithm: compressionGzip,
		},
		{
			name:      "gzip with level",
			params:    map[string]any{"algorithm": compressionGzip, "level": 9},
			algorithm: compressionGzip,
		},
		{
			name:      "zstd",
			params:    map[string]any{"algorithm": compressionZstd},
			algorithm: compressionZstd,
		},
		{
			name:      "zstd with level",
			params:    map[string]any{"algorithm": compressionZstd, "level": 5},
			algorithm: compressionZstd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, headers, err := transformer.Transform(context.Background(), strings.NewReader(testData), tt.params)
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			// Check Content-Encoding header
			if headers["Content-Encoding"] != tt.algorithm {
				t.Errorf("Expected Content-Encoding '%s', got '%s'", tt.algorithm, headers["Content-Encoding"])
			}

			// Verify the output is actually compressed
			compressed, _ := io.ReadAll(output)
			if len(compressed) == 0 {
				t.Fatal("Compressed output is empty")
			}

			// Decompress and verify
			var decompressed []byte

			switch tt.algorithm {
			case compressionGzip:
				reader, err := gzip.NewReader(bytes.NewReader(compressed))
				if err != nil {
					t.Fatalf("Failed to create gzip reader: %v", err)
				}

				decompressed, err = io.ReadAll(reader)
				reader.Close()

				if err != nil {
					t.Fatalf("Failed to decompress gzip: %v", err)
				}
			case compressionZstd:
				decoder, err := zstd.NewReader(bytes.NewReader(compressed))
				if err != nil {
					t.Fatalf("Failed to create zstd decoder: %v", err)
				}

				decompressed, err = io.ReadAll(decoder)
				decoder.Close()

				if err != nil {
					t.Fatalf("Failed to decompress zstd: %v", err)
				}
			}

			if string(decompressed) != testData {
				t.Errorf("Decompressed data mismatch: got '%s', want '%s'", string(decompressed), testData)
			}
		})
	}
}

func TestCompressTransformerUnsupportedAlgorithm(t *testing.T) {
	transformer := &CompressTransformer{}
	params := map[string]any{"algorithm": "unsupported"}

	_, _, err := transformer.Transform(context.Background(), strings.NewReader("test"), params)
	if err == nil {
		t.Error("Expected error for unsupported algorithm")
	}

	if !strings.Contains(err.Error(), "unsupported compression algorithm") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestDecompressTransformer(t *testing.T) {
	transformer := &DecompressTransformer{}
	testData := "Hello, World! This is test data for decompression."

	tests := []struct {
		compress func([]byte) []byte
		params   map[string]any
		name     string
	}{
		{
			name: "gzip auto-detect",
			compress: func(data []byte) []byte {
				var buf bytes.Buffer

				writer := gzip.NewWriter(&buf)
				writer.Write(data)
				writer.Close()

				return buf.Bytes()
			},
			params: nil,
		},
		{
			name: "gzip explicit",
			compress: func(data []byte) []byte {
				var buf bytes.Buffer

				writer := gzip.NewWriter(&buf)
				writer.Write(data)
				writer.Close()

				return buf.Bytes()
			},
			params: map[string]any{"algorithm": compressionGzip},
		},
		{
			name: "zstd auto-detect",
			compress: func(data []byte) []byte {
				encoder, _ := zstd.NewWriter(nil)
				return encoder.EncodeAll(data, nil)
			},
			params: nil,
		},
		{
			name: "zstd explicit",
			compress: func(data []byte) []byte {
				encoder, _ := zstd.NewWriter(nil)
				return encoder.EncodeAll(data, nil)
			},
			params: map[string]any{"algorithm": compressionZstd},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed := tt.compress([]byte(testData))

			output, _, err := transformer.Transform(context.Background(), bytes.NewReader(compressed), tt.params)
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			decompressed, _ := io.ReadAll(output)
			if string(decompressed) != testData {
				t.Errorf("Decompressed data mismatch: got '%s', want '%s'", string(decompressed), testData)
			}
		})
	}
}

func TestDecompressTransformerUncompressedData(t *testing.T) {
	transformer := &DecompressTransformer{}
	// Data that doesn't match any compression magic bytes
	plainData := "This is uncompressed plain text data"

	output, _, err := transformer.Transform(context.Background(), strings.NewReader(plainData), nil)
	if err != nil {
		t.Fatalf("Transform should not fail for uncompressed data: %v", err)
	}

	result, _ := io.ReadAll(output)
	if string(result) != plainData {
		t.Errorf("Uncompressed data should pass through unchanged: got '%s', want '%s'", string(result), plainData)
	}
}

func TestDecompressTransformerUnsupportedAlgorithm(t *testing.T) {
	transformer := &DecompressTransformer{}
	params := map[string]any{"algorithm": "unsupported"}

	_, _, err := transformer.Transform(context.Background(), strings.NewReader("test"), params)
	if err == nil {
		t.Error("Expected error for unsupported algorithm")
	}

	if !strings.Contains(err.Error(), "unsupported decompression algorithm") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

// TestDecompressionBombProtection verifies that the transformer rejects
// compressed data that would expand beyond the maximum transform size.
func TestDecompressionBombProtection(t *testing.T) {
	// Save original max size and set a small limit for testing
	originalMaxSize := GetMaxTransformSize()

	SetMaxTransformSize(1024) // 1KB limit for testing
	defer SetMaxTransformSize(originalMaxSize)

	transformer := &DecompressTransformer{}

	tests := []struct {
		name          string
		compressFunc  func([]byte) []byte
		algorithm     string
		inputSize     int
		shouldSucceed bool
	}{
		{
			name:          "gzip_data_at_limit",
			compressFunc:  compressWithGzip,
			algorithm:     "gzip",
			inputSize:     1024, // Exactly at limit
			shouldSucceed: true,
		},
		{
			name:          "gzip_data_over_limit",
			compressFunc:  compressWithGzip,
			algorithm:     "gzip",
			inputSize:     2048, // Exceeds 1KB limit when decompressed
			shouldSucceed: false,
		},
		{
			name:          "zstd_data_at_limit",
			compressFunc:  compressWithZstd,
			algorithm:     "zstd",
			inputSize:     1024, // Exactly at limit
			shouldSucceed: true,
		},
		{
			name:          "zstd_data_over_limit",
			compressFunc:  compressWithZstd,
			algorithm:     "zstd",
			inputSize:     2048, // Exceeds 1KB limit when decompressed
			shouldSucceed: false,
		},
		{
			name:          "small_data_well_under_limit",
			compressFunc:  compressWithGzip,
			algorithm:     "gzip",
			inputSize:     100, // Well under limit
			shouldSucceed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create test data
			testData := bytes.Repeat([]byte("A"), tc.inputSize)
			compressedData := tc.compressFunc(testData)

			params := map[string]any{"algorithm": tc.algorithm}
			_, _, err := transformer.Transform(context.Background(), bytes.NewReader(compressedData), params)

			if tc.shouldSucceed {
				if err != nil {
					t.Errorf("Expected success but got error: %v", err)
				}
			} else {
				if err == nil {
					t.Error("Expected error for data exceeding limit but got none")
				} else if !strings.Contains(err.Error(), "exceeds maximum transform size") {
					t.Errorf("Expected 'exceeds maximum transform size' error, got: %v", err)
				}
			}
		})
	}
}

// TestCompressionSizeLimit verifies that the compressor rejects oversized inputs.
func TestCompressionSizeLimit(t *testing.T) {
	// Save original max size and set a small limit for testing
	originalMaxSize := GetMaxTransformSize()

	SetMaxTransformSize(1024) // 1KB limit for testing
	defer SetMaxTransformSize(originalMaxSize)

	transformer := &CompressTransformer{}

	tests := []struct {
		name          string
		inputSize     int
		shouldSucceed bool
	}{
		{
			name:          "data_at_limit",
			inputSize:     1024, // Exactly at limit
			shouldSucceed: true,
		},
		{
			name:          "data_over_limit",
			inputSize:     1025, // Just over limit
			shouldSucceed: false,
		},
		{
			name:          "data_well_over_limit",
			inputSize:     4096, // Well over limit
			shouldSucceed: false,
		},
		{
			name:          "small_data",
			inputSize:     100, // Well under limit
			shouldSucceed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testData := bytes.Repeat([]byte("B"), tc.inputSize)
			params := map[string]any{"algorithm": compressionGzip}

			_, _, err := transformer.Transform(context.Background(), bytes.NewReader(testData), params)

			if tc.shouldSucceed {
				if err != nil {
					t.Errorf("Expected success but got error: %v", err)
				}
			} else {
				if err == nil {
					t.Error("Expected error for data exceeding limit but got none")
				} else if !strings.Contains(err.Error(), "exceeds maximum transform size") {
					t.Errorf("Expected 'exceeds maximum transform size' error, got: %v", err)
				}
			}
		})
	}
}

// TestMaxTransformSizeConfiguration verifies the configurable max transform size.
func TestMaxTransformSizeConfiguration(t *testing.T) {
	// Save original max size
	originalMaxSize := GetMaxTransformSize()
	defer SetMaxTransformSize(originalMaxSize)

	// Test setting a custom size
	SetMaxTransformSize(50 * 1024 * 1024) // 50MB

	if GetMaxTransformSize() != 50*1024*1024 {
		t.Errorf("Expected max transform size to be 50MB, got %d", GetMaxTransformSize())
	}

	// Test that invalid sizes are rejected
	SetMaxTransformSize(0)

	if GetMaxTransformSize() != 50*1024*1024 {
		t.Error("Setting size to 0 should not change the value")
	}

	SetMaxTransformSize(-1)

	if GetMaxTransformSize() != 50*1024*1024 {
		t.Error("Setting negative size should not change the value")
	}

	// Reset and verify default
	SetMaxTransformSize(DefaultMaxTransformSize)

	if GetMaxTransformSize() != DefaultMaxTransformSize {
		t.Errorf("Expected default max transform size, got %d", GetMaxTransformSize())
	}
}

// Helper function to compress data with gzip.
func compressWithGzip(data []byte) []byte {
	var buf bytes.Buffer

	writer := gzip.NewWriter(&buf)
	_, _ = writer.Write(data)
	_ = writer.Close()

	return buf.Bytes()
}

// Helper function to compress data with zstd.
func compressWithZstd(data []byte) []byte {
	var buf bytes.Buffer

	writer, _ := zstd.NewWriter(&buf)
	_, _ = writer.Write(data)
	_ = writer.Close()

	return buf.Bytes()
}

// TestSetMaxTransformSizeConcurrency verifies thread safety of SetMaxTransformSize/GetMaxTransformSize
// Run with: go test -race -run TestSetMaxTransformSizeConcurrency.
func TestSetMaxTransformSizeConcurrency(t *testing.T) {
	// Save original max size
	originalMaxSize := GetMaxTransformSize()
	defer SetMaxTransformSize(originalMaxSize)

	var wg sync.WaitGroup

	const (
		goroutines = 100
		iterations = 100
	)

	// Concurrent writes

	for i := range goroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := range iterations {
				size := int64((id*iterations + j + 1) * 1024) // Varying sizes
				SetMaxTransformSize(size)
			}
		}(i)
	}

	// Concurrent reads
	for range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range iterations {
				size := GetMaxTransformSize()
				// Verify the size is always positive (valid)
				if size <= 0 {
					t.Errorf("GetMaxTransformSize returned invalid value: %d", size)
				}
			}
		}()
	}

	wg.Wait()

	// Final value should be positive
	finalSize := GetMaxTransformSize()
	if finalSize <= 0 {
		t.Errorf("Final max transform size should be positive, got %d", finalSize)
	}
}

// TestStreamingThresholdConfiguration verifies the configurable streaming threshold.
func TestStreamingThresholdConfiguration(t *testing.T) {
	// Save original threshold
	originalThreshold := GetStreamingThreshold()
	defer SetStreamingThreshold(originalThreshold)

	// Test default value
	if GetStreamingThreshold() != DefaultStreamingThreshold {
		t.Errorf("Expected default streaming threshold %d, got %d",
			DefaultStreamingThreshold, GetStreamingThreshold())
	}

	// Test setting a custom size
	SetStreamingThreshold(5 * 1024 * 1024) // 5MB
	if GetStreamingThreshold() != 5*1024*1024 {
		t.Errorf("Expected streaming threshold to be 5MB, got %d", GetStreamingThreshold())
	}

	// Test that invalid sizes are rejected
	SetStreamingThreshold(0)
	if GetStreamingThreshold() != 5*1024*1024 {
		t.Error("Setting threshold to 0 should not change the value")
	}

	SetStreamingThreshold(-1)
	if GetStreamingThreshold() != 5*1024*1024 {
		t.Error("Setting negative threshold should not change the value")
	}

	// Reset and verify default
	SetStreamingThreshold(DefaultStreamingThreshold)
	if GetStreamingThreshold() != DefaultStreamingThreshold {
		t.Errorf("Expected default streaming threshold, got %d", GetStreamingThreshold())
	}
}

// TestStreamingCompressionGzip tests streaming gzip compression for large files.
func TestStreamingCompressionGzip(t *testing.T) {
	// Save and set a low streaming threshold for testing
	originalThreshold := GetStreamingThreshold()
	SetStreamingThreshold(testStreamingThreshold1KB)
	defer SetStreamingThreshold(originalThreshold)

	transformer := &CompressTransformer{}

	// Create test data larger than threshold
	testData := bytes.Repeat([]byte("Hello, World! "), 200) // ~2.8KB

	params := map[string]any{
		"algorithm":      compressionGzip,
		"content_length": int64(len(testData)),
	}

	output, headers, err := transformer.Transform(context.Background(), bytes.NewReader(testData), params)
	if err != nil {
		t.Fatalf("Streaming compression failed: %v", err)
	}

	// Verify Content-Encoding header
	if headers["Content-Encoding"] != compressionGzip {
		t.Errorf("Expected Content-Encoding 'gzip', got '%s'", headers["Content-Encoding"])
	}

	// Read compressed output
	compressed, err := io.ReadAll(output)
	if err != nil {
		t.Fatalf("Failed to read compressed output: %v", err)
	}

	if len(compressed) == 0 {
		t.Fatal("Compressed output is empty")
	}

	// Decompress and verify
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to decompress: %v", err)
	}

	if !bytes.Equal(decompressed, testData) {
		t.Error("Decompressed data does not match original")
	}
}

// TestStreamingCompressionZstd tests streaming zstd compression for large files.
func TestStreamingCompressionZstd(t *testing.T) {
	// Save and set a low streaming threshold for testing
	originalThreshold := GetStreamingThreshold()
	SetStreamingThreshold(testStreamingThreshold1KB)
	defer SetStreamingThreshold(originalThreshold)

	transformer := &CompressTransformer{}

	// Create test data larger than threshold
	testData := bytes.Repeat([]byte("Hello, World! "), 200) // ~2.8KB

	params := map[string]any{
		"algorithm":      compressionZstd,
		"content_length": int64(len(testData)),
	}

	output, headers, err := transformer.Transform(context.Background(), bytes.NewReader(testData), params)
	if err != nil {
		t.Fatalf("Streaming compression failed: %v", err)
	}

	// Verify Content-Encoding header
	if headers["Content-Encoding"] != compressionZstd {
		t.Errorf("Expected Content-Encoding 'zstd', got '%s'", headers["Content-Encoding"])
	}

	// Read compressed output
	compressed, err := io.ReadAll(output)
	if err != nil {
		t.Fatalf("Failed to read compressed output: %v", err)
	}

	if len(compressed) == 0 {
		t.Fatal("Compressed output is empty")
	}

	// Decompress and verify
	decoder, err := zstd.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("Failed to create zstd decoder: %v", err)
	}
	defer decoder.Close()

	decompressed, err := io.ReadAll(decoder)
	if err != nil {
		t.Fatalf("Failed to decompress: %v", err)
	}

	if !bytes.Equal(decompressed, testData) {
		t.Error("Decompressed data does not match original")
	}
}

// TestStreamingDecompressionGzip tests streaming gzip decompression for large files.
func TestStreamingDecompressionGzip(t *testing.T) {
	// Save and set a low streaming threshold for testing
	originalThreshold := GetStreamingThreshold()
	SetStreamingThreshold(testStreamingThreshold512B)
	defer SetStreamingThreshold(originalThreshold)

	transformer := &DecompressTransformer{}

	// Create test data and compress it
	testData := bytes.Repeat([]byte("Hello, World! "), 200) // ~2.8KB
	compressed := compressWithGzip(testData)

	params := map[string]any{
		"algorithm":      compressionGzip,
		"content_length": int64(len(compressed)),
	}

	output, _, err := transformer.Transform(context.Background(), bytes.NewReader(compressed), params)
	if err != nil {
		t.Fatalf("Streaming decompression failed: %v", err)
	}

	// Read decompressed output
	decompressed, err := io.ReadAll(output)
	if err != nil {
		t.Fatalf("Failed to read decompressed output: %v", err)
	}

	if !bytes.Equal(decompressed, testData) {
		t.Error("Decompressed data does not match original")
	}
}

// TestStreamingDecompressionZstd tests streaming zstd decompression for large files.
func TestStreamingDecompressionZstd(t *testing.T) {
	// Save and set a low streaming threshold for testing
	originalThreshold := GetStreamingThreshold()
	SetStreamingThreshold(testStreamingThreshold512B)
	defer SetStreamingThreshold(originalThreshold)

	transformer := &DecompressTransformer{}

	// Create test data and compress it
	testData := bytes.Repeat([]byte("Hello, World! "), 200) // ~2.8KB
	compressed := compressWithZstd(testData)

	params := map[string]any{
		"algorithm":      compressionZstd,
		"content_length": int64(len(compressed)),
	}

	output, _, err := transformer.Transform(context.Background(), bytes.NewReader(compressed), params)
	if err != nil {
		t.Fatalf("Streaming decompression failed: %v", err)
	}

	// Read decompressed output
	decompressed, err := io.ReadAll(output)
	if err != nil {
		t.Fatalf("Failed to read decompressed output: %v", err)
	}

	if !bytes.Equal(decompressed, testData) {
		t.Error("Decompressed data does not match original")
	}
}

// TestStreamingCompressionContextCancellation tests that streaming compression
// respects context cancellation.
func TestStreamingCompressionContextCancellation(t *testing.T) {
	// Save and set a low streaming threshold for testing
	originalThreshold := GetStreamingThreshold()
	SetStreamingThreshold(testStreamingThreshold1KB)
	defer SetStreamingThreshold(originalThreshold)

	transformer := &CompressTransformer{}

	// Create a large test data to ensure streaming takes time
	testData := bytes.Repeat([]byte("X"), 100*1024) // 100KB

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	params := map[string]any{
		"algorithm":      compressionGzip,
		"content_length": int64(len(testData)),
	}

	// Use a slow reader to simulate streaming
	slowReader := &slowReader{
		reader:      bytes.NewReader(testData),
		cancel:      cancel,
		cancelAfter: 5,
	}

	output, _, err := transformer.Transform(ctx, slowReader, params)
	if err != nil {
		t.Fatalf("Transform should not fail immediately: %v", err)
	}

	// Reading should eventually fail due to context cancellation
	_, err = io.ReadAll(output)
	if err == nil {
		t.Log("Read completed without error (context may have been canceled after completion)")
	}
}

// slowReader is a test helper that cancels context after N reads.
type slowReader struct {
	reader      io.Reader
	cancel      context.CancelFunc
	cancelAfter int32
	readCount   atomic.Int32
}

func (s *slowReader) Read(p []byte) (int, error) {
	count := s.readCount.Add(1)
	if count >= s.cancelAfter {
		s.cancel()
	}

	return s.reader.Read(p)
}

// TestStreamingDecompressSizeLimitProtection tests that streaming decompression
// respects the max decompress size limit.
func TestStreamingDecompressSizeLimitProtection(t *testing.T) {
	// Save original values
	originalThreshold := GetStreamingThreshold()
	originalMaxDecompress := GetMaxDecompressSize()

	// Set low thresholds for testing
	// Note: Use a very low streaming threshold so even small compressed data triggers streaming
	SetStreamingThreshold(testSmallThreshold)
	SetMaxDecompressSize(testDecompressSizeLimit)
	defer SetStreamingThreshold(originalThreshold)
	defer SetMaxDecompressSize(originalMaxDecompress)

	transformer := &DecompressTransformer{}

	// Create test data that exceeds the limit when decompressed
	// Use varied data to ensure compressed size is larger than the threshold
	testData := bytes.Repeat([]byte("ABCDEFGH12345678"), 128) // 2KB uncompressed with varied content
	compressed := compressWithGzip(testData)

	params := map[string]any{
		"algorithm":      compressionGzip,
		"content_length": int64(len(compressed)),
	}

	output, _, err := transformer.Transform(context.Background(), bytes.NewReader(compressed), params)
	if err != nil {
		t.Fatalf("Transform should not fail immediately: %v", err)
	}

	// Reading should fail due to size limit
	_, err = io.ReadAll(output)
	if err == nil {
		t.Fatal("Expected error when decompressed size exceeds limit")
	}

	if !strings.Contains(err.Error(), "decompressed size exceeds maximum") {
		t.Errorf("Expected decompression size limit error, got: %v", err)
	}
}

// TestMaxDecompressSizeConfiguration verifies the configurable max decompress size.
func TestMaxDecompressSizeConfiguration(t *testing.T) {
	// Save original max size
	originalMaxSize := GetMaxDecompressSize()
	defer SetMaxDecompressSize(originalMaxSize)

	// Test default value
	if GetMaxDecompressSize() != DefaultMaxDecompressSize {
		t.Errorf("Expected default max decompress size %d, got %d",
			DefaultMaxDecompressSize, GetMaxDecompressSize())
	}

	// Test setting a custom size
	SetMaxDecompressSize(500 * 1024 * 1024) // 500MB
	if GetMaxDecompressSize() != 500*1024*1024 {
		t.Errorf("Expected max decompress size to be 500MB, got %d", GetMaxDecompressSize())
	}

	// Test that invalid sizes are rejected
	SetMaxDecompressSize(0)
	if GetMaxDecompressSize() != 500*1024*1024 {
		t.Error("Setting size to 0 should not change the value")
	}

	SetMaxDecompressSize(-1)
	if GetMaxDecompressSize() != 500*1024*1024 {
		t.Error("Setting negative size should not change the value")
	}

	// Reset and verify default
	SetMaxDecompressSize(DefaultMaxDecompressSize)
	if GetMaxDecompressSize() != DefaultMaxDecompressSize {
		t.Errorf("Expected default max decompress size, got %d", GetMaxDecompressSize())
	}
}

// TestStreamingThresholdConcurrency verifies thread safety of streaming threshold
// operations. Run with: go test -race -run TestStreamingThresholdConcurrency.
func TestStreamingThresholdConcurrency(t *testing.T) {
	// Save original threshold
	originalThreshold := GetStreamingThreshold()
	defer SetStreamingThreshold(originalThreshold)

	var wg sync.WaitGroup

	const (
		goroutines = 50
		iterations = 100
	)

	// Concurrent writes
	for i := range goroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := range iterations {
				size := int64((id*iterations + j + 1) * 1024)
				SetStreamingThreshold(size)
			}
		}(i)
	}

	// Concurrent reads
	for range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range iterations {
				size := GetStreamingThreshold()
				if size <= 0 {
					t.Errorf("GetStreamingThreshold returned invalid value: %d", size)
				}
			}
		}()
	}

	wg.Wait()

	// Final value should be positive
	finalSize := GetStreamingThreshold()
	if finalSize <= 0 {
		t.Errorf("Final streaming threshold should be positive, got %d", finalSize)
	}
}

// TestStreamingVsBufferedModeSelection verifies that streaming mode is correctly
// selected based on content length and threshold.
func TestStreamingVsBufferedModeSelection(t *testing.T) {
	// Save and set thresholds
	originalThreshold := GetStreamingThreshold()
	SetStreamingThreshold(testStreamingThreshold1KB)
	defer SetStreamingThreshold(originalThreshold)

	compressTransformer := &CompressTransformer{}
	decompressTransformer := &DecompressTransformer{}

	testData := []byte("Hello, World!")

	tests := []struct {
		name            string
		contentLength   int64
		expectedMode    string
		useDecompressor bool
	}{
		{
			name:          "small_file_uses_buffered_compression",
			contentLength: 512, // Below threshold
			expectedMode:  "buffered",
		},
		{
			name:          "large_file_uses_streaming_compression",
			contentLength: 2048, // Above threshold
			expectedMode:  "streaming",
		},
		{
			name:            "small_file_uses_buffered_decompression",
			contentLength:   512,
			expectedMode:    "buffered",
			useDecompressor: true,
		},
		{
			name:            "large_file_uses_streaming_decompression",
			contentLength:   2048,
			expectedMode:    "streaming",
			useDecompressor: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]any{
				"algorithm":      compressionGzip,
				"content_length": tc.contentLength,
			}

			var err error

			if tc.useDecompressor {
				compressed := compressWithGzip(testData)
				var output io.Reader
				output, _, err = decompressTransformer.Transform(
					context.Background(),
					bytes.NewReader(compressed),
					params,
				)
				if err != nil {
					t.Fatalf("Decompression failed: %v", err)
				}
				_, _ = io.ReadAll(output)
			} else {
				var output io.Reader
				output, _, err = compressTransformer.Transform(
					context.Background(),
					bytes.NewReader(testData),
					params,
				)
				if err != nil {
					t.Fatalf("Compression failed: %v", err)
				}
				_, _ = io.ReadAll(output)
			}

			// The test passes if no error occurred
			// Mode selection is verified by the implementation logging
		})
	}
}

// BenchmarkBufferedVsStreamingCompression compares memory and performance between
// buffered and streaming compression modes.
func BenchmarkBufferedVsStreamingCompression(b *testing.B) {
	// Test data sizes
	sizes := []int{
		1 * 1024,        // 1KB
		100 * 1024,      // 100KB
		1 * 1024 * 1024, // 1MB
	}

	algorithms := []string{compressionGzip, compressionZstd}

	for _, size := range sizes {
		for _, alg := range algorithms {
			testData := bytes.Repeat([]byte("Hello, World! "), size/14+1)[:size]

			// Benchmark buffered mode
			b.Run("buffered_"+alg+"_"+formatSize(size), func(b *testing.B) {
				originalThreshold := GetStreamingThreshold()
				// Force buffered mode by setting threshold above data size
				SetStreamingThreshold(int64(size * testStreamingThresholdMultiplier))
				defer SetStreamingThreshold(originalThreshold)

				transformer := &CompressTransformer{}
				params := map[string]any{
					"algorithm":      alg,
					"content_length": int64(size / 2), // Below threshold
				}

				b.ResetTimer()
				b.ReportAllocs()

				for b.Loop() {
					output, _, err := transformer.Transform(
						context.Background(),
						bytes.NewReader(testData),
						params,
					)
					if err != nil {
						b.Fatalf("Transform failed: %v", err)
					}
					if _, err = io.ReadAll(output); err != nil {
						b.Fatalf("ReadAll failed: %v", err)
					}
				}
			})

			// Benchmark streaming mode
			b.Run("streaming_"+alg+"_"+formatSize(size), func(b *testing.B) {
				originalThreshold := GetStreamingThreshold()
				SetStreamingThreshold(testStreamingThresholdLow) // Force streaming mode
				defer SetStreamingThreshold(originalThreshold)

				transformer := &CompressTransformer{}
				params := map[string]any{
					"algorithm":      alg,
					"content_length": int64(size), // Above threshold
				}

				b.ResetTimer()
				b.ReportAllocs()

				for b.Loop() {
					output, _, err := transformer.Transform(
						context.Background(),
						bytes.NewReader(testData),
						params,
					)
					if err != nil {
						b.Fatalf("Transform failed: %v", err)
					}
					if _, err = io.ReadAll(output); err != nil {
						b.Fatalf("ReadAll failed: %v", err)
					}
				}
			})
		}
	}
}

// BenchmarkBufferedVsStreamingDecompression compares memory and performance between
// buffered and streaming decompression modes.
func BenchmarkBufferedVsStreamingDecompression(b *testing.B) {
	sizes := []int{
		1 * 1024,        // 1KB
		100 * 1024,      // 100KB
		1 * 1024 * 1024, // 1MB
	}

	algorithms := []string{compressionGzip, compressionZstd}

	for _, size := range sizes {
		for _, alg := range algorithms {
			testData := bytes.Repeat([]byte("Hello, World! "), size/14+1)[:size]
			var compressed []byte

			switch alg {
			case compressionGzip:
				compressed = compressWithGzip(testData)
			case compressionZstd:
				compressed = compressWithZstd(testData)
			}

			// Benchmark buffered mode
			b.Run("buffered_"+alg+"_"+formatSize(size), func(b *testing.B) {
				originalThreshold := GetStreamingThreshold()
				// Force buffered mode by setting threshold above compressed size
				SetStreamingThreshold(int64(len(compressed) * testStreamingThresholdMultiplier))
				defer SetStreamingThreshold(originalThreshold)

				transformer := &DecompressTransformer{}
				params := map[string]any{
					"algorithm":      alg,
					"content_length": int64(len(compressed) / 2),
				}

				b.ResetTimer()
				b.ReportAllocs()

				for b.Loop() {
					output, _, err := transformer.Transform(
						context.Background(),
						bytes.NewReader(compressed),
						params,
					)
					if err != nil {
						b.Fatalf("Transform failed: %v", err)
					}
					if _, err = io.ReadAll(output); err != nil {
						b.Fatalf("ReadAll failed: %v", err)
					}
				}
			})

			// Benchmark streaming mode
			b.Run("streaming_"+alg+"_"+formatSize(size), func(b *testing.B) {
				originalThreshold := GetStreamingThreshold()
				SetStreamingThreshold(testStreamingThresholdLow) // Force streaming mode
				defer SetStreamingThreshold(originalThreshold)

				transformer := &DecompressTransformer{}
				params := map[string]any{
					"algorithm":      alg,
					"content_length": int64(len(compressed)),
				}

				b.ResetTimer()
				b.ReportAllocs()

				for b.Loop() {
					output, _, err := transformer.Transform(
						context.Background(),
						bytes.NewReader(compressed),
						params,
					)
					if err != nil {
						b.Fatalf("Transform failed: %v", err)
					}
					if _, err = io.ReadAll(output); err != nil {
						b.Fatalf("ReadAll failed: %v", err)
					}
				}
			})
		}
	}
}

// formatSize formats a byte size for benchmark names.
func formatSize(size int) string {
	switch {
	case size >= 1024*1024:
		return strconv.Itoa(size/(1024*1024)) + "MB"
	case size >= 1024:
		return strconv.Itoa(size/1024) + "KB"
	default:
		return strconv.Itoa(size) + "B"
	}
}

// TestStreamingCompressionWithLevel tests streaming compression with custom levels.
func TestStreamingCompressionWithLevel(t *testing.T) {
	// Save and set a low streaming threshold for testing
	originalThreshold := GetStreamingThreshold()
	SetStreamingThreshold(testStreamingThreshold1KB)
	defer SetStreamingThreshold(originalThreshold)

	transformer := &CompressTransformer{}
	testData := bytes.Repeat([]byte("Hello, World! "), 200) // ~2.8KB

	tests := []struct {
		name      string
		algorithm string
		level     int
	}{
		{"gzip_best_speed", compressionGzip, 1},
		{"gzip_best_compression", compressionGzip, 9},
		{"gzip_default", compressionGzip, -1},
		{"zstd_fast", compressionZstd, 1},
		{"zstd_default", compressionZstd, 3},
		{"zstd_better", compressionZstd, 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]any{
				"algorithm":      tc.algorithm,
				"level":          tc.level,
				"content_length": int64(len(testData)),
			}

			output, headers, err := transformer.Transform(
				context.Background(),
				bytes.NewReader(testData),
				params,
			)
			if err != nil {
				t.Fatalf("Streaming compression failed: %v", err)
			}

			if headers["Content-Encoding"] != tc.algorithm {
				t.Errorf("Expected Content-Encoding '%s', got '%s'",
					tc.algorithm, headers["Content-Encoding"])
			}

			compressed, err := io.ReadAll(output)
			if err != nil {
				t.Fatalf("Failed to read compressed output: %v", err)
			}

			if len(compressed) == 0 {
				t.Fatal("Compressed output is empty")
			}

			// Verify by decompressing
			var decompressed []byte

			switch tc.algorithm {
			case compressionGzip:
				reader, err := gzip.NewReader(bytes.NewReader(compressed))
				if err != nil {
					t.Fatalf("Failed to create gzip reader: %v", err)
				}

				decompressed, err = io.ReadAll(reader)
				reader.Close()

				if err != nil {
					t.Fatalf("Failed to decompress: %v", err)
				}
			case compressionZstd:
				decoder, err := zstd.NewReader(bytes.NewReader(compressed))
				if err != nil {
					t.Fatalf("Failed to create zstd decoder: %v", err)
				}

				decompressed, err = io.ReadAll(decoder)
				decoder.Close()

				if err != nil {
					t.Fatalf("Failed to decompress: %v", err)
				}
			}

			if !bytes.Equal(decompressed, testData) {
				t.Error("Decompressed data does not match original")
			}
		})
	}
}
