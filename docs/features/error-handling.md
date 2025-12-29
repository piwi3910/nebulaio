# Error Handling Guide

This guide covers error handling patterns, error codes, and troubleshooting strategies for NebulaIO.

## S3 API Error Responses

All S3 API errors follow the AWS S3 error response format.

### Error Response Format

```xml

<?xml version="1.0" encoding="UTF-8"?>
<Error>
    <Code>NoSuchBucket</Code>
    <Message>The specified bucket does not exist</Message>
    <BucketName>my-bucket</BucketName>
    <RequestId>4442587FB7D0A2F9</RequestId>
</Error>

```text

JSON format (when `Accept: application/json` is specified):

```json

{
    "Code": "NoSuchBucket",
    "Message": "The specified bucket does not exist",
    "BucketName": "my-bucket",
    "RequestId": "4442587FB7D0A2F9"
}

```bash

### HTTP Status Codes

| Status Code | Category | Description |
| ------------- | ---------- | ------------- |
| 400 | Client Error | Malformed request, validation errors |
| 403 | Authorization | Access denied, signature mismatch |
| 404 | Not Found | Bucket or object doesn't exist |
| 409 | Conflict | Bucket already exists, concurrent modification |
| 500 | Server Error | Internal server error |
| 503 | Service Unavailable | Server overloaded, try again later |

---

## Common S3 Error Codes

### Access & Authentication Errors

| Error Code | HTTP Status | Description | Resolution |
| ------------ | ------------- | ------------- | ------------ |
| `AccessDenied` | 403 | Access to the resource is denied | Check IAM policies and bucket policies |
| `InvalidAccessKeyId` | 403 | Access key doesn't exist | Verify access key configuration |
| `SignatureDoesNotMatch` | 403 | Calculated signature doesn't match | Check secret key, ensure clock is synced |
| `ExpiredToken` | 400 | Security token has expired | Refresh credentials or session token |
| `AccountProblem` | 403 | Account issue prevents operation | Contact administrator |

### Bucket Errors

| Error Code | HTTP Status | Description | Resolution |
| ------------ | ------------- | ------------- | ------------ |
| `NoSuchBucket` | 404 | Bucket doesn't exist | Verify bucket name, check region |
| `BucketAlreadyExists` | 409 | Bucket name taken globally | Choose a different bucket name |
| `BucketAlreadyOwnedByYou` | 409 | You already own this bucket | Use existing bucket |
| `BucketNotEmpty` | 409 | Cannot delete non-empty bucket | Delete objects first or use force delete |
| `InvalidBucketName` | 400 | Bucket name doesn't follow rules | Use 3-63 chars, lowercase, no underscores |
| `TooManyBuckets` | 400 | Exceeded bucket limit | Delete unused buckets |

### Object Errors

| Error Code | HTTP Status | Description | Resolution |
| ------------ | ------------- | ------------- | ------------ |
| `NoSuchKey` | 404 | Object key doesn't exist | Verify object key, check versioning |
| `InvalidObjectState` | 403 | Object in archive/glacier | Restore object before access |
| `EntityTooLarge` | 400 | Object exceeds size limit | Use multipart upload for large files |
| `EntityTooSmall` | 400 | Part too small for multipart | Minimum 5MB per part (except last) |
| `KeyTooLongError` | 400 | Object key exceeds 1024 bytes | Use shorter key names |
| `InvalidDigest` | 400 | Content-MD5 doesn't match | Recalculate and send correct digest |

### Multipart Upload Errors

| Error Code | HTTP Status | Description | Resolution |
| ------------ | ------------- | ------------- | ------------ |
| `NoSuchUpload` | 404 | Upload ID doesn't exist | Initiate new multipart upload |
| `InvalidPart` | 400 | Part specified is invalid | Verify part number and ETag |
| `InvalidPartOrder` | 400 | Parts not in ascending order | Sort parts by part number |
| `EntityTooSmall` | 400 | Part smaller than 5MB | Increase part size |

### Rate Limiting Errors

| Error Code | HTTP Status | Description | Resolution |
| ------------ | ------------- | ------------- | ------------ |
| `SlowDown` | 503 | Request rate too high | Implement exponential backoff |
| `ServiceUnavailable` | 503 | Service temporarily unavailable | Retry with backoff |
| `RequestThrottled` | 503 | Request was throttled | Reduce request rate |

