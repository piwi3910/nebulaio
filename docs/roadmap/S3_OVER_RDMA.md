# S3 over RDMA Roadmap

## Overview

This document outlines the roadmap for implementing S3 over RDMA (Remote Direct Memory Access) in NebulaIO. This feature enables ultra-low latency object storage access by bypassing the kernel network stack and enabling direct memory-to-memory transfers between client and server.

## Why S3 over RDMA?

### Performance Benefits

| Metric | Traditional TCP | RDMA |
| -------- | ----------------- | ------ |
| Latency (p50) | 100-500 μs | 1-5 μs |
| Latency (p99) | 1-5 ms | 10-50 μs |
| CPU Usage | High | Near-zero |
| Throughput (per node) | 10-25 GB/s | 100+ GB/s |
| Small Object IOPS | 100K-500K | 1M+ |

### Target Use Cases

1. **AI/ML Training Pipelines**
   - Loading training data with minimal GPU idle time
   - Checkpoint save/restore operations
   - Model weight distribution

2. **High-Frequency Trading**
   - Market data storage and retrieval
   - Transaction logging with guaranteed latency

3. **Scientific Computing**
   - Large-scale simulation data access
   - Real-time sensor data ingestion

4. **In-Memory Computing**
   - Extending memory capacity with remote storage
   - Disaggregated memory architectures

## Technical Architecture

### Protocol Stack Comparison

```text

Traditional S3:
┌─────────────────┐
│   S3 Client     │
├─────────────────┤
│   HTTP/HTTPS    │
├─────────────────┤
│   TCP/IP        │
├─────────────────┤
│   NIC Driver    │
├─────────────────┤
│   Network Card  │
└─────────────────┘

S3 over RDMA:
┌─────────────────┐
│   S3 Client     │
├─────────────────┤
│   S3 over RDMA  │
├─────────────────┤
│   RDMA Verbs    │
├─────────────────┤
│   RDMA NIC      │
└─────────────────┘

```bash

### RDMA Transport Options

| Transport | Hardware Required | Performance | Cost |
| ----------- | ------------------- | ------------- | ------ |
| InfiniBand | InfiniBand NICs + Switches | Highest | $$$ |
| RoCE v2 | RDMA-capable Ethernet NICs | Very High | $$ |
| iWARP | iWARP NICs | High | $ |

### Proposed Architecture

```text

┌─────────────────────────────────────────────────────────────┐
│                        S3 Client                             │
├─────────────────────────────────────────────────────────────┤
│                   Transport Selector                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │ HTTP/HTTPS  │  │  RDMA/RoCE  │  │  Hybrid (Auto-select)│ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      NebulaIO Gateway                        │
│  ┌─────────────────────────────────────────────────────┐   │
│  │                  Listener Manager                     │   │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────────┐    │   │
│  │  │HTTP :9000 │  │HTTPS:9443 │  │ RDMA :9100    │    │   │
│  │  └───────────┘  └───────────┘  └───────────────┘    │   │
│  └─────────────────────────────────────────────────────┘   │
│                              │                               │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              Request Router                           │   │
│  │  - Parse S3 operation from both transports           │   │
│  │  - Route to appropriate handler                      │   │
│  │  - Handle memory registration for RDMA               │   │
│  └─────────────────────────────────────────────────────┘   │
│                              │                               │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              RDMA Memory Manager                      │   │
│  │  - Pre-registered memory pools                       │   │
│  │  - Zero-copy data paths                              │   │
│  │  - Memory region lifecycle management                │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘

```bash

## Implementation Phases

### Phase 1: Foundation (Q2 2025)

**Objective**: Establish RDMA infrastructure and basic connectivity

**Deliverables**:

1. RDMA transport abstraction layer
2. Connection management (RC - Reliable Connection)
3. Memory registration framework
4. Basic GetObject over RDMA

**Technical Details**:

```go

// internal/transport/rdma/transport.go

type RDMATransport struct {
    device      *RDMADevice
    protDomain  *ProtectionDomain
    memRegions  *MemoryRegionPool
    connections map[string]*RDMAConnection
}

type RDMAConnection struct {
    qp          *QueuePair
    cq          *CompletionQueue
    sendBuf     *MemoryRegion
    recvBuf     *MemoryRegion
}

// S3 operation mapping
type S3OverRDMARequest struct {
    OpCode      uint8   // GET, PUT, DELETE, etc.
    BucketLen   uint16
    KeyLen      uint16
    BodyLen     uint64
    Bucket      []byte
    Key         []byte
    Headers     []byte
    // For RDMA READ: remote memory address for data
    RemoteAddr  uint64
    RemoteKey   uint32
}

```text

