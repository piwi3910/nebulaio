// Package httputil provides shared HTTP client utilities with optimized connection pooling.
package httputil

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// Default transport configuration for connection pooling.
const (
	DefaultMaxIdleConns        = 100
	DefaultMaxIdleConnsPerHost = 10
	DefaultIdleConnTimeout     = 90 * time.Second
	DefaultDialTimeout         = 30 * time.Second
	DefaultKeepAlive           = 30 * time.Second
	DefaultTLSHandshakeTimeout = 10 * time.Second
	DefaultExpectContinue      = 1 * time.Second
)

// ClientConfig holds configuration options for creating an HTTP client.
type ClientConfig struct {
	// Timeout specifies a time limit for requests made by this Client.
	// A Timeout of zero means no timeout.
	Timeout time.Duration

	// MaxIdleConns controls the maximum number of idle (keep-alive) connections
	// across all hosts. Zero means no limit.
	MaxIdleConns int

	// MaxIdleConnsPerHost controls the maximum idle (keep-alive) connections
	// to keep per-host. Zero means DefaultMaxIdleConnsPerHost (2).
	MaxIdleConnsPerHost int

	// IdleConnTimeout is the maximum amount of time an idle connection will
	// remain idle before closing itself.
	IdleConnTimeout time.Duration

	// TLSConfig specifies the TLS configuration to use.
	// If nil, the default configuration is used.
	TLSConfig *tls.Config

	// SkipTLSVerify, if true, disables TLS certificate verification.
	// This should only be used in development/testing environments.
	SkipTLSVerify bool

	// DisableKeepAlives, if true, disables HTTP keep-alives and will only use
	// the connection to the server for a single HTTP request.
	DisableKeepAlives bool
}

// DefaultConfig returns the default client configuration with optimized pooling.
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		Timeout:             30 * time.Second,
		MaxIdleConns:        DefaultMaxIdleConns,
		MaxIdleConnsPerHost: DefaultMaxIdleConnsPerHost,
		IdleConnTimeout:     DefaultIdleConnTimeout,
		DisableKeepAlives:   false,
	}
}

// NewClient creates a new HTTP client with optimized connection pooling.
// If cfg is nil, DefaultConfig() is used.
func NewClient(cfg *ClientConfig) *http.Client {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Apply defaults for zero values
	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns == 0 {
		maxIdleConns = DefaultMaxIdleConns
	}

	maxIdleConnsPerHost := cfg.MaxIdleConnsPerHost
	if maxIdleConnsPerHost == 0 {
		maxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	}

	idleConnTimeout := cfg.IdleConnTimeout
	if idleConnTimeout == 0 {
		idleConnTimeout = DefaultIdleConnTimeout
	}

	// Build TLS config - clone user-provided config to avoid mutation
	var tlsConfig *tls.Config
	if cfg.TLSConfig != nil {
		tlsConfig = cfg.TLSConfig.Clone()
	} else {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	if cfg.SkipTLSVerify {
		tlsConfig.InsecureSkipVerify = true
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   DefaultDialTimeout,
			KeepAlive: DefaultKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   DefaultTLSHandshakeTimeout,
		ExpectContinueTimeout: DefaultExpectContinue,
		TLSClientConfig:       tlsConfig,
		DisableKeepAlives:     cfg.DisableKeepAlives,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}
}

// NewClientWithTimeout creates a new HTTP client with optimized connection pooling
// and the specified timeout. This is a convenience function for the common case.
func NewClientWithTimeout(timeout time.Duration) *http.Client {
	cfg := DefaultConfig()
	cfg.Timeout = timeout

	return NewClient(cfg)
}

// NewClientWithTLS creates a new HTTP client with the specified TLS configuration.
func NewClientWithTLS(timeout time.Duration, tlsConfig *tls.Config) *http.Client {
	cfg := DefaultConfig()
	cfg.Timeout = timeout
	cfg.TLSConfig = tlsConfig

	return NewClient(cfg)
}

// defaultClient is a package-level shared HTTP client for simple use cases.
// It is safe for concurrent use by multiple goroutines.
// IMPORTANT: Do not modify the returned client's Transport or other fields.
// Use NewClient() for cases requiring custom configuration.
var defaultClient = NewClient(DefaultConfig())

// Default returns the default shared HTTP client with connection pooling.
// This client has a 30-second timeout and default pooling settings.
// The returned client is safe for concurrent use but should not be modified.
// For custom timeouts or configurations, use NewClient() instead.
//
// WARNING: The returned client is shared across all callers. Modifying its
// fields (Transport, Timeout, etc.) will affect all other users of this client
// and may cause undefined behavior. Always create a new client with NewClient()
// if you need custom configuration.
func Default() *http.Client {
	return defaultClient
}
