// Package firewall provides an S3-aware data firewall with QoS, rate limiting,
// bandwidth throttling, and enhanced access control.
package firewall

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Config configures the data firewall
type Config struct {
	// Enabled enables the firewall
	Enabled bool `json:"enabled" yaml:"enabled"`

	// DefaultPolicy is the default action: allow, deny
	DefaultPolicy string `json:"defaultPolicy" yaml:"default_policy"`

	// RateLimiting configures request rate limiting
	RateLimiting RateLimitConfig `json:"rateLimiting" yaml:"rate_limiting"`

	// Bandwidth configures bandwidth throttling
	Bandwidth BandwidthConfig `json:"bandwidth" yaml:"bandwidth"`

	// Connections configures connection limits
	Connections ConnectionConfig `json:"connections" yaml:"connections"`

	// Rules are firewall rules evaluated in order
	Rules []Rule `json:"rules" yaml:"rules"`

	// IPAllowlist is a list of allowed IP addresses/CIDRs
	IPAllowlist []string `json:"ipAllowlist" yaml:"ip_allowlist"`

	// IPBlocklist is a list of blocked IP addresses/CIDRs
	IPBlocklist []string `json:"ipBlocklist" yaml:"ip_blocklist"`

	// AuditEnabled enables firewall audit logging
	AuditEnabled bool `json:"auditEnabled" yaml:"audit_enabled"`
}

// RateLimitConfig configures rate limiting
type RateLimitConfig struct {
	// Enabled enables rate limiting
	Enabled bool `json:"enabled" yaml:"enabled"`

	// RequestsPerSecond is the default requests per second limit
	RequestsPerSecond int `json:"requestsPerSecond" yaml:"requests_per_second"`

	// BurstSize is the maximum burst size
	BurstSize int `json:"burstSize" yaml:"burst_size"`

	// PerUser enables per-user rate limiting
	PerUser bool `json:"perUser" yaml:"per_user"`

	// PerIP enables per-IP rate limiting
	PerIP bool `json:"perIP" yaml:"per_ip"`

	// PerBucket enables per-bucket rate limiting
	PerBucket bool `json:"perBucket" yaml:"per_bucket"`

	// UserLimits are per-user rate limit overrides
	UserLimits map[string]int `json:"userLimits" yaml:"user_limits"`

	// BucketLimits are per-bucket rate limit overrides
	BucketLimits map[string]int `json:"bucketLimits" yaml:"bucket_limits"`
}

// BandwidthConfig configures bandwidth throttling
type BandwidthConfig struct {
	// Enabled enables bandwidth throttling
	Enabled bool `json:"enabled" yaml:"enabled"`

	// MaxBytesPerSecond is the global max bandwidth in bytes/second
	MaxBytesPerSecond int64 `json:"maxBytesPerSecond" yaml:"max_bytes_per_second"`

	// MaxBytesPerSecondPerUser is per-user bandwidth limit
	MaxBytesPerSecondPerUser int64 `json:"maxBytesPerSecondPerUser" yaml:"max_bytes_per_second_per_user"`

	// MaxBytesPerSecondPerBucket is per-bucket bandwidth limit
	MaxBytesPerSecondPerBucket int64 `json:"maxBytesPerSecondPerBucket" yaml:"max_bytes_per_second_per_bucket"`

	// UserLimits are per-user bandwidth overrides
	UserLimits map[string]int64 `json:"userLimits" yaml:"user_limits"`

	// BucketLimits are per-bucket bandwidth overrides
	BucketLimits map[string]int64 `json:"bucketLimits" yaml:"bucket_limits"`
}

// ConnectionConfig configures connection limits
type ConnectionConfig struct {
	// Enabled enables connection limiting
	Enabled bool `json:"enabled" yaml:"enabled"`

	// MaxConnections is the global max concurrent connections
	MaxConnections int `json:"maxConnections" yaml:"max_connections"`

	// MaxConnectionsPerIP is per-IP connection limit
	MaxConnectionsPerIP int `json:"maxConnectionsPerIP" yaml:"max_connections_per_ip"`

	// MaxConnectionsPerUser is per-user connection limit
	MaxConnectionsPerUser int `json:"maxConnectionsPerUser" yaml:"max_connections_per_user"`

	// IdleTimeout is the idle connection timeout
	IdleTimeout time.Duration `json:"idleTimeout" yaml:"idle_timeout"`
}

