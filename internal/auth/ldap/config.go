package ldap

import (
	"errors"
	"time"

	"github.com/piwi3910/nebulaio/internal/auth"
)

// LDAP search scope constants.
const (
	scopeBaseObject    = 0 // ldap.ScopeBaseObject
	scopeSingleLevel   = 1 // ldap.ScopeSingleLevel
	scopeWholeSubtree  = 2 // ldap.ScopeWholeSubtree
)

// Config holds LDAP provider configuration.
type Config struct {
	// Base configuration
	auth.ProviderConfig `yaml:",inline"`

	// ServerURL is the LDAP server URL (ldap:// or ldaps://)
	ServerURL string `json:"serverUrl" yaml:"serverUrl"`

	// BindDN is the DN to bind with for searches
	BindDN string `json:"bindDn" yaml:"bindDn"`

	// BindPassword is the password for the bind DN
	BindPassword string `json:"bindPassword" yaml:"bindPassword"`

	// TLS configuration
	TLS TLSConfig `json:"tls,omitempty" yaml:"tls,omitempty"`

	// User search configuration
	UserSearch UserSearchConfig `json:"userSearch" yaml:"userSearch"`

	// Group search configuration
	GroupSearch GroupSearchConfig `json:"groupSearch,omitempty" yaml:"groupSearch,omitempty"`

	// Connection pool settings
	Pool PoolConfig `json:"pool,omitempty" yaml:"pool,omitempty"`
}

// TLSConfig holds TLS settings for LDAP.
type TLSConfig struct {
	CACertFile         string `json:"caCertFile,omitempty" yaml:"caCertFile,omitempty"`
	ClientCertFile     string `json:"clientCertFile,omitempty" yaml:"clientCertFile,omitempty"`
	ClientKeyFile      string `json:"clientKeyFile,omitempty" yaml:"clientKeyFile,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify" yaml:"insecureSkipVerify"`
	StartTLS           bool   `json:"startTls" yaml:"startTls"`
}

// UserSearchConfig holds user search settings.
type UserSearchConfig struct {
	Attributes AttributeMapping `json:"attributes" yaml:"attributes"`
	BaseDN     string           `json:"baseDn" yaml:"baseDn"`
	Filter     string           `json:"filter" yaml:"filter"`
	Scope      string           `json:"scope" yaml:"scope"`
	SizeLimit  int              `json:"sizeLimit,omitempty" yaml:"sizeLimit,omitempty"`
	TimeLimit  time.Duration    `json:"timeLimit,omitempty" yaml:"timeLimit,omitempty"`
}

// GroupSearchConfig holds group search settings.
type GroupSearchConfig struct {
	// BaseDN is the base DN for group searches
	BaseDN string `json:"baseDn" yaml:"baseDn"`

	// Filter is the LDAP filter for finding groups
	// Use {dn} for the user's DN, {username} for username
	Filter string `json:"filter" yaml:"filter"`

	// Scope is the search scope (base, one, sub)
	Scope string `json:"scope" yaml:"scope"`

	// NameAttribute is the attribute containing the group name
	NameAttribute string `json:"nameAttribute" yaml:"nameAttribute"`

	// SizeLimit limits the number of results
	SizeLimit int `json:"sizeLimit,omitempty" yaml:"sizeLimit,omitempty"`

	// TimeLimit limits the search time
	TimeLimit time.Duration `json:"timeLimit,omitempty" yaml:"timeLimit,omitempty"`
}

