# Bucket Versioning

NebulaIO supports S3-compatible bucket versioning to maintain multiple versions of objects, enabling recovery from accidental deletions and overwrites.

## Overview

Bucket versioning provides:

- **Version History**: Keep all versions of every object
- **Accidental Deletion Protection**: Deleted objects become delete markers, not permanently removed
- **Point-in-Time Recovery**: Restore previous versions at any time
- **Audit Trail**: Track all changes to objects over time

## Versioning States

Buckets can be in one of three versioning states:

| State | Description |
|-------|-------------|
| Unversioned | Default state, no version history |
| Enabled | All objects get unique version IDs |
| Suspended | New objects get null version ID, existing versions preserved |

## Configuration

### Enable Versioning

```bash
# Using AWS CLI
aws s3api put-bucket-versioning \
  --bucket my-bucket \
  --versioning-configuration Status=Enabled \
  --endpoint-url http://localhost:9000

# Using nebulaio-cli
nebulaio-cli bucket versioning enable my-bucket
```

### Suspend Versioning

```bash
# Using AWS CLI
aws s3api put-bucket-versioning \
  --bucket my-bucket \
  --versioning-configuration Status=Suspended \
  --endpoint-url http://localhost:9000

# Using nebulaio-cli
nebulaio-cli bucket versioning suspend my-bucket
```

### Check Versioning Status

```bash
# Using AWS CLI
aws s3api get-bucket-versioning \
  --bucket my-bucket \
  --endpoint-url http://localhost:9000

# Using nebulaio-cli
nebulaio-cli bucket versioning status my-bucket
```

## Working with Versions

### Upload Object (Creates New Version)

```bash
# Each upload creates a new version
aws s3 cp file.txt s3://my-bucket/file.txt \
  --endpoint-url http://localhost:9000

# Response includes VersionId
# VersionId: abc123def456
```

### List Object Versions

```bash
# List all versions
aws s3api list-object-versions \
  --bucket my-bucket \
  --endpoint-url http://localhost:9000

# List versions for specific prefix
aws s3api list-object-versions \
  --bucket my-bucket \
  --prefix documents/ \
  --endpoint-url http://localhost:9000
```

### Get Specific Version

```bash
# Get specific version of an object
aws s3api get-object \
  --bucket my-bucket \
  --key file.txt \
  --version-id abc123def456 \
  output.txt \
  --endpoint-url http://localhost:9000
```

### Delete Specific Version

```bash
# Permanently delete a specific version
aws s3api delete-object \
  --bucket my-bucket \
  --key file.txt \
  --version-id abc123def456 \
  --endpoint-url http://localhost:9000
```

### Restore Previous Version

```bash
# Copy a previous version to become the current version
aws s3api copy-object \
  --bucket my-bucket \
  --key file.txt \
  --copy-source my-bucket/file.txt?versionId=abc123def456 \
  --endpoint-url http://localhost:9000
```

## Delete Markers

When you delete an object in a versioned bucket:

1. NebulaIO inserts a **delete marker** as the current version
2. The object appears deleted but previous versions still exist
3. GET requests for the object return 404

### Remove Delete Marker

```bash
# List versions to find the delete marker
aws s3api list-object-versions \
  --bucket my-bucket \
  --prefix file.txt \
  --endpoint-url http://localhost:9000

# Delete the delete marker to restore the object
aws s3api delete-object \
  --bucket my-bucket \
  --key file.txt \
  --version-id DELETE_MARKER_VERSION_ID \
  --endpoint-url http://localhost:9000
```

## MFA Delete

For additional protection, enable MFA Delete to require multi-factor authentication for:

- Permanently deleting object versions
- Changing versioning state

### Enable MFA Delete

```yaml
# config.yaml
buckets:
  mfa_delete:
    enabled: true
    serial_number: arn:aws:iam::123456789:mfa/admin
```

```bash
# Enable via API (requires MFA)
aws s3api put-bucket-versioning \
  --bucket my-bucket \
  --versioning-configuration Status=Enabled,MFADelete=Enabled \
  --mfa "arn:aws:iam::123456789:mfa/admin 123456" \
  --endpoint-url http://localhost:9000
```

## Storage Considerations

### Version Storage

- Each version is stored as a complete object
- Versions consume storage quota
- Use lifecycle policies to manage old versions

### Lifecycle Integration

Combine versioning with lifecycle policies to:

- Automatically delete old versions after N days
- Transition old versions to cheaper storage tiers
- Keep only the last N versions

```yaml
# Example lifecycle rule
lifecycle:
  rules:
    - id: version-cleanup
      status: Enabled
      noncurrent_version_expiration:
        noncurrent_days: 90
      noncurrent_version_transitions:
        - noncurrent_days: 30
          storage_class: GLACIER
```

See [Lifecycle Policies](lifecycle.md) for more details.

## Best Practices

1. **Enable versioning early**: Enable before storing important data
2. **Use lifecycle policies**: Prevent unlimited version growth
3. **Consider MFA Delete**: For production buckets with critical data
4. **Monitor storage usage**: Versions accumulate quickly with frequent updates
5. **Test recovery procedures**: Regularly verify you can restore versions

## API Reference

### Supported Operations

| Operation | Description |
|-----------|-------------|
| PutBucketVersioning | Enable/suspend versioning |
| GetBucketVersioning | Get versioning status |
| ListObjectVersions | List all object versions |
| GetObject (with versionId) | Get specific version |
| DeleteObject (with versionId) | Delete specific version |
| CopyObject (with versionId) | Copy specific version |

### Response Headers

| Header | Description |
|--------|-------------|
| x-amz-version-id | Version ID of the object |
| x-amz-delete-marker | True if object is a delete marker |

## Related Documentation

- [Lifecycle Policies](lifecycle.md)
- [Object Lock](object-lock.md)
- [Replication](replication.md)
- [S3 Compatibility](../api/s3-compatibility.md)
