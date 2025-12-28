# AI/ML Features Security Guide

This guide covers security considerations, best practices, and configurations for NebulaIO's AI/ML features.

## Overview

NebulaIO's AI/ML features (S3 Express, GPUDirect, MCP Server, NIM Integration, RDMA) require additional security considerations due to their high-performance nature and integration with external services.

---

## S3 Express One Zone Security

### Access Control

S3 Express sessions use temporary credentials with limited scope:

```yaml
s3_express:
  enabled: true

  security:
    # Session token configuration
    session_duration: 3600           # Maximum 1 hour
    max_session_duration: 14400      # Maximum 4 hours
    session_renew_threshold: 300     # Renew 5 min before expiry

    # Scope restrictions
    restrict_to_bucket: true         # Session limited to specific bucket
    restrict_to_prefix: false        # Optional: limit to key prefix

    # IP restrictions
    require_source_ip: true
    allowed_cidrs:
      - "10.0.0.0/8"
      - "192.168.0.0/16"
```

### Atomic Append Security

```yaml
s3_express:
  atomic_append:
    enabled: true

    # Prevent abuse
    max_append_size: 5368709120      # 5GB max per append
    max_appends_per_minute: 1000     # Rate limit
    max_object_size: 53687091200     # 50GB max total size

    # Audit logging
    audit_appends: true
```

### Best Practices

1. **Use short session durations**: Default to 1 hour, extend only when necessary
2. **Restrict by IP**: Always specify allowed CIDRs in production
3. **Monitor session creation**: Alert on unusual session patterns
4. **Audit atomic appends**: Track all append operations for compliance

---

## GPUDirect Storage Security

### Device Access Control

```yaml
gpudirect:
  enabled: true

  security:
    # GPU device restrictions
    allowed_devices: [0, 1, 2, 3]    # Only specific GPUs
    require_cuda_mps: false          # Multi-Process Service

    # Buffer security
    buffer_pool:
      max_size: 8589934592           # 8GB limit
      per_user_limit: 2147483648     # 2GB per user
      zero_on_free: true             # Clear memory on release

    # DMA restrictions
    dma:
      require_iommu: true            # Require IOMMU protection
      allowed_transfer_size: 268435456  # 256MB max single transfer
```

### Memory Protection

```yaml
gpudirect:
  memory_protection:
    # Prevent GPU memory leaks
    timeout_seconds: 300             # Release buffers after 5 min idle
    max_allocations_per_user: 100

    # Prevent data exfiltration
    disallow_peer_access: false      # Control GPU-to-GPU transfers
    require_encryption: true         # Encrypt data in GPU memory
```

### Best Practices

1. **Enable IOMMU**: Protect against DMA attacks
2. **Set buffer limits**: Prevent memory exhaustion
3. **Zero memory on release**: Prevent data leakage between users
4. **Monitor GPU memory usage**: Alert on unusual patterns

---

## MCP Server Security

The Model Context Protocol server allows AI agents to interact with NebulaIO.

### Authentication & Authorization

```yaml
mcp:
  enabled: true
  port: 9005

  security:
    # Authentication
    auth_required: true
    auth_methods:
      - bearer_token
      - api_key
      - mtls

    # Token configuration
    token_expiry: 3600
    refresh_enabled: true
    max_refresh_count: 24            # Max 24 hour total session

    # Authorization
    require_explicit_grants: true    # No implicit permissions
    default_policy: deny
```

### Tool Execution Security

```yaml
mcp:
  tools:
    enabled: true

    security:
      # Limit available tools
      allowed_tools:
        - read_object
        - write_object
        - list_objects
      blocked_tools:
        - delete_bucket
        - modify_policy

      # Execution limits
      max_execution_time: 60         # 60 second timeout
      max_concurrent_executions: 10
      max_result_size: 10485760      # 10MB max result

      # Sandboxing
      sandbox_enabled: true
      network_access: restricted     # restricted | none | full
```

### Resource Access Security

```yaml
mcp:
  resources:
    enabled: true

    security:
      # Access control
      require_bucket_policy: true
      honor_object_acl: true

      # Content limits
      max_read_size: 104857600       # 100MB max read
      allowed_content_types:
        - "text/*"
        - "application/json"
        - "application/xml"
      blocked_content_types:
        - "application/octet-stream"

      # Rate limiting
      requests_per_minute: 60
      bytes_per_minute: 1073741824   # 1GB
```

