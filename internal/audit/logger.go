// Package audit provides compliance-ready audit logging for NebulaIO.
//
// The package implements detailed audit trails for security-sensitive operations,
// supporting compliance requirements for SOC2, PCI DSS, HIPAA, GDPR, and FedRAMP.
//
// Features:
//   - Structured JSON audit logs
//   - Cryptographic integrity chain (HMAC-based)
//   - Sensitive data masking
//   - Log rotation and retention policies
//   - Webhook delivery for SIEM integration
//
// Logged events include:
//   - Authentication attempts (success/failure)
//   - Authorization decisions
//   - Object access and modifications
//   - Administrative operations
//   - Configuration changes
//
// Example log entry:
//
//	{"timestamp": "2025-01-15T10:30:00Z", "event": "ObjectAccess",
//	 "user": "alice", "bucket": "data", "key": "report.pdf", "action": "GetObject"}
package audit

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// EventType represents the type of audit event.
type EventType string

// Event types for S3 operations.
const (
	// Object events.
	EventObjectCreated         EventType = "s3:ObjectCreated:Put"
	EventObjectCreatedCopy     EventType = "s3:ObjectCreated:Copy"
	EventObjectCreatedPost     EventType = "s3:ObjectCreated:Post"
	EventObjectCreatedComplete EventType = "s3:ObjectCreated:CompleteMultipartUpload"
	EventObjectRemoved         EventType = "s3:ObjectRemoved:Delete"
	EventObjectRemovedMany     EventType = "s3:ObjectRemoved:DeleteMarkerCreated"
	EventObjectAccessed        EventType = "s3:ObjectAccessed:Get"
	EventObjectAccessedHead    EventType = "s3:ObjectAccessed:Head"

	// Bucket events.
	EventBucketCreated EventType = "s3:BucketCreated"
	EventBucketRemoved EventType = "s3:BucketRemoved"
	EventBucketListed  EventType = "s3:BucketListed"

	// Auth events.
	EventAuthLogin          EventType = "auth:Login"
	EventAuthLoginFailed    EventType = "auth:LoginFailed"
	EventAuthLogout         EventType = "auth:Logout"
	EventAuthTokenRefreshed EventType = "auth:TokenRefreshed"

	// Access key events.
	EventAccessKeyCreated EventType = "iam:AccessKeyCreated"
	EventAccessKeyDeleted EventType = "iam:AccessKeyDeleted"

	// User events.
	EventUserCreated         EventType = "iam:UserCreated"
	EventUserUpdated         EventType = "iam:UserUpdated"
	EventUserDeleted         EventType = "iam:UserDeleted"
	EventUserPasswordChanged EventType = "iam:UserPasswordChanged"

	// Policy events.
	EventPolicyCreated EventType = "iam:PolicyCreated"
	EventPolicyUpdated EventType = "iam:PolicyUpdated"
	EventPolicyDeleted EventType = "iam:PolicyDeleted"

	// Multipart events.
	EventMultipartCreated   EventType = "s3:MultipartUpload:Created"
	EventMultipartAborted   EventType = "s3:MultipartUpload:Aborted"
	EventMultipartCompleted EventType = "s3:MultipartUpload:Completed"
	EventMultipartPartAdded EventType = "s3:MultipartUpload:PartAdded"

	// Placement group events.
	EventPlacementGroupNodeJoined    EventType = "cluster:PlacementGroup:NodeJoined"
	EventPlacementGroupNodeLeft      EventType = "cluster:PlacementGroup:NodeLeft"
	EventPlacementGroupStatusChanged EventType = "cluster:PlacementGroup:StatusChanged"
	EventPlacementGroupCreated       EventType = "cluster:PlacementGroup:Created"
	EventPlacementGroupDeleted       EventType = "cluster:PlacementGroup:Deleted"
)

// EventSource represents the source of the audit event.
type EventSource string

const (
	SourceS3      EventSource = "s3"
	SourceAdmin   EventSource = "admin"
	SourceConsole EventSource = "console"
	SourceSystem  EventSource = "system"
	SourceCluster EventSource = "cluster"
)

// Result represents the outcome of an operation.
type Result string

const (
	ResultSuccess Result = "success"
	ResultFailure Result = "failure"
)

