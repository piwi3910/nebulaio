package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/metrics"
	"github.com/rs/zerolog/log"
)

// callbackTimeout is the maximum time allowed for a callback to complete
const callbackTimeout = 5 * time.Second

// PlacementGroupAuditLogger is an interface for audit logging to avoid circular imports
type PlacementGroupAuditLogger interface {
	LogNodeJoined(groupID, nodeID, datacenter, region string)
	LogNodeLeft(groupID, nodeID, reason string)
	LogStatusChanged(groupID, oldStatus, newStatus string)
}

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

	// Context for cancelling pending callbacks during shutdown
	ctx    context.Context
	cancel context.CancelFunc

	// Cached values for frequently accessed read operations
	// These are invalidated when the corresponding data changes
	cachedLocalNodes     []string
	cachedLocalNodesHash uint64 // Hash of nodes to detect changes
	cacheGeneration      uint64 // Incremented on any mutation

	// Callbacks for group events
	onNodeJoinedGroup   func(groupID PlacementGroupID, nodeID string)
	onNodeLeftGroup     func(groupID PlacementGroupID, nodeID string)
	onGroupStatusChange func(groupID PlacementGroupID, status PlacementGroupStatus)

	// Optional audit logger for membership changes
	auditLogger PlacementGroupAuditLogger
}

// NewPlacementGroupManager creates a new placement group manager
func NewPlacementGroupManager(config PlacementGroupConfig) (*PlacementGroupManager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	mgr := &PlacementGroupManager{
		config:      config,
		groups:      make(map[PlacementGroupID]*PlacementGroup),
		nodeToGroup: make(map[string]PlacementGroupID),
		ctx:         ctx,
		cancel:      cancel,
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

		// Initialize status based on node count
		// This ensures groups have a valid status from creation
		if group.Status == "" {
			if len(group.Nodes) == 0 {
				group.Status = PlacementGroupStatusOffline
			} else if group.MinNodes > 0 && len(group.Nodes) < group.MinNodes {
				group.Status = PlacementGroupStatusDegraded
			} else {
				group.Status = PlacementGroupStatusHealthy
			}
		}
	}

	if mgr.localGroup == nil && config.LocalGroupID != "" {
		return nil, fmt.Errorf("local placement group %s not found in configuration", config.LocalGroupID)
	}

	// Initialize metrics for all placement groups
	for _, group := range mgr.groups {
		metrics.SetPlacementGroupNodes(string(group.ID), group.Datacenter, group.Region, len(group.Nodes))
		metrics.SetPlacementGroupStatusMetric(string(group.ID), string(group.Status))
		metrics.SetPlacementGroupInfo(string(group.ID), group.Name, group.Datacenter, group.Region, group.IsLocal)
	}

	log.Info().
		Str("local_group", string(config.LocalGroupID)).
		Int("total_groups", len(config.Groups)).
		Msg("Placement group manager initialized")

	return mgr, nil
}

// LocalGroup returns a copy of the placement group this node belongs to.
// Returns nil if no local group is configured.
func (m *PlacementGroupManager) LocalGroup() *PlacementGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.localGroup == nil {
		return nil
	}
	// Return a copy to prevent external mutation
	return m.copyGroup(m.localGroup)
}

// GetGroup returns a copy of the placement group by ID.
// Returns (nil, false) if the group ID is not found.
func (m *PlacementGroupManager) GetGroup(id PlacementGroupID) (*PlacementGroup, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	group, ok := m.groups[id]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent external mutation
	return m.copyGroup(group), true
}

// GetNodeGroup returns a copy of the placement group a node belongs to.
// Returns (nil, false) if the node is not assigned to any group.
func (m *PlacementGroupManager) GetNodeGroup(nodeID string) (*PlacementGroup, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	groupID, ok := m.nodeToGroup[nodeID]
	if !ok {
		return nil, false
	}

	group, ok := m.groups[groupID]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent external mutation
	return m.copyGroup(group), true
}

// AllGroups returns copies of all known placement groups
func (m *PlacementGroupManager) AllGroups() []*PlacementGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	groups := make([]*PlacementGroup, 0, len(m.groups))
	for _, g := range m.groups {
		// Return copies to prevent external mutation
		groups = append(groups, m.copyGroup(g))
	}
	return groups
}

