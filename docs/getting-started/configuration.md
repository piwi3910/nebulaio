# Configuration Reference

NebulaIO can be configured using a YAML configuration file, environment variables, or command-line flags.

## Configuration Precedence

Configuration values are applied in the following order (highest priority first):

1. **Command-line flags** - Override all other settings
2. **Environment variables** - Override config file and defaults
3. **Configuration file** - Override defaults
4. **Built-in defaults** - Used when no other value is specified

## Configuration File

The default location is `/etc/nebulaio/config.yaml`. Use the `--config` flag for an alternative path:

```bash
nebulaio --config /path/to/config.yaml
```

## Environment Variables

All options can be set via environment variables using the `NEBULAIO_` prefix. Nested keys use underscores:

| Config Key | Environment Variable |
|------------|---------------------|
| `node_id` | `NEBULAIO_NODE_ID` |
| `s3_port` | `NEBULAIO_S3_PORT` |
| `cluster.bootstrap` | `NEBULAIO_CLUSTER_BOOTSTRAP` |
| `storage.data_dir` | `NEBULAIO_STORAGE_DATA_DIR` |
| `auth.root_user` | `NEBULAIO_AUTH_ROOT_USER` |

---

## Server Settings

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `node_id` | string | auto-generated | Unique identifier for this node |
| `node_name` | string | hostname | Human-readable node name |
| `s3_port` | int | `9000` | Port for S3 API traffic |
| `admin_port` | int | `9001` | Port for Admin/Console API |
| `console_port` | int | `9002` | Port for web console static files |
| `bind_address` | string | `0.0.0.0` | Address to bind all ports |

### TLS Configuration

NebulaIO is **secure by default** with TLS enabled and automatic certificate generation.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `tls.enabled` | bool | `true` | Enable TLS encryption (enabled by default) |
| `tls.auto_generate` | bool | `true` | Auto-generate self-signed certificates if none provided |
| `tls.cert_dir` | string | `./data/certs` | Directory for storing certificates |
| `tls.cert_file` | string | - | Path to TLS certificate (overrides auto-generation) |
| `tls.key_file` | string | - | Path to TLS private key (overrides auto-generation) |
| `tls.ca_file` | string | - | Path to CA certificate for mTLS |
| `tls.require_client_cert` | bool | `false` | Require client certificates (mTLS) |
| `tls.min_version` | string | `1.2` | Minimum TLS version: `1.2` or `1.3` |
| `tls.organization` | string | `NebulaIO` | Organization name for auto-generated certificates |
| `tls.validity_days` | int | `365` | Validity period for auto-generated server certificates |
| `tls.dns_names` | list | `[]` | Additional DNS names for certificate SAN |
| `tls.ip_addresses` | list | `[]` | Additional IP addresses for certificate SAN |

**Note:** When `auto_generate` is enabled and no certificates are provided, NebulaIO automatically:

- Creates a CA certificate (10-year validity)
- Generates a server certificate signed by the CA
- Detects and includes all local IP addresses in the certificate SAN
- Regenerates certificates 30 days before expiry

---

## Storage Settings

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.data_dir` | string | `/var/lib/nebulaio` | Root data directory |
| `storage.backend` | string | `fs` | Storage backend: `fs`, `erasure`, `volume` |
| `storage.path` | string | `{data_dir}/objects` | Object storage path |
| `storage.default_storage_class` | string | `STANDARD` | Default storage class |

### Volume Storage (High Performance)

The volume backend stores objects in pre-allocated volume files with block-based allocation, optimized for high-throughput workloads.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.volume.max_volume_size` | uint64 | `34359738368` | Maximum volume file size (32GB) |
| `storage.volume.auto_create` | bool | `true` | Automatically create new volumes when needed |

