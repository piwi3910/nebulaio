package metadata

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
	"github.com/piwi3910/nebulaio/internal/audit"
	"github.com/rs/zerolog/log"
)

// RaftConfig holds configuration for the Raft store
type RaftConfig struct {
	NodeID    string
	DataDir   string
	Bootstrap bool
	RaftBind  string
}

// RaftStore implements the Store interface using Raft for consensus
// and BadgerDB for the underlying key-value storage
type RaftStore struct {
	config RaftConfig

	raft   *raft.Raft
	fsm    *fsm
	badger *badger.DB

	_mu sync.RWMutex // Reserved for future thread-safe operations
}

// NewRaftStore creates a new Raft-backed metadata store
func NewRaftStore(config RaftConfig) (*RaftStore, error) {
	// Create directories
	raftDir := filepath.Join(config.DataDir, "raft")
	badgerDir := filepath.Join(config.DataDir, "badger")

	if err := os.MkdirAll(raftDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create raft directory: %w", err)
	}
	if err := os.MkdirAll(badgerDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create badger directory: %w", err)
	}

	// Open BadgerDB
	opts := badger.DefaultOptions(badgerDir)
	opts.Logger = nil // Disable badger logging
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger: %w", err)
	}

	// Create FSM
	f := &fsm{db: db}

	// Configure Raft
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(config.NodeID)
	raftConfig.SnapshotThreshold = 1024
	raftConfig.SnapshotInterval = 30 * time.Second

	// Create Raft transport
	addr, err := net.ResolveTCPAddr("tcp", config.RaftBind)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve raft bind address: %w", err)
	}

	transport, err := raft.NewTCPTransport(config.RaftBind, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft transport: %w", err)
	}

	// Create Raft log store and stable store
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(raftDir, "raft-log.db"))
	if err != nil {
		return nil, fmt.Errorf("failed to create raft log store: %w", err)
	}

	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(raftDir, "raft-stable.db"))
	if err != nil {
		return nil, fmt.Errorf("failed to create raft stable store: %w", err)
	}

	// Create snapshot store
	snapshotStore, err := raft.NewFileSnapshotStore(raftDir, 2, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	// Create Raft instance
	r, err := raft.NewRaft(raftConfig, f, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft: %w", err)
	}

	// Bootstrap if needed
	if config.Bootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raft.ServerID(config.NodeID),
					Address: transport.LocalAddr(),
				},
			},
		}
		f := r.BootstrapCluster(configuration)
		if err := f.Error(); err != nil && err != raft.ErrCantBootstrap {
			log.Warn().Err(err).Msg("Failed to bootstrap cluster (may already be bootstrapped)")
		}
	}

	store := &RaftStore{
		config: config,
		raft:   r,
		fsm:    f,
		badger: db,
	}

	// Wait for leader election (with timeout)
	log.Info().Msg("Waiting for Raft leader election...")
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			log.Warn().Msg("Timeout waiting for leader election, continuing anyway")
			goto done
		case <-ticker.C:
			if r.State() == raft.Leader || r.Leader() != "" {
				log.Info().
					Str("leader", string(r.Leader())).
					Str("state", r.State().String()).
					Msg("Raft cluster ready")
				goto done
			}
		}
	}
done:

	return store, nil
}

// Close shuts down the Raft store
func (s *RaftStore) Close() error {
	if err := s.raft.Shutdown().Error(); err != nil {
		return err
	}
	return s.badger.Close()
}

// IsLeader returns true if this node is the Raft leader
func (s *RaftStore) IsLeader() bool {
	return s.raft.State() == raft.Leader
}

// LeaderAddress returns the address of the current leader
func (s *RaftStore) LeaderAddress() string {
	return string(s.raft.Leader())
}

// GetRaft returns the underlying Raft instance
func (s *RaftStore) GetRaft() *raft.Raft {
	return s.raft
}

// State returns the current Raft state
func (s *RaftStore) State() raft.RaftState {
	return s.raft.State()
}

