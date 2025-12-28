# Storage Architecture

NebulaIO's storage layer is designed for durability, performance, and flexibility. This document covers the internals of how objects are stored, protected, and managed on disk.

## Storage Layer Overview

The storage subsystem consists of multiple components that work together to provide reliable, high-performance object storage.

```
                              ┌──────────────────────────────────────────────────────────┐
                              │                    Storage Layer                         │
                              ├──────────────────────────────────────────────────────────┤
                              │                                                          │
                              │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
                              │  │   DRAM      │  │ Compression │  │ Encryption  │      │
                              │  │   Cache     │  │   Layer     │  │   Layer     │      │
                              │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘      │
                              │         │                │                │              │
                              │         └────────────────┼────────────────┘              │
                              │                          │                               │
                              │                  ┌───────▼───────┐                       │
                              │                  │ Erasure Coding│                       │
                              │                  │  Reed-Solomon │                       │
                              │                  └───────┬───────┘                       │
                              │                          │                               │
                              │  ┌───────────────────────┼───────────────────────┐      │
                              │  │                       │                       │      │
                              │  ▼                       ▼                       ▼      │
                              │ ┌────────┐          ┌────────┐          ┌────────┐     │
                              │ │ Shard 1│   ...    │ Shard N│   ...    │Shard M │     │
                              │ │ (Data) │          │ (Data) │          │(Parity)│     │
                              │ └────┬───┘          └────┬───┘          └────┬───┘     │
                              │      │                   │                   │          │
                              └──────┼───────────────────┼───────────────────┼──────────┘
                                     │                   │                   │
                              ┌──────▼───────┐   ┌───────▼──────┐   ┌───────▼──────┐
                              │   Disk 1     │   │   Disk N     │   │   Disk M     │
                              │   /dev/sda   │   │   /dev/sdn   │   │   /dev/sdm   │
                              └──────────────┘   └──────────────┘   └──────────────┘
```

---

## File System Backend

NebulaIO's primary storage backend uses the local filesystem. This provides portability across different storage media (NVMe, SSD, HDD) and works with any POSIX-compliant filesystem.

### Directory Structure

```
/data
├── metadata/                     # Raft and metadata storage
│   ├── raft/                     # Raft consensus logs
│   │   ├── logs/                 # Write-ahead log entries
│   │   └── snapshots/            # Periodic state snapshots
│   └── kv/                       # Key-value metadata store
│       ├── buckets.db            # Bucket definitions
│       ├── objects.db            # Object metadata index
│       └── users.db              # User and IAM data
│
├── objects/                      # Object data storage
│   └── {bucket-hash}/            # 2-char hash prefix for distribution
│       └── {bucket-name}/
│           └── {key-hash}/       # 4-char hash for key distribution
│               └── {version}/    # Version ID (or "null" for unversioned)
│                   ├── data      # Object content (or shard references)
│                   ├── meta.json # Object metadata
│                   └── parts/    # Multipart upload parts
│
├── shards/                       # Erasure coded shards
│   └── {shard-set-id}/
│       ├── shard.0              # Data shard 0
│       ├── shard.1              # Data shard 1
│       ├── ...
│       ├── shard.N              # Data shard N
│       └── shard.P              # Parity shards
│
└── temp/                         # Temporary upload staging
    └── {upload-id}/
        └── parts/
```

### Backend Interface

The storage backend implements a clean interface for pluggability:

```go
type Backend interface {
    Init(ctx context.Context) error
    Close() error
    PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64) (*PutResult, error)
    GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
    DeleteObject(ctx context.Context, bucket, key string) error
    ObjectExists(ctx context.Context, bucket, key string) (bool, error)
    CreateBucket(ctx context.Context, bucket string) error
    DeleteBucket(ctx context.Context, bucket string) error
    BucketExists(ctx context.Context, bucket string) (bool, error)
    GetStorageInfo(ctx context.Context) (*StorageInfo, error)
}
```

---

## Volume Storage Backend

The Volume storage backend is a high-performance alternative to the filesystem backend. Instead of storing one file per object (which incurs filesystem overhead), objects are stored in large pre-allocated volume files with efficient block-based allocation.

### When to Use Volume Backend

| Use Case | Recommended Backend |
|----------|---------------------|
| Mixed object sizes (small + large) | **Volume** |
| Millions of small objects (<1MB) | **Volume** |
| Maximum IOPS performance | **Volume** |
| Simple deployment, portability | Filesystem |
| Need multipart upload support | Filesystem |

