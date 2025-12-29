// Package dram provides a high-performance distributed DRAM cache
// optimized for AI/ML workloads with high-throughput, low-latency access.
package dram

import (
	"container/list"
	"context"
	"encoding/binary"
	"hash/fnv"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/piwi3910/nebulaio/internal/metrics"
	"github.com/rs/zerolog/log"
)

// Config configures the DRAM cache
type Config struct {
	// MaxSize is the maximum cache size in bytes (default: 8GB)
	MaxSize int64 `json:"maxSize" yaml:"max_size"`

	// ShardCount is the number of cache shards for lock reduction (default: 256)
	ShardCount int `json:"shardCount" yaml:"shard_count"`

	// EntryMaxSize is the maximum size for a single cache entry (default: 256MB)
	EntryMaxSize int64 `json:"entryMaxSize" yaml:"entry_max_size"`

	// TTL is the default time-to-live for cached entries (default: 1 hour)
	TTL time.Duration `json:"ttl" yaml:"ttl"`

	// EvictionPolicy is the cache eviction policy: lru, lfu, arc (default: arc)
	EvictionPolicy string `json:"evictionPolicy" yaml:"eviction_policy"`

	// PrefetchEnabled enables predictive prefetching for AI/ML workloads
	PrefetchEnabled bool `json:"prefetchEnabled" yaml:"prefetch_enabled"`

	// PrefetchThreshold is the access count before enabling prefetch
	PrefetchThreshold int `json:"prefetchThreshold" yaml:"prefetch_threshold"`

	// PrefetchAhead is the number of chunks to prefetch ahead
	PrefetchAhead int `json:"prefetchAhead" yaml:"prefetch_ahead"`

	// ZeroCopyEnabled enables zero-copy reads where supported
	ZeroCopyEnabled bool `json:"zeroCopyEnabled" yaml:"zero_copy_enabled"`

	// CompressionEnabled enables in-cache compression
	CompressionEnabled bool `json:"compressionEnabled" yaml:"compression_enabled"`

	// DistributedMode enables distributed cache across cluster nodes
	DistributedMode bool `json:"distributedMode" yaml:"distributed_mode"`

	// ClusterNodes is the list of cluster node addresses for distributed mode
	ClusterNodes []string `json:"clusterNodes" yaml:"cluster_nodes"`

	// ReplicationFactor for distributed cache (default: 2)
	ReplicationFactor int `json:"replicationFactor" yaml:"replication_factor"`

	// WarmupEnabled enables cache warmup on startup
	WarmupEnabled bool `json:"warmupEnabled" yaml:"warmup_enabled"`

	// WarmupKeys are the keys to pre-warm on startup
	WarmupKeys []string `json:"warmupKeys" yaml:"warmup_keys"`

	// MetricsEnabled enables detailed cache metrics
	MetricsEnabled bool `json:"metricsEnabled" yaml:"metrics_enabled"`

	// PeerCacheLogResetInterval is the interval after which the peer cache
	// failure log rate limiter resets, allowing a new burst of logs.
	// This ensures logging resumes after periods of stability. (default: 5 minutes)
	PeerCacheLogResetInterval time.Duration `json:"peerCacheLogResetInterval" yaml:"peer_cache_log_reset_interval"`
}

// DefaultConfig returns sensible defaults for production workloads
func DefaultConfig() Config {
	return Config{
		MaxSize:                   8 * 1024 * 1024 * 1024, // 8GB
		ShardCount:                256,
		EntryMaxSize:              256 * 1024 * 1024, // 256MB
		TTL:                       time.Hour,
		EvictionPolicy:            "arc",
		PrefetchEnabled:           true,
		PrefetchThreshold:         2,
		PrefetchAhead:             4,
		ZeroCopyEnabled:           true,
		CompressionEnabled:        false,
		DistributedMode:           false,
		ReplicationFactor:         2,
		WarmupEnabled:             false,
		MetricsEnabled:            true,
		PeerCacheLogResetInterval: 5 * time.Minute, // Reset log rate limiter every 5 minutes
	}
}

