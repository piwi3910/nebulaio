package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"github.com/rs/zerolog/log"
)

// DiscoveryConfig holds configuration for cluster discovery
type DiscoveryConfig struct {
	NodeID        string
	AdvertiseAddr string
	JoinAddresses []string
	GossipPort    int
	RaftPort      int
	S3Port        int
	AdminPort     int
	Role          string // "gateway" or "storage"
	Version       string
}

// Discovery manages node discovery and cluster membership
type Discovery struct {
	config     DiscoveryConfig
	members    map[string]*NodeInfo
	mu         sync.RWMutex
	raft       *raft.Raft
	membership *Membership

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NodeInfo represents information about a cluster node
type NodeInfo struct {
	NodeID     string    `json:"node_id"`
	RaftAddr   string    `json:"raft_addr"`
	S3Addr     string    `json:"s3_addr"`
	AdminAddr  string    `json:"admin_addr"`
	GossipAddr string    `json:"gossip_addr"`
	Role       string    `json:"role"` // gateway, storage
	Version    string    `json:"version"`
	Status     string    `json:"status"` // alive, dead, leaving
	JoinedAt   time.Time `json:"joined_at"`
	LastSeen   time.Time `json:"last_seen"`
}

// NodeMeta is the metadata broadcast via gossip
type NodeMeta struct {
	NodeID    string `json:"node_id"`
	RaftAddr  string `json:"raft_addr"`
	S3Addr    string `json:"s3_addr"`
	AdminAddr string `json:"admin_addr"`
	Role      string `json:"role"`
	Version   string `json:"version"`
}

// NewDiscovery creates a new Discovery instance
func NewDiscovery(config DiscoveryConfig) *Discovery {
	ctx, cancel := context.WithCancel(context.Background())
	return &Discovery{
		config:  config,
		members: make(map[string]*NodeInfo),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// SetRaft sets the Raft instance for the discovery
func (d *Discovery) SetRaft(r *raft.Raft) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.raft = r
}

// Start starts the discovery service
func (d *Discovery) Start(ctx context.Context) error {
	log.Info().
		Str("node_id", d.config.NodeID).
		Str("advertise_addr", d.config.AdvertiseAddr).
		Int("gossip_port", d.config.GossipPort).
		Msg("Starting cluster discovery")

	// Build node metadata
	host := d.config.AdvertiseAddr
	if host == "" {
		host = getOutboundIP()
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
		if err := d.Join(d.config.JoinAddresses[0]); err != nil {
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

// retryJoin retries joining the cluster
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
				if err := d.Join(addr); err != nil {
					log.Debug().Err(err).Str("addr", addr).Msg("Retry join failed")
					continue
				}
				log.Info().Str("addr", addr).Msg("Successfully joined cluster")
				return
			}
		}
	}
}

// healthCheckLoop periodically updates member status
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

// updateMemberStatus updates the status of all members
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

// Stop stops the discovery service
func (d *Discovery) Stop() error {
	log.Info().Msg("Stopping cluster discovery")

	d.cancel()

	if d.membership != nil {
		if err := d.membership.Leave(5 * time.Second); err != nil {
			log.Error().Err(err).Msg("Error leaving cluster")
		}
	}

	d.wg.Wait()
	return nil
}

// Join joins an existing cluster via the given address
func (d *Discovery) Join(addr string) error {
	if d.membership == nil {
		return fmt.Errorf("membership not initialized")
	}

	log.Info().Str("addr", addr).Msg("Joining cluster")

	if err := d.membership.Join([]string{addr}); err != nil {
		return fmt.Errorf("failed to join cluster at %s: %w", addr, err)
	}

	return nil
}

// Leave gracefully leaves the cluster
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

// Members returns all known cluster members
func (d *Discovery) Members() []*NodeInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	members := make([]*NodeInfo, 0, len(d.members))
	for _, m := range d.members {
		members = append(members, m)
	}
	return members
}

// GetMember returns information about a specific member
func (d *Discovery) GetMember(nodeID string) (*NodeInfo, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	info, ok := d.members[nodeID]
	return info, ok
}

// LocalNode returns the local node information
func (d *Discovery) LocalNode() *NodeInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.members[d.config.NodeID]
}

// IsLeader returns true if the local node is the Raft leader
func (d *Discovery) IsLeader() bool {
	if d.raft == nil {
		return false
	}
	return d.raft.State() == raft.Leader
}

