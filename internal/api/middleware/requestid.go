// Package middleware provides HTTP middleware for the S3 API.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync/atomic"
	"time"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey int

const (
	// requestIDKey is the context key for the request ID.
	requestIDKey contextKey = iota
)

// Request ID generation constants.
const (
	requestIDBufferSize  = 16
	extendedIDBufferSize = 48
	// Bit shift positions for timestamp encoding.
	shiftBits40 = 40
	shiftBits32 = 32
	shiftBits24 = 24
	shiftBits16 = 16
	shiftBits8  = 8
)

// requestCounter provides a monotonically increasing counter for request IDs.
var requestCounter uint64

// RequestID is a middleware that generates a unique request ID for each request.
// The request ID is added to the response headers and made available in the context.
// The format follows AWS S3's x-amz-request-id header format.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if request ID was already set (e.g., from a load balancer)
		requestID := r.Header.Get("X-Amz-Request-Id")
		if requestID == "" {
			requestID = generateRequestID()
		}

		// Add request ID to response headers
		w.Header().Set("X-Amz-Request-Id", requestID)
		w.Header().Set("X-Amz-Id-2", generateExtendedRequestID())

		// Add request ID to context
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from the context.
// Returns an empty string if no request ID is present.
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}

	return ""
}

// generateRequestID generates a unique request ID.
// The format is designed to be similar to AWS S3's request ID format:
// - 16 characters of base64-encoded random bytes
// - Includes timestamp component for sortability.
func generateRequestID() string {
	// Get current counter value and increment
	counter := atomic.AddUint64(&requestCounter, 1)

	// Get timestamp in milliseconds
	//nolint:gosec // G115: timestamp is always positive for current time
	timestamp := uint64(time.Now().UnixNano() / int64(time.Millisecond))

	// Create a buffer with timestamp, counter, and random bytes
	buf := make([]byte, requestIDBufferSize)

	// First 6 bytes: timestamp (48 bits, covers ~8900 years)
	buf[0] = byte(timestamp >> shiftBits40)
	buf[1] = byte(timestamp >> shiftBits32)
	buf[2] = byte(timestamp >> shiftBits24)
	buf[3] = byte(timestamp >> shiftBits16)
	buf[4] = byte(timestamp >> shiftBits8)
	buf[5] = byte(timestamp)

	// Next 4 bytes: counter
	buf[6] = byte(counter >> shiftBits24)
	buf[7] = byte(counter >> shiftBits16)
	buf[8] = byte(counter >> shiftBits8)
	buf[9] = byte(counter)

	// Last 6 bytes: random
	rand.Read(buf[10:])

	// Encode as URL-safe base64 without padding
	return base64.RawURLEncoding.EncodeToString(buf)
}

// generateExtendedRequestID generates the extended request ID (x-amz-id-2).
// This is typically a longer identifier used for troubleshooting.
func generateExtendedRequestID() string {
	buf := make([]byte, extendedIDBufferSize)
	rand.Read(buf)

	return base64.StdEncoding.EncodeToString(buf)
}

// RequestIDFromHeader extracts the request ID from response headers.
// This is useful for logging and debugging.
func RequestIDFromHeader(h http.Header) string {
	return h.Get("X-Amz-Request-Id")
}

// ExtendedRequestIDFromHeader extracts the extended request ID from response headers.
func ExtendedRequestIDFromHeader(h http.Header) string {
	return h.Get("X-Amz-Id-2")
}

// RequestLogger is a middleware that logs request information including the request ID.
// This should be used after RequestID middleware.
type RequestLogger struct {
	// Logger is a function that receives log messages.
	// If nil, no logging is performed.
	Logger func(requestID, method, path string, statusCode int, duration time.Duration)
}

// responseRecorder wraps http.ResponseWriter to capture the status code.
type responseRecorder struct {
	http.ResponseWriter

	statusCode int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

// Middleware returns the logging middleware handler.
func (rl *RequestLogger) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		rec := &responseRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		// Log if logger is configured
		if rl.Logger != nil {
			requestID := GetRequestID(r.Context())
			duration := time.Since(start)
			rl.Logger(requestID, r.Method, r.URL.Path, rec.statusCode, duration)
		}
	})
}

// SetRequestID sets a request ID in the context.
// This is useful for testing or when request ID needs to be injected.
func SetRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// NewRequestIDGenerator creates a custom request ID generator function.
// The prefix is prepended to all generated request IDs.
func NewRequestIDGenerator(prefix string) func() string {
	return func() string {
		return prefix + generateRequestID()
	}
}