#### Direct I/O Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.volume.direct_io.enabled` | bool | `true` | Enable O_DIRECT to bypass kernel cache |
| `storage.volume.direct_io.block_alignment` | int | `4096` | Buffer/offset alignment (bytes) |
| `storage.volume.direct_io.use_memory_pool` | bool | `true` | Pool aligned buffers for reduced allocations |
| `storage.volume.direct_io.fallback_on_error` | bool | `true` | Fall back to buffered I/O on failure |

#### Raw Device Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.volume.raw_devices.enabled` | bool | `false` | Enable raw block device access |
| `storage.volume.raw_devices.safety.check_filesystem` | bool | `true` | Verify no filesystem exists |
| `storage.volume.raw_devices.safety.require_confirmation` | bool | `true` | Require explicit confirmation |
| `storage.volume.raw_devices.safety.write_signature` | bool | `true` | Write NebulaIO signature to device |
| `storage.volume.raw_devices.safety.exclusive_lock` | bool | `true` | Use flock() for exclusive access |
| `storage.volume.raw_devices.devices` | list | `[]` | List of raw devices with path, tier, and size |

#### Tier Directories (Alternative to Raw Devices)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.volume.tier_directories.hot` | string | - | Hot tier directory (NVMe) |
| `storage.volume.tier_directories.warm` | string | - | Warm tier directory (SSD) |
| `storage.volume.tier_directories.cold` | string | - | Cold tier directory (HDD) |

**When to use Volume backend:**

- High-throughput workloads with many small to medium objects
- Reduced filesystem overhead (no per-object files)
- Predictable performance with pre-allocated storage

### Erasure Coding

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.erasure_coding.enabled` | bool | `true` | Enable erasure coding |
| `storage.erasure_coding.data_shards` | int | `4` | Number of data shards |
| `storage.erasure_coding.parity_shards` | int | `2` | Number of parity shards |

**Common configurations:** 4+2 (50% overhead, 2 failures), 8+4 (50% overhead, 4 failures), 10+4 (40% overhead, 4 failures).

### Placement Groups

Placement groups define data locality boundaries for erasure coding and tiering operations.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.placement_groups.local_group_id` | string | - | ID of this node's placement group |
| `storage.placement_groups.min_nodes_for_erasure` | int | `14` | Global minimum nodes for erasure coding |
| `storage.placement_groups.groups` | list | - | List of placement group definitions |

**Placement Group Definition:**

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `id` | string | Yes | Unique placement group identifier |
| `name` | string | No | Human-readable name |
| `datacenter` | string | Yes | Datacenter identifier |
| `region` | string | Yes | Geographic region |
| `min_nodes` | int | Yes | Minimum nodes for healthy status |
| `max_nodes` | int | No | Maximum allowed nodes (0 = unlimited) |

**Example Configuration:**

```yaml
storage:
  default_redundancy:
    enabled: true
    data_shards: 10
    parity_shards: 4

  placement_groups:
    local_group_id: pg-dc1
    min_nodes_for_erasure: 14

    groups:
      - id: pg-dc1
        name: US East Primary
        datacenter: dc1
        region: us-east-1
        min_nodes: 14
        max_nodes: 50

      - id: pg-dc2
        name: US West DR
        datacenter: dc2
        region: us-west-2
        min_nodes: 14
        max_nodes: 50

    replication_targets:
      - pg-dc2  # Cross-datacenter replication
```

**Environment Variables:**

| Config Key | Environment Variable |
|------------|---------------------|
| `storage.placement_groups.local_group_id` | `NEBULAIO_STORAGE_PLACEMENT_GROUPS_LOCAL_GROUP_ID` |
| `storage.placement_groups.min_nodes_for_erasure` | `NEBULAIO_STORAGE_PLACEMENT_GROUPS_MIN_NODES_FOR_ERASURE` |

**Validation Rules:**

- `min_nodes` must be >= `data_shards + parity_shards`
- `local_group_id` must reference an existing group
- All nodes in a group should have the same configuration

---

## Authentication

### Root Credentials

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `auth.root_user` | string | `admin` | Root/admin username |
| `auth.root_password` | string | - | Root/admin password (**required**) |
| `auth.jwt_secret` | string | auto-generated | Secret for JWT signing |
| `auth.jwt_expiry` | duration | `24h` | JWT token expiry time |