// Rule represents a firewall rule
type Rule struct {
	// ID is the unique rule identifier
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable rule name
	Name string `json:"name" yaml:"name"`

	// Priority determines rule evaluation order (lower = higher priority)
	Priority int `json:"priority" yaml:"priority"`

	// Enabled enables/disables the rule
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Action is the rule action: allow, deny, throttle
	Action string `json:"action" yaml:"action"`

	// Match contains matching criteria
	Match RuleMatch `json:"match" yaml:"match"`

	// RateLimit is the rate limit to apply when action is "throttle"
	RateLimit *int `json:"rateLimit,omitempty" yaml:"rate_limit,omitempty"`

	// BandwidthLimit is the bandwidth limit in bytes/second
	BandwidthLimit *int64 `json:"bandwidthLimit,omitempty" yaml:"bandwidth_limit,omitempty"`
}

// RuleMatch contains criteria for matching requests
type RuleMatch struct {
	// SourceIPs are source IP addresses/CIDRs to match
	SourceIPs []string `json:"sourceIPs,omitempty" yaml:"source_ips,omitempty"`

	// Users are user names to match
	Users []string `json:"users,omitempty" yaml:"users,omitempty"`

	// Buckets are bucket names to match (supports wildcards)
	Buckets []string `json:"buckets,omitempty" yaml:"buckets,omitempty"`

	// Operations are S3 operations to match (GetObject, PutObject, etc.)
	Operations []string `json:"operations,omitempty" yaml:"operations,omitempty"`

	// KeyPrefixes are object key prefixes to match
	KeyPrefixes []string `json:"keyPrefixes,omitempty" yaml:"key_prefixes,omitempty"`

	// ContentTypes are content types to match
	ContentTypes []string `json:"contentTypes,omitempty" yaml:"content_types,omitempty"`

	// MinSize is the minimum object size to match
	MinSize int64 `json:"minSize,omitempty" yaml:"min_size,omitempty"`

	// MaxSize is the maximum object size to match
	MaxSize int64 `json:"maxSize,omitempty" yaml:"max_size,omitempty"`

	// TimeWindow restricts the rule to certain hours
	TimeWindow *TimeWindow `json:"timeWindow,omitempty" yaml:"time_window,omitempty"`
}

// TimeWindow defines time-based rule matching
type TimeWindow struct {
	// StartHour is the start hour (0-23)
	StartHour int `json:"startHour" yaml:"start_hour"`

	// EndHour is the end hour (0-23)
	EndHour int `json:"endHour" yaml:"end_hour"`

	// DaysOfWeek are days when rule applies (0=Sunday, 6=Saturday)
	DaysOfWeek []int `json:"daysOfWeek,omitempty" yaml:"days_of_week,omitempty"`

	// Timezone is the timezone for time matching
	Timezone string `json:"timezone" yaml:"timezone"`
}

// Request represents an incoming S3 request for firewall evaluation
type Request struct {
	SourceIP    string
	User        string
	Bucket      string
	Key         string
	Operation   string
	ContentType string
	Size        int64
	Timestamp   time.Time
}

// Decision represents a firewall decision
type Decision struct {
	Allowed        bool
	Rule           *Rule
	RateLimit      int     // Requests per second (0 = no limit)
	BandwidthLimit int64   // Bytes per second (0 = no limit)
	Reason         string
}

// Firewall is the main data firewall
type Firewall struct {
	config Config
	mu     sync.RWMutex

	// Rate limiters
	globalLimiter    *TokenBucket
	userLimiters     map[string]*TokenBucket
	ipLimiters       map[string]*TokenBucket
	bucketLimiters   map[string]*TokenBucket
	limiterMu        sync.RWMutex

	// Bandwidth trackers
	globalBandwidth  *BandwidthTracker
	userBandwidth    map[string]*BandwidthTracker
	bucketBandwidth  map[string]*BandwidthTracker
	bandwidthMu      sync.RWMutex

	// Connection counters
	globalConnections int64
	ipConnections     map[string]int64
	userConnections   map[string]int64
	connectionMu      sync.RWMutex

	// IP networks (parsed allowlist/blocklist)
	allowedNets []*net.IPNet
	blockedNets []*net.IPNet

	// Statistics
	stats FirewallStats
}