// LeaderID returns the ID of the current leader
func (d *Discovery) LeaderID() string {
	if d.raft == nil {
		return ""
	}

	leaderAddr := string(d.raft.Leader())
	if leaderAddr == "" {
		return ""
	}

	// Find node by raft address
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, info := range d.members {
		if info.RaftAddr == leaderAddr {
			return info.NodeID
		}
	}

	return ""
}

// onNodeJoin is called when a new node joins the cluster
func (d *Discovery) onNodeJoin(nodeID string, meta []byte) {
	var nodeMeta NodeMeta
	if err := json.Unmarshal(meta, &nodeMeta); err != nil {
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

	// If we're the leader, add the new node to Raft cluster
	if d.raft != nil && d.raft.State() == raft.Leader {
		d.addRaftVoter(nodeID, nodeMeta.RaftAddr)
	}
}

// onNodeLeave is called when a node leaves the cluster
func (d *Discovery) onNodeLeave(nodeID string) {
	log.Info().Str("node_id", nodeID).Msg("Node left cluster")

	d.mu.Lock()
	if info, ok := d.members[nodeID]; ok {
		info.Status = "dead"
	}
	d.mu.Unlock()

	// If we're the leader, remove the node from Raft cluster
	if d.raft != nil && d.raft.State() == raft.Leader {
		d.removeRaftVoter(nodeID)
	}
}

// onNodeUpdate is called when a node's metadata is updated
func (d *Discovery) onNodeUpdate(nodeID string, meta []byte) {
	var nodeMeta NodeMeta
	if err := json.Unmarshal(meta, &nodeMeta); err != nil {
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

// addRaftVoter adds a node as a Raft voter
func (d *Discovery) addRaftVoter(nodeID, raftAddr string) {
	if d.raft == nil {
		return
	}

	log.Info().
		Str("node_id", nodeID).
		Str("raft_addr", raftAddr).
		Msg("Adding node as Raft voter")

	future := d.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(raftAddr), 0, 10*time.Second)
	if err := future.Error(); err != nil {
		log.Error().Err(err).
			Str("node_id", nodeID).
			Msg("Failed to add Raft voter")
	}
}

// removeRaftVoter removes a node from the Raft cluster
func (d *Discovery) removeRaftVoter(nodeID string) {
	if d.raft == nil {
		return
	}

	log.Info().Str("node_id", nodeID).Msg("Removing node from Raft cluster")

	future := d.raft.RemoveServer(raft.ServerID(nodeID), 0, 10*time.Second)
	if err := future.Error(); err != nil {
		log.Error().Err(err).
			Str("node_id", nodeID).
			Msg("Failed to remove Raft voter")
	}
}

// AddVoter manually adds a Raft voter (for API use)
func (d *Discovery) AddVoter(nodeID, raftAddr string) error {
	if d.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	if d.raft.State() != raft.Leader {
		return fmt.Errorf("not leader")
	}

	future := d.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(raftAddr), 0, 10*time.Second)
	return future.Error()
}

// RemoveVoter manually removes a Raft voter (for API use)
func (d *Discovery) RemoveVoter(nodeID string) error {
	if d.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	if d.raft.State() != raft.Leader {
		return fmt.Errorf("not leader")
	}

	future := d.raft.RemoveServer(raft.ServerID(nodeID), 0, 10*time.Second)
	return future.Error()
}

// TransferLeadership transfers leadership to another node
func (d *Discovery) TransferLeadership(targetID string) error {
	if d.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	if d.raft.State() != raft.Leader {
		return fmt.Errorf("not leader")
	}

	d.mu.RLock()
	targetNode, ok := d.members[targetID]
	d.mu.RUnlock()

	if !ok {
		return fmt.Errorf("target node not found: %s", targetID)
	}

	future := d.raft.LeadershipTransferToServer(
		raft.ServerID(targetID),
		raft.ServerAddress(targetNode.RaftAddr),
	)
	return future.Error()
}

// GetRaftStats returns Raft cluster statistics
func (d *Discovery) GetRaftStats() map[string]string {
	if d.raft == nil {
		return nil
	}
	return d.raft.Stats()
}

// GetRaftConfiguration returns the current Raft configuration
func (d *Discovery) GetRaftConfiguration() (raft.Configuration, error) {
	if d.raft == nil {
		return raft.Configuration{}, fmt.Errorf("raft not initialized")
	}

	future := d.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		return raft.Configuration{}, err
	}
	return future.Configuration(), nil
}

// getOutboundIP gets the preferred outbound IP of this machine
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
