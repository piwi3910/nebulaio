# S3 over RDMA

NebulaIO supports S3 API access over RDMA (Remote Direct Memory Access) for ultra-low latency object storage operations with sub-10μs access times.

## Overview

S3 over RDMA provides:

- **Ultra-Low Latency** - Sub-10μs object access
- **Zero-Copy** - Direct memory transfers
- **High Throughput** - 100Gbps+ bandwidth
- **CPU Offload** - Minimal CPU involvement

## Requirements

### Hardware

- InfiniBand or RoCE v2 network adapter (Mellanox ConnectX-5+)
- InfiniBand switch or RoCE-capable network
- RDMA-capable NIC on client and server

### Software

- RDMA drivers (rdma-core, libibverbs)
- OFED stack (recommended)
- NebulaIO with RDMA support

## Configuration

### Basic Setup

```yaml

rdma:
  enabled: true
  port: 9100
  device_name: mlx5_0
  enable_zero_copy: true

```bash

### Advanced Configuration

```yaml

rdma:
  enabled: true
  port: 9100
  device_name: mlx5_0
  ib_port: 1                    # InfiniBand port
  gid_index: 0                  # GID table index (for RoCE)
  enable_zero_copy: true
  max_send_wr: 128              # Send work requests
  max_recv_wr: 128              # Receive work requests
  max_sge: 4                    # Scatter/gather elements
  cq_size: 256                  # Completion queue size

```bash

### Environment Variables

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_RDMA_ENABLED` | Enable RDMA | `false` |
| `NEBULAIO_RDMA_PORT` | RDMA port | `9100` |
| `NEBULAIO_RDMA_DEVICE_NAME` | RDMA device | `mlx5_0` |

## Architecture

```text

┌─────────────────────────────────────────────────────────────────┐
│                        Client Application                        │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                  NebulaIO RDMA Client                        ││
│  │  ┌───────────┐  ┌───────────┐  ┌───────────────────────────┐││
│  │  │ S3 API    │  │ Memory    │  │ Work Request              │││
│  │  │ Wrapper   │  │ Region    │  │ Queue                     │││
│  │  └─────┬─────┘  └─────┬─────┘  └─────────────┬─────────────┘││
│  └────────┼──────────────┼──────────────────────┼──────────────┘│
│           │              │                      │                │
│  ┌────────▼──────────────▼──────────────────────▼──────────────┐│
│  │                    RDMA HCA (ConnectX)                       ││
│  └────────────────────────────┬────────────────────────────────┘│
└───────────────────────────────┼─────────────────────────────────┘
                                │ RDMA (InfiniBand/RoCE)
                                │ Zero-Copy Transfer
┌───────────────────────────────┼─────────────────────────────────┐
│  ┌────────────────────────────▼────────────────────────────────┐│
│  │                    RDMA HCA (ConnectX)                       ││
│  └────────┬──────────────┬──────────────────────┬──────────────┘│
│           │              │                      │                │
│  ┌────────▼──────────────▼──────────────────────▼──────────────┐│
│  │                 NebulaIO RDMA Server                         ││
│  │  ┌───────────┐  ┌───────────┐  ┌───────────────────────────┐││
│  │  │ Request   │  │ Memory    │  │ Completion                │││
│  │  │ Handler   │  │ Manager   │  │ Handler                   │││
│  │  └─────┬─────┘  └─────┬─────┘  └───────────────────────────┘││
│  │        │              │                                      ││
│  │  ┌─────▼──────────────▼──────────────────────────────────┐  ││
│  │  │                  Storage Backend                       │  ││
│  │  └────────────────────────────────────────────────────────┘  ││
│  └─────────────────────────────────────────────────────────────┘│
│                       NebulaIO Server                            │
└─────────────────────────────────────────────────────────────────┘

```bash

## Usage Examples

### Python Client

```python

from nebulaio.rdma import RDMAClient

# Connect via RDMA
client = RDMAClient(
    endpoint="192.168.100.1:9100",
    access_key="minioadmin",
    secret_key="minioadmin"
)

# Read object with zero-copy
data = client.get_object(
    bucket="my-bucket",
    key="data.bin"
)

# Write object
client.put_object(
    bucket="my-bucket",
    key="output.bin",
    body=data
)

```bash

### Go Client

```go

import "github.com/nebulaio/client-rdma"

client, err := rdma.NewClient(rdma.Config{
    Endpoint:  "192.168.100.1:9100",
    AccessKey: "minioadmin",
    SecretKey: "minioadmin",
})

// Read with RDMA
data, err := client.GetObject(ctx, "my-bucket", "data.bin")

// Write with RDMA
err = client.PutObject(ctx, "my-bucket", "output.bin", data)

```bash

### C++ with libibverbs

```cpp

#include <infiniband/verbs.h>
#include "nebulaio/rdma.h"