// FirewallStats contains firewall statistics
type FirewallStats struct {
	RequestsAllowed  int64 `json:"requestsAllowed"`
	RequestsDenied   int64 `json:"requestsDenied"`
	RequestsThrottled int64 `json:"requestsThrottled"`
	RateLimitHits    int64 `json:"rateLimitHits"`
	BandwidthLimitHits int64 `json:"bandwidthLimitHits"`
	ConnectionLimitHits int64 `json:"connectionLimitHits"`
	RulesEvaluated   int64 `json:"rulesEvaluated"`
}

// DefaultConfig returns sensible firewall defaults
func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		DefaultPolicy: "allow",
		RateLimiting: RateLimitConfig{
			Enabled:           false,
			RequestsPerSecond: 1000,
			BurstSize:         100,
			PerUser:           true,
			PerIP:             true,
			PerBucket:         false,
		},
		Bandwidth: BandwidthConfig{
			Enabled:                    false,
			MaxBytesPerSecond:          1024 * 1024 * 1024, // 1 GB/s
			MaxBytesPerSecondPerUser:   100 * 1024 * 1024,  // 100 MB/s
			MaxBytesPerSecondPerBucket: 500 * 1024 * 1024,  // 500 MB/s
		},
		Connections: ConnectionConfig{
			Enabled:               false,
			MaxConnections:        10000,
			MaxConnectionsPerIP:   100,
			MaxConnectionsPerUser: 500,
			IdleTimeout:           60 * time.Second,
		},
		AuditEnabled: true,
	}
}

// New creates a new firewall
func New(config Config) (*Firewall, error) {
	fw := &Firewall{
		config:          config,
		userLimiters:    make(map[string]*TokenBucket),
		ipLimiters:      make(map[string]*TokenBucket),
		bucketLimiters:  make(map[string]*TokenBucket),
		userBandwidth:   make(map[string]*BandwidthTracker),
		bucketBandwidth: make(map[string]*BandwidthTracker),
		ipConnections:   make(map[string]int64),
		userConnections: make(map[string]int64),
	}

	// Parse IP allowlist - collect invalid entries for summary logging
	var invalidAllowlistEntries []string
	for _, cidr := range config.IPAllowlist {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			ip := net.ParseIP(cidr)
			if ip == nil {
				invalidAllowlistEntries = append(invalidAllowlistEntries, cidr)
				continue
			}
			var parseErr error
			if ip.To4() != nil {
				_, network, parseErr = net.ParseCIDR(cidr + "/32")
			} else {
				_, network, parseErr = net.ParseCIDR(cidr + "/128")
			}
			if parseErr != nil {
				invalidAllowlistEntries = append(invalidAllowlistEntries, cidr)
				continue
			}
		}
		if network != nil {
			fw.allowedNets = append(fw.allowedNets, network)
		}
	}
	if len(invalidAllowlistEntries) > 0 {
		log.Warn().
			Int("count", len(invalidAllowlistEntries)).
			Strs("entries", invalidAllowlistEntries).
			Msg("skipped invalid IP allowlist entries - check firewall configuration")
	}

	// Parse IP blocklist - collect invalid entries for summary logging
	var invalidBlocklistEntries []string
	for _, cidr := range config.IPBlocklist {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			ip := net.ParseIP(cidr)
			if ip == nil {
				invalidBlocklistEntries = append(invalidBlocklistEntries, cidr)
				continue
			}
			var parseErr error
			if ip.To4() != nil {
				_, network, parseErr = net.ParseCIDR(cidr + "/32")
			} else {
				_, network, parseErr = net.ParseCIDR(cidr + "/128")
			}
			if parseErr != nil {
				invalidBlocklistEntries = append(invalidBlocklistEntries, cidr)
				continue
			}
		}
		if network != nil {
			fw.blockedNets = append(fw.blockedNets, network)
		}
	}
	if len(invalidBlocklistEntries) > 0 {
		log.Warn().
			Int("count", len(invalidBlocklistEntries)).
			Strs("entries", invalidBlocklistEntries).
			Msg("skipped invalid IP blocklist entries - check firewall configuration")
	}

	// Initialize global rate limiter
	if config.RateLimiting.Enabled {
		fw.globalLimiter = NewTokenBucket(
			config.RateLimiting.RequestsPerSecond,
			config.RateLimiting.BurstSize,
		)
	}

	// Initialize global bandwidth tracker
	if config.Bandwidth.Enabled {
		fw.globalBandwidth = NewBandwidthTracker(config.Bandwidth.MaxBytesPerSecond)
	}

	return fw, nil
}