### CORS Configuration

```yaml
mcp:
  cors:
    enabled: true
    allowed_origins:
      - "https://claude.ai"
      - "https://chat.openai.com"
      - "https://your-app.example.com"
    allowed_methods:
      - POST
    allowed_headers:
      - Authorization
      - Content-Type
    max_age: 3600
```

### Best Practices

1. **Require authentication**: Never run MCP without auth in production
2. **Limit tool access**: Only enable tools needed for your use case
3. **Set execution timeouts**: Prevent runaway operations
4. **Monitor agent activity**: Log all tool executions
5. **Use allowlisted origins**: Restrict CORS to known AI platforms

---

## NVIDIA NIM Integration Security

### API Key Management

```yaml
nim:
  enabled: true

  security:
    # API key storage
    api_key: ${NIM_API_KEY}          # Use environment variable
    api_key_rotation: true
    api_key_rotation_days: 90

    # Key restrictions
    organization_id: ${NIM_ORG_ID}
    allowed_models:
      - "meta/llama-3.1-8b-instruct"
      - "nvidia/grounding-dino"
    blocked_models:
      - "*embedding*"                 # Block embedding models
```

### Request Security

```yaml
nim:
  security:
    # Content filtering
    input_validation:
      max_input_size: 1048576        # 1MB max input
      allowed_input_types:
        - "image/jpeg"
        - "image/png"
        - "text/plain"
        - "application/json"
      scan_for_malware: true

    # Output validation
    output_validation:
      max_output_size: 10485760      # 10MB max output
      sanitize_output: true

    # Network security
    network:
      use_private_endpoint: true     # Use VPC endpoint if available
      require_tls: true
      verify_certificates: true
```

### Cost Controls

```yaml
nim:
  security:
    cost_controls:
      enabled: true
      daily_limit_usd: 100
      monthly_limit_usd: 2000
      per_user_limit_usd: 50
      alert_threshold_percent: 80
```

### Best Practices

1. **Use environment variables**: Never hardcode API keys
2. **Rotate keys regularly**: 90 days maximum
3. **Set cost limits**: Prevent runaway inference costs
4. **Validate inputs**: Scan content before sending to NIM
5. **Use private endpoints**: Avoid public internet when possible

---

## S3 over RDMA Security

### Transport Security

```yaml
rdma:
  enabled: true
  port: 9100

  security:
    # Authentication
    require_authentication: true
    auth_method: preshared_key       # preshared_key | certificate

    preshared_key:
      key: ${RDMA_PSK}               # Use environment variable
      rotation_days: 30

    # Certificate-based auth
    certificate:
      cert_file: /etc/nebulaio/rdma/server.crt
      key_file: /etc/nebulaio/rdma/server.key
      ca_file: /etc/nebulaio/rdma/ca.crt
      require_client_cert: true
```

### Memory Registration Security

```yaml
rdma:
  memory:
    # Registration limits
    max_mr_size: 1073741824          # 1GB max memory region
    max_mr_count: 1000               # Max registered regions
    per_connection_limit: 10

    # Protection
    access_flags:
      local_write: true
      remote_write: true
      remote_read: true
      remote_atomic: false           # Disable atomic operations

    # Cleanup
    deregister_on_disconnect: true
    timeout_seconds: 300
```

### Network Isolation

```yaml
rdma:
  network:
    # Device restrictions
    allowed_devices:
      - mlx5_0
      - mlx5_1
    allowed_ports: [1]
    allowed_gid_indices: [0, 1]

    # VLAN isolation
    require_vlan: true
    allowed_vlans: [100, 101]

    # Traffic isolation
    partition_key: 0x8001
    service_level: 0
```

### Best Practices

1. **Use dedicated RDMA network**: Isolate from regular traffic
2. **Enable mTLS**: Require client certificates for RDMA connections
3. **Limit memory registration**: Prevent memory exhaustion attacks
4. **Use partition keys**: Isolate different tenants
5. **Monitor RDMA connections**: Alert on unusual connection patterns

---

## BlueField DPU Security

### Crypto Offload Security

