# Placement Groups Architecture

Placement groups are the fundamental unit of data locality and fault isolation in NebulaIO's distributed storage system. This document describes the architecture, design decisions, and implementation details of the placement group subsystem.

## Overview

A placement group is a logical collection of storage nodes that share data locality for erasure coding and tiering operations. Objects are stored within a single placement group, while cross-placement group operations are reserved for disaster recovery (full object replication).

```
                    ┌─────────────────────────────────────────────────────┐
                    │                    NebulaIO Cluster                 │
                    │                                                     │
   ┌────────────────┴─────────────────┐    ┌──────────────────────────────┴──┐
   │       Placement Group: DC1       │    │       Placement Group: DC2      │
   │        (us-east-1)               │    │        (us-west-2)              │
   │  ┌─────────────────────────────┐ │    │  ┌─────────────────────────────┐│
   │  │   Object Storage (Primary)  │ │    │  │   Object Storage (Replica)  ││
   │  │   Erasure Coding: 10+4      │ │    │  │   Full Object Copies        ││
   │  │   ┌───┐ ┌───┐ ┌───┐ ┌───┐  │ │    │  │   ┌───┐ ┌───┐ ┌───┐        ││
   │  │   │N1 │ │N2 │ │N3 │...│N14│  │ │    │  │   │N1 │ │N2 │ │N3 │        ││
   │  │   └───┘ └───┘ └───┘ └───┘  │ │    │  │   └───┘ └───┘ └───┘        ││
   │  └─────────────────────────────┘ │    │  └─────────────────────────────┘│
   │                                  │    │                                 │
   │  Local Operations:               │    │  DR Replication Target          │
   │  - Erasure encode/decode         │    │                                 │
   │  - Tiering (hot/warm/cold)       │    │                                 │
   │  - Object versioning             │    │                                 │
   └──────────────────────────────────┘    └─────────────────────────────────┘
```

## Design Principles

### 1. Data Locality

All shards of an erasure-coded object reside within the same placement group. This ensures:

- **Low-latency reconstruction**: Failed shards can be rebuilt from local nodes
- **Predictable performance**: Network latency is bounded within a datacenter
- **Simplified failure domains**: Node failures affect only their placement group

### 2. Fault Isolation

Each placement group operates independently:

- Node failures in one group don't affect other groups
- Status transitions are localized (healthy, degraded, offline)
- Capacity limits prevent overcommitment

### 3. Horizontal Scalability

- Add placement groups to scale storage capacity
- Each group can have different node counts and configurations
- Cross-group replication provides disaster recovery

## Core Components

### PlacementGroup Structure

```go
type PlacementGroup struct {
    ID          PlacementGroupID     // Unique identifier
    Name        string               // Human-readable name
    Datacenter  string               // Physical datacenter location
    Region      string               // Geographic region
    Nodes       []string             // Member node IDs
    MinNodes    int                  // Minimum nodes for healthy status
    MaxNodes    int                  // Maximum allowed nodes
    IsLocal     bool                 // True if this node belongs here
    Status      PlacementGroupStatus // Current health status
}
```

### PlacementGroupManager

The manager handles all placement group operations:

```go
type PlacementGroupManager struct {
    config      PlacementGroupConfig
    localGroup  *PlacementGroup          // This node's placement group
    groups      map[PlacementGroupID]*PlacementGroup
    nodeToGroup map[string]PlacementGroupID

    // Caching for performance
    cachedLocalNodes     []string
    cachedLocalNodesHash uint64
    cacheGeneration      uint64

    // Event callbacks
    onNodeJoinedGroup   func(groupID PlacementGroupID, nodeID string)
    onNodeLeftGroup     func(groupID PlacementGroupID, nodeID string)
    onGroupStatusChange func(groupID PlacementGroupID, status PlacementGroupStatus)

    // Audit logging
    auditLogger PlacementGroupAuditLogger
}
```

## Status Model

Placement groups have four possible statuses:

| Status | Condition | Impact |
|--------|-----------|--------|
| `healthy` | `len(Nodes) >= MinNodes` | Full functionality |
| `degraded` | `len(Nodes) < MinNodes` | Reduced redundancy, warns |
| `offline` | No nodes available | Read-only or unavailable |
| `unknown` | Status not determined | Transitional state |

### Status Transitions

```
                    ┌──────────┐
                    │  unknown │
                    └────┬─────┘
                         │ (initialization)
                         ▼
    ┌───────────────────────────────────────┐
    │                                       │
    ▼                                       │
┌───────────┐  add nodes    ┌───────────┐  │
│ degraded  │──────────────►│  healthy  │──┘
└───────────┘◄──────────────└───────────┘
             remove nodes
    │                           │
    │ no nodes                  │ manual/failure
    ▼                           ▼
┌───────────┐              ┌───────────┐
│  offline  │◄─────────────│  offline  │
└───────────┘              └───────────┘
```

## Shard Distribution

