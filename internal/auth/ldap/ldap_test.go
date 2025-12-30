package ldap

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/auth"
)

func TestConfigValidation(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		cfg := Config{
			ServerURL:    "ldap://localhost:389",
			BindDN:       "cn=admin,dc=example,dc=com",
			BindPassword: "admin",
			UserSearch: UserSearchConfig{
				BaseDN: "ou=users,dc=example,dc=com",
				Filter: "(uid={username})",
				Scope:  "sub",
				Attributes: AttributeMapping{
					Username: "uid",
				},
			},
		}

		err := cfg.Validate()
		if err != nil {
			t.Errorf("valid config should not error: %v", err)
		}
	})

	t.Run("MissingServerURL", func(t *testing.T) {
		cfg := Config{
			BindDN:       "cn=admin,dc=example,dc=com",
			BindPassword: "admin",
			UserSearch: UserSearchConfig{
				BaseDN: "ou=users,dc=example,dc=com",
				Filter: "(uid={username})",
				Attributes: AttributeMapping{
					Username: "uid",
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing server URL")
		}
	})

	t.Run("MissingBindDN", func(t *testing.T) {
		cfg := Config{
			ServerURL:    "ldap://localhost:389",
			BindPassword: "admin",
			UserSearch: UserSearchConfig{
				BaseDN: "ou=users,dc=example,dc=com",
				Filter: "(uid={username})",
				Attributes: AttributeMapping{
					Username: "uid",
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing bind DN")
		}
	})

	t.Run("MissingBindPassword", func(t *testing.T) {
		cfg := Config{
			ServerURL: "ldap://localhost:389",
			BindDN:    "cn=admin,dc=example,dc=com",
			UserSearch: UserSearchConfig{
				BaseDN: "ou=users,dc=example,dc=com",
				Filter: "(uid={username})",
				Attributes: AttributeMapping{
					Username: "uid",
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing bind password")
		}
	})

	t.Run("MissingUserSearchBaseDN", func(t *testing.T) {
		cfg := Config{
			ServerURL:    "ldap://localhost:389",
			BindDN:       "cn=admin,dc=example,dc=com",
			BindPassword: "admin",
			UserSearch: UserSearchConfig{
				Filter: "(uid={username})",
				Attributes: AttributeMapping{
					Username: "uid",
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing user search base DN")
		}
	})

	t.Run("MissingUserSearchFilter", func(t *testing.T) {
		cfg := Config{
			ServerURL:    "ldap://localhost:389",
			BindDN:       "cn=admin,dc=example,dc=com",
			BindPassword: "admin",
			UserSearch: UserSearchConfig{
				BaseDN: "ou=users,dc=example,dc=com",
				Attributes: AttributeMapping{
					Username: "uid",
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing user search filter")
		}
	})

	t.Run("MissingUsernameAttribute", func(t *testing.T) {
		cfg := Config{
			ServerURL:    "ldap://localhost:389",
			BindDN:       "cn=admin,dc=example,dc=com",
			BindPassword: "admin",
			UserSearch: UserSearchConfig{
				BaseDN:     "ou=users,dc=example,dc=com",
				Filter:     "(uid={username})",
				Attributes: AttributeMapping{},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing username attribute")
		}
	})

	t.Run("InvalidScope", func(t *testing.T) {
		cfg := Config{
			ServerURL:    "ldap://localhost:389",
			BindDN:       "cn=admin,dc=example,dc=com",
			BindPassword: "admin",
			UserSearch: UserSearchConfig{
				BaseDN: "ou=users,dc=example,dc=com",
				Filter: "(uid={username})",
				Scope:  "invalid",
				Attributes: AttributeMapping{
					Username: "uid",
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for invalid scope")
		}
	})

	t.Run("GroupSearchMissingFilter", func(t *testing.T) {
		cfg := Config{
			ServerURL:    "ldap://localhost:389",
			BindDN:       "cn=admin,dc=example,dc=com",
			BindPassword: "admin",
			UserSearch: UserSearchConfig{
				BaseDN: "ou=users,dc=example,dc=com",
				Filter: "(uid={username})",
				Attributes: AttributeMapping{
					Username: "uid",
				},
			},
			GroupSearch: GroupSearchConfig{
				BaseDN:        "ou=groups,dc=example,dc=com",
				NameAttribute: "cn",
				// Missing Filter
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing group search filter")
		}
	})
}

func TestSearchScope(t *testing.T) {
	tests := []struct {
		scope    string
		expected int
	}{
		{"base", 0},
		{"one", 1},
		{"sub", 2},
		{"", 2},        // Default
		{"invalid", 2}, // Default for invalid
	}

	for _, tc := range tests {
		t.Run(tc.scope, func(t *testing.T) {
			result := SearchScope(tc.scope)
			if result != tc.expected {
				t.Errorf("expected %d for scope %q, got %d", tc.expected, tc.scope, result)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.UserSearch.Filter == "" {
		t.Error("default config should have user search filter")
	}

	if cfg.UserSearch.Attributes.Username == "" {
		t.Error("default config should have username attribute")
	}

	if cfg.Pool.MaxConnections <= 0 {
		t.Error("default config should have positive max connections")
	}
}

func TestActiveDirectoryConfig(t *testing.T) {
	cfg := ActiveDirectoryConfig()

	if cfg.UserSearch.Attributes.Username != "sAMAccountName" {
		t.Error("AD config should use sAMAccountName for username")
	}

	if cfg.UserSearch.Attributes.UniqueID != "objectGUID" {
		t.Error("AD config should use objectGUID for unique ID")
	}
}

func TestExtractCNFromDN(t *testing.T) {
	tests := []struct {
		dn       string
		expected string
	}{
		{"CN=Admins,OU=Groups,DC=example,DC=com", "Admins"},
		{"cn=users,dc=example,dc=com", "users"},
		{"CN=Test Group,OU=Security,DC=corp,DC=local", "Test Group"},
		{"ou=users,dc=example,dc=com", ""}, // No CN
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.dn, func(t *testing.T) {
			result := extractCNFromDN(tc.dn)
			if result != tc.expected {
				t.Errorf("expected %q for DN %q, got %q", tc.expected, tc.dn, result)
			}
		})
	}
}

func TestAuthErrors(t *testing.T) {
	t.Run("ErrAuthenticationFailed", func(t *testing.T) {
		err := &auth.ErrAuthenticationFailed{Message: "invalid credentials"}
		if err.Error() != "invalid credentials" {
			t.Error("error message mismatch")
		}
	})

	t.Run("ErrUserNotFound", func(t *testing.T) {
		err := &auth.ErrUserNotFound{Username: "testuser"}
		if err.Error() != "user not found: testuser" {
			t.Error("error message mismatch")
		}
	})

	t.Run("ErrProviderUnavailable", func(t *testing.T) {
		err := &auth.ErrProviderUnavailable{Provider: "ldap"}
		if err.Error() != "provider unavailable: ldap" {
			t.Error("error message mismatch")
		}

		cause := &auth.ErrAuthenticationFailed{Message: "test"}

		errWithCause := &auth.ErrProviderUnavailable{Provider: "ldap", Cause: cause}
		if errWithCause.Error() != "provider unavailable: ldap: test" {
			t.Error("error with cause message mismatch")
		}

		if errWithCause.Unwrap() != cause {
			t.Error("unwrap should return cause")
		}
	})

	t.Run("ErrInvalidToken", func(t *testing.T) {
		err := &auth.ErrInvalidToken{Reason: "expired"}
		if err.Error() != "invalid token: expired" {
			t.Error("error message mismatch")
		}
	})

	t.Run("ErrTokenExpired", func(t *testing.T) {
		err := &auth.ErrTokenExpired{}
		if err.Error() != "token has expired" {
			t.Error("error message mismatch")
		}
	})
}

func TestSyncConfig(t *testing.T) {
	cfg := DefaultSyncConfig()

	if cfg.Interval <= 0 {
		t.Error("default sync interval should be positive")
	}

	if cfg.PageSize <= 0 {
		t.Error("default page size should be positive")
	}
}

// mockUserStore is a mock implementation of UserStore for testing.
type mockUserStore struct {
	users map[string]*auth.User
	mu    sync.RWMutex
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{
		users: make(map[string]*auth.User),
	}
}

func (m *mockUserStore) UpsertUser(_ context.Context, user *auth.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.users[user.Username] = user

	return nil
}

func (m *mockUserStore) DeleteUser(_ context.Context, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.users, username)

	return nil
}

func (m *mockUserStore) ListUsers(_ context.Context, provider string) ([]*auth.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var users []*auth.User
	for _, u := range m.users {
		if u.Provider == provider {
			users = append(users, u)
		}
	}

	return users, nil
}

func (m *mockUserStore) GetUser(_ context.Context, username string) (*auth.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if user, ok := m.users[username]; ok {
		return user, nil
	}

	return nil, &auth.ErrUserNotFound{Username: username}
}

func TestUserSyncServiceStatus(t *testing.T) {
	store := newMockUserStore()

	// Create a minimal config for testing (won't actually connect)
	cfg := Config{
		ProviderConfig: auth.ProviderConfig{
			Name: "test-ldap",
		},
		ServerURL:    "ldap://localhost:389",
		BindDN:       "cn=admin,dc=test,dc=com",
		BindPassword: "test",
		UserSearch: UserSearchConfig{
			BaseDN: "ou=users,dc=test,dc=com",
			Filter: "(uid={username})",
			Attributes: AttributeMapping{
				Username: "uid",
			},
		},
		Pool: PoolConfig{
			MaxConnections: 5,
			ConnectTimeout: time.Second,
		},
	}

	syncCfg := DefaultSyncConfig()
	syncCfg.Enabled = false // Don't actually sync

	// We can't create a real provider without an LDAP server
	// Test sync status independently
	syncSvc := &UserSyncService{
		store:  store,
		config: syncCfg,
		stopCh: make(chan struct{}),
	}

	t.Run("InitialStatus", func(t *testing.T) {
		status := syncSvc.Status()
		if !status.LastSync.IsZero() {
			t.Error("initial last sync should be zero")
		}

		if len(status.Errors) != 0 {
			t.Error("initial errors should be empty")
		}
	})

	t.Run("GetSyncErrors", func(t *testing.T) {
		errors := syncSvc.GetSyncErrors()
		if len(errors) != 0 {
			t.Error("initial sync errors should be empty")
		}
	})

	t.Run("GetLastSync", func(t *testing.T) {
		lastSync := syncSvc.GetLastSync()
		if !lastSync.IsZero() {
			t.Error("initial last sync should be zero")
		}
	})

	// Test validation that would fail without a server
	t.Run("ConfigValidation", func(t *testing.T) {
		err := cfg.Validate()
		if err != nil {
			t.Errorf("config validation failed: %v", err)
		}
	})
}

func TestProviderInterface(t *testing.T) {
	// Ensure Provider implements auth.Provider interface
	var _ auth.Provider = (*Provider)(nil)
}

func TestUserStore(t *testing.T) {
	store := newMockUserStore()
	ctx := context.Background()

	t.Run("UpsertAndGet", func(t *testing.T) {
		user := &auth.User{
			Username:    "testuser",
			DisplayName: "Test User",
			Email:       "test@example.com",
			Provider:    "ldap",
		}

		err := store.UpsertUser(ctx, user)
		if err != nil {
			t.Fatalf("upsert failed: %v", err)
		}

		retrieved, err := store.GetUser(ctx, "testuser")
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}

		if retrieved.Username != "testuser" {
			t.Error("username mismatch")
		}
	})

	t.Run("ListUsers", func(t *testing.T) {
		users, err := store.ListUsers(ctx, "ldap")
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}

		if len(users) != 1 {
			t.Errorf("expected 1 user, got %d", len(users))
		}
	})

	t.Run("DeleteUser", func(t *testing.T) {
		err := store.DeleteUser(ctx, "testuser")
		if err != nil {
			t.Fatalf("delete failed: %v", err)
		}

		_, err = store.GetUser(ctx, "testuser")
		if err == nil {
			t.Error("expected error for deleted user")
		}
	})

	t.Run("GetNonexistent", func(t *testing.T) {
		_, err := store.GetUser(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent user")
		}
	})
}

func TestTLSConfig(t *testing.T) {
	t.Run("DefaultTLS", func(t *testing.T) {
		tlsCfg := TLSConfig{}

		config, err := buildTLSConfig(tlsCfg)
		if err != nil {
			t.Fatalf("failed to build TLS config: %v", err)
		}

		if config.InsecureSkipVerify {
			t.Error("insecure skip verify should be false by default")
		}
	})

	t.Run("InsecureSkipVerify", func(t *testing.T) {
		tlsCfg := TLSConfig{
			InsecureSkipVerify: true,
		}

		config, err := buildTLSConfig(tlsCfg)
		if err != nil {
			t.Fatalf("failed to build TLS config: %v", err)
		}

		if !config.InsecureSkipVerify {
			t.Error("insecure skip verify should be true")
		}
	})

	t.Run("NonexistentCACert", func(t *testing.T) {
		tlsCfg := TLSConfig{
			CACertFile: "/nonexistent/ca.crt",
		}

		_, err := buildTLSConfig(tlsCfg)
		if err == nil {
			t.Error("expected error for nonexistent CA cert")
		}
	})

	t.Run("NonexistentClientCert", func(t *testing.T) {
		tlsCfg := TLSConfig{
			ClientCertFile: "/nonexistent/client.crt",
			ClientKeyFile:  "/nonexistent/client.key",
		}

		_, err := buildTLSConfig(tlsCfg)
		if err == nil {
			t.Error("expected error for nonexistent client cert")
		}
	})
}

func TestGroupMapping(t *testing.T) {
	mapping := auth.GroupMapping{
		ExternalGroup:  "LDAP-Admins",
		InternalPolicy: "admin",
	}

	if mapping.ExternalGroup != "LDAP-Admins" {
		t.Error("external group mismatch")
	}

	if mapping.InternalPolicy != "admin" {
		t.Error("internal policy mismatch")
	}
}

func TestExternalTokenPair(t *testing.T) {
	pair := auth.ExternalTokenPair{
		AccessToken:      "access-token",
		RefreshToken:     "refresh-token",
		TokenType:        "Bearer",
		ExpiresIn:        3600,
		RefreshExpiresIn: 86400,
	}

	if pair.AccessToken != "access-token" {
		t.Error("access token mismatch")
	}

	if pair.TokenType != "Bearer" {
		t.Error("token type mismatch")
	}
}

func TestUser(t *testing.T) {
	now := time.Now()
	user := auth.User{
		Username:    "testuser",
		DisplayName: "Test User",
		Email:       "test@example.com",
		Groups:      []string{"users", "admins"},
		Attributes: map[string][]string{
			"department": {"Engineering"},
		},
		Provider:   "ldap",
		ProviderID: "12345",
		Enabled:    true,
		LastLogin:  &now,
	}

	if user.Username != "testuser" {
		t.Error("username mismatch")
	}

	if len(user.Groups) != 2 {
		t.Error("groups count mismatch")
	}

	if user.Attributes["department"][0] != "Engineering" {
		t.Error("attributes mismatch")
	}
}