### Volume Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Volume Storage Architecture                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Volume Manager                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  - Routes objects to appropriate volumes                             │  │
│  │  - Manages multi-volume capacity                                     │  │
│  │  - Auto-creates new volumes when needed                              │  │
│  │  - Maintains global object index                                     │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                    │                                        │
│                   ┌────────────────┼────────────────┐                       │
│                   ▼                ▼                ▼                       │
│  ┌────────────────────┐ ┌────────────────────┐ ┌────────────────────┐      │
│  │  Volume 1 (32GB)   │ │  Volume 2 (32GB)   │ │  Volume N (32GB)   │      │
│  │  ┌──────────────┐  │ │  ┌──────────────┐  │ │  ┌──────────────┐  │      │
│  │  │  Superblock  │  │ │  │  Superblock  │  │ │  │  Superblock  │  │      │
│  │  ├──────────────┤  │ │  ├──────────────┤  │ │  ├──────────────┤  │      │
│  │  │ Alloc Bitmap │  │ │  │ Alloc Bitmap │  │ │  │ Alloc Bitmap │  │      │
│  │  ├──────────────┤  │ │  ├──────────────┤  │ │  ├──────────────┤  │      │
│  │  │  Block Types │  │ │  │  Block Types │  │ │  │  Block Types │  │      │
│  │  ├──────────────┤  │ │  ├──────────────┤  │ │  ├──────────────┤  │      │
│  │  │ Object Index │  │ │  │ Object Index │  │ │  │ Object Index │  │      │
│  │  ├──────────────┤  │ │  ├──────────────┤  │ │  ├──────────────┤  │      │
│  │  │              │  │ │  │              │  │ │  │              │  │      │
│  │  │ Data Blocks  │  │ │  │ Data Blocks  │  │ │  │ Data Blocks  │  │      │
│  │  │   (4MB each) │  │ │  │   (4MB each) │  │ │  │   (4MB each) │  │      │
│  │  │              │  │ │  │              │  │ │  │              │  │      │
│  │  └──────────────┘  │ │  └──────────────┘  │ │  └──────────────┘  │      │
│  └────────────────────┘ └────────────────────┘ └────────────────────┘      │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Volume File Format

Each volume file is a pre-allocated file (default 32GB) with the following layout:

```
┌──────────────────────────────────────────────────────────────────────────┐
│ Offset 0        │ Superblock (4KB)                                      │
│                 │ - Magic: "NEBULAVL"                                    │
│                 │ - Volume ID (UUID), size, block count                  │
│                 │ - Object count, creation/modified timestamps           │
├─────────────────┼───────────────────────────────────────────────────────┤
│ Offset 4KB      │ Allocation Bitmap (4KB)                               │
│                 │ - 1 bit per 4MB block                                  │
│                 │ - 0 = free, 1 = allocated                              │
├─────────────────┼───────────────────────────────────────────────────────┤
│ Offset 8KB      │ Block Type Map (32KB)                                 │
│                 │ - 1 byte per block indicating type                     │
│                 │ - FREE, LARGE, PACKED_TINY, PACKED_SMALL, SPANNING    │
├─────────────────┼───────────────────────────────────────────────────────┤
│ Offset 40KB     │ Object Index (up to 64MB)                             │
│                 │ - Sorted by key hash for O(log n) lookups              │
│                 │ - Includes: key, block#, offset, size, timestamps      │
├─────────────────┼───────────────────────────────────────────────────────┤
│ Offset 64MB     │ Data Blocks (4MB each)                                │
│                 │ - Block 0: First data block                            │
│                 │ - Block N: Last data block                             │
│                 │ - Each block can be: Packed, Large, or Spanning        │
└──────────────────────────────────────────────────────────────────────────┘
```

### Size Classes and Packing

Small objects are packed together into blocks to minimize wasted space:

| Size Class | Object Size    | Block Type    | Packing Behavior |
|------------|----------------|---------------|------------------|
| TINY       | ≤ 64KB         | PACKED_TINY   | Many per block   |
| SMALL      | ≤ 256KB        | PACKED_SMALL  | Several per block|
| MEDIUM     | ≤ 1MB          | PACKED_MED    | Few per block    |
| LARGE      | ≤ 4MB          | LARGE         | One per block    |
| SPANNING   | > 4MB          | SPANNING      | Multiple blocks  |

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Packed Block Structure (4MB)                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  Block Header (64 bytes)                                              │  │
│  │  - Type, object count, free offset, checksum                         │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  Object 1                                                             │  │
│  │  ┌─────────────┐ ┌─────────────────┐ ┌─────────────────────────────┐ │  │
│  │  │ Entry (32B) │ │ Key (var len)   │ │ Data (var len)              │ │  │
│  │  │ KeyHash,    │ │ "bucket/key1"   │ │ [object bytes...]           │ │  │
│  │  │ Size, CRC   │ │                 │ │                             │ │  │
│  │  └─────────────┘ └─────────────────┘ └─────────────────────────────┘ │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  Object 2                                                             │  │
│  │  ┌─────────────┐ ┌─────────────────┐ ┌─────────────────────────────┐ │  │
│  │  │ Entry (32B) │ │ Key (var len)   │ │ Data (var len)              │ │  │
│  │  └─────────────┘ └─────────────────┘ └─────────────────────────────┘ │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  Object N ...                                                         │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  [Free Space]                                                         │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Configuration

