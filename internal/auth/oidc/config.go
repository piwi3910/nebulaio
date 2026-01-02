package oidc

import (
	"errors"
	"net/url"
	"slices"
	"time"

	"github.com/piwi3910/nebulaio/internal/auth"
)

// OIDC claim and scope constants.
const (
	claimGroups = "groups"
	scopeOpenID = "openid"
)

// Config holds OIDC provider configuration.
type Config struct {
	// 8-byte fields (slices, time.Duration = int64)
	Scopes         []string      `json:"scopes,omitempty"         yaml:"scopes,omitempty"`
	AllowedDomains []string      `json:"allowedDomains,omitempty" yaml:"allowedDomains,omitempty"`
	AdminGroups    []string      `json:"adminGroups,omitempty"    yaml:"adminGroups,omitempty"`
	RequestTimeout time.Duration `json:"requestTimeout"           yaml:"requestTimeout"`
	// Structs
	ClaimsMapping       ClaimsMapping    `json:"claimsMapping" yaml:"claimsMapping"`
	auth.ProviderConfig `yaml:",inline"` //nolint:embeddedstructfieldcheck // Grouped by size for memory layout

	// Strings
	ClientSecret      string `json:"clientSecret"                yaml:"clientSecret"`
	ClientID          string `json:"clientId"                    yaml:"clientId"`
	RedirectURL       string `json:"redirectUrl"                 yaml:"redirectUrl"`
	IssuerURL         string `json:"issuerUrl"                   yaml:"issuerUrl"`
	TokenEndpointAuth string `json:"tokenEndpointAuth,omitempty" yaml:"tokenEndpointAuth,omitempty"`
	// 1-byte fields (bool)
	PKCE               bool `json:"pkce"               yaml:"pkce"`
	SkipIssuerCheck    bool `json:"skipIssuerCheck"    yaml:"skipIssuerCheck"`
	InsecureSkipVerify bool `json:"insecureSkipVerify" yaml:"insecureSkipVerify"`
}

// ClaimsMapping maps OIDC claims to user fields.
type ClaimsMapping struct {
	Additional  map[string]string `json:"additional,omitempty" yaml:"additional,omitempty"`
	Username    string            `json:"username,omitempty" yaml:"username,omitempty"`
	Email       string            `json:"email,omitempty" yaml:"email,omitempty"`
	DisplayName string            `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Groups      string            `json:"groups,omitempty" yaml:"groups,omitempty"`
	Picture     string            `json:"picture,omitempty" yaml:"picture,omitempty"`
}

// DefaultConfig returns sensible defaults.
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

// GoogleConfig returns defaults for Google.
func GoogleConfig() Config {
	cfg := DefaultConfig()
	cfg.IssuerURL = "https://accounts.google.com"
	cfg.Scopes = []string{"openid", "profile", "email"}
	cfg.ClaimsMapping.Username = "email"

	return cfg
}

// KeycloakConfig returns defaults for Keycloak.
func KeycloakConfig() Config {
	cfg := DefaultConfig()
	cfg.Scopes = []string{"openid", "profile", "email", "roles"}
	cfg.ClaimsMapping.Groups = "roles"

	return cfg
}

// OktaConfig returns defaults for Okta.
func OktaConfig() Config {
	cfg := DefaultConfig()
	cfg.Scopes = []string{"openid", "profile", "email", "groups"}
	cfg.ClaimsMapping.Groups = claimGroups

	return cfg
}

// AzureADConfig returns defaults for Azure AD.
func AzureADConfig() Config {
	cfg := DefaultConfig()
	cfg.Scopes = []string{"openid", "profile", "email"}
	cfg.ClaimsMapping.Username = "preferred_username"
	cfg.ClaimsMapping.Groups = claimGroups

	return cfg
}

// Auth0Config returns defaults for Auth0.
func Auth0Config() Config {
	cfg := DefaultConfig()
	cfg.Scopes = []string{"openid", "profile", "email"}
	// Auth0 uses namespace for custom claims
	return cfg
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.IssuerURL == "" {
		return errors.New("issuer URL is required")
	}

	// Validate issuer URL format
	_, issuerErr := url.Parse(c.IssuerURL)
	if issuerErr != nil {
		return errors.New("invalid issuer URL: " + issuerErr.Error())
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
	_, redirectErr := url.Parse(c.RedirectURL)
	if redirectErr != nil {
		return errors.New("invalid redirect URL: " + redirectErr.Error())
	}

	// Ensure at least openid scope
	if !slices.Contains(c.Scopes, scopeOpenID) {
		return errors.New("'" + scopeOpenID + "' scope is required")
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

// AuthorizationState holds the state for an OAuth authorization request.
type AuthorizationState struct {
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	State        string    `json:"state"`
	Nonce        string    `json:"nonce"`
	CodeVerifier string    `json:"codeVerifier,omitempty"`
	RedirectURI  string    `json:"redirectUri"`
}

// IsExpired checks if the authorization state has expired.
func (s *AuthorizationState) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// StateStore is the interface for storing authorization states.
type StateStore interface {
	// SaveState saves an authorization state
	SaveState(state *AuthorizationState) error
	// GetState retrieves and deletes an authorization state
	GetState(stateValue string) (*AuthorizationState, error)
	// CleanupExpired removes expired states
	CleanupExpired() error
}

// InMemoryStateStore is a simple in-memory state store.
type InMemoryStateStore struct {
	states map[string]*AuthorizationState
}

// NewInMemoryStateStore creates a new in-memory state store.
func NewInMemoryStateStore() *InMemoryStateStore {
	return &InMemoryStateStore{
		states: make(map[string]*AuthorizationState),
	}
}

// SaveState saves an authorization state.
func (s *InMemoryStateStore) SaveState(state *AuthorizationState) error {
	s.states[state.State] = state
	return nil
}

// GetState retrieves and deletes an authorization state.
func (s *InMemoryStateStore) GetState(stateValue string) (*AuthorizationState, error) {
	state, ok := s.states[stateValue]
	if !ok {
		return nil, errors.New("state not found")
	}

	delete(s.states, stateValue)

	return state, nil
}

// CleanupExpired removes expired states.
func (s *InMemoryStateStore) CleanupExpired() error {
	now := time.Now()
	for key, state := range s.states {
		if now.After(state.ExpiresAt) {
			delete(s.states, key)
		}
	}

	return nil
}
