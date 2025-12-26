package ldap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/piwi3910/nebulaio/internal/auth"
)

// Provider implements auth.Provider for LDAP/Active Directory
type Provider struct {
	config     Config
	pool       *connPool
	mu         sync.RWMutex
	tlsConfig  *tls.Config
	closed     bool
}

// connPool manages a pool of LDAP connections
type connPool struct {
	connections chan *ldap.Conn
	maxSize     int
	config      Config
	tlsConfig   *tls.Config
}

// NewProvider creates a new LDAP provider
func NewProvider(cfg Config) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Set defaults
	if cfg.Pool.MaxConnections <= 0 {
		cfg.Pool.MaxConnections = 10
	}
	if cfg.Pool.ConnectTimeout <= 0 {
		cfg.Pool.ConnectTimeout = 10 * time.Second
	}
	if cfg.Pool.RequestTimeout <= 0 {
		cfg.Pool.RequestTimeout = 30 * time.Second
	}

	// Build TLS config
	tlsConfig, err := buildTLSConfig(cfg.TLS)
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config: %w", err)
	}

	provider := &Provider{
		config:    cfg,
		tlsConfig: tlsConfig,
	}

	// Initialize connection pool
	provider.pool = &connPool{
		connections: make(chan *ldap.Conn, cfg.Pool.MaxConnections),
		maxSize:     cfg.Pool.MaxConnections,
		config:      cfg,
		tlsConfig:   tlsConfig,
	}

	// Test initial connection
	conn, err := provider.pool.get(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP server: %w", err)
	}
	provider.pool.put(conn)

	return provider, nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	if p.config.Name != "" {
		return p.config.Name
	}
	return "ldap"
}

// Authenticate validates credentials against LDAP
func (p *Provider) Authenticate(ctx context.Context, username, password string) (*auth.User, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name()}
	}
	p.mu.RUnlock()

	if username == "" || password == "" {
		return nil, &auth.ErrAuthenticationFailed{Message: "username and password required"}
	}

	// Get connection from pool
	conn, err := p.pool.get(ctx)
	if err != nil {
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name(), Cause: err}
	}
	defer p.pool.put(conn)

	// Search for the user
	userDN, user, err := p.searchUser(conn, username)
	if err != nil {
		return nil, err
	}

	// Attempt to bind as the user to verify password
	err = conn.Bind(userDN, password)
	if err != nil {
		// Re-bind as service account
		if bindErr := conn.Bind(p.config.BindDN, p.config.BindPassword); bindErr != nil {
			// Connection is bad, don't return to pool
			p.pool.discard(conn)
			return nil, &auth.ErrProviderUnavailable{Provider: p.Name(), Cause: bindErr}
		}
		return nil, &auth.ErrAuthenticationFailed{Message: "invalid credentials"}
	}

	// Re-bind as service account for subsequent operations
	if err := conn.Bind(p.config.BindDN, p.config.BindPassword); err != nil {
		p.pool.discard(conn)
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name(), Cause: err}
	}

	// Get groups if configured
	if p.config.GroupSearch.BaseDN != "" {
		groups, err := p.searchGroups(conn, userDN, username)
		if err != nil {
			// Log but don't fail authentication
			fmt.Printf("warning: failed to get groups for user %s: %v\n", username, err)
		} else {
			user.Groups = groups
		}
	}

	now := time.Now()
	user.LastLogin = &now
	user.Provider = p.Name()

	return user, nil
}

// GetUser retrieves user information
func (p *Provider) GetUser(ctx context.Context, username string) (*auth.User, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name()}
	}
	p.mu.RUnlock()

	conn, err := p.pool.get(ctx)
	if err != nil {
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name(), Cause: err}
	}
	defer p.pool.put(conn)

	userDN, user, err := p.searchUser(conn, username)
	if err != nil {
		return nil, err
	}

	// Get groups if configured
	if p.config.GroupSearch.BaseDN != "" {
		groups, err := p.searchGroups(conn, userDN, username)
		if err == nil {
			user.Groups = groups
		}
	}

	user.Provider = p.Name()
	return user, nil
}

