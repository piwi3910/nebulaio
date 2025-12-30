package tiering

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

// ErrStatsNotFound is returned when access stats are not found for an object.
var ErrStatsNotFound = errors.New("access stats not found")

// Access trend constants.
const (
	trendIncreasing = "increasing"
	trendStable     = "stable"
	trendDeclining  = "declining"
)

// AccessTracker tracks object access patterns for tiering decisions.
type AccessTracker struct {
	persistStore    AccessStatsStore
	stopChan        chan struct{}
	lruList         *list.List
	lruMap          map[string]*list.Element
	stats           map[string]*ObjectAccessStats
	pendingFlush    map[string]*ObjectAccessStats
	flushWg         sync.WaitGroup
	maxEntries      int
	historyDuration time.Duration
	maxHistorySize  int
	flushInterval   time.Duration
	statsMu         sync.RWMutex
	pendingFlushMu  sync.Mutex
}

// AccessStatsStore interface for persistent storage.
type AccessStatsStore interface {
	Get(ctx context.Context, bucket, key string) (*ObjectAccessStats, error)
	Put(ctx context.Context, stats *ObjectAccessStats) error
	Delete(ctx context.Context, bucket, key string) error
	List(ctx context.Context, bucket string) ([]*ObjectAccessStats, error)
}

// AccessTrackerConfig configures the access tracker.
type AccessTrackerConfig struct {
	PersistStore    AccessStatsStore
	MaxEntries      int
	HistoryDuration time.Duration
	MaxHistorySize  int
	FlushInterval   time.Duration
}

// DefaultAccessTrackerConfig returns default configuration.
func DefaultAccessTrackerConfig() AccessTrackerConfig {
	return AccessTrackerConfig{
		MaxEntries:      100000,
		HistoryDuration: 30 * 24 * time.Hour, // 30 days
		MaxHistorySize:  1000,
		FlushInterval:   5 * time.Minute,
	}
}

// NewAccessTracker creates a new access tracker.
func NewAccessTracker(cfg AccessTrackerConfig) *AccessTracker {
	tracker := &AccessTracker{
		stats:           make(map[string]*ObjectAccessStats),
		lruList:         list.New(),
		lruMap:          make(map[string]*list.Element),
		maxEntries:      cfg.MaxEntries,
		historyDuration: cfg.HistoryDuration,
		maxHistorySize:  cfg.MaxHistorySize,
		persistStore:    cfg.PersistStore,
		flushInterval:   cfg.FlushInterval,
		stopChan:        make(chan struct{}),
		pendingFlush:    make(map[string]*ObjectAccessStats),
	}

	// Start background flush if store is configured
	if cfg.PersistStore != nil {
		tracker.flushWg.Add(1)

		go tracker.backgroundFlush()
	}

	return tracker
}

// RecordAccess records an object access.
func (t *AccessTracker) RecordAccess(ctx context.Context, bucket, key, operation string, size int64) {
	fullKey := bucket + "/" + key
	now := time.Now()

	t.statsMu.Lock()
	defer t.statsMu.Unlock()

	// Get or create stats
	stats, exists := t.stats[fullKey]
	if !exists {
		// Check if we need to evict
		if len(t.stats) >= t.maxEntries {
			t.evictLRU()
		}

		stats = &ObjectAccessStats{
			Bucket:        bucket,
			Key:           key,
			AccessHistory: make([]AccessRecord, 0, t.maxHistorySize),
		}
		t.stats[fullKey] = stats

		// Add to LRU
		elem := t.lruList.PushFront(fullKey)
		t.lruMap[fullKey] = elem
	} else {
		// Move to front of LRU
		if elem, ok := t.lruMap[fullKey]; ok {
			t.lruList.MoveToFront(elem)
		}
	}

	// Update stats
	stats.LastAccessed = now
	stats.AccessCount++

	// Add to history
	record := AccessRecord{
		Timestamp: now,
		Operation: operation,
		BytesRead: size,
	}

	stats.AccessHistory = append(stats.AccessHistory, record)

	// Trim history if needed
	if len(stats.AccessHistory) > t.maxHistorySize {
		stats.AccessHistory = stats.AccessHistory[len(stats.AccessHistory)-t.maxHistorySize:]
	}

	// Remove old history entries
	cutoff := now.Add(-t.historyDuration)
	for i := 0; i < len(stats.AccessHistory); i++ {
		if stats.AccessHistory[i].Timestamp.After(cutoff) {
			if i > 0 {
				stats.AccessHistory = stats.AccessHistory[i:]
			}

			break
		}
	}

	// Update computed metrics
	t.updateMetrics(stats)

	// Mark for flush
	if t.persistStore != nil {
		t.pendingFlushMu.Lock()
		t.pendingFlush[fullKey] = stats
		t.pendingFlushMu.Unlock()
	}
}