// Evaluate evaluates a request against the firewall
func (fw *Firewall) Evaluate(ctx context.Context, req *Request) *Decision {
	if !fw.config.Enabled {
		return &Decision{Allowed: true}
	}

	atomic.AddInt64(&fw.stats.RulesEvaluated, 1)

	// Check IP blocklist first
	if fw.isIPBlocked(req.SourceIP) {
		atomic.AddInt64(&fw.stats.RequestsDenied, 1)
		return &Decision{
			Allowed: false,
			Reason:  "IP address is blocked",
		}
	}

	// Check IP allowlist
	if len(fw.allowedNets) > 0 && !fw.isIPAllowed(req.SourceIP) {
		atomic.AddInt64(&fw.stats.RequestsDenied, 1)
		return &Decision{
			Allowed: false,
			Reason:  "IP address is not in allowlist",
		}
	}

	// Check connection limits
	if fw.config.Connections.Enabled {
		if !fw.checkConnectionLimits(req) {
			atomic.AddInt64(&fw.stats.ConnectionLimitHits, 1)
			atomic.AddInt64(&fw.stats.RequestsDenied, 1)
			return &Decision{
				Allowed: false,
				Reason:  "Connection limit exceeded",
			}
		}
	}

	// Evaluate rules
	for _, rule := range fw.config.Rules {
		if !rule.Enabled {
			continue
		}

		if fw.matchRule(&rule, req) {
			switch rule.Action {
			case "deny":
				atomic.AddInt64(&fw.stats.RequestsDenied, 1)
				return &Decision{
					Allowed: false,
					Rule:    &rule,
					Reason:  "Denied by rule: " + rule.Name,
				}
			case "allow":
				atomic.AddInt64(&fw.stats.RequestsAllowed, 1)
				return &Decision{
					Allowed: true,
					Rule:    &rule,
				}
			case "throttle":
				atomic.AddInt64(&fw.stats.RequestsThrottled, 1)
				decision := &Decision{
					Allowed: true,
					Rule:    &rule,
				}
				if rule.RateLimit != nil {
					decision.RateLimit = *rule.RateLimit
				}
				if rule.BandwidthLimit != nil {
					decision.BandwidthLimit = *rule.BandwidthLimit
				}
				return decision
			}
		}
	}

	// Check rate limits
	if fw.config.RateLimiting.Enabled {
		if !fw.checkRateLimits(req) {
			atomic.AddInt64(&fw.stats.RateLimitHits, 1)
			atomic.AddInt64(&fw.stats.RequestsDenied, 1)
			return &Decision{
				Allowed: false,
				Reason:  "Rate limit exceeded",
			}
		}
	}

	// Apply default policy
	if fw.config.DefaultPolicy == "deny" {
		atomic.AddInt64(&fw.stats.RequestsDenied, 1)
		return &Decision{
			Allowed: false,
			Reason:  "Default policy is deny",
		}
	}

	atomic.AddInt64(&fw.stats.RequestsAllowed, 1)
	return &Decision{Allowed: true}
}

// CheckBandwidth checks and consumes bandwidth
func (fw *Firewall) CheckBandwidth(user, bucket string, bytes int64) bool {
	if !fw.config.Bandwidth.Enabled {
		return true
	}

	// Check global bandwidth
	if fw.globalBandwidth != nil && !fw.globalBandwidth.TryConsume(bytes) {
		atomic.AddInt64(&fw.stats.BandwidthLimitHits, 1)
		return false
	}

	// Check per-user bandwidth
	if fw.config.Bandwidth.MaxBytesPerSecondPerUser > 0 && user != "" {
		tracker := fw.getOrCreateUserBandwidth(user)
		if !tracker.TryConsume(bytes) {
			atomic.AddInt64(&fw.stats.BandwidthLimitHits, 1)
			return false
		}
	}

	// Check per-bucket bandwidth
	if fw.config.Bandwidth.MaxBytesPerSecondPerBucket > 0 && bucket != "" {
		tracker := fw.getOrCreateBucketBandwidth(bucket)
		if !tracker.TryConsume(bytes) {
			atomic.AddInt64(&fw.stats.BandwidthLimitHits, 1)
			return false
		}
	}

	return true
}