// Stats returns Raft statistics
func (s *RaftStore) Stats() map[string]string {
	return s.raft.Stats()
}

// JoinCluster joins an existing Raft cluster by contacting the leader
// This is called by a non-bootstrap node to join the cluster
func (s *RaftStore) JoinCluster(leaderAddr, nodeID, raftAddr string) error {
	log.Info().
		Str("leader_addr", leaderAddr).
		Str("node_id", nodeID).
		Str("raft_addr", raftAddr).
		Msg("Requesting to join Raft cluster")

	// The actual joining is done via the leader's AddVoter call
	// This method is called by the leader to add a new voter
	return nil
}

// AddVoter adds a new voting member to the cluster
// This must be called on the leader
func (s *RaftStore) AddVoter(nodeID, raftAddr string) error {
	if s.raft.State() != raft.Leader {
		return fmt.Errorf("not the leader, current leader: %s", s.raft.Leader())
	}

	log.Info().
		Str("node_id", nodeID).
		Str("raft_addr", raftAddr).
		Msg("Adding voter to Raft cluster")

	future := s.raft.AddVoter(
		raft.ServerID(nodeID),
		raft.ServerAddress(raftAddr),
		0,
		30*time.Second,
	)

	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to add voter: %w", err)
	}

	log.Info().
		Str("node_id", nodeID).
		Msg("Successfully added voter to Raft cluster")

	return nil
}

// AddNonvoter adds a new non-voting member to the cluster
// Non-voters receive log replication but don't vote in elections
func (s *RaftStore) AddNonvoter(nodeID, raftAddr string) error {
	if s.raft.State() != raft.Leader {
		return fmt.Errorf("not the leader, current leader: %s", s.raft.Leader())
	}

	log.Info().
		Str("node_id", nodeID).
		Str("raft_addr", raftAddr).
		Msg("Adding non-voter to Raft cluster")

	future := s.raft.AddNonvoter(
		raft.ServerID(nodeID),
		raft.ServerAddress(raftAddr),
		0,
		30*time.Second,
	)

	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to add non-voter: %w", err)
	}

	return nil
}

// RemoveServer removes a server from the cluster
func (s *RaftStore) RemoveServer(nodeID string) error {
	if s.raft.State() != raft.Leader {
		return fmt.Errorf("not the leader, current leader: %s", s.raft.Leader())
	}

	log.Info().
		Str("node_id", nodeID).
		Msg("Removing server from Raft cluster")

	future := s.raft.RemoveServer(
		raft.ServerID(nodeID),
		0,
		30*time.Second,
	)

	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to remove server: %w", err)
	}

	log.Info().
		Str("node_id", nodeID).
		Msg("Successfully removed server from Raft cluster")

	return nil
}

// DemoteVoter demotes a voter to a non-voter
func (s *RaftStore) DemoteVoter(nodeID string) error {
	if s.raft.State() != raft.Leader {
		return fmt.Errorf("not the leader, current leader: %s", s.raft.Leader())
	}

	// Get current configuration to find the address
	configFuture := s.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return fmt.Errorf("failed to get configuration: %w", err)
	}

	var raftAddr raft.ServerAddress
	for _, server := range configFuture.Configuration().Servers {
		if server.ID == raft.ServerID(nodeID) {
			raftAddr = server.Address
			break
		}
	}

	if raftAddr == "" {
		return fmt.Errorf("node not found in configuration: %s", nodeID)
	}

	log.Info().
		Str("node_id", nodeID).
		Msg("Demoting voter to non-voter")

	future := s.raft.DemoteVoter(
		raft.ServerID(nodeID),
		0,
		30*time.Second,
	)

	return future.Error()
}

// TransferLeadership transfers leadership to another server
func (s *RaftStore) TransferLeadership(targetID, targetAddr string) error {
	if s.raft.State() != raft.Leader {
		return fmt.Errorf("not the leader")
	}

	log.Info().
		Str("target_id", targetID).
		Str("target_addr", targetAddr).
		Msg("Transferring leadership")

	future := s.raft.LeadershipTransferToServer(
		raft.ServerID(targetID),
		raft.ServerAddress(targetAddr),
	)

	return future.Error()
}

