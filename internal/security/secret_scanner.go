// Package security provides security-related functionality including secret scanning.
package security

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// SecretType represents the type of secret detected.
type SecretType string

const (
	SecretTypeAWSAccessKey      SecretType = "aws_access_key"
	SecretTypeAWSSecretKey      SecretType = "aws_secret_key"
	SecretTypeGCPServiceAccount SecretType = "gcp_service_account"
	SecretTypeAzureStorageKey   SecretType = "azure_storage_key"
	SecretTypeGitHubToken       SecretType = "github_token"
	SecretTypeGitHubAppToken    SecretType = "github_app_token"
	SecretTypeGitLabToken       SecretType = "gitlab_token"
	SecretTypeSlackToken        SecretType = "slack_token"
	SecretTypeSlackWebhook      SecretType = "slack_webhook"
	SecretTypeStripeKey         SecretType = "stripe_key"
	SecretTypeTwilioKey         SecretType = "twilio_key"
	SecretTypeSendGridKey       SecretType = "sendgrid_key"
	SecretTypeMailchimpKey      SecretType = "mailchimp_key"
	SecretTypeSSHPrivateKey     SecretType = "ssh_private_key"
	SecretTypePGPPrivateKey     SecretType = "pgp_private_key"
	SecretTypeRSAPrivateKey     SecretType = "rsa_private_key"
	SecretTypeCertificate       SecretType = "certificate"
	SecretTypeJWT               SecretType = "jwt"
	SecretTypeGenericAPIKey     SecretType = "generic_api_key"
	SecretTypeGenericPassword   SecretType = "generic_password"
	SecretTypeBasicAuth         SecretType = "basic_auth"
	SecretTypeBearerToken       SecretType = "bearer_token"
	SecretTypeDatabaseURL       SecretType = "database_url"
	SecretTypePrivateKeyPEM     SecretType = "private_key_pem"
	SecretTypeOpenAIKey         SecretType = "openai_key"
	SecretTypeAnthropicKey      SecretType = "anthropic_key"
	SecretTypeHuggingFaceToken  SecretType = "huggingface_token"
	SecretTypeNPMToken          SecretType = "npm_token"
	SecretTypePyPIToken         SecretType = "pypi_token"
	SecretTypeDockerHubToken    SecretType = "dockerhub_token"
	SecretTypeHerokuKey         SecretType = "heroku_key"
	SecretTypeDigitalOceanToken SecretType = "digitalocean_token"
	SecretTypeFirebaseKey       SecretType = "firebase_key"
)

// Severity represents the severity of a secret finding.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// SecretPattern defines a pattern for detecting secrets.
type SecretPattern struct {
	Type        SecretType
	Name        string
	Description string
	Pattern     *regexp.Regexp
	Severity    Severity
	Validator   func(match string) bool
	Keywords    []string
}

// SecretFinding represents a detected secret.
type SecretFinding struct {
	DetectedAt    time.Time  `json:"detectedAt"`
	VersionID     string     `json:"versionId,omitempty"`
	Context       string     `json:"context,omitempty"`
	Description   string     `json:"description"`
	Bucket        string     `json:"bucket"`
	Key           string     `json:"key"`
	ID            string     `json:"id"`
	Type          SecretType `json:"type"`
	Severity      Severity   `json:"severity"`
	Match         string     `json:"match"`
	ColumnEnd     int        `json:"columnEnd,omitempty"`
	ColumnStart   int        `json:"columnStart,omitempty"`
	LineNumber    int        `json:"lineNumber,omitempty"`
	Verified      bool       `json:"verified"`
	FalsePositive bool       `json:"falsePositive"`
}

// ScanResult contains the results of a secret scan.
type ScanResult struct {
	ScannedAt   time.Time        `json:"scannedAt"`
	ID          string           `json:"id"`
	Bucket      string           `json:"bucket"`
	Key         string           `json:"key"`
	ContentType string           `json:"contentType"`
	Error       string           `json:"error,omitempty"`
	Findings    []*SecretFinding `json:"findings"`
	Size        int64            `json:"size"`
	ScanTime    time.Duration    `json:"scanTime"`
}