**Dependencies**:

- RDMA-capable hardware for development/testing
- libibverbs (Linux RDMA user-space library)
- Go RDMA bindings (github.com/Mellanox/goroce or similar)

### Phase 2: Core Operations (Q3 2025)

**Objective**: Implement all S3 operations over RDMA

**Deliverables**:

1. PutObject with zero-copy uploads
2. Multipart upload support
3. DeleteObject, HeadObject
4. ListObjects with streaming results
5. Concurrent connection handling

**Zero-Copy Data Path**:

```text

Client PutObject:
1. Client registers memory containing object data
2. Client sends request with remote memory address
3. Server performs RDMA READ directly from client memory
4. Server writes to storage
5. Server sends completion response

Client GetObject:
1. Client registers receive buffer
2. Client sends request with buffer address
3. Server performs RDMA WRITE directly to client buffer
4. Client receives completion notification

```bash

### Phase 3: Performance Optimization (Q4 2025)

**Objective**: Achieve target performance metrics

**Deliverables**:

1. Memory pool pre-allocation
2. Request pipelining
3. Adaptive batching for small objects
4. NUMA-aware memory allocation
5. Connection multiplexing

**Target Metrics**:

- GetObject 4KB: < 5 μs (p99)
- GetObject 1MB: < 50 μs (p99)
- PutObject 4KB: < 10 μs (p99)
- Throughput: > 100 GB/s aggregate

### Phase 4: Production Readiness (Q1 2026)

**Objective**: Production-ready deployment

**Deliverables**:

1. Hybrid transport (automatic fallback to TCP)
2. Authentication over RDMA
3. Encryption support (hardware offload where available)
4. Monitoring and metrics
5. Client SDK (Go, Python, C++)
6. Kubernetes deployment guide

## Client SDK Design

### Go Client Example

```go

package main

import (
    "github.com/piwi3910/nebulaio/client/rdma"
)

func main() {
    // Create RDMA-enabled client
    client, err := rdma.NewClient(rdma.Config{
        Endpoints:    []string{"rdma://node1:9100", "rdma://node2:9100"},
        FallbackHTTP: true,                    // Fall back to HTTP if RDMA unavailable
        MemoryPool:   rdma.PoolSize(1 << 30), // 1GB pre-allocated
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Zero-copy GetObject
    buf := client.AllocateBuffer(1 << 20) // 1MB from registered pool
    defer client.ReleaseBuffer(buf)

    n, err := client.GetObject(ctx, "bucket", "key", buf)
    // buf now contains object data with zero copies

    // Zero-copy PutObject
    data := client.AllocateBuffer(len(myData))
    copy(data, myData)
    err = client.PutObject(ctx, "bucket", "key", data)
}

```bash

### Python Client Example

```python

from nebulaio import RDMAClient

# Create client with automatic RDMA detection
client = RDMAClient(
    endpoints=["rdma://node1:9100"],
    fallback_http=True
)

# NumPy integration for ML workloads
import numpy as np

# Direct load into numpy array (zero-copy)
array = client.get_object_numpy("bucket", "model_weights.npy")

# Direct save from numpy array (zero-copy)
client.put_object_numpy("bucket", "checkpoint.npy", weights_array)

```bash

## Hardware Requirements

### Minimum Requirements

| Component | Specification |
| ----------- | --------------- |
| RDMA NIC | ConnectX-4 or newer (Mellanox/NVIDIA) |
| Switch | RoCE v2 capable (for Ethernet) or InfiniBand |
| Memory | 32GB+ for memory regions |
| Kernel | Linux 4.15+ with RDMA drivers |

### Recommended Setup

| Component | Specification |
| ----------- | --------------- |
| NIC | NVIDIA ConnectX-6 Dx (100/200 GbE) |
| Switch | Ethernet with ECN, PFC (lossless) |
| CPU | AMD EPYC or Intel Xeon with NUMA |
| Memory | 256GB+ DDR4/DDR5 |
| Storage | NVMe SSD with high IOPS |

### Network Configuration

```yaml

# Required for RoCE v2 performance
network:
  # Enable Priority Flow Control
  pfc:
    enabled: true
    priority: 3

  # Enable Explicit Congestion Notification
  ecn:
    enabled: true
    threshold: 100  # packets

  # DSCP marking for QoS
  dscp: 26  # AF31

  # MTU (jumbo frames recommended)
  mtu: 9000

```bash

## Integration with Existing Features

### DRAM Cache + RDMA