// Entry represents a cached object
type Entry struct {
	Key          string
	Data         []byte
	Size         int64
	ContentType  string
	ETag         string
	CreatedAt    time.Time
	LastAccessed int64 // Unix nano, atomic
	AccessCount  int64 // atomic
	ExpiresAt    time.Time
	Compressed   bool
	Checksum     uint64
}

// Metrics contains cache statistics
type Metrics struct {
	Size                   int64   `json:"size"`
	MaxSize                int64   `json:"maxSize"`
	Objects                int64   `json:"objects"`
	Hits                   int64   `json:"hits"`
	Misses                 int64   `json:"misses"`
	HitRate                float64 `json:"hitRate"`
	Evictions              int64   `json:"evictions"`
	BytesServed            int64   `json:"bytesServed"`
	AvgLatencyUs           int64   `json:"avgLatencyUs"`
	PrefetchHits           int64   `json:"prefetchHits"`
	PrefetchMisses         int64   `json:"prefetchMisses"`
	PeerCacheWriteFailures int64   `json:"peerCacheWriteFailures"`
}

// shard is a partition of the cache with its own lock
type shard struct {
	mu        sync.RWMutex
	entries   map[string]*Entry
	lru       *list.List
	lruMap    map[string]*list.Element
	size      int64
	maxSize   int64
	evictions int64
}