```yaml
storage:
  backend: volume    # Use volume storage backend

  volume:
    max_volume_size: 34359738368   # 32GB per volume file
    auto_create: true               # Create new volumes as needed
```

### Performance Characteristics

| Metric | Volume Backend | Filesystem Backend |
|--------|----------------|-------------------|
| Small object write | ~50μs | ~500μs |
| Small object read | ~20μs | ~200μs |
| Metadata overhead | ~50 bytes/obj | ~4KB/obj (inode) |
| Files created | 1 per 32GB | 1 per object |
| Best for | Many small objects | Large objects |

### Direct I/O (O_DIRECT) Support

The volume backend supports direct I/O (O_DIRECT on Linux) to bypass the kernel page cache. This provides:

- **Predictable latency**: Avoids page cache eviction unpredictability
- **No double-buffering**: Data goes directly from application buffers to disk
- **Better memory utilization**: Application manages its own buffers efficiently
- **Reduced CPU overhead**: Kernel doesn't need to copy data between buffers

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Regular I/O vs Direct I/O                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Regular I/O (O_RDWR)                    Direct I/O (O_DIRECT)              │
│                                                                             │
│  ┌──────────────┐                        ┌──────────────┐                   │
│  │  Application │                        │  Application │                   │
│  │  Buffer      │                        │  Aligned     │                   │
│  └──────┬───────┘                        │  Buffer      │                   │
│         │ copy                           └──────┬───────┘                   │
│         ▼                                       │                           │
│  ┌──────────────┐                               │ DMA                       │
│  │  Kernel      │                               │ (Direct Memory Access)    │
│  │  Page Cache  │                               │                           │
│  └──────┬───────┘                               │                           │
│         │ copy                                  │                           │
│         ▼                                       ▼                           │
│  ┌──────────────┐                        ┌──────────────┐                   │
│  │  Disk        │                        │  Disk        │                   │
│  └──────────────┘                        └──────────────┘                   │
│                                                                             │
│  Latency: Variable (cache hit/miss)       Latency: Predictable              │
│  Memory: 2x (app + kernel buffers)        Memory: 1x (app buffers only)     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Alignment Requirements

Direct I/O requires proper alignment:

| Requirement | Value | Description |
|-------------|-------|-------------|
| Buffer alignment | 4096 bytes | Memory buffers must be 4KB aligned |
| Offset alignment | 4096 bytes | File offsets must be 4KB aligned |
| Size alignment | 4096 bytes | Transfer sizes must be multiples of 4KB |

The volume backend automatically handles alignment using an aligned buffer pool:

```go
// Automatically allocates 4KB-aligned buffers for O_DIRECT
buf := dio.GetAlignedBuffer(BlockSize)  // 4MB aligned buffer
defer dio.PutAlignedBuffer(buf)         // Return to pool for reuse
```

#### Platform Support

| Platform | O_DIRECT | Fallback |
|----------|----------|----------|
| Linux | ✅ Full support | N/A |
| macOS | ❌ Not available | Buffered I/O |
| Windows | ❌ Not available | Buffered I/O |

On non-Linux platforms, the volume backend automatically falls back to buffered I/O with no configuration changes required.

#### Configuration

```yaml
storage:
  backend: volume
  volume:
    max_volume_size: 34359738368   # 32GB
    auto_create: true
    direct_io:
      enabled: true                # Enable O_DIRECT on Linux
      block_alignment: 4096        # 4KB alignment (standard)
      use_memory_pool: true        # Pool aligned buffers
      fallback_on_error: true      # Fall back to buffered I/O on errors
```

#### Performance Impact

| Metric | Buffered I/O | Direct I/O | Notes |
|--------|--------------|------------|-------|
| Write latency (p50) | 100μs | 80μs | More consistent |
| Write latency (p99) | 5ms | 1ms | No cache flush spikes |
| Read latency (p50) | 50μs | 150μs | Cold read penalty |
| Read latency (p99) | 10ms | 500μs | Predictable |
| Memory usage | 2x | 1x | No kernel buffers |

**Recommendation**: Enable direct I/O for workloads that:
- Have their own caching layer (like DRAM cache)
- Need predictable latency
- Are write-heavy
- Have large objects (>64KB)

### Limitations

- Multipart uploads not yet supported (use filesystem backend if required)
- No inline compression (can be added in future)
- Compaction not yet implemented (space from deleted objects reclaimed on restart)

---

## Raw Block Device Support

For maximum I/O performance, NebulaIO can bypass the filesystem entirely and write directly to raw block devices (e.g., `/dev/nvme0n1`, `/dev/sdb`). This eliminates filesystem overhead and provides the most predictable latency.