---

## Admin API Error Responses

Admin API errors use JSON format with detailed error information.

### Error Response Format

```json

{
    "error": {
        "code": "VALIDATION_ERROR",
        "message": "Invalid configuration value",
        "details": {
            "field": "replication.factor",
            "value": "0",
            "constraint": "must be >= 1"
        },
        "request_id": "req-12345",
        "timestamp": "2024-01-15T10:30:00Z"
    }
}

```bash

### Admin Error Codes

| Error Code | HTTP Status | Description |
| ------------ | ------------- | ------------- |
| `UNAUTHORIZED` | 401 | Missing or invalid authentication |
| `FORBIDDEN` | 403 | Insufficient permissions |
| `NOT_FOUND` | 404 | Resource not found |
| `VALIDATION_ERROR` | 400 | Invalid request parameters |
| `CONFLICT` | 409 | Resource already exists or state conflict |
| `INTERNAL_ERROR` | 500 | Internal server error |
| `CLUSTER_ERROR` | 500 | Cluster operation failed |
| `STORAGE_ERROR` | 500 | Storage subsystem error |

---

## Error Handling Best Practices

### 1. Implement Retry Logic

```python

import time
import random
from botocore.exceptions import ClientError

def s3_operation_with_retry(operation, max_retries=5):
    """Execute S3 operation with exponential backoff."""
    retryable_errors = ['SlowDown', 'ServiceUnavailable', 'RequestThrottled']

    for attempt in range(max_retries):
        try:
            return operation()
        except ClientError as e:
            error_code = e.response['Error']['Code']

            if error_code not in retryable_errors:
                raise  # Non-retryable error

            if attempt == max_retries - 1:
                raise  # Max retries exceeded

            # Exponential backoff with jitter
            wait_time = (2 ** attempt) + random.uniform(0, 1)
            print(f"Retrying in {wait_time:.2f}s (attempt {attempt + 1}/{max_retries})")
            time.sleep(wait_time)

```bash

### 2. Handle Specific Error Types

```python

from botocore.exceptions import ClientError

def get_object_safely(s3_client, bucket, key):
    """Get object with proper error handling."""
    try:
        response = s3_client.get_object(Bucket=bucket, Key=key)
        return response['Body'].read()

    except ClientError as e:
        error_code = e.response['Error']['Code']

        if error_code == 'NoSuchKey':
            print(f"Object {key} not found in {bucket}")
            return None

        elif error_code == 'NoSuchBucket':
            raise ValueError(f"Bucket {bucket} does not exist")

        elif error_code == 'AccessDenied':
            raise PermissionError(f"Access denied to {bucket}/{key}")

        else:
            raise  # Re-raise unexpected errors

```bash

### 3. Validate Before Operations

```python

def validate_bucket_name(name):
    """Validate bucket name before creation."""
    errors = []

    if len(name) < 3 or len(name) > 63:
        errors.append("Bucket name must be 3-63 characters")

    if not name[0].isalnum():
        errors.append("Bucket name must start with letter or number")

    if '_' in name:
        errors.append("Bucket name cannot contain underscores")

    if name != name.lower():
        errors.append("Bucket name must be lowercase")

    if errors:
        raise ValueError(f"Invalid bucket name: {'; '.join(errors)}")

    return True

```bash

### 4. Log Errors with Context

```python

import logging
import json

logger = logging.getLogger('nebulaio')

def log_s3_error(operation, bucket, key, error):
    """Log S3 error with full context."""
    error_info = {
        'operation': operation,
        'bucket': bucket,
        'key': key,
        'error_code': error.response['Error']['Code'],
        'error_message': error.response['Error']['Message'],
        'request_id': error.response.get('ResponseMetadata', {}).get('RequestId'),
        'http_status': error.response.get('ResponseMetadata', {}).get('HTTPStatusCode'),
    }

    logger.error(f"S3 operation failed: {json.dumps(error_info)}")

```text

---

## Cluster Error Handling

### Raft Consensus Errors

| Error | Cause | Resolution |
| ------- | ------- | ------------ |
| `ErrNoLeader` | No leader elected | Wait for election, check node connectivity |
| `ErrTimeout` | Operation timed out | Increase timeout, check network latency |
| `ErrNotMember` | Node not in cluster | Verify cluster configuration |
| `ErrShutdown` | Cluster is shutting down | Wait for restart |
| `ErrCompacted` | Log entry compacted | Snapshot was applied, retry operation |

