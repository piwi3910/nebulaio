package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCanonicalURI(t *testing.T) {
	gen := NewPresignedURLGenerator("us-east-1", "http://localhost:9000")

	tests := []struct {
		name     string
		endpoint string
		bucket   string
		key      string
		expected string
	}{
		{
			name:     "path-style with bucket and key",
			endpoint: "http://localhost:9000",
			bucket:   "test-bucket",
			key:      "test-key",
			expected: "/test-bucket/test-key",
		},
		{
			name:     "path-style with bucket only",
			endpoint: "http://localhost:9000",
			bucket:   "test-bucket",
			key:      "",
			expected: "/test-bucket",
		},
		{
			name:     "virtual-hosted with key",
			endpoint: "",
			bucket:   "test-bucket",
			key:      "test-key",
			expected: "/test-key",
		},
		{
			name:     "virtual-hosted without key",
			endpoint: "",
			bucket:   "test-bucket",
			key:      "",
			expected: "/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := gen.buildCanonicalURI(tc.endpoint, tc.bucket, tc.key)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGeneratePresignedURL(t *testing.T) {
	gen := NewPresignedURLGenerator("us-east-1", "http://localhost:9000")

	presignedURL, err := gen.GeneratePresignedURL(PresignParams{
		Method:      http.MethodGet,
		Bucket:      "test-bucket",
		Key:         "test-key",
		AccessKeyID: "AKIAIOSFODNN7EXAMPLE",
		SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region:      "us-east-1",
		Endpoint:    "http://localhost:9000",
		Expiration:  15 * time.Minute,
	})

	require.NoError(t, err)
	t.Logf("Generated URL: %s", presignedURL)

	parsedURL, err := url.Parse(presignedURL)
	require.NoError(t, err)

	assert.Equal(t, "localhost:9000", parsedURL.Host)
	assert.Equal(t, "/test-bucket/test-key", parsedURL.Path)
	assert.NotEmpty(t, parsedURL.Query().Get("X-Amz-Signature"))
	assert.NotEmpty(t, parsedURL.Query().Get("X-Amz-Credential"))
	assert.NotEmpty(t, parsedURL.Query().Get("X-Amz-Date"))
}

func TestPresignedURLValidation(t *testing.T) {
	gen := NewPresignedURLGenerator("us-east-1", "http://localhost:9000")

	secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

	presignedURL, err := gen.GeneratePresignedURL(PresignParams{
		Method:      http.MethodGet,
		Bucket:      "test-bucket",
		Key:         "test-key",
		AccessKeyID: "AKIAIOSFODNN7EXAMPLE",
		SecretKey:   secretKey,
		Region:      "us-east-1",
		Endpoint:    "http://localhost:9000",
		Expiration:  15 * time.Minute,
	})
	require.NoError(t, err)

	t.Logf("Generated presigned URL: %s", presignedURL)

	// Parse the URL and create a request
	parsedURL, err := url.Parse(presignedURL)
	require.NoError(t, err)

	t.Logf("Parsed URL path: %s", parsedURL.Path)
	t.Logf("Parsed URL host: %s", parsedURL.Host)

	// Create a request from the presigned URL
	req := httptest.NewRequest(http.MethodGet, parsedURL.RequestURI(), nil)
	req.Host = parsedURL.Host

	t.Logf("Request URL path: %s", req.URL.Path)
	t.Logf("Request host: %s", req.Host)

	// Parse the presigned URL info
	info, err := ParsePresignedURL(req)
	require.NoError(t, err, "Failed to parse presigned URL")

	t.Logf("Parsed info - AccessKeyID: %s, Region: %s", info.AccessKeyID, info.Region)

	// Validate the signature
	err = ValidatePresignedSignature(req, info, secretKey)
	assert.NoError(t, err, "Signature validation should pass")
}