```yaml
dpu:
  enabled: true

  crypto:
    enabled: true

    security:
      # Key management
      key_storage: tpm              # tpm | vault | local
      key_rotation_days: 90
      key_derivation: hkdf_sha256

      # Algorithm restrictions
      allowed_algorithms:
        - AES-256-GCM
        - ChaCha20-Poly1305
      min_key_size: 256

      # Audit
      log_crypto_operations: true
```

### Compression Offload Security

```yaml
dpu:
  compression:
    enabled: true

    security:
      # Prevent decompression bombs
      max_compression_ratio: 100
      max_decompressed_size: 1073741824  # 1GB

      # Rate limiting
      max_operations_per_second: 10000
```

### Best Practices

1. **Use hardware key storage**: TPM or HSM for key management
2. **Rotate keys regularly**: DPU-managed keys need rotation too
3. **Limit compression ratios**: Prevent decompression bomb attacks
4. **Monitor DPU health**: Alert on hardware errors

---

## General Security Recommendations

### Network Segmentation

```
┌─────────────────────────────────────────────────────────────────┐
│                    Production Network                            │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐           │
│  │ Load        │   │ NebulaIO    │   │ NebulaIO    │           │
│  │ Balancer    │───│ S3 API      │───│ Admin API   │           │
│  │             │   │ :9000       │   │ :9001       │           │
│  └─────────────┘   └──────┬──────┘   └─────────────┘           │
└─────────────────────────────┼────────────────────────────────────┘
                              │
┌─────────────────────────────┼────────────────────────────────────┐
│                    AI/ML Network (Isolated)                      │
│  ┌─────────────┐   ┌───────┴─────┐   ┌─────────────┐           │
│  │ MCP Server  │   │ GPUDirect   │   │ RDMA        │           │
│  │ :9005       │   │ Storage     │   │ :9100       │           │
│  └─────────────┘   └─────────────┘   └─────────────┘           │
└─────────────────────────────────────────────────────────────────┘
```

### Audit Logging

```yaml
audit:
  enabled: true

  # AI/ML specific events
  aiml_events:
    - mcp_tool_execution
    - nim_inference_request
    - gpudirect_transfer
    - rdma_connection
    - s3_express_session_create
    - dpu_crypto_operation

  # Retention
  retention_days: 365              # Longer retention for AI/ML

  # Alerting
  alerts:
    - event: mcp_tool_execution
      condition: "tool = 'delete_bucket'"
      severity: critical

    - event: nim_inference_request
      condition: "cost > 10"
      severity: warning
```

### Rate Limiting

```yaml
firewall:
  rate_limiting:
    # AI/ML endpoints
    aiml_endpoints:
      mcp:
        requests_per_second: 100
        burst_size: 50

      nim:
        requests_per_second: 50
        burst_size: 10

      gpudirect:
        transfers_per_second: 1000
        bytes_per_second: 10737418240  # 10GB/s

      rdma:
        connections_per_second: 100
        operations_per_second: 100000
```

---

## Security Checklist

### Before Enabling AI/ML Features

- [ ] Review and enable only needed features
- [ ] Configure authentication for all endpoints
- [ ] Set up network segmentation
- [ ] Configure rate limiting
- [ ] Enable audit logging
- [ ] Set up cost controls (NIM)
- [ ] Configure memory limits (GPUDirect, RDMA)
- [ ] Set execution timeouts (MCP)
- [ ] Review and restrict tool access (MCP)
- [ ] Configure TLS/mTLS where applicable
- [ ] Set up monitoring and alerting
- [ ] Document security configurations
- [ ] Conduct security review

### Regular Security Tasks

- [ ] Rotate API keys (NIM, MCP) - Every 90 days
- [ ] Rotate RDMA pre-shared keys - Every 30 days
- [ ] Review audit logs - Weekly
- [ ] Review cost reports - Weekly
- [ ] Update allowed origins/IPs - As needed
- [ ] Review tool execution logs - Weekly
- [ ] Check for security updates - Monthly

---

## Related Documentation

- [MCP Server](mcp-server.md) - AI agent integration
- [GPUDirect Storage](gpudirect.md) - GPU acceleration
- [S3 Express](s3-express.md) - Low-latency operations
- [Security Features](security-features.md) - General security
- [Audit Logging](../operations/audit-logging.md) - Audit configuration
