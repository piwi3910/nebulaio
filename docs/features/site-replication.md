# Site Replication

NebulaIO's Site Replication enables multi-datacenter active-active replication for globally distributed deployments with automatic failover and conflict resolution.

## Overview

Site Replication provides:

- **Active-Active Replication**: Read and write from any datacenter
- **Automatic Failover**: Seamless failover when a site becomes unavailable
- **Conflict Resolution**: Configurable strategies for concurrent writes
- **Low Latency**: Asynchronous replication with tunable consistency
- **Geo-Redundancy**: Protect data across geographic regions

## Architecture

```
                              Global Metadata Coordinator
                                         │
                    ┌────────────────────┼────────────────────┐
                    │                    │                    │
                    ▼                    ▼                    ▼
    ┌───────────────────────┐ ┌───────────────────────┐ ┌───────────────────────┐
    │     US-EAST Site      │ │     EU-WEST Site      │ │    APAC Site          │
    │  ┌─────────────────┐  │ │  ┌─────────────────┐  │ │  ┌─────────────────┐  │
    │  │  NebulaIO       │  │ │  │  NebulaIO       │  │ │  │  NebulaIO       │  │
    │  │  Cluster        │◄─┼─┼─►│  Cluster        │◄─┼─┼─►│  Cluster        │  │
    │  │  (3-5 nodes)    │  │ │  │  (3-5 nodes)    │  │ │  │  (3-5 nodes)    │  │
    │  └─────────────────┘  │ │  └─────────────────┘  │ │  └─────────────────┘  │
    │         │             │ │         │             │ │         │             │
    │  ┌──────▼──────────┐  │ │  ┌──────▼──────────┐  │ │  ┌──────▼──────────┐  │
    │  │ Replication     │  │ │  │ Replication     │  │ │  │ Replication     │  │
    │  │ Agent           │◄─┼─┼─►│ Agent           │◄─┼─┼─►│ Agent           │  │
    │  └─────────────────┘  │ │  └─────────────────┘  │ │  └─────────────────┘  │
    └───────────────────────┘ └───────────────────────┘ └───────────────────────┘
              │                         │                         │
              └─────────────────────────┴─────────────────────────┘
                              Replication Mesh
```

### Components

| Component | Description |
|-----------|-------------|
| Site | A complete NebulaIO cluster in a datacenter |
| Replication Agent | Handles bidirectional sync between sites |
| Metadata Coordinator | Tracks global state and version vectors |
| Conflict Resolver | Applies conflict resolution policies |

## Configuration

### Primary Site Setup

```yaml
site_replication:
  enabled: true
  site_name: us-east-1
  site_region: us-east
  role: primary

  # Replication settings
  replication_mode: async          # async or sync
  sync_interval: 1s                # For async mode
  batch_size: 1000                 # Objects per batch
  max_bandwidth: 1073741824        # 1GB/s limit

  # Peer sites
  peers:
    - name: eu-west-1
      endpoint: https://nebulaio-eu.example.com:9000
      access_key: ${EU_ACCESS_KEY}
      secret_key: ${EU_SECRET_KEY}
      region: eu-west
    - name: apac-1
      endpoint: https://nebulaio-apac.example.com:9000
      access_key: ${APAC_ACCESS_KEY}
      secret_key: ${APAC_SECRET_KEY}
      region: apac
```

### Peer Site Setup

```yaml
site_replication:
  enabled: true
  site_name: eu-west-1
  site_region: eu-west
  role: peer

  primary:
    endpoint: https://nebulaio-us.example.com:9000
    access_key: ${US_ACCESS_KEY}
    secret_key: ${US_SECRET_KEY}

  peers:
    - name: apac-1
      endpoint: https://nebulaio-apac.example.com:9000
      access_key: ${APAC_ACCESS_KEY}
      secret_key: ${APAC_SECRET_KEY}
```

## Setting Up Site Replication

### Step 1: Prepare Sites

Ensure each site has a running NebulaIO cluster:

```bash
# Verify cluster health on each site
curl https://us-east.example.com:9000/health/ready
curl https://eu-west.example.com:9000/health/ready
curl https://apac.example.com:9000/health/ready
```

### Step 2: Initialize Primary Site

```bash
# Initialize site replication on primary
curl -X POST https://us-east.example.com:9000/admin/site-replication/init \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "site_name": "us-east-1",
    "site_region": "us-east"
  }'
```

