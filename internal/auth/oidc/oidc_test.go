package oidc

import (
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/auth"
)

func TestConfigValidation(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		cfg := Config{
			IssuerURL:    "https://auth.example.com",
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			RedirectURL:  "https://app.example.com/callback",
			Scopes:       []string{"openid", "profile", "email"},
		}

		err := cfg.Validate()
		if err != nil {
			t.Errorf("valid config should not error: %v", err)
		}
	})

	t.Run("MissingIssuerURL", func(t *testing.T) {
		cfg := Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			RedirectURL:  "https://app.example.com/callback",
			Scopes:       []string{"openid"},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing issuer URL")
		}
	})

	t.Run("MissingClientID", func(t *testing.T) {
		cfg := Config{
			IssuerURL:    "https://auth.example.com",
			ClientSecret: "test-secret",
			RedirectURL:  "https://app.example.com/callback",
			Scopes:       []string{"openid"},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing client ID")
		}
	})

	t.Run("MissingClientSecretWithoutPKCE", func(t *testing.T) {
		cfg := Config{
			IssuerURL:   "https://auth.example.com",
			ClientID:    "test-client",
			RedirectURL: "https://app.example.com/callback",
			Scopes:      []string{"openid"},
			PKCE:        false,
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing client secret without PKCE")
		}
	})

	t.Run("NoClientSecretWithPKCE", func(t *testing.T) {
		cfg := Config{
			IssuerURL:   "https://auth.example.com",
			ClientID:    "test-client",
			RedirectURL: "https://app.example.com/callback",
			Scopes:      []string{"openid"},
			PKCE:        true,
		}

		err := cfg.Validate()
		if err != nil {
			t.Errorf("PKCE client without secret should be valid: %v", err)
		}
	})

	t.Run("MissingRedirectURL", func(t *testing.T) {
		cfg := Config{
			IssuerURL:    "https://auth.example.com",
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			Scopes:       []string{"openid"},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing redirect URL")
		}
	})

	t.Run("MissingOpenIDScope", func(t *testing.T) {
		cfg := Config{
			IssuerURL:    "https://auth.example.com",
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			RedirectURL:  "https://app.example.com/callback",
			Scopes:       []string{"profile", "email"}, // Missing openid
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing openid scope")
		}
	})

	t.Run("InvalidTokenEndpointAuth", func(t *testing.T) {
		cfg := Config{
			IssuerURL:         "https://auth.example.com",
			ClientID:          "test-client",
			ClientSecret:      "test-secret",
			RedirectURL:       "https://app.example.com/callback",
			Scopes:            []string{"openid"},
			TokenEndpointAuth: "invalid_method",
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for invalid token endpoint auth")
		}
	})

	t.Run("ValidTokenEndpointAuth", func(t *testing.T) {
		validMethods := []string{"client_secret_basic", "client_secret_post", "none"}
		for _, method := range validMethods {
			cfg := Config{
				IssuerURL:         "https://auth.example.com",
				ClientID:          "test-client",
				ClientSecret:      "test-secret",
				RedirectURL:       "https://app.example.com/callback",
				Scopes:            []string{"openid"},
				TokenEndpointAuth: method,
			}

			err := cfg.Validate()
			if err != nil {
				t.Errorf("valid token endpoint auth %s should not error: %v", method, err)
			}
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Type != "oidc" {
		t.Error("default config should have type 'oidc'")
	}

	hasOpenID := false

	for _, scope := range cfg.Scopes {
		if scope == "openid" {
			hasOpenID = true
			break
		}
	}

	if !hasOpenID {
		t.Error("default config should have openid scope")
	}

	if cfg.TokenEndpointAuth != "client_secret_basic" {
		t.Error("default token endpoint auth should be client_secret_basic")
	}

	if !cfg.PKCE {
		t.Error("default config should have PKCE enabled")
	}
}

func TestProviderPresets(t *testing.T) {
	t.Run("GoogleConfig", func(t *testing.T) {
		cfg := GoogleConfig()
		if cfg.IssuerURL != "https://accounts.google.com" {
			t.Error("Google config should have correct issuer URL")
		}

		if cfg.ClaimsMapping.Username != "email" {
			t.Error("Google config should use email as username")
		}
	})

	t.Run("KeycloakConfig", func(t *testing.T) {
		cfg := KeycloakConfig()
		if cfg.ClaimsMapping.Groups != "roles" {
			t.Error("Keycloak config should use roles claim for groups")
		}
	})

	t.Run("OktaConfig", func(t *testing.T) {
		cfg := OktaConfig()
		if cfg.ClaimsMapping.Groups != "groups" {
			t.Error("Okta config should use groups claim")
		}
	})

	t.Run("AzureADConfig", func(t *testing.T) {
		cfg := AzureADConfig()
		if cfg.ClaimsMapping.Username != "preferred_username" {
			t.Error("Azure AD config should use preferred_username")
		}
	})

	t.Run("Auth0Config", func(t *testing.T) {
		cfg := Auth0Config()
		hasOpenID := false

		for _, scope := range cfg.Scopes {
			if scope == "openid" {
				hasOpenID = true
				break
			}
		}

		if !hasOpenID {
			t.Error("Auth0 config should have openid scope")
		}
	})
}

func TestAuthorizationState(t *testing.T) {
	t.Run("NotExpired", func(t *testing.T) {
		state := AuthorizationState{
			State:     "test-state",
			Nonce:     "test-nonce",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(10 * time.Minute),
		}

		if state.IsExpired() {
			t.Error("state should not be expired")
		}
	})

	t.Run("Expired", func(t *testing.T) {
		state := AuthorizationState{
			State:     "test-state",
			Nonce:     "test-nonce",
			CreatedAt: time.Now().Add(-20 * time.Minute),
			ExpiresAt: time.Now().Add(-10 * time.Minute),
		}

		if !state.IsExpired() {
			t.Error("state should be expired")
		}
	})
}

func TestInMemoryStateStore(t *testing.T) {
	store := NewInMemoryStateStore()

	t.Run("SaveAndGet", func(t *testing.T) {
		state := &AuthorizationState{
			State:     "test-state",
			Nonce:     "test-nonce",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(10 * time.Minute),
		}

		err := store.SaveState(state)
		if err != nil {
			t.Fatalf("save state failed: %v", err)
		}

		retrieved, err := store.GetState("test-state")
		if err != nil {
			t.Fatalf("get state failed: %v", err)
		}

		if retrieved.State != "test-state" {
			t.Error("state mismatch")
		}

		// State should be deleted after get
		_, err = store.GetState("test-state")
		if err == nil {
			t.Error("expected error for deleted state")
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		_, err := store.GetState("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent state")
		}
	})

	t.Run("CleanupExpired", func(t *testing.T) {
		// Add expired state
		expiredState := &AuthorizationState{
			State:     "expired-state",
			Nonce:     "nonce",
			CreatedAt: time.Now().Add(-20 * time.Minute),
			ExpiresAt: time.Now().Add(-10 * time.Minute),
		}
		_ = store.SaveState(expiredState)

		// Add valid state
		validState := &AuthorizationState{
			State:     "valid-state",
			Nonce:     "nonce",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(10 * time.Minute),
		}
		_ = store.SaveState(validState)

		// Cleanup
		err := store.CleanupExpired()
		if err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}

		// Expired should be gone
		_, err = store.GetState("expired-state")
		if err == nil {
			t.Error("expired state should be cleaned up")
		}

		// Valid should still exist
		_, err = store.GetState("valid-state")
		if err != nil {
			t.Error("valid state should still exist")
		}
	})
}

func TestClaimsToUser(t *testing.T) {
	p := &Provider{
		config: Config{
			ClaimsMapping: ClaimsMapping{
				Username:    "preferred_username",
				Email:       "email",
				DisplayName: "name",
				Groups:      "groups",
				Additional: map[string]string{
					"department": "department",
				},
			},
		},
	}

	t.Run("FullClaims", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub":                "user-123",
			"preferred_username": "testuser",
			"email":              "test@example.com",
			"name":               "Test User",
			"groups":             []interface{}{"users", "admins"},
			"department":         "Engineering",
		}

		user := p.claimsToUser(claims)

		if user.Username != "testuser" {
			t.Errorf("expected username 'testuser', got %s", user.Username)
		}

		if user.Email != "test@example.com" {
			t.Errorf("expected email 'test@example.com', got %s", user.Email)
		}

		if user.DisplayName != "Test User" {
			t.Errorf("expected display name 'Test User', got %s", user.DisplayName)
		}

		if len(user.Groups) != 2 {
			t.Errorf("expected 2 groups, got %d", len(user.Groups))
		}

		if user.ProviderID != "user-123" {
			t.Error("provider ID should be set from sub claim")
		}

		if user.Attributes["department"][0] != "Engineering" {
			t.Error("additional attribute should be set")
		}
	})

	t.Run("FallbackToSub", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub": "user-123",
		}

		user := p.claimsToUser(claims)

		if user.Username != "user-123" {
			t.Error("should fallback to sub for username")
		}
	})

	t.Run("GroupsAsStrings", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub":    "user-123",
			"groups": []string{"group1", "group2"},
		}

		user := p.claimsToUser(claims)

		if len(user.Groups) != 2 {
			t.Errorf("expected 2 groups, got %d", len(user.Groups))
		}
	})

	t.Run("SingleGroupString", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub":    "user-123",
			"groups": "single-group",
		}

		user := p.claimsToUser(claims)

		if len(user.Groups) != 1 || user.Groups[0] != "single-group" {
			t.Error("should handle single group as string")
		}
	})
}

