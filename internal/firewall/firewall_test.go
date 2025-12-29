package firewall

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewFirewall(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	if fw == nil {
		t.Fatal("Firewall is nil")
	}

	if !fw.config.Enabled {
		t.Error("Firewall should be enabled")
	}
}

func TestFirewallDisabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	req := &Request{
		SourceIP:  "192.168.1.100",
		User:      "testuser",
		Bucket:    "testbucket",
		Operation: "GetObject",
	}

	decision := fw.Evaluate(context.Background(), req)
	if !decision.Allowed {
		t.Error("Request should be allowed when firewall is disabled")
	}
}

func TestIPBlocklist(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.IPBlocklist = []string{"192.168.1.100", "10.0.0.0/8"}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	tests := []struct {
		name      string
		ip        string
		expectDeny bool
	}{
		{"Blocked IP", "192.168.1.100", true},
		{"Blocked CIDR", "10.0.0.50", true},
		{"Allowed IP", "172.16.0.1", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &Request{
				SourceIP:  tc.ip,
				Operation: "GetObject",
			}
			decision := fw.Evaluate(context.Background(), req)
			if tc.expectDeny && decision.Allowed {
				t.Errorf("Expected request from %s to be denied", tc.ip)
			}
			if !tc.expectDeny && !decision.Allowed {
				t.Errorf("Expected request from %s to be allowed", tc.ip)
			}
		})
	}
}

func TestIPAllowlist(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.IPAllowlist = []string{"192.168.1.0/24", "10.0.0.5"}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	tests := []struct {
		name       string
		ip         string
		expectDeny bool
	}{
		{"Allowed CIDR", "192.168.1.50", false},
		{"Allowed IP", "10.0.0.5", false},
		{"Not in allowlist", "172.16.0.1", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &Request{
				SourceIP:  tc.ip,
				Operation: "GetObject",
			}
			decision := fw.Evaluate(context.Background(), req)
			if tc.expectDeny && decision.Allowed {
				t.Errorf("Expected request from %s to be denied", tc.ip)
			}
			if !tc.expectDeny && !decision.Allowed {
				t.Errorf("Expected request from %s to be allowed, reason: %s", tc.ip, decision.Reason)
			}
		})
	}
}

func TestIPv6Blocklist(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.IPBlocklist = []string{"2001:db8::/32", "fd00::1"}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	tests := []struct {
		name       string
		ip         string
		expectDeny bool
	}{
		{"Blocked IPv6 CIDR", "2001:db8::1", true},
		{"Blocked IPv6 single", "fd00::1", true},
		{"Allowed IPv6", "2001:db9::1", false},
		{"Allowed IPv4", "192.168.1.1", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &Request{
				SourceIP:  tc.ip,
				Operation: "GetObject",
			}
			decision := fw.Evaluate(context.Background(), req)
			if tc.expectDeny && decision.Allowed {
				t.Errorf("Expected request from %s to be denied", tc.ip)
			}
			if !tc.expectDeny && !decision.Allowed {
				t.Errorf("Expected request from %s to be allowed", tc.ip)
			}
		})
	}
}

func TestIPv6Allowlist(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.IPAllowlist = []string{"2001:db8::/32", "::1"}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	tests := []struct {
		name       string
		ip         string
		expectDeny bool
	}{
		{"Allowed IPv6 CIDR", "2001:db8::100", false},
		{"Allowed IPv6 loopback", "::1", false},
		{"Denied IPv6 outside allowlist", "2001:db9::1", true},
		{"Denied IPv4", "192.168.1.1", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &Request{
				SourceIP:  tc.ip,
				Operation: "GetObject",
			}
			decision := fw.Evaluate(context.Background(), req)
			if tc.expectDeny && decision.Allowed {
				t.Errorf("Expected request from %s to be denied", tc.ip)
			}
			if !tc.expectDeny && !decision.Allowed {
				t.Errorf("Expected request from %s to be allowed, reason: %s", tc.ip, decision.Reason)
			}
		})
	}
}