### When to Use Raw Block Devices

| Use Case | Recommended Approach |
|----------|----------------------|
| Maximum IOPS/throughput | **Raw block device** |
| Ultra-low latency (< 100μs) | **Raw block device** |
| NVMe drives dedicated to storage | **Raw block device** |
| Mixed workloads, portability | Volume files on filesystem |
| Shared storage, snapshots needed | Volume files on filesystem |

### Raw Device Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Raw Block Device Architecture                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Volume Manager                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  - Routes objects to appropriate devices/tiers                      │  │
│  │  - Manages tiered storage (NVMe hot, SSD warm, HDD cold)           │  │
│  │  - Maintains global object index                                    │  │
│  │  - Handles data migration between tiers                             │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                    │                                        │
│         ┌──────────────────────────┼──────────────────────────┐            │
│         ▼                          ▼                          ▼            │
│  ┌────────────────────┐ ┌────────────────────┐ ┌────────────────────┐      │
│  │  HOT TIER (NVMe)   │ │  WARM TIER (SSD)   │ │  COLD TIER (HDD)   │      │
│  │  /dev/nvme0n1      │ │  /dev/sda          │ │  /dev/sdb          │      │
│  │  ┌──────────────┐  │ │  ┌──────────────┐  │ │  ┌──────────────┐  │      │
│  │  │  O_DIRECT    │  │ │  │  O_DIRECT    │  │ │  │  O_DIRECT    │  │      │
│  │  │  No FS layer │  │ │  │  No FS layer │  │ │  │  No FS layer │  │      │
│  │  └──────────────┘  │ │  └──────────────┘  │ │  └──────────────┘  │      │
│  │                    │ │                    │ │                    │      │
│  │  ┌──────────────┐  │ │  ┌──────────────┐  │ │  ┌──────────────┐  │      │
│  │  │  Superblock  │  │ │  │  Superblock  │  │ │  │  Superblock  │  │      │
│  │  ├──────────────┤  │ │  ├──────────────┤  │ │  ├──────────────┤  │      │
│  │  │ Alloc Bitmap │  │ │  │ Alloc Bitmap │  │ │  │ Alloc Bitmap │  │      │
│  │  ├──────────────┤  │ │  ├──────────────┤  │ │  ├──────────────┤  │      │
│  │  │ Data Blocks  │  │ │  │ Data Blocks  │  │ │  │ Data Blocks  │  │      │
│  │  │   (4MB)      │  │ │  │   (4MB)      │  │ │  │   (4MB)      │  │      │
│  │  └──────────────┘  │ │  └──────────────┘  │ │  └──────────────┘  │      │
│  └────────────────────┘ └────────────────────┘ └────────────────────┘      │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### I/O Path Comparison

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         I/O Path Comparison                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Filesystem Backend                Raw Block Device                         │
│  (Current)                         (New)                                    │
│                                                                             │
│  ┌──────────────┐                  ┌──────────────┐                        │
│  │  Application │                  │  Application │                        │
│  └──────┬───────┘                  └──────┬───────┘                        │
│         │                                 │                                 │
│         ▼                                 │                                 │
│  ┌──────────────┐                         │                                 │
│  │  Volume File │                         │                                 │
│  │  (32GB .neb) │                         │                                 │
│  └──────┬───────┘                         │                                 │
│         │                                 │                                 │
│         ▼                                 │                                 │
│  ┌──────────────┐                         │ O_DIRECT                        │
│  │  Filesystem  │                         │ (bypass all)                    │
│  │  (ext4/xfs)  │                         │                                 │
│  └──────┬───────┘                         │                                 │
│         │                                 │                                 │
│         ▼                                 ▼                                 │
│  ┌──────────────┐                  ┌──────────────┐                        │
│  │ Block Device │                  │ Block Device │                        │
│  │  /dev/sda1   │                  │  /dev/nvme0  │                        │
│  └──────────────┘                  └──────────────┘                        │
│                                                                             │
│  Latency: 100-500μs                Latency: 20-50μs                        │
│  Overhead: FS + journaling         Overhead: None                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Configuration

```yaml
storage:
  backend: volume

  volume:
    # Use raw block devices instead of volume files
    raw_devices:
      enabled: true

      # Define devices by tier for tiered storage
      devices:
        # Hot tier - fastest NVMe drives for active data
        - path: /dev/nvme0n1
          tier: hot
          size: 0              # 0 = use entire device

        - path: /dev/nvme1n1
          tier: hot
          size: 0

        # Warm tier - SSDs for moderately accessed data
        - path: /dev/sda
          tier: warm
          size: 0

        # Cold tier - HDDs for infrequently accessed data
        - path: /dev/sdb
          tier: cold
          size: 0

        - path: /dev/sdc
          tier: cold
          size: 0

    # Still support volume files for mixed configurations
    auto_create: true          # Auto-create file-based volumes
    max_volume_size: 34359738368  # For file-based volumes

    direct_io:
      enabled: true            # Always use O_DIRECT for raw devices
      block_alignment: 4096
      use_memory_pool: true
```