### Hash-Based Placement

Objects are distributed across nodes using consistent hashing:

```go
func GetShardPlacementNodesForObject(bucket, key string, numShards int) ([]string, error)
```

**Algorithm:**

1. Hash the object path (`bucket/key`) using FNV-1a
2. Use the hash as an offset into the node list
3. Select `numShards` consecutive nodes (wrapping around)

**Properties:**

- **Deterministic**: Same object always maps to same nodes
- **Uniform**: Objects distributed evenly across nodes
- **Stable**: Adding/removing nodes minimally disrupts placement

### Erasure Coding Integration

```
Object (1MB)
    │
    ▼
┌─────────────────────────────────────────────────────┐
│  Erasure Encoder (Reed-Solomon 10+4)                │
│                                                     │
│  ┌────┐ ┌────┐ ┌────┐     ┌────┐ ┌────┐ ┌────┐    │
│  │ D0 │ │ D1 │ │ D2 │ ... │ D9 │ │ P0 │ │ P3 │    │
│  └──┬─┘ └──┬─┘ └──┬─┘     └──┬─┘ └──┬─┘ └──┬─┘    │
└─────┼──────┼──────┼──────────┼──────┼──────┼───────┘
      │      │      │          │      │      │
      ▼      ▼      ▼          ▼      ▼      ▼
   Node1  Node2  Node3      Node10 Node11 Node14

   (All nodes within the same Placement Group)
```

## Configuration

### YAML Configuration

```yaml
storage:
  default_redundancy:
    enabled: true
    data_shards: 10
    parity_shards: 4
    replication_factor: 2  # Cross-group replication

  placement_groups:
    local_group_id: pg-dc1
    min_nodes_for_erasure: 14  # Global minimum

    groups:
      - id: pg-dc1
        name: US East Datacenter
        datacenter: dc1
        region: us-east-1
        min_nodes: 14
        max_nodes: 50

      - id: pg-dc2
        name: US West Datacenter
        datacenter: dc2
        region: us-west-2
        min_nodes: 14
        max_nodes: 50

    replication_targets:
      - pg-dc2  # DR target for pg-dc1
```

### Configuration Validation

The system validates configuration at startup:

1. **Redundancy vs Nodes**: `min_nodes >= data_shards + parity_shards`
2. **Replication Factor**: `replication_factor <= len(replication_targets)`
3. **Group References**: All referenced group IDs must exist
4. **Node Limits**: `min_nodes <= max_nodes` (if max is set)

## Caching Strategy

Frequently accessed data is cached to reduce lock contention:

### Cached Data

- Local group node list
- Node membership hashes

### Cache Invalidation

- On node join/leave
- Cache generation counter prevents stale reads
- Lock upgrade pattern for cache misses

```go
// Read path (optimized)
func (m *PlacementGroupManager) LocalGroupNodes() []string {
    m.mu.RLock()
    if m.cachedLocalNodes != nil {
        // Fast path: return cached copy
        nodes := make([]string, len(m.cachedLocalNodes))
        copy(nodes, m.cachedLocalNodes)
        m.mu.RUnlock()
        return nodes
    }
    // Slow path: upgrade lock, rebuild cache
    m.mu.RUnlock()
    m.mu.Lock()
    defer m.mu.Unlock()
    m.updateLocalNodesCache()
    // ... return copy
}
```

## Audit Logging

All membership changes are logged for compliance:

### Event Types

| Event | Description |
|-------|-------------|
| `cluster:PlacementGroup:NodeJoined` | Node added to group |
| `cluster:PlacementGroup:NodeLeft` | Node removed from group |
| `cluster:PlacementGroup:StatusChanged` | Group health status changed |
| `cluster:PlacementGroup:Created` | New group created |
| `cluster:PlacementGroup:Deleted` | Group removed |

### Integration

```go
// Set up audit logging
auditAdapter := audit.NewPlacementGroupAuditAdapter(auditLogger)
pgManager.SetAuditLogger(auditAdapter)
```

## Metrics

### Prometheus Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `nebulaio_placement_group_nodes` | Gauge | group_id, datacenter, region | Node count per group |
| `nebulaio_placement_group_status` | Gauge | group_id | Status (1=healthy, 2=degraded, 3=offline) |
| `nebulaio_placement_group_info` | Info | group_id, name, datacenter, region, is_local | Group metadata |

### Alerting Thresholds

```yaml
# Example Prometheus alerts
groups:
  - name: placement_groups
    rules:
      - alert: PlacementGroupDegraded
        expr: nebulaio_placement_group_status == 2
        for: 5m
        labels:
          severity: warning

      - alert: PlacementGroupOffline
        expr: nebulaio_placement_group_status == 3
        for: 1m
        labels:
          severity: critical
```

## Callback System

Event callbacks allow integration with other subsystems:

