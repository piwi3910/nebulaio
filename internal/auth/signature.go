package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Signature validation constants.
const (
	regexMatchParts    = 2  // Expected number of parts in a regex match (full match + capture group)
	signatureCredParts = 5  // Number of parts in AWS Signature credential
	timeSkewMinutes    = 15 // Maximum allowed time difference in minutes
)

// SignatureValidator validates AWS Signature Version 4 requests.
type SignatureValidator struct {
	authService *Service
	region      string
}

// NewSignatureValidator creates a new signature validator.
func NewSignatureValidator(authService *Service, region string) *SignatureValidator {
	if region == "" {
		region = "us-east-1"
	}

	return &SignatureValidator{
		authService: authService,
		region:      region,
	}
}

// AuthorizationHeader represents a parsed Authorization header.
type AuthorizationHeader struct {
	Algorithm     string
	Credential    string
	Signature     string
	AccessKeyID   string
	DateStamp     string
	Region        string
	Service       string
	SignedHeaders []string
}

// ValidationResult contains the result of signature validation.
type ValidationResult struct {
	AccessKeyID string
	UserID      string
	Username    string
	IsValid     bool
}

// ValidateRequest validates an incoming S3 request using AWS Signature Version 4.
func (v *SignatureValidator) ValidateRequest(ctx context.Context, r *http.Request) (*ValidationResult, error) {
	// Check if this is a presigned URL request
	if IsPresignedRequest(r) {
		return v.validatePresignedRequest(ctx, r)
	}

	// Otherwise, validate using Authorization header
	return v.validateAuthorizationHeader(ctx, r)
}

// validatePresignedRequest validates a presigned URL request.
func (v *SignatureValidator) validatePresignedRequest(ctx context.Context, r *http.Request) (*ValidationResult, error) {
	// Parse presigned URL parameters
	info, err := ParsePresignedURL(r)
	if err != nil {
		return nil, fmt.Errorf("invalid presigned URL: %w", err)
	}

	// Check expiration
	err = ValidatePresignedExpiration(info)
	if err != nil {
		return nil, err
	}

	// Get the secret key for this access key
	accessKey, user, err := v.authService.ValidateAccessKey(ctx, info.AccessKeyID)
	if err != nil {
		return nil, fmt.Errorf("invalid access key: %w", err)
	}

	// Validate the signature
	err = ValidatePresignedSignature(r, info, accessKey.SecretAccessKey)
	if err != nil {
		return nil, fmt.Errorf("signature validation failed: %w", err)
	}

	return &ValidationResult{
		AccessKeyID: info.AccessKeyID,
		UserID:      user.ID,
		Username:    user.Username,
		IsValid:     true,
	}, nil
}

// validateAuthorizationHeader validates an Authorization header.
func (v *SignatureValidator) validateAuthorizationHeader(ctx context.Context, r *http.Request) (*ValidationResult, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, errors.New("missing Authorization header")
	}

	// Parse the Authorization header
	auth, err := parseAuthorizationHeader(authHeader)
	if err != nil {
		return nil, fmt.Errorf("invalid Authorization header: %w", err)
	}

	// Get the secret key for this access key
	accessKey, user, err := v.authService.ValidateAccessKey(ctx, auth.AccessKeyID)
	if err != nil {
		return nil, fmt.Errorf("invalid access key: %w", err)
	}

	// Get the X-Amz-Date or Date header
	amzDate := r.Header.Get("X-Amz-Date")
	if amzDate == "" {
		amzDate = r.Header.Get("Date")
	}

	if amzDate == "" {
		return nil, errors.New("missing date header")
	}

	// Parse the date
	var date time.Time

	date, err = time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		// Try parsing HTTP date format
		date, err = time.Parse(http.TimeFormat, amzDate)
		if err != nil {
			return nil, fmt.Errorf("invalid date format: %w", err)
		}
	}

	// Check if the request is within the allowed time window (15 minutes)
	now := time.Now().UTC()

	timeDiff := now.Sub(date)
	if timeDiff < -timeSkewMinutes*time.Minute || timeDiff > timeSkewMinutes*time.Minute {
		return nil, errors.New("request time too skewed")
	}

	// Validate the signature
	err = v.validateHeaderSignature(r, auth, accessKey.SecretAccessKey, amzDate)
	if err != nil {
		return nil, fmt.Errorf("signature validation failed: %w", err)
	}

	return &ValidationResult{
		AccessKeyID: auth.AccessKeyID,
		UserID:      user.ID,
		Username:    user.Username,
		IsValid:     true,
	}, nil
}