### Tiered Device Configuration

Assign different physical devices to different storage tiers:

```yaml
storage:
  backend: volume

  volume:
    raw_devices:
      enabled: true

      # Tier definitions with device assignments
      tiers:
        hot:
          devices:
            - /dev/nvme0n1     # 1TB NVMe
            - /dev/nvme1n1     # 1TB NVMe
          total_capacity: 2TB
          latency_target: 50μs

        warm:
          devices:
            - /dev/sda         # 4TB SSD
            - /dev/sdb         # 4TB SSD
          total_capacity: 8TB
          latency_target: 1ms

        cold:
          devices:
            - /dev/sdc         # 16TB HDD
            - /dev/sdd         # 16TB HDD
          total_capacity: 32TB
          latency_target: 20ms

# Tiering policies work with raw devices
tiering:
  enabled: true
  policies:
    - name: default-lifecycle
      transitions:
        - days: 7
          from_tier: hot
          to_tier: warm
        - days: 30
          from_tier: warm
          to_tier: cold
```

### Device Initialization

Raw devices must be initialized before use:

```bash
# Initialize a raw device for NebulaIO
nebulaio-cli storage device init /dev/nvme0n1 --tier hot

# List configured devices
nebulaio-cli storage device list
# DEVICE         TIER   SIZE      USED      FREE      STATUS
# /dev/nvme0n1   hot    1.0 TB    456 GB    544 GB    healthy
# /dev/nvme1n1   hot    1.0 TB    389 GB    611 GB    healthy
# /dev/sda       warm   4.0 TB    2.1 TB    1.9 TB    healthy

# Check device health
nebulaio-cli storage device health /dev/nvme0n1
```

### Safety Features

Raw block device access requires additional safety measures:

| Feature | Description |
|---------|-------------|
| Device validation | Verifies device is not mounted and has no filesystem |
| Exclusive access | Uses `flock()` to prevent concurrent access |
| Device signatures | Writes NebulaIO signature to prevent accidental reuse |
| Graceful degradation | Falls back to file-based volumes if device unavailable |

```yaml
storage:
  volume:
    raw_devices:
      safety:
        # Verify device has no existing filesystem
        check_filesystem: true

        # Require explicit confirmation for new devices
        require_confirmation: true

        # Write device signature for identification
        write_signature: true

        # Lock device exclusively
        exclusive_lock: true
```

### Performance Characteristics

| Metric | File-Based Volume | Raw Block Device | Improvement |
|--------|-------------------|------------------|-------------|
| 4KB random read | 80μs | 25μs | 3.2x |
| 4KB random write | 100μs | 30μs | 3.3x |
| 4MB sequential read | 2ms | 0.5ms | 4x |
| 4MB sequential write | 3ms | 0.8ms | 3.75x |
| IOPS (4KB) | 50,000 | 150,000 | 3x |
| Throughput | 2 GB/s | 6 GB/s | 3x |

*Benchmarks on enterprise NVMe (Intel P5510)*

### Kubernetes Deployment

For Kubernetes deployments, raw block devices require special configuration:

```yaml
# values.yaml for Helm chart
persistence:
  enabled: true

  # Standard PVC for metadata/WAL
  storageClass: "standard"
  size: 10Gi

  # Raw block devices (requires privileged mode)
  rawDevices:
    enabled: true
    devices:
      - name: hot-nvme
        path: /dev/nvme0n1
        tier: hot
      - name: warm-ssd
        path: /dev/sda
        tier: warm

# Pod must run privileged for raw device access
podSecurityContext:
  privileged: true

# Node selector for nodes with specific devices
nodeSelector:
  storage.nebulaio.io/nvme: "true"
  storage.nebulaio.io/tier-hot: "true"
```

### Limitations

- **No snapshots**: Filesystem-level snapshots not available
- **Exclusive access**: Device cannot be shared with other applications
- **Manual partitioning**: Device must be dedicated to NebulaIO
- **Privileged mode**: Requires elevated privileges in containers
- **No resize**: Cannot dynamically resize raw device allocation

---

## Object Storage Format

Each object is stored with its content and metadata in a structured format.

### Object Metadata (meta.json)

```json
{
  "bucket": "my-bucket",
  "key": "documents/report.pdf",
  "version_id": "v2024123456789",
  "size": 1048576,
  "content_type": "application/pdf",
  "etag": "d41d8cd98f00b204e9800998ecf8427e",
  "created_at": "2024-01-15T10:30:00Z",
  "modified_at": "2024-01-15T10:30:00Z",
  "storage_class": "STANDARD",
  "user_metadata": {
    "x-amz-meta-author": "John Doe"
  },
  "encryption": {
    "algorithm": "AES256-GCM",
    "key_id": "arn:aws:kms:us-east-1:123:key/abc"
  },
  "compression": {
    "algorithm": "zstd",
    "original_size": 2097152
  },
  "erasure": {
    "data_shards": 10,
    "parity_shards": 4,
    "shard_size": 104858,
    "shard_set_id": "abc123def456"
  }
}
```

