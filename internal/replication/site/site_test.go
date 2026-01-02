package site

import (
	"context"
	"testing"
	"time"
)

func TestSiteValidation(t *testing.T) {
	t.Run("ValidSite", func(t *testing.T) {
		site := Site{
			Name:      "site1",
			Endpoint:  "s3.site1.example.com",
			AccessKey: "AKIAIOSFODNN7EXAMPLE",
			SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		}

		err := site.Validate()
		if err != nil {
			t.Errorf("valid site should not error: %v", err)
		}
	})

	t.Run("MissingSiteName", func(t *testing.T) {
		site := Site{
			Endpoint:  "s3.site1.example.com",
			AccessKey: "key",
			SecretKey: "secret",
		}

		err := site.Validate()
		if err == nil {
			t.Error("expected error for missing site name")
		}
	})

	t.Run("MissingEndpoint", func(t *testing.T) {
		site := Site{
			Name:      "site1",
			AccessKey: "key",
			SecretKey: "secret",
		}

		err := site.Validate()
		if err == nil {
			t.Error("expected error for missing endpoint")
		}
	})

	t.Run("MissingCredentials", func(t *testing.T) {
		site := Site{
			Name:     "site1",
			Endpoint: "s3.site1.example.com",
		}

		err := site.Validate()
		if err == nil {
			t.Error("expected error for missing credentials")
		}
	})
}

func TestConfigValidation(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		cfg := Config{
			Sites: []Site{
				{
					Name:      "local",
					Endpoint:  "s3.local.example.com",
					AccessKey: "key",
					SecretKey: "secret",
					IsLocal:   true,
				},
				{
					Name:      "remote",
					Endpoint:  "s3.remote.example.com",
					AccessKey: "key",
					SecretKey: "secret",
				},
			},
			HealthCheckInterval: time.Minute,
			SyncInterval:        5 * time.Minute,
		}

		err := cfg.Validate()
		if err != nil {
			t.Errorf("valid config should not error: %v", err)
		}
	})

	t.Run("NoSites", func(t *testing.T) {
		cfg := Config{
			Sites: []Site{},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for no sites")
		}
	})

	t.Run("DuplicateSiteNames", func(t *testing.T) {
		cfg := Config{
			Sites: []Site{
				{
					Name:      "site1",
					Endpoint:  "s3.site1.example.com",
					AccessKey: "key",
					SecretKey: "secret",
					IsLocal:   true,
				},
				{
					Name:      "site1", // Duplicate
					Endpoint:  "s3.site2.example.com",
					AccessKey: "key",
					SecretKey: "secret",
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for duplicate site names")
		}
	})

	t.Run("MultipleLocalSites", func(t *testing.T) {
		cfg := Config{
			Sites: []Site{
				{
					Name:      "local1",
					Endpoint:  "s3.local1.example.com",
					AccessKey: "key",
					SecretKey: "secret",
					IsLocal:   true,
				},
				{
					Name:      "local2",
					Endpoint:  "s3.local2.example.com",
					AccessKey: "key",
					SecretKey: "secret",
					IsLocal:   true,
				},
			},
		}

		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for multiple local sites")
		}
	})
}

func TestConfigHelpers(t *testing.T) {
	cfg := Config{
		Sites: []Site{
			{
				Name:      "local",
				Endpoint:  "s3.local.example.com",
				AccessKey: "key",
				SecretKey: "secret",
				IsLocal:   true,
			},
			{
				Name:      "remote1",
				Endpoint:  "s3.remote1.example.com",
				AccessKey: "key",
				SecretKey: "secret",
			},
			{
				Name:      "remote2",
				Endpoint:  "s3.remote2.example.com",
				AccessKey: "key",
				SecretKey: "secret",
			},
		},
	}

	t.Run("GetLocalSite", func(t *testing.T) {
		local := cfg.GetLocalSite()
		if local == nil {
			t.Fatal("should return local site")
			return
		}

		if local.Name != "local" {
			t.Errorf("expected local site name 'local', got %s", local.Name)
		}
	})

	t.Run("GetRemoteSites", func(t *testing.T) {
		remotes := cfg.GetRemoteSites()
		if len(remotes) != 2 {
			t.Errorf("expected 2 remote sites, got %d", len(remotes))
		}
	})

	t.Run("GetSite", func(t *testing.T) {
		site := cfg.GetSite("remote1")
		if site == nil {
			t.Fatal("should find site remote1")
		}

		if site.Name != "remote1" {
			t.Errorf("expected site name 'remote1', got %s", site.Name)
		}

		notFound := cfg.GetSite("nonexistent")
		if notFound != nil {
			t.Error("should return nil for nonexistent site")
		}
	})
}

