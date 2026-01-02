package auth_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePresignedURL(t *testing.T) {
	gen := auth.NewPresignedURLGenerator("us-east-1", "http://localhost:9000")

	presignedURL, err := gen.GeneratePresignedURL(auth.PresignParams{
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
	gen := auth.NewPresignedURLGenerator("us-east-1", "http://localhost:9000")

	secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

	presignedURL, err := gen.GeneratePresignedURL(auth.PresignParams{
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
	info, err := auth.ParsePresignedURL(req)
	require.NoError(t, err, "Failed to parse presigned URL")

	t.Logf("Parsed info - AccessKeyID: %s, Region: %s", info.AccessKeyID, info.Region)

	// Validate the signature
	err = auth.ValidatePresignedSignature(req, info, secretKey)
	assert.NoError(t, err, "Signature validation should pass")
}

func TestPresignedURLWithEmptyKey(t *testing.T) {
	gen := auth.NewPresignedURLGenerator("us-east-1", "http://localhost:9000")

	presignedURL, err := gen.GeneratePresignedURL(auth.PresignParams{
		Method:      http.MethodGet,
		Bucket:      "test-bucket",
		Key:         "",
		AccessKeyID: "AKIAIOSFODNN7EXAMPLE",
		SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region:      "us-east-1",
		Endpoint:    "http://localhost:9000",
		Expiration:  15 * time.Minute,
	})

	require.NoError(t, err)

	parsedURL, err := url.Parse(presignedURL)
	require.NoError(t, err)

	// For path-style with empty key, path should be /bucket
	assert.Equal(t, "/test-bucket", parsedURL.Path)
}

func TestPresignedURLWithVirtualHostedStyle(t *testing.T) {
	gen := auth.NewPresignedURLGenerator("us-east-1", "")

	presignedURL, err := gen.GeneratePresignedURL(auth.PresignParams{
		Method:      http.MethodGet,
		Bucket:      "test-bucket",
		Key:         "test-key",
		AccessKeyID: "AKIAIOSFODNN7EXAMPLE",
		SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region:      "us-east-1",
		Endpoint:    "",
		Expiration:  15 * time.Minute,
	})

	require.NoError(t, err)

	parsedURL, err := url.Parse(presignedURL)
	require.NoError(t, err)

	// Virtual-hosted style: bucket in host, key in path
	assert.Contains(t, parsedURL.Host, "test-bucket")
	assert.Equal(t, "/test-key", parsedURL.Path)
}

func TestPresignedURLWithSpecialCharacters(t *testing.T) {
	gen := auth.NewPresignedURLGenerator("us-east-1", "http://localhost:9000")
	secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

	specialKeys := []string{
		"folder/file.txt",
		"path/to/deep/file.txt",
		"file with spaces.txt",
		"file-with-dashes.txt",
		"file_with_underscores.txt",
	}

	for _, key := range specialKeys {
		t.Run(key, func(t *testing.T) {
			presignedURL, err := gen.GeneratePresignedURL(auth.PresignParams{
				Method:      http.MethodGet,
				Bucket:      "test-bucket",
				Key:         key,
				AccessKeyID: "AKIAIOSFODNN7EXAMPLE",
				SecretKey:   secretKey,
				Region:      "us-east-1",
				Endpoint:    "http://localhost:9000",
				Expiration:  15 * time.Minute,
			})
			require.NoError(t, err, "Failed to generate presigned URL for key '%s'", key)

			t.Logf("Key: %s", key)
			t.Logf("Presigned URL: %s", presignedURL)

			// Parse the URL and create a request
			parsedURL, err := url.Parse(presignedURL)
			require.NoError(t, err)

			t.Logf("Parsed URL path: %s", parsedURL.Path)
			t.Logf("RequestURI: %s", parsedURL.RequestURI())

			// Create a request from the presigned URL
			req := httptest.NewRequest(http.MethodGet, parsedURL.RequestURI(), nil)
			req.Host = parsedURL.Host

			t.Logf("Request URL path: %s", req.URL.Path)

			// Parse the presigned URL info
			info, err := auth.ParsePresignedURL(req)
			require.NoError(t, err, "Failed to parse presigned URL for key '%s'", key)

			// Validate the signature
			err = auth.ValidatePresignedSignature(req, info, secretKey)
			assert.NoError(t, err, "Signature validation should pass for key '%s'", key)
		})
	}
}
