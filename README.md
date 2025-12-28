# NebulaIO

An S3-compatible object storage system with a full-featured web GUI, designed to scale from single-node deployments to distributed clusters. **100% feature parity with MinIO**, including all 2025 AI/ML features - all features included free.

## Features

### Core Features
- **Full S3 API Compatibility** - Works with existing S3 SDKs and tools (AWS CLI, boto3, etc.)
- **Web Console** - Modern React-based admin and user portals
- **Distributed Metadata** - Dragonboat consensus for high availability and ultra-low latency (1.3ms, 1.25M writes/sec)
- **Scalable Storage** - Start with local filesystem, use volume backend for high performance, or scale to erasure-coded distributed storage
- **IAM** - Users, groups, policies, and S3-compatible access keys
- **Multi-tenant** - Role-based access control with admin and user portals

### Advanced Features

#### Data Durability
- **Erasure Coding** - Reed-Solomon encoding with configurable data/parity shards (default: 10+4)
- **Data Compression** - Support for Zstandard, LZ4, and Gzip compression with automatic content-type detection

#### Replication
- **Bucket Replication** - Async replication with configurable rules and filtering
- **Site Replication** - Active-Active multi-datacenter sync with IAM/policy synchronization

#### Identity & Security
- **LDAP/Active Directory** - Identity integration with user sync and group-to-policy mapping
- **OIDC/SSO** - OpenID Connect support for single sign-on (Keycloak, Okta, Auth0, etc.)
- **KMS Integration** - Envelope encryption with HashiCorp Vault, AWS KMS, or local key management
- **Object Lock (WORM)** - GOVERNANCE and COMPLIANCE modes with legal hold support

#### Operations
- **Event Notifications** - Webhook, Kafka, RabbitMQ, NATS, Redis, AWS SNS/SQS targets
- **Storage Tiering** - Hot/Warm/Cold/Archive tiers with LRU caching and policy-based transitions
- **CLI Tool** - Full-featured command-line interface for all operations

#### S3 API Extensions
- **Object Versioning** - Full version management including delete markers
- **Lifecycle Policies** - Automated object expiration and transition rules
- **Multipart Uploads** - Large file support with resumable uploads
- **S3 Select** - SQL queries on CSV/JSON/Parquet files
- **GetObjectAttributes** - Efficient metadata retrieval without downloading content

### AI/ML & High-Performance Features (2025)

#### S3 Express One Zone
- **Atomic Appends** - Append data to objects for streaming workloads
- **Accelerated PUT/LIST** - Sub-millisecond latency for hot data
- **Directory Buckets** - Optimized for single-zone deployments

#### Apache Iceberg
- **Native Tables** - First-class Iceberg table format support
- **REST Catalog** - Standard Iceberg REST catalog API
- **ACID Transactions** - Full transaction support with snapshot isolation

#### MCP Server (Model Context Protocol)
- **AI Agent Integration** - Native support for Claude, ChatGPT, and other AI agents
- **Tool Execution** - Agents can read, write, and manage objects
- **Resource Access** - Secure access to bucket contents for AI workloads

#### GPUDirect Storage
- **Zero-Copy Transfers** - Direct GPU-to-storage data paths
- **NVIDIA GDS Support** - Bypass CPU for AI/ML training data loading
- **Multi-GPU** - Support for peer-to-peer GPU transfers

#### BlueField DPU Support
- **Crypto Offload** - AES-GCM encryption on NVIDIA SmartNIC
- **Compression Offload** - Hardware-accelerated Deflate/LZ4
- **Network Acceleration** - RDMA and storage offload via DPU

#### S3 over RDMA
- **Ultra-Low Latency** - Sub-10μs object access via RDMA
- **Zero-Copy** - Direct memory transfers between client and server
- **libibverbs Integration** - Full verbs API support for InfiniBand/RoCE