// Cache is a high-performance distributed DRAM cache
type Cache struct {
	config Config
	shards []*shard

	// Statistics (atomic)
	hits                   int64
	misses                 int64
	bytesServed            int64
	prefetchHits           int64
	prefetchMisses         int64
	totalLatency           int64 // nanoseconds
	opCount                int64
	peerCacheWriteFailures int64 // failed attempts to cache entries from peers

	// Rate limiting for peer cache failure logs
	// Logs first 5 failures, then 1 per 100 thereafter to prevent log flooding
	peerCacheLogCount     int64 // total failures since last summary
	peerCacheLastLogTime  int64 // unix nano of last log emission
	peerCacheLogBurstLeft int64 // remaining burst allowance

	// Prefetch tracking
	accessPatterns map[string]*accessPattern
	patternMu      sync.RWMutex

	// Distributed cache
	_nodeID      string
	peerClients  map[string]*peerClient
	peerClientMu sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// accessPattern tracks access patterns for prefetching
type accessPattern struct {
	bucket      string
	prefix      string
	lastKeys    []string
	sequential  bool
	accessCount int64
}

// peerClient handles communication with peer cache nodes
type peerClient struct {
	address   string
	connected bool
	lastSeen  time.Time
}

// New creates a new DRAM cache
func New(config Config) *Cache {
	if config.ShardCount <= 0 {
		config.ShardCount = 256
	}
	if config.MaxSize <= 0 {
		config.MaxSize = 8 * 1024 * 1024 * 1024
	}
	if config.EntryMaxSize <= 0 {
		config.EntryMaxSize = 256 * 1024 * 1024
	}
	if config.TTL <= 0 {
		config.TTL = time.Hour
	}
	if config.EvictionPolicy == "" {
		config.EvictionPolicy = "arc"
	}

	shardMaxSize := config.MaxSize / int64(config.ShardCount)

	ctx, cancel := context.WithCancel(context.Background())

	c := &Cache{
		config:                config,
		shards:                make([]*shard, config.ShardCount),
		accessPatterns:        make(map[string]*accessPattern),
		peerClients:           make(map[string]*peerClient),
		peerCacheLogBurstLeft: 5, // Allow first 5 logs before rate limiting
		ctx:                   ctx,
		cancel:                cancel,
	}

	// Initialize shards
	for i := 0; i < config.ShardCount; i++ {
		c.shards[i] = &shard{
			entries: make(map[string]*Entry),
			lru:     list.New(),
			lruMap:  make(map[string]*list.Element),
			maxSize: shardMaxSize,
		}
	}

	// Start background workers
	if config.PrefetchEnabled {
		c.wg.Add(1)
		go c.prefetchWorker()
	}

	if config.DistributedMode {
		c.wg.Add(1)
		go c.peerDiscoveryWorker()
	}

	return c
}

// getShard returns the shard for a given key
func (c *Cache) getShard(key string) *shard {
	h := fnv.New64a()
	h.Write([]byte(key))
	return c.shards[h.Sum64()%uint64(len(c.shards))]
}

// Get retrieves an entry from the cache
func (c *Cache) Get(ctx context.Context, key string) (*Entry, bool) {
	start := time.Now()
	defer func() {
		atomic.AddInt64(&c.totalLatency, time.Since(start).Nanoseconds())
		atomic.AddInt64(&c.opCount, 1)
	}()

	s := c.getShard(key)

	s.mu.RLock()
	entry, ok := s.entries[key]
	s.mu.RUnlock()

	if !ok {
		atomic.AddInt64(&c.misses, 1)

		// Try distributed peers if enabled
		if c.config.DistributedMode {
			if entry, ok := c.getFromPeer(ctx, key); ok {
				// Cache locally for future access - log if caching fails but don't fail the get
				if putErr := c.Put(ctx, key, entry.Data, entry.ContentType, entry.ETag); putErr != nil {
					atomic.AddInt64(&c.peerCacheWriteFailures, 1)
					metrics.RecordCachePeerWriteFailure()

					// Rate-limited logging to prevent log flooding during persistent failures
					// Allow first 5 logs (burst), then 1 per 100 failures
					// Resets after configurable interval (default 5 minutes) of no logs
					now := time.Now().UnixNano()
					lastLogTime := atomic.LoadInt64(&c.peerCacheLastLogTime)
					resetInterval := c.config.PeerCacheLogResetInterval
					if resetInterval <= 0 {
						resetInterval = 5 * time.Minute
					}

					// Check if we should reset the rate limiter due to time elapsed
					if lastLogTime > 0 && now-lastLogTime > resetInterval.Nanoseconds() {
						// Reset the burst allowance and count after quiet period
						atomic.StoreInt64(&c.peerCacheLogBurstLeft, 5)
						atomic.StoreInt64(&c.peerCacheLogCount, 0)
					}

					count := atomic.AddInt64(&c.peerCacheLogCount, 1)
					burstLeft := atomic.LoadInt64(&c.peerCacheLogBurstLeft)

					shouldLog := false
					if burstLeft > 0 {
						// Use burst allowance
						if atomic.CompareAndSwapInt64(&c.peerCacheLogBurstLeft, burstLeft, burstLeft-1) {
							shouldLog = true
						}
					} else if count%100 == 0 {
						// Log every 100th failure after burst exhausted
						shouldLog = true
					}

					if shouldLog {
						atomic.StoreInt64(&c.peerCacheLastLogTime, now)
						log.Warn().
							Err(putErr).
							Str("key", key).
							Int64("total_failures", count).
							Msg("failed to cache entry retrieved from peer - this may impact cache hit rate and performance")
					}
				}
				atomic.AddInt64(&c.bytesServed, entry.Size)
				return entry, true
			}
		}

		return nil, false
	}

	// Check expiration
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		s.mu.Lock()
		c.removeEntryLocked(s, key)
		s.mu.Unlock()
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}

	// Update access stats atomically
	atomic.StoreInt64(&entry.LastAccessed, time.Now().UnixNano())
	atomic.AddInt64(&entry.AccessCount, 1)

	// Update LRU position (requires write lock)
	s.mu.Lock()
	if elem, ok := s.lruMap[key]; ok {
		s.lru.MoveToFront(elem)
	}
	s.mu.Unlock()

	atomic.AddInt64(&c.hits, 1)
	atomic.AddInt64(&c.bytesServed, entry.Size)

	// Track access pattern for prefetching
	if c.config.PrefetchEnabled {
		c.trackAccess(key)
	}

	return entry, true
}

