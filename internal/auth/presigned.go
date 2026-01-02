package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// AWS Signature Version 4 constants.
	algorithmAWS4HMACSHA256 = "AWS4-HMAC-SHA256"
	serviceS3               = "s3"
	requestTypeAWS4         = "aws4_request"

	// Header and payload constants.
	headerHost      = "host"
	unsignedPayload = "UNSIGNED-PAYLOAD"

	// Maximum presigned URL expiration (7 days in seconds).
	maxPresignExpiration = 7 * 24 * 60 * 60

	// Default expiration (1 hour).
	defaultPresignExpiration = 3600

	// Minimum expiration (1 second).
	minPresignExpiration = 1

	// awsCredentialParts is the expected number of parts in an AWS credential string.
	// Format: ACCESS_KEY/YYYYMMDD/region/service/aws4_request.
	awsCredentialParts = 5
)

// PresignParams contains parameters for generating a presigned URL.
type PresignParams struct {
	Headers     map[string]string
	QueryParams map[string]string
	Method      string
	Bucket      string
	Key         string
	AccessKeyID string
	SecretKey   string
	Region      string
	Endpoint    string
	Expiration  time.Duration
}

// PresignedURLGenerator generates S3-compatible presigned URLs.
type PresignedURLGenerator struct {
	defaultRegion   string
	defaultEndpoint string
}

// NewPresignedURLGenerator creates a new presigned URL generator.
func NewPresignedURLGenerator(defaultRegion, defaultEndpoint string) *PresignedURLGenerator {
	if defaultRegion == "" {
		defaultRegion = "us-east-1"
	}

	return &PresignedURLGenerator{
		defaultRegion:   defaultRegion,
		defaultEndpoint: defaultEndpoint,
	}
}

// GeneratePresignedURL creates a presigned URL for an S3 operation.
func (g *PresignedURLGenerator) GeneratePresignedURL(params PresignParams) (string, error) {
	if err := g.validatePresignParams(params); err != nil {
		return "", err
	}

	g.applyDefaults(&params)

	expirationSeconds, err := g.validateExpiration(params.Expiration)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	credentialScope := fmt.Sprintf("%s/%s/%s/%s", dateStamp, params.Region, serviceS3, requestTypeAWS4)
	canonicalURI := g.buildCanonicalURI(params.Endpoint, params.Bucket, params.Key)
	host := g.getHost(params.Endpoint, params.Bucket)
	signedHeaders := g.buildSignedHeaders(params.Headers)

	queryParams := g.buildPresignedQueryParams(params, credentialScope, amzDate, expirationSeconds, signedHeaders)
	canonicalQueryString := g.buildCanonicalQueryString(queryParams)
	canonicalHeaders := g.buildCanonicalHeaders(host, params.Headers)

	canonicalRequest := g.buildCanonicalRequest(params.Method, canonicalURI, canonicalQueryString, canonicalHeaders, signedHeaders)
	stringToSign := g.buildStringToSign(amzDate, credentialScope, canonicalRequest)
	signature := g.calculateSignature(params.SecretKey, dateStamp, params.Region, stringToSign)

	queryParams.Set("X-Amz-Signature", signature)

	presignedURL := g.buildURL(params.Endpoint, params.Bucket, params.Key, queryParams)

	return presignedURL, nil
}

func (g *PresignedURLGenerator) validatePresignParams(params PresignParams) error {
	if params.Method == "" {
		return errors.New("method is required")
	}

	if params.Bucket == "" {
		return errors.New("bucket is required")
	}

	if params.AccessKeyID == "" {
		return errors.New("access key ID is required")
	}

	if params.SecretKey == "" {
		return errors.New("secret key is required")
	}

	return nil
}

func (g *PresignedURLGenerator) applyDefaults(params *PresignParams) {
	if params.Region == "" {
		params.Region = g.defaultRegion
	}

	if params.Endpoint == "" {
		params.Endpoint = g.defaultEndpoint
	}
}

