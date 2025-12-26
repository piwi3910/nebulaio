# NebulaIO

An S3-compatible object storage system with a full-featured web GUI, designed to scale from single-node deployments to distributed enterprise clusters.

## Features

- **Full S3 API Compatibility** - Works with existing S3 SDKs and tools (AWS CLI, boto3, etc.)
- **Web Console** - Modern React-based admin and user portals
- **Distributed Metadata** - Raft consensus for high availability (works single-node too)
- **Scalable Storage** - Start with local filesystem, scale to erasure-coded distributed storage
- **IAM** - Users, groups, policies, and S3-compatible access keys
- **Multi-tenant** - Role-based access control with admin and user portals

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
  backend: fs
  default_storage_class: STANDARD

auth:
  root_user: admin
  root_password: changeme
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
├── cmd/nebulaio/           # Main binary
├── internal/
│   ├── api/s3/             # S3 API handlers
│   ├── api/admin/          # Admin API handlers
│   ├── api/console/        # Console API handlers
│   ├── auth/               # Authentication & IAM
│   ├── bucket/             # Bucket service
│   ├── object/             # Object service
│   ├── storage/            # Storage backends
│   ├── metadata/           # Raft + BadgerDB
│   └── config/             # Configuration
├── pkg/s3types/            # Public S3 types
├── web/                    # React frontend
├── deployments/            # Docker, K8s
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
- [ ] Phase 2: Multipart uploads, versioning, lifecycle policies
- [ ] Phase 3: Multi-node clustering, erasure coding
- [ ] Phase 4: Cross-cluster replication, encryption at rest

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please read our contributing guidelines before submitting PRs.
