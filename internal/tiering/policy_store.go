package tiering

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// PolicyStore provides storage and retrieval for policies.
type PolicyStore interface {
	// Create creates a new policy
	Create(ctx context.Context, policy *AdvancedPolicy) error

	// Get retrieves a policy by ID
	Get(ctx context.Context, id string) (*AdvancedPolicy, error)

	// Update updates an existing policy
	Update(ctx context.Context, policy *AdvancedPolicy) error

	// Delete removes a policy
	Delete(ctx context.Context, id string) error

	// List returns all policies
	List(ctx context.Context) ([]*AdvancedPolicy, error)

	// ListByScope returns policies for a scope
	ListByScope(ctx context.Context, scope PolicyScope, target string) ([]*AdvancedPolicy, error)

	// ListByType returns policies of a specific type
	ListByType(ctx context.Context, policyType PolicyType) ([]*AdvancedPolicy, error)

	// GetStats returns statistics for a policy
	GetStats(ctx context.Context, id string) (*PolicyStats, error)

	// UpdateStats updates statistics for a policy
	UpdateStats(ctx context.Context, stats *PolicyStats) error

	// Watch watches for policy changes
	Watch(ctx context.Context) (<-chan PolicyChangeEvent, error)
}

// PolicyChangeEvent represents a change to a policy.
type PolicyChangeEvent struct {
	Timestamp time.Time
	Policy    *AdvancedPolicy
	Type      PolicyChangeType
}

// PolicyChangeType defines types of policy changes.
type PolicyChangeType string

const (
	PolicyChangeCreated PolicyChangeType = "created"
	PolicyChangeUpdated PolicyChangeType = "updated"
	PolicyChangeDeleted PolicyChangeType = "deleted"
)

// HybridPolicyStore combines config file and metadata store.
type HybridPolicyStore struct {
	cacheAge   time.Time
	metaStore  MetadataStore
	cache      map[string]*AdvancedPolicy
	statsStore map[string]*PolicyStats
	stopChan   chan struct{}
	configPath string
	watchers   []chan PolicyChangeEvent
	syncWg     sync.WaitGroup
	configMu   sync.RWMutex
	cacheMu    sync.RWMutex
	statsMu    sync.RWMutex
	watchMu    sync.Mutex
}

// MetadataStore interface for bucket/object-level metadata.
type MetadataStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)
	Watch(ctx context.Context, prefix string) (<-chan MetadataEvent, error)
}

// MetadataEvent represents a metadata change.
type MetadataEvent struct {
	Type  string
	Key   string
	Value []byte
}

// HybridPolicyStoreConfig configures the hybrid store.
type HybridPolicyStoreConfig struct {
	MetadataStore MetadataStore
	ConfigPath    string
	CacheTTL      time.Duration
	SyncInterval  time.Duration
}

// NewHybridPolicyStore creates a new hybrid policy store.
func NewHybridPolicyStore(cfg HybridPolicyStoreConfig) (*HybridPolicyStore, error) {
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute
	}

	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = 1 * time.Minute
	}

	store := &HybridPolicyStore{
		configPath: cfg.ConfigPath,
		metaStore:  cfg.MetadataStore,
		cache:      make(map[string]*AdvancedPolicy),
		statsStore: make(map[string]*PolicyStats),
		watchers:   make([]chan PolicyChangeEvent, 0),
		stopChan:   make(chan struct{}),
	}

	// Load initial policies
	err := store.loadFromConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Start background sync
	store.syncWg.Add(1)

	go store.backgroundSync(cfg.SyncInterval)

	return store, nil
}

