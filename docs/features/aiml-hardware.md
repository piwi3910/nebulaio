# AI/ML Features Hardware Prerequisites

This guide details the hardware requirements for NebulaIO's AI/ML features including GPUDirect Storage, BlueField DPU, S3 over RDMA, and NIM Integration.

## Feature Overview

| Feature | Required Hardware | Optional Hardware | Minimum Driver |
| --------- | ------------------ | ------------------- | ---------------- |
| GPUDirect Storage | NVIDIA GPU (Volta+) | NVMe with P2P | CUDA 11.4+ |
| BlueField DPU | BlueField-2/3 DPU | - | DOCA 2.0+ |
| S3 over RDMA | InfiniBand/RoCE NIC | - | OFED 5.4+ |
| NIM Integration | None (API-based) | GPU for local NIM | CUDA 12.0+ |
| S3 Express | NVMe storage | - | - |

---

## GPUDirect Storage

### Supported GPUs

| GPU Generation | Models | GDS Support |
| ---------------- | -------- | ------------- |
| NVIDIA Hopper | H100, H200 | Full |
| NVIDIA Ada | RTX 4090, L4, L40 | Full |
| NVIDIA Ampere | A100, A30, A10 | Full |
| NVIDIA Turing | T4 | Limited |
| NVIDIA Volta | V100 | Limited |

### Minimum Requirements

```yaml

gpudirect:
  gpu:
    generation: volta              # Volta or newer
    memory: 16GB                   # Minimum VRAM
    count: 1                       # At least 1 GPU

  storage:
    type: nvme                     # NVMe required
    interface: pcie4               # PCIe 4.0+ recommended
    capacity: 1TB                  # Per drive

  system:
    ram: 64GB                      # Minimum system RAM
    cpu_cores: 16                  # Minimum cores

  driver:
    cuda: "11.4"                   # Minimum CUDA version
    nvidia_driver: "470"           # Minimum driver version
    gds: "1.0"                     # GPUDirect Storage

```bash

### Recommended Configuration

```yaml

# High-performance GPUDirect setup
gpudirect:
  gpu:
    model: "NVIDIA A100 80GB"
    count: 8
    interconnect: "NVLink"

  storage:
    drives:
      - type: "nvme"
        model: "Samsung PM9A3"
        capacity: "7.68TB"
        count: 4
    raid: "stripe"                 # For max throughput

  system:
    ram: 512GB
    cpu: "AMD EPYC 7763"
    cores: 64

  pcie:
    version: 4.0
    lanes_per_gpu: 16
    topology: "direct"             # Direct GPU-NVMe paths

```bash

### Topology Requirements

```text

Optimal Topology (Direct Path):
┌─────────────────────────────────────────────────────────────┐
│                    PCIe Root Complex                         │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │                    PCIe Switch                           │ │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐    │ │
│  │  │ GPU 0   │  │ GPU 1   │  │ NVMe 0  │  │ NVMe 1  │    │ │
│  │  │ x16     │  │ x16     │  │ x4      │  │ x4      │    │ │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘    │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘

Suboptimal Topology (Avoid):
┌─────────────┐              ┌─────────────┐
│    GPU      │──── CPU ─────│    NVMe     │
└─────────────┘  (Bottleneck) └─────────────┘

```bash

### Verification Commands

```bash

# Check GPU and GDS support
nvidia-smi
gdscheck -p

# Check PCIe topology
nvidia-smi topo -m

# Verify GPUDirect paths
gdsio -d /dev/nvme0n1 -s 1G -i 1M -x 0 -w 4

# Expected output:
# GPU 0 -> NVMe0: Direct path available
# Throughput: ~6.5 GB/s

```text

---

## BlueField DPU

### Supported DPUs

| Model | Generation | Crypto | Compression | RDMA |
| ------- | ------------ | -------- | ------------- | ------ |
| BlueField-3 | 3rd Gen | AES-XTS, AES-GCM | LZ4, Deflate | 400Gb/s |
| BlueField-2 | 2nd Gen | AES-XTS, AES-GCM | LZ4 | 200Gb/s |

### Minimum Requirements

```yaml

dpu:
  model: "BlueField-2"             # BF-2 or BF-3
  firmware: "24.35"                # Minimum firmware
  doca: "2.0"                      # DOCA SDK version

  host:
    pcie_slot: x16                 # Full x16 required
    numa_node: 0                   # Match with storage
    iommu: enabled                 # Required for security

```bash

