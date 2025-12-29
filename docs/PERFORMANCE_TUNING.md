# NebulaIO Performance Tuning Guide

This guide provides comprehensive recommendations for optimizing NebulaIO performance across different deployment scenarios and workloads.

## Table of Contents

1. [Hardware Recommendations](#hardware-recommendations)
2. [Operating System Tuning](#operating-system-tuning)
3. [Network Configuration](#network-configuration)
4. [Storage Optimization](#storage-optimization)
5. [Memory Management](#memory-management)
6. [CPU Optimization](#cpu-optimization)
7. [S3 API Tuning](#s3-api-tuning)
8. [AI/ML Workload Optimization](#aiml-workload-optimization)
9. [Cluster Configuration](#cluster-configuration)
10. [Monitoring and Profiling](#monitoring-and-profiling)

---

## Hardware Recommendations

### Minimum Requirements

| Component | Development | Production | High-Performance |
| ----------- | ------------- | ------------ | ------------------ |
| CPU | 4 cores | 16 cores | 32+ cores |
| RAM | 8 GB | 64 GB | 256+ GB |
| Storage | SSD 100GB | NVMe 1TB+ | NVMe RAID 10TB+ |
| Network | 1 Gbps | 10 Gbps | 25-100 Gbps |

### Recommended Hardware for AI/ML Workloads

```yaml

# High-performance AI/ML configuration
compute:
  cpu: AMD EPYC 7763 or Intel Xeon Platinum 8380
  cores: 64+

memory:
  capacity: 512GB+
  type: DDR4-3200 or DDR5
  channels: 8

storage:
  type: NVMe SSD
  capacity: 10TB+ per node
  iops: 1M+ random read
  throughput: 7GB/s sequential

network:
  primary: 100GbE
  rdma: InfiniBand HDR or RoCE v2

accelerators:
  gpu: NVIDIA A100/H100 (for GPUDirect)
  dpu: NVIDIA BlueField-3 (optional)

```text

---

## Operating System Tuning

### Kernel Parameters

Add to `/etc/sysctl.conf`:

```bash

# Network tuning
net.core.somaxconn = 65535
net.core.netdev_max_backlog = 65535
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.core.rmem_default = 1048576
net.core.wmem_default = 1048576
net.ipv4.tcp_rmem = 4096 1048576 16777216
net.ipv4.tcp_wmem = 4096 1048576 16777216
net.ipv4.tcp_max_syn_backlog = 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_keepalive_time = 300
net.ipv4.tcp_keepalive_probes = 5
net.ipv4.tcp_keepalive_intvl = 15

# Memory tuning
vm.swappiness = 10
vm.dirty_ratio = 40
vm.dirty_background_ratio = 10
vm.vfs_cache_pressure = 50
vm.max_map_count = 262144

# File system tuning
fs.file-max = 2097152
fs.nr_open = 2097152
fs.aio-max-nr = 1048576

# IPC tuning
kernel.shmmax = 68719476736
kernel.shmall = 4294967296

```text

Apply changes:

```bash

sudo sysctl -p

```bash

### File Descriptor Limits

Edit `/etc/security/limits.conf`:

```bash

# NebulaIO user limits
nebulaio soft nofile 1048576
nebulaio hard nofile 1048576
nebulaio soft nproc 65535
nebulaio hard nproc 65535
nebulaio soft memlock unlimited
nebulaio hard memlock unlimited

```bash

### Transparent Huge Pages

Disable THP for consistent performance:

```bash

echo never > /sys/kernel/mm/transparent_hugepage/enabled
echo never > /sys/kernel/mm/transparent_hugepage/defrag

```text

Add to `/etc/rc.local` for persistence.

---

## Network Configuration

### TCP Tuning

```bash

# Enable TCP BBR congestion control
echo "net.core.default_qdisc=fq" >> /etc/sysctl.conf
echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.conf

# Enable TCP Fast Open
echo "net.ipv4.tcp_fastopen=3" >> /etc/sysctl.conf

```bash

### Network Interface Tuning

```bash

# Increase ring buffer sizes
ethtool -G eth0 rx 4096 tx 4096

# Enable receive-side scaling
ethtool -L eth0 combined 16

# Disable interrupt coalescing for low latency
ethtool -C eth0 rx-usecs 0 tx-usecs 0

# Enable jumbo frames (if supported)
ip link set eth0 mtu 9000

```bash

### RDMA Configuration

For RDMA-enabled deployments:

```bash

# Load RDMA kernel modules
modprobe ib_core
modprobe rdma_ucm
modprobe ib_uverbs

# Configure RDMA device
rdma link add rxe0 type rxe netdev eth0

# Verify RDMA
ibv_devinfo

```text

---

## Storage Optimization

### NVMe Tuning

```bash

# Set I/O scheduler to none for NVMe
echo none > /sys/block/nvme0n1/queue/scheduler

# Increase read-ahead
blockdev --setra 2048 /dev/nvme0n1

# Set optimal queue depth
echo 1024 > /sys/block/nvme0n1/queue/nr_requests

```bash

### Filesystem Configuration

```bash

# Mount options for XFS (recommended)
mount -o noatime,nodiratime,logbufs=8,logbsize=256k,allocsize=64m /dev/nvme0n1 /data

# Mount options for ext4
mount -o noatime,nodiratime,data=writeback,barrier=0,nobh /dev/nvme0n1 /data

```bash

### RAID Configuration

For software RAID with mdadm:

```bash

# Create RAID 10 array
mdadm --create /dev/md0 --level=10 --raid-devices=4 /dev/nvme[0-3]n1

# Set stripe cache size
echo 32768 > /sys/block/md0/md/stripe_cache_size

# Set read-ahead
blockdev --setra 65536 /dev/md0

```text

---

## Memory Management

### NebulaIO Memory Configuration

```yaml

# config.yaml
memory:
  # Total cache size (recommend 60-70% of available RAM)
  cache_size: 128GB

  # Read cache allocation
  read_cache:
    size: 80GB
    ttl: 1h
    eviction: lru

  # Write buffer allocation
  write_buffer:
    size: 32GB
    flush_threshold: 80%
    flush_interval: 5s

  # Metadata cache
  metadata_cache:
    size: 16GB
    preload: true

  # Buffer pools for I/O
  buffer_pools:
    small: 4KB
    medium: 64KB
    large: 1MB
    count: 10000

```bash

### Garbage Collection Tuning

```bash

# Set Go garbage collection target (lower = more frequent GC)
export GOGC=100

# Set memory limit for Go runtime
export GOMEMLIMIT=200GB

# Enable memory ballast for reduced GC frequency
export NEBULAIO_MEMORY_BALLAST=10GB

```bash

### NUMA Optimization

```bash

# Pin NebulaIO to specific NUMA nodes
numactl --cpunodebind=0 --membind=0 ./nebulaio

# Or use systemd service configuration
# /etc/systemd/system/nebulaio.service
[Service]
ExecStart=/usr/bin/numactl --interleave=all /usr/local/bin/nebulaio

```text

---

## CPU Optimization

### CPU Governor

```bash

# Set performance governor
for cpu in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
    echo performance > $cpu
done

# Disable CPU frequency scaling
cpupower frequency-set -g performance

```bash

### CPU Affinity

```yaml

# config.yaml
cpu:
  # Number of worker threads
  workers: 32

  # CPU affinity configuration
  affinity:
    # Pin API handlers to specific CPUs
    api_cpus: "0-7"

    # Pin I/O workers to specific CPUs
    io_cpus: "8-23"

    # Pin background tasks to specific CPUs
    background_cpus: "24-31"

  # Enable CPU pinning
  pin_threads: true

```bash

### SIMD Optimization

NebulaIO automatically uses SIMD instructions when available:

```bash

# Verify CPU capabilities
cat /proc/cpuinfo | grep -E "avx|sse|neon"

# Enable specific optimizations
export NEBULAIO_SIMD=avx512

```text

---

## S3 API Tuning

### Request Handling

```yaml

# config.yaml
s3:
  # Maximum concurrent requests
  max_connections: 10000

  # Request timeout
  request_timeout: 5m

  # Keep-alive settings
  keep_alive:
    enabled: true
    timeout: 60s
    max_requests: 1000

  # Request queue configuration
  queue:
    size: 10000
    timeout: 30s

  # Multipart upload settings
  multipart:
    # Minimum part size (5MB required by S3 spec)
    min_part_size: 5MB

    # Recommended part size for large uploads
    recommended_part_size: 100MB

    # Maximum parts per upload
    max_parts: 10000

    # Concurrent part uploads
    concurrent_parts: 10

```bash

### Compression Settings

```yaml

# config.yaml
compression:
  # Enable automatic compression
  enabled: true

  # Compression algorithm (zstd recommended)
  algorithm: zstd

  # Compression level (1-22 for zstd)
  level: 3

  # Minimum size to compress
  min_size: 1KB

  # Skip compression for these content types
  skip_types:
    - image/jpeg
    - image/png
    - video/*
    - application/gzip
    - application/zip

```bash

### Caching Configuration

```yaml

# config.yaml
caching:
  # Enable response caching
  enabled: true

  # Cache small objects in memory
  memory_cache:
    max_object_size: 1MB
    total_size: 10GB

  # Cache metadata
  metadata_cache:
    enabled: true
    ttl: 5m

  # Enable read-ahead for sequential access
  read_ahead:
    enabled: true
    size: 10MB
    trigger_sequential_reads: 3

```text

---

## AI/ML Workload Optimization

### GPUDirect Storage

```yaml

# config.yaml
gpudirect:
  enabled: true

  # Buffer pool configuration
  buffer_pool:
    size: 16GB
    block_size: 2MB

  # CUDA settings
  cuda:
    devices: [0, 1, 2, 3]
    streams_per_device: 4

  # Batch settings
  batching:
    enabled: true
    max_batch_size: 100
    max_wait_time: 10ms

```bash

### RDMA Configuration

```yaml

# config.yaml
rdma:
  enabled: true

  # Device configuration
  device: mlx5_0
  port: 1

  # Memory registration
  memory:
    max_mr_size: 1GB
    inline_threshold: 256B

  # Connection settings
  connections:
    max_qp: 1000
    max_cq: 100
    max_wr: 4096

  # Performance settings
  performance:
    use_odp: true
    use_prefetch: true

```bash

### S3 Express Configuration

```yaml

# config.yaml
s3_express:
  enabled: true

  # Session settings
  sessions:
    max_active: 10000
    timeout: 5m

  # Performance optimizations
  optimizations:
    single_zone: true
    reduced_latency: true

  # Availability zone
  availability_zone: us-east-1a

```bash

### Iceberg Integration

```yaml

# config.yaml
iceberg:
  enabled: true

  # Catalog configuration
  catalog:
    type: rest
    warehouse: s3://data-lake/warehouse

  # Performance settings
  performance:
    # Enable predicate pushdown
    predicate_pushdown: true

    # Column pruning
    column_pruning: true

    # Metadata caching
    metadata_cache:
      enabled: true
      ttl: 5m

    # Compaction settings
    compaction:
      target_file_size: 512MB
      min_files: 5

```text

---

## Cluster Configuration

### Dragonboat Consensus Tuning

Dragonboat uses `config.Config` for Raft shard configuration and `config.NodeHostConfig` for the NodeHost.

```yaml

# config.yaml
cluster:
  # Shard and replica configuration (config.Config)
  shard_id: 1
  replica_id: 1  # Must be unique per node (uint64)

  # RTT-based timeouts (Dragonboat uses RTT multiples, not durations)
  rtt_millisecond: 200  # Base RTT between nodes
  raft_election_rtt: 10  # Election timeout = 10 * RTT = 2000ms
  raft_heartbeat_rtt: 1  # Heartbeat = 1 * RTT = 200ms

  # WAL directory (use fastest storage available - NVMe recommended)
  wal_dir: /fast/nvme/wal
  node_host_dir: /var/lib/nebulaio/nodehost

  # Snapshot settings
  snapshot_entries: 1024  # Create snapshot every N entries
  compaction_overhead: 500  # Entries to retain after snapshot

  # Quorum checking (recommended for production)
  check_quorum: true

```text

**NodeHost Configuration** (applied at NodeHost level):

```yaml

cluster:
  # NodeHost settings
  raft_address: 10.0.1.10:9003  # Address for Raft communication

  # Advanced NodeHost tuning (config.NodeHostConfig)
  max_send_queue_size: 0  # 0 = use Dragonboat defaults
  max_receive_queue_size: 0

```text

**Performance Notes**:

- Dragonboat achieves 1.25M writes/sec with 1.3ms latency
- WAL directory on NVMe can improve performance by 10-20%
- Batch proposals are automatically enabled for high throughput
- Pipelined replication reduces round-trip latency
- `GetLeaderID` returns 4 values: `(leaderID, term, valid, error)` - always check `valid` before using
- Use `SyncRequestAddNonVoting` for adding non-voting members (not observers)
- Use `SyncRequestAddReplica` for adding voting members

**Dragonboat API Notes**:

- Leader operations: `SyncPropose`, `SyncRead`
- Membership changes: `SyncRequestAddReplica`, `SyncRequestAddNonVoting`, `SyncRequestDeleteReplica`
- Snapshots: `SyncRequestSnapshot`
- Leadership transfer: `RequestLeaderTransfer`

### Replication Settings

```yaml

# config.yaml
replication:
  # Synchronous replication factor
  sync_replicas: 2

  # Asynchronous replication
  async:
    enabled: true
    batch_size: 1000
    interval: 1s

  # Network settings
  network:
    timeout: 30s
    retry_attempts: 3
    retry_delay: 1s

```bash

### Load Balancing

```yaml

# config.yaml
load_balancing:
  # Algorithm: round_robin, least_connections, weighted
  algorithm: least_connections

  # Health check settings
  health_check:
    interval: 5s
    timeout: 2s
    unhealthy_threshold: 3
    healthy_threshold: 2

  # Connection draining
  drain:
    enabled: true
    timeout: 30s

```text

---

## Monitoring and Profiling

### Prometheus Metrics

Key metrics to monitor:

```yaml

# Latency metrics
nebulaio_s3_request_duration_seconds:
  - p50 target: < 10ms
  - p99 target: < 100ms
  - p999 target: < 500ms

# Throughput metrics
nebulaio_s3_bytes_transferred_total:
  - target: > 10 GB/s per node

# Error rates
nebulaio_s3_requests_total{status=~"5.."}:
  - target: < 0.1%

# Resource utilization
nebulaio_cpu_usage_percent:
  - target: < 80%
nebulaio_memory_usage_bytes:
  - target: < 85% of allocated

```bash

### Profiling

Enable runtime profiling:

```yaml

# config.yaml
profiling:
  enabled: true

  # pprof endpoint
  pprof:
    enabled: true
    port: 6060

  # Continuous profiling
  continuous:
    enabled: true
    interval: 1m
    retention: 24h

```text

Access profiling data:

```bash

# CPU profile
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Memory profile
go tool pprof http://localhost:6060/debug/pprof/heap

# Goroutine profile
go tool pprof http://localhost:6060/debug/pprof/goroutine

# Block profile
go tool pprof http://localhost:6060/debug/pprof/block

```bash

### Tracing

Enable distributed tracing:

```yaml

# config.yaml
tracing:
  enabled: true

  # OpenTelemetry configuration
  otlp:
    endpoint: otel-collector:4317
    insecure: false

  # Sampling
  sampling:
    rate: 0.1  # 10% of requests

  # Trace export
  export:
    batch_size: 512
    interval: 5s

```text

---

## Performance Benchmarking

### Built-in Benchmarks

Run NebulaIO benchmarks:

```bash

# Run all benchmarks
nebulaio benchmark --all

# Specific benchmarks
nebulaio benchmark --type=read --size=1MB --concurrency=100
nebulaio benchmark --type=write --size=100MB --concurrency=50
nebulaio benchmark --type=list --prefix=/data --concurrency=20

# Compare results
nebulaio benchmark --compare=baseline.json

```bash

### Expected Performance Targets

| Operation | Target (Single Node) | Target (Cluster) |
| ----------- | --------------------- | ------------------ |
| GET (1KB) | 500K ops/s | 2M ops/s |
| GET (1MB) | 10 GB/s | 40 GB/s |
| PUT (1KB) | 200K ops/s | 800K ops/s |
| PUT (1MB) | 5 GB/s | 20 GB/s |
| LIST | 50K objects/s | 200K objects/s |
| DELETE | 100K ops/s | 400K ops/s |

### Stress Testing

```bash

# Generate load with warp
warp mixed \
  --host=localhost:9000 \
  --access-key=admin \
  --secret-key=password \
  --duration=5m \
  --concurrent=100 \
  --obj.size=1MiB

# Analyze results
warp analyze warp-mixed-*.csv.zst

```text

---

## Troubleshooting Performance Issues

### Common Issues and Solutions

1. **High latency on small objects**
   - Enable memory caching for small objects
   - Tune TCP settings for low latency
   - Use connection pooling

2. **Low throughput on large objects**
   - Increase multipart part size
   - Enable parallel uploads
   - Tune storage I/O scheduler

3. **Memory pressure**
   - Adjust cache sizes
   - Enable memory limits
   - Tune GC parameters

4. **CPU bottlenecks**
   - Enable SIMD optimizations
   - Adjust worker thread count
   - Use CPU affinity

5. **Network bottlenecks**
   - Enable RDMA if available
   - Tune TCP buffer sizes
   - Enable jumbo frames

### Diagnostic Commands

```bash

# Check system resources
nebulaio diag system

# Check storage performance
nebulaio diag storage --benchmark

# Check network performance
nebulaio diag network --target=node2:9000

# Generate diagnostic report
nebulaio diag report --output=diag-$(date +%Y%m%d).tar.gz

```

---

## Additional Resources

- [NebulaIO Documentation](../README.md)
- [Troubleshooting Guide](./TROUBLESHOOTING.md)
- [API Reference](./api/openapi.yaml)
- [Deployment Guides](./README.md#deployment-guides)
