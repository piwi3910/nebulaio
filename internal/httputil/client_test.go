package httputil_test

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/httputil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := httputil.DefaultConfig()

	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.Equal(t, httputil.DefaultMaxIdleConns, cfg.MaxIdleConns)
	assert.Equal(t, httputil.DefaultMaxIdleConnsPerHost, cfg.MaxIdleConnsPerHost)
	assert.Equal(t, httputil.DefaultIdleConnTimeout, cfg.IdleConnTimeout)
	assert.False(t, cfg.DisableKeepAlives)
	assert.False(t, cfg.SkipTLSVerify)
	assert.Nil(t, cfg.TLSConfig)
}

func TestNewClient_WithNilConfig(t *testing.T) {
	t.Parallel()

	client := httputil.NewClient(nil)

	require.NotNil(t, client)
	assert.Equal(t, 30*time.Second, client.Timeout)
	assert.NotNil(t, client.Transport)
}

func TestNewClient_WithCustomConfig(t *testing.T) {
	t.Parallel()

	cfg := &httputil.ClientConfig{
		Timeout:             60 * time.Second,
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     120 * time.Second,
	}

	client := httputil.NewClient(cfg)

	require.NotNil(t, client)
	assert.Equal(t, 60*time.Second, client.Timeout)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, 50, transport.MaxIdleConns)
	assert.Equal(t, 5, transport.MaxIdleConnsPerHost)
	assert.Equal(t, 120*time.Second, transport.IdleConnTimeout)
}

func TestNewClient_AppliesDefaultsForZeroValues(t *testing.T) {
	t.Parallel()

	cfg := &httputil.ClientConfig{
		Timeout: 45 * time.Second,
		// Leave other values as zero
	}

	client := httputil.NewClient(cfg)

	require.NotNil(t, client)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, httputil.DefaultMaxIdleConns, transport.MaxIdleConns)
	assert.Equal(t, httputil.DefaultMaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	assert.Equal(t, httputil.DefaultIdleConnTimeout, transport.IdleConnTimeout)
}

func TestNewClient_WithSkipTLSVerify(t *testing.T) {
	t.Parallel()

	cfg := &httputil.ClientConfig{
		Timeout:       30 * time.Second,
		SkipTLSVerify: true,
	}

	client := httputil.NewClient(cfg)

	require.NotNil(t, client)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.TLSClientConfig)
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
}

func TestNewClient_WithCustomTLSConfig(t *testing.T) {
	t.Parallel()

	customTLS := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: "example.com",
	}

	cfg := &httputil.ClientConfig{
		Timeout:   30 * time.Second,
		TLSConfig: customTLS,
	}

	client := httputil.NewClient(cfg)

	require.NotNil(t, client)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.TLSClientConfig)
	assert.Equal(t, uint16(tls.VersionTLS13), transport.TLSClientConfig.MinVersion)
	assert.Equal(t, "example.com", transport.TLSClientConfig.ServerName)
}

func TestNewClient_DoesNotMutateProvidedTLSConfig(t *testing.T) {
	t.Parallel()

	customTLS := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}
	originalInsecure := customTLS.InsecureSkipVerify

	cfg := &httputil.ClientConfig{
		Timeout:       30 * time.Second,
		TLSConfig:     customTLS,
		SkipTLSVerify: true,
	}

	client := httputil.NewClient(cfg)
	require.NotNil(t, client)

	// Original config should not be mutated
	assert.Equal(t, originalInsecure, customTLS.InsecureSkipVerify)

	// But the transport's config should have InsecureSkipVerify set
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
}

func TestNewClient_EnforcesMinTLSVersion(t *testing.T) {
	t.Parallel()

	cfg := &httputil.ClientConfig{
		Timeout: 30 * time.Second,
	}

	client := httputil.NewClient(cfg)

	require.NotNil(t, client)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.TLSClientConfig)
	assert.Equal(t, uint16(tls.VersionTLS12), transport.TLSClientConfig.MinVersion)
}

func TestNewClient_WithDisableKeepAlives(t *testing.T) {
	t.Parallel()

	cfg := &httputil.ClientConfig{
		Timeout:           30 * time.Second,
		DisableKeepAlives: true,
	}

	client := httputil.NewClient(cfg)

	require.NotNil(t, client)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.True(t, transport.DisableKeepAlives)
}

func TestNewClientWithTimeout(t *testing.T) {
	t.Parallel()

	client := httputil.NewClientWithTimeout(90 * time.Second)

	require.NotNil(t, client)
	assert.Equal(t, 90*time.Second, client.Timeout)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, httputil.DefaultMaxIdleConns, transport.MaxIdleConns)
	assert.Equal(t, httputil.DefaultMaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
}

func TestNewClientWithTLS(t *testing.T) {
	t.Parallel()

	customTLS := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: "secure.example.com",
	}

	client := httputil.NewClientWithTLS(45*time.Second, customTLS)

	require.NotNil(t, client)
	assert.Equal(t, 45*time.Second, client.Timeout)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.TLSClientConfig)
	assert.Equal(t, "secure.example.com", transport.TLSClientConfig.ServerName)
}

func TestDefault(t *testing.T) {
	t.Parallel()

	client := httputil.Default()

	require.NotNil(t, client)
	assert.Equal(t, 30*time.Second, client.Timeout)

	// Should return the same instance
	client2 := httputil.Default()
	assert.Same(t, client, client2)
}

func TestConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 100, httputil.DefaultMaxIdleConns)
	assert.Equal(t, 10, httputil.DefaultMaxIdleConnsPerHost)
	assert.Equal(t, 90*time.Second, httputil.DefaultIdleConnTimeout)
	assert.Equal(t, 30*time.Second, httputil.DefaultDialTimeout)
	assert.Equal(t, 30*time.Second, httputil.DefaultKeepAlive)
	assert.Equal(t, 10*time.Second, httputil.DefaultTLSHandshakeTimeout)
	assert.Equal(t, 1*time.Second, httputil.DefaultExpectContinue)
}
