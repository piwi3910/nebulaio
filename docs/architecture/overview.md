# Architecture Overview

NebulaIO is a distributed, S3-compatible object storage system designed for high availability, scalability, and performance. It provides 100% feature parity with MinIO, including all 2025 AI/ML features.

## High-Level Architecture

```mermaid
flowchart TB
    subgraph LB[Load Balancer]
        lb[HAProxy / Nginx / Cloud LB]
    end

    subgraph Cluster[NebulaIO Cluster]
        subgraph Node1[Node 1]
            s3_1[S3 API :9000]
            admin_1[Admin :9001]
            console_1[Console :9002]
            raft_1[Raft :9003]
        end
        subgraph Node2[Node 2]
            s3_2[S3 API :9000]
            admin_2[Admin :9001]
            console_2[Console :9002]
            raft_2[Raft :9003]
        end
        subgraph Node3[Node 3]
            s3_3[S3 API :9000]
            admin_3[Admin :9001]
            console_3[Console :9002]
            raft_3[Raft :9003]
        end
    end

    subgraph Storage[Storage Layer]
        store1[(Node 1 Storage)]
        store2[(Node 2 Storage)]
        store3[(Node 3 Storage)]
    end

    lb --> s3_1 & s3_2 & s3_3
    raft_1 <-.->|Gossip :9004| raft_2 <-.->|Gossip :9004| raft_3
    raft_1 <-.-> raft_3
    Node1 --> store1
    Node2 --> store2
    Node3 --> store3
```

### ASCII Diagram (for terminal viewing)

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
           │  Console :9002  │            │  Console :9002  │            │  Console :9002  │
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

### 6. Storage Backends

Handles object data storage with multiple backend options.

**Supported Backends**:
- **Filesystem (fs)**: Local or network filesystem (default)
- **Volume**: High-performance block-based storage with pre-allocated volume files and Direct I/O
- **Erasure Coding**: Reed-Solomon distributed storage with configurable data/parity shards

**Additional Capabilities**:
- **Compression**: Zstandard, LZ4, or Gzip compression with automatic content-type detection
- **Storage Tiering**: Hot/Warm/Cold/Archive tiers with policy-based transitions
- **Raw Device Access**: Direct block device access for maximum performance

---

## Data Flow

### Object Upload (PutObject)

```mermaid
sequenceDiagram
    participant C as Client
    participant LB as Load Balancer
    participant N as Node (Leader)
    participant R as Raft Cluster
    participant S as Storage

    C->>LB: PUT /bucket/key
    LB->>N: Route request
    N->>N: Validate auth (SigV4)
    N->>N: Check bucket exists
    N->>R: Replicate metadata
    R-->>N: Consensus achieved
    N->>S: Write object data
    S-->>N: Success
    N-->>LB: 200 OK
    LB-->>C: 200 OK
```

### Object Download (GetObject)

```mermaid
sequenceDiagram
    participant C as Client
    participant LB as Load Balancer
    participant N as Any Node
    participant Cache as DRAM Cache
    participant S as Storage

    C->>LB: GET /bucket/key
    LB->>N: Route request
    N->>N: Validate auth (SigV4)
    N->>Cache: Check cache
    alt Cache Hit
        Cache-->>N: Return cached data
    else Cache Miss
        N->>S: Read from storage
        S-->>N: Stream data
        N->>Cache: Update cache
    end
    N-->>LB: 200 OK + body
    LB-->>C: Stream response
```

### ASCII Diagrams (for terminal viewing)

**Object Upload:**
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

**Object Download:**
```
Client                  Load Balancer           Any Node                Storage
  │                          │                       │                     │
  │── GET /bucket/key ──────►│                       │                     │
  │                          │── Route to node ─────►│                     │
  │                          │                       │── Validate auth ───►│
  │                          │                       │── Check cache ─────►│
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
│   └── kv/                # Key-value store (BadgerDB)
├── objects/               # Object data (Filesystem backend)
│   └── {bucket}/
│       └── {key-hash}/
│           └── {version}
├── volumes/               # Volume backend data
│   ├── hot/               # Hot tier (NVMe)
│   ├── warm/              # Warm tier (SSD)
│   └── cold/              # Cold tier (HDD)
├── erasure/               # Erasure coding shards
│   └── {shard-id}/
├── certs/                 # TLS certificates (auto-generated)
│   ├── ca.crt
│   ├── server.crt
│   └── server.key
├── audit/                 # Audit logs
│   └── audit.log
├── iceberg/               # Iceberg table metadata
└── temp/                  # Temporary uploads
```

---

## Security Model

NebulaIO is **secure by default** with TLS enabled for all communications.

### Authentication

- **S3 API**: AWS Signature V4 (presigned URLs supported)
- **Admin API**: JWT tokens with refresh token rotation
- **Console**: Session-based with JWT
- **LDAP**: Active Directory / LDAP integration
- **OIDC/SSO**: OpenID Connect (Keycloak, Okta, Auth0, etc.)

### Authorization

- IAM policies (AWS-compatible policy language)
- Bucket policies
- Access Control Lists (ACLs)
- Object Lock (WORM) with Governance and Compliance modes

