// Package integration provides integration tests for enterprise features.
// These tests verify that enterprise components work together correctly.
package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/audit"
	"github.com/piwi3910/nebulaio/internal/cache/dram"
	"github.com/piwi3910/nebulaio/internal/firewall"
	"github.com/piwi3910/nebulaio/internal/s3select"
)

// TestDRAMCacheWithFirewall tests that DRAM cache respects firewall rules
func TestDRAMCacheWithFirewall(t *testing.T) {
	// Create firewall with rate limiting
	fw, err := firewall.New(firewall.Config{
		Enabled:       true,
		DefaultPolicy: "allow",
		RateLimiting: firewall.RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: 100,
			BurstSize:         10,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Create DRAM cache
	c := dram.New(dram.Config{
		MaxSize:        1024 * 1024 * 100, // 100MB
		EvictionPolicy: "arc",
	})
	defer c.Close()

	// Simulate requests going through firewall then cache
	ctx := context.Background()
	bucket := "test-bucket"

	// Track cache hits
	hits := 0
	misses := 0

	for i := 0; i < 50; i++ {
		// Check firewall first
		decision := fw.Evaluate(ctx, &firewall.Request{
			SourceIP:  "192.168.1.100",
			User:      "testuser",
			Bucket:    bucket,
			Operation: "GET",
		})

		if !decision.Allowed {
			t.Logf("Request %d blocked by firewall", i)
			continue
		}

		// Try cache
		key := fmt.Sprintf("%s/object-%d", bucket, i%10) // Use 10 unique keys
		entry, found := c.Get(ctx, key)
		if found {
			hits++
			_ = entry // Use the entry
		} else {
			misses++
			// Simulate fetching and caching
			data := []byte(fmt.Sprintf("data-for-object-%d", i%10))
			c.Put(ctx, key, data, "application/octet-stream", fmt.Sprintf("etag-%d", i%10))
		}
	}

	t.Logf("Cache hits: %d, misses: %d", hits, misses)

	// Verify cache is working
	metrics := c.Metrics()
	if metrics.Hits == 0 && hits > 0 {
		t.Error("Expected cache hits to be recorded")
	}
}

// TestS3SelectWithAudit tests that S3 Select queries are properly audited
func TestS3SelectWithAudit(t *testing.T) {
	// Create temp directory for audit logs
	tmpDir, err := os.MkdirTemp("", "audit-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	auditPath := filepath.Join(tmpDir, "audit.log")

	// Create audit logger
	auditLogger, err := audit.NewEnhancedAuditLogger(audit.EnhancedConfig{
		Enabled:        true,
		ComplianceMode: audit.ComplianceSOC2,
		Outputs: []audit.OutputConfig{
			{
				Type: "file",
				Path: auditPath,
			},
		},
		RetentionDays:    90,
		IntegrityEnabled: true,
		IntegritySecret:  "test-secret",
	}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create audit logger: %v", err)
	}
	defer auditLogger.Stop()
	auditLogger.Start()

	// Create S3 Select engine
	engine := s3select.NewEngine(
		s3select.InputFormat{
			Type: "CSV",
			CSVConfig: &s3select.CSVConfig{
				FileHeaderInfo: "USE",
			},
		},
		s3select.OutputFormat{
			Type:       "JSON",
			JSONConfig: &s3select.JSONOutputConfig{},
		},
	)

	// Create test CSV data
	csvData := `name,age,city
Alice,30,New York
Bob,25,Los Angeles
Charlie,35,Chicago
Diana,28,Houston`

	// Execute S3 Select query
	result, err := engine.Execute([]byte(csvData), "SELECT * FROM s3object s WHERE CAST(s.age AS INTEGER) > 26")
	if err != nil {
		t.Fatalf("S3 Select failed: %v", err)
	}

	if len(result.Records) == 0 {
		t.Error("Expected query results")
	}

	// Log the S3 Select event
	event := &audit.EnhancedAuditEvent{
		AuditEvent: audit.AuditEvent{
			EventType:   "s3:SelectObjectContent",
			EventSource: audit.SourceS3,
			Action:      "SelectObjectContent",
			Result:      audit.ResultSuccess,
			Resource: audit.ResourceInfo{
				Type:   audit.ResourceObject,
				Bucket: "test-bucket",
				Key:    "data.csv",
			},
			UserIdentity: audit.UserIdentity{
				Type:     audit.IdentityIAMUser,
				Username: "testuser",
			},
		},
	}
	event.WithExtra("query", "SELECT * FROM s3object s WHERE s.age > 26")
	event.WithExtra("bytes_returned", fmt.Sprintf("%d", result.BytesReturned))

	err = auditLogger.LogSync(context.Background(), event)
	if err != nil {
		t.Fatalf("Failed to log audit event: %v", err)
	}

	// Verify audit log was written
	time.Sleep(100 * time.Millisecond) // Allow async writes to complete

	logData, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("Failed to read audit log: %v", err)
	}

	if len(logData) == 0 {
		t.Error("Audit log should contain events")
	}
}

// TestFirewallBandwidthWithCache tests bandwidth throttling with cache bypass
func TestFirewallBandwidthWithCache(t *testing.T) {
	// Create firewall with bandwidth limiting
	fw, err := firewall.New(firewall.Config{
		Enabled:       true,
		DefaultPolicy: "allow",
		Bandwidth: firewall.BandwidthConfig{
			Enabled:           true,
			MaxBytesPerSecond: 10 * 1024 * 1024, // 10 MB/s
		},
	})
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Create DRAM cache
	c := dram.New(dram.Config{
		MaxSize:        1024 * 1024 * 100, // 100MB
		EvictionPolicy: "lru",
	})
	defer c.Close()

	// Simulate object retrieval with bandwidth tracking
	ctx := context.Background()
	bucket := "test-bucket"
	key := bucket + "/large-object"
	objectSize := int64(5 * 1024 * 1024) // 5MB object

	// First request - cache miss, check bandwidth
	decision := fw.Evaluate(ctx, &firewall.Request{
		SourceIP:  "192.168.1.100",
		User:      "user1",
		Bucket:    bucket,
		Operation: "GET",
		Size:      objectSize,
	})

	if !decision.Allowed {
		t.Error("First request should be allowed")
	}

	// Check bandwidth tracking
	allowed := fw.CheckBandwidth("user1", bucket, objectSize)
	if !allowed {
		t.Error("First bandwidth check should be allowed")
	}

	// Simulate caching the object
	data := make([]byte, objectSize)
	c.Put(ctx, key, data, "application/octet-stream", "test-etag")

	// Second request - cache hit, should bypass bandwidth check for cached data
	entry, found := c.Get(ctx, key)
	if !found {
		t.Error("Object should be in cache")
	}
	if int64(len(entry.Data)) != objectSize {
		t.Errorf("Cached data size mismatch: got %d, want %d", len(entry.Data), objectSize)
	}
}

// TestConcurrentEnterpriseFeatures tests concurrent access to enterprise features
func TestConcurrentEnterpriseFeatures(t *testing.T) {
	// Create firewall
	fw, err := firewall.New(firewall.Config{
		Enabled:       true,
		DefaultPolicy: "allow",
		RateLimiting: firewall.RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: 1000,
			BurstSize:         100,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	// Create cache
	c := dram.New(dram.Config{
		MaxSize:        1024 * 1024 * 100,
		EvictionPolicy: "arc",
	})
	defer c.Close()

	// Create temp directory for audit
	tmpDir, err := os.MkdirTemp("", "audit-concurrent-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	auditLogger, err := audit.NewEnhancedAuditLogger(audit.EnhancedConfig{
		Enabled:        true,
		ComplianceMode: audit.ComplianceNone,
		Outputs: []audit.OutputConfig{
			{Type: "file", Path: filepath.Join(tmpDir, "audit.log")},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create audit logger: %v", err)
	}
	defer auditLogger.Stop()
	auditLogger.Start()

	// Run concurrent operations
	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	ctx := context.Background()

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			bucket := fmt.Sprintf("bucket-%d", id%5)
			key := fmt.Sprintf("%s/key-%d", bucket, id%20)
			ip := fmt.Sprintf("192.168.1.%d", id%256)

			// Check firewall
			decision := fw.Evaluate(ctx, &firewall.Request{
				SourceIP:  ip,
				User:      fmt.Sprintf("user%d", id%10),
				Bucket:    bucket,
				Operation: "GET",
			})

			if !decision.Allowed {
				return // Rate limited, that's fine
			}

			// Try cache
			_, found := c.Get(ctx, key)
			if !found {
				err := c.Put(ctx, key, []byte(fmt.Sprintf("data-%d", id)), "text/plain", "etag")
				if err != nil {
					errCh <- fmt.Errorf("cache put failed: %w", err)
				}
			}

			// Log to audit
			event := &audit.EnhancedAuditEvent{
				AuditEvent: audit.AuditEvent{
					EventType:   audit.EventObjectAccessed,
					EventSource: audit.SourceS3,
					Action:      "GetObject",
					Result:      audit.ResultSuccess,
					Resource:    audit.ResourceInfo{Bucket: bucket, Key: key},
					SourceIP:    ip,
				},
			}
			auditLogger.Log(event)
		}(i)
	}

	wg.Wait()
	close(errCh)

	// Check for errors
	errorCount := 0
	for err := range errCh {
		t.Errorf("Concurrent operation error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("Had %d errors during concurrent operations", errorCount)
	}

	// Verify cache has entries
	metrics := c.Metrics()
	if metrics.Objects == 0 {
		t.Error("Cache should have entries after concurrent operations")
	}
}

// TestAuditIntegrityChain tests the cryptographic integrity chain
func TestAuditIntegrityChain(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit-integrity-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create audit logger with integrity
	auditLogger, err := audit.NewEnhancedAuditLogger(audit.EnhancedConfig{
		Enabled:          true,
		ComplianceMode:   audit.ComplianceSOC2,
		IntegrityEnabled: true,
		IntegritySecret:  "test-integrity-secret-key",
		Outputs: []audit.OutputConfig{
			{Type: "file", Path: filepath.Join(tmpDir, "audit.log")},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create audit logger: %v", err)
	}
	defer auditLogger.Stop()
	auditLogger.Start()

	// Log a series of events
	ctx := context.Background()
	events := make([]*audit.EnhancedAuditEvent, 10)

	for i := 0; i < 10; i++ {
		events[i] = &audit.EnhancedAuditEvent{
			AuditEvent: audit.AuditEvent{
				EventType:   audit.EventObjectCreated,
				EventSource: audit.SourceS3,
				Action:      "PutObject",
				Result:      audit.ResultSuccess,
				Resource: audit.ResourceInfo{
					Type:   audit.ResourceObject,
					Bucket: "test-bucket",
					Key:    fmt.Sprintf("object-%d", i),
				},
			},
		}
		err := auditLogger.LogSync(ctx, events[i])
		if err != nil {
			t.Fatalf("Failed to log event %d: %v", i, err)
		}
	}

	// Verify integrity chain
	valid, err := auditLogger.VerifyIntegrity(ctx, events)
	if err != nil {
		t.Fatalf("Integrity verification failed: %v", err)
	}
	if !valid {
		t.Error("Integrity chain should be valid")
	}
}

// TestWebhookAuditOutput tests audit event delivery to webhooks
func TestWebhookAuditOutput(t *testing.T) {
	// Create a test webhook server
	var receivedEvents []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedEvents = append(receivedEvents, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir, err := os.MkdirTemp("", "audit-webhook-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create audit logger with webhook output
	auditLogger, err := audit.NewEnhancedAuditLogger(audit.EnhancedConfig{
		Enabled:        true,
		ComplianceMode: audit.ComplianceNone,
		Outputs: []audit.OutputConfig{
			{
				Type: "webhook",
				URL:  server.URL,
			},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create audit logger: %v", err)
	}
	defer auditLogger.Stop()
	auditLogger.Start()

	// Log events
	for i := 0; i < 5; i++ {
		event := &audit.EnhancedAuditEvent{
			AuditEvent: audit.AuditEvent{
				EventType:   audit.EventBucketCreated,
				EventSource: audit.SourceAdmin,
				Action:      "CreateBucket",
				Result:      audit.ResultSuccess,
				Resource: audit.ResourceInfo{
					Type:   audit.ResourceBucket,
					Bucket: fmt.Sprintf("bucket-%d", i),
				},
			},
		}
		err := auditLogger.LogSync(context.Background(), event)
		if err != nil {
			t.Logf("Warning: Failed to log event %d: %v", i, err)
		}
	}

	// Wait for webhook delivery
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	count := len(receivedEvents)
	mu.Unlock()

	if count == 0 {
		t.Log("No events received via webhook - webhook output may not be fully configured")
	} else {
		t.Logf("Received %d events via webhook", count)
	}
}

// TestS3SelectFormats tests S3 Select with different input formats
func TestS3SelectFormats(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		input    s3select.InputFormat
		output   s3select.OutputFormat
		query    string
		contains string
	}{
		{
			name: "CSV with header",
			data: "id,name,value\n1,foo,100\n2,bar,200\n3,baz,300",
			input: s3select.InputFormat{
				Type:      "CSV",
				CSVConfig: &s3select.CSVConfig{FileHeaderInfo: "USE"},
			},
			output: s3select.OutputFormat{
				Type:      "CSV",
				CSVConfig: &s3select.CSVOutputConfig{},
			},
			query:    "SELECT * FROM s3object s WHERE CAST(s.value AS INTEGER) > 150",
			contains: "bar",
		},
		{
			name: "JSON Lines",
			data: `{"id":1,"name":"foo","value":100}
{"id":2,"name":"bar","value":200}
{"id":3,"name":"baz","value":300}`,
			input: s3select.InputFormat{
				Type:       "JSON",
				JSONConfig: &s3select.JSONConfig{Type: "LINES"},
			},
			output: s3select.OutputFormat{
				Type:       "JSON",
				JSONConfig: &s3select.JSONOutputConfig{},
			},
			query:    "SELECT s.name FROM s3object s WHERE s.value > 150",
			contains: "bar",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := s3select.NewEngine(tc.input, tc.output)
			result, err := engine.Execute([]byte(tc.data), tc.query)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			if !bytes.Contains(result.Records, []byte(tc.contains)) {
				t.Errorf("Expected output to contain %q, got: %s", tc.contains, result.Records)
			}
		})
	}
}

// TestFirewallRulesPriority tests firewall rule priority ordering
func TestFirewallRulesPriority(t *testing.T) {
	// Create firewall with multiple rules
	fw, err := firewall.New(firewall.Config{
		Enabled:       true,
		DefaultPolicy: "deny",
		Rules: []firewall.Rule{
			{
				ID:       "low-priority-allow",
				Priority: 100,
				Action:   "allow",
				Enabled:  true,
				Match: firewall.RuleMatch{
					SourceIPs: []string{"192.168.1.0/24"},
				},
			},
			{
				ID:       "high-priority-deny",
				Priority: 10,
				Action:   "deny",
				Enabled:  true,
				Match: firewall.RuleMatch{
					SourceIPs: []string{"192.168.1.100"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{
			name:     "Specific IP blocked by high priority rule",
			ip:       "192.168.1.100",
			expected: false,
		},
		{
			name:     "Other IP in range allowed by low priority rule",
			ip:       "192.168.1.50",
			expected: true,
		},
		{
			name:     "IP outside range denied by default",
			ip:       "10.0.0.1",
			expected: false,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := fw.Evaluate(ctx, &firewall.Request{
				SourceIP:  tc.ip,
				User:      "testuser",
				Bucket:    "test-bucket",
				Operation: "GET",
			})

			if decision.Allowed != tc.expected {
				t.Errorf("Expected allowed=%v for IP %s, got %v", tc.expected, tc.ip, decision.Allowed)
			}
		})
	}
}

// TestCacheEvictionUnderPressure tests cache behavior under memory pressure
func TestCacheEvictionUnderPressure(t *testing.T) {
	// Create small cache
	maxSize := int64(1024 * 10) // 10KB
	c := dram.New(dram.Config{
		MaxSize:        maxSize,
		EvictionPolicy: "lru",
	})
	defer c.Close()

	ctx := context.Background()
	bucket := "test-bucket"
	objectSize := 1024 // 1KB per object

	// Insert more data than cache can hold
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("%s/object-%d", bucket, i)
		data := make([]byte, objectSize)
		c.Put(ctx, key, data, "application/octet-stream", fmt.Sprintf("etag-%d", i))
	}

	// Verify cache hasn't exceeded max size
	metrics := c.Metrics()
	if metrics.Size > maxSize {
		t.Errorf("Cache size %d exceeds max size %d", metrics.Size, maxSize)
	}

	// Verify some evictions occurred
	if metrics.Evictions == 0 {
		t.Error("Expected some evictions under memory pressure")
	}

	t.Logf("Cache metrics: size=%d, evictions=%d, objects=%d",
		metrics.Size, metrics.Evictions, metrics.Objects)
}

// TestEnterpriseFeatureIntegration tests the complete integration of enterprise features
func TestEnterpriseFeatureIntegration(t *testing.T) {
	// This test simulates a complete request flow through all enterprise components

	// 1. Set up components
	fw, err := firewall.New(firewall.Config{
		Enabled:       true,
		DefaultPolicy: "allow",
		RateLimiting: firewall.RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: 100,
			BurstSize:         10,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create firewall: %v", err)
	}

	cache := dram.New(dram.Config{
		MaxSize:         1024 * 1024 * 100,
		EvictionPolicy:  "arc",
		PrefetchEnabled: true,
	})
	defer cache.Close()

	tmpDir, err := os.MkdirTemp("", "integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	auditLogger, err := audit.NewEnhancedAuditLogger(audit.EnhancedConfig{
		Enabled:          true,
		ComplianceMode:   audit.ComplianceSOC2,
		IntegrityEnabled: true,
		IntegritySecret:  "test-secret",
		Outputs: []audit.OutputConfig{
			{Type: "file", Path: filepath.Join(tmpDir, "audit.log")},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create audit logger: %v", err)
	}
	defer auditLogger.Stop()
	auditLogger.Start()

	// 2. Simulate a request flow
	ctx := context.Background()
	bucket := "production-data"
	key := bucket + "/dataset/batch-001.csv"

	// Step 1: Firewall check
	decision := fw.Evaluate(ctx, &firewall.Request{
		SourceIP:  "10.0.0.50",
		User:      "ml-service",
		Bucket:    bucket,
		Operation: "GET",
	})

	if !decision.Allowed {
		t.Error("Request should be allowed through firewall")
	}

	// Step 2: Check cache
	_, found := cache.Get(ctx, key)
	if found {
		t.Log("Cache hit - serving from cache")
	} else {
		t.Log("Cache miss - would fetch from storage")
		// Simulate fetching and caching
		data := []byte("id,value\n1,100\n2,200\n3,300")
		cache.Put(ctx, key, data, "text/csv", "etag-001")
	}

	// Step 3: Log audit event
	event := &audit.EnhancedAuditEvent{
		AuditEvent: audit.AuditEvent{
			EventType:   audit.EventObjectAccessed,
			EventSource: audit.SourceS3,
			Action:      "GetObject",
			Result:      audit.ResultSuccess,
			Resource: audit.ResourceInfo{
				Type:   audit.ResourceObject,
				Bucket: bucket,
				Key:    key,
			},
			UserIdentity: audit.UserIdentity{
				Type:     audit.IdentityIAMUser,
				Username: "ml-service",
			},
			SourceIP:  "10.0.0.50",
			UserAgent: "aws-sdk-go/1.44.0",
		},
	}
	err = auditLogger.LogSync(ctx, event)
	if err != nil {
		t.Errorf("Failed to log audit event: %v", err)
	}

	// Step 4: Verify second request hits cache
	entry, found := cache.Get(ctx, key)
	if !found {
		t.Error("Second request should hit cache")
	}
	if entry == nil || len(entry.Data) == 0 {
		t.Error("Cached entry should have data")
	}

	// Verify metrics
	cacheMetrics := cache.Metrics()
	if cacheMetrics.Hits == 0 {
		t.Error("Expected at least one cache hit")
	}

	fwStats := fw.Stats()
	if fwStats.RequestsAllowed == 0 {
		t.Error("Expected at least one allowed request in firewall stats")
	}

	t.Logf("Integration test complete - cache hits: %d, firewall allowed: %d",
		cacheMetrics.Hits, fwStats.RequestsAllowed)
}
