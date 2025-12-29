# Enhanced Audit Logging

NebulaIO's Enhanced Audit Logging provides compliance-ready audit trails with cryptographic integrity, multiple output destinations, and support for major compliance frameworks.

## Overview

Enhanced Audit Logging delivers:

- **Compliance Support**: SOC2, PCI-DSS, HIPAA, GDPR, FedRAMP
- **Cryptographic Integrity**: HMAC-based tamper detection
- **Multiple Outputs**: File, Webhook, SIEM integration
- **Data Masking**: Automatic PII/sensitive data protection
- **Log Rotation**: Size and time-based rotation with compression
- **Export Formats**: JSON, CSV, CEF (Common Event Format)

## Architecture

```text

┌─────────────────────────────────────────────────────────────┐
│                  Enhanced Audit Logger                       │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   Event     │  │  Integrity  │  │   Data Masking      │  │
│  │  Enrichment │──│   Chain     │──│                     │  │
│  │             │  │  (HMAC)     │  │  ****@example.com   │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│         │                                    │               │
│         ▼                                    ▼               │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                    Output Router                         ││
│  └────────────┬───────────────┬────────────────┬───────────┘│
│               │               │                │             │
│               ▼               ▼                ▼             │
│        ┌──────────┐   ┌──────────────┐  ┌──────────────┐    │
│        │   File   │   │   Webhook    │  │     SIEM     │    │
│        │ (Rotate) │   │  (HTTP/S)    │  │  (CEF/JSON)  │    │
│        └──────────┘   └──────────────┘  └──────────────┘    │
└─────────────────────────────────────────────────────────────┘

```bash

## Configuration

```yaml

audit:
  enabled: true
  compliance_mode: soc2              # none | soc2 | pci | hipaa | gdpr | fedramp

  # File output
  file_path: /var/log/nebulaio/audit.log

  # Retention
  retention_days: 365                # How long to keep logs
  buffer_size: 10000                 # Event buffer size

  # Cryptographic integrity
  integrity_enabled: true
  integrity_secret: "your-secret-key-here"

  # Data masking
  mask_sensitive_data: true
  sensitive_fields:
    - password
    - secret_key
    - credit_card
    - ssn
    - email

  # Rotation
  rotation:
    enabled: true
    max_size_mb: 100                 # Rotate at 100MB
    max_age_days: 7                  # Or after 7 days
    max_backups: 10                  # Keep 10 rotated files
    compress: true                   # Compress rotated files

  # Webhook output
  webhook:
    enabled: true
    url: https://siem.example.com/events
    headers:
      Authorization: "Bearer token"
    batch_size: 100
    flush_interval: 5s
    retry_count: 3

```bash

## Compliance Modes

### SOC 2

Focuses on security, availability, and confidentiality:

```yaml

audit:
  compliance_mode: soc2

```text

Captures:

- All authentication events
- Access control changes
- Data access events
- System configuration changes
- Security incidents

### PCI-DSS

Payment Card Industry compliance:

```yaml

audit:
  compliance_mode: pci
  mask_sensitive_data: true
  sensitive_fields:
    - credit_card
    - cvv
    - pan

```text

Captures:

- Cardholder data access
- Authentication attempts
- Access control modifications
- Network activity

### HIPAA

Healthcare data protection:

```yaml

audit:
  compliance_mode: hipaa
  mask_sensitive_data: true
  sensitive_fields:
    - patient_id
    - ssn
    - medical_record

```text

Captures:

- PHI access events
- User authentication
- Data modifications
- Access control changes

### GDPR

EU data protection:

```yaml

audit:
  compliance_mode: gdpr
  mask_sensitive_data: true

```text

Captures:

- Personal data access
- Data subject requests
- Consent changes
- Cross-border transfers

### FedRAMP

US government compliance:

```yaml

audit:
  compliance_mode: fedramp
  integrity_enabled: true

```text

Captures:

- All security-relevant events
- System access
- Configuration changes
- Incident response actions

## Event Structure

### Standard Event

```json

{
  "id": "evt_abc123",
  "timestamp": "2024-01-15T10:30:00.000Z",
  "sequence_number": 12345,
  "session_id": "sess_xyz789",
  "event_type": "s3:ObjectAccessed:Get",
  "event_source": "s3",
  "request_id": "req_def456",
  "user_identity": {
    "type": "IAMUser",
    "user_id": "user123",
    "username": "alice",
    "access_key_id": "AKIA..."
  },
  "source_ip": "192.168.1.100",
  "user_agent": "aws-sdk-go/1.44.0",
  "resource": {
    "type": "object",
    "bucket": "my-bucket",
    "key": "documents/report.pdf"
  },
  "action": "GetObject",
  "result": "success",
  "duration_ms": 45,
  "bytes_out": 1048576
}

```bash

