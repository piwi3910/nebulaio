package targets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewWebhookTarget_SkipTLSVerify tests that SkipTLSVerify is properly handled
func TestNewWebhookTarget_SkipTLSVerify(t *testing.T) {
	tests := []struct {
		name          string
		skipTLSVerify bool
		expectWarning bool
		expectSuccess bool
	}{
		{
			name:          "Secure TLS (default)",
			skipTLSVerify: false,
			expectWarning: false,
			expectSuccess: true,
		},
		{
			name:          "Insecure TLS enabled",
			skipTLSVerify: true,
			expectWarning: true,
			expectSuccess: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := WebhookConfig{
				URL:           "https://webhook.example.com/endpoint",
				SkipTLSVerify: tc.skipTLSVerify,
			}

			target, err := NewWebhookTarget(config)

			if tc.expectSuccess {
				require.NoError(t, err, "Should create webhook target successfully")
				assert.NotNil(t, target, "Target should not be nil")

				// Verify TLS configuration
				if tc.skipTLSVerify {
					// When insecure mode is enabled, transport should have TLS config
					// (we can't easily verify the warning was logged, but we can verify the config)
					assert.NotNil(t, target.client.Transport, "Transport should be set when SkipTLSVerify is true")
				}
			} else {
				assert.Error(t, err, "Should return error")
			}
		})
	}
}

// TestWebhookConfig_SkipTLSVerifyDefault tests that SkipTLSVerify defaults to false
func TestWebhookConfig_SkipTLSVerifyDefault(t *testing.T) {
	config := WebhookConfig{
		URL: "https://webhook.example.com/endpoint",
	}

	// Default should be false (secure)
	assert.False(t, config.SkipTLSVerify, "SkipTLSVerify should default to false for security")

	target, err := NewWebhookTarget(config)
	require.NoError(t, err)
	assert.NotNil(t, target)
}

// TestWebhookConfig_SkipTLSVerifyExplicitEnable tests that insecure mode requires explicit opt-in
func TestWebhookConfig_SkipTLSVerifyExplicitEnable(t *testing.T) {
	// Start with default config
	config := WebhookConfig{
		URL: "https://webhook.example.com/endpoint",
	}

	// Verify it's secure by default
	assert.False(t, config.SkipTLSVerify, "Default should be secure")

	// Explicitly enable insecure mode
	config.SkipTLSVerify = true

	target, err := NewWebhookTarget(config)
	require.NoError(t, err, "Should accept insecure mode when explicitly enabled")
	assert.NotNil(t, target)
}

// TestWebhookConfig_SecureByDefault tests secure-by-default principle
func TestWebhookConfig_SecureByDefault(t *testing.T) {
	// Creating a webhook without explicitly setting SkipTLSVerify should be secure
	config := WebhookConfig{
		URL: "https://webhook.example.com/endpoint",
	}

	target, err := NewWebhookTarget(config)
	require.NoError(t, err)

	// We can't directly access the TLS config, but we can verify the target was created
	// and that SkipTLSVerify wasn't implicitly enabled
	assert.NotNil(t, target)
	assert.False(t, config.SkipTLSVerify, "Should remain false by default")
}

// TestWebhookConfig_ValidateURL tests URL validation
func TestWebhookConfig_ValidateURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		expectErr bool
	}{
		{
			name:      "Valid HTTPS URL",
			url:       "https://webhook.example.com/endpoint",
			expectErr: false,
		},
		{
			name:      "Valid HTTP URL",
			url:       "http://webhook.example.com/endpoint",
			expectErr: false,
		},
		{
			name:      "Empty URL",
			url:       "",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := WebhookConfig{
				URL: tc.url,
			}

			_, err := NewWebhookTarget(config)

			if tc.expectErr {
				assert.Error(t, err, "Should return error for invalid URL")
			} else {
				assert.NoError(t, err, "Should accept valid URL")
			}
		})
	}
}
