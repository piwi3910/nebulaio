# Capacity Planning Guide

This guide helps you plan storage capacity, node sizing, and placement group configuration for NebulaIO deployments.

## Overview

Capacity planning for NebulaIO involves:
1. **Storage capacity** - Raw storage needed for data + overhead
2. **Node sizing** - CPU, RAM, and network per node
3. **Placement groups** - Data locality and fault domain configuration
4. **Erasure coding overhead** - Parity storage requirements

---

## Storage Capacity Planning

### Calculating Raw Storage

```
Total Raw Storage = Data Size × Erasure Coding Overhead × Replication Factor
```

**Example:**
- Data size: 100 TB
- Erasure coding: 10+4 (40% overhead)
- Replication factor: 2 (cross-DC)

```
Raw Storage = 100 TB × 1.4 × 2 = 280 TB
```

### Erasure Coding Overhead

| Configuration | Data Shards | Parity Shards | Overhead | Fault Tolerance |
|--------------|-------------|---------------|----------|-----------------|
| 4+2 | 4 | 2 | 50% | 2 nodes |
| 8+4 | 8 | 4 | 50% | 4 nodes |
| 10+4 | 10 | 4 | 40% | 4 nodes |
| 16+4 | 16 | 4 | 25% | 4 nodes |

### Storage Tier Sizing

| Tier | Recommended Capacity | Use Case |
|------|---------------------|----------|
| Hot (NVMe) | 5-15% of total | Frequently accessed data |
| Warm (SSD) | 20-40% of total | Moderate access data |
| Cold (HDD) | 50-75% of total | Infrequent access |
| Archive | Unlimited | Long-term retention |

**Example for 1 PB deployment:**
```yaml
storage_allocation:
  hot_tier: 100 TB    # 10%
  warm_tier: 300 TB   # 30%
  cold_tier: 600 TB   # 60%
```

---

## Node Sizing

### Minimum Node Requirements

| Deployment | CPU Cores | RAM | NVMe | Network |
|------------|-----------|-----|------|---------|
| Development | 4 | 16 GB | 500 GB | 1 GbE |
| Small Production | 16 | 64 GB | 2 TB | 10 GbE |
| Medium Production | 32 | 128 GB | 8 TB | 25 GbE |
| Large Production | 64+ | 256+ GB | 16+ TB | 100 GbE |

### RAM Requirements

**Base formula:**
```
RAM per Node = Base + (Objects × Metadata Size) + Cache + Buffers
```

| Component | Size Per Node |
|-----------|---------------|
| Base system | 4 GB |
| Object metadata | ~200 bytes per object |
| DRAM cache | 8-64 GB (configurable) |
| I/O buffers | 2-8 GB |
| Raft state | 1-4 GB |

**Example for 100 million objects (5 nodes):**
```
Metadata per node: 100M / 5 × 200 bytes = 4 GB
Total RAM: 4 GB (base) + 4 GB (metadata) + 32 GB (cache) + 4 GB (buffers) = 44 GB
Recommended: 64 GB per node
```

### CPU Requirements

| Workload | CPU Cores/Node | Notes |
|----------|----------------|-------|
| Light I/O | 8-16 | Small files, low concurrency |
| Medium I/O | 16-32 | Mixed workloads |
| Heavy I/O | 32-64 | High concurrency, compression |
| AI/ML | 64+ | GPUDirect, high throughput |

---

## Placement Group Planning

### Concept Overview

Placement groups define data locality boundaries:

```
┌─────────────────────────────────────────────────────────────┐
│                    NebulaIO Cluster                          │
│                                                              │
│   ┌───────────────────────┐   ┌───────────────────────┐    │
│   │  Placement Group 1    │   │  Placement Group 2    │    │
│   │  (Datacenter East)    │   │  (Datacenter West)    │    │
│   │                       │   │                       │    │
│   │  ┌─────┐ ┌─────┐     │   │  ┌─────┐ ┌─────┐     │    │
│   │  │Node1│ │Node2│     │   │  │Node4│ │Node5│     │    │
│   │  └─────┘ └─────┘     │   │  └─────┘ └─────┘     │    │
│   │  ┌─────┐ ┌─────┐     │   │  ┌─────┐ ┌─────┐     │    │
│   │  │Node3│ │Node7│     │   │  │Node6│ │Node8│     │    │
│   │  └─────┘ └─────┘     │   │  └─────┘ └─────┘     │    │
│   │                       │   │                       │    │
│   │  Erasure: 4+2        │   │  Erasure: 4+2        │    │
│   │  (shards stay local)  │   │  (shards stay local)  │    │
│   └───────────────────────┘   └───────────────────────┘    │
│                                                              │
│              ↓ DR Replication (full copies) ↓               │
└─────────────────────────────────────────────────────────────┘
```

### Sizing Placement Groups

**Minimum nodes per placement group:**
```
Min Nodes = Data Shards + Parity Shards + Spare Nodes
```

| Erasure Config | Min Nodes | Recommended | Max Nodes |
|---------------|-----------|-------------|-----------|
| 4+2 | 6 | 8-10 | 50 |
| 8+4 | 12 | 14-16 | 100 |
| 10+4 | 14 | 16-20 | 100 |
| 16+4 | 20 | 24-30 | 200 |

### Placement Group Configuration

```yaml
storage:
  placement_groups:
    local_group_id: pg-east
    min_nodes_for_erasure: 6

    groups:
      - id: pg-east
        name: "US East Primary"
        datacenter: dc-east
        region: us-east-1
        min_nodes: 6
        max_nodes: 50

      - id: pg-west
        name: "US West DR"
        datacenter: dc-west
        region: us-west-2
        min_nodes: 6
        max_nodes: 50

    replication_targets:
      - pg-west
```

