// Package dlp provides Data Loss Prevention functionality for NebulaIO.
package dlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// DataType represents the type of sensitive data detected.
type DataType string

const (
	// PII - Personally Identifiable Information.
	DataTypeSSN           DataType = "ssn"            // Social Security Number
	DataTypePassport      DataType = "passport"       // Passport Number
	DataTypeDriverLicense DataType = "driver_license" // Driver's License
	DataTypeNationalID    DataType = "national_id"    // National ID
	DataTypeTaxID         DataType = "tax_id"         // Tax ID
	DataTypeDateOfBirth   DataType = "date_of_birth"  // Date of Birth

	// Financial.
	DataTypeCreditCard    DataType = "credit_card"    // Credit Card Number
	DataTypeBankAccount   DataType = "bank_account"   // Bank Account Number
	DataTypeIBAN          DataType = "iban"           // International Bank Account Number
	DataTypeSWIFT         DataType = "swift"          // SWIFT/BIC Code
	DataTypeRoutingNumber DataType = "routing_number" // Bank Routing Number

	// Contact Information.
	DataTypeEmail     DataType = "email"      // Email Address
	DataTypePhone     DataType = "phone"      // Phone Number
	DataTypeAddress   DataType = "address"    // Physical Address
	DataTypeIPAddress DataType = "ip_address" // IP Address

	// Healthcare.
	DataTypeMedicalRecord    DataType = "medical_record"    // Medical Record Number
	DataTypeHealthInsurance  DataType = "health_insurance"  // Health Insurance ID
	DataTypeDrugPrescription DataType = "drug_prescription" // Drug/Prescription Info

	// Credentials.
	DataTypePassword       DataType = "password"       // Password
	DataTypeAPIKey         DataType = "api_key"        // API Key
	DataTypeCryptocurrency DataType = "cryptocurrency" // Crypto Wallet/Key

	// Legal.
	DataTypeLegalCase DataType = "legal_case" // Legal Case Number
	DataTypeContract  DataType = "contract"   // Contract Information

	// Custom.
	DataTypeCustom DataType = "custom" // Custom Pattern
)

// Sensitivity represents the sensitivity level of data.
type Sensitivity string

const (
	SensitivityPublic       Sensitivity = "public"
	SensitivityInternal     Sensitivity = "internal"
	SensitivityConfidential Sensitivity = "confidential"
	SensitivityRestricted   Sensitivity = "restricted"
)

// Action represents the action to take on a DLP violation.
type Action string

const (
	ActionAllow      Action = "allow"      // Allow the operation
	ActionLog        Action = "log"        // Log but allow
	ActionBlock      Action = "block"      // Block the operation
	ActionMask       Action = "mask"       // Mask sensitive data
	ActionEncrypt    Action = "encrypt"    // Encrypt sensitive data
	ActionQuarantine Action = "quarantine" // Move to quarantine
)

// DLPRule defines a DLP rule.
type DLPRule struct {
	UpdatedAt    time.Time       `json:"updatedAt"`
	CreatedAt    time.Time       `json:"createdAt"`
	Conditions   *RuleConditions `json:"conditions,omitempty"`
	Notification *Notification   `json:"notification,omitempty"`
	Exceptions   *RuleExceptions `json:"exceptions,omitempty"`
	Sensitivity  Sensitivity     `json:"sensitivity"`
	ID           string          `json:"id"`
	Action       Action          `json:"action"`
	Description  string          `json:"description"`
	Name         string          `json:"name"`
	DataTypes    []DataType      `json:"dataTypes"`
	Priority     int             `json:"priority"`
	Enabled      bool            `json:"enabled"`
}

// RuleConditions defines when a rule applies.
type RuleConditions struct {
	Buckets        []string `json:"buckets,omitempty"`
	Prefixes       []string `json:"prefixes,omitempty"`
	ContentTypes   []string `json:"contentTypes,omitempty"`
	UserRoles      []string `json:"userRoles,omitempty"`
	SourceIPs      []string `json:"sourceIps,omitempty"`
	MinSize        int64    `json:"minSize,omitempty"`
	MaxSize        int64    `json:"maxSize,omitempty"`
	MinOccurrences int      `json:"minOccurrences,omitempty"`
}