### Enhanced Event (with Compliance)

```json

{
  "id": "evt_abc123",
  "timestamp": "2024-01-15T10:30:00.000Z",
  "sequence_number": 12345,
  "event_type": "s3:ObjectAccessed:Get",
  "compliance": {
    "framework": "hipaa",
    "data_classification": "phi",
    "retention_required": true
  },
  "integrity": {
    "algorithm": "HMAC-SHA256",
    "signature": "abc123...",
    "previous_hash": "def456...",
    "chain_valid": true
  },
  "geo_location": {
    "country": "US",
    "region": "CA",
    "city": "San Francisco"
  }
}

```bash

## Event Types

### S3 Object Events

| Event Type | Description |
| ------------ | ------------- |
| s3:ObjectCreated:Put | Object uploaded |
| s3:ObjectCreated:Post | Object uploaded via POST |
| s3:ObjectCreated:Copy | Object copied |
| s3:ObjectCreated:CompleteMultipartUpload | Multipart upload completed |
| s3:ObjectRemoved:Delete | Object deleted |
| s3:ObjectRemoved:DeleteMarkerCreated | Delete marker created |
| s3:ObjectAccessed:Get | Object downloaded |
| s3:ObjectAccessed:Head | Object metadata retrieved |

### S3 Bucket Events

| Event Type | Description |
| ------------ | ------------- |
| s3:BucketCreated | Bucket created |
| s3:BucketRemoved | Bucket deleted |
| s3:BucketListed | Bucket contents listed |

### Authentication Events

| Event Type | Description |
| ------------ | ------------- |
| auth:Login | User logged in |
| auth:LoginFailed | Failed login attempt |
| auth:Logout | User logged out |
| auth:TokenRefreshed | Token refreshed |

### IAM Events

| Event Type | Description |
| ------------ | ------------- |
| iam:UserCreated | User account created |
| iam:UserUpdated | User account modified |
| iam:UserDeleted | User account deleted |
| iam:PolicyCreated | Policy created |
| iam:PolicyUpdated | Policy modified |
| iam:PolicyDeleted | Policy deleted |
| iam:AccessKeyCreated | Access key generated |
| iam:AccessKeyDeleted | Access key revoked |

## Cryptographic Integrity

### Chain of Custody

Each event includes a cryptographic hash that chains to the previous event:

```text

Event 1: hash(event_1 + secret) = H1
Event 2: hash(event_2 + H1 + secret) = H2
Event 3: hash(event_3 + H2 + secret) = H3
...

```text

This creates an immutable chain where tampering is detectable.

### Verification

```bash

curl -X POST http://localhost:9000/admin/audit/verify \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "start_time": "2024-01-01T00:00:00Z",
    "end_time": "2024-01-31T23:59:59Z"
  }'

```text

Response:

```json

{
  "valid": true,
  "events_verified": 125678,
  "chain_intact": true,
  "first_event": "evt_abc123",
  "last_event": "evt_xyz789"
}

```bash

## Data Masking

### Automatic Masking

Sensitive fields are automatically masked:

```json

{
  "user_identity": {
    "username": "alice",
    "email": "a****@example.com"
  },
  "extra": {
    "credit_card": "****-****-****-1234",
    "ssn": "***-**-6789"
  }
}

```bash

### Custom Patterns

```yaml

audit:
  mask_sensitive_data: true
  sensitive_fields:
    - password
    - secret
    - token
    - api_key
  mask_patterns:
    - "\\b\\d{3}-\\d{2}-\\d{4}\\b"  # SSN pattern
    - "\\b\\d{16}\\b"               # Credit card

```bash

## Output Destinations

### File Output

```yaml

audit:
  file_path: /var/log/nebulaio/audit.log
  rotation:
    enabled: true
    max_size_mb: 100
    max_age_days: 7
    max_backups: 10
    compress: true

```bash

### Webhook Output

```yaml

audit:
  webhook:
    enabled: true
    url: https://siem.example.com/events
    headers:
      Authorization: "Bearer token"
      Content-Type: "application/json"
    batch_size: 100
    flush_interval: 5s
    retry_count: 3
    timeout: 30s

```bash

### Multiple Outputs

```yaml

audit:
  outputs:
    - type: file
      path: /var/log/nebulaio/audit.log
    - type: webhook
      url: https://siem.example.com/events
    - type: webhook
      url: https://backup-siem.example.com/events

```bash

## Export Formats

### JSON Export

```bash

curl -X GET "http://localhost:9000/admin/audit/export?format=json&start=2024-01-01&end=2024-01-31" \
  -H "Authorization: Bearer $TOKEN" \
  -o audit-export.json

```bash

### CSV Export

