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
  -e NEBULAIO_AUTH_ROOT_PASSWORD=changeme \
  ghcr.io/piwi3910/nebulaio:latest
```

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
  -e NEBULAIO_AUTH_ROOT_PASSWORD=your-secure-password \
  -e NEBULAIO_LOG_LEVEL=info \
  ghcr.io/piwi3910/nebulaio:latest
```

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
      - NEBULAIO_AUTH_ROOT_PASSWORD=changeme
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
```

Run:

```bash
docker-compose up -d
docker-compose logs -f
```

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
```

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
      - NEBULAIO_AUTH_ROOT_PASSWORD=changeme
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
      - NEBULAIO_AUTH_ROOT_PASSWORD=changeme
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
      - NEBULAIO_AUTH_ROOT_PASSWORD=changeme
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
```

Run:

```bash
docker-compose -f docker-compose.cluster.yml up -d

# Watch logs
docker-compose -f docker-compose.cluster.yml logs -f

# Check cluster status
curl http://localhost:9001/api/v1/admin/cluster/nodes | jq
curl http://localhost:9001/api/v1/admin/cluster/health | jq
```

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
```

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
```

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
```

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
```

### Minimal Image (No Web Console)

Create `Dockerfile.minimal`:

```dockerfile
FROM golang:1.23-alpine AS builder

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
```

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
```

Deploy:

```bash
docker stack deploy -c docker-compose.swarm.yml nebulaio
docker service ls
docker service logs -f nebulaio_nebulaio
```

---

## Environment Variables Reference

| Variable | Description | Default |
|----------|-------------|---------|
| `NEBULAIO_DATA_DIR` | Data directory path | `/data` |
| `NEBULAIO_S3_PORT` | S3 API port | `9000` |
| `NEBULAIO_ADMIN_PORT` | Admin/Console API port | `9001` |
| `NEBULAIO_CONSOLE_PORT` | Web Console port | `9002` |
| `NEBULAIO_NODE_ID` | Unique node identifier | Auto-generated |
| `NEBULAIO_NODE_NAME` | Human-readable node name | hostname |
| `NEBULAIO_AUTH_ROOT_USER` | Initial admin username | `admin` |
| `NEBULAIO_AUTH_ROOT_PASSWORD` | Initial admin password | `admin123` |
| `NEBULAIO_LOG_LEVEL` | Log level | `info` |
| `NEBULAIO_CLUSTER_BOOTSTRAP` | Bootstrap new cluster | `true` |
| `NEBULAIO_CLUSTER_JOIN_ADDRESSES` | Comma-separated join addresses | - |
| `NEBULAIO_CLUSTER_ADVERTISE_ADDRESS` | Address to advertise | Auto-detected |
| `NEBULAIO_CLUSTER_RAFT_PORT` | Raft consensus port | `9003` |
| `NEBULAIO_CLUSTER_GOSSIP_PORT` | Gossip protocol port | `9004` |
| `NEBULAIO_CLUSTER_NODE_ROLE` | Node role (storage/management) | `storage` |
| `NEBULAIO_CLUSTER_CLUSTER_NAME` | Cluster name | `default` |
| `NEBULAIO_CLUSTER_EXPECT_NODES` | Expected nodes (bootstrap) | `1` |

---

## Next Steps

- [Kubernetes Deployment](kubernetes.md)
- [Monitoring Setup](../operations/monitoring.md)
- [Backup & Recovery](../operations/backup.md)
