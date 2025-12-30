package middleware

import (
	"context"
	"net/http"

	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/rs/zerolog/log"
)

// S3AuthContextKey is the context key for S3 auth information.
type S3AuthContextKey struct{}

// S3AuthInfo contains authentication information for an S3 request.
type S3AuthInfo struct {
	AccessKeyID string
	UserID      string
	Username    string
	IsAnonymous bool
}

// S3AuthConfig configures the S3 authentication middleware.
type S3AuthConfig struct {
	// AuthService is the authentication service for validating credentials
	AuthService *auth.Service
	// Region is the default region for signature validation
	Region string
	// AllowAnonymous allows requests without authentication (for public buckets)
	AllowAnonymous bool
}

// S3Auth creates a middleware that handles S3 API authentication.
// It supports both AWS Signature Version 4 Authorization header and presigned URLs.
func S3Auth(cfg S3AuthConfig) func(http.Handler) http.Handler {
	validator := auth.NewSignatureValidator(cfg.AuthService, cfg.Region)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Check if this is a presigned URL request
			isPresigned := auth.IsPresignedRequest(r)

			// Check if there's an Authorization header
			hasAuthHeader := r.Header.Get("Authorization") != ""

			// If no authentication is present
			if !isPresigned && !hasAuthHeader {
				if cfg.AllowAnonymous {
					// Allow anonymous access
					authInfo := &S3AuthInfo{
						IsAnonymous: true,
					}
					ctx = context.WithValue(ctx, S3AuthContextKey{}, authInfo)
					next.ServeHTTP(w, r.WithContext(ctx))

					return
				}

				// Require authentication
				writeS3Error(w, "AccessDenied", "No authentication provided", http.StatusForbidden)

				return
			}

			// Validate the request
			result, err := validator.ValidateRequest(ctx, r)
			if err != nil {
				log.Debug().
					Err(err).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Bool("presigned", isPresigned).
					Msg("S3 authentication failed")

				// Determine the appropriate error code
				errorCode := "SignatureDoesNotMatch"
				statusCode := http.StatusForbidden

				if isPresigned {
					// Check for specific presigned URL errors
					errStr := err.Error()
					switch {
					case contains(errStr, "expired"):
						errorCode = "AccessDenied"
						statusCode = http.StatusForbidden
					case contains(errStr, "invalid access key"):
						errorCode = "InvalidAccessKeyId"
						statusCode = http.StatusForbidden
					case contains(errStr, "signature"):
						errorCode = "SignatureDoesNotMatch"
						statusCode = http.StatusForbidden
					}
				}

				writeS3Error(w, errorCode, err.Error(), statusCode)

				return
			}

			// Set auth info in context
			authInfo := &S3AuthInfo{
				AccessKeyID: result.AccessKeyID,
				UserID:      result.UserID,
				Username:    result.Username,
				IsAnonymous: false,
			}

			ctx = context.WithValue(ctx, S3AuthContextKey{}, authInfo)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetS3AuthInfo retrieves the S3 auth info from the request context.
func GetS3AuthInfo(ctx context.Context) *S3AuthInfo {
	if info, ok := ctx.Value(S3AuthContextKey{}).(*S3AuthInfo); ok {
		return info
	}

	return nil
}

// GetOwnerID returns the owner ID for S3 operations.
// Returns the user ID if authenticated, or "anonymous" if not authenticated.
func GetOwnerID(ctx context.Context) string {
	info := GetS3AuthInfo(ctx)
	if info != nil && !info.IsAnonymous && info.UserID != "" {
		return info.UserID
	}

	return "anonymous"
}

// GetOwnerDisplayName returns the owner display name for S3 operations.
// Returns the username if authenticated, or "anonymous" if not authenticated.
func GetOwnerDisplayName(ctx context.Context) string {
	info := GetS3AuthInfo(ctx)
	if info != nil && !info.IsAnonymous && info.Username != "" {
		return info.Username
	}

	return "anonymous"
}

// RequireS3Auth is a middleware that requires valid S3 authentication.
func RequireS3Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := GetS3AuthInfo(r.Context())
		if info == nil || info.IsAnonymous {
			writeS3Error(w, "AccessDenied", "Authentication required", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// contains is a helper to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

// writeS3Error writes an S3-formatted XML error response.
func writeS3Error(w http.ResponseWriter, code, message string, status int) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)

	response := `<?xml version="1.0" encoding="UTF-8"?>
<Error>
    <Code>` + code + `</Code>
    <Message>` + escapeXML(message) + `</Message>
</Error>`

	w.Write([]byte(response))
}

// escapeXML escapes special XML characters.
func escapeXML(s string) string {
	result := s

	replacements := map[string]string{
		"&":  "&amp;",
		"<":  "&lt;",
		">":  "&gt;",
		"'":  "&apos;",
		"\"": "&quot;",
	}
	for old, new := range replacements {
		result = replaceAll(result, old, new)
	}

	return result
}

// replaceAll replaces all occurrences of old with replacement in s.
func replaceAll(s, old, replacement string) string {
	if old == replacement || old == "" {
		return s
	}

	var result []byte

	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result = append(result, replacement...)
			i += len(old)
		} else {
			result = append(result, s[i])
			i++
		}
	}

	return string(result)
}