// ScannerConfig configures the secret scanner.
type ScannerConfig struct {
	AllowedTypes     []SecretType
	CustomPatterns   []*SecretPattern
	DisabledPatterns []SecretType
	ContextLines     int
	MaxLineLength    int
	MaxFileSize      int64
	Concurrency      int
	EnableValidation bool
	IncludeContext   bool
	RedactSecrets    bool
	BlockOnFinding   bool
	AlertOnFinding   bool
	ScanBinaryFiles  bool
}

// SecretScanner scans objects for secrets.
type SecretScanner struct {
	storage  SecretStorage
	config   *ScannerConfig
	findings map[string][]*SecretFinding
	stats    *ScannerStats
	patterns []*SecretPattern
	mu       sync.RWMutex
}

// SecretStorage interface for persisting findings.
type SecretStorage interface {
	SaveFinding(ctx context.Context, finding *SecretFinding) error
	GetFindings(ctx context.Context, bucket, key string) ([]*SecretFinding, error)
	MarkFalsePositive(ctx context.Context, findingID string) error
	DeleteFindings(ctx context.Context, bucket, key string) error
}

// ScannerStats tracks scanner statistics.
type ScannerStats struct {
	LastScanTime       time.Time
	FindingsByType     map[SecretType]int64
	FindingsBySeverity map[Severity]int64
	ObjectsScanned     int64
	BytesScanned       int64
	FindingsTotal      int64
	ScanErrors         int64
}

// DefaultConfig returns the default scanner configuration.
func DefaultConfig() *ScannerConfig {
	return &ScannerConfig{
		MaxFileSize:      100 * 1024 * 1024, // 100 MB
		MaxLineLength:    10000,
		ScanBinaryFiles:  false,
		EnableValidation: true,
		Concurrency:      10,
		IncludeContext:   true,
		ContextLines:     2,
		RedactSecrets:    true,
		BlockOnFinding:   false,
		AlertOnFinding:   true,
	}
}

// NewSecretScanner creates a new secret scanner.
func NewSecretScanner(config *ScannerConfig, storage SecretStorage) *SecretScanner {
	if config == nil {
		config = DefaultConfig()
	}

	scanner := &SecretScanner{
		config:   config,
		patterns: buildPatterns(config),
		findings: make(map[string][]*SecretFinding),
		stats: &ScannerStats{
			FindingsByType:     make(map[SecretType]int64),
			FindingsBySeverity: make(map[Severity]int64),
		},
		storage: storage,
	}

	return scanner
}

// buildPatterns builds the list of patterns to scan for.
func buildPatterns(config *ScannerConfig) []*SecretPattern {
	patterns := defaultPatterns()

	// Add custom patterns
	if config.CustomPatterns != nil {
		patterns = append(patterns, config.CustomPatterns...)
	}

	// Filter disabled patterns
	if len(config.DisabledPatterns) > 0 {
		disabled := make(map[SecretType]bool)
		for _, t := range config.DisabledPatterns {
			disabled[t] = true
		}

		filtered := make([]*SecretPattern, 0)

		for _, p := range patterns {
			if !disabled[p.Type] {
				filtered = append(filtered, p)
			}
		}

		patterns = filtered
	}

	return patterns
}

