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

	mu sync.RWMutex
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
	cmdCreateBucket          commandType = "create_bucket"
	cmdDeleteBucket          commandType = "delete_bucket"
	cmdUpdateBucket          commandType = "update_bucket"
	cmdPutObjectMeta         commandType = "put_object_meta"
	cmdDeleteObjectMeta      commandType = "delete_object_meta"
	cmdCreateMultipartUpload commandType = "create_multipart_upload"
	cmdAbortMultipartUpload  commandType = "abort_multipart_upload"
	cmdCompleteMultipartUpload commandType = "complete_multipart_upload"
	cmdAddUploadPart         commandType = "add_upload_part"
	cmdCreateUser            commandType = "create_user"
	cmdUpdateUser            commandType = "update_user"
	cmdDeleteUser            commandType = "delete_user"
	cmdCreateAccessKey       commandType = "create_access_key"
	cmdDeleteAccessKey       commandType = "delete_access_key"
	cmdCreatePolicy          commandType = "create_policy"
	cmdUpdatePolicy          commandType = "update_policy"
	cmdDeletePolicy          commandType = "delete_policy"
	cmdAddNode               commandType = "add_node"
	cmdRemoveNode            commandType = "remove_node"
)

type command struct {
	Type commandType     `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Key prefixes for BadgerDB
const (
	prefixBucket    = "bucket:"
	prefixObject    = "object:"
	prefixMultipart = "multipart:"
	prefixUser      = "user:"
	prefixUsername  = "username:"
	prefixAccessKey = "accesskey:"
	prefixPolicy    = "policy:"
	prefixNode      = "node:"
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

// Snapshot returns a snapshot of the FSM
func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	return &fsmSnapshot{db: f.db}, nil
}

// Restore restores the FSM from a snapshot
func (f *fsm) Restore(rc io.ReadCloser) error {
	defer rc.Close()

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
		sink.Cancel()
		return err
	}

	return sink.Close()
}

// Release is called when the snapshot is no longer needed
func (s *fsmSnapshot) Release() {}