### Network Security

- **TLS**: Enabled by default for all external communication (HTTPS)
- **mTLS**: Optional for inter-node cluster communication
- **Network policies**: Kubernetes NetworkPolicy support
- **Data Firewall**: QoS, rate limiting, and bandwidth throttling

### Data Protection

- **Encryption at Rest**: KMS integration (HashiCorp Vault, AWS KMS, local)
- **Key Rotation**: Automated encryption key lifecycle management
- **DLP**: Data Loss Prevention with PII detection
- **Secret Scanning**: Detect credentials in stored objects

### Compliance

- **Audit Logging**: SOC2, PCI DSS, HIPAA, GDPR, FedRAMP compliant
- **Integrity Verification**: HMAC-based log integrity
- **Access Analytics**: Real-time anomaly detection

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

## AI/ML Architecture (2025)

NebulaIO includes advanced AI/ML capabilities for high-performance workloads.

```mermaid
flowchart TB
    subgraph API[API Endpoints]
        s3[S3 API :9000]
        admin[Admin API :9001]
        mcp[MCP Server :9005]
        rdma[RDMA :9100]
        iceberg_api[Iceberg Catalog :9006]
    end

    subgraph ML[AI/ML Acceleration]
        gpudirect[GPUDirect Storage]
        dpu[BlueField DPU]
        nim[NVIDIA NIM]
    end

    subgraph Features[Advanced Features]
        express[S3 Express One Zone]
        iceberg[Apache Iceberg Tables]
        tiering[Storage Tiering]
        cache[DRAM Cache]
    end

    subgraph Storage[Storage Layer]
        fs[Filesystem]
        volume[Volume Backend]
        erasure[Erasure Coding]
    end

    s3 --> gpudirect
    s3 --> express
    mcp --> nim
    rdma --> gpudirect

    gpudirect --> dpu
    dpu --> volume

    express --> cache
    iceberg --> tiering
    tiering --> fs & volume & erasure
```

### AI/ML Features

| Feature | Port | Description |
|---------|------|-------------|
| **S3 Express One Zone** | 9000 | Sub-millisecond latency, atomic appends |
| **Apache Iceberg** | 9006 | ACID transactions, REST catalog |
| **MCP Server** | 9005 | AI agent integration (Claude, ChatGPT) |
| **GPUDirect Storage** | - | Zero-copy GPU-to-storage transfers |
| **BlueField DPU** | - | Hardware offload for crypto/compression |
| **S3 over RDMA** | 9100 | Sub-10μs latency via InfiniBand/RoCE |
| **NVIDIA NIM** | - | AI inference on stored objects |

---

## Complete Component Diagram

```mermaid
flowchart TB
    subgraph Clients[Clients]
        awscli[AWS CLI]
        sdk[S3 SDKs]
        console[Web Console]
        ai[AI Agents]
    end

    subgraph Gateway[API Gateway]
        subgraph Middleware[Middleware Layer]
            auth[Auth/SigV4]
            firewall[Data Firewall]
            audit[Audit Logger]
            metrics[Prometheus Metrics]
        end

        s3api[S3 API Handler]
        adminapi[Admin API Handler]
        mcphandler[MCP Handler]
    end

    subgraph Core[Core Services]
        bucket[Bucket Service]
        object[Object Service]
        iam[IAM Service]
        replication[Replication Service]
        lifecycle[Lifecycle Manager]
        tiering[Tiering Service]
        events[Event Emitter]
    end

    subgraph Consensus[Distributed Consensus]
        dragonboat[Dragonboat Raft]
        badger[(BadgerDB)]
        gossip[Memberlist Gossip]
    end

    subgraph Storage[Storage Backends]
        fs[Filesystem Backend]
        vol[Volume Backend]
        erasure[Erasure Coding]
        compression[Compression Layer]
    end

    subgraph Acceleration[Hardware Acceleration]
        gpudirect[GPUDirect]
        dpu[BlueField DPU]
        rdma[RDMA Transport]
    end

    Clients --> Gateway
    Gateway --> Middleware
    Middleware --> s3api & adminapi & mcphandler
    s3api --> Core
    Core --> Consensus
    Core --> Storage
    Consensus --> badger
    Storage --> Acceleration
```

---

## Network Ports Summary

| Port | Service | Protocol | Description |
|------|---------|----------|-------------|
| 9000 | S3 API | HTTPS | S3-compatible object storage API |
| 9001 | Admin API | HTTPS | Management REST API + Prometheus metrics |
| 9002 | Console | HTTPS | Web UI static files |
| 9003 | Raft | TCP | Dragonboat consensus protocol |
| 9004 | Gossip | TCP/UDP | Memberlist node discovery |
| 9005 | MCP | HTTPS | Model Context Protocol for AI agents |
| 9006 | Iceberg | HTTPS | Iceberg REST catalog |
| 9100 | RDMA | RDMA | Ultra-low latency object access |

---

## Next Steps

- [Clustering & HA](clustering.md)
- [Storage Backend](storage.md)
- [Security Model](security.md)
- [Advanced Features](advanced-features.md)
