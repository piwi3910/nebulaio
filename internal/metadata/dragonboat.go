package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/lni/dragonboat/v4"
	"github.com/lni/dragonboat/v4/config"
	"github.com/lni/dragonboat/v4/statemachine"
	"github.com/rs/zerolog/log"
)

// hashNodeID converts a string node ID to a uint64 replica ID
func hashNodeID(nodeID string) uint64 {
	// Try parsing as uint64 first
	if id, err := strconv.ParseUint(nodeID, 10, 64); err == nil {
		return id
	}
	// Fall back to hash
	h := fnv.New64a()
	h.Write([]byte(nodeID))
	return h.Sum64()
}

// DragonboatConfig holds configuration for the Dragonboat store
type DragonboatConfig struct {
	NodeID         uint64
	ShardID        uint64
	DataDir        string
	RaftAddress    string
	Bootstrap      bool
	InitialMembers map[uint64]string // nodeID -> raftAddress
}

// DragonboatStore implements the Store interface using Dragonboat for consensus
// and BadgerDB for the underlying key-value storage
type DragonboatStore struct {
	config   DragonboatConfig
	nodeHost *dragonboat.NodeHost
	shardID  uint64
	nodeID   uint64
	badger   *badger.DB

	_mu sync.RWMutex // Reserved for future thread-safe operations
}

// NewDragonboatStore creates a new Dragonboat-backed metadata store
func NewDragonboatStore(cfg DragonboatConfig) (*DragonboatStore, error) {
	// Create directories
	badgerDir := filepath.Join(cfg.DataDir, "badger")
	nodeHostDir := filepath.Join(cfg.DataDir, "nodehost")
	walDir := filepath.Join(cfg.DataDir, "wal")

	if err := os.MkdirAll(badgerDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create badger directory: %w", err)
	}
	if err := os.MkdirAll(nodeHostDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create nodehost directory: %w", err)
	}
	if err := os.MkdirAll(walDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create wal directory: %w", err)
	}

	// Open BadgerDB
	opts := badger.DefaultOptions(badgerDir)
	opts.Logger = nil // Disable badger logging
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger: %w", err)
	}

	// Configure NodeHost
	nhConfig := config.NodeHostConfig{
		WALDir:         walDir,
		NodeHostDir:    nodeHostDir,
		RTTMillisecond: 200,
		RaftAddress:    cfg.RaftAddress,
	}

	// Create NodeHost
	nh, err := dragonboat.NewNodeHost(nhConfig)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create nodehost: %w", err)
	}

	// Configure Raft shard
	rc := config.Config{
		ReplicaID:          cfg.NodeID,
		ShardID:            cfg.ShardID,
		ElectionRTT:        10,
		HeartbeatRTT:       1,
		CheckQuorum:        true,
		SnapshotEntries:    1024,
		CompactionOverhead: 500,
	}

	// Create state machine factory
	store := &DragonboatStore{
		config:   cfg,
		nodeHost: nh,
		shardID:  cfg.ShardID,
		nodeID:   cfg.NodeID,
		badger:   db,
	}

	smFactory := func(shardID uint64, nodeID uint64) statemachine.IStateMachine {
		return newStateMachine(db)
	}

	// Start or join the replica
	if cfg.Bootstrap {
		// Bootstrap a new cluster
		if err := nh.StartReplica(cfg.InitialMembers, false, smFactory, rc); err != nil {
			nh.Close()
			db.Close()
			return nil, fmt.Errorf("failed to start replica: %w", err)
		}
		log.Info().
			Uint64("shard_id", cfg.ShardID).
			Uint64("node_id", cfg.NodeID).
			Msg("Bootstrapped new Dragonboat cluster")
	} else {
		// Join existing cluster
		if err := nh.StartReplica(nil, true, smFactory, rc); err != nil {
			nh.Close()
			db.Close()
			return nil, fmt.Errorf("failed to start replica: %w", err)
		}
		log.Info().
			Uint64("shard_id", cfg.ShardID).
			Uint64("node_id", cfg.NodeID).
			Msg("Joined existing Dragonboat cluster")
	}

	// Wait for leader election
	log.Info().Msg("Waiting for Dragonboat leader election...")
	if err := store.WaitForLeader(30 * time.Second); err != nil {
		log.Warn().Err(err).Msg("Timeout waiting for leader election, continuing anyway")
	} else {
		leaderID, _, _, _ := nh.GetLeaderID(cfg.ShardID)
		log.Info().
			Uint64("leader_id", leaderID).
			Bool("is_leader", store.IsLeader()).
			Msg("Dragonboat cluster ready")
	}

	return store, nil
}

// Close shuts down the Dragonboat store
func (s *DragonboatStore) Close() error {
	s.nodeHost.Close()
	return s.badger.Close()
}

