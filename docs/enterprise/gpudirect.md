# GPUDirect Storage

NebulaIO supports NVIDIA GPUDirect Storage (GDS) for zero-copy data transfers between GPU memory and storage, eliminating CPU bottlenecks in AI/ML training pipelines.

## Overview

GPUDirect Storage provides:

- **Zero-Copy Transfers** - Direct DMA between GPU and storage
- **Reduced Latency** - Bypass CPU memory entirely
- **Higher Throughput** - Saturate NVMe and network bandwidth
- **Multi-GPU Support** - Peer-to-peer transfers between GPUs

## Requirements

### Hardware

- NVIDIA GPU with GPUDirect Storage support (A100, H100, etc.)
- NVIDIA driver 470+ with GDS support
- NVMe SSDs or RDMA-capable network

### Software

- CUDA 11.4+
- cuFile library (part of CUDA toolkit)
- NebulaIO with GPUDirect enabled

## Configuration

### Basic Setup

```yaml
gpudirect:
  enabled: true
  buffer_pool_size: 1073741824  # 1GB buffer pool
  enable_async: true
```

### Advanced Configuration

```yaml
gpudirect:
  enabled: true
  device_ids: [0, 1]            # Specific GPUs (empty = auto-detect)
  buffer_pool_size: 4294967296  # 4GB buffer pool
  max_transfer_size: 268435456  # 256MB max single transfer
  enable_async: true            # Asynchronous operations
  enable_p2p: true              # Peer-to-peer GPU transfers
  queue_depth: 64               # I/O queue depth
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEBULAIO_GPUDIRECT_ENABLED` | Enable GPUDirect | `false` |
| `NEBULAIO_GPUDIRECT_BUFFER_POOL_SIZE` | Buffer pool size | `1073741824` |

## Usage Examples

### Python with cuPy

```python
import cupy as cp
from nebulaio import GPUDirectClient

# Initialize client
client = GPUDirectClient(
    endpoint="http://localhost:9000",
    access_key="minioadmin",
    secret_key="minioadmin"
)

# Allocate GPU memory
gpu_buffer = cp.zeros((1024, 1024, 1024), dtype=cp.float32)

# Direct read from storage to GPU
client.read_to_gpu(
    bucket="training-data",
    key="model/weights.bin",
    gpu_buffer=gpu_buffer
)

# Process on GPU
result = some_gpu_computation(gpu_buffer)

# Direct write from GPU to storage
client.write_from_gpu(
    bucket="results",
    key="output/processed.bin",
    gpu_buffer=result
)
```

### PyTorch DataLoader

```python
import torch
from nebulaio.pytorch import GPUDirectDataset

class TrainingDataset(GPUDirectDataset):
    def __init__(self, bucket, prefix):
        super().__init__(
            endpoint="http://localhost:9000",
            bucket=bucket,
            prefix=prefix
        )

    def __getitem__(self, idx):
        # Data loaded directly to GPU via GPUDirect
        return self.load_to_gpu(f"batch_{idx}.pt")

dataset = TrainingDataset("training-data", "batches/")
loader = torch.utils.data.DataLoader(
    dataset,
    batch_size=32,
    num_workers=4,
    pin_memory=False  # Not needed with GPUDirect
)
```

### NVIDIA DALI

```python
from nvidia.dali import pipeline_def
from nvidia.dali.plugin.pytorch import DALIGenericIterator
from nebulaio.dali import GPUDirectReader

@pipeline_def
def training_pipeline():
    images = GPUDirectReader(
        endpoint="http://localhost:9000",
        bucket="images",
        prefix="train/"
    )
    return images

pipe = training_pipeline(batch_size=64, num_threads=4, device_id=0)
```

### C++ cuFile API

```cpp
#include <cufile.h>
#include "nebulaio/gpudirect.h"

// Initialize GPUDirect
CUfileDescr_t cf_descr;
CUfileHandle_t cf_handle;
nebulaio::GPUDirectInit(&cf_descr, "localhost:9000");

// Open object
nebulaio::GPUDirectOpen(&cf_handle, "my-bucket", "data.bin");

// Allocate GPU buffer
void* gpu_buffer;
cudaMalloc(&gpu_buffer, size);

// Direct read
ssize_t bytes_read = cuFileRead(cf_handle, gpu_buffer, size, 0, 0);

// Process...

// Direct write
ssize_t bytes_written = cuFileWrite(cf_handle, gpu_buffer, size, 0, 0);

// Cleanup
cuFileHandleDeregister(cf_handle);
cudaFree(gpu_buffer);
```

## Performance Tuning

### Buffer Pool Sizing

```yaml
gpudirect:
  # For training workloads: 4-8GB
  buffer_pool_size: 4294967296

  # For inference: 1-2GB
  buffer_pool_size: 1073741824
```

### Queue Depth

```yaml
gpudirect:
  # High throughput: 128
  queue_depth: 128

  # Low latency: 16-32
  queue_depth: 32
```

### Multi-GPU Configuration

```yaml
gpudirect:
  enabled: true
  device_ids: [0, 1, 2, 3]  # All 4 GPUs
  enable_p2p: true          # Enable peer-to-peer
```

## Benchmarks

| Operation | Without GDS | With GDS | Improvement |
|-----------|-------------|----------|-------------|
| 1GB Read | 850ms | 120ms | 7x faster |
| 1GB Write | 920ms | 140ms | 6.5x faster |
| Batch Load | 2.1s | 280ms | 7.5x faster |

### Bandwidth

| Configuration | Throughput |
|---------------|------------|
| Single GPU | ~12 GB/s |
| Dual GPU | ~24 GB/s |
| Quad GPU + NVLink | ~48 GB/s |

## Integration with Other Features

### S3 Express

```yaml
s3_express:
  enabled: true
gpudirect:
  enabled: true
# Express buckets automatically optimized for GPUDirect
```

### RDMA

```yaml
rdma:
  enabled: true
gpudirect:
  enabled: true
# Combines RDMA network with GPUDirect for maximum throughput
```

## Troubleshooting

### Common Issues

1. **GPUDirect not detected**
   - Check NVIDIA driver version (470+)
   - Verify cuFile library is installed
   - Check GPU compatibility

2. **Low performance**
   - Increase buffer pool size
   - Check for PCIe bandwidth bottlenecks
   - Verify NVMe drives support GDS

3. **Memory errors**
   - Reduce max_transfer_size
   - Check GPU memory availability

### Diagnostics

```bash
# Check GDS status
nvidia-smi nvlink --status
nvidia-smi topo -m

# Verify cuFile
ldconfig -p | grep cufile
```

### Logs

Enable debug logging:

```yaml
log_level: debug
```

Look for logs with `gpudirect` tag.

## API Reference

### GPUDirectClient Methods

| Method | Description |
|--------|-------------|
| `read_to_gpu(bucket, key, buffer)` | Read object directly to GPU |
| `write_from_gpu(bucket, key, buffer)` | Write GPU buffer to object |
| `async_read(bucket, key, buffer)` | Async read with callback |
| `async_write(bucket, key, buffer)` | Async write with callback |

## See Also

- [S3 over RDMA](rdma.md) - Network acceleration
- [BlueField DPU](dpu.md) - Offload processing
- [S3 Express One Zone](s3-express.md) - Low-latency storage