---

## Erasure Coding Implementation

NebulaIO uses Reed-Solomon erasure coding to protect data against disk failures without full replication.

### How It Works

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Reed-Solomon Erasure Coding                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Original Data (1 MB)                                                       │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │ ██████████████████████████████████████████████████████████████████  │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                    │                                        │
│                                    ▼                                        │
│  Split into Data Shards (10 x 102.4 KB each)                               │
│  ┌────┐┌────┐┌────┐┌────┐┌────┐┌────┐┌────┐┌────┐┌────┐┌────┐             │
│  │ D0 ││ D1 ││ D2 ││ D3 ││ D4 ││ D5 ││ D6 ││ D7 ││ D8 ││ D9 │             │
│  └────┘└────┘└────┘└────┘└────┘└────┘└────┘└────┘└────┘└────┘             │
│                                    │                                        │
│                                    ▼                                        │
│  Generate Parity Shards (4 x 102.4 KB each)                                │
│  ┌────┐┌────┐┌────┐┌────┐                                                  │
│  │ P0 ││ P1 ││ P2 ││ P3 │  ← Computed from D0-D9 using GF(2^8) math       │
│  └────┘└────┘└────┘└────┘                                                  │
│                                                                             │
│  Total: 14 shards (10 data + 4 parity) = 1.43 MB (43% overhead)            │
│  Can lose ANY 4 shards and still recover data                              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Configuration Presets

| Preset   | Data Shards | Parity Shards | Total | Max Failures | Overhead |
|----------|-------------|---------------|-------|--------------|----------|
| Minimal  | 4           | 2             | 6     | 2            | 50%      |
| Standard | 10          | 4             | 14    | 4            | 40%      |
| Maximum  | 8           | 8             | 16    | 8            | 100%     |

### Encoding Process

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐     ┌──────────────┐
│  Read Data  │────►│ Pad to Even  │────►│   Split     │────►│  RS Encode   │
│  Stream     │     │  Shard Size  │     │  into N     │     │  Add Parity  │
└─────────────┘     └──────────────┘     │  Shards     │     └──────┬───────┘
                                         └─────────────┘            │
                                                                    ▼
┌─────────────┐     ┌──────────────┐     ┌─────────────┐     ┌──────────────┐
│   Verify    │◄────│  Distribute  │◄────│  Checksum   │◄────│   14 Total   │
│   Storage   │     │  to Disks    │     │  Each Shard │     │   Shards     │
└─────────────┘     └──────────────┘     └─────────────┘     └──────────────┘
```

### Decoding Process

When reading an object, only the data shards are needed if all are available. If any shards are missing or corrupted, the Reed-Solomon decoder reconstructs them:

```
Available Shards:  D0 D1 D2 __ D4 D5 __ D7 D8 D9 P0 __ P2 P3
                            ↑       ↑          ↑
                        D3 missing  D6 missing  P1 missing

