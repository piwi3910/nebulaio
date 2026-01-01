package bucket

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test constants for validation limits.
const (
	minBucketNameLength = 3
	maxBucketNameLength = 63
	maxObjectKeyLength  = 1024
	maxMetadataSize     = 2048
	maxTagKeyLen        = 128
	maxTagValueLen      = 256
	maxTagCount         = 50
	tagCountExceeded    = 51
)

// TestBucketNameSecurityValidation tests security aspects of bucket name validation.
// This uses the production validateBucketName function from service.go.
func TestBucketNameSecurityValidation(t *testing.T) {
	t.Run("valid bucket names pass validation", func(t *testing.T) {
		validNames := []string{
			"my-bucket",
			"test-bucket-123",
			"bucket.with.dots",
			"a-1",
			"abc",
			strings.Repeat("a", maxBucketNameLength),
			"my-test-bucket",
			"production-data-2024",
			"bucket-with-many-hyphens",
			"123-starts-with-number",
			"ends-with-number-123",
		}

		for _, name := range validNames {
			t.Run(name, func(t *testing.T) {
				err := validateBucketName(name)
				assert.NoError(t, err, "Should be valid: %s", name)
			})
		}
	})

	t.Run("invalid bucket names - length violations", func(t *testing.T) {
		testCases := []struct {
			testName   string
			bucketName string
		}{
			{"empty", ""},
			{"too short - 1 char", "a"},
			{"too short - 2 chars", "ab"},
			{"too long - 64 chars", strings.Repeat("a", maxBucketNameLength+1)},
			{"too long - 100 chars", strings.Repeat("a", 100)},
		}

		for _, testCase := range testCases {
			t.Run(testCase.testName, func(t *testing.T) {
				err := validateBucketName(testCase.bucketName)
				assert.Error(t, err, "Should be invalid: %s", testCase.testName)
			})
		}
	})

	t.Run("invalid bucket names - format violations", func(t *testing.T) {
		testCases := []struct {
			testName   string
			bucketName string
		}{
			{"starts with hyphen", "-start-with-dash"},
			{"ends with hyphen", "end-with-dash-"},
			{"starts with dot", ".start-with-dot"},
			{"ends with dot", "end-with-dot."},
			{"uppercase letters", "UPPERCASE"},
			{"mixed case", "MixedCase"},
			{"contains spaces", "has spaces"},
			{"contains underscore", "has_underscore"},
			{"IP address format", "192.168.1.1"},
			{"IP-like pattern", "10.0.0.1"},
			{"another IP pattern", "172.16.0.1"},
		}

		for _, testCase := range testCases {
			t.Run(testCase.testName, func(t *testing.T) {
				err := validateBucketName(testCase.bucketName)
				assert.Error(t, err, "Should be invalid: %s", testCase.testName)
			})
		}
	})

	t.Run("security attack patterns rejected", func(t *testing.T) {
		// These patterns should be rejected by the regex-based validation
		testCases := []struct {
			testName   string
			bucketName string
		}{
			{"SQL injection chars", "bucket'; DROP TABLE--"},
			{"command injection", "bucket; rm -rf /"},
			{"path traversal", "../../../etc/passwd"},
			{"null byte", "bucket\x00name"},
			{"control characters", "bucket\x01\x02name"},
			{"unicode bypass", "bücket"},
			{"XSS attempt", "bucket<script>"},
		}

		for _, testCase := range testCases {
			t.Run(testCase.testName, func(t *testing.T) {
				err := validateBucketName(testCase.bucketName)
				assert.Error(t, err, "Should reject: %s", testCase.testName)
			})
		}
	})
}

