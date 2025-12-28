# Storage Tiering

NebulaIO provides intelligent storage tiering to optimize costs while maintaining performance. Objects automatically transition between Hot, Warm, Cold, and Archive tiers based on access patterns and lifecycle policies.

## Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Storage Tiering Flow                               │
├─────────────────────────────────────────────────────────────────────────────┤
│   New Upload ──► HOT (NVMe) ──30d──► WARM (SSD) ──90d──► COLD (HDD)        │
│                      │                                        │              │
│                      ▼                                   365d │              │
│               ┌─────────────┐                                 ▼              │
│               │ DRAM Cache  │                          ARCHIVE (Tape)       │
│               └─────────────┘                                                │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Tier Definitions

| Tier | Storage Class | Media | Latency | Cost | Use Case |
|------|--------------|-------|---------|------|----------|
| Hot | STANDARD | NVMe/SSD | < 1ms | $$$$ | Active workloads, ML training |
| Warm | STANDARD_IA | SSD/HDD | 5-20ms | $$$ | Moderate access, recent data |
| Cold | GLACIER_IR | HDD/NAS | 50-200ms | $$ | Infrequent access, backups |
| Archive | DEEP_ARCHIVE | Tape | Hours | $ | Compliance, long-term retention |

### Configuration

```yaml
tiering:
  enabled: true
  tiers:
    hot:
      storage_class: STANDARD
      media_type: nvme
      cache_eligible: true
    warm:
      storage_class: STANDARD_IA
      media_type: ssd
    cold:
      storage_class: GLACIER_IR
      media_type: hdd
    archive:
      storage_class: DEEP_ARCHIVE
      media_type: tape
      min_retention_days: 180
```

## Physical Device Configuration

NebulaIO supports assigning different physical devices (NVMe, SSD, HDD) to different storage tiers for optimal performance and cost efficiency.

### Raw Block Device Tiering

For maximum performance, use raw block devices directly without filesystem overhead:

```yaml
storage:
  backend: volume

  volume:
    raw_devices:
      enabled: true

      # Assign devices to tiers
      devices:
        # Hot tier - NVMe for active data
        - path: /dev/nvme0n1
          tier: hot

        - path: /dev/nvme1n1
          tier: hot

        # Warm tier - SATA SSDs for moderate access
        - path: /dev/sda
          tier: warm

        - path: /dev/sdb
          tier: warm

        # Cold tier - HDDs for infrequent access
        - path: /dev/sdc
          tier: cold

        - path: /dev/sdd
          tier: cold

tiering:
  enabled: true
  policies:
    - name: standard-lifecycle
      transitions:
        - days: 7
          from_tier: hot
          to_tier: warm
        - days: 30
          from_tier: warm
          to_tier: cold
```

### File-Based Volume Tiering

For environments where raw device access isn't possible, use directory-based tiering:

```yaml
storage:
  backend: volume

  volume:
    # Define volume directories per tier
    tier_directories:
      hot: /mnt/nvme/nebulaio       # NVMe mount point
      warm: /mnt/ssd/nebulaio       # SSD mount point
      cold: /mnt/hdd/nebulaio       # HDD mount point

    max_volume_size: 34359738368     # 32GB per volume

tiering:
  enabled: true
```

### Tier Capacity Planning

| Tier | Recommended Media | Typical Capacity | Data Retention |
|------|------------------|------------------|----------------|
| Hot | NVMe | 5-10% of total | < 7 days since access |
| Warm | SSD | 20-30% of total | 7-30 days since access |
| Cold | HDD | 60-70% of total | > 30 days since access |

### Device Discovery

```bash
# List available block devices
nebulaio-cli storage device discover
# DEVICE         TYPE    SIZE      MOUNT     ELIGIBLE
# /dev/nvme0n1   NVMe    1.0 TB    -         ✓
# /dev/nvme1n1   NVMe    1.0 TB    -         ✓
# /dev/sda       SSD     4.0 TB    -         ✓
# /dev/sdb       SSD     4.0 TB    /data     ✗ (mounted)
# /dev/sdc       HDD     16.0 TB   -         ✓

# Initialize device for a specific tier
nebulaio-cli storage device init /dev/nvme0n1 --tier hot

# View tier assignment
nebulaio-cli storage tier devices
# TIER    DEVICES                    TOTAL CAPACITY
# hot     /dev/nvme0n1, /dev/nvme1n1 2.0 TB
# warm    /dev/sda                    4.0 TB
# cold    /dev/sdc, /dev/sdd          32.0 TB
```

## Lifecycle Policies

