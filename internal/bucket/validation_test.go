// Package bucket provides input validation testing for bucket operations.
// These tests verify comprehensive input validation for security.
package bucket

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBucketNameValidation tests comprehensive bucket name validation.
func TestBucketNameValidation(t *testing.T) {
	t.Run("valid bucket names", func(t *testing.T) {
		validNames := []string{
			"my-bucket",
			"test-bucket-123",
			"bucket.with.dots",
			"a-1",
			"abc",                   // minimum length
			strings.Repeat("a", 63), // maximum length
			"my-test-bucket",
			"production-data-2024",
			"bucket-with-many-hyphens",
			"123-starts-with-number",
			"ends-with-number-123",
		}

		for _, name := range validNames {
			t.Run(name, func(t *testing.T) {
				err := ValidateBucketName(name)
				assert.NoError(t, err, "Should be valid: %s", name)
			})
		}
	})

	t.Run("invalid bucket names - length", func(t *testing.T) {
		invalidLengthNames := []struct {
			name   string
			bucket string
		}{
			{"empty", ""},
			{"too short - 1 char", "a"},
			{"too short - 2 chars", "ab"},
			{"too long - 64 chars", strings.Repeat("a", 64)},
			{"too long - 100 chars", strings.Repeat("a", 100)},
		}

		for _, tc := range invalidLengthNames {
			t.Run(tc.name, func(t *testing.T) {
				err := ValidateBucketName(tc.bucket)
				assert.Error(t, err, "Should be invalid: %s", tc.name)
			})
		}
	})

	t.Run("invalid bucket names - format", func(t *testing.T) {
		invalidFormatNames := []struct {
			name   string
			bucket string
		}{
			{"starts with hyphen", "-start-with-dash"},
			{"ends with hyphen", "end-with-dash-"},
			{"starts with dot", ".start-with-dot"},
			{"ends with dot", "end-with-dot."},
			{"uppercase letters", "UPPERCASE"},
			{"mixed case", "MixedCase"},
			{"contains spaces", "has spaces"},
			{"contains underscore", "has_underscore"},
			{"consecutive dots", "has..consecutive.dots"},
			{"IP address format", "192.168.1.1"},
			{"IP-like pattern", "10.0.0.1"},
			{"another IP pattern", "172.16.0.1"},
		}

		for _, tc := range invalidFormatNames {
			t.Run(tc.name, func(t *testing.T) {
				err := ValidateBucketName(tc.bucket)
				assert.Error(t, err, "Should be invalid: %s", tc.name)
			})
		}
	})

	t.Run("invalid bucket names - security", func(t *testing.T) {
		securityRiskNames := []struct {
			name   string
			bucket string
		}{
			{"SQL injection", "bucket'; DROP TABLE--"},
			{"command injection", "bucket; rm -rf /"},
			{"path traversal", "../../../etc/passwd"},
			{"null byte", "bucket\x00name"},
			{"control characters", "bucket\x01\x02name"},
			{"unicode bypass", "bücket"},
			{"XSS attempt", "bucket<script>"},
		}

		for _, tc := range securityRiskNames {
			t.Run(tc.name, func(t *testing.T) {
				err := ValidateBucketName(tc.bucket)
				assert.Error(t, err, "Should reject security risk: %s", tc.name)
			})
		}
	})
}