Step 1: Verify checksums of available shards
Step 2: Reconstruct D3 and D6 using P0, P2, P3
Step 3: Concatenate D0-D9 to recover original data
Step 4: Remove padding and return data
```

---

## Data Sharding and Distribution

### Shard Placement Strategy

Shards are distributed across available storage devices using a placement algorithm that ensures:

1. **No two shards on the same disk** - Maximizes fault tolerance
2. **Even distribution** - Balances storage utilization
3. **Locality awareness** - Considers rack/zone topology

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Cluster Topology                                  │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  Zone A                    Zone B                    Zone C             │
│  ┌─────────────────┐      ┌─────────────────┐      ┌─────────────────┐ │
│  │ Rack 1          │      │ Rack 1          │      │ Rack 1          │ │
│  │ ┌─────┐ ┌─────┐ │      │ ┌─────┐ ┌─────┐ │      │ ┌─────┐ ┌─────┐ │ │
│  │ │Node1│ │Node2│ │      │ │Node5│ │Node6│ │      │ │Node9│ │NodeA│ │ │
│  │ │D0,P1│ │D1,P2│ │      │ │D4,P0│ │D5   │ │      │ │D8   │ │D9,P3│ │ │
│  │ └─────┘ └─────┘ │      │ └─────┘ └─────┘ │      │ └─────┘ └─────┘ │ │
│  │ ┌─────┐ ┌─────┐ │      │ ┌─────┐ ┌─────┐ │      │ ┌─────┐ ┌─────┐ │ │
│  │ │Node3│ │Node4│ │      │ │Node7│ │Node8│ │      │ │NodeB│ │NodeC│ │ │
│  │ │D2   │ │D3   │ │      │ │D6   │ │D7   │ │      │ │     │ │     │ │ │
│  │ └─────┘ └─────┘ │      │ └─────┘ └─────┘ │      │ └─────┘ └─────┘ │ │
│  └─────────────────┘      └─────────────────┘      └─────────────────┘ │
│                                                                         │
│  Distribution ensures zone failure tolerance: Any zone can fail        │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

### Placement Algorithm

```go
// Simplified placement logic
func PlaceShards(shards []Shard, topology ClusterTopology) []Placement {
    placements := make([]Placement, len(shards))
    usedNodes := make(map[string]bool)

    for i, shard := range shards {
        // Select node in least-used zone that hasn't been used
        node := selectOptimalNode(topology, usedNodes)
        placements[i] = Placement{
            ShardIndex: i,
            NodeID:     node.ID,
            DiskPath:   node.SelectDisk(),
        }
        usedNodes[node.ID] = true
    }
    return placements
}
```

---

## Compression

NebulaIO supports transparent compression to reduce storage costs and improve throughput for compressible data.

### Supported Algorithms

| Algorithm | Speed     | Ratio    | Use Case                    |
|-----------|-----------|----------|-----------------------------|
| Zstandard | Fast      | Excellent| Default, balanced           |
| LZ4       | Very Fast | Good     | High-throughput workloads   |
| Gzip      | Moderate  | Good     | Maximum compatibility       |

### Compression Levels

| Level   | Speed Impact | Compression Ratio |
|---------|--------------|-------------------|
| Fastest | Minimal      | Lower             |
| Default | Balanced     | Balanced          |
| Best    | Significant  | Maximum           |

### Compression Decision Flow

```
┌──────────────┐     ┌────────────────┐     ┌───────────────┐
│ Object Write │────►│ Check Size     │────►│ Check Content │
│              │     │ >= 1KB ?       │     │ Type          │
└──────────────┘     └───────┬────────┘     └───────┬───────┘
                             │                      │
                     No ─────┤                      │
                             │              Excluded ────────┐
                     Yes ────▼                      │        │
               ┌─────────────────────┐      Yes ────┘        │
               │ Compress with       │                       │
               │ selected algorithm  │                       ▼
               └──────────┬──────────┘            ┌──────────────────┐
                          │                       │ Store Uncompressed│
                          ▼                       └──────────────────┘
               ┌─────────────────────┐
               │ Compressed < Original│
               │ ?                    │
               └──────────┬──────────┘
                          │
                  No ─────┤
                          │
                  Yes ────▼
               ┌─────────────────────┐
               │ Store Compressed    │
               │ + Metadata          │
               └─────────────────────┘
```

### Excluded Content Types

The following content types are not compressed (already compressed):

- Images: `image/jpeg`, `image/png`, `image/gif`, `image/webp`
- Video: `video/mp4`, `video/webm`, `video/mpeg`
- Audio: `audio/mpeg`, `audio/mp4`, `audio/ogg`
- Archives: `application/zip`, `application/gzip`, `application/x-7z-compressed`

---

## Encryption at Rest

NebulaIO implements envelope encryption for data protection.

### Encryption Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Envelope Encryption                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  KMS (Key Management Service)                                               │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  Master Key (KEK - Key Encryption Key)                               │  │
│  │  - Stored securely in KMS (Vault, AWS KMS, local)                    │  │
│  │  - Never leaves KMS boundary                                          │  │
│  │  - Used to wrap/unwrap Data Encryption Keys                          │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                    │                                        │
│                                    │ Wraps/Unwraps                         │
│                                    ▼                                        │
│  Data Encryption Key (DEK)                                                  │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  - Generated per object or set of objects                            │  │
│  │  - Wrapped (encrypted) by KEK before storage                         │  │
│  │  - Plaintext DEK cached briefly, then cleared from memory            │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                    │                                        │
│                                    │ Encrypts                              │
│                                    ▼                                        │
│  Object Data                                                                │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  Plaintext ──► AES-256-GCM ──► Ciphertext + Auth Tag                 │  │
│  │                    or                                                 │  │
│  │  Plaintext ──► ChaCha20-Poly1305 ──► Ciphertext + Auth Tag           │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Supported Algorithms

| Algorithm         | Key Size | Block Size | Authentication |
|-------------------|----------|------------|----------------|
| AES-256-GCM       | 256 bits | 128 bits   | GHASH          |
| ChaCha20-Poly1305 | 256 bits | Stream     | Poly1305       |

### Key Rotation

```
┌────────────────┐     ┌────────────────┐     ┌────────────────┐
│  Current DEK   │     │  New DEK       │     │  Object        │
│  (wrapped)     │     │  (generated)   │     │  Re-encrypted  │
└───────┬────────┘     └───────┬────────┘     └───────┬────────┘
        │                      │                      │
        ▼                      ▼                      ▼
   Decrypt data          Encrypt data           Store new
   with old DEK          with new DEK           wrapped DEK
