# Docker Deployment

This guide covers deploying NebulaIO using Docker and Docker Compose.

## Prerequisites

- Docker 20.10+
- Docker Compose 2.0+ (for multi-container deployments)
- 2GB RAM minimum per container
- Persistent storage volume

## Quick Start

```bash

# Pull and run
docker run -d \
  --name nebulaio \
  -p 9000:9000 \
  -p 9001:9001 \
  -p 9002:9002 \
  -v nebulaio-data:/data \
  -e NEBULAIO_AUTH_ROOT_PASSWORD=YourSecurePass123 \
  ghcr.io/piwi3910/nebulaio:latest

```text

---

## Single Node Deployment {#single-node}

### Using Docker Run

```bash

# Create data volume
docker volume create nebulaio-data

# Run NebulaIO
docker run -d \
  --name nebulaio \
  --restart unless-stopped \
  -p 9000:9000 \
  -p 9001:9001 \
  -p 9002:9002 \
  -v nebulaio-data:/data \
  -e NEBULAIO_AUTH_ROOT_USER=admin \
  -e NEBULAIO_AUTH_ROOT_PASSWORD=YourSecurePass123 \  # min 12 chars, uppercase, lowercase, number
  -e NEBULAIO_LOG_LEVEL=info \
  ghcr.io/piwi3910/nebulaio:latest

```bash

### Using Docker Compose

Create `docker-compose.yml`:

```yaml

version: '3.8'

services:
  nebulaio:
    image: ghcr.io/piwi3910/nebulaio:latest
    container_name: nebulaio
    restart: unless-stopped
    ports:
      - "9000:9000"  # S3 API
      - "9001:9001"  # Admin/Console API
      - "9002:9002"  # Web Console
    volumes:
      - nebulaio-data:/data
    environment:
      - NEBULAIO_DATA_DIR=/data
      - NEBULAIO_AUTH_ROOT_USER=admin
      - NEBULAIO_AUTH_ROOT_PASSWORD=YourSecurePass123
      - NEBULAIO_LOG_LEVEL=info
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:9001/health/ready"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s

volumes:
  nebulaio-data:
    driver: local

```text

Run:

```bash

docker-compose up -d
docker-compose logs -f

```bash

### With Custom Configuration

Mount a configuration file:

```yaml

version: '3.8'

services:
  nebulaio:
    image: ghcr.io/piwi3910/nebulaio:latest
    container_name: nebulaio
    restart: unless-stopped
    ports:
      - "9000:9000"
      - "9001:9001"
      - "9002:9002"
    volumes:
      - nebulaio-data:/data
      - ./config.yaml:/etc/nebulaio/config.yaml:ro
    command: ["--config", "/etc/nebulaio/config.yaml"]
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:9001/health/ready"]
      interval: 30s
      timeout: 10s
      retries: 3

volumes:
  nebulaio-data:
    driver: local

```text

---

## HA Cluster Deployment {#ha-cluster}

### 3-Node Cluster with Docker Compose

Create `docker-compose.cluster.yml`:

