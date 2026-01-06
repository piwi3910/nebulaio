// Package firewall provides an S3-aware data firewall with QoS, rate limiting,
// bandwidth throttling, and enhanced access control.
package firewall

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Default configuration constants.
const (
	defaultRequestsPerSecond        = 1000
	defaultRateLimitBurstSize       = 100
	defaultMaxBytesPerSecond        = 1024 * 1024 * 1024 // 1 GB/s
	defaultMaxBytesPerSecondPerUser = 100 * 1024 * 1024  // 100 MB/s
	defaultMaxBytesPerSecondBucket  = 500 * 1024 * 1024  // 500 MB/s
	defaultMaxConnections           = 10000
	defaultMaxConnectionsPerIP      = 100
	defaultMaxConnectionsPerUser    = 500
	defaultIdleTimeoutSeconds       = 60
)

// Config configures the data firewall.
type Config struct {
	// 8-byte fields (slices)
	Rules       []Rule   `json:"rules"       yaml:"rules"`
	IPAllowlist []string `json:"ipAllowlist" yaml:"ip_allowlist"`
	IPBlocklist []string `json:"ipBlocklist" yaml:"ip_blocklist"`
	// Structs
	Bandwidth    BandwidthConfig  `json:"bandwidth"    yaml:"bandwidth"`
	RateLimiting RateLimitConfig  `json:"rateLimiting" yaml:"rate_limiting"`
	Connections  ConnectionConfig `json:"connections"  yaml:"connections"`
	// Strings
	DefaultPolicy string `json:"defaultPolicy" yaml:"default_policy"`
	// 1-byte fields (bool)
	Enabled      bool `json:"enabled"      yaml:"enabled"`
	AuditEnabled bool `json:"auditEnabled" yaml:"audit_enabled"`
}

// RateLimitConfig configures rate limiting.
type RateLimitConfig struct {
	UserLimits              map[string]int `json:"userLimits" yaml:"user_limits"`
	BucketLimits            map[string]int `json:"bucketLimits" yaml:"bucket_limits"`
	OperationLimits         map[string]int `json:"operationLimits" yaml:"operation_limits"`
	RequestsPerSecond       int            `json:"requestsPerSecond" yaml:"requests_per_second"`
	BurstSize               int            `json:"burstSize" yaml:"burst_size"`
	ObjectCreationLimit     int            `json:"objectCreationLimit" yaml:"object_creation_limit"`
	ObjectCreationBurstSize int            `json:"objectCreationBurstSize" yaml:"object_creation_burst_size"`
	Enabled                 bool           `json:"enabled" yaml:"enabled"`
	PerUser                 bool           `json:"perUser" yaml:"per_user"`
	PerIP                   bool           `json:"perIP" yaml:"per_ip"`
	PerBucket               bool           `json:"perBucket" yaml:"per_bucket"`
}

// BandwidthConfig configures bandwidth throttling.
type BandwidthConfig struct {
	UserLimits                 map[string]int64 `json:"userLimits" yaml:"user_limits"`
	BucketLimits               map[string]int64 `json:"bucketLimits" yaml:"bucket_limits"`
	MaxBytesPerSecond          int64            `json:"maxBytesPerSecond" yaml:"max_bytes_per_second"`
	MaxBytesPerSecondPerUser   int64            `json:"maxBytesPerSecondPerUser" yaml:"max_bytes_per_second_per_user"`
	MaxBytesPerSecondPerBucket int64            `json:"maxBytesPerSecondPerBucket" yaml:"max_bytes_per_second_per_bucket"`
	Enabled                    bool             `json:"enabled" yaml:"enabled"`
}

// ConnectionConfig configures connection limits.
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