// LocalGroupNodes returns a copy of all nodes in the local placement group.
// Returns nil if no local group is configured.
// This method uses caching to avoid repeated allocations for frequent calls.
func (m *PlacementGroupManager) LocalGroupNodes() []string {
	m.mu.RLock()

	if m.localGroup == nil {
		m.mu.RUnlock()
		return nil
	}

	// Check if cache is valid
	if m.cachedLocalNodes != nil {
		// Return a copy of the cached slice
		nodes := make([]string, len(m.cachedLocalNodes))
		copy(nodes, m.cachedLocalNodes)
		m.mu.RUnlock()
		return nodes
	}

	// Cache miss - need to update (upgrade to write lock)
	m.mu.RUnlock()
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if m.localGroup == nil {
		return nil
	}

	// Update cache
	m.updateLocalNodesCache()

	// Return a copy
	nodes := make([]string, len(m.cachedLocalNodes))
	copy(nodes, m.cachedLocalNodes)
	return nodes
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

// ReplicationTargets returns copies of placement groups configured for DR replication
func (m *PlacementGroupManager) ReplicationTargets() []*PlacementGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	targets := make([]*PlacementGroup, 0, len(m.config.ReplicationTargets))
	for _, targetID := range m.config.ReplicationTargets {
		if group, ok := m.groups[targetID]; ok {
			// Return copies to prevent external mutation
			targets = append(targets, m.copyGroup(group))
		}
	}
	return targets
}

// AddNodeToGroup adds a node to a placement group
func (m *PlacementGroupManager) AddNodeToGroup(groupID PlacementGroupID, nodeID string) error {
	m.mu.Lock()

	group, ok := m.groups[groupID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("placement group %s not found", groupID)
	}

	// Check if already in this group
	for _, n := range group.Nodes {
		if n == nodeID {
			m.mu.Unlock()
			return nil // Already a member
		}
	}

	// Check max nodes
	if group.MaxNodes > 0 && len(group.Nodes) >= group.MaxNodes {
		m.mu.Unlock()
		return fmt.Errorf("placement group %s is at maximum capacity (%d nodes)", groupID, group.MaxNodes)
	}

	// Add node
	group.Nodes = append(group.Nodes, nodeID)
	m.nodeToGroup[nodeID] = groupID
	groupSize := len(group.Nodes)

	// Invalidate cache since membership changed
	m.invalidateCache()

	// Check if group should transition to healthy status
	// If we now have enough nodes for minimum requirements, update status
	var statusCallback func(PlacementGroupID, PlacementGroupStatus)
	var newStatus PlacementGroupStatus
	oldStatus := group.Status
	if group.Status == PlacementGroupStatusDegraded && groupSize >= group.MinNodes {
		group.Status = PlacementGroupStatusHealthy
		newStatus = PlacementGroupStatusHealthy
		statusCallback = m.onGroupStatusChange
	}

	// Capture callback and audit logger under lock to prevent data race
	callback := m.onNodeJoinedGroup
	auditLogger := m.auditLogger
	datacenter := group.Datacenter
	region := group.Region
	m.mu.Unlock()

	log.Info().
		Str("group_id", string(groupID)).
		Str("node_id", nodeID).
		Int("group_size", groupSize).
		Msg("Node added to placement group")

	// Update metrics for node count
	metrics.SetPlacementGroupNodes(string(groupID), datacenter, region, groupSize)

	// Audit log the membership change
	if auditLogger != nil {
		auditLogger.LogNodeJoined(string(groupID), nodeID, datacenter, region)
	}

	// Notify about status change if it occurred
	if statusCallback != nil && newStatus != "" {
		log.Info().
			Str("group_id", string(groupID)).
			Str("old_status", string(oldStatus)).
			Str("new_status", string(newStatus)).
			Msg("Placement group status changed")
		// Update status metrics
		metrics.SetPlacementGroupStatusMetric(string(groupID), string(newStatus))
		// Audit log the status change
		if auditLogger != nil {
			auditLogger.LogStatusChanged(string(groupID), string(oldStatus), string(newStatus))
		}
		go m.safeCallback(func() { statusCallback(groupID, newStatus) })
	}

	if callback != nil {
		go m.safeCallback(func() { callback(groupID, nodeID) })
	}

	return nil
}

