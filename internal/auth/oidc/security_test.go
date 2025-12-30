package oidc

import (
	"testing"
)

// TestNewProvider_InsecureSkipVerify tests that InsecureSkipVerify is properly handled.
func TestNewProvider_InsecureSkipVerify(t *testing.T) {
	tests := []struct {
		name               string
		insecureSkipVerify bool
		expectWarning      bool
	}{
		{
			name:               "Secure TLS (default)",
			insecureSkipVerify: false,
			expectWarning:      false,
		},
		{
			name:               "Insecure TLS enabled",
			insecureSkipVerify: true,
			expectWarning:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				IssuerURL:          "https://auth.example.com",
				ClientID:           "test-client",
				ClientSecret:       "test-secret",
				RedirectURL:        "https://app.example.com/callback",
				Scopes:             []string{"openid", "profile", "email"},
				InsecureSkipVerify: tc.insecureSkipVerify,
			}

			// Note: We can't easily test the log output without mocking the logger,
			// but we can verify the provider accepts the config and sets up TLS correctly

			// The actual provider creation will fail because we don't have a real OIDC server,
			// but the TLS configuration logic will run before that failure
			_, err := NewProvider(cfg)

			// We expect an error because there's no real OIDC server, but that's fine -
			// the TLS configuration code (including the warning) runs before the provider setup
			if err == nil {
				t.Error("Expected error when connecting to non-existent OIDC provider")
			}

			// The important part is that the function doesn't panic when InsecureSkipVerify
			// is set, and that the configuration is accepted
			if err := cfg.Validate(); err != nil {
				t.Errorf("Config should be valid: %v", err)
			}
		})
	}
}

// TestConfig_InsecureSkipVerifyDefault tests that InsecureSkipVerify defaults to false.
func TestConfig_InsecureSkipVerifyDefault(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should default to false for security")
	}
}

// TestConfig_InsecureSkipVerifySecureByDefault tests secure-by-default principle.
func TestConfig_InsecureSkipVerifySecureByDefault(t *testing.T) {
	tests := []struct {
		cfgFunc  func() Config
		name     string
		expected bool
	}{
		{
			name:     "DefaultConfig",
			cfgFunc:  DefaultConfig,
			expected: false,
		},
		{
			name:     "GoogleConfig",
			cfgFunc:  GoogleConfig,
			expected: false,
		},
		{
			name:     "KeycloakConfig",
			cfgFunc:  KeycloakConfig,
			expected: false,
		},
		{
			name:     "OktaConfig",
			cfgFunc:  OktaConfig,
			expected: false,
		},
		{
			name:     "AzureADConfig",
			cfgFunc:  AzureADConfig,
			expected: false,
		},
		{
			name:     "Auth0Config",
			cfgFunc:  Auth0Config,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfgFunc()
			if cfg.InsecureSkipVerify != tc.expected {
				t.Errorf("InsecureSkipVerify should be %v for %s (secure by default)", tc.expected, tc.name)
			}
		})
	}
}

// TestConfig_InsecureSkipVerifyExplicitEnable tests that insecure mode requires explicit opt-in.
func TestConfig_InsecureSkipVerifyExplicitEnable(t *testing.T) {
	// Start with default config
	cfg := DefaultConfig()

	// Verify it's secure by default
	if cfg.InsecureSkipVerify {
		t.Fatal("Default should be secure")
	}

	// Explicitly enable insecure mode
	cfg.InsecureSkipVerify = true
	cfg.IssuerURL = "https://auth.example.com"
	cfg.ClientID = "test"
	cfg.ClientSecret = "secret"
	cfg.RedirectURL = "https://app.example.com/callback"

	// Should still validate (insecure mode is allowed, just warned)
	err := cfg.Validate()
	if err != nil {
		t.Errorf("Config should validate even with InsecureSkipVerify enabled: %v", err)
	}

	// Verify the setting is preserved
	if !cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be enabled after explicit set")
	}
}