// GetStats returns access stats for an object.
func (t *AccessTracker) GetStats(ctx context.Context, bucket, key string) (*ObjectAccessStats, error) {
	fullKey := bucket + "/" + key

	t.statsMu.RLock()
	stats, exists := t.stats[fullKey]
	t.statsMu.RUnlock()

	if exists {
		return stats, nil
	}

	// Try persistent store
	if t.persistStore != nil {
		return t.persistStore.Get(ctx, bucket, key)
	}

	return nil, ErrStatsNotFound
}

// UpdateTier updates the current tier for an object.
func (t *AccessTracker) UpdateTier(ctx context.Context, bucket, key string, tier TierType, previousTier TierType) {
	fullKey := bucket + "/" + key
	now := time.Now()

	t.statsMu.Lock()
	defer t.statsMu.Unlock()

	stats, exists := t.stats[fullKey]
	if !exists {
		return
	}

	stats.CurrentTier = tier
	stats.LastTransitionAt = &now
	stats.LastTransitionFrom = previousTier
	stats.TransitionCount++
	stats.ConsecutiveEvaluations = 0
}

// RecordEvaluation records a policy evaluation (for anti-thrash).
func (t *AccessTracker) RecordEvaluation(ctx context.Context, bucket, key string) {
	fullKey := bucket + "/" + key

	t.statsMu.Lock()
	defer t.statsMu.Unlock()

	stats, exists := t.stats[fullKey]
	if exists {
		stats.ConsecutiveEvaluations++
	}
}

// ResetEvaluations resets consecutive evaluations.
func (t *AccessTracker) ResetEvaluations(ctx context.Context, bucket, key string) {
	fullKey := bucket + "/" + key

	t.statsMu.Lock()
	defer t.statsMu.Unlock()

	stats, exists := t.stats[fullKey]
	if exists {
		stats.ConsecutiveEvaluations = 0
	}
}

// updateMetrics computes derived metrics from access history.
func (t *AccessTracker) updateMetrics(stats *ObjectAccessStats) {
	now := time.Now()

	// Count accesses in time windows
	stats.AccessesLast24h = 0
	stats.AccessesLast7d = 0
	stats.AccessesLast30d = 0

	cutoff24h := now.Add(-24 * time.Hour)
	cutoff7d := now.Add(-7 * 24 * time.Hour)
	cutoff30d := now.Add(-30 * 24 * time.Hour)

	for _, record := range stats.AccessHistory {
		if record.Timestamp.After(cutoff24h) {
			stats.AccessesLast24h++
		}

		if record.Timestamp.After(cutoff7d) {
			stats.AccessesLast7d++
		}

		if record.Timestamp.After(cutoff30d) {
			stats.AccessesLast30d++
		}
	}

	// Calculate average per day over last 30 days
	if len(stats.AccessHistory) > 0 {
		oldestAccess := stats.AccessHistory[0].Timestamp

		daysCovered := now.Sub(oldestAccess).Hours() / 24
		if daysCovered < 1 {
			daysCovered = 1
		}

		if daysCovered > 30 {
			daysCovered = 30
		}

		stats.AverageAccessesDay = float64(stats.AccessesLast30d) / daysCovered
	}

	// Detect access trend
	stats.AccessTrend = t.detectTrend(stats)
}

