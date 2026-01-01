// Package cluster provides cluster management and node discovery for NebulaIO.
//
// The package handles:
//   - Node discovery via gossip protocol (memberlist)
//   - Cluster membership tracking
//   - Leader election coordination with Dragonboat
//   - Node health monitoring
//   - Placement group management
//
// Cluster communication uses two protocols:
//   - Raft (port 9003): Consensus for metadata operations
//   - Gossip (port 9004): Node discovery and health checks
//
// The Discovery service maintains a consistent view of the cluster across
// all nodes, enabling proper request routing and failover.
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/lni/dragonboat/v4"
	"github.com/rs/zerolog/log"
)

// DiscoveryConfig holds configuration for cluster discovery.
type DiscoveryConfig struct {
	NodeID        string
	AdvertiseAddr string
	Role          string
	Version       string
	JoinAddresses []string
	GossipPort    int
	RaftPort      int
	S3Port        int
	AdminPort     int
}

// Discovery manages node discovery and cluster membership.
type Discovery struct {
	ctx        context.Context
	members    map[string]*NodeInfo
	nodeHost   *dragonboat.NodeHost
	membership *Membership
	cancel     context.CancelFunc
	config     DiscoveryConfig
	wg         sync.WaitGroup
	shardID    uint64
	mu         sync.RWMutex
}

// NodeInfo represents information about a cluster node.
type NodeInfo struct {
	JoinedAt   time.Time `json:"joined_at"`
	LastSeen   time.Time `json:"last_seen"`
	NodeID     string    `json:"node_id"`
	RaftAddr   string    `json:"raft_addr"`
	S3Addr     string    `json:"s3_addr"`
	AdminAddr  string    `json:"admin_addr"`
	GossipAddr string    `json:"gossip_addr"`
	Role       string    `json:"role"`
	Version    string    `json:"version"`
	Status     string    `json:"status"`
}

// NodeMeta is the metadata broadcast via gossip.
type NodeMeta struct {
	NodeID    string `json:"node_id"`
	RaftAddr  string `json:"raft_addr"`
	S3Addr    string `json:"s3_addr"`
	AdminAddr string `json:"admin_addr"`
	Role      string `json:"role"`
	Version   string `json:"version"`
}