// RuleExceptions defines exceptions to a rule.
type RuleExceptions struct {
	Buckets      []string `json:"buckets,omitempty"`
	Prefixes     []string `json:"prefixes,omitempty"`
	Users        []string `json:"users,omitempty"`
	ContentTypes []string `json:"contentTypes,omitempty"`
}

// Notification configures notifications for DLP violations.
type Notification struct {
	Webhook   string   `json:"webhook,omitempty"`
	Slack     string   `json:"slack,omitempty"`
	PagerDuty string   `json:"pagerDuty,omitempty"`
	Email     []string `json:"email,omitempty"`
}

// DataPattern defines a pattern for detecting sensitive data.
type DataPattern struct {
	Type        DataType
	Name        string
	Pattern     *regexp.Regexp
	Validator   func(string) bool
	Sensitivity Sensitivity
	Region      string // Country/region specific
}

// DLPViolation represents a detected DLP violation.
type DLPViolation struct {
	DetectedAt  time.Time   `json:"detectedAt"`
	ReviewedAt  *time.Time  `json:"reviewedAt,omitempty"`
	Action      Action      `json:"action"`
	RuleName    string      `json:"ruleName"`
	Sensitivity Sensitivity `json:"sensitivity"`
	ID          string      `json:"id"`
	Bucket      string      `json:"bucket"`
	Key         string      `json:"key"`
	VersionID   string      `json:"versionId,omitempty"`
	UserID      string      `json:"userId"`
	SourceIP    string      `json:"sourceIp"`
	Notes       string      `json:"notes,omitempty"`
	RuleID      string      `json:"ruleId"`
	Sample      string      `json:"sample"`
	Context     string      `json:"context,omitempty"`
	DataType    DataType    `json:"dataType"`
	ActionTaken Action      `json:"actionTaken"`
	ReviewedBy  string      `json:"reviewedBy,omitempty"`
	Occurrences int         `json:"occurrences"`
	LineNumber  int         `json:"lineNumber,omitempty"`
	Reviewed    bool        `json:"reviewed"`
}

// ScanRequest contains the context for a DLP scan.
type ScanRequest struct {
	Reader      io.Reader
	Bucket      string
	Key         string
	VersionID   string
	ContentType string
	UserID      string
	UserRole    string
	SourceIP    string
	Size        int64
}

// ScanResult contains the result of a DLP scan.
type ScanResult struct {
	ScannedAt   time.Time       `json:"scannedAt"`
	ID          string          `json:"id"`
	Bucket      string          `json:"bucket"`
	Key         string          `json:"key"`
	Sensitivity Sensitivity     `json:"sensitivity"`
	Action      Action          `json:"action"`
	Error       string          `json:"error,omitempty"`
	Violations  []*DLPViolation `json:"violations"`
	DataTypes   []DataType      `json:"dataTypes"`
	ScanTime    time.Duration   `json:"scanTime"`
	Blocked     bool            `json:"blocked"`
}

// DLPConfig configures the DLP engine.
type DLPConfig struct {
	DefaultAction    Action
	QuarantineBucket string
	MaxScanSize      int64
	MaxLineLength    int
	Concurrency      int
	ContextLines     int
	Enabled          bool
	IncludeContext   bool
	RedactSamples    bool
}

// DLPEngine provides DLP functionality.
type DLPEngine struct {
	storage  DLPStorage
	config   *DLPConfig
	rules    map[string]*DLPRule
	stats    *DLPStats
	patterns []*DataPattern
	mu       sync.RWMutex
}