func TestExtractGroups(t *testing.T) {
	t.Run("ArrayOfInterfaces", func(t *testing.T) {
		claims := map[string]interface{}{
			"groups": []interface{}{"group1", "group2", "group3"},
		}

		groups := extractGroups(claims, "groups")
		if len(groups) != 3 {
			t.Errorf("expected 3 groups, got %d", len(groups))
		}
	})

	t.Run("ArrayOfStrings", func(t *testing.T) {
		claims := map[string]interface{}{
			"groups": []string{"group1", "group2"},
		}

		groups := extractGroups(claims, "groups")
		if len(groups) != 2 {
			t.Errorf("expected 2 groups, got %d", len(groups))
		}
	})

	t.Run("SingleString", func(t *testing.T) {
		claims := map[string]interface{}{
			"groups": "single",
		}

		groups := extractGroups(claims, "groups")
		if len(groups) != 1 || groups[0] != "single" {
			t.Error("should handle single string group")
		}
	})

	t.Run("MissingClaim", func(t *testing.T) {
		claims := map[string]interface{}{}

		groups := extractGroups(claims, "groups")
		if len(groups) != 0 {
			t.Error("should return empty for missing claim")
		}
	})
}

func TestIsEmailDomainAllowed(t *testing.T) {
	t.Run("NoRestrictions", func(t *testing.T) {
		p := &Provider{
			config: Config{
				AllowedDomains: []string{},
			},
		}

		if !p.isEmailDomainAllowed("test@any.com") {
			t.Error("should allow any domain when no restrictions")
		}
	})

	t.Run("AllowedDomain", func(t *testing.T) {
		p := &Provider{
			config: Config{
				AllowedDomains: []string{"example.com", "test.com"},
			},
		}

		if !p.isEmailDomainAllowed("user@example.com") {
			t.Error("should allow example.com")
		}

		if !p.isEmailDomainAllowed("user@test.com") {
			t.Error("should allow test.com")
		}
	})

	t.Run("NotAllowedDomain", func(t *testing.T) {
		p := &Provider{
			config: Config{
				AllowedDomains: []string{"example.com"},
			},
		}

		if p.isEmailDomainAllowed("user@other.com") {
			t.Error("should not allow other.com")
		}
	})

	t.Run("CaseInsensitive", func(t *testing.T) {
		p := &Provider{
			config: Config{
				AllowedDomains: []string{"Example.COM"},
			},
		}

		if !p.isEmailDomainAllowed("user@example.com") {
			t.Error("should be case insensitive")
		}
	})

	t.Run("InvalidEmail", func(t *testing.T) {
		p := &Provider{
			config: Config{
				AllowedDomains: []string{"example.com"},
			},
		}

		if p.isEmailDomainAllowed("invalid-email") {
			t.Error("should reject invalid email")
		}
	})
}

