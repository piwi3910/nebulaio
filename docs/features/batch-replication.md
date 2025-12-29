# Batch Replication

NebulaIO's Batch Replication enables bulk data migration, disaster recovery, and cross-cluster synchronization with job-based orchestration and progress tracking.

## Overview

Batch Replication provides:

- **Bulk Migration**: Move large datasets between buckets or clusters
- **Disaster Recovery**: Replicate data to remote sites for DR
- **Cross-Region Sync**: Synchronize data across geographic regions
- **Scheduled Replication**: Automate regular backup operations
- **Progress Tracking**: Real-time job status and metrics

## Architecture

```text

┌─────────────────────────────────────────────────────────────┐
│                    Batch Replication Manager                 │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ Job Queue   │  │   Workers   │  │  Progress Tracker   │  │
│  │             │  │ (Parallel)  │  │                     │  │
│  │  ┌───────┐  │  │  ┌───────┐  │  │  Objects: 1234/5000 │  │
│  │  │ Job 1 │  │  │  │ W1    │  │  │  Bytes: 45GB/100GB  │  │
│  │  │ Job 2 │  │  │  │ W2    │  │  │  Failed: 5          │  │
│  │  │ Job 3 │  │  │  │ W3    │  │  │  Rate: 150MB/s      │  │
│  │  └───────┘  │  │  └───────┘  │  │                     │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
├─────────────────────────────────────────────────────────────┤
│                    Source Bucket                             │
│                         │                                    │
│            ┌────────────┼────────────┐                      │
│            ▼            ▼            ▼                      │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐           │
│  │  Dest Bucket │ │  Remote S3  │ │  Another    │           │
│  │  (Same Clstr)│ │  (AWS/MinIO)│ │  NebulaIO   │           │
│  └─────────────┘ └─────────────┘ └─────────────┘           │
└─────────────────────────────────────────────────────────────┘

```bash

## Configuration

```yaml

batch_replication:
  enabled: true
  max_concurrent_jobs: 5          # Maximum parallel jobs
  default_concurrency: 10         # Workers per job
  max_retries: 3                  # Retry failed objects
  retry_delay: 5s                 # Delay between retries
  history_limit: 100              # Jobs to keep in history
  checkpoint_interval: 1000       # Objects between checkpoints

```bash

## Job Definition

### Basic Job

```json

{
  "job_id": "migration-2024-01",
  "source_bucket": "production-data",
  "destination_bucket": "backup-data",
  "description": "Monthly backup of production data"
}

```bash

### Cross-Cluster Job

```json

{
  "job_id": "dr-replication-001",
  "source_bucket": "critical-data",
  "destination_bucket": "critical-data",
  "destination_endpoint": "https://dr-site.example.com:9000",
  "destination_access_key": "AKIADR...",
  "destination_secret_key": "...",
  "description": "Disaster recovery replication"
}

```bash

### Filtered Job

```json

{
  "job_id": "replicate-logs-2024",
  "source_bucket": "logs",
  "destination_bucket": "logs-archive",
  "prefix": "2024/",
  "min_size": 1024,
  "max_size": 1073741824,
  "created_after": "2024-01-01T00:00:00Z",
  "created_before": "2024-12-31T23:59:59Z",
  "tags": {
    "retention": "long-term"
  }
}

```bash

### Throttled Job

```json

{
  "job_id": "low-priority-backup",
  "source_bucket": "archives",
  "destination_bucket": "cold-storage",
  "concurrency": 2,
  "rate_limit_bytes_per_sec": 52428800,
  "priority": 100
}

```bash

## API Usage

### Create Job

```bash

curl -X POST http://localhost:9000/admin/batch/jobs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "job_id": "migration-001",
    "source_bucket": "source",
    "destination_bucket": "destination",
    "description": "Initial data migration",
    "concurrency": 20
  }'

```bash

### Start Job

```bash

curl -X POST http://localhost:9000/admin/batch/jobs/migration-001/start \
  -H "Authorization: Bearer $TOKEN"

```bash

### Get Job Status

```bash

curl -X GET http://localhost:9000/admin/batch/jobs/migration-001 \
  -H "Authorization: Bearer $TOKEN"

```text

