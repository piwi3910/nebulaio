# NebulaIO

An S3-compatible object storage system with a full-featured web GUI, designed to scale from single-node deployments to distributed enterprise clusters.

## Features

### Core Features
- **Full S3 API Compatibility** - Works with existing S3 SDKs and tools (AWS CLI, boto3, etc.)
- **Web Console** - Modern React-based admin and user portals
- **Distributed Metadata** - Raft consensus for high availability (works single-node too)
- **Scalable Storage** - Start with local filesystem, scale to erasure-coded distributed storage
- **IAM** - Users, groups, policies, and S3-compatible access keys
- **Multi-tenant** - Role-based access control with admin and user portals

### Enterprise Features

#### Data Durability
- **Erasure Coding** - Reed-Solomon encoding with configurable data/parity shards (default: 10+4)
- **Data Compression** - Support for Zstandard, LZ4, and Gzip compression with automatic content-type detection

#### Replication
- **Bucket Replication** - Async replication with configurable rules and filtering
- **Site Replication** - Active-Active multi-datacenter sync with IAM/policy synchronization

#### Identity & Security
- **LDAP/Active Directory** - Enterprise identity integration with user sync and group-to-policy mapping
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

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Web Console                          │
│                    (React + Mantine)                        │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                     API Gateway (Go)                        │
│  ┌─────────────────────┐  ┌─────────────────────┐          │
│  │   S3 API (:9000)    │  │  Admin API (:9001)  │          │
│  └─────────────────────┘  └─────────────────────┘          │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                  Metadata Store (Raft)                      │
│                    (BadgerDB backend)                       │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                  Storage Layer                              │
│           (Filesystem / Erasure Coding)                     │
└─────────────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.23+
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

storage:
  backend: erasure  # fs | erasure
  compression: zstd  # none | zstd | lz4 | gzip
  default_storage_class: STANDARD
  erasure:
    data_shards: 10
    parity_shards: 4

auth:
  root_user: admin
  root_password: changeme

# LDAP Authentication (optional)
identity:
  ldap:
    enabled: false
    server_url: ldap://ldap.example.com:389
    bind_dn: cn=admin,dc=example,dc=com
    bind_password: secret
    user_search_base: ou=users,dc=example,dc=com
    user_search_filter: "(uid={username})"
    group_search_base: ou=groups,dc=example,dc=com
    tls: false
  oidc:
    enabled: false
    issuer_url: https://auth.example.com
    client_id: nebulaio
    client_secret: secret
    redirect_url: http://localhost:9001/callback
    scopes: ["openid", "profile", "email"]

# KMS Integration (optional)
kms:
  provider: local  # local | vault | aws | gcp | azure
  vault:
    address: https://vault.example.com:8200
    token: hvs.xxxxx
    mount: transit
    key_name: nebulaio-master

# Replication (optional)
replication:
  enabled: true
  sites:
    - name: site2
      endpoint: https://site2.example.com:9000
      access_key: AKIAXXXXXXXX
      secret_key: xxxxx

# Event Notifications (optional)
events:
  targets:
    - name: webhook1
      type: webhook
      url: https://events.example.com/s3
    - name: kafka1
      type: kafka
      brokers: ["kafka1:9092", "kafka2:9092"]
      topic: s3-events

# Storage Tiering (optional)
tiering:
  enabled: true
  hot_cache_size: 10GB
  cold_storage:
    type: s3
    endpoint: https://s3.amazonaws.com
    bucket: nebulaio-cold-tier
```

## API Endpoints

### S3 API (Port 9000)

Standard S3-compatible API supporting:
- Bucket operations (CreateBucket, DeleteBucket, ListBuckets, etc.)
- Object operations (PutObject, GetObject, DeleteObject, CopyObject, etc.)
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

### Console API (Port 9001)

User-facing API for self-service:

```
GET    /api/v1/console/me             # Current user
GET    /api/v1/console/buckets        # My buckets
GET    /api/v1/console/me/keys        # My access keys
```

## Using the NebulaIO CLI

NebulaIO includes a native CLI tool (`nebulaio-cli`) for managing storage:

```bash
# Build the CLI
make build-cli

# Configure
nebulaio-cli config set endpoint http://localhost:9000
nebulaio-cli config set access-key YOUR_ACCESS_KEY
nebulaio-cli config set secret-key YOUR_SECRET_KEY

# Bucket operations
nebulaio-cli bucket list                    # List all buckets
nebulaio-cli bucket create my-bucket        # Create bucket
nebulaio-cli bucket delete my-bucket        # Delete bucket
nebulaio-cli mb s3://my-bucket              # Create (short form)
nebulaio-cli rb s3://my-bucket              # Delete (short form)

# Object operations
nebulaio-cli object put file.txt s3://my-bucket/file.txt    # Upload
nebulaio-cli object get s3://my-bucket/file.txt ./local.txt # Download
nebulaio-cli object list s3://my-bucket                     # List objects
nebulaio-cli cp file.txt s3://my-bucket/                    # Copy (short form)
nebulaio-cli ls s3://my-bucket                              # List (short form)
nebulaio-cli rm s3://my-bucket/file.txt                     # Remove (short form)
nebulaio-cli cat s3://my-bucket/file.txt                    # View contents

# Admin operations
nebulaio-cli admin versioning enable my-bucket    # Enable versioning
nebulaio-cli admin versioning status my-bucket    # Check versioning status
nebulaio-cli admin policy set my-bucket policy.json # Set bucket policy
nebulaio-cli admin lifecycle set my-bucket rules.json # Set lifecycle rules
nebulaio-cli admin replication set my-bucket config.json # Set replication
```

## Using with AWS CLI

```bash
# Configure credentials
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY

# Use NebulaIO
aws --endpoint-url http://localhost:9000 s3 ls
aws --endpoint-url http://localhost:9000 s3 mb s3://my-bucket
aws --endpoint-url http://localhost:9000 s3 cp file.txt s3://my-bucket/
```

## Using with Python (boto3)

```python
import boto3

s3 = boto3.client(
    's3',
    endpoint_url='http://localhost:9000',
    aws_access_key_id='YOUR_ACCESS_KEY',
    aws_secret_access_key='YOUR_SECRET_KEY'
)

# List buckets
response = s3.list_buckets()
for bucket in response['Buckets']:
    print(bucket['Name'])
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
│   ├── object/             # Object service (with retention/lock)
│   ├── storage/            # Storage backends
│   │   ├── erasure/        # Erasure coding
│   │   └── compression/    # Compression (zstd, lz4, gzip)
│   ├── replication/        # Bucket & site replication
│   │   └── site/           # Multi-datacenter sync
│   ├── kms/                # KMS providers (vault, aws, gcp, azure, local)
│   ├── encryption/         # Envelope encryption
│   ├── events/             # Event notifications
│   │   └── targets/        # Webhook, Kafka, AMQP, NATS, Redis, AWS
│   ├── tiering/            # Storage tiering & caching
│   ├── metadata/           # Raft + BadgerDB
│   └── config/             # Configuration
├── pkg/s3types/            # Public S3 types
├── web/                    # React frontend
├── deployments/            # Docker, K8s, Helm, Operator
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
- [x] Phase 5: Enterprise identity (LDAP, OIDC/SSO)
- [x] Phase 6: Event notifications, storage tiering, caching
- [x] Phase 7: Object Lock (WORM compliance), CLI tool
- [ ] Phase 8: S3 Select for Parquet, advanced analytics

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please read our contributing guidelines before submitting PRs.