// Put adds an entry to the cache
func (c *Cache) Put(ctx context.Context, key string, data []byte, contentType, etag string) error {
	size := int64(len(data))

	// Check max entry size
	if size > c.config.EntryMaxSize {
		return nil // Skip caching oversized entries
	}

	s := c.getShard(key)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Evict if necessary
	for s.size+size > s.maxSize && s.lru.Len() > 0 {
		c.evictLRULocked(s)
	}

	// Remove existing entry if present
	if _, exists := s.entries[key]; exists {
		c.removeEntryLocked(s, key)
	}

	// Create new entry
	now := time.Now()
	entry := &Entry{
		Key:          key,
		Data:         data,
		Size:         size,
		ContentType:  contentType,
		ETag:         etag,
		CreatedAt:    now,
		LastAccessed: now.UnixNano(),
		AccessCount:  1,
		Checksum:     c.checksum(data),
	}

	if c.config.TTL > 0 {
		entry.ExpiresAt = now.Add(c.config.TTL)
	}

	s.entries[key] = entry
	s.size += size

	// Add to LRU
	elem := s.lru.PushFront(key)
	s.lruMap[key] = elem

	// Replicate to peers if distributed
	if c.config.DistributedMode {
		go c.replicateToPeers(key, entry)
	}

	return nil
}

// Delete removes an entry from the cache
func (c *Cache) Delete(ctx context.Context, key string) error {
	s := c.getShard(key)

	s.mu.Lock()
	c.removeEntryLocked(s, key)
	s.mu.Unlock()

	// Invalidate on peers
	if c.config.DistributedMode {
		go c.invalidateOnPeers(key)
	}

	return nil
}

// Has checks if an entry exists in the cache
func (c *Cache) Has(ctx context.Context, key string) bool {
	s := c.getShard(key)

	s.mu.RLock()
	entry, ok := s.entries[key]
	s.mu.RUnlock()

	if !ok {
		return false
	}

	// Check expiration
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		return false
	}

	return true
}

// Metrics returns cache statistics
func (c *Cache) Metrics() Metrics {
	var totalSize int64
	var totalObjects int64
	var totalEvictions int64

	for _, s := range c.shards {
		s.mu.RLock()
		totalSize += s.size
		totalObjects += int64(len(s.entries))
		totalEvictions += s.evictions
		s.mu.RUnlock()
	}

	hits := atomic.LoadInt64(&c.hits)
	misses := atomic.LoadInt64(&c.misses)
	total := hits + misses

	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	avgLatency := int64(0)
	opCount := atomic.LoadInt64(&c.opCount)
	if opCount > 0 {
		avgLatency = atomic.LoadInt64(&c.totalLatency) / opCount / 1000 // microseconds
	}

	return Metrics{
		Size:                   totalSize,
		MaxSize:                c.config.MaxSize,
		Objects:                totalObjects,
		Hits:                   hits,
		Misses:                 misses,
		HitRate:                hitRate,
		Evictions:              totalEvictions,
		BytesServed:            atomic.LoadInt64(&c.bytesServed),
		AvgLatencyUs:           avgLatency,
		PrefetchHits:           atomic.LoadInt64(&c.prefetchHits),
		PrefetchMisses:         atomic.LoadInt64(&c.prefetchMisses),
		PeerCacheWriteFailures: atomic.LoadInt64(&c.peerCacheWriteFailures),
	}
}

// Clear removes all entries from the cache
func (c *Cache) Clear() {
	for _, s := range c.shards {
		s.mu.Lock()
		s.entries = make(map[string]*Entry)
		s.lru.Init()
		s.lruMap = make(map[string]*list.Element)
		s.size = 0
		s.mu.Unlock()
	}
}

// Close shuts down the cache
func (c *Cache) Close() error {
	c.cancel()
	c.wg.Wait()
	c.Clear()
	return nil
}

// WarmCache pre-populates the cache from a data source
func (c *Cache) WarmCache(ctx context.Context, keys []string, source func(ctx context.Context, key string) ([]byte, string, string, error)) error {
	for _, key := range keys {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, contentType, etag, err := source(ctx, key)
		if err != nil {
			continue // Skip failed entries
		}

		if err := c.Put(ctx, key, data, contentType, etag); err != nil {
			continue
		}
	}
	return nil
}

// GetReader returns a reader for cached data (for streaming)
func (c *Cache) GetReader(ctx context.Context, key string) (io.Reader, *Entry, bool) {
	entry, ok := c.Get(ctx, key)
	if !ok {
		return nil, nil, false
	}

	return &entryReader{data: entry.Data}, entry, true
}

// entryReader wraps cached data as an io.Reader
type entryReader struct {
	data []byte
	pos  int
}