### Handling Cluster Errors

```go

// Example: Handle cluster errors in Go
func performClusterOperation(ctx context.Context, op Operation) error {
    for retries := 0; retries < 3; retries++ {
        err := cluster.Execute(ctx, op)

        if err == nil {
            return nil
        }

        switch {
        case errors.Is(err, raft.ErrNoLeader):
            // Wait for leader election
            time.Sleep(time.Second * time.Duration(retries+1))
            continue

        case errors.Is(err, raft.ErrTimeout):
            // Timeout - may need to increase timeout or check network
            log.Warn().Err(err).Msg("operation timed out, retrying")
            continue

        case errors.Is(err, raft.ErrNotMember):
            // Node configuration issue - don't retry
            return fmt.Errorf("node not a cluster member: %w", err)

        default:
            // Unknown error
            return fmt.Errorf("cluster operation failed: %w", err)
        }
    }

    return errors.New("max retries exceeded")
}

```text

---

## Storage Layer Errors

### Common Storage Errors

| Error | Cause | Resolution |
| ------- | ------- | ------------ |
| `ErrDiskFull` | Storage volume full | Add storage, clean up data |
| `ErrCorrupted` | Data corruption detected | Run integrity check, restore from backup |
| `ErrIOTimeout` | Disk I/O timeout | Check disk health, reduce load |
| `ErrQuorumNotMet` | Not enough nodes for erasure coding | Bring nodes online |

### Erasure Coding Errors

| Error | Cause | Resolution |
| ------- | ------- | ------------ |
| `ErrInsufficientShards` | Not enough shards to reconstruct | Restore missing nodes |
| `ErrTooManyFailures` | More than parity shards failed | Data may be unrecoverable |
| `ErrPlacementFailure` | Cannot satisfy placement policy | Add nodes in required regions |

---

## Monitoring Error Rates

### Prometheus Metrics

NebulaIO exposes error metrics for monitoring:

```promql

# S3 API error rate by error code
sum(rate(nebulaio_s3_errors_total[5m])) by (code)

# Error rate percentage
sum(rate(nebulaio_s3_errors_total[5m])) / sum(rate(nebulaio_s3_requests_total[5m])) * 100

# Admin API errors
sum(rate(nebulaio_admin_errors_total[5m])) by (code)

# Storage layer errors
sum(rate(nebulaio_storage_errors_total[5m])) by (type)

```bash

### Alerting Rules

```yaml

groups:
  - name: nebulaio-errors
    rules:
      - alert: HighErrorRate
        expr: |
          sum(rate(nebulaio_s3_errors_total[5m]))
          / sum(rate(nebulaio_s3_requests_total[5m])) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High S3 error rate (>5%)"

      - alert: ClusterErrors
        expr: sum(rate(nebulaio_cluster_errors_total[5m])) > 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Cluster consensus errors detected"

```text

---

## Debugging Tips

### Enable Debug Logging

```bash

# Via environment variable
export NEBULAIO_LOG_LEVEL=debug
./nebulaio

# Via command line
./nebulaio --log-level debug

# Via configuration file
log_level: debug

```bash

### Trace Request Flow

```bash

# Get request ID from error response
REQUEST_ID="4442587FB7D0A2F9"

# Search logs for request
grep "$REQUEST_ID" /var/log/nebulaio/server.log

# Or use structured query
cat /var/log/nebulaio/server.log | jq 'select(.request_id == "'$REQUEST_ID'")'

```bash

### Check Component Health

```bash

# Overall health
curl -sk https://localhost:9001/health | jq

# Detailed component status
curl -sk https://localhost:9001/health/detailed | jq

# Cluster health
curl -sk https://localhost:9001/api/v1/admin/cluster/health | jq

# Storage health
curl -sk https://localhost:9001/api/v1/admin/storage/health | jq

```

---

## Related Documentation

- [Troubleshooting Guide](../TROUBLESHOOTING.md) - Specific issue resolution
- [Monitoring Guide](../operations/monitoring.md) - Metrics and alerting
- [Admin API Reference](../api/admin-api.md) - API error details
- [Configuration Guide](../getting-started/configuration.md) - Error-related settings
