# Architecture Overview

NebulaIO is a distributed, S3-compatible object storage system designed for high availability, scalability, and performance.

## High-Level Architecture

```
                                    ┌─────────────────────────────────────────┐
                                    │             Load Balancer               │
                                    │        (HAProxy / Nginx / Cloud LB)     │
                                    └──────────────┬───────────┬──────────────┘
                                                   │           │
                    ┌──────────────────────────────┼───────────┼──────────────────────────────┐
                    │                              │           │                              │
           ┌────────▼────────┐            ┌────────▼────────┐            ┌────────▼────────┐
           │   NebulaIO #1   │            │   NebulaIO #2   │            │   NebulaIO #3   │
           │                 │◄──────────►│                 │◄──────────►│                 │
           │  S3 API :9000   │   Gossip   │  S3 API :9000   │   Gossip   │  S3 API :9000   │
           │  Admin  :9001   │   :9004    │  Admin  :9001   │   :9004    │  Admin  :9001   │
           │  Raft   :9003   │            │  Raft   :9003   │            │  Raft   :9003   │
           └────────┬────────┘            └────────┬────────┘            └────────┬────────┘
                    │                              │                              │
           ┌────────▼────────┐            ┌────────▼────────┐            ┌────────▼────────┐
           │  Local Storage  │            │  Local Storage  │            │  Local Storage  │
           │    /data/node1  │            │    /data/node2  │            │    /data/node3  │
           └─────────────────┘            └─────────────────┘            └─────────────────┘
```

## Core Components

### 1. S3 API Layer

The S3 API layer handles all client requests using the Amazon S3 protocol.

**Responsibilities**:
- Parse and validate S3 requests
- AWS Signature V4 authentication
- Request routing to appropriate handlers
- Response formatting

**Port**: 9000

### 2. Admin API

The Admin API provides cluster management and monitoring capabilities.

**Responsibilities**:
- Cluster management (add/remove nodes)
- User and IAM management
- Bucket policy management
- Health checks and metrics

**Port**: 9001

### 3. Web Console

A browser-based interface for managing NebulaIO.

**Features**:
- Bucket management
- Object browsing and upload
- User management
- Cluster monitoring

**Port**: 9002

### 4. Dragonboat Consensus Layer

Handles distributed consensus for metadata operations using the Dragonboat library.

**Features**:
- Leader election
- Log replication with batching and pipelining
- Consistent reads and writes
- Automatic failover
- High performance: 1.25M writes/sec, 1.3ms latency
- Multi-Raft groups for horizontal scaling
- Efficient incremental snapshots

**Port**: 9003

### 5. Gossip Protocol (Memberlist)

Provides cluster membership and node discovery.

**Features**:
- Node discovery
- Failure detection
- Cluster state propagation
- Health checking

**Port**: 9004 (TCP/UDP)

### 6. Storage Backend

Handles object data storage.

**Supported Backends**:
- **Filesystem (fs)**: Local or network filesystem
- *Future*: S3 (for tiered storage)

---

## Data Flow

### Object Upload (PutObject)

```
Client                  Load Balancer           Node (Leader)           Storage
  │                          │                       │                     │
  │── PUT /bucket/key ──────►│                       │                     │
  │                          │── Route to node ─────►│                     │
  │                          │                       │── Validate auth ───►│
  │                          │                       │── Check bucket ────►│
  │                          │                       │── Replicate via ───►│
  │                          │                       │   Raft              │
  │                          │                       │── Write data ──────►│
  │                          │                       │◄── Success ─────────│
  │◄── 200 OK ───────────────│◄──────────────────────│                     │
```

### Object Download (GetObject)

```
Client                  Load Balancer           Any Node                Storage
  │                          │                       │                     │
  │── GET /bucket/key ──────►│                       │                     │
  │                          │── Route to node ─────►│                     │
  │                          │                       │── Validate auth ───►│
  │                          │                       │── Check metadata ──►│
  │                          │                       │── Read data ───────►│
  │                          │                       │◄── Stream data ─────│
  │◄── 200 OK + body ────────│◄──────────────────────│                     │
```

---

## Cluster Topology

### Single Node

Simple deployment for development or testing.

```
┌──────────────────────┐
│     NebulaIO         │
│  ┌────────────────┐  │
│  │  S3 + Admin +  │  │
│  │  Console +     │  │
│  │  Storage       │  │
│  └────────────────┘  │
│         │            │
│  ┌──────▼───────┐    │
│  │   Storage    │    │
│  └──────────────┘    │
└──────────────────────┘
```