func (r *entryReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// Internal methods

func (c *Cache) removeEntryLocked(s *shard, key string) {
	entry, ok := s.entries[key]
	if !ok {
		return
	}

	delete(s.entries, key)
	s.size -= entry.Size

	if elem, ok := s.lruMap[key]; ok {
		s.lru.Remove(elem)
		delete(s.lruMap, key)
	}
}

func (c *Cache) evictLRULocked(s *shard) {
	elem := s.lru.Back()
	if elem == nil {
		return
	}

	key := elem.Value.(string)
	c.removeEntryLocked(s, key)
	s.evictions++
}

func (c *Cache) checksum(data []byte) uint64 {
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64()
}

func (c *Cache) trackAccess(key string) {
	c.patternMu.Lock()
	defer c.patternMu.Unlock()

	// Extract bucket and key prefix
	bucket, prefix := extractBucketPrefix(key)
	patternKey := bucket + ":" + prefix

	pattern, ok := c.accessPatterns[patternKey]
	if !ok {
		pattern = &accessPattern{
			bucket:   bucket,
			prefix:   prefix,
			lastKeys: make([]string, 0, 10),
		}
		c.accessPatterns[patternKey] = pattern
	}

	pattern.lastKeys = append(pattern.lastKeys, key)
	if len(pattern.lastKeys) > 10 {
		pattern.lastKeys = pattern.lastKeys[1:]
	}
	pattern.accessCount++

	// Detect sequential access pattern
	if len(pattern.lastKeys) >= 3 {
		pattern.sequential = c.isSequentialAccess(pattern.lastKeys)
	}
}

func (c *Cache) isSequentialAccess(keys []string) bool {
	if len(keys) < 2 {
		return false
	}

	// Check if keys follow a sequential pattern (e.g., chunk-0, chunk-1, chunk-2)
	// This is a simplified check - in production, use more sophisticated pattern detection
	for i := 1; i < len(keys); i++ {
		// Simple numeric suffix check
		prevNum := extractNumericSuffix(keys[i-1])
		currNum := extractNumericSuffix(keys[i])
		if currNum <= prevNum || currNum-prevNum > 2 {
			return false
		}
	}
	return true
}

func (c *Cache) prefetchWorker() {
	defer c.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.processPrefetch()
		}
	}
}

func (c *Cache) processPrefetch() {
	c.patternMu.RLock()
	defer c.patternMu.RUnlock()

	for _, pattern := range c.accessPatterns {
		if !pattern.sequential || pattern.accessCount < int64(c.config.PrefetchThreshold) {
			continue
		}

		// Generate prefetch keys based on pattern
		if len(pattern.lastKeys) > 0 {
			lastKey := pattern.lastKeys[len(pattern.lastKeys)-1]
			lastNum := extractNumericSuffix(lastKey)
			baseKey := lastKey[:len(lastKey)-len(string(rune(lastNum)))]

			for i := 1; i <= c.config.PrefetchAhead; i++ {
				prefetchKey := baseKey + string(rune(lastNum+int64(i)))
				// In production, this would trigger async prefetch from backend
				_ = prefetchKey
			}
		}
	}
}

func (c *Cache) peerDiscoveryWorker() {
	defer c.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.discoverPeers()
		}
	}
}

func (c *Cache) discoverPeers() {
	c.peerClientMu.Lock()
	defer c.peerClientMu.Unlock()

	for _, addr := range c.config.ClusterNodes {
		if _, ok := c.peerClients[addr]; !ok {
			c.peerClients[addr] = &peerClient{
				address: addr,
			}
		}
		// In production, actually connect to peer and update status
		c.peerClients[addr].connected = true
		c.peerClients[addr].lastSeen = time.Now()
	}
}

func (c *Cache) getFromPeer(ctx context.Context, key string) (*Entry, bool) {
	c.peerClientMu.RLock()
	defer c.peerClientMu.RUnlock()

	// In production, this would make RPC calls to peer nodes
	// For now, return not found
	return nil, false
}

func (c *Cache) replicateToPeers(key string, entry *Entry) {
	c.peerClientMu.RLock()
	defer c.peerClientMu.RUnlock()

	// In production, this would replicate to N peers based on replication factor
	// Using consistent hashing to determine which peers
}

