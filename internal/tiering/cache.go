package tiering

import (
	"bytes"
	"container/list"
	"context"
	"io"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// CacheConfig configures the hot cache layer
type CacheConfig struct {
	// MaxSize is the maximum cache size in bytes
	MaxSize int64 `json:"maxSize" yaml:"maxSize"`

	// MaxObjects is the maximum number of objects in cache
	MaxObjects int `json:"maxObjects,omitempty" yaml:"maxObjects,omitempty"`

	// TTL is the default time-to-live for cached objects
	TTL time.Duration `json:"ttl,omitempty" yaml:"ttl,omitempty"`

	// EvictionPolicy determines how objects are evicted (lru, lfu, fifo)
	EvictionPolicy string `json:"evictionPolicy,omitempty" yaml:"evictionPolicy,omitempty"`

	// WriteThrough writes to both cache and backend simultaneously
	WriteThrough bool `json:"writeThrough,omitempty" yaml:"writeThrough,omitempty"`

	// WriteBack writes to cache first, then asynchronously to backend
	WriteBack bool `json:"writeBack,omitempty" yaml:"writeBack,omitempty"`

	// ReadAhead prefetches objects predicted to be accessed
	ReadAhead bool `json:"readAhead,omitempty" yaml:"readAhead,omitempty"`

	// ReadAheadThreshold is the access count after which to prefetch
	ReadAheadThreshold int `json:"readAheadThreshold,omitempty" yaml:"readAheadThreshold,omitempty"`
}

// DefaultCacheConfig returns sensible cache defaults
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxSize:            10 * 1024 * 1024 * 1024, // 10GB
		MaxObjects:         100000,
		TTL:                24 * time.Hour,
		EvictionPolicy:     "lru",
		WriteThrough:       true,
		WriteBack:          false,
		ReadAhead:          true,
		ReadAheadThreshold: 3,
	}
}

// CacheEntry represents a cached object
type CacheEntry struct {
	Key          string
	Size         int64
	Data         []byte
	ContentType  string
	ETag         string
	CreatedAt    time.Time
	LastAccessed time.Time
	AccessCount  int
	ExpiresAt    time.Time
}

// Cache provides an LRU cache for hot objects
type Cache struct {
	config CacheConfig
	mu     sync.RWMutex

	// Storage for cached data
	entries map[string]*CacheEntry
	lruList *list.List
	lruMap  map[string]*list.Element

	// Statistics
	currentSize int64
	hits        int64
	misses      int64
	evictions   int64

	// Background cache backend (optional)
	backend backend.Backend

	// Write-back queue
	writeBackQueue chan writeBackItem
	writeBackWg    sync.WaitGroup
	closed         bool
}

type writeBackItem struct {
	bucket string
	key    string
	data   []byte
}

// NewCache creates a new cache
func NewCache(config CacheConfig, cacheBackend backend.Backend) *Cache {
	c := &Cache{
		config:         config,
		entries:        make(map[string]*CacheEntry),
		lruList:        list.New(),
		lruMap:         make(map[string]*list.Element),
		backend:        cacheBackend,
		writeBackQueue: make(chan writeBackItem, 1000),
	}

	// Start write-back worker if enabled
	if config.WriteBack && cacheBackend != nil {
		c.writeBackWg.Add(1)
		go c.writeBackWorker()
	}

	return c
}

// Get retrieves an object from cache
func (c *Cache) Get(ctx context.Context, key string) (*CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}

	// Check expiration
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		c.removeEntry(key)
		c.misses++
		return nil, false
	}

	// Update access info
	entry.LastAccessed = time.Now()
	entry.AccessCount++

	// Move to front of LRU list
	if elem, ok := c.lruMap[key]; ok {
		c.lruList.MoveToFront(elem)
	}

	c.hits++
	return entry, true
}