### Step 3: Add Peer Sites

```bash
# Add EU site as peer
curl -X POST https://us-east.example.com:9000/admin/site-replication/peers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "eu-west-1",
    "endpoint": "https://nebulaio-eu.example.com:9000",
    "access_key": "...",
    "secret_key": "...",
    "region": "eu-west"
  }'

# Add APAC site as peer
curl -X POST https://us-east.example.com:9000/admin/site-replication/peers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "apac-1",
    "endpoint": "https://nebulaio-apac.example.com:9000",
    "access_key": "...",
    "secret_key": "...",
    "region": "apac"
  }'
```

### Step 4: Enable Bucket Replication

```bash
# Replicate specific bucket across all sites
curl -X PUT https://us-east.example.com:9000/admin/site-replication/buckets/my-bucket \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "replicate_to": ["eu-west-1", "apac-1"],
    "sync_mode": "bidirectional"
  }'
```

### Step 5: Verify Replication Status

```bash
curl https://us-east.example.com:9000/admin/site-replication/status \
  -H "Authorization: Bearer $TOKEN"
```

## Conflict Resolution

### Resolution Strategies

| Strategy | Description | Use Case |
|----------|-------------|----------|
| `last_write_wins` | Latest timestamp wins | General purpose |
| `site_priority` | Configured site priority wins | Hierarchical deployments |
| `version_vector` | Vector clock comparison | Strong consistency needs |
| `merge` | Merge concurrent changes | Append-only data |
| `custom` | User-defined resolver | Complex business logic |

### Configuration

```yaml
site_replication:
  conflict_resolution:
    strategy: last_write_wins

    # Site priorities for site_priority strategy
    site_priorities:
      us-east-1: 100
      eu-west-1: 90
      apac-1: 80

    # Conflict logging
    log_conflicts: true
    conflict_log_bucket: replication-conflicts
```

### Custom Conflict Resolver

```yaml
site_replication:
  conflict_resolution:
    strategy: custom
    webhook_url: https://resolver.example.com/resolve
    timeout: 5s
```

Webhook receives:

```json
{
  "bucket": "my-bucket",
  "key": "path/to/object",
  "versions": [
    {
      "site": "us-east-1",
      "version_id": "v1",
      "timestamp": "2024-01-15T10:30:00Z",
      "etag": "abc123",
      "size": 1024
    },
    {
      "site": "eu-west-1",
      "version_id": "v2",
      "timestamp": "2024-01-15T10:30:01Z",
      "etag": "def456",
      "size": 1048
    }
  ]
}
```

## Monitoring and Metrics

### Prometheus Metrics

```
# Replication lag by site
nebulaio_site_replication_lag_seconds{source="us-east-1",target="eu-west-1"}

# Objects pending replication
nebulaio_site_replication_pending_objects{site="us-east-1"}

# Replication throughput
nebulaio_site_replication_bytes_total{site="us-east-1",direction="outbound"}
nebulaio_site_replication_bytes_total{site="us-east-1",direction="inbound"}

# Conflict counts
nebulaio_site_replication_conflicts_total{site="us-east-1",resolution="last_write_wins"}

# Site health
nebulaio_site_replication_peer_healthy{site="us-east-1",peer="eu-west-1"}

# Sync status
nebulaio_site_replication_sync_status{site="us-east-1"} # 0=syncing, 1=synced, 2=degraded
```

### Grafana Dashboard

Key panels to configure:

- Replication lag trend across sites
- Objects pending sync per site
- Conflict rate over time
- Bandwidth utilization between sites
- Peer health status

### Status API

```bash
# Overall replication status
curl https://localhost:9000/admin/site-replication/status \
  -H "Authorization: Bearer $TOKEN"
```

Response:

```json
{
  "site": "us-east-1",
  "status": "healthy",
  "peers": [
    {
      "name": "eu-west-1",
      "status": "connected",
      "lag_seconds": 0.5,
      "pending_objects": 12,
      "last_sync": "2024-01-15T12:00:00Z"
    },
    {
      "name": "apac-1",
      "status": "connected",
      "lag_seconds": 1.2,
      "pending_objects": 45,
      "last_sync": "2024-01-15T11:59:58Z"
    }
  ],
  "conflicts_last_hour": 3,
  "bytes_replicated_last_hour": 10737418240
}
```

## Failover Procedures

