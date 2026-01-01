package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/piwi3910/nebulaio/internal/metrics"
)

// HTTP method and operation constants.
const (
	methodGET           = "GET"
	operationUnknown    = "Unknown"
)

// MetricsMiddleware records request metrics.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Track active connections
		metrics.IncrementActiveConnections()
		defer metrics.DecrementActiveConnections()

		// Track bytes received (Content-Length header)
		if r.ContentLength > 0 {
			metrics.AddBytesReceived(r.ContentLength)
		}

		// Wrap response writer to capture status code and bytes written
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

		// Call the next handler
		next.ServeHTTP(ww, r)

		// Record duration
		duration := time.Since(start)

		// Get operation name from the request
		operation := extractS3Operation(r)

		// Get bucket name if available
		bucket := chi.URLParam(r, "bucket")

		// Record metrics
		metrics.RecordRequest(r.Method, operation, ww.Status(), duration)

		// Record S3 operation if bucket is available
		if bucket != "" {
			metrics.RecordS3Operation(operation, bucket)
		}

		// Track bytes sent
		if ww.BytesWritten() > 0 {
			metrics.AddBytesSent(int64(ww.BytesWritten()))
		}

		// Record errors
		if ww.Status() >= http.StatusBadRequest {
			errorType := getErrorType(ww.Status())
			metrics.RecordError(operation, errorType)
		}
	})
}

// extractS3Operation extracts the S3 operation name from the request.
func extractS3Operation(r *http.Request) string {
	method := r.Method
	path := r.URL.Path

	// Service-level operations
	if path == "/" && method == methodGET {
		return "ListBuckets"
	}

	// Check for bucket-level vs object-level
	parts := strings.Split(strings.Trim(path, "/"), "/")

	const (
		minBucketParts = 1
		minKeyParts    = 2
	)

	hasBucket := len(parts) >= minBucketParts && parts[0] != ""
	hasKey := len(parts) >= minKeyParts && parts[1] != ""

	// Bucket-level operations
	if hasBucket && !hasKey {
		return extractBucketOperation(r)
	}

	// Object-level operations
	if hasBucket && hasKey {
		return extractObjectOperation(r)
	}

	return operationUnknown
}

// extractBucketOperation extracts bucket-level operation name.
func extractBucketOperation(r *http.Request) string {
	method := r.Method
	query := r.URL.Query()

	switch method {
	case "PUT":
		return "CreateBucket"
	case "DELETE":
		return "DeleteBucket"
	case "HEAD":
		return "HeadBucket"
	case methodGET:
		return extractBucketGetOperation(query)
	default:
		return operationUnknown
	}
}

// extractBucketGetOperation extracts bucket GET operation based on query params.
func extractBucketGetOperation(query map[string][]string) string {
	if _, ok := query["versioning"]; ok {
		return "GetBucketVersioning"
	}

	if _, ok := query["policy"]; ok {
		return "GetBucketPolicy"
	}

	if _, ok := query["tagging"]; ok {
		return "GetBucketTagging"
	}

	if _, ok := query["cors"]; ok {
		return "GetBucketCORS"
	}

	if _, ok := query["lifecycle"]; ok {
		return "GetBucketLifecycle"
	}

	if _, ok := query["uploads"]; ok {
		return "ListMultipartUploads"
	}

	if _, ok := query["location"]; ok {
		return "GetBucketLocation"
	}

	if _, ok := query["acl"]; ok {
		return "GetBucketAcl"
	}

	return "ListObjectsV2"
}

// extractObjectOperation extracts object-level operation name.
func extractObjectOperation(r *http.Request) string {
	method := r.Method
	query := r.URL.Query()

	switch method {
	case "PUT":
		return extractObjectPutOperation(r, query)
	case methodGET:
		return extractObjectGetOperation(query)
	case "DELETE":
		return extractObjectDeleteOperation(query)
	case "HEAD":
		return "HeadObject"
	case "POST":
		return extractObjectPostOperation(query)
	default:
		return operationUnknown
	}
}

// extractObjectPutOperation extracts object PUT operation.
func extractObjectPutOperation(r *http.Request, query map[string][]string) string {
	if _, ok := query["partNumber"]; ok {
		return "UploadPart"
	}

	if r.Header.Get("X-Amz-Copy-Source") != "" {
		return "CopyObject"
	}

	return "PutObject"
}

// extractObjectGetOperation extracts object GET operation.
func extractObjectGetOperation(query map[string][]string) string {
	if _, ok := query["uploadId"]; ok {
		return "ListParts"
	}

	if _, ok := query["acl"]; ok {
		return "GetObjectAcl"
	}

	if _, ok := query["tagging"]; ok {
		return "GetObjectTagging"
	}

	return "GetObject"
}

// extractObjectDeleteOperation extracts object DELETE operation.
func extractObjectDeleteOperation(query map[string][]string) string {
	if _, ok := query["uploadId"]; ok {
		return "AbortMultipartUpload"
	}

	return "DeleteObject"
}

// extractObjectPostOperation extracts object POST operation.
func extractObjectPostOperation(query map[string][]string) string {
	if _, ok := query["uploads"]; ok {
		return "CreateMultipartUpload"
	}

	if _, ok := query["uploadId"]; ok {
		return "CompleteMultipartUpload"
	}

	if _, ok := query["delete"]; ok {
		return "DeleteObjects"
	}

	return "PostObject"
}

// getErrorType returns an error type string based on HTTP status code.
func getErrorType(status int) string {
	switch {
	case status == http.StatusBadRequest:
		return "BadRequest"
	case status == http.StatusUnauthorized:
		return "Unauthorized"
	case status == http.StatusForbidden:
		return "Forbidden"
	case status == http.StatusNotFound:
		return "NotFound"
	case status == http.StatusMethodNotAllowed:
		return "MethodNotAllowed"
	case status == http.StatusConflict:
		return "Conflict"
	case status == http.StatusInternalServerError:
		return "InternalError"
	case status == http.StatusNotImplemented:
		return "NotImplemented"
	case status == http.StatusServiceUnavailable:
		return "ServiceUnavailable"
	case status >= http.StatusBadRequest && status < http.StatusInternalServerError:
		return "ClientError"
	case status >= http.StatusInternalServerError:
		return "ServerError"
	default:
		return operationUnknown
	}
}

// ByteCountingResponseWriter wraps http.ResponseWriter to count bytes written.
type ByteCountingResponseWriter struct {
	http.ResponseWriter

	bytesWritten int64
	statusCode   int
}

// NewByteCountingResponseWriter creates a new ByteCountingResponseWriter.
func NewByteCountingResponseWriter(w http.ResponseWriter) *ByteCountingResponseWriter {
	return &ByteCountingResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

// Write writes data and counts bytes.
func (w *ByteCountingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)

	return n, err
}

// WriteHeader captures the status code.
func (w *ByteCountingResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// BytesWritten returns the number of bytes written.
func (w *ByteCountingResponseWriter) BytesWritten() int64 {
	return w.bytesWritten
}

// StatusCode returns the HTTP status code.
func (w *ByteCountingResponseWriter) StatusCode() int {
	return w.statusCode
}

// ContentLengthFromHeader extracts content length from request header.
func ContentLengthFromHeader(r *http.Request) int64 {
	if cl := r.Header.Get("Content-Length"); cl != "" {
		length, err := strconv.ParseInt(cl, 10, 64)
		if err == nil {
			return length
		}
	}

	return 0
}