func TestIPv6MixedRules(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			ID:       "deny-ipv6-uploads",
			Name:     "Deny IPv6 Uploads",
			Priority: 1,
			Enabled:  true,
			Action:   "deny",
			Match: RuleMatch{
				SourceIPs:  []string{"2001:db8::/32"},
				Operations: []string{"PutObject"},
			},
		},
	}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	tests := []struct {
		name       string
		ip         string
		operation  string
		expectDeny bool
	}{
		{"Deny IPv6 PutObject", "2001:db8::1", "PutObject", true},
		{"Allow IPv6 GetObject", "2001:db8::1", "GetObject", false},
		{"Allow other IPv6 PutObject", "2001:db9::1", "PutObject", false},
		{"Allow IPv4 PutObject", "192.168.1.1", "PutObject", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &Request{
				SourceIP:  tc.ip,
				Operation: tc.operation,
			}
			decision := fw.Evaluate(context.Background(), req)
			if tc.expectDeny && decision.Allowed {
				t.Errorf("Expected request from %s with operation %s to be denied", tc.ip, tc.operation)
			}
			if !tc.expectDeny && !decision.Allowed {
				t.Errorf("Expected request from %s with operation %s to be allowed", tc.ip, tc.operation)
			}
		})
	}
}

func TestRuleMatching(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			ID:       "deny-uploads",
			Name:     "Deny Uploads to Logs",
			Priority: 1,
			Enabled:  true,
			Action:   "deny",
			Match: RuleMatch{
				Buckets:    []string{"logs*"},
				Operations: []string{"PutObject"},
			},
		},
		{
			ID:       "allow-admin",
			Name:     "Allow Admin",
			Priority: 2,
			Enabled:  true,
			Action:   "allow",
			Match: RuleMatch{
				Users: []string{"admin"},
			},
		},
	}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	tests := []struct {
		name       string
		req        *Request
		expectDeny bool
	}{
		{
			name: "Deny upload to logs bucket",
			req: &Request{
				SourceIP:  "192.168.1.1",
				User:      "testuser",
				Bucket:    "logs-2024",
				Operation: "PutObject",
			},
			expectDeny: true,
		},
		{
			name: "Allow read from logs bucket",
			req: &Request{
				SourceIP:  "192.168.1.1",
				User:      "testuser",
				Bucket:    "logs-2024",
				Operation: "GetObject",
			},
			expectDeny: false,
		},
		{
			name: "Allow admin user",
			req: &Request{
				SourceIP:  "192.168.1.1",
				User:      "admin",
				Bucket:    "any-bucket",
				Operation: "PutObject",
			},
			expectDeny: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := fw.Evaluate(context.Background(), tc.req)
			if tc.expectDeny && decision.Allowed {
				t.Error("Expected request to be denied")
			}
			if !tc.expectDeny && !decision.Allowed {
				t.Errorf("Expected request to be allowed, reason: %s", decision.Reason)
			}
		})
	}
}

func TestTimeWindowRule(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			ID:       "business-hours-only",
			Name:     "Allow During Business Hours",
			Priority: 1,
			Enabled:  true,
			Action:   "deny",
			Match: RuleMatch{
				TimeWindow: &TimeWindow{
					StartHour: 18, // 6 PM
					EndHour:   6,  // 6 AM (after hours)
					Timezone:  "UTC",
				},
			},
		},
	}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Test during business hours (noon UTC)
	businessHoursTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	req := &Request{
		SourceIP:  "192.168.1.1",
		Operation: "GetObject",
		Timestamp: businessHoursTime,
	}

	decision := fw.Evaluate(context.Background(), req)
	if !decision.Allowed {
		t.Error("Request during business hours should be allowed")
	}

	// Test after hours (10 PM UTC)
	afterHoursTime := time.Date(2024, 1, 15, 22, 0, 0, 0, time.UTC)
	req.Timestamp = afterHoursTime

	decision = fw.Evaluate(context.Background(), req)
	if decision.Allowed {
		t.Error("Request after hours should be denied")
	}
}