// RemoveNodeFromGroup removes a node from a placement group
func (m *PlacementGroupManager) RemoveNodeFromGroup(groupID PlacementGroupID, nodeID string) error {
	m.mu.Lock()

	group, ok := m.groups[groupID]
	if !ok {
		m.mu.Unlock()
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
		m.mu.Unlock()
		return fmt.Errorf("node %s not found in placement group %s", nodeID, groupID)
	}

	delete(m.nodeToGroup, nodeID)

	// Invalidate cache since membership changed
	m.invalidateCache()

	// Update group status if below minimum
	var statusCallback func(PlacementGroupID, PlacementGroupStatus)
	var newStatus PlacementGroupStatus
	var oldStatus PlacementGroupStatus
	if group.MinNodes > 0 && len(group.Nodes) < group.MinNodes {
		oldStatus = group.Status
		group.Status = PlacementGroupStatusDegraded
		newStatus = group.Status
		if oldStatus != group.Status {
			statusCallback = m.onGroupStatusChange
		}
	}

	groupSize := len(group.Nodes)
	datacenter := group.Datacenter
	region := group.Region
	// Capture callback and audit logger under lock to prevent data race
	nodeLeftCallback := m.onNodeLeftGroup
	auditLogger := m.auditLogger
	m.mu.Unlock()

	log.Info().
		Str("group_id", string(groupID)).
		Str("node_id", nodeID).
		Int("group_size", groupSize).
		Msg("Node removed from placement group")

	// Update metrics for node count
	metrics.SetPlacementGroupNodes(string(groupID), datacenter, region, groupSize)

	// Audit log the membership change
	if auditLogger != nil {
		auditLogger.LogNodeLeft(string(groupID), nodeID, "")
	}

	if statusCallback != nil {
		// Update status metrics
		metrics.SetPlacementGroupStatusMetric(string(groupID), string(newStatus))
		// Audit log the status change
		if auditLogger != nil {
			auditLogger.LogStatusChanged(string(groupID), string(oldStatus), string(newStatus))
		}
		go m.safeCallback(func() { statusCallback(groupID, newStatus) })
	}

	if nodeLeftCallback != nil {
		go m.safeCallback(func() { nodeLeftCallback(groupID, nodeID) })
	}

	return nil
}

