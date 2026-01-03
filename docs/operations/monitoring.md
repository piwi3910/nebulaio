# Monitoring and Observability

NebulaIO provides comprehensive monitoring through Prometheus metrics, health endpoints, and structured logging.

## Prometheus Metrics Overview

Metrics are exposed on the admin API port (default: 9001):

```bash

curl http://localhost:9001/metrics

```bash

## Available Metrics

### Storage Metrics

```bash

nebulaio_storage_capacity_bytes{node="...",pool="..."}     # Total capacity
nebulaio_storage_used_bytes{node="...",pool="..."}         # Used storage
nebulaio_storage_available_bytes{node="...",pool="..."}    # Available storage
nebulaio_storage_utilization_ratio{node="...",pool="..."}  # Utilization percentage
nebulaio_objects_total{bucket="..."}                       # Object count
nebulaio_buckets_total                                     # Bucket count

```bash

### Request Metrics

```bash

nebulaio_s3_requests_total{operation="...",status="...",bucket="..."}  # Request count
nebulaio_s3_errors_total{operation="...",error_code="..."}             # Error count
nebulaio_s3_bytes_received_total{bucket="..."}                         # Bytes received
nebulaio_s3_bytes_sent_total{bucket="..."}                             # Bytes sent

```bash

### Latency Metrics

```bash

nebulaio_s3_request_duration_seconds_bucket{operation="...",le="..."}  # Duration histogram
nebulaio_s3_time_to_first_byte_seconds{operation="...",quantile="..."}  # TTFB
nebulaio_storage_read_latency_seconds{pool="..."}                       # Storage read latency
nebulaio_storage_write_latency_seconds{pool="..."}                      # Storage write latency

```bash

### Cache Metrics

```bash

nebulaio_cache_hits_total{cache_type="..."}       # Cache hits
nebulaio_cache_misses_total{cache_type="..."}     # Cache misses
nebulaio_cache_size_bytes{cache_type="..."}       # Current cache size
nebulaio_cache_evictions_total{cache_type="..."}  # Cache evictions

```bash

### Replication Metrics

```bash

nebulaio_replication_lag_seconds{source="...",destination="..."}   # Replication lag
nebulaio_replication_pending_operations{destination="..."}         # Pending operations
nebulaio_replication_bytes_total{destination="...",direction="..."} # Bytes transferred
nebulaio_replication_healthy{destination="..."}                    # Health status (1/0)

```bash

### Lambda Compression Metrics

Metrics for S3 Object Lambda compression and decompression operations:

```bash

nebulaio_lambda_compression_operations_total{algorithm="...",operation="...",status="..."}  # Total operations
nebulaio_lambda_compression_duration_seconds{algorithm="...",operation="..."}               # Operation duration histogram
nebulaio_lambda_compression_ratio{algorithm="..."}                                          # Compression ratio (compressed/original)
nebulaio_lambda_compression_bytes_processed_total{algorithm="...",operation="..."}          # Total bytes processed
nebulaio_lambda_operations_in_flight{algorithm="..."}                                       # Current in-flight operations

```bash

**Labels:**
- `algorithm`: Compression algorithm (`gzip`, `zstd`, or `auto` for auto-detection)
- `operation`: Operation type (`compress` or `decompress`)
- `status`: Result status (`success` or `error`)

**Compression Ratio Interpretation:**
- Values closer to 0 indicate better compression (e.g., 0.3 = 70% size reduction)
- Values closer to 1 indicate poor compression (e.g., 0.9 = 10% size reduction)
- Buckets: 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0

**Example Queries:**

```promql
# Compression operations rate by algorithm
rate(nebulaio_lambda_compression_operations_total[5m])

# Average compression ratio
avg(nebulaio_lambda_compression_ratio) by (algorithm)

# P95 compression latency
histogram_quantile(0.95, rate(nebulaio_lambda_compression_duration_seconds_bucket[5m]))

# Total bytes compressed per second
sum(rate(nebulaio_lambda_compression_bytes_processed_total{operation="compress"}[5m]))

# Current in-flight operations
sum(nebulaio_lambda_operations_in_flight) by (algorithm)
```

## Grafana Dashboards

### Request Rate Panel

```json

{
  "title": "Request Rate",
  "targets": [{
    "expr": "sum(rate(nebulaio_s3_requests_total[5m])) by (operation)",
    "legendFormat": "{{operation}}"
  }]
}

```bash

### Latency Percentiles Panel

```json

{
  "title": "Request Latency",
  "targets": [
    {"expr": "histogram_quantile(0.50, rate(nebulaio_s3_request_duration_seconds_bucket[5m]))", "legendFormat": "p50"},
    {"expr": "histogram_quantile(0.95, rate(nebulaio_s3_request_duration_seconds_bucket[5m]))", "legendFormat": "p95"},
    {"expr": "histogram_quantile(0.99, rate(nebulaio_s3_request_duration_seconds_bucket[5m]))", "legendFormat": "p99"}
  ]
}

```bash

### Storage Utilization Panel