// Rule represents a firewall rule.
type Rule struct {
	// 8-byte fields (pointers)
	RateLimit      *int   `json:"rateLimit,omitempty"      yaml:"rate_limit,omitempty"`
	BandwidthLimit *int64 `json:"bandwidthLimit,omitempty" yaml:"bandwidth_limit,omitempty"`
	// Structs
	Match RuleMatch `json:"match" yaml:"match"`
	// Strings
	ID     string `json:"id"     yaml:"id"`
	Name   string `json:"name"   yaml:"name"`
	Action string `json:"action" yaml:"action"`
	// 4-byte fields (int)
	Priority int `json:"priority" yaml:"priority"`
	// 1-byte fields (bool)
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// RuleMatch contains criteria for matching requests.
type RuleMatch struct {
	TimeWindow   *TimeWindow `json:"timeWindow,omitempty" yaml:"time_window,omitempty"`
	SourceIPs    []string    `json:"sourceIPs,omitempty" yaml:"source_ips,omitempty"`
	Users        []string    `json:"users,omitempty" yaml:"users,omitempty"`
	Buckets      []string    `json:"buckets,omitempty" yaml:"buckets,omitempty"`
	Operations   []string    `json:"operations,omitempty" yaml:"operations,omitempty"`
	KeyPrefixes  []string    `json:"keyPrefixes,omitempty" yaml:"key_prefixes,omitempty"`
	ContentTypes []string    `json:"contentTypes,omitempty" yaml:"content_types,omitempty"`
	MinSize      int64       `json:"minSize,omitempty" yaml:"min_size,omitempty"`
	MaxSize      int64       `json:"maxSize,omitempty" yaml:"max_size,omitempty"`
}

// TimeWindow defines time-based rule matching.
type TimeWindow struct {
	Timezone   string `json:"timezone" yaml:"timezone"`
	DaysOfWeek []int  `json:"daysOfWeek,omitempty" yaml:"days_of_week,omitempty"`
	StartHour  int    `json:"startHour" yaml:"start_hour"`
	EndHour    int    `json:"endHour" yaml:"end_hour"`
}

// Request represents an incoming S3 request for firewall evaluation.
type Request struct {
	Timestamp   time.Time
	SourceIP    string
	User        string
	Bucket      string
	Key         string
	Operation   string
	ContentType string
	Size        int64
}

// Decision represents a firewall decision.
type Decision struct {
	Rule           *Rule
	Reason         string
	RateLimit      int
	BandwidthLimit int64
	Allowed        bool
}

// Firewall is the main data firewall.
type Firewall struct {
	globalBandwidth   *BandwidthTracker
	bucketBandwidth   map[string]*BandwidthTracker
	globalLimiter     *TokenBucket
	userBandwidth     map[string]*BandwidthTracker
	ipLimiters        map[string]*TokenBucket
	bucketLimiters    map[string]*TokenBucket
	operationLimiters map[string]*TokenBucket
	userConnections   map[string]int64
	ipConnections     map[string]int64
	userLimiters      map[string]*TokenBucket
	allowedNets       []*net.IPNet
	blockedNets       []*net.IPNet
	config            Config
	stats             FirewallStats
	globalConnections int64
	bandwidthMu       sync.RWMutex
	mu                sync.RWMutex
	limiterMu         sync.RWMutex
	connectionMu      sync.RWMutex
}

// FirewallStats contains firewall statistics.
type FirewallStats struct {
	RequestsAllowed     int64 `json:"requestsAllowed"`
	RequestsDenied      int64 `json:"requestsDenied"`
	RequestsThrottled   int64 `json:"requestsThrottled"`
	RateLimitHits       int64 `json:"rateLimitHits"`
	BandwidthLimitHits  int64 `json:"bandwidthLimitHits"`
	ConnectionLimitHits int64 `json:"connectionLimitHits"`
	RulesEvaluated      int64 `json:"rulesEvaluated"`
}

// DefaultConfig returns sensible firewall defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		DefaultPolicy: "allow",
		RateLimiting: RateLimitConfig{
			Enabled:           false,
			RequestsPerSecond: defaultRequestsPerSecond,
			BurstSize:         defaultRateLimitBurstSize,
			PerUser:           true,
			PerIP:             true,
			PerBucket:         false,
		},
		Bandwidth: BandwidthConfig{
			Enabled:                    false,
			MaxBytesPerSecond:          defaultMaxBytesPerSecond,        // 1 GB/s
			MaxBytesPerSecondPerUser:   defaultMaxBytesPerSecondPerUser, // 100 MB/s
			MaxBytesPerSecondPerBucket: defaultMaxBytesPerSecondBucket,  // 500 MB/s
		},
		Connections: ConnectionConfig{
			Enabled:               false,
			MaxConnections:        defaultMaxConnections,
			MaxConnectionsPerIP:   defaultMaxConnectionsPerIP,
			MaxConnectionsPerUser: defaultMaxConnectionsPerUser,
			IdleTimeout:           defaultIdleTimeoutSeconds * time.Second,
		},
		AuditEnabled: true,
	}
}

