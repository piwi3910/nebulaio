# Standalone Deployment

This guide covers deploying NebulaIO as a standalone binary on bare metal or virtual machines.

## Prerequisites

- Linux, macOS, or Windows (amd64 or arm64)
- 2GB RAM minimum (4GB+ recommended for production)
- 10GB disk space minimum for system (plus storage for objects)
- Network access for client connections

## Installation Methods

### Method 1: Download Pre-built Binary

```bash
# Download latest release (Linux amd64)
curl -LO https://github.com/piwi3910/nebulaio/releases/latest/download/nebulaio-linux-amd64.tar.gz
tar -xzf nebulaio-linux-amd64.tar.gz
sudo mv nebulaio /usr/local/bin/

# Verify installation
nebulaio --version
```

Available binaries:

- `nebulaio-linux-amd64.tar.gz`
- `nebulaio-linux-arm64.tar.gz`
- `nebulaio-darwin-amd64.tar.gz`
- `nebulaio-darwin-arm64.tar.gz`
- `nebulaio-windows-amd64.zip`

### Method 2: Build from Source

```bash
# Prerequisites
# - Go 1.24+
# - Node.js 20+ (for web console)
# - Make

git clone https://github.com/piwi3910/nebulaio.git
cd nebulaio

# Build everything (backend + web console)
make all

# Binary is at ./bin/nebulaio
sudo cp bin/nebulaio /usr/local/bin/
```

---

## Single Node Deployment {#single-node}

### Quick Start

```bash
# Create data directory
sudo mkdir -p /var/lib/nebulaio
sudo chown $USER:$USER /var/lib/nebulaio

# Run with defaults
nebulaio --data /var/lib/nebulaio
```

### Configuration File

Create `/etc/nebulaio/config.yaml`:

```yaml
# Node Configuration
node_id: node-1
node_name: nebulaio-primary

# Data Storage
data_dir: /var/lib/nebulaio

# Network Ports
s3_port: 9000
admin_port: 9001
console_port: 9002

# Cluster (single-node mode)
cluster:
  bootstrap: true
  shard_id: 1
  replica_id: 1
  raft_port: 9003

# Storage
storage:
  backend: fs
  path: /var/lib/nebulaio/objects
  default_storage_class: STANDARD

# Authentication
auth:
  root_user: admin
  root_password: changeme  # CHANGE THIS!
  jwt_secret: ""  # Auto-generated if empty

# Logging
log_level: info
log_format: json
```

Run with config file:

```bash
nebulaio --config /etc/nebulaio/config.yaml
```

### Systemd Service

Create `/etc/systemd/system/nebulaio.service`:

```ini
[Unit]
Description=NebulaIO S3-Compatible Object Storage
Documentation=https://github.com/piwi3910/nebulaio
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=nebulaio
Group=nebulaio
ExecStart=/usr/local/bin/nebulaio --config /etc/nebulaio/config.yaml
Restart=always
RestartSec=5
LimitNOFILE=65535

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/nebulaio /var/log/nebulaio
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

Setup and start:

```bash
# Create user
sudo useradd -r -s /bin/false nebulaio
sudo mkdir -p /var/lib/nebulaio /var/log/nebulaio /etc/nebulaio
sudo chown -R nebulaio:nebulaio /var/lib/nebulaio /var/log/nebulaio

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable nebulaio
sudo systemctl start nebulaio

# Check status
sudo systemctl status nebulaio
journalctl -u nebulaio -f
```

---

## HA Cluster Deployment {#ha-cluster}

A minimum of 3 nodes is recommended for high availability (Raft consensus requires a majority).

### Network Requirements

| Port | Protocol | Purpose |
|------|----------|---------|
| 9000 | TCP | S3 API (client traffic) |
| 9001 | TCP | Admin/Console API |
| 9002 | TCP | Web Console (static files) |
| 9003 | TCP | Raft consensus (inter-node) |
| 9004 | TCP/UDP | Gossip protocol (node discovery) |

Ensure all nodes can communicate on ports 9003 and 9004.

### Node 1 (Bootstrap Node)

`/etc/nebulaio/config.yaml` on node1:

```yaml
node_id: node-1
node_name: nebulaio-node1
data_dir: /var/lib/nebulaio

