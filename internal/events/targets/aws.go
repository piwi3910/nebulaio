package targets

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/events"
)

// SQSConfig configures an AWS SQS target.
type SQSConfig struct {
	QueueURL            string           `json:"queueUrl" yaml:"queueUrl"`
	Region              string           `json:"region" yaml:"region"`
	AccessKeyID         string           `json:"-" yaml:"accessKeyId,omitempty"`
	SecretAccessKey     string           `json:"-" yaml:"secretAccessKey,omitempty"`
	SessionToken        string           `json:"-" yaml:"sessionToken,omitempty"`
	Endpoint            string           `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	MessageGroupID      string           `json:"messageGroupId,omitempty" yaml:"messageGroupId,omitempty"`
	events.TargetConfig `yaml:",inline"` //nolint:embeddedstructfieldcheck // Grouped with config fields

	DelaySeconds int `json:"delay_seconds,omitempty" yaml:"delay_seconds,omitempty"`
}

// DefaultSQSConfig returns a default SQS configuration.
func DefaultSQSConfig() SQSConfig {
	return SQSConfig{
		TargetConfig: events.TargetConfig{
			Type:       "sqs",
			Enabled:    true,
			QueueSize:  10000,
			MaxRetries: 3,
		},
		Region: "us-east-1",
	}
}

// SQSTarget publishes events to AWS SQS
// Note: This is a placeholder implementation. In production, you would use
// github.com/aws/aws-sdk-go-v2/service/sqs.
type SQSTarget struct {
	config    SQSConfig
	mu        sync.RWMutex
	closed    bool
	connected bool
	// In production: client *sqs.Client
}

// NewSQSTarget creates a new SQS target.
func NewSQSTarget(config SQSConfig) (*SQSTarget, error) {
	if config.QueueURL == "" {
		return nil, fmt.Errorf("%w: queueUrl is required", events.ErrInvalidConfig)
	}

	if config.Region == "" {
		return nil, fmt.Errorf("%w: region is required", events.ErrInvalidConfig)
	}

	t := &SQSTarget{
		config: config,
	}

	// In production, you would initialize the SQS client here:
	// cfg, err := awsconfig.LoadDefaultConfig(ctx,
	//     awsconfig.WithRegion(config.Region),
	// )
	// t.client = sqs.NewFromConfig(cfg)

	t.connected = true

	return t, nil
}

// Name returns the target name.
func (t *SQSTarget) Name() string {
	return t.config.Name
}

// Type returns the target type.
func (t *SQSTarget) Type() string {
	return "sqs"
}

// Publish sends an event to SQS.
func (t *SQSTarget) Publish(ctx context.Context, event *events.S3Event) error {
	t.mu.RLock()

	if t.closed {
		t.mu.RUnlock()
		return events.ErrTargetClosed
	}

	t.mu.RUnlock()

	body, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// In production, you would send to SQS:
	// input := &sqs.SendMessageInput{
	//     QueueUrl:    &t.config.QueueURL,
	//     MessageBody: aws.String(string(body)),
	// }
	// if t.config.MessageGroupID != "" {
	//     input.MessageGroupId = &t.config.MessageGroupID
	// }
	// _, err = t.client.SendMessage(ctx, input)
	_ = body // Placeholder

	return nil
}

// IsHealthy checks if the SQS connection is healthy.
func (t *SQSTarget) IsHealthy(ctx context.Context) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return !t.closed && t.connected
}

// Close closes the SQS target.
func (t *SQSTarget) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	t.connected = false

	return nil
}

// Ensure SQSTarget implements Target.
var _ events.Target = (*SQSTarget)(nil)

func init() {
	events.RegisterTargetFactory("sqs", func(config map[string]any) (events.Target, error) {
		cfg := DefaultSQSConfig()

		if name, ok := config["name"].(string); ok {
			cfg.Name = name
		}

		if queueURL, ok := config["queueUrl"].(string); ok {
			cfg.QueueURL = queueURL
		}

		if region, ok := config["region"].(string); ok {
			cfg.Region = region
		}

		if accessKeyID, ok := config["accessKeyId"].(string); ok {
			cfg.AccessKeyID = accessKeyID
		}

		if secretAccessKey, ok := config["secretAccessKey"].(string); ok {
			cfg.SecretAccessKey = secretAccessKey
		}

		if endpoint, ok := config["endpoint"].(string); ok {
			cfg.Endpoint = endpoint
		}

		return NewSQSTarget(cfg)
	})
}

// SNSConfig configures an AWS SNS target.
type SNSConfig struct {
	TopicARN            string           `json:"topicArn" yaml:"topicArn"`
	Region              string           `json:"region" yaml:"region"`
	AccessKeyID         string           `json:"-" yaml:"accessKeyId,omitempty"`
	SecretAccessKey     string           `json:"-" yaml:"secretAccessKey,omitempty"`
	SessionToken        string           `json:"-" yaml:"sessionToken,omitempty"`
	Endpoint            string           `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Subject             string           `json:"subject,omitempty" yaml:"subject,omitempty"`
	events.TargetConfig `yaml:",inline"` //nolint:embeddedstructfieldcheck // Grouped with config fields
}