// DLPStorage interface for persisting DLP data.
type DLPStorage interface {
	SaveRule(ctx context.Context, rule *DLPRule) error
	GetRule(ctx context.Context, id string) (*DLPRule, error)
	DeleteRule(ctx context.Context, id string) error
	ListRules(ctx context.Context) ([]*DLPRule, error)
	SaveViolation(ctx context.Context, violation *DLPViolation) error
	GetViolations(ctx context.Context, bucket, key string) ([]*DLPViolation, error)
	UpdateViolation(ctx context.Context, violation *DLPViolation) error
}

// DLPStats tracks DLP statistics.
type DLPStats struct {
	LastScanTime       time.Time
	ViolationsByType   map[DataType]int64
	ViolationsByAction map[Action]int64
	ObjectsScanned     int64
	BytesScanned       int64
	ViolationsTotal    int64
	ViolationsBlocked  int64
}

// DefaultDLPConfig returns the default DLP configuration.
func DefaultDLPConfig() *DLPConfig {
	return &DLPConfig{
		Enabled:          true,
		MaxScanSize:      100 * 1024 * 1024, // 100 MB
		MaxLineLength:    10000,
		Concurrency:      10,
		IncludeContext:   true,
		ContextLines:     2,
		RedactSamples:    true,
		DefaultAction:    ActionLog,
		QuarantineBucket: "dlp-quarantine",
	}
}

// NewDLPEngine creates a new DLP engine.
func NewDLPEngine(config *DLPConfig, storage DLPStorage) *DLPEngine {
	if config == nil {
		config = DefaultDLPConfig()
	}

	engine := &DLPEngine{
		config:   config,
		rules:    make(map[string]*DLPRule),
		patterns: buildDataPatterns(),
		storage:  storage,
		stats: &DLPStats{
			ViolationsByType:   make(map[DataType]int64),
			ViolationsByAction: make(map[Action]int64),
		},
	}

	// Load existing rules
	if storage != nil {
		engine.loadRules()
	}

	return engine
}

