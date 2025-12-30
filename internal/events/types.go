package events

import (
	"encoding/json"
	"time"
)

// Request ID generation constants.
const (
	requestIDHexLength         = 8  // Length of random hex for request ID
	extendedRequestIDHexLength = 32 // Length of random hex for extended request ID
)

// S3Event represents an S3 event notification.
type S3Event struct {
	// Records contains the event records
	Records []S3EventRecord `json:"Records"`
}

// S3EventRecord represents a single S3 event record.
type S3EventRecord struct {
	// EventVersion is the event version
	EventVersion string `json:"eventVersion"`

	// EventSource is the source of the event
	EventSource string `json:"eventSource"`

	// AWSRegion is the AWS region (or cluster region)
	AWSRegion string `json:"awsRegion"`

	// EventTime is when the event occurred
	EventTime time.Time `json:"eventTime"`

	// EventName is the type of event (e.g., s3:ObjectCreated:Put)
	EventName string `json:"eventName"`

	// UserIdentity contains the user identity information
	UserIdentity UserIdentity `json:"userIdentity"`

	// RequestParameters contains request parameters
	RequestParameters map[string]string `json:"requestParameters"`

	// ResponseElements contains response elements
	ResponseElements map[string]string `json:"responseElements"`

	// S3 contains the S3-specific event data
	S3 S3Entity `json:"s3"`
}

// UserIdentity contains user identity information.
type UserIdentity struct {
	// PrincipalID is the principal ID
	PrincipalID string `json:"principalId"`
}

// S3Entity contains S3-specific event data.
type S3Entity struct {
	// SchemaVersion is the schema version
	SchemaVersion string `json:"s3SchemaVersion"`

	// ConfigurationID is the notification configuration ID
	ConfigurationID string `json:"configurationId,omitempty"`

	// Bucket contains bucket information
	Bucket S3Bucket `json:"bucket"`

	// Object contains object information
	Object S3Object `json:"object"`
}

// S3Bucket contains bucket information.
type S3Bucket struct {
	// Name is the bucket name
	Name string `json:"name"`

	// OwnerIdentity contains the bucket owner identity
	OwnerIdentity UserIdentity `json:"ownerIdentity"`

	// ARN is the bucket ARN
	ARN string `json:"arn"`
}

// S3Object contains object information.
type S3Object struct {
	Key       string `json:"key"`
	ETag      string `json:"eTag,omitempty"`
	VersionID string `json:"versionId,omitempty"`
	Sequencer string `json:"sequencer,omitempty"`
	Size      int64  `json:"size"`
}

// EventType represents the type of S3 event.
type EventType string

// S3 event types.
const (
	// Object created events.
	EventObjectCreated                        EventType = "s3:ObjectCreated:*"
	EventObjectCreatedPut                     EventType = "s3:ObjectCreated:Put"
	EventObjectCreatedPost                    EventType = "s3:ObjectCreated:Post"
	EventObjectCreatedCopy                    EventType = "s3:ObjectCreated:Copy"
	EventObjectCreatedCompleteMultipartUpload EventType = "s3:ObjectCreated:CompleteMultipartUpload"

	// Object removed events.
	EventObjectRemoved                    EventType = "s3:ObjectRemoved:*"
	EventObjectRemovedDelete              EventType = "s3:ObjectRemoved:Delete"
	EventObjectRemovedDeleteMarkerCreated EventType = "s3:ObjectRemoved:DeleteMarkerCreated"

	// Object restore events.
	EventObjectRestorePost      EventType = "s3:ObjectRestore:Post"
	EventObjectRestoreCompleted EventType = "s3:ObjectRestore:Completed"

	// Replication events.
	EventReplicationOperationFailed    EventType = "s3:Replication:OperationFailedReplication"
	EventReplicationOperationCompleted EventType = "s3:Replication:OperationReplicatedAfterThreshold"

	// Lifecycle events.
	EventLifecycleExpiration                    EventType = "s3:LifecycleExpiration:*"
	EventLifecycleExpirationDelete              EventType = "s3:LifecycleExpiration:Delete"
	EventLifecycleExpirationDeleteMarkerCreated EventType = "s3:LifecycleExpiration:DeleteMarkerCreated"

	// Object tagging events.
	EventObjectTagging       EventType = "s3:ObjectTagging:*"
	EventObjectTaggingPut    EventType = "s3:ObjectTagging:Put"
	EventObjectTaggingDelete EventType = "s3:ObjectTagging:Delete"

	// Object ACL events.
	EventObjectAclPut EventType = "s3:ObjectAcl:Put"

	// Test event.
	EventTestEvent EventType = "s3:TestEvent"
)