// validateHeaderSignature validates the signature from Authorization header.
func (v *SignatureValidator) validateHeaderSignature(r *http.Request, auth *AuthorizationHeader, secretKey, amzDate string) error {
	// Extract date stamp from amzDate
	dateStamp := auth.DateStamp
	if dateStamp == "" && len(amzDate) >= 8 {
		dateStamp = amzDate[:8]
	}

	// Build the credential scope
	credentialScope := fmt.Sprintf("%s/%s/%s/%s", dateStamp, auth.Region, auth.Service, requestTypeAWS4)

	// Get the canonical URI (path)
	canonicalURI := r.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// Build canonical headers
	var canonicalHeaders strings.Builder

	for _, h := range auth.SignedHeaders {
		var headerValue string
		if h == "host" {
			headerValue = r.Host
		} else {
			headerValue = r.Header.Get(h)
		}

		canonicalHeaders.WriteString(fmt.Sprintf("%s:%s\n", strings.ToLower(h), strings.TrimSpace(headerValue)))
	}

	// Build canonical query string
	canonicalQueryString := buildSortedQueryString(r.URL.Query())

	// Get the payload hash
	payloadHash := r.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		payloadHash = "UNSIGNED-PAYLOAD"
	}

	// Build canonical request
	canonicalRequest := strings.Join([]string{
		r.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders.String(),
		strings.Join(auth.SignedHeaders, ";"),
		payloadHash,
	}, "\n")

	// Hash the canonical request
	canonicalRequestHash := sha256Hex([]byte(canonicalRequest))

	// Build string to sign
	stringToSign := strings.Join([]string{
		algorithmAWS4HMACSHA256,
		amzDate,
		credentialScope,
		canonicalRequestHash,
	}, "\n")

	// Calculate the signing key
	signingKey := getSigningKey(secretKey, dateStamp, auth.Region, auth.Service)

	// Calculate the expected signature
	expectedSignature := hmacSHA256Hex(signingKey, []byte(stringToSign))

	// Compare signatures (using constant-time comparison via hmac.Equal is done in presigned.go)
	if auth.Signature != expectedSignature {
		return errors.New("signature mismatch")
	}

	return nil
}

// parseAuthorizationHeader parses an AWS Signature V4 Authorization header.
func parseAuthorizationHeader(header string) (*AuthorizationHeader, error) {
	// Format: AWS4-HMAC-SHA256 Credential=.../..., SignedHeaders=..., Signature=...
	if !strings.HasPrefix(header, algorithmAWS4HMACSHA256+" ") {
		return nil, errors.New("unsupported algorithm")
	}

	header = strings.TrimPrefix(header, algorithmAWS4HMACSHA256+" ")

	// Parse key=value pairs
	auth := &AuthorizationHeader{
		Algorithm: algorithmAWS4HMACSHA256,
	}

	// Use regex to extract components
	credentialRe := regexp.MustCompile(`Credential=([^,\s]+)`)
	signedHeadersRe := regexp.MustCompile(`SignedHeaders=([^,\s]+)`)
	signatureRe := regexp.MustCompile(`Signature=([^,\s]+)`)

	if match := credentialRe.FindStringSubmatch(header); len(match) == regexMatchParts {
		auth.Credential = match[1]
	} else {
		return nil, errors.New("missing Credential")
	}

	if match := signedHeadersRe.FindStringSubmatch(header); len(match) == regexMatchParts {
		auth.SignedHeaders = strings.Split(match[1], ";")
	} else {
		return nil, errors.New("missing SignedHeaders")
	}

	if match := signatureRe.FindStringSubmatch(header); len(match) == regexMatchParts {
		auth.Signature = match[1]
	} else {
		return nil, errors.New("missing Signature")
	}

	// Parse credential: ACCESS_KEY/YYYYMMDD/region/service/aws4_request
	credParts := strings.Split(auth.Credential, "/")
	if len(credParts) != signatureCredParts {
		return nil, errors.New("invalid credential format")
	}

	auth.AccessKeyID = credParts[0]
	auth.DateStamp = credParts[1]
	auth.Region = credParts[2]
	auth.Service = credParts[3]

	if credParts[4] != requestTypeAWS4 {
		return nil, fmt.Errorf("invalid request type: %s", credParts[4])
	}

	return auth, nil
}

// GenerateSignature generates an AWS Signature V4 signature for testing purposes.
func GenerateSignature(method, uri string, queryParams url.Values, headers map[string]string, payloadHash, accessKeyID, secretKey, region, dateStamp, amzDate string) string {
	// Build signed headers list
	headerKeys := make([]string, 0, len(headers))
	for k := range headers {
		headerKeys = append(headerKeys, strings.ToLower(k))
	}

	sort.Strings(headerKeys)
	signedHeaders := strings.Join(headerKeys, ";")

	// Build canonical headers
	var canonicalHeaders strings.Builder
	for _, k := range headerKeys {
		canonicalHeaders.WriteString(fmt.Sprintf("%s:%s\n", k, strings.TrimSpace(headers[k])))
	}

	// Build canonical query string
	canonicalQueryString := buildSortedQueryString(queryParams)

	// Build canonical request
	canonicalRequest := strings.Join([]string{
		method,
		uri,
		canonicalQueryString,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	// Hash the canonical request
	canonicalRequestHash := sha256Hex([]byte(canonicalRequest))

	// Build credential scope
	credentialScope := fmt.Sprintf("%s/%s/%s/%s", dateStamp, region, serviceS3, requestTypeAWS4)

	// Build string to sign
	stringToSign := strings.Join([]string{
		algorithmAWS4HMACSHA256,
		amzDate,
		credentialScope,
		canonicalRequestHash,
	}, "\n")

	// Calculate the signing key
	signingKey := getSigningKey(secretKey, dateStamp, region, serviceS3)

	// Calculate the signature
	return hmacSHA256Hex(signingKey, []byte(stringToSign))
}
