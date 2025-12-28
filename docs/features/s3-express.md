# S3 Express One Zone

S3 Express One Zone provides ultra-low latency object storage for performance-critical workloads. It offers sub-millisecond latency for PUT and GET operations, atomic append support for streaming data, and directory bucket organization.

## Overview

S3 Express One Zone is designed for:

- **AI/ML Training** - Fast data loading for GPU workloads
- **Real-time Analytics** - Low-latency data access
- **Streaming Ingestion** - Atomic appends for log aggregation
- **High-frequency Trading** - Microsecond-level access

## Features

### Atomic Appends

Append data to existing objects without overwriting:

```bash
# Append data to an object
curl -X POST "http://localhost:9000/my-express-bucket--use1-az1--x-s3/data.log?append" \
  -H "Content-Type: text/plain" \
  -d "New log entry at $(date)"
```

### Session-Based Authentication

Create sessions for reduced authentication overhead:

```bash
# Create a session
curl -X POST "http://localhost:9000/?session" \
  -H "X-Amz-Create-Session-Mode: ReadWrite" \
  -H "Authorization: AWS4-HMAC-SHA256 ..."
```

### Directory Buckets

Organize objects in a hierarchical structure optimized for single-zone deployments:

```
bucket--zone--x-s3/
├── project-a/
│   ├── data/
│   └── models/
└── project-b/
    ├── data/
    └── models/
```

## Configuration

### Basic Setup

```yaml
s3_express:
  enabled: true
  default_zone: use1-az1
  session_duration: 300        # Session validity in seconds
  max_append_size: 5368709120  # 5GB max append size
  enable_atomic_append: true
```

### Multiple Zones

```yaml
s3_express:
  enabled: true
  default_zone: use1-az1
  zones:
    - id: use1-az1
      name: US East 1 AZ1
      region: us-east-1
      storage_class: EXPRESS_ONEZONE
    - id: usw2-az2
      name: US West 2 AZ2
      region: us-west-2
      storage_class: EXPRESS_ONEZONE
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEBULAIO_S3_EXPRESS_ENABLED` | Enable S3 Express | `false` |
| `NEBULAIO_S3_EXPRESS_DEFAULT_ZONE` | Default availability zone | `use1-az1` |

## Usage Examples

### Python (boto3)

```python
import boto3

# Create S3 Express client
s3 = boto3.client(
    's3',
    endpoint_url='http://localhost:9000',
    aws_access_key_id='minioadmin',
    aws_secret_access_key='minioadmin'
)

# Create directory bucket
s3.create_bucket(
    Bucket='ml-data--use1-az1--x-s3',
    CreateBucketConfiguration={
        'LocationConstraint': 'use1-az1',
        'Location': {'Type': 'AvailabilityZone', 'Name': 'use1-az1'},
        'Bucket': {'DataRedundancy': 'SingleAvailabilityZone', 'Type': 'Directory'}
    }
)

# Upload with low latency
s3.put_object(
    Bucket='ml-data--use1-az1--x-s3',
    Key='training/batch-001.parquet',
    Body=training_data
)
```

### Go SDK

```go
import (
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Create directory bucket
_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
    Bucket: aws.String("ml-data--use1-az1--x-s3"),
    CreateBucketConfiguration: &types.CreateBucketConfiguration{
        Location: &types.LocationInfo{
            Type: types.LocationTypeAvailabilityZone,
            Name: aws.String("use1-az1"),
        },
        Bucket: &types.BucketInfo{
            DataRedundancy: types.DataRedundancySingleAvailabilityZone,
            Type:           types.BucketTypeDirectory,
        },
    },
})
```

### Atomic Append for Streaming

```python
import requests

# Append log entries
for entry in log_stream:
    response = requests.post(
        f"http://localhost:9000/logs--use1-az1--x-s3/app.log?append",
        data=f"{entry}\n",
        headers={
            "Content-Type": "text/plain",
            "Authorization": auth_header
        }
    )
```

## Performance Tuning

### Optimal Settings for AI/ML

```yaml
s3_express:
  enabled: true
  default_zone: use1-az1
  session_duration: 3600       # Longer sessions for batch jobs
  max_append_size: 5368709120  # Full 5GB for large appends
  enable_atomic_append: true
  zones:
    - id: use1-az1
      name: GPU Zone
      region: us-east-1
      storage_class: EXPRESS_ONEZONE
```

### Benchmarks

| Operation | Latency | Throughput |
|-----------|---------|------------|
| PUT (small) | < 1ms | 10,000+ ops/sec |
| GET (small) | < 500μs | 20,000+ ops/sec |
| Append | < 2ms | 5,000+ ops/sec |
| Session create | < 5ms | - |

## Integration with Other Features

### GPUDirect Storage

```yaml
s3_express:
  enabled: true
gpudirect:
  enabled: true
  # Express buckets are automatically optimized for GPUDirect
```

### Apache Iceberg

```yaml
s3_express:
  enabled: true
iceberg:
  enabled: true
  warehouse: s3://warehouse--use1-az1--x-s3/
  # Iceberg metadata stored in Express for fast commits
```

## Troubleshooting

### Common Issues

1. **Bucket naming**: Express buckets must follow the pattern `bucket--zone--x-s3`
2. **Zone availability**: Ensure the zone is configured before creating buckets
3. **Session expiry**: Refresh sessions before the duration expires

### Logs

Enable debug logging:

```yaml
log_level: debug
```

Look for logs with `s3_express` tag.

## API Reference

### CreateSession

```
POST /?session HTTP/1.1
Host: bucket--zone--x-s3.localhost:9000
X-Amz-Create-Session-Mode: ReadWrite
```

### AppendObject

```
POST /object-key?append HTTP/1.1
Host: bucket--zone--x-s3.localhost:9000
Content-Type: application/octet-stream

[data to append]
```

## See Also

- [Apache Iceberg Integration](iceberg.md)
- [GPUDirect Storage](gpudirect.md)
- [S3 over RDMA](rdma.md)