func TestDefaultPolicyDeny(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.DefaultPolicy = "deny"

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	req := &Request{
		SourceIP:  "192.168.1.1",
		Operation: "GetObject",
	}

	decision := fw.Evaluate(context.Background(), req)
	if decision.Allowed {
		t.Error("Request should be denied by default policy")
	}
}

func TestTokenBucket(t *testing.T) {
	// 10 requests per second, burst of 5
	tb := NewTokenBucket(10, 5)

	// Should be able to make 5 requests immediately (burst)
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Errorf("Request %d should be allowed (within burst)", i+1)
		}
	}

	// 6th request should be denied
	if tb.Allow() {
		t.Error("Request should be denied (burst exhausted)")
	}

	// Wait for refill
	time.Sleep(150 * time.Millisecond) // Should refill ~1.5 tokens

	if !tb.Allow() {
		t.Error("Request should be allowed after refill")
	}
}

func TestTokenBucketAllowN(t *testing.T) {
	tb := NewTokenBucket(100, 10)

	// Should allow 5 tokens
	if !tb.AllowN(5) {
		t.Error("Should allow 5 tokens")
	}

	// Should allow 5 more tokens
	if !tb.AllowN(5) {
		t.Error("Should allow 5 more tokens")
	}

	// Should not allow 1 more (all used)
	if tb.AllowN(1) {
		t.Error("Should not allow any more tokens")
	}
}

func TestBandwidthTracker(t *testing.T) {
	// 1 MB/s limit
	bt := NewBandwidthTracker(1024 * 1024)

	// Should allow 500KB
	if !bt.TryConsume(512 * 1024) {
		t.Error("Should allow 512KB")
	}

	// Should allow another 500KB
	if !bt.TryConsume(512 * 1024) {
		t.Error("Should allow another 512KB")
	}

	// Should deny 1 more byte (would exceed limit)
	if bt.TryConsume(1) {
		t.Error("Should deny - would exceed 1MB limit")
	}

	// Check current usage
	usage := bt.CurrentUsage()
	if usage != 1024*1024 {
		t.Errorf("Expected usage of 1MB, got %d", usage)
	}

	// Wait for window to clear
	time.Sleep(1100 * time.Millisecond)

	// Should allow again
	if !bt.TryConsume(512 * 1024) {
		t.Error("Should allow after window reset")
	}
}

func TestRateLimiting(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RateLimiting.Enabled = true
	config.RateLimiting.RequestsPerSecond = 5
	config.RateLimiting.BurstSize = 3
	config.RateLimiting.PerIP = true

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	req := &Request{
		SourceIP:  "192.168.1.1",
		Operation: "GetObject",
	}

	// First 3 requests should succeed (burst)
	for i := 0; i < 3; i++ {
		decision := fw.Evaluate(context.Background(), req)
		if !decision.Allowed {
			t.Errorf("Request %d should be allowed (within burst)", i+1)
		}
	}

	// 4th request should fail
	decision := fw.Evaluate(context.Background(), req)
	if decision.Allowed {
		t.Error("Request should be denied (rate limited)")
	}

	// Check stats
	stats := fw.Stats()
	if stats.RateLimitHits == 0 {
		t.Error("Should have recorded rate limit hit")
	}
}

func TestConnectionTracking(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Connections.Enabled = true
	config.Connections.MaxConnections = 100
	config.Connections.MaxConnectionsPerIP = 2

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Track 2 connections from same IP
	fw.TrackConnection("192.168.1.1", "user1")
	fw.TrackConnection("192.168.1.1", "user1")

	// 3rd connection should be denied
	req := &Request{
		SourceIP:  "192.168.1.1",
		User:      "user1",
		Operation: "GetObject",
	}
	decision := fw.Evaluate(context.Background(), req)
	if decision.Allowed {
		t.Error("Should deny - connection limit exceeded")
	}

	// Release a connection
	fw.ReleaseConnection("192.168.1.1", "user1")

	// Now should be allowed
	decision = fw.Evaluate(context.Background(), req)
	if !decision.Allowed {
		t.Errorf("Should allow after releasing connection: %s", decision.Reason)
	}
}

