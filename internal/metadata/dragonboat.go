package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/lni/dragonboat/v4"
	"github.com/lni/dragonboat/v4/config"
	"github.com/lni/dragonboat/v4/statemachine"
	"github.com/rs/zerolog/log"
)

// Dragonboat configuration constants.
const (
	dirPermissions            = 0750                   // Directory permissions for metadata storage
	defaultRTTMillisecond     = 200                    // RTT in milliseconds for Raft
	defaultElectionRTT        = 10                     // Election RTT multiplier
	defaultSnapshotEntries    = 1024                   // Entries before snapshot
	defaultCompactionOverhead = 500                    // Compaction overhead entries
	leaderWaitTimeout         = 30 * time.Second       // Timeout waiting for leader election
	operationTimeout          = 30 * time.Second       // Timeout for Raft operations
	shortOperationTimeout     = 5 * time.Second        // Short timeout for quick operations
	syncReadTimeout           = 10 * time.Second       // Timeout for sync read operations
	listOperationTimeout      = 60 * time.Second       // Timeout for list operations
	defaultListMaxKeys        = 100                    // Default max keys for list operations
	pollingInterval           = 100 * time.Millisecond // Polling interval for leader checks
)

// hashNodeID converts a string node ID to a uint64 replica ID.
func hashNodeID(nodeID string) uint64 {
	// Try parsing as uint64 first
	id, err := strconv.ParseUint(nodeID, 10, 64)
	if err == nil {
		return id
	}
	// Fall back to hash
	h := fnv.New64a()
	h.Write([]byte(nodeID))

	return h.Sum64()
}

// DragonboatConfig holds configuration for the Dragonboat store.
type DragonboatConfig struct {
	InitialMembers map[uint64]string
	DataDir        string
	RaftAddress    string
	NodeID         uint64
	ShardID        uint64
	Bootstrap      bool
}

// DragonboatStore implements the Store interface using Dragonboat for consensus
// and BadgerDB for the underlying key-value storage.
type DragonboatStore struct {
	// 8-byte fields (pointers, uint64)
	nodeHost *dragonboat.NodeHost
	badger   *badger.DB
	shardID  uint64
	nodeID   uint64
	// Structs
	config DragonboatConfig
}

// NewDragonboatStore creates a new Dragonboat-backed metadata store.
func NewDragonboatStore(cfg DragonboatConfig) (*DragonboatStore, error) {
	// Create directories
	badgerDir := filepath.Join(cfg.DataDir, "badger")
	nodeHostDir := filepath.Join(cfg.DataDir, "nodehost")
	walDir := filepath.Join(cfg.DataDir, "wal")

	err := os.MkdirAll(badgerDir, dirPermissions)
	if err != nil {
		return nil, fmt.Errorf("failed to create badger directory: %w", err)
	}

	err = os.MkdirAll(nodeHostDir, dirPermissions)
	if err != nil {
		return nil, fmt.Errorf("failed to create nodehost directory: %w", err)
	}

	err = os.MkdirAll(walDir, dirPermissions)
	if err != nil {
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
		RTTMillisecond: defaultRTTMillisecond,
		RaftAddress:    cfg.RaftAddress,
	}

	// Create NodeHost
	nh, err := dragonboat.NewNodeHost(nhConfig)
	if err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close BadgerDB during cleanup after NodeHost creation failed")
		}

		return nil, fmt.Errorf("failed to create nodehost: %w", err)
	}

	// Configure Raft shard
	rc := config.Config{
		ReplicaID:          cfg.NodeID,
		ShardID:            cfg.ShardID,
		ElectionRTT:        defaultElectionRTT,
		HeartbeatRTT:       1,
		CheckQuorum:        true,
		SnapshotEntries:    defaultSnapshotEntries,
		CompactionOverhead: defaultCompactionOverhead,
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
		err := nh.StartReplica(cfg.InitialMembers, false, smFactory, rc)
		if err != nil {
			nh.Close()

			closeErr := db.Close()
			if closeErr != nil {
				log.Error().Err(closeErr).Msg("failed to close BadgerDB during cleanup after replica start failed")
			}

			return nil, fmt.Errorf("failed to start replica: %w", err)
		}

		log.Info().
			Uint64("shard_id", cfg.ShardID).
			Uint64("node_id", cfg.NodeID).
			Msg("Bootstrapped new Dragonboat cluster")
	} else {
		// Join existing cluster
		err := nh.StartReplica(nil, true, smFactory, rc)
		if err != nil {
			nh.Close()

			closeErr := db.Close()
			if closeErr != nil {
				log.Error().Err(closeErr).Msg("failed to close BadgerDB during cleanup after replica join failed")
			}

			return nil, fmt.Errorf("failed to start replica: %w", err)
		}

		log.Info().
			Uint64("shard_id", cfg.ShardID).
			Uint64("node_id", cfg.NodeID).
			Msg("Joined existing Dragonboat cluster")
	}

	// Wait for leader election
	log.Info().Msg("Waiting for Dragonboat leader election...")

	err = store.WaitForLeader(leaderWaitTimeout)
	if err != nil {
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

// Close shuts down the Dragonboat store.
func (s *DragonboatStore) Close() error {
	s.nodeHost.Close()
	return s.badger.Close()
}

// IsLeader returns true if this node is the Raft leader.
func (s *DragonboatStore) IsLeader() bool {
	leaderID, _, valid, err := s.nodeHost.GetLeaderID(s.shardID)
	if err != nil || !valid {
		return false
	}

	return leaderID == s.nodeID
}

// LeaderAddress returns the address of the current leader.
func (s *DragonboatStore) LeaderAddress() (string, error) {
	leaderID, _, valid, err := s.nodeHost.GetLeaderID(s.shardID)
	if err != nil {
		return "", err
	}

	if !valid {
		return "", errors.New("no valid leader")
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

	return "", errors.New("leader address not found")
}

// GetNodeHost returns the underlying NodeHost instance.
func (s *DragonboatStore) GetNodeHost() *dragonboat.NodeHost {
	return s.nodeHost
}

// State returns the current node state.
func (s *DragonboatStore) State() string {
	if s.IsLeader() {
		return "Leader"
	}

	return "Follower"
}

// Stats returns node statistics.
func (s *DragonboatStore) Stats() map[string]string {
	stats := make(map[string]string)
	stats["shard_id"] = strconv.FormatUint(s.shardID, 10)
	stats["node_id"] = strconv.FormatUint(s.nodeID, 10)
	stats["state"] = s.State()

	leaderID, _, valid, _ := s.nodeHost.GetLeaderID(s.shardID)
	if valid {
		stats["leader_id"] = strconv.FormatUint(leaderID, 10)
	}

	return stats
}

// JoinCluster adds this node to an existing cluster
// This is called by a non-bootstrap node to join the cluster.
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
// This must be called on the leader.
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

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
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
// Non-voters receive log replication but don't vote in elections.
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

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()

	err := s.nodeHost.SyncRequestAddNonVoting(ctx, s.shardID, replicaID, raftAddr, 0)
	if err != nil {
		return fmt.Errorf("failed to add non-voter: %w", err)
	}

	return nil
}

// RemoveServer removes a server from the cluster.
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

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
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

// GetConfiguration returns the current cluster configuration.
func (s *DragonboatStore) GetConfiguration(ctx context.Context) (*dragonboat.Membership, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, shortOperationTimeout)
	defer cancel()

	membership, err := s.nodeHost.SyncGetShardMembership(timeoutCtx, s.shardID)
	if err != nil {
		return nil, err
	}

	return membership, nil
}

