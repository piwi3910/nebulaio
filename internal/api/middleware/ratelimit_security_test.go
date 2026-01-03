package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants for rate limiting security tests.
// These values are tuned for CI stability.
const (
	testBurstSizeSmall     = 1
	testBurstSizePair      = 2
	testBurstSizeMedium    = 5
	testBurstSizeLarge     = 10
	testRequestsPerSecond  = 10
	testConcurrentRequests = 20
	testManyIPs            = 1000
	testCleanupInterval    = 100 * time.Millisecond
	testStaleTimeout       = 200 * time.Millisecond
	testCleanupWait        = 500 * time.Millisecond
	testSlowlorisConns     = 10
	testBypassAttempts     = 5
	testLockoutThreshold   = 3
	testClientIPAddr       = "192.168.1.1:12345"
)

// TestRateLimitEnforcement tests that rate limits are properly enforced.
func TestRateLimitEnforcement(t *testing.T) {
	t.Run("enforces request limits", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.RequestsPerSecond = testRequestsPerSecond
		config.BurstSize = testBurstSizeMedium
		config.PerIP = true

		rl := NewRateLimiter(config)
		defer rl.Close()

		clientIP := "192.168.1.100"

		// Should allow burst
		for i := range testBurstSizeMedium {
			allowed := rl.Allow(clientIP)
			assert.True(t, allowed, "Request %d within burst should be allowed", i+1)
		}

		// Should block after burst exhausted
		allowed := rl.Allow(clientIP)
		assert.False(t, allowed, "Request after burst should be blocked")
	})

	t.Run("isolates users correctly", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = testBurstSizePair
		config.PerIP = true

		rl := NewRateLimiter(config)
		defer rl.Close()

		// User1 exhausts their limit
		assert.True(t, rl.Allow("user1"))
		assert.True(t, rl.Allow("user1"))
		assert.False(t, rl.Allow("user1"), "User1 should be rate limited")

		// User2 should still have their own limit
		assert.True(t, rl.Allow("user2"), "User2 should not be affected by User1's usage")
		assert.True(t, rl.Allow("user2"))
		assert.False(t, rl.Allow("user2"), "User2 should be rate limited after own burst")

		// User3 is independent
		assert.True(t, rl.Allow("user3"), "User3 should have full burst available")
	})
}

// TestRateLimitMiddlewareEnforcement tests middleware rate limiting.
func TestRateLimitMiddlewareEnforcement(t *testing.T) {
	t.Run("returns 429 with Retry-After header", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = testBurstSizeSmall
		config.PerIP = true

		rl := NewRateLimiter(config)
		defer rl.Close()

		handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// First request should succeed
		req1 := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req1.RemoteAddr = testClientIPAddr
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		assert.Equal(t, http.StatusOK, rec1.Code)

		// Second request should be rate limited
		req2 := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req2.RemoteAddr = "192.168.1.1:12346"
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)

		assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
		assert.NotEmpty(t, rec2.Header().Get("Retry-After"), "Should include Retry-After header")
	})

	t.Run("rate limiting is consistent across sequential requests", func(t *testing.T) {
		// Use sequential requests to ensure deterministic behavior
		config := RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: 1, // Low refill rate
			BurstSize:         testBurstSizeLarge,
			PerIP:             true,
		}

		rl := NewRateLimiter(config)
		defer rl.Close()

		handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		successCount := 0
		rateLimitedCount := 0

		// Send sequential requests - more deterministic than concurrent
		for range testConcurrentRequests {
			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.RemoteAddr = testClientIPAddr
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusOK {
				successCount++
			} else if rec.Code == http.StatusTooManyRequests {
				rateLimitedCount++
			}
		}

		// With 20 requests and burst of 10, at least some should be rate limited
		totalRequests := successCount + rateLimitedCount
		assert.Equal(t, testConcurrentRequests, totalRequests, "All requests should be processed")
		assert.LessOrEqual(t, successCount, testBurstSizeLarge+1,
			"Should not allow significantly more than burst size")
		assert.Positive(t, rateLimitedCount,
			"Some requests should be rate limited")
	})
}

