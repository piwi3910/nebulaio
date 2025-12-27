# Key Management System (KMS) Integration

NebulaIO provides enterprise-grade Key Management System integration for secure encryption key management. This enables centralized key storage, automated key rotation, and compliance with security standards.

## Overview

The KMS integration supports envelope encryption, where data encryption keys (DEKs) are encrypted by key encryption keys (KEKs) managed by external KMS providers. This architecture provides:

- **Centralized Key Management**: Store and manage keys in dedicated security infrastructure
- **Separation of Duties**: Storage administrators cannot access encryption keys
- **Audit Trails**: All key operations logged by the KMS provider
- **Compliance**: Meet regulatory requirements for key management (PCI DSS, HIPAA, SOC 2)

```
+------------------+     +------------------+     +------------------+
|    NebulaIO      |     |   KMS Provider   |     |   Key Storage    |
|    Storage       |<--->|   (Vault/AWS/    |<--->|   (HSM/Secure    |
|    Server        |     |    Azure/GCP)    |     |    Backend)      |
+------------------+     +------------------+     +------------------+
        |                        |
        v                        v
   Data encrypted           Master keys
   with DEKs                stored securely
```

## Supported Providers

| Provider | Description | Status |
|----------|-------------|--------|
| [HashiCorp Vault](#hashicorp-vault) | Transit secrets engine for encryption operations | Production |
| [AWS KMS](#aws-kms) | Amazon Key Management Service | Production |
| [Azure Key Vault](#azure-key-vault) | Microsoft Azure key management | Production |
| [GCP Cloud KMS](#gcp-cloud-kms) | Google Cloud Key Management Service | Production |
| [Local](#local-provider) | File-based keys for development/testing | Development |

---

## HashiCorp Vault

HashiCorp Vault Transit secrets engine provides encryption-as-a-service without exposing keys.

### Configuration

```yaml
# config.yaml
encryption:
  enabled: true
  kms:
    provider: vault
    vault:
      address: https://vault.example.com:8200
      token: ${VAULT_TOKEN}
      namespace: admin                    # Enterprise only
      mount_path: transit
      key_name: nebulaio-master

      # TLS settings
      tls_ca_cert: /etc/nebulaio/vault-ca.crt
      tls_client_cert: /etc/nebulaio/vault-client.crt
      tls_client_key: /etc/nebulaio/vault-client.key
      tls_skip_verify: false              # Never in production

      # Connection settings
      timeout: 30s
      max_retries: 3
      key_cache_ttl: 5m
```

### Vault Setup

```bash
# Enable transit secrets engine
vault secrets enable transit

# Create encryption key for NebulaIO
vault write -f transit/keys/nebulaio-master \
  type=aes256-gcm96 \
  auto_rotate_period=90d

# Create policy for NebulaIO
vault policy write nebulaio-kms - <<EOF
path "transit/encrypt/nebulaio-master" {
  capabilities = ["update"]
}
path "transit/decrypt/nebulaio-master" {
  capabilities = ["update"]
}
path "transit/datakey/plaintext/nebulaio-master" {
  capabilities = ["update"]
}
path "transit/keys/nebulaio-master" {
  capabilities = ["read"]
}
path "transit/keys/nebulaio-master/rotate" {
  capabilities = ["update"]
}
EOF

# Create AppRole for NebulaIO
vault auth enable approle
vault write auth/approle/role/nebulaio \
  token_policies="nebulaio-kms" \
  token_ttl=1h \
  token_max_ttl=4h
```

---

## AWS KMS

Amazon Web Services Key Management Service for cloud-native deployments.

### Configuration

```yaml
# config.yaml
encryption:
  enabled: true
  kms:
    provider: aws
    aws:
      region: us-east-1
      key_id: arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012

      # Authentication (choose one)
      # Option 1: IAM role (recommended for EC2/EKS)
      use_iam_role: true

      # Option 2: Access keys
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_SECRET_ACCESS_KEY}

      # Option 3: Assume role
      role_arn: arn:aws:iam::123456789012:role/NebulaIOKMSRole
      external_id: nebulaio-external-id

      # Connection settings
      endpoint: ""                        # Custom endpoint (LocalStack)
      key_cache_ttl: 5m
```

### IAM Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "kms:Encrypt",
        "kms:Decrypt",
        "kms:GenerateDataKey",
        "kms:GenerateDataKeyWithoutPlaintext",
        "kms:DescribeKey"
      ],
      "Resource": "arn:aws:kms:us-east-1:123456789012:key/12345678-*"
    }
  ]
}
```

---

## Azure Key Vault

Microsoft Azure Key Vault for Azure-native deployments.

### Configuration

```yaml
# config.yaml
encryption:
  enabled: true
  kms:
    provider: azure
    azure:
      vault_url: https://nebulaio-kv.vault.azure.net/
      key_name: nebulaio-master
      key_version: ""                     # Empty for latest

      # Authentication (choose one)
      # Option 1: Managed Identity (recommended)
      use_managed_identity: true

      # Option 2: Service Principal
      tenant_id: ${AZURE_TENANT_ID}
      client_id: ${AZURE_CLIENT_ID}
      client_secret: ${AZURE_CLIENT_SECRET}

      # Connection settings
      key_cache_ttl: 5m
```

### Azure RBAC

Assign the "Key Vault Crypto User" role to your application:

```bash
az role assignment create \
  --role "Key Vault Crypto User" \
  --assignee <app-object-id> \
  --scope /subscriptions/<sub-id>/resourceGroups/<rg>/providers/Microsoft.KeyVault/vaults/<vault-name>
```

---

## GCP Cloud KMS

Google Cloud Key Management Service for GCP-native deployments.

### Configuration

```yaml
# config.yaml
encryption:
  enabled: true
  kms:
    provider: gcp
    gcp:
      project_id: my-project
      location: us-central1
      key_ring: nebulaio-keyring
      key_name: nebulaio-master
      key_version: 1                      # 0 for latest

      # Authentication
      # Option 1: Default credentials (GCE/GKE)
      use_default_credentials: true

      # Option 2: Service account key file
      credentials_file: /etc/nebulaio/gcp-sa.json

      # Connection settings
      key_cache_ttl: 5m
```

### IAM Permissions

```bash
gcloud kms keys add-iam-policy-binding nebulaio-master \
  --keyring=nebulaio-keyring \
  --location=us-central1 \
  --member="serviceAccount:nebulaio@my-project.iam.gserviceaccount.com" \
  --role="roles/cloudkms.cryptoKeyEncrypterDecrypter"
```

---

## Key Rotation

NebulaIO supports automatic and manual key rotation with zero-downtime re-encryption.

### Rotation Policies

```yaml
# config.yaml
encryption:
  key_rotation:
    enabled: true
    check_interval: 1h

    policies:
      - key_type: MASTER
        rotation_interval: 8760h          # 1 year
        max_key_age: 9600h
        grace_period: 720h                # 30 days
        notify_before_expiry: 720h
        retain_versions: 3
        require_reencryption: true

      - key_type: BUCKET
        rotation_interval: 4320h          # 180 days
        max_key_age: 5040h
        grace_period: 336h                # 14 days
        retain_versions: 3
        require_reencryption: false
```

### Manual Rotation

```bash
# Rotate a specific key
curl -X POST "http://localhost:9001/api/v1/keys/key-123/rotate" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"trigger": "manual", "initiated_by": "admin"}'

