package middleware

import (
	"net/http"
)

// SecurityHeadersConfig holds configuration for security headers
type SecurityHeadersConfig struct {
	// ContentSecurityPolicy sets the Content-Security-Policy header
	// Default: "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'"
	ContentSecurityPolicy string

	// XFrameOptions sets the X-Frame-Options header
	// Default: "DENY"
	XFrameOptions string

	// XContentTypeOptions sets the X-Content-Type-Options header
	// Default: "nosniff"
	XContentTypeOptions string

	// XSSProtection sets the X-XSS-Protection header
	// Default: "1; mode=block"
	XSSProtection string

	// StrictTransportSecurity sets the Strict-Transport-Security header
	// Default: "max-age=31536000; includeSubDomains" (1 year)
	StrictTransportSecurity string

	// ReferrerPolicy sets the Referrer-Policy header
	// Default: "strict-origin-when-cross-origin"
	ReferrerPolicy string

	// PermissionsPolicy sets the Permissions-Policy header
	// Default: "geolocation=(), microphone=(), camera=()"
	PermissionsPolicy string

	// CacheControl sets the Cache-Control header for non-static resources
	// Default: "no-store, no-cache, must-revalidate"
	CacheControl string

	// EnableHSTS enables the Strict-Transport-Security header
	// Should be enabled when TLS is in use
	EnableHSTS bool
}

// DefaultSecurityHeadersConfig returns sensible defaults for security headers
func DefaultSecurityHeadersConfig() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		ContentSecurityPolicy:   "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'",
		XFrameOptions:           "DENY",
		XContentTypeOptions:     "nosniff",
		XSSProtection:           "1; mode=block",
		StrictTransportSecurity: "max-age=31536000; includeSubDomains",
		ReferrerPolicy:          "strict-origin-when-cross-origin",
		PermissionsPolicy:       "geolocation=(), microphone=(), camera=(), payment=()",
		CacheControl:            "no-store, no-cache, must-revalidate",
		EnableHSTS:              true,
	}
}

// SecurityHeaders returns a middleware that adds security headers to responses
func SecurityHeaders(cfg SecurityHeadersConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Prevent clickjacking
			if cfg.XFrameOptions != "" {
				w.Header().Set("X-Frame-Options", cfg.XFrameOptions)
			}

			// Prevent MIME type sniffing
			if cfg.XContentTypeOptions != "" {
				w.Header().Set("X-Content-Type-Options", cfg.XContentTypeOptions)
			}

			// XSS Protection (legacy, but still useful for older browsers)
			if cfg.XSSProtection != "" {
				w.Header().Set("X-XSS-Protection", cfg.XSSProtection)
			}

			// Content Security Policy
			if cfg.ContentSecurityPolicy != "" {
				w.Header().Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
			}

			// HTTP Strict Transport Security (only if TLS is enabled)
			if cfg.EnableHSTS && cfg.StrictTransportSecurity != "" {
				w.Header().Set("Strict-Transport-Security", cfg.StrictTransportSecurity)
			}

			// Referrer Policy
			if cfg.ReferrerPolicy != "" {
				w.Header().Set("Referrer-Policy", cfg.ReferrerPolicy)
			}

			// Permissions Policy (formerly Feature-Policy)
			if cfg.PermissionsPolicy != "" {
				w.Header().Set("Permissions-Policy", cfg.PermissionsPolicy)
			}

			// Cache control for API responses (not static assets)
			if cfg.CacheControl != "" && !isStaticAsset(r.URL.Path) {
				w.Header().Set("Cache-Control", cfg.CacheControl)
				w.Header().Set("Pragma", "no-cache")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isStaticAsset checks if the request is for a static asset
func isStaticAsset(path string) bool {
	staticExtensions := []string{".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".eot"}
	for _, ext := range staticExtensions {
		if len(path) > len(ext) && path[len(path)-len(ext):] == ext {
			return true
		}
	}
	return false
}

// S3SecurityHeaders returns a middleware with S3-appropriate security headers
// S3 has different requirements than the web console
func S3SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Prevent MIME type sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// S3 responses should not be framed
			w.Header().Set("X-Frame-Options", "DENY")

			// S3 API responses are typically not cached by default
			// Individual handlers may override this for specific caching scenarios
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Cache-Control", "no-store")
			}

			next.ServeHTTP(w, r)
		})
	}
}
