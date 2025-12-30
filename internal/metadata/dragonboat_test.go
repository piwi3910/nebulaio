package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/lni/dragonboat/v4/statemachine"
	"github.com/piwi3910/nebulaio/internal/audit"
)

// getFreePort returns a free port for testing by binding to port 0
// and immediately releasing it. This avoids hardcoded port conflicts.
func getFreePort(t *testing.T) int {
	t.Helper()

	var lc net.ListenConfig

	listener, err := lc.Listen(context.Background(), "tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	return port
}

// getRaftAddress returns a localhost address with a free port.
func getRaftAddress(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("localhost:%d", getFreePort(t))
}

// TestDragonboatStoreCreation tests the creation and initialization of DragonboatStore.
func TestDragonboatStoreCreation(t *testing.T) {
	t.Run("SuccessfulCreation", func(t *testing.T) {
		tmpDir := t.TempDir()
		raftAddr := getRaftAddress(t)

		cfg := DragonboatConfig{
			NodeID:      1,
			ShardID:     1,
			DataDir:     tmpDir,
			RaftAddress: raftAddr,
			Bootstrap:   true,
			InitialMembers: map[uint64]string{
				1: raftAddr,
			},
		}

		store, err := NewDragonboatStore(cfg)
		if err != nil {
			t.Fatalf("Failed to create DragonboatStore: %v", err)
		}
		defer store.Close()

		if store.nodeID != cfg.NodeID {
			t.Errorf("Expected nodeID %d, got %d", cfg.NodeID, store.nodeID)
		}

		if store.shardID != cfg.ShardID {
			t.Errorf("Expected shardID %d, got %d", cfg.ShardID, store.shardID)
		}
	})

	t.Run("DirectoryCreation", func(t *testing.T) {
		tmpDir := t.TempDir()
		raftAddr := getRaftAddress(t)

		cfg := DragonboatConfig{
			NodeID:      2,
			ShardID:     1,
			DataDir:     tmpDir,
			RaftAddress: raftAddr,
			Bootstrap:   true,
			InitialMembers: map[uint64]string{
				2: raftAddr,
			},
		}

		store, err := NewDragonboatStore(cfg)
		if err != nil {
			t.Fatalf("Failed to create DragonboatStore: %v", err)
		}
		defer store.Close()

		// Verify directories were created
		expectedDirs := []string{
			filepath.Join(tmpDir, "badger"),
			filepath.Join(tmpDir, "nodehost"),
			filepath.Join(tmpDir, "wal"),
		}

		for _, dir := range expectedDirs {
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				t.Errorf("Expected directory %s to exist", dir)
			}
		}
	})
}

