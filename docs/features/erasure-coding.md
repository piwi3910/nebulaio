# Erasure Coding

NebulaIO uses Reed-Solomon erasure coding to provide data durability with minimal storage overhead compared to traditional replication.

## What is Erasure Coding and Why Use It

Erasure coding breaks data into fragments, encodes them with redundant pieces, and distributes them across nodes. Unlike 3x replication (200% overhead), erasure coding achieves similar durability with 40-100% overhead.

| Method | Storage Overhead | Fault Tolerance | Use Case |
| -------- | ----------------- | ----------------- | ---------- |
| 3x Replication | 200% | 2 node failures | Simple setups |
| Erasure Coding (10+4) | 40% | 4 node failures | Production |
| Erasure Coding (8+8) | 100% | 8 node failures | Mission critical |

**Key Benefits**: Storage efficiency, configurable redundancy, automatic recovery, and distributed fault tolerance.

## Reed-Solomon Algorithm Basics

```text

┌─────────────────────────────────────────────────────────────────┐
│                 Reed-Solomon Encoding Process                    │
├─────────────────────────────────────────────────────────────────┤
│   Original Data              Encoded Shards                      │
│   ┌───────────┐              ┌────┐ ┌────┐ ┌────┐ ┌────┐        │
│   │  Object   │ ──Encode──►  │ D1 │ │ D2 │ │ D3 │ │ D4 │ Data   │
│   │   Data    │              ├────┤ ├────┤                       │
│   └───────────┘              │ P1 │ │ P2 │           Parity      │
│                              └────┘ └────┘                       │
│   Recovery: Any 4 shards can reconstruct original data          │
└─────────────────────────────────────────────────────────────────┘

```text

1. **Split**: Divide object into `k` data shards
2. **Encode**: Generate `m` parity shards using Reed-Solomon math
3. **Distribute**: Store shards across different nodes
4. **Recover**: Reconstruct from any `k` shards

## Configuration

```yaml

erasure:
  enabled: true
  data_shards: 10              # Number of data shards (k)
  parity_shards: 4             # Number of parity shards (m)
  shard_size: 1048576          # Shard size in bytes (1MB default)
  data_dir: /data/shards       # Local shard storage directory

```bash

### Presets

| Preset | Data | Parity | Overhead | Max Failures |
| -------- | ------ | -------- | ---------- | -------------- |
| minimal | 4 | 2 | 50% | 2 |
| standard | 10 | 4 | 40% | 4 |
| maximum | 8 | 8 | 100% | 8 |

### Parameter Constraints

- Total shards (data + parity) must not exceed 256
- Minimum 2 data shards, at least 1 parity shard required
- Shard size minimum: 1024 bytes

## Storage Overhead Calculations

```text

Overhead = (parity_shards / data_shards) * 100%

```text

| Configuration | Overhead | Effective Capacity |
| -------------- | ---------- | ------------------- |
| 4+2 | 50% | 66.7% |
| 10+4 | 40% | 71.4% |
| 16+4 | 25% | 80% |
| 8+8 | 100% | 50% |

**Example**: 10+4 storing 1TB requires 1.4TB raw storage. 10TB raw provides 7.14TB usable.

## Fault Tolerance

Maximum tolerable failures equals the number of parity shards.

| Configuration | Annual Durability | Max Failures |
| -------------- | ------------------- | -------------- |
| 10+4 | 99.999999999% (11 nines) | 4 |
| 8+8 | 99.9999999999% (12 nines) | 8 |
| 3x Replication | 99.999999% (8 nines) | 2 |

## Performance Implications

| Operation | 10+4 Config | Notes |
| ----------- | ------------- | ------- |
| Encode | ~500 MB/s | Per CPU core |
| Decode (healthy) | ~800 MB/s | Direct read |
| Decode (recovery) | ~300 MB/s | Reconstruction |

**I/O Patterns**:

- Writes: 14 parallel writes (1.4x network traffic for 10+4)
- Reads (healthy): 10 parallel reads (1x network traffic)
- Reads (degraded): Additional CPU for reconstruction

## Best Practices

### Small Clusters (4-8 nodes)

```yaml

erasure:
  data_shards: 4
  parity_shards: 2

```text

50% overhead, tolerates 2 failures. Minimum 6 nodes recommended.

### Production Clusters (12+ nodes)

```yaml

erasure:
  data_shards: 10
  parity_shards: 4

```text

40% overhead, tolerates 4 failures. Best balance for most workloads.

### Mission-Critical Data

```yaml

erasure:
  data_shards: 8
  parity_shards: 8

```text

100% overhead, tolerates 8 failures. For compliance or critical archives.

## Per-Bucket and Per-Object Redundancy

NebulaIO supports configuring redundancy at multiple levels: system-wide, per-bucket, and per-object.

### Redundancy Configuration Hierarchy