```yaml

version: '3.8'

# 3-Node NebulaIO HA Cluster
#
# Access Points:
# - Node 1: S3=localhost:9000, Admin=localhost:9001
# - Node 2: S3=localhost:9100, Admin=localhost:9101
# - Node 3: S3=localhost:9200, Admin=localhost:9201

services:
  nebulaio-1:
    image: ghcr.io/piwi3910/nebulaio:latest
    container_name: nebulaio-1
    hostname: nebulaio-1
    restart: unless-stopped
    ports:
      - "9000:9000"
      - "9001:9001"
      - "9002:9002"
    volumes:
      - nebulaio-data-1:/data
    environment:
      - NEBULAIO_NODE_ID=node-1
      - NEBULAIO_NODE_NAME=nebulaio-1
      - NEBULAIO_DATA_DIR=/data
      - NEBULAIO_AUTH_ROOT_USER=admin
      - NEBULAIO_AUTH_ROOT_PASSWORD=YourSecurePass123
      - NEBULAIO_CLUSTER_BOOTSTRAP=true
      - NEBULAIO_CLUSTER_ADVERTISE_ADDRESS=nebulaio-1
      - NEBULAIO_CLUSTER_RAFT_PORT=9003
      - NEBULAIO_CLUSTER_GOSSIP_PORT=9004
      - NEBULAIO_CLUSTER_NODE_ROLE=storage
      - NEBULAIO_CLUSTER_CLUSTER_NAME=nebulaio-cluster
      - NEBULAIO_CLUSTER_EXPECT_NODES=3
    networks:
      nebulaio-cluster:
        aliases:
          - nebulaio-1
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:9001/health/ready"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s

  nebulaio-2:
    image: ghcr.io/piwi3910/nebulaio:latest
    container_name: nebulaio-2
    hostname: nebulaio-2
    restart: unless-stopped
    ports:
      - "9100:9000"
      - "9101:9001"
      - "9102:9002"
    volumes:
      - nebulaio-data-2:/data
    environment:
      - NEBULAIO_NODE_ID=node-2
      - NEBULAIO_NODE_NAME=nebulaio-2
      - NEBULAIO_DATA_DIR=/data
      - NEBULAIO_AUTH_ROOT_USER=admin
      - NEBULAIO_AUTH_ROOT_PASSWORD=YourSecurePass123
      - NEBULAIO_CLUSTER_BOOTSTRAP=false
      - NEBULAIO_CLUSTER_JOIN_ADDRESSES=nebulaio-1:9004
      - NEBULAIO_CLUSTER_ADVERTISE_ADDRESS=nebulaio-2
      - NEBULAIO_CLUSTER_RAFT_PORT=9003
      - NEBULAIO_CLUSTER_GOSSIP_PORT=9004
      - NEBULAIO_CLUSTER_NODE_ROLE=storage
      - NEBULAIO_CLUSTER_CLUSTER_NAME=nebulaio-cluster
    networks:
      nebulaio-cluster:
        aliases:
          - nebulaio-2
    depends_on:
      nebulaio-1:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:9001/health/ready"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s

  nebulaio-3:
    image: ghcr.io/piwi3910/nebulaio:latest
    container_name: nebulaio-3
    hostname: nebulaio-3
    restart: unless-stopped
    ports:
      - "9200:9000"
      - "9201:9001"
      - "9202:9002"
    volumes:
      - nebulaio-data-3:/data
    environment:
      - NEBULAIO_NODE_ID=node-3
      - NEBULAIO_NODE_NAME=nebulaio-3
      - NEBULAIO_DATA_DIR=/data
      - NEBULAIO_AUTH_ROOT_USER=admin
      - NEBULAIO_AUTH_ROOT_PASSWORD=YourSecurePass123
      - NEBULAIO_CLUSTER_BOOTSTRAP=false
      - NEBULAIO_CLUSTER_JOIN_ADDRESSES=nebulaio-1:9004
      - NEBULAIO_CLUSTER_ADVERTISE_ADDRESS=nebulaio-3
      - NEBULAIO_CLUSTER_RAFT_PORT=9003
      - NEBULAIO_CLUSTER_GOSSIP_PORT=9004
      - NEBULAIO_CLUSTER_NODE_ROLE=storage
      - NEBULAIO_CLUSTER_CLUSTER_NAME=nebulaio-cluster
    networks:
      nebulaio-cluster:
        aliases:
          - nebulaio-3
    depends_on:
      nebulaio-1:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:9001/health/ready"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s

volumes:
  nebulaio-data-1:
  nebulaio-data-2:
  nebulaio-data-3:

networks:
  nebulaio-cluster:
    driver: bridge
    ipam:
      config:
        - subnet: 172.28.0.0/16

```text

Run:

```bash

docker-compose -f docker-compose.cluster.yml up -d

# Watch logs
docker-compose -f docker-compose.cluster.yml logs -f

# Check cluster status
curl http://localhost:9001/api/v1/admin/cluster/nodes | jq
curl http://localhost:9001/api/v1/admin/cluster/health | jq

```text

---

## Production Deployment {#production}

### With Traefik (Reverse Proxy + TLS)