// GetConfiguration returns the current Raft configuration
func (s *RaftStore) GetConfiguration() (raft.Configuration, error) {
	future := s.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		return raft.Configuration{}, err
	}
	return future.Configuration(), nil
}

// GetServers returns the list of servers in the cluster
func (s *RaftStore) GetServers() ([]raft.Server, error) {
	config, err := s.GetConfiguration()
	if err != nil {
		return nil, err
	}
	return config.Servers, nil
}

// WaitForLeader waits for a leader to be elected
func (s *RaftStore) WaitForLeader(timeout time.Duration) error {
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCh:
			return fmt.Errorf("timeout waiting for leader")
		case <-ticker.C:
			if s.raft.Leader() != "" {
				return nil
			}
		}
	}
}

// LeaderCh returns a channel that signals leadership changes
func (s *RaftStore) LeaderCh() <-chan bool {
	return s.raft.LeaderCh()
}

// ForwardToLeader is a helper that returns the leader address for forwarding
// Returns empty string if this node is the leader
func (s *RaftStore) ForwardToLeader() string {
	if s.IsLeader() {
		return ""
	}
	return string(s.raft.Leader())
}

// Barrier is used to wait for all pending operations to complete
func (s *RaftStore) Barrier(timeout time.Duration) error {
	future := s.raft.Barrier(timeout)
	return future.Error()
}

// Snapshot triggers a manual snapshot
func (s *RaftStore) Snapshot() error {
	future := s.raft.Snapshot()
	return future.Error()
}

// LastIndex returns the last index in the log
func (s *RaftStore) LastIndex() uint64 {
	return s.raft.LastIndex()
}

// AppliedIndex returns the last applied index
func (s *RaftStore) AppliedIndex() uint64 {
	return s.raft.AppliedIndex()
}

// apply sends a command through Raft
func (s *RaftStore) apply(cmd *command) error {
	if !s.IsLeader() {
		return fmt.Errorf("not leader")
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	future := s.raft.Apply(data, 10*time.Second)
	if err := future.Error(); err != nil {
		return err
	}

	if resp := future.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}

	return nil
}

// get retrieves a value from BadgerDB
func (s *RaftStore) get(key []byte) ([]byte, error) {
	var val []byte
	err := s.badger.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		val, err = item.ValueCopy(nil)
		return err
	})
	return val, err
}

// scan performs a prefix scan on BadgerDB
func (s *RaftStore) scan(prefix []byte) ([][]byte, error) {
	var results [][]byte
	err := s.badger.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			results = append(results, val)
		}
		return nil
	})
	return results, err
}

// Command types
type commandType string

const (
	cmdCreateBucket            commandType = "create_bucket"
	cmdDeleteBucket            commandType = "delete_bucket"
	cmdUpdateBucket            commandType = "update_bucket"
	cmdPutObjectMeta           commandType = "put_object_meta"
	cmdDeleteObjectMeta        commandType = "delete_object_meta"
	cmdPutObjectMetaVersioned  commandType = "put_object_meta_versioned"
	cmdDeleteObjectVersion     commandType = "delete_object_version"
	cmdCreateMultipartUpload   commandType = "create_multipart_upload"
	cmdAbortMultipartUpload    commandType = "abort_multipart_upload"
	cmdCompleteMultipartUpload commandType = "complete_multipart_upload"
	cmdAddUploadPart           commandType = "add_upload_part"
	cmdCreateUser              commandType = "create_user"
	cmdUpdateUser              commandType = "update_user"
	cmdDeleteUser              commandType = "delete_user"
	cmdCreateAccessKey         commandType = "create_access_key"
	cmdDeleteAccessKey         commandType = "delete_access_key"
	cmdCreatePolicy            commandType = "create_policy"
	cmdUpdatePolicy            commandType = "update_policy"
	cmdDeletePolicy            commandType = "delete_policy"
	cmdAddNode                 commandType = "add_node"
	cmdRemoveNode              commandType = "remove_node"
	cmdStoreAuditEvent         commandType = "store_audit_event"
	cmdDeleteAuditEvent        commandType = "delete_audit_event"
)

