package targets

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/events"
)

// AMQPConfig configures an AMQP (RabbitMQ) target
type AMQPConfig struct {
	events.TargetConfig `yaml:",inline"`

	// URL is the AMQP connection URL (amqp://user:pass@host:port/vhost)
	URL string `json:"-" yaml:"url"`

	// Exchange is the exchange name to publish to
	Exchange string `json:"exchange" yaml:"exchange"`

	// ExchangeType is the type of exchange (direct, fanout, topic, headers)
	ExchangeType string `json:"exchangeType,omitempty" yaml:"exchangeType,omitempty"`

	// RoutingKey is the routing key for messages
	RoutingKey string `json:"routingKey,omitempty" yaml:"routingKey,omitempty"`

	// Queue is the queue name (for direct queue publishing)
	Queue string `json:"queue,omitempty" yaml:"queue,omitempty"`

	// Durable indicates if the exchange/queue should be durable
	Durable bool `json:"durable,omitempty" yaml:"durable,omitempty"`

	// AutoDelete indicates if the exchange/queue should auto-delete
	AutoDelete bool `json:"autoDelete,omitempty" yaml:"autoDelete,omitempty"`

	// Mandatory indicates if publishing should fail if no queue is bound
	Mandatory bool `json:"mandatory,omitempty" yaml:"mandatory,omitempty"`

	// Immediate indicates if messages should be delivered immediately
	Immediate bool `json:"immediate,omitempty" yaml:"immediate,omitempty"`

	// TLS configuration
	TLSEnabled    bool   `json:"tlsEnabled,omitempty" yaml:"tlsEnabled,omitempty"`
	TLSCACert     string `json:"tlsCaCert,omitempty" yaml:"tlsCaCert,omitempty"`
	TLSClientCert string `json:"tlsClientCert,omitempty" yaml:"tlsClientCert,omitempty"`
	TLSClientKey  string `json:"-" yaml:"tlsClientKey,omitempty"`
	TLSSkipVerify bool   `json:"tlsSkipVerify,omitempty" yaml:"tlsSkipVerify,omitempty"`

	// ReconnectDelay is the delay between reconnection attempts
	ReconnectDelay time.Duration `json:"reconnectDelay,omitempty" yaml:"reconnectDelay,omitempty"`
}

// DefaultAMQPConfig returns a default AMQP configuration
func DefaultAMQPConfig() AMQPConfig {
	return AMQPConfig{
		TargetConfig: events.TargetConfig{
			Type:       "amqp",
			Enabled:    true,
			QueueSize:  10000,
			MaxRetries: 3,
		},
		ExchangeType:   "topic",
		RoutingKey:     "s3.event",
		Durable:        true,
		ReconnectDelay: 5 * time.Second,
	}
}

// AMQPTarget publishes events to AMQP (RabbitMQ)
// Note: This is a placeholder implementation. In production, you would use
// a library like github.com/rabbitmq/amqp091-go
type AMQPTarget struct {
	config    AMQPConfig
	mu        sync.RWMutex
	closed    bool
	connected bool
	// In production: conn *amqp.Connection, channel *amqp.Channel
}

// NewAMQPTarget creates a new AMQP target
func NewAMQPTarget(config AMQPConfig) (*AMQPTarget, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("%w: URL is required", events.ErrInvalidConfig)
	}
	if config.Exchange == "" && config.Queue == "" {
		return nil, fmt.Errorf("%w: exchange or queue is required", events.ErrInvalidConfig)
	}

	t := &AMQPTarget{
		config: config,
	}

	// In production, you would establish the AMQP connection here:
	// conn, err := amqp.Dial(config.URL)
	// ...

	t.connected = true
	return t, nil
}

// Name returns the target name
func (t *AMQPTarget) Name() string {
	return t.config.Name
}

// Type returns the target type
func (t *AMQPTarget) Type() string {
	return "amqp"
}

// Publish sends an event to AMQP
func (t *AMQPTarget) Publish(ctx context.Context, event *events.S3Event) error {
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

	// In production, you would publish to AMQP:
	// err = t.channel.PublishWithContext(ctx,
	//     t.config.Exchange,
	//     t.config.RoutingKey,
	//     t.config.Mandatory,
	//     t.config.Immediate,
	//     amqp.Publishing{
	//         ContentType: "application/json",
	//         Body:        body,
	//     },
	// )
	_ = body // Placeholder

	return nil
}

// IsHealthy checks if the AMQP connection is healthy
func (t *AMQPTarget) IsHealthy(ctx context.Context) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return !t.closed && t.connected
}

// Close closes the AMQP target
func (t *AMQPTarget) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	t.connected = false
	// In production: t.channel.Close(); t.conn.Close()
	return nil
}

// Ensure AMQPTarget implements Target
var _ events.Target = (*AMQPTarget)(nil)

func init() {
	events.RegisterTargetFactory("amqp", func(config map[string]interface{}) (events.Target, error) {
		cfg := DefaultAMQPConfig()

		if name, ok := config["name"].(string); ok {
			cfg.Name = name
		}
		if url, ok := config["url"].(string); ok {
			cfg.URL = url
		}
		if exchange, ok := config["exchange"].(string); ok {
			cfg.Exchange = exchange
		}
		if exchangeType, ok := config["exchangeType"].(string); ok {
			cfg.ExchangeType = exchangeType
		}
		if routingKey, ok := config["routingKey"].(string); ok {
			cfg.RoutingKey = routingKey
		}
		if queue, ok := config["queue"].(string); ok {
			cfg.Queue = queue
		}
		if durable, ok := config["durable"].(bool); ok {
			cfg.Durable = durable
		}

		return NewAMQPTarget(cfg)
	})
}
