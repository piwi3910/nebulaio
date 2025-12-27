# Security Architecture

This document describes the security architecture of NebulaIO, covering authentication, authorization, encryption, network security, and data protection.

## Security Model Overview

NebulaIO implements a defense-in-depth security model with multiple layers:

```
┌─────────────────────────────────────────────────────────────────────┐
│                       Client Applications                            │
└─────────────────────────────────────────────────────────────────────┘
                                  │
    ┌─────────────────────────────┼─────────────────────────────┐
    │  Network Security: TLS, IP Filtering, Rate Limiting       │
    ├─────────────────────────────┼─────────────────────────────┤
    │  Authentication: AWS Sig V4, LDAP, OIDC/SSO               │
    ├─────────────────────────────┼─────────────────────────────┤
    │  Authorization: IAM Policies, Bucket Policies, ACLs       │
    ├─────────────────────────────┼─────────────────────────────┤
    │  Data Protection: Encryption, Object Lock, Versioning     │
    ├─────────────────────────────┼─────────────────────────────┤
    │  Audit & Monitoring: Logging, Analytics, Tracing          │
    └─────────────────────────────┴─────────────────────────────┘
```

---

## Authentication

### Access Key / Secret Key (AWS Signature V4)

The primary S3 API authentication uses AWS Signature Version 4.

```yaml
auth:
  signature_version: v4
  access_key_min_length: 16
  secret_key_min_length: 32
```

```bash
# Create user with access keys
nebulaio-cli admin user add alice --generate-keys

# Rotate access keys
nebulaio-cli admin user keys rotate alice
```

### LDAP Integration

Enterprise authentication via LDAP or Active Directory:

```yaml
auth:
  ldap:
    enabled: true
    server: ldap://ldap.example.com:389
    bind_dn: "cn=nebulaio,ou=service,dc=example,dc=com"
    bind_password: "${LDAP_BIND_PASSWORD}"
    user_search_base: "ou=users,dc=example,dc=com"
    user_search_filter: "(uid={username})"
    start_tls: true
    group_mappings:
      - ldap_group: "cn=storage-admins,ou=groups,dc=example,dc=com"
        policy: admin
      - ldap_group: "cn=developers,ou=groups,dc=example,dc=com"
        policy: readwrite
```

### OIDC / SSO

OpenID Connect for single sign-on with identity providers:

```yaml
auth:
  oidc:
    enabled: true
    provider_url: https://auth.example.com
    client_id: nebulaio
    client_secret: "${OIDC_CLIENT_SECRET}"
    scopes: [openid, profile, email, groups]
    claims:
      username: preferred_username
      groups: groups
    group_mappings:
      - claim_value: storage-admins
        policy: admin
```

---

## Authorization

### IAM Policies

AWS-compatible policies defining user/group permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["s3:GetObject", "s3:PutObject", "s3:ListBucket"],
    "Resource": ["arn:aws:s3:::my-bucket", "arn:aws:s3:::my-bucket/*"]
  }]
}
```

**Built-in Policies**: `admin`, `readwrite`, `readonly`, `writeonly`, `diagnostics`

```bash
nebulaio-cli admin policy attach alice readwrite
nebulaio-cli admin policy create dev-policy policy.json
```

### Bucket Policies

Fine-grained bucket-level access control with conditions:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Deny",
    "Principal": "*",
    "Action": "s3:*",
    "Resource": ["arn:aws:s3:::internal-data/*"],
    "Condition": {
      "NotIpAddress": {"aws:SourceIp": ["10.0.0.0/8"]}
    }
  }]
}
```

### Access Control Lists (ACLs)

S3-compatible canned ACLs for object-level permissions:

| ACL | Description |
|-----|-------------|
| `private` | Owner has full control (default) |
| `public-read` | Owner full control, public read |
| `authenticated-read` | Owner full control, authenticated users read |
| `bucket-owner-full-control` | Bucket and object owner have full control |

---

## Encryption

### TLS for Transport

All external communication encrypted with TLS 1.2+:

```yaml
tls:
  enabled: true
  cert_file: /etc/nebulaio/tls.crt
  key_file: /etc/nebulaio/tls.key
  min_version: "1.2"
  auto_tls:
    enabled: true
    domain: storage.example.com
```

### Server-Side Encryption (SSE-S3)