// UpdateGroupStatus updates the status of a placement group
func (m *PlacementGroupManager) UpdateGroupStatus(groupID PlacementGroupID, status PlacementGroupStatus) error {
	m.mu.Lock()

	group, ok := m.groups[groupID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("placement group %s not found", groupID)
	}

	oldStatus := group.Status
	group.Status = status

	// Capture callback and audit logger under lock to prevent data race
	var callback func(PlacementGroupID, PlacementGroupStatus)
	var auditLogger PlacementGroupAuditLogger
	if oldStatus != status {
		callback = m.onGroupStatusChange
		auditLogger = m.auditLogger
	}
	m.mu.Unlock()

	if oldStatus != status {
		log.Info().
			Str("group_id", string(groupID)).
			Str("old_status", string(oldStatus)).
			Str("new_status", string(status)).
			Msg("Placement group status changed")

		// Update status metrics
		metrics.SetPlacementGroupStatusMetric(string(groupID), string(status))

		// Audit log the status change
		if auditLogger != nil {
			auditLogger.LogStatusChanged(string(groupID), string(oldStatus), string(status))
		}

		if callback != nil {
			go m.safeCallback(func() { callback(groupID, status) })
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

// GetShardPlacementNodes returns nodes for distributing erasure shards.
// Returns nodes from the local placement group only.
// Returns an empty slice (not nil) for single node mode when no local group is configured.
// Returns (nil, error) if there are not enough nodes in the placement group.
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

	// Return nodes for shard distribution (first N nodes for deterministic placement)
	// For hash-based distribution, use GetShardPlacementNodesForObject
	nodes := make([]string, numShards)
	copy(nodes, m.localGroup.Nodes[:numShards])
	return nodes, nil
}

// GetShardPlacementNodesForObject returns nodes for distributing erasure shards
// using hash-based distribution for better load balancing across nodes.
// The bucket and key are used to determine the starting offset for node selection.
// Returns (nil, error) if numShards is <= 0, if no local group is configured, or
// if there are not enough nodes in the placement group.
// This method uses cached node lists for improved performance on frequent calls.
func (m *PlacementGroupManager) GetShardPlacementNodesForObject(bucket, key string, numShards int) ([]string, error) {
	// Validate numShards parameter
	if numShards <= 0 {
		return nil, fmt.Errorf("numShards must be positive, got %d", numShards)
	}

	m.mu.RLock()

	if m.localGroup == nil {
		m.mu.RUnlock()
		return nil, fmt.Errorf("no local placement group configured")
	}

	// Use cached nodes if available for better performance
	var sourceNodes []string
	if m.cachedLocalNodes != nil {
		sourceNodes = m.cachedLocalNodes
	} else {
		sourceNodes = m.localGroup.Nodes
	}

	nodeCount := len(sourceNodes)
	if nodeCount < numShards {
		m.mu.RUnlock()
		return nil, fmt.Errorf("not enough nodes in placement group: need %d, have %d",
			numShards, nodeCount)
	}

	// Use FNV hash for deterministic but distributed node selection.
	// This ensures the same object always maps to the same nodes,
	// while distributing objects evenly across the cluster.
	//
	// SECURITY NOTE: FNV-1a is used for load distribution, not cryptographic security.
	// An attacker who knows the hash algorithm could potentially craft keys that
	// concentrate shards on specific nodes. For security-critical deployments,
	// consider additional measures like rate limiting or key validation.
	// See docs/architecture/hash-distribution-security.md for details.
	h := fnv.New32a()
	h.Write([]byte(bucket))
	h.Write([]byte("/"))
	h.Write([]byte(key))
	offset := int(h.Sum32()) % nodeCount

	// Select nodes starting from the hash-determined offset
	nodes := make([]string, numShards)
	for i := 0; i < numShards; i++ {
		nodes[i] = sourceNodes[(offset+i)%nodeCount]
	}

	m.mu.RUnlock()
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

// SetAuditLogger sets the audit logger for recording membership changes
func (m *PlacementGroupManager) SetAuditLogger(logger PlacementGroupAuditLogger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditLogger = logger
}

// MarshalJSON implements json.Marshaler
func (m *PlacementGroupManager) MarshalJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create copies of groups directly to avoid deadlock from calling AllGroups()
	groups := make([]*PlacementGroup, 0, len(m.groups))
	for _, g := range m.groups {
		groups = append(groups, m.copyGroup(g))
	}

	return json.Marshal(struct {
		LocalGroupID string            `json:"local_group_id"`
		Groups       []*PlacementGroup `json:"groups"`
	}{
		LocalGroupID: string(m.config.LocalGroupID),
		Groups:       groups,
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

// invalidateCache increments the cache generation and clears cached values
// Must be called while holding the write lock
func (m *PlacementGroupManager) invalidateCache() {
	m.cacheGeneration++
	m.cachedLocalNodes = nil
	m.cachedLocalNodesHash = 0
}

// updateLocalNodesCache updates the cached local nodes list
// Must be called while holding at least a read lock
func (m *PlacementGroupManager) updateLocalNodesCache() {
	if m.localGroup == nil {
		m.cachedLocalNodes = nil
		m.cachedLocalNodesHash = 0
		return
	}

	// Compute hash of current nodes
	h := fnv.New64a()
	for _, n := range m.localGroup.Nodes {
		h.Write([]byte(n))
		h.Write([]byte{0}) // separator
	}
	newHash := h.Sum64()

	// Only update if hash changed
	if newHash != m.cachedLocalNodesHash {
		m.cachedLocalNodes = make([]string, len(m.localGroup.Nodes))
		copy(m.cachedLocalNodes, m.localGroup.Nodes)
		m.cachedLocalNodesHash = newHash
	}
}

// copyGroup creates a deep copy of a PlacementGroup to prevent external mutation
func (m *PlacementGroupManager) copyGroup(g *PlacementGroup) *PlacementGroup {
	if g == nil {
		return nil
	}
	// Create a copy of the nodes slice
	nodesCopy := make([]string, len(g.Nodes))
	copy(nodesCopy, g.Nodes)

	return &PlacementGroup{
		ID:         g.ID,
		Name:       g.Name,
		Datacenter: g.Datacenter,
		Region:     g.Region,
		Nodes:      nodesCopy,
		MinNodes:   g.MinNodes,
		MaxNodes:   g.MaxNodes,
		IsLocal:    g.IsLocal,
		Status:     g.Status,
	}
}

// safeCallbackWithTimeout executes a callback function with panic recovery, timeout, and context cancellation
func (m *PlacementGroupManager) safeCallbackWithTimeout(fn func()) {
	// Check if context is already cancelled (shutdown in progress)
	select {
	case <-m.ctx.Done():
		log.Debug().Msg("Skipping placement group callback - context cancelled")
		return
	default:
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Interface("panic", r).
					Msg("Panic recovered in placement group callback")
			}
		}()
		fn()
	}()

	select {
	case <-done:
		// Callback completed successfully
	case <-m.ctx.Done():
		// Context cancelled during callback execution (shutdown)
		log.Debug().Msg("Placement group callback interrupted by shutdown")
	case <-time.After(callbackTimeout):
		log.Warn().
			Dur("timeout", callbackTimeout).
			Msg("Placement group callback timed out")
	}
}

// safeCallback executes a callback function with panic recovery (deprecated, use safeCallbackWithTimeout)
func (m *PlacementGroupManager) safeCallback(fn func()) {
	m.safeCallbackWithTimeout(fn)
}

// Close gracefully shuts down the placement group manager, cancelling any pending callbacks
func (m *PlacementGroupManager) Close() error {
	m.cancel()
	return nil
}
