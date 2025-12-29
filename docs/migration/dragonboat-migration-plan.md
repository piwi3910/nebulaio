# Migration Plan: HashiCorp Raft to Dragonboat

## Executive Summary

This document outlines the plan to migrate NebulaIO's distributed consensus layer from HashiCorp Raft to [Dragonboat](https://github.com/lni/dragonboat), a high-performance multi-group Raft library.

### Why Migrate?

| Aspect | HashiCorp Raft | Dragonboat |
| -------- | --------------- | ------------ |
| Performance | Good | **1.25M writes/sec** (16-byte payloads) |
| Latency | ~5-10ms | **1.3ms avg, 2.6ms P99** |
| Multi-group | No | **Yes (native)** |
| Dependencies | Uses deprecated `armon/go-metrics` | Pure Go, minimal deps |
| Maintenance | Deprecation warnings until 2025 | Active v4.0 development |

### Scope of Changes

| Category | Files Affected | Effort |
| ---------- | --------------- | -------- |
| Core Implementation | 3 files (~1,800 lines) | High |
| Configuration | 2 files | Medium |
| API Handlers | 3 files | Medium |
| Server/Health/Metrics | 3 files | Low |
| Deployment (K8s, Helm, Docker) | 8+ files | Medium |
| Documentation | 12+ files | Medium |
| Tests | New test files needed | High |

---

## Phase 1: Core Library Migration

### 1.1 Create New Dragonboat Store Implementation

**File: `internal/metadata/dragonboat.go`** (NEW)

Replace HashiCorp Raft with Dragonboat NodeHost:

```go

package metadata

import (
    "context"
    "github.com/lni/dragonboat/v4"
    "github.com/lni/dragonboat/v4/config"
    "github.com/lni/dragonboat/v4/statemachine"
)

// DragonboatConfig holds configuration for the Dragonboat store
type DragonboatConfig struct {
    NodeID      uint64   // Replica ID (was string, now uint64)
    ShardID     uint64   // Shard/Cluster ID (new concept)
    DataDir     string
    RaftAddress string   // host:port for Raft communication
    Bootstrap   bool
    InitialMembers map[uint64]string // ReplicaID -> RaftAddress
}

// DragonboatStore implements the Store interface using Dragonboat
type DragonboatStore struct {
    config   DragonboatConfig
    nodeHost *dragonboat.NodeHost
    shardID  uint64
    replicaID uint64
    badger   *badger.DB
}

```text

**Key API Mappings:**

| HashiCorp Raft | Dragonboat |
| ---------------- | ------------ |
| `raft.NewRaft()` | `dragonboat.NewNodeHost()` + `nh.StartReplica()` |
| `raft.Apply()` | `nh.SyncPropose()` |
| `raft.State() == Leader` | `nh.GetLeaderID()` |
| `raft.AddVoter()` | `nh.SyncRequestAddReplica()` |
| `raft.RemoveServer()` | `nh.SyncRequestDeleteReplica()` |
| `raft.Snapshot()` | `nh.SyncRequestSnapshot()` |
| `raft.BootstrapCluster()` | `nh.StartReplica(initialMembers, false, ...)` |
| `raft.GetConfiguration()` | `nh.SyncGetShardMembership()` |
| FSM interface | `statemachine.IStateMachine` interface |

### 1.2 Implement State Machine Interface

**File: `internal/metadata/dragonboat_fsm.go`** (NEW)

```go

package metadata

import (
    "io"
    "github.com/lni/dragonboat/v4/statemachine"
)

// stateMachine implements dragonboat's IStateMachine interface
type stateMachine struct {
    shardID   uint64
    replicaID uint64
    db        *badger.DB
}

// Compile-time interface check
var _ statemachine.IStateMachine = (*stateMachine)(nil)

func (s *stateMachine) Update(data []byte) (statemachine.Result, error) {
    // Decode command and apply to BadgerDB
    // Similar logic to current fsm.Apply()
}

func (s *stateMachine) Lookup(query interface{}) (interface{}, error) {
    // Handle read queries
}

func (s *stateMachine) SaveSnapshot(w io.Writer,
    fc statemachine.ISnapshotFileCollection,
    done <-chan struct{}) error {
    // Serialize BadgerDB state
}

func (s *stateMachine) RecoverFromSnapshot(r io.Reader,
    fc statemachine.ISnapshotFileCollection,
    done <-chan struct{}) error {
    // Restore from snapshot
}

func (s *stateMachine) Close() error {
    return nil // BadgerDB closed separately
}

```bash