```json

{
  "title": "Storage Utilization",
  "targets": [{
    "expr": "sum(nebulaio_storage_used_bytes) / sum(nebulaio_storage_capacity_bytes) * 100"
  }]
}

```bash

## Alerting Rules

```yaml

groups:
  - name: nebulaio
    rules:
      - alert: StorageSpaceCritical
        expr: nebulaio_storage_utilization_ratio > 0.90
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Storage above 90% on {{ $labels.node }}"

      - alert: HighErrorRate
        expr: |
          sum(rate(nebulaio_s3_errors_total[5m])) /
          sum(rate(nebulaio_s3_requests_total[5m])) > 0.01
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Error rate above 1%"

      - alert: HighLatency
        expr: histogram_quantile(0.99, rate(nebulaio_s3_request_duration_seconds_bucket[5m])) > 1
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "P99 latency exceeds 1 second"

      - alert: NodeDown
        expr: nebulaio_cluster_node_healthy == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Node {{ $labels.node }} is down"

      - alert: ReplicationLagHigh
        expr: nebulaio_replication_lag_seconds > 300
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Replication lag exceeds 5 minutes"

      - alert: LowCacheHitRatio
        expr: |
          sum(rate(nebulaio_cache_hits_total[5m])) /
          (sum(rate(nebulaio_cache_hits_total[5m])) + sum(rate(nebulaio_cache_misses_total[5m]))) < 0.5
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "Cache hit ratio below 50%"

```bash

## Health Check Endpoints

### Liveness Probe

```bash

curl http://localhost:9001/health/live
# Response: {"status": "ok", "timestamp": "2024-01-15T10:30:00Z"}

```bash

### Readiness Probe

```bash

curl http://localhost:9001/health/ready
# Response: {"status": "ready", "checks": {"storage": "ok", "cluster": "ok"}}

```bash

### Detailed Health

```bash

curl http://localhost:9001/health

```text

```json

{
  "status": "healthy",
  "version": "1.0.0",
  "uptime_seconds": 86400,
  "components": {
    "storage": {"status": "healthy", "used_percent": 45.2},
    "cluster": {"status": "healthy", "role": "leader", "members": 3},
    "replication": {"status": "healthy", "lag_seconds": 2}
  }
}

```bash

### Kubernetes Configuration

```yaml

livenessProbe:
  httpGet:
    path: /health/live
    port: 9001
  initialDelaySeconds: 10
  periodSeconds: 10
readinessProbe:
  httpGet:
    path: /health/ready
    port: 9001
  initialDelaySeconds: 5
  periodSeconds: 5

```bash

## Log Aggregation

### Log Format

NebulaIO outputs structured JSON logs:

```json

{
  "timestamp": "2024-01-15T10:30:00.123Z",
  "level": "info",
  "message": "Request completed",
  "request_id": "req_abc123",
  "operation": "GetObject",
  "bucket": "my-bucket",
  "status": 200,
  "duration_ms": 45
}

```bash

### Configuration

```yaml

logging:
  level: info          # debug, info, warn, error
  format: json         # json, text
  output: stdout
  file:
    path: /var/log/nebulaio/nebulaio.log
    max_size_mb: 100
    max_backups: 10
    compress: true

```bash

### Loki Integration

```yaml

scrape_configs:
  - job_name: nebulaio
    static_configs:
      - targets: [localhost]
        labels:
          job: nebulaio
          __path__: /var/log/nebulaio/*.log
    pipeline_stages:
      - json:
          expressions:
            level: level
            operation: operation
      - labels:
          level:
          operation:

```bash

## Performance Monitoring

### Key Performance Indicators

| Metric | Target | Critical |
| -------- | -------- | ---------- |
| Request latency p99 | < 100ms | > 1s |
| Error rate | < 0.1% | > 1% |
| Cache hit ratio | > 80% | < 50% |
| Storage utilization | < 70% | > 90% |
| Replication lag | < 60s | > 300s |

### Useful PromQL Queries

```promql

# Requests per second
sum(rate(nebulaio_s3_requests_total[1m]))

# Throughput (MB/s)
sum(rate(nebulaio_s3_bytes_sent_total[1m])) / 1048576

# Cache hit ratio
sum(rate(nebulaio_cache_hits_total[5m])) /
(sum(rate(nebulaio_cache_hits_total[5m])) + sum(rate(nebulaio_cache_misses_total[5m])))

# Days until storage full
(nebulaio_storage_capacity_bytes - nebulaio_storage_used_bytes) /
deriv(nebulaio_storage_used_bytes[7d])

```bash

## Troubleshooting

### Diagnostic Commands

```bash

# Check metrics endpoint
curl -s http://localhost:9001/metrics | grep nebulaio_

# Check health status
curl -s http://localhost:9001/health | jq .

# Check cluster status
curl -s http://localhost:9001/admin/cluster/status | jq .

```

### Common Issues

**High latency:** Check storage I/O metrics, cache hit ratios, and network connectivity.

**Increasing errors:** Review component health endpoints and error logs for patterns.

**Replication lag:** Verify network bandwidth and check pending operation counts.
