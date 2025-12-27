# Clustering & High Availability

NebulaIO uses a distributed architecture for high availability and fault tolerance, powered by the Dragonboat consensus library.

## Cluster Components

### Dragonboat Consensus

NebulaIO uses Dragonboat, a high-performance multi-group Raft implementation, for distributed consensus on metadata operations.

**Performance Characteristics**:
- **1.25 million writes/second** sustained throughput
- **1.3ms average latency** for metadata operations
- **Multi-Raft Groups** for horizontal scaling
- **Efficient snapshots** with minimal impact on ongoing operations

**Key Concepts**:
- **ShardID**: Identifies a Raft group (uint64, default: 1)
- **ReplicaID**: Unique identifier for each node within a shard (uint64, must be unique per node)
- **Leader**: Handles all write operations for a shard
- **Followers**: Replicate log from leader
- **Voters**: Participate in leader election (added via `SyncRequestAddReplica`)
- **Non-Voters**: Receive replication but don't vote (added via `SyncRequestAddNonVoting`)

**Key API Methods**:
- `GetLeaderID(shardID)` returns `(leaderID, term, valid, error)` - always check `valid` flag
- `SyncPropose` for proposing commands through Raft
- `SyncGetShardMembership` for retrieving current cluster membership

**Quorum**:
- Requires majority of voters for operations
- 3 nodes: tolerates 1 failure
- 5 nodes: tolerates 2 failures
- 7 nodes: tolerates 3 failures

### Gossip Protocol

Uses Hashicorp's memberlist for cluster membership.

**Features**:
- Automatic node discovery
- Failure detection (5-second timeout)
- Cluster state propagation
- No single point of failure

---

## Cluster Modes

### Bootstrap Mode

The first node in a cluster bootstraps a new Raft cluster.

```yaml
cluster:
  bootstrap: true
  advertise_address: 10.0.1.10
```

### Join Mode

Subsequent nodes join the existing cluster.

```yaml
cluster:
  bootstrap: false
  join_addresses:
    - 10.0.1.10:9004
```

---

## Node Roles

### Storage Node

Handles both metadata and object storage.

```yaml
cluster:
  node_role: storage
```

### Management Node

Handles only metadata operations. No object storage.

```yaml
cluster:
  node_role: management
```

### Hybrid Node (Default)

Handles both metadata and storage.

```yaml
cluster:
  node_role: hybrid
```

---

## High Availability Configuration

### Minimum HA Setup (3 nodes)

```
┌───────────────┐     ┌───────────────┐     ┌───────────────┐
│    Node 1     │◄───►│    Node 2     │◄───►│    Node 3     │
│   (Leader)    │     │  (Follower)   │     │  (Follower)   │
│   Voter: Yes  │     │   Voter: Yes  │     │   Voter: Yes  │
└───────────────┘     └───────────────┘     └───────────────┘
```

**Configuration**:

Node 1 (Bootstrap):
```yaml
node_id: node-1
cluster:
  bootstrap: true
  shard_id: 1
  replica_id: 1
  advertise_address: 10.0.1.10
  expect_nodes: 3
```

Node 2 & 3 (Join):
```yaml
node_id: node-2  # or node-3
cluster:
  bootstrap: false
  shard_id: 1
  replica_id: 2  # or 3 for node-3
  join_addresses:
    - 10.0.1.10:9004
  advertise_address: 10.0.1.11  # or 10.0.1.12
```

### Production Setup (5 nodes)

```
┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐
│  Node 1   │  │  Node 2   │  │  Node 3   │  │  Node 4   │  │  Node 5   │
│  (Leader) │  │ (Follower)│  │ (Follower)│  │ (Follower)│  │ (Follower)│
│  Voter    │  │  Voter    │  │  Voter    │  │  Voter    │  │  Voter    │
└─────┬─────┘  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘
      │              │              │              │              │
      └──────────────┴──────────────┴──────────────┴──────────────┘
                              Gossip Mesh
```

---

## Separate Planes Architecture

For large deployments, separate management and storage concerns.

### Management Plane

- 3, 5, or 7 nodes (always odd)
- Participates in Raft consensus
- Handles metadata operations
- Small storage footprint

```yaml
cluster:
  node_role: management
  voter: true
```

### Storage Plane

- Any number of nodes
- Does not participate in voting
- Handles object storage
- Scales independently

```yaml
cluster:
  node_role: storage
  voter: false
```

### Architecture

```
                    MANAGEMENT PLANE
                    (Raft Voters)
    ┌─────────────────────────────────────────┐
    │  ┌────────┐  ┌────────┐  ┌────────┐     │
    │  │ Mgmt-1 │  │ Mgmt-2 │  │ Mgmt-3 │     │
    │  │ Voter  │  │ Voter  │  │ Voter  │     │
    │  └────┬───┘  └────┬───┘  └────┬───┘     │
    │       └──────┬────┴────┬─────┘          │
    │              │ Raft    │                │
    └──────────────┼─────────┼────────────────┘
                   │ Gossip  │
    ┌──────────────┼─────────┼────────────────┐
    │       ┌──────┴─────┬───┴────┐           │
    │  ┌────▼────┐ ┌─────▼───┐ ┌──▼─────┐     │
    │  │ Stor-1  │ │ Stor-2  │ │ Stor-3 │ ... │
    │  │Non-Voter│ │Non-Voter│ │Non-Voter│    │
    │  └─────────┘ └─────────┘ └─────────┘    │
    │              STORAGE PLANE              │
    │            (Raft Non-Voters)            │
    └─────────────────────────────────────────┘
```

### Benefits