NebulaIO-managed AES-256 encryption at rest:

```bash
aws s3api put-bucket-encryption \
  --endpoint-url http://localhost:9000 \
  --bucket my-bucket \
  --server-side-encryption-configuration '{
    "Rules": [{"ApplyServerSideEncryptionByDefault": {"SSEAlgorithm": "AES256"}}]
  }'
```

### Server-Side Encryption with KMS (SSE-KMS)

External key management integration:

```yaml
encryption:
  kms:
    provider: vault  # vault, aws, azure, gcp
    vault:
      address: https://vault.example.com
      mount: transit
      key_name: nebulaio-master
```

```bash
aws s3 cp file.txt s3://my-bucket/ \
  --endpoint-url http://localhost:9000 \
  --sse aws:kms --sse-kms-key-id arn:aws:kms:...
```

### Client-Side Encryption

NebulaIO supports client-side encryption where data is encrypted before upload. Use AWS SDK encryption clients or custom implementations.

---

## Network Security

### Data Firewall

Built-in traffic control with rate limiting and IP filtering:

```yaml
firewall:
  enabled: true
  ip_allowlist: ["10.0.0.0/8", "192.168.0.0/16"]
  ip_blocklist: ["203.0.113.0/24"]
  rate_limiting:
    enabled: true
    requests_per_second: 1000
    burst_size: 100
  bandwidth:
    max_bytes_per_second: 1073741824  # 1 GB/s
```

### mTLS for Internal Communication

Mutual TLS between cluster nodes:

```yaml
mtls:
  enabled: true
  ca:
    common_name: NebulaIO Internal CA
  tls:
    min_version: "1.2"
    require_client_cert: true
```

### Kubernetes Network Policies

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: nebulaio-policy
spec:
  podSelector:
    matchLabels:
      app: nebulaio
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              name: production
      ports:
        - port: 9000
```

---

## Audit Logging

Comprehensive audit trails with cryptographic integrity:

```yaml
audit:
  enabled: true
  compliance_mode: soc2  # soc2, pci, hipaa, gdpr, fedramp
  file_path: /var/log/nebulaio/audit.log
  integrity_enabled: true
  mask_sensitive_data: true
  webhook:
    url: https://siem.example.com/events
```

**Logged Events**: Object operations, authentication, IAM changes, bucket modifications.

See [Audit Logging](../features/audit-logging.md) for details.

---

## Data Protection

### Object Lock (WORM)

Write-Once-Read-Many protection for compliance:

```bash
aws s3api create-bucket \
  --endpoint-url http://localhost:9000 \
  --bucket compliance-bucket \
  --object-lock-enabled-for-bucket

aws s3api put-object-lock-configuration \
  --bucket compliance-bucket \
  --object-lock-configuration '{
    "ObjectLockEnabled": "Enabled",
    "Rule": {"DefaultRetention": {"Mode": "COMPLIANCE", "Days": 365}}
  }'
```

### Versioning

Protection against accidental deletion:

```bash
aws s3api put-bucket-versioning \
  --endpoint-url http://localhost:9000 \
  --bucket my-bucket \
  --versioning-configuration Status=Enabled
```

---

## Security Best Practices

### Authentication
- Rotate access keys every 90 days
- Use LDAP/OIDC for enterprise environments
- Enforce minimum 32-character secret keys

### Authorization
- Follow least privilege principle
- Regularly audit permissions quarterly
- Use bucket policies for public access controls

### Encryption
- Enable TLS for all endpoints
- Enable default bucket encryption (SSE-S3)
- Use KMS for sensitive/regulated data

### Network
- Restrict access with IP allowlisting
- Enable rate limiting to prevent abuse
- Use mTLS for cluster communication

### Monitoring
- Enable audit logging with integrity
- Configure SIEM integration
- Set up anomaly detection alerts

---

## Compliance Support

| Framework | Key Features |
|-----------|--------------|
| SOC 2 | Audit logging, access controls, encryption |
| PCI DSS | Encryption, key management, audit trails |
| HIPAA | Access controls, audit logging, data masking |
| GDPR | Data protection, access logs, encryption |
| FedRAMP | All features, cryptographic integrity |

---

## Related Documentation

- [Security Features](../features/security-features.md)
- [Audit Logging](../features/audit-logging.md)
- [Data Firewall](../features/data-firewall.md)