### Capacity per Placement Group

**Formula:**
```
PG Capacity = (Nodes × Storage per Node) / Erasure Overhead
```

**Example: 10 nodes with 10 TB NVMe, 10+4 erasure:**
```
Raw capacity: 10 × 10 TB = 100 TB
Usable capacity: 100 TB / 1.4 = 71 TB
```

---

## Scaling Guidelines

### When to Add Nodes

| Metric | Threshold | Action |
|--------|-----------|--------|
| Storage utilization | > 70% | Add storage nodes |
| CPU utilization | > 80% sustained | Add compute nodes |
| Network bandwidth | > 70% | Add nodes or upgrade network |
| Request latency | > SLA | Scale horizontally |

### Scaling Strategies

**Vertical Scaling (Single Node):**
- Add RAM for more cache
- Add NVMe drives
- Upgrade network

**Horizontal Scaling (Add Nodes):**
- Better for large datasets
- Improves fault tolerance
- Linear capacity increase

**Placement Group Expansion:**
- Add nodes to existing placement groups
- Create new placement groups for geographic expansion

### Growth Planning

| Current | 1 Year | 2 Years | 3 Years |
|---------|--------|---------|---------|
| 10 TB | 25 TB | 60 TB | 150 TB |

**Typical growth factors:**
- Conservative: 1.5x per year
- Moderate: 2.5x per year
- Aggressive: 4x per year

---

## Workload-Specific Planning

### General Object Storage

```yaml
# Balanced configuration
nodes: 6-12
erasure: 4+2
cache: 8-16 GB per node
network: 10-25 GbE
```

### AI/ML Training Data

```yaml
# High-throughput configuration
nodes: 8-20
erasure: 8+4
storage:
  hot_tier: 50% (NVMe)
  warm_tier: 50% (SSD)
cache: 64-128 GB per node
network: 100 GbE + RDMA
gpu: 4-8 per node (GPUDirect)
```

### Log Aggregation

```yaml
# Write-heavy configuration
nodes: 8-16
erasure: 10+4
storage:
  hot_tier: 10% (NVMe)
  warm_tier: 30% (SSD)
  cold_tier: 60% (HDD)
cache: 16-32 GB per node
compression: zstd level 3
```

### Backup/Archive

```yaml
# Cost-optimized configuration
nodes: 6-10
erasure: 16+4
storage:
  warm_tier: 20% (SSD)
  cold_tier: 80% (HDD)
cache: 8 GB per node
compression: zstd level 7
tiering:
  warm_to_cold: 7 days
  cold_to_archive: 30 days
```

---

## Multi-Datacenter Planning

### Two-Datacenter Setup

```yaml
# Active-Passive
datacenters:
  primary:
    placement_group: pg-primary
    nodes: 10
    capacity: 100 TB
    role: read/write

  secondary:
    placement_group: pg-secondary
    nodes: 10
    capacity: 100 TB
    role: DR replica
```

### Three-Datacenter Setup

```yaml
# Active-Active-Active
datacenters:
  east:
    placement_group: pg-east
    nodes: 8
    replication: [pg-central, pg-west]

  central:
    placement_group: pg-central
    nodes: 8
    replication: [pg-east, pg-west]

  west:
    placement_group: pg-west
    nodes: 8
    replication: [pg-east, pg-central]
```

### Cross-DC Bandwidth

| Replication | Write Rate | Required Bandwidth |
|-------------|------------|-------------------|
| Sync | 100 MB/s | 1 Gbps minimum |
| Async | 1 GB/s | 10 Gbps minimum |
| Bulk | 10 GB/s | 100 Gbps dedicated |

---

## Capacity Planning Checklist

### Initial Deployment

- [ ] Calculate total data capacity needed
- [ ] Determine erasure coding configuration
- [ ] Size RAM for metadata and cache
- [ ] Plan network bandwidth
- [ ] Define placement groups
- [ ] Plan replication topology

### Growth Planning

- [ ] Project 1/2/3 year growth
- [ ] Plan upgrade path (vertical vs horizontal)
- [ ] Budget for hardware additions
- [ ] Plan placement group expansion
- [ ] Consider geographic expansion

### Monitoring Setup

- [ ] Set capacity alerts (70%, 80%, 90%)
- [ ] Monitor per-tier utilization
- [ ] Track growth rate
- [ ] Review placement group health

---

## Quick Reference Calculator

### Storage Capacity

```bash
# Calculate usable capacity
usable_capacity() {
  raw=$1
  data_shards=$2
  parity_shards=$3
  echo "$raw / (1 + $parity_shards / $data_shards)" | bc -l
}

# Example: 100 TB raw with 10+4 erasure
usable_capacity 100 10 4
# Result: 71.4 TB
```

### Node Count

```bash
# Minimum nodes for erasure config
min_nodes() {
  data=$1
  parity=$2
  spare=${3:-2}
  echo $((data + parity + spare))
}

# Example: 10+4 with 2 spare nodes
min_nodes 10 4 2
# Result: 16 nodes
```

---

## Related Documentation

- [Placement Groups](placement-groups.md) - Detailed configuration
- [Storage Architecture](storage.md) - Backend options
- [Clustering](clustering.md) - Multi-node setup
- [Performance Tuning](../PERFORMANCE_TUNING.md) - Optimization
