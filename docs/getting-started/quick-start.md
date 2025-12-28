# Quick Start Guide

Get NebulaIO running in minutes with this guide covering installation, configuration, and basic operations.

## Prerequisites

- **Go 1.24+** (for building from source)
- **Docker 20.10+** (optional, for containerized deployment)
- **AWS CLI** or **MinIO Client (mc)** (optional, for S3 operations)

---

## Installation

### Option 1: Download Binary Release

```bash
# Linux (amd64)
curl -LO https://github.com/piwi3910/nebulaio/releases/latest/download/nebulaio-linux-amd64.tar.gz
tar -xzf nebulaio-linux-amd64.tar.gz
sudo mv nebulaio /usr/local/bin/

# macOS (arm64)
curl -LO https://github.com/piwi3910/nebulaio/releases/latest/download/nebulaio-darwin-arm64.tar.gz
tar -xzf nebulaio-darwin-arm64.tar.gz
sudo mv nebulaio /usr/local/bin/

# Verify installation
nebulaio --version
```

### Option 2: Using Docker

```bash
docker run -d \
  --name nebulaio \
  -p 9000:9000 \
  -p 9001:9001 \
  -v nebulaio-data:/data \
  -e NEBULAIO_AUTH_ROOT_USER=admin \
  -e NEBULAIO_AUTH_ROOT_PASSWORD=changeme \
  ghcr.io/piwi3910/nebulaio:latest
```

### Option 3: Build from Source

```bash
git clone https://github.com/piwi3910/nebulaio.git
cd nebulaio
make build

# Binaries are in ./bin/
# nebulaio      - Main server
# nebulaio-cli  - Command-line client
```

---

## Starting NebulaIO

Run with default settings:

```bash
./bin/nebulaio
```

Default configuration:

- **S3 API**: `http://localhost:9000`
- **Admin API**: `http://localhost:9001`
- **Data directory**: `./data`
- **Credentials**: `admin` / `admin123`

Custom configuration via environment variables:

```bash
export NEBULAIO_DATA_DIR=/var/lib/nebulaio
export NEBULAIO_AUTH_ROOT_USER=admin
export NEBULAIO_AUTH_ROOT_PASSWORD=your-secure-password

./bin/nebulaio
```

---

## Verify Installation

```bash
# Health check
curl http://localhost:9001/health
# {"status":"healthy"}

# Detailed health status
curl http://localhost:9001/health/detailed

# Readiness probe
curl http://localhost:9001/health/ready
```

---

## Basic Usage with AWS CLI

Configure the AWS CLI:

```bash
aws configure set aws_access_key_id admin
aws configure set aws_secret_access_key admin123
aws configure set region us-east-1

# Create endpoint alias
alias nebulaio='aws --endpoint-url http://localhost:9000 s3'
```

### Create a Bucket and Upload an Object

```bash
# Create bucket
nebulaio mb s3://my-bucket

# Upload file
echo "Hello, NebulaIO!" > hello.txt
nebulaio cp hello.txt s3://my-bucket/

# List objects
nebulaio ls s3://my-bucket/

# Download file
nebulaio cp s3://my-bucket/hello.txt downloaded.txt
```

---

## Alternative: MinIO Client (mc)

```bash
# Configure mc
mc alias set nebulaio http://localhost:9000 admin admin123

# Basic operations
mc mb nebulaio/my-bucket
mc cp myfile.txt nebulaio/my-bucket/
mc ls nebulaio/my-bucket/
```

---

## Using the NebulaIO CLI

```bash
# Configure
./bin/nebulaio-cli config set endpoint http://localhost:9000
./bin/nebulaio-cli config set access-key admin
./bin/nebulaio-cli config set secret-key admin123

# Create bucket and upload
./bin/nebulaio-cli bucket create my-bucket
./bin/nebulaio-cli cp myfile.txt s3://my-bucket/
./bin/nebulaio-cli ls s3://my-bucket/
```

See the [CLI documentation](cli.md) for complete reference.

---

## Next Steps

### Deployment Guides

- [Docker Deployment](../deployment/docker.md) - Production Docker setup
- [Kubernetes Deployment](../deployment/kubernetes.md) - Cloud-native deployment
- [Standalone Deployment](../deployment/standalone.md) - Binary installation

### Configuration

- [Configuration Reference](configuration.md) - All configuration options
- [CLI Tool Guide](cli.md) - Command-line reference

### Advanced Features

- [Erasure Coding](../features/erasure-coding.md) - Data durability
- [Bucket Replication](../features/replication.md) - Cross-cluster replication
- [Object Lock](../features/object-lock.md) - WORM compliance
- [Event Notifications](../features/events.md) - Webhook and Kafka integration

### AI/ML Features

- [MCP Server](../features/mcp-server.md) - AI agent integration
- [GPUDirect Storage](../features/gpudirect.md) - GPU-accelerated data access
- [S3 Express](../features/s3-express.md) - Low-latency operations
