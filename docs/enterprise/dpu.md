# BlueField DPU Support

NebulaIO supports NVIDIA BlueField Data Processing Units (DPUs) for hardware-accelerated cryptography, compression, and RDMA offload.

## Overview

BlueField DPUs are SmartNICs that offload data path processing from the CPU:

- **Crypto Offload** - AES-GCM encryption at line rate
- **Compression Offload** - Hardware Deflate/LZ4 compression
- **RDMA Acceleration** - Direct memory access over network
- **Storage Offload** - NVMe-oF target acceleration

## Requirements

### Hardware

- NVIDIA BlueField-2 or BlueField-3 DPU
- Supported server platform (PCIe 4.0+ recommended)

### Software

- NVIDIA DOCA SDK 2.0+
- BlueField firmware 24.x+
- NebulaIO with DPU support enabled

## Configuration

### Basic Setup

```yaml
dpu:
  enabled: true
  device_name: mlx5_0
  enable_crypto: true
  enable_compression: true
```

### Advanced Configuration

```yaml
dpu:
  enabled: true
  device_name: mlx5_0
  enable_crypto: true           # AES-GCM offload
  enable_compression: true      # Deflate/LZ4 offload
  enable_rdma: true             # RDMA acceleration
  crypto_queue_size: 256        # Crypto work queue depth
  compression_queue_size: 256   # Compression work queue depth
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEBULAIO_DPU_ENABLED` | Enable DPU offload | `false` |
| `NEBULAIO_DPU_DEVICE_NAME` | DPU device | `mlx5_0` |
| `NEBULAIO_DPU_ENABLE_CRYPTO` | Enable crypto offload | `true` |
| `NEBULAIO_DPU_ENABLE_COMPRESSION` | Enable compression | `true` |

## Features

### Cryptographic Offload

Hardware-accelerated encryption:

```yaml
dpu:
  enabled: true
  enable_crypto: true
  crypto_queue_size: 512

# Combined with server-side encryption
storage:
  encryption:
    enabled: true
    algorithm: AES-256-GCM  # Offloaded to DPU
```

Supported algorithms:
- AES-128-GCM
- AES-256-GCM
- AES-128-XTS
- AES-256-XTS

### Compression Offload

Hardware-accelerated compression:

```yaml
dpu:
  enabled: true
  enable_compression: true
  compression_queue_size: 512

storage:
  compression: deflate  # Offloaded to DPU
```

Supported algorithms:
- Deflate (GZIP compatible)
- LZ4

### RDMA Acceleration

Network-level offload:

```yaml
dpu:
  enabled: true
  enable_rdma: true

rdma:
  enabled: true
  device_name: mlx5_0  # Same device as DPU
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Host System                              │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                     NebulaIO Server                          ││
│  │  ┌───────────┐  ┌───────────┐  ┌───────────────────────────┐││
│  │  │ S3 API    │  │ Object    │  │ Metadata                  │││
│  │  │ Handler   │  │ Service   │  │ Store                     │││
│  │  └─────┬─────┘  └─────┬─────┘  └───────────────────────────┘││
│  │        │              │                                      ││
│  │  ┌─────▼──────────────▼──────────────────────────────────┐  ││
│  │  │                DPU Offload Layer                       │  ││
│  │  │  ┌──────────┐  ┌──────────┐  ┌──────────────────────┐ │  ││
│  │  │  │  Crypto  │  │  Comp.   │  │  RDMA                │ │  ││
│  │  │  │  Offload │  │  Offload │  │  Offload             │ │  ││
│  │  │  └────┬─────┘  └────┬─────┘  └──────────┬───────────┘ │  ││
│  │  └───────┼─────────────┼───────────────────┼─────────────┘  ││
│  └──────────┼─────────────┼───────────────────┼────────────────┘│
│             │             │                   │                  │
│  ┌──────────▼─────────────▼───────────────────▼────────────────┐│
│  │                   BlueField DPU                              ││
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   ││
│  │  │  Crypto      │  │  Compression │  │  RDMA Engine     │   ││
│  │  │  Engine      │  │  Engine      │  │  (ConnectX)      │   ││
│  │  │  (AES-GCM)   │  │  (Deflate)   │  │                  │   ││
│  │  └──────────────┘  └──────────────┘  └──────────────────┘   ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

## Performance

### Crypto Offload Benchmarks

| Algorithm | CPU | DPU | Improvement |
|-----------|-----|-----|-------------|
| AES-256-GCM | 3.2 GB/s | 25 GB/s | 7.8x |
| AES-128-GCM | 4.1 GB/s | 25 GB/s | 6.1x |

### Compression Benchmarks

| Algorithm | CPU | DPU | Improvement |
|-----------|-----|-----|-------------|
| Deflate | 800 MB/s | 8 GB/s | 10x |
| LZ4 | 2.5 GB/s | 12 GB/s | 4.8x |

### CPU Savings

With DPU offload enabled:
- Crypto: 80-90% CPU reduction
- Compression: 70-85% CPU reduction
- RDMA: 60-75% CPU reduction

## Usage Examples

### Transparent Offload

When DPU is enabled, offload happens automatically:

```python
import boto3

s3 = boto3.client('s3', endpoint_url='http://localhost:9000')

# Upload with server-side encryption
# Encryption automatically offloaded to DPU
s3.put_object(
    Bucket='secure-bucket',
    Key='data.bin',
    Body=large_data,
    ServerSideEncryption='AES256'
)
```

### Monitoring Offload

Check DPU utilization:

```bash
# DPU statistics
curl http://localhost:9001/api/v1/admin/dpu/stats

# Response:
{
  "crypto_operations": 1234567,
  "crypto_bytes_processed": 12345678901234,
  "compression_operations": 234567,
  "compression_ratio": 3.2,
  "rdma_operations": 345678
}
```

## Troubleshooting

### Common Issues

1. **DPU not detected**
   - Check device presence: `lspci | grep Mellanox`
   - Verify driver: `modinfo mlx5_core`
   - Check DOCA SDK installation

2. **Low performance**
   - Increase queue sizes
   - Check DPU firmware version
   - Verify PCIe link speed

3. **Crypto errors**
   - Verify supported algorithms
   - Check key sizes

### Diagnostics

```bash
# DPU device info
mlxconfig -d /dev/mst/mt41686_pciconf0 query

# DOCA info
doca_info

# Performance counters
mlxlink -d mlx5_0 -c
```

### Logs

Enable debug logging:

```yaml
log_level: debug
```

Look for logs with `dpu` tag.

## Integration

### With GPUDirect

```yaml
dpu:
  enabled: true
  enable_rdma: true
gpudirect:
  enabled: true
# Data flows: GPU <-> DPU <-> Network (bypassing CPU)
```

### With S3 over RDMA

```yaml
dpu:
  enabled: true
  enable_rdma: true
  enable_crypto: true
rdma:
  enabled: true
  device_name: mlx5_0
# RDMA traffic encrypted by DPU at line rate
```

## Best Practices

### Queue Sizing

```yaml
dpu:
  # High throughput workloads
  crypto_queue_size: 512
  compression_queue_size: 512

  # Low latency workloads
  crypto_queue_size: 128
  compression_queue_size: 128
```

### Algorithm Selection

- **AES-256-GCM**: Best for encryption with integrity
- **Deflate**: Best compression ratio
- **LZ4**: Best compression speed

## See Also

- [S3 over RDMA](rdma.md) - Ultra-low latency access
- [GPUDirect Storage](gpudirect.md) - GPU data path
- [S3 Express One Zone](s3-express.md) - Low-latency storage