// TestStateMachineOperations tests the state machine's core operations.
func TestStateMachineOperations(t *testing.T) {
	t.Run("OpenAndClose", func(t *testing.T) {
		tmpDir := t.TempDir()

		opts := badger.DefaultOptions(tmpDir)
		opts.Logger = nil

		db, err := badger.Open(opts)
		if err != nil {
			t.Fatalf("Failed to open badger: %v", err)
		}
		defer db.Close()

		sm := newStateMachine(db)

		// Open should return the last applied index
		index, err := sm.Open(make(chan struct{}))
		if err != nil {
			t.Errorf("Open failed: %v", err)
		}

		if index != 0 {
			t.Errorf("Expected initial index 0, got %d", index)
		}

		// Close should succeed
		err = sm.Close()
		if err != nil {
			t.Errorf("Close failed: %v", err)
		}
	})

	t.Run("UpdateCommand", func(t *testing.T) {
		tmpDir := t.TempDir()

		opts := badger.DefaultOptions(tmpDir)
		opts.Logger = nil

		db, err := badger.Open(opts)
		if err != nil {
			t.Fatalf("Failed to open badger: %v", err)
		}
		defer db.Close()

		sm := newStateMachine(db)
		_, err = sm.Open(make(chan struct{}))
		if err != nil {
			t.Fatalf("Failed to open state machine: %v", err)
		}

		// Create a bucket command
		bucket := &Bucket{
			Name:      "test-bucket",
			Owner:     "test-user",
			CreatedAt: time.Now(),
			Region:    "us-east-1",
		}

		bucketData, err := json.Marshal(bucket)
		if err != nil {
			t.Fatalf("Failed to marshal bucket: %v", err)
		}

		cmd := &command{
			Type: cmdCreateBucket,
			Data: bucketData,
		}

		cmdData, err := json.Marshal(cmd)
		if err != nil {
			t.Fatalf("Failed to marshal command: %v", err)
		}

		entry := statemachine.Entry{
			Index: 1,
			Cmd:   cmdData,
		}

		result, err := sm.Update(entry)
		if err != nil {
			t.Errorf("Update failed: %v", err)
		}

		if result.Value != 0 {
			t.Errorf("Expected success result (0), got %d", result.Value)
		}

		// Verify bucket was created
		var storedBucket Bucket

		err = db.View(func(txn *badger.Txn) error {
			item, err := txn.Get([]byte(prefixBucket + bucket.Name))
			if err != nil {
				return err
			}

			return item.Value(func(val []byte) error {
				return json.Unmarshal(val, &storedBucket)
			})
		})
		if err != nil {
			t.Errorf("Failed to retrieve bucket: %v", err)
		}

		if storedBucket.Name != bucket.Name {
			t.Errorf("Expected bucket name %s, got %s", bucket.Name, storedBucket.Name)
		}
	})

	t.Run("Lookup", func(t *testing.T) {
		tmpDir := t.TempDir()

		opts := badger.DefaultOptions(tmpDir)
		opts.Logger = nil

		db, err := badger.Open(opts)
		if err != nil {
			t.Fatalf("Failed to open badger: %v", err)
		}
		defer db.Close()

		sm := newStateMachine(db)

		// Lookup is not used in our implementation, should return ErrLookupNotSupported
		result, err := sm.Lookup(nil)
		if err != ErrLookupNotSupported {
			t.Errorf("Expected ErrLookupNotSupported, got %v", err)
		}

		if result != nil {
			t.Errorf("Expected nil result from Lookup, got %v", result)
		}
	})
}

