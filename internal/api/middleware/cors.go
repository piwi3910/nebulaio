package middleware

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/bucket"
	"github.com/piwi3910/nebulaio/internal/metadata"
)

// S3CORSMiddleware handles S3-specific CORS for bucket requests
// This is different from the admin API CORS which allows all origins.
type S3CORSMiddleware struct {
	bucketService *bucket.Service
}

// NewS3CORSMiddleware creates a new S3 CORS middleware.
func NewS3CORSMiddleware(bucketService *bucket.Service) *S3CORSMiddleware {
	return &S3CORSMiddleware{
		bucketService: bucketService,
	}
}

// Handler returns the middleware handler function.
func (m *S3CORSMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// If no origin header, this is not a CORS request
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Extract bucket name from URL
		bucketName := chi.URLParam(r, "bucket")
		if bucketName == "" {
			// For root-level operations (like ListBuckets), allow CORS with defaults
			m.setDefaultCORSHeaders(w, origin, r.Method)

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)

			return
		}

		// Get bucket's CORS configuration
		rules, err := m.bucketService.GetCORS(r.Context(), bucketName)
		if err != nil {
			// No CORS configuration - allow request to proceed but don't set CORS headers
			// This means the browser will reject cross-origin responses
			if strings.Contains(err.Error(), "no CORS") {
				next.ServeHTTP(w, r)
				return
			}
			// Bucket not found or other error - let the request proceed to handler
			next.ServeHTTP(w, r)

			return
		}

		// Handle preflight request (OPTIONS)
		if r.Method == http.MethodOptions {
			m.handlePreflight(w, r, rules, origin)
			return
		}

		// Handle actual request
		m.handleActualRequest(w, r, next, rules, origin)
	})
}

// handlePreflight handles CORS preflight OPTIONS requests.
func (m *S3CORSMiddleware) handlePreflight(w http.ResponseWriter, r *http.Request, rules []metadata.CORSRule, origin string) {
	// Get the requested method from Access-Control-Request-Method header
	requestMethod := r.Header.Get("Access-Control-Request-Method")
	if requestMethod == "" {
		// Not a valid preflight request
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get requested headers
	requestHeaders := r.Header.Get("Access-Control-Request-Headers")

	// Find a matching CORS rule
	rule := m.bucketService.FindMatchingCORSRule(rules, origin, requestMethod)
	if rule == nil {
		// No matching rule - return 403
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Check if requested headers are allowed
	if requestHeaders != "" {
		if !m.areHeadersAllowed(rule.AllowedHeaders, requestHeaders) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}

	// Set CORS preflight response headers
	allowedOrigin, _ := bucket.MatchCORSOrigin(rule.AllowedOrigins, origin)
	w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(rule.AllowedMethods, ", "))

	if len(rule.AllowedHeaders) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(rule.AllowedHeaders, ", "))
	}

	if rule.MaxAgeSeconds > 0 {
		w.Header().Set("Access-Control-Max-Age", strconv.Itoa(rule.MaxAgeSeconds))
	}

	// Vary header is important for caching
	w.Header().Add("Vary", "Origin")
	w.Header().Add("Vary", "Access-Control-Request-Method")
	w.Header().Add("Vary", "Access-Control-Request-Headers")

	w.WriteHeader(http.StatusOK)
}

// handleActualRequest handles the actual CORS request (not preflight).
func (m *S3CORSMiddleware) handleActualRequest(w http.ResponseWriter, r *http.Request, next http.Handler, rules []metadata.CORSRule, origin string) {
	// Find a matching CORS rule
	rule := m.bucketService.FindMatchingCORSRule(rules, origin, r.Method)
	if rule == nil {
		// No matching rule - proceed without CORS headers
		// The browser will reject the response
		next.ServeHTTP(w, r)
		return
	}

	// Set CORS response headers
	allowedOrigin, _ := bucket.MatchCORSOrigin(rule.AllowedOrigins, origin)
	w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)

	if len(rule.ExposeHeaders) > 0 {
		w.Header().Set("Access-Control-Expose-Headers", strings.Join(rule.ExposeHeaders, ", "))
	}

	// Vary header is important for caching
	w.Header().Add("Vary", "Origin")

	// S3 specific headers that should be exposed
	s3ExposeHeaders := []string{
		"ETag",
		"X-Amz-Version-Id",
		"X-Amz-Delete-Marker",
		"X-Amz-Request-Id",
	}

	// Merge S3-specific headers with user-defined expose headers
	currentExpose := w.Header().Get("Access-Control-Expose-Headers")
	if currentExpose != "" {
		allHeaders := make([]string, 0, len(s3ExposeHeaders)+1)
		allHeaders = append(allHeaders, s3ExposeHeaders...)
		allHeaders = append(allHeaders, strings.Split(currentExpose, ", ")...)
		w.Header().Set("Access-Control-Expose-Headers", strings.Join(unique(allHeaders), ", "))
	} else {
		w.Header().Set("Access-Control-Expose-Headers", strings.Join(s3ExposeHeaders, ", "))
	}

	next.ServeHTTP(w, r)
}

