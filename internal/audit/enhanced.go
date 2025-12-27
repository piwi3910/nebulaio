package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ComplianceMode represents the compliance standard to follow
type ComplianceMode string

const (
	ComplianceNone    ComplianceMode = "none"
	ComplianceSOC2    ComplianceMode = "soc2"
	CompliancePCI     ComplianceMode = "pci"
	ComplianceHIPAA   ComplianceMode = "hipaa"
	ComplianceGDPR    ComplianceMode = "gdpr"
	ComplianceFedRAMP ComplianceMode = "fedramp"
)

// OutputType represents the type of audit output
type OutputType string

const (
	OutputFile    OutputType = "file"
	OutputWebhook OutputType = "webhook"
	OutputSyslog  OutputType = "syslog"
	OutputKafka   OutputType = "kafka"
	OutputS3      OutputType = "s3"
)

// EnhancedConfig holds enhanced audit configuration
type EnhancedConfig struct {
	// Enabled enables audit logging
	Enabled bool `json:"enabled" yaml:"enabled"`

	// ComplianceMode sets the compliance standard
	ComplianceMode ComplianceMode `json:"complianceMode" yaml:"compliance_mode"`

	// Outputs configures where audit logs are sent
	Outputs []OutputConfig `json:"outputs" yaml:"outputs"`

	// RetentionDays is how long to keep audit logs
	RetentionDays int `json:"retentionDays" yaml:"retention_days"`

	// BufferSize is the async buffer size
	BufferSize int `json:"bufferSize" yaml:"buffer_size"`

	// IntegrityEnabled enables cryptographic integrity verification
	IntegrityEnabled bool `json:"integrityEnabled" yaml:"integrity_enabled"`

	// IntegritySecret is the HMAC secret for integrity
	IntegritySecret string `json:"integritySecret" yaml:"integrity_secret"`

	// IncludeRequestBody includes request body in logs (for compliance)
	IncludeRequestBody bool `json:"includeRequestBody" yaml:"include_request_body"`

	// IncludeResponseBody includes response body in logs
	IncludeResponseBody bool `json:"includeResponseBody" yaml:"include_response_body"`

	// MaskSensitiveData masks sensitive data like passwords
	MaskSensitiveData bool `json:"maskSensitiveData" yaml:"mask_sensitive_data"`

	// SensitiveFields are fields to mask
	SensitiveFields []string `json:"sensitiveFields" yaml:"sensitive_fields"`

	// RotationConfig for file rotation
	Rotation RotationConfig `json:"rotation" yaml:"rotation"`

	// FilterRules for filtering audit events
	FilterRules []FilterRule `json:"filterRules" yaml:"filter_rules"`
}

// OutputConfig configures an audit output
type OutputConfig struct {
	// Type is the output type
	Type OutputType `json:"type" yaml:"type"`

	// Enabled enables this output
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Name is a friendly name for this output
	Name string `json:"name" yaml:"name"`

	// Path is the file path (for file output)
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// URL is the endpoint URL (for webhook, syslog, kafka, s3)
	URL string `json:"url,omitempty" yaml:"url,omitempty"`

	// AuthToken for authenticated webhooks
	AuthToken string `json:"authToken,omitempty" yaml:"auth_token,omitempty"`

	// TLS enables TLS for the connection
	TLS bool `json:"tls,omitempty" yaml:"tls,omitempty"`

	// BatchSize for batched outputs
	BatchSize int `json:"batchSize,omitempty" yaml:"batch_size,omitempty"`

	// FlushInterval for batched outputs
	FlushInterval time.Duration `json:"flushInterval,omitempty" yaml:"flush_interval,omitempty"`

	// Format is the output format (json, csv, cef)
	Format string `json:"format,omitempty" yaml:"format,omitempty"`
}

// RotationConfig configures log rotation
type RotationConfig struct {
	// Enabled enables log rotation
	Enabled bool `json:"enabled" yaml:"enabled"`

	// MaxSizeMB is the max file size before rotation
	MaxSizeMB int `json:"maxSizeMB" yaml:"max_size_mb"`

	// MaxBackups is the max number of old files to keep
	MaxBackups int `json:"maxBackups" yaml:"max_backups"`

	// MaxAgeDays is the max age of old files
	MaxAgeDays int `json:"maxAgeDays" yaml:"max_age_days"`

	// Compress compresses rotated files
	Compress bool `json:"compress" yaml:"compress"`
}