> **Password Requirements:** The root password must meet the following criteria:
> - Minimum 12 characters
> - At least one uppercase letter
> - At least one lowercase letter
> - At least one number
>
> Set via environment variable: `export NEBULAIO_AUTH_ROOT_PASSWORD="YourSecurePass123"`

### LDAP Integration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `auth.ldap.enabled` | bool | `false` | Enable LDAP authentication |
| `auth.ldap.server` | string | - | LDAP server address |
| `auth.ldap.port` | int | `389` | LDAP port (636 for TLS) |
| `auth.ldap.use_tls` | bool | `false` | Use TLS connection |
| `auth.ldap.bind_dn` | string | - | Bind DN for LDAP queries |
| `auth.ldap.bind_password` | string | - | Bind password |
| `auth.ldap.base_dn` | string | - | Base DN for user searches |
| `auth.ldap.user_filter` | string | `(uid=%s)` | User search filter |

### OIDC Integration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `auth.oidc.enabled` | bool | `false` | Enable OIDC authentication |
| `auth.oidc.issuer` | string | - | OIDC issuer URL |
| `auth.oidc.client_id` | string | - | OIDC client ID |
| `auth.oidc.client_secret` | string | - | OIDC client secret |
| `auth.oidc.redirect_url` | string | - | OAuth redirect URL |
| `auth.oidc.scopes` | list | `[openid, profile, email]` | OIDC scopes |

---

## Clustering

### Dragonboat Consensus