// loadFromConfig loads policies from the config file.
func (s *HybridPolicyStore) loadFromConfig() error {
	s.configMu.Lock()
	defer s.configMu.Unlock()

	if s.configPath == "" {
		return nil
	}

	// Check if file exists
	_, err := os.Stat(s.configPath)
	if os.IsNotExist(err) {
		return nil // No config file, that's OK
	}

	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config struct {
		Policies []*AdvancedPolicy `json:"policies" yaml:"policies"`
	}

	err = json.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	for _, policy := range config.Policies {
		if policy.Scope == PolicyScopeGlobal {
			s.cache[policy.ID] = policy
		}
	}

	s.cacheAge = time.Now()

	return nil
}

// saveToConfig saves global policies to the config file.
func (s *HybridPolicyStore) saveToConfig() error {
	s.configMu.Lock()
	defer s.configMu.Unlock()

	if s.configPath == "" {
		return nil
	}

	s.cacheMu.RLock()

	var policies []*AdvancedPolicy

	for _, policy := range s.cache {
		if policy.Scope == PolicyScopeGlobal {
			policies = append(policies, policy)
		}
	}

	s.cacheMu.RUnlock()

	// Sort by priority
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Priority < policies[j].Priority
	})

	config := struct {
		Policies []*AdvancedPolicy `json:"policies"`
	}{
		Policies: policies,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Ensure directory exists
	err = os.MkdirAll(filepath.Dir(s.configPath), 0750)
	if err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	err = os.WriteFile(s.configPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// backgroundSync periodically syncs from storage.
func (s *HybridPolicyStore) backgroundSync(interval time.Duration) {
	defer s.syncWg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			err := s.syncFromMetadata()
			if err != nil {
				// Log error but continue
			}
		}
	}
}

// syncFromMetadata syncs policies from the metadata store.
func (s *HybridPolicyStore) syncFromMetadata() error {
	if s.metaStore == nil {
		return nil
	}

	// INTENTIONAL: Using context.Background() for background sync operation.
	// This is called internally to refresh the policy cache, not tied to requests.
	// The timeout provides the cancellation mechanism for the metadata fetch.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	keys, err := s.metaStore.List(ctx, "policies/")
	if err != nil {
		return err
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	for _, key := range keys {
		data, err := s.metaStore.Get(ctx, key)
		if err != nil {
			continue
		}

		var policy AdvancedPolicy
		err = json.Unmarshal(data, &policy)
		if err != nil {
			continue
		}

		s.cache[policy.ID] = &policy
	}

	s.cacheAge = time.Now()

	return nil
}

// Create creates a new policy.
func (s *HybridPolicyStore) Create(ctx context.Context, policy *AdvancedPolicy) error {
	err := policy.Validate()
	if err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}

	s.cacheMu.Lock()

	if _, exists := s.cache[policy.ID]; exists {
		s.cacheMu.Unlock()
		return fmt.Errorf("policy %s already exists", policy.ID)
	}

	now := time.Now()
	policy.CreatedAt = now
	policy.UpdatedAt = now
	policy.Version = 1

	s.cache[policy.ID] = policy
	s.cacheMu.Unlock()

	// Persist based on scope
	if policy.Scope == PolicyScopeGlobal {
		err := s.saveToConfig()
		if err != nil {
			return err
		}
	} else if s.metaStore != nil {
		data, err := json.Marshal(policy)
		if err != nil {
			return err
		}

		err = s.metaStore.Put(ctx, "policies/"+policy.ID, string(data))
		if err != nil {
			return err
		}
	}

	// Notify watchers
	s.notifyChange(PolicyChangeEvent{
		Type:      PolicyChangeCreated,
		Policy:    policy,
		Timestamp: now,
	})

	return nil
}

// Get retrieves a policy by ID.
func (s *HybridPolicyStore) Get(ctx context.Context, id string) (*AdvancedPolicy, error) {
	s.cacheMu.RLock()
	policy, exists := s.cache[id]
	s.cacheMu.RUnlock()

	if exists {
		return policy, nil
	}

	// Try metadata store
	if s.metaStore != nil {
		data, err := s.metaStore.Get(ctx, "policies/"+id)
		if err == nil {
			var policy AdvancedPolicy

			err := json.Unmarshal(data, &policy)
			if err == nil {
				s.cacheMu.Lock()
				s.cache[id] = &policy
				s.cacheMu.Unlock()

				return &policy, nil
			}
		}
	}

	return nil, fmt.Errorf("policy %s not found", id)
}

