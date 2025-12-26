package oidc

import (
	"errors"
	"net/url"
	"time"

	"github.com/piwi3910/nebulaio/internal/auth"
)

// Config holds OIDC provider configuration
type Config struct {
	// Base configuration
	auth.ProviderConfig `yaml:",inline"`

	// IssuerURL is the OIDC issuer URL (e.g., https://accounts.google.com)
	IssuerURL string `json:"issuerUrl" yaml:"issuerUrl"`

	// ClientID is the OAuth client ID
	ClientID string `json:"clientId" yaml:"clientId"`

	// ClientSecret is the OAuth client secret
	ClientSecret string `json:"clientSecret" yaml:"clientSecret"`

	// RedirectURL is the callback URL for OAuth flow
	RedirectURL string `json:"redirectUrl" yaml:"redirectUrl"`

	// Scopes to request (default: openid, profile, email)
	Scopes []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`

	// ClaimsMapping maps OIDC claims to user fields
	ClaimsMapping ClaimsMapping `json:"claimsMapping,omitempty" yaml:"claimsMapping,omitempty"`

	// TokenEndpointAuth is the authentication method for token endpoint
	// Options: client_secret_basic, client_secret_post, none
	TokenEndpointAuth string `json:"tokenEndpointAuth,omitempty" yaml:"tokenEndpointAuth,omitempty"`

	// PKCE enables Proof Key for Code Exchange
	PKCE bool `json:"pkce" yaml:"pkce"`

	// AllowedDomains restricts login to specific email domains
	AllowedDomains []string `json:"allowedDomains,omitempty" yaml:"allowedDomains,omitempty"`

	// AdminGroups are groups that get admin role
	AdminGroups []string `json:"adminGroups,omitempty" yaml:"adminGroups,omitempty"`

	// RequestTimeout for HTTP requests to the provider
	RequestTimeout time.Duration `json:"requestTimeout" yaml:"requestTimeout"`

	// SkipIssuerCheck disables issuer verification (not recommended)
	SkipIssuerCheck bool `json:"skipIssuerCheck" yaml:"skipIssuerCheck"`

	// InsecureSkipVerify disables TLS certificate verification
	InsecureSkipVerify bool `json:"insecureSkipVerify" yaml:"insecureSkipVerify"`
}

// ClaimsMapping maps OIDC claims to user fields
type ClaimsMapping struct {
	// Username is the claim for the username (default: preferred_username or sub)
	Username string `json:"username,omitempty" yaml:"username,omitempty"`

	// Email is the claim for email (default: email)
	Email string `json:"email,omitempty" yaml:"email,omitempty"`

	// DisplayName is the claim for display name (default: name)
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty"`

	// Groups is the claim for groups (default: groups)
	Groups string `json:"groups,omitempty" yaml:"groups,omitempty"`

	// Picture is the claim for avatar URL (default: picture)
	Picture string `json:"picture,omitempty" yaml:"picture,omitempty"`

	// Additional maps custom claims to user attributes
	Additional map[string]string `json:"additional,omitempty" yaml:"additional,omitempty"`
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		ProviderConfig: auth.ProviderConfig{
			Type:    "oidc",
			Enabled: false,
		},
		Scopes:            []string{"openid", "profile", "email", "groups"},
		TokenEndpointAuth: "client_secret_basic",
		PKCE:              true,
		RequestTimeout:    30 * time.Second,
		ClaimsMapping: ClaimsMapping{
			Username:    "preferred_username",
			Email:       "email",
			DisplayName: "name",
			Groups:      "groups",
			Picture:     "picture",
		},
	}
}

// Presets for common OIDC providers

// GoogleConfig returns defaults for Google
func GoogleConfig() Config {
	cfg := DefaultConfig()
	cfg.IssuerURL = "https://accounts.google.com"
	cfg.Scopes = []string{"openid", "profile", "email"}
	cfg.ClaimsMapping.Username = "email"
	return cfg
}

// KeycloakConfig returns defaults for Keycloak
func KeycloakConfig() Config {
	cfg := DefaultConfig()
	cfg.Scopes = []string{"openid", "profile", "email", "roles"}
	cfg.ClaimsMapping.Groups = "roles"
	return cfg
}

