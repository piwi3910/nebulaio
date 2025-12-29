# Rate Limiting Configuration

NebulaIO includes comprehensive rate limiting capabilities to protect against abuse, ensure fair resource allocation, and mitigate denial-of-service attacks.

## Overview

Rate limiting in NebulaIO operates at multiple levels:

1. **Global Rate Limiting** - System-wide request limits
2. **Per-User Rate Limiting** - Limits per authenticated user
3. **Per-IP Rate Limiting** - Limits per client IP address
4. **Per-Bucket Rate Limiting** - Limits per storage bucket
5. **Per-Operation Rate Limiting** - Limits per S3 operation type
6. **Object Creation Rate Limiting** - Specialized protection against hash DoS attacks

## Configuration

### YAML Configuration

```yaml
firewall:
  enabled: true
  default_policy: allow
  audit_enabled: true

  rate_limiting:
    enabled: true
    requests_per_second: 1000    # Global RPS limit
    burst_size: 100              # Maximum burst size
    per_user: true               # Enable per-user limits
    per_ip: true                 # Enable per-IP limits
    per_bucket: false            # Enable per-bucket limits

    # Object creation rate limiting (hash DoS mitigation)
    # Protects against attacks that target object key distribution
    object_creation_limit: 1000   # RPS for PutObject, CopyObject, etc.
    object_creation_burst_size: 2000  # Burst size for object creation

    # Per-operation overrides
    operation_limits:
      GetObject: 5000            # Higher limit for reads
      PutObject: 1000            # Default for writes
      DeleteObject: 500          # Lower limit for deletes
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `NEBULAIO_FIREWALL_ENABLED` | `false` | Enable the data firewall |
| `NEBULAIO_FIREWALL_RATE_LIMITING_ENABLED` | `true` | Enable rate limiting |
| `NEBULAIO_FIREWALL_RATE_LIMIT_RPS` | `1000` | Global requests per second |
| `NEBULAIO_FIREWALL_RATE_LIMIT_BURST` | `100` | Maximum burst size |
| `NEBULAIO_FIREWALL_RATE_LIMIT_PER_USER` | `true` | Per-user rate limiting |
| `NEBULAIO_FIREWALL_RATE_LIMIT_PER_IP` | `true` | Per-IP rate limiting |
| `NEBULAIO_FIREWALL_RATE_LIMIT_PER_BUCKET` | `false` | Per-bucket rate limiting |
| `NEBULAIO_FIREWALL_OBJECT_CREATION_LIMIT` | `1000` | Object creation RPS (hash DoS protection). Set to `0` to use global rate limit. |
| `NEBULAIO_FIREWALL_OBJECT_CREATION_BURST` | `2000` | Object creation burst size. Set to `0` to use global burst size. |

> **Note:** When `object_creation_limit` or `object_creation_burst_size` is set to `0`, the system falls back to using the global `requests_per_second` and `burst_size` values respectively. This allows you to disable specialized object creation rate limiting while keeping general rate limiting active.

## Hash DoS Mitigation

### What is Hash DoS?

Hash DoS (Hash-based Denial of Service) attacks exploit hash table implementations by creating many objects with keys that hash to the same internal hash table bucket (not to be confused with S3 buckets), degrading lookup performance from O(1) to O(n).

### Protection Strategy

NebulaIO provides specialized rate limiting for object creation operations:

| Operation | Protected | Description |
|-----------|-----------|-------------|
| `PutObject` | Yes | Standard object uploads |
| `CopyObject` | Yes | Server-side object copies |
| `CompleteMultipartUpload` | Yes | Multipart upload completion |
| `UploadPart` | Yes | Multipart upload parts |

### Configuration Example

```yaml
firewall:
  rate_limiting:
    enabled: true
    # Standard rate limits
    requests_per_second: 5000
    burst_size: 500

    # Hash DoS protection - more restrictive for object creation
    object_creation_limit: 1000   # Limit object creation to 1000 RPS
    object_creation_burst_size: 2000  # Allow burst up to 2000
```

### Recommended Settings

| Workload Type | object_creation_limit | object_creation_burst_size |
|---------------|----------------------|---------------------------|
| Development | 100 | 200 |
| Standard Production | 1000 | 2000 |
| High-Throughput | 5000 | 10000 |
| AI/ML Training | 10000 | 20000 |

## Peer Cache Failure Logging

When using distributed DRAM cache, peer cache failures are rate-limited to prevent log flooding:

- **First 5 failures**: Logged immediately with full details
- **Subsequent failures**: Logged at 1 per 100 failures
- **Summary logging**: Periodic summaries of total failures

This prevents a failing peer from generating excessive log volume while still providing visibility into issues.

### Monitoring

Monitor cache health using Prometheus metrics:

```promql
# Peer cache write failures
nebulaio_cache_peer_write_failures_total