// New creates a new firewall.
func New(config Config) (*Firewall, error) {
	fw := &Firewall{
		config:            config,
		userLimiters:      make(map[string]*TokenBucket),
		ipLimiters:        make(map[string]*TokenBucket),
		bucketLimiters:    make(map[string]*TokenBucket),
		operationLimiters: make(map[string]*TokenBucket),
		userBandwidth:     make(map[string]*BandwidthTracker),
		bucketBandwidth:   make(map[string]*BandwidthTracker),
		ipConnections:     make(map[string]int64),
		userConnections:   make(map[string]int64),
	}

	fw.parseIPAllowlist(config.IPAllowlist)
	fw.parseIPBlocklist(config.IPBlocklist)
	fw.initializeRateLimiter(config)
	fw.initializeBandwidthTracker(config)

	return fw, nil
}

func (fw *Firewall) parseIPAllowlist(allowlist []string) {
	networks, invalid := fw.parseCIDRList(allowlist)
	fw.allowedNets = networks

	if len(invalid) > 0 {
		log.Warn().
			Int("count", len(invalid)).
			Strs("entries", invalid).
			Msg("skipped invalid IP allowlist entries - check firewall configuration")
	}
}

func (fw *Firewall) parseIPBlocklist(blocklist []string) {
	networks, invalid := fw.parseCIDRList(blocklist)
	fw.blockedNets = networks

	if len(invalid) > 0 {
		log.Warn().
			Int("count", len(invalid)).
			Strs("entries", invalid).
			Msg("skipped invalid IP blocklist entries - check firewall configuration")
	}
}

func (fw *Firewall) parseCIDRList(cidrs []string) ([]*net.IPNet, []string) {
	var networks []*net.IPNet
	var invalid []string

	for _, cidr := range cidrs {
		network, err := fw.parseCIDREntry(cidr)
		if err != nil {
			invalid = append(invalid, cidr)
			continue
		}
		if network != nil {
			networks = append(networks, network)
		}
	}

	return networks, invalid
}

func (fw *Firewall) parseCIDREntry(cidr string) (*net.IPNet, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err == nil {
		return network, nil
	}

	// Try parsing as plain IP
	ip := net.ParseIP(cidr)
	if ip == nil {
		return nil, errors.New("invalid IP or CIDR")
	}

	// Convert IP to CIDR
	return fw.ipToCIDR(ip, cidr)
}

func (fw *Firewall) ipToCIDR(ip net.IP, original string) (*net.IPNet, error) {
	var cidr string
	if ip.To4() != nil {
		cidr = original + "/32"
	} else {
		cidr = original + "/128"
	}

	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	return network, nil
}

func (fw *Firewall) initializeRateLimiter(config Config) {
	if config.RateLimiting.Enabled {
		fw.globalLimiter = NewTokenBucket(
			config.RateLimiting.RequestsPerSecond,
			config.RateLimiting.BurstSize,
		)
	}
}

func (fw *Firewall) initializeBandwidthTracker(config Config) {
	if config.Bandwidth.Enabled {
		fw.globalBandwidth = NewBandwidthTracker(config.Bandwidth.MaxBytesPerSecond)
	}
}

// Evaluate evaluates a request against the firewall.
func (fw *Firewall) Evaluate(ctx context.Context, req *Request) *Decision {
	if !fw.config.Enabled {
		return &Decision{Allowed: true}
	}

	atomic.AddInt64(&fw.stats.RulesEvaluated, 1)

	if decision := fw.checkIPBlocklist(req); decision != nil {
		return decision
	}

	if decision := fw.checkIPAllowlist(req); decision != nil {
		return decision
	}

	if decision := fw.checkConnectionLimitsDecision(req); decision != nil {
		return decision
	}

	if decision := fw.evaluateRules(req); decision != nil {
		return decision
	}

	if decision := fw.checkRateLimitsDecision(req); decision != nil {
		return decision
	}

	return fw.applyDefaultPolicy()
}

func (fw *Firewall) checkIPBlocklist(req *Request) *Decision {
	if fw.isIPBlocked(req.SourceIP) {
		atomic.AddInt64(&fw.stats.RequestsDenied, 1)

		return &Decision{
			Allowed: false,
			Reason:  "IP address is blocked",
		}
	}
	return nil
}

