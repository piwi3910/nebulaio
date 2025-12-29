# DRAM Cache

NebulaIO's DRAM Cache provides high-performance in-memory caching optimized for AI/ML workloads, delivering sub-100μs read latencies and 10GB/s+ throughput.

## Overview

The DRAM Cache uses adaptive replacement cache (ARC) algorithm to intelligently cache frequently and recently accessed objects. It includes ML-optimized prefetching for sequential access patterns common in training workloads.

## Features

| Feature | Description |
| --------- | ------------- |
| ARC Eviction | Adaptive algorithm balancing recency and frequency |
| Prefetching | ML-aware sequential read optimization |
| Admission Control | Prevents cache pollution from one-time reads |
| Statistics | Real-time hit rates, memory usage, and latency metrics |
| Tiered Storage | Hot objects in DRAM, warm in SSD, cold in object store |

## Configuration

```yaml

cache:
  enabled: true
  max_size: 8589934592        # 8GB cache size
  eviction_policy: arc         # lru | arc | lfu
  prefetch_enabled: true       # Enable ML prefetching
  prefetch_threshold: 3        # Sequential reads to trigger prefetch
  prefetch_count: 5            # Objects to prefetch ahead
  admission_policy: always     # always | frequency | size
  min_object_size: 0           # Minimum size to cache
  max_object_size: 104857600   # Maximum size to cache (100MB)
  ttl: 3600                    # Default TTL in seconds
  stats_enabled: true          # Enable statistics collection

```bash

## Eviction Policies

### ARC (Adaptive Replacement Cache) - Recommended

ARC dynamically balances between LRU (recently used) and LFU (frequently used) based on workload patterns. It maintains ghost lists to learn access patterns.

```text

┌─────────────────────────────────────────────────────────────┐
│                        ARC Cache                             │
├─────────────────────────────────────────────────────────────┤
│  T1 (Recently Used)     │     T2 (Frequently Used)          │
│  ┌───┐ ┌───┐ ┌───┐     │     ┌───┐ ┌───┐ ┌───┐            │
│  │ A │ │ B │ │ C │     │     │ X │ │ Y │ │ Z │            │
│  └───┘ └───┘ └───┘     │     └───┘ └───┘ └───┘            │
├─────────────────────────────────────────────────────────────┤
│  B1 (Ghost T1)          │     B2 (Ghost T2)                 │
│  Evicted from T1        │     Evicted from T2               │
└─────────────────────────────────────────────────────────────┘

```bash

### LRU (Least Recently Used)

Simple eviction based on access time. Best for workloads with strong temporal locality.

### LFU (Least Frequently Used)

Evicts based on access count. Best for workloads with stable hot sets.

## ML Workload Optimization

### Prefetching

The cache detects sequential access patterns common in ML training:

```go

// Training loop typically reads data sequentially
for batch := range dataloader {
    // Cache prefetches next batches automatically
    model.Train(batch)
}

```text

When the prefetcher detects `prefetch_threshold` sequential reads, it automatically loads the next `prefetch_count` objects into cache.

### Admission Control

Prevents cache pollution from checkpoint writes and one-time reads:

| Policy | Description | Use Case |
| -------- | ------------- | ---------- |
| always | Cache everything | Small datasets |
| frequency | Only cache after N accesses | Large datasets with hotspots |
| size | Only cache objects under size limit | Mixed workloads |

## API Usage

### Check Cache Status

```bash

curl -X GET http://localhost:9000/admin/cache/stats \
  -H "Authorization: Bearer $TOKEN"

```text

Response:

```json

{
  "enabled": true,
  "size": 4294967296,
  "max_size": 8589934592,
  "hit_count": 1523456,
  "miss_count": 234567,
  "hit_rate": 0.866,
  "eviction_count": 45678,
  "prefetch_hits": 123456,
  "avg_latency_us": 45
}

```bash

### Warm Cache

Pre-populate cache with specific objects:

```bash

curl -X POST http://localhost:9000/admin/cache/warm \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "bucket": "ml-datasets",
    "prefix": "training/epoch-1/",
    "recursive": true
  }'

```bash

### Invalidate Cache

Clear specific entries or entire cache:

```bash

# Invalidate by prefix
curl -X DELETE "http://localhost:9000/admin/cache/invalidate?bucket=ml-datasets&prefix=checkpoints/" \
  -H "Authorization: Bearer $TOKEN"

# Clear entire cache
curl -X DELETE http://localhost:9000/admin/cache/clear \
  -H "Authorization: Bearer $TOKEN"

```bash

## Performance Benchmarks

| Metric | Without Cache | With Cache | Improvement |
| -------- | -------------- | ------------ | ------------- |
| Read Latency (p50) | 2.5ms | 45μs | 55x |
| Read Latency (p99) | 15ms | 120μs | 125x |
| Throughput | 500MB/s | 10GB/s | 20x |
| IOPS | 5,000 | 150,000 | 30x |

**Note:** Benchmarks performed on AWS r6i.4xlarge with 128GB RAM

## Integration with AI/ML Frameworks

### PyTorch DataLoader

```python

from torch.utils.data import DataLoader
from nebulaio import S3Dataset

# NebulaIO cache handles prefetching automatically
dataset = S3Dataset(
    bucket="ml-datasets",
    prefix="imagenet/train/",
    endpoint="http://nebulaio:9000"
)

loader = DataLoader(
    dataset,
    batch_size=256,
    num_workers=8,
    prefetch_factor=2  # Works with NebulaIO prefetching
)

```bash

### TensorFlow tf.data

```python

import tensorflow as tf

def load_from_nebulaio(path):
    # NebulaIO cache automatically caches frequently accessed files
    return tf.io.read_file(f"s3://ml-datasets/{path}")

dataset = tf.data.Dataset.list_files("s3://ml-datasets/train/*.tfrecord")
dataset = dataset.map(load_from_nebulaio, num_parallel_calls=tf.data.AUTOTUNE)
dataset = dataset.prefetch(tf.data.AUTOTUNE)

```bash

## Monitoring

### Prometheus Metrics

```bash

# Cache hit rate
nebulaio_cache_hit_rate

# Cache size in bytes
nebulaio_cache_size_bytes

# Cache operations per second
nebulaio_cache_operations_total{operation="get|put|evict"}

# Prefetch effectiveness
nebulaio_cache_prefetch_hits_total
nebulaio_cache_prefetch_misses_total

# Latency histogram
nebulaio_cache_latency_seconds{quantile="0.5|0.9|0.99"}

```

### Grafana Dashboard

Import the DRAM Cache dashboard from `dashboards/dram-cache.json` for real-time monitoring.

## Best Practices

1. **Size appropriately**: Set cache size to 10-20% of your hot dataset
2. **Enable prefetching**: Always enable for ML training workloads
3. **Use ARC**: Default policy works best for most workloads
4. **Monitor hit rates**: Target >80% hit rate for optimal performance
5. **Set max object size**: Exclude large objects (>100MB) from cache
6. **Warm before training**: Pre-populate cache before training jobs start

## Troubleshooting

### Low Hit Rate

- Check if objects are being evicted too quickly (increase cache size)
- Verify admission policy isn't too restrictive
- Check for cache pollution from one-time reads

### High Memory Usage

- Reduce max_size configuration
- Lower max_object_size to exclude large objects
- Enable more aggressive eviction with LRU policy

### Prefetch Not Working

- Verify prefetch_enabled is true
- Check prefetch_threshold isn't too high
- Ensure sequential access pattern is consistent