Automate object transitions with lifecycle policies based on age or access patterns.

```yaml
tiering:
  lifecycle_policies:
    - name: standard-lifecycle
      transitions:
        - days: 30
          storage_class: STANDARD_IA
        - days: 90
          storage_class: GLACIER_IR
        - days: 365
          storage_class: DEEP_ARCHIVE
      expiration:
        days: 2555  # 7 years
    - name: ml-datasets
      filters:
        bucket_prefix: ml-
      transitions:
        - days: 7
          storage_class: STANDARD_IA
```

### S3 API Compatible

```bash
aws s3api put-bucket-lifecycle-configuration --bucket my-bucket \
  --lifecycle-configuration '{
    "Rules": [{"ID": "archive-logs", "Filter": {"Prefix": "logs/"},
      "Transitions": [{"Days": 30, "StorageClass": "GLACIER_IR"}]}]
  }' --endpoint-url http://localhost:9000
```

## Manual Tier Transitions

```bash
# Transition object to cold tier
nebulaio-cli admin tier transition --bucket my-bucket --key data.bin --target-tier cold

# Transition by prefix
nebulaio-cli admin tier transition --bucket my-bucket --prefix archived/ --target-tier archive

# Restore from archive
nebulaio-cli admin tier restore --bucket my-bucket --key data.tar.gz --days 7 --tier bulk
```

### S3 API

```bash
# Transition via CopyObject
aws s3 cp s3://bucket/data.bin s3://bucket/data.bin \
  --storage-class GLACIER_IR --endpoint-url http://localhost:9000

# Restore from Glacier
aws s3api restore-object --bucket my-bucket --key data.tar.gz \
  --restore-request '{"Days":7,"GlacierJobParameters":{"Tier":"Bulk"}}' \
  --endpoint-url http://localhost:9000
```

## DRAM Cache Integration

The DRAM cache accelerates hot data access regardless of storage tier. Objects accessed frequently are automatically cached and optionally promoted to warmer tiers.

```yaml
cache:
  enabled: true
  tier_integration:
    cache_on_tier_access: true
    auto_promote_threshold: 10  # accesses/day to promote cold to warm
    ml_prefetch_tiers: [hot, warm]
```

## Cost Optimization Strategies

### Access-Based Tiering

```yaml
tiering:
  access_based:
    enabled: true
    hot_to_warm: {days_since_access: 30, min_accesses_to_stay: 5}
    warm_to_cold: {days_since_access: 60}
    cold_to_archive: {days_since_access: 180}
```

### Intelligent Tiering

```yaml
tiering:
  intelligent:
    enabled: true
    optimization_frequency: daily
    cost_weight: 0.7
    performance_weight: 0.3
```

### Cost Savings

| Scenario | Hot Only | With Tiering | Savings |
|----------|----------|--------------|---------|
| 100TB, 10% active | $2,500/mo | $850/mo | 66% |
| 1PB, 5% active | $25,000/mo | $5,200/mo | 79% |

## Monitoring

### Prometheus Metrics

```
nebulaio_tier_objects_total{tier="hot|warm|cold|archive"}
nebulaio_tier_bytes_total{tier="hot|warm|cold|archive"}
nebulaio_tier_transitions_total{from="hot",to="warm"}
nebulaio_tier_retrievals_total{tier="cold|archive"}
nebulaio_lifecycle_transitions_total{policy="name",status="success|failed"}
```

### CLI Monitoring

```bash
nebulaio-cli admin tier stats
# Tier       Objects      Size          Percentage
# hot        1,234,567    45.2 TB       15.2%
# warm       3,456,789    123.4 TB      41.5%
# cold       2,345,678    98.7 TB       33.2%
# archive    567,890      30.1 TB       10.1%
```

## Best Practices

1. **Define policies early**: Set lifecycle rules before uploading data
2. **Monitor access patterns**: Use metrics to refine transition thresholds
3. **Consider retrieval costs**: Factor in fees when choosing cold/archive
4. **Use intelligent tiering**: Enable for unpredictable workloads
5. **Set minimum retention**: Archive tier should have minimum retention periods
6. **Combine with DRAM cache**: Hot objects benefit from cache acceleration

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Objects not transitioning | Check lifecycle policy filters and scanner interval |
| Slow archive retrieval | Use expedited tier (higher cost) or pre-warm objects |
| High transition costs | Increase age thresholds or use size filters |

## Next Steps

- [Configure DRAM Cache](dram-cache.md) - Optimize hot tier performance
- [Set up erasure coding](erasure-coding.md) - Data protection across tiers