// Stats returns firewall statistics
func (fw *Firewall) Stats() FirewallStats {
	return FirewallStats{
		RequestsAllowed:    atomic.LoadInt64(&fw.stats.RequestsAllowed),
		RequestsDenied:     atomic.LoadInt64(&fw.stats.RequestsDenied),
		RequestsThrottled:  atomic.LoadInt64(&fw.stats.RequestsThrottled),
		RateLimitHits:      atomic.LoadInt64(&fw.stats.RateLimitHits),
		BandwidthLimitHits: atomic.LoadInt64(&fw.stats.BandwidthLimitHits),
		ConnectionLimitHits: atomic.LoadInt64(&fw.stats.ConnectionLimitHits),
		RulesEvaluated:     atomic.LoadInt64(&fw.stats.RulesEvaluated),
	}
}

// Internal methods

func (fw *Firewall) isIPBlocked(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	for _, network := range fw.blockedNets {
		if network.Contains(parsedIP) {
			return true
		}
	}
	return false
}

func (fw *Firewall) isIPAllowed(ip string) bool {
	if len(fw.allowedNets) == 0 {
		return true
	}
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	for _, network := range fw.allowedNets {
		if network.Contains(parsedIP) {
			return true
		}
	}
	return false
}

func (fw *Firewall) checkConnectionLimits(req *Request) bool {
	fw.connectionMu.RLock()
	defer fw.connectionMu.RUnlock()

	// Check global connections
	if atomic.LoadInt64(&fw.globalConnections) >= int64(fw.config.Connections.MaxConnections) {
		return false
	}

	// Check per-IP connections
	if fw.config.Connections.MaxConnectionsPerIP > 0 {
		if fw.ipConnections[req.SourceIP] >= int64(fw.config.Connections.MaxConnectionsPerIP) {
			return false
		}
	}

	// Check per-user connections
	if fw.config.Connections.MaxConnectionsPerUser > 0 && req.User != "" {
		if fw.userConnections[req.User] >= int64(fw.config.Connections.MaxConnectionsPerUser) {
			return false
		}
	}

	return true
}

func (fw *Firewall) checkRateLimits(req *Request) bool {
	// Check global rate limit
	if fw.globalLimiter != nil && !fw.globalLimiter.Allow() {
		return false
	}

	// Check per-IP rate limit
	if fw.config.RateLimiting.PerIP && req.SourceIP != "" {
		limiter := fw.getOrCreateIPLimiter(req.SourceIP)
		if !limiter.Allow() {
			return false
		}
	}

	// Check per-user rate limit
	if fw.config.RateLimiting.PerUser && req.User != "" {
		limiter := fw.getOrCreateUserLimiter(req.User)
		if !limiter.Allow() {
			return false
		}
	}

	// Check per-bucket rate limit
	if fw.config.RateLimiting.PerBucket && req.Bucket != "" {
		limiter := fw.getOrCreateBucketLimiter(req.Bucket)
		if !limiter.Allow() {
			return false
		}
	}

	return true
}