// DefaultSNSConfig returns a default SNS configuration.
func DefaultSNSConfig() SNSConfig {
	return SNSConfig{
		TargetConfig: events.TargetConfig{
			Type:       "sns",
			Enabled:    true,
			QueueSize:  10000,
			MaxRetries: 3,
		},
		Region: "us-east-1",
	}
}

// SNSTarget publishes events to AWS SNS
// Note: This is a placeholder implementation. In production, you would use
// github.com/aws/aws-sdk-go-v2/service/sns.
type SNSTarget struct {
	config    SNSConfig
	mu        sync.RWMutex
	closed    bool
	connected bool
	// In production: client *sns.Client
}

// NewSNSTarget creates a new SNS target.
func NewSNSTarget(config SNSConfig) (*SNSTarget, error) {
	if config.TopicARN == "" {
		return nil, fmt.Errorf("%w: topicArn is required", events.ErrInvalidConfig)
	}

	if config.Region == "" {
		return nil, fmt.Errorf("%w: region is required", events.ErrInvalidConfig)
	}

	t := &SNSTarget{
		config: config,
	}

	// In production, you would initialize the SNS client here:
	// cfg, err := awsconfig.LoadDefaultConfig(ctx,
	//     awsconfig.WithRegion(config.Region),
	// )
	// t.client = sns.NewFromConfig(cfg)

	t.connected = true

	return t, nil
}

// Name returns the target name.
func (t *SNSTarget) Name() string {
	return t.config.Name
}

// Type returns the target type.
func (t *SNSTarget) Type() string {
	return "sns"
}

// Publish sends an event to SNS.
func (t *SNSTarget) Publish(ctx context.Context, event *events.S3Event) error {
	t.mu.RLock()

	if t.closed {
		t.mu.RUnlock()
		return events.ErrTargetClosed
	}

	t.mu.RUnlock()

	body, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// In production, you would publish to SNS:
	// input := &sns.PublishInput{
	//     TopicArn: &t.config.TopicARN,
	//     Message:  aws.String(string(body)),
	// }
	// if t.config.Subject != "" {
	//     input.Subject = &t.config.Subject
	// }
	// _, err = t.client.Publish(ctx, input)
	_ = body // Placeholder

	return nil
}

// IsHealthy checks if the SNS connection is healthy.
func (t *SNSTarget) IsHealthy(ctx context.Context) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return !t.closed && t.connected
}

// Close closes the SNS target.
func (t *SNSTarget) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	t.connected = false

	return nil
}

// Ensure SNSTarget implements Target.
var _ events.Target = (*SNSTarget)(nil)

func init() {
	events.RegisterTargetFactory("sns", func(config map[string]any) (events.Target, error) {
		cfg := DefaultSNSConfig()

		if name, ok := config["name"].(string); ok {
			cfg.Name = name
		}

		if topicARN, ok := config["topicArn"].(string); ok {
			cfg.TopicARN = topicARN
		}

		if region, ok := config["region"].(string); ok {
			cfg.Region = region
		}

		if accessKeyID, ok := config["accessKeyId"].(string); ok {
			cfg.AccessKeyID = accessKeyID
		}

		if secretAccessKey, ok := config["secretAccessKey"].(string); ok {
			cfg.SecretAccessKey = secretAccessKey
		}

		if endpoint, ok := config["endpoint"].(string); ok {
			cfg.Endpoint = endpoint
		}

		if subject, ok := config["subject"].(string); ok {
			cfg.Subject = subject
		}

		return NewSNSTarget(cfg)
	})
}

// These are placeholders for type information only.
var _ time.Duration