// NewDiscovery creates a new Discovery instance.
func NewDiscovery(config DiscoveryConfig) *Discovery {
	ctx, cancel := context.WithCancel(context.Background())

	return &Discovery{
		config:  config,
		members: make(map[string]*NodeInfo),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// SetNodeHost sets the NodeHost instance and shard ID for the discovery.
func (d *Discovery) SetNodeHost(nh *dragonboat.NodeHost, shardID uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nodeHost = nh
	d.shardID = shardID
}

// Start starts the discovery service.
func (d *Discovery) Start(ctx context.Context) error {
	log.Info().
		Str("node_id", d.config.NodeID).
		Str("advertise_addr", d.config.AdvertiseAddr).
		Int("gossip_port", d.config.GossipPort).
		Msg("Starting cluster discovery")

	// Build node metadata
	host := d.config.AdvertiseAddr
	if host == "" {
		host = getOutboundIP(ctx)
	}

	meta := NodeMeta{
		NodeID:    d.config.NodeID,
		RaftAddr:  fmt.Sprintf("%s:%d", host, d.config.RaftPort),
		S3Addr:    fmt.Sprintf("%s:%d", host, d.config.S3Port),
		AdminAddr: fmt.Sprintf("%s:%d", host, d.config.AdminPort),
		Role:      d.config.Role,
		Version:   d.config.Version,
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal node metadata: %w", err)
	}

	// Create membership config
	membershipConfig := MembershipConfig{
		NodeID:        d.config.NodeID,
		BindAddr:      "0.0.0.0",
		BindPort:      d.config.GossipPort,
		AdvertiseAddr: host,
		AdvertisePort: d.config.GossipPort,
		NodeMeta:      metaBytes,
	}

	// Create membership
	membership, err := NewMembership(membershipConfig, d.onNodeJoin, d.onNodeLeave, d.onNodeUpdate)
	if err != nil {
		return fmt.Errorf("failed to create membership: %w", err)
	}

	d.membership = membership

	// Add self to members
	d.mu.Lock()
	d.members[d.config.NodeID] = &NodeInfo{
		NodeID:     d.config.NodeID,
		RaftAddr:   meta.RaftAddr,
		S3Addr:     meta.S3Addr,
		AdminAddr:  meta.AdminAddr,
		GossipAddr: fmt.Sprintf("%s:%d", host, d.config.GossipPort),
		Role:       d.config.Role,
		Version:    d.config.Version,
		Status:     "alive",
		JoinedAt:   time.Now(),
		LastSeen:   time.Now(),
	}
	d.mu.Unlock()

	// Join existing cluster if join addresses provided
	if len(d.config.JoinAddresses) > 0 {
		err := d.Join(d.config.JoinAddresses[0])
		if err != nil {
			log.Warn().Err(err).Msg("Failed to join cluster, will retry")
			// Start background retry
			d.wg.Add(1)

			go d.retryJoin()
		}
	}

	// Start health check loop
	d.wg.Add(1)

	go d.healthCheckLoop()

	return nil
}

// retryJoin retries joining the cluster.
func (d *Discovery) retryJoin() {
	defer d.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			for _, addr := range d.config.JoinAddresses {
				err := d.Join(addr)
				if err != nil {
					log.Debug().Err(err).Str("addr", addr).Msg("Retry join failed")
					continue
				}

				log.Info().Str("addr", addr).Msg("Successfully joined cluster")

				return
			}
		}
	}
}

// healthCheckLoop periodically updates member status.
func (d *Discovery) healthCheckLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.updateMemberStatus()
		}
	}
}

// updateMemberStatus updates the status of all members.
func (d *Discovery) updateMemberStatus() {
	if d.membership == nil {
		return
	}

	members := d.membership.Members()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Update last seen for alive members
	for _, m := range members {
		if m.Name == d.config.NodeID {
			continue
		}

		if info, ok := d.members[m.Name]; ok {
			info.LastSeen = time.Now()
			info.Status = "alive"
		}
	}

	// Mark members not in memberlist as potentially dead
	memberNames := make(map[string]bool)
	for _, m := range members {
		memberNames[m.Name] = true
	}

	for id, info := range d.members {
		if id == d.config.NodeID {
			continue
		}

		if !memberNames[id] {
			if info.Status == "alive" {
				info.Status = "suspect"
			} else if info.Status == "suspect" && time.Since(info.LastSeen) > 30*time.Second {
				info.Status = "dead"
			}
		}
	}
}

// Stop stops the discovery service.
func (d *Discovery) Stop() error {
	log.Info().Msg("Stopping cluster discovery")

	d.cancel()

	if d.membership != nil {
		err := d.membership.Leave(5 * time.Second)
		if err != nil {
			log.Error().Err(err).Msg("Error leaving cluster")
		}
	}

	d.wg.Wait()

	return nil
}

// Join joins an existing cluster via the given address.
func (d *Discovery) Join(addr string) error {
	if d.membership == nil {
		return errors.New("membership not initialized")
	}

	log.Info().Str("addr", addr).Msg("Joining cluster")

	err := d.membership.Join([]string{addr})
	if err != nil {
		return fmt.Errorf("failed to join cluster at %s: %w", addr, err)
	}

	return nil
}

// Leave gracefully leaves the cluster.
func (d *Discovery) Leave() error {
	if d.membership == nil {
		return nil
	}

	d.mu.Lock()

	if info, ok := d.members[d.config.NodeID]; ok {
		info.Status = "leaving"
	}

	d.mu.Unlock()

	return d.membership.Leave(5 * time.Second)
}