// Put adds an object to the cache
func (c *Cache) Put(ctx context.Context, key string, data []byte, contentType, etag string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	size := int64(len(data))

	// Check if we need to evict
	for c.currentSize+size > c.config.MaxSize || len(c.entries) >= c.config.MaxObjects {
		if c.lruList.Len() == 0 {
			break
		}
		c.evictLRU()
	}

	// Remove existing entry if present
	if _, exists := c.entries[key]; exists {
		c.removeEntry(key)
	}

	// Create new entry
	entry := &CacheEntry{
		Key:          key,
		Size:         size,
		Data:         data,
		ContentType:  contentType,
		ETag:         etag,
		CreatedAt:    time.Now(),
		LastAccessed: time.Now(),
		AccessCount:  1,
	}

	if c.config.TTL > 0 {
		entry.ExpiresAt = time.Now().Add(c.config.TTL)
	}

	c.entries[key] = entry
	c.currentSize += size

	// Add to LRU list
	elem := c.lruList.PushFront(key)
	c.lruMap[key] = elem

	// Write-back to persistent cache if configured
	if c.config.WriteBack && c.backend != nil && !c.closed {
		// Parse bucket/key
		parts := splitFirst(key, "/")
		if len(parts) == 2 {
			select {
			case c.writeBackQueue <- writeBackItem{bucket: parts[0], key: parts[1], data: data}:
			default:
				// Queue full, skip write-back
			}
		}
	}

	return nil
}

// Delete removes an object from cache
func (c *Cache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.removeEntry(key)
	return nil
}

// Has checks if an object is in cache
func (c *Cache) Has(ctx context.Context, key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return false
	}

	// Check expiration
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		return false
	}

	return true
}

// Size returns the current cache size in bytes
func (c *Cache) Size() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSize
}

// Count returns the number of objects in cache
func (c *Cache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Stats returns cache statistics
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hitRate := float64(0)
	total := c.hits + c.misses
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return CacheStats{
		Size:       c.currentSize,
		MaxSize:    c.config.MaxSize,
		Objects:    len(c.entries),
		MaxObjects: c.config.MaxObjects,
		Hits:       c.hits,
		Misses:     c.misses,
		HitRate:    hitRate,
		Evictions:  c.evictions,
	}
}

// CacheStats contains cache statistics
type CacheStats struct {
	Size       int64   `json:"size"`
	MaxSize    int64   `json:"maxSize"`
	Objects    int     `json:"objects"`
	MaxObjects int     `json:"maxObjects"`
	Hits       int64   `json:"hits"`
	Misses     int64   `json:"misses"`
	HitRate    float64 `json:"hitRate"`
	Evictions  int64   `json:"evictions"`
}

// Clear removes all objects from cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
	c.lruList.Init()
	c.lruMap = make(map[string]*list.Element)
	c.currentSize = 0
}

// Close closes the cache and releases resources
func (c *Cache) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	// Wait for write-back to complete
	close(c.writeBackQueue)
	c.writeBackWg.Wait()

	return nil
}

// removeEntry removes an entry without locking
func (c *Cache) removeEntry(key string) {
	entry, ok := c.entries[key]
	if !ok {
		return
	}

	delete(c.entries, key)
	c.currentSize -= entry.Size

	if elem, ok := c.lruMap[key]; ok {
		c.lruList.Remove(elem)
		delete(c.lruMap, key)
	}
}

// evictLRU removes the least recently used entry
func (c *Cache) evictLRU() {
	elem := c.lruList.Back()
	if elem == nil {
		return
	}

	key := elem.Value.(string)
	c.removeEntry(key)
	c.evictions++
}

// writeBackWorker persists cached data to the backend
func (c *Cache) writeBackWorker() {
	defer c.writeBackWg.Done()

	for item := range c.writeBackQueue {
		if c.backend == nil {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = c.backend.PutObject(ctx, item.bucket, item.key, bytes.NewReader(item.data), int64(len(item.data)))
		cancel()
	}
}

// bytesReader wraps a byte slice as an io.Reader
type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// Prefetch loads an object into cache from the backend
func (c *Cache) Prefetch(ctx context.Context, bucket, key string, source backend.Backend) error {
	cacheKey := bucket + "/" + key

	// Skip if already cached
	if c.Has(ctx, cacheKey) {
		return nil
	}

	// Get from source
	reader, err := source.GetObject(ctx, bucket, key)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	// Read data
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	// Add to cache
	return c.Put(ctx, cacheKey, data, "", "")
}

// WarmCache pre-populates the cache with frequently accessed objects
func (c *Cache) WarmCache(ctx context.Context, keys []string, source backend.Backend) error {
	for _, key := range keys {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Parse bucket/key from combined key
		parts := splitFirst(key, "/")
		if len(parts) != 2 {
			continue
		}

		if err := c.Prefetch(ctx, parts[0], parts[1], source); err != nil {
			// Log error but continue
			continue
		}
	}
	return nil
}

// splitFirst splits a string on the first occurrence of sep
func splitFirst(s, sep string) []string {
	idx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == sep[0] {
			idx = i
			break
		}
	}
	if idx == -1 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+1:]}
}