// NotificationConfiguration represents bucket notification configuration.
type NotificationConfiguration struct {
	// TopicConfigurations is for SNS topics
	TopicConfigurations []TopicConfiguration `json:"TopicConfigurations,omitempty" xml:"TopicConfiguration,omitempty"`

	// QueueConfigurations is for SQS queues
	QueueConfigurations []QueueConfiguration `json:"QueueConfigurations,omitempty" xml:"QueueConfiguration,omitempty"`

	// LambdaFunctionConfigurations is for Lambda functions (or webhooks)
	LambdaFunctionConfigurations []LambdaFunctionConfiguration `json:"CloudFunctionConfigurations,omitempty" xml:"CloudFunctionConfiguration,omitempty"`
}

// TopicConfiguration configures SNS topic notifications.
type TopicConfiguration struct {
	Filter   *NotificationFilter `json:"Filter,omitempty" xml:"Filter,omitempty"`
	ID       string              `json:"Id,omitempty" xml:"Id,omitempty"`
	TopicARN string              `json:"Topic" xml:"Topic"`
	Events   []EventType         `json:"Events" xml:"Event"`
}

// QueueConfiguration configures SQS queue notifications.
type QueueConfiguration struct {
	Filter   *NotificationFilter `json:"Filter,omitempty" xml:"Filter,omitempty"`
	ID       string              `json:"Id,omitempty" xml:"Id,omitempty"`
	QueueARN string              `json:"Queue" xml:"Queue"`
	Events   []EventType         `json:"Events" xml:"Event"`
}

// LambdaFunctionConfiguration configures Lambda/webhook notifications.
type LambdaFunctionConfiguration struct {
	Filter            *NotificationFilter `json:"Filter,omitempty" xml:"Filter,omitempty"`
	ID                string              `json:"Id,omitempty" xml:"Id,omitempty"`
	LambdaFunctionARN string              `json:"CloudFunction" xml:"CloudFunction"`
	Events            []EventType         `json:"Events" xml:"Event"`
}

// NotificationFilter contains filter rules.
type NotificationFilter struct {
	// Key contains the key filter rules
	Key *KeyFilter `json:"S3Key,omitempty" xml:"S3Key,omitempty"`
}

// KeyFilter contains key filter rules.
type KeyFilter struct {
	// FilterRules is the list of filter rules
	FilterRules []FilterRule `json:"FilterRules" xml:"FilterRule"`
}

// FilterRule represents a single filter rule.
type FilterRule struct {
	// Name is the filter name (prefix or suffix)
	Name string `json:"Name" xml:"Name"`

	// Value is the filter value
	Value string `json:"Value" xml:"Value"`
}

// NewS3Event creates a new S3 event.
func NewS3Event(eventType EventType, bucket, key string, size int64, etag, versionID, accessKey string) *S3Event {
	now := time.Now()

	return &S3Event{
		Records: []S3EventRecord{
			{
				EventVersion: "2.2",
				EventSource:  "nebulaio:s3",
				AWSRegion:    "us-east-1",
				EventTime:    now,
				EventName:    string(eventType),
				UserIdentity: UserIdentity{
					PrincipalID: accessKey,
				},
				RequestParameters: map[string]string{
					"sourceIPAddress": "127.0.0.1",
				},
				ResponseElements: map[string]string{
					"x-amz-request-id": generateRequestID(),
					"x-amz-id-2":       generateExtendedRequestID(),
				},
				S3: S3Entity{
					SchemaVersion: "1.0",
					Bucket: S3Bucket{
						Name: bucket,
						OwnerIdentity: UserIdentity{
							PrincipalID: accessKey,
						},
						ARN: "arn:aws:s3:::" + bucket,
					},
					Object: S3Object{
						Key:       key,
						Size:      size,
						ETag:      etag,
						VersionID: versionID,
						Sequencer: generateSequencer(),
					},
				},
			},
		},
	}
}

// ToJSON serializes the event to JSON.
func (e *S3Event) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// generateRequestID generates a unique request ID.
func generateRequestID() string {
	return time.Now().Format("20060102150405") + "-" + randomHex(requestIDHexLength)
}

// generateExtendedRequestID generates an extended request ID.
func generateExtendedRequestID() string {
	return randomHex(extendedRequestIDHexLength)
}

// generateSequencer generates a sequencer value.
func generateSequencer() string {
	return time.Now().Format("20060102150405.000000000")
}

// randomHex generates a random hex string.
func randomHex(n int) string {
	const hexChars = "0123456789ABCDEF"

	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[time.Now().UnixNano()%16]
	}

	return string(b)
}