### Automatic Failover

NebulaIO detects site failures and automatically redirects traffic:

```yaml
site_replication:
  failover:
    enabled: true
    health_check_interval: 5s
    failure_threshold: 3            # Consecutive failures before failover
    recovery_threshold: 5           # Consecutive successes before recovery

    # DNS-based failover
    dns_failover:
      enabled: true
      ttl: 30
      provider: route53             # route53, cloudflare, or custom
```

### Manual Failover

```bash
# Promote peer to primary
curl -X POST https://eu-west.example.com:9000/admin/site-replication/promote \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "reason": "Planned maintenance on us-east-1"
  }'

# Demote old primary when recovered
curl -X POST https://us-east.example.com:9000/admin/site-replication/demote \
  -H "Authorization: Bearer $TOKEN"
```

### Failover Sequence

```
1. Site Failure Detected
         │
         ▼
2. Health Checks Fail (3x)
         │
         ▼
3. Mark Site Degraded
         │
         ▼
4. Redirect Traffic to Healthy Peers
         │
         ▼
5. Promote Peer to Primary (if needed)
         │
         ▼
6. Continue Operations
         │
         ▼
7. Failed Site Recovers
         │
         ▼
8. Resync Delta Changes
         │
         ▼
9. Restore Normal Operations
```

### Recovery After Failover

```bash
# Check recovery status
curl https://us-east.example.com:9000/admin/site-replication/recovery-status \
  -H "Authorization: Bearer $TOKEN"

# Force full resync if needed
curl -X POST https://us-east.example.com:9000/admin/site-replication/resync \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "full",
    "from_site": "eu-west-1"
  }'
```

## Network Requirements

### Port Requirements

| Port | Protocol | Direction | Purpose |
|------|----------|-----------|---------|
| 9000 | HTTPS | Bidirectional | S3 API and replication data |
| 9004 | TCP | Bidirectional | Cluster gossip (if cross-cluster) |
| 9005 | HTTPS | Bidirectional | Replication control plane |

### Bandwidth Recommendations

| Deployment Size | Minimum Bandwidth | Recommended |
|-----------------|-------------------|-------------|
| Small (<1TB) | 100 Mbps | 1 Gbps |
| Medium (1-10TB) | 1 Gbps | 10 Gbps |
| Large (>10TB) | 10 Gbps | 25+ Gbps |

### Latency Considerations

```yaml
site_replication:
  # Adjust timeouts for high-latency links
  network:
    connect_timeout: 10s
    request_timeout: 60s
    idle_timeout: 120s

    # Retry configuration
    max_retries: 3
    retry_backoff: exponential
    retry_max_delay: 30s
```

### Security

```yaml
site_replication:
  security:
    # Require TLS for all replication traffic
    require_tls: true
    min_tls_version: "1.3"

    # mTLS between sites
    mtls:
      enabled: true
      ca_cert: /path/to/ca.crt
      client_cert: /path/to/client.crt
      client_key: /path/to/client.key

    # Encrypt replication data at rest
    encryption:
      enabled: true
      key_id: replication-key
```

## Best Practices

1. **Start with async replication**: Synchronous replication significantly impacts latency
2. **Monitor replication lag**: Set alerts for lag exceeding acceptable thresholds
3. **Test failover regularly**: Conduct planned failover drills quarterly
4. **Use dedicated bandwidth**: Separate replication traffic from user traffic
5. **Configure appropriate conflict resolution**: Choose strategy based on data patterns
6. **Enable conflict logging**: Track conflicts for analysis and tuning
7. **Plan for network partitions**: Ensure each site can operate independently
8. **Document runbooks**: Prepare procedures for common failure scenarios

## Troubleshooting

### High Replication Lag

1. Check network bandwidth between sites
2. Verify no rate limiting is active
3. Increase batch size for large backlogs
4. Check for slow peer sites

### Frequent Conflicts

1. Review conflict logs for patterns
2. Consider application-level coordination
3. Evaluate if synchronous replication is needed
4. Adjust conflict resolution strategy

### Site Not Syncing

1. Verify network connectivity between sites
2. Check authentication credentials
3. Review replication agent logs
4. Ensure bucket replication is enabled

## Next Steps

- [Batch Replication](batch-replication.md) - For one-time migrations
- [Clustering](../architecture/clustering.md) - Local high availability
- [Security Features](security-features.md) - mTLS and encryption
