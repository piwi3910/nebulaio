package audit

import (
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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

	"github.com/rs/zerolog/log"
)

// ComplianceMode represents the compliance standard to follow.
type ComplianceMode string

const (
	ComplianceNone    ComplianceMode = "none"
	ComplianceSOC2    ComplianceMode = "soc2"
	CompliancePCI     ComplianceMode = "pci"
	ComplianceHIPAA   ComplianceMode = "hipaa"
	ComplianceGDPR    ComplianceMode = "gdpr"
	ComplianceFedRAMP ComplianceMode = "fedramp"
)

// OutputType represents the type of audit output.
type OutputType string

const (
	OutputFile    OutputType = "file"
	OutputWebhook OutputType = "webhook"
	OutputSyslog  OutputType = "syslog"
	OutputKafka   OutputType = "kafka"
	OutputS3      OutputType = "s3"
)

// Default configuration values.
const (
	defaultRetentionDays      = 90
	defaultBufferSize         = 10000
	defaultRotationMaxSizeMB  = 100
	defaultRotationMaxBackups = 10
	defaultRotationMaxAgeDays = 30
	// File permission constants.
	auditDirPermissions  = 0750
	auditFilePermissions = 0600
	// Default flush interval in seconds.
	defaultFlushIntervalSec = 30
	// HTTP client timeout threshold.
	httpTimeoutThreshold = 400
	// Default webhook batch size.
	defaultWebhookBatchSize = 100
)

// EnhancedConfig holds enhanced audit configuration.
type EnhancedConfig struct {
	IntegritySecret     string         `json:"integritySecret" yaml:"integrity_secret"`
	ComplianceMode      ComplianceMode `json:"complianceMode" yaml:"compliance_mode"`
	Outputs             []OutputConfig `json:"outputs" yaml:"outputs"`
	FilterRules         []FilterRule   `json:"filterRules" yaml:"filter_rules"`
	SensitiveFields     []string       `json:"sensitiveFields" yaml:"sensitive_fields"`
	Rotation            RotationConfig `json:"rotation" yaml:"rotation"`
	RetentionDays       int            `json:"retentionDays" yaml:"retention_days"`
	BufferSize          int            `json:"bufferSize" yaml:"buffer_size"`
	IncludeRequestBody  bool           `json:"includeRequestBody" yaml:"include_request_body"`
	IncludeResponseBody bool           `json:"includeResponseBody" yaml:"include_response_body"`
	MaskSensitiveData   bool           `json:"maskSensitiveData" yaml:"mask_sensitive_data"`
	IntegrityEnabled    bool           `json:"integrityEnabled" yaml:"integrity_enabled"`
	Enabled             bool           `json:"enabled" yaml:"enabled"`
}

// OutputConfig configures an audit output.
type OutputConfig struct {
	Type          OutputType    `json:"type" yaml:"type"`
	Name          string        `json:"name" yaml:"name"`
	Path          string        `json:"path,omitempty" yaml:"path,omitempty"`
	URL           string        `json:"url,omitempty" yaml:"url,omitempty"`
	AuthToken     string        `json:"authToken,omitempty" yaml:"auth_token,omitempty"`
	Format        string        `json:"format,omitempty" yaml:"format,omitempty"`
	BatchSize     int           `json:"batchSize,omitempty" yaml:"batch_size,omitempty"`
	FlushInterval time.Duration `json:"flushInterval,omitempty" yaml:"flush_interval,omitempty"`
	Enabled       bool          `json:"enabled" yaml:"enabled"`
	TLS           bool          `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// RotationConfig configures log rotation.
type RotationConfig struct {
	MaxSizeMB  int  `json:"maxSizeMB" yaml:"max_size_mb"`
	MaxBackups int  `json:"maxBackups" yaml:"max_backups"`
	MaxAgeDays int  `json:"maxAgeDays" yaml:"max_age_days"`
	Enabled    bool `json:"enabled" yaml:"enabled"`
	Compress   bool `json:"compress" yaml:"compress"`
}

// FilterRule defines a filter for audit events.
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

// EnhancedAuditEvent extends AuditEvent with additional fields.
type EnhancedAuditEvent struct {
	AuditEvent
	// 8-byte fields (pointers, int64)
	Integrity      *IntegrityInfo  `json:"integrity,omitempty"`
	Compliance     *ComplianceInfo `json:"compliance,omitempty"`
	GeoLocation    *GeoLocation    `json:"geoLocation,omitempty"`
	SequenceNumber int64           `json:"sequenceNumber"`
	// Map fields (8 bytes on 64-bit)
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	RequestHeaders  map[string]string `json:"requestHeaders,omitempty"`
	// String fields
	RequestPath   string `json:"requestPath,omitempty"`
	RequestMethod string `json:"requestMethod,omitempty"`
	SessionID     string `json:"sessionId,omitempty"`
	PreviousHash  string `json:"previousHash,omitempty"`
	EventHash     string `json:"eventHash,omitempty"`
	// 4-byte fields
	ResponseStatus int `json:"responseStatus,omitempty"`
}

// GeoLocation contains geographic information.
type GeoLocation struct {
	Country   string  `json:"country,omitempty"`
	Region    string  `json:"region,omitempty"`
	City      string  `json:"city,omitempty"`
	ASN       string  `json:"asn,omitempty"`
	ASNOrg    string  `json:"asnOrg,omitempty"`
	Latitude  float64 `json:"latitude,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`
}