// AttributeMapping maps LDAP attributes to user fields.
type AttributeMapping struct {
	Additional  map[string]string `json:"additional,omitempty" yaml:"additional,omitempty"`
	Username    string            `json:"username" yaml:"username"`
	DisplayName string            `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Email       string            `json:"email,omitempty" yaml:"email,omitempty"`
	MemberOf    string            `json:"memberOf,omitempty" yaml:"memberOf,omitempty"`
	UniqueID    string            `json:"uniqueId,omitempty" yaml:"uniqueId,omitempty"`
}

// PoolConfig holds connection pool settings.
type PoolConfig struct {
	// MaxConnections is the maximum number of connections
	MaxConnections int `json:"maxConnections" yaml:"maxConnections"`

	// MaxIdleConnections is the maximum number of idle connections
	MaxIdleConnections int `json:"maxIdleConnections" yaml:"maxIdleConnections"`

	// ConnectTimeout is the connection timeout
	ConnectTimeout time.Duration `json:"connectTimeout" yaml:"connectTimeout"`

	// RequestTimeout is the request timeout
	RequestTimeout time.Duration `json:"requestTimeout" yaml:"requestTimeout"`

	// IdleTimeout is how long to keep idle connections
	IdleTimeout time.Duration `json:"idleTimeout" yaml:"idleTimeout"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		ProviderConfig: auth.ProviderConfig{
			Type:    "ldap",
			Enabled: false,
		},
		TLS: TLSConfig{
			InsecureSkipVerify: false,
			StartTLS:           false,
		},
		UserSearch: UserSearchConfig{
			Filter: "(&(objectClass=person)(uid={username}))",
			Scope:  "sub",
			Attributes: AttributeMapping{
				Username:    "uid",
				DisplayName: "cn",
				Email:       "mail",
				MemberOf:    "memberOf",
				UniqueID:    "entryUUID",
			},
			SizeLimit: 1,
			TimeLimit: 10 * time.Second,
		},
		GroupSearch: GroupSearchConfig{
			Filter:        "(&(objectClass=groupOfNames)(member={dn}))",
			Scope:         "sub",
			NameAttribute: "cn",
			SizeLimit:     100,
			TimeLimit:     10 * time.Second,
		},
		Pool: PoolConfig{
			MaxConnections:     10,
			MaxIdleConnections: 5,
			ConnectTimeout:     10 * time.Second,
			RequestTimeout:     30 * time.Second,
			IdleTimeout:        5 * time.Minute,
		},
	}
}

// ActiveDirectoryConfig returns defaults for Active Directory.
func ActiveDirectoryConfig() Config {
	cfg := DefaultConfig()
	cfg.UserSearch.Filter = "(&(objectClass=user)(sAMAccountName={username}))"
	cfg.UserSearch.Attributes = AttributeMapping{
		Username:    "sAMAccountName",
		DisplayName: "displayName",
		Email:       "mail",
		MemberOf:    "memberOf",
		UniqueID:    "objectGUID",
	}
	cfg.GroupSearch.Filter = "(&(objectClass=group)(member={dn}))"
	cfg.GroupSearch.NameAttribute = "cn"

	return cfg
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.ServerURL == "" {
		return errors.New("server URL is required")
	}

	if c.BindDN == "" {
		return errors.New("bind DN is required")
	}

	if c.BindPassword == "" {
		return errors.New("bind password is required")
	}

	if c.UserSearch.BaseDN == "" {
		return errors.New("user search base DN is required")
	}

	if c.UserSearch.Filter == "" {
		return errors.New("user search filter is required")
	}

	if c.UserSearch.Attributes.Username == "" {
		return errors.New("username attribute mapping is required")
	}

	// Validate scope values
	validScopes := map[string]bool{"base": true, "one": true, "sub": true}
	if c.UserSearch.Scope != "" && !validScopes[c.UserSearch.Scope] {
		return errors.New("invalid user search scope: must be base, one, or sub")
	}

	if c.GroupSearch.BaseDN != "" {
		if c.GroupSearch.Filter == "" {
			return errors.New("group search filter is required when group base DN is set")
		}

		if c.GroupSearch.NameAttribute == "" {
			return errors.New("group name attribute is required when group search is configured")
		}

		if c.GroupSearch.Scope != "" && !validScopes[c.GroupSearch.Scope] {
			return errors.New("invalid group search scope: must be base, one, or sub")
		}
	}

	return nil
}

// SearchScope converts scope string to LDAP constant.
func SearchScope(scope string) int {
	switch scope {
	case "base":
		return scopeBaseObject
	case "one":
		return scopeSingleLevel
	case "sub":
		return scopeWholeSubtree
	default:
		return scopeWholeSubtree // Default to subtree
	}
}
