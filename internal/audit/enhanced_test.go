package audit

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockAuditStore implements AuditStore for testing.
type mockAuditStore struct {
	events []*AuditEvent
	mu     sync.Mutex
}

func (m *mockAuditStore) StoreAuditEvent(ctx context.Context, event *AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.events = append(m.events, event)

	return nil
}

func (m *mockAuditStore) ListAuditEvents(ctx context.Context, filter AuditFilter) (*AuditListResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := &AuditListResult{
		Events: make([]AuditEvent, 0),
	}

	for _, e := range m.events {
		// Apply filters
		if !filter.StartTime.IsZero() && e.Timestamp.Before(filter.StartTime) {
			continue
		}

		if !filter.EndTime.IsZero() && e.Timestamp.After(filter.EndTime) {
			continue
		}

		if filter.Bucket != "" && e.Resource.Bucket != filter.Bucket {
			continue
		}

		if filter.User != "" && e.UserIdentity.Username != filter.User {
			continue
		}

		if filter.EventType != "" && string(e.EventType) != filter.EventType {
			continue
		}

		if filter.Result != "" && string(e.Result) != filter.Result {
			continue
		}

		result.Events = append(result.Events, *e)

		if filter.MaxResults > 0 && len(result.Events) >= filter.MaxResults {
			break
		}
	}

	return result, nil
}

// mockGeoLookup implements GeoLookup for testing.
type mockGeoLookup struct{}

func (m *mockGeoLookup) Lookup(ip string) (*GeoLocation, error) {
	return &GeoLocation{
		Country: "US",
		Region:  "California",
		City:    "San Francisco",
	}, nil
}

func TestEnhancedAuditLoggerBasic(t *testing.T) {
	config := DefaultEnhancedConfig()
	config.IntegrityEnabled = true
	config.IntegritySecret = "test-secret-key"

	store := &mockAuditStore{}

	logger, err := NewEnhancedAuditLogger(config, store, &mockGeoLookup{})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Start()
	defer logger.Stop()

	// Log an event
	event := &EnhancedAuditEvent{
		AuditEvent: AuditEvent{
			EventType:   EventObjectCreated,
			EventSource: SourceS3,
			Action:      "PutObject",
			Result:      ResultSuccess,
			UserIdentity: UserIdentity{
				Type:     IdentityIAMUser,
				Username: "testuser",
			},
			Resource: ResourceInfo{
				Type:   ResourceObject,
				Bucket: "test-bucket",
				Key:    "test-key.txt",
			},
			SourceIP: "192.168.1.100",
		},
	}

	logger.Log(event)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	stats := logger.Stats()
	if stats.EventsProcessed != 1 {
		t.Errorf("Expected 1 event processed, got %d", stats.EventsProcessed)
	}
}

func TestEnhancedAuditLoggerWithFileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	config := DefaultEnhancedConfig()
	config.Outputs = []OutputConfig{
		{
			Type:    OutputFile,
			Enabled: true,
			Name:    "test-file",
			Path:    logPath,
		},
	}

	store := &mockAuditStore{}

	logger, err := NewEnhancedAuditLogger(config, store, nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Start()

	// Log multiple events synchronously for reliable test
	for i := range 10 {
		event := &EnhancedAuditEvent{
			AuditEvent: AuditEvent{
				EventType:   EventObjectCreated,
				EventSource: SourceS3,
				Action:      "PutObject",
				Result:      ResultSuccess,
				UserIdentity: UserIdentity{
					Username: "user" + string(rune('0'+i)),
				},
				Resource: ResourceInfo{
					Bucket: "bucket",
					Key:    "key" + string(rune('0'+i)),
				},
			},
		}
		err := logger.LogSync(context.Background(), event)
		if err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Stop and flush
	logger.Stop()

	// Read log file
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	// Count lines
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Errorf("Expected 10 log lines, got %d", len(lines))
	}

	// Verify JSON parsing
	for _, line := range lines {
		var event EnhancedAuditEvent
		err := json.Unmarshal([]byte(line), &event)
		if err != nil {
			t.Errorf("Failed to parse log line: %v", err)
		}
	}
}