### Recommended Configuration

```yaml

# Production DPU setup
dpu:
  model: "BlueField-3"
  count: 2                         # Redundancy

  offload:
    crypto:
      enabled: true
      algorithms: ["AES-256-GCM"]
      throughput: "100Gbps"

    compression:
      enabled: true
      algorithms: ["LZ4", "Deflate"]
      throughput: "50Gbps"

    rdma:
      enabled: true
      ports: 2
      speed: "200Gb"

  arm_cores:
    count: 16
    frequency: "2.5GHz"
    memory: "32GB"

```bash

### Installation

```bash

# Check DPU detection
lspci | grep -i mellanox

# Install DOCA SDK
wget https://developer.nvidia.com/doca-installer
sudo ./doca-installer

# Verify installation
doca_info

# Check DPU mode
mlxconfig -d /dev/mst/mt41686_pciconf0 q | grep DPU_MODE

# Output:
# DPU_MODE: DPU_MODE_EMBEDDED(1)

```text

---

## S3 over RDMA

### Supported NICs

| NIC Family | Models | Max Speed | RDMA Type |
| ------------ | -------- | ----------- | ----------- |
| ConnectX-7 | MCX75xxxx | 400GbE | RoCE v2, InfiniBand |
| ConnectX-6 | MCX65xxxx | 200GbE | RoCE v2, InfiniBand |
| ConnectX-5 | MCX55xxxx | 100GbE | RoCE v2, InfiniBand |

### Minimum Requirements

```yaml

rdma:
  nic:
    type: "ConnectX-5"             # ConnectX-5 or newer
    speed: "100Gb"
    rdma_type: "RoCE"              # or "InfiniBand"

  driver:
    ofed: "5.4"                    # Minimum MLNX_OFED
    firmware: "16.35"              # Minimum NIC firmware

  host:
    cpu_cores: 8
    ram: 32GB
    iommu: enabled

  network:
    mtu: 9000                      # Jumbo frames
    pfc: enabled                   # Priority Flow Control

```bash

### Recommended Configuration

```yaml

# High-performance RDMA setup
rdma:
  nic:
    model: "ConnectX-7"
    count: 2                       # Dual-port or 2 NICs
    speed: "400Gb"

  driver:
    ofed: "23.10"
    firmware: "28.38"

  configuration:
    mtu: 9000
    flow_control: "pfc"
    ecn: enabled
    dcqcn: enabled                 # Congestion control

  buffers:
    send_queue_size: 8192
    recv_queue_size: 8192
    max_inline: 64

  memory:
    registered_memory: "16GB"
    hugepages: enabled

```bash

### Network Requirements

```yaml

# Network infrastructure for RDMA
network:
  switches:
    type: "Spectrum-4"             # Or equivalent
    features:
      - "PFC per priority"
      - "ECN support"
      - "DCQCN"
      - "Lossless fabric"

  cabling:
    type: "QSFP-DD"
    speed: "400Gb"
    length: "< 100m"               # Copper or Active

  vlan:
    storage_vlan: 100
    priority: 5                    # High priority for RoCE
    pfc_priority: 3                # PFC on priority 3

```bash

### Verification Commands

```bash

# Check RDMA devices
ibv_devinfo

# Test RDMA connectivity
ib_send_bw -d mlx5_0 -a

# Verify lossless configuration
mlnx_qos -i ens1f0np0

# Expected output:
# PFC enabled: 3 (priority 3)
# Trust mode: dscp

```text

---

## NIM Integration

### Local NIM Deployment

For running NIM inference locally:

```yaml

nim_local:
  gpu:
    type: "NVIDIA GPU"
    memory: 80GB                   # For large models
    count: 8                       # For distributed inference

  models:
    llama_3_70b:
      gpu_memory: 160GB            # 2x A100 80GB
      cpu_memory: 256GB

    llama_3_8b:
      gpu_memory: 20GB             # 1x A100 40GB
      cpu_memory: 64GB

    grounding_dino:
      gpu_memory: 24GB
      cpu_memory: 32GB

```bash

### API-Only Integration

For cloud NIM (no local GPU required):