// GetServers returns the list of servers in the cluster.
func (s *DragonboatStore) GetServers(ctx context.Context) (map[uint64]string, error) {
	membership, err := s.GetConfiguration(ctx)
	if err != nil {
		return nil, err
	}

	return membership.Nodes, nil
}

// ClusterServerInfo represents a server in the cluster configuration
// Used by the admin API for a common interface between Raft implementations.
type ClusterServerInfo struct {
	Address string
	ID      uint64
	IsVoter bool
}

// ClusterConfiguration represents the cluster membership
// Used by the admin API for a common interface between Raft implementations.
type ClusterConfiguration struct {
	Servers []ClusterServerInfo
}

// GetClusterConfiguration returns the cluster configuration in a common format.
func (s *DragonboatStore) GetClusterConfiguration(ctx context.Context) (*ClusterConfiguration, error) {
	membership, err := s.GetConfiguration(ctx)
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
// This method initiates a transfer by requesting the target node to be the leader.
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

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()

	// Request leadership transfer
	err := s.nodeHost.RequestLeaderTransfer(s.shardID, replicaID)
	if err != nil {
		return fmt.Errorf("failed to request leadership transfer: %w", err)
	}

	// Wait for the transfer to complete
	ticker := time.NewTicker(pollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return errors.New("timeout waiting for leadership transfer")
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

// WaitForLeader waits for a leader to be elected.
func (s *DragonboatStore) WaitForLeader(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(pollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return errors.New("timeout waiting for leader")
		case <-ticker.C:
			leaderID, _, valid, err := s.nodeHost.GetLeaderID(s.shardID)
			if err == nil && valid && leaderID != 0 {
				return nil
			}
		}
	}
}

// Snapshot triggers a manual snapshot.
func (s *DragonboatStore) Snapshot() error {
	ctx, cancel := context.WithTimeout(context.Background(), listOperationTimeout)
	defer cancel()

	_, err := s.nodeHost.SyncRequestSnapshot(ctx, s.shardID, dragonboat.SnapshotOption{})

	return err
}

// LastIndex returns the last index in the log.
func (s *DragonboatStore) LastIndex() (uint64, error) {
	// Dragonboat doesn't expose this directly, we'd need to track it
	// For now, return 0
	return 0, nil
}

// AppliedIndex returns the last applied index.
func (s *DragonboatStore) AppliedIndex() (uint64, error) {
	// Dragonboat doesn't expose this directly, we'd need to track it
	// For now, return 0
	return 0, nil
}

// apply sends a command through Dragonboat.
func (s *DragonboatStore) apply(cmd *command) error {
	if !s.IsLeader() {
		return errors.New("not leader")
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), syncReadTimeout)
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

// get retrieves a value from BadgerDB.
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

// scan performs a prefix scan on BadgerDB.
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