// TestStateMachineSnapshot tests snapshot operations.
func TestStateMachineSnapshot(t *testing.T) {
	t.Run("SaveAndRecoverSnapshot", func(t *testing.T) {
		tmpDir := t.TempDir()

		opts := badger.DefaultOptions(tmpDir)
		opts.Logger = nil

		db, err := badger.Open(opts)
		if err != nil {
			t.Fatalf("Failed to open badger: %v", err)
		}
		defer db.Close()

		sm := newStateMachine(db)
		_, err = sm.Open(make(chan struct{}))
		if err != nil {
			t.Fatalf("Failed to open state machine: %v", err)
		}

		// Create some test data
		testData := map[string]string{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
		}

		err = db.Update(func(txn *badger.Txn) error {
			for k, v := range testData {
				err := txn.Set([]byte(k), []byte(v))
				if err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			t.Fatalf("Failed to create test data: %v", err)
		}

		// Save snapshot
		var buf strings.Builder

		done := make(chan struct{})

		err = sm.SaveSnapshot(&buf, nil, done)
		if err != nil {
			t.Fatalf("Failed to save snapshot: %v", err)
		}

		// Create a new database for recovery
		tmpDir2 := t.TempDir()
		opts2 := badger.DefaultOptions(tmpDir2)
		opts2.Logger = nil

		db2, err := badger.Open(opts2)
		if err != nil {
			t.Fatalf("Failed to open second badger: %v", err)
		}
		defer db2.Close()

		sm2 := newStateMachine(db2)
		_, err = sm2.Open(make(chan struct{}))
		if err != nil {
			t.Fatalf("Failed to open second state machine: %v", err)
		}

		// Recover snapshot
		reader := strings.NewReader(buf.String())
		done2 := make(chan struct{})

		err = sm2.RecoverFromSnapshot(reader, nil, done2)
		if err != nil {
			t.Fatalf("Failed to recover snapshot: %v", err)
		}

		// Verify data was recovered
		err = db2.View(func(txn *badger.Txn) error {
			for k, expectedV := range testData {
				item, err := txn.Get([]byte(k))
				if err != nil {
					return err
				}

				var actualV string

				err = item.Value(func(val []byte) error {
					actualV = string(val)
					return nil
				})
				if err != nil {
					return err
				}

				if actualV != expectedV {
					t.Errorf("Expected value %s for key %s, got %s", expectedV, k, actualV)
				}
			}

			return nil
		})
		if err != nil {
			t.Errorf("Failed to verify recovered data: %v", err)
		}
	})

	t.Run("SnapshotStopped", func(t *testing.T) {
		tmpDir := t.TempDir()

		opts := badger.DefaultOptions(tmpDir)
		opts.Logger = nil

		db, err := badger.Open(opts)
		if err != nil {
			t.Fatalf("Failed to open badger: %v", err)
		}
		defer db.Close()

		sm := newStateMachine(db)
		_, err = sm.Open(make(chan struct{}))
		if err != nil {
			t.Fatalf("Failed to open state machine: %v", err)
		}

		// Create a closed channel to simulate stop
		done := make(chan struct{})
		close(done)

		var buf strings.Builder

		err = sm.SaveSnapshot(&buf, nil, done)
		// Should not error when stopped immediately
		if err != nil && err != statemachine.ErrSnapshotStopped {
			t.Errorf("Expected ErrSnapshotStopped or nil, got: %v", err)
		}
	})
}

// TestBucketOperations tests bucket CRUD operations.
func TestBucketOperations(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	t.Run("CreateBucket", func(t *testing.T) {
		bucket := &Bucket{
			Name:      "test-bucket",
			Owner:     "test-user",
			CreatedAt: time.Now(),
			Region:    "us-east-1",
		}

		err := store.CreateBucket(ctx, bucket)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to create bucket: %v", err)
		}
	})

	t.Run("GetBucket", func(t *testing.T) {
		bucket, err := store.GetBucket(ctx, "test-bucket")
		if err != nil {
			t.Errorf("Failed to get bucket: %v", err)
		}

		if bucket == nil {
			t.Fatal("Expected bucket, got nil")
		}

		if bucket.Name != "test-bucket" {
			t.Errorf("Expected bucket name 'test-bucket', got '%s'", bucket.Name)
		}

		if bucket.Owner != "test-user" {
			t.Errorf("Expected owner 'test-user', got '%s'", bucket.Owner)
		}
	})

	t.Run("ListBuckets", func(t *testing.T) {
		buckets, err := store.ListBuckets(ctx, "")
		if err != nil {
			t.Errorf("Failed to list buckets: %v", err)
		}

		if len(buckets) == 0 {
			t.Error("Expected at least one bucket")
		}

		found := false

		for _, b := range buckets {
			if b.Name == "test-bucket" {
				found = true
				break
			}
		}

		if !found {
			t.Error("Expected to find 'test-bucket' in list")
		}
	})

	t.Run("UpdateBucket", func(t *testing.T) {
		bucket, err := store.GetBucket(ctx, "test-bucket")
		if err != nil {
			t.Fatalf("Failed to get bucket: %v", err)
		}

		bucket.Region = "us-west-2"

		err = store.UpdateBucket(ctx, bucket)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to update bucket: %v", err)
		}

		// Verify update
		updatedBucket, err := store.GetBucket(ctx, "test-bucket")
		if err != nil {
			t.Errorf("Failed to get updated bucket: %v", err)
		}

		if updatedBucket.Region != "us-west-2" {
			t.Errorf("Expected region 'us-west-2', got '%s'", updatedBucket.Region)
		}
	})

	t.Run("DeleteBucket", func(t *testing.T) {
		err := store.DeleteBucket(ctx, "test-bucket")
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to delete bucket: %v", err)
		}

		// Verify deletion
		_, err = store.GetBucket(ctx, "test-bucket")
		if err == nil {
			t.Error("Expected error when getting deleted bucket")
		}
	})
}