func TestBandwidthThrottling(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Bandwidth.Enabled = true
	config.Bandwidth.MaxBytesPerSecond = 1024 * 1024 // 1 MB/s

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Should allow first 500KB
	if !fw.CheckBandwidth("user1", "bucket1", 512*1024) {
		t.Error("Should allow first 512KB")
	}

	// Should allow another 500KB
	if !fw.CheckBandwidth("user1", "bucket1", 512*1024) {
		t.Error("Should allow another 512KB")
	}

	// Should deny going over limit
	if fw.CheckBandwidth("user1", "bucket1", 1024) {
		t.Error("Should deny - bandwidth limit exceeded")
	}

	stats := fw.Stats()
	if stats.BandwidthLimitHits == 0 {
		t.Error("Should have recorded bandwidth limit hit")
	}
}

func TestUpdateConfig(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Update to block an IP
	newConfig := config
	newConfig.IPBlocklist = []string{"192.168.1.100"}

	err = fw.UpdateConfig(newConfig)
	if err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	// Now that IP should be blocked
	req := &Request{
		SourceIP:  "192.168.1.100",
		Operation: "GetObject",
	}
	decision := fw.Evaluate(context.Background(), req)
	if decision.Allowed {
		t.Error("Blocked IP should be denied after config update")
	}
}

func TestResetStats(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.DefaultPolicy = "deny"

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Generate some stats
	req := &Request{SourceIP: "192.168.1.1", Operation: "GetObject"}
	fw.Evaluate(context.Background(), req)
	fw.Evaluate(context.Background(), req)

	stats := fw.Stats()
	if stats.RequestsDenied == 0 {
		t.Error("Should have recorded denied requests")
	}

	// Reset and verify
	fw.ResetStats()
	stats = fw.Stats()
	if stats.RequestsDenied != 0 {
		t.Error("Stats should be reset to 0")
	}
}

func TestConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RateLimiting.Enabled = true
	config.RateLimiting.RequestsPerSecond = 1000
	config.RateLimiting.BurstSize = 100
	config.RateLimiting.PerIP = true

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	var wg sync.WaitGroup
	numGoroutines := 10
	requestsPerGoroutine := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				req := &Request{
					SourceIP:  "192.168.1.1",
					User:      "testuser",
					Operation: "GetObject",
				}
				fw.Evaluate(context.Background(), req)
			}
		}(i)
	}

	wg.Wait()

	stats := fw.Stats()
	totalEvaluated := stats.RequestsAllowed + stats.RequestsDenied
	expectedTotal := int64(numGoroutines * requestsPerGoroutine)

	if totalEvaluated != expectedTotal {
		t.Errorf("Expected %d evaluations, got %d", expectedTotal, totalEvaluated)
	}
}

func TestSizeFiltering(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			ID:       "large-file-throttle",
			Name:     "Throttle Large Files",
			Priority: 1,
			Enabled:  true,
			Action:   "throttle",
			Match: RuleMatch{
				MinSize: 100 * 1024 * 1024, // 100MB
			},
			BandwidthLimit: func() *int64 { v := int64(10 * 1024 * 1024); return &v }(), // 10 MB/s
		},
	}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Small file should not be throttled
	smallReq := &Request{
		SourceIP:  "192.168.1.1",
		Operation: "GetObject",
		Size:      1024 * 1024, // 1MB
	}
	decision := fw.Evaluate(context.Background(), smallReq)
	if decision.BandwidthLimit != 0 {
		t.Error("Small file should not have bandwidth limit")
	}

	// Large file should be throttled
	largeReq := &Request{
		SourceIP:  "192.168.1.1",
		Operation: "GetObject",
		Size:      200 * 1024 * 1024, // 200MB
	}
	decision = fw.Evaluate(context.Background(), largeReq)
	if decision.BandwidthLimit == 0 {
		t.Error("Large file should have bandwidth limit")
	}
	if decision.BandwidthLimit != 10*1024*1024 {
		t.Errorf("Expected 10MB/s limit, got %d", decision.BandwidthLimit)
	}
}