func (fw *Firewall) checkIPAllowlist(req *Request) *Decision {
	if len(fw.allowedNets) > 0 && !fw.isIPAllowed(req.SourceIP) {
		atomic.AddInt64(&fw.stats.RequestsDenied, 1)

		return &Decision{
			Allowed: false,
			Reason:  "IP address is not in allowlist",
		}
	}
	return nil
}

func (fw *Firewall) checkConnectionLimitsDecision(req *Request) *Decision {
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
	return nil
}

func (fw *Firewall) evaluateRules(req *Request) *Decision {
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
	return nil
}

func (fw *Firewall) checkRateLimitsDecision(req *Request) *Decision {
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
	return nil
}

func (fw *Firewall) applyDefaultPolicy() *Decision {
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

// CheckBandwidth checks and consumes bandwidth.
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

// Stats returns firewall statistics.
func (fw *Firewall) Stats() FirewallStats {
	return FirewallStats{
		RequestsAllowed:     atomic.LoadInt64(&fw.stats.RequestsAllowed),
		RequestsDenied:      atomic.LoadInt64(&fw.stats.RequestsDenied),
		RequestsThrottled:   atomic.LoadInt64(&fw.stats.RequestsThrottled),
		RateLimitHits:       atomic.LoadInt64(&fw.stats.RateLimitHits),
		BandwidthLimitHits:  atomic.LoadInt64(&fw.stats.BandwidthLimitHits),
		ConnectionLimitHits: atomic.LoadInt64(&fw.stats.ConnectionLimitHits),
		RulesEvaluated:      atomic.LoadInt64(&fw.stats.RulesEvaluated),
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
	if !fw.checkGlobalRateLimit() {
		return false
	}

	if !fw.checkIPRateLimit(req) {
		return false
	}

	if !fw.checkUserRateLimit(req) {
		return false
	}

	if !fw.checkBucketRateLimit(req) {
		return false
	}

	if !fw.checkOperationRateLimit(req) {
		return false
	}

	return true
}

func (fw *Firewall) checkGlobalRateLimit() bool {
	if fw.globalLimiter != nil && !fw.globalLimiter.Allow() {
		return false
	}
	return true
}

func (fw *Firewall) checkIPRateLimit(req *Request) bool {
	if fw.config.RateLimiting.PerIP && req.SourceIP != "" {
		limiter := fw.getOrCreateIPLimiter(req.SourceIP)
		if !limiter.Allow() {
			return false
		}
	}
	return true
}

func (fw *Firewall) checkUserRateLimit(req *Request) bool {
	if fw.config.RateLimiting.PerUser && req.User != "" {
		limiter := fw.getOrCreateUserLimiter(req.User)
		if !limiter.Allow() {
			return false
		}
	}
	return true
}

func (fw *Firewall) checkBucketRateLimit(req *Request) bool {
	if fw.config.RateLimiting.PerBucket && req.Bucket != "" {
		limiter := fw.getOrCreateBucketLimiter(req.Bucket)
		if !limiter.Allow() {
			return false
		}
	}
	return true
}

func (fw *Firewall) checkOperationRateLimit(req *Request) bool {
	if req.Operation == "" {
		return true
	}

	if isObjectCreationOperation(req.Operation) && fw.config.RateLimiting.ObjectCreationLimit > 0 {
		limiter := fw.getOrCreateOperationLimiter(req.Operation)
		return limiter.Allow()
	}

	if limit, ok := fw.config.RateLimiting.OperationLimits[req.Operation]; ok && limit > 0 {
		limiter := fw.getOrCreateOperationLimiter(req.Operation)
		return limiter.Allow()
	}

	return true
}

// isObjectCreationOperation checks if the operation creates objects (vulnerable to hash DoS).
func isObjectCreationOperation(operation string) bool {
	switch operation {
	case "PutObject", "CopyObject", "CompleteMultipartUpload", "UploadPart":
		return true
	default:
		return false
	}
}

func (fw *Firewall) matchRule(rule *Rule, req *Request) bool {
	match := &rule.Match

	return fw.matchSourceIPs(match.SourceIPs, req.SourceIP) &&
		fw.matchUsers(match.Users, req.User) &&
		fw.matchBuckets(match.Buckets, req.Bucket) &&
		fw.matchOperations(match.Operations, req.Operation) &&
		fw.matchKeyPrefixes(match.KeyPrefixes, req.Key) &&
		fw.matchContentTypes(match.ContentTypes, req.ContentType) &&
		fw.matchSizeConstraints(match.MinSize, match.MaxSize, req.Size) &&
		fw.matchTimeConstraints(match.TimeWindow, req.Timestamp)
}

// matchSourceIPs checks if request source IP matches any allowed CIDR/IP.
func (fw *Firewall) matchSourceIPs(sourceIPs []string, requestIP string) bool {
	if len(sourceIPs) == 0 {
		return true
	}

	reqIP := net.ParseIP(requestIP)
	for _, cidr := range sourceIPs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			if net.ParseIP(cidr).Equal(reqIP) {
				return true
			}
			continue
		}

		if network.Contains(reqIP) {
			return true
		}
	}

	return false
}