// ComplianceInfo contains compliance-specific information.
type ComplianceInfo struct {
	Framework          ComplianceMode `json:"framework"`
	DataClassification string         `json:"dataClassification,omitempty"`
	ApprovalID         string         `json:"approvalId,omitempty"`
	ControlIDs         []string       `json:"controlIds,omitempty"`
	IsPrivileged       bool           `json:"isPrivileged,omitempty"`
	RequiresApproval   bool           `json:"requiresApproval,omitempty"`
}

// IntegrityInfo contains integrity verification data.
type IntegrityInfo struct {
	Timestamp  time.Time `json:"timestamp"`
	Algorithm  string    `json:"algorithm"`
	Signature  string    `json:"signature"`
	ChainValid bool      `json:"chainValid"`
}

// EnhancedAuditLogger provides enhanced audit logging capabilities.
type EnhancedAuditLogger struct {
	// 8-byte fields (pointers, slices, channels, int64, functions)
	ctx       context.Context
	geoLookup GeoLookup
	store     AuditStore
	buffer    chan *EnhancedAuditEvent
	cancel    context.CancelFunc
	outputs   []AuditOutput
	sequence  int64
	// Strings
	lastHash string
	// Structs
	config EnhancedConfig
	stats  AuditStats
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

// AuditStats tracks audit logging statistics.
type AuditStats struct {
	EventsProcessed   int64 `json:"eventsProcessed"`
	EventsDropped     int64 `json:"eventsDropped"`
	EventsFailed      int64 `json:"eventsFailed"`
	BytesWritten      int64 `json:"bytesWritten"`
	OutputErrors      int64 `json:"outputErrors"`
	IntegrityVerified int64 `json:"integrityVerified"`
	IntegrityFailed   int64 `json:"integrityFailed"`
}

// AuditOutput is the interface for audit outputs.
type AuditOutput interface {
	Write(ctx context.Context, event *EnhancedAuditEvent) error
	Flush(ctx context.Context) error
	Close() error
	Name() string
}

// GeoLookup provides geographic lookups.
type GeoLookup interface {
	Lookup(ip string) (*GeoLocation, error)
}

// DefaultEnhancedConfig returns sensible defaults.
func DefaultEnhancedConfig() EnhancedConfig {
	return EnhancedConfig{
		Enabled:           true,
		ComplianceMode:    ComplianceNone,
		RetentionDays:     defaultRetentionDays,
		BufferSize:        defaultBufferSize,
		IntegrityEnabled:  true,
		MaskSensitiveData: true,
		SensitiveFields:   []string{"password", "secret", "token", "credential", "key"},
		Rotation: RotationConfig{
			Enabled:    true,
			MaxSizeMB:  defaultRotationMaxSizeMB,
			MaxBackups: defaultRotationMaxBackups,
			MaxAgeDays: defaultRotationMaxAgeDays,
			Compress:   true,
		},
	}
}

// NewEnhancedAuditLogger creates a new enhanced audit logger.
func NewEnhancedAuditLogger(config EnhancedConfig, store AuditStore, geoLookup GeoLookup) (*EnhancedAuditLogger, error) {
	if config.BufferSize <= 0 {
		config.BufferSize = defaultBufferSize
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

		output, err := CreateOutput(outputCfg)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create output %s: %w", outputCfg.Name, err)
		}

		logger.outputs = append(logger.outputs, output)
	}

	return logger, nil
}

// Start begins processing audit events.
func (l *EnhancedAuditLogger) Start() {
	l.wg.Add(1)

	go l.processEvents()
}

// Stop gracefully shuts down the logger.
func (l *EnhancedAuditLogger) Stop() error {
	l.cancel()
	close(l.buffer)
	l.wg.Wait()

	var errs []error

	for _, output := range l.outputs {
		err := output.Flush(context.Background())
		if err != nil {
			errs = append(errs, err)
		}

		err = output.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing outputs: %v", errs)
	}

	return nil
}

// Log queues an enhanced audit event.
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

// LogSync logs an event synchronously.
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

// Stats returns audit statistics.
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