### 1.3 Update Store Interface Methods

**File: `internal/metadata/dragonboat_store.go`** (NEW)

Implement all metadata operations using Dragonboat:

```go

func (s *DragonboatStore) CreateBucket(ctx context.Context, bucket *Bucket) error {
    cmd := Command{Type: CmdCreateBucket, Data: bucket}
    data, _ := json.Marshal(cmd)

    session := s.nodeHost.GetNoOPSession(s.shardID)
    _, err := s.nodeHost.SyncPropose(ctx, session, data)
    return err
}

func (s *DragonboatStore) IsLeader() bool {
    leaderID, _, valid, err := s.nodeHost.GetLeaderID(s.shardID)
    if err != nil || !valid {
        return false
    }
    return leaderID == s.replicaID
}

func (s *DragonboatStore) GetLeaderAddress() (string, error) {
    membership, err := s.nodeHost.SyncGetShardMembership(
        context.Background(), s.shardID)
    if err != nil {
        return "", err
    }
    leaderID, _, valid, _ := s.nodeHost.GetLeaderID(s.shardID)
    if !valid {
        return "", ErrNoLeader
    }
    return membership.Nodes[leaderID], nil
}

```bash

### 1.4 Breaking Changes to Address

| Change | Migration Action |
| -------- | ----------------- |
| NodeID: string → uint64 | Add ID mapping/hashing function |
| Single cluster → ShardID concept | Use ShardID=1 for metadata shard |
| BoltDB log store → Built-in | Remove raft-boltdb dependency |
| ServerSuffrage → NonVoting/Witness flags | Update voter/non-voter logic |
| Transport: TCP only → configurable | Use default gRPC transport |

---

## Phase 2: Configuration Updates

### 2.1 Update Config Struct

**File: `internal/config/config.go`**

```go

type ClusterConfig struct {
    // Existing fields
    NodeID         string   `mapstructure:"node_id"`
    JoinAddresses  []string `mapstructure:"join_addresses"`
    Bootstrap      bool     `mapstructure:"bootstrap"`
    RaftPort       int      `mapstructure:"raft_port"`
    GossipPort     int      `mapstructure:"gossip_port"`

    // New Dragonboat-specific fields
    ShardID        uint64   `mapstructure:"shard_id"`        // Default: 1
    ReplicaID      uint64   `mapstructure:"replica_id"`      // Auto-generated if 0
    WALDir         string   `mapstructure:"wal_dir"`         // Separate WAL directory
    SnapshotCount  uint64   `mapstructure:"snapshot_count"`  // Entries between snapshots
    CompactionOverhead uint64 `mapstructure:"compaction_overhead"`

    // Deprecated (remove in future)
    // RaftBind - now derived from RaftPort
}

```bash

### 2.2 Update Default Configuration

```yaml

cluster:
  shard_id: 1                    # Metadata shard ID
  replica_id: 0                  # Auto-assign based on node_id hash
  raft_port: 9003
  wal_dir: ""                    # Empty = use data_dir/wal
  snapshot_count: 10000          # Snapshot every 10k entries
  compaction_overhead: 5000

```text

---

## Phase 3: Cluster Discovery Integration

### 3.1 Update Discovery Module

**File: `internal/cluster/discovery.go`**