// areHeadersAllowed checks if all requested headers are allowed by the CORS rule.
func (m *S3CORSMiddleware) areHeadersAllowed(allowedHeaders []string, requestedHeaders string) bool {
	if len(allowedHeaders) == 0 {
		// No headers allowed but headers were requested
		return requestedHeaders == ""
	}

	// Check for wildcard
	if slices.Contains(allowedHeaders, "*") {
		return true
	}

	// Parse requested headers
	for reqHeader := range strings.SplitSeq(requestedHeaders, ",") {
		reqHeader = strings.TrimSpace(strings.ToLower(reqHeader))
		if reqHeader == "" {
			continue
		}

		found := false

		for _, allowed := range allowedHeaders {
			if strings.ToLower(allowed) == reqHeader {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}

// setDefaultCORSHeaders sets minimal CORS headers for non-bucket operations.
func (m *S3CORSMiddleware) setDefaultCORSHeaders(w http.ResponseWriter, origin, method string) {
	// For root operations (like ListBuckets), we can allow based on origin
	// In production, this might be more restrictive
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Amz-Date, X-Amz-Content-SHA256")
	w.Header().Set("Access-Control-Expose-Headers", "ETag, X-Amz-Request-Id")
	w.Header().Set("Access-Control-Max-Age", "3600")
	w.Header().Add("Vary", "Origin")
}

// unique returns a slice with duplicate strings removed.
func unique(strs []string) []string {
	seen := make(map[string]bool)

	result := make([]string, 0, len(strs))
	for _, s := range strs {
		lower := strings.ToLower(s)
		if !seen[lower] {
			seen[lower] = true

			result = append(result, s)
		}
	}

	return result
}

// SimpleS3CORSHeaders adds basic CORS headers to responses
// This is a simpler middleware that can be used when bucket-specific CORS is not needed.
type SimpleS3CORSHeaders struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

// DefaultSimpleS3CORSHeaders returns default CORS headers suitable for S3 operations.
func DefaultSimpleS3CORSHeaders() *SimpleS3CORSHeaders {
	return &SimpleS3CORSHeaders{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "HEAD", "PUT", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Authorization",
			"Content-Type",
			"Content-Length",
			"Content-MD5",
			"X-Amz-Date",
			"X-Amz-Content-SHA256",
			"X-Amz-Security-Token",
			"X-Amz-User-Agent",
			"X-Amz-Acl",
			"X-Amz-Copy-Source",
			"X-Amz-Metadata-Directive",
			"X-Amz-Storage-Class",
		},
		ExposedHeaders: []string{
			"ETag",
			"Content-Length",
			"Content-Type",
			"X-Amz-Version-Id",
			"X-Amz-Delete-Marker",
			"X-Amz-Request-Id",
			"X-Amz-Id-2",
		},
		AllowCredentials: false,
		MaxAge:           86400, //nolint:mnd // 24 hours in seconds, standard CORS max age
	}
}

// Handler returns the middleware handler for simple CORS.
func (c *SimpleS3CORSHeaders) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Check if origin is allowed
		allowedOrigin := c.getAllowedOrigin(origin)
		if allowedOrigin == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Handle preflight
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(c.AllowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(c.AllowedHeaders, ", "))

			if c.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if c.MaxAge > 0 {
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(c.MaxAge))
			}

			w.Header().Add("Vary", "Origin")
			w.WriteHeader(http.StatusOK)

			return
		}

		// Set headers for actual request
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)

		if len(c.ExposedHeaders) > 0 {
			w.Header().Set("Access-Control-Expose-Headers", strings.Join(c.ExposedHeaders, ", "))
		}

		if c.AllowCredentials {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		w.Header().Add("Vary", "Origin")

		next.ServeHTTP(w, r)
	})
}

// getAllowedOrigin returns the allowed origin or empty string if not allowed.
func (c *SimpleS3CORSHeaders) getAllowedOrigin(requestOrigin string) string {
	for _, allowed := range c.AllowedOrigins {
		if allowed == "*" {
			return "*"
		}

		if allowed == requestOrigin {
			return requestOrigin
		}
		// Wildcard matching
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:]
			if strings.HasSuffix(requestOrigin, suffix) {
				return requestOrigin
			}
		}
	}

	return ""
}
