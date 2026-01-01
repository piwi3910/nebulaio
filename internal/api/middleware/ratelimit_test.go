package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenBucketLimiter(t *testing.T) {
	t.Run("allows requests within burst", func(t *testing.T) {
		limiter := NewTokenBucketLimiter(10, 5)

		// Should allow 5 requests immediately (burst size)
		for range 5 {
			assert.True(t, limiter.Allow(), "request should be allowed")
		}
	})

	t.Run("blocks requests after burst exhausted", func(t *testing.T) {
		limiter := NewTokenBucketLimiter(10, 3)

		// Exhaust burst
		for range 3 {
			require.True(t, limiter.Allow())
		}

		// Next request should be blocked
		assert.False(t, limiter.Allow(), "request should be blocked after burst exhausted")
	})

	t.Run("refills tokens over time", func(t *testing.T) {
		limiter := NewTokenBucketLimiter(10, 2)

		// Exhaust tokens
		require.True(t, limiter.Allow())
		require.True(t, limiter.Allow())
		require.False(t, limiter.Allow())

		// Wait for token refill (100ms should give ~1 token at 10 rps)
		time.Sleep(150 * time.Millisecond)

		// Should have token again
		assert.True(t, limiter.Allow(), "should have token after refill")
	})
}

func TestRateLimiter(t *testing.T) {
	t.Run("disabled allows all requests", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = false

		rl := NewRateLimiter(config)
		defer rl.Close()

		for range 100 {
			assert.True(t, rl.Allow("192.168.1.1"))
		}
	})

	t.Run("per-IP limiting creates separate limiters", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = 2
		config.PerIP = true

		rl := NewRateLimiter(config)
		defer rl.Close()

		// IP1 can use its own burst
		assert.True(t, rl.Allow("192.168.1.1"))
		assert.True(t, rl.Allow("192.168.1.1"))

		// IP2 has its own separate burst
		assert.True(t, rl.Allow("192.168.1.2"))
		assert.True(t, rl.Allow("192.168.1.2"))
	})

	t.Run("global limiting shares limiter", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = 3
		config.PerIP = false

		rl := NewRateLimiter(config)
		defer rl.Close()

		// All IPs share the same burst
		assert.True(t, rl.Allow("192.168.1.1"))
		assert.True(t, rl.Allow("192.168.1.2"))
		assert.True(t, rl.Allow("192.168.1.3"))
		assert.False(t, rl.Allow("192.168.1.4"))
	})

	t.Run("excluded paths are not rate limited", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.ExcludedPaths = []string{"/health", "/metrics"}

		rl := NewRateLimiter(config)
		defer rl.Close()

		assert.True(t, rl.isExcludedPath("/health"))
		assert.True(t, rl.isExcludedPath("/metrics"))
		assert.False(t, rl.isExcludedPath("/api/buckets"))
	})
}

