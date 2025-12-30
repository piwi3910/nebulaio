// Package testutil provides testing utilities and mock implementations
// for NebulaIO unit and integration tests.
//
// This package centralizes common testing infrastructure to:
// - Reduce mock duplication across test files
// - Standardize on testify assertions
// - Provide consistent error injection patterns
// - Ensure thread-safe mock implementations
//
// Usage:
//
//	import (
//		"github.com/piwi3910/nebulaio/internal/testutil/mocks"
//		"github.com/stretchr/testify/assert"
//		"github.com/stretchr/testify/require"
//	)
//
//	func TestSomething(t *testing.T) {
//		store := mocks.NewMockMetadataStore()
//		storage := mocks.NewMockStorageBackend()
//
//		// Configure error injection
//		store.SetCreateBucketError(someError)
//
//		// Run test...
//		require.NoError(t, err)
//		assert.Equal(t, expected, actual)
//	}
package testutil

import (
	"os"
	"strings"
)

// ContainsString checks if the string s contains the substring substr.
// This is a convenience wrapper around strings.Contains for test assertions.
func ContainsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// ContainsStringInsensitive checks if the string s contains the substring substr (case-insensitive).
// Useful for comparing error messages or log output where case may vary.
func ContainsStringInsensitive(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// GetEnvOrDefault returns the environment variable value or a default if not set.
// This is useful for configurable test parameters like test timeouts or resource limits.
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// MultipartUploadKey generates a consistent key for multipart upload storage.
// Format: "bucket/key/uploadID".
func MultipartUploadKey(bucket, key, uploadID string) string {
	return bucket + "/" + key + "/" + uploadID
}