```yaml

nim_cloud:
  # No GPU required
  requirements:
    network:
      egress: "HTTPS to api.nvidia.com"
      bandwidth: "100Mbps"         # Minimum

    cpu: 4                         # For request processing
    ram: 8GB

```bash

### Recommended Configuration

```yaml

# Hybrid NIM setup
nim:
  local:
    enabled: true
    gpu:
      model: "NVIDIA H100"
      count: 8
      memory: "80GB each"

    models:
      - "meta/llama-3.1-70b-instruct"   # Local inference
      - "nvidia/nemo-retriever"          # Local embedding

  cloud:
    enabled: true
    models:
      - "nvidia/grounding-dino"          # Cloud for vision
      - "nvidia/parakeet-tdt-1.1b"       # Cloud for audio

```text

---

## S3 Express One Zone

### Storage Requirements

```yaml

s3_express:
  storage:
    type: "nvme"
    tier: "hot"
    iops: 1000000                  # 1M+ IOPS
    latency: "< 100us"

  recommended:
    drives:
      - model: "Intel Optane P5800X"
      - model: "Samsung PM9A3"
      - model: "Micron 7450 Pro"

    raid:
      type: "stripe"
      drives: 4
      expected_iops: 4000000       # 4M IOPS

```text

---

## System Requirements Summary

### Development Environment

```yaml

development:
  cpu: "8 cores"
  ram: "32GB"
  storage: "500GB NVMe"
  gpu: "Optional (GTX 1080 Ti+)"
  network: "10GbE"

```bash

### Production Environment

```yaml

production:
  cpu: "32+ cores (EPYC/Xeon)"
  ram: "256GB+"
  storage:
    hot: "4x 7.68TB NVMe"
    warm: "8x 15TB SSD"
    cold: "HDDs as needed"
  gpu: "4-8x A100/H100"
  network:
    storage: "100GbE RoCE"
    frontend: "25GbE"
  dpu: "BlueField-3 (optional)"

```bash

### High-Performance AI/ML

```yaml

high_performance:
  cpu: "2x AMD EPYC 9654 (192 cores)"
  ram: "2TB DDR5"
  storage:
    express_zone: "8x Optane P5800X (RAID 0)"
    hot: "16x 7.68TB NVMe"
  gpu:
    count: 8
    model: "H100 SXM"
    interconnect: "NVSwitch"
  network:
    rdma: "400GbE InfiniBand"
    frontend: "100GbE"
  dpu: "2x BlueField-3"

```

---

## Compatibility Matrix

| Feature | Linux | Windows | Container |
| --------- | ------- | --------- | ----------- |
| GPUDirect Storage | Yes | No | Limited |
| BlueField DPU | Yes | No | Yes (privileged) |
| S3 over RDMA | Yes | No | Yes (host network) |
| NIM Integration | Yes | Yes | Yes |
| S3 Express | Yes | Yes | Yes |

### Linux Distribution Support

| Distribution | GPUDirect | DPU | RDMA |
| -------------- | ----------- | ----- | ------ |
| Ubuntu 22.04 | Yes | Yes | Yes |
| RHEL 9 | Yes | Yes | Yes |
| Rocky 9 | Yes | Yes | Yes |
| Debian 12 | Yes | Limited | Yes |

---

## Verification Checklist

Before enabling AI/ML features:

- [ ] GPU: Verify `nvidia-smi` shows expected GPUs
- [ ] GDS: Run `gdscheck -p` for GPUDirect support
- [ ] RDMA: Verify `ibv_devinfo` shows expected devices
- [ ] DPU: Confirm `doca_info` shows DPU capabilities
- [ ] Network: Test MTU 9000 end-to-end
- [ ] IOMMU: Verify enabled in BIOS and kernel
- [ ] Drivers: Confirm minimum versions installed
- [ ] Topology: Check PCIe paths with `nvidia-smi topo`
- [ ] Firmware: Update to recommended versions

---

## Related Documentation

- [GPUDirect Storage](gpudirect.md) - Configuration guide
- [BlueField DPU](dpu.md) - DPU setup
- [RDMA Transport](rdma.md) - RDMA configuration
- [Performance Tuning](../PERFORMANCE_TUNING.md) - Optimization guide
- [AI/ML Security](aiml-security.md) - Security considerations