// detectTrend analyzes access history to detect patterns.
func (t *AccessTracker) detectTrend(stats *ObjectAccessStats) string {
	if len(stats.AccessHistory) < 7 {
		return "unknown"
	}

	now := time.Now()

	// Compare recent week to previous week
	recentCutoff := now.Add(-7 * 24 * time.Hour)
	previousCutoff := now.Add(-14 * 24 * time.Hour)

	recentCount := 0
	previousCount := 0

	for _, record := range stats.AccessHistory {
		if record.Timestamp.After(recentCutoff) {
			recentCount++
		} else if record.Timestamp.After(previousCutoff) {
			previousCount++
		}
	}

	// Determine trend
	if previousCount == 0 {
		if recentCount > 0 {
			return trendIncreasing
		}

		return trendStable
	}

	ratio := float64(recentCount) / float64(previousCount)

	if ratio > 1.3 {
		return trendIncreasing
	} else if ratio < 0.7 {
		return trendDeclining
	}

	return trendStable
}

// evictLRU removes the least recently used entry.
func (t *AccessTracker) evictLRU() {
	elem := t.lruList.Back()
	if elem == nil {
		return
	}

	key := elem.Value.(string)
	delete(t.stats, key)
	t.lruList.Remove(elem)
	delete(t.lruMap, key)
}

// backgroundFlush periodically flushes to persistent storage.
func (t *AccessTracker) backgroundFlush() {
	defer t.flushWg.Done()

	ticker := time.NewTicker(t.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopChan:
			// Final flush
			t.flush()
			return
		case <-ticker.C:
			t.flush()
		}
	}
}

// flush writes pending stats to persistent storage.
func (t *AccessTracker) flush() {
	if t.persistStore == nil {
		return
	}

	t.pendingFlushMu.Lock()
	toFlush := t.pendingFlush
	t.pendingFlush = make(map[string]*ObjectAccessStats)
	t.pendingFlushMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, stats := range toFlush {
		err := t.persistStore.Put(ctx, stats)
		if err != nil {
			// Re-add to pending on failure
			t.pendingFlushMu.Lock()
			t.pendingFlush[stats.Bucket+"/"+stats.Key] = stats
			t.pendingFlushMu.Unlock()
		}
	}
}

// GetHotObjects returns the most accessed objects.
func (t *AccessTracker) GetHotObjects(ctx context.Context, limit int) []*ObjectAccessStats {
	t.statsMu.RLock()
	defer t.statsMu.RUnlock()

	// Collect all stats
	all := make([]*ObjectAccessStats, 0, len(t.stats))
	for _, stats := range t.stats {
		all = append(all, stats)
	}

	// Sort by access count (descending)
	for i := range len(all) - 1 {
		for j := i + 1; j < len(all); j++ {
			if all[j].AccessCount > all[i].AccessCount {
				all[i], all[j] = all[j], all[i]
			}
		}
	}

	if len(all) > limit {
		all = all[:limit]
	}

	return all
}

// GetColdObjects returns objects not accessed recently.
func (t *AccessTracker) GetColdObjects(ctx context.Context, inactiveDays int, limit int) []*ObjectAccessStats {
	t.statsMu.RLock()
	defer t.statsMu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(inactiveDays) * 24 * time.Hour)

	var cold []*ObjectAccessStats

	for _, stats := range t.stats {
		if stats.LastAccessed.Before(cutoff) {
			cold = append(cold, stats)
		}
	}

	// Sort by last access (oldest first)
	for i := range len(cold) - 1 {
		for j := i + 1; j < len(cold); j++ {
			if cold[j].LastAccessed.Before(cold[i].LastAccessed) {
				cold[i], cold[j] = cold[j], cold[i]
			}
		}
	}

	if len(cold) > limit {
		cold = cold[:limit]
	}

	return cold
}