// FilterRule defines a filter for audit events
type FilterRule struct {
	// Name is the rule name
	Name string `json:"name" yaml:"name"`

	// Action is the action: include, exclude
	Action string `json:"action" yaml:"action"`

	// EventTypes to match
	EventTypes []EventType `json:"eventTypes,omitempty" yaml:"event_types,omitempty"`

	// Users to match
	Users []string `json:"users,omitempty" yaml:"users,omitempty"`

	// Buckets to match
	Buckets []string `json:"buckets,omitempty" yaml:"buckets,omitempty"`

	// IPs to match
	IPs []string `json:"ips,omitempty" yaml:"ips,omitempty"`

	// Results to match (success, failure)
	Results []Result `json:"results,omitempty" yaml:"results,omitempty"`
}

// EnhancedAuditEvent extends AuditEvent with additional fields
type EnhancedAuditEvent struct {
	AuditEvent

	// SequenceNumber is the event sequence number
	SequenceNumber int64 `json:"sequenceNumber"`

	// SessionID is the user session ID
	SessionID string `json:"sessionId,omitempty"`

	// RequestPath is the full request path
	RequestPath string `json:"requestPath,omitempty"`

	// RequestMethod is the HTTP method
	RequestMethod string `json:"requestMethod,omitempty"`

	// RequestHeaders are the request headers (sanitized)
	RequestHeaders map[string]string `json:"requestHeaders,omitempty"`

	// ResponseStatus is the HTTP status code
	ResponseStatus int `json:"responseStatus,omitempty"`

	// ResponseHeaders are the response headers
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`

	// GeoLocation is the geographic location
	GeoLocation *GeoLocation `json:"geoLocation,omitempty"`

	// Compliance contains compliance-specific info
	Compliance *ComplianceInfo `json:"compliance,omitempty"`

	// Integrity contains integrity verification data
	Integrity *IntegrityInfo `json:"integrity,omitempty"`

	// PreviousHash is the hash of the previous event
	PreviousHash string `json:"previousHash,omitempty"`

	// EventHash is the hash of this event
	EventHash string `json:"eventHash,omitempty"`
}

// GeoLocation contains geographic information
type GeoLocation struct {
	Country     string  `json:"country,omitempty"`
	Region      string  `json:"region,omitempty"`
	City        string  `json:"city,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
	ASN         string  `json:"asn,omitempty"`
	ASNOrg      string  `json:"asnOrg,omitempty"`
}

// ComplianceInfo contains compliance-specific information
type ComplianceInfo struct {
	// Framework is the compliance framework
	Framework ComplianceMode `json:"framework"`

	// ControlIDs are the relevant control IDs
	ControlIDs []string `json:"controlIds,omitempty"`

	// DataClassification is the data classification level
	DataClassification string `json:"dataClassification,omitempty"`

	// IsPrivileged indicates if this was a privileged operation
	IsPrivileged bool `json:"isPrivileged,omitempty"`

	// RequiresApproval indicates if this action required approval
	RequiresApproval bool `json:"requiresApproval,omitempty"`

	// ApprovalID is the approval ID if applicable
	ApprovalID string `json:"approvalId,omitempty"`
}

// IntegrityInfo contains integrity verification data
type IntegrityInfo struct {
	// Algorithm is the hash algorithm
	Algorithm string `json:"algorithm"`

	// Signature is the HMAC signature
	Signature string `json:"signature"`

	// ChainValid indicates if the chain is valid
	ChainValid bool `json:"chainValid"`

	// Timestamp is the server timestamp
	Timestamp time.Time `json:"timestamp"`
}

// EnhancedAuditLogger provides enhanced audit logging capabilities
type EnhancedAuditLogger struct {
	config     EnhancedConfig
	outputs    []AuditOutput
	buffer     chan *EnhancedAuditEvent
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	sequence   int64
	lastHash   string
	stats      AuditStats
	geoLookup  GeoLookup
	store      AuditStore
}