// Members returns all known cluster members.
func (d *Discovery) Members() []*NodeInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	members := make([]*NodeInfo, 0, len(d.members))
	for _, m := range d.members {
		members = append(members, m)
	}

	return members
}

// GetMember returns information about a specific member.
func (d *Discovery) GetMember(nodeID string) (*NodeInfo, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	info, ok := d.members[nodeID]

	return info, ok
}

// LocalNode returns the local node information.
func (d *Discovery) LocalNode() *NodeInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.members[d.config.NodeID]
}

// IsLeader returns true if the local node is the Raft leader.
func (d *Discovery) IsLeader() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.nodeHost == nil {
		return false
	}

	leaderID, _, valid, err := d.nodeHost.GetLeaderID(d.shardID)
	if err != nil || !valid {
		return false
	}

	// Check if we are the leader
	info := d.nodeHost.GetNodeHostInfo(dragonboat.DefaultNodeHostInfoOption)
	for _, ci := range info.ShardInfoList {
		if ci.ShardID == d.shardID {
			return ci.ReplicaID == leaderID
		}
	}

	return false
}

// LeaderID returns the ID of the current leader.
func (d *Discovery) LeaderID(ctx context.Context) string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.nodeHost == nil {
		return ""
	}

	leaderID, _, valid, err := d.nodeHost.GetLeaderID(d.shardID)
	if err != nil || !valid {
		return ""
	}

	// Get membership to find node info
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	membership, err := d.nodeHost.SyncGetShardMembership(timeoutCtx, d.shardID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get shard membership")
		return ""
	}

	// Find the leader in our members map by replica ID
	for _, info := range d.members {
		nodeReplicaID := hashNodeID(info.NodeID)
		if nodeReplicaID == leaderID {
			return info.NodeID
		}
	}

	// If not found in our members, check if it's in the membership
	for nodeID := range membership.Nodes {
		for _, info := range d.members {
			if hashNodeID(info.NodeID) == nodeID {
				if nodeID == leaderID {
					return info.NodeID
				}
			}
		}
	}

	return ""
}

// onNodeJoin is called when a new node joins the cluster.
func (d *Discovery) onNodeJoin(nodeID string, meta []byte) {
	var nodeMeta NodeMeta

	err := json.Unmarshal(meta, &nodeMeta)
	if err != nil {
		log.Error().Err(err).Str("node_id", nodeID).Msg("Failed to unmarshal node metadata")
		return
	}

	log.Info().
		Str("node_id", nodeID).
		Str("raft_addr", nodeMeta.RaftAddr).
		Str("role", nodeMeta.Role).
		Msg("Node joined cluster")

	d.mu.Lock()
	d.members[nodeID] = &NodeInfo{
		NodeID:    nodeID,
		RaftAddr:  nodeMeta.RaftAddr,
		S3Addr:    nodeMeta.S3Addr,
		AdminAddr: nodeMeta.AdminAddr,
		Role:      nodeMeta.Role,
		Version:   nodeMeta.Version,
		Status:    "alive",
		JoinedAt:  time.Now(),
		LastSeen:  time.Now(),
	}
	d.mu.Unlock()

	// If we're the leader, add the new node to Dragonboat cluster
	if d.IsLeader() {
		d.addRaftVoter(nodeID, nodeMeta.RaftAddr)
	}
}

// onNodeLeave is called when a node leaves the cluster.
func (d *Discovery) onNodeLeave(nodeID string) {
	log.Info().Str("node_id", nodeID).Msg("Node left cluster")

	d.mu.Lock()

	if info, ok := d.members[nodeID]; ok {
		info.Status = "dead"
	}

	d.mu.Unlock()

	// If we're the leader, remove the node from Dragonboat cluster
	if d.IsLeader() {
		d.removeRaftVoter(nodeID)
	}
}