#### NVIDIA NIM Integration
- **Inference on Objects** - Run AI inference on stored data
- **Model Support** - LLM, Vision, Audio, Multimodal, Embedding models
- **Streaming** - Real-time inference with streaming responses

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Web Console                          │
│                    (React + Mantine)                        │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                     API Gateway (Go)                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │ S3 (:9000)  │  │Admin (:9001)│  │ MCP (:9005) RDMA    │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│              AI/ML Acceleration Layer                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │  GPUDirect  │  │ BlueField   │  │  NIM Inference      │ │
│  │   Storage   │  │    DPU      │  │   Microservices     │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│              Metadata Store (Dragonboat)                    │
│                    (BadgerDB backend)                       │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                  Storage Layer                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │ Filesystem  │  │   Volume    │  │   Erasure Coding    │ │
│  │   Backend   │  │   Backend   │  │      Backend        │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
│                                    ┌─────────────────────┐ │
│                                    │  S3 Express Zones   │ │
│                                    │  (Iceberg Tables)   │ │
│                                    └─────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.24+
- Node.js 20+
- Make

### Build

```bash
# Clone the repository
git clone https://github.com/piwi3910/nebulaio.git
cd nebulaio

# Build everything
make build

# Or build with web console
make web-build
make build
```

### Run

```bash
# Run with default settings
./bin/nebulaio

# Or with custom options
./bin/nebulaio --data ./data --s3-port 9000 --admin-port 9001 --debug
```

### Docker

```bash
# Build and run with Docker
docker-compose up -d

# Or build manually
docker build -t nebulaio:latest -f deployments/docker/Dockerfile .
docker run -p 9000:9000 -p 9001:9001 -v nebulaio-data:/data nebulaio:latest
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEBULAIO_DATA_DIR` | Data directory path | `./data` |
| `NEBULAIO_S3_PORT` | S3 API port | `9000` |
| `NEBULAIO_ADMIN_PORT` | Admin/Console API port | `9001` |
| `NEBULAIO_AUTH_ROOT_USER` | Initial admin username | `admin` |
| `NEBULAIO_AUTH_ROOT_PASSWORD` | Initial admin password | `admin123` |
| `NEBULAIO_LOG_LEVEL` | Log level (debug, info, warn, error) | `info` |

#### AI/ML Feature Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEBULAIO_S3_EXPRESS_ENABLED` | Enable S3 Express One Zone | `false` |
| `NEBULAIO_ICEBERG_ENABLED` | Enable Apache Iceberg support | `false` |
| `NEBULAIO_MCP_ENABLED` | Enable MCP Server for AI agents | `false` |
| `NEBULAIO_GPUDIRECT_ENABLED` | Enable GPUDirect Storage | `false` |
| `NEBULAIO_DPU_ENABLED` | Enable BlueField DPU offload | `false` |
| `NEBULAIO_RDMA_ENABLED` | Enable S3 over RDMA | `false` |
| `NEBULAIO_NIM_ENABLED` | Enable NVIDIA NIM integration | `false` |
| `NEBULAIO_NIM_API_KEY` | NVIDIA API key for NIM | `""` |

### Configuration File

```yaml
# nebulaio.yaml
node_name: node-1
data_dir: /data/nebulaio

s3_port: 9000
admin_port: 9001
console_port: 9002

cluster:
  bootstrap: true
  raft_port: 9003
  shard_id: 1
  replica_id: 1

storage:
  backend: erasure  # fs | erasure | volume
  compression: zstd  # none | zstd | lz4 | gzip
  default_storage_class: STANDARD
  erasure:
    data_shards: 10
    parity_shards: 4
  # volume:                           # High-performance block storage
  #   max_volume_size: 34359738368    # 32GB per volume file
  #   auto_create: true

auth:
  root_user: admin
  root_password: changeme

# AI/ML Features (2025)
s3_express:
  enabled: true
  default_zone: use1-az1
  enable_atomic_append: true

iceberg:
  enabled: true
  catalog_type: rest
  warehouse: s3://warehouse/
  enable_acid: true

mcp:
  enabled: true
  port: 9005
  enable_tools: true
  enable_resources: true

gpudirect:
  enabled: true
  buffer_pool_size: 1073741824  # 1GB
  enable_async: true

dpu:
  enabled: true
  enable_crypto: true
  enable_compression: true
  enable_rdma: true

rdma:
  enabled: true
  port: 9100
  device_name: mlx5_0
  enable_zero_copy: true

nim:
  enabled: true
  api_key: your-nvidia-api-key
  default_model: meta/llama-3.1-8b-instruct
  enable_streaming: true
```

## API Endpoints

### S3 API (Port 9000)

Standard S3-compatible API supporting:
- Bucket operations (CreateBucket, DeleteBucket, ListBuckets, etc.)
- Object operations (PutObject, GetObject, DeleteObject, CopyObject, etc.)
- S3 Express operations (CreateSession, atomic appends)
- Multipart uploads
- Versioning
- And more...