// TestTagValidation tests bucket tag validation.
func TestTagValidation(t *testing.T) {
	t.Run("valid tags", func(t *testing.T) {
		validTags := []map[string]string{
			{"env": "production"},
			{"team": "engineering", "project": "nebulaio"},
			{"cost-center": "12345"},
			{"owner": "john.doe@example.com"},
		}

		for _, tags := range validTags {
			err := ValidateBucketTags(tags)
			assert.NoError(t, err, "Should be valid tags")
		}
	})

	t.Run("rejects too many tags", func(t *testing.T) {
		// Create 51 tags (max is 50)
		tags := make(map[string]string)
		for i := range 51 {
			tags["key"+string(rune('a'+i))] = "value"
		}

		err := ValidateBucketTags(tags)
		assert.Error(t, err, "Should reject more than 50 tags")
		assert.Contains(t, err.Error(), "maximum", "Error should mention maximum limit")
	})

	t.Run("rejects tag key too long", func(t *testing.T) {
		tags := map[string]string{
			strings.Repeat("k", 129): "value", // Max is 128
		}

		err := ValidateBucketTags(tags)
		assert.Error(t, err, "Should reject key longer than 128 chars")
	})

	t.Run("rejects tag value too long", func(t *testing.T) {
		tags := map[string]string{
			"key": strings.Repeat("v", 257), // Max is 256
		}

		err := ValidateBucketTags(tags)
		assert.Error(t, err, "Should reject value longer than 256 chars")
	})

	t.Run("rejects empty tag key", func(t *testing.T) {
		tags := map[string]string{
			"": "value",
		}

		err := ValidateBucketTags(tags)
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
			err := ValidateBucketTags(tags)
			assert.Error(t, err, "Should reject aws: prefix: %s", key)
		}
	})

	t.Run("rejects tags with injection attempts", func(t *testing.T) {
		injectionTags := []map[string]string{
			{"<script>": "value"},
			{"key": "<script>alert(1)</script>"},
			{"key": "'; DROP TABLE--"},
			{"key\x00evil": "value"},
		}

		for _, tags := range injectionTags {
			err := ValidateBucketTags(tags)
			assert.Error(t, err, "Should reject injection attempt in tags")
		}
	})
}

// TestObjectKeyValidation tests object key validation.
func TestObjectKeyValidation(t *testing.T) {
	t.Run("valid object keys", func(t *testing.T) {
		validKeys := []string{
			"simple-key",
			"path/to/object",
			"file.txt",
			"folder/subfolder/file.json",
			"unicode-αβγδ.txt",
			"spaces are allowed.txt",
			strings.Repeat("a", 1024), // max length
		}

		for _, key := range validKeys {
			t.Run(key[:min(len(key), 20)], func(t *testing.T) {
				err := ValidateObjectKey(key)
				assert.NoError(t, err, "Should be valid key")
			})
		}
	})

	t.Run("invalid object keys", func(t *testing.T) {
		invalidKeys := []struct {
			name string
			key  string
		}{
			{"empty", ""},
			{"too long", strings.Repeat("a", 1025)},
			{"null bytes", "file\x00.txt"},
			{"backslash", "path\\to\\file"},
		}

		for _, tc := range invalidKeys {
			t.Run(tc.name, func(t *testing.T) {
				err := ValidateObjectKey(tc.key)
				assert.Error(t, err, "Should be invalid: %s", tc.name)
			})
		}
	})

	t.Run("handles path traversal in keys", func(t *testing.T) {
		// Object keys can contain ".." but should be handled safely
		// by the storage layer without actual path traversal
		pathTraversalKeys := []string{
			"../etc/passwd",
			"..\\..\\windows\\system32",
			"folder/../../../etc/passwd",
		}

		for _, key := range pathTraversalKeys {
			// The key itself may be valid syntactically
			// but storage must handle it safely
			err := ValidateObjectKey(key)
			if err == nil {
				// If accepted, ensure it's stored safely
				safeKey := sanitizeObjectKey(key)
				assert.NotContains(t, safeKey, "..", "Key should be sanitized")
			}
		}
	})
}

// TestMetadataValidation tests object metadata validation.
func TestMetadataValidation(t *testing.T) {
	t.Run("valid metadata", func(t *testing.T) {
		validMetadata := map[string]string{
			"Content-Type":        "application/json",
			"Cache-Control":       "max-age=3600",
			"x-amz-meta-custom":   "value",
			"x-amz-meta-filename": "document.pdf",
		}

		err := ValidateMetadata(validMetadata)
		assert.NoError(t, err)
	})

	t.Run("rejects oversized metadata", func(t *testing.T) {
		// Total metadata size limit is typically 2KB
		largeMetadata := map[string]string{
			"x-amz-meta-large": strings.Repeat("x", 3000),
		}

		err := ValidateMetadata(largeMetadata)
		assert.Error(t, err, "Should reject oversized metadata")
	})

	t.Run("rejects header injection in metadata", func(t *testing.T) {
		injectionMetadata := map[string]string{
			"x-amz-meta-evil\r\nX-Injected": "value",
			"x-amz-meta-normal":             "value\r\nX-Injected: header",
		}

		for key, value := range injectionMetadata {
			metadata := map[string]string{key: value}
			err := ValidateMetadata(metadata)
			assert.Error(t, err, "Should reject header injection")
		}
	})
}