func TestVectorClock(t *testing.T) {
	t.Run("Increment", func(t *testing.T) {
		vc := NewVectorClock()
		vc.Increment("node1")

		if vc.Get("node1") != 1 {
			t.Errorf("expected clock value 1, got %d", vc.Get("node1"))
		}

		vc.Increment("node1")

		if vc.Get("node1") != 2 {
			t.Errorf("expected clock value 2, got %d", vc.Get("node1"))
		}
	})

	t.Run("Merge", func(t *testing.T) {
		vc1 := NewVectorClock()
		vc1.Increment("node1")
		vc1.Increment("node1")

		vc2 := NewVectorClock()
		vc2.Increment("node2")
		vc2.Increment("node2")
		vc2.Increment("node2")

		vc1.Merge(vc2)

		if vc1.Get("node1") != 2 {
			t.Errorf("expected node1 clock value 2, got %d", vc1.Get("node1"))
		}

		if vc1.Get("node2") != 3 {
			t.Errorf("expected node2 clock value 3, got %d", vc1.Get("node2"))
		}
	})

	t.Run("Compare_HappensBefore", func(t *testing.T) {
		vc1 := NewVectorClock()
		vc1.Increment("node1")

		vc2 := NewVectorClock()
		vc2.Increment("node1")
		vc2.Increment("node1")

		// vc1 happens before vc2
		if vc1.Compare(vc2) != -1 {
			t.Error("vc1 should happen before vc2")
		}

		// vc2 happens after vc1
		if vc2.Compare(vc1) != 1 {
			t.Error("vc2 should happen after vc1")
		}
	})

	t.Run("Compare_Concurrent", func(t *testing.T) {
		vc1 := NewVectorClock()
		vc1.Increment("node1")

		vc2 := NewVectorClock()
		vc2.Increment("node2")

		// Concurrent events
		if vc1.Compare(vc2) != 0 {
			t.Error("vc1 and vc2 should be concurrent")
		}
	})

	t.Run("Compare_Equal", func(t *testing.T) {
		vc1 := NewVectorClock()
		vc1.Increment("node1")

		vc2 := NewVectorClock()
		vc2.Increment("node1")

		// Equal clocks
		if vc1.Compare(vc2) != 0 {
			t.Error("equal clocks should return 0")
		}
	})

	t.Run("Copy", func(t *testing.T) {
		vc1 := NewVectorClock()
		vc1.Increment("node1")
		vc1.Increment("node2")

		vc2 := vc1.Copy()
		vc2.Increment("node1")

		// Original should be unchanged
		if vc1.Get("node1") != 1 {
			t.Error("original should be unchanged after copy modification")
		}

		if vc2.Get("node1") != 2 {
			t.Error("copy should have incremented value")
		}
	})
}

func TestConflictResolution(t *testing.T) {
	handler := &defaultConflictHandler{
		resolution: ConflictLastWriteWins,
		localSite:  "local",
	}

	t.Run("LastWriteWins_ByTimestamp", func(t *testing.T) {
		now := time.Now()
		local := &ObjectVersion{
			VersionID:    "v1",
			LastModified: now.Add(-time.Hour),
		}
		remote := &ObjectVersion{
			VersionID:    "v2",
			LastModified: now,
		}

		winner, err := handler.Resolve(local, remote)
		if err != nil {
			t.Fatalf("resolve failed: %v", err)
		}

		if winner.VersionID != "v2" {
			t.Error("should pick remote (newer) version")
		}
	})

	t.Run("LastWriteWins_ByVectorClock", func(t *testing.T) {
		now := time.Now()

		localVC := NewVectorClock()
		localVC.Increment("local")
		localVC.Increment("local")

		remoteVC := NewVectorClock()
		remoteVC.Increment("remote")

		local := &ObjectVersion{
			VersionID:    "v1",
			LastModified: now,
			VectorClock:  localVC,
		}
		remote := &ObjectVersion{
			VersionID:    "v2",
			LastModified: now.Add(time.Hour), // Remote has newer timestamp
			VectorClock:  remoteVC,
		}

		// Local has higher vector clock, but they're concurrent
		// Should fall back to timestamp
		winner, err := handler.Resolve(local, remote)
		if err != nil {
			t.Fatalf("resolve failed: %v", err)
		}

		// Concurrent clocks fall back to timestamp
		if winner.VersionID != "v2" {
			t.Error("concurrent clocks should fall back to timestamp comparison")
		}
	})

	t.Run("LocalWins", func(t *testing.T) {
		handler := &defaultConflictHandler{
			resolution: ConflictLocalWins,
			localSite:  "local",
		}

		now := time.Now()
		local := &ObjectVersion{
			VersionID:    "v1",
			LastModified: now.Add(-time.Hour),
		}
		remote := &ObjectVersion{
			VersionID:    "v2",
			LastModified: now,
		}

		winner, err := handler.Resolve(local, remote)
		if err != nil {
			t.Fatalf("resolve failed: %v", err)
		}

		if winner.VersionID != "v1" {
			t.Error("local should always win with LocalWins strategy")
		}
	})

	t.Run("RemoteWins", func(t *testing.T) {
		handler := &defaultConflictHandler{
			resolution: ConflictRemoteWins,
			localSite:  "local",
		}

		now := time.Now()
		local := &ObjectVersion{
			VersionID:    "v1",
			LastModified: now,
		}
		remote := &ObjectVersion{
			VersionID:    "v2",
			LastModified: now.Add(-time.Hour),
		}

		winner, err := handler.Resolve(local, remote)
		if err != nil {
			t.Fatalf("resolve failed: %v", err)
		}

		if winner.VersionID != "v2" {
			t.Error("remote should always win with RemoteWins strategy")
		}
	})
}