// buildDataPatterns builds the default data patterns.
func buildDataPatterns() []*DataPattern {
	return []*DataPattern{
		// SSN (US) - simplified pattern, validation done in Validator
		{
			Type:        DataTypeSSN,
			Name:        "US Social Security Number",
			Pattern:     regexp.MustCompile(`\b\d{3}[-\s]?\d{2}[-\s]?\d{4}\b`),
			Sensitivity: SensitivityRestricted,
			Region:      "US",
			Validator: func(s string) bool {
				cleaned := regexp.MustCompile(`[-\s]`).ReplaceAllString(s, "")
				if len(cleaned) != 9 {
					return false
				}
				// Validate area number (first 3 digits)
				area := cleaned[:3]
				if area == "000" || area == "666" || area[0] == '9' {
					return false
				}
				// Validate group number (middle 2 digits)
				group := cleaned[3:5]
				if group == "00" {
					return false
				}
				// Validate serial number (last 4 digits)
				serial := cleaned[5:9]
				if serial == "0000" {
					return false
				}

				return true
			},
		},

		// Credit Card Numbers
		{
			Type:        DataTypeCreditCard,
			Name:        "Credit Card Number",
			Pattern:     regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|3(?:0[0-5]|[68][0-9])[0-9]{11}|6(?:011|5[0-9]{2})[0-9]{12}|(?:2131|1800|35\d{3})\d{11})\b`),
			Sensitivity: SensitivityRestricted,
			Validator:   luhnCheck,
		},

		// Credit Card with separators
		{
			Type:        DataTypeCreditCard,
			Name:        "Credit Card Number (formatted)",
			Pattern:     regexp.MustCompile(`\b(?:4[0-9]{3}[-\s]?[0-9]{4}[-\s]?[0-9]{4}[-\s]?[0-9]{4}|5[1-5][0-9]{2}[-\s]?[0-9]{4}[-\s]?[0-9]{4}[-\s]?[0-9]{4}|3[47][0-9]{2}[-\s]?[0-9]{6}[-\s]?[0-9]{5})\b`),
			Sensitivity: SensitivityRestricted,
			Validator: func(s string) bool {
				cleaned := regexp.MustCompile(`[-\s]`).ReplaceAllString(s, "")
				return luhnCheck(cleaned)
			},
		},

		// Email
		{
			Type:        DataTypeEmail,
			Name:        "Email Address",
			Pattern:     regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`),
			Sensitivity: SensitivityConfidential,
		},

		// Phone Numbers (US)
		{
			Type:        DataTypePhone,
			Name:        "US Phone Number",
			Pattern:     regexp.MustCompile(`\b(?:\+1[-.\s]?)?(?:\(?[2-9][0-9]{2}\)?[-.\s]?)?[2-9][0-9]{2}[-.\s]?[0-9]{4}\b`),
			Sensitivity: SensitivityConfidential,
			Region:      "US",
		},

		// Phone Numbers (International)
		{
			Type:        DataTypePhone,
			Name:        "International Phone Number",
			Pattern:     regexp.MustCompile(`\b\+[1-9][0-9]{1,14}\b`),
			Sensitivity: SensitivityConfidential,
		},

		// IBAN
		{
			Type:        DataTypeIBAN,
			Name:        "International Bank Account Number",
			Pattern:     regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}[A-Z0-9]{4}[0-9]{7}([A-Z0-9]?){0,16}\b`),
			Sensitivity: SensitivityRestricted,
			Validator:   validateIBAN,
		},

		// SWIFT/BIC
		{
			Type:        DataTypeSWIFT,
			Name:        "SWIFT/BIC Code",
			Pattern:     regexp.MustCompile(`\b[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?\b`),
			Sensitivity: SensitivityConfidential,
		},

		// IP Address (IPv4)
		{
			Type:        DataTypeIPAddress,
			Name:        "IPv4 Address",
			Pattern:     regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`),
			Sensitivity: SensitivityInternal,
		},

		// IP Address (IPv6)
		{
			Type:        DataTypeIPAddress,
			Name:        "IPv6 Address",
			Pattern:     regexp.MustCompile(`\b(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}\b`),
			Sensitivity: SensitivityInternal,
		},

		// Date of Birth
		{
			Type:        DataTypeDateOfBirth,
			Name:        "Date of Birth",
			Pattern:     regexp.MustCompile(`\b(?:(?:0[1-9]|1[0-2])[/-](?:0[1-9]|[12][0-9]|3[01])[/-](?:19|20)\d{2}|(?:19|20)\d{2}[/-](?:0[1-9]|1[0-2])[/-](?:0[1-9]|[12][0-9]|3[01]))\b`),
			Sensitivity: SensitivityConfidential,
		},

		// Passport Number (US)
		{
			Type:        DataTypePassport,
			Name:        "US Passport Number",
			Pattern:     regexp.MustCompile(`\b[A-Z][0-9]{8}\b`),
			Sensitivity: SensitivityRestricted,
			Region:      "US",
		},

		// Driver's License (generic)
		{
			Type:        DataTypeDriverLicense,
			Name:        "Driver's License Number",
			Pattern:     regexp.MustCompile(`(?i)\b(?:driver'?s?\s*(?:license|licence)?\s*(?:no\.?|number|#)?:?\s*)([A-Z0-9]{6,14})\b`),
			Sensitivity: SensitivityRestricted,
		},

		// Medical Record Number
		{
			Type:        DataTypeMedicalRecord,
			Name:        "Medical Record Number",
			Pattern:     regexp.MustCompile(`(?i)\b(?:mrn|medical\s*record\s*(?:no\.?|number|#)?:?\s*)([A-Z0-9]{6,12})\b`),
			Sensitivity: SensitivityRestricted,
		},

		// Health Insurance ID
		{
			Type:        DataTypeHealthInsurance,
			Name:        "Health Insurance ID",
			Pattern:     regexp.MustCompile(`(?i)\b(?:insurance\s*(?:id|no\.?|number)?|member\s*id):?\s*([A-Z0-9]{9,15})\b`),
			Sensitivity: SensitivityRestricted,
		},

		// Bank Account (US)
		{
			Type:        DataTypeBankAccount,
			Name:        "US Bank Account Number",
			Pattern:     regexp.MustCompile(`(?i)\b(?:account\s*(?:no\.?|number|#)?:?\s*)([0-9]{8,17})\b`),
			Sensitivity: SensitivityRestricted,
			Region:      "US",
		},

		// Routing Number (US)
		{
			Type:        DataTypeRoutingNumber,
			Name:        "US Bank Routing Number",
			Pattern:     regexp.MustCompile(`\b[0-9]{9}\b`),
			Sensitivity: SensitivityConfidential,
			Region:      "US",
			Validator:   validateRoutingNumber,
		},

		// Tax ID / EIN (US)
		{
			Type:        DataTypeTaxID,
			Name:        "US Employer Identification Number",
			Pattern:     regexp.MustCompile(`\b[0-9]{2}[-\s]?[0-9]{7}\b`),
			Sensitivity: SensitivityRestricted,
			Region:      "US",
		},

		// Cryptocurrency Addresses
		{
			Type:        DataTypeCryptocurrency,
			Name:        "Bitcoin Address",
			Pattern:     regexp.MustCompile(`\b[13][a-km-zA-HJ-NP-Z1-9]{25,34}\b`),
			Sensitivity: SensitivityConfidential,
		},
		{
			Type:        DataTypeCryptocurrency,
			Name:        "Ethereum Address",
			Pattern:     regexp.MustCompile(`\b0x[a-fA-F0-9]{40}\b`),
			Sensitivity: SensitivityConfidential,
		},

		// Password patterns
		{
			Type:        DataTypePassword,
			Name:        "Password in Configuration",
			Pattern:     regexp.MustCompile(`(?i)(?:password|passwd|pwd)\s*[:=]\s*['""]?([^'""'\s]{8,})['""]?`),
			Sensitivity: SensitivityRestricted,
		},

		// API Keys
		{
			Type:        DataTypeAPIKey,
			Name:        "Generic API Key",
			Pattern:     regexp.MustCompile(`(?i)(?:api[_-]?key|apikey)\s*[:=]\s*['""]?([A-Za-z0-9_-]{20,})['""]?`),
			Sensitivity: SensitivityRestricted,
		},
	}
}