// Query queries audit events.
func (l *EnhancedAuditLogger) Query(ctx context.Context, filter AuditFilter) (*AuditListResult, error) {
	if l.store == nil {
		return nil, errors.New("no audit store configured")
	}

	return l.store.ListAuditEvents(ctx, filter)
}

// VerifyIntegrity verifies the integrity of the audit chain.
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

// Export exports audit events to a file.
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

			err := l.processEvent(l.ctx, event)
			if err != nil {
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
		err := output.Write(ctx, event)
		if err != nil {
			errs = append(errs, err)

			atomic.AddInt64(&l.stats.OutputErrors, 1)
		}
	}

	// Store in database
	if l.store != nil {
		err := l.store.StoreAuditEvent(ctx, &event.AuditEvent)
		if err != nil {
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
		geo, err := l.geoLookup.Lookup(event.SourceIP)
		if err == nil {
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
	if !l.matchesEventTypes(rule.EventTypes, event.EventType) {
		return false
	}

	if !l.matchesUsers(rule.Users, event.UserIdentity.Username) {
		return false
	}

	if !l.matchesBuckets(rule.Buckets, event.Resource.Bucket) {
		return false
	}

	if !l.matchesIPs(rule.IPs, event.SourceIP) {
		return false
	}

	if !l.matchesResults(rule.Results, event.Result) {
		return false
	}

	return true
}

func (l *EnhancedAuditLogger) matchesEventTypes(allowed []EventType, actual EventType) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, et := range allowed {
		if et == actual {
			return true
		}
	}
	return false
}

func (l *EnhancedAuditLogger) matchesUsers(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, u := range allowed {
		if u == actual {
			return true
		}
	}
	return false
}

func (l *EnhancedAuditLogger) matchesBuckets(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, b := range allowed {
		if b == actual {
			return true
		}
	}
	return false
}

func (l *EnhancedAuditLogger) matchesIPs(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, ip := range allowed {
		if ip == actual {
			return true
		}
	}
	return false
}

func (l *EnhancedAuditLogger) matchesResults(allowed []Result, actual Result) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, r := range allowed {
		if r == actual {
			return true
		}
	}
	return false
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

	_, err := writer.Write([]byte(header))
	if err != nil {
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

		_, err := writer.Write([]byte(row))
		if err != nil {
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
		_, err := writer.Write([]byte(cef))
		if err != nil {
			return err
		}
	}

	return nil
}

// Output implementations

// FileOutput writes audit events to a file.
type FileOutput struct {
	file     *os.File
	name     string
	path     string
	rotation RotationConfig
	size     int64
	mu       sync.Mutex
}

func newFileOutput(cfg OutputConfig, rotation RotationConfig) (*FileOutput, error) {
	if cfg.Path == "" {
		return nil, errors.New("file path is required")
	}

	// Ensure directory exists with secure permissions
	dir := filepath.Dir(cfg.Path)
	err := os.MkdirAll(dir, auditDirPermissions)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, auditFilePermissions)
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
		err := o.rotate()
		if err != nil {
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
	err := os.Rename(o.path, rotatedPath)
	if err != nil {
		return err
	}

	// Open new file with secure permissions
	f, err := os.OpenFile(o.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, auditFilePermissions)
	if err != nil {
		return err
	}

	o.file = f
	o.size = 0

	// Compress and cleanup in a single goroutine to ensure proper sequencing
	// This prevents the race condition where cleanup could run before compression finishes
	//
	// INTENTIONAL DESIGN: This goroutine does NOT respect context cancellation.
	// Compression and cleanup are critical audit log maintenance operations that should
	// complete even if the original write request is cancelled. This is a "fire-and-forget"
	// background task that ensures audit log integrity and disk space management.
	//
	// The goroutine will always terminate because:
	//   - compressFile() performs bounded file I/O operations
	//   - cleanupOldFiles() performs bounded directory operations
	//   - No long-running loops or blocking operations
	go func() {
		var compressionSucceeded = true

		// First, compress the rotated file if configured
		if o.rotation.Compress {
			err := o.compressFile(rotatedPath)
			if err != nil {
				log.Error().
					Err(err).
					Str("file", rotatedPath).
					Msg("Audit log compression failed, skipping cleanup to preserve uncompressed file")

				compressionSucceeded = false
			}
		}

		// Only run cleanup if:
		// 1. Compression is disabled (no compression to fail), OR
		// 2. Compression is enabled AND succeeded
		// This ensures we don't delete uncompressed files if compression failed
		if compressionSucceeded && (o.rotation.MaxBackups > 0 || o.rotation.MaxAgeDays > 0) {
			o.cleanupOldFiles()
		}
	}()

	return nil
}

// compressFile compresses a rotated log file using gzip with streaming
// to avoid loading the entire file into memory (important for large audit logs)
// Returns an error if compression fails, allowing the caller to handle the failure appropriately.
func (o *FileOutput) compressFile(filePath string) error {
	// Open the original file for streaming read
	//nolint:gosec // G304: filePath is from trusted log rotation config
	inFile, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open log file for compression: %w", err)
	}

	defer func() {
		err := inFile.Close()
		if err != nil {
			log.Warn().Err(err).Str("file", filePath).Msg("Failed to close log file after compression")
		}
	}()

	// Create the compressed file
	compressedPath := filePath + ".gz"

	//nolint:gosec // G304: compressedPath is from trusted log rotation config
	outFile, err := os.Create(compressedPath)
	if err != nil {
		return fmt.Errorf("failed to create compressed log file: %w", err)
	}

	// Create gzip writer for streaming compression
	gzWriter := gzip.NewWriter(outFile)

	// Stream compress the file instead of loading it all into memory
	// This significantly reduces memory pressure for large audit logs
	_, err = io.Copy(gzWriter, inFile)
	if err != nil {
		// Close both writers before cleaning up (errors ignored during cleanup)
		_ = gzWriter.Close()
		_ = outFile.Close()
		_ = os.Remove(compressedPath)

		return fmt.Errorf("failed to stream compress data: %w", err)
	}

	// Close gzip writer to flush all data
	err = gzWriter.Close()
	if err != nil {
		_ = outFile.Close()
		_ = os.Remove(compressedPath)

		return fmt.Errorf("failed to finalize gzip compression: %w", err)
	}

	// Close the output file
	err = outFile.Close()
	if err != nil {
		_ = os.Remove(compressedPath)
		return fmt.Errorf("failed to close compressed log file: %w", err)
	}

	// Remove the original uncompressed file
	err = os.Remove(filePath)
	if err != nil {
		// Don't fail the entire compression if we can't remove the original
		// Log the warning but return success since compression itself succeeded
		log.Warn().Err(err).Str("file", filePath).Msg("Failed to remove original log file after compression")
	}

	log.Info().
		Str("original", filePath).
		Str("compressed", compressedPath).
		Msg("Audit log file compressed successfully")

	return nil
}

