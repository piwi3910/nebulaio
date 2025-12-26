package erasure

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
)

// NodeInfo represents a storage node in the cluster
type NodeInfo struct {
	ID       string
	Address  string
	Status   string // "alive", "suspect", "dead"
	Capacity int64  // Available storage in bytes
	Used     int64  // Used storage in bytes
}

// PlacementStrategy determines how shards are distributed across nodes
type PlacementStrategy interface {
	// PlaceShards returns node assignments for each shard index
	PlaceShards(objectKey string, numShards int, nodes []NodeInfo) ([]NodeAssignment, error)

	// GetShardLocation returns the node assignment for a specific shard
	GetShardLocation(objectKey string, shardIndex int, nodes []NodeInfo) (*NodeAssignment, error)
}

// NodeAssignment represents a shard-to-node assignment
type NodeAssignment struct {
	ShardIndex int
	NodeID     string
	NodeAddr   string
}

// ConsistentHashPlacement uses consistent hashing for shard placement
type ConsistentHashPlacement struct {
	virtualNodes int
}

// NewConsistentHashPlacement creates a new consistent hash placement strategy
func NewConsistentHashPlacement(virtualNodes int) *ConsistentHashPlacement {
	if virtualNodes <= 0 {
		virtualNodes = 100 // Default virtual nodes per physical node
	}
	return &ConsistentHashPlacement{
		virtualNodes: virtualNodes,
	}
}

// PlaceShards distributes shards across nodes using consistent hashing
func (p *ConsistentHashPlacement) PlaceShards(objectKey string, numShards int, nodes []NodeInfo) ([]NodeAssignment, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes available for shard placement")
	}

	// Filter to alive nodes only
	aliveNodes := filterAliveNodes(nodes)
	if len(aliveNodes) == 0 {
		return nil, fmt.Errorf("no alive nodes available")
	}

	assignments := make([]NodeAssignment, numShards)
	usedNodes := make(map[string]int) // Track how many shards each node has

	for i := 0; i < numShards; i++ {
		// Create a unique key for this shard
		shardKey := fmt.Sprintf("%s/shard/%d", objectKey, i)

		// Find the best node for this shard (avoiding already-used nodes if possible)
		node := p.findBestNode(shardKey, aliveNodes, usedNodes)

		assignments[i] = NodeAssignment{
			ShardIndex: i,
			NodeID:     node.ID,
			NodeAddr:   node.Address,
		}
		usedNodes[node.ID]++
	}

	return assignments, nil
}

// GetShardLocation returns the node assignment for a specific shard
func (p *ConsistentHashPlacement) GetShardLocation(objectKey string, shardIndex int, nodes []NodeInfo) (*NodeAssignment, error) {
	assignments, err := p.PlaceShards(objectKey, shardIndex+1, nodes)
	if err != nil {
		return nil, err
	}
	return &assignments[shardIndex], nil
}

// findBestNode finds the best node for a shard using consistent hashing
// while trying to spread shards across different nodes
func (p *ConsistentHashPlacement) findBestNode(shardKey string, nodes []NodeInfo, usedNodes map[string]int) NodeInfo {
	if len(nodes) == 1 {
		return nodes[0]
	}

	// Create a ring with virtual nodes
	type ringEntry struct {
		hash   uint64
		nodeID string
		node   NodeInfo
	}

	ring := make([]ringEntry, 0, len(nodes)*p.virtualNodes)

	for _, node := range nodes {
		for v := 0; v < p.virtualNodes; v++ {
			vNodeKey := fmt.Sprintf("%s:%d", node.ID, v)
			hash := hashKey(vNodeKey)
			ring = append(ring, ringEntry{
				hash:   hash,
				nodeID: node.ID,
				node:   node,
			})
		}
	}

	// Sort ring by hash
	sort.Slice(ring, func(i, j int) bool {
		return ring[i].hash < ring[j].hash
	})

	// Hash the shard key
	shardHash := hashKey(shardKey)

	// Find the first node in the ring with hash >= shardHash
	// Try to avoid nodes that already have many shards
	minUsage := int(^uint(0) >> 1) // max int
	for _, node := range nodes {
		if usedNodes[node.ID] < minUsage {
			minUsage = usedNodes[node.ID]
		}
	}

	// Allow nodes with at most minUsage + 1 shards
	maxAllowed := minUsage + 1

	// Walk the ring looking for a suitable node
	startIdx := sort.Search(len(ring), func(i int) bool {
		return ring[i].hash >= shardHash
	})

	for i := 0; i < len(ring); i++ {
		idx := (startIdx + i) % len(ring)
		entry := ring[idx]

		// Check if this node can accept more shards
		if usedNodes[entry.nodeID] <= maxAllowed {
			return entry.node
		}
	}

	// Fallback: return the first node in ring order
	return ring[startIdx%len(ring)].node
}