type command struct {
	Type commandType     `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Key prefixes for BadgerDB
const (
	prefixBucket        = "bucket:"
	prefixObject        = "object:"
	prefixObjectVersion = "objver:"
	prefixMultipart     = "multipart:"
	prefixUser          = "user:"
	prefixUsername      = "username:"
	prefixAccessKey     = "accesskey:"
	prefixPolicy        = "policy:"
	prefixNode          = "node:"
	prefixAudit         = "audit:"
)

// fsm implements raft.FSM for the metadata store
type fsm struct {
	db *badger.DB
}

// Apply applies a Raft log entry to the FSM
func (f *fsm) Apply(l *raft.Log) interface{} {
	var cmd command
	if err := json.Unmarshal(l.Data, &cmd); err != nil {
		return err
	}

	switch cmd.Type {
	case cmdCreateBucket:
		var bucket Bucket
		if err := json.Unmarshal(cmd.Data, &bucket); err != nil {
			return err
		}
		return f.applyCreateBucket(&bucket)

	case cmdDeleteBucket:
		var name string
		if err := json.Unmarshal(cmd.Data, &name); err != nil {
			return err
		}
		return f.applyDeleteBucket(name)

	case cmdUpdateBucket:
		var bucket Bucket
		if err := json.Unmarshal(cmd.Data, &bucket); err != nil {
			return err
		}
		return f.applyUpdateBucket(&bucket)

	case cmdPutObjectMeta:
		var meta ObjectMeta
		if err := json.Unmarshal(cmd.Data, &meta); err != nil {
			return err
		}
		return f.applyPutObjectMeta(&meta)

	case cmdDeleteObjectMeta:
		var data struct {
			Bucket string `json:"bucket"`
			Key    string `json:"key"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return f.applyDeleteObjectMeta(data.Bucket, data.Key)

	case cmdPutObjectMetaVersioned:
		var data struct {
			Meta                *ObjectMeta `json:"meta"`
			PreserveOldVersions bool        `json:"preserve_old_versions"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return f.applyPutObjectMetaVersioned(data.Meta, data.PreserveOldVersions)

	case cmdDeleteObjectVersion:
		var data struct {
			Bucket    string `json:"bucket"`
			Key       string `json:"key"`
			VersionID string `json:"version_id"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return f.applyDeleteObjectVersion(data.Bucket, data.Key, data.VersionID)

	case cmdCreateUser:
		var user User
		if err := json.Unmarshal(cmd.Data, &user); err != nil {
			return err
		}
		return f.applyCreateUser(&user)

	case cmdUpdateUser:
		var user User
		if err := json.Unmarshal(cmd.Data, &user); err != nil {
			return err
		}
		return f.applyUpdateUser(&user)

	case cmdDeleteUser:
		var id string
		if err := json.Unmarshal(cmd.Data, &id); err != nil {
			return err
		}
		return f.applyDeleteUser(id)

	case cmdCreateAccessKey:
		var key AccessKey
		if err := json.Unmarshal(cmd.Data, &key); err != nil {
			return err
		}
		return f.applyCreateAccessKey(&key)

	case cmdDeleteAccessKey:
		var id string
		if err := json.Unmarshal(cmd.Data, &id); err != nil {
			return err
		}
		return f.applyDeleteAccessKey(id)

	case cmdCreatePolicy:
		var policy Policy
		if err := json.Unmarshal(cmd.Data, &policy); err != nil {
			return err
		}
		return f.applyCreatePolicy(&policy)

	case cmdUpdatePolicy:
		var policy Policy
		if err := json.Unmarshal(cmd.Data, &policy); err != nil {
			return err
		}
		return f.applyUpdatePolicy(&policy)

	case cmdDeletePolicy:
		var name string
		if err := json.Unmarshal(cmd.Data, &name); err != nil {
			return err
		}
		return f.applyDeletePolicy(name)

	case cmdCreateMultipartUpload:
		var upload MultipartUpload
		if err := json.Unmarshal(cmd.Data, &upload); err != nil {
			return err
		}
		return f.applyCreateMultipartUpload(&upload)

	case cmdAbortMultipartUpload:
		var data struct {
			Bucket   string `json:"bucket"`
			Key      string `json:"key"`
			UploadID string `json:"upload_id"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return f.applyAbortMultipartUpload(data.Bucket, data.Key, data.UploadID)

	case cmdCompleteMultipartUpload:
		var data struct {
			Bucket   string `json:"bucket"`
			Key      string `json:"key"`
			UploadID string `json:"upload_id"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return f.applyCompleteMultipartUpload(data.Bucket, data.Key, data.UploadID)

	case cmdAddUploadPart:
		var data struct {
			Bucket   string     `json:"bucket"`
			Key      string     `json:"key"`
			UploadID string     `json:"upload_id"`
			Part     UploadPart `json:"part"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return f.applyAddUploadPart(data.Bucket, data.Key, data.UploadID, &data.Part)

	case cmdAddNode:
		var node NodeInfo
		if err := json.Unmarshal(cmd.Data, &node); err != nil {
			return err
		}
		return f.applyAddNode(&node)

	case cmdRemoveNode:
		var nodeID string
		if err := json.Unmarshal(cmd.Data, &nodeID); err != nil {
			return err
		}
		return f.applyRemoveNode(nodeID)

	case cmdStoreAuditEvent:
		var event audit.AuditEvent
		if err := json.Unmarshal(cmd.Data, &event); err != nil {
			return err
		}
		return f.applyStoreAuditEvent(&event)

	case cmdDeleteAuditEvent:
		var eventID string
		if err := json.Unmarshal(cmd.Data, &eventID); err != nil {
			return err
		}
		return f.applyDeleteAuditEvent(eventID)
	}

	return fmt.Errorf("unknown command type: %s", cmd.Type)
}

// FSM apply methods
func (f *fsm) applyCreateBucket(bucket *Bucket) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixBucket + bucket.Name)

		// Check if bucket already exists
		_, err := txn.Get(key)
		if err == nil {
			return fmt.Errorf("bucket already exists: %s", bucket.Name)
		}
		if err != badger.ErrKeyNotFound {
			return err
		}

		data, err := json.Marshal(bucket)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyDeleteBucket(name string) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixBucket + name)
		return txn.Delete(key)
	})
}

func (f *fsm) applyUpdateBucket(bucket *Bucket) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixBucket + bucket.Name)
		data, err := json.Marshal(bucket)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyPutObjectMeta(meta *ObjectMeta) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s", prefixObject, meta.Bucket, meta.Key))
		data, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyDeleteObjectMeta(bucket, objKey string) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s", prefixObject, bucket, objKey))
		return txn.Delete(key)
	})
}

func (f *fsm) applyPutObjectMetaVersioned(meta *ObjectMeta, preserveOldVersions bool) error {
	return f.db.Update(func(txn *badger.Txn) error {
		// Current object key (for latest version pointer)
		currentKey := []byte(fmt.Sprintf("%s%s/%s", prefixObject, meta.Bucket, meta.Key))

		if preserveOldVersions {
			// Get current version and mark it as not latest
			item, err := txn.Get(currentKey)
			if err == nil {
				var oldMeta ObjectMeta
				err = item.Value(func(val []byte) error {
					return json.Unmarshal(val, &oldMeta)
				})
				if err == nil && oldMeta.VersionID != "" {
					// Mark old version as not latest
					oldMeta.IsLatest = false
					oldData, err := json.Marshal(&oldMeta)
					if err != nil {
						return err
					}

					// Store old version with compound key: objver:{bucket}/{key}#{versionID}
					oldVersionKey := []byte(fmt.Sprintf("%s%s/%s#%s", prefixObjectVersion, oldMeta.Bucket, oldMeta.Key, oldMeta.VersionID))
					if err := txn.Set(oldVersionKey, oldData); err != nil {
						return err
					}
				}
			}
		}

		// Mark new version as latest
		meta.IsLatest = true

		// Store new version in version history if it has a version ID
		if meta.VersionID != "" {
			versionKey := []byte(fmt.Sprintf("%s%s/%s#%s", prefixObjectVersion, meta.Bucket, meta.Key, meta.VersionID))
			versionData, err := json.Marshal(meta)
			if err != nil {
				return err
			}
			if err := txn.Set(versionKey, versionData); err != nil {
				return err
			}
		}

		// Store/update current version pointer
		data, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return txn.Set(currentKey, data)
	})
}

func (f *fsm) applyDeleteObjectVersion(bucket, objKey, versionID string) error {
	return f.db.Update(func(txn *badger.Txn) error {
		// Delete the specific version
		versionKey := []byte(fmt.Sprintf("%s%s/%s#%s", prefixObjectVersion, bucket, objKey, versionID))
		if err := txn.Delete(versionKey); err != nil && err != badger.ErrKeyNotFound {
			return err
		}

		// Check if this was the current/latest version
		currentKey := []byte(fmt.Sprintf("%s%s/%s", prefixObject, bucket, objKey))
		item, err := txn.Get(currentKey)
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}
			return err
		}

		var currentMeta ObjectMeta
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &currentMeta)
		})
		if err != nil {
			return err
		}

		// If we deleted the current version, we need to find the next latest version
		if currentMeta.VersionID == versionID {
			// Find the most recent remaining version
			prefix := []byte(fmt.Sprintf("%s%s/%s#", prefixObjectVersion, bucket, objKey))
			opts := badger.DefaultIteratorOptions
			opts.Prefix = prefix
			opts.Reverse = true // Get newest first (ULIDs are sortable)

			it := txn.NewIterator(opts)
			defer it.Close()

			it.Seek(append(prefix, 0xFF)) // Seek to end of prefix range

			if it.ValidForPrefix(prefix) {
				// Found another version, make it the current
				item := it.Item()
				val, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}

				var newLatest ObjectMeta
				if err := json.Unmarshal(val, &newLatest); err != nil {
					return err
				}

				newLatest.IsLatest = true
				data, err := json.Marshal(&newLatest)
				if err != nil {
					return err
				}

				// Update version store
				if err := txn.Set(item.KeyCopy(nil), data); err != nil {
					return err
				}

				// Update current pointer
				return txn.Set(currentKey, data)
			}

			// No more versions, delete the current pointer
			return txn.Delete(currentKey)
		}

		return nil
	})
}

func (f *fsm) applyCreateUser(user *User) error {
	return f.db.Update(func(txn *badger.Txn) error {
		// Store by ID
		key := []byte(prefixUser + user.ID)
		data, err := json.Marshal(user)
		if err != nil {
			return err
		}
		if err := txn.Set(key, data); err != nil {
			return err
		}

		// Store username -> ID mapping
		usernameKey := []byte(prefixUsername + user.Username)
		return txn.Set(usernameKey, []byte(user.ID))
	})
}

func (f *fsm) applyUpdateUser(user *User) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixUser + user.ID)
		data, err := json.Marshal(user)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyDeleteUser(id string) error {
	return f.db.Update(func(txn *badger.Txn) error {
		// Get user first to delete username mapping
		key := []byte(prefixUser + id)
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		var user User
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &user)
		})
		if err != nil {
			return err
		}

		// Delete username mapping
		usernameKey := []byte(prefixUsername + user.Username)
		if err := txn.Delete(usernameKey); err != nil {
			return err
		}

		// Delete user
		return txn.Delete(key)
	})
}

func (f *fsm) applyCreateAccessKey(accessKey *AccessKey) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixAccessKey + accessKey.AccessKeyID)
		data, err := json.Marshal(accessKey)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyDeleteAccessKey(accessKeyID string) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixAccessKey + accessKeyID)
		return txn.Delete(key)
	})
}

func (f *fsm) applyCreatePolicy(policy *Policy) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixPolicy + policy.Name)
		data, err := json.Marshal(policy)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyUpdatePolicy(policy *Policy) error {
	return f.applyCreatePolicy(policy)
}

func (f *fsm) applyDeletePolicy(name string) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixPolicy + name)
		return txn.Delete(key)
	})
}

func (f *fsm) applyCreateMultipartUpload(upload *MultipartUpload) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s/%s", prefixMultipart, upload.Bucket, upload.Key, upload.UploadID))
		data, err := json.Marshal(upload)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyAbortMultipartUpload(bucket, objKey, uploadID string) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s/%s", prefixMultipart, bucket, objKey, uploadID))
		return txn.Delete(key)
	})
}

func (f *fsm) applyCompleteMultipartUpload(bucket, objKey, uploadID string) error {
	return f.applyAbortMultipartUpload(bucket, objKey, uploadID)
}

func (f *fsm) applyAddUploadPart(bucket, objKey, uploadID string, part *UploadPart) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s/%s", prefixMultipart, bucket, objKey, uploadID))
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		var upload MultipartUpload
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &upload)
		})
		if err != nil {
			return err
		}

		// Add or update part
		found := false
		for i, p := range upload.Parts {
			if p.PartNumber == part.PartNumber {
				upload.Parts[i] = *part
				found = true
				break
			}
		}
		if !found {
			upload.Parts = append(upload.Parts, *part)
		}

		data, err := json.Marshal(&upload)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyAddNode(node *NodeInfo) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixNode + node.ID)
		data, err := json.Marshal(node)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyRemoveNode(nodeID string) error {
	return f.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixNode + nodeID)
		return txn.Delete(key)
	})
}

func (f *fsm) applyStoreAuditEvent(event *audit.AuditEvent) error {
	return f.db.Update(func(txn *badger.Txn) error {
		// Use timestamp + ID as key for time-based ordering
		key := []byte(fmt.Sprintf("%s%s:%s", prefixAudit, event.Timestamp.Format(time.RFC3339Nano), event.ID))
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		return txn.Set(key, data)
	})
}

func (f *fsm) applyDeleteAuditEvent(eventID string) error {
	// We need to find and delete by event ID since we don't have the timestamp
	return f.db.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(prefixAudit)
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			val, err := item.ValueCopy(nil)
			if err != nil {
				continue
			}

			var event audit.AuditEvent
			if err := json.Unmarshal(val, &event); err != nil {
				continue
			}

			if event.ID == eventID {
				return txn.Delete(item.KeyCopy(nil))
			}
		}
		return nil
	})
}

// Snapshot returns a snapshot of the FSM
func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	return &fsmSnapshot{db: f.db}, nil
}

// Restore restores the FSM from a snapshot
func (f *fsm) Restore(rc io.ReadCloser) error {
	defer func() { _ = rc.Close() }()

	// Clear existing data
	err := f.db.DropAll()
	if err != nil {
		return err
	}

	// Read snapshot data
	decoder := json.NewDecoder(rc)
	return f.db.Update(func(txn *badger.Txn) error {
		for {
			var kv struct {
				Key   []byte `json:"key"`
				Value []byte `json:"value"`
			}
			if err := decoder.Decode(&kv); err == io.EOF {
				break
			} else if err != nil {
				return err
			}
			if err := txn.Set(kv.Key, kv.Value); err != nil {
				return err
			}
		}
		return nil
	})
}

// fsmSnapshot implements raft.FSMSnapshot
type fsmSnapshot struct {
	db *badger.DB
}

// Persist writes the snapshot to the given sink
func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	encoder := json.NewEncoder(sink)

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			kv := struct {
				Key   []byte `json:"key"`
				Value []byte `json:"value"`
			}{Key: key, Value: val}

			if err := encoder.Encode(&kv); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		_ = sink.Cancel()
		return err
	}

	return sink.Close()
}

// Release is called when the snapshot is no longer needed
func (s *fsmSnapshot) Release() {}