func (fw *Firewall) matchRule(rule *Rule, req *Request) bool {
	match := &rule.Match

	// Check source IPs
	if len(match.SourceIPs) > 0 {
		found := false
		reqIP := net.ParseIP(req.SourceIP)
		for _, cidr := range match.SourceIPs {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				if net.ParseIP(cidr).Equal(reqIP) {
					found = true
					break
				}
				continue
			}
			if network.Contains(reqIP) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check users
	if len(match.Users) > 0 && !contains(match.Users, req.User) {
		return false
	}

	// Check buckets
	if len(match.Buckets) > 0 && !matchWildcard(match.Buckets, req.Bucket) {
		return false
	}

	// Check operations
	if len(match.Operations) > 0 && !contains(match.Operations, req.Operation) {
		return false
	}

	// Check key prefixes
	if len(match.KeyPrefixes) > 0 {
		found := false
		for _, prefix := range match.KeyPrefixes {
			if len(req.Key) >= len(prefix) && req.Key[:len(prefix)] == prefix {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check content types
	if len(match.ContentTypes) > 0 && !contains(match.ContentTypes, req.ContentType) {
		return false
	}

	// Check size constraints
	if match.MinSize > 0 && req.Size < match.MinSize {
		return false
	}
	if match.MaxSize > 0 && req.Size > match.MaxSize {
		return false
	}

	// Check time window
	if match.TimeWindow != nil && !fw.matchTimeWindow(match.TimeWindow, req.Timestamp) {
		return false
	}

	return true
}

func (fw *Firewall) matchTimeWindow(window *TimeWindow, t time.Time) bool {
	loc := time.UTC
	if window.Timezone != "" {
		if l, err := time.LoadLocation(window.Timezone); err == nil {
			loc = l
		}
	}

	t = t.In(loc)
	hour := t.Hour()

	// Check hour range
	if window.StartHour <= window.EndHour {
		if hour < window.StartHour || hour > window.EndHour {
			return false
		}
	} else {
		// Wrap around midnight
		if hour < window.StartHour && hour > window.EndHour {
			return false
		}
	}

	// Check day of week
	if len(window.DaysOfWeek) > 0 {
		day := int(t.Weekday())
		found := false
		for _, d := range window.DaysOfWeek {
			if d == day {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func (fw *Firewall) getOrCreateUserLimiter(user string) *TokenBucket {
	fw.limiterMu.RLock()
	limiter, ok := fw.userLimiters[user]
	fw.limiterMu.RUnlock()

	if ok {
		return limiter
	}

	fw.limiterMu.Lock()
	defer fw.limiterMu.Unlock()

	// Double-check
	if limiter, ok := fw.userLimiters[user]; ok {
		return limiter
	}

	rps := fw.config.RateLimiting.RequestsPerSecond
	if override, ok := fw.config.RateLimiting.UserLimits[user]; ok {
		rps = override
	}

	limiter = NewTokenBucket(rps, fw.config.RateLimiting.BurstSize)
	fw.userLimiters[user] = limiter
	return limiter
}

func (fw *Firewall) getOrCreateIPLimiter(ip string) *TokenBucket {
	fw.limiterMu.RLock()
	limiter, ok := fw.ipLimiters[ip]
	fw.limiterMu.RUnlock()

	if ok {
		return limiter
	}

	fw.limiterMu.Lock()
	defer fw.limiterMu.Unlock()

	if limiter, ok := fw.ipLimiters[ip]; ok {
		return limiter
	}

	limiter = NewTokenBucket(
		fw.config.RateLimiting.RequestsPerSecond,
		fw.config.RateLimiting.BurstSize,
	)
	fw.ipLimiters[ip] = limiter
	return limiter
}

func (fw *Firewall) getOrCreateBucketLimiter(bucket string) *TokenBucket {
	fw.limiterMu.RLock()
	limiter, ok := fw.bucketLimiters[bucket]
	fw.limiterMu.RUnlock()

	if ok {
		return limiter
	}

	fw.limiterMu.Lock()
	defer fw.limiterMu.Unlock()

	if limiter, ok := fw.bucketLimiters[bucket]; ok {
		return limiter
	}

	rps := fw.config.RateLimiting.RequestsPerSecond
	if override, ok := fw.config.RateLimiting.BucketLimits[bucket]; ok {
		rps = override
	}

	limiter = NewTokenBucket(rps, fw.config.RateLimiting.BurstSize)
	fw.bucketLimiters[bucket] = limiter
	return limiter
}

func (fw *Firewall) getOrCreateUserBandwidth(user string) *BandwidthTracker {
	fw.bandwidthMu.RLock()
	tracker, ok := fw.userBandwidth[user]
	fw.bandwidthMu.RUnlock()

	if ok {
		return tracker
	}

	fw.bandwidthMu.Lock()
	defer fw.bandwidthMu.Unlock()

	if tracker, ok := fw.userBandwidth[user]; ok {
		return tracker
	}

	limit := fw.config.Bandwidth.MaxBytesPerSecondPerUser
	if override, ok := fw.config.Bandwidth.UserLimits[user]; ok {
		limit = override
	}

	tracker = NewBandwidthTracker(limit)
	fw.userBandwidth[user] = tracker
	return tracker
}

func (fw *Firewall) getOrCreateBucketBandwidth(bucket string) *BandwidthTracker {
	fw.bandwidthMu.RLock()
	tracker, ok := fw.bucketBandwidth[bucket]
	fw.bandwidthMu.RUnlock()

	if ok {
		return tracker
	}

	fw.bandwidthMu.Lock()
	defer fw.bandwidthMu.Unlock()

	if tracker, ok := fw.bucketBandwidth[bucket]; ok {
		return tracker
	}

	limit := fw.config.Bandwidth.MaxBytesPerSecondPerBucket
	if override, ok := fw.config.Bandwidth.BucketLimits[bucket]; ok {
		limit = override
	}

	tracker = NewBandwidthTracker(limit)
	fw.bucketBandwidth[bucket] = tracker
	return tracker
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func matchWildcard(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if pattern == "*" || pattern == value {
			return true
		}
		// Simple prefix wildcard: "bucket*"
		if len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
			prefix := pattern[:len(pattern)-1]
			if len(value) >= len(prefix) && value[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

// TokenBucket implements the token bucket rate limiting algorithm
type TokenBucket struct {
	mu           sync.Mutex
	tokens       float64
	maxTokens    float64
	refillRate   float64 // tokens per second
	lastRefill   time.Time
}

// NewTokenBucket creates a new token bucket rate limiter
func NewTokenBucket(requestsPerSecond, burstSize int) *TokenBucket {
	return &TokenBucket{
		tokens:     float64(burstSize),
		maxTokens:  float64(burstSize),
		refillRate: float64(requestsPerSecond),
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed and consumes a token if so
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// AllowN checks if n tokens are available and consumes them if so
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	needed := float64(n)
	if tb.tokens >= needed {
		tb.tokens -= needed
		return true
	}
	return false
}

// refill adds tokens based on elapsed time (must be called with lock held)
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.lastRefill = now

	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
}

// Tokens returns current token count (for monitoring)
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return tb.tokens
}

// BandwidthTracker tracks bandwidth usage with a sliding window
type BandwidthTracker struct {
	mu            sync.Mutex
	maxBytesPerSec int64
	windowSize    time.Duration
	buckets       []bandwidthBucket
	bucketDuration time.Duration
	numBuckets    int
}

type bandwidthBucket struct {
	bytes     int64
	timestamp time.Time
}

// NewBandwidthTracker creates a new bandwidth tracker
func NewBandwidthTracker(maxBytesPerSecond int64) *BandwidthTracker {
	numBuckets := 10 // 10 buckets for 1 second = 100ms granularity
	windowSize := time.Second
	bucketDuration := windowSize / time.Duration(numBuckets)

	return &BandwidthTracker{
		maxBytesPerSec: maxBytesPerSecond,
		windowSize:     windowSize,
		buckets:        make([]bandwidthBucket, numBuckets),
		bucketDuration: bucketDuration,
		numBuckets:     numBuckets,
	}
}

// TryConsume attempts to consume bytes and returns true if allowed
func (bt *BandwidthTracker) TryConsume(bytes int64) bool {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	now := time.Now()
	bt.cleanOldBuckets(now)

	// Calculate current usage in the window
	var currentUsage int64
	for _, bucket := range bt.buckets {
		if !bucket.timestamp.IsZero() && now.Sub(bucket.timestamp) < bt.windowSize {
			currentUsage += bucket.bytes
		}
	}

	// Check if adding this would exceed the limit
	if currentUsage+bytes > bt.maxBytesPerSec {
		return false
	}

	// Find the current bucket and add bytes
	bucketIndex := bt.getBucketIndex(now)
	bucket := &bt.buckets[bucketIndex]

	// If bucket is old, reset it
	if bucket.timestamp.IsZero() || now.Sub(bucket.timestamp) >= bt.bucketDuration {
		bucket.bytes = bytes
		bucket.timestamp = now
	} else {
		bucket.bytes += bytes
	}

	return true
}

// CurrentUsage returns current bandwidth usage in bytes per second
func (bt *BandwidthTracker) CurrentUsage() int64 {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	now := time.Now()
	bt.cleanOldBuckets(now)

	var total int64
	for _, bucket := range bt.buckets {
		if !bucket.timestamp.IsZero() && now.Sub(bucket.timestamp) < bt.windowSize {
			total += bucket.bytes
		}
	}
	return total
}

// cleanOldBuckets removes stale buckets (must be called with lock held)
func (bt *BandwidthTracker) cleanOldBuckets(now time.Time) {
	for i := range bt.buckets {
		if !bt.buckets[i].timestamp.IsZero() && now.Sub(bt.buckets[i].timestamp) >= bt.windowSize {
			bt.buckets[i] = bandwidthBucket{}
		}
	}
}

// getBucketIndex returns the bucket index for a given time
func (bt *BandwidthTracker) getBucketIndex(t time.Time) int {
	return int(t.UnixNano()/int64(bt.bucketDuration)) % bt.numBuckets
}

// Connection tracking methods

// TrackConnection increments connection counts
func (fw *Firewall) TrackConnection(ip, user string) {
	fw.connectionMu.Lock()
	defer fw.connectionMu.Unlock()

	atomic.AddInt64(&fw.globalConnections, 1)
	if ip != "" {
		fw.ipConnections[ip]++
	}
	if user != "" {
		fw.userConnections[user]++
	}
}

// ReleaseConnection decrements connection counts
func (fw *Firewall) ReleaseConnection(ip, user string) {
	fw.connectionMu.Lock()
	defer fw.connectionMu.Unlock()

	atomic.AddInt64(&fw.globalConnections, -1)
	if ip != "" {
		if fw.ipConnections[ip] > 0 {
			fw.ipConnections[ip]--
		}
	}
	if user != "" {
		if fw.userConnections[user] > 0 {
			fw.userConnections[user]--
		}
	}
}

// UpdateConfig updates the firewall configuration dynamically
func (fw *Firewall) UpdateConfig(config Config) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	fw.config = config

	// Re-parse IP lists
	fw.allowedNets = nil
	fw.blockedNets = nil

	for _, cidr := range config.IPAllowlist {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			ip := net.ParseIP(cidr)
			if ip == nil {
				continue
			}
			if ip.To4() != nil {
				_, network, _ = net.ParseCIDR(cidr + "/32")
			} else {
				_, network, _ = net.ParseCIDR(cidr + "/128")
			}
		}
		if network != nil {
			fw.allowedNets = append(fw.allowedNets, network)
		}
	}

	for _, cidr := range config.IPBlocklist {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			ip := net.ParseIP(cidr)
			if ip == nil {
				continue
			}
			if ip.To4() != nil {
				_, network, _ = net.ParseCIDR(cidr + "/32")
			} else {
				_, network, _ = net.ParseCIDR(cidr + "/128")
			}
		}
		if network != nil {
			fw.blockedNets = append(fw.blockedNets, network)
		}
	}

	// Update global limiters
	if config.RateLimiting.Enabled {
		fw.globalLimiter = NewTokenBucket(
			config.RateLimiting.RequestsPerSecond,
			config.RateLimiting.BurstSize,
		)
	}

	if config.Bandwidth.Enabled {
		fw.globalBandwidth = NewBandwidthTracker(config.Bandwidth.MaxBytesPerSecond)
	}

	return nil
}

// GetConfig returns the current firewall configuration
func (fw *Firewall) GetConfig() Config {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return fw.config
}

// ResetStats resets firewall statistics
func (fw *Firewall) ResetStats() {
	atomic.StoreInt64(&fw.stats.RequestsAllowed, 0)
	atomic.StoreInt64(&fw.stats.RequestsDenied, 0)
	atomic.StoreInt64(&fw.stats.RequestsThrottled, 0)
	atomic.StoreInt64(&fw.stats.RateLimitHits, 0)
	atomic.StoreInt64(&fw.stats.BandwidthLimitHits, 0)
	atomic.StoreInt64(&fw.stats.ConnectionLimitHits, 0)
	atomic.StoreInt64(&fw.stats.RulesEvaluated, 0)
}