func TestKeyPrefixMatching(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			ID:       "protect-secrets",
			Name:     "Block Access to Secrets",
			Priority: 1,
			Enabled:  true,
			Action:   "deny",
			Match: RuleMatch{
				KeyPrefixes: []string{"secrets/", ".hidden/"},
			},
		},
	}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	tests := []struct {
		key        string
		expectDeny bool
	}{
		{"secrets/api-key.txt", true},
		{".hidden/config.json", true},
		{"public/document.pdf", false},
		{"data/secrets-backup.txt", false}, // "secrets" is not a prefix
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			req := &Request{
				SourceIP:  "192.168.1.1",
				Key:       tc.key,
				Operation: "GetObject",
			}
			decision := fw.Evaluate(context.Background(), req)
			if tc.expectDeny && decision.Allowed {
				t.Errorf("Access to %s should be denied", tc.key)
			}
			if !tc.expectDeny && !decision.Allowed {
				t.Errorf("Access to %s should be allowed", tc.key)
			}
		})
	}
}

func TestContentTypeFiltering(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			ID:       "deny-executables",
			Name:     "Block Executables",
			Priority: 1,
			Enabled:  true,
			Action:   "deny",
			Match: RuleMatch{
				ContentTypes: []string{"application/x-executable", "application/x-msdownload"},
				Operations:   []string{"PutObject"},
			},
		},
	}

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Should deny executable upload
	req := &Request{
		SourceIP:    "192.168.1.1",
		Operation:   "PutObject",
		ContentType: "application/x-executable",
	}
	decision := fw.Evaluate(context.Background(), req)
	if decision.Allowed {
		t.Error("Executable upload should be denied")
	}

	// Should allow document upload
	req.ContentType = "application/pdf"
	decision = fw.Evaluate(context.Background(), req)
	if !decision.Allowed {
		t.Error("PDF upload should be allowed")
	}
}

func BenchmarkFirewallEvaluate(b *testing.B) {
	config := DefaultConfig()
	config.Enabled = true
	config.RateLimiting.Enabled = true
	config.RateLimiting.RequestsPerSecond = 100000
	config.RateLimiting.BurstSize = 10000

	fw, _ := New(config)

	req := &Request{
		SourceIP:  "192.168.1.1",
		User:      "testuser",
		Bucket:    "testbucket",
		Key:       "path/to/object.txt",
		Operation: "GetObject",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fw.Evaluate(context.Background(), req)
	}
}

func BenchmarkTokenBucket(b *testing.B) {
	tb := NewTokenBucket(1000000, 100000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Allow()
	}
}

func BenchmarkBandwidthTracker(b *testing.B) {
	bt := NewBandwidthTracker(1024 * 1024 * 1024) // 1 GB/s

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bt.TryConsume(1024)
	}
}

