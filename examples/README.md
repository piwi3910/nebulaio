# NebulaIO Usage Examples

This directory contains usage examples for NebulaIO's enterprise features.

## Directory Structure

```
examples/
├── python/         # Python examples using boto3
├── go/             # Go examples using AWS SDK
└── shell/          # Shell scripts using curl and AWS CLI
```

## Enterprise Features

### DRAM Cache

The high-performance in-memory cache for AI/ML workloads:
- [Python ML Training Example](python/ml_training_cache.py)
- [Cache Warmup Script](shell/cache_warmup.sh)

### S3 Select

SQL queries on CSV/JSON/Parquet without full download:
- [Python S3 Select Example](python/s3_select_query.py)
- [Go S3 Select Example](go/s3_select.go)
- [Shell S3 Select Example](shell/s3_select.sh)

### Data Firewall

Rate limiting, bandwidth control, and access rules:
- [Firewall Configuration](shell/firewall_config.sh)
- [Rate Limit Testing](python/rate_limit_test.py)

### Batch Replication

Bulk data migration and disaster recovery:
- [Create Replication Job](shell/batch_replication.sh)
- [Migration Script](python/batch_migration.py)

### Audit Logging

Compliance-ready audit trails:
- [Audit Log Query](shell/audit_query.sh)
- [Audit Export Example](python/audit_export.py)

## Prerequisites

### Python
```bash
pip install boto3 pandas
```

### Go
```bash
go get github.com/aws/aws-sdk-go-v2/service/s3
```

### Shell
- AWS CLI v2
- curl
- jq

## Configuration

Set these environment variables:

```bash
export NEBULAIO_ENDPOINT="http://localhost:9000"
export NEBULAIO_ACCESS_KEY="your-access-key"
export NEBULAIO_SECRET_KEY="your-secret-key"
export NEBULAIO_ADMIN_ENDPOINT="http://localhost:9001"
export NEBULAIO_ADMIN_TOKEN="your-admin-token"
```