```yaml

version: '3.8'

services:
  traefik:
    image: traefik:v2.10
    container_name: traefik
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
      - "9000:9000"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./traefik:/etc/traefik
      - ./certs:/certs
    command:
      - "--api.dashboard=true"
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      - "--entrypoints.s3.address=:9000"
      - "--certificatesresolvers.letsencrypt.acme.email=admin@example.com"
      - "--certificatesresolvers.letsencrypt.acme.storage=/certs/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web"

  nebulaio-1:
    image: ghcr.io/piwi3910/nebulaio:latest
    container_name: nebulaio-1
    restart: unless-stopped
    volumes:
      - nebulaio-data-1:/data
    environment:
      - NEBULAIO_NODE_ID=node-1
      - NEBULAIO_CLUSTER_BOOTSTRAP=true
      - NEBULAIO_CLUSTER_ADVERTISE_ADDRESS=nebulaio-1
      - NEBULAIO_CLUSTER_CLUSTER_NAME=production
      - NEBULAIO_AUTH_ROOT_PASSWORD=${ADMIN_PASSWORD}
    labels:
      - "traefik.enable=true"
      # S3 API
      - "traefik.http.routers.nebulaio-s3.rule=Host(`s3.example.com`)"
      - "traefik.http.routers.nebulaio-s3.entrypoints=s3"
      - "traefik.http.routers.nebulaio-s3.service=nebulaio-s3"
      - "traefik.http.services.nebulaio-s3.loadbalancer.server.port=9000"
      # Admin API
      - "traefik.http.routers.nebulaio-admin.rule=Host(`admin.example.com`)"
      - "traefik.http.routers.nebulaio-admin.entrypoints=websecure"
      - "traefik.http.routers.nebulaio-admin.tls.certresolver=letsencrypt"
      - "traefik.http.routers.nebulaio-admin.service=nebulaio-admin"
      - "traefik.http.services.nebulaio-admin.loadbalancer.server.port=9001"
    networks:
      - traefik-net
      - nebulaio-cluster

  nebulaio-2:
    image: ghcr.io/piwi3910/nebulaio:latest
    container_name: nebulaio-2
    restart: unless-stopped
    volumes:
      - nebulaio-data-2:/data
    environment:
      - NEBULAIO_NODE_ID=node-2
      - NEBULAIO_CLUSTER_BOOTSTRAP=false
      - NEBULAIO_CLUSTER_JOIN_ADDRESSES=nebulaio-1:9004
      - NEBULAIO_CLUSTER_ADVERTISE_ADDRESS=nebulaio-2
      - NEBULAIO_AUTH_ROOT_PASSWORD=${ADMIN_PASSWORD}
    labels:
      - "traefik.enable=true"
      - "traefik.http.services.nebulaio-s3.loadbalancer.server.port=9000"
      - "traefik.http.services.nebulaio-admin.loadbalancer.server.port=9001"
    depends_on:
      - nebulaio-1
    networks:
      - traefik-net
      - nebulaio-cluster

  nebulaio-3:
    image: ghcr.io/piwi3910/nebulaio:latest
    container_name: nebulaio-3
    restart: unless-stopped
    volumes:
      - nebulaio-data-3:/data
    environment:
      - NEBULAIO_NODE_ID=node-3
      - NEBULAIO_CLUSTER_BOOTSTRAP=false
      - NEBULAIO_CLUSTER_JOIN_ADDRESSES=nebulaio-1:9004
      - NEBULAIO_CLUSTER_ADVERTISE_ADDRESS=nebulaio-3
      - NEBULAIO_AUTH_ROOT_PASSWORD=${ADMIN_PASSWORD}
    labels:
      - "traefik.enable=true"
      - "traefik.http.services.nebulaio-s3.loadbalancer.server.port=9000"
      - "traefik.http.services.nebulaio-admin.loadbalancer.server.port=9001"
    depends_on:
      - nebulaio-1
    networks:
      - traefik-net
      - nebulaio-cluster

volumes:
  nebulaio-data-1:
  nebulaio-data-2:
  nebulaio-data-3:

networks:
  traefik-net:
    external: true
  nebulaio-cluster:
    driver: bridge

```bash

### With Prometheus & Grafana

```yaml

version: '3.8'

services:
  nebulaio:
    image: ghcr.io/piwi3910/nebulaio:latest
    # ... standard config ...

  prometheus:
    image: prom/prometheus:latest
    container_name: prometheus
    restart: unless-stopped
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'

  grafana:
    image: grafana/grafana:latest
    container_name: grafana
    restart: unless-stopped
    ports:
      - "3000:3000"
    volumes:
      - grafana-data:/var/lib/grafana
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin

volumes:
  prometheus-data:
  grafana-data:

```text

`prometheus.yml`:

```yaml

global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'nebulaio'
    static_configs:
      - targets:
        - 'nebulaio-1:9001'
        - 'nebulaio-2:9001'
        - 'nebulaio-3:9001'
    metrics_path: /metrics

```text

---

## Building Custom Images

### Multi-Architecture Build

```bash

# Build for multiple architectures
docker buildx create --use
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t ghcr.io/piwi3910/nebulaio:latest \
  -f deployments/docker/Dockerfile \
  --push .

```bash

### Minimal Image (No Web Console)