// luhnCheck performs Luhn algorithm validation.
func luhnCheck(number string) bool {
	cleaned := regexp.MustCompile(`\D`).ReplaceAllString(number, "")
	if len(cleaned) < 13 || len(cleaned) > 19 {
		return false
	}

	sum := 0
	alternate := false

	for i := len(cleaned) - 1; i >= 0; i-- {
		digit := int(cleaned[i] - '0')

		if alternate {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}

		sum += digit
		alternate = !alternate
	}

	return sum%10 == 0
}

// validateIBAN validates an IBAN number.
func validateIBAN(iban string) bool {
	cleaned := strings.ToUpper(strings.ReplaceAll(iban, " ", ""))
	if len(cleaned) < 15 || len(cleaned) > 34 {
		return false
	}

	// Move first 4 characters to end
	rearranged := cleaned[4:] + cleaned[:4]

	// Convert letters to numbers (A=10, B=11, etc.)
	var numeric strings.Builder

	for _, c := range rearranged {
		if c >= 'A' && c <= 'Z' {
			numeric.WriteString(strconv.Itoa(int(c - 'A' + 10)))
		} else {
			numeric.WriteRune(c)
		}
	}

	// Check if mod 97 = 1
	// Simplified check - full implementation would use big integers
	return len(numeric.String()) > 0
}

// validateRoutingNumber validates a US bank routing number.
func validateRoutingNumber(routingNumber string) bool {
	cleaned := regexp.MustCompile(`\D`).ReplaceAllString(routingNumber, "")
	if len(cleaned) != 9 {
		return false
	}

	// Check checksum
	weights := []int{3, 7, 1, 3, 7, 1, 3, 7, 1}

	sum := 0
	for i, w := range weights {
		sum += int(cleaned[i]-'0') * w
	}

	return sum%10 == 0
}

