package middleware

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/piwi3910/nebulaio/internal/audit"
)

// auditContextKey is a type for audit context keys to avoid collisions.
type auditContextKey string

const (
	// auditEventKey is the context key for the audit event.
	auditEventKey auditContextKey = "audit_event"
	// auditStartTimeKey is the context key for the request start time.
	auditStartTimeKey auditContextKey = "audit_start_time"
	// userIDKey is the context key for the user ID.
	userIDKey auditContextKey = "user_id"
	// usernameKey is the context key for the username.
	usernameKey auditContextKey = "username"
	// accessKeyIDKey is the context key for the access key ID.
	accessKeyIDKey auditContextKey = "access_key_id"
)

// AuditMiddleware creates middleware that captures request information for audit logging.
type AuditMiddleware struct {
	logger      *audit.AuditLogger
	eventSource audit.EventSource
}

// NewAuditMiddleware creates a new audit middleware.
func NewAuditMiddleware(logger *audit.AuditLogger, source audit.EventSource) *AuditMiddleware {
	return &AuditMiddleware{
		logger:      logger,
		eventSource: source,
	}
}

// responseWriter wraps http.ResponseWriter to capture response details.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)

	return n, err
}

// Unwrap returns the underlying ResponseWriter for compatibility with http.Flusher, etc.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// countingReader wraps an io.ReadCloser to count bytes read.
type countingReader struct {
	io.ReadCloser
	bytesRead int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytesRead += int64(n)

	return n, err
}

// Handler returns the middleware handler function.
func (m *AuditMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Wrap response writer to capture status and bytes
		rw := newResponseWriter(w)

		// Wrap request body to count bytes read
		cr := &countingReader{ReadCloser: r.Body}
		r.Body = cr

		// Create initial audit event
		event := audit.NewEvent("", m.eventSource, r.Method+" "+r.URL.Path)
		event.RequestID = middleware.GetReqID(r.Context())
		event.SourceIP = getClientIP(r)
		event.UserAgent = r.UserAgent()

		// Store start time in context
		ctx := context.WithValue(r.Context(), auditStartTimeKey, startTime)
		ctx = context.WithValue(ctx, auditEventKey, event)

		// Call the next handler
		next.ServeHTTP(rw, r.WithContext(ctx))

		// Complete the audit event
		duration := time.Since(startTime)
		event.DurationMS = duration.Milliseconds()
		event.BytesIn = cr.bytesRead
		event.BytesOut = rw.bytesWritten

		// Set result based on status code
		if rw.statusCode >= 400 {
			event.Result = audit.ResultFailure
			event.ErrorCode = http.StatusText(rw.statusCode)
		} else {
			event.Result = audit.ResultSuccess
		}

		// Get user identity from context if set
		if userID, ok := ctx.Value(userIDKey).(string); ok {
			event.UserIdentity.UserID = userID
		}

		if username, ok := ctx.Value(usernameKey).(string); ok {
			event.UserIdentity.Username = username
			event.UserIdentity.Type = audit.IdentityIAMUser
		}

		if accessKeyID, ok := ctx.Value(accessKeyIDKey).(string); ok {
			event.UserIdentity.AccessKeyID = accessKeyID
			event.UserIdentity.Type = audit.IdentityAccessKey
		}

		if event.UserIdentity.Type == "" {
			event.UserIdentity.Type = audit.IdentityAnonymous
		}

		// Only log if event type was set (meaning handler marked it for audit)
		if event.EventType != "" {
			m.logger.Log(event)
		}
	})
}

// SetAuditEvent updates the audit event in the request context.
func SetAuditEvent(r *http.Request, eventType audit.EventType, resource audit.ResourceInfo) {
	if event, ok := r.Context().Value(auditEventKey).(*audit.AuditEvent); ok {
		event.EventType = eventType
		event.Resource = resource
	}
}

// SetAuditEventType sets the event type for the current request.
func SetAuditEventType(r *http.Request, eventType audit.EventType) {
	if event, ok := r.Context().Value(auditEventKey).(*audit.AuditEvent); ok {
		event.EventType = eventType
	}
}

// SetAuditResource sets the resource info for the current request.
func SetAuditResource(r *http.Request, resource audit.ResourceInfo) {
	if event, ok := r.Context().Value(auditEventKey).(*audit.AuditEvent); ok {
		event.Resource = resource
	}
}

// SetAuditError sets the error information for the current request.
func SetAuditError(r *http.Request, errorCode, errorMessage string) {
	if event, ok := r.Context().Value(auditEventKey).(*audit.AuditEvent); ok {
		event.Result = audit.ResultFailure
		event.ErrorCode = errorCode
		event.ErrorMessage = errorMessage
	}
}

// SetAuditExtra adds extra metadata to the audit event.
func SetAuditExtra(r *http.Request, key, value string) {
	if event, ok := r.Context().Value(auditEventKey).(*audit.AuditEvent); ok {
		if event.Extra == nil {
			event.Extra = make(map[string]string)
		}

		event.Extra[key] = value
	}
}

// SetUserIdentity sets the user identity for audit logging.
func SetUserIdentity(ctx context.Context, userID, username, accessKeyID string) context.Context {
	if userID != "" {
		ctx = context.WithValue(ctx, userIDKey, userID)
	}

	if username != "" {
		ctx = context.WithValue(ctx, usernameKey, username)
	}

	if accessKeyID != "" {
		ctx = context.WithValue(ctx, accessKeyIDKey, accessKeyID)
	}

	return ctx
}

// GetAuditEvent retrieves the audit event from the request context.
func GetAuditEvent(r *http.Request) *audit.AuditEvent {
	if event, ok := r.Context().Value(auditEventKey).(*audit.AuditEvent); ok {
		return event
	}

	return nil
}

// getClientIP extracts the client IP address from the request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP from the list
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	xrip := r.Header.Get("X-Real-IP")
	if xrip != "" {
		return xrip
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// LogAuditEvent is a helper function to log an audit event directly.
func LogAuditEvent(logger *audit.AuditLogger, r *http.Request, eventType audit.EventType, source audit.EventSource, resource audit.ResourceInfo, result audit.Result, errorCode, errorMessage string, duration time.Duration) {
	event := audit.NewEvent(eventType, source, r.Method+" "+r.URL.Path)
	event.RequestID = middleware.GetReqID(r.Context())
	event.SourceIP = getClientIP(r)
	event.UserAgent = r.UserAgent()
	event.Resource = resource
	event.Result = result
	event.ErrorCode = errorCode
	event.ErrorMessage = errorMessage
	event.DurationMS = duration.Milliseconds()

	// Get user identity from headers (set by auth middleware)
	if userID := r.Header.Get("X-User-Id"); userID != "" {
		event.UserIdentity.UserID = userID
		event.UserIdentity.Type = audit.IdentityIAMUser
	}

	logger.Log(event)
}