// onNodeUpdate is called when a node's metadata is updated.
func (d *Discovery) onNodeUpdate(nodeID string, meta []byte) {
	var nodeMeta NodeMeta

	err := json.Unmarshal(meta, &nodeMeta)
	if err != nil {
		log.Error().Err(err).Str("node_id", nodeID).Msg("Failed to unmarshal node metadata")
		return
	}

	d.mu.Lock()

	if info, ok := d.members[nodeID]; ok {
		info.RaftAddr = nodeMeta.RaftAddr
		info.S3Addr = nodeMeta.S3Addr
		info.AdminAddr = nodeMeta.AdminAddr
		info.Role = nodeMeta.Role
		info.Version = nodeMeta.Version
		info.LastSeen = time.Now()
	}

	d.mu.Unlock()
}

// addRaftVoter adds a node as a Dragonboat replica.
func (d *Discovery) addRaftVoter(nodeID, raftAddr string) {
	d.mu.RLock()
	nodeHost := d.nodeHost
	shardID := d.shardID
	d.mu.RUnlock()

	if nodeHost == nil {
		return
	}

	log.Info().
		Str("node_id", nodeID).
		Str("raft_addr", raftAddr).
		Msg("Adding node as Dragonboat replica")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get current membership to obtain config change index
	membership, err := nodeHost.SyncGetShardMembership(ctx, shardID)
	if err != nil {
		log.Error().Err(err).
			Str("node_id", nodeID).
			Msg("Failed to get shard membership")

		return
	}

	// Add the replica
	replicaID := hashNodeID(nodeID)

	addErr := nodeHost.SyncRequestAddReplica(ctx, shardID, replicaID, raftAddr, membership.ConfigChangeID)
	if addErr != nil {
		log.Error().Err(addErr).
			Str("node_id", nodeID).
			Uint64("replica_id", replicaID).
			Msg("Failed to add Dragonboat replica")
	}
}

// removeRaftVoter removes a node from the Dragonboat cluster.
func (d *Discovery) removeRaftVoter(nodeID string) {
	d.mu.RLock()
	nodeHost := d.nodeHost
	shardID := d.shardID
	d.mu.RUnlock()

	if nodeHost == nil {
		return
	}

	log.Info().Str("node_id", nodeID).Msg("Removing node from Dragonboat cluster")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get current membership to obtain config change index
	membership, err := nodeHost.SyncGetShardMembership(ctx, shardID)
	if err != nil {
		log.Error().Err(err).
			Str("node_id", nodeID).
			Msg("Failed to get shard membership")

		return
	}

	// Remove the replica
	replicaID := hashNodeID(nodeID)

	deleteErr := nodeHost.SyncRequestDeleteReplica(ctx, shardID, replicaID, membership.ConfigChangeID)
	if deleteErr != nil {
		log.Error().Err(deleteErr).
			Str("node_id", nodeID).
			Uint64("replica_id", replicaID).
			Msg("Failed to remove Dragonboat replica")
	}
}