```go

import (
    "github.com/lni/dragonboat/v4"
)

type Discovery struct {
    config     DiscoveryConfig
    members    map[string]*NodeInfo
    mu         sync.RWMutex
    nodeHost   *dragonboat.NodeHost  // Changed from *raft.Raft
    shardID    uint64                // New field
    membership *Membership
    // ...
}

func (d *Discovery) SetNodeHost(nh *dragonboat.NodeHost, shardID uint64) {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.nodeHost = nh
    d.shardID = shardID
}

func (d *Discovery) IsLeader() bool {
    if d.nodeHost == nil {
        return false
    }
    leaderID, _, valid, _ := d.nodeHost.GetLeaderID(d.shardID)
    if !valid {
        return false
    }
    info := d.nodeHost.GetNodeHostInfo(dragonboat.DefaultNodeHostInfoOption)
    for _, ci := range info.ShardInfoList {
        if ci.ShardID == d.shardID {
            return ci.ReplicaID == leaderID
        }
    }
    return false
}

func (d *Discovery) addRaftVoter(node *NodeInfo) error {
    membership, err := d.nodeHost.SyncGetShardMembership(
        context.Background(), d.shardID)
    if err != nil {
        return err
    }

    replicaID := hashNodeID(node.NodeID) // Convert string to uint64
    return d.nodeHost.SyncRequestAddReplica(
        context.Background(),
        d.shardID,
        replicaID,
        node.RaftAddr,
        membership.ConfigChangeID,
    )
}

```text

---

## Phase 4: API Handler Updates

### 4.1 Update Admin Cluster Handler

**File: `internal/api/admin/cluster.go`**

```go

type ClusterHandler struct {
    store     *metadata.DragonboatStore  // Type change
    discovery *cluster.Discovery
}

func (h *ClusterHandler) GetRaftConfiguration(w http.ResponseWriter, r *http.Request) {
    membership, err := h.store.GetMembership(r.Context())
    if err != nil {
        // handle error
    }

    // Map Dragonboat Membership to API response
    config := ClusterConfig{
        ConfigChangeID: membership.ConfigChangeID,
        Servers: make([]ServerInfo, 0),
    }

    for replicaID, addr := range membership.Nodes {
        config.Servers = append(config.Servers, ServerInfo{
            ID:       replicaID,
            Address:  addr,
            Suffrage: "Voter",
        })
    }
    for replicaID, addr := range membership.NonVotings {
        config.Servers = append(config.Servers, ServerInfo{
            ID:       replicaID,
            Address:  addr,
            Suffrage: "NonVoter",
        })
    }
    // ...
}

func (h *ClusterHandler) GetRaftStats(w http.ResponseWriter, r *http.Request) {
    info := h.store.GetNodeHostInfo()
    // Map to stats response
}

```bash

### 4.2 Update Handler Factory

**File: `internal/api/admin/handler.go`**

Update `GetRaftState()` to use Dragonboat API.

---

## Phase 5: Server Initialization

### 5.1 Update Server Startup

**File: `internal/server/server.go`**

```go

func NewServer(cfg *config.Config) (*Server, error) {
    // ...

    // Create NodeHost configuration
    nhConfig := config.NodeHostConfig{
        NodeHostDir:    filepath.Join(cfg.DataDir, "dragonboat"),
        WALDir:        cfg.Cluster.WALDir,
        RaftAddress:   fmt.Sprintf("%s:%d", bindAddr, cfg.Cluster.RaftPort),
        // Enable Prometheus metrics
        EnableMetrics: true,
    }

    // Create DragonboatStore
    storeConfig := metadata.DragonboatConfig{
        NodeID:      hashNodeID(cfg.NodeID),
        ShardID:     cfg.Cluster.ShardID,
        DataDir:     cfg.DataDir,
        RaftAddress: nhConfig.RaftAddress,
        Bootstrap:   cfg.Cluster.Bootstrap,
    }

    if cfg.Cluster.Bootstrap {
        storeConfig.InitialMembers = map[uint64]string{
            storeConfig.NodeID: nhConfig.RaftAddress,
        }
    }

    srv.metaStore, err = metadata.NewDragonboatStore(storeConfig)
    if err != nil {
        return nil, err
    }

    // Connect to discovery
    srv.discovery.SetNodeHost(srv.metaStore.GetNodeHost(), storeConfig.ShardID)

    // ...
}

```text

---