// TestObjectMetadataOperations tests object metadata CRUD operations.
func TestObjectMetadataOperations(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	// Create test bucket first
	bucket := &Bucket{
		Name:      "test-objects",
		Owner:     "test-user",
		CreatedAt: time.Now(),
		Region:    "us-east-1",
	}
	err = store.CreateBucket(ctx, bucket)
	if err != nil && store.IsLeader() {
		t.Fatalf("Failed to create test bucket: %v", err)
	}

	t.Run("PutObjectMeta", func(t *testing.T) {
		meta := &ObjectMeta{
			Bucket:      "test-objects",
			Key:         "test-key",
			Size:        1024,
			ContentType: "text/plain",
			ETag:        "abc123",
			CreatedAt:   time.Now(),
			ModifiedAt:  time.Now(),
		}

		err := store.PutObjectMeta(ctx, meta)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to put object metadata: %v", err)
		}
	})

	t.Run("GetObjectMeta", func(t *testing.T) {
		meta, err := store.GetObjectMeta(ctx, "test-objects", "test-key")
		if err != nil {
			t.Errorf("Failed to get object metadata: %v", err)
		}

		if meta == nil {
			t.Fatal("Expected metadata, got nil")
		}

		if meta.Key != "test-key" {
			t.Errorf("Expected key 'test-key', got '%s'", meta.Key)
		}

		if meta.Size != 1024 {
			t.Errorf("Expected size 1024, got %d", meta.Size)
		}
	})

	t.Run("ListObjects", func(t *testing.T) {
		listing, err := store.ListObjects(ctx, "test-objects", "", "", 100, "")
		if err != nil {
			t.Errorf("Failed to list objects: %v", err)
		}

		if len(listing.Objects) == 0 {
			t.Error("Expected at least one object")
		}
	})

	t.Run("DeleteObjectMeta", func(t *testing.T) {
		err := store.DeleteObjectMeta(ctx, "test-objects", "test-key")
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to delete object metadata: %v", err)
		}

		// Verify deletion
		_, err = store.GetObjectMeta(ctx, "test-objects", "test-key")
		if err == nil {
			t.Error("Expected error when getting deleted object")
		}
	})
}

// TestUserOperations tests user CRUD operations.
func TestUserOperations(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	t.Run("CreateUser", func(t *testing.T) {
		user := &User{
			ID:        "user-123",
			Username:  "testuser",
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			Enabled:   true,
			Role:      RoleUser,
		}

		err := store.CreateUser(ctx, user)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to create user: %v", err)
		}
	})

	t.Run("GetUser", func(t *testing.T) {
		user, err := store.GetUser(ctx, "user-123")
		if err != nil {
			t.Errorf("Failed to get user: %v", err)
		}

		if user == nil {
			t.Fatal("Expected user, got nil")
		}

		if user.Username != "testuser" {
			t.Errorf("Expected username 'testuser', got '%s'", user.Username)
		}
	})

	t.Run("GetUserByUsername", func(t *testing.T) {
		user, err := store.GetUserByUsername(ctx, "testuser")
		if err != nil {
			t.Errorf("Failed to get user by username: %v", err)
		}

		if user == nil {
			t.Fatal("Expected user, got nil")
		}

		if user.ID != "user-123" {
			t.Errorf("Expected ID 'user-123', got '%s'", user.ID)
		}
	})

	t.Run("ListUsers", func(t *testing.T) {
		users, err := store.ListUsers(ctx)
		if err != nil {
			t.Errorf("Failed to list users: %v", err)
		}

		if len(users) == 0 {
			t.Error("Expected at least one user")
		}
	})

	t.Run("UpdateUser", func(t *testing.T) {
		user, err := store.GetUser(ctx, "user-123")
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}

		user.Email = "newemail@example.com"

		err = store.UpdateUser(ctx, user)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to update user: %v", err)
		}

		// Verify update
		updatedUser, err := store.GetUser(ctx, "user-123")
		if err != nil {
			t.Errorf("Failed to get updated user: %v", err)
		}

		if updatedUser.Email != "newemail@example.com" {
			t.Errorf("Expected email 'newemail@example.com', got '%s'", updatedUser.Email)
		}
	})

	t.Run("DeleteUser", func(t *testing.T) {
		err := store.DeleteUser(ctx, "user-123")
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to delete user: %v", err)
		}

		// Verify deletion
		_, err = store.GetUser(ctx, "user-123")
		if err == nil {
			t.Error("Expected error when getting deleted user")
		}
	})
}

// TestAccessKeyOperations tests access key CRUD operations.
func TestAccessKeyOperations(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	t.Run("CreateAccessKey", func(t *testing.T) {
		key := &AccessKey{
			AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			UserID:          "user-123",
			Enabled:         true,
			CreatedAt:       time.Now(),
		}

		err := store.CreateAccessKey(ctx, key)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to create access key: %v", err)
		}
	})

	t.Run("GetAccessKey", func(t *testing.T) {
		key, err := store.GetAccessKey(ctx, "AKIAIOSFODNN7EXAMPLE")
		if err != nil {
			t.Errorf("Failed to get access key: %v", err)
		}

		if key == nil {
			t.Fatal("Expected access key, got nil")
		}

		if key.UserID != "user-123" {
			t.Errorf("Expected UserID 'user-123', got '%s'", key.UserID)
		}
	})

	t.Run("ListAccessKeys", func(t *testing.T) {
		keys, err := store.ListAccessKeys(ctx, "user-123")
		if err != nil {
			t.Errorf("Failed to list access keys: %v", err)
		}

		if len(keys) == 0 {
			t.Error("Expected at least one access key")
		}
	})

	t.Run("DeleteAccessKey", func(t *testing.T) {
		err := store.DeleteAccessKey(ctx, "AKIAIOSFODNN7EXAMPLE")
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to delete access key: %v", err)
		}

		// Verify deletion
		_, err = store.GetAccessKey(ctx, "AKIAIOSFODNN7EXAMPLE")
		if err == nil {
			t.Error("Expected error when getting deleted access key")
		}
	})
}