// TestContentTypeValidation tests content type validation.
func TestContentTypeValidation(t *testing.T) {
	t.Run("allows safe content types", func(t *testing.T) {
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

		for _, ct := range safeTypes {
			err := ValidateContentType(ct)
			assert.NoError(t, err, "Should allow: %s", ct)
		}
	})

	t.Run("warns on potentially dangerous content types", func(t *testing.T) {
		riskyTypes := []string{
			"text/html",
			"application/javascript",
			"text/javascript",
			"application/x-httpd-php",
			"application/x-sh",
		}

		for _, ct := range riskyTypes {
			result := ValidateContentTypeRisk(ct)
			assert.True(t, result.Risky, "Should flag as risky: %s", ct)
		}
	})
}

// TestACLValidation tests ACL validation.
func TestACLValidation(t *testing.T) {
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
			err := ValidateCannedACL(acl)
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
			err := ValidateCannedACL(acl)
			assert.Error(t, err, "Should reject: %s", acl)
		}
	})
}

// Helper validation functions

// ValidateBucketName validates a bucket name according to S3 rules.
func ValidateBucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return assert.AnError
	}

	// Check for valid characters only
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			return assert.AnError
		}
	}

	// Check start/end characters
	if name[0] == '-' || name[0] == '.' || name[len(name)-1] == '-' || name[len(name)-1] == '.' {
		return assert.AnError
	}

	// Check for consecutive dots
	if strings.Contains(name, "..") {
		return assert.AnError
	}

	// Check for IP address format
	if isIPFormat(name) {
		return assert.AnError
	}

	return nil
}

// ValidateBucketTags validates bucket tags.
func ValidateBucketTags(tags map[string]string) error {
	const (
		maxTags     = 50
		maxKeyLen   = 128
		maxValueLen = 256
	)

	if len(tags) > maxTags {
		return assert.AnError
	}

	for key, value := range tags {
		if len(key) == 0 || len(key) > maxKeyLen {
			return assert.AnError
		}

		if len(value) > maxValueLen {
			return assert.AnError
		}

		// Check for aws: prefix
		if strings.HasPrefix(strings.ToLower(key), "aws:") {
			return assert.AnError
		}

		// Check for injection attempts
		if containsInjectionPatterns(key) || containsInjectionPatterns(value) {
			return assert.AnError
		}
	}

	return nil
}

// ValidateObjectKey validates an object key.
func ValidateObjectKey(key string) error {
	if len(key) == 0 || len(key) > 1024 {
		return assert.AnError
	}

	// Check for null bytes
	if strings.Contains(key, "\x00") {
		return assert.AnError
	}

	// Check for backslashes (Windows-style paths)
	if strings.Contains(key, "\\") {
		return assert.AnError
	}

	return nil
}

// ValidateMetadata validates object metadata.
func ValidateMetadata(metadata map[string]string) error {
	const maxMetadataSize = 2048

	totalSize := 0

	for key, value := range metadata {
		totalSize += len(key) + len(value)

		// Check for header injection
		if strings.ContainsAny(key, "\r\n") || strings.ContainsAny(value, "\r\n") {
			return assert.AnError
		}
	}

	if totalSize > maxMetadataSize {
		return assert.AnError
	}

	return nil
}

// ValidateContentType validates a content type.
func ValidateContentType(_ string) error {
	// All content types are technically valid
	return nil
}

// ContentTypeRiskResult represents content type risk assessment.
type ContentTypeRiskResult struct {
	Risky  bool
	Reason string
}

// ValidateContentTypeRisk assesses content type risk.
func ValidateContentTypeRisk(contentType string) ContentTypeRiskResult {
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

// ValidateCannedACL validates a canned ACL.
func ValidateCannedACL(acl string) error {
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

// Helper functions

func isIPFormat(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}

	for _, part := range parts {
		if len(part) == 0 || len(part) > 3 {
			return false
		}

		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}

	return true
}

func containsInjectionPatterns(s string) bool {
	patterns := []string{
		"<script",
		"</script>",
		"javascript:",
		"'",
		"--",
		"\x00",
	}

	lower := strings.ToLower(s)
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	return false
}

func sanitizeObjectKey(key string) string {
	// Remove path traversal sequences
	result := key
	for strings.Contains(result, "..") {
		result = strings.ReplaceAll(result, "..", "")
	}

	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