## Phase 6: Metrics Updates

### 6.1 Update Prometheus Metrics

**File: `internal/metrics/metrics.go`**

```go

// Update metric names and collection
var (
    raftState = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "nebulaio_raft_state",
            Help: "Current Raft state (1=leader, 0=follower/candidate)",
        },
        []string{"shard_id"},
    )

    raftIsLeader = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "nebulaio_raft_is_leader",
            Help: "Whether this node is the Raft leader",
        },
        []string{"shard_id"},
    )

    // New Dragonboat-specific metrics
    raftProposalLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nebulaio_raft_proposal_latency_seconds",
            Help:    "Latency of Raft proposals",
            Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
        },
        []string{"shard_id"},
    )
)

func CollectRaftMetrics(store *metadata.DragonboatStore) {
    info := store.GetNodeHostInfo()
    for _, shard := range info.ShardInfoList {
        leaderID, _, valid, _ := store.GetNodeHost().GetLeaderID(shard.ShardID)
        isLeader := valid && leaderID == shard.ReplicaID

        shardLabel := fmt.Sprintf("%d", shard.ShardID)
        if isLeader {
            raftState.WithLabelValues(shardLabel).Set(1)
            raftIsLeader.WithLabelValues(shardLabel).Set(1)
        } else {
            raftState.WithLabelValues(shardLabel).Set(0)
            raftIsLeader.WithLabelValues(shardLabel).Set(0)
        }
    }
}

```text

---

## Phase 7: Deployment Updates

### 7.1 Kubernetes StatefulSet

**File: `deployments/kubernetes/base/statefulset.yaml`**

```yaml

# Port remains the same (9003)
ports:
  - name: raft
    containerPort: 9003
    protocol: TCP

# Add WAL volume for better performance
volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 10Gi
  - metadata:
      name: wal
    spec:
      accessModes: ["ReadWriteOnce"]
      storageClassName: fast-ssd  # Use fast storage for WAL
      resources:
        requests:
          storage: 5Gi

```bash

### 7.2 ConfigMap Updates

**File: `deployments/kubernetes/base/configmap.yaml`**

```yaml

data:
  nebulaio.yaml: |
    cluster:
      shard_id: 1
      raft_port: 9003
      wal_dir: /data/wal
      snapshot_count: 10000

```bash

### 7.3 Docker Compose Updates

**File: `docker-compose.cluster.yml`**

```yaml

services:
  nebulaio-1:
    environment:
      NEBULAIO_CLUSTER_SHARD_ID: "1"
      NEBULAIO_CLUSTER_REPLICA_ID: "1"
      NEBULAIO_CLUSTER_RAFT_PORT: "9003"
    volumes:
      - node1-data:/data
      - node1-wal:/data/wal  # Separate WAL volume

```bash

### 7.4 Helm Chart Updates

**File: `deployments/helm/nebulaio/values.yaml`**

```yaml

cluster:
  shardId: 1
  raftPort: 9003
  walDir: "/data/wal"
  snapshotCount: 10000
  compactionOverhead: 5000

```text

---

## Phase 8: Documentation Updates

### 8.1 Files to Update

| File | Changes Required |
| ------ | ----------------- |
| `README.md` | Update feature description |
| `docs/architecture/clustering.md` | Complete rewrite for Dragonboat |
| `docs/architecture/overview.md` | Update consensus section |
| `docs/deployment/standalone.md` | Update configuration examples |
| `docs/deployment/docker.md` | Update environment variables |
| `docs/deployment/kubernetes.md` | Update deployment examples |
| `docs/getting-started/configuration.md` | Add new config options |
| `docs/operations/scaling.md` | Update membership change docs |
| `docs/operations/troubleshooting.md` | Update troubleshooting commands |
| `docs/PERFORMANCE_TUNING.md` | Update Raft tuning section |
| `docs/TROUBLESHOOTING.md` | Update Raft debugging |

### 8.2 New Documentation

**File: `docs/migration/hashicorp-to-dragonboat.md`** (NEW)

Migration guide for existing deployments.

---

## Phase 9: Testing Strategy