func TestEnhancedAuditLoggerIntegrity(t *testing.T) {
	config := DefaultEnhancedConfig()
	config.IntegrityEnabled = true
	config.IntegritySecret = "test-secret-key"

	store := &mockAuditStore{}

	logger, err := NewEnhancedAuditLogger(config, store, nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Start()

	// Create events with chain
	events := make([]*EnhancedAuditEvent, 5)
	for i := range 5 {
		events[i] = &EnhancedAuditEvent{
			AuditEvent: AuditEvent{
				EventType:   EventObjectCreated,
				EventSource: SourceS3,
				Action:      "PutObject",
				Result:      ResultSuccess,
				UserIdentity: UserIdentity{
					Username: "user",
				},
				Resource: ResourceInfo{
					Bucket: "bucket",
					Key:    "key" + string(rune('0'+i)),
				},
			},
		}
		err := logger.LogSync(context.Background(), events[i])
		if err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	logger.Stop()

	// Verify integrity chain
	valid, err := logger.VerifyIntegrity(context.Background(), events)
	if err != nil {
		t.Fatalf("Integrity verification failed: %v", err)
	}

	if !valid {
		t.Error("Integrity check should be valid")
	}

	stats := logger.Stats()
	if stats.IntegrityVerified != 5 {
		t.Errorf("Expected 5 integrity verifications, got %d", stats.IntegrityVerified)
	}
}

func TestEnhancedAuditLoggerFiltering(t *testing.T) {
	config := DefaultEnhancedConfig()
	config.FilterRules = []FilterRule{
		{
			Name:   "exclude-list-ops",
			Action: "exclude",
			EventTypes: []EventType{
				EventBucketListed,
			},
		},
	}

	store := &mockAuditStore{}

	logger, err := NewEnhancedAuditLogger(config, store, nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Start()

	// Log a PUT event (should be included)
	putEvent := &EnhancedAuditEvent{
		AuditEvent: AuditEvent{
			EventType: EventObjectCreated,
		},
	}
	logger.Log(putEvent)

	// Log a LIST event (should be excluded)
	listEvent := &EnhancedAuditEvent{
		AuditEvent: AuditEvent{
			EventType: EventBucketListed,
		},
	}
	logger.Log(listEvent)

	time.Sleep(100 * time.Millisecond)
	logger.Stop()

	stats := logger.Stats()
	if stats.EventsProcessed != 1 {
		t.Errorf("Expected 1 event processed (LIST excluded), got %d", stats.EventsProcessed)
	}
}

func TestEnhancedAuditLoggerQuery(t *testing.T) {
	store := &mockAuditStore{}
	config := DefaultEnhancedConfig()

	logger, err := NewEnhancedAuditLogger(config, store, nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Start()

	// Log events
	now := time.Now()
	for i := range 10 {
		event := &EnhancedAuditEvent{
			AuditEvent: AuditEvent{
				Timestamp:   now.Add(time.Duration(i) * time.Minute),
				EventType:   EventObjectCreated,
				EventSource: SourceS3,
				Result:      ResultSuccess,
				UserIdentity: UserIdentity{
					Username: "user" + string(rune('0'+i%3)),
				},
				Resource: ResourceInfo{
					Bucket: "bucket" + string(rune('0'+i%2)),
				},
			},
		}
		logger.LogSync(context.Background(), event)
	}

	logger.Stop()

	// Query by bucket
	result, err := logger.Query(context.Background(), AuditFilter{
		Bucket: "bucket0",
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Events) != 5 {
		t.Errorf("Expected 5 events for bucket0, got %d", len(result.Events))
	}

	// Query with max results
	result, err = logger.Query(context.Background(), AuditFilter{
		MaxResults: 3,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Events) != 3 {
		t.Errorf("Expected 3 events with limit, got %d", len(result.Events))
	}
}

func TestEnhancedAuditLoggerMasking(t *testing.T) {
	config := DefaultEnhancedConfig()
	config.MaskSensitiveData = true
	config.SensitiveFields = []string{"password", "secret", "authorization"}

	store := &mockAuditStore{}

	logger, err := NewEnhancedAuditLogger(config, store, nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Start()

	event := &EnhancedAuditEvent{
		AuditEvent: AuditEvent{
			EventType: EventAuthLogin,
			Extra: map[string]string{
				"password": "secret123",
				"username": "testuser",
			},
		},
		RequestHeaders: map[string]string{
			"Authorization": "Bearer token123",
			"Content-Type":  "application/json",
		},
	}

	logger.LogSync(context.Background(), event)
	logger.Stop()

	// Check that sensitive data is masked
	if event.Extra["password"] != "***MASKED***" {
		t.Error("Password should be masked")
	}

	if event.Extra["username"] == "***MASKED***" {
		t.Error("Username should not be masked")
	}

	if event.RequestHeaders["Authorization"] != "***MASKED***" {
		t.Error("Authorization header should be masked")
	}

	if event.RequestHeaders["Content-Type"] == "***MASKED***" {
		t.Error("Content-Type should not be masked")
	}
}

func TestEnhancedAuditLoggerCompliance(t *testing.T) {
	tests := []struct {
		mode       ComplianceMode
		eventType  EventType
		expectCtls bool
	}{
		{ComplianceSOC2, EventAuthLogin, true},
		{ComplianceSOC2, EventObjectCreated, true},
		{CompliancePCI, EventUserCreated, true},
		{ComplianceHIPAA, EventObjectAccessed, true},
		{ComplianceGDPR, EventPolicyCreated, true},
		{ComplianceNone, EventObjectCreated, false},
	}

	for _, tc := range tests {
		t.Run(string(tc.mode)+"-"+string(tc.eventType), func(t *testing.T) {
			config := DefaultEnhancedConfig()
			config.ComplianceMode = tc.mode

			store := &mockAuditStore{}

			logger, err := NewEnhancedAuditLogger(config, store, nil)
			if err != nil {
				t.Fatalf("Failed to create logger: %v", err)
			}

			logger.Start()

			event := &EnhancedAuditEvent{
				AuditEvent: AuditEvent{
					EventType: tc.eventType,
				},
			}

			logger.LogSync(context.Background(), event)
			logger.Stop()

			if tc.expectCtls {
				if event.Compliance == nil {
					t.Error("Expected compliance info")
				} else if event.Compliance.Framework != tc.mode {
					t.Errorf("Expected framework %s, got %s", tc.mode, event.Compliance.Framework)
				}
			} else {
				if event.Compliance != nil {
					t.Error("Did not expect compliance info for ComplianceNone")
				}
			}
		})
	}
}

func TestEnhancedAuditLoggerExport(t *testing.T) {
	store := &mockAuditStore{}
	config := DefaultEnhancedConfig()

	logger, err := NewEnhancedAuditLogger(config, store, nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Start()

	// Log events
	for i := range 5 {
		event := &EnhancedAuditEvent{
			AuditEvent: AuditEvent{
				Timestamp:   time.Now(),
				EventType:   EventObjectCreated,
				EventSource: SourceS3,
				Action:      "PutObject",
				Result:      ResultSuccess,
				UserIdentity: UserIdentity{
					Username: "user",
				},
				Resource: ResourceInfo{
					Bucket: "bucket",
					Key:    "key" + string(rune('0'+i)),
				},
				DurationMS: int64(i * 10),
			},
		}
		logger.LogSync(context.Background(), event)
	}

	logger.Stop()

	// Test JSON export
	t.Run("JSON", func(t *testing.T) {
		var buf strings.Builder

		err := logger.Export(context.Background(), AuditFilter{}, "json", &buf)
		if err != nil {
			t.Fatalf("JSON export failed: %v", err)
		}

		var events []AuditEvent
		if err := json.Unmarshal([]byte(buf.String()), &events); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}

		if len(events) != 5 {
			t.Errorf("Expected 5 events, got %d", len(events))
		}
	})

	// Test CSV export
	t.Run("CSV", func(t *testing.T) {
		var buf strings.Builder

		err := logger.Export(context.Background(), AuditFilter{}, "csv", &buf)
		if err != nil {
			t.Fatalf("CSV export failed: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 6 { // Header + 5 events
			t.Errorf("Expected 6 lines, got %d", len(lines))
		}
	})

	// Test CEF export
	t.Run("CEF", func(t *testing.T) {
		var buf strings.Builder

		err := logger.Export(context.Background(), AuditFilter{}, "cef", &buf)
		if err != nil {
			t.Fatalf("CEF export failed: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 5 {
			t.Errorf("Expected 5 CEF lines, got %d", len(lines))
		}

		if !strings.HasPrefix(lines[0], "CEF:0|NebulaIO|") {
			t.Error("CEF format incorrect")
		}
	})
}

func TestEnhancedAuditLoggerGeoLocation(t *testing.T) {
	config := DefaultEnhancedConfig()

	store := &mockAuditStore{}

	logger, err := NewEnhancedAuditLogger(config, store, &mockGeoLookup{})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Start()

	event := &EnhancedAuditEvent{
		AuditEvent: AuditEvent{
			EventType: EventObjectAccessed,
			SourceIP:  "8.8.8.8",
		},
	}

	logger.LogSync(context.Background(), event)
	logger.Stop()

	if event.GeoLocation == nil {
		t.Error("Expected geo location")
	} else if event.GeoLocation.Country != "US" {
		t.Errorf("Expected country US, got %s", event.GeoLocation.Country)
	}
}

func TestEnhancedAuditLoggerConcurrency(t *testing.T) {
	config := DefaultEnhancedConfig()
	config.BufferSize = 1000

	store := &mockAuditStore{}

	logger, err := NewEnhancedAuditLogger(config, store, nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Start()

	var wg sync.WaitGroup

	numGoroutines := 10
	eventsPerGoroutine := 100

	for i := range numGoroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for range eventsPerGoroutine {
				event := &EnhancedAuditEvent{
					AuditEvent: AuditEvent{
						EventType: EventObjectCreated,
						UserIdentity: UserIdentity{
							Username: "user" + string(rune('0'+id)),
						},
					},
				}
				logger.Log(event)
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)
	logger.Stop()

	stats := logger.Stats()
	expectedTotal := int64(numGoroutines * eventsPerGoroutine)
	actualTotal := stats.EventsProcessed + stats.EventsDropped

	if actualTotal != expectedTotal {
		t.Errorf("Expected %d total events, got %d processed + %d dropped = %d",
			expectedTotal, stats.EventsProcessed, stats.EventsDropped, actualTotal)
	}
}

func TestFileOutputRotation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	cfg := OutputConfig{
		Type:    OutputFile,
		Enabled: true,
		Name:    "rotation-test",
		Path:    logPath,
	}

	rotation := RotationConfig{
		Enabled:   true,
		MaxSizeMB: 1, // 1MB
	}

	output, err := newFileOutput(cfg, rotation)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}
	defer output.Close()

	// Write enough data to trigger rotation
	largeData := make([]byte, 1024) // 1KB
	for i := range largeData {
		largeData[i] = 'x'
	}

	event := &EnhancedAuditEvent{
		AuditEvent: AuditEvent{
			Extra: map[string]string{
				"data": string(largeData),
			},
		},
	}

	// Write 1500 events (~1.5MB) to trigger rotation
	for range 1500 {
		err := output.Write(context.Background(), event)
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	output.Flush(context.Background())

	// Check for rotated files
	files, err := filepath.Glob(logPath + "*")
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}

	if len(files) < 2 {
		t.Errorf("Expected at least 2 files after rotation, got %d", len(files))
	}
}

func TestWebhookOutput(t *testing.T) {
	cfg := OutputConfig{
		Type:      OutputWebhook,
		Enabled:   true,
		Name:      "test-webhook",
		URL:       "http://localhost:9999/audit", // Won't actually connect
		BatchSize: 5,
	}

	output, err := newWebhookOutput(cfg)
	if err != nil {
		t.Fatalf("Failed to create output: %v", err)
	}
	defer output.Close()

	// Write events (won't actually send since endpoint doesn't exist)
	for range 3 {
		event := &EnhancedAuditEvent{
			AuditEvent: AuditEvent{
				EventType: EventObjectCreated,
			},
		}
		output.Write(context.Background(), event) // Error expected, but shouldn't panic
	}
}

func TestDefaultEnhancedConfig(t *testing.T) {
	cfg := DefaultEnhancedConfig()

	if !cfg.Enabled {
		t.Error("Default should be enabled")
	}

	if cfg.ComplianceMode != ComplianceNone {
		t.Error("Default compliance mode should be none")
	}

	if cfg.RetentionDays != 90 {
		t.Errorf("Default retention should be 90 days, got %d", cfg.RetentionDays)
	}

	if cfg.BufferSize != 10000 {
		t.Errorf("Default buffer size should be 10000, got %d", cfg.BufferSize)
	}

	if !cfg.IntegrityEnabled {
		t.Error("Default should have integrity enabled")
	}

	if !cfg.MaskSensitiveData {
		t.Error("Default should mask sensitive data")
	}
}

// BenchmarkEnhancedAuditLogger benchmarks the logger.
func BenchmarkEnhancedAuditLogger(b *testing.B) {
	config := DefaultEnhancedConfig()
	config.IntegrityEnabled = false // Disable for pure throughput

	store := &mockAuditStore{}
	logger, _ := NewEnhancedAuditLogger(config, store, nil)

	logger.Start()
	defer logger.Stop()

	event := &EnhancedAuditEvent{
		AuditEvent: AuditEvent{
			EventType:   EventObjectCreated,
			EventSource: SourceS3,
			Action:      "PutObject",
			Result:      ResultSuccess,
			UserIdentity: UserIdentity{
				Username: "user",
			},
			Resource: ResourceInfo{
				Bucket: "bucket",
				Key:    "key",
			},
		},
	}

	b.ResetTimer()

	for range b.N {
		logger.Log(event)
	}
}

// TestExportToWriter tests the export functionality to io.Writer.
func TestExportToWriter(t *testing.T) {
	store := &mockAuditStore{}
	config := DefaultEnhancedConfig()

	logger, _ := NewEnhancedAuditLogger(config, store, nil)
	logger.Start()

	for range 3 {
		event := &EnhancedAuditEvent{
			AuditEvent: AuditEvent{
				Timestamp:  time.Now(),
				EventType:  EventObjectCreated,
				DurationMS: 100,
			},
		}
		logger.LogSync(context.Background(), event)
	}

	logger.Stop()

	// Test unsupported format
	err := logger.Export(context.Background(), AuditFilter{}, "xml", io.Discard)
	if err == nil {
		t.Error("Expected error for unsupported format")
	}
}
