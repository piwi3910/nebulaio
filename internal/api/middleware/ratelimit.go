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
	// maxElapsedNanos caps the elapsed time to prevent integer overflow in token calculation.
	// One hour in nanoseconds is safe for multiplication with reasonable refill rates.
	maxElapsedNanos = int64(time.Hour)
)

// RateLimitConfig configures the rate limiting middleware.
type RateLimitConfig struct {
	// ExcludedPaths are paths that should not be rate limited (e.g., health checks).
	ExcludedPaths []string

	// TrustedProxies are IP addresses or CIDR ranges of trusted reverse proxies.
	// X-Forwarded-For and X-Real-IP headers are only trusted from these sources.
	// If empty, proxy headers are never trusted (prevents header spoofing attacks).
	TrustedProxies []string

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

	// Cap elapsed time to prevent integer overflow in token calculation.
	// This ensures elapsed * refillRate won't overflow even with high refill rates.
	if elapsed > maxElapsedNanos {
		elapsed = maxElapsedNanos
	}

	tokensToAdd := (elapsed * l.refillRate) / int64(time.Second)

	if tokensToAdd > 0 {
		if l.lastRefill.CompareAndSwap(lastRefill, now) {
			// Use CAS loop to safely add tokens without race conditions.
			// This prevents lost updates when multiple goroutines refill simultaneously.
			for {
				current := l.tokens.Load()

				newTokens := current + tokensToAdd
				if newTokens > l.maxTokens {
					newTokens = l.maxTokens
				}

				if l.tokens.CompareAndSwap(current, newTokens) {
					break
				}
			}
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
	stopCh           chan struct{}
	config           RateLimitConfig
	limiters         sync.Map   // map[string]*TokenBucketLimiter
	trustedProxyNets []*net.IPNet // Parsed trusted proxy CIDR ranges
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		config: config,
		stopCh: make(chan struct{}),
	}

	// Parse trusted proxy CIDR ranges
	for _, proxy := range config.TrustedProxies {
		// Try parsing as CIDR first
		_, ipNet, err := net.ParseCIDR(proxy)
		if err != nil {
			// Not a CIDR, try as single IP
			ip := net.ParseIP(proxy)
			if ip != nil {
				// Convert single IP to /32 (IPv4) or /128 (IPv6) CIDR
				if ip.To4() != nil {
					_, ipNet, _ = net.ParseCIDR(proxy + "/32")
				} else {
					_, ipNet, _ = net.ParseCIDR(proxy + "/128")
				}
			}
		}

		if ipNet != nil {
			rl.trustedProxyNets = append(rl.trustedProxyNets, ipNet)
		} else {
			log.Warn().Str("proxy", proxy).Msg("Invalid trusted proxy IP/CIDR, skipping")
		}
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

// isTrustedProxy checks if the given IP is from a trusted proxy.
func (rl *RateLimiter) isTrustedProxy(ipStr string) bool {
	if len(rl.trustedProxyNets) == 0 {
		return false
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	for _, ipNet := range rl.trustedProxyNets {
		if ipNet.Contains(ip) {
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

			// Extract client IP with trusted proxy validation
			ip := rl.extractClientIP(r)

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
// It only trusts X-Forwarded-For and X-Real-IP headers if the request
// comes from a configured trusted proxy to prevent header spoofing attacks.
func (rl *RateLimiter) extractClientIP(r *http.Request) string {
	// First, get the direct connection IP
	directIP := extractDirectIP(r.RemoteAddr)

	// Only trust proxy headers if the direct connection is from a trusted proxy
	if rl.isTrustedProxy(directIP) {
		// Check X-Forwarded-For header (for requests behind proxy)
		xff := r.Header.Get("X-Forwarded-For")
		if xff != "" {
			// Take the first IP in the chain (the original client)
			xff = strings.TrimSpace(xff)
			if idx := strings.Index(xff, ","); idx > 0 {
				clientIP := strings.TrimSpace(xff[:idx])
				if clientIP != "" {
					return clientIP
				}
			} else if xff != "" {
				return xff
			}
		}

		// Check X-Real-IP header
		xri := r.Header.Get("X-Real-IP")
		if xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	// Use direct connection IP (no trusted proxy or no proxy headers)
	return directIP
}

// extractDirectIP extracts the IP from a RemoteAddr (host:port format).
func extractDirectIP(remoteAddr string) string {
	// Handle IPv6 addresses with brackets like [::1]:port
	if strings.HasPrefix(remoteAddr, "[") {
		if idx := strings.LastIndex(remoteAddr, "]:"); idx > 0 {
			return remoteAddr[1:idx]
		}
	}

	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// RemoteAddr might not have a port (unusual but possible)
		// Try to parse as IP directly
		if net.ParseIP(remoteAddr) != nil {
			return remoteAddr
		}
		// Log warning for malformed RemoteAddr and return as-is
		log.Debug().Str("remoteAddr", remoteAddr).Msg("Could not parse RemoteAddr")

		return remoteAddr
	}

	return ip
}