# Start re-encryption job after rotation
curl -X POST "http://localhost:9001/api/v1/keys/key-123/reencrypt" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "old_version": 1,
    "new_version": 2,
    "buckets": ["my-bucket"],
    "concurrency": 10
  }'
```

---

## Envelope Encryption

NebulaIO uses envelope encryption for efficient and secure data protection.

### How It Works

1. **Data Encryption Key (DEK)**: Unique key generated for each object
2. **Key Encryption Key (KEK)**: Master key in KMS that encrypts DEKs
3. **Encrypted DEK**: Stored alongside the encrypted object

```
Object Upload:
1. Generate random DEK (AES-256)
2. Encrypt object data with DEK
3. Send DEK to KMS for encryption with KEK
4. Store encrypted DEK + encrypted data

Object Download:
1. Retrieve encrypted DEK from object metadata
2. Send encrypted DEK to KMS for decryption
3. Decrypt object data with plaintext DEK
4. Return plaintext object to client
```

### Configuration

```yaml
encryption:
  envelope:
    enabled: true
    dek_algorithm: AES_256_GCM
    dek_cache_ttl: 5m
    dek_cache_max_size: 10000
```

---

## Per-Bucket Encryption Keys

Configure different encryption keys for different buckets to support data isolation and compliance requirements.

### Configuration

```yaml
# config.yaml
encryption:
  enabled: true
  default_key_id: default-master-key

  bucket_keys:
    - bucket: pii-data
      key_id: vault:transit/keys/pii-key
      algorithm: AES_256_GCM

    - bucket: financial-records
      key_id: aws:arn:aws:kms:us-east-1:123456789012:key/finance-key
      algorithm: AES_256_GCM

    - bucket: healthcare-data
      key_id: azure:https://hipaa-kv.vault.azure.net/keys/hipaa-key
      algorithm: AES_256_GCM