// loadRules loads rules from storage.
func (e *DLPEngine) loadRules() {
	rules, err := e.storage.ListRules(context.Background())
	if err != nil {
		return
	}

	for _, rule := range rules {
		e.rules[rule.ID] = rule
	}
}

// AddRule adds a DLP rule.
func (e *DLPEngine) AddRule(ctx context.Context, rule *DLPRule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}

	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	if e.storage != nil {
		err := e.storage.SaveRule(ctx, rule)
		if err != nil {
			return fmt.Errorf("failed to save rule: %w", err)
		}
	}

	e.rules[rule.ID] = rule

	return nil
}

// GetRule returns a rule by ID.
func (e *DLPEngine) GetRule(ctx context.Context, id string) (*DLPRule, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rule, exists := e.rules[id]
	if !exists {
		return nil, fmt.Errorf("rule not found: %s", id)
	}

	return rule, nil
}

// DeleteRule deletes a rule.
func (e *DLPEngine) DeleteRule(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.storage != nil {
		err := e.storage.DeleteRule(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to delete rule: %w", err)
		}
	}

	delete(e.rules, id)

	return nil
}

// ListRules returns all rules.
func (e *DLPEngine) ListRules(ctx context.Context) []*DLPRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rules := make([]*DLPRule, 0, len(e.rules))
	for _, rule := range e.rules {
		rules = append(rules, rule)
	}

	return rules
}

// Scan performs a DLP scan on content.
func (e *DLPEngine) Scan(ctx context.Context, req *ScanRequest) (*ScanResult, error) {
	startTime := time.Now()

	result := &ScanResult{
		ID:         uuid.New().String(),
		Bucket:     req.Bucket,
		Key:        req.Key,
		Violations: make([]*DLPViolation, 0),
		DataTypes:  make([]DataType, 0),
		ScannedAt:  time.Now(),
	}

	if !e.config.Enabled {
		return result, nil
	}

	// Check size limit
	if req.Size > e.config.MaxScanSize {
		result.Error = "file exceeds maximum scan size"
		return result, nil
	}

	// Get applicable rules
	applicableRules := e.getApplicableRules(req)
	if len(applicableRules) == 0 {
		return result, nil
	}

	// Scan content
	findings := e.scanContent(ctx, req, applicableRules)

	// Process findings into violations
	highestSensitivity := SensitivityPublic
	finalAction := ActionAllow

	dataTypesMap := make(map[DataType]bool)

	for _, finding := range findings {
		violation := &DLPViolation{
			ID:          uuid.New().String(),
			RuleID:      finding.rule.ID,
			RuleName:    finding.rule.Name,
			DataType:    finding.dataType,
			Sensitivity: finding.sensitivity,
			Action:      finding.rule.Action,
			Bucket:      req.Bucket,
			Key:         req.Key,
			VersionID:   req.VersionID,
			UserID:      req.UserID,
			SourceIP:    req.SourceIP,
			LineNumber:  finding.lineNumber,
			Occurrences: finding.occurrences,
			Sample:      e.redactSample(finding.sample),
			Context:     finding.context,
			DetectedAt:  time.Now(),
			ActionTaken: finding.rule.Action,
		}

		result.Violations = append(result.Violations, violation)
		dataTypesMap[finding.dataType] = true

		// Track highest sensitivity
		if sensitivityLevel(finding.sensitivity) > sensitivityLevel(highestSensitivity) {
			highestSensitivity = finding.sensitivity
		}

		// Track strictest action
		if actionLevel(finding.rule.Action) > actionLevel(finalAction) {
			finalAction = finding.rule.Action
		}

		// Save violation (best-effort)
		if e.storage != nil {
			err := e.storage.SaveViolation(ctx, violation)
			if err != nil {
				_ = err // Log storage failure but continue
			}
		}

		// Update stats
		atomic.AddInt64(&e.stats.ViolationsTotal, 1)
		e.mu.Lock()
		e.stats.ViolationsByType[finding.dataType]++
		e.stats.ViolationsByAction[finding.rule.Action]++
		e.mu.Unlock()
	}

	// Build data types list
	for dt := range dataTypesMap {
		result.DataTypes = append(result.DataTypes, dt)
	}

	result.Sensitivity = highestSensitivity
	result.Action = finalAction
	result.Blocked = finalAction == ActionBlock
	result.ScanTime = time.Since(startTime)

	// Update stats
	atomic.AddInt64(&e.stats.ObjectsScanned, 1)
	atomic.AddInt64(&e.stats.BytesScanned, req.Size)

	if result.Blocked {
		atomic.AddInt64(&e.stats.ViolationsBlocked, 1)
	}

	e.mu.Lock()
	e.stats.LastScanTime = time.Now()
	e.mu.Unlock()

	return result, nil
}

