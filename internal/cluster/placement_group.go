package cluster

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// PlacementGroupID uniquely identifies a placement group
type PlacementGroupID string

// PlacementGroup represents a group of nodes that share local storage operations.
// Erasure coding and tiering happen within a placement group.
// Cross-placement group operations are for disaster recovery (full object copies).
type PlacementGroup struct {
	ID          PlacementGroupID `json:"id"`
	Name        string           `json:"name"`
	Datacenter  string           `json:"datacenter"`
	Region      string           `json:"region"`
	Nodes       []string         `json:"nodes"` // Node IDs in this group
	MinNodes    int              `json:"min_nodes"`
	MaxNodes    int              `json:"max_nodes"`
	IsLocal     bool             `json:"is_local"` // True if this node belongs to this group
	Status      PlacementGroupStatus `json:"status"`
}

// PlacementGroupStatus represents the health status of a placement group
type PlacementGroupStatus string

const (
	PlacementGroupStatusHealthy   PlacementGroupStatus = "healthy"
	PlacementGroupStatusDegraded  PlacementGroupStatus = "degraded"
	PlacementGroupStatusOffline   PlacementGroupStatus = "offline"
	PlacementGroupStatusUnknown   PlacementGroupStatus = "unknown"
)

// PlacementGroupConfig holds configuration for placement group management
type PlacementGroupConfig struct {
	// LocalGroupID is the placement group this node belongs to
	LocalGroupID PlacementGroupID `json:"local_group_id"`
	// Groups is the list of all known placement groups
	Groups []PlacementGroup `json:"groups"`
	// MinNodesForErasure is the minimum nodes needed for erasure coding
	MinNodesForErasure int `json:"min_nodes_for_erasure"`
	// ReplicationTargets are placement groups to replicate to for DR
	ReplicationTargets []PlacementGroupID `json:"replication_targets"`
}

// PlacementGroupManager manages placement groups and node assignments
type PlacementGroupManager struct {
	config      PlacementGroupConfig
	localGroup  *PlacementGroup
	groups      map[PlacementGroupID]*PlacementGroup
	nodeToGroup map[string]PlacementGroupID
	mu          sync.RWMutex

	// Callbacks for group events
	onNodeJoinedGroup  func(groupID PlacementGroupID, nodeID string)
	onNodeLeftGroup    func(groupID PlacementGroupID, nodeID string)
	onGroupStatusChange func(groupID PlacementGroupID, status PlacementGroupStatus)
}

// NewPlacementGroupManager creates a new placement group manager
func NewPlacementGroupManager(config PlacementGroupConfig) (*PlacementGroupManager, error) {
	mgr := &PlacementGroupManager{
		config:      config,
		groups:      make(map[PlacementGroupID]*PlacementGroup),
		nodeToGroup: make(map[string]PlacementGroupID),
	}

	// Initialize groups from config
	for i := range config.Groups {
		group := &config.Groups[i]
		mgr.groups[group.ID] = group

		// Map nodes to groups
		for _, nodeID := range group.Nodes {
			mgr.nodeToGroup[nodeID] = group.ID
		}

		// Track local group
		if group.ID == config.LocalGroupID {
			mgr.localGroup = group
			group.IsLocal = true
		}
	}

	if mgr.localGroup == nil && config.LocalGroupID != "" {
		return nil, fmt.Errorf("local placement group %s not found in configuration", config.LocalGroupID)
	}

	log.Info().
		Str("local_group", string(config.LocalGroupID)).
		Int("total_groups", len(config.Groups)).
		Msg("Placement group manager initialized")

	return mgr, nil
}

// LocalGroup returns the placement group this node belongs to
func (m *PlacementGroupManager) LocalGroup() *PlacementGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.localGroup
}

// GetGroup returns a placement group by ID
func (m *PlacementGroupManager) GetGroup(id PlacementGroupID) (*PlacementGroup, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	group, ok := m.groups[id]
	return group, ok
}

// GetNodeGroup returns the placement group a node belongs to
func (m *PlacementGroupManager) GetNodeGroup(nodeID string) (*PlacementGroup, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	groupID, ok := m.nodeToGroup[nodeID]
	if !ok {
		return nil, false
	}

	group, ok := m.groups[groupID]
	return group, ok
}

// AllGroups returns all known placement groups
func (m *PlacementGroupManager) AllGroups() []*PlacementGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	groups := make([]*PlacementGroup, 0, len(m.groups))
	for _, g := range m.groups {
		groups = append(groups, g)
	}
	return groups
}

// LocalGroupNodes returns all nodes in the local placement group
func (m *PlacementGroupManager) LocalGroupNodes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.localGroup == nil {
		return nil
	}
	return m.localGroup.Nodes
}

// IsLocalGroupNode checks if a node is in the local placement group
func (m *PlacementGroupManager) IsLocalGroupNode(nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	groupID, ok := m.nodeToGroup[nodeID]
	if !ok {
		return false
	}

	return groupID == m.config.LocalGroupID
}

// ReplicationTargets returns placement groups configured for DR replication
func (m *PlacementGroupManager) ReplicationTargets() []*PlacementGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	targets := make([]*PlacementGroup, 0, len(m.config.ReplicationTargets))
	for _, targetID := range m.config.ReplicationTargets {
		if group, ok := m.groups[targetID]; ok {
			targets = append(targets, group)
		}
	}
	return targets
}