```

### API Configuration

```bash
# Set bucket encryption key
curl -X PUT "http://localhost:9001/api/v1/buckets/pii-data/encryption" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "key_id": "vault:transit/keys/pii-key",
    "algorithm": "AES_256_GCM",
    "enabled": true
  }'

# Get bucket encryption configuration
curl -X GET "http://localhost:9001/api/v1/buckets/pii-data/encryption" \
  -H "Authorization: Bearer $TOKEN"
```

---

## Troubleshooting

### Common Issues

#### Connection Failed

```
Error: failed to connect to KMS provider: connection refused
```

**Solutions:**
- Verify KMS provider address is correct and reachable
- Check network connectivity and firewall rules
- Verify TLS certificates are valid and trusted

#### Authentication Failed

```
Error: KMS authentication failed: invalid credentials
```

**Solutions:**
- Verify credentials (token, access key, service principal)
- Check credential expiration
- Verify IAM policies/RBAC assignments

#### Key Not Found

```
Error: key not found: nebulaio-master
```

**Solutions:**
- Verify key exists in KMS provider
- Check key name matches configuration
- Verify application has read permissions on key

#### Decryption Failed

```
Error: decryption failed: key version not available
```

**Solutions:**
- Ensure old key versions are retained during rotation
- Check `min_decryption_version` setting in Vault
- Verify key has not been deleted

### Diagnostic Commands

```bash
# Check KMS provider status
nebulaio-cli admin kms status

# List available keys
nebulaio-cli admin kms keys list

# Test encryption/decryption
nebulaio-cli admin kms test --key-id nebulaio-master

# View key rotation history
nebulaio-cli admin kms keys history nebulaio-master

# Check encryption metrics
curl -s http://localhost:9001/metrics | grep nebulaio_kms_
```

### Health Checks

```yaml
# Kubernetes liveness probe
livenessProbe:
  httpGet:
    path: /health/kms
    port: 9001
  initialDelaySeconds: 10
  periodSeconds: 30
```

### Logging

Enable debug logging for KMS operations:

```yaml
logging:
  level: debug
  components:
    kms: debug
    encryption: debug
```

---

## Related Documentation

- [Security Architecture](../architecture/security.md)
- [Security Features](security-features.md)
- [Audit Logging](audit-logging.md)
- [Configuration Reference](../getting-started/configuration.md)
