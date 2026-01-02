package targets

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/events"
)

// NATSConfig configures a NATS target.
type NATSConfig struct {
	events.TargetConfig `yaml:",inline"`

	// Servers is the list of NATS server URLs
	Servers []string `json:"servers" yaml:"servers"`

	// Subject is the NATS subject to publish to
	Subject string `json:"subject" yaml:"subject"`

	// Username for authentication
	Username string `json:"-" yaml:"username,omitempty"`

	// Password for authentication
	Password string `json:"-" yaml:"password,omitempty"`

	// Token for token-based authentication
	Token string `json:"-" yaml:"token,omitempty"`

	// NKey is the NKey seed for authentication
	NKey string `json:"-" yaml:"nkey,omitempty"`

	// CredentialsFile is the path to a credentials file
	CredentialsFile string `json:"credentialsFile,omitempty" yaml:"credentialsFile,omitempty"`

	// TLS configuration
	TLSEnabled    bool   `json:"tlsEnabled,omitempty"    yaml:"tlsEnabled,omitempty"`
	TLSCACert     string `json:"tlsCaCert,omitempty"     yaml:"tlsCaCert,omitempty"`
	TLSClientCert string `json:"tlsClientCert,omitempty" yaml:"tlsClientCert,omitempty"`
	TLSClientKey  string `json:"-"                       yaml:"tlsClientKey,omitempty"`
	TLSSkipVerify bool   `json:"tlsSkipVerify,omitempty" yaml:"tlsSkipVerify,omitempty"`

	// JetStream enables JetStream for at-least-once delivery
	JetStream bool `json:"jetStream,omitempty" yaml:"jetStream,omitempty"`

	// Stream is the JetStream stream name
	Stream string `json:"stream,omitempty" yaml:"stream,omitempty"`

	// MaxReconnects is the maximum number of reconnection attempts
	MaxReconnects int `json:"maxReconnects,omitempty" yaml:"maxReconnects,omitempty"`

	// ReconnectWait is the delay between reconnection attempts
	ReconnectWait time.Duration `json:"reconnectWait,omitempty" yaml:"reconnectWait,omitempty"`
}

// DefaultNATSConfig returns a default NATS configuration.
func DefaultNATSConfig() NATSConfig {
	// Use NATS_URL environment variable if set, otherwise use default
	servers := []string{"nats://localhost:4222"}
	if natsURL := os.Getenv("NATS_URL"); natsURL != "" {
		servers = strings.Split(natsURL, ",")
	}

	return NATSConfig{
		TargetConfig: events.TargetConfig{
			Type:       "nats",
			Enabled:    true,
			QueueSize:  10000,
			MaxRetries: 3,
		},
		Servers:       servers,
		Subject:       "s3.events",
		MaxReconnects: 10,
		ReconnectWait: 2 * time.Second,
	}
}

// NATSTarget publishes events to NATS
// Note: This is a placeholder implementation. In production, you would use
// github.com/nats-io/nats.go.
type NATSTarget struct {
	config    NATSConfig
	mu        sync.RWMutex
	closed    bool
	connected bool
	// In production: conn *nats.Conn, js nats.JetStreamContext
}

// NewNATSTarget creates a new NATS target.
func NewNATSTarget(config NATSConfig) (*NATSTarget, error) {
	if len(config.Servers) == 0 {
		return nil, fmt.Errorf("%w: servers are required", events.ErrInvalidConfig)
	}

	if config.Subject == "" {
		return nil, fmt.Errorf("%w: subject is required", events.ErrInvalidConfig)
	}

	t := &NATSTarget{
		config: config,
	}

	// In production, you would establish the NATS connection here:
	// opts := []nats.Option{
	//     nats.MaxReconnects(config.MaxReconnects),
	//     nats.ReconnectWait(config.ReconnectWait),
	// }
	// conn, err := nats.Connect(strings.Join(config.Servers, ","), opts...)
	// ...

	t.connected = true

	return t, nil
}

// Name returns the target name.
func (t *NATSTarget) Name() string {
	return t.config.Name
}

// Type returns the target type.
func (t *NATSTarget) Type() string {
	return "nats"
}

// Publish sends an event to NATS.
func (t *NATSTarget) Publish(ctx context.Context, event *events.S3Event) error {
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

	// In production, you would publish to NATS:
	// if t.config.JetStream {
	//     _, err = t.js.Publish(t.config.Subject, body)
	// } else {
	//     err = t.conn.Publish(t.config.Subject, body)
	// }
	_ = body // Placeholder

	return nil
}

// IsHealthy checks if the NATS connection is healthy.
func (t *NATSTarget) IsHealthy(ctx context.Context) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return !t.closed && t.connected
}

// Close closes the NATS target.
func (t *NATSTarget) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	t.connected = false
	// In production: t.conn.Close()
	return nil
}

// Ensure NATSTarget implements Target.
var _ events.Target = (*NATSTarget)(nil)

func init() {
	events.RegisterTargetFactory("nats", func(config map[string]any) (events.Target, error) {
		cfg := DefaultNATSConfig()

		if name, ok := config["name"].(string); ok {
			cfg.Name = name
		}

		if servers, ok := config["servers"].([]any); ok {
			cfg.Servers = make([]string, len(servers))
			for i, s := range servers {
				if str, ok := s.(string); ok {
					cfg.Servers[i] = str
				}
			}
		}

		if subject, ok := config["subject"].(string); ok {
			cfg.Subject = subject
		}

		if username, ok := config["username"].(string); ok {
			cfg.Username = username
		}

		if password, ok := config["password"].(string); ok {
			cfg.Password = password
		}

		if jetStream, ok := config["jetStream"].(bool); ok {
			cfg.JetStream = jetStream
		}

		if stream, ok := config["stream"].(string); ok {
			cfg.Stream = stream
		}

		return NewNATSTarget(cfg)
	})
}