```bash

curl -X GET "http://localhost:9000/admin/audit/export?format=csv&start=2024-01-01&end=2024-01-31" \
  -H "Authorization: Bearer $TOKEN" \
  -o audit-export.csv

```bash

### CEF Export (SIEM Integration)

```bash

curl -X GET "http://localhost:9000/admin/audit/export?format=cef&start=2024-01-01&end=2024-01-31" \
  -H "Authorization: Bearer $TOKEN" \
  -o audit-export.cef

```text

CEF format example:

```text

CEF:0|NebulaIO|AuditLog|1.0|s3:ObjectAccessed:Get|Object Access|5|src=192.168.1.100 suser=alice dst=my-bucket/file.pdf outcome=success

```bash

## API Usage

### Query Audit Events

```bash

curl -X GET "http://localhost:9000/admin/audit/events?bucket=my-bucket&user=alice&start=2024-01-01" \
  -H "Authorization: Bearer $TOKEN"

```bash

### Filter Options

| Parameter | Description | Example |
| ----------- | ------------- | --------- |
| start_time | Start of time range | 2024-01-01T00:00:00Z |
| end_time | End of time range | 2024-01-31T23:59:59Z |
| bucket | Filter by bucket | my-bucket |
| user | Filter by username | alice |
| event_type | Filter by event type | s3:ObjectCreated:* |
| result | Filter by result | success, failure |
| source_ip | Filter by IP | 192.168.1.100 |
| max_results | Limit results | 1000 |
| next_token | Pagination token | abc123 |

### Get Audit Statistics

```bash

curl -X GET "http://localhost:9000/admin/audit/stats?start=2024-01-01&end=2024-01-31" \
  -H "Authorization: Bearer $TOKEN"

```text

Response:

```json

{
  "total_events": 1234567,
  "events_by_type": {
    "s3:ObjectAccessed:Get": 500000,
    "s3:ObjectCreated:Put": 300000,
    "auth:Login": 50000
  },
  "events_by_result": {
    "success": 1200000,
    "failure": 34567
  },
  "top_users": [
    {"username": "alice", "events": 100000},
    {"username": "bob", "events": 75000}
  ],
  "integrity_status": "valid"
}

```bash

## Log Rotation

### Size-Based

```yaml

rotation:
  enabled: true
  max_size_mb: 100    # Rotate when file reaches 100MB

```bash

### Time-Based

```yaml

rotation:
  enabled: true
  max_age_days: 7     # Rotate weekly

```bash

### Retention

```yaml

rotation:
  max_backups: 10     # Keep 10 rotated files
  compress: true      # Compress with gzip

```bash

### Rotated File Naming

```text

audit.log           # Current log
audit.log.1.gz      # Most recent rotation
audit.log.2.gz      # Second most recent
...
audit.log.10.gz     # Oldest (will be deleted on next rotation)

```bash

## SIEM Integration

### Splunk

```yaml

audit:
  webhook:
    url: https://splunk.example.com:8088/services/collector/event
    headers:
      Authorization: "Splunk YOUR-HEC-TOKEN"

```bash

### Elasticsearch

```yaml

audit:
  webhook:
    url: https://elasticsearch.example.com:9200/nebulaio-audit/_doc
    headers:
      Authorization: "Basic base64-credentials"

```bash

### Datadog

```yaml

audit:
  webhook:
    url: https://http-intake.logs.datadoghq.com/v1/input
    headers:
      DD-API-KEY: "your-api-key"

```bash

## Monitoring

### Prometheus Metrics

```bash

# Audit events by type
nebulaio_audit_events_total{event_type="...",result="success|failure"}

# Audit log size
nebulaio_audit_log_size_bytes

# Integrity chain status
nebulaio_audit_integrity_valid

# Webhook delivery
nebulaio_audit_webhook_deliveries_total{status="success|failure"}
nebulaio_audit_webhook_latency_seconds

```

## Best Practices

1. **Enable integrity**: Always enable cryptographic integrity for compliance
2. **Use appropriate retention**: Match retention to compliance requirements
3. **Configure rotation**: Prevent disk exhaustion with rotation
4. **Mask sensitive data**: Enable masking for PII/PHI workloads
5. **Multiple outputs**: Send to both file and SIEM for redundancy
6. **Regular verification**: Periodically verify integrity chain
7. **Secure secrets**: Protect integrity secret keys
8. **Monitor delivery**: Alert on webhook delivery failures

## Troubleshooting

### Missing Events

1. Check buffer size isn't too small
2. Verify audit is enabled for event type
3. Check filter rules aren't excluding events

### Integrity Verification Fails

1. Verify integrity secret hasn't changed
2. Check for log file tampering
3. Ensure no events were manually deleted

### Webhook Delivery Failures

1. Check endpoint URL is correct
2. Verify authentication headers
3. Check network connectivity
4. Review timeout settings