// cleanupOldFiles removes old log files based on MaxBackups and MaxAgeDays.
func (o *FileOutput) cleanupOldFiles() {
	dir := filepath.Dir(o.path)
	baseName := filepath.Base(o.path)

	rotatedFiles := o.findRotatedFiles(dir, baseName)
	o.removeOldFiles(rotatedFiles)
}

func (o *FileOutput) findRotatedFiles(dir, baseName string) []rotatedFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("Failed to read log directory for cleanup")
		return nil
	}

	var rotatedFiles []rotatedFile

	for _, entry := range entries {
		if rf := o.processLogEntry(entry, dir, baseName); rf != nil {
			rotatedFiles = append(rotatedFiles, *rf)
		}
	}

	sort.Slice(rotatedFiles, func(i, j int) bool {
		return rotatedFiles[i].modTime.After(rotatedFiles[j].modTime)
	})

	return rotatedFiles
}

func (o *FileOutput) processLogEntry(entry os.DirEntry, dir, baseName string) *rotatedFile {
	if entry.IsDir() {
		return nil
	}

	name := entry.Name()
	if name == baseName || !strings.HasPrefix(name, baseName+".") {
		return nil
	}

	info, err := entry.Info()
	if err != nil {
		return nil
	}

	return &rotatedFile{
		path:    filepath.Join(dir, name),
		modTime: info.ModTime(),
	}
}

func (o *FileOutput) removeOldFiles(rotatedFiles []rotatedFile) {
	now := time.Now()

	for i, rf := range rotatedFiles {
		if o.shouldDeleteFile(i, rf.modTime, now) {
			err := os.Remove(rf.path)
			if err != nil {
				log.Warn().Err(err).Str("file", rf.path).Msg("Failed to remove old log file during cleanup")
			}
		}
	}
}

func (o *FileOutput) shouldDeleteFile(index int, modTime, now time.Time) bool {
	if o.rotation.MaxBackups > 0 && index >= o.rotation.MaxBackups {
		return true
	}

	if o.rotation.MaxAgeDays > 0 {
		age := now.Sub(modTime)
		if age > time.Duration(o.rotation.MaxAgeDays)*24*time.Hour {
			return true
		}
	}

	return false
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

// WebhookOutput sends audit events to a webhook.
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
		return nil, errors.New("webhook URL is required")
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = defaultWebhookBatchSize
	}

	return &WebhookOutput{
		name:      cfg.Name,
		url:       cfg.URL,
		authToken: cfg.AuthToken,
		client:    &http.Client{Timeout: defaultFlushIntervalSec * time.Second},
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, strings.NewReader(string(data)))
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

	if resp.StatusCode >= httpTimeoutThreshold {
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

// CreateOutput creates an output based on configuration.
func CreateOutput(cfg OutputConfig) (AuditOutput, error) {
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
