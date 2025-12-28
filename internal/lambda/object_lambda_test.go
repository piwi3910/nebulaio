package lambda

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
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
	if err != nil {
		t.Fatalf("CreateAccessPoint failed: %v", err)
	}

	retrieved, err := svc.GetAccessPoint("test-ap")
	if err != nil {
		t.Fatalf("GetAccessPoint failed: %v", err)
	}

	if retrieved.Name != "test-ap" {
		t.Errorf("Expected name 'test-ap', got '%s'", retrieved.Name)
	}

	if retrieved.Arn == "" {
		t.Error("ARN should be auto-generated")
	}
}

func TestCreateAccessPointValidation(t *testing.T) {
	svc := NewObjectLambdaService()

	tests := []struct {
		name    string
		cfg     *AccessPointConfig
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
	for i := 0; i < 3; i++ {
		cfg := &AccessPointConfig{
			Name:                  "test-ap-" + string(rune('a'+i)),
			SupportingAccessPoint: "supporting",
			TransformationConfigurations: []TransformationConfig{
				{Actions: []string{"GetObject"}, ContentTransformation: ContentTransformation{Type: TransformBuiltIn}},
			},
		}
		if err := svc.CreateAccessPoint(cfg); err != nil {
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
	if err := svc.CreateAccessPoint(cfg); err != nil {
		t.Fatalf("CreateAccessPoint failed: %v", err)
	}

	err := svc.DeleteAccessPoint("test-ap")
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
		params   map[string]interface{}
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
			params:   map[string]interface{}{"replacement": "***HIDDEN***"},
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
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
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
		name   string
		params map[string]interface{}
		check  func(map[string]interface{}) bool
	}{
		{
			name: "include fields",
			params: map[string]interface{}{
				"include": []interface{}{"id", "name"},
			},
			check: func(r map[string]interface{}) bool {
				_, hasSecret := r["secret"]
				return !hasSecret && r["id"] != nil && r["name"] != nil
			},
		},
		{
			name: "exclude fields",
			params: map[string]interface{}{
				"exclude": []interface{}{"secret"},
			},
			check: func(r map[string]interface{}) bool {
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
			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
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
	if err := json.Unmarshal(data, &result); err != nil {
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
	if err := svc.CreateAccessPoint(cfg); err != nil {
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
		if resp.StatusCode != 200 {
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
		params    map[string]interface{}
		algorithm string
	}{
		{
			name:      "gzip default",
			params:    nil,
			algorithm: "gzip",
		},
		{
			name:      "gzip explicit",
			params:    map[string]interface{}{"algorithm": "gzip"},
			algorithm: "gzip",
		},
		{
			name:      "gzip with level",
			params:    map[string]interface{}{"algorithm": "gzip", "level": 9},
			algorithm: "gzip",
		},
		{
			name:      "zstd",
			params:    map[string]interface{}{"algorithm": "zstd"},
			algorithm: "zstd",
		},
		{
			name:      "zstd with level",
			params:    map[string]interface{}{"algorithm": "zstd", "level": 5},
			algorithm: "zstd",
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
			case "gzip":
				reader, err := gzip.NewReader(bytes.NewReader(compressed))
				if err != nil {
					t.Fatalf("Failed to create gzip reader: %v", err)
				}
				decompressed, err = io.ReadAll(reader)
				reader.Close()
				if err != nil {
					t.Fatalf("Failed to decompress gzip: %v", err)
				}
			case "zstd":
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
	params := map[string]interface{}{"algorithm": "unsupported"}

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
		name      string
		compress  func([]byte) []byte
		params    map[string]interface{}
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
			params: map[string]interface{}{"algorithm": "gzip"},
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
			params: map[string]interface{}{"algorithm": "zstd"},
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
	params := map[string]interface{}{"algorithm": "unsupported"}

	_, _, err := transformer.Transform(context.Background(), strings.NewReader("test"), params)
	if err == nil {
		t.Error("Expected error for unsupported algorithm")
	}
	if !strings.Contains(err.Error(), "unsupported decompression algorithm") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

// TestDecompressionBombProtection verifies that the transformer rejects
// compressed data that would expand beyond the maximum transform size
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

			params := map[string]interface{}{"algorithm": tc.algorithm}
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

// TestCompressionSizeLimit verifies that the compressor rejects oversized inputs
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
			params := map[string]interface{}{"algorithm": "gzip"}

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

// TestMaxTransformSizeConfiguration verifies the configurable max transform size
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

// Helper function to compress data with gzip
func compressWithGzip(data []byte) []byte {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, _ = writer.Write(data)
	_ = writer.Close()
	return buf.Bytes()
}

// Helper function to compress data with zstd
func compressWithZstd(data []byte) []byte {
	var buf bytes.Buffer
	writer, _ := zstd.NewWriter(&buf)
	_, _ = writer.Write(data)
	_ = writer.Close()
	return buf.Bytes()
}