// UserIdentityType represents the type of user identity.
type UserIdentityType string

const (
	IdentityIAMUser   UserIdentityType = "IAMUser"
	IdentityAccessKey UserIdentityType = "AccessKey"
	IdentityAnonymous UserIdentityType = "Anonymous"
	IdentitySystem    UserIdentityType = "System"
)

// UserIdentity represents information about who performed the action.
type UserIdentity struct {
	Type        UserIdentityType `json:"type"`
	UserID      string           `json:"user_id,omitempty"`
	Username    string           `json:"username,omitempty"`
	AccessKeyID string           `json:"access_key_id,omitempty"`
}

// ResourceType represents the type of resource being accessed.
type ResourceType string

const (
	ResourceBucket         ResourceType = "bucket"
	ResourceObject         ResourceType = "object"
	ResourceUser           ResourceType = "user"
	ResourcePolicy         ResourceType = "policy"
	ResourceKey            ResourceType = "accesskey"
	ResourcePlacementGroup ResourceType = "placementgroup"
	ResourceClusterNode    ResourceType = "clusternode"
)

// ResourceInfo contains information about the resource being accessed.
type ResourceInfo struct {
	Type             ResourceType `json:"type"`
	Bucket           string       `json:"bucket,omitempty"`
	Key              string       `json:"key,omitempty"`
	VersionID        string       `json:"version_id,omitempty"`
	UserID           string       `json:"user_id,omitempty"`
	PolicyName       string       `json:"policy_name,omitempty"`
	PlacementGroupID string       `json:"placement_group_id,omitempty"`
	NodeID           string       `json:"node_id,omitempty"`
}

