# Object Lock (WORM)

NebulaIO supports S3-compatible Object Lock for Write-Once-Read-Many (WORM) protection, enabling immutable storage for regulatory compliance and data protection.

## Overview

Object Lock prevents objects from being deleted or overwritten for a specified retention period:

- **Regulatory Compliance**: SEC Rule 17a-4, FINRA, CFTC, MiFID II
- **Legal Hold**: Preserve data for litigation or investigation
- **Ransomware Protection**: Prevent malicious deletion or encryption

| Concept | Description |
|---------|-------------|
| Retention Period | Duration during which an object cannot be deleted |
| Retention Mode | Governance or Compliance - determines bypass rules |
| Legal Hold | Indefinite hold independent of retention |
| Default Retention | Bucket-level default for new objects |

## Retention Modes

### Governance Mode

Protects objects but allows bypass with `s3:BypassGovernanceRetention` permission:

```bash
# Apply Governance mode retention
aws s3api put-object-retention --endpoint-url http://localhost:9000 \
  --bucket compliance-bucket --key financial-report.pdf \
  --retention '{"Mode":"GOVERNANCE","RetainUntilDate":"2027-12-31T00:00:00Z"}'

# Bypass Governance mode (requires permission)
aws s3api delete-object --endpoint-url http://localhost:9000 \
  --bucket compliance-bucket --key financial-report.pdf \
  --version-id abc123 --bypass-governance-retention
```

### Compliance Mode

Strongest protection - no user can bypass until retention expires:

```bash
aws s3api put-object-retention --endpoint-url http://localhost:9000 \
  --bucket sec-records --key trade-confirmation.pdf \
  --retention '{"Mode":"COMPLIANCE","RetainUntilDate":"2032-12-31T00:00:00Z"}'
```

| Feature | Governance | Compliance |
|---------|------------|------------|
| Delete before expiry | With permission | Never |
| Shorten retention | With permission | Never |
| Extend retention | Yes | Yes |
| SEC 17a-4 compliant | No | Yes |

## Legal Holds

Legal holds provide indefinite protection independent of retention periods:

```bash
# Place legal hold
aws s3api put-object-legal-hold --endpoint-url http://localhost:9000 \
  --bucket evidence-bucket --key contract-2024.pdf \
  --legal-hold '{"Status":"ON"}'

# Check status
aws s3api get-object-legal-hold --endpoint-url http://localhost:9000 \
  --bucket evidence-bucket --key contract-2024.pdf

# Remove legal hold
aws s3api put-object-legal-hold --endpoint-url http://localhost:9000 \
  --bucket evidence-bucket --key contract-2024.pdf \
  --legal-hold '{"Status":"OFF"}'
```

## Default Bucket Retention

Object Lock must be enabled when creating the bucket:

```bash
# Create bucket with Object Lock enabled
aws s3api create-bucket --endpoint-url http://localhost:9000 \
  --bucket compliance-bucket --object-lock-enabled-for-bucket

# Set default retention for new objects
aws s3api put-object-lock-configuration --endpoint-url http://localhost:9000 \
  --bucket compliance-bucket --object-lock-configuration '{
    "ObjectLockEnabled": "Enabled",
    "Rule": {"DefaultRetention": {"Mode": "COMPLIANCE", "Years": 7}}
  }'
```

## Configuring Object Lock

### Server Configuration

```yaml
object_lock:
  enabled: true
  defaults:
    mode: GOVERNANCE
    days: 30
  compliance:
    require_versioning: true
    audit_logging: true
    prevent_bucket_deletion: true
```

### IAM Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "s3:GetObjectRetention", "s3:PutObjectRetention",
      "s3:GetObjectLegalHold", "s3:PutObjectLegalHold",
      "s3:GetBucketObjectLockConfiguration", "s3:PutBucketObjectLockConfiguration"
    ],
    "Resource": ["arn:aws:s3:::compliance-bucket/*"]
  }]
}
```

## Compliance Considerations

### Regulatory Requirements

| Regulation | Retention Mode | Duration |
|------------|----------------|----------|
| SEC 17a-4 | Compliance | 6-7 years |
| FINRA 4511 | Compliance | 6 years |
| HIPAA | Compliance | 6 years |
| SOX | Compliance | 7 years |
| MiFID II | Compliance | 5-7 years |

### Best Practices

1. **Test with Governance Mode First**: Before deploying Compliance mode
2. **Document Retention Policies**: Maintain clear documentation per data category
3. **Separate Compliance Buckets**: Use dedicated buckets for compliance data
4. **Regular Audits**: Review Object Lock configurations and access logs
5. **Immutable Audit Logs**: Store audit logs in a separate locked bucket

## AWS CLI Examples

### Complete Workflow

```bash
# 1. Create Object Lock enabled bucket
aws s3api create-bucket --endpoint-url http://localhost:9000 \
  --bucket financial-records --object-lock-enabled-for-bucket

# 2. Configure default 7-year Compliance retention
aws s3api put-object-lock-configuration --endpoint-url http://localhost:9000 \
  --bucket financial-records --object-lock-configuration '{
    "ObjectLockEnabled": "Enabled",
    "Rule": {"DefaultRetention": {"Mode": "COMPLIANCE", "Years": 7}}
  }'

# 3. Upload object (gets default retention)
aws s3 cp quarterly-report.pdf s3://financial-records/ --endpoint-url http://localhost:9000

# 4. Verify retention
aws s3api get-object-retention --endpoint-url http://localhost:9000 \
  --bucket financial-records --key quarterly-report.pdf

# 5. Apply legal hold
aws s3api put-object-legal-hold --endpoint-url http://localhost:9000 \
  --bucket financial-records --key quarterly-report.pdf \
  --legal-hold '{"Status":"ON"}'

# 6. Extend retention period
aws s3api put-object-retention --endpoint-url http://localhost:9000 \
  --bucket financial-records --key quarterly-report.pdf \
  --retention '{"Mode":"COMPLIANCE","RetainUntilDate":"2035-12-31T00:00:00Z"}'
```

## Next Steps

- [Configure Audit Logging](audit-logging.md) for compliance reporting
- [Set up Replication](replication.md) for disaster recovery of locked objects
- [Review Security Features](security-features.md) for comprehensive protection