s3_port: 9000
admin_port: 9001
console_port: 9002

cluster:
  bootstrap: true  # Only node1 bootstraps
  shard_id: 1
  replica_id: 1
  advertise_address: 10.0.1.10  # This node's IP
  raft_port: 9003
  gossip_port: 9004
  node_role: storage
  cluster_name: production-cluster
  expect_nodes: 3

storage:
  backend: fs
  path: /var/lib/nebulaio/objects

auth:
  root_user: admin
  root_password: your-secure-password-here
```

### Node 2 & Node 3 (Join Nodes)

`/etc/nebulaio/config.yaml` on node2:

```yaml
node_id: node-2
node_name: nebulaio-node2
data_dir: /var/lib/nebulaio

s3_port: 9000
admin_port: 9001
console_port: 9002

cluster:
  bootstrap: false
  shard_id: 1
  replica_id: 2  # Unique per node (3 for node-3)
  join_addresses:
    - 10.0.1.10:9004  # Node 1's gossip address
  advertise_address: 10.0.1.11  # This node's IP
  raft_port: 9003
  gossip_port: 9004
  node_role: storage
  cluster_name: production-cluster

storage:
  backend: fs
  path: /var/lib/nebulaio/objects

auth:
  root_user: admin
  root_password: your-secure-password-here
```

Node 3 is identical but with `node_id: node-3`, `advertise_address: 10.0.1.12`.

### Start Cluster

```bash
# Start node 1 first (bootstrap)
sudo systemctl start nebulaio  # on node1

# Wait for node1 to be healthy
curl http://10.0.1.10:9001/health/ready

# Start node 2 and 3
sudo systemctl start nebulaio  # on node2
sudo systemctl start nebulaio  # on node3
```

### Verify Cluster

```bash
# Check cluster status
curl http://10.0.1.10:9001/api/v1/admin/cluster/nodes | jq

# Check leader
curl http://10.0.1.10:9001/api/v1/admin/cluster/leader | jq

# Cluster health
curl http://10.0.1.10:9001/api/v1/admin/cluster/health | jq
```

---

## Production Deployment {#production}

### Load Balancer Setup

For production, place a load balancer in front of all nodes:

#### HAProxy Configuration

```haproxy
# /etc/haproxy/haproxy.cfg

frontend s3_frontend
    bind *:9000
    mode tcp
    default_backend s3_backend

frontend admin_frontend
    bind *:9001
    mode http
    default_backend admin_backend

backend s3_backend
    mode tcp
    balance roundrobin
    option tcp-check
    server node1 10.0.1.10:9000 check
    server node2 10.0.1.11:9000 check
    server node3 10.0.1.12:9000 check

backend admin_backend
    mode http
    balance roundrobin
    option httpchk GET /health/ready
    http-check expect status 200
    server node1 10.0.1.10:9001 check
    server node2 10.0.1.11:9001 check
    server node3 10.0.1.12:9001 check
```

#### Nginx Configuration

```nginx
# /etc/nginx/conf.d/nebulaio.conf

upstream nebulaio_s3 {
    least_conn;
    server 10.0.1.10:9000 max_fails=3 fail_timeout=30s;
    server 10.0.1.11:9000 max_fails=3 fail_timeout=30s;
    server 10.0.1.12:9000 max_fails=3 fail_timeout=30s;
}

upstream nebulaio_admin {
    least_conn;
    server 10.0.1.10:9001 max_fails=3 fail_timeout=30s;
    server 10.0.1.11:9001 max_fails=3 fail_timeout=30s;
    server 10.0.1.12:9001 max_fails=3 fail_timeout=30s;
}