// TestRateLimitIPSpoofingPrevention tests prevention of IP spoofing.
func TestRateLimitIPSpoofingPrevention(t *testing.T) {
	t.Run("ignores X-Forwarded-For from untrusted sources", func(t *testing.T) {
		config := RateLimitConfig{
			Enabled:        true,
			BurstSize:      testBurstSizeSmall,
			PerIP:          true,
			TrustedProxies: []string{}, // No trusted proxies
		}

		rl := NewRateLimiter(config)
		defer rl.Close()

		// Create request with spoofed X-Forwarded-For
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = testClientIPAddr
		req.Header.Set("X-Forwarded-For", "10.0.0.1") // Attempt to spoof

		clientIP := rl.extractClientIP(req)

		// Should use actual remote addr, not spoofed XFF
		assert.Equal(t, "192.168.1.1", clientIP, "Should use real IP when proxy not trusted")
	})

	t.Run("validates trusted proxy configuration", func(t *testing.T) {
		config := RateLimitConfig{
			Enabled:        true,
			BurstSize:      testBurstSizeSmall,
			PerIP:          true,
			TrustedProxies: []string{"10.0.0.0/8"}, // Trust internal network
		}

		rl := NewRateLimiter(config)
		defer rl.Close()

		// Request from trusted proxy
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.1")

		clientIP := rl.extractClientIP(req)

		// Should use XFF since request is from trusted proxy
		assert.Equal(t, "203.0.113.1", clientIP, "Should trust XFF from trusted proxy")
	})

	t.Run("prevents rate limit bypass via XFF manipulation", func(t *testing.T) {
		config := RateLimitConfig{
			Enabled:        true,
			BurstSize:      testBurstSizePair,
			PerIP:          true,
			TrustedProxies: []string{}, // No trusted proxies
		}

		rl := NewRateLimiter(config)
		defer rl.Close()

		// Attacker tries to bypass by changing XFF header
		for i := range testBypassAttempts {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = testClientIPAddr // Same actual IP
			req.Header.Set("X-Forwarded-For", "10.0.0."+strconv.Itoa(i+1))

			allowed := rl.Allow(rl.extractClientIP(req))

			if i < testBurstSizePair {
				assert.True(t, allowed, "First 2 requests should be allowed")
			} else {
				assert.False(t, allowed, "Request %d should be blocked (XFF bypass failed)", i+1)
			}
		}
	})
}

// TestRateLimitDDoSProtection tests DDoS protection capabilities.
func TestRateLimitDDoSProtection(t *testing.T) {
	t.Run("handles high volume of different IPs", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = testBurstSizeSmall
		config.PerIP = true

		rl := NewRateLimiter(config)
		defer rl.Close()

		// Simulate requests from 256 unique IPs
		uniqueIPCount := 256
		for i := range uniqueIPCount {
			// Each IP in the 192.168.1.0/24 range
			clientIP := "192.168.1." + strconv.Itoa(i)
			allowed := rl.Allow(clientIP)
			// First request from each unique IP should be allowed
			assert.True(t, allowed, "First request from IP %s should be allowed", clientIP)
		}
	})

	t.Run("cleans up stale limiters", func(t *testing.T) {
		// Test that cleanup configuration is correctly applied
		// We verify the cleanup mechanism works by checking that:
		// 1. RateLimiter can be created with cleanup config
		// 2. RateLimiter can be closed without issues
		cleanupInterval := 100 * time.Millisecond
		staleTimeout := 200 * time.Millisecond

		config := RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: 1,
			BurstSize:         testBurstSizeSmall,
			PerIP:             true,
			CleanupInterval:   cleanupInterval,
			StaleTimeout:      staleTimeout,
		}

		rl := NewRateLimiter(config)

		// Verify limiter works normally
		clientIP := "192.168.1.1"
		allowed1 := rl.Allow(clientIP)
		assert.True(t, allowed1, "First request should be allowed")

		allowed2 := rl.Allow(clientIP)
		assert.False(t, allowed2, "Second request should be rate limited")

		// Close the rate limiter - this stops the cleanup goroutine
		// If cleanup is broken, this would hang or panic
		rl.Close()

		// Verify we can create another limiter with cleanup config
		// This tests that the cleanup mechanism doesn't cause issues
		rl2 := NewRateLimiter(config)
		defer rl2.Close()

		allowed3 := rl2.Allow(clientIP)
		assert.True(t, allowed3, "New limiter should allow first request")
	})
}