```text

System Default  →  Bucket Override  →  Object Override
    ↓                    ↓                    ↓
storage:           bucket config         object metadata
  default_         redundancy:           x-amz-meta-
  redundancy:      data_shards: 8       redundancy: ...

```bash

### System Default Configuration

```yaml

storage:
  default_redundancy:
    enabled: true
    data_shards: 10
    parity_shards: 4
    placement_policy: spread    # spread, local, rack-aware, zone-aware
    replication_factor: 1       # DR copies to other placement groups

```bash

### Per-Bucket Configuration

Override the default redundancy for specific buckets:

```bash

# Create bucket with custom redundancy
aws s3api create-bucket --bucket critical-data \
  --create-bucket-configuration '{
    "Redundancy": {
      "Enabled": true,
      "DataShards": 8,
      "ParityShards": 8,
      "PlacementPolicy": "zone-aware"
    }
  }'

# Update existing bucket
nebulaio-cli bucket set-redundancy critical-data \
  --data-shards 8 --parity-shards 8 --placement-policy zone-aware

```bash

### Per-Object Configuration

Set redundancy for individual objects using metadata:

```bash

# Upload with custom redundancy
aws s3 cp important-file.dat s3://my-bucket/important-file.dat \
  --metadata '{"redundancy-data-shards":"8","redundancy-parity-shards":"8"}'

```bash

### Redundancy Presets

| Preset | Data | Parity | Overhead | Use Case |
| -------- | ------ | -------- | ---------- | ---------- |
| `none` | - | - | 0% | Development/testing only |
| `minimal` | 4 | 2 | 50% | Small clusters, lower durability |
| `standard` | 10 | 4 | 40% | Production default |
| `maximum` | 8 | 8 | 100% | Mission-critical, compliance |

### Placement Policies

| Policy | Description |
| -------- | ------------- |
| `spread` | Maximize distribution across all available nodes |
| `local` | Prefer local node storage (single-node mode) |
| `rack-aware` | Distribute shards across different racks |
| `zone-aware` | Distribute shards across availability zones |

### Example: Multi-Tier Redundancy

```yaml

# Hot tier: lower redundancy for fast access
storage:
  tiering:
    tiers:
      hot:
        backend: erasure
        erasure_config:
          data_shards: 10
          parity_shards: 4

      # Cold tier: higher redundancy for long-term storage
      cold:
        backend: erasure
        erasure_config:
          data_shards: 8
          parity_shards: 8

```bash

### High-Throughput Workloads

```yaml

erasure:
  data_shards: 16
  parity_shards: 4
  shard_size: 4194304  # 4MB shards

```text

25% overhead, higher parallelism. Requires 20+ nodes.

## CLI Commands

### Status and Configuration

```bash

# Show erasure coding status
nebulaio-cli admin erasure status

# Set using preset
nebulaio-cli admin erasure set --preset standard

# Set custom configuration
nebulaio-cli admin erasure set --data-shards 10 --parity-shards 4

```bash

### Health and Verification

```bash

# Check shard health
nebulaio-cli admin erasure health

# Verify specific object
nebulaio-cli admin erasure verify --bucket my-bucket --key path/to/object

```bash

### Rebuild Operations

```bash

# Trigger rebuild for degraded objects
nebulaio-cli admin erasure rebuild

# Rebuild specific bucket
nebulaio-cli admin erasure rebuild --bucket my-bucket

# Check rebuild status
nebulaio-cli admin erasure rebuild-status

```bash

### Troubleshooting

```bash

# List degraded objects
nebulaio-cli admin erasure list-degraded

# Force immediate rebuild
nebulaio-cli admin erasure rebuild --priority high

```bash

## Monitoring

### Prometheus Metrics

```text

nebulaio_erasure_encode_operations_total
nebulaio_erasure_decode_operations_total
nebulaio_erasure_reconstruct_operations_total
nebulaio_erasure_encode_duration_seconds{quantile="0.5|0.9|0.99"}
nebulaio_erasure_healthy_objects
nebulaio_erasure_degraded_objects
nebulaio_erasure_objects_at_risk

```bash

### Alert Configuration

```yaml

groups:
  - name: erasure-coding
    rules:
      - alert: DegradedObjects
        expr: nebulaio_erasure_degraded_objects > 0
        for: 5m
        labels:
          severity: warning

      - alert: ObjectsAtRisk
        expr: nebulaio_erasure_objects_at_risk > 0
        for: 1m
        labels:
          severity: critical

```

## Troubleshooting

**Degraded Objects**: Objects have lost shards but remain recoverable. Run `nebulaio-cli admin erasure rebuild` to restore.

**Rebuild Failures**: Check node connectivity, disk space, hardware health, and network bandwidth.

**Performance Issues**: Increase shard size for large objects, ensure adequate CPU, use fewer shards on small clusters.
