package targets

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/events"
	"github.com/piwi3910/nebulaio/internal/httputil"
	"github.com/rs/zerolog/log"
)

// WebhookConfig configures a webhook target
type WebhookConfig struct {
	events.TargetConfig `yaml:",inline"`

	// URL is the webhook endpoint URL
	URL string `json:"url" yaml:"url"`

	// AuthToken is an optional authorization token
	AuthToken string `json:"-" yaml:"authToken,omitempty"`

	// Secret is used to sign the webhook payload
	Secret string `json:"-" yaml:"secret,omitempty"`

	// Headers are additional HTTP headers to send
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`

	// Timeout is the HTTP request timeout
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// SkipTLSVerify disables TLS certificate verification
	SkipTLSVerify bool `json:"skipTlsVerify,omitempty" yaml:"skipTlsVerify,omitempty"`
}

// DefaultWebhookConfig returns a default webhook configuration
func DefaultWebhookConfig() WebhookConfig {
	return WebhookConfig{
		TargetConfig: events.TargetConfig{
			Type:       "webhook",
			Enabled:    true,
			QueueSize:  1000,
			MaxRetries: 3,
		},
		Timeout: 30 * time.Second,
	}
}

// WebhookTarget publishes events to HTTP endpoints
type WebhookTarget struct {
	config WebhookConfig
	client *http.Client
	mu     sync.RWMutex
	closed bool
}

// NewWebhookTarget creates a new webhook target
func NewWebhookTarget(config WebhookConfig) (*WebhookTarget, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("%w: URL is required", events.ErrInvalidConfig)
	}

	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	// Use shared httputil configuration for connection pooling
	clientCfg := httputil.DefaultConfig()
	clientCfg.Timeout = config.Timeout

	if config.SkipTLSVerify {
		log.Warn().
			Str("url", config.URL).
			Msg("WARNING: TLS certificate verification disabled for webhook target - this should only be used in development/testing")
		// G402: InsecureSkipVerify is user-configurable for dev/test environments
		// Even when skipping verification, we still enforce TLS 1.2+ for encryption
		clientCfg.TLSConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // G402: User-configured for development/testing only
			MinVersion:         tls.VersionTLS12,
		}
	}

	return &WebhookTarget{
		config: config,
		client: httputil.NewClient(clientCfg),
	}, nil
}

// Name returns the target name
func (t *WebhookTarget) Name() string {
	return t.config.Name
}

// Type returns the target type
func (t *WebhookTarget) Type() string {
	return "webhook"
}

// Publish sends an event to the webhook endpoint
func (t *WebhookTarget) Publish(ctx context.Context, event *events.S3Event) error {
	t.mu.RLock()
	if t.closed {
		t.mu.RUnlock()
		return events.ErrTargetClosed
	}
	t.mu.RUnlock()

	// Serialize event
	body, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NebulaIO/1.0")

	if t.config.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+t.config.AuthToken)
	}

	// Sign payload if secret is configured
	if t.config.Secret != "" {
		signature := t.signPayload(body)
		req.Header.Set("X-NebulaIO-Signature-256", signature)
	}

	// Add custom headers
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}

	// Send request
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check response
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: HTTP %d: %s", events.ErrPublishFailed, resp.StatusCode, string(body))
	}

	return nil
}

// signPayload signs the payload using HMAC-SHA256
func (t *WebhookTarget) signPayload(payload []byte) string {
	h := hmac.New(sha256.New, []byte(t.config.Secret))
	h.Write(payload)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// IsHealthy checks if the webhook endpoint is healthy
func (t *WebhookTarget) IsHealthy(ctx context.Context) bool {
	t.mu.RLock()
	if t.closed {
		t.mu.RUnlock()
		return false
	}
	t.mu.RUnlock()

	// Try a HEAD request to check if the endpoint is reachable
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, t.config.URL, nil)
	if err != nil {
		return false
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()

	// Consider 2xx and 405 (Method Not Allowed) as healthy
	return resp.StatusCode < 400 || resp.StatusCode == 405
}

// Close closes the webhook target
func (t *WebhookTarget) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	t.client.CloseIdleConnections()
	return nil
}

// Ensure WebhookTarget implements Target
var _ events.Target = (*WebhookTarget)(nil)

func init() {
	events.RegisterTargetFactory("webhook", func(config map[string]any) (events.Target, error) {
		cfg := DefaultWebhookConfig()

		if name, ok := config["name"].(string); ok {
			cfg.Name = name
		}
		if url, ok := config["url"].(string); ok {
			cfg.URL = url
		}
		if token, ok := config["authToken"].(string); ok {
			cfg.AuthToken = token
		}
		if secret, ok := config["secret"].(string); ok {
			cfg.Secret = secret
		}
		if headers, ok := config["headers"].(map[string]string); ok {
			cfg.Headers = headers
		}
		if timeout, ok := config["timeout"].(string); ok {
			if d, err := time.ParseDuration(timeout); err == nil {
				cfg.Timeout = d
			}
		}
		if skip, ok := config["skipTlsVerify"].(bool); ok {
			cfg.SkipTLSVerify = skip
		}

		return NewWebhookTarget(cfg)
	})
}