1. **Independent Scaling**: Add storage nodes without affecting consensus
2. **Faster Elections**: Fewer voters = faster leader election
3. **Resource Isolation**: Management nodes can be smaller
4. **Dedicated Resources**: Storage nodes optimized for I/O

---

## Failure Handling

### Node Failure

1. Gossip detects node failure (~5 seconds)
2. If leader fails, new election triggered
3. Operations continue with quorum
4. Failed node automatically re-joins when recovered

### Network Partition

```
    Partition A          │          Partition B
  ┌─────────────────┐    │    ┌─────────────────┐
  │ Node 1 (Leader) │    │    │     Node 3      │
  │     Node 2      │    │    │                 │
  └─────────────────┘    │    └─────────────────┘
        Majority         │        Minority
    (Can operate)        │    (Read-only/Unavailable)
```

**Behavior**:
- Partition with majority continues operating
- Partition without majority becomes unavailable
- Automatic healing when partition resolves

### Data Loss Prevention

- All writes go through Dragonboat consensus
- Data replicated to majority before acknowledgment
- Efficient incremental snapshots for fast recovery
- Write-Ahead Log (WAL) for durability
- Automatic log compaction to manage disk usage

---

## Scaling Operations

### Adding Nodes

1. Configure new node with `join_addresses`, `shard_id`, unique `replica_id`, and `raft_address`
2. Start the new node
3. Node joins cluster via gossip
4. Dragonboat adds as non-voter initially (via `SyncRequestAddNonVoting`)
5. Promote to voter if needed (via `SyncRequestAddReplica`)

```bash
# Check cluster status
curl http://localhost:9001/api/v1/admin/cluster/nodes

# Check Dragonboat shard membership
curl http://localhost:9001/api/v1/admin/cluster/membership

# Promote to voter (internally calls SyncRequestAddReplica)
curl -X POST http://localhost:9001/api/v1/admin/cluster/nodes/{node-id}/promote
```

### Removing Nodes

1. Demote from voter if applicable
2. Remove from cluster (via `SyncRequestDeleteReplica`)
3. Stop the node

```bash
# Remove node (internally calls SyncRequestDeleteReplica)
curl -X DELETE http://localhost:9001/api/v1/admin/cluster/nodes/{node-id}
```

### Graceful Shutdown

```bash
# Graceful shutdown transfers leadership
kill -SIGTERM <pid>
```

---

## Performance Considerations

### Leader Bottleneck

All writes go through the leader. Dragonboat's optimizations help:
- **Batch proposals** reduce round-trips
- **Pipelined replication** improves throughput
- Use faster storage (NVMe) for WAL directory
- Consider read replicas for read-heavy workloads

### Network Latency

Dragonboat performance depends on network latency:
- Use low-latency networks between nodes (10GbE+ recommended)
- Co-locate nodes in same region/datacenter
- Tune RTT and election timeouts for high-latency networks

```yaml
cluster:
  # RTT-based timeouts (Dragonboat uses RTT multiples)
  rtt_millisecond: 500  # Higher for cross-datacenter
  raft_election_rtt: 10  # Election timeout = 10 * RTT = 5000ms
  raft_heartbeat_rtt: 2  # Heartbeat = 2 * RTT = 1000ms
  wal_dir: /fast/nvme/wal  # Separate fast storage for WAL
  snapshot_entries: 10000  # Snapshot every N entries
  compaction_overhead: 500  # Log entries to keep after snapshot
```

### Memory Usage

Each node maintains:
- In-memory Raft log (recent entries before compaction)
- Metadata cache
- Connection pools
- Dragonboat internal buffers

Tune based on cluster size:

```yaml
performance:
  metadata_cache_size: 100000  # entries

cluster:
  # Dragonboat-specific tuning
  snapshot_entries: 10000  # Snapshot frequency
  compaction_overhead: 500  # Entries to retain post-snapshot
  check_quorum: true  # Verify quorum before serving reads
```

---

## Monitoring

### Key Metrics

| Metric | Description | Alert Threshold |
|--------|-------------|-----------------|
| `nebulaio_dragonboat_state` | Dragonboat state (1=follower, 2=leader) | No leader for 2min |
| `nebulaio_dragonboat_peers` | Number of Dragonboat peers | < expected |
| `nebulaio_gossip_nodes` | Nodes in gossip cluster | < expected |
| `nebulaio_dragonboat_commit_latency` | Dragonboat commit latency | > 10ms |
| `nebulaio_dragonboat_snapshot_count` | Snapshots created | Track for health |

### Health Checks

```bash
# Cluster health
curl http://localhost:9001/api/v1/admin/cluster/health

# Individual node health
curl http://localhost:9001/health/ready
curl http://localhost:9001/health/live
```

---

## Troubleshooting

### No Leader

**Symptoms**: Write operations fail, "no leader" errors

**Causes**:
- Not enough voters (quorum)
- Network partition
- All nodes recently restarted

**Solutions**:
1. Check if quorum exists
2. Verify network connectivity on port 9003
3. Check logs for election issues

### Split Brain

**Symptoms**: Two leaders reported

**Causes**:
- Network partition resolved incorrectly
- Time skew between nodes

**Solutions**:
1. Restart minority partition nodes
2. Sync time between nodes (use NTP)
3. Check network for intermittent issues

### Slow Elections

**Symptoms**: Long time to elect new leader

**Causes**:
- High network latency
- Election timeout too short
- Too many voters

**Solutions**:
1. Increase election timeout
2. Reduce number of voters
3. Improve network latency

---

## Next Steps

- [Storage Backend](storage.md)
- [Security Model](security.md)
- [Scaling Guide](../operations/scaling.md)