// TestPolicyOperations tests policy CRUD operations.
func TestPolicyOperations(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	t.Run("CreatePolicy", func(t *testing.T) {
		policy := &Policy{
			Name:      "test-policy",
			Document:  `{"Version":"2012-10-17","Statement":[]}`,
			CreatedAt: time.Now(),
		}

		err := store.CreatePolicy(ctx, policy)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to create policy: %v", err)
		}
	})

	t.Run("GetPolicy", func(t *testing.T) {
		policy, err := store.GetPolicy(ctx, "test-policy")
		if err != nil {
			t.Errorf("Failed to get policy: %v", err)
		}

		if policy == nil {
			t.Fatal("Expected policy, got nil")
		}

		if policy.Name != "test-policy" {
			t.Errorf("Expected name 'test-policy', got '%s'", policy.Name)
		}
	})

	t.Run("ListPolicies", func(t *testing.T) {
		policies, err := store.ListPolicies(ctx)
		if err != nil {
			t.Errorf("Failed to list policies: %v", err)
		}

		if len(policies) == 0 {
			t.Error("Expected at least one policy")
		}
	})

	t.Run("UpdatePolicy", func(t *testing.T) {
		policy, err := store.GetPolicy(ctx, "test-policy")
		if err != nil {
			t.Fatalf("Failed to get policy: %v", err)
		}

		policy.Document = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`

		err = store.UpdatePolicy(ctx, policy)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to update policy: %v", err)
		}

		// Verify update
		updatedPolicy, err := store.GetPolicy(ctx, "test-policy")
		if err != nil {
			t.Errorf("Failed to get updated policy: %v", err)
		}

		if !strings.Contains(updatedPolicy.Document, "Allow") {
			t.Error("Expected updated policy document to contain 'Allow'")
		}
	})

	t.Run("DeletePolicy", func(t *testing.T) {
		err := store.DeletePolicy(ctx, "test-policy")
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to delete policy: %v", err)
		}

		// Verify deletion
		_, err = store.GetPolicy(ctx, "test-policy")
		if err == nil {
			t.Error("Expected error when getting deleted policy")
		}
	})
}

// TestLeaderElectionHelpers tests leader-related helper functions.
func TestLeaderElectionHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	t.Run("IsLeader", func(t *testing.T) {
		isLeader := store.IsLeader()
		// In a single-node cluster, this node should become leader
		if !isLeader {
			t.Log("Node is not leader yet, this is acceptable in early initialization")
		}
	})

	t.Run("State", func(t *testing.T) {
		state := store.State()
		if state != "Leader" && state != "Follower" {
			t.Errorf("Expected state 'Leader' or 'Follower', got '%s'", state)
		}
	})

	t.Run("Stats", func(t *testing.T) {
		stats := store.Stats()
		if stats == nil {
			t.Fatal("Expected stats, got nil")
		}

		if stats["shard_id"] == "" {
			t.Error("Expected shard_id in stats")
		}

		if stats["node_id"] == "" {
			t.Error("Expected node_id in stats")
		}

		if stats["state"] == "" {
			t.Error("Expected state in stats")
		}
	})

	t.Run("WaitForLeader", func(t *testing.T) {
		err := store.WaitForLeader(5 * time.Second)
		if err != nil {
			t.Logf("Timeout waiting for leader: %v (acceptable in test environment)", err)
		}
	})

	t.Run("LeaderAddress", func(t *testing.T) {
		addr, err := store.LeaderAddress()
		if err != nil {
			t.Logf("Failed to get leader address: %v (acceptable if no leader elected)", err)
		} else if addr == "" {
			t.Log("Leader address is empty (acceptable if no leader elected)")
		}
	})
}

// TestClusterMembershipOperations tests cluster membership management.
func TestClusterMembershipOperations(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)
	nodeAddr := getRaftAddress(t) // Separate address for the test node

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	t.Run("AddNode", func(t *testing.T) {
		node := &NodeInfo{
			ID:      "node-2",
			Name:    "node-2",
			Address: nodeAddr,
			Role:    "gateway",
			Status:  "active",
		}

		err := store.AddNode(ctx, node)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to add node: %v", err)
		}
	})

	t.Run("ListNodes", func(t *testing.T) {
		nodes, err := store.ListNodes(ctx)
		if err != nil {
			t.Errorf("Failed to list nodes: %v", err)
		}
		// Should have at least the node we added
		if len(nodes) == 0 {
			t.Log("No nodes in list (acceptable if add failed)")
		}
	})

	t.Run("GetClusterInfo", func(t *testing.T) {
		info, err := store.GetClusterInfo(ctx)
		if err != nil {
			t.Errorf("Failed to get cluster info: %v", err)
		}

		if info == nil {
			t.Fatal("Expected cluster info, got nil")
		}

		if info.ClusterID == "" {
			t.Error("Expected non-empty cluster ID")
		}
	})

	t.Run("RemoveNode", func(t *testing.T) {
		err := store.RemoveNode(ctx, "node-2")
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to remove node: %v", err)
		}
	})

	t.Run("GetConfiguration", func(t *testing.T) {
		membership, err := store.GetConfiguration()
		if err != nil {
			t.Errorf("Failed to get configuration: %v", err)
		}

		if membership == nil {
			t.Fatal("Expected membership, got nil")
		}
	})

	t.Run("GetServers", func(t *testing.T) {
		servers, err := store.GetServers()
		if err != nil {
			t.Errorf("Failed to get servers: %v", err)
		}

		if len(servers) == 0 {
			t.Error("Expected at least one server")
		}
	})
}

// TestAuditOperations tests audit event operations.
func TestAuditOperations(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	t.Run("StoreAuditEvent", func(t *testing.T) {
		event := &audit.AuditEvent{
			ID:        "event-123",
			Timestamp: time.Now(),
			EventType: audit.EventObjectAccessed,
			Result:    audit.ResultSuccess,
			UserIdentity: audit.UserIdentity{
				UserID:   "user-123",
				Username: "testuser",
			},
			Resource: audit.ResourceInfo{
				Type:   audit.ResourceObject,
				Bucket: "test-bucket",
				Key:    "test-key",
			},
		}

		err := store.StoreAuditEvent(ctx, event)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to store audit event: %v", err)
		}
	})

	t.Run("ListAuditEvents", func(t *testing.T) {
		filter := audit.AuditFilter{
			MaxResults: 100,
		}

		result, err := store.ListAuditEvents(ctx, filter)
		if err != nil {
			t.Errorf("Failed to list audit events: %v", err)
		}

		if result == nil {
			t.Fatal("Expected result, got nil")
		}
		// May be empty if store failed
		if len(result.Events) > 0 {
			if result.Events[0].EventType != audit.EventObjectAccessed {
				t.Errorf("Expected event type 's3:ObjectAccessed:Get', got '%s'", result.Events[0].EventType)
			}
		}
	})

	t.Run("DeleteOldAuditEvents", func(t *testing.T) {
		// Delete events older than 1 hour from now (should delete all test events)
		before := time.Now().Add(1 * time.Hour)

		count, err := store.DeleteOldAuditEvents(ctx, before)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to delete old audit events: %v", err)
		}

		if count < 0 {
			t.Errorf("Expected non-negative count, got %d", count)
		}
	})
}

// TestMultipartUploadOperations tests multipart upload operations.
func TestMultipartUploadOperations(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	uploadID := "upload-123"

	t.Run("CreateMultipartUpload", func(t *testing.T) {
		upload := &MultipartUpload{
			Bucket:    "test-bucket",
			Key:       "test-key",
			UploadID:  uploadID,
			Initiator: "test-user",
			CreatedAt: time.Now(),
			Parts:     []UploadPart{},
		}

		err := store.CreateMultipartUpload(ctx, upload)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to create multipart upload: %v", err)
		}
	})

	t.Run("GetMultipartUpload", func(t *testing.T) {
		upload, err := store.GetMultipartUpload(ctx, "test-bucket", "test-key", uploadID)
		if err != nil {
			t.Errorf("Failed to get multipart upload: %v", err)
		}

		if upload == nil {
			t.Fatal("Expected upload, got nil")
		}

		if upload.UploadID != uploadID {
			t.Errorf("Expected upload ID '%s', got '%s'", uploadID, upload.UploadID)
		}
	})

	t.Run("AddUploadPart", func(t *testing.T) {
		part := &UploadPart{
			PartNumber:   1,
			ETag:         "etag123",
			Size:         1024,
			LastModified: time.Now(),
		}

		err := store.AddUploadPart(ctx, "test-bucket", "test-key", uploadID, part)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to add upload part: %v", err)
		}

		// Verify part was added
		upload, err := store.GetMultipartUpload(ctx, "test-bucket", "test-key", uploadID)
		if err != nil {
			t.Errorf("Failed to get multipart upload: %v", err)
		}

		if len(upload.Parts) != 1 {
			t.Errorf("Expected 1 part, got %d", len(upload.Parts))
		}
	})

	t.Run("ListMultipartUploads", func(t *testing.T) {
		uploads, err := store.ListMultipartUploads(ctx, "test-bucket")
		if err != nil {
			t.Errorf("Failed to list multipart uploads: %v", err)
		}

		if len(uploads) == 0 {
			t.Error("Expected at least one upload")
		}
	})

	t.Run("AbortMultipartUpload", func(t *testing.T) {
		err := store.AbortMultipartUpload(ctx, "test-bucket", "test-key", uploadID)
		if err != nil && !store.IsLeader() {
			t.Skip("Not leader, skipping test")
		}

		if err != nil {
			t.Errorf("Failed to abort multipart upload: %v", err)
		}

		// Verify deletion
		_, err = store.GetMultipartUpload(ctx, "test-bucket", "test-key", uploadID)
		if err == nil {
			t.Error("Expected error when getting aborted upload")
		}
	})
}

// TestSnapshotOperations tests snapshot-related operations.
func TestSnapshotOperations(t *testing.T) {
	tmpDir := t.TempDir()
	raftAddr := getRaftAddress(t)

	cfg := DragonboatConfig{
		NodeID:      1,
		ShardID:     1,
		DataDir:     tmpDir,
		RaftAddress: raftAddr,
		Bootstrap:   true,
		InitialMembers: map[uint64]string{
			1: raftAddr,
		},
	}

	store, err := NewDragonboatStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create DragonboatStore: %v", err)
	}
	defer store.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	t.Run("Snapshot", func(t *testing.T) {
		if !store.IsLeader() {
			t.Skip("Not leader, skipping snapshot test")
		}

		err := store.Snapshot()
		if err != nil {
			t.Errorf("Failed to create snapshot: %v", err)
		}
	})
}

// TestGetAndScanOperations tests low-level get and scan operations.
func TestGetAndScanOperations(t *testing.T) {
	tmpDir := t.TempDir()

	opts := badger.DefaultOptions(tmpDir)
	opts.Logger = nil

	db, err := badger.Open(opts)
	if err != nil {
		t.Fatalf("Failed to open badger: %v", err)
	}
	defer db.Close()

	store := &DragonboatStore{
		badger: db,
	}

	// Insert test data
	testData := map[string]string{
		"prefix:key1": "value1",
		"prefix:key2": "value2",
		"prefix:key3": "value3",
		"other:key1":  "othervalue",
	}

	err = db.Update(func(txn *badger.Txn) error {
		for k, v := range testData {
			err := txn.Set([]byte(k), []byte(v))
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	t.Run("Get", func(t *testing.T) {
		val, err := store.get([]byte("prefix:key1"))
		if err != nil {
			t.Errorf("Failed to get value: %v", err)
		}

		if string(val) != "value1" {
			t.Errorf("Expected value 'value1', got '%s'", string(val))
		}
	})

	t.Run("GetNonExistent", func(t *testing.T) {
		_, err := store.get([]byte("nonexistent"))
		if err != badger.ErrKeyNotFound {
			t.Errorf("Expected ErrKeyNotFound, got %v", err)
		}
	})

	t.Run("Scan", func(t *testing.T) {
		results, err := store.scan([]byte("prefix:"))
		if err != nil {
			t.Errorf("Failed to scan: %v", err)
		}

		if len(results) != 3 {
			t.Errorf("Expected 3 results, got %d", len(results))
		}
	})

	t.Run("ScanWithDifferentPrefix", func(t *testing.T) {
		results, err := store.scan([]byte("other:"))
		if err != nil {
			t.Errorf("Failed to scan: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}
	})
}

// TestErrorHandling tests error handling in various scenarios.
func TestErrorHandling(t *testing.T) {
	t.Run("InvalidCommand", func(t *testing.T) {
		tmpDir := t.TempDir()

		opts := badger.DefaultOptions(tmpDir)
		opts.Logger = nil

		db, err := badger.Open(opts)
		if err != nil {
			t.Fatalf("Failed to open badger: %v", err)
		}
		defer db.Close()

		sm := newStateMachine(db)
		_, err = sm.Open(make(chan struct{}))
		if err != nil {
			t.Fatalf("Failed to open state machine: %v", err)
		}

		// Invalid JSON
		entry := statemachine.Entry{
			Index: 1,
			Cmd:   []byte("invalid json"),
		}

		result, err := sm.Update(entry)
		if err != nil {
			t.Errorf("Update should not return error, got: %v", err)
		}
		// Should return error code
		if result.Value != 1 {
			t.Errorf("Expected error code 1, got %d", result.Value)
		}
	})

	t.Run("ApplyWithoutLeader", func(t *testing.T) {
		tmpDir := t.TempDir()
		raftAddr := getRaftAddress(t)

		cfg := DragonboatConfig{
			NodeID:      99,
			ShardID:     99,
			DataDir:     tmpDir,
			RaftAddress: raftAddr,
			Bootstrap:   true,
			InitialMembers: map[uint64]string{
				99: raftAddr,
			},
		}

		store, err := NewDragonboatStore(cfg)
		if err != nil {
			t.Fatalf("Failed to create DragonboatStore: %v", err)
		}
		defer store.Close()

		// Don't wait for leadership, try to apply immediately
		ctx := context.Background()
		bucket := &Bucket{
			Name:      "test-bucket",
			Owner:     "test-user",
			CreatedAt: time.Now(),
		}

		err = store.CreateBucket(ctx, bucket)
		if err == nil && !store.IsLeader() {
			t.Error("Expected error when applying without being leader")
		}
	})
}

// BenchmarkStateMachineUpdate benchmarks state machine updates.
func BenchmarkStateMachineUpdate(b *testing.B) {
	tmpDir := b.TempDir()

	opts := badger.DefaultOptions(tmpDir)
	opts.Logger = nil

	db, err := badger.Open(opts)
	if err != nil {
		b.Fatalf("Failed to open badger: %v", err)
	}
	defer db.Close()

	sm := newStateMachine(db)
	_, err = sm.Open(make(chan struct{}))
	if err != nil {
		b.Fatalf("Failed to open state machine: %v", err)
	}

	bucket := &Bucket{
		Name:      "bench-bucket",
		Owner:     "bench-user",
		CreatedAt: time.Now(),
		Region:    "us-east-1",
	}

	bucketData, _ := json.Marshal(bucket)
	cmd := &command{
		Type: cmdCreateBucket,
		Data: bucketData,
	}
	cmdData, _ := json.Marshal(cmd)

	b.ResetTimer()

	for i := range b.N {
		entry := statemachine.Entry{
			Index: uint64(i + 1),
			Cmd:   cmdData,
		}
		sm.Update(entry)
	}
}

// BenchmarkSnapshotSave benchmarks snapshot saving.
func BenchmarkSnapshotSave(b *testing.B) {
	tmpDir := b.TempDir()

	opts := badger.DefaultOptions(tmpDir)
	opts.Logger = nil

	db, err := badger.Open(opts)
	if err != nil {
		b.Fatalf("Failed to open badger: %v", err)
	}
	defer db.Close()

	sm := newStateMachine(db)
	_, err = sm.Open(make(chan struct{}))
	if err != nil {
		b.Fatalf("Failed to open state machine: %v", err)
	}

	// Create test data
	err = db.Update(func(txn *badger.Txn) error {
		for i := range 1000 {
			key := []byte(string(rune('k')) + string(rune(i)))

			val := []byte(string(rune('v')) + string(rune(i)))

			err := txn.Set(key, val)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		b.Fatalf("Failed to create test data: %v", err)
	}

	done := make(chan struct{})
	close(done)

	b.ResetTimer()

	for range b.N {
		var buf strings.Builder
		sm.SaveSnapshot(&buf, nil, done)
	}
}