// AuditStats tracks audit logging statistics
type AuditStats struct {
	EventsProcessed  int64 `json:"eventsProcessed"`
	EventsDropped    int64 `json:"eventsDropped"`
	EventsFailed     int64 `json:"eventsFailed"`
	BytesWritten     int64 `json:"bytesWritten"`
	OutputErrors     int64 `json:"outputErrors"`
	IntegrityVerified int64 `json:"integrityVerified"`
	IntegrityFailed  int64 `json:"integrityFailed"`
}

// AuditOutput is the interface for audit outputs
type AuditOutput interface {
	Write(ctx context.Context, event *EnhancedAuditEvent) error
	Flush(ctx context.Context) error
	Close() error
	Name() string
}

// GeoLookup provides geographic lookups
type GeoLookup interface {
	Lookup(ip string) (*GeoLocation, error)
}

// DefaultEnhancedConfig returns sensible defaults
func DefaultEnhancedConfig() EnhancedConfig {
	return EnhancedConfig{
		Enabled:            true,
		ComplianceMode:     ComplianceNone,
		RetentionDays:      90,
		BufferSize:         10000,
		IntegrityEnabled:   true,
		MaskSensitiveData:  true,
		SensitiveFields:    []string{"password", "secret", "token", "credential", "key"},
		Rotation: RotationConfig{
			Enabled:    true,
			MaxSizeMB:  100,
			MaxBackups: 10,
			MaxAgeDays: 30,
			Compress:   true,
		},
	}
}

// NewEnhancedAuditLogger creates a new enhanced audit logger
func NewEnhancedAuditLogger(config EnhancedConfig, store AuditStore, geoLookup GeoLookup) (*EnhancedAuditLogger, error) {
	if config.BufferSize <= 0 {
		config.BufferSize = 10000
	}

	ctx, cancel := context.WithCancel(context.Background())

	logger := &EnhancedAuditLogger{
		config:    config,
		buffer:    make(chan *EnhancedAuditEvent, config.BufferSize),
		ctx:       ctx,
		cancel:    cancel,
		geoLookup: geoLookup,
		store:     store,
	}

	// Initialize outputs
	for _, outputCfg := range config.Outputs {
		if !outputCfg.Enabled {
			continue
		}

		output, err := createOutput(outputCfg)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create output %s: %w", outputCfg.Name, err)
		}
		logger.outputs = append(logger.outputs, output)
	}

	return logger, nil
}

// Start begins processing audit events
func (l *EnhancedAuditLogger) Start() {
	l.wg.Add(1)
	go l.processEvents()
}

// Stop gracefully shuts down the logger
func (l *EnhancedAuditLogger) Stop() error {
	l.cancel()
	close(l.buffer)
	l.wg.Wait()

	var errs []error
	for _, output := range l.outputs {
		if err := output.Flush(context.Background()); err != nil {
			errs = append(errs, err)
		}
		if err := output.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing outputs: %v", errs)
	}
	return nil
}

// Log queues an enhanced audit event
func (l *EnhancedAuditLogger) Log(event *EnhancedAuditEvent) {
	if !l.config.Enabled {
		return
	}

	// Apply filters
	if !l.shouldLog(event) {
		return
	}

	// Enrich event
	l.enrichEvent(event)

	select {
	case l.buffer <- event:
		// Queued successfully
	default:
		atomic.AddInt64(&l.stats.EventsDropped, 1)
	}
}

// LogSync logs an event synchronously
func (l *EnhancedAuditLogger) LogSync(ctx context.Context, event *EnhancedAuditEvent) error {
	if !l.config.Enabled {
		return nil
	}

	if !l.shouldLog(event) {
		return nil
	}

	l.enrichEvent(event)
	return l.processEvent(ctx, event)
}