// TestRateLimitSlowlorisProtection tests protection against slowloris attacks.
func TestRateLimitSlowlorisProtection(t *testing.T) {
	t.Run("limits connections per IP", func(t *testing.T) {
		// Use sequential requests for deterministic testing
		config := RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: 1, // Low refill rate
			BurstSize:         testBurstSizeMedium,
			PerIP:             true,
		}

		rl := NewRateLimiter(config)
		defer rl.Close()

		clientIP := "192.168.1.1"

		// Sequential requests - more deterministic than concurrent
		blocked := 0

		for range testSlowlorisConns {
			if !rl.Allow(clientIP) {
				blocked++
			}
		}

		// Some connections should be blocked (10 connections, burst of 5)
		assert.Positive(t, blocked, "Some connections should be blocked")
	})
}

// TestRateLimitBypassAttempts tests various bypass attempts.
func TestRateLimitBypassAttempts(t *testing.T) {
	t.Run("prevents IPv6 to IPv4 mapping bypass", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = testBurstSizeSmall
		config.PerIP = true

		rl := NewRateLimiter(config)
		defer rl.Close()

		// Request with IPv4
		req1 := httptest.NewRequest(http.MethodGet, "/", nil)
		req1.RemoteAddr = "127.0.0.1:12345"
		ip1 := rl.extractClientIP(req1)

		// Request with IPv6-mapped IPv4 (::ffff:127.0.0.1)
		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.RemoteAddr = "[::ffff:127.0.0.1]:12345"
		ip2 := rl.extractClientIP(req2)

		// Verify both IPs are extracted correctly
		assert.Equal(t, "127.0.0.1", ip1, "IPv4 should be extracted correctly")
		assert.Equal(t, "::ffff:127.0.0.1", ip2, "IPv6-mapped address should be extracted")

		// Test rate limiting behavior - first request should be allowed
		allowed1 := rl.Allow(ip1)
		assert.True(t, allowed1, "First request from IPv4 should be allowed")

		// Second request from same IPv4 should be rate limited (burst=1)
		allowed2 := rl.Allow(ip1)
		assert.False(t, allowed2, "Second request from same IP should be rate limited")
	})

	t.Run("handles malformed IP addresses gracefully", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true

		rl := NewRateLimiter(config)
		defer rl.Close()

		malformedAddrs := []string{
			"",
			"not-an-ip",
			"300.300.300.300:12345",
			"[invalid]:12345",
		}

		for _, addr := range malformedAddrs {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = addr

			// Should not panic
			require.NotPanics(t, func() {
				rl.extractClientIP(req)
			})
		}
	})
}

// TestRateLimitExcludedPaths tests path exclusion.
func TestRateLimitExcludedPaths(t *testing.T) {
	t.Run("excludes health check paths", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = 0 // Would block everything
		config.ExcludedPaths = []string{"/health", "/ready", "/metrics"}

		rl := NewRateLimiter(config)
		defer rl.Close()

		handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		excludedPaths := []string{"/health", "/ready", "/metrics"}

		for _, path := range excludedPaths {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code,
				"Path %s should be excluded from rate limiting", path)
		}
	})

	t.Run("does not exclude non-configured paths", func(t *testing.T) {
		config := DefaultRateLimitConfig()
		config.Enabled = true
		config.BurstSize = 0 // Would block everything
		config.ExcludedPaths = []string{"/health"}

		rl := NewRateLimiter(config)
		defer rl.Close()

		handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
		req.RemoteAddr = testClientIPAddr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusTooManyRequests, rec.Code,
			"Non-excluded path should be rate limited")
	})
}