### HA Cluster (3+ nodes)

Production deployment with high availability.

```
                    ┌───────────────┐
                    │ Load Balancer │
                    └───────┬───────┘
                            │
        ┌───────────────────┼───────────────────┐
        │                   │                   │
┌───────▼───────┐   ┌───────▼───────┐   ┌───────▼───────┐
│   Node 1      │   │   Node 2      │   │   Node 3      │
│   (Leader)    │◄─►│   (Follower)  │◄─►│   (Follower)  │
│               │   │               │   │               │
│  Raft: Voter  │   │  Raft: Voter  │   │  Raft: Voter  │
│  Role: Hybrid │   │  Role: Hybrid │   │  Role: Hybrid │
└───────────────┘   └───────────────┘   └───────────────┘
```

### Separated Planes (Large Scale)

For large deployments, separate management and storage planes.

```
                    ┌───────────────┐
                    │ Load Balancer │
                    └───────┬───────┘
                            │
    ┌───────────────────────┴───────────────────────┐
    │                                               │
    │              MANAGEMENT PLANE                 │
    │  ┌─────────┐   ┌─────────┐   ┌─────────┐      │
    │  │ Mgmt 1  │◄─►│ Mgmt 2  │◄─►│ Mgmt 3  │      │
    │  │ (Voter) │   │ (Voter) │   │ (Voter) │      │
    │  └─────────┘   └─────────┘   └─────────┘      │
    │                                               │
    └───────────────────────┬───────────────────────┘
                            │
    ┌───────────────────────┴───────────────────────┐
    │              STORAGE PLANE                    │
    │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ... N    │
    │  │Storage 1│ │Storage 2│ │Storage 3│          │
    │  │(Non-Vtr)│ │(Non-Vtr)│ │(Non-Vtr)│          │
    │  └─────────┘ └─────────┘ └─────────┘          │
    │                                               │
    └───────────────────────────────────────────────┘
```

**Management Plane**:
- 3, 5, or 7 nodes (odd number for Dragonboat quorum)
- Handles metadata operations
- Participates in Dragonboat consensus
- Small storage footprint
- Optimized for low-latency metadata access

**Storage Plane**:
- Any number of nodes
- Handles data storage
- Non-voting Dragonboat members
- Large storage capacity
- Optimized for high-throughput data operations

---

## Metadata Store

NebulaIO uses an embedded key-value store for metadata.

**Stored Data**:
- Bucket metadata
- Object metadata (keys, sizes, ETags)
- User and IAM data
- Access policies
- Cluster configuration

**Consistency**:
- Strong consistency via Dragonboat
- Linearizable reads available
- Eventual consistency for non-critical data
- Sub-millisecond metadata operations (1.3ms average)

---

## Storage Layout

```
/data
├── metadata/              # Dragonboat and metadata
│   ├── dragonboat/        # Dragonboat directories
│   │   ├── wal/           # Write-Ahead Log
│   │   ├── snapshots/     # State snapshots
│   │   └── nodehost/      # NodeHost state
│   └── kv/                # Key-value store
├── objects/               # Object data
│   └── {bucket}/
│       └── {key-hash}/
│           └── {version}
└── temp/                  # Temporary uploads
```

---

## Security Model

### Authentication

- **S3 API**: AWS Signature V4
- **Admin API**: JWT tokens
- **Console**: Session-based with JWT

### Authorization

- IAM policies (AWS-compatible)
- Bucket policies
- Access Control Lists (ACLs)

### Network Security

- TLS for all external communication
- mTLS for inter-node communication (optional)
- Network policies for Kubernetes

---

## Scalability

### Horizontal Scaling

- Add storage nodes to increase capacity
- All nodes can serve read requests
- Writes go through Raft leader

### Vertical Scaling

- Increase CPU for more concurrent requests
- Increase memory for larger metadata cache
- Increase storage for more data

### Limits

| Component | Default Limit | Configurable |
|-----------|---------------|--------------|
| Max object size | 5 GB | Yes |
| Max bucket count | Unlimited | No |
| Max objects per bucket | Unlimited | No |
| Max concurrent connections | 10,000 | Yes |

---

## Next Steps

- [Clustering & HA](clustering.md)
- [Storage Backend](storage.md)
- [Security Model](security.md)