### 9.1 Unit Tests

**File: `internal/metadata/dragonboat_test.go`** (NEW)

```go

func TestDragonboatStore_CreateBucket(t *testing.T) { ... }
func TestDragonboatStore_LeaderElection(t *testing.T) { ... }
func TestDragonboatStore_Snapshot(t *testing.T) { ... }
func TestDragonboatStore_MembershipChange(t *testing.T) { ... }

```bash

### 9.2 Integration Tests

**File: `internal/metadata/dragonboat_integration_test.go`** (NEW)

```go

//go:build integration

func TestDragonboat_ThreeNodeCluster(t *testing.T) { ... }
func TestDragonboat_LeaderFailover(t *testing.T) { ... }
func TestDragonboat_NodeJoinLeave(t *testing.T) { ... }

```bash

### 9.3 Benchmark Tests

```go

func BenchmarkDragonboat_Propose(b *testing.B) { ... }
func BenchmarkDragonboat_Read(b *testing.B) { ... }

```text

---

## Phase 10: Migration Path for Existing Deployments

### 10.1 Data Migration Strategy

#### Option A: Clean Migration (Recommended for small deployments)

1. Export all metadata from existing cluster
2. Shut down old cluster
3. Deploy new Dragonboat-based cluster
4. Import metadata

#### Option B: Rolling Migration (For zero-downtime)

1. Add Dragonboat nodes as non-voters
2. Sync state via external mechanism
3. Switch over traffic
4. Remove old nodes

### 10.2 Breaking Changes Notice

```markdown

## Breaking Changes in vX.0.0

### Raft Implementation Change
NebulaIO has migrated from HashiCorp Raft to Dragonboat for improved performance.

**Configuration Changes:**
- `node_id` now internally mapped to `uint64` (no change in config format)
- New optional: `shard_id` (default: 1)
- New optional: `wal_dir` for separate WAL storage

**API Changes:**
- `/api/v1/cluster/config` response format slightly changed
- Raft stats endpoint returns different metrics

**Data Migration:**
- Existing Raft logs are NOT compatible
- See migration guide: docs/migration/hashicorp-to-dragonboat.md

```text

---

## Implementation Timeline

### Week 1-2: Core Implementation

- [ ] Create `dragonboat.go` with NodeHost setup
- [ ] Create `dragonboat_fsm.go` with state machine
- [ ] Create `dragonboat_store.go` with metadata operations
- [ ] Update `go.mod` dependencies

### Week 3: Integration

- [ ] Update `internal/config/config.go`
- [ ] Update `internal/server/server.go`
- [ ] Update `internal/cluster/discovery.go`
- [ ] Update API handlers

### Week 4: Testing

- [ ] Unit tests for all new code
- [ ] Integration tests for cluster operations
- [ ] Performance benchmarks

### Week 5: Deployment & Docs

- [ ] Update all Kubernetes manifests
- [ ] Update Helm charts
- [ ] Update Docker Compose
- [ ] Update all documentation

### Week 6: Migration Tools

- [ ] Create migration CLI command
- [ ] Write migration guide
- [ ] Test upgrade paths

---

## Dependencies

### Add

```go

require (
    github.com/lni/dragonboat/v4 v4.0.0
)

```bash

### Remove

```go

// These can be removed after migration
github.com/hashicorp/raft v1.7.3
github.com/hashicorp/raft-boltdb v0.0.0-...

```text

---

## Rollback Plan

If issues arise during migration:

1. Keep HashiCorp Raft code in a separate branch
2. Use build tags to switch implementations:

   ```go

   //go:build !dragonboat
   // hashicorp implementation

   //go:build dragonboat
   // dragonboat implementation

   ```

1. Document rollback procedure in operations guide

---

## Success Criteria

- [ ] All existing tests pass with Dragonboat
- [ ] Performance improvement demonstrated (>2x throughput)
- [ ] Latency improvement demonstrated (<3ms P99)
- [ ] Zero deprecated dependency warnings
- [ ] Documentation complete
- [ ] Migration guide tested on real cluster