func (g *PresignedURLGenerator) validateExpiration(expiration time.Duration) (int64, error) {
	expirationSeconds := int64(expiration.Seconds())
	if expirationSeconds <= 0 {
		expirationSeconds = defaultPresignExpiration
	}

	if expirationSeconds > maxPresignExpiration {
		return 0, fmt.Errorf("expiration cannot exceed 7 days (%d seconds)", maxPresignExpiration)
	}

	if expirationSeconds < minPresignExpiration {
		expirationSeconds = minPresignExpiration
	}

	return expirationSeconds, nil
}

func (g *PresignedURLGenerator) buildCanonicalURI(endpoint, bucket, key string) string {
	var canonicalURI string

	if endpoint != "" {
		// Path-style URL: include bucket in the path
		canonicalURI = "/" + bucket
		if key != "" {
			canonicalURI += "/" + key
		}
	} else {
		// Virtual-hosted style: just the key
		canonicalURI = "/" + key
		if key == "" {
			canonicalURI = "/"
		}
	}

	return canonicalURI
}

func (g *PresignedURLGenerator) buildSignedHeaders(headers map[string]string) string {
	signedHeaders := headerHost

	if len(headers) > 0 {
		headerList := make([]string, 0, len(headers)+1)

		headerList = append(headerList, headerHost)
		for h := range headers {
			headerList = append(headerList, strings.ToLower(h))
		}

		sort.Strings(headerList)
		signedHeaders = strings.Join(headerList, ";")
	}

	return signedHeaders
}

func (g *PresignedURLGenerator) buildPresignedQueryParams(params PresignParams, credentialScope, amzDate string, expirationSeconds int64, signedHeaders string) url.Values {
	queryParams := url.Values{}
	queryParams.Set("X-Amz-Algorithm", algorithmAWS4HMACSHA256)
	queryParams.Set("X-Amz-Credential", params.AccessKeyID+"/"+credentialScope)
	queryParams.Set("X-Amz-Date", amzDate)
	queryParams.Set("X-Amz-Expires", strconv.FormatInt(expirationSeconds, 10))
	queryParams.Set("X-Amz-SignedHeaders", signedHeaders)

	// Add any additional query parameters
	for k, v := range params.QueryParams {
		queryParams.Set(k, v)
	}

	return queryParams
}

func (g *PresignedURLGenerator) buildCanonicalHeaders(host string, headers map[string]string) string {
	canonicalHeaders := fmt.Sprintf("host:%s\n", host)

	if len(headers) > 0 {
		headerKeys := make([]string, 0, len(headers))
		for k := range headers {
			headerKeys = append(headerKeys, strings.ToLower(k))
		}

		sort.Strings(headerKeys)

		var headerBuilder strings.Builder
		headerBuilder.WriteString(fmt.Sprintf("host:%s\n", host))

		for _, k := range headerKeys {
			if k == headerHost {
				continue
			}

			headerBuilder.WriteString(fmt.Sprintf("%s:%s\n", k, strings.TrimSpace(headers[k])))
		}

		canonicalHeaders = headerBuilder.String()
	}

	return canonicalHeaders
}

func (g *PresignedURLGenerator) buildCanonicalRequest(method, canonicalURI, canonicalQueryString, canonicalHeaders, signedHeaders string) string {
	// For presigned URLs, we use UNSIGNED-PAYLOAD
	payloadHash := unsignedPayload

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	return canonicalRequest
}

func (g *PresignedURLGenerator) buildStringToSign(amzDate, credentialScope, canonicalRequest string) string {
	canonicalRequestHash := sha256Hex([]byte(canonicalRequest))

	stringToSign := strings.Join([]string{
		algorithmAWS4HMACSHA256,
		amzDate,
		credentialScope,
		canonicalRequestHash,
	}, "\n")

	return stringToSign
}

func (g *PresignedURLGenerator) calculateSignature(secretKey, dateStamp, region, stringToSign string) string {
	signingKey := g.getSignatureKey(secretKey, dateStamp, region, serviceS3)
	signature := hmacSHA256Hex(signingKey, []byte(stringToSign))
	return signature
}

