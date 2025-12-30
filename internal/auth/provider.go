package auth

import (
	"context"
	"time"
)

// Provider is the interface for external authentication providers.
type Provider interface {
	// Name returns the provider name
	Name() string

	// Authenticate validates credentials and returns user info
	Authenticate(ctx context.Context, username, password string) (*User, error)

	// GetUser retrieves user information by username
	GetUser(ctx context.Context, username string) (*User, error)

	// GetGroups retrieves groups for a user
	GetGroups(ctx context.Context, username string) ([]string, error)

	// ValidateToken validates a token and returns user info
	ValidateToken(ctx context.Context, token string) (*User, error)

	// Refresh refreshes an authentication token
	Refresh(ctx context.Context, refreshToken string) (*ExternalTokenPair, error)

	// Close closes any connections
	Close() error
}

// User represents an authenticated user.
type User struct {
	Attributes  map[string][]string `json:"attributes,omitempty"`
	LastLogin   *time.Time          `json:"lastLogin,omitempty"`
	Username    string              `json:"username"`
	DisplayName string              `json:"displayName,omitempty"`
	Email       string              `json:"email,omitempty"`
	Provider    string              `json:"provider"`
	ProviderID  string              `json:"providerId,omitempty"`
	Groups      []string            `json:"groups,omitempty"`
	Enabled     bool                `json:"enabled"`
}

// ExternalTokenPair holds access and refresh tokens from external providers.
type ExternalTokenPair struct {
	// AccessToken is the JWT access token
	AccessToken string `json:"accessToken"`
	// RefreshToken is used to refresh the access token
	RefreshToken string `json:"refreshToken,omitempty"`
	// TokenType is the token type (usually "Bearer")
	TokenType string `json:"tokenType"`
	// ExpiresIn is the token expiration time in seconds
	ExpiresIn int64 `json:"expiresIn"`
	// RefreshExpiresIn is the refresh token expiration time
	RefreshExpiresIn int64 `json:"refreshExpiresIn,omitempty"`
}

// GroupMapping maps external groups to internal policies.
type GroupMapping struct {
	// ExternalGroup is the group name from the external provider
	ExternalGroup string `json:"externalGroup"`
	// InternalPolicy is the NebulaIO policy to assign
	InternalPolicy string `json:"internalPolicy"`
}

// ProviderConfig is the base configuration for auth providers.
type ProviderConfig struct {
	Name          string         `json:"name" yaml:"name"`
	Type          string         `json:"type" yaml:"type"`
	DefaultPolicy string         `json:"defaultPolicy,omitempty" yaml:"defaultPolicy,omitempty"`
	GroupMappings []GroupMapping `json:"groupMappings,omitempty" yaml:"groupMappings,omitempty"`
	Priority      int            `json:"priority" yaml:"priority"`
	Enabled       bool           `json:"enabled" yaml:"enabled"`
}

// ErrAuthenticationFailed is returned when authentication fails.
type ErrAuthenticationFailed struct {
	Message string
}

func (e *ErrAuthenticationFailed) Error() string {
	return e.Message
}

// ErrUserNotFound is returned when a user is not found.
type ErrUserNotFound struct {
	Username string
}

func (e *ErrUserNotFound) Error() string {
	return "user not found: " + e.Username
}

// ErrProviderUnavailable is returned when the provider is unavailable.
type ErrProviderUnavailable struct {
	Cause    error
	Provider string
}

func (e *ErrProviderUnavailable) Error() string {
	if e.Cause != nil {
		return "provider unavailable: " + e.Provider + ": " + e.Cause.Error()
	}

	return "provider unavailable: " + e.Provider
}

func (e *ErrProviderUnavailable) Unwrap() error {
	return e.Cause
}

// ErrInvalidToken is returned when a token is invalid.
type ErrInvalidToken struct {
	Reason string
}

func (e *ErrInvalidToken) Error() string {
	return "invalid token: " + e.Reason
}

// ErrTokenExpired is returned when a token has expired.
type ErrTokenExpired struct{}

func (e *ErrTokenExpired) Error() string {
	return "token has expired"
}
