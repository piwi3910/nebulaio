package ldap

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/piwi3910/nebulaio/internal/auth"
)

// UserSyncService synchronizes LDAP users with the local database
// Fields ordered by size to minimize padding.
type UserSyncService struct {
	syncErrors []error
	stopCh     chan struct{}
	provider   *Provider
	store      UserStore
	config     SyncConfig
	mu         sync.RWMutex
	lastSync   time.Time
	started    bool
}

// UserStore is the interface for storing synced users
type UserStore interface {
	// UpsertUser creates or updates a user
	UpsertUser(ctx context.Context, user *auth.User) error
	// DeleteUser deletes a user by username
	DeleteUser(ctx context.Context, username string) error
	// ListUsers lists all users from a provider
	ListUsers(ctx context.Context, provider string) ([]*auth.User, error)
	// GetUser gets a user by username
	GetUser(ctx context.Context, username string) (*auth.User, error)
}

// SyncConfig holds user synchronization configuration
// Fields ordered by size to minimize padding.
type SyncConfig struct {
	// Filter is an additional LDAP filter to apply
	Filter string `json:"filter,omitempty" yaml:"filter,omitempty"`

	// Interval is how often to sync
	Interval time.Duration `json:"interval" yaml:"interval"`

	// PageSize is the page size for paged searches
	PageSize int `json:"pageSize" yaml:"pageSize"`

	// Enabled indicates if sync is enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// SyncOnStartup triggers a sync when the service starts
	SyncOnStartup bool `json:"syncOnStartup" yaml:"syncOnStartup"`

	// DeleteOrphans deletes users that no longer exist in LDAP
	DeleteOrphans bool `json:"deleteOrphans" yaml:"deleteOrphans"`
}

// DefaultSyncConfig returns sensible defaults
func DefaultSyncConfig() SyncConfig {
	return SyncConfig{
		Enabled:       false,
		Interval:      15 * time.Minute,
		SyncOnStartup: true,
		DeleteOrphans: false,
		PageSize:      500,
	}
}

// NewUserSyncService creates a new user sync service
func NewUserSyncService(provider *Provider, store UserStore, cfg SyncConfig) *UserSyncService {
	if cfg.Interval <= 0 {
		cfg.Interval = 15 * time.Minute
	}
	if cfg.PageSize <= 0 {
		cfg.PageSize = 500
	}

	return &UserSyncService{
		provider: provider,
		store:    store,
		config:   cfg,
		stopCh:   make(chan struct{}),
	}
}

// Start starts the sync service
func (s *UserSyncService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("sync service already started")
	}
	s.started = true
	s.mu.Unlock()

	// Sync on startup if configured
	if s.config.SyncOnStartup {
		if err := s.Sync(ctx); err != nil {
			// Log but don't fail startup
			fmt.Printf("warning: initial LDAP sync failed: %v\n", err)
		}
	}

	// Start periodic sync
	go s.syncLoop(ctx)

	return nil
}

// Stop stops the sync service
func (s *UserSyncService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	close(s.stopCh)
	s.started = false
}

// Sync performs a synchronization
func (s *UserSyncService) Sync(ctx context.Context) error {
	s.mu.Lock()
	s.syncErrors = nil
	s.mu.Unlock()

	// Get all LDAP users
	ldapUsers, err := s.fetchAllUsers(ctx)
	if err != nil {
		s.addSyncError(err)
		return fmt.Errorf("failed to fetch LDAP users: %w", err)
	}

	// Track which users we've seen
	seenUsers := make(map[string]bool)

	// Upsert all users
	for _, user := range ldapUsers {
		user.Provider = s.provider.Name()
		seenUsers[user.Username] = true

		if err := s.store.UpsertUser(ctx, user); err != nil {
			s.addSyncError(fmt.Errorf("failed to upsert user %s: %w", user.Username, err))
		}
	}

	// Delete orphans if configured
	if s.config.DeleteOrphans {
		if err := s.deleteOrphans(ctx, seenUsers); err != nil {
			s.addSyncError(err)
		}
	}

	s.mu.Lock()
	s.lastSync = time.Now()
	s.mu.Unlock()

	return nil
}