Create `Dockerfile.minimal`:

```dockerfile

FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /nebulaio ./cmd/nebulaio

FROM scratch
COPY --from=builder /nebulaio /nebulaio
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 9000 9001 9003
ENTRYPOINT ["/nebulaio"]

```text

---

## Docker Swarm Deployment

```yaml

version: '3.8'

services:
  nebulaio:
    image: ghcr.io/piwi3910/nebulaio:latest
    deploy:
      replicas: 3
      placement:
        max_replicas_per_node: 1
        constraints:
          - node.role == worker
      update_config:
        parallelism: 1
        delay: 30s
        failure_action: rollback
      restart_policy:
        condition: on-failure
        delay: 5s
        max_attempts: 3
    ports:
      - target: 9000
        published: 9000
        mode: host
      - target: 9001
        published: 9001
        mode: host
    volumes:
      - nebulaio-data:/data
    environment:
      - NEBULAIO_CLUSTER_ADVERTISE_ADDRESS={{.Node.Hostname}}
      - NEBULAIO_CLUSTER_JOIN_ADDRESSES=tasks.nebulaio:9004
    networks:
      - nebulaio-overlay

volumes:
  nebulaio-data:
    driver: local

networks:
  nebulaio-overlay:
    driver: overlay
    attachable: true

```text

Deploy:

```bash

docker stack deploy -c docker-compose.swarm.yml nebulaio
docker service ls
docker service logs -f nebulaio_nebulaio

```

---

## Environment Variables Reference

### Core Settings

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_DATA_DIR` | Data directory path | `/data` |
| `NEBULAIO_S3_PORT` | S3 API port | `9000` |
| `NEBULAIO_ADMIN_PORT` | Admin/Console API port | `9001` |
| `NEBULAIO_CONSOLE_PORT` | Web Console port | `9002` |
| `NEBULAIO_NODE_ID` | Unique node identifier | Auto-generated |
| `NEBULAIO_NODE_NAME` | Human-readable node name | hostname |
| `NEBULAIO_LOG_LEVEL` | Log level (debug, info, warn, error) | `info` |

### Authentication

> **Security Warning**: Never use weak or default passwords in production. The root password must be explicitly set and meet minimum security requirements.

| Variable | Description | Default | Requirements |
| ---------- | ------------- | --------- | -------------- |
| `NEBULAIO_AUTH_ROOT_USER` | Initial admin username | `admin` | - |
| `NEBULAIO_AUTH_ROOT_PASSWORD` | Initial admin password | **Required** | Min 12 chars, mixed case, numbers |
| `NEBULAIO_AUTH_JWT_SECRET` | JWT signing secret | Auto-generated | - |
| `NEBULAIO_AUTH_TOKEN_EXPIRY` | Token expiry (minutes) | `60` | - |
| `NEBULAIO_AUTH_REFRESH_TOKEN_EXPIRY` | Refresh token expiry (hours) | `168` | - |

### TLS Configuration

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_TLS_ENABLED` | Enable TLS (secure by default) | `true` |
| `NEBULAIO_TLS_CERT_DIR` | Certificate directory | `./data/certs` |
| `NEBULAIO_TLS_CERT_FILE` | Custom certificate path | - |
| `NEBULAIO_TLS_KEY_FILE` | Custom key path | - |
| `NEBULAIO_TLS_CA_FILE` | CA certificate path | - |
| `NEBULAIO_TLS_MIN_VERSION` | Minimum TLS version (1.2 or 1.3) | `1.2` |
| `NEBULAIO_TLS_REQUIRE_CLIENT_CERT` | Enable mTLS | `false` |
| `NEBULAIO_TLS_AUTO_GENERATE` | Auto-generate certificates | `true` |
| `NEBULAIO_TLS_VALIDITY_DAYS` | Certificate validity (days) | `365` |