```go
// Register callbacks
pgManager.SetOnNodeJoinedGroup(func(groupID PlacementGroupID, nodeID string) {
    // Trigger rebalancing, update routing tables, etc.
})

pgManager.SetOnNodeLeftGroup(func(groupID PlacementGroupID, nodeID string) {
    // Trigger shard recovery, update routing tables, etc.
})

pgManager.SetOnGroupStatusChange(func(groupID PlacementGroupID, status PlacementGroupStatus) {
    // Alert, update health checks, etc.
})
```

### Callback Safety

Callbacks are executed with:

- **Panic recovery**: Callbacks can't crash the manager
- **Timeout protection**: 5-second maximum execution time
- **Asynchronous execution**: Don't block the mutation path

## Thread Safety

The manager uses `sync.RWMutex` for concurrent access:

| Operation | Lock Type |
|-----------|-----------|
| Read group info | RLock |
| Read node list | RLock |
| Add/remove node | Lock |
| Update status | Lock |
| Set callbacks | Lock |

### Lock Ordering

To prevent deadlocks:

1. Always acquire manager lock before group-specific operations
2. Release locks before invoking callbacks
3. Capture callback references while holding lock, invoke after release

## Best Practices

### Deployment

1. **Size placement groups appropriately**:
   - Minimum: `data_shards + parity_shards` nodes
   - Recommended: 1.5-2x minimum for headroom

2. **Geographic separation**:
   - One placement group per datacenter
   - Cross-group replication for DR

3. **Capacity planning**:
   - Set `max_nodes` to prevent overcommitment
   - Monitor `min_nodes` vs actual for degradation risk

### Operations

1. **Rolling upgrades**:
   - Drain one node at a time
   - Wait for group to stabilize before continuing

2. **Adding capacity**:
   - Add nodes to existing groups before creating new groups
   - New groups require rebalancing of existing data

3. **Handling failures**:
   - Monitor degraded status
   - Replace failed nodes promptly
   - Use offline status for maintenance windows

## Raft Integration

Placement groups integrate with Dragonboat (Raft) for metadata consistency and membership coordination.

### Metadata Consistency

Placement group state is replicated through Raft to ensure consistency:

```
┌──────────────────────────────────────────────────────────────────┐
│                         Raft Consensus                           │
│                                                                  │
│   ┌──────────┐     ┌──────────┐     ┌──────────┐                │
│   │  Leader  │────►│ Follower │────►│ Follower │                │
│   │  Node 1  │     │  Node 2  │     │  Node 3  │                │
│   └──────────┘     └──────────┘     └──────────┘                │
│        │                                                         │
│        ▼                                                         │
│   ┌──────────────────────────────────────────┐                  │
│   │  Placement Group Metadata (Replicated)   │                  │
│   │  - Group configurations                  │                  │
│   │  - Node membership                       │                  │
│   │  - Object location mappings              │                  │
│   └──────────────────────────────────────────┘                  │
└──────────────────────────────────────────────────────────────────┘
```

### Membership Coordination

When nodes join or leave a placement group, the changes are proposed through Raft:

1. **Node Join Request**

   ```
   Client Request → Leader Proposes → Log Replication → Apply Locally
   ```

2. **State Machine Updates**
   - The placement group manager applies membership changes from the committed log
   - All nodes converge to the same membership state

3. **Consistency Guarantees**
   - Strong consistency for group membership
   - Linearizable reads for placement decisions
   - No split-brain scenarios

### Recovery Behavior

During node failures and leader elections:

| Scenario | Behavior |
|----------|----------|
| Leader fails | New leader elected, placement state intact |
| Follower fails | Group continues with remaining quorum |
| Network partition | Minority side cannot modify membership |
| Node rejoins | Catches up from Raft log or snapshot |

### Configuration Synchronization

Placement group configuration changes flow through Raft:

```go
// Configuration change proposal (pseudocode)
type PlacementGroupConfigChange struct {
    Type     ChangeType     // AddNode, RemoveNode, UpdateConfig
    GroupID  PlacementGroupID
    NodeID   string
    Config   *PlacementGroup
}

// Applied via state machine
func (sm *StateMachine) Apply(entry raftpb.Entry) {
    var change PlacementGroupConfigChange
    if err := json.Unmarshal(entry.Data, &change); err != nil {
        return
    }

    switch change.Type {
    case AddNode:
        sm.pgManager.AddNodeToGroup(change.GroupID, change.NodeID)
    case RemoveNode:
        sm.pgManager.RemoveNodeFromGroup(change.GroupID, change.NodeID)
    }
}
```

### Snapshot Strategy

Raft snapshots include placement group state:

- **Snapshot Contents**: All placement group configurations, node memberships
- **Snapshot Frequency**: Configurable, default after 10,000 log entries
- **Recovery**: New nodes restore from snapshot + log replay

## Related Documentation

- [Clustering & High Availability](clustering.md)
- [Hash Distribution Security](hash-distribution-security.md)
- [Erasure Coding](../features/erasure-coding.md)
- [Storage Architecture](storage.md)