// GetLastSync returns the last sync time
func (s *UserSyncService) GetLastSync() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSync
}

// GetSyncErrors returns any errors from the last sync
func (s *UserSyncService) GetSyncErrors() []error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	errors := make([]error, len(s.syncErrors))
	copy(errors, s.syncErrors)
	return errors
}

// syncLoop runs periodic synchronization
func (s *UserSyncService) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			if err := s.Sync(ctx); err != nil {
				fmt.Printf("warning: LDAP sync failed: %v\n", err)
			}
		}
	}
}

// fetchAllUsers fetches all users from LDAP
func (s *UserSyncService) fetchAllUsers(ctx context.Context) ([]*auth.User, error) {
	conn, err := s.provider.pool.get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.provider.pool.put(conn)

	// Build search filter - search for all users with the username attribute
	filter := fmt.Sprintf("(%s=*)", s.provider.config.UserSearch.Attributes.Username)
	if s.config.Filter != "" {
		filter = "(&" + filter + s.config.Filter + ")"
	}

	// Build attributes to retrieve
	attrs := []string{"dn"}
	mapping := s.provider.config.UserSearch.Attributes
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

	var users []*auth.User

	// Use paged search for large directories
	// Validate PageSize is within uint32 range
	pageSize := s.config.PageSize
	const maxPageSize = ^uint32(0)
	if pageSize < 0 || pageSize > int(maxPageSize) {
		return nil, fmt.Errorf("invalid page size: %d (must be between 0 and %d)", pageSize, maxPageSize) // nolint:err113 // dynamic error with context
	}
	pagingControl := ldap.NewControlPaging(uint32(pageSize)) // #nosec G115 - validated above

	for {
		searchReq := ldap.NewSearchRequest(
			s.provider.config.UserSearch.BaseDN,
			SearchScope(s.provider.config.UserSearch.Scope),
			ldap.NeverDerefAliases,
			0, // No size limit for sync
			0, // No time limit
			false,
			filter,
			attrs,
			[]ldap.Control{pagingControl},
		)

		result, err := conn.Search(searchReq)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}

		for _, entry := range result.Entries {
			user := s.provider.entryToUser(entry)
			if user.Username != "" {
				users = append(users, user)
			}
		}

		// Check if there are more pages
		pagingResult := ldap.FindControl(result.Controls, ldap.ControlTypePaging)
		if pagingResult == nil {
			break
		}

		cookie := pagingResult.(*ldap.ControlPaging).Cookie
		if len(cookie) == 0 {
			break
		}

		pagingControl.SetCookie(cookie)
	}

	return users, nil
}

// deleteOrphans deletes users that no longer exist in LDAP
func (s *UserSyncService) deleteOrphans(ctx context.Context, seenUsers map[string]bool) error {
	// Get all local users from this provider
	localUsers, err := s.store.ListUsers(ctx, s.provider.Name())
	if err != nil {
		return fmt.Errorf("failed to list local users: %w", err)
	}

	for _, user := range localUsers {
		if !seenUsers[user.Username] {
			if err := s.store.DeleteUser(ctx, user.Username); err != nil {
				s.addSyncError(fmt.Errorf("failed to delete orphan user %s: %w", user.Username, err))
			}
		}
	}

	return nil
}

// addSyncError adds an error to the sync errors list
func (s *UserSyncService) addSyncError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncErrors = append(s.syncErrors, err)
}

// SyncStatus returns the current sync status
type SyncStatus struct {
	// LastSync is when the last sync completed
	LastSync time.Time `json:"lastSync"`
	// NextSync is when the next sync is scheduled
	NextSync time.Time `json:"nextSync"`
	// Errors contains any errors from the last sync
	Errors []string `json:"errors,omitempty"`
	// Running indicates if a sync is currently running
	Running bool `json:"running"`
}

// Status returns the current sync status
func (s *UserSyncService) Status() SyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := SyncStatus{
		LastSync: s.lastSync,
		Running:  false,
	}

	if !s.lastSync.IsZero() {
		status.NextSync = s.lastSync.Add(s.config.Interval)
	}

	for _, err := range s.syncErrors {
		status.Errors = append(status.Errors, err.Error())
	}

	return status
}