### Clustering

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_CLUSTER_BOOTSTRAP` | Bootstrap new cluster | `true` |
| `NEBULAIO_CLUSTER_JOIN_ADDRESSES` | Comma-separated join addresses | - |
| `NEBULAIO_CLUSTER_ADVERTISE_ADDRESS` | Address to advertise | Auto-detected |
| `NEBULAIO_CLUSTER_RAFT_PORT` | Raft consensus port | `9003` |
| `NEBULAIO_CLUSTER_GOSSIP_PORT` | Gossip protocol port | `9004` |
| `NEBULAIO_CLUSTER_NODE_ROLE` | Node role (storage/management) | `storage` |
| `NEBULAIO_CLUSTER_CLUSTER_NAME` | Cluster name | `default` |
| `NEBULAIO_CLUSTER_EXPECT_NODES` | Expected nodes (bootstrap) | `1` |
| `NEBULAIO_CLUSTER_SHARD_ID` | Dragonboat shard ID | `1` |
| `NEBULAIO_CLUSTER_REPLICA_ID` | Dragonboat replica ID | Auto-generated |

### Storage

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_STORAGE_BACKEND` | Backend: fs, volume, erasure | `fs` |
| `NEBULAIO_STORAGE_DEFAULT_STORAGE_CLASS` | Default storage class | `STANDARD` |
| `NEBULAIO_STORAGE_MAX_OBJECT_SIZE` | Maximum object size (bytes) | `5497558138880` (5TB) |
| `NEBULAIO_STORAGE_VOLUME_MAX_VOLUME_SIZE` | Volume file size (bytes) | `34359738368` (32GB) |
| `NEBULAIO_STORAGE_VOLUME_AUTO_CREATE` | Auto-create volumes | `true` |
| `NEBULAIO_STORAGE_VOLUME_DIRECT_IO_ENABLED` | Enable O_DIRECT | `true` |
| `NEBULAIO_STORAGE_VOLUME_TIER_DIRECTORIES_HOT` | Hot tier directory | - |
| `NEBULAIO_STORAGE_VOLUME_TIER_DIRECTORIES_WARM` | Warm tier directory | - |
| `NEBULAIO_STORAGE_VOLUME_TIER_DIRECTORIES_COLD` | Cold tier directory | - |

### Compression

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_STORAGE_COMPRESSION_ENABLED` | Enable compression | `false` |
| `NEBULAIO_STORAGE_COMPRESSION_ALGORITHM` | Algorithm: zstd, lz4, gzip | `zstd` |
| `NEBULAIO_STORAGE_COMPRESSION_LEVEL` | Compression level (1-19) | `3` |
| `NEBULAIO_STORAGE_COMPRESSION_AUTO_DETECT` | Skip pre-compressed types | `true` |

### DRAM Cache

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_CACHE_ENABLED` | Enable DRAM cache | `false` |
| `NEBULAIO_CACHE_MAX_SIZE` | Maximum cache size (bytes) | `8589934592` (8GB) |
| `NEBULAIO_CACHE_SHARD_COUNT` | Cache shards | `256` |
| `NEBULAIO_CACHE_TTL` | TTL in seconds | `3600` |
| `NEBULAIO_CACHE_EVICTION_POLICY` | Policy: lru, lfu, arc | `arc` |
| `NEBULAIO_CACHE_PREFETCH_ENABLED` | Enable prefetching | `true` |
| `NEBULAIO_CACHE_DISTRIBUTED_MODE` | Distributed cache | `false` |

### Data Firewall (QoS)

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_FIREWALL_ENABLED` | Enable data firewall | `false` |
| `NEBULAIO_FIREWALL_DEFAULT_POLICY` | Default policy: allow, deny | `allow` |
| `NEBULAIO_FIREWALL_RATE_LIMITING_ENABLED` | Enable rate limiting | `false` |
| `NEBULAIO_FIREWALL_RATE_LIMITING_REQUESTS_PER_SECOND` | Requests per second | `1000` |
| `NEBULAIO_FIREWALL_RATE_LIMITING_BURST_SIZE` | Burst size | `100` |
| `NEBULAIO_FIREWALL_BANDWIDTH_ENABLED` | Enable bandwidth throttling | `false` |
| `NEBULAIO_FIREWALL_BANDWIDTH_MAX_BYTES_PER_SECOND` | Max bandwidth | `1073741824` (1GB/s) |
| `NEBULAIO_FIREWALL_CONNECTIONS_ENABLED` | Enable connection limits | `false` |
| `NEBULAIO_FIREWALL_CONNECTIONS_MAX_CONNECTIONS` | Max connections | `10000` |

### Audit Logging

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_AUDIT_ENABLED` | Enable audit logging | `true` |
| `NEBULAIO_AUDIT_COMPLIANCE_MODE` | Compliance: none, soc2, pci, hipaa, gdpr, fedramp | `none` |
| `NEBULAIO_AUDIT_FILE_PATH` | Audit log file path | `./data/audit/audit.log` |
| `NEBULAIO_AUDIT_RETENTION_DAYS` | Retention period (days) | `90` |
| `NEBULAIO_AUDIT_INTEGRITY_ENABLED` | Enable HMAC integrity | `true` |