func TestRateLimitMiddleware(t *testing.T) {
	t.Run("allows requests when disabled", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = false

		rl := NewRateLimiter(config)
		defer rl.Close()

		handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/buckets", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("returns 429 when rate limited", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = 1
		config.PerIP = true

		rl := NewRateLimiter(config)
		defer rl.Close()

		handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// First request succeeds
		req1 := httptest.NewRequest(http.MethodGet, "/api/buckets", nil)
		req1.RemoteAddr = "192.168.1.1:12345"
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		assert.Equal(t, http.StatusOK, rec1.Code)

		// Second request is rate limited
		req2 := httptest.NewRequest(http.MethodGet, "/api/buckets", nil)
		req2.RemoteAddr = "192.168.1.1:12346"
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
		assert.NotEmpty(t, rec2.Header().Get("Retry-After"))
	})

	t.Run("skips excluded paths", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = 0 // Would block all requests
		config.ExcludedPaths = []string{"/health"}

		rl := NewRateLimiter(config)
		defer rl.Close()

		handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestExtractClientIP(t *testing.T) {
	// Create a rate limiter with 127.0.0.1 as trusted proxy for tests that use XFF/XRI headers
	trustedConfig := RateLimitConfig{
		Enabled:        true,
		TrustedProxies: []string{"127.0.0.1", "::1"},
	}
	rlTrusted := NewRateLimiter(trustedConfig)
	defer rlTrusted.Close()

	// Create a rate limiter without trusted proxies
	untrustedConfig := RateLimitConfig{
		Enabled: true,
	}
	rlUntrusted := NewRateLimiter(untrustedConfig)
	defer rlUntrusted.Close()

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		expected   string
		useTrusted bool // whether to use the rate limiter with trusted proxies
	}{
		{
			name:       "RemoteAddr only",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
			useTrusted: false,
		},
		{
			name:       "X-Forwarded-For single IP (trusted proxy)",
			remoteAddr: "127.0.0.1:12345",
			xff:        "203.0.113.1",
			expected:   "203.0.113.1",
			useTrusted: true,
		},
		{
			name:       "X-Forwarded-For single IP (untrusted proxy)",
			remoteAddr: "127.0.0.1:12345",
			xff:        "203.0.113.1",
			expected:   "127.0.0.1", // Uses direct IP when proxy not trusted
			useTrusted: false,
		},
		{
			name:       "X-Forwarded-For multiple IPs (trusted proxy)",
			remoteAddr: "127.0.0.1:12345",
			xff:        "203.0.113.1, 198.51.100.1, 192.0.2.1",
			expected:   "203.0.113.1",
			useTrusted: true,
		},
		{
			name:       "X-Real-IP (trusted proxy)",
			remoteAddr: "127.0.0.1:12345",
			xri:        "203.0.113.5",
			expected:   "203.0.113.5",
			useTrusted: true,
		},
		{
			name:       "X-Real-IP (untrusted proxy)",
			remoteAddr: "127.0.0.1:12345",
			xri:        "203.0.113.5",
			expected:   "127.0.0.1", // Uses direct IP when proxy not trusted
			useTrusted: false,
		},
		{
			name:       "X-Forwarded-For takes precedence (trusted proxy)",
			remoteAddr: "127.0.0.1:12345",
			xff:        "203.0.113.1",
			xri:        "203.0.113.5",
			expected:   "203.0.113.1",
			useTrusted: true,
		},
		{
			name:       "IPv6 RemoteAddr",
			remoteAddr: "[2001:db8::1]:12345",
			expected:   "2001:db8::1",
			useTrusted: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr

			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}

			if tc.xri != "" {
				req.Header.Set("X-Real-IP", tc.xri)
			}

			var ip string
			if tc.useTrusted {
				ip = rlTrusted.extractClientIP(req)
			} else {
				ip = rlUntrusted.extractClientIP(req)
			}

			assert.Equal(t, tc.expected, ip)
		})
	}
}

func TestExtractDirectIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{"IPv4 with port", "192.168.1.1:12345", "192.168.1.1"},
		{"IPv6 with port", "[2001:db8::1]:12345", "2001:db8::1"},
		{"IPv4 without port", "192.168.1.1", "192.168.1.1"},
		{"IPv6 without port", "2001:db8::1", "2001:db8::1"},
		{"localhost with port", "127.0.0.1:8080", "127.0.0.1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := extractDirectIP(tc.remoteAddr)
			assert.Equal(t, tc.expected, ip)
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "/api/health",
			expected: "/api/health",
		},
		{
			name:     "bucket path",
			path:     "/buckets/my-bucket",
			expected: "/buckets/{bucket}",
		},
		{
			name:     "object path",
			path:     "/buckets/my-bucket/objects/my-object.txt",
			expected: "/buckets/{bucket}/objects/{object}",
		},
		{
			name:     "UUID in path",
			path:     "/uploads/550e8400-e29b-41d4-a716-446655440000",
			expected: "/uploads/{upload}",
		},
		{
			name:     "complex path with UUID",
			path:     "/buckets/test-bucket/objects/550e8400-e29b-41d4-a716-446655440000/versions/v1",
			expected: "/buckets/{bucket}/objects/{object}/versions/{version}",
		},
		{
			name:     "users path",
			path:     "/users/admin",
			expected: "/users/{user}",
		},
		{
			name:     "keys path",
			path:     "/keys/access-key-123",
			expected: "/keys/{key}",
		},
		{
			name:     "policies path",
			path:     "/policies/read-only",
			expected: "/policies/{policy}",
		},
		{
			name:     "groups path",
			path:     "/groups/admins",
			expected: "/groups/{group}",
		},
		{
			name:     "root path",
			path:     "/",
			expected: "/",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizePath(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}