// defaultPatterns returns the default secret patterns.
func defaultPatterns() []*SecretPattern {
	return []*SecretPattern{
		// AWS
		{
			Type:        SecretTypeAWSAccessKey,
			Name:        "AWS Access Key ID",
			Description: "AWS Access Key ID detected",
			Pattern:     regexp.MustCompile(`(?i)(AKIA|A3T|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[0-9A-Z]{16}`),
			Severity:    SeverityCritical,
			Keywords:    []string{"aws", "amazon", "access_key", "accesskey"},
		},
		{
			Type:        SecretTypeAWSSecretKey,
			Name:        "AWS Secret Access Key",
			Description: "AWS Secret Access Key detected",
			Pattern:     regexp.MustCompile(`(?i)aws_?secret_?access_?key['":\s]*[=:]\s*['"]?([A-Za-z0-9/+=]{40})['"]?`),
			Severity:    SeverityCritical,
			Keywords:    []string{"aws", "secret", "access_key"},
		},

		// GCP
		{
			Type:        SecretTypeGCPServiceAccount,
			Name:        "GCP Service Account Key",
			Description: "Google Cloud Platform service account key detected",
			Pattern:     regexp.MustCompile(`"type"\s*:\s*"service_account"`),
			Severity:    SeverityCritical,
			Keywords:    []string{"gcp", "google", "service_account"},
		},

		// Azure
		{
			Type:        SecretTypeAzureStorageKey,
			Name:        "Azure Storage Key",
			Description: "Azure Storage account key detected",
			Pattern:     regexp.MustCompile(`(?i)DefaultEndpointsProtocol=https?;AccountName=[^;]+;AccountKey=([A-Za-z0-9+/=]{88})`),
			Severity:    SeverityCritical,
			Keywords:    []string{"azure", "storage", "account_key"},
		},

		// GitHub
		{
			Type:        SecretTypeGitHubToken,
			Name:        "GitHub Token",
			Description: "GitHub personal access token detected",
			Pattern:     regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"github", "token"},
		},
		{
			Type:        SecretTypeGitHubAppToken,
			Name:        "GitHub App Token",
			Description: "GitHub App token detected",
			Pattern:     regexp.MustCompile(`(ghu|ghs)_[A-Za-z0-9]{36}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"github", "app", "token"},
		},

		// GitLab
		{
			Type:        SecretTypeGitLabToken,
			Name:        "GitLab Token",
			Description: "GitLab personal or project access token detected",
			Pattern:     regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{20,}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"gitlab", "token"},
		},

		// Slack
		{
			Type:        SecretTypeSlackToken,
			Name:        "Slack Token",
			Description: "Slack API token detected",
			Pattern:     regexp.MustCompile(`xox[baprs]-[0-9]{10,13}-[0-9]{10,13}[a-zA-Z0-9-]*`),
			Severity:    SeverityHigh,
			Keywords:    []string{"slack", "token", "xox"},
		},
		{
			Type:        SecretTypeSlackWebhook,
			Name:        "Slack Webhook",
			Description: "Slack incoming webhook URL detected",
			Pattern:     regexp.MustCompile(`https://hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]+`),
			Severity:    SeverityMedium,
			Keywords:    []string{"slack", "webhook"},
		},

		// Stripe
		{
			Type:        SecretTypeStripeKey,
			Name:        "Stripe API Key",
			Description: "Stripe API key detected",
			Pattern:     regexp.MustCompile(`(?:sk|rk)_(live|test)_[A-Za-z0-9]{24,}`),
			Severity:    SeverityCritical,
			Keywords:    []string{"stripe", "api_key"},
		},

		// OpenAI
		{
			Type:        SecretTypeOpenAIKey,
			Name:        "OpenAI API Key",
			Description: "OpenAI API key detected",
			Pattern:     regexp.MustCompile(`sk-[A-Za-z0-9]{48}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"openai", "api_key"},
		},

		// Anthropic
		{
			Type:        SecretTypeAnthropicKey,
			Name:        "Anthropic API Key",
			Description: "Anthropic API key detected",
			Pattern:     regexp.MustCompile(`sk-ant-api[0-9]{2}-[A-Za-z0-9_-]{86}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"anthropic", "claude", "api_key"},
		},

		// Private Keys
		{
			Type:        SecretTypeSSHPrivateKey,
			Name:        "SSH Private Key",
			Description: "SSH private key detected",
			Pattern:     regexp.MustCompile(`-----BEGIN (?:RSA |DSA |EC |OPENSSH )?PRIVATE KEY-----`),
			Severity:    SeverityCritical,
			Keywords:    []string{"ssh", "private", "key"},
		},
		{
			Type:        SecretTypeRSAPrivateKey,
			Name:        "RSA Private Key",
			Description: "RSA private key detected",
			Pattern:     regexp.MustCompile(`-----BEGIN RSA PRIVATE KEY-----`),
			Severity:    SeverityCritical,
			Keywords:    []string{"rsa", "private", "key"},
		},
		{
			Type:        SecretTypePGPPrivateKey,
			Name:        "PGP Private Key",
			Description: "PGP private key block detected",
			Pattern:     regexp.MustCompile(`-----BEGIN PGP PRIVATE KEY BLOCK-----`),
			Severity:    SeverityCritical,
			Keywords:    []string{"pgp", "private", "key"},
		},

		// JWT
		{
			Type:        SecretTypeJWT,
			Name:        "JSON Web Token",
			Description: "JSON Web Token detected",
			Pattern:     regexp.MustCompile(`eyJ[A-Za-z0-9_-]*\.eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]*`),
			Severity:    SeverityMedium,
			Keywords:    []string{"jwt", "token"},
			Validator: func(match string) bool {
				parts := strings.Split(match, ".")
				return len(parts) == 3
			},
		},

		// Generic patterns
		{
			Type:        SecretTypeGenericAPIKey,
			Name:        "Generic API Key",
			Description: "Potential API key detected",
			Pattern:     regexp.MustCompile(`(?i)(?:api[_-]?key|apikey)['":\s]*[=:]\s*['"]?([A-Za-z0-9_-]{20,})['"]?`),
			Severity:    SeverityMedium,
			Keywords:    []string{"api_key", "apikey"},
		},
		{
			Type:        SecretTypeGenericPassword,
			Name:        "Generic Password",
			Description: "Potential password in configuration detected",
			Pattern:     regexp.MustCompile(`(?i)(?:password|passwd|pwd)['":\s]*[=:]\s*['"]?([^\s'"]{8,})['"]?`),
			Severity:    SeverityMedium,
			Keywords:    []string{"password", "passwd", "pwd"},
		},
		{
			Type:        SecretTypeBasicAuth,
			Name:        "Basic Authentication",
			Description: "Basic authentication credentials detected",
			Pattern:     regexp.MustCompile(`(?i)basic\s+[A-Za-z0-9+/=]{20,}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"basic", "auth"},
		},
		{
			Type:        SecretTypeBearerToken,
			Name:        "Bearer Token",
			Description: "Bearer token detected",
			Pattern:     regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_.-]{20,}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"bearer", "token"},
		},
		{
			Type:        SecretTypeDatabaseURL,
			Name:        "Database Connection URL",
			Description: "Database connection string with credentials detected",
			Pattern:     regexp.MustCompile(`(?i)(?:mysql|postgres|mongodb|redis|amqp):\/\/[^:]+:[^@]+@[^\s]+`),
			Severity:    SeverityCritical,
			Keywords:    []string{"mysql", "postgres", "mongodb", "redis", "database"},
		},

		// NPM/PyPI
		{
			Type:        SecretTypeNPMToken,
			Name:        "NPM Token",
			Description: "NPM access token detected",
			Pattern:     regexp.MustCompile(`npm_[A-Za-z0-9]{36}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"npm", "token"},
		},
		{
			Type:        SecretTypePyPIToken,
			Name:        "PyPI Token",
			Description: "PyPI API token detected",
			Pattern:     regexp.MustCompile(`pypi-AgE[A-Za-z0-9_-]{50,}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"pypi", "token"},
		},

		// Other services
		{
			Type:        SecretTypeTwilioKey,
			Name:        "Twilio API Key",
			Description: "Twilio API key detected",
			Pattern:     regexp.MustCompile(`SK[A-Za-z0-9]{32}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"twilio", "api_key"},
		},
		{
			Type:        SecretTypeSendGridKey,
			Name:        "SendGrid API Key",
			Description: "SendGrid API key detected",
			Pattern:     regexp.MustCompile(`SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"sendgrid", "api_key"},
		},
		{
			Type:        SecretTypeHerokuKey,
			Name:        "Heroku API Key",
			Description: "Heroku API key detected",
			Pattern:     regexp.MustCompile(`(?i)heroku[_-]?api[_-]?key['":\s]*[=:]\s*['"]?([A-Za-z0-9-]{36})['"]?`),
			Severity:    SeverityHigh,
			Keywords:    []string{"heroku", "api_key"},
		},
		{
			Type:        SecretTypeDigitalOceanToken,
			Name:        "DigitalOcean Token",
			Description: "DigitalOcean access token detected",
			Pattern:     regexp.MustCompile(`dop_v1_[A-Za-z0-9]{64}`),
			Severity:    SeverityHigh,
			Keywords:    []string{"digitalocean", "token"},
		},
		{
			Type:        SecretTypeFirebaseKey,
			Name:        "Firebase API Key",
			Description: "Firebase API key detected",
			Pattern:     regexp.MustCompile(`AIza[A-Za-z0-9_-]{35}`),
			Severity:    SeverityMedium,
			Keywords:    []string{"firebase", "api_key"},
		},
	}
}

// ScanObject scans an object for secrets.
func (s *SecretScanner) ScanObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType string) (*ScanResult, error) {
	startTime := time.Now()

	result := &ScanResult{
		ID:          uuid.New().String(),
		Bucket:      bucket,
		Key:         key,
		Size:        size,
		ContentType: contentType,
		Findings:    make([]*SecretFinding, 0),
		ScannedAt:   time.Now(),
	}

	// Check size limit
	if size > s.config.MaxFileSize {
		result.Error = "file exceeds maximum size limit"
		return result, nil
	}

	// Check if binary file
	if !s.config.ScanBinaryFiles && isBinaryContentType(contentType) {
		return result, nil
	}

	// Read content
	var (
		content []byte
		err     error
	)

	if size < 10*1024*1024 { // Less than 10MB, read all
		content, err = io.ReadAll(io.LimitReader(reader, s.config.MaxFileSize))
		if err != nil {
			result.Error = err.Error()
			//nolint:nilerr // Error is captured in result.Error, successful scan result returned
			return result, nil
		}

		result.Findings = s.scanContent(bucket, key, content)
	} else {
		// Larger files, scan line by line
		result.Findings = s.scanStream(bucket, key, reader)
	}

	// Update stats
	atomic.AddInt64(&s.stats.ObjectsScanned, 1)
	atomic.AddInt64(&s.stats.BytesScanned, size)

	for _, finding := range result.Findings {
		atomic.AddInt64(&s.stats.FindingsTotal, 1)
		s.mu.Lock()
		s.stats.FindingsByType[finding.Type]++
		s.stats.FindingsBySeverity[finding.Severity]++
		s.mu.Unlock()

		// Save finding
		if s.storage != nil {
			_ = s.storage.SaveFinding(ctx, finding)
		}
	}

	result.ScanTime = time.Since(startTime)

	// Cache findings
	s.mu.Lock()
	s.findings[bucket+"/"+key] = result.Findings
	s.stats.LastScanTime = time.Now()
	s.mu.Unlock()

	return result, nil
}

// scanContent scans content for secrets.
func (s *SecretScanner) scanContent(bucket, key string, content []byte) []*SecretFinding {
	findings := make([]*SecretFinding, 0)
	lines := bytes.Split(content, []byte("\n"))

	for lineNum, line := range lines {
		lineStr := string(line)

		// Skip very long lines
		if len(lineStr) > s.config.MaxLineLength {
			continue
		}

		for _, pattern := range s.patterns {
			matches := pattern.Pattern.FindAllStringIndex(lineStr, -1)
			for _, match := range matches {
				matchStr := lineStr[match[0]:match[1]]

				// Validate if validator exists
				if pattern.Validator != nil && !pattern.Validator(matchStr) {
					continue
				}

				// Check if allowed
				if s.isAllowed(pattern.Type) {
					continue
				}

				finding := &SecretFinding{
					ID:          uuid.New().String(),
					Type:        pattern.Type,
					Severity:    pattern.Severity,
					Description: pattern.Description,
					Bucket:      bucket,
					Key:         key,
					LineNumber:  lineNum + 1,
					ColumnStart: match[0] + 1,
					ColumnEnd:   match[1] + 1,
					Match:       s.redactMatch(matchStr),
					DetectedAt:  time.Now(),
				}

				// Add context if enabled
				if s.config.IncludeContext {
					finding.Context = s.getContext(lines, lineNum, s.config.ContextLines)
				}

				findings = append(findings, finding)
			}
		}
	}

	return findings
}

// scanStream scans a stream for secrets line by line.
func (s *SecretScanner) scanStream(bucket, key string, reader io.Reader) []*SecretFinding {
	findings := make([]*SecretFinding, 0)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, s.config.MaxLineLength), s.config.MaxLineLength)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip very long lines
		if len(line) > s.config.MaxLineLength {
			continue
		}

		for _, pattern := range s.patterns {
			matches := pattern.Pattern.FindAllStringIndex(line, -1)
			for _, match := range matches {
				matchStr := line[match[0]:match[1]]

				if pattern.Validator != nil && !pattern.Validator(matchStr) {
					continue
				}

				if s.isAllowed(pattern.Type) {
					continue
				}

				finding := &SecretFinding{
					ID:          uuid.New().String(),
					Type:        pattern.Type,
					Severity:    pattern.Severity,
					Description: pattern.Description,
					Bucket:      bucket,
					Key:         key,
					LineNumber:  lineNum,
					ColumnStart: match[0] + 1,
					ColumnEnd:   match[1] + 1,
					Match:       s.redactMatch(matchStr),
					DetectedAt:  time.Now(),
				}

				findings = append(findings, finding)
			}
		}
	}

	return findings
}

// isAllowed checks if a secret type is allowed.
func (s *SecretScanner) isAllowed(t SecretType) bool {
	for _, allowed := range s.config.AllowedTypes {
		if allowed == t {
			return true
		}
	}

	return false
}

// redactMatch redacts a match for safe display.
func (s *SecretScanner) redactMatch(match string) string {
	if !s.config.RedactSecrets {
		return match
	}

	if len(match) <= 8 {
		return "***"
	}

	// Show first and last 4 characters
	return match[:4] + "..." + match[len(match)-4:]
}

// getContext returns surrounding lines for context.
func (s *SecretScanner) getContext(lines [][]byte, lineNum, contextLines int) string {
	start := lineNum - contextLines
	if start < 0 {
		start = 0
	}

	end := lineNum + contextLines + 1
	if end > len(lines) {
		end = len(lines)
	}

	var context []string

	for i := start; i < end; i++ {
		prefix := "  "
		if i == lineNum {
			prefix = "> "
		}

		context = append(context, prefix+string(lines[i]))
	}

	return strings.Join(context, "\n")
}

// isBinaryContentType checks if content type indicates binary.
func isBinaryContentType(contentType string) bool {
	binaryTypes := []string{
		"application/octet-stream",
		"image/",
		"video/",
		"audio/",
		"application/pdf",
		"application/zip",
		"application/gzip",
		"application/x-tar",
		"application/x-executable",
	}

	for _, t := range binaryTypes {
		if strings.HasPrefix(contentType, t) {
			return true
		}
	}

	return false
}

// GetFindings returns cached findings for an object.
func (s *SecretScanner) GetFindings(ctx context.Context, bucket, key string) ([]*SecretFinding, error) {
	s.mu.RLock()
	findings := s.findings[bucket+"/"+key]
	s.mu.RUnlock()

	if findings != nil {
		return findings, nil
	}

	if s.storage != nil {
		return s.storage.GetFindings(ctx, bucket, key)
	}

	return nil, nil
}

// MarkFalsePositive marks a finding as false positive.
func (s *SecretScanner) MarkFalsePositive(ctx context.Context, findingID string) error {
	if s.storage != nil {
		return s.storage.MarkFalsePositive(ctx, findingID)
	}

	return nil
}

// GetStats returns scanner statistics.
func (s *SecretScanner) GetStats() *ScannerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Copy stats
	stats := &ScannerStats{
		ObjectsScanned:     s.stats.ObjectsScanned,
		BytesScanned:       s.stats.BytesScanned,
		FindingsTotal:      s.stats.FindingsTotal,
		FindingsByType:     make(map[SecretType]int64),
		FindingsBySeverity: make(map[Severity]int64),
		ScanErrors:         s.stats.ScanErrors,
		LastScanTime:       s.stats.LastScanTime,
	}

	for k, v := range s.stats.FindingsByType {
		stats.FindingsByType[k] = v
	}

	for k, v := range s.stats.FindingsBySeverity {
		stats.FindingsBySeverity[k] = v
	}

	return stats
}

// ShouldBlock returns true if upload should be blocked based on findings.
func (s *SecretScanner) ShouldBlock(findings []*SecretFinding) bool {
	if !s.config.BlockOnFinding {
		return false
	}

	for _, f := range findings {
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			return true
		}
	}

	return false
}

// ScanResultToJSON converts a scan result to JSON.
func ScanResultToJSON(result *ScanResult) ([]byte, error) {
	return json.MarshalIndent(result, "", "  ")
}

// AddCustomPattern adds a custom pattern to the scanner.
func (s *SecretScanner) AddCustomPattern(pattern *SecretPattern) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.patterns = append(s.patterns, pattern)
}

// RemovePattern removes a pattern by type.
func (s *SecretScanner) RemovePattern(t SecretType) {
	s.mu.Lock()
	defer s.mu.Unlock()

	newPatterns := make([]*SecretPattern, 0)

	for _, p := range s.patterns {
		if p.Type != t {
			newPatterns = append(newPatterns, p)
		}
	}

	s.patterns = newPatterns
}