// matchUsers checks if request user matches any allowed user.
func (fw *Firewall) matchUsers(users []string, requestUser string) bool {
	if len(users) == 0 {
		return true
	}
	return contains(users, requestUser)
}

// matchBuckets checks if request bucket matches any wildcard pattern.
func (fw *Firewall) matchBuckets(buckets []string, requestBucket string) bool {
	if len(buckets) == 0 {
		return true
	}
	return matchWildcard(buckets, requestBucket)
}

// matchOperations checks if request operation matches any allowed operation.
func (fw *Firewall) matchOperations(operations []string, requestOperation string) bool {
	if len(operations) == 0 {
		return true
	}
	return contains(operations, requestOperation)
}

// matchKeyPrefixes checks if request key starts with any allowed prefix.
func (fw *Firewall) matchKeyPrefixes(keyPrefixes []string, requestKey string) bool {
	if len(keyPrefixes) == 0 {
		return true
	}

	for _, prefix := range keyPrefixes {
		if len(requestKey) >= len(prefix) && requestKey[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}

// matchContentTypes checks if request content type matches any allowed type.
func (fw *Firewall) matchContentTypes(contentTypes []string, requestContentType string) bool {
	if len(contentTypes) == 0 {
		return true
	}
	return contains(contentTypes, requestContentType)
}

// matchSizeConstraints checks if request size is within allowed range.
func (fw *Firewall) matchSizeConstraints(minSize, maxSize, requestSize int64) bool {
	if minSize > 0 && requestSize < minSize {
		return false
	}

	if maxSize > 0 && requestSize > maxSize {
		return false
	}

	return true
}

// matchTimeConstraints checks if request time is within allowed time window.
func (fw *Firewall) matchTimeConstraints(timeWindow *TimeWindow, requestTime time.Time) bool {
	if timeWindow == nil {
		return true
	}
	return fw.matchTimeWindow(timeWindow, requestTime)
}

func (fw *Firewall) matchTimeWindow(window *TimeWindow, t time.Time) bool {
	loc := time.UTC
	if window.Timezone != "" {
		l, loadErr := time.LoadLocation(window.Timezone)
		if loadErr == nil {
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
		if !slices.Contains(window.DaysOfWeek, day) {
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

func (fw *Firewall) getOrCreateOperationLimiter(operation string) *TokenBucket {
	fw.limiterMu.RLock()
	limiter, ok := fw.operationLimiters[operation]
	fw.limiterMu.RUnlock()

	if ok {
		return limiter
	}

	fw.limiterMu.Lock()
	defer fw.limiterMu.Unlock()

	if limiter, ok := fw.operationLimiters[operation]; ok {
		return limiter
	}

	// Determine rate limit for this operation
	rps := fw.config.RateLimiting.RequestsPerSecond
	burstSize := fw.config.RateLimiting.BurstSize

	// Check for specific object creation limit
	if isObjectCreationOperation(operation) && fw.config.RateLimiting.ObjectCreationLimit > 0 {
		rps = fw.config.RateLimiting.ObjectCreationLimit
		if fw.config.RateLimiting.ObjectCreationBurstSize > 0 {
			burstSize = fw.config.RateLimiting.ObjectCreationBurstSize
		}
	} else if override, ok := fw.config.RateLimiting.OperationLimits[operation]; ok {
		rps = override
	}

	limiter = NewTokenBucket(rps, burstSize)
	fw.operationLimiters[operation] = limiter

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
	return slices.Contains(slice, item)
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

// TokenBucket implements the token bucket rate limiting algorithm.
type TokenBucket struct {
	lastRefill time.Time
	tokens     float64
	maxTokens  float64
	refillRate float64
	mu         sync.Mutex
}

// NewTokenBucket creates a new token bucket rate limiter.
func NewTokenBucket(requestsPerSecond, burstSize int) *TokenBucket {
	return &TokenBucket{
		tokens:     float64(burstSize),
		maxTokens:  float64(burstSize),
		refillRate: float64(requestsPerSecond),
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed and consumes a token if so.
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

// AllowN checks if n tokens are available and consumes them if so.
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

// refill adds tokens based on elapsed time (must be called with lock held).
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.lastRefill = now

	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
}

// Tokens returns current token count (for monitoring).
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	return tb.tokens
}

// BandwidthTracker tracks bandwidth usage with a sliding window.
type BandwidthTracker struct {
	buckets        []bandwidthBucket
	maxBytesPerSec int64
	windowSize     time.Duration
	bucketDuration time.Duration
	numBuckets     int
	mu             sync.Mutex
}

type bandwidthBucket struct {
	timestamp time.Time
	bytes     int64
}

// NewBandwidthTracker creates a new bandwidth tracker.
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

// TryConsume attempts to consume bytes and returns true if allowed.
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

// CurrentUsage returns current bandwidth usage in bytes per second.
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

// cleanOldBuckets removes stale buckets (must be called with lock held).
func (bt *BandwidthTracker) cleanOldBuckets(now time.Time) {
	for i := range bt.buckets {
		if !bt.buckets[i].timestamp.IsZero() && now.Sub(bt.buckets[i].timestamp) >= bt.windowSize {
			bt.buckets[i] = bandwidthBucket{}
		}
	}
}

// getBucketIndex returns the bucket index for a given time.
func (bt *BandwidthTracker) getBucketIndex(t time.Time) int {
	return int(t.UnixNano()/int64(bt.bucketDuration)) % bt.numBuckets
}

// Connection tracking methods

// TrackConnection increments connection counts.
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

// ReleaseConnection decrements connection counts.
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

// UpdateConfig updates the firewall configuration dynamically.
func (fw *Firewall) UpdateConfig(config Config) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	fw.config = config

	fw.updateIPLists(config)
	fw.updateGlobalLimiters(config)

	return nil
}

func (fw *Firewall) updateIPLists(config Config) {
	fw.allowedNets = nil
	fw.blockedNets = nil

	fw.allowedNets = fw.parseNetworkList(config.IPAllowlist)
	fw.blockedNets = fw.parseNetworkList(config.IPBlocklist)
}

func (fw *Firewall) parseNetworkList(cidrList []string) []*net.IPNet {
	var networks []*net.IPNet

	for _, cidr := range cidrList {
		network := fw.parseCIDROrIP(cidr)
		if network != nil {
			networks = append(networks, network)
		}
	}

	return networks
}

func (fw *Firewall) parseCIDROrIP(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return fw.parseIPAsNetwork(cidr)
	}

	return network
}

func (fw *Firewall) parseIPAsNetwork(ipStr string) *net.IPNet {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil
	}

	var network *net.IPNet
	if ip.To4() != nil {
		_, network, _ = net.ParseCIDR(ipStr + "/32")
	} else {
		_, network, _ = net.ParseCIDR(ipStr + "/128")
	}

	return network
}

func (fw *Firewall) updateGlobalLimiters(config Config) {
	if config.RateLimiting.Enabled {
		fw.globalLimiter = NewTokenBucket(
			config.RateLimiting.RequestsPerSecond,
			config.RateLimiting.BurstSize,
		)
	}

	if config.Bandwidth.Enabled {
		fw.globalBandwidth = NewBandwidthTracker(config.Bandwidth.MaxBytesPerSecond)
	}
}

// GetConfig returns the current firewall configuration.
func (fw *Firewall) GetConfig() Config {
	fw.mu.RLock()
	defer fw.mu.RUnlock()

	return fw.config
}

// ResetStats resets firewall statistics.
func (fw *Firewall) ResetStats() {
	atomic.StoreInt64(&fw.stats.RequestsAllowed, 0)
	atomic.StoreInt64(&fw.stats.RequestsDenied, 0)
	atomic.StoreInt64(&fw.stats.RequestsThrottled, 0)
	atomic.StoreInt64(&fw.stats.RateLimitHits, 0)
	atomic.StoreInt64(&fw.stats.BandwidthLimitHits, 0)
	atomic.StoreInt64(&fw.stats.ConnectionLimitHits, 0)
	atomic.StoreInt64(&fw.stats.RulesEvaluated, 0)
}