// IsLeader returns true if this node is the Raft leader
func (s *DragonboatStore) IsLeader() bool {
	leaderID, _, valid, err := s.nodeHost.GetLeaderID(s.shardID)
	if err != nil || !valid {
		return false
	}
	return leaderID == s.nodeID
}

// LeaderAddress returns the address of the current leader
func (s *DragonboatStore) LeaderAddress() (string, error) {
	leaderID, _, valid, err := s.nodeHost.GetLeaderID(s.shardID)
	if err != nil {
		return "", err
	}
	if !valid {
		return "", fmt.Errorf("no valid leader")
	}

	// Get membership info to map leader ID to address
	membership, err := s.nodeHost.SyncGetShardMembership(context.Background(), s.shardID)
	if err != nil {
		return "", err
	}

	for nodeID, addr := range membership.Nodes {
		if nodeID == leaderID {
			return addr, nil
		}
	}

	return "", fmt.Errorf("leader address not found")
}

// GetNodeHost returns the underlying NodeHost instance
func (s *DragonboatStore) GetNodeHost() *dragonboat.NodeHost {
	return s.nodeHost
}

// State returns the current node state
func (s *DragonboatStore) State() string {
	if s.IsLeader() {
		return "Leader"
	}
	return "Follower"
}

// Stats returns node statistics
func (s *DragonboatStore) Stats() map[string]string {
	stats := make(map[string]string)
	stats["shard_id"] = fmt.Sprintf("%d", s.shardID)
	stats["node_id"] = fmt.Sprintf("%d", s.nodeID)
	stats["state"] = s.State()

	leaderID, _, valid, _ := s.nodeHost.GetLeaderID(s.shardID)
	if valid {
		stats["leader_id"] = fmt.Sprintf("%d", leaderID)
	}

	return stats
}

// JoinCluster adds this node to an existing cluster
// This is called by a non-bootstrap node to join the cluster
func (s *DragonboatStore) JoinCluster(leaderNodeID uint64, nodeID uint64, raftAddr string) error {
	log.Info().
		Uint64("leader_node_id", leaderNodeID).
		Uint64("node_id", nodeID).
		Str("raft_addr", raftAddr).
		Msg("Requesting to join Dragonboat cluster")

	// The actual join happens via AddVoter on the leader
	return nil
}

// AddVoter adds a new voting member to the cluster
// This must be called on the leader
func (s *DragonboatStore) AddVoter(nodeID string, raftAddr string) error {
	if !s.IsLeader() {
		leaderAddr, _ := s.LeaderAddress()
		return fmt.Errorf("not the leader, current leader address: %s", leaderAddr)
	}

	replicaID := hashNodeID(nodeID)
	log.Info().
		Str("node_id", nodeID).
		Uint64("replica_id", replicaID).
		Str("raft_addr", raftAddr).
		Msg("Adding voter to Dragonboat cluster")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := s.nodeHost.SyncRequestAddReplica(ctx, s.shardID, replicaID, raftAddr, 0)
	if err != nil {
		return fmt.Errorf("failed to add voter: %w", err)
	}

	log.Info().
		Str("node_id", nodeID).
		Msg("Successfully added voter to Dragonboat cluster")

	return nil
}

// AddNonvoter adds a new non-voting member to the cluster
// Non-voters receive log replication but don't vote in elections
func (s *DragonboatStore) AddNonvoter(nodeID string, raftAddr string) error {
	if !s.IsLeader() {
		leaderAddr, _ := s.LeaderAddress()
		return fmt.Errorf("not the leader, current leader address: %s", leaderAddr)
	}

	replicaID := hashNodeID(nodeID)
	log.Info().
		Str("node_id", nodeID).
		Uint64("replica_id", replicaID).
		Str("raft_addr", raftAddr).
		Msg("Adding non-voter to Dragonboat cluster")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := s.nodeHost.SyncRequestAddNonVoting(ctx, s.shardID, replicaID, raftAddr, 0)
	if err != nil {
		return fmt.Errorf("failed to add non-voter: %w", err)
	}

	return nil
}

// RemoveServer removes a server from the cluster
func (s *DragonboatStore) RemoveServer(nodeID string) error {
	if !s.IsLeader() {
		leaderAddr, _ := s.LeaderAddress()
		return fmt.Errorf("not the leader, current leader address: %s", leaderAddr)
	}

	replicaID := hashNodeID(nodeID)
	log.Info().
		Str("node_id", nodeID).
		Uint64("replica_id", replicaID).
		Msg("Removing server from Dragonboat cluster")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := s.nodeHost.SyncRequestDeleteReplica(ctx, s.shardID, replicaID, 0)
	if err != nil {
		return fmt.Errorf("failed to remove server: %w", err)
	}

	log.Info().
		Str("node_id", nodeID).
		Msg("Successfully removed server from Dragonboat cluster")

	return nil
}