// GetGroups retrieves groups for a user
func (p *Provider) GetGroups(ctx context.Context, username string) ([]string, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name()}
	}
	p.mu.RUnlock()

	conn, err := p.pool.get(ctx)
	if err != nil {
		return nil, &auth.ErrProviderUnavailable{Provider: p.Name(), Cause: err}
	}
	defer p.pool.put(conn)

	// First get user DN
	userDN, _, err := p.searchUser(conn, username)
	if err != nil {
		return nil, err
	}

	return p.searchGroups(conn, userDN, username)
}

// ValidateToken is not supported for LDAP (use session-based auth)
func (p *Provider) ValidateToken(_ context.Context, _ string) (*auth.User, error) {
	return nil, &auth.ErrInvalidToken{Reason: "LDAP does not support token validation"}
}

// Refresh is not supported for LDAP
func (p *Provider) Refresh(_ context.Context, _ string) (*auth.ExternalTokenPair, error) {
	return nil, &auth.ErrInvalidToken{Reason: "LDAP does not support token refresh"}
}

// Close closes all connections
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	p.pool.close()
	return nil
}

// searchUser searches for a user by username
func (p *Provider) searchUser(conn *ldap.Conn, username string) (string, *auth.User, error) {
	// Build search filter
	filter := strings.ReplaceAll(p.config.UserSearch.Filter, "{username}", ldap.EscapeFilter(username))

	// Build attributes to retrieve
	attrs := []string{"dn"}
	mapping := p.config.UserSearch.Attributes
	if mapping.Username != "" {
		attrs = append(attrs, mapping.Username)
	}
	if mapping.DisplayName != "" {
		attrs = append(attrs, mapping.DisplayName)
	}
	if mapping.Email != "" {
		attrs = append(attrs, mapping.Email)
	}
	if mapping.MemberOf != "" {
		attrs = append(attrs, mapping.MemberOf)
	}
	if mapping.UniqueID != "" {
		attrs = append(attrs, mapping.UniqueID)
	}
	for _, attr := range mapping.Additional {
		attrs = append(attrs, attr)
	}

	searchReq := ldap.NewSearchRequest(
		p.config.UserSearch.BaseDN,
		SearchScope(p.config.UserSearch.Scope),
		ldap.NeverDerefAliases,
		p.config.UserSearch.SizeLimit,
		int(p.config.UserSearch.TimeLimit.Seconds()),
		false,
		filter,
		attrs,
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return "", nil, fmt.Errorf("user search failed: %w", err)
	}

	if len(result.Entries) == 0 {
		return "", nil, &auth.ErrUserNotFound{Username: username}
	}

	if len(result.Entries) > 1 {
		return "", nil, fmt.Errorf("multiple users found for username: %s", username)
	}

	entry := result.Entries[0]
	user := p.entryToUser(entry)

	return entry.DN, user, nil
}

// searchGroups searches for groups a user belongs to
func (p *Provider) searchGroups(conn *ldap.Conn, userDN, username string) ([]string, error) {
	if p.config.GroupSearch.BaseDN == "" {
		return nil, nil
	}

	// Build search filter
	filter := p.config.GroupSearch.Filter
	filter = strings.ReplaceAll(filter, "{dn}", ldap.EscapeFilter(userDN))
	filter = strings.ReplaceAll(filter, "{username}", ldap.EscapeFilter(username))

	searchReq := ldap.NewSearchRequest(
		p.config.GroupSearch.BaseDN,
		SearchScope(p.config.GroupSearch.Scope),
		ldap.NeverDerefAliases,
		p.config.GroupSearch.SizeLimit,
		int(p.config.GroupSearch.TimeLimit.Seconds()),
		false,
		filter,
		[]string{p.config.GroupSearch.NameAttribute},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("group search failed: %w", err)
	}

	groups := make([]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		groupName := entry.GetAttributeValue(p.config.GroupSearch.NameAttribute)
		if groupName != "" {
			groups = append(groups, groupName)
		}
	}

	return groups, nil
}