// GetTrendingObjects returns objects with increasing access patterns.
func (t *AccessTracker) GetTrendingObjects(ctx context.Context, limit int) []*ObjectAccessStats {
	t.statsMu.RLock()
	defer t.statsMu.RUnlock()

	var trending []*ObjectAccessStats

	for _, stats := range t.stats {
		if stats.AccessTrend == "increasing" {
			trending = append(trending, stats)
		}
	}

	// Sort by average daily accesses (descending)
	for i := range len(trending) - 1 {
		for j := i + 1; j < len(trending); j++ {
			if trending[j].AverageAccessesDay > trending[i].AverageAccessesDay {
				trending[i], trending[j] = trending[j], trending[i]
			}
		}
	}

	if len(trending) > limit {
		trending = trending[:limit]
	}

	return trending
}

// GetDecliningObjects returns objects with declining access patterns.
func (t *AccessTracker) GetDecliningObjects(ctx context.Context, limit int) []*ObjectAccessStats {
	t.statsMu.RLock()
	defer t.statsMu.RUnlock()

	var declining []*ObjectAccessStats

	for _, stats := range t.stats {
		if stats.AccessTrend == "declining" {
			declining = append(declining, stats)
		}
	}

	// Sort by average daily accesses (ascending - least accessed first)
	for i := range len(declining) - 1 {
		for j := i + 1; j < len(declining); j++ {
			if declining[j].AverageAccessesDay < declining[i].AverageAccessesDay {
				declining[i], declining[j] = declining[j], declining[i]
			}
		}
	}

	if len(declining) > limit {
		declining = declining[:limit]
	}

	return declining
}

// Stats returns tracker statistics.
func (t *AccessTracker) Stats() AccessTrackerStats {
	t.statsMu.RLock()
	defer t.statsMu.RUnlock()

	return AccessTrackerStats{
		TrackedObjects: len(t.stats),
		MaxObjects:     t.maxEntries,
	}
}

// AccessTrackerStats contains tracker statistics.
type AccessTrackerStats struct {
	TrackedObjects int `json:"trackedObjects"`
	MaxObjects     int `json:"maxObjects"`
}

// Close stops the access tracker.
func (t *AccessTracker) Close() error {
	close(t.stopChan)
	t.flushWg.Wait()

	return nil
}

// InMemoryAccessStatsStore implements AccessStatsStore for testing.
type InMemoryAccessStatsStore struct {
	data map[string]*ObjectAccessStats
	mu   sync.RWMutex
}

// NewInMemoryAccessStatsStore creates an in-memory store.
func NewInMemoryAccessStatsStore() *InMemoryAccessStatsStore {
	return &InMemoryAccessStatsStore{
		data: make(map[string]*ObjectAccessStats),
	}
}

// Get retrieves stats.
func (s *InMemoryAccessStatsStore) Get(ctx context.Context, bucket, key string) (*ObjectAccessStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats, exists := s.data[bucket+"/"+key]
	if !exists {
		//nolint:nilnil // nil,nil indicates not found (not an error condition)
		return nil, nil
	}

	return stats, nil
}

// Put stores stats.
func (s *InMemoryAccessStatsStore) Put(ctx context.Context, stats *ObjectAccessStats) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[stats.Bucket+"/"+stats.Key] = stats

	return nil
}

// Delete removes stats.
func (s *InMemoryAccessStatsStore) Delete(ctx context.Context, bucket, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, bucket+"/"+key)

	return nil
}

// List returns stats for a bucket.
func (s *InMemoryAccessStatsStore) List(ctx context.Context, bucket string) ([]*ObjectAccessStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := bucket + "/"

	var result []*ObjectAccessStats

	for key, stats := range s.data {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			result = append(result, stats)
		}
	}

	return result, nil
}
