// Package middleware provides HTTP middleware for the NebulaIO API.
package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Default rate limiting configuration values.
const (
	defaultRequestsPerSecond = 100
	defaultBurstSize         = 50
	defaultStaleTimeoutMins  = 5
)

// RateLimitConfig configures the rate limiting middleware.
type RateLimitConfig struct {
	// ExcludedPaths are paths that should not be rate limited (e.g., health checks).
	ExcludedPaths []string

	// CleanupInterval is how often to clean up stale rate limiters.
	CleanupInterval time.Duration

	// StaleTimeout is how long a rate limiter can be unused before cleanup.
	StaleTimeout time.Duration

	// RequestsPerSecond is the default requests per second limit.
	RequestsPerSecond int

	// BurstSize is the maximum burst size.
	BurstSize int

	// Enabled enables rate limiting.
	Enabled bool

	// PerIP enables per-IP rate limiting.
	PerIP bool
}

// DefaultRateLimitConfig returns sensible defaults for rate limiting.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Enabled:           false,
		RequestsPerSecond: defaultRequestsPerSecond,
		BurstSize:         defaultBurstSize,
		PerIP:             true,
		CleanupInterval:   time.Minute,
		StaleTimeout:      defaultStaleTimeoutMins * time.Minute,
		ExcludedPaths:     []string{"/health", "/ready", "/metrics"},
	}
}

// TokenBucketLimiter implements a token bucket rate limiter.
type TokenBucketLimiter struct {
	tokens     atomic.Int64
	lastRefill atomic.Int64
	lastUsed   atomic.Int64
	maxTokens  int64
	refillRate int64 // tokens per second
}

// NewTokenBucketLimiter creates a new token bucket rate limiter.
func NewTokenBucketLimiter(rps, burst int) *TokenBucketLimiter {
	l := &TokenBucketLimiter{
		maxTokens:  int64(burst),
		refillRate: int64(rps),
	}
	l.tokens.Store(int64(burst))
	l.lastRefill.Store(time.Now().UnixNano())
	l.lastUsed.Store(time.Now().UnixNano())

	return l
}

// Allow checks if a request is allowed under the rate limit.
func (l *TokenBucketLimiter) Allow() bool {
	now := time.Now().UnixNano()
	l.lastUsed.Store(now)

	// Refill tokens based on time elapsed
	lastRefill := l.lastRefill.Load()
	elapsed := now - lastRefill
	tokensToAdd := (elapsed * l.refillRate) / int64(time.Second)

	if tokensToAdd > 0 {
		if l.lastRefill.CompareAndSwap(lastRefill, now) {
			current := l.tokens.Load()

			newTokens := current + tokensToAdd
			if newTokens > l.maxTokens {
				newTokens = l.maxTokens
			}

			l.tokens.Store(newTokens)
		}
	}

	// Try to consume a token
	for {
		current := l.tokens.Load()
		if current <= 0 {
			return false
		}

		if l.tokens.CompareAndSwap(current, current-1) {
			return true
		}
	}
}

// RateLimiter manages per-IP rate limiters.
//
//nolint:govet // sync.Map has internal alignment requirements that prevent optimization
type RateLimiter struct {
	stopCh   chan struct{}
	config   RateLimitConfig
	limiters sync.Map // map[string]*TokenBucketLimiter
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		config: config,
		stopCh: make(chan struct{}),
	}

	if config.Enabled && config.PerIP {
		go rl.cleanupLoop()
	}

	return rl
}

// cleanupLoop periodically removes stale rate limiters.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

// cleanup removes stale rate limiters.
func (rl *RateLimiter) cleanup() {
	now := time.Now().UnixNano()
	staleTimeout := rl.config.StaleTimeout.Nanoseconds()

	rl.limiters.Range(func(key, value interface{}) bool {
		limiter := value.(*TokenBucketLimiter)

		lastUsed := limiter.lastUsed.Load()
		if now-lastUsed > staleTimeout {
			rl.limiters.Delete(key)
		}

		return true
	})
}

// Close stops the cleanup goroutine.
func (rl *RateLimiter) Close() {
	close(rl.stopCh)
}

// Allow checks if a request from the given IP is allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	if !rl.config.Enabled {
		return true
	}

	if !rl.config.PerIP {
		// Global rate limiting - use empty key
		ip = ""
	}

	limiterI, loaded := rl.limiters.LoadOrStore(ip, NewTokenBucketLimiter(
		rl.config.RequestsPerSecond,
		rl.config.BurstSize,
	))
	limiter := limiterI.(*TokenBucketLimiter)

	if !loaded {
		log.Debug().Str("ip", ip).Msg("Created new rate limiter")
	}

	return limiter.Allow()
}

// isExcludedPath checks if a path should be excluded from rate limiting.
func (rl *RateLimiter) isExcludedPath(path string) bool {
	for _, excluded := range rl.config.ExcludedPaths {
		if path == excluded {
			return true
		}
	}

	return false
}

// RateLimitMiddleware returns a middleware that applies rate limiting.
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Skip rate limiting for excluded paths
			if rl.isExcludedPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Extract client IP
			ip := extractClientIP(r)

			if !rl.Allow(ip) {
				log.Warn().
					Str("ip", ip).
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("Rate limit exceeded")

				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractClientIP extracts the client IP from the request.
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for requests behind proxy)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP in the chain
		if idx := strings.Index(xff, ","); idx > 0 {
			return xff[:idx]
		}

		return xff
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}