NebulaIO uses [Dragonboat](https://github.com/lni/dragonboat), a high-performance multi-group Raft consensus library.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `cluster.bootstrap` | bool | `false` | Bootstrap new cluster (first node only) |
| `cluster.shard_id` | uint64 | `1` | Raft shard/group identifier |
| `cluster.replica_id` | uint64 | auto | Unique replica ID within shard (derived from node_id if not set) |
| `cluster.raft_port` | int | `9003` | Dragonboat consensus port |
| `cluster.raft_address` | string | - | Full Raft address (host:port) for Dragonboat NodeHost |
| `cluster.advertise_address` | string | - | Address advertised to other nodes |
| `cluster.join_addresses` | list | - | Addresses of existing cluster nodes (gossip port) |
| `cluster.initial_members` | map | - | Initial cluster members map (replica_id -> raft_address) |
| `cluster.raft_election_rtt` | int | `10` | Election timeout in RTT multiples |
| `cluster.raft_heartbeat_rtt` | int | `1` | Heartbeat interval in RTT multiples |
| `cluster.rtt_millisecond` | int | `200` | RTT in milliseconds between nodes |
| `cluster.wal_dir` | string | `{data_dir}/wal` | Write-Ahead Log directory (use NVMe for best performance) |
| `cluster.snapshot_entries` | uint64 | `1024` | Create snapshot every N log entries |
| `cluster.compaction_overhead` | uint64 | `500` | Log entries to keep after snapshot |
| `cluster.check_quorum` | bool | `true` | Enable quorum checking for leader validity |

**Dragonboat Terminology**:

- **ShardID**: Identifies a Raft group (equivalent to cluster ID)
- **ReplicaID**: Unique identifier for each node within a shard (must be unique per node)
- **RTT**: Round-Trip Time - election and heartbeat timeouts are expressed as multiples of RTT

### Gossip Protocol

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `cluster.gossip_port` | int | `9004` | Gossip protocol port |
| `cluster.gossip_interval` | duration | `200ms` | Gossip interval |
| `cluster.cluster_name` | string | `default` | Cluster name (must match on all nodes) |
| `cluster.expect_nodes` | int | `0` | Expected node count (0 = disabled) |
| `cluster.node_role` | string | `storage` | Node role: `storage`, `management` |

---

## Caching

### DRAM Cache

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `cache.enabled` | bool | `false` | Enable DRAM cache |
| `cache.max_size` | int64 | `8589934592` | Maximum cache size (8GB) |
| `cache.shard_count` | int | `256` | Number of cache shards (reduces lock contention) |
| `cache.entry_max_size` | int64 | `268435456` | Maximum size per entry (256MB) |
| `cache.ttl` | int | `3600` | Default TTL in seconds (1 hour) |
| `cache.eviction_policy` | string | `arc` | Eviction policy: `lru`, `lfu`, `arc` |
| `cache.prefetch_enabled` | bool | `true` | Enable predictive prefetching (AI/ML) |
| `cache.prefetch_threshold` | int | `2` | Access count before enabling prefetch |
| `cache.prefetch_ahead` | int | `4` | Number of chunks to prefetch |
| `cache.zero_copy_enabled` | bool | `true` | Enable zero-copy reads where supported |
| `cache.distributed_mode` | bool | `false` | Enable distributed cache across nodes |
| `cache.replication_factor` | int | `2` | Replication factor for distributed cache |
| `cache.warmup_enabled` | bool | `false` | Enable cache warmup on startup |
| `cache.warmup_keys` | list | `[]` | Keys to pre-warm on startup |

---

## Data Firewall (QoS)

The data firewall provides rate limiting, bandwidth throttling, and connection management.

### General Settings

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `firewall.enabled` | bool | `false` | Enable data firewall |
| `firewall.default_policy` | string | `allow` | Default policy: `allow`, `deny` |
| `firewall.ip_allowlist` | list | `[]` | Allowed IP addresses/CIDRs |
| `firewall.ip_blocklist` | list | `[]` | Blocked IP addresses/CIDRs |
| `firewall.audit_enabled` | bool | `true` | Enable firewall audit logging |

### Rate Limiting

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `firewall.rate_limiting.enabled` | bool | `false` | Enable rate limiting |
| `firewall.rate_limiting.requests_per_second` | int | `1000` | Global requests per second limit |
| `firewall.rate_limiting.burst_size` | int | `100` | Maximum burst size |
| `firewall.rate_limiting.per_user` | bool | `true` | Apply per-user rate limits |
| `firewall.rate_limiting.per_ip` | bool | `true` | Apply per-IP rate limits |
| `firewall.rate_limiting.per_bucket` | bool | `false` | Apply per-bucket rate limits |

### Bandwidth Throttling

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `firewall.bandwidth.enabled` | bool | `false` | Enable bandwidth throttling |
| `firewall.bandwidth.max_bytes_per_second` | int64 | `1073741824` | Global max bandwidth (1GB/s) |
| `firewall.bandwidth.max_bytes_per_second_per_user` | int64 | `104857600` | Per-user bandwidth (100MB/s) |
| `firewall.bandwidth.max_bytes_per_second_per_bucket` | int64 | `524288000` | Per-bucket bandwidth (500MB/s) |

### Connection Limits

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `firewall.connections.enabled` | bool | `false` | Enable connection limiting |
| `firewall.connections.max_connections` | int | `10000` | Maximum concurrent connections |
| `firewall.connections.max_connections_per_ip` | int | `100` | Per-IP connection limit |
| `firewall.connections.max_connections_per_user` | int | `500` | Per-user connection limit |
| `firewall.connections.idle_timeout_seconds` | int | `60` | Idle connection timeout |

---

## Audit Logging

Enhanced audit logging for compliance and security monitoring.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `audit.enabled` | bool | `true` | Enable audit logging |
| `audit.compliance_mode` | string | `none` | Compliance standard: `none`, `soc2`, `pci`, `hipaa`, `gdpr`, `fedramp` |
| `audit.file_path` | string | `./data/audit/audit.log` | Audit log file path |
| `audit.retention_days` | int | `90` | How long to keep audit logs |
| `audit.buffer_size` | int | `10000` | Async buffer size |
| `audit.integrity_enabled` | bool | `true` | Enable cryptographic integrity (HMAC) |
| `audit.integrity_secret` | string | auto | HMAC secret (auto-generated if empty) |
| `audit.mask_sensitive_data` | bool | `true` | Mask passwords and secrets |

### Audit Log Rotation

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `audit.rotation.enabled` | bool | `true` | Enable log rotation |
| `audit.rotation.max_size_mb` | int | `100` | Max file size before rotation |
| `audit.rotation.max_backups` | int | `10` | Max rotated files to keep |
| `audit.rotation.max_age_days` | int | `30` | Max age of rotated files |
| `audit.rotation.compress` | bool | `true` | Compress rotated files |

### Audit Webhook

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `audit.webhook.enabled` | bool | `false` | Enable webhook output |
| `audit.webhook.url` | string | - | Webhook endpoint URL |
| `audit.webhook.auth_token` | string | - | Authentication token |
| `audit.webhook.batch_size` | int | `100` | Events per batch |
| `audit.webhook.flush_interval_seconds` | int | `30` | Flush interval |

---

## Logging and Metrics

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `log_level` | string | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `log_format` | string | `json` | Log format: `json`, `text` |
| `log_output` | string | `stdout` | Output: `stdout`, `stderr`, or file path |
| `metrics.enabled` | bool | `true` | Enable Prometheus metrics |
| `metrics.path` | string | `/metrics` | Metrics endpoint path |

---

## AI/ML Features (2025)

All AI/ML features are disabled by default and enabled via configuration.

### S3 Express One Zone

Ultra-low latency storage with atomic append operations.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `s3_express.enabled` | bool | `false` | Enable S3 Express One Zone |
| `s3_express.default_zone` | string | `use1-az1` | Default availability zone |
| `s3_express.session_duration` | int | `3600` | Session token duration (seconds) |
| `s3_express.max_append_size` | int64 | `5368709120` | Max atomic append size (5GB) |
| `s3_express.enable_atomic_append` | bool | `true` | Enable atomic append operations |

### Apache Iceberg

Native table format support for data lakehouse workloads.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `iceberg.enabled` | bool | `false` | Enable Iceberg support |
| `iceberg.catalog_type` | string | `rest` | Catalog type: `rest`, `hive`, `glue` |
| `iceberg.catalog_uri` | string | `http://localhost:8181` | Catalog service URI |
| `iceberg.warehouse` | string | `s3://warehouse/` | Default warehouse location |
| `iceberg.default_file_format` | string | `parquet` | File format: `parquet`, `orc`, `avro` |
| `iceberg.metadata_path` | string | `./data/iceberg` | Metadata storage path |
| `iceberg.snapshot_retention` | int | `10` | Snapshots to retain |
| `iceberg.expire_snapshots_older_than` | int | `168` | Expire snapshots older than (hours) |
| `iceberg.enable_acid` | bool | `true` | Enable ACID transactions |

### MCP Server (AI Agent Integration)

Model Context Protocol server for Claude, ChatGPT, and other AI agents.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `mcp.enabled` | bool | `false` | Enable MCP server |
| `mcp.port` | int | `9005` | MCP server port |
| `mcp.max_connections` | int | `100` | Maximum concurrent connections |
| `mcp.enable_tools` | bool | `true` | Enable tool execution |
| `mcp.enable_resources` | bool | `true` | Enable resource access |
| `mcp.enable_prompts` | bool | `true` | Enable prompt templates |
| `mcp.allowed_origins` | list | `[]` | CORS allowed origins |
| `mcp.auth_required` | bool | `true` | Require authentication |
| `mcp.rate_limit_per_minute` | int | `60` | Rate limit per minute |

### GPUDirect Storage

Zero-copy GPU-to-storage transfers for ML training workloads.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `gpudirect.enabled` | bool | `false` | Enable GPUDirect Storage |
| `gpudirect.devices` | list | `[]` | GPU device IDs to use |
| `gpudirect.buffer_pool_size` | int64 | `1073741824` | Buffer pool size (1GB) |
| `gpudirect.max_transfer_size` | int64 | `268435456` | Max transfer size (256MB) |
| `gpudirect.enable_async` | bool | `true` | Enable async transfers |
| `gpudirect.cuda_stream_count` | int | `4` | CUDA streams per GPU |
| `gpudirect.enable_p2p` | bool | `true` | Enable peer-to-peer GPU transfers |
| `gpudirect.nvme_path` | string | `/dev/nvme*` | NVMe device path pattern |

### BlueField DPU

NVIDIA BlueField SmartNIC offload for crypto and compression.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `dpu.enabled` | bool | `false` | Enable DPU offload |
| `dpu.device_index` | int | `0` | DPU device index |
| `dpu.enable_crypto` | bool | `true` | Enable crypto offload |
| `dpu.enable_compression` | bool | `true` | Enable compression offload |
| `dpu.enable_storage` | bool | `true` | Enable storage offload |
| `dpu.enable_network` | bool | `true` | Enable network offload |
| `dpu.enable_rdma` | bool | `true` | Enable RDMA via DPU |
| `dpu.enable_regex` | bool | `false` | Enable regex offload |
| `dpu.health_check_interval` | int | `30` | Health check interval (seconds) |
| `dpu.fallback_on_error` | bool | `true` | Fall back to CPU on error |
| `dpu.min_size_for_offload` | int | `4096` | Min size to offload (bytes) |

### S3 over RDMA

Ultra-low latency object access via InfiniBand/RoCE.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `rdma.enabled` | bool | `false` | Enable RDMA transport |
| `rdma.port` | int | `9100` | RDMA listener port |
| `rdma.device_name` | string | `mlx5_0` | RDMA device name |
| `rdma.gid_index` | int | `0` | GID index for RoCE |
| `rdma.max_send_wr` | int | `128` | Max send work requests per QP |
| `rdma.max_recv_wr` | int | `128` | Max receive work requests per QP |
| `rdma.max_send_sge` | int | `1` | Max scatter/gather per send |
| `rdma.max_recv_sge` | int | `1` | Max scatter/gather per receive |
| `rdma.max_inline_data` | int | `64` | Max inline data size |
| `rdma.memory_pool_size` | int64 | `1073741824` | Registered memory pool (1GB) |
| `rdma.enable_zero_copy` | bool | `true` | Enable zero-copy transfers |
| `rdma.fallback_to_tcp` | bool | `true` | Fall back to TCP if RDMA unavailable |

### NVIDIA NIM Integration

AI inference on stored objects using NVIDIA NIM.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `nim.enabled` | bool | `false` | Enable NIM integration |
| `nim.endpoints` | list | `[https://integrate.api.nvidia.com/v1]` | NIM server endpoints |
| `nim.api_key` | string | - | NVIDIA API key |
| `nim.organization_id` | string | - | NVIDIA organization ID |
| `nim.default_model` | string | `meta/llama-3.1-8b-instruct` | Default inference model |
| `nim.timeout` | int | `60` | Inference timeout (seconds) |
| `nim.max_retries` | int | `3` | Max retries for failed requests |
| `nim.max_batch_size` | int | `100` | Max batch inference size |
| `nim.enable_streaming` | bool | `true` | Enable streaming responses |
| `nim.cache_results` | bool | `true` | Cache inference results |
| `nim.cache_ttl` | int | `3600` | Cache TTL (seconds) |
| `nim.enable_metrics` | bool | `true` | Enable detailed metrics |
| `nim.process_on_upload` | bool | `false` | Auto-process on upload |
| `nim.process_content_types` | list | `[image/*, text/*, application/json]` | Content types to process |

---

## Example Configurations

### Development (Single Node)

```yaml
node_id: dev-node
s3_port: 9000
admin_port: 9001

storage:
  data_dir: ./data

cluster:
  bootstrap: true

auth:
  root_user: admin
  root_password: ${NEBULAIO_AUTH_ROOT_PASSWORD}  # Required, min 12 chars

# TLS is enabled by default with auto-generated certificates
# Uncomment to disable for local development only:
# tls:
#   enabled: false

log_level: debug
log_format: text
```

### High-Performance Volume Storage

```yaml
node_id: volume-node
s3_port: 9000
admin_port: 9001

storage:
  backend: volume
  data_dir: /var/lib/nebulaio
  volume:
    max_volume_size: 34359738368  # 32GB volumes
    auto_create: true

cluster:
  bootstrap: true

auth:
  root_user: admin
  root_password: ${NEBULAIO_AUTH_ROOT_PASSWORD}

log_level: info
```

### Production (3-Node Cluster)

```yaml
node_id: prod-node-1
node_name: nebulaio-prod-1

# TLS is enabled by default with auto-generated certificates
# For production, you can either:
# 1. Use auto-generated certificates (default)
# 2. Provide your own certificates:
tls:
  enabled: true
  # Option 1: Auto-generate (default - just ensure cert_dir is persistent)
  auto_generate: true
  cert_dir: /etc/nebulaio/certs
  # Option 2: Use custom certificates
  # cert_file: /etc/nebulaio/certs/server.crt
  # key_file: /etc/nebulaio/certs/server.key
  # ca_file: /etc/nebulaio/certs/ca.crt

storage:
  data_dir: /var/lib/nebulaio
  erasure_coding:
    enabled: true
    data_shards: 4
    parity_shards: 2

cluster:
  bootstrap: true
  shard_id: 1
  replica_id: 1
  raft_port: 9003
  raft_address: 10.0.1.10:9003
  gossip_port: 9004
  advertise_address: 10.0.1.10
  cluster_name: production
  expect_nodes: 3
  wal_dir: /fast/nvme/wal
  rtt_millisecond: 200
  raft_election_rtt: 10
  raft_heartbeat_rtt: 1
  snapshot_entries: 1024
  compaction_overhead: 500
  check_quorum: true

auth:
  root_user: admin
  root_password: ${NEBULAIO_AUTH_ROOT_PASSWORD}

cache:
  enabled: true
  size: 8GB
  type: arc

log_level: info
log_format: json
log_output: /var/log/nebulaio/nebulaio.log

metrics:
  enabled: true

audit:
  enabled: true
  output: /var/log/nebulaio/audit.log
```

### High-Performance Configuration

```yaml
storage:
  erasure_coding:
    data_shards: 8
    parity_shards: 4

cache:
  enabled: true
  size: 32GB
  type: arc
  metadata_cache_size: 100000
  readahead:
    enabled: true
    size: 16MB

resources:
  max_connections: 50000
  workers: 0  # auto (NumCPU)
  io_threads: 8
```

### LDAP Authentication

```yaml
auth:
  ldap:
    enabled: true
    server: ldap.example.com
    port: 636
    use_tls: true
    bind_dn: cn=nebulaio,ou=services,dc=example,dc=com
    bind_password: ${NEBULAIO_LDAP_BIND_PASSWORD}
    base_dn: ou=users,dc=example,dc=com
    user_filter: "(uid=%s)"
```

### OIDC Authentication

```yaml
auth:
  oidc:
    enabled: true
    issuer: https://auth.example.com/realms/nebulaio
    client_id: nebulaio-client
    client_secret: ${NEBULAIO_OIDC_CLIENT_SECRET}
    redirect_url: https://storage.example.com/oauth/callback
    scopes: [openid, profile, email, groups]
```

---

## Validating Configuration

```bash
# Check configuration syntax
nebulaio config validate --config /etc/nebulaio/config.yaml

# Show effective configuration (with defaults)
nebulaio config show --config /etc/nebulaio/config.yaml
```

---

## Next Steps

- [Quick Start Guide](quick-start.md) - Get started with NebulaIO
- [CLI Tool](cli.md) - Using the command-line interface
- [Standalone Deployment](../deployment/standalone.md) - Production deployment guide