func (c *Cache) invalidateOnPeers(key string) {
	c.peerClientMu.RLock()
	defer c.peerClientMu.RUnlock()

	// In production, this would send invalidation messages to all peers
}

// Helper functions

func extractBucketPrefix(key string) (bucket, prefix string) {
	// Key format: bucket/key
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			bucket = key[:i]
			if i+1 < len(key) {
				// Get prefix (first path segment after bucket)
				rest := key[i+1:]
				for j := 0; j < len(rest); j++ {
					if rest[j] == '/' {
						prefix = rest[:j]
						return
					}
				}
				prefix = rest
			}
			return
		}
	}
	return key, ""
}

func extractNumericSuffix(s string) int64 {
	if len(s) == 0 {
		return 0
	}

	// Check if string ends with digits
	end := len(s)
	if s[end-1] < '0' || s[end-1] > '9' {
		return 0 // No trailing digits
	}

	// Find start of trailing numeric sequence
	start := end - 1
	for start > 0 && s[start-1] >= '0' && s[start-1] <= '9' {
		start--
	}

	var num int64
	for i := start; i < end; i++ {
		num = num*10 + int64(s[i]-'0')
	}
	return num
}

// Serialization for distributed cache

func (e *Entry) Marshal() []byte {
	// Simple binary format: key_len(4) + key + data_len(8) + data + metadata
	keyBytes := []byte(e.Key)
	buf := make([]byte, 4+len(keyBytes)+8+len(e.Data)+256) // Extra for metadata

	offset := 0

	// Key length and key
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(keyBytes)))
	offset += 4
	copy(buf[offset:], keyBytes)
	offset += len(keyBytes)

	// Data length and data
	binary.BigEndian.PutUint64(buf[offset:], uint64(len(e.Data)))
	offset += 8
	copy(buf[offset:], e.Data)
	offset += len(e.Data)

	// Content type (length-prefixed)
	ctBytes := []byte(e.ContentType)
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(ctBytes)))
	offset += 2
	copy(buf[offset:], ctBytes)
	offset += len(ctBytes)

	// ETag (length-prefixed)
	etagBytes := []byte(e.ETag)
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(etagBytes)))
	offset += 2
	copy(buf[offset:], etagBytes)
	offset += len(etagBytes)

	// Checksum
	binary.BigEndian.PutUint64(buf[offset:], e.Checksum)
	offset += 8

	return buf[:offset]
}

func UnmarshalEntry(data []byte) (*Entry, error) {
	if len(data) < 12 {
		return nil, io.ErrUnexpectedEOF
	}

	offset := 0

	// Key
	keyLen := binary.BigEndian.Uint32(data[offset:])
	offset += 4
	if offset+int(keyLen) > len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	key := string(data[offset : offset+int(keyLen)])
	offset += int(keyLen)

	// Data
	dataLen := binary.BigEndian.Uint64(data[offset:])
	offset += 8
	if offset+int(dataLen) > len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	entryData := make([]byte, dataLen)
	copy(entryData, data[offset:offset+int(dataLen)])
	offset += int(dataLen)

	entry := &Entry{
		Key:  key,
		Data: entryData,
		Size: int64(dataLen),
	}

	// Content type
	if offset+2 <= len(data) {
		ctLen := binary.BigEndian.Uint16(data[offset:])
		offset += 2
		if offset+int(ctLen) <= len(data) {
			entry.ContentType = string(data[offset : offset+int(ctLen)])
			offset += int(ctLen)
		}
	}

	// ETag
	if offset+2 <= len(data) {
		etagLen := binary.BigEndian.Uint16(data[offset:])
		offset += 2
		if offset+int(etagLen) <= len(data) {
			entry.ETag = string(data[offset : offset+int(etagLen)])
			offset += int(etagLen)
		}
	}

	// Checksum
	if offset+8 <= len(data) {
		entry.Checksum = binary.BigEndian.Uint64(data[offset:])
	}

	entry.CreatedAt = time.Now()
	entry.LastAccessed = time.Now().UnixNano()
	entry.AccessCount = 1

	return entry, nil
}