// getHost returns the host for the request.
func (g *PresignedURLGenerator) getHost(endpoint, bucket string) string {
	if endpoint == "" {
		// Default AWS S3 style
		return bucket + ".s3.amazonaws.com"
	}

	// Parse the endpoint to get the host
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}

	// For path-style URLs, just return the host
	return u.Host
}

// buildURL constructs the final presigned URL.
func (g *PresignedURLGenerator) buildURL(endpoint, bucket, key string, queryParams url.Values) string {
	var baseURL string

	if endpoint == "" {
		// Virtual-hosted style URL
		baseURL = fmt.Sprintf("https://%s.s3.amazonaws.com", bucket)
	} else {
		// Path-style URL for custom endpoints
		baseURL = strings.TrimSuffix(endpoint, "/") + "/" + bucket
	}

	path := ""
	if key != "" {
		path = "/" + url.PathEscape(key)
	}

	return baseURL + path + "?" + g.buildCanonicalQueryString(queryParams)
}

// buildCanonicalQueryString builds a sorted, URL-encoded query string.
func (g *PresignedURLGenerator) buildCanonicalQueryString(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var pairs []string

	for _, k := range keys {
		for _, v := range params[k] {
			pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}

	return strings.Join(pairs, "&")
}

// getSignatureKey derives the signing key using AWS Signature Version 4.
func (g *PresignedURLGenerator) getSignatureKey(secretKey, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte(requestTypeAWS4))

	return kSigning
}

// PresignedURLInfo contains parsed presigned URL parameters.
type PresignedURLInfo struct {
	Date          time.Time
	Algorithm     string
	Credential    string
	Signature     string
	AccessKeyID   string
	DateStamp     string
	Region        string
	Service       string
	SignedHeaders []string
	Expires       int64
}

// ParsePresignedURL parses presigned URL query parameters.
func ParsePresignedURL(r *http.Request) (*PresignedURLInfo, error) {
	query := r.URL.Query()

	algorithm := query.Get("X-Amz-Algorithm")
	if algorithm != algorithmAWS4HMACSHA256 {
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}

	credential := query.Get("X-Amz-Credential")
	if credential == "" {
		return nil, errors.New("missing X-Amz-Credential")
	}

	// Parse credential: ACCESS_KEY/YYYYMMDD/region/service/aws4_request
	credParts := strings.Split(credential, "/")
	if len(credParts) != awsCredentialParts {
		return nil, errors.New("invalid credential format")
	}

	accessKeyID := credParts[0]
	dateStamp := credParts[1]
	region := credParts[2]
	service := credParts[3]
	requestType := credParts[4]

	if service != serviceS3 {
		return nil, fmt.Errorf("invalid service: %s", service)
	}

	if requestType != requestTypeAWS4 {
		return nil, fmt.Errorf("invalid request type: %s", requestType)
	}

	amzDate := query.Get("X-Amz-Date")
	if amzDate == "" {
		return nil, errors.New("missing X-Amz-Date")
	}

	date, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		return nil, fmt.Errorf("invalid X-Amz-Date format: %w", err)
	}

	expiresStr := query.Get("X-Amz-Expires")
	if expiresStr == "" {
		return nil, errors.New("missing X-Amz-Expires")
	}

	expires, err := strconv.ParseInt(expiresStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid X-Amz-Expires: %w", err)
	}

	if expires < minPresignExpiration || expires > maxPresignExpiration {
		return nil, fmt.Errorf("X-Amz-Expires must be between %d and %d seconds", minPresignExpiration, maxPresignExpiration)
	}

	signedHeadersStr := query.Get("X-Amz-SignedHeaders")
	if signedHeadersStr == "" {
		return nil, errors.New("missing X-Amz-SignedHeaders")
	}

	signedHeaders := strings.Split(signedHeadersStr, ";")

	signature := query.Get("X-Amz-Signature")
	if signature == "" {
		return nil, errors.New("missing X-Amz-Signature")
	}

	return &PresignedURLInfo{
		Algorithm:     algorithm,
		Credential:    credential,
		Date:          date,
		Expires:       expires,
		SignedHeaders: signedHeaders,
		Signature:     signature,
		AccessKeyID:   accessKeyID,
		DateStamp:     dateStamp,
		Region:        region,
		Service:       service,
	}, nil
}