// Update updates an existing policy.
func (s *HybridPolicyStore) Update(ctx context.Context, policy *AdvancedPolicy) error {
	err := policy.Validate()
	if err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}

	s.cacheMu.Lock()

	existing, exists := s.cache[policy.ID]
	if !exists {
		s.cacheMu.Unlock()
		return fmt.Errorf("policy %s not found", policy.ID)
	}

	// Optimistic locking
	if policy.Version != existing.Version {
		s.cacheMu.Unlock()
		return fmt.Errorf("policy version conflict: expected %d, got %d", existing.Version, policy.Version)
	}

	policy.UpdatedAt = time.Now()
	policy.Version++
	s.cache[policy.ID] = policy
	s.cacheMu.Unlock()

	// Persist
	if policy.Scope == PolicyScopeGlobal {
		err := s.saveToConfig()
		if err != nil {
			return err
		}
	} else if s.metaStore != nil {
		data, err := json.Marshal(policy)
		if err != nil {
			return err
		}

		err = s.metaStore.Put(ctx, "policies/"+policy.ID, string(data))
		if err != nil {
			return err
		}
	}

	// Notify watchers
	s.notifyChange(PolicyChangeEvent{
		Type:      PolicyChangeUpdated,
		Policy:    policy,
		Timestamp: time.Now(),
	})

	return nil
}

// Delete removes a policy.
func (s *HybridPolicyStore) Delete(ctx context.Context, id string) error {
	s.cacheMu.Lock()

	policy, exists := s.cache[id]
	if !exists {
		s.cacheMu.Unlock()
		return fmt.Errorf("policy %s not found", id)
	}

	delete(s.cache, id)
	s.cacheMu.Unlock()

	// Remove from storage
	if policy.Scope == PolicyScopeGlobal {
		err := s.saveToConfig()
		if err != nil {
			return err
		}
	} else if s.metaStore != nil {
		err := s.metaStore.Delete(ctx, "policies/"+id)
		if err != nil {
			return err
		}
	}

	// Notify watchers
	s.notifyChange(PolicyChangeEvent{
		Type:      PolicyChangeDeleted,
		Policy:    policy,
		Timestamp: time.Now(),
	})

	return nil
}

// List returns all policies.
func (s *HybridPolicyStore) List(ctx context.Context) ([]*AdvancedPolicy, error) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	policies := make([]*AdvancedPolicy, 0, len(s.cache))
	for _, policy := range s.cache {
		policies = append(policies, policy)
	}

	// Sort by priority
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Priority < policies[j].Priority
	})

	return policies, nil
}

// ListByScope returns policies for a scope.
func (s *HybridPolicyStore) ListByScope(ctx context.Context, scope PolicyScope, target string) ([]*AdvancedPolicy, error) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	var policies []*AdvancedPolicy

	for _, policy := range s.cache {
		if policy.Scope == scope {
			// For bucket scope, check if target matches
			if scope == PolicyScopeBucket && target != "" {
				matched := false

				for _, bucket := range policy.Selector.Buckets {
					if matchWildcard(bucket, target) {
						matched = true
						break
					}
				}

				if !matched {
					continue
				}
			}

			policies = append(policies, policy)
		}
	}

	// Sort by priority
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Priority < policies[j].Priority
	})

	return policies, nil
}

// ListByType returns policies of a specific type.
func (s *HybridPolicyStore) ListByType(ctx context.Context, policyType PolicyType) ([]*AdvancedPolicy, error) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	var policies []*AdvancedPolicy

	for _, policy := range s.cache {
		if policy.Type == policyType {
			policies = append(policies, policy)
		}
	}

	// Sort by priority
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Priority < policies[j].Priority
	})

	return policies, nil
}