### Admin API (Port 9001)

RESTful API for system management:

```
POST   /api/v1/admin/auth/login       # Login
GET    /api/v1/admin/users            # List users
POST   /api/v1/admin/users            # Create user
GET    /api/v1/admin/buckets          # List buckets
POST   /api/v1/admin/buckets          # Create bucket
GET    /api/v1/admin/cluster/status   # Cluster status
```

### MCP Server (Port 9005)

Model Context Protocol for AI agent integration:

```
POST   /mcp                           # JSON-RPC 2.0 endpoint
       - tools/list                   # List available tools
       - tools/call                   # Execute a tool
       - resources/list               # List resources
       - resources/read               # Read resource content
```

### RDMA (Port 9100)

Ultra-low latency S3 access via RDMA:
- InfiniBand and RoCE v2 support
- Zero-copy data transfers
- Sub-10μs latency for small objects

## Using with AI Agents

### Claude Desktop / MCP

```json
{
  "mcpServers": {
    "nebulaio": {
      "command": "curl",
      "args": ["-X", "POST", "http://localhost:9005/mcp"]
    }
  }
}
```

### Python with NIM

```python
from nebulaio import NIMClient

client = NIMClient(
    endpoint="http://localhost:9000",
    nim_endpoint="https://integrate.api.nvidia.com/v1",
    api_key="your-nvidia-api-key"
)

# Run inference on stored image
result = client.infer_object(
    bucket="my-bucket",
    key="image.jpg",
    model="nvidia/grounding-dino",
    task="detection"
)
```

## Development

### Project Structure

```
nebulaio/
├── cmd/
│   ├── nebulaio/           # Main server binary
│   └── nebulaio-cli/       # CLI tool
├── internal/
│   ├── api/s3/             # S3 API handlers
│   ├── api/admin/          # Admin API handlers
│   ├── api/console/        # Console API handlers
│   ├── auth/               # Authentication & IAM
│   │   ├── ldap/           # LDAP provider
│   │   └── oidc/           # OIDC provider
│   ├── bucket/             # Bucket service
│   ├── object/             # Object service
│   ├── storage/            # Storage backends
│   │   ├── erasure/        # Erasure coding
│   │   ├── volume/         # High-performance volume storage
│   │   └── compression/    # Compression
│   ├── express/            # S3 Express One Zone
│   ├── iceberg/            # Apache Iceberg
│   ├── mcp/                # MCP Server
│   ├── gpudirect/          # GPUDirect Storage
│   ├── dpu/                # BlueField DPU
│   ├── transport/rdma/     # S3 over RDMA
│   ├── nim/                # NVIDIA NIM
│   ├── replication/        # Bucket & site replication
│   ├── kms/                # KMS providers
│   ├── events/             # Event notifications
│   ├── tiering/            # Storage tiering
│   ├── metadata/           # Raft + BadgerDB
│   └── config/             # Configuration
├── pkg/s3types/            # Public S3 types
├── web/                    # React frontend
├── deployments/            # Docker, K8s, Helm, Operator
├── examples/               # Usage examples
└── docs/                   # Documentation
```

### Running in Development

```bash
# Backend (with hot reload)
make dev

# Frontend (in separate terminal)
cd web && npm run dev
```

### Running Tests

```bash
make test
```

## Roadmap

- [x] Phase 1: Core S3 operations, basic IAM, web console
- [x] Phase 2: Multipart uploads, versioning, lifecycle policies
- [x] Phase 3: Multi-node clustering, erasure coding, data compression
- [x] Phase 4: Cross-cluster replication, encryption at rest (KMS integration)
- [x] Phase 5: Identity integration (LDAP, OIDC/SSO)
- [x] Phase 6: Event notifications, storage tiering, caching
- [x] Phase 7: Object Lock (WORM compliance), CLI tool
- [x] Phase 8: S3 Express, Iceberg, MCP Server, GPUDirect, DPU, RDMA, NIM

## MinIO Comparison

NebulaIO implements **100% feature parity** with MinIO, including all 2025 features. See [docs/MINIO_COMPARISON.md](docs/MINIO_COMPARISON.md) for detailed comparison.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please read our contributing guidelines before submitting PRs.