// AddNodeToGroup adds a node to a placement group
func (m *PlacementGroupManager) AddNodeToGroup(groupID PlacementGroupID, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	group, ok := m.groups[groupID]
	if !ok {
		return fmt.Errorf("placement group %s not found", groupID)
	}

	// Check if already in this group
	for _, n := range group.Nodes {
		if n == nodeID {
			return nil // Already a member
		}
	}

	// Check max nodes
	if group.MaxNodes > 0 && len(group.Nodes) >= group.MaxNodes {
		return fmt.Errorf("placement group %s is at maximum capacity (%d nodes)", groupID, group.MaxNodes)
	}

	// Add node
	group.Nodes = append(group.Nodes, nodeID)
	m.nodeToGroup[nodeID] = groupID

	log.Info().
		Str("group_id", string(groupID)).
		Str("node_id", nodeID).
		Int("group_size", len(group.Nodes)).
		Msg("Node added to placement group")

	if m.onNodeJoinedGroup != nil {
		go m.onNodeJoinedGroup(groupID, nodeID)
	}

	return nil
}

// RemoveNodeFromGroup removes a node from a placement group
func (m *PlacementGroupManager) RemoveNodeFromGroup(groupID PlacementGroupID, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	group, ok := m.groups[groupID]
	if !ok {
		return fmt.Errorf("placement group %s not found", groupID)
	}

	// Find and remove node
	found := false
	for i, n := range group.Nodes {
		if n == nodeID {
			group.Nodes = append(group.Nodes[:i], group.Nodes[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("node %s not found in placement group %s", nodeID, groupID)
	}

	delete(m.nodeToGroup, nodeID)

	// Update group status if below minimum
	if group.MinNodes > 0 && len(group.Nodes) < group.MinNodes {
		oldStatus := group.Status
		group.Status = PlacementGroupStatusDegraded
		if oldStatus != group.Status && m.onGroupStatusChange != nil {
			go m.onGroupStatusChange(groupID, group.Status)
		}
	}

	log.Info().
		Str("group_id", string(groupID)).
		Str("node_id", nodeID).
		Int("group_size", len(group.Nodes)).
		Msg("Node removed from placement group")

	if m.onNodeLeftGroup != nil {
		go m.onNodeLeftGroup(groupID, nodeID)
	}

	return nil
}

// UpdateGroupStatus updates the status of a placement group
func (m *PlacementGroupManager) UpdateGroupStatus(groupID PlacementGroupID, status PlacementGroupStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	group, ok := m.groups[groupID]
	if !ok {
		return fmt.Errorf("placement group %s not found", groupID)
	}

	oldStatus := group.Status
	group.Status = status

	if oldStatus != status {
		log.Info().
			Str("group_id", string(groupID)).
			Str("old_status", string(oldStatus)).
			Str("new_status", string(status)).
			Msg("Placement group status changed")

		if m.onGroupStatusChange != nil {
			go m.onGroupStatusChange(groupID, status)
		}
	}

	return nil
}

// CanPerformErasureCoding checks if the local group has enough nodes for erasure coding
func (m *PlacementGroupManager) CanPerformErasureCoding(dataShards, parityShards int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.localGroup == nil {
		// Single node mode - can only do local erasure coding
		return true
	}

	totalShards := dataShards + parityShards
	return len(m.localGroup.Nodes) >= totalShards
}

// GetShardPlacementNodes returns nodes for distributing erasure shards
// Returns nodes from the local placement group only
func (m *PlacementGroupManager) GetShardPlacementNodes(numShards int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.localGroup == nil {
		// Single node mode
		return []string{}, nil
	}

	if len(m.localGroup.Nodes) < numShards {
		return nil, fmt.Errorf("not enough nodes in placement group: need %d, have %d",
			numShards, len(m.localGroup.Nodes))
	}

	// Return nodes for shard distribution
	nodes := make([]string, numShards)
	copy(nodes, m.localGroup.Nodes[:numShards])
	return nodes, nil
}

// SetOnNodeJoinedGroup sets the callback for when a node joins a group
func (m *PlacementGroupManager) SetOnNodeJoinedGroup(fn func(PlacementGroupID, string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onNodeJoinedGroup = fn
}

// SetOnNodeLeftGroup sets the callback for when a node leaves a group
func (m *PlacementGroupManager) SetOnNodeLeftGroup(fn func(PlacementGroupID, string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onNodeLeftGroup = fn
}

// SetOnGroupStatusChange sets the callback for when a group's status changes
func (m *PlacementGroupManager) SetOnGroupStatusChange(fn func(PlacementGroupID, PlacementGroupStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onGroupStatusChange = fn
}

// MarshalJSON implements json.Marshaler
func (m *PlacementGroupManager) MarshalJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return json.Marshal(struct {
		LocalGroupID string             `json:"local_group_id"`
		Groups       []*PlacementGroup  `json:"groups"`
	}{
		LocalGroupID: string(m.config.LocalGroupID),
		Groups:       m.AllGroups(),
	})
}

// PlacementGroupNodeMeta is metadata about a node's placement group membership
// This is embedded in the cluster membership node metadata
type PlacementGroupNodeMeta struct {
	PlacementGroupID PlacementGroupID `json:"placement_group_id"`
	Datacenter       string           `json:"datacenter"`
	Region           string           `json:"region"`
}

// EncodeNodeMeta encodes placement group node metadata for cluster membership
func EncodeNodeMeta(meta PlacementGroupNodeMeta) ([]byte, error) {
	return json.Marshal(meta)
}

// DecodeNodeMeta decodes placement group node metadata from cluster membership
func DecodeNodeMeta(data []byte) (PlacementGroupNodeMeta, error) {
	var meta PlacementGroupNodeMeta
	err := json.Unmarshal(data, &meta)
	return meta, err
}
