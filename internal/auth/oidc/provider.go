// Package oidc provides OpenID Connect (OIDC) authentication for NebulaIO.
//
// The package supports integration with OIDC identity providers such as:
//   - Keycloak
//   - Okta
//   - Auth0
//   - Azure AD
//   - Google Workspace
//
// Features include:
//   - Authorization code flow with PKCE
//   - Token validation and refresh
//   - Group claim mapping to IAM policies
//   - Session management
//
// Example usage:
//
//	config := oidc.Config{
//	    IssuerURL:    "https://auth.example.com",
//	    ClientID:     "nebulaio",
//	    ClientSecret: "secret",
//	    RedirectURL:  "https://storage.example.com/callback",
//	}
//	provider, err := oidc.NewProvider(ctx, config)
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
)

// Provider implements auth.Provider for OpenID Connect
type Provider struct {
	config       Config
	oidcProvider *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
	stateStore   StateStore
	httpClient   *http.Client
	mu           sync.RWMutex
	closed       bool
}

// NewProvider creates a new OIDC provider
func NewProvider(cfg Config) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Set defaults
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 30 * time.Second
	}

	// Create HTTP client
	httpClient := &http.Client{
		Timeout: cfg.RequestTimeout,
	}

	if cfg.InsecureSkipVerify {
		log.Warn().
			Str("issuer", cfg.IssuerURL).
			Msg("WARNING: TLS certificate verification disabled for OIDC provider - this should only be used in development/testing")
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	// Create OIDC provider
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, httpClient)
	oidcProvider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Create ID token verifier
	verifierConfig := &oidc.Config{
		ClientID:          cfg.ClientID,
		SkipIssuerCheck:   cfg.SkipIssuerCheck,
		SkipClientIDCheck: false,
	}
	verifier := oidcProvider.Verifier(verifierConfig)

	// Create OAuth2 config
	oauth2Config := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     oidcProvider.Endpoint(),
		RedirectURL:  cfg.RedirectURL,
		Scopes:       cfg.Scopes,
	}

	// Set auth style based on config
	switch cfg.TokenEndpointAuth {
	case "client_secret_post":
		oauth2Config.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	case "none":
		oauth2Config.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	default:
		oauth2Config.Endpoint.AuthStyle = oauth2.AuthStyleInHeader
	}

	provider := &Provider{
		config:       cfg,
		oidcProvider: oidcProvider,
		verifier:     verifier,
		oauth2Config: oauth2Config,
		stateStore:   NewInMemoryStateStore(),
		httpClient:   httpClient,
	}

	return provider, nil
}

// SetStateStore sets a custom state store
func (p *Provider) SetStateStore(store StateStore) {
	p.stateStore = store
}

// Name returns the provider name
func (p *Provider) Name() string {
	if p.config.Name != "" {
		return p.config.Name
	}
	return "oidc"
}

// Authenticate is not directly supported for OIDC (use OAuth flow instead)
func (p *Provider) Authenticate(_ context.Context, _, _ string) (*auth.User, error) {
	return nil, &auth.ErrAuthenticationFailed{
		Message: "OIDC requires OAuth2 flow, use GetAuthorizationURL and HandleCallback",
	}
}

// GetUser retrieves user information using an access token
func (p *Provider) GetUser(ctx context.Context, accessToken string) (*auth.User, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name()}
	}
	p.mu.RUnlock()

	// Use the userinfo endpoint
	userInfo, err := p.oidcProvider.UserInfo(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: accessToken,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Extract claims
	var claims map[string]interface{}
	if err := userInfo.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	return p.claimsToUser(claims), nil
}

// GetGroups retrieves groups for a user (from access token claims)
func (p *Provider) GetGroups(ctx context.Context, accessToken string) ([]string, error) {
	user, err := p.GetUser(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	return user.Groups, nil
}

// ValidateToken validates an ID token and returns user info
func (p *Provider) ValidateToken(ctx context.Context, idToken string) (*auth.User, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name()}
	}
	p.mu.RUnlock()

	// Verify the ID token
	token, err := p.verifier.Verify(ctx, idToken)
	if err != nil {
		return nil, &auth.ErrInvalidToken{Reason: err.Error()}
	}

	// Extract claims
	var claims map[string]interface{}
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	return p.claimsToUser(claims), nil
}

// Refresh refreshes tokens using a refresh token
func (p *Provider) Refresh(ctx context.Context, refreshToken string) (*auth.ExternalTokenPair, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name()}
	}
	p.mu.RUnlock()

	// Create a token source with the refresh token
	token := &oauth2.Token{
		RefreshToken: refreshToken,
	}

	tokenSource := p.oauth2Config.TokenSource(ctx, token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, &auth.ErrInvalidToken{Reason: err.Error()}
	}

	return &auth.ExternalTokenPair{
		AccessToken:  newToken.AccessToken,
		RefreshToken: newToken.RefreshToken,
		TokenType:    newToken.TokenType,
		ExpiresIn:    int64(time.Until(newToken.Expiry).Seconds()),
	}, nil
}

// Close closes the provider
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// GetAuthorizationURL generates an authorization URL for the OAuth flow
func (p *Provider) GetAuthorizationURL(redirectURI string) (string, *AuthorizationState, error) {
	state, err := generateRandomString(32)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate state: %w", err)
	}

	nonce, err := generateRandomString(32)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	authState := &AuthorizationState{
		State:       state,
		Nonce:       nonce,
		RedirectURI: redirectURI,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}

	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("nonce", nonce),
	}

	// Add PKCE if enabled
	if p.config.PKCE {
		verifier, err := generateRandomString(64)
		if err != nil {
			return "", nil, fmt.Errorf("failed to generate PKCE verifier: %w", err)
		}
		authState.CodeVerifier = verifier

		// Generate code challenge
		challenge := generateCodeChallenge(verifier)
		opts = append(opts,
			oauth2.SetAuthURLParam("code_challenge", challenge),
			oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		)
	}

	// Override redirect URI if provided
	config := p.oauth2Config
	if redirectURI != "" {
		config.RedirectURL = redirectURI
	}

	authURL := config.AuthCodeURL(state, opts...)

	// Save state for validation
	if err := p.stateStore.SaveState(authState); err != nil {
		return "", nil, fmt.Errorf("failed to save state: %w", err)
	}

	return authURL, authState, nil
}