func TestManagerCreation(t *testing.T) {
	t.Run("ValidManager", func(t *testing.T) {
		cfg := Config{
			Sites: []Site{
				{
					Name:      "local",
					Endpoint:  "s3.local.example.com",
					AccessKey: "key",
					SecretKey: "secret",
					IsLocal:   true,
				},
				{
					Name:      "remote",
					Endpoint:  "s3.remote.example.com",
					AccessKey: "key",
					SecretKey: "secret",
				},
			},
			HealthCheckInterval: time.Minute,
			SyncInterval:        5 * time.Minute,
			ConflictResolution:  ConflictLastWriteWins,
		}

		mgr, err := NewManager(cfg, DefaultManagerConfig())
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		if mgr.localSite != "local" {
			t.Errorf("expected local site 'local', got %s", mgr.localSite)
		}
	})

	t.Run("InvalidConfig", func(t *testing.T) {
		cfg := Config{
			Sites: []Site{}, // Invalid - no sites
		}

		_, err := NewManager(cfg, DefaultManagerConfig())
		if err == nil {
			t.Error("expected error for invalid config")
		}
	})

	t.Run("NoLocalSite", func(t *testing.T) {
		cfg := Config{
			Sites: []Site{
				{
					Name:      "remote",
					Endpoint:  "s3.remote.example.com",
					AccessKey: "key",
					SecretKey: "secret",
					// IsLocal: false (default)
				},
			},
		}

		_, err := NewManager(cfg, DefaultManagerConfig())
		if err == nil {
			t.Error("expected error for no local site")
		}
	})
}

func TestManagerOperations(t *testing.T) {
	cfg := Config{
		Sites: []Site{
			{
				Name:      "local",
				Endpoint:  "localhost:9000",
				AccessKey: "key",
				SecretKey: "secret",
				IsLocal:   true,
			},
		},
		HealthCheckInterval: time.Minute,
		SyncInterval:        5 * time.Minute,
		ConflictResolution:  ConflictLastWriteWins,
	}

	mgr, err := NewManager(cfg, DefaultManagerConfig())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	t.Run("GetSyncState", func(t *testing.T) {
		state, err := mgr.GetSyncState("local")
		if err != nil {
			t.Fatalf("failed to get sync state: %v", err)
		}

		if state == nil {
			t.Error("sync state should not be nil")
		}
	})

	t.Run("GetSyncState_NotFound", func(t *testing.T) {
		_, err := mgr.GetSyncState("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent site")
		}
	})

	t.Run("GetAllSyncStates", func(t *testing.T) {
		states := mgr.GetAllSyncStates()
		if len(states) != 1 {
			t.Errorf("expected 1 sync state, got %d", len(states))
		}
	})

	t.Run("RemoveSite_LocalSite", func(t *testing.T) {
		err := mgr.RemoveSite("local")
		if err == nil {
			t.Error("expected error when removing local site")
		}
	})

	t.Run("RemoveSite_NotFound", func(t *testing.T) {
		err := mgr.RemoveSite("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent site")
		}
	})

	t.Run("TriggerSiteSync_LocalSite", func(t *testing.T) {
		err := mgr.TriggerSiteSync(context.Background(), "local")
		if err == nil {
			t.Error("expected error when syncing local site")
		}
	})

	t.Run("TriggerSiteSync_NotFound", func(t *testing.T) {
		err := mgr.TriggerSiteSync(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent site")
		}
	})
}

func TestEvents(t *testing.T) {
	cfg := Config{
		Sites: []Site{
			{
				Name:      "local",
				Endpoint:  "localhost:9000",
				AccessKey: "key",
				SecretKey: "secret",
				IsLocal:   true,
			},
		},
		HealthCheckInterval: time.Minute,
		SyncInterval:        5 * time.Minute,
	}

	mgr, err := NewManager(cfg, DefaultManagerConfig())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	t.Run("EventChannel", func(t *testing.T) {
		events := mgr.Events()
		if events == nil {
			t.Error("events channel should not be nil")
		}
	})
}

func TestDefaultManagerConfig(t *testing.T) {
	cfg := DefaultManagerConfig()

	if cfg.SyncWorkers <= 0 {
		t.Error("sync workers should be positive")
	}

	if cfg.EventBufferSize <= 0 {
		t.Error("event buffer size should be positive")
	}
}