// entryToUser converts an LDAP entry to an auth.User
func (p *Provider) entryToUser(entry *ldap.Entry) *auth.User {
	mapping := p.config.UserSearch.Attributes

	user := &auth.User{
		Enabled:    true,
		Attributes: make(map[string][]string),
	}

	// Map standard attributes
	if mapping.Username != "" {
		user.Username = entry.GetAttributeValue(mapping.Username)
	}
	if mapping.DisplayName != "" {
		user.DisplayName = entry.GetAttributeValue(mapping.DisplayName)
	}
	if mapping.Email != "" {
		user.Email = entry.GetAttributeValue(mapping.Email)
	}
	if mapping.UniqueID != "" {
		user.ProviderID = entry.GetAttributeValue(mapping.UniqueID)
	}

	// Get groups from memberOf attribute if present
	if mapping.MemberOf != "" {
		memberOf := entry.GetAttributeValues(mapping.MemberOf)
		for _, dn := range memberOf {
			// Extract CN from DN
			cn := extractCNFromDN(dn)
			if cn != "" {
				user.Groups = append(user.Groups, cn)
			}
		}
	}

	// Map additional attributes
	for key, attr := range mapping.Additional {
		values := entry.GetAttributeValues(attr)
		if len(values) > 0 {
			user.Attributes[key] = values
		}
	}

	return user
}

// extractCNFromDN extracts the CN from a DN string
func extractCNFromDN(dn string) string {
	parts := strings.Split(dn, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "cn=") {
			return part[3:]
		}
	}
	return ""
}

// buildTLSConfig builds the TLS configuration
func buildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	// Load CA certificate if specified
	if cfg.CACertFile != "" {
		caCert, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// Load client certificate if specified
	if cfg.ClientCertFile != "" && cfg.ClientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// Connection pool methods

func (p *connPool) get(ctx context.Context) (*ldap.Conn, error) {
	// Try to get an existing connection
	select {
	case conn := <-p.connections:
		// Test if connection is still valid
		if err := conn.Bind(p.config.BindDN, p.config.BindPassword); err == nil {
			return conn, nil
		}
		// Connection is bad, close it and create new one
		_ = conn.Close()
	default:
		// No connection available
	}

	// Create new connection
	return p.create(ctx)
}

func (p *connPool) create(ctx context.Context) (*ldap.Conn, error) {
	var conn *ldap.Conn
	var err error

	// Determine if using LDAPS
	isLDAPS := strings.HasPrefix(p.config.ServerURL, "ldaps://")

	// Set dial timeout
	ldap.DefaultTimeout = p.config.Pool.ConnectTimeout

	if isLDAPS {
		conn, err = ldap.DialURL(p.config.ServerURL, ldap.DialWithTLSConfig(p.tlsConfig))
	} else {
		conn, err = ldap.DialURL(p.config.ServerURL)
		if err == nil && p.config.TLS.StartTLS {
			err = conn.StartTLS(p.tlsConfig)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP server: %w", err)
	}

	// Bind as service account
	if err := conn.Bind(p.config.BindDN, p.config.BindPassword); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to bind: %w", err)
	}

	return conn, nil
}

func (p *connPool) put(conn *ldap.Conn) {
	select {
	case p.connections <- conn:
		// Returned to pool
	default:
		// Pool is full, close connection
		_ = conn.Close()
	}
}

func (p *connPool) discard(conn *ldap.Conn) {
	if conn != nil {
		_ = conn.Close()
	}
}

func (p *connPool) close() {
	close(p.connections)
	for conn := range p.connections {
		_ = conn.Close()
	}
}