### AI/ML Features (2025)

#### S3 Express One Zone

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_S3_EXPRESS_ENABLED` | Enable S3 Express | `false` |
| `NEBULAIO_S3_EXPRESS_DEFAULT_ZONE` | Default zone | `use1-az1` |
| `NEBULAIO_S3_EXPRESS_SESSION_DURATION` | Session duration (seconds) | `3600` |
| `NEBULAIO_S3_EXPRESS_ENABLE_ATOMIC_APPEND` | Enable atomic appends | `true` |

#### Apache Iceberg

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_ICEBERG_ENABLED` | Enable Iceberg | `false` |
| `NEBULAIO_ICEBERG_CATALOG_TYPE` | Catalog: rest, hive, glue | `rest` |
| `NEBULAIO_ICEBERG_CATALOG_URI` | Catalog URI | `http://localhost:8181` |
| `NEBULAIO_ICEBERG_WAREHOUSE` | Warehouse location | `s3://warehouse/` |
| `NEBULAIO_ICEBERG_ENABLE_ACID` | Enable ACID transactions | `true` |

#### MCP Server

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_MCP_ENABLED` | Enable MCP server | `false` |
| `NEBULAIO_MCP_PORT` | MCP server port | `9005` |
| `NEBULAIO_MCP_ENABLE_TOOLS` | Enable tool execution | `true` |
| `NEBULAIO_MCP_ENABLE_RESOURCES` | Enable resource access | `true` |
| `NEBULAIO_MCP_AUTH_REQUIRED` | Require authentication | `true` |
| `NEBULAIO_MCP_RATE_LIMIT_PER_MINUTE` | Rate limit | `60` |

#### GPUDirect Storage

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_GPUDIRECT_ENABLED` | Enable GPUDirect | `false` |
| `NEBULAIO_GPUDIRECT_BUFFER_POOL_SIZE` | Buffer pool (bytes) | `1073741824` (1GB) |
| `NEBULAIO_GPUDIRECT_MAX_TRANSFER_SIZE` | Max transfer (bytes) | `268435456` (256MB) |
| `NEBULAIO_GPUDIRECT_ENABLE_ASYNC` | Async transfers | `true` |
| `NEBULAIO_GPUDIRECT_ENABLE_P2P` | P2P GPU transfers | `true` |

#### BlueField DPU

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_DPU_ENABLED` | Enable DPU offload | `false` |
| `NEBULAIO_DPU_DEVICE_INDEX` | DPU device index | `0` |
| `NEBULAIO_DPU_ENABLE_CRYPTO` | Crypto offload | `true` |
| `NEBULAIO_DPU_ENABLE_COMPRESSION` | Compression offload | `true` |
| `NEBULAIO_DPU_ENABLE_RDMA` | RDMA offload | `true` |
| `NEBULAIO_DPU_FALLBACK_ON_ERROR` | Fall back to CPU | `true` |

#### S3 over RDMA

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_RDMA_ENABLED` | Enable RDMA | `false` |
| `NEBULAIO_RDMA_PORT` | RDMA port | `9100` |
| `NEBULAIO_RDMA_DEVICE_NAME` | RDMA device | `mlx5_0` |
| `NEBULAIO_RDMA_ENABLE_ZERO_COPY` | Zero-copy transfers | `true` |
| `NEBULAIO_RDMA_FALLBACK_TO_TCP` | Fall back to TCP | `true` |
| `NEBULAIO_RDMA_MEMORY_POOL_SIZE` | Memory pool (bytes) | `1073741824` (1GB) |

#### NVIDIA NIM

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_NIM_ENABLED` | Enable NIM | `false` |
| `NEBULAIO_NIM_API_KEY` | NVIDIA API key | - |
| `NEBULAIO_NIM_DEFAULT_MODEL` | Default model | `meta/llama-3.1-8b-instruct` |
| `NEBULAIO_NIM_TIMEOUT` | Timeout (seconds) | `60` |
| `NEBULAIO_NIM_ENABLE_STREAMING` | Enable streaming | `true` |
| `NEBULAIO_NIM_CACHE_RESULTS` | Cache results | `true` |
| `NEBULAIO_NIM_PROCESS_ON_UPLOAD` | Process on upload | `false` |

---

## Next Steps

- [Kubernetes Deployment](kubernetes.md)
- [Monitoring Setup](../operations/monitoring.md)
- [Backup & Recovery](../operations/backup.md)
