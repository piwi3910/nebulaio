// Package analytics provides access analytics with anomaly detection for NebulaIO
package analytics

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AccessType represents the type of access operation.
type AccessType string

const (
	AccessTypeRead      AccessType = "READ"
	AccessTypeWrite     AccessType = "WRITE"
	AccessTypeDelete    AccessType = "DELETE"
	AccessTypeList      AccessType = "LIST"
	AccessTypeHead      AccessType = "HEAD"
	AccessTypeCopy      AccessType = "COPY"
	AccessTypeMultipart AccessType = "MULTIPART"
	AccessTypeACL       AccessType = "ACL"
	AccessTypePolicy    AccessType = "POLICY"
)

// AnomalyType represents the type of anomaly detected.
type AnomalyType string

const (
	AnomalyTypeUnusualTime          AnomalyType = "UNUSUAL_TIME"
	AnomalyTypeUnusualLocation      AnomalyType = "UNUSUAL_LOCATION"
	AnomalyTypeHighVolume           AnomalyType = "HIGH_VOLUME"
	AnomalyTypeUnusualPattern       AnomalyType = "UNUSUAL_PATTERN"
	AnomalyTypeSensitiveAccess      AnomalyType = "SENSITIVE_ACCESS"
	AnomalyTypeFirstTimeAccess      AnomalyType = "FIRST_TIME_ACCESS"
	AnomalyTypeBulkOperation        AnomalyType = "BULK_OPERATION"
	AnomalyTypeRapidChanges         AnomalyType = "RAPID_CHANGES"
	AnomalyTypeUnusualUserAgent     AnomalyType = "UNUSUAL_USER_AGENT"
	AnomalyTypeGeoVelocity          AnomalyType = "GEO_VELOCITY"
	AnomalyTypePermissionEscalation AnomalyType = "PERMISSION_ESCALATION"
	AnomalyTypeDataExfiltration     AnomalyType = "DATA_EXFILTRATION"
)

// Severity represents the severity level of an anomaly.
type Severity string