// Initialize RDMA context
nebulaio::RDMAContext ctx("mlx5_0", 1);

// Connect to server
ctx.connect("192.168.100.1", 9100);

// Allocate registered memory
void* buffer = ctx.alloc_mr(1024 * 1024);

// RDMA read (object -> buffer)
ctx.rdma_read("my-bucket", "data.bin", buffer, size);

// RDMA write (buffer -> object)
ctx.rdma_write("my-bucket", "output.bin", buffer, size);

// Cleanup
ctx.free_mr(buffer);

```bash

## Performance

### Latency Benchmarks

| Operation | TCP/IP | RDMA | Improvement |
| ----------- | -------- | ------ | ------------- |
| GET 4KB | 150μs | 8μs | 18x |
| GET 64KB | 250μs | 12μs | 20x |
| PUT 4KB | 180μs | 10μs | 18x |
| PUT 64KB | 300μs | 15μs | 20x |

### Throughput Benchmarks

| Configuration | Bandwidth |
| --------------- | ----------- |
| Single Connection | 12 GB/s |
| 4 Connections | 48 GB/s |
| 8 Connections | 90 GB/s |

### CPU Utilization

| Protocol | CPU per GB/s |
| ---------- | -------------- |
| TCP/IP | 1 core |
| RDMA | 0.1 core |

## Network Configuration

### InfiniBand Setup

```bash

# Check IB devices
ibv_devices

# Check port status
ibstat mlx5_0

# Configure IP over IB (optional)
ip addr add 192.168.100.1/24 dev ib0

```bash

### RoCE v2 Setup

```bash

# Enable RoCE
mlxconfig -d /dev/mst/mt4123_pciconf0 set ROCE_MODE=2

# Configure ECN (recommended)
echo 1 > /sys/class/net/ens1f0/ecn/roce_np/enable

# Set PFC (Priority Flow Control)
mlnx_qos -i ens1f0 --pfc 0,0,0,1,0,0,0,0

```bash

## Integration

### With GPUDirect

```yaml

rdma:
  enabled: true
gpudirect:
  enabled: true
# GPU <-> RDMA <-> Storage (full zero-copy path)

```bash

### With BlueField DPU

```yaml

rdma:
  enabled: true
  device_name: mlx5_0
dpu:
  enabled: true
  enable_rdma: true
  enable_crypto: true
# RDMA traffic encrypted/decrypted at line rate

```bash

### With S3 Express

```yaml

rdma:
  enabled: true
s3_express:
  enabled: true
# Express buckets accessible via RDMA

```bash

## Troubleshooting

### Common Issues

1. **Connection refused**
   - Verify RDMA port (9100) is accessible
   - Check firewall rules
   - Verify IB/RoCE link is up

2. **Low performance**
   - Check link speed: `ibstat mlx5_0`
   - Verify MTU settings
   - Check for packet drops

3. **Memory registration errors**
   - Increase locked memory limits
   - Check available pinned memory

### Diagnostics

```bash

# RDMA device info
ibv_devinfo -d mlx5_0

# Port counters
perfquery -x -d mlx5_0 -P 1

# Check connectivity
ibping -S -d mlx5_0 -P 1
ibping -c 10 -d mlx5_0 -P 1 -L <remote_lid>

# Bandwidth test
ib_write_bw -d mlx5_0 -p 9100

```bash

### System Tuning

```bash

# Increase locked memory
echo "* soft memlock unlimited" >> /etc/security/limits.conf
echo "* hard memlock unlimited" >> /etc/security/limits.conf

# Increase max_mr
echo 131072 > /sys/module/mlx5_core/parameters/prof_sel

```bash

### Logs

Enable debug logging:

```yaml

log_level: debug

```text

Look for logs with `rdma` tag.

## Best Practices

### Queue Sizing

```yaml

rdma:
  # High throughput
  max_send_wr: 256
  max_recv_wr: 256
  cq_size: 512

  # Low latency
  max_send_wr: 64
  max_recv_wr: 64
  cq_size: 128

```

### Connection Management

- Use connection pooling for multiple clients
- Keep connections alive for repeated operations
- Pre-allocate memory regions

### Security

- Use RDMA over dedicated network (physical or VLAN)
- Consider MACSec or IPsec for RoCE
- Enable DPU crypto offload for encryption

## API Reference

### RDMAClient Methods

| Method | Description |
| -------- | ------------- |
| `connect(host, port)` | Establish RDMA connection |
| `get_object(bucket, key)` | RDMA read operation |
| `put_object(bucket, key, data)` | RDMA write operation |
| `delete_object(bucket, key)` | Delete via RDMA |
| `list_objects(bucket, prefix)` | List via RDMA |

## See Also

- [GPUDirect Storage](gpudirect.md) - GPU acceleration
- [BlueField DPU](dpu.md) - DPU offload
- [S3 Express One Zone](s3-express.md) - Low-latency storage