// hashKey produces a consistent hash for a key
func hashKey(key string) uint64 {
	h := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(h[:8])
}

// filterAliveNodes returns only nodes that are alive
func filterAliveNodes(nodes []NodeInfo) []NodeInfo {
	alive := make([]NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		if n.Status == "alive" || n.Status == "" {
			alive = append(alive, n)
		}
	}
	return alive
}

// RoundRobinPlacement distributes shards in round-robin fashion
type RoundRobinPlacement struct{}

// NewRoundRobinPlacement creates a new round-robin placement strategy
func NewRoundRobinPlacement() *RoundRobinPlacement {
	return &RoundRobinPlacement{}
}

// PlaceShards distributes shards across nodes in round-robin order
func (p *RoundRobinPlacement) PlaceShards(objectKey string, numShards int, nodes []NodeInfo) ([]NodeAssignment, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes available for shard placement")
	}

	aliveNodes := filterAliveNodes(nodes)
	if len(aliveNodes) == 0 {
		return nil, fmt.Errorf("no alive nodes available")
	}

	assignments := make([]NodeAssignment, numShards)

	for i := 0; i < numShards; i++ {
		node := aliveNodes[i%len(aliveNodes)]
		assignments[i] = NodeAssignment{
			ShardIndex: i,
			NodeID:     node.ID,
			NodeAddr:   node.Address,
		}
	}

	return assignments, nil
}

// GetShardLocation returns the node assignment for a specific shard
func (p *RoundRobinPlacement) GetShardLocation(objectKey string, shardIndex int, nodes []NodeInfo) (*NodeAssignment, error) {
	assignments, err := p.PlaceShards(objectKey, shardIndex+1, nodes)
	if err != nil {
		return nil, err
	}
	return &assignments[shardIndex], nil
}

// CapacityAwarePlacement considers node capacity for placement
type CapacityAwarePlacement struct {
	base PlacementStrategy
}

// NewCapacityAwarePlacement creates a capacity-aware placement strategy
func NewCapacityAwarePlacement() *CapacityAwarePlacement {
	return &CapacityAwarePlacement{
		base: NewConsistentHashPlacement(100),
	}
}

// PlaceShards distributes shards considering node capacity
func (p *CapacityAwarePlacement) PlaceShards(objectKey string, numShards int, nodes []NodeInfo) ([]NodeAssignment, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes available for shard placement")
	}

	// Filter nodes with capacity
	available := make([]NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		if n.Status == "alive" || n.Status == "" {
			if n.Capacity == 0 || n.Used < n.Capacity {
				available = append(available, n)
			}
		}
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no nodes with available capacity")
	}

	// Sort by available capacity (descending)
	sort.Slice(available, func(i, j int) bool {
		availI := available[i].Capacity - available[i].Used
		availJ := available[j].Capacity - available[j].Used
		return availI > availJ
	})

	return p.base.PlaceShards(objectKey, numShards, available)
}

// GetShardLocation returns the node assignment for a specific shard
func (p *CapacityAwarePlacement) GetShardLocation(objectKey string, shardIndex int, nodes []NodeInfo) (*NodeAssignment, error) {
	assignments, err := p.PlaceShards(objectKey, shardIndex+1, nodes)
	if err != nil {
		return nil, err
	}
	return &assignments[shardIndex], nil
}