// GetConfiguration returns the current cluster configuration
func (s *DragonboatStore) GetConfiguration() (*dragonboat.Membership, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	membership, err := s.nodeHost.SyncGetShardMembership(ctx, s.shardID)
	if err != nil {
		return nil, err
	}

	return membership, nil
}

// GetServers returns the list of servers in the cluster
func (s *DragonboatStore) GetServers() (map[uint64]string, error) {
	membership, err := s.GetConfiguration()
	if err != nil {
		return nil, err
	}
	return membership.Nodes, nil
}

// ClusterServerInfo represents a server in the cluster configuration
// Used by the admin API for a common interface between Raft implementations
type ClusterServerInfo struct {
	ID      uint64
	Address string
	IsVoter bool
}

// ClusterConfiguration represents the cluster membership
// Used by the admin API for a common interface between Raft implementations
type ClusterConfiguration struct {
	Servers []ClusterServerInfo
}

// GetClusterConfiguration returns the cluster configuration in a common format
func (s *DragonboatStore) GetClusterConfiguration() (*ClusterConfiguration, error) {
	membership, err := s.GetConfiguration()
	if err != nil {
		return nil, err
	}

	config := &ClusterConfiguration{
		Servers: make([]ClusterServerInfo, 0, len(membership.Nodes)+len(membership.NonVotings)),
	}

	// Add voting members
	for nodeID, addr := range membership.Nodes {
		config.Servers = append(config.Servers, ClusterServerInfo{
			ID:      nodeID,
			Address: addr,
			IsVoter: true,
		})
	}

	// Add non-voting members
	for nodeID, addr := range membership.NonVotings {
		config.Servers = append(config.Servers, ClusterServerInfo{
			ID:      nodeID,
			Address: addr,
			IsVoter: false,
		})
	}

	return config, nil
}

// TransferLeadership transfers leadership to another node
// Dragonboat handles leader transfer automatically during configuration changes
// This method initiates a transfer by requesting the target node to be the leader
func (s *DragonboatStore) TransferLeadership(targetID string, targetAddr string) error {
	if !s.IsLeader() {
		leaderAddr, _ := s.LeaderAddress()
		return fmt.Errorf("not the leader, current leader address: %s", leaderAddr)
	}

	replicaID := hashNodeID(targetID)
	log.Info().
		Str("target_id", targetID).
		Uint64("target_replica_id", replicaID).
		Str("target_addr", targetAddr).
		Msg("Requesting leadership transfer")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Request leadership transfer
	err := s.nodeHost.RequestLeaderTransfer(s.shardID, replicaID)
	if err != nil {
		return fmt.Errorf("failed to request leadership transfer: %w", err)
	}

	// Wait for the transfer to complete
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for leadership transfer")
		case <-ticker.C:
			leaderID, _, valid, err := s.nodeHost.GetLeaderID(s.shardID)
			if err == nil && valid && leaderID == replicaID {
				log.Info().
					Str("target_id", targetID).
					Msg("Leadership transfer completed")
				return nil
			}
		}
	}
}

// WaitForLeader waits for a leader to be elected
func (s *DragonboatStore) WaitForLeader(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for leader")
		case <-ticker.C:
			leaderID, _, valid, err := s.nodeHost.GetLeaderID(s.shardID)
			if err == nil && valid && leaderID != 0 {
				return nil
			}
		}
	}
}

// Snapshot triggers a manual snapshot
func (s *DragonboatStore) Snapshot() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, err := s.nodeHost.SyncRequestSnapshot(ctx, s.shardID, dragonboat.SnapshotOption{})
	return err
}

// LastIndex returns the last index in the log
func (s *DragonboatStore) LastIndex() (uint64, error) {
	// Dragonboat doesn't expose this directly, we'd need to track it
	// For now, return 0
	return 0, nil
}

// AppliedIndex returns the last applied index
func (s *DragonboatStore) AppliedIndex() (uint64, error) {
	// Dragonboat doesn't expose this directly, we'd need to track it
	// For now, return 0
	return 0, nil
}

// apply sends a command through Dragonboat
func (s *DragonboatStore) apply(cmd *command) error {
	if !s.IsLeader() {
		return fmt.Errorf("not leader")
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := s.nodeHost.SyncPropose(ctx, s.nodeHost.GetNoOPSession(s.shardID), data)
	if err != nil {
		return err
	}

	// Check if the result is an error
	if result.Value > 0 {
		// Non-zero value indicates an error occurred in the state machine
		return fmt.Errorf("state machine error: code %d", result.Value)
	}

	return nil
}

// get retrieves a value from BadgerDB
func (s *DragonboatStore) get(key []byte) ([]byte, error) {
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
func (s *DragonboatStore) scan(prefix []byte) ([][]byte, error) {
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