Response:

```json

{
  "job_id": "migration-001",
  "status": "running",
  "source_bucket": "source",
  "destination_bucket": "destination",
  "progress": {
    "total_objects": 50000,
    "completed_objects": 12345,
    "failed_objects": 5,
    "total_bytes": 107374182400,
    "completed_bytes": 26843545600,
    "current_rate_bytes_sec": 157286400,
    "estimated_completion": "2024-01-15T14:30:00Z"
  },
  "created_at": "2024-01-15T10:00:00Z",
  "started_at": "2024-01-15T10:01:00Z"
}

```bash

### List Jobs

```bash

curl -X GET http://localhost:9000/admin/batch/jobs \
  -H "Authorization: Bearer $TOKEN"

```bash

### Pause Job

```bash

curl -X POST http://localhost:9000/admin/batch/jobs/migration-001/pause \
  -H "Authorization: Bearer $TOKEN"

```bash

### Resume Job

```bash

curl -X POST http://localhost:9000/admin/batch/jobs/migration-001/resume \
  -H "Authorization: Bearer $TOKEN"

```bash

### Cancel Job

```bash

curl -X POST http://localhost:9000/admin/batch/jobs/migration-001/cancel \
  -H "Authorization: Bearer $TOKEN"

```bash

### Delete Job

```bash

curl -X DELETE http://localhost:9000/admin/batch/jobs/migration-001 \
  -H "Authorization: Bearer $TOKEN"

```bash

### Retry Failed Objects

```bash

curl -X POST http://localhost:9000/admin/batch/jobs/migration-001/retry-failed \
  -H "Authorization: Bearer $TOKEN"

```bash

## Job States

```text

┌─────────┐     start      ┌─────────┐
│ Pending │───────────────►│ Running │
└─────────┘                 └────┬────┘
                                 │
              ┌──────────────────┼──────────────────┐
              │                  │                  │
              ▼                  ▼                  ▼
        ┌───────────┐     ┌───────────┐     ┌───────────┐
        │  Paused   │     │ Completed │     │  Failed   │
        └─────┬─────┘     └───────────┘     └───────────┘
              │
              │ resume
              ▼
        ┌───────────┐
        │  Running  │
        └───────────┘
              │
              │ cancel
              ▼
        ┌───────────┐
        │ Cancelled │
        └───────────┘

```text

| State | Description |
| ------- | ------------- |
| pending | Job created, waiting to start |
| running | Actively replicating objects |
| paused | Temporarily stopped, can resume |
| completed | All objects replicated successfully |
| failed | Job stopped due to errors |
| cancelled | User cancelled the job |

## Filtering Options

### By Prefix

```json

{
  "prefix": "logs/2024/01/"
}

```bash

### By Size

```json

{
  "min_size": 1024,           // Skip files < 1KB
  "max_size": 1073741824      // Skip files > 1GB
}

```bash

### By Date

```json

{
  "created_after": "2024-01-01T00:00:00Z",
  "created_before": "2024-01-31T23:59:59Z"
}

```bash

### By Tags

```json

{
  "tags": {
    "environment": "production",
    "backup": "true"
  }
}

```bash

### Combined Filters

```json

{
  "prefix": "important/",
  "min_size": 1024,
  "created_after": "2024-01-01T00:00:00Z",
  "tags": {
    "critical": "true"
  }
}

```bash

## Performance Tuning

### Concurrency

```json

{
  "concurrency": 20    // Worker threads per job
}

```text

| Workload | Recommended Concurrency |
| ---------- | ------------------------ |
| Small files (<1MB) | 50-100 |
| Medium files (1MB-100MB) | 20-50 |
| Large files (>100MB) | 5-20 |
| Mixed | 20-30 |

### Bandwidth Limiting

```json

{
  "rate_limit_bytes_per_sec": 104857600   // 100 MB/s
}

```bash

### Checkpointing

Jobs automatically checkpoint progress every `checkpoint_interval` objects. This enables:

- Resume after failure
- Progress persistence across restarts
- Accurate progress tracking

## Cross-Cluster Replication

### To AWS S3