// OktaConfig returns defaults for Okta
func OktaConfig() Config {
	cfg := DefaultConfig()
	cfg.Scopes = []string{"openid", "profile", "email", "groups"}
	cfg.ClaimsMapping.Groups = "groups"
	return cfg
}

// AzureADConfig returns defaults for Azure AD
func AzureADConfig() Config {
	cfg := DefaultConfig()
	cfg.Scopes = []string{"openid", "profile", "email"}
	cfg.ClaimsMapping.Username = "preferred_username"
	cfg.ClaimsMapping.Groups = "groups"
	return cfg
}

// Auth0Config returns defaults for Auth0
func Auth0Config() Config {
	cfg := DefaultConfig()
	cfg.Scopes = []string{"openid", "profile", "email"}
	// Auth0 uses namespace for custom claims
	return cfg
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.IssuerURL == "" {
		return errors.New("issuer URL is required")
	}

	// Validate issuer URL format
	if _, err := url.Parse(c.IssuerURL); err != nil {
		return errors.New("invalid issuer URL: " + err.Error())
	}

	if c.ClientID == "" {
		return errors.New("client ID is required")
	}

	// Client secret is optional for public clients (with PKCE)
	if c.ClientSecret == "" && !c.PKCE {
		return errors.New("client secret is required when PKCE is disabled")
	}

	if c.RedirectURL == "" {
		return errors.New("redirect URL is required")
	}

	// Validate redirect URL format
	if _, err := url.Parse(c.RedirectURL); err != nil {
		return errors.New("invalid redirect URL: " + err.Error())
	}

	// Ensure at least openid scope
	hasOpenID := false
	for _, scope := range c.Scopes {
		if scope == "openid" {
			hasOpenID = true
			break
		}
	}
	if !hasOpenID {
		return errors.New("'openid' scope is required")
	}

	// Validate token endpoint auth method
	validAuthMethods := map[string]bool{
		"client_secret_basic": true,
		"client_secret_post":  true,
		"none":                true,
	}
	if c.TokenEndpointAuth != "" && !validAuthMethods[c.TokenEndpointAuth] {
		return errors.New("invalid token endpoint auth method: " + c.TokenEndpointAuth)
	}

	return nil
}

// AuthorizationState holds the state for an OAuth authorization request
type AuthorizationState struct {
	// State is the OAuth state parameter
	State string `json:"state"`
	// Nonce is used for ID token validation
	Nonce string `json:"nonce"`
	// CodeVerifier is the PKCE code verifier (if PKCE is enabled)
	CodeVerifier string `json:"codeVerifier,omitempty"`
	// RedirectURI is the redirect URI for this request
	RedirectURI string `json:"redirectUri"`
	// CreatedAt is when this state was created
	CreatedAt time.Time `json:"createdAt"`
	// ExpiresAt is when this state expires
	ExpiresAt time.Time `json:"expiresAt"`
}

// IsExpired checks if the authorization state has expired
func (s *AuthorizationState) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// StateStore is the interface for storing authorization states
type StateStore interface {
	// SaveState saves an authorization state
	SaveState(state *AuthorizationState) error
	// GetState retrieves and deletes an authorization state
	GetState(stateValue string) (*AuthorizationState, error)
	// CleanupExpired removes expired states
	CleanupExpired() error
}

// InMemoryStateStore is a simple in-memory state store
type InMemoryStateStore struct {
	states map[string]*AuthorizationState
}

// NewInMemoryStateStore creates a new in-memory state store
func NewInMemoryStateStore() *InMemoryStateStore {
	return &InMemoryStateStore{
		states: make(map[string]*AuthorizationState),
	}
}

// SaveState saves an authorization state
func (s *InMemoryStateStore) SaveState(state *AuthorizationState) error {
	s.states[state.State] = state
	return nil
}

// GetState retrieves and deletes an authorization state
func (s *InMemoryStateStore) GetState(stateValue string) (*AuthorizationState, error) {
	state, ok := s.states[stateValue]
	if !ok {
		return nil, errors.New("state not found")
	}
	delete(s.states, stateValue)
	return state, nil
}

// CleanupExpired removes expired states
func (s *InMemoryStateStore) CleanupExpired() error {
	now := time.Now()
	for key, state := range s.states {
		if now.After(state.ExpiresAt) {
			delete(s.states, key)
		}
	}
	return nil
}
