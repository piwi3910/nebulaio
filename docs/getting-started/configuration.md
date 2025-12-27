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

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `tls.enabled` | bool | `false` | Enable TLS encryption |
| `tls.cert_file` | string | - | Path to TLS certificate |
| `tls.key_file` | string | - | Path to TLS private key |
| `tls.ca_file` | string | - | Path to CA certificate for mTLS |
| `tls.client_auth` | string | `none` | Client auth: `none`, `request`, `require` |
| `tls.min_version` | string | `TLS12` | Minimum TLS version |

---

## Storage Settings

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.data_dir` | string | `/var/lib/nebulaio` | Root data directory |
| `storage.backend` | string | `fs` | Storage backend |
| `storage.path` | string | `{data_dir}/objects` | Object storage path |
| `storage.default_storage_class` | string | `STANDARD` | Default storage class |

### Erasure Coding

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.erasure_coding.enabled` | bool | `true` | Enable erasure coding |
| `storage.erasure_coding.data_shards` | int | `4` | Number of data shards |
| `storage.erasure_coding.parity_shards` | int | `2` | Number of parity shards |

**Common configurations:** 4+2 (50% overhead, 2 failures), 8+4 (50% overhead, 4 failures), 10+4 (40% overhead, 4 failures).

---

## Authentication

### Root Credentials

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `auth.root_user` | string | `admin` | Root/admin username |
| `auth.root_password` | string | - | Root/admin password (required) |
| `auth.jwt_secret` | string | auto-generated | Secret for JWT signing |
| `auth.jwt_expiry` | duration | `24h` | JWT token expiry time |

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

### Raft Consensus

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `cluster.bootstrap` | bool | `false` | Bootstrap new cluster (first node only) |
| `cluster.raft_port` | int | `9003` | Raft consensus port |
| `cluster.advertise_address` | string | - | Address advertised to other nodes |
| `cluster.join_addresses` | list | - | Addresses of existing cluster nodes |
| `cluster.raft_election_timeout` | duration | `1s` | Raft election timeout |
| `cluster.raft_heartbeat_timeout` | duration | `500ms` | Raft heartbeat timeout |

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
| `cache.enabled` | bool | `true` | Enable DRAM cache |
| `cache.size` | string | `1GB` | Maximum cache size |
| `cache.type` | string | `lru` | Eviction policy: `lru`, `lfu`, `arc` |
| `cache.ttl` | duration | `5m` | Default cache TTL |
| `cache.metadata_cache_size` | int | `10000` | Metadata entries to cache |
| `cache.metadata_cache_ttl` | duration | `60s` | Metadata cache TTL |
| `cache.readahead.enabled` | bool | `true` | Enable read-ahead |
| `cache.readahead.size` | string | `4MB` | Read-ahead window size |

---

## Logging and Metrics

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `log_level` | string | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `log_format` | string | `json` | Log format: `json`, `text` |
| `log_output` | string | `stdout` | Output: `stdout`, `stderr`, or file path |
| `metrics.enabled` | bool | `true` | Enable Prometheus metrics |
| `metrics.path` | string | `/metrics` | Metrics endpoint path |
| `audit.enabled` | bool | `false` | Enable audit logging |
| `audit.output` | string | `stdout` | Audit log output |

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
  root_password: admin123

log_level: debug
log_format: text
```

### Production (3-Node Cluster)

```yaml
node_id: prod-node-1
node_name: nebulaio-prod-1

tls:
  enabled: true
  cert_file: /etc/nebulaio/certs/server.crt
  key_file: /etc/nebulaio/certs/server.key

storage:
  data_dir: /var/lib/nebulaio
  erasure_coding:
    enabled: true
    data_shards: 4
    parity_shards: 2

cluster:
  bootstrap: true
  raft_port: 9003
  gossip_port: 9004
  advertise_address: 10.0.1.10
  cluster_name: production
  expect_nodes: 3

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