```json

{
  "job_id": "to-aws-s3",
  "source_bucket": "local-data",
  "destination_bucket": "aws-backup-bucket",
  "destination_endpoint": "https://s3.us-east-1.amazonaws.com",
  "destination_access_key": "AKIAIOSFODNN7EXAMPLE",
  "destination_secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  "destination_region": "us-east-1"
}

```bash

### To Another NebulaIO Cluster

```json

{
  "job_id": "cross-dc-sync",
  "source_bucket": "production",
  "destination_bucket": "production",
  "destination_endpoint": "https://nebulaio-dc2.example.com:9000",
  "destination_access_key": "...",
  "destination_secret_key": "..."
}

```bash

### To MinIO

```json

{
  "job_id": "to-minio",
  "source_bucket": "data",
  "destination_bucket": "backup",
  "destination_endpoint": "https://minio.example.com:9000",
  "destination_access_key": "...",
  "destination_secret_key": "..."
}

```bash

## Scheduled Replication

### Using Cron

```bash

# Daily backup at 2 AM
0 2 * * * curl -X POST http://localhost:9000/admin/batch/jobs/daily-backup/start -H "Authorization: Bearer $TOKEN"

```bash

### Using Kubernetes CronJob

```yaml

apiVersion: batch/v1
kind: CronJob
metadata:
  name: nebulaio-backup
spec:
  schedule: "0 2 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: curlimages/curl
            command:
            - curl
            - -X
            - POST
            - http://nebulaio:9000/admin/batch/jobs/daily-backup/start
            - -H
            - "Authorization: Bearer $(TOKEN)"
          restartPolicy: OnFailure

```bash

## Monitoring

### Prometheus Metrics

```bash

# Job counts by status
nebulaio_batch_jobs_total{status="pending|running|completed|failed"}

# Current job progress
nebulaio_batch_job_objects_total{job_id="..."}
nebulaio_batch_job_objects_completed{job_id="..."}
nebulaio_batch_job_bytes_total{job_id="..."}
nebulaio_batch_job_bytes_completed{job_id="..."}

# Replication rate
nebulaio_batch_replication_rate_bytes{job_id="..."}

# Errors
nebulaio_batch_job_errors_total{job_id="...",error_type="..."}

```bash

### Webhooks

Configure webhooks for job status notifications:

```json

{
  "job_id": "important-migration",
  "source_bucket": "critical",
  "destination_bucket": "backup",
  "notification_webhook": "https://slack.example.com/webhook",
  "notify_on": ["completed", "failed", "paused"]
}

```bash

## Error Handling

### Retry Configuration

```json

{
  "max_retries": 3,
  "retry_delay": "5s",
  "retry_backoff": "exponential"
}

```bash

### Failed Objects Report

```bash

curl -X GET http://localhost:9000/admin/batch/jobs/migration-001/failed \
  -H "Authorization: Bearer $TOKEN"

```text

Response:

```json

{
  "job_id": "migration-001",
  "failed_objects": [
    {
      "key": "data/corrupted-file.dat",
      "error": "checksum mismatch",
      "attempts": 3,
      "last_attempt": "2024-01-15T12:30:00Z"
    }
  ]
}

```

## Best Practices

1. **Test with small jobs first**: Validate configuration before large migrations
2. **Use appropriate concurrency**: Balance speed vs. resource usage
3. **Set bandwidth limits for production**: Avoid impacting live traffic
4. **Monitor progress**: Use webhooks or polling for long jobs
5. **Plan for failures**: Configure retries and checkpointing
6. **Use filters wisely**: Reduce scope to necessary objects
7. **Schedule during off-peak**: Run large jobs during low-traffic periods
8. **Verify after completion**: Spot-check replicated data integrity

## Troubleshooting

### Job Stuck in Running

1. Check worker errors: `GET /admin/batch/jobs/{id}/errors`
2. Verify destination is reachable
3. Check network connectivity
4. Review rate limiting settings

### Slow Replication

1. Increase concurrency for small files
2. Check bandwidth limits
3. Verify network throughput
4. Monitor source/destination IOPS

### High Failure Rate

1. Check destination bucket permissions
2. Verify network stability
3. Review object size limits
4. Check for corrupt source objects