// Stats returns audit statistics
func (l *EnhancedAuditLogger) Stats() AuditStats {
	return AuditStats{
		EventsProcessed:   atomic.LoadInt64(&l.stats.EventsProcessed),
		EventsDropped:     atomic.LoadInt64(&l.stats.EventsDropped),
		EventsFailed:      atomic.LoadInt64(&l.stats.EventsFailed),
		BytesWritten:      atomic.LoadInt64(&l.stats.BytesWritten),
		OutputErrors:      atomic.LoadInt64(&l.stats.OutputErrors),
		IntegrityVerified: atomic.LoadInt64(&l.stats.IntegrityVerified),
		IntegrityFailed:   atomic.LoadInt64(&l.stats.IntegrityFailed),
	}
}

// Query queries audit events
func (l *EnhancedAuditLogger) Query(ctx context.Context, filter AuditFilter) (*AuditListResult, error) {
	if l.store == nil {
		return nil, fmt.Errorf("no audit store configured")
	}
	return l.store.ListAuditEvents(ctx, filter)
}

// VerifyIntegrity verifies the integrity of the audit chain
func (l *EnhancedAuditLogger) VerifyIntegrity(ctx context.Context, events []*EnhancedAuditEvent) (bool, error) {
	if !l.config.IntegrityEnabled {
		return true, nil
	}

	// Sort by sequence number
	sort.Slice(events, func(i, j int) bool {
		return events[i].SequenceNumber < events[j].SequenceNumber
	})

	var prevHash string
	for _, event := range events {
		// Verify previous hash matches
		if event.PreviousHash != prevHash {
			atomic.AddInt64(&l.stats.IntegrityFailed, 1)
			return false, fmt.Errorf("chain broken at sequence %d", event.SequenceNumber)
		}

		// Verify event hash
		expectedHash := l.computeEventHash(event, prevHash)
		if event.EventHash != expectedHash {
			atomic.AddInt64(&l.stats.IntegrityFailed, 1)
			return false, fmt.Errorf("hash mismatch at sequence %d", event.SequenceNumber)
		}

		// Verify HMAC signature
		if event.Integrity != nil && event.Integrity.Signature != "" {
			if !l.verifySignature(event) {
				atomic.AddInt64(&l.stats.IntegrityFailed, 1)
				return false, fmt.Errorf("signature invalid at sequence %d", event.SequenceNumber)
			}
		}

		prevHash = event.EventHash
		atomic.AddInt64(&l.stats.IntegrityVerified, 1)
	}

	return true, nil
}