// GetStats returns statistics for a policy.
func (s *HybridPolicyStore) GetStats(ctx context.Context, id string) (*PolicyStats, error) {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()

	stats, exists := s.statsStore[id]
	if !exists {
		return &PolicyStats{PolicyID: id}, nil
	}

	return stats, nil
}

// UpdateStats updates statistics for a policy.
func (s *HybridPolicyStore) UpdateStats(ctx context.Context, stats *PolicyStats) error {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	s.statsStore[stats.PolicyID] = stats

	return nil
}

// Watch watches for policy changes.
func (s *HybridPolicyStore) Watch(ctx context.Context) (<-chan PolicyChangeEvent, error) {
	ch := make(chan PolicyChangeEvent, 100)

	s.watchMu.Lock()
	s.watchers = append(s.watchers, ch)
	s.watchMu.Unlock()

	// Clean up when context is done
	go func() {
		<-ctx.Done()
		s.watchMu.Lock()

		for i, w := range s.watchers {
			if w == ch {
				s.watchers = append(s.watchers[:i], s.watchers[i+1:]...)
				break
			}
		}

		s.watchMu.Unlock()
		close(ch)
	}()

	return ch, nil
}

// notifyChange notifies all watchers of a policy change.
func (s *HybridPolicyStore) notifyChange(event PolicyChangeEvent) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	for _, ch := range s.watchers {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}

// Close closes the policy store.
func (s *HybridPolicyStore) Close() error {
	close(s.stopChan)
	s.syncWg.Wait()

	s.watchMu.Lock()

	for _, ch := range s.watchers {
		close(ch)
	}

	s.watchers = nil
	s.watchMu.Unlock()

	return nil
}

// InMemoryMetadataStore implements MetadataStore for testing.
type InMemoryMetadataStore struct {
	data     map[string][]byte
	watchers []chan MetadataEvent
	mu       sync.RWMutex
	watchMu  sync.Mutex
}

// NewInMemoryMetadataStore creates an in-memory metadata store.
func NewInMemoryMetadataStore() *InMemoryMetadataStore {
	return &InMemoryMetadataStore{
		data:     make(map[string][]byte),
		watchers: make([]chan MetadataEvent, 0),
	}
}

// Get retrieves a value.
func (s *InMemoryMetadataStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, exists := s.data[key]
	if !exists {
		return nil, fmt.Errorf("key %s not found", key)
	}

	return data, nil
}

// Put stores a value.
func (s *InMemoryMetadataStore) Put(ctx context.Context, key, value string) error {
	s.mu.Lock()
	s.data[key] = []byte(value)
	s.mu.Unlock()

	s.notifyWatch(MetadataEvent{Type: "put", Key: key, Value: []byte(value)})

	return nil
}

// Delete removes a value.
func (s *InMemoryMetadataStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()

	s.notifyWatch(MetadataEvent{Type: "delete", Key: key})

	return nil
}

// List returns keys with prefix.
func (s *InMemoryMetadataStore) List(ctx context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []string

	for key := range s.data {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// Watch watches for changes.
func (s *InMemoryMetadataStore) Watch(ctx context.Context, prefix string) (<-chan MetadataEvent, error) {
	ch := make(chan MetadataEvent, 100)

	s.watchMu.Lock()
	s.watchers = append(s.watchers, ch)
	s.watchMu.Unlock()

	go func() {
		<-ctx.Done()
		s.watchMu.Lock()

		for i, w := range s.watchers {
			if w == ch {
				s.watchers = append(s.watchers[:i], s.watchers[i+1:]...)
				break
			}
		}

		s.watchMu.Unlock()
		close(ch)
	}()

	return ch, nil
}

func (s *InMemoryMetadataStore) notifyWatch(event MetadataEvent) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	for _, ch := range s.watchers {
		select {
		case ch <- event:
		default:
		}
	}
}
