# NebulaIO Usage Examples

This directory contains usage examples for NebulaIO's enterprise features.

## Directory Structure

```
examples/
├── python/         # Python examples using boto3
├── go/             # Go examples using AWS SDK
└── shell/          # Shell scripts using curl and AWS CLI
```

## Core Enterprise Features

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

## AI/ML Features (2025)

### S3 Express One Zone

Ultra-low latency storage with atomic appends:
- [Python Express Bucket Example](python/s3_express.py)
- [Shell Express Operations](shell/s3_express.sh)

### Apache Iceberg

Native table format for data lakehouse workloads:
- [Python Iceberg with Spark](python/iceberg_spark.py)
- [PyIceberg Direct Access](python/iceberg_pyiceberg.py)

### MCP Server (AI Agents)

Integration with Claude, ChatGPT, and other LLMs:
- [Python MCP Client](python/mcp_client.py)
- [Shell MCP Operations](shell/mcp_operations.sh)

### GPUDirect Storage

Zero-copy GPU-to-storage transfers:
- [PyTorch DataLoader Example](python/gpudirect_pytorch.py)
- [CuPy Direct Access](python/gpudirect_cupy.py)

### S3 over RDMA

Sub-10μs latency via InfiniBand/RoCE:
- [Python RDMA Client](python/rdma_client.py)
- [Shell RDMA Test](shell/rdma_benchmark.sh)

### NVIDIA NIM

AI inference on stored objects:
- [Python NIM Inference](python/nim_inference.py)
- [Shell NIM Examples](shell/nim_examples.sh)

## Prerequisites

### Python
```bash
pip install boto3 pandas

# For AI/ML features
pip install pyiceberg pyspark cupy-cuda12x torch
pip install mcp  # Model Context Protocol client
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

# For AI/ML features
export NEBULAIO_MCP_ENDPOINT="http://localhost:9005"
export NEBULAIO_ICEBERG_ENDPOINT="http://localhost:9006"
export NEBULAIO_RDMA_ENDPOINT="192.168.100.1:9100"
export NVIDIA_API_KEY="nvapi-XXXX"  # For NIM
```