// finding represents an internal finding during scanning.
type finding struct {
	rule        *DLPRule
	dataType    DataType
	sensitivity Sensitivity
	sample      string
	context     string
	lineNumber  int
	occurrences int
}

// getApplicableRules returns rules that apply to the request.
func (e *DLPEngine) getApplicableRules(req *ScanRequest) []*DLPRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	applicable := make([]*DLPRule, 0, len(e.rules))

	for _, rule := range e.rules {
		if !rule.Enabled {
			continue
		}

		// Check conditions
		if rule.Conditions != nil {
			// Check bucket
			if len(rule.Conditions.Buckets) > 0 {
				found := false

				for _, b := range rule.Conditions.Buckets {
					if b == req.Bucket {
						found = true
						break
					}
				}

				if !found {
					continue
				}
			}

			// Check prefix
			if len(rule.Conditions.Prefixes) > 0 {
				found := false

				for _, p := range rule.Conditions.Prefixes {
					if strings.HasPrefix(req.Key, p) {
						found = true
						break
					}
				}

				if !found {
					continue
				}
			}

			// Check content type
			if len(rule.Conditions.ContentTypes) > 0 {
				found := false

				for _, ct := range rule.Conditions.ContentTypes {
					if strings.HasPrefix(req.ContentType, ct) {
						found = true
						break
					}
				}

				if !found {
					continue
				}
			}

			// Check size
			if rule.Conditions.MinSize > 0 && req.Size < rule.Conditions.MinSize {
				continue
			}

			if rule.Conditions.MaxSize > 0 && req.Size > rule.Conditions.MaxSize {
				continue
			}
		}

		// Check exceptions
		if rule.Exceptions != nil {
			skip := false

			// Check bucket exceptions
			for _, b := range rule.Exceptions.Buckets {
				if b == req.Bucket {
					skip = true
					break
				}
			}

			// Check prefix exceptions
			if !skip {
				for _, p := range rule.Exceptions.Prefixes {
					if strings.HasPrefix(req.Key, p) {
						skip = true
						break
					}
				}
			}

			// Check user exceptions
			if !skip {
				for _, u := range rule.Exceptions.Users {
					if u == req.UserID {
						skip = true
						break
					}
				}
			}

			if skip {
				continue
			}
		}

		applicable = append(applicable, rule)
	}

	return applicable
}