// AuditEvent represents a single audit log entry.
type AuditEvent struct {
	Timestamp    time.Time         `json:"timestamp"`
	Extra        map[string]string `json:"extra,omitempty"`
	Resource     ResourceInfo      `json:"resource"`
	UserIdentity UserIdentity      `json:"user_identity"`
	EventSource  EventSource       `json:"event_source"`
	ID           string            `json:"id"`
	SourceIP     string            `json:"source_ip"`
	UserAgent    string            `json:"user_agent"`
	EventType    EventType         `json:"event_type"`
	Action       string            `json:"action"`
	Result       Result            `json:"result"`
	ErrorCode    string            `json:"error_code,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	RequestID    string            `json:"request_id"`
	DurationMS   int64             `json:"duration_ms"`
	BytesIn      int64             `json:"bytes_in,omitempty"`
	BytesOut     int64             `json:"bytes_out,omitempty"`
}

// AuditFilter contains filter criteria for listing audit events.
type AuditFilter struct {
	StartTime  time.Time `json:"start_time,omitempty"`
	EndTime    time.Time `json:"end_time,omitempty"`
	Bucket     string    `json:"bucket,omitempty"`
	User       string    `json:"user,omitempty"`
	EventType  string    `json:"event_type,omitempty"`
	Result     string    `json:"result,omitempty"`
	NextToken  string    `json:"next_token,omitempty"`
	MaxResults int       `json:"max_results,omitempty"`
}

// AuditListResult contains the result of listing audit events.
type AuditListResult struct {
	NextToken string       `json:"next_token,omitempty"`
	Events    []AuditEvent `json:"events"`
}

// AuditStore is the interface for storing and retrieving audit events.
type AuditStore interface {
	StoreAuditEvent(ctx context.Context, event *AuditEvent) error
	ListAuditEvents(ctx context.Context, filter AuditFilter) (*AuditListResult, error)
}

// AuditLogger handles logging of audit events.
type AuditLogger struct {
	store    AuditStore
	buffer   chan *AuditEvent
	file     *os.File
	cancel   context.CancelFunc
	filePath string
	wg       sync.WaitGroup
	mu       sync.Mutex
}

// Config holds configuration for the audit logger.
type Config struct {
	Store        AuditStore
	FilePath     string // Optional: path to audit log file
	BufferSize   int    // Size of the event buffer channel
	FlushTimeout time.Duration
}

// NewAuditLogger creates a new AuditLogger instance.
func NewAuditLogger(config Config) (*AuditLogger, error) {
	if config.BufferSize <= 0 {
		config.BufferSize = 1000
	}

	logger := &AuditLogger{
		store:    config.Store,
		buffer:   make(chan *AuditEvent, config.BufferSize),
		filePath: config.FilePath,
	}

	// Open file for logging if path is specified
	if config.FilePath != "" {
		//nolint:gosec // G302: Log files may need to be readable by other processes
		f, err := os.OpenFile(config.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
		if err != nil {
			return nil, err
		}

		logger.file = f
	}

	return logger, nil
}

// Start begins processing audit events from the buffer.
func (l *AuditLogger) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	l.wg.Add(1)

	go l.processEvents(ctx)
}

// Stop gracefully shuts down the audit logger.
func (l *AuditLogger) Stop() {
	l.cancel()
	close(l.buffer)
	l.wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		_ = l.file.Close()
	}
}

// Log queues an audit event for processing.
func (l *AuditLogger) Log(event *AuditEvent) {
	// Ensure event has an ID and timestamp
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	select {
	case l.buffer <- event:
		// Event queued successfully
	default:
		// Buffer is full, log and drop
		log.Warn().
			Str("event_id", event.ID).
			Str("event_type", string(event.EventType)).
			Msg("Audit buffer full, dropping event")
	}
}

// LogSync logs an event synchronously (blocking).
func (l *AuditLogger) LogSync(ctx context.Context, event *AuditEvent) error {
	// Ensure event has an ID and timestamp
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	return l.processEvent(ctx, event)
}

// processEvents runs in a goroutine to process buffered events.
func (l *AuditLogger) processEvents(ctx context.Context) {
	defer l.wg.Done()

	for {
		select {
		case event, ok := <-l.buffer:
			if !ok {
				// Channel closed, drain remaining events
				for event := range l.buffer {
					err := l.processEvent(ctx, event)
					if err != nil {
						log.Error().Err(err).Str("event_id", event.ID).Msg("Failed to process audit event during shutdown")
					}
				}

				return
			}

			err := l.processEvent(ctx, event)
			if err != nil {
				log.Error().Err(err).Str("event_id", event.ID).Msg("Failed to process audit event")
			}
		case <-ctx.Done():
			return
		}
	}
}

// processEvent handles a single audit event.
func (l *AuditLogger) processEvent(ctx context.Context, event *AuditEvent) error {
	// Write to file if configured
	if l.file != nil {
		l.mu.Lock()

		data, err := json.Marshal(event)
		if err != nil {
			l.mu.Unlock()
			return err
		}

		data = append(data, '\n')
		_, err = l.file.Write(data)
		if err != nil {
			l.mu.Unlock()
			return err
		}

		l.mu.Unlock()
	}

	// Store in database if store is configured
	if l.store != nil {
		err := l.store.StoreAuditEvent(ctx, event)
		if err != nil {
			return err
		}
	}

	// Also log to zerolog for visibility
	log.Debug().
		Str("event_id", event.ID).
		Str("event_type", string(event.EventType)).
		Str("source", string(event.EventSource)).
		Str("result", string(event.Result)).
		Str("user", event.UserIdentity.Username).
		Str("source_ip", event.SourceIP).
		Int64("duration_ms", event.DurationMS).
		Msg("Audit event")

	return nil
}

// NewEvent creates a new AuditEvent with common fields populated.
func NewEvent(eventType EventType, source EventSource, action string) *AuditEvent {
	return &AuditEvent{
		ID:          uuid.New().String(),
		Timestamp:   time.Now().UTC(),
		EventType:   eventType,
		EventSource: source,
		Action:      action,
		Result:      ResultSuccess,
		Extra:       make(map[string]string),
	}
}

// WithRequestInfo adds request information to the event.
func (e *AuditEvent) WithRequestInfo(requestID, sourceIP, userAgent string) *AuditEvent {
	e.RequestID = requestID
	e.SourceIP = sourceIP
	e.UserAgent = userAgent

	return e
}

// WithUserIdentity adds user identity information to the event.
func (e *AuditEvent) WithUserIdentity(identity UserIdentity) *AuditEvent {
	e.UserIdentity = identity
	return e
}

// WithResource adds resource information to the event.
func (e *AuditEvent) WithResource(resource ResourceInfo) *AuditEvent {
	e.Resource = resource
	return e
}

// WithResult sets the result of the operation.
func (e *AuditEvent) WithResult(result Result, errorCode, errorMessage string) *AuditEvent {
	e.Result = result
	e.ErrorCode = errorCode
	e.ErrorMessage = errorMessage

	return e
}

// WithDuration sets the duration of the operation.
func (e *AuditEvent) WithDuration(duration time.Duration) *AuditEvent {
	e.DurationMS = duration.Milliseconds()
	return e
}

// WithBytes sets the bytes transferred.
func (e *AuditEvent) WithBytes(bytesIn, bytesOut int64) *AuditEvent {
	e.BytesIn = bytesIn
	e.BytesOut = bytesOut

	return e
}

// WithExtra adds extra metadata to the event.
func (e *AuditEvent) WithExtra(key, value string) *AuditEvent {
	if e.Extra == nil {
		e.Extra = make(map[string]string)
	}

	e.Extra[key] = value

	return e
}

// NewPlacementGroupEvent creates a new audit event for placement group operations.
func NewPlacementGroupEvent(eventType EventType, groupID, nodeID string) *AuditEvent {
	event := NewEvent(eventType, SourceCluster, string(eventType))
	event.Resource = ResourceInfo{
		Type:             ResourcePlacementGroup,
		PlacementGroupID: groupID,
		NodeID:           nodeID,
	}
	event.UserIdentity = UserIdentity{
		Type:     IdentitySystem,
		Username: "system",
	}

	return event
}

// NewPlacementGroupNodeJoinedEvent creates an audit event for node joining a placement group.
func NewPlacementGroupNodeJoinedEvent(groupID, nodeID, datacenter, region string) *AuditEvent {
	event := NewPlacementGroupEvent(EventPlacementGroupNodeJoined, groupID, nodeID)
	event.WithExtra("datacenter", datacenter)
	event.WithExtra("region", region)

	return event
}

// NewPlacementGroupNodeLeftEvent creates an audit event for node leaving a placement group.
func NewPlacementGroupNodeLeftEvent(groupID, nodeID, reason string) *AuditEvent {
	event := NewPlacementGroupEvent(EventPlacementGroupNodeLeft, groupID, nodeID)
	if reason != "" {
		event.WithExtra("reason", reason)
	}

	return event
}

// NewPlacementGroupStatusChangedEvent creates an audit event for placement group status changes.
func NewPlacementGroupStatusChangedEvent(groupID, oldStatus, newStatus string) *AuditEvent {
	event := NewPlacementGroupEvent(EventPlacementGroupStatusChanged, groupID, "")
	event.WithExtra("old_status", oldStatus)
	event.WithExtra("new_status", newStatus)

	return event
}

// PlacementGroupAuditAdapter adapts an AuditLogger to the PlacementGroupAuditLogger interface
// used by the cluster package for audit logging.
type PlacementGroupAuditAdapter struct {
	logger *AuditLogger
}

// NewPlacementGroupAuditAdapter creates a new adapter for the audit logger.
func NewPlacementGroupAuditAdapter(logger *AuditLogger) *PlacementGroupAuditAdapter {
	return &PlacementGroupAuditAdapter{logger: logger}
}

// LogNodeJoined logs a node joining a placement group.
func (a *PlacementGroupAuditAdapter) LogNodeJoined(groupID, nodeID, datacenter, region string) {
	if a.logger == nil {
		return
	}

	event := NewPlacementGroupNodeJoinedEvent(groupID, nodeID, datacenter, region)
	a.logger.Log(event)
}

// LogNodeLeft logs a node leaving a placement group.
func (a *PlacementGroupAuditAdapter) LogNodeLeft(groupID, nodeID, reason string) {
	if a.logger == nil {
		return
	}

	event := NewPlacementGroupNodeLeftEvent(groupID, nodeID, reason)
	a.logger.Log(event)
}

// LogStatusChanged logs a placement group status change.
func (a *PlacementGroupAuditAdapter) LogStatusChanged(groupID, oldStatus, newStatus string) {
	if a.logger == nil {
		return
	}

	event := NewPlacementGroupStatusChangedEvent(groupID, oldStatus, newStatus)
	a.logger.Log(event)
}
