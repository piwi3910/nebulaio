package targets

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/events"
)

// KafkaConfig configures a Kafka target.
type KafkaConfig struct {
	events.TargetConfig `yaml:",inline"`

	// Brokers is the list of Kafka broker addresses
	Brokers []string `json:"brokers" yaml:"brokers"`

	// Topic is the Kafka topic to publish to
	Topic string `json:"topic" yaml:"topic"`

	// SASL authentication
	SASLUsername  string `json:"-"                       yaml:"saslUsername,omitempty"`
	SASLPassword  string `json:"-"                       yaml:"saslPassword,omitempty"`
	SASLMechanism string `json:"saslMechanism,omitempty" yaml:"saslMechanism,omitempty"`

	// TLS configuration
	TLSEnabled    bool   `json:"tlsEnabled,omitempty"    yaml:"tlsEnabled,omitempty"`
	TLSCACert     string `json:"tlsCaCert,omitempty"     yaml:"tlsCaCert,omitempty"`
	TLSClientCert string `json:"tlsClientCert,omitempty" yaml:"tlsClientCert,omitempty"`
	TLSClientKey  string `json:"-"                       yaml:"tlsClientKey,omitempty"`
	TLSSkipVerify bool   `json:"tlsSkipVerify,omitempty" yaml:"tlsSkipVerify,omitempty"`

	// Producer settings
	BatchSize    int           `json:"batchSize,omitempty"    yaml:"batchSize,omitempty"`
	BatchTimeout time.Duration `json:"batchTimeout,omitempty" yaml:"batchTimeout,omitempty"`
	MaxRetries   int           `json:"maxRetries,omitempty"   yaml:"maxRetries,omitempty"`

	// Compression type (none, gzip, snappy, lz4, zstd)
	Compression string `json:"compression,omitempty" yaml:"compression,omitempty"`
}

// DefaultKafkaConfig returns a default Kafka configuration.
func DefaultKafkaConfig() KafkaConfig {
	return KafkaConfig{
		TargetConfig: events.TargetConfig{
			Type:       "kafka",
			Enabled:    true,
			QueueSize:  10000,
			MaxRetries: 3,
		},
		BatchSize:    100,
		BatchTimeout: 1 * time.Second,
		MaxRetries:   3,
		Compression:  "snappy",
	}
}

// KafkaTarget publishes events to Kafka
// Note: This is a placeholder implementation. In production, you would use
// a Kafka client library like segmentio/kafka-go or Shopify/sarama.
type KafkaTarget struct {
	config    KafkaConfig
	mu        sync.RWMutex
	closed    bool
	connected bool
	// In production: writer *kafka.Writer
}

// NewKafkaTarget creates a new Kafka target.
func NewKafkaTarget(config KafkaConfig) (*KafkaTarget, error) {
	if len(config.Brokers) == 0 {
		return nil, fmt.Errorf("%w: brokers are required", events.ErrInvalidConfig)
	}

	if config.Topic == "" {
		return nil, fmt.Errorf("%w: topic is required", events.ErrInvalidConfig)
	}

	t := &KafkaTarget{
		config: config,
	}

	// In production, you would initialize the Kafka producer here:
	// t.writer = &kafka.Writer{
	//     Addr:         kafka.TCP(config.Brokers...),
	//     Topic:        config.Topic,
	//     Balancer:     &kafka.LeastBytes{},
	//     BatchSize:    config.BatchSize,
	//     BatchTimeout: config.BatchTimeout,
	// }

	t.connected = true

	return t, nil
}

// Name returns the target name.
func (t *KafkaTarget) Name() string {
	return t.config.Name
}

// Type returns the target type.
func (t *KafkaTarget) Type() string {
	return "kafka"
}

// Publish sends an event to Kafka.
func (t *KafkaTarget) Publish(ctx context.Context, event *events.S3Event) error {
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

	// In production, you would publish to Kafka:
	// err = t.writer.WriteMessages(ctx, kafka.Message{
	//     Key:   []byte(event.Records[0].S3.Bucket.Name + "/" + event.Records[0].S3.Object.Key),
	//     Value: body,
	// })
	_ = body // Placeholder

	return nil
}

// IsHealthy checks if the Kafka connection is healthy.
func (t *KafkaTarget) IsHealthy(ctx context.Context) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return !t.closed && t.connected
}

// Close closes the Kafka target.
func (t *KafkaTarget) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	t.connected = false
	// In production: return t.writer.Close()
	return nil
}

// Ensure KafkaTarget implements Target.
var _ events.Target = (*KafkaTarget)(nil)

func init() {
	events.RegisterTargetFactory("kafka", func(config map[string]interface{}) (events.Target, error) {
		cfg := DefaultKafkaConfig()

		if name, ok := config["name"].(string); ok {
			cfg.Name = name
		}

		if brokers, ok := config["brokers"].([]interface{}); ok {
			cfg.Brokers = make([]string, len(brokers))
			for i, b := range brokers {
				if s, ok := b.(string); ok {
					cfg.Brokers[i] = s
				}
			}
		}

		if topic, ok := config["topic"].(string); ok {
			cfg.Topic = topic
		}

		if username, ok := config["saslUsername"].(string); ok {
			cfg.SASLUsername = username
		}

		if password, ok := config["saslPassword"].(string); ok {
			cfg.SASLPassword = password
		}

		return NewKafkaTarget(cfg)
	})
}