const (
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

// Analytics configuration constants.
const (
	defaultMinEventsForBaseline   = 100
	defaultAnomalyThreshold       = 3.0
	defaultBatchSize              = 1000
	defaultEventChannelSize       = 10000
	thresholdUnusualTime          = 2.0
	thresholdHighVolume           = 5.0
	thresholdHighVolumeWindowMins = 15
	thresholdBulkDelete           = 100
	thresholdBulkDeleteWindowMins = 5
	thresholdGeoVelocity          = 500
	thresholdDataExfiltration     = 10
	thresholdRapidChanges         = 5
	thresholdRapidChangesWindowMins = 10
	baselineDays                  = 24
	minBaselineEvents             = 2
	heatmapDegrees                = 180
	embeddingDim                  = 1024
	minGeoEventsForCheck          = 2
	degreesToRadiansDivisor       = 180
	haversineDivisor              = 2
	bytesPerGB                    = 1024 * 1024 * 1024
)

// AccessEvent represents a single access event.
type AccessEvent struct {
	Timestamp    time.Time         `json:"timestamp"`
	GeoLocation  *GeoLocation      `json:"geo_location,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	RequestID    string            `json:"request_id"`
	SessionID    string            `json:"session_id,omitempty"`
	Key          string            `json:"key"`
	AccessType   AccessType        `json:"access_type"`
	SourceIP     string            `json:"source_ip"`
	UserAgent    string            `json:"user_agent"`
	Region       string            `json:"region"`
	UserID       string            `json:"user_id"`
	AccessKeyID  string            `json:"access_key_id"`
	TenantID     string            `json:"tenant_id,omitempty"`
	Bucket       string            `json:"bucket"`
	ErrorCode    string            `json:"error_code,omitempty"`
	ID           string            `json:"id"`
	StatusCode   int               `json:"status_code"`
	Duration     time.Duration     `json:"duration_ms"`
	BytesWritten int64             `json:"bytes_written"`
	BytesRead    int64             `json:"bytes_read"`
}

// GeoLocation represents geographic location information.
type GeoLocation struct {
	Country   string  `json:"country"`
	Region    string  `json:"region"`
	City      string  `json:"city"`
	ISP       string  `json:"isp,omitempty"`
	ASN       string  `json:"asn,omitempty"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Anomaly represents a detected anomaly.
type Anomaly struct {
	Timestamp      time.Time         `json:"timestamp"`
	Observed       *BaselineMetrics  `json:"observed,omitempty"`
	Baseline       *BaselineMetrics  `json:"baseline,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	AcknowledgedAt *time.Time        `json:"acknowledged_at,omitempty"`
	Description    string            `json:"description"`
	Severity       Severity          `json:"severity"`
	ID             string            `json:"id"`
	AcknowledgedBy string            `json:"acknowledged_by,omitempty"`
	UserID         string            `json:"user_id"`
	Resolution     string            `json:"resolution,omitempty"`
	Type           AnomalyType       `json:"type"`
	Events         []*AccessEvent    `json:"events"`
	Score          float64           `json:"score"`
	Acknowledged   bool              `json:"acknowledged"`
}

// BaselineMetrics represents baseline behavior metrics.
type BaselineMetrics struct {
	LastUpdated        time.Time          `json:"last_updated"`
	CommonOperations   map[AccessType]int `json:"common_operations"`
	ActiveHours        []int              `json:"active_hours"`
	CommonBuckets      []string           `json:"common_buckets"`
	CommonIPs          []string           `json:"common_ips"`
	CommonUserAgents   []string           `json:"common_user_agents"`
	CommonRegions      []string           `json:"common_regions"`
	AvgRequestsPerHour float64            `json:"avg_requests_per_hour"`
	AvgBytesPerHour    float64            `json:"avg_bytes_per_hour"`
	StdDevRequests     float64            `json:"std_dev_requests"`
	StdDevBytes        float64            `json:"std_dev_bytes"`
}

// UserBaseline represents a user's baseline behavior.
type UserBaseline struct {
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	Metrics    *BaselineMetrics `json:"metrics"`
	UserID     string           `json:"user_id"`
	EventCount int64            `json:"event_count"`
	IsStable   bool             `json:"is_stable"`
}

// AnomalyRule defines a rule for detecting anomalies.
type AnomalyRule struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Type        AnomalyType     `json:"type"`
	Severity    Severity        `json:"severity"`
	Description string          `json:"description"`
	Conditions  []RuleCondition `json:"conditions"`
	Actions     []RuleAction    `json:"actions"`
	Threshold   float64         `json:"threshold"`
	Window      time.Duration   `json:"window"`
	Enabled     bool            `json:"enabled"`
}

// RuleCondition defines a condition for an anomaly rule.
type RuleCondition struct {
	Value    interface{} `json:"value"`
	Field    string      `json:"field"`
	Operator string      `json:"operator"`
}

// RuleAction defines an action to take when an anomaly is detected.
type RuleAction struct {
	Parameters map[string]string `json:"parameters"`
	Type       string            `json:"type"`
}

// AnalyticsConfig contains configuration for the analytics engine.
type AnalyticsConfig struct {
	BaselineWindow       time.Duration `json:"baseline_window"`
	MinEventsForBaseline int           `json:"min_events_for_baseline"`
	AnomalyThreshold     float64       `json:"anomaly_threshold"`
	RetentionPeriod      time.Duration `json:"retention_period"`
	SamplingRate         float64       `json:"sampling_rate"`
	EnableRealTime       bool          `json:"enable_realtime"`
	BatchSize            int           `json:"batch_size"`
	FlushInterval        time.Duration `json:"flush_interval"`
}

// AnalyticsStats contains statistics for the analytics engine.
type AnalyticsStats struct {
	LastUpdated         time.Time        `json:"last_updated"`
	TopBuckets          map[string]int64 `json:"top_buckets"`
	TopOperations       map[string]int64 `json:"top_operations"`
	AnomaliesBySeverity map[string]int64 `json:"anomalies_by_severity"`
	AnomaliesByType     map[string]int64 `json:"anomalies_by_type"`
	TotalEvents         int64            `json:"total_events"`
	EventsLastHour      int64            `json:"events_last_hour"`
	AnomaliesDetected   int64            `json:"anomalies_detected"`
	ActiveUsers         int64            `json:"active_users"`
}

// AccessAnalytics provides access analytics with anomaly detection.
type AccessAnalytics struct {
	storage      AnalyticsStorage
	alertHandler AlertHandler
	config       *AnalyticsConfig
	baselines    map[string]*UserBaseline
	rules        map[string]*AnomalyRule
	stats        *AnalyticsStats
	eventChan    chan *AccessEvent
	stopChan     chan struct{}
	events       []*AccessEvent
	anomalies    []*Anomaly
	wg           sync.WaitGroup
	mu           sync.RWMutex
}

// AnalyticsStorage defines the interface for analytics persistence.
type AnalyticsStorage interface {
	StoreEvent(ctx context.Context, event *AccessEvent) error
	GetEvents(ctx context.Context, filter *EventFilter) ([]*AccessEvent, error)
	StoreBaseline(ctx context.Context, baseline *UserBaseline) error
	GetBaseline(ctx context.Context, userID string) (*UserBaseline, error)
	StoreAnomaly(ctx context.Context, anomaly *Anomaly) error
	GetAnomalies(ctx context.Context, filter *AnomalyFilter) ([]*Anomaly, error)
	GetStats(ctx context.Context) (*AnalyticsStats, error)
	Cleanup(ctx context.Context, before time.Time) error
}

// AlertHandler defines the interface for handling anomaly alerts.
type AlertHandler interface {
	SendAlert(ctx context.Context, anomaly *Anomaly) error
	SendBulkAlert(ctx context.Context, anomalies []*Anomaly) error
}

// EventFilter defines filters for querying events.
type EventFilter struct {
	UserID     string     `json:"user_id,omitempty"`
	Bucket     string     `json:"bucket,omitempty"`
	AccessType AccessType `json:"access_type,omitempty"`
	StartTime  time.Time  `json:"start_time"`
	EndTime    time.Time  `json:"end_time"`
	SourceIP   string     `json:"source_ip,omitempty"`
	Limit      int        `json:"limit"`
	Offset     int        `json:"offset"`
}

// AnomalyFilter defines filters for querying anomalies.
type AnomalyFilter struct {
	StartTime    time.Time   `json:"start_time"`
	EndTime      time.Time   `json:"end_time"`
	Acknowledged *bool       `json:"acknowledged,omitempty"`
	UserID       string      `json:"user_id,omitempty"`
	Type         AnomalyType `json:"type,omitempty"`
	Severity     Severity    `json:"severity,omitempty"`
	Limit        int         `json:"limit"`
	Offset       int         `json:"offset"`
}

// NewAccessAnalytics creates a new access analytics engine.
func NewAccessAnalytics(config *AnalyticsConfig, storage AnalyticsStorage, alertHandler AlertHandler) *AccessAnalytics {
	if config == nil {
		config = &AnalyticsConfig{
			BaselineWindow:       7 * 24 * time.Hour, // 7 days
			MinEventsForBaseline: defaultMinEventsForBaseline,
			AnomalyThreshold:     defaultAnomalyThreshold,      // 3 standard deviations
			RetentionPeriod:      30 * 24 * time.Hour,          // 30 days
			SamplingRate:         1.0,                          // 100%
			EnableRealTime:       true,
			BatchSize:            defaultBatchSize,
			FlushInterval:        time.Minute,
		}
	}

	aa := &AccessAnalytics{
		config:    config,
		events:    make([]*AccessEvent, 0),
		baselines: make(map[string]*UserBaseline),
		anomalies: make([]*Anomaly, 0),
		rules:     make(map[string]*AnomalyRule),
		stats: &AnalyticsStats{
			TopBuckets:          make(map[string]int64),
			TopOperations:       make(map[string]int64),
			AnomaliesBySeverity: make(map[string]int64),
			AnomaliesByType:     make(map[string]int64),
		},
		storage:      storage,
		alertHandler: alertHandler,
		eventChan:    make(chan *AccessEvent, defaultEventChannelSize),
		stopChan:     make(chan struct{}),
	}

	// Initialize default rules
	aa.initializeDefaultRules()

	// Start background processing if real-time is enabled
	if config.EnableRealTime {
		aa.wg.Add(1)

		go aa.processEvents()
	}

	return aa
}

// initializeDefaultRules sets up default anomaly detection rules.
func (aa *AccessAnalytics) initializeDefaultRules() {
	defaultRules := []*AnomalyRule{
		{
			ID:          "unusual-time-access",
			Name:        "Unusual Time Access",
			Type:        AnomalyTypeUnusualTime,
			Enabled:     true,
			Severity:    SeverityMedium,
			Threshold:   thresholdUnusualTime,
			Window:      time.Hour,
			Description: "Detects access during unusual hours for a user",
		},
		{
			ID:          "high-volume-requests",
			Name:        "High Volume Requests",
			Type:        AnomalyTypeHighVolume,
			Enabled:     true,
			Severity:    SeverityHigh,
			Threshold:   thresholdHighVolume,
			Window:      thresholdHighVolumeWindowMins * time.Minute,
			Description: "Detects unusually high request volume",
		},
		{
			ID:          "bulk-delete-operation",
			Name:        "Bulk Delete Operation",
			Type:        AnomalyTypeBulkOperation,
			Enabled:     true,
			Severity:    SeverityCritical,
			Threshold:   thresholdBulkDelete,
			Window:      thresholdBulkDeleteWindowMins * time.Minute,
			Description: "Detects bulk delete operations",
			Conditions: []RuleCondition{
				{Field: "access_type", Operator: "eq", Value: "DELETE"},
			},
		},
		{
			ID:          "geo-velocity-violation",
			Name:        "Impossible Travel",
			Type:        AnomalyTypeGeoVelocity,
			Enabled:     true,
			Severity:    SeverityCritical,
			Threshold:   thresholdGeoVelocity, // 500 km/h max velocity
			Window:      time.Hour,
			Description: "Detects impossible travel based on location changes",
		},
		{
			ID:          "data-exfiltration",
			Name:        "Data Exfiltration",
			Type:        AnomalyTypeDataExfiltration,
			Enabled:     true,
			Severity:    SeverityCritical,
			Threshold:   thresholdDataExfiltration, // 10GB
			Window:      time.Hour,
			Description: "Detects large data downloads",
		},
		{
			ID:          "first-time-bucket-access",
			Name:        "First Time Bucket Access",
			Type:        AnomalyTypeFirstTimeAccess,
			Enabled:     true,
			Severity:    SeverityLow,
			Threshold:   1,
			Window:      baselineDays * time.Hour,
			Description: "Detects first-time access to a bucket",
		},
		{
			ID:          "unusual-user-agent",
			Name:        "Unusual User Agent",
			Type:        AnomalyTypeUnusualUserAgent,
			Enabled:     true,
			Severity:    SeverityMedium,
			Threshold:   1,
			Window:      time.Hour,
			Description: "Detects access from unusual user agents",
		},
		{
			ID:          "rapid-permission-changes",
			Name:        "Rapid Permission Changes",
			Type:        AnomalyTypeRapidChanges,
			Enabled:     true,
			Severity:    SeverityHigh,
			Threshold:   thresholdRapidChanges,
			Window:      thresholdRapidChangesWindowMins * time.Minute,
			Description: "Detects rapid ACL or policy changes",
			Conditions: []RuleCondition{
				{Field: "access_type", Operator: "in", Value: []string{"ACL", "POLICY"}},
			},
		},
	}

	for _, rule := range defaultRules {
		aa.rules[rule.ID] = rule
	}
}

// RecordEvent records an access event.
func (aa *AccessAnalytics) RecordEvent(ctx context.Context, event *AccessEvent) error {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Store event
	if aa.storage != nil {
		err := aa.storage.StoreEvent(ctx, event)
		if err != nil {
			return err
		}
	}

	// Send to real-time processing
	if aa.config.EnableRealTime {
		select {
		case aa.eventChan <- event:
		default:
			// Channel full, drop event (could log warning)
		}
	}

	// Update stats
	aa.mu.Lock()
	aa.stats.TotalEvents++
	aa.stats.TopBuckets[event.Bucket]++
	aa.stats.TopOperations[string(event.AccessType)]++
	aa.mu.Unlock()

	return nil
}

// processEvents processes events in real-time.
func (aa *AccessAnalytics) processEvents() {
	defer aa.wg.Done()

	batch := make([]*AccessEvent, 0, aa.config.BatchSize)

	ticker := time.NewTicker(aa.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-aa.stopChan:
			// Process remaining events
			if len(batch) > 0 {
				aa.processBatch(batch)
			}

			return

		case event := <-aa.eventChan:
			batch = append(batch, event)
			if len(batch) >= aa.config.BatchSize {
				aa.processBatch(batch)
				batch = make([]*AccessEvent, 0, aa.config.BatchSize)
			}

		case <-ticker.C:
			if len(batch) > 0 {
				aa.processBatch(batch)
				batch = make([]*AccessEvent, 0, aa.config.BatchSize)
			}
		}
	}
}

// processBatch processes a batch of events.
func (aa *AccessAnalytics) processBatch(events []*AccessEvent) {
	ctx := context.Background()

	// Group events by user
	userEvents := make(map[string][]*AccessEvent)
	for _, event := range events {
		userEvents[event.UserID] = append(userEvents[event.UserID], event)
	}

	// Detect anomalies for each user
	var detectedAnomalies []*Anomaly

	for userID, events := range userEvents {
		anomalies := aa.detectAnomalies(ctx, userID, events)
		detectedAnomalies = append(detectedAnomalies, anomalies...)

		// Update baseline
		aa.updateBaseline(ctx, userID, events)
	}

	// Store and alert on anomalies
	if len(detectedAnomalies) > 0 {
		aa.mu.Lock()

		aa.anomalies = append(aa.anomalies, detectedAnomalies...)
		for _, anomaly := range detectedAnomalies {
			aa.stats.AnomaliesDetected++
			aa.stats.AnomaliesBySeverity[string(anomaly.Severity)]++
			aa.stats.AnomaliesByType[string(anomaly.Type)]++
		}

		aa.mu.Unlock()

		// Send alerts (best-effort, log errors)
		if aa.alertHandler != nil {
			err := aa.alertHandler.SendBulkAlert(ctx, detectedAnomalies)
			if err != nil {
				// Log alert sending failure but continue
				_ = err
			}
		}
	}
}

// detectAnomalies detects anomalies in user events.
func (aa *AccessAnalytics) detectAnomalies(ctx context.Context, userID string, events []*AccessEvent) []*Anomaly {
	var anomalies []*Anomaly

	// Get user baseline
	baseline := aa.getBaseline(ctx, userID)

	for _, rule := range aa.rules {
		if !rule.Enabled {
			continue
		}

		anomaly := aa.checkRule(rule, userID, events, baseline)
		if anomaly != nil {
			anomalies = append(anomalies, anomaly)
		}
	}

	return anomalies
}

// checkRule checks a single rule against events.
func (aa *AccessAnalytics) checkRule(rule *AnomalyRule, userID string, events []*AccessEvent, baseline *UserBaseline) *Anomaly {
	// Filter events by rule conditions
	filteredEvents := aa.filterEventsByConditions(events, rule.Conditions)
	if len(filteredEvents) == 0 {
		return nil
	}

	var anomaly *Anomaly

	switch rule.Type {
	case AnomalyTypeUnusualTime:
		anomaly = aa.checkUnusualTime(rule, userID, filteredEvents, baseline)
	case AnomalyTypeHighVolume:
		anomaly = aa.checkHighVolume(rule, userID, filteredEvents, baseline)
	case AnomalyTypeBulkOperation:
		anomaly = aa.checkBulkOperation(rule, userID, filteredEvents)
	case AnomalyTypeGeoVelocity:
		anomaly = aa.checkGeoVelocity(rule, userID, filteredEvents)
	case AnomalyTypeDataExfiltration:
		anomaly = aa.checkDataExfiltration(rule, userID, filteredEvents)
	case AnomalyTypeFirstTimeAccess:
		anomaly = aa.checkFirstTimeAccess(rule, userID, filteredEvents, baseline)
	case AnomalyTypeUnusualUserAgent:
		anomaly = aa.checkUnusualUserAgent(rule, userID, filteredEvents, baseline)
	case AnomalyTypeRapidChanges:
		anomaly = aa.checkRapidChanges(rule, userID, filteredEvents)
	}

	return anomaly
}

// filterEventsByConditions filters events based on rule conditions.
func (aa *AccessAnalytics) filterEventsByConditions(events []*AccessEvent, conditions []RuleCondition) []*AccessEvent {
	if len(conditions) == 0 {
		return events
	}

	var filtered []*AccessEvent

	for _, event := range events {
		if aa.matchesConditions(event, conditions) {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

// matchesConditions checks if an event matches all conditions.
func (aa *AccessAnalytics) matchesConditions(event *AccessEvent, conditions []RuleCondition) bool {
	for _, cond := range conditions {
		if !aa.matchesCondition(event, cond) {
			return false
		}
	}

	return true
}

// matchesCondition checks if an event matches a single condition.
func (aa *AccessAnalytics) matchesCondition(event *AccessEvent, cond RuleCondition) bool {
	fieldValue := aa.getFieldValue(event, cond.Field)
	if fieldValue == nil {
		return false
	}

	return aa.evaluateOperator(fieldValue, cond.Operator, cond.Value)
}

func (aa *AccessAnalytics) getFieldValue(event *AccessEvent, field string) interface{} {
	switch field {
	case "access_type":
		return string(event.AccessType)
	case "bucket":
		return event.Bucket
	case "user_id":
		return event.UserID
	case "source_ip":
		return event.SourceIP
	case "status_code":
		return event.StatusCode
	default:
		return nil
	}
}

func (aa *AccessAnalytics) evaluateOperator(fieldValue interface{}, operator string, condValue interface{}) bool {
	switch operator {
	case "eq":
		return fieldValue == condValue
	case "ne":
		return fieldValue != condValue
	case "in":
		return aa.evaluateInOperator(fieldValue, condValue)
	case "contains":
		return aa.evaluateContainsOperator(fieldValue, condValue)
	default:
		return false
	}
}

func (aa *AccessAnalytics) evaluateInOperator(fieldValue, condValue interface{}) bool {
	values, ok := condValue.([]string)
	if !ok {
		return false
	}

	for _, v := range values {
		if fieldValue == v {
			return true
		}
	}

	return false
}

func (aa *AccessAnalytics) evaluateContainsOperator(fieldValue, condValue interface{}) bool {
	s, ok := fieldValue.(string)
	if !ok {
		return false
	}

	v, ok := condValue.(string)
	if !ok {
		return false
	}

	return len(s) > 0 && len(v) > 0 && (s == v || len(s) > len(v))
}

// checkUnusualTime detects access during unusual hours.
func (aa *AccessAnalytics) checkUnusualTime(rule *AnomalyRule, userID string, events []*AccessEvent, baseline *UserBaseline) *Anomaly {
	if baseline == nil || len(baseline.Metrics.ActiveHours) == 0 {
		return nil
	}

	activeHoursMap := make(map[int]bool)
	for _, h := range baseline.Metrics.ActiveHours {
		activeHoursMap[h] = true
	}

	var unusualEvents []*AccessEvent

	for _, event := range events {
		hour := event.Timestamp.Hour()
		if !activeHoursMap[hour] {
			unusualEvents = append(unusualEvents, event)
		}
	}

	if len(unusualEvents) == 0 {
		return nil
	}

	return &Anomaly{
		ID:          uuid.New().String(),
		Type:        AnomalyTypeUnusualTime,
		Severity:    rule.Severity,
		Timestamp:   time.Now(),
		UserID:      userID,
		Description: "Access detected during unusual hours",
		Events:      unusualEvents,
		Score:       float64(len(unusualEvents)) / float64(len(events)),
		Baseline:    baseline.Metrics,
	}
}

// checkHighVolume detects unusually high request volume.
func (aa *AccessAnalytics) checkHighVolume(rule *AnomalyRule, userID string, events []*AccessEvent, baseline *UserBaseline) *Anomaly {
	if baseline == nil || baseline.Metrics.AvgRequestsPerHour == 0 {
		return nil
	}

	// Calculate current requests per hour
	now := time.Now()
	windowStart := now.Add(-rule.Window)

	var countInWindow int

	for _, event := range events {
		if event.Timestamp.After(windowStart) {
			countInWindow++
		}
	}

	// Scale to hourly rate
	hourlyRate := float64(countInWindow) * (float64(time.Hour) / float64(rule.Window))

	// Calculate z-score
	if baseline.Metrics.StdDevRequests == 0 {
		return nil
	}

	zscore := (hourlyRate - baseline.Metrics.AvgRequestsPerHour) / baseline.Metrics.StdDevRequests

	if zscore < rule.Threshold {
		return nil
	}

	return &Anomaly{
		ID:          uuid.New().String(),
		Type:        AnomalyTypeHighVolume,
		Severity:    rule.Severity,
		Timestamp:   time.Now(),
		UserID:      userID,
		Description: "Unusually high request volume detected",
		Events:      events,
		Score:       zscore,
		Baseline:    baseline.Metrics,
		Observed: &BaselineMetrics{
			AvgRequestsPerHour: hourlyRate,
		},
	}
}

// checkBulkOperation detects bulk operations.
func (aa *AccessAnalytics) checkBulkOperation(rule *AnomalyRule, userID string, events []*AccessEvent) *Anomaly {
	if float64(len(events)) < rule.Threshold {
		return nil
	}

	// Count operations by type within window
	now := time.Now()
	windowStart := now.Add(-rule.Window)
	opCounts := make(map[AccessType]int)

	for _, event := range events {
		if event.Timestamp.After(windowStart) {
			opCounts[event.AccessType]++
		}
	}

	// Check for bulk deletes specifically
	if opCounts[AccessTypeDelete] >= int(rule.Threshold) {
		return &Anomaly{
			ID:          uuid.New().String(),
			Type:        AnomalyTypeBulkOperation,
			Severity:    rule.Severity,
			Timestamp:   time.Now(),
			UserID:      userID,
			Description: "Bulk delete operation detected",
			Events:      events,
			Score:       float64(opCounts[AccessTypeDelete]),
			Metadata: map[string]string{
				"operation_type": "DELETE",
				"count":          string(rune(opCounts[AccessTypeDelete])),
			},
		}
	}

	return nil
}

// checkGeoVelocity detects impossible travel.
func (aa *AccessAnalytics) checkGeoVelocity(rule *AnomalyRule, userID string, events []*AccessEvent) *Anomaly {
	// Need at least 2 events with geo locations
	var geoEvents []*AccessEvent

	for _, event := range events {
		if event.GeoLocation != nil {
			geoEvents = append(geoEvents, event)
		}
	}

	if len(geoEvents) < minGeoEventsForCheck {
		return nil
	}

	// Sort by timestamp
	sort.Slice(geoEvents, func(i, j int) bool {
		return geoEvents[i].Timestamp.Before(geoEvents[j].Timestamp)
	})

	// Check velocity between consecutive events
	for i := 1; i < len(geoEvents); i++ {
		prev := geoEvents[i-1]
		curr := geoEvents[i]

		// Calculate distance (Haversine formula)
		distance := haversineDistance(
			prev.GeoLocation.Latitude, prev.GeoLocation.Longitude,
			curr.GeoLocation.Latitude, curr.GeoLocation.Longitude,
		)

		// Calculate time difference in hours
		timeDiff := curr.Timestamp.Sub(prev.Timestamp).Hours()
		if timeDiff == 0 {
			timeDiff = 0.001 // Avoid division by zero
		}

		// Calculate velocity in km/h
		velocity := distance / timeDiff

		if velocity > rule.Threshold {
			return &Anomaly{
				ID:          uuid.New().String(),
				Type:        AnomalyTypeGeoVelocity,
				Severity:    rule.Severity,
				Timestamp:   time.Now(),
				UserID:      userID,
				Description: "Impossible travel detected - locations too far apart in time",
				Events:      []*AccessEvent{prev, curr},
				Score:       velocity,
				Metadata: map[string]string{
					"distance_km":  string(rune(int(distance))),
					"velocity_kmh": string(rune(int(velocity))),
					"from_country": prev.GeoLocation.Country,
					"to_country":   curr.GeoLocation.Country,
				},
			}
		}
	}

	return nil
}

// haversineDistance calculates distance between two coordinates in km.
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371 // km

	dLat := (lat2 - lat1) * math.Pi / degreesToRadiansDivisor
	dLon := (lon2 - lon1) * math.Pi / degreesToRadiansDivisor

	a := math.Sin(dLat/haversineDivisor)*math.Sin(dLat/haversineDivisor) +
		math.Cos(lat1*math.Pi/degreesToRadiansDivisor)*math.Cos(lat2*math.Pi/degreesToRadiansDivisor)*
			math.Sin(dLon/haversineDivisor)*math.Sin(dLon/haversineDivisor)

	c := haversineDivisor * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadius * c
}

// checkDataExfiltration detects large data downloads.
func (aa *AccessAnalytics) checkDataExfiltration(rule *AnomalyRule, userID string, events []*AccessEvent) *Anomaly {
	now := time.Now()
	windowStart := now.Add(-rule.Window)

	var (
		totalBytesRead int64
		readEvents     []*AccessEvent
	)

	for _, event := range events {
		if event.Timestamp.After(windowStart) && event.AccessType == AccessTypeRead {
			totalBytesRead += event.BytesRead
			readEvents = append(readEvents, event)
		}
	}

	// Convert threshold from GB to bytes
	thresholdBytes := int64(rule.Threshold * bytesPerGB)

	if totalBytesRead < thresholdBytes {
		return nil
	}

	return &Anomaly{
		ID:          uuid.New().String(),
		Type:        AnomalyTypeDataExfiltration,
		Severity:    rule.Severity,
		Timestamp:   time.Now(),
		UserID:      userID,
		Description: "Large data download detected - potential data exfiltration",
		Events:      readEvents,
		Score:       float64(totalBytesRead) / float64(thresholdBytes),
		Metadata: map[string]string{
			"total_bytes":     string(rune(totalBytesRead)),
			"threshold_bytes": string(rune(thresholdBytes)),
		},
	}
}

// checkFirstTimeAccess detects first-time access to resources.
func (aa *AccessAnalytics) checkFirstTimeAccess(rule *AnomalyRule, userID string, events []*AccessEvent, baseline *UserBaseline) *Anomaly {
	if baseline == nil {
		return nil
	}

	commonBucketsMap := make(map[string]bool)
	for _, b := range baseline.Metrics.CommonBuckets {
		commonBucketsMap[b] = true
	}

	var newBucketEvents []*AccessEvent

	newBuckets := make(map[string]bool)

	for _, event := range events {
		if !commonBucketsMap[event.Bucket] && !newBuckets[event.Bucket] {
			newBuckets[event.Bucket] = true
			newBucketEvents = append(newBucketEvents, event)
		}
	}

	if len(newBucketEvents) == 0 {
		return nil
	}

	return &Anomaly{
		ID:          uuid.New().String(),
		Type:        AnomalyTypeFirstTimeAccess,
		Severity:    rule.Severity,
		Timestamp:   time.Now(),
		UserID:      userID,
		Description: "First-time access to bucket(s) detected",
		Events:      newBucketEvents,
		Score:       float64(len(newBucketEvents)),
		Baseline:    baseline.Metrics,
	}
}

// checkUnusualUserAgent detects access from unusual user agents.
func (aa *AccessAnalytics) checkUnusualUserAgent(rule *AnomalyRule, userID string, events []*AccessEvent, baseline *UserBaseline) *Anomaly {
	if baseline == nil || len(baseline.Metrics.CommonUserAgents) == 0 {
		return nil
	}

	commonUAMap := make(map[string]bool)
	for _, ua := range baseline.Metrics.CommonUserAgents {
		commonUAMap[ua] = true
	}

	var unusualEvents []*AccessEvent

	for _, event := range events {
		if !commonUAMap[event.UserAgent] {
			unusualEvents = append(unusualEvents, event)
		}
	}

	if len(unusualEvents) == 0 {
		return nil
	}

	return &Anomaly{
		ID:          uuid.New().String(),
		Type:        AnomalyTypeUnusualUserAgent,
		Severity:    rule.Severity,
		Timestamp:   time.Now(),
		UserID:      userID,
		Description: "Access from unusual user agent detected",
		Events:      unusualEvents,
		Score:       float64(len(unusualEvents)) / float64(len(events)),
		Baseline:    baseline.Metrics,
	}
}

// checkRapidChanges detects rapid permission or configuration changes.
func (aa *AccessAnalytics) checkRapidChanges(rule *AnomalyRule, userID string, events []*AccessEvent) *Anomaly {
	now := time.Now()
	windowStart := now.Add(-rule.Window)

	var changeEvents []*AccessEvent

	for _, event := range events {
		if event.Timestamp.After(windowStart) {
			if event.AccessType == AccessTypeACL || event.AccessType == AccessTypePolicy {
				changeEvents = append(changeEvents, event)
			}
		}
	}

	if float64(len(changeEvents)) < rule.Threshold {
		return nil
	}

	return &Anomaly{
		ID:          uuid.New().String(),
		Type:        AnomalyTypeRapidChanges,
		Severity:    rule.Severity,
		Timestamp:   time.Now(),
		UserID:      userID,
		Description: "Rapid permission/policy changes detected",
		Events:      changeEvents,
		Score:       float64(len(changeEvents)),
	}
}

// getBaseline retrieves or creates a baseline for a user.
func (aa *AccessAnalytics) getBaseline(ctx context.Context, userID string) *UserBaseline {
	aa.mu.RLock()
	baseline, exists := aa.baselines[userID]
	aa.mu.RUnlock()

	if exists {
		return baseline
	}

	// Try to load from storage
	if aa.storage != nil {
		stored, err := aa.storage.GetBaseline(ctx, userID)
		if err == nil && stored != nil {
			aa.mu.Lock()
			aa.baselines[userID] = stored
			aa.mu.Unlock()

			return stored
		}
	}

	return nil
}

// updateBaseline updates the baseline for a user.
func (aa *AccessAnalytics) updateBaseline(ctx context.Context, userID string, events []*AccessEvent) {
	aa.mu.Lock()
	defer aa.mu.Unlock()

	baseline, exists := aa.baselines[userID]
	if !exists {
		baseline = &UserBaseline{
			UserID:    userID,
			CreatedAt: time.Now(),
			Metrics: &BaselineMetrics{
				CommonOperations: make(map[AccessType]int),
			},
		}
		aa.baselines[userID] = baseline
	}

	// Update metrics
	metrics := baseline.Metrics

	// Track active hours
	hourCounts := make(map[int]int)
	for _, h := range metrics.ActiveHours {
		hourCounts[h]++
	}

	for _, event := range events {
		hourCounts[event.Timestamp.Hour()]++
	}

	metrics.ActiveHours = make([]int, 0)
	for h := range hourCounts {
		metrics.ActiveHours = append(metrics.ActiveHours, h)
	}

	// Track common buckets
	bucketSet := make(map[string]bool)
	for _, b := range metrics.CommonBuckets {
		bucketSet[b] = true
	}

	for _, event := range events {
		bucketSet[event.Bucket] = true
	}

	metrics.CommonBuckets = make([]string, 0, len(bucketSet))
	for b := range bucketSet {
		metrics.CommonBuckets = append(metrics.CommonBuckets, b)
	}

	// Track operations
	for _, event := range events {
		metrics.CommonOperations[event.AccessType]++
	}

	// Track IPs
	ipSet := make(map[string]bool)
	for _, ip := range metrics.CommonIPs {
		ipSet[ip] = true
	}

	for _, event := range events {
		ipSet[event.SourceIP] = true
	}

	metrics.CommonIPs = make([]string, 0, len(ipSet))
	for ip := range ipSet {
		metrics.CommonIPs = append(metrics.CommonIPs, ip)
	}

	// Track user agents
	uaSet := make(map[string]bool)
	for _, ua := range metrics.CommonUserAgents {
		uaSet[ua] = true
	}

	for _, event := range events {
		uaSet[event.UserAgent] = true
	}

	metrics.CommonUserAgents = make([]string, 0, len(uaSet))
	for ua := range uaSet {
		metrics.CommonUserAgents = append(metrics.CommonUserAgents, ua)
	}

	baseline.EventCount += int64(len(events))
	baseline.UpdatedAt = time.Now()
	metrics.LastUpdated = time.Now()

	// Mark as stable if enough events
	if baseline.EventCount >= int64(aa.config.MinEventsForBaseline) {
		baseline.IsStable = true
	}

	// Store updated baseline (best-effort)
	if aa.storage != nil {
		err := aa.storage.StoreBaseline(ctx, baseline)
		if err != nil {
			// Log storage failure but continue
			_ = err
		}
	}
}

// GetAnomalies retrieves anomalies based on filter.
func (aa *AccessAnalytics) GetAnomalies(ctx context.Context, filter *AnomalyFilter) ([]*Anomaly, error) {
	if aa.storage != nil {
		return aa.storage.GetAnomalies(ctx, filter)
	}

	aa.mu.RLock()
	defer aa.mu.RUnlock()

	var result []*Anomaly

	for _, anomaly := range aa.anomalies {
		if aa.matchesAnomalyFilter(anomaly, filter) {
			result = append(result, anomaly)
		}
	}

	// Apply pagination
	if filter.Offset > 0 && filter.Offset < len(result) {
		result = result[filter.Offset:]
	}

	if filter.Limit > 0 && filter.Limit < len(result) {
		result = result[:filter.Limit]
	}

	return result, nil
}

// matchesAnomalyFilter checks if an anomaly matches the filter.
func (aa *AccessAnalytics) matchesAnomalyFilter(anomaly *Anomaly, filter *AnomalyFilter) bool {
	if filter.UserID != "" && anomaly.UserID != filter.UserID {
		return false
	}

	if filter.Type != "" && anomaly.Type != filter.Type {
		return false
	}

	if filter.Severity != "" && anomaly.Severity != filter.Severity {
		return false
	}

	if filter.Acknowledged != nil && anomaly.Acknowledged != *filter.Acknowledged {
		return false
	}

	if !filter.StartTime.IsZero() && anomaly.Timestamp.Before(filter.StartTime) {
		return false
	}

	if !filter.EndTime.IsZero() && anomaly.Timestamp.After(filter.EndTime) {
		return false
	}

	return true
}

// AcknowledgeAnomaly marks an anomaly as acknowledged.
func (aa *AccessAnalytics) AcknowledgeAnomaly(ctx context.Context, anomalyID, userID, resolution string) error {
	aa.mu.Lock()
	defer aa.mu.Unlock()

	for _, anomaly := range aa.anomalies {
		if anomaly.ID == anomalyID {
			now := time.Now()
			anomaly.Acknowledged = true
			anomaly.AcknowledgedBy = userID
			anomaly.AcknowledgedAt = &now
			anomaly.Resolution = resolution

			if aa.storage != nil {
				return aa.storage.StoreAnomaly(ctx, anomaly)
			}

			return nil
		}
	}

	return nil
}

// AddRule adds a custom anomaly detection rule.
func (aa *AccessAnalytics) AddRule(rule *AnomalyRule) error {
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}

	aa.mu.Lock()
	aa.rules[rule.ID] = rule
	aa.mu.Unlock()

	return nil
}

// RemoveRule removes an anomaly detection rule.
func (aa *AccessAnalytics) RemoveRule(ruleID string) {
	aa.mu.Lock()
	delete(aa.rules, ruleID)
	aa.mu.Unlock()
}

// GetRules returns all anomaly detection rules.
func (aa *AccessAnalytics) GetRules() []*AnomalyRule {
	aa.mu.RLock()
	defer aa.mu.RUnlock()

	rules := make([]*AnomalyRule, 0, len(aa.rules))
	for _, rule := range aa.rules {
		rules = append(rules, rule)
	}

	return rules
}

// GetStats returns analytics statistics.
func (aa *AccessAnalytics) GetStats(ctx context.Context) (*AnalyticsStats, error) {
	if aa.storage != nil {
		return aa.storage.GetStats(ctx)
	}

	aa.mu.RLock()
	defer aa.mu.RUnlock()

	stats := *aa.stats
	stats.LastUpdated = time.Now()

	return &stats, nil
}

// GetUserBaseline returns the baseline for a specific user.
func (aa *AccessAnalytics) GetUserBaseline(ctx context.Context, userID string) (*UserBaseline, error) {
	baseline := aa.getBaseline(ctx, userID)
	if baseline == nil {
		//nolint:nilnil // nil,nil indicates no baseline found (not an error condition)
		return nil, nil
	}

	return baseline, nil
}

// GenerateReport generates an analytics report.
func (aa *AccessAnalytics) GenerateReport(ctx context.Context, startTime, endTime time.Time) (*AnalyticsReport, error) {
	filter := &AnomalyFilter{
		StartTime: startTime,
		EndTime:   endTime,
	}

	anomalies, err := aa.GetAnomalies(ctx, filter)
	if err != nil {
		return nil, err
	}

	stats, err := aa.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	// Calculate severity distribution
	severityDist := make(map[Severity]int)
	typeDist := make(map[AnomalyType]int)
	userDist := make(map[string]int)

	for _, anomaly := range anomalies {
		severityDist[anomaly.Severity]++
		typeDist[anomaly.Type]++
		userDist[anomaly.UserID]++
	}

	// Find top affected users
	type userCount struct {
		userID string
		count  int
	}

	userCounts := make([]userCount, 0, len(userDist))
	for u, c := range userDist {
		userCounts = append(userCounts, userCount{u, c})
	}

	sort.Slice(userCounts, func(i, j int) bool {
		return userCounts[i].count > userCounts[j].count
	})

	topUsers := make([]string, 0)
	for i := 0; i < 10 && i < len(userCounts); i++ {
		topUsers = append(topUsers, userCounts[i].userID)
	}

	return &AnalyticsReport{
		GeneratedAt:       time.Now(),
		StartTime:         startTime,
		EndTime:           endTime,
		TotalEvents:       stats.TotalEvents,
		TotalAnomalies:    int64(len(anomalies)),
		SeverityBreakdown: severityDist,
		TypeBreakdown:     typeDist,
		TopAffectedUsers:  topUsers,
		Anomalies:         anomalies,
	}, nil
}

// AnalyticsReport represents a generated analytics report.
type AnalyticsReport struct {
	GeneratedAt       time.Time           `json:"generated_at"`
	StartTime         time.Time           `json:"start_time"`
	EndTime           time.Time           `json:"end_time"`
	SeverityBreakdown map[Severity]int    `json:"severity_breakdown"`
	TypeBreakdown     map[AnomalyType]int `json:"type_breakdown"`
	TopAffectedUsers  []string            `json:"top_affected_users"`
	Anomalies         []*Anomaly          `json:"anomalies"`
	TotalEvents       int64               `json:"total_events"`
	TotalAnomalies    int64               `json:"total_anomalies"`
}

// ExportJSON exports the report as JSON.
func (r *AnalyticsReport) ExportJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Stop stops the analytics engine.
func (aa *AccessAnalytics) Stop() {
	close(aa.stopChan)
	aa.wg.Wait()
}

// Cleanup removes old data based on retention policy.
func (aa *AccessAnalytics) Cleanup(ctx context.Context) error {
	before := time.Now().Add(-aa.config.RetentionPeriod)

	if aa.storage != nil {
		return aa.storage.Cleanup(ctx, before)
	}

	aa.mu.Lock()
	defer aa.mu.Unlock()

	// Clean up in-memory events
	var newEvents []*AccessEvent

	for _, event := range aa.events {
		if event.Timestamp.After(before) {
			newEvents = append(newEvents, event)
		}
	}

	aa.events = newEvents

	// Clean up old anomalies (keep acknowledged ones longer)
	var newAnomalies []*Anomaly

	acknowledgedBefore := before.Add(-30 * 24 * time.Hour) // Keep acknowledged for extra 30 days

	for _, anomaly := range aa.anomalies {
		if anomaly.Acknowledged {
			if anomaly.Timestamp.After(acknowledgedBefore) {
				newAnomalies = append(newAnomalies, anomaly)
			}
		} else {
			if anomaly.Timestamp.After(before) {
				newAnomalies = append(newAnomalies, anomaly)
			}
		}
	}

	aa.anomalies = newAnomalies

	return nil
}
