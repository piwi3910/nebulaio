# Bucket Replication

NebulaIO supports S3-compatible bucket replication for automatic, real-time data synchronization between buckets across regions, clusters, or storage tiers.

## Overview

Bucket replication automatically copies objects from a source bucket to one or more destinations:

- **Cross-Region Replication (CRR)**: Replicate to different geographic regions
- **Same-Region Replication (SRR)**: Replicate within the same region
- **Cross-Cluster Replication**: Sync between NebulaIO clusters or to AWS S3/MinIO
- **Selective Replication**: Filter by prefix or tags
- **Delete Marker Replication**: Optionally replicate delete operations

## Replication Modes

### Asynchronous Replication (Default)

Objects are queued for background replication with eventual consistency:

```yaml

replication:
  mode: async
  workers: 10
  retry_max: 3

```bash

### Synchronous Replication

Waits for destination confirmation before acknowledging writes:

```yaml

replication:
  mode: sync
  timeout: 30s
  require_all_destinations: true

```bash

## Configuration via Replication Rules

### Basic Replication Configuration

```bash

cat > replication.json << 'EOF'
{
  "Role": "arn:aws:iam::123456789012:role/replication-role",
  "Rules": [{
    "ID": "replicate-all",
    "Priority": 1,
    "Status": "Enabled",
    "Filter": {},
    "Destination": { "Bucket": "arn:aws:s3:::destination-bucket" }
  }]
}
EOF

aws s3api put-bucket-replication \
  --endpoint-url http://localhost:9000 \
  --bucket source-bucket \
  --replication-configuration file://replication.json

```bash

### View and Delete Configuration

```bash

aws s3api get-bucket-replication --endpoint-url http://localhost:9000 --bucket source-bucket
aws s3api delete-bucket-replication --endpoint-url http://localhost:9000 --bucket source-bucket

```bash

## Filtering

### Prefix Filtering

```bash

cat > prefix-filter.json << 'EOF'
{
  "Role": "arn:aws:iam::123456789012:role/replication-role",
  "Rules": [{
    "ID": "replicate-logs",
    "Priority": 1,
    "Status": "Enabled",
    "Filter": { "Prefix": "logs/" },
    "Destination": { "Bucket": "arn:aws:s3:::logs-archive" }
  }]
}
EOF

aws s3api put-bucket-replication \
  --endpoint-url http://localhost:9000 \
  --bucket source-bucket \
  --replication-configuration file://prefix-filter.json

```bash

### Tag Filtering

```bash

cat > tag-filter.json << 'EOF'
{
  "Role": "arn:aws:iam::123456789012:role/replication-role",
  "Rules": [{
    "ID": "replicate-critical",
    "Priority": 1,
    "Status": "Enabled",
    "Filter": { "Tag": { "Key": "priority", "Value": "critical" } },
    "Destination": { "Bucket": "arn:aws:s3:::critical-backup" }
  }]
}
EOF

aws s3api put-bucket-replication \
  --endpoint-url http://localhost:9000 \
  --bucket source-bucket \
  --replication-configuration file://tag-filter.json

```bash

### Combined Prefix and Tag Filtering

```bash

cat > combined-filter.json << 'EOF'
{
  "Role": "arn:aws:iam::123456789012:role/replication-role",
  "Rules": [{
    "ID": "replicate-prod-logs",
    "Priority": 1,
    "Status": "Enabled",
    "Filter": {
      "And": {
        "Prefix": "logs/",
        "Tags": [{ "Key": "environment", "Value": "production" }]
      }
    },
    "Destination": { "Bucket": "arn:aws:s3:::prod-logs-archive" }
  }]
}
EOF

aws s3api put-bucket-replication \
  --endpoint-url http://localhost:9000 \
  --bucket source-bucket \
  --replication-configuration file://combined-filter.json

```bash

## Replication Status Tracking

### Object-Level Status

```bash

aws s3api head-object \
  --endpoint-url http://localhost:9000 \
  --bucket source-bucket \
  --key path/to/object.txt

```text

Status values: `PENDING`, `COMPLETED`, `FAILED`, `REPLICA`

### Queue Statistics

```bash

curl -X GET "http://localhost:9000/admin/replication/stats" -H "Authorization: Bearer $TOKEN"

```text

Response:

```json

{"total": 1500, "pending": 150, "in_progress": 25, "completed": 1300, "failed": 25}

```bash

## Monitoring Replication Lag

### Prometheus Metrics

```bash

nebulaio_replication_queue_depth{bucket="source-bucket"}
nebulaio_replication_lag_seconds{bucket="source-bucket"}
nebulaio_replication_bytes_per_second{bucket="source-bucket"}
nebulaio_replication_errors_total{bucket="source-bucket",error_type="network"}

```bash

### Alerting Rules

```yaml

groups:
  - name: replication
    rules:
      - alert: ReplicationLagHigh
        expr: nebulaio_replication_lag_seconds > 300
        for: 5m
        labels:
          severity: warning
      - alert: ReplicationQueueBacklog
        expr: nebulaio_replication_queue_depth > 10000
        for: 10m
        labels:
          severity: critical

```bash

## Troubleshooting Common Issues

### Objects Not Replicating

```bash

aws s3api get-bucket-replication --endpoint-url http://localhost:9000 --bucket source-bucket
curl -X GET "http://localhost:9000/admin/replication/health" -H "Authorization: Bearer $TOKEN"

```text

Common causes: Rule disabled, filter mismatch, destination unreachable, invalid credentials.

### Replication Lag Increasing

Increase workers in configuration:

```yaml

replication:
  workers: 20
  batch_size: 100

```bash

### Failed Replications

```bash

# Get failed items
curl -X GET "http://localhost:9000/admin/replication/failed?limit=10" -H "Authorization: Bearer $TOKEN"

# Retry failed
curl -X POST "http://localhost:9000/admin/replication/retry-failed" -H "Authorization: Bearer $TOKEN"

```bash

### Permission Errors

1. Verify destination bucket exists
2. Check credentials have write permissions
3. Ensure source bucket policy allows replication role

### Network Timeouts

```yaml

replication:
  timeout: 60s
  retry_delay: 10s
  retry_max: 5

```

## Best Practices

1. Start with async replication unless strong consistency is required
2. Use prefix filters to reduce scope and improve performance
3. Monitor queue depth and scale workers accordingly
4. Set up alerts for lag and failure thresholds
5. Test failover regularly by simulating destination outages
6. Review failed items daily and address root causes promptly