// IsPresignedRequest checks if the request is a presigned URL request.
func IsPresignedRequest(r *http.Request) bool {
	query := r.URL.Query()
	return query.Get("X-Amz-Signature") != "" && query.Get("X-Amz-Algorithm") != ""
}

// ValidatePresignedExpiration checks if the presigned URL has expired.
func ValidatePresignedExpiration(info *PresignedURLInfo) error {
	expirationTime := info.Date.Add(time.Duration(info.Expires) * time.Second)
	if time.Now().UTC().After(expirationTime) {
		return errors.New("presigned URL has expired")
	}

	return nil
}

// ValidatePresignedSignature validates the signature of a presigned URL request.
func ValidatePresignedSignature(r *http.Request, info *PresignedURLInfo, secretKey string) error {
	// Build the credential scope
	credentialScope := fmt.Sprintf("%s/%s/%s/%s", info.DateStamp, info.Region, info.Service, requestTypeAWS4)

	// Get the canonical URI (path)
	canonicalURI := r.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// Build canonical headers from request
	var canonicalHeaders strings.Builder

	for _, h := range info.SignedHeaders {
		var headerValue string
		if h == headerHost {
			headerValue = r.Host
		} else {
			headerValue = r.Header.Get(h)
		}

		canonicalHeaders.WriteString(fmt.Sprintf("%s:%s\n", strings.ToLower(h), strings.TrimSpace(headerValue)))
	}

	// Build canonical query string (exclude X-Amz-Signature)
	queryParams := url.Values{}

	for k, v := range r.URL.Query() {
		if k != "X-Amz-Signature" {
			queryParams[k] = v
		}
	}

	canonicalQueryString := buildSortedQueryString(queryParams)

	// For presigned URLs, we use UNSIGNED-PAYLOAD
	payloadHash := unsignedPayload

	// Build canonical request
	canonicalRequest := strings.Join([]string{
		r.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders.String(),
		strings.Join(info.SignedHeaders, ";"),
		payloadHash,
	}, "\n")

	// Hash the canonical request
	canonicalRequestHash := sha256Hex([]byte(canonicalRequest))

	// Build string to sign
	amzDate := info.Date.Format("20060102T150405Z")
	stringToSign := strings.Join([]string{
		algorithmAWS4HMACSHA256,
		amzDate,
		credentialScope,
		canonicalRequestHash,
	}, "\n")

	// Calculate the signing key
	signingKey := getSigningKey(secretKey, info.DateStamp, info.Region, info.Service)

	// Calculate the expected signature
	expectedSignature := hmacSHA256Hex(signingKey, []byte(stringToSign))

	// Compare signatures
	if !hmac.Equal([]byte(info.Signature), []byte(expectedSignature)) {
		return errors.New("signature mismatch")
	}

	return nil
}

// buildSortedQueryString builds a sorted, URL-encoded query string.
func buildSortedQueryString(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var pairs []string

	for _, k := range keys {
		for _, v := range params[k] {
			pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}

	return strings.Join(pairs, "&")
}

// getSigningKey derives the signing key using AWS Signature Version 4.
func getSigningKey(secretKey, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte(requestTypeAWS4))

	return kSigning
}

// hmacSHA256 calculates HMAC-SHA256.
func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)

	return h.Sum(nil)
}

// hmacSHA256Hex calculates HMAC-SHA256 and returns hex string.
func hmacSHA256Hex(key, data []byte) string {
	return hex.EncodeToString(hmacSHA256(key, data))
}

// sha256Hex calculates SHA256 hash and returns hex string.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