// HandleCallback handles the OAuth callback and returns tokens
func (p *Provider) HandleCallback(ctx context.Context, code, stateValue string) (*TokenResponse, error) {
	// Retrieve and validate state
	authState, err := p.stateStore.GetState(stateValue)
	if err != nil {
		return nil, &auth.ErrInvalidToken{Reason: "invalid state"}
	}

	if authState.IsExpired() {
		return nil, &auth.ErrTokenExpired{}
	}

	// Prepare exchange options
	opts := []oauth2.AuthCodeOption{}

	// Add PKCE verifier if enabled
	if p.config.PKCE && authState.CodeVerifier != "" {
		opts = append(opts, oauth2.SetAuthURLParam("code_verifier", authState.CodeVerifier))
	}

	// Override redirect URI if it was customized
	config := p.oauth2Config
	if authState.RedirectURI != "" {
		config.RedirectURL = authState.RedirectURI
	}

	// Exchange code for tokens
	token, err := config.Exchange(ctx, code, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Extract ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in response")
	}

	// Verify ID token
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, &auth.ErrInvalidToken{Reason: err.Error()}
	}

	// Verify nonce
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	if nonce, ok := claims["nonce"].(string); !ok || nonce != authState.Nonce {
		return nil, &auth.ErrInvalidToken{Reason: "nonce mismatch"}
	}

	// Extract user from claims
	user := p.claimsToUser(claims)

	// Check allowed domains
	if len(p.config.AllowedDomains) > 0 {
		if !p.isEmailDomainAllowed(user.Email) {
			return nil, &auth.ErrAuthenticationFailed{
				Message: "email domain not allowed",
			}
		}
	}

	return &TokenResponse{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		IDToken:      rawIDToken,
		TokenType:    token.TokenType,
		ExpiresIn:    int64(time.Until(token.Expiry).Seconds()),
		User:         user,
	}, nil
}

// TokenResponse represents the response from token exchange
type TokenResponse struct {
	AccessToken  string     `json:"accessToken"`
	RefreshToken string     `json:"refreshToken,omitempty"`
	IDToken      string     `json:"idToken"`
	TokenType    string     `json:"tokenType"`
	ExpiresIn    int64      `json:"expiresIn"`
	User         *auth.User `json:"user"`
}

// claimsToUser converts OIDC claims to an auth.User
func (p *Provider) claimsToUser(claims map[string]interface{}) *auth.User {
	mapping := p.config.ClaimsMapping
	user := &auth.User{
		Provider:   p.Name(),
		Enabled:    true,
		Attributes: make(map[string][]string),
	}

	// Get username
	if mapping.Username != "" {
		if val, ok := claims[mapping.Username].(string); ok {
			user.Username = val
		}
	}
	// Fallback to sub if no username
	if user.Username == "" {
		if sub, ok := claims["sub"].(string); ok {
			user.Username = sub
		}
	}

	// Get email
	if mapping.Email != "" {
		if val, ok := claims[mapping.Email].(string); ok {
			user.Email = val
		}
	}

	// Get display name
	if mapping.DisplayName != "" {
		if val, ok := claims[mapping.DisplayName].(string); ok {
			user.DisplayName = val
		}
	}

	// Get groups
	if mapping.Groups != "" {
		user.Groups = extractGroups(claims, mapping.Groups)
	}

	// Get provider ID (sub claim)
	if sub, ok := claims["sub"].(string); ok {
		user.ProviderID = sub
	}

	// Get additional attributes
	for key, claimName := range mapping.Additional {
		if val, ok := claims[claimName]; ok {
			switch v := val.(type) {
			case string:
				user.Attributes[key] = []string{v}
			case []interface{}:
				var strs []string
				for _, item := range v {
					if s, ok := item.(string); ok {
						strs = append(strs, s)
					}
				}
				user.Attributes[key] = strs
			}
		}
	}

	return user
}

// extractGroups extracts groups from claims
func extractGroups(claims map[string]interface{}, groupsClaim string) []string {
	var groups []string

	val, ok := claims[groupsClaim]
	if !ok {
		return groups
	}

	switch v := val.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				groups = append(groups, s)
			}
		}
	case []string:
		groups = v
	case string:
		// Single group as string
		groups = []string{v}
	}

	return groups
}

// isEmailDomainAllowed checks if the email domain is in the allowed list
func (p *Provider) isEmailDomainAllowed(email string) bool {
	if len(p.config.AllowedDomains) == 0 {
		return true
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}

	domain := strings.ToLower(parts[1])
	for _, allowed := range p.config.AllowedDomains {
		if strings.ToLower(allowed) == domain {
			return true
		}
	}

	return false
}

// generateRandomString generates a random string of specified length
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes)[:length], nil
}

// generateCodeChallenge generates a PKCE code challenge
func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// IsAdminGroup checks if any of the user's groups are admin groups
func (p *Provider) IsAdminGroup(groups []string) bool {
	if len(p.config.AdminGroups) == 0 {
		return false
	}

	for _, userGroup := range groups {
		for _, adminGroup := range p.config.AdminGroups {
			if userGroup == adminGroup {
				return true
			}
		}
	}
	return false
}
