# Security Features

NebulaIO provides comprehensive enterprise security features including access analytics, encryption key management, mTLS, and distributed tracing for complete visibility and protection.

## Feature Overview

| Feature | Description | Status |
|---------|-------------|--------|
| [Access Analytics](#access-analytics) | Real-time anomaly detection and behavior analysis | Production |
| [Key Rotation](#encryption-key-rotation) | Automated encryption key lifecycle management | Production |
| [mTLS](#mtls-internal-communication) | Mutual TLS for secure internal communication | Production |
| [Distributed Tracing](#opentelemetry-tracing) | OpenTelemetry-compatible request tracing | Production |

## Access Analytics

Real-time access monitoring with machine learning-based anomaly detection to identify suspicious behavior patterns.

### Features

- **Behavior Baselines**: Automatically learns normal user behavior patterns
- **Anomaly Detection**: 8+ built-in detection rules
- **Real-time Alerts**: Immediate notification of suspicious activity
- **Compliance Reports**: Generate audit-ready reports

### Anomaly Types Detected

| Anomaly Type | Description | Default Severity |
|-------------|-------------|------------------|
| Unusual Time Access | Access outside normal hours | Medium |
| High Volume Requests | Request rate exceeds baseline | High |
| Bulk Operations | Mass delete/modify operations | Critical |
| Impossible Travel | Geographically impossible location changes | Critical |
| Data Exfiltration | Large data downloads | Critical |
| First-Time Access | New bucket access | Low |
| Unusual User Agent | Unknown client applications | Medium |
| Rapid Changes | Frequent permission modifications | High |

### Configuration

```yaml
# config.yaml
analytics:
  enabled: true
  baseline_window: 168h        # 7 days for baseline
  min_events_for_baseline: 100
  anomaly_threshold: 3.0       # Standard deviations
  retention_period: 720h       # 30 days
  sampling_rate: 1.0           # 100% sampling
  enable_realtime: true
  batch_size: 1000
  flush_interval: 1m

  # Alert destinations
  alerts:
    webhook:
      enabled: true
      url: https://alerts.example.com/security
      headers:
        Authorization: "Bearer ${ALERT_TOKEN}"
    email:
      enabled: true
      recipients:
        - security@example.com
```

### Custom Rules

```yaml
analytics:
  custom_rules:
    - id: sensitive-bucket-access
      name: Sensitive Bucket Access
      type: SENSITIVE_ACCESS
      enabled: true
      severity: HIGH
      threshold: 1
      window: 1h
      conditions:
        - field: bucket
          operator: in
          value: ["secrets", "credentials", "pii-data"]
      actions:
        - type: alert
        - type: log
```

### API Examples

```bash
# Get anomalies
curl -X GET "http://localhost:9001/api/v1/analytics/anomalies?severity=HIGH&limit=100" \
  -H "Authorization: Bearer $TOKEN"

# Get user baseline
curl -X GET "http://localhost:9001/api/v1/analytics/baselines/user123" \
  -H "Authorization: Bearer $TOKEN"

# Generate report
curl -X POST "http://localhost:9001/api/v1/analytics/reports" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "start_time": "2025-01-01T00:00:00Z",
    "end_time": "2025-01-31T23:59:59Z",
    "format": "json"
  }'

# Acknowledge anomaly
curl -X POST "http://localhost:9001/api/v1/analytics/anomalies/anom-123/acknowledge" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"resolution": "Verified as authorized access"}'
```

## Encryption Key Rotation

Automated encryption key lifecycle management with zero-downtime rotation and re-encryption.

### Features

- **Multiple Key Types**: Master, Data, Bucket, Object, Customer-managed keys
- **Automatic Rotation**: Policy-based scheduled rotation
- **Key Versioning**: Multiple versions for seamless transitions
- **Re-encryption Jobs**: Background object re-encryption
- **HKDF Key Derivation**: Secure key derivation for per-object keys

### Key Types

| Type | Purpose | Default Rotation |
|------|---------|------------------|
| Master | Wraps all other keys | 1 year |
| Data | Encrypts object data | 90 days |
| Bucket | Per-bucket encryption | 6 months |
| Object | Per-object keys (derived) | N/A |
| Customer | Customer-managed keys | Custom |

### Configuration

```yaml
# config.yaml
encryption:
  enabled: true
  algorithm: AES-256-GCM

  key_rotation:
    enabled: true
    check_interval: 1h

    policies:
      - key_type: MASTER
        rotation_interval: 8760h   # 1 year
        max_key_age: 9600h
        grace_period: 720h         # 30 days
        notify_before_expiry: 720h
        retain_versions: 3
        require_reencryption: true

      - key_type: DATA
        rotation_interval: 2160h   # 90 days
        max_key_age: 2880h
        grace_period: 168h         # 7 days
        notify_before_expiry: 336h # 14 days
        retain_versions: 5
        require_reencryption: true

      - key_type: BUCKET
        rotation_interval: 4320h   # 180 days
        max_key_age: 5040h
        grace_period: 336h         # 14 days
        retain_versions: 3
        require_reencryption: false

  # KMS integration (optional)
  kms:
    provider: vault
    vault:
      address: https://vault.example.com
      mount: transit
      key_name: nebulaio-master
```

### API Examples

```bash
# Create a new key
curl -X POST "http://localhost:9001/api/v1/keys" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "BUCKET",
    "algorithm": "AES-256-GCM",
    "alias": "prod-bucket-key",
    "metadata": {"environment": "production"}
  }'

# Rotate a key
curl -X POST "http://localhost:9001/api/v1/keys/key-123/rotate" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"trigger": "manual", "initiated_by": "admin"}'

# List keys
curl -X GET "http://localhost:9001/api/v1/keys?type=DATA" \
  -H "Authorization: Bearer $TOKEN"

# Get rotation history
curl -X GET "http://localhost:9001/api/v1/keys/key-123/rotations" \
  -H "Authorization: Bearer $TOKEN"

# Start re-encryption job
curl -X POST "http://localhost:9001/api/v1/keys/key-123/reencrypt" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "old_version": 1,
    "new_version": 2,
    "buckets": ["my-bucket"],
    "concurrency": 10
  }'
```

## mTLS Internal Communication

Mutual TLS authentication for secure communication between NebulaIO cluster nodes and services.

### Features

- **Built-in CA**: Automatic Certificate Authority management
- **Auto-Issuance**: Automatic certificate issuance for services
- **Auto-Renewal**: Certificates renewed before expiry
- **CRL Support**: Certificate Revocation Lists
- **Strong Ciphers**: Modern TLS 1.2+ with strong cipher suites

### Configuration

```yaml
# config.yaml
mtls:
  enabled: true
  cert_dir: /data/certs

  ca:
    common_name: NebulaIO Internal CA
    validity_duration: 87600h    # 10 years
    organization:
      - NebulaIO

  certificates:
    validity_duration: 8760h     # 1 year
    auto_renew_before: 720h      # 30 days

  tls:
    min_version: "1.2"
    require_client_cert: true
    cipher_suites:
      - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
      - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
      - TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305

  crl:
    enabled: true
    update_interval: 24h
```

### Certificate Types

| Type | Purpose | Key Usage |
|------|---------|-----------|
| Server | Node-to-node server auth | Digital Signature, Key Encipherment |
| Client | Node-to-node client auth | Digital Signature |
| Peer | Both server and client | Digital Signature, Key Encipherment |

### API Examples

```bash
# Initialize CA
curl -X POST "http://localhost:9001/api/v1/mtls/ca/init" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"common_name": "NebulaIO CA"}'

# Issue certificate
curl -X POST "http://localhost:9001/api/v1/mtls/certificates" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "type": "PEER",
    "common_name": "node-01.cluster.local",
    "dns_names": ["node-01", "node-01.cluster.local"],
    "ip_addresses": ["10.0.0.1"]
  }'

# List certificates
curl -X GET "http://localhost:9001/api/v1/mtls/certificates" \
  -H "Authorization: Bearer $TOKEN"

# Revoke certificate
curl -X POST "http://localhost:9001/api/v1/mtls/certificates/cert-123/revoke" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"reason": "Key compromise"}'

# Generate CRL
curl -X GET "http://localhost:9001/api/v1/mtls/crl" \
  -H "Authorization: Bearer $TOKEN"

# Export CA certificate
curl -X GET "http://localhost:9001/api/v1/mtls/ca/certificate" \
  -H "Authorization: Bearer $TOKEN"
```

## OpenTelemetry Tracing

Distributed tracing for complete request visibility across the NebulaIO cluster.

### Features

- **W3C Trace Context**: Standard trace propagation
- **Multiple Exporters**: OTLP, Jaeger, Zipkin support
- **Automatic Instrumentation**: HTTP, S3 API, database operations
- **Custom Spans**: Add custom spans to operations
- **Sampling**: Configurable sampling rates

### Propagation Formats

| Format | Header | Use Case |
|--------|--------|----------|
| W3C Trace Context | `traceparent`, `tracestate` | Standard (recommended) |
| B3 | `X-B3-TraceId`, `X-B3-SpanId` | Zipkin compatibility |
| Jaeger | `uber-trace-id` | Jaeger compatibility |

### Configuration

```yaml
# config.yaml
tracing:
  enabled: true
  service_name: nebulaio
  service_version: 1.0.0
  environment: production

  sampling:
    rate: 0.1              # 10% sampling

  exporter:
    type: otlp             # otlp, jaeger, zipkin, console
    endpoint: http://otel-collector:4317
    headers:
      Authorization: "Bearer ${OTEL_TOKEN}"

  batch:
    timeout: 5s
    max_size: 512

  propagator: w3c          # w3c, b3, jaeger
```

### Kubernetes Deployment

```yaml
# deployments/kubernetes/tracing.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nebulaio-tracing
data:
  OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-collector:4317"
  OTEL_SERVICE_NAME: "nebulaio"
  OTEL_TRACES_SAMPLER: "parentbased_traceidratio"
  OTEL_TRACES_SAMPLER_ARG: "0.1"
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: nebulaio
spec:
  template:
    spec:
      containers:
        - name: nebulaio
          envFrom:
            - configMapRef:
                name: nebulaio-tracing
```

### Trace Attributes

Standard attributes added to all spans:

| Attribute | Description |
|-----------|-------------|
| `service.name` | Service name (nebulaio) |
| `service.version` | Service version |
| `deployment.environment` | Environment (production, staging) |
| `host.name` | Hostname |
| `s3.operation` | S3 operation name |
| `s3.bucket` | Bucket name |
| `s3.key` | Object key |
| `http.method` | HTTP method |
| `http.url` | Request URL |
| `http.status_code` | Response status code |

### Viewing Traces

Traces can be viewed in:
- **Jaeger UI**: http://jaeger:16686
- **Grafana Tempo**: Via Grafana data source
- **AWS X-Ray**: With OTLP/X-Ray exporter
- **Console**: Development mode logging

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        NebulaIO Security Layer                           │
├─────────────────────────────────────────────────────────────────────────┤
│  ┌──────────────────┐  ┌──────────────────┐  ┌────────────────────────┐ │
│  │  Access Analytics │  │   Key Rotation   │  │   Distributed Tracing │ │
│  │  ──────────────── │  │  ────────────── │  │  ───────────────────── │ │
│  │  • Anomaly Detect │  │  • Auto Rotation │  │  • W3C Trace Context  │ │
│  │  • Baselines      │  │  • Key Versions  │  │  • Span Collection    │ │
│  │  • Alerting       │  │  • Re-encryption │  │  • OTLP Export        │ │
│  └────────┬─────────┘  └────────┬─────────┘  └───────────┬────────────┘ │
│           │                     │                         │              │
│  ┌────────▼─────────────────────▼─────────────────────────▼────────────┐ │
│  │                           mTLS Layer                                 │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌────────────┐  │ │
│  │  │     CA      │  │   Server    │  │   Client    │  │    CRL     │  │ │
│  │  │ Management  │  │   Certs     │  │   Certs     │  │  Support   │  │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └────────────┘  │ │
│  └──────────────────────────────────────────────────────────────────────┘ │
│                                    │                                      │
│  ┌─────────────────────────────────▼────────────────────────────────────┐ │
│  │                        S3 API / Admin API                             │ │
│  └───────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Compliance Integration

These security features support compliance with:

| Framework | Features Used |
|-----------|---------------|
| SOC 2 | Access Analytics, Audit Logs, Key Rotation |
| PCI DSS | Encryption, Key Management, mTLS |
| HIPAA | Access Controls, Audit Trails, Encryption |
| GDPR | Access Logs, Data Protection, Key Management |
| FedRAMP | All security features |

## Performance Impact

| Feature | Overhead | Notes |
|---------|----------|-------|
| Access Analytics | < 1ms per request | Async processing |
| Key Rotation | Zero-downtime | Background re-encryption |
| mTLS | ~2-5ms handshake | Connection pooling helps |
| Tracing | < 0.5ms per span | Sampling recommended |

## Next Steps

- [Configure Audit Logging](audit-logging.md)
- [Set up Data Firewall](data-firewall.md)
- [Enable DLP Features](dlp.md)
- [Configure Secret Scanning](secret-scanning.md)