```text

┌──────────────┐    RDMA READ     ┌──────────────┐
│    Client    │◄────────────────►│  DRAM Cache  │
└──────────────┘                  └──────────────┘
                                         │
                                  Cache Miss
                                         ▼
                                  ┌──────────────┐
                                  │ Object Store │
                                  └──────────────┘

Benefits:
- Sub-microsecond cache hits via RDMA
- Zero CPU overhead on cache path
- Direct GPU memory access (GPUDirect)

```bash

### Erasure Coding + RDMA

```text

Client writes 1 object:

┌──────────────┐
│    Client    │
└──────┬───────┘
       │ RDMA WRITE (single buffer)
       ▼
┌──────────────────────────────────────────┐
│           NebulaIO Gateway                │
│   ┌──────────────────────────────────┐   │
│   │      Erasure Encoder (GPU)        │   │
│   │   Split into 10 data + 4 parity   │   │
│   └──────────────────────────────────┘   │
│            │ RDMA WRITE (parallel)        │
│   ┌────────┼────────┬────────┬────────┐  │
│   ▼        ▼        ▼        ▼        ▼  │
│ Node1   Node2    Node3   ...       Node14│
└──────────────────────────────────────────┘

```

## Benchmarking Plan

### Micro-Benchmarks

1. **Latency**
   - Empty request/response RTT
   - 4KB GET latency distribution
   - 1MB GET latency distribution

2. **Throughput**
   - Single connection bandwidth
   - Multi-connection aggregate
   - Small object IOPS

3. **CPU Efficiency**
   - CPU cycles per operation
   - Context switches per operation

### Macro-Benchmarks

1. **ML Training Data Loading**
   - ImageNet-style dataset loading
   - Compare epoch time vs NFS/TCP S3

2. **Checkpoint/Restore**
   - Large model checkpoint save time
   - Restore and resume time

3. **Mixed Workloads**
   - Combined read/write patterns
   - Multi-tenant scenarios

## Risk Assessment

| Risk | Mitigation |
| ------ | ------------ |
| Hardware availability | Hybrid mode with HTTP fallback |
| Driver compatibility | Support multiple RDMA libraries |
| Network configuration complexity | Provide automated setup tools |
| Security concerns | Implement RDMA-compatible auth |
| Debugging difficulty | Comprehensive tracing/logging |

## Timeline Summary

| Phase | Timeline | Status |
| ------- | ---------- | -------- |
| Research & Design | Q1 2025 | ✅ Complete |
| Phase 1: Foundation | Q2 2025 | ✅ Complete |
| Phase 2: Core Operations | Q3 2025 | In Progress |
| Phase 3: Optimization | Q4 2025 | Planned |
| Phase 4: Production | Q1 2026 | Planned |

## Implementation Status

### Phase 1: Foundation (✅ Complete)

The following components have been implemented in `internal/transport/rdma/`:

| Component | File | Status |
| ----------- | ------ | -------- |
| RDMA transport abstraction | `transport.go` | ✅ |
| Connection management | `transport.go` | ✅ |
| Memory region pool | `transport.go` | ✅ |
| Queue pair management | `transport.go` | ✅ |
| S3 operations over RDMA | `operations.go` | ✅ |
| Client SDK | `client.go` | ✅ |
| Server implementation | `server.go` | ✅ |
| Unit tests | `transport_test.go` | ✅ |

**Simulated Mode**: The implementation includes a simulated mode (`DeviceName: "simulated"`) for development and testing without actual RDMA hardware. This allows:

- Unit testing of all RDMA code paths
- Development on standard machines
- CI/CD pipeline integration

**Next Steps for Phase 2**:

- Integration with libibverbs for real hardware
- Performance benchmarking with actual RDMA NICs
- GPU Direct integration for AI/ML workloads

## Resources

### Documentation

- [RDMA Programming Guide](https://www.rdmamojo.com/)
- [Mellanox OFED Documentation](https://docs.nvidia.com/networking/)
- [Linux RDMA Subsystem](https://www.kernel.org/doc/html/latest/infiniband/)

### Reference Implementations

- [HERD (RDMA Key-Value)](https://github.com/efficient/HERD)
- [FaRM (Microsoft)](https://www.microsoft.com/en-us/research/project/farm/)

### Dependencies

- [go-rdma](https://github.com/Mellanox/goroce) - Go RDMA bindings
- [libibverbs](https://github.com/linux-rdma/rdma-core) - RDMA user-space library

## Conclusion

S3 over RDMA represents a significant advancement in object storage performance, enabling new use cases in AI/ML, HPC, and low-latency applications. The phased implementation approach ensures we can deliver incremental value while building toward the full vision.