// TestFirewallInvalidCIDR verifies that invalid CIDR entries are handled gracefully
// and don't cause the firewall to fail initialization
func TestFirewallInvalidCIDR(t *testing.T) {
	tests := []struct {
		name        string
		allowlist   []string
		blocklist   []string
		expectError bool
	}{
		{
			name:        "Valid CIDR entries",
			allowlist:   []string{"192.168.1.0/24", "10.0.0.0/8"},
			blocklist:   []string{"172.16.0.0/12"},
			expectError: false,
		},
		{
			name:        "Valid single IPs",
			allowlist:   []string{"192.168.1.100", "10.0.0.1"},
			blocklist:   []string{"172.16.0.1"},
			expectError: false,
		},
		{
			name:        "Invalid CIDR entries are skipped",
			allowlist:   []string{"not-an-ip", "192.168.1.0/24", "also-invalid"},
			blocklist:   []string{"bad-entry", "10.0.0.0/8"},
			expectError: false, // Should succeed, just skip invalid entries
		},
		{
			name:        "Mixed valid and invalid entries",
			allowlist:   []string{"192.168.1.100", "garbage", "10.0.0.1"},
			blocklist:   []string{"invalid", "172.16.0.1", "not-a-cidr"},
			expectError: false,
		},
		{
			name:        "Empty lists",
			allowlist:   []string{},
			blocklist:   []string{},
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := DefaultConfig()
			config.Enabled = true
			config.IPAllowlist = tc.allowlist
			config.IPBlocklist = tc.blocklist

			fw, err := New(config)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if fw == nil {
				t.Fatal("Firewall is nil")
			}

			// Verify that valid entries were processed
			// Test with an IP that should work if valid entries were added
			if len(tc.blocklist) > 0 {
				// Find a valid entry to test
				for _, entry := range tc.blocklist {
					if entry == "10.0.0.0/8" {
						req := &Request{
							SourceIP:  "10.0.0.50",
							Operation: "GetObject",
						}
						decision := fw.Evaluate(context.Background(), req)
						if decision.Allowed {
							t.Error("Expected IP in blocklist to be denied")
						}
						break
					}
				}
			}

			// Verify allowlist entries work correctly
			if len(tc.allowlist) > 0 {
				for _, entry := range tc.allowlist {
					if entry == "192.168.1.0/24" {
						req := &Request{
							SourceIP:  "192.168.1.100",
							Operation: "GetObject",
						}
						decision := fw.Evaluate(context.Background(), req)
						if !decision.Allowed {
							t.Error("Expected IP in allowlist to be allowed")
						}
						break
					}
					if entry == "192.168.1.100" {
						req := &Request{
							SourceIP:  "192.168.1.100",
							Operation: "GetObject",
						}
						decision := fw.Evaluate(context.Background(), req)
						if !decision.Allowed {
							t.Error("Expected exact IP in allowlist to be allowed")
						}
						break
					}
				}
			}
		})
	}
}

func TestObjectCreationRateLimit(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RateLimiting.Enabled = true
	config.RateLimiting.ObjectCreationLimit = 5
	config.RateLimiting.ObjectCreationBurstSize = 2

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Test that object creation operations are rate limited
	operations := []string{"PutObject", "CopyObject", "CompleteMultipartUpload", "UploadPart"}
	for _, op := range operations {
		t.Run(op, func(t *testing.T) {
			// Make requests up to burst limit - should be allowed
			for i := 0; i < 2; i++ {
				req := &Request{
					SourceIP:  "192.168.1.1",
					Operation: op,
					Bucket:    "test-bucket",
					Key:       "test-key",
				}
				decision := fw.Evaluate(context.Background(), req)
				if !decision.Allowed {
					t.Errorf("Request %d for %s should be allowed within burst limit", i+1, op)
				}
			}
		})
	}
}

func TestIsObjectCreationOperation(t *testing.T) {
	tests := []struct {
		operation  string
		isCreation bool
	}{
		{"PutObject", true},
		{"CopyObject", true},
		{"CompleteMultipartUpload", true},
		{"UploadPart", true},
		{"GetObject", false},
		{"DeleteObject", false},
		{"HeadObject", false},
		{"ListObjects", false},
		{"CreateBucket", false},
	}

	for _, tc := range tests {
		t.Run(tc.operation, func(t *testing.T) {
			result := isObjectCreationOperation(tc.operation)
			if result != tc.isCreation {
				t.Errorf("isObjectCreationOperation(%s) = %v, want %v", tc.operation, result, tc.isCreation)
			}
		})
	}
}

func TestObjectCreationRateLimitVsRegularOperations(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RateLimiting.Enabled = true
	config.RateLimiting.ObjectCreationLimit = 3
	config.RateLimiting.ObjectCreationBurstSize = 2

	fw, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// GetObject should not be affected by object creation rate limit
	for i := 0; i < 10; i++ {
		req := &Request{
			SourceIP:  "192.168.1.1",
			Operation: "GetObject",
			Bucket:    "test-bucket",
			Key:       "test-key",
		}
		decision := fw.Evaluate(context.Background(), req)
		if !decision.Allowed {
			t.Errorf("GetObject request %d should not be affected by object creation limit", i+1)
		}
	}
}