// AddVoter manually adds a Dragonboat replica (for API use).
func (d *Discovery) AddVoter(nodeID, raftAddr string) error {
	d.mu.RLock()
	nodeHost := d.nodeHost
	shardID := d.shardID
	d.mu.RUnlock()

	if nodeHost == nil {
		return errors.New("nodeHost not initialized")
	}

	if !d.IsLeader() {
		return errors.New("not leader")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get current membership to obtain config change index
	membership, err := nodeHost.SyncGetShardMembership(ctx, shardID)
	if err != nil {
		return fmt.Errorf("failed to get shard membership: %w", err)
	}

	// Add the replica
	replicaID := hashNodeID(nodeID)

	addErr := nodeHost.SyncRequestAddReplica(ctx, shardID, replicaID, raftAddr, membership.ConfigChangeID)
	if addErr != nil {
		return fmt.Errorf("failed to add replica: %w", addErr)
	}

	return nil
}

// RemoveVoter manually removes a Dragonboat replica (for API use).
func (d *Discovery) RemoveVoter(nodeID string) error {
	d.mu.RLock()
	nodeHost := d.nodeHost
	shardID := d.shardID
	d.mu.RUnlock()

	if nodeHost == nil {
		return errors.New("nodeHost not initialized")
	}

	if !d.IsLeader() {
		return errors.New("not leader")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get current membership to obtain config change index
	membership, err := nodeHost.SyncGetShardMembership(ctx, shardID)
	if err != nil {
		return fmt.Errorf("failed to get shard membership: %w", err)
	}

	// Remove the replica
	replicaID := hashNodeID(nodeID)
	err = nodeHost.SyncRequestDeleteReplica(ctx, shardID, replicaID, membership.ConfigChangeID)
	if err != nil {
		return fmt.Errorf("failed to remove replica: %w", err)
	}

	return nil
}

// TransferLeadership transfers leadership to another node.
func (d *Discovery) TransferLeadership(targetID string) error {
	d.mu.RLock()
	nodeHost := d.nodeHost
	shardID := d.shardID
	d.mu.RUnlock()

	if nodeHost == nil {
		return errors.New("nodeHost not initialized")
	}

	if !d.IsLeader() {
		return errors.New("not leader")
	}

	// Get target replica ID
	targetReplicaID := hashNodeID(targetID)

	// Request leadership transfer
	err := nodeHost.RequestLeaderTransfer(shardID, targetReplicaID)
	if err != nil {
		return fmt.Errorf("failed to transfer leadership: %w", err)
	}

	return nil
}

// GetRaftStats returns Dragonboat cluster statistics.
func (d *Discovery) GetRaftStats() map[string]string {
	d.mu.RLock()
	nodeHost := d.nodeHost
	shardID := d.shardID
	d.mu.RUnlock()

	if nodeHost == nil {
		return nil
	}

	stats := make(map[string]string)
	info := nodeHost.GetNodeHostInfo(dragonboat.DefaultNodeHostInfoOption)

	// Find our shard info
	for _, ci := range info.ShardInfoList {
		if ci.ShardID == shardID {
			stats["shard_id"] = strconv.FormatUint(ci.ShardID, 10)
			stats["replica_id"] = strconv.FormatUint(ci.ReplicaID, 10)
			stats["is_leader"] = strconv.FormatBool(ci.IsLeader)
			stats["state_machine_type"] = fmt.Sprintf("%d", ci.StateMachineType)
			stats["pending"] = strconv.FormatBool(ci.Pending)

			break
		}
	}

	return stats
}

// ShardMembershipInfo represents the shard membership configuration.
type ShardMembershipInfo struct {
	Nodes          map[uint64]string
	Witnesses      map[uint64]string
	Removed        map[uint64]struct{}
	ConfigChangeID uint64
}

// GetRaftConfiguration returns the current Dragonboat shard membership.
func (d *Discovery) GetRaftConfiguration() (*ShardMembershipInfo, error) {
	d.mu.RLock()
	nodeHost := d.nodeHost
	shardID := d.shardID
	d.mu.RUnlock()

	if nodeHost == nil {
		return nil, errors.New("nodeHost not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	membership, err := nodeHost.SyncGetShardMembership(ctx, shardID)
	if err != nil {
		return nil, fmt.Errorf("failed to get shard membership: %w", err)
	}

	return &ShardMembershipInfo{
		ConfigChangeID: membership.ConfigChangeID,
		Nodes:          membership.Nodes,
		Witnesses:      membership.Witnesses,
		Removed:        membership.Removed,
	}, nil
}

// getOutboundIP gets the preferred outbound IP of this machine.
func getOutboundIP(ctx context.Context) string {
	dialCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	var d net.Dialer

	conn, err := d.DialContext(dialCtx, "udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}

	defer func() { _ = conn.Close() }()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP.String()
}

// hashNodeID converts a string node ID to a uint64 replica ID for Dragonboat.
func hashNodeID(nodeID string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(nodeID))

	return h.Sum64()
}