# Rate limited operations
nebulaio_object_creation_rate_limited_total{reason="hash_dos_protection"}
```

## Deployment Examples

> **Naming Conventions:** Different deployment methods use appropriate naming conventions for their platform:
>
> - **Environment variables**: `SCREAMING_SNAKE_CASE` (e.g., `NEBULAIO_FIREWALL_OBJECT_CREATION_LIMIT`)
> - **YAML config files**: `snake_case` (e.g., `object_creation_limit`)
> - **Helm values**: `camelCase` (e.g., `objectCreationLimit`) - automatically converted to snake_case in generated ConfigMaps

### Docker Compose

```yaml
services:
  nebulaio:
    environment:
      - NEBULAIO_FIREWALL_ENABLED=true
      - NEBULAIO_FIREWALL_RATE_LIMITING_ENABLED=true
      - NEBULAIO_FIREWALL_RATE_LIMIT_RPS=1000
      - NEBULAIO_FIREWALL_RATE_LIMIT_BURST=100
      - NEBULAIO_FIREWALL_OBJECT_CREATION_LIMIT=1000
      - NEBULAIO_FIREWALL_OBJECT_CREATION_BURST=2000
```

### Kubernetes ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nebulaio-config
data:
  config.yaml: |
    firewall:
      enabled: true
      rate_limiting:
        enabled: true
        requests_per_second: 1000
        burst_size: 100
        object_creation_limit: 1000
        object_creation_burst_size: 2000
```

### Helm Values

```yaml
firewall:
  enabled: true
  rateLimiting:
    enabled: true
    requestsPerSecond: 1000
    burstSize: 100
    objectCreationLimit: 1000
    objectCreationBurstSize: 2000
```

## Behavior When Limits Are Reached

When rate limits are exceeded:

1. **HTTP Response**: Client receives `429 Too Many Requests`
2. **Retry-After Header**: Includes suggested retry delay
3. **Metrics**: Counter incremented for monitoring
4. **Audit Log**: Event logged (if audit enabled)

### Example Response

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 1
Content-Type: application/xml

<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>SlowDown</Code>
  <Message>Please reduce your request rate.</Message>
  <Resource>/bucket/key</Resource>
  <RequestId>...</RequestId>
</Error>
```

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `nebulaio_requests_total` | Counter | Total requests by operation and status |
| `nebulaio_object_creation_rate_limited_total` | Counter | Object creation requests rate limited |
| `nebulaio_active_connections` | Gauge | Current active connections |
| `nebulaio_cache_peer_write_failures_total` | Counter | Distributed cache peer write failures |

### Example Grafana Query

```promql
# Total requests per second by status
rate(nebulaio_requests_total[5m])

# Object creation rate limiting events
rate(nebulaio_object_creation_rate_limited_total[5m])

# Monitor cache peer health
rate(nebulaio_cache_peer_write_failures_total[5m])
```

## Best Practices

1. **Start Conservative**: Begin with lower limits and increase based on observed traffic
2. **Monitor Before Enforcing**: Enable audit logging before strict enforcement
3. **Separate Limits by Operation**: Use per-operation limits for fine-grained control
4. **Enable Hash DoS Protection**: Always enable object creation limits in production
5. **Use Per-User Limits**: Prevent single users from consuming all resources
6. **Set Appropriate Burst Sizes**: Allow for legitimate traffic spikes

## Troubleshooting

### High Rate Limiting Events

If you see many rate-limited requests:

1. Check current limits vs. actual traffic patterns
2. Review per-user and per-IP distributions
3. Consider increasing limits or burst sizes
4. Look for abuse patterns (single IP, unusual operations)

### Legitimate Traffic Being Limited

If legitimate traffic is being rate limited:

1. Increase `burst_size` for bursty workloads
2. Adjust `requests_per_second` based on capacity
3. Consider per-bucket limits to isolate workloads
4. Review client retry behavior

## Next Steps

- [Data Firewall Configuration](../features/data-firewall.md)
- [Monitoring Guide](../operations/monitoring.md)
- [Security Features](../features/security-features.md)