// TestTagSecurityValidation tests security aspects of bucket tag validation.
// This uses the production validateBucketTags function from service.go.
func TestTagSecurityValidation(t *testing.T) {
	t.Run("valid tags pass validation", func(t *testing.T) {
		validTags := []map[string]string{
			{"env": "production"},
			{"team": "engineering", "project": "nebulaio"},
			{"cost-center": "12345"},
			{"owner": "john.doe@example.com"},
		}

		for _, tags := range validTags {
			err := validateBucketTags(tags)
			assert.NoError(t, err, "Should be valid tags")
		}
	})

	t.Run("rejects too many tags", func(t *testing.T) {
		tags := make(map[string]string)
		for i := range tagCountExceeded {
			tags["key"+string(rune('a'+i))] = "value"
		}

		err := validateBucketTags(tags)
		assert.Error(t, err, "Should reject more than 50 tags")
	})

	t.Run("rejects tag key too long", func(t *testing.T) {
		tags := map[string]string{
			strings.Repeat("k", maxTagKeyLen+1): "value",
		}

		err := validateBucketTags(tags)
		assert.Error(t, err, "Should reject key longer than 128 chars")
	})

	t.Run("rejects tag value too long", func(t *testing.T) {
		tags := map[string]string{
			"key": strings.Repeat("v", maxTagValueLen+1),
		}

		err := validateBucketTags(tags)
		assert.Error(t, err, "Should reject value longer than 256 chars")
	})

	t.Run("rejects empty tag key", func(t *testing.T) {
		tags := map[string]string{
			"": "value",
		}

		err := validateBucketTags(tags)
		assert.Error(t, err, "Should reject empty key")
	})

	t.Run("rejects reserved aws: prefix", func(t *testing.T) {
		reservedPrefixes := []string{
			"aws:tag",
			"AWS:tag",
			"Aws:tag",
		}

		for _, key := range reservedPrefixes {
			tags := map[string]string{key: "value"}
			err := validateBucketTags(tags)
			assert.Error(t, err, "Should reject aws: prefix: %s", key)
		}
	})
}

// TestObjectKeySecurityValidation tests object key validation for security.
func TestObjectKeySecurityValidation(t *testing.T) {
	t.Run("valid object keys", func(t *testing.T) {
		validKeys := []string{
			"simple-key",
			"path/to/object",
			"file.txt",
			"folder/subfolder/file.json",
			"unicode-αβγδ.txt",
			"spaces are allowed.txt",
			strings.Repeat("a", maxObjectKeyLength),
		}

		for _, key := range validKeys {
			displayKey := key
			if len(key) > 20 {
				displayKey = key[:20] + "..."
			}
			t.Run(displayKey, func(t *testing.T) {
				err := validateObjectKeySecurity(key)
				assert.NoError(t, err, "Should be valid key")
			})
		}
	})

	t.Run("invalid object keys", func(t *testing.T) {
		testCases := []struct {
			testName string
			key      string
		}{
			{"empty", ""},
			{"too long", strings.Repeat("a", maxObjectKeyLength+1)},
			{"null bytes", "file\x00.txt"},
			{"backslash", "path\\to\\file"},
		}

		for _, testCase := range testCases {
			t.Run(testCase.testName, func(t *testing.T) {
				err := validateObjectKeySecurity(testCase.key)
				assert.Error(t, err, "Should be invalid: %s", testCase.testName)
			})
		}
	})

	t.Run("path traversal in keys is sanitized", func(t *testing.T) {
		pathTraversalKeys := []string{
			"../etc/passwd",
			"..\\..\\windows\\system32",
			"folder/../../../etc/passwd",
		}

		for _, key := range pathTraversalKeys {
			safeKey := sanitizeObjectKey(key)
			assert.NotContains(t, safeKey, "..", "Key should be sanitized")
		}
	})
}

// TestMetadataSecurityValidation tests object metadata validation for security.
func TestMetadataSecurityValidation(t *testing.T) {
	t.Run("valid metadata passes", func(t *testing.T) {
		validMetadata := map[string]string{
			"Content-Type":        "application/json",
			"Cache-Control":       "max-age=3600",
			"x-amz-meta-custom":   "value",
			"x-amz-meta-filename": "document.pdf",
		}

		err := validateMetadataSecurity(validMetadata)
		assert.NoError(t, err)
	})

	t.Run("rejects oversized metadata", func(t *testing.T) {
		const oversizedValueLen = 3000
		largeMetadata := map[string]string{
			"x-amz-meta-large": strings.Repeat("x", oversizedValueLen),
		}

		err := validateMetadataSecurity(largeMetadata)
		assert.Error(t, err, "Should reject oversized metadata")
	})

	t.Run("rejects header injection in metadata", func(t *testing.T) {
		injectionMetadata := map[string]string{
			"x-amz-meta-evil\r\nX-Injected": "value",
			"x-amz-meta-normal":             "value\r\nX-Injected: header",
		}

		for key, value := range injectionMetadata {
			metadata := map[string]string{key: value}
			err := validateMetadataSecurity(metadata)
			assert.Error(t, err, "Should reject header injection")
		}
	})
}