func TestIsAdminGroup(t *testing.T) {
	t.Run("NoAdminGroups", func(t *testing.T) {
		p := &Provider{
			config: Config{
				AdminGroups: []string{},
			},
		}

		if p.IsAdminGroup([]string{"admins"}) {
			t.Error("should return false when no admin groups configured")
		}
	})

	t.Run("IsAdmin", func(t *testing.T) {
		p := &Provider{
			config: Config{
				AdminGroups: []string{"admins", "superusers"},
			},
		}

		if !p.IsAdminGroup([]string{"users", "admins"}) {
			t.Error("should detect admin group")
		}
	})

	t.Run("NotAdmin", func(t *testing.T) {
		p := &Provider{
			config: Config{
				AdminGroups: []string{"admins"},
			},
		}

		if p.IsAdminGroup([]string{"users", "readers"}) {
			t.Error("should not detect admin when not in admin groups")
		}
	})
}

func TestGenerateRandomString(t *testing.T) {
	t.Run("GeneratesCorrectLength", func(t *testing.T) {
		str, err := generateRandomString(32)
		if err != nil {
			t.Fatalf("failed to generate: %v", err)
		}

		if len(str) != 32 {
			t.Errorf("expected length 32, got %d", len(str))
		}
	})

	t.Run("GeneratesUnique", func(t *testing.T) {
		str1, _ := generateRandomString(32)
		str2, _ := generateRandomString(32)

		if str1 == str2 {
			t.Error("should generate unique strings")
		}
	})
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "test-verifier-123456789012345678901234567890"
	challenge := generateCodeChallenge(verifier)

	if challenge == "" {
		t.Error("challenge should not be empty")
	}

	if challenge == verifier {
		t.Error("challenge should not equal verifier")
	}

	// Same verifier should produce same challenge
	challenge2 := generateCodeChallenge(verifier)
	if challenge != challenge2 {
		t.Error("same verifier should produce same challenge")
	}
}

func TestProviderInterface(t *testing.T) {
	// Ensure Provider implements auth.Provider interface
	var _ auth.Provider = (*Provider)(nil)
}

func TestTokenResponse(t *testing.T) {
	resp := TokenResponse{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		IDToken:      "id-token",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		User: &auth.User{
			Username: "testuser",
		},
	}

	if resp.AccessToken != "access-token" {
		t.Error("access token mismatch")
	}

	if resp.User.Username != "testuser" {
		t.Error("user should be set")
	}
}