```

---

## Storage Pools and Tiering

NebulaIO supports multiple storage tiers for cost optimization.

### Tier Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Storage Tiers                                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  HOT TIER (DRAM Cache)                                               │  │
│  │  ┌──────────┐                                                        │  │
│  │  │ ARC Cache│  Latency: < 50us   Capacity: 1-256 GB                 │  │
│  │  └──────────┘  Use: Frequently accessed objects                      │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                          │ Cache Miss                                       │
│                          ▼                                                  │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  WARM TIER (Primary Storage - NVMe/SSD)                              │  │
│  │  ┌──────────┐                                                        │  │
│  │  │ FS Store │  Latency: < 1ms    Capacity: TBs                      │  │
│  │  └──────────┘  Use: Active data, recent uploads                      │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                          │ Policy Transition                                │
│                          ▼                                                  │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  COLD TIER (HDD / Network Storage)                                   │  │
│  │  ┌──────────┐                                                        │  │
│  │  │Cold Store│  Latency: 10-100ms Capacity: PBs                      │  │
│  │  └──────────┘  Use: Infrequently accessed, cost-sensitive            │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                          │ Policy Transition                                │
│                          ▼                                                  │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  ARCHIVE TIER (Tape / Glacier-like)                                  │  │
│  │  ┌──────────┐                                                        │  │
│  │  │ Archive  │  Latency: Hours    Capacity: Unlimited                │  │
│  │  └──────────┘  Use: Compliance, long-term retention                  │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Transition Policies

```yaml
tiering:
  enabled: true
  policies:
    - name: archive-old-logs
      filters:
        - field: key_prefix
          operator: prefix
          value: "logs/"
        - field: age_days
          operator: gt
          value: 90
      target_tier: cold

    - name: archive-after-year
      filters:
        - field: age_days
          operator: gt
          value: 365
      target_tier: archive
```

---

## Garbage Collection

NebulaIO implements background garbage collection to reclaim storage from deleted objects.

### GC Process

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Garbage Collection                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. Mark Phase                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  - Scan metadata for deleted objects                                 │  │
│  │  - Identify orphaned shards (no object references)                   │  │
│  │  - Mark expired multipart uploads                                    │  │
│  │  - Respect delete markers for versioned buckets                      │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                    │                                        │
│                                    ▼                                        │
│  2. Grace Period                                                            │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  - Wait for configured grace period (default: 24 hours)              │  │
│  │  - Allows recovery from accidental deletion                          │  │
│  │  - Ensures all in-flight operations complete                         │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                    │                                        │
│                                    ▼                                        │
│  3. Sweep Phase                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  - Delete marked object data files                                   │  │
│  │  - Remove orphaned shards from all nodes                             │  │
│  │  - Clean up temporary upload directories                             │  │
│  │  - Update storage metrics                                            │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### GC Configuration

```yaml
storage:
  gc:
    enabled: true
    interval: 1h              # Run every hour
    grace_period: 24h         # Wait 24 hours before deletion
    batch_size: 1000          # Process 1000 objects per cycle
    max_workers: 4            # Parallel deletion workers
```

---

## Performance Considerations

### I/O Optimization

| Optimization | Description | Impact |
|--------------|-------------|--------|
| Direct I/O | Bypass kernel page cache for large objects | Reduced memory pressure |
| Read-ahead | Prefetch sequential data | Improved throughput |
| Write coalescing | Batch small writes | Reduced IOPS |
| Shard parallelism | Read/write shards concurrently | 10-14x parallelism |

### Tuning Parameters

```yaml
storage:
  backend:
    type: fs
    path: /data

  performance:
    # Buffer sizes
    read_buffer_size: 1MB
    write_buffer_size: 4MB

    # Concurrency
    max_concurrent_uploads: 100
    max_concurrent_downloads: 200
    shard_io_workers: 32

    # I/O settings
    use_direct_io: true         # For objects > 64MB
    sync_writes: false          # Use fsync on commit
    prefetch_enabled: true

  erasure:
    preset: standard            # 10+4 configuration
    shard_size: 1MB
```

### Performance Metrics

| Metric | Description | Target |
|--------|-------------|--------|
| Write latency (p99) | Time to write and confirm | < 50ms |
| Read latency (p99) | Time to first byte | < 10ms |
| Throughput | Sustained transfer rate | > 10 GB/s aggregate |
| IOPS | Small object operations | > 50,000 ops/sec |

---

## Next Steps

- [Clustering & HA](clustering.md) - Distributed deployment patterns
- [Security Model](security.md) - Authentication and authorization
- [Performance Tuning](../PERFORMANCE_TUNING.md) - Optimization guide
