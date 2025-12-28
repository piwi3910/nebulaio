# NebulaIO Quick Reference Guide

A single-page reference for common operations, ports, and configuration patterns.

## Network Ports

| Port | Service | Protocol | Access Level | Required |
|------|---------|----------|--------------|----------|
| 9000 | S3 API | HTTPS | Public/Internal | Yes |
| 9001 | Admin API | HTTPS | Internal Only | Yes |
| 9002 | Web Console | HTTPS | Internal Only | Optional |
| 9003 | Raft Consensus | TCP | Cluster Only | Cluster Mode |
| 9004 | Gossip/Discovery | UDP/TCP | Cluster Only | Cluster Mode |

## Configuration Patterns

### Minimal Development Setup

```yaml
# config.yaml - Development/Testing
server:
  data_dir: ./data
  s3_port: 9000
  admin_port: 9001

storage:
  backend: fs
  data_path: ./data/objects

auth:
  root_user: admin
  root_password: "${NEBULAIO_AUTH_ROOT_PASSWORD}"  # Required, min 12 chars

tls:
  enabled: true
  auto_generate: true
```

### Production HA Setup

```yaml
# config.yaml - Production with Clustering
server:
  data_dir: /var/lib/nebulaio
  s3_port: 9000
  admin_port: 9001
  console_port: 9002

cluster:
  enabled: true
  node_id: "node-1"
  peers:
    - "node-2:9003"
    - "node-3:9003"
  raft_port: 9003
  gossip_port: 9004

storage:
  backend: volume
  data_path: /var/lib/nebulaio/objects
  erasure:
    enabled: true
    data_shards: 4
    parity_shards: 2

cache:
  enabled: true
  max_size: 8589934592  # 8GB in bytes

auth:
  root_user: admin
  root_password: "${NEBULAIO_AUTH_ROOT_PASSWORD}"

tls:
  enabled: true
  cert_file: /etc/nebulaio/tls/server.crt
  key_file: /etc/nebulaio/tls/server.key
```

### High-Performance Setup

```yaml
# config.yaml - Maximum Performance
server:
  data_dir: /var/lib/nebulaio

storage:
  backend: volume
  data_path: /mnt/nvme/nebulaio
  direct_io: true
  sync_writes: false

cache:
  enabled: true
  max_size: 17179869184  # 16GB in bytes
  shard_count: 512
  prefetch_enabled: true

performance:
  max_connections: 10000
  read_buffer_size: 1048576   # 1MB
  write_buffer_size: 1048576  # 1MB
```

## Environment Variables

### Authentication

```bash
# Required - must be set before starting
export NEBULAIO_AUTH_ROOT_USER=admin
export NEBULAIO_AUTH_ROOT_PASSWORD="YourSecurePassword123!"  # Min 12 chars

# Optional - API keys
export NEBULAIO_AUTH_ACCESS_KEY=your-access-key
export NEBULAIO_AUTH_SECRET_KEY=your-secret-key
```

### TLS Configuration

```bash
# Auto-generate self-signed certificates (default)
export NEBULAIO_TLS_ENABLED=true
export NEBULAIO_TLS_AUTO_GENERATE=true

# Use custom certificates
export NEBULAIO_TLS_CERT_FILE=/path/to/cert.pem
export NEBULAIO_TLS_KEY_FILE=/path/to/key.pem
```

### Cluster Settings

```bash
export NEBULAIO_CLUSTER_ENABLED=true
export NEBULAIO_CLUSTER_NODE_ID=node-1
export NEBULAIO_CLUSTER_PEERS=node-2:9003,node-3:9003
export NEBULAIO_CLUSTER_RAFT_PORT=9003
export NEBULAIO_CLUSTER_GOSSIP_PORT=9004
```

### Feature Flags

```bash
# Advanced features (disabled by default)
export NEBULAIO_S3_EXPRESS_ENABLED=false
export NEBULAIO_ICEBERG_ENABLED=false
export NEBULAIO_MCP_ENABLED=false
export NEBULAIO_GPUDIRECT_ENABLED=false
export NEBULAIO_RDMA_ENABLED=false
```

## Common Commands

### Server Operations

```bash
# Start server with config file
./nebulaio --config config.yaml

# Start with debug logging
./nebulaio --config config.yaml --debug

# Start with specific ports
./nebulaio --s3-port 9000 --admin-port 9001 --data ./data
```

### Health & Monitoring

```bash
# Check cluster health
curl -k https://localhost:9001/health

# View Prometheus metrics
curl -k https://localhost:9001/metrics

# Check node status
curl -k https://localhost:9001/api/v1/status
```

### S3 Operations (using AWS CLI)

```bash
# Configure AWS CLI
aws configure set default.s3.signature_version s3v4

# Create bucket
aws --endpoint-url https://localhost:9000 s3 mb s3://my-bucket --no-verify-ssl

# Upload file
aws --endpoint-url https://localhost:9000 s3 cp file.txt s3://my-bucket/ --no-verify-ssl

# List objects
aws --endpoint-url https://localhost:9000 s3 ls s3://my-bucket/ --no-verify-ssl

# Download file
aws --endpoint-url https://localhost:9000 s3 cp s3://my-bucket/file.txt ./downloaded.txt --no-verify-ssl
```

## Storage Backend Selection

```
What is your use case?
│
├─► Development/Testing
│   └─► Use: backend: fs
│       Simple filesystem storage, no special requirements
│
├─► High Throughput Needed?
│   └─► Yes: Use: backend: volume
│       Pre-allocated volume files with Direct I/O support
│
├─► Data Durability Critical?
│   └─► Yes: Enable erasure coding
│       erasure:
│         enabled: true
│         data_shards: 4
│         parity_shards: 2
│
└─► General Production
    └─► Use: backend: volume with cache enabled
        Balanced performance and reliability
```

## Quick Troubleshooting

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| Connection refused on 9000 | Server not running | Check `./nebulaio` process |
| TLS certificate errors | Self-signed cert | Use `--no-verify-ssl` or add CA |
| "Access Denied" on S3 ops | Auth not configured | Set access/secret keys |
| Slow performance | Cache disabled | Enable cache, increase size |
| Cluster not forming | Firewall blocking ports | Open 9003/9004 between nodes |
| Out of memory | Cache too large | Reduce `cache.max_size` |

## Default Values Reference

| Setting | Default | Environment Variable |
|---------|---------|---------------------|
| S3 Port | 9000 | `NEBULAIO_S3_PORT` |
| Admin Port | 9001 | `NEBULAIO_ADMIN_PORT` |
| Console Port | 9002 | `NEBULAIO_CONSOLE_PORT` |
| Data Directory | ./data | `NEBULAIO_DATA_DIR` |
| Cache Size | 8GB | `NEBULAIO_CACHE_MAX_SIZE` |
| TLS Enabled | true | `NEBULAIO_TLS_ENABLED` |
| Max Object Size | 5TB | `NEBULAIO_MAX_OBJECT_SIZE` |
| Erasure Data Shards | 4 | `NEBULAIO_ERASURE_DATA_SHARDS` |
| Erasure Parity Shards | 2 | `NEBULAIO_ERASURE_PARITY_SHARDS` |

## Related Documentation

- [Configuration Guide](getting-started/configuration.md)
- [Deployment Options](deployment/)
- [Architecture Overview](architecture/overview.md)
- [Security Features](features/security-features.md)
- [Troubleshooting Guide](TROUBLESHOOTING.md)