server {
    listen 9000;

    location / {
        proxy_pass http://nebulaio_s3;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        client_max_body_size 5G;
    }
}

server {
    listen 9001;

    location / {
        proxy_pass http://nebulaio_admin;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }

    location /health {
        proxy_pass http://nebulaio_admin;
        access_log off;
    }
}
```

### TLS/SSL Configuration

For production, enable TLS:

```yaml
# config.yaml
tls:
  enabled: true
  cert_file: /etc/nebulaio/certs/server.crt
  key_file: /etc/nebulaio/certs/server.key
  # Optional: mutual TLS
  ca_file: /etc/nebulaio/certs/ca.crt
  client_auth: require  # none, request, require
```

Generate self-signed certificates (for testing):

```bash
# Generate CA
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 1825 -out ca.crt -subj "/CN=NebulaIO CA"

# Generate server certificate
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr -subj "/CN=nebulaio.local"
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 -sha256
```

### Resource Limits

Production configuration recommendations:

```yaml
# config.yaml
resources:
  max_connections: 10000
  max_request_body_size: 5GB
  request_timeout: 300s

performance:
  workers: 0  # 0 = auto (NumCPU)
  io_threads: 4

cache:
  metadata_cache_size: 10000
  metadata_cache_ttl: 60s
```

### Monitoring Setup

NebulaIO exposes Prometheus metrics at `/metrics` on the admin port:

```bash
curl http://localhost:9001/metrics
```

Prometheus scrape config:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'nebulaio'
    static_configs:
      - targets:
        - '10.0.1.10:9001'
        - '10.0.1.11:9001'
        - '10.0.1.12:9001'
    metrics_path: /metrics
```

Key metrics to monitor:

- `nebulaio_http_requests_total` - Request counts by endpoint
- `nebulaio_http_request_duration_seconds` - Request latency
- `nebulaio_storage_bytes_total` - Total storage used
- `nebulaio_objects_total` - Object count
- `nebulaio_raft_state` - Raft cluster state (leader/follower)

---

## Scaling

### Adding Nodes

To add a new node to an existing cluster:

1. Configure the new node with `bootstrap: false`
2. Add existing nodes to `join_addresses`
3. Start the new node
4. (Optional) Promote to voter: `POST /api/v1/admin/cluster/nodes`

### Removing Nodes

```bash
# Graceful removal
curl -X DELETE http://localhost:9001/api/v1/admin/cluster/nodes/node-4
```

### Separate Management and Storage Planes

For larger deployments, you can run separate management and storage nodes:

**Management Node** (Raft voter, no storage):

```yaml
node_role: management
cluster:
  voter: true
storage:
  enabled: false
```

**Storage Node** (Raft non-voter, storage only):

```yaml
node_role: storage
cluster:
  voter: false
storage:
  enabled: true
```

This allows scaling storage independently from the Raft consensus group.

---

## Troubleshooting

### Common Issues

**Node won't join cluster**

```bash
# Check gossip connectivity
nc -zv <bootstrap-node> 9004

# Check logs
journalctl -u nebulaio -f

# Verify cluster name matches
grep cluster_name /etc/nebulaio/config.yaml
```

**Leader election timeout**

```bash
# Check Raft port connectivity
nc -zv <other-node> 9003

# Increase election timeout for high-latency networks
# config.yaml:
cluster:
  raft_election_timeout: 5s
  raft_heartbeat_timeout: 2s
```

**Split-brain scenario**

- Ensure you have an odd number of voters (3, 5, 7)
- Check network partitions between nodes
- Never manually edit Raft state

### Log Levels

```bash
# Enable debug logging
nebulaio --config /etc/nebulaio/config.yaml --log-level debug

# Or in config.yaml
log_level: debug
```

---

## Next Steps

- [Monitoring Setup](../operations/monitoring.md)
- [Backup & Recovery](../operations/backup.md)
- [S3 API Reference](../api/s3-compatibility.md)