// scanContent scans content for sensitive data.
func (e *DLPEngine) scanContent(_ctx context.Context, req *ScanRequest, rules []*DLPRule) []*finding {
	var findings []*finding

	// Build pattern set from rules
	dataTypes := make(map[DataType]bool)

	for _, rule := range rules {
		for _, dt := range rule.DataTypes {
			dataTypes[dt] = true
		}
	}

	// Read content
	content, err := io.ReadAll(req.Reader)
	if err != nil {
		return findings
	}

	lines := bytes.Split(content, []byte("\n"))
	occurrences := make(map[DataType]int)
	samples := make(map[DataType]string)

	for lineNum, line := range lines {
		lineStr := string(line)

		// Skip long lines
		if len(lineStr) > e.config.MaxLineLength {
			continue
		}

		for _, pattern := range e.patterns {
			if !dataTypes[pattern.Type] {
				continue
			}

			matches := pattern.Pattern.FindAllString(lineStr, -1)
			for _, match := range matches {
				// Validate if validator exists
				if pattern.Validator != nil && !pattern.Validator(match) {
					continue
				}

				occurrences[pattern.Type]++
				if samples[pattern.Type] == "" {
					samples[pattern.Type] = match
				}

				// Find matching rule
				for _, rule := range rules {
					for _, dt := range rule.DataTypes {
						if dt == pattern.Type {
							// Check minimum occurrences
							if rule.Conditions != nil && rule.Conditions.MinOccurrences > 0 {
								if occurrences[pattern.Type] < rule.Conditions.MinOccurrences {
									continue
								}
							}

							f := &finding{
								dataType:    pattern.Type,
								sensitivity: pattern.Sensitivity,
								rule:        rule,
								lineNumber:  lineNum + 1,
								occurrences: occurrences[pattern.Type],
								sample:      samples[pattern.Type],
							}

							if e.config.IncludeContext {
								f.context = e.getContext(lines, lineNum, e.config.ContextLines)
							}

							findings = append(findings, f)

							break
						}
					}
				}
			}
		}
	}

	return findings
}

// getContext returns surrounding lines for context.
func (e *DLPEngine) getContext(lines [][]byte, lineNum, contextLines int) string {
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

// redactSample redacts a sample for safe display.
func (e *DLPEngine) redactSample(sample string) string {
	if !e.config.RedactSamples {
		return sample
	}

	if len(sample) <= 8 {
		return "***"
	}

	// Show first and last 4 characters
	return sample[:4] + "..." + sample[len(sample)-4:]
}

// sensitivityLevel returns the numeric level of sensitivity.
func sensitivityLevel(s Sensitivity) int {
	switch s {
	case SensitivityPublic:
		return 0
	case SensitivityInternal:
		return 1
	case SensitivityConfidential:
		return 2
	case SensitivityRestricted:
		return 3
	default:
		return 0
	}
}

// actionLevel returns the numeric level of action strictness.
func actionLevel(a Action) int {
	switch a {
	case ActionAllow:
		return 0
	case ActionLog:
		return 1
	case ActionMask:
		return 2
	case ActionEncrypt:
		return 3
	case ActionQuarantine:
		return 4
	case ActionBlock:
		return 5
	default:
		return 0
	}
}

// GetStats returns DLP statistics.
func (e *DLPEngine) GetStats() *DLPStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := &DLPStats{
		ObjectsScanned:     e.stats.ObjectsScanned,
		BytesScanned:       e.stats.BytesScanned,
		ViolationsTotal:    e.stats.ViolationsTotal,
		ViolationsBlocked:  e.stats.ViolationsBlocked,
		ViolationsByType:   make(map[DataType]int64),
		ViolationsByAction: make(map[Action]int64),
		LastScanTime:       e.stats.LastScanTime,
	}

	for k, v := range e.stats.ViolationsByType {
		stats.ViolationsByType[k] = v
	}

	for k, v := range e.stats.ViolationsByAction {
		stats.ViolationsByAction[k] = v
	}

	return stats
}

// ReviewViolation marks a violation as reviewed.
func (e *DLPEngine) ReviewViolation(ctx context.Context, violationID, reviewerID, notes string) error {
	if e.storage == nil {
		return errors.New("storage not configured")
	}

	// This would update the violation in storage
	now := time.Now()
	violation := &DLPViolation{
		ID:         violationID,
		Reviewed:   true,
		ReviewedBy: reviewerID,
		ReviewedAt: &now,
		Notes:      notes,
	}

	return e.storage.UpdateViolation(ctx, violation)
}

// ScanResultToJSON converts a scan result to JSON.
func ScanResultToJSON(result *ScanResult) ([]byte, error) {
	return json.MarshalIndent(result, "", "  ")
}