// TestContentTypeSecurityValidation tests content type risk assessment.
func TestContentTypeSecurityValidation(t *testing.T) {
	t.Run("safe content types", func(t *testing.T) {
		safeTypes := []string{
			"application/json",
			"application/xml",
			"application/pdf",
			"text/plain",
			"text/csv",
			"image/jpeg",
			"image/png",
			"image/gif",
			"video/mp4",
			"audio/mpeg",
			"application/octet-stream",
			"application/zip",
		}

		for _, contentType := range safeTypes {
			result := assessContentTypeRisk(contentType)
			assert.False(t, result.Risky, "Should not flag as risky: %s", contentType)
		}
	})

	t.Run("risky content types are flagged", func(t *testing.T) {
		riskyTypes := []string{
			"text/html",
			"application/javascript",
			"text/javascript",
			"application/x-httpd-php",
			"application/x-sh",
		}

		for _, contentType := range riskyTypes {
			result := assessContentTypeRisk(contentType)
			assert.True(t, result.Risky, "Should flag as risky: %s", contentType)
		}
	})
}

// TestACLSecurityValidation tests ACL validation.
func TestACLSecurityValidation(t *testing.T) {
	t.Run("accepts valid canned ACLs", func(t *testing.T) {
		validACLs := []string{
			"private",
			"public-read",
			"public-read-write",
			"authenticated-read",
			"aws-exec-read",
			"bucket-owner-read",
			"bucket-owner-full-control",
		}

		for _, acl := range validACLs {
			err := validateCannedACLSecurity(acl)
			assert.NoError(t, err, "Should accept: %s", acl)
		}
	})

	t.Run("rejects invalid ACLs", func(t *testing.T) {
		invalidACLs := []string{
			"",
			"invalid-acl",
			"public-execute",
			"all-access",
		}

		for _, acl := range invalidACLs {
			err := validateCannedACLSecurity(acl)
			assert.Error(t, err, "Should reject: %s", acl)
		}
	})
}

// Security validation helper functions for testing.
// These helpers validate security aspects for tests.

// validateObjectKeySecurity validates an object key for security issues.
func validateObjectKeySecurity(key string) error {
	if len(key) == 0 || len(key) > maxObjectKeyLength {
		return assert.AnError
	}

	if strings.Contains(key, "\x00") {
		return assert.AnError
	}

	if strings.Contains(key, "\\") {
		return assert.AnError
	}

	return nil
}

// validateMetadataSecurity validates object metadata for security issues.
func validateMetadataSecurity(metadata map[string]string) error {
	totalSize := 0

	for key, value := range metadata {
		totalSize += len(key) + len(value)

		if strings.ContainsAny(key, "\r\n") || strings.ContainsAny(value, "\r\n") {
			return assert.AnError
		}
	}

	if totalSize > maxMetadataSize {
		return assert.AnError
	}

	return nil
}

// ContentTypeRiskResult represents content type risk assessment.
type ContentTypeRiskResult struct {
	Risky  bool
	Reason string
}

// assessContentTypeRisk assesses content type risk for security.
func assessContentTypeRisk(contentType string) ContentTypeRiskResult {
	riskyTypes := map[string]string{
		"text/html":               "HTML files can contain scripts",
		"application/javascript":  "JavaScript can be executed",
		"text/javascript":         "JavaScript can be executed",
		"application/x-httpd-php": "PHP files can execute server-side",
		"application/x-sh":        "Shell scripts can be executed",
	}

	if reason, ok := riskyTypes[contentType]; ok {
		return ContentTypeRiskResult{Risky: true, Reason: reason}
	}

	return ContentTypeRiskResult{Risky: false}
}

// validateCannedACLSecurity validates a canned ACL.
func validateCannedACLSecurity(acl string) error {
	validACLs := map[string]bool{
		"private":                   true,
		"public-read":               true,
		"public-read-write":         true,
		"authenticated-read":        true,
		"aws-exec-read":             true,
		"bucket-owner-read":         true,
		"bucket-owner-full-control": true,
	}

	if !validACLs[acl] {
		return assert.AnError
	}

	return nil
}

// sanitizeObjectKey removes path traversal sequences from object keys.
func sanitizeObjectKey(key string) string {
	result := key
	for strings.Contains(result, "..") {
		result = strings.ReplaceAll(result, "..", "")
	}

	return result
}