// Export exports audit events to a file
func (l *EnhancedAuditLogger) Export(ctx context.Context, filter AuditFilter, format string, writer io.Writer) error {
	result, err := l.Query(ctx, filter)
	if err != nil {
		return err
	}

	switch format {
	case "json":
		enc := json.NewEncoder(writer)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Events)
	case "csv":
		return l.exportCSV(result.Events, writer)
	case "cef":
		return l.exportCEF(result.Events, writer)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// Internal methods

func (l *EnhancedAuditLogger) processEvents() {
	defer l.wg.Done()

	for {
		select {
		case event, ok := <-l.buffer:
			if !ok {
				// Drain remaining
				for event := range l.buffer {
					_ = l.processEvent(l.ctx, event)
				}
				return
			}
			if err := l.processEvent(l.ctx, event); err != nil {
				atomic.AddInt64(&l.stats.EventsFailed, 1)
			}
		case <-l.ctx.Done():
			return
		}
	}
}

func (l *EnhancedAuditLogger) processEvent(ctx context.Context, event *EnhancedAuditEvent) error {
	// Add integrity chain
	if l.config.IntegrityEnabled {
		l.addIntegrity(event)
	}

	// Write to all outputs
	var errs []error
	for _, output := range l.outputs {
		if err := output.Write(ctx, event); err != nil {
			errs = append(errs, err)
			atomic.AddInt64(&l.stats.OutputErrors, 1)
		}
	}

	// Store in database
	if l.store != nil {
		if err := l.store.StoreAuditEvent(ctx, &event.AuditEvent); err != nil {
			errs = append(errs, err)
		}
	}

	atomic.AddInt64(&l.stats.EventsProcessed, 1)

	if len(errs) > 0 {
		return fmt.Errorf("output errors: %v", errs)
	}
	return nil
}

func (l *EnhancedAuditLogger) enrichEvent(event *EnhancedAuditEvent) {
	// Assign sequence number
	event.SequenceNumber = atomic.AddInt64(&l.sequence, 1)

	// Ensure timestamp
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	// Ensure ID
	if event.ID == "" {
		event.ID = fmt.Sprintf("%d-%d", event.Timestamp.UnixNano(), event.SequenceNumber)
	}

	// Add geo location
	if l.geoLookup != nil && event.SourceIP != "" {
		if geo, err := l.geoLookup.Lookup(event.SourceIP); err == nil {
			event.GeoLocation = geo
		}
	}

	// Add compliance info
	if l.config.ComplianceMode != ComplianceNone {
		event.Compliance = l.buildComplianceInfo(event)
	}

	// Mask sensitive data
	if l.config.MaskSensitiveData {
		l.maskSensitiveData(event)
	}
}

func (l *EnhancedAuditLogger) shouldLog(event *EnhancedAuditEvent) bool {
	for _, rule := range l.config.FilterRules {
		if l.matchesFilterRule(&rule, event) {
			return rule.Action != "exclude"
		}
	}
	return true // Default to include
}

func (l *EnhancedAuditLogger) matchesFilterRule(rule *FilterRule, event *EnhancedAuditEvent) bool {
	// Check event types
	if len(rule.EventTypes) > 0 {
		found := false
		for _, et := range rule.EventTypes {
			if et == event.EventType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check users
	if len(rule.Users) > 0 {
		found := false
		for _, u := range rule.Users {
			if u == event.UserIdentity.Username {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check buckets
	if len(rule.Buckets) > 0 {
		found := false
		for _, b := range rule.Buckets {
			if b == event.Resource.Bucket {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check IPs
	if len(rule.IPs) > 0 {
		found := false
		for _, ip := range rule.IPs {
			if ip == event.SourceIP {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check results
	if len(rule.Results) > 0 {
		found := false
		for _, r := range rule.Results {
			if r == event.Result {
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

func (l *EnhancedAuditLogger) addIntegrity(event *EnhancedAuditEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()

	event.PreviousHash = l.lastHash
	event.EventHash = l.computeEventHash(event, l.lastHash)

	// Add signature
	if l.config.IntegritySecret != "" {
		event.Integrity = &IntegrityInfo{
			Algorithm:  "HMAC-SHA256",
			Signature:  l.signEvent(event),
			ChainValid: true,
			Timestamp:  time.Now().UTC(),
		}
	}

	l.lastHash = event.EventHash
}

func (l *EnhancedAuditLogger) computeEventHash(event *EnhancedAuditEvent, prevHash string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s%d%s%s%s%s%s%s",
		prevHash,
		event.SequenceNumber,
		event.Timestamp.Format(time.RFC3339Nano),
		event.EventType,
		event.UserIdentity.Username,
		event.Resource.Bucket,
		event.Resource.Key,
		event.Result)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (l *EnhancedAuditLogger) signEvent(event *EnhancedAuditEvent) string {
	mac := hmac.New(sha256.New, []byte(l.config.IntegritySecret))
	mac.Write([]byte(event.EventHash))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (l *EnhancedAuditLogger) verifySignature(event *EnhancedAuditEvent) bool {
	expected := l.signEvent(event)
	return hmac.Equal([]byte(expected), []byte(event.Integrity.Signature))
}

func (l *EnhancedAuditLogger) buildComplianceInfo(event *EnhancedAuditEvent) *ComplianceInfo {
	info := &ComplianceInfo{
		Framework: l.config.ComplianceMode,
	}

	// Map event types to control IDs based on compliance framework
	switch l.config.ComplianceMode {
	case ComplianceSOC2:
		info.ControlIDs = l.mapSOC2Controls(event)
	case CompliancePCI:
		info.ControlIDs = l.mapPCIControls(event)
	case ComplianceHIPAA:
		info.ControlIDs = l.mapHIPAAControls(event)
	case ComplianceGDPR:
		info.ControlIDs = l.mapGDPRControls(event)
	}

	// Check for privileged operations
	info.IsPrivileged = l.isPrivilegedOperation(event)

	return info
}

func (l *EnhancedAuditLogger) mapSOC2Controls(event *EnhancedAuditEvent) []string {
	var controls []string
	switch {
	case strings.HasPrefix(string(event.EventType), "auth:"):
		controls = append(controls, "CC6.1", "CC6.2") // Access controls
	case strings.HasPrefix(string(event.EventType), "iam:"):
		controls = append(controls, "CC6.3", "CC6.4") // User management
	case strings.HasPrefix(string(event.EventType), "s3:"):
		controls = append(controls, "CC6.7", "CC7.2") // Data access
	}
	return controls
}

func (l *EnhancedAuditLogger) mapPCIControls(event *EnhancedAuditEvent) []string {
	var controls []string
	switch {
	case strings.HasPrefix(string(event.EventType), "auth:"):
		controls = append(controls, "8.1", "8.2") // Authentication
	case strings.HasPrefix(string(event.EventType), "iam:"):
		controls = append(controls, "8.5", "8.6") // Identity management
	case strings.HasPrefix(string(event.EventType), "s3:"):
		controls = append(controls, "10.2", "10.3") // Audit trails
	}
	return controls
}

func (l *EnhancedAuditLogger) mapHIPAAControls(event *EnhancedAuditEvent) []string {
	var controls []string
	switch {
	case strings.HasPrefix(string(event.EventType), "auth:"):
		controls = append(controls, "164.312(d)")
	case strings.HasPrefix(string(event.EventType), "s3:"):
		controls = append(controls, "164.312(b)", "164.312(c)")
	}
	return controls
}

func (l *EnhancedAuditLogger) mapGDPRControls(event *EnhancedAuditEvent) []string {
	var controls []string
	switch {
	case strings.HasPrefix(string(event.EventType), "s3:"):
		controls = append(controls, "Art. 30", "Art. 32")
	case strings.HasPrefix(string(event.EventType), "iam:"):
		controls = append(controls, "Art. 25", "Art. 32")
	}
	return controls
}

func (l *EnhancedAuditLogger) isPrivilegedOperation(event *EnhancedAuditEvent) bool {
	privilegedOps := []EventType{
		EventUserCreated,
		EventUserDeleted,
		EventPolicyCreated,
		EventPolicyDeleted,
		EventAccessKeyCreated,
		EventAccessKeyDeleted,
	}
	for _, op := range privilegedOps {
		if event.EventType == op {
			return true
		}
	}
	return false
}

func (l *EnhancedAuditLogger) maskSensitiveData(event *EnhancedAuditEvent) {
	for _, field := range l.config.SensitiveFields {
		if event.Extra != nil {
			if _, ok := event.Extra[field]; ok {
				event.Extra[field] = "***MASKED***"
			}
		}
		if event.RequestHeaders != nil {
			for k := range event.RequestHeaders {
				if strings.EqualFold(k, field) || strings.Contains(strings.ToLower(k), field) {
					event.RequestHeaders[k] = "***MASKED***"
				}
			}
		}
	}
}

func (l *EnhancedAuditLogger) exportCSV(events []AuditEvent, writer io.Writer) error {
	// Write header
	header := "id,timestamp,event_type,user,source_ip,bucket,key,result,duration_ms\n"
	if _, err := writer.Write([]byte(header)); err != nil {
		return err
	}

	// Write rows
	for _, e := range events {
		row := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%d\n",
			e.ID,
			e.Timestamp.Format(time.RFC3339),
			e.EventType,
			e.UserIdentity.Username,
			e.SourceIP,
			e.Resource.Bucket,
			e.Resource.Key,
			e.Result,
			e.DurationMS,
		)
		if _, err := writer.Write([]byte(row)); err != nil {
			return err
		}
	}
	return nil
}

func (l *EnhancedAuditLogger) exportCEF(events []AuditEvent, writer io.Writer) error {
	// CEF (Common Event Format) for SIEM integration
	for _, e := range events {
		severity := 1
		if e.Result == ResultFailure {
			severity = 5
		}

		cef := fmt.Sprintf("CEF:0|NebulaIO|ObjectStorage|1.0|%s|%s|%d|"+
			"src=%s suser=%s cs1=%s cs2=%s outcome=%s\n",
			e.EventType,
			e.Action,
			severity,
			e.SourceIP,
			e.UserIdentity.Username,
			e.Resource.Bucket,
			e.Resource.Key,
			e.Result,
		)
		if _, err := writer.Write([]byte(cef)); err != nil {
			return err
		}
	}
	return nil
}

// Output implementations

// FileOutput writes audit events to a file
type FileOutput struct {
	name     string
	path     string
	file     *os.File
	mu       sync.Mutex
	rotation RotationConfig
	size     int64
}

func newFileOutput(cfg OutputConfig, rotation RotationConfig) (*FileOutput, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("file path is required")
	}

	// Ensure directory exists
	dir := filepath.Dir(cfg.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	info, _ := f.Stat()
	size := int64(0)
	if info != nil {
		size = info.Size()
	}

	return &FileOutput{
		name:     cfg.Name,
		path:     cfg.Path,
		file:     f,
		rotation: rotation,
		size:     size,
	}, nil
}

func (o *FileOutput) Write(ctx context.Context, event *EnhancedAuditEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Check rotation
	if o.rotation.Enabled && o.size > int64(o.rotation.MaxSizeMB)*1024*1024 {
		if err := o.rotate(); err != nil {
			return err
		}
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	n, err := o.file.Write(data)
	if err != nil {
		return err
	}
	o.size += int64(n)

	return nil
}

func (o *FileOutput) rotate() error {
	_ = o.file.Close()

	// Rename current file
	timestamp := time.Now().Format("20060102-150405")
	rotatedPath := fmt.Sprintf("%s.%s", o.path, timestamp)
	if err := os.Rename(o.path, rotatedPath); err != nil {
		return err
	}

	// Open new file
	f, err := os.OpenFile(o.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	o.file = f
	o.size = 0

	// TODO: Compress old file if configured

	// TODO: Clean up old files based on MaxBackups and MaxAgeDays

	return nil
}

func (o *FileOutput) Flush(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.file.Sync()
}

func (o *FileOutput) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.file.Close()
}

func (o *FileOutput) Name() string {
	return o.name
}

// WebhookOutput sends audit events to a webhook
type WebhookOutput struct {
	name      string
	url       string
	authToken string
	client    *http.Client
	batch     []*EnhancedAuditEvent
	batchSize int
	mu        sync.Mutex
}

func newWebhookOutput(cfg OutputConfig) (*WebhookOutput, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("webhook URL is required")
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	return &WebhookOutput{
		name:      cfg.Name,
		url:       cfg.URL,
		authToken: cfg.AuthToken,
		client:    &http.Client{Timeout: 30 * time.Second},
		batch:     make([]*EnhancedAuditEvent, 0, batchSize),
		batchSize: batchSize,
	}, nil
}

func (o *WebhookOutput) Write(ctx context.Context, event *EnhancedAuditEvent) error {
	o.mu.Lock()
	o.batch = append(o.batch, event)
	shouldFlush := len(o.batch) >= o.batchSize
	o.mu.Unlock()

	if shouldFlush {
		return o.Flush(ctx)
	}
	return nil
}

func (o *WebhookOutput) Flush(ctx context.Context) error {
	o.mu.Lock()
	if len(o.batch) == 0 {
		o.mu.Unlock()
		return nil
	}
	batch := o.batch
	o.batch = make([]*EnhancedAuditEvent, 0, o.batchSize)
	o.mu.Unlock()

	data, err := json.Marshal(batch)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.url, strings.NewReader(string(data)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	if o.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+o.authToken)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (o *WebhookOutput) Close() error {
	return nil
}

func (o *WebhookOutput) Name() string {
	return o.name
}

// createOutput creates an output based on configuration
func createOutput(cfg OutputConfig) (AuditOutput, error) {
	switch cfg.Type {
	case OutputFile:
		return newFileOutput(cfg, RotationConfig{
			Enabled:    true,
			MaxSizeMB:  100,
			MaxBackups: 10,
		})
	case OutputWebhook:
		return newWebhookOutput(cfg)
	default:
		return nil, fmt.Errorf("unsupported output type: %s", cfg.Type)
	}
}
