# Lifecycle Policies

NebulaIO supports S3-compatible lifecycle policies to automate object management, including expiration, transitions, and version cleanup.

## Overview

Lifecycle policies enable:

- **Automatic Expiration**: Delete objects after a specified period
- **Storage Tiering**: Transition objects to cheaper storage classes
- **Version Management**: Clean up old object versions
- **Cost Optimization**: Reduce storage costs automatically

## Policy Structure

A lifecycle configuration consists of rules that define actions based on conditions:

```xml
<LifecycleConfiguration>
  <Rule>
    <ID>rule-id</ID>
    <Status>Enabled</Status>
    <Filter>
      <Prefix>logs/</Prefix>
    </Filter>
    <Expiration>
      <Days>90</Days>
    </Expiration>
  </Rule>
</LifecycleConfiguration>
```

## Configuration

### Using AWS CLI

```bash
# Create lifecycle policy
aws s3api put-bucket-lifecycle-configuration \
  --bucket my-bucket \
  --lifecycle-configuration file://lifecycle.json \
  --endpoint-url http://localhost:9000

# Get current policy
aws s3api get-bucket-lifecycle-configuration \
  --bucket my-bucket \
  --endpoint-url http://localhost:9000

# Delete lifecycle policy
aws s3api delete-bucket-lifecycle \
  --bucket my-bucket \
  --endpoint-url http://localhost:9000
```

### Using nebulaio-cli

```bash
# Set lifecycle policy
nebulaio-cli bucket lifecycle set my-bucket --config lifecycle.json

# Get lifecycle policy
nebulaio-cli bucket lifecycle get my-bucket

# Remove lifecycle policy
nebulaio-cli bucket lifecycle remove my-bucket
```

## Rule Examples

### Basic Expiration

Delete objects older than 90 days:

```json
{
  "Rules": [
    {
      "ID": "expire-old-objects",
      "Status": "Enabled",
      "Filter": {},
      "Expiration": {
        "Days": 90
      }
    }
  ]
}
```

### Prefix-Based Expiration

Delete log files after 30 days:

```json
{
  "Rules": [
    {
      "ID": "expire-logs",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "logs/"
      },
      "Expiration": {
        "Days": 30
      }
    }
  ]
}
```

### Tag-Based Expiration

Delete objects with specific tags:

```json
{
  "Rules": [
    {
      "ID": "expire-temp-files",
      "Status": "Enabled",
      "Filter": {
        "Tag": {
          "Key": "type",
          "Value": "temporary"
        }
      },
      "Expiration": {
        "Days": 7
      }
    }
  ]
}
```

### Expire on Specific Date

Delete objects after a specific date:

```json
{
  "Rules": [
    {
      "ID": "expire-on-date",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "campaign-2024/"
      },
      "Expiration": {
        "Date": "2025-01-01T00:00:00.000Z"
      }
    }
  ]
}
```

### Storage Class Transition

Transition objects to cheaper storage:

```json
{
  "Rules": [
    {
      "ID": "archive-old-data",
      "Status": "Enabled",
      "Filter": {},
      "Transitions": [
        {
          "Days": 30,
          "StorageClass": "STANDARD_IA"
        },
        {
          "Days": 90,
          "StorageClass": "GLACIER"
        }
      ]
    }
  ]
}
```

### Version Expiration

Clean up old object versions:

```json
{
  "Rules": [
    {
      "ID": "version-cleanup",
      "Status": "Enabled",
      "Filter": {},
      "NoncurrentVersionExpiration": {
        "NoncurrentDays": 30
      },
      "NoncurrentVersionTransitions": [
        {
          "NoncurrentDays": 7,
          "StorageClass": "STANDARD_IA"
        }
      ]
    }
  ]
}
```

### Delete Markers Cleanup

Remove expired delete markers:

```json
{
  "Rules": [
    {
      "ID": "cleanup-delete-markers",
      "Status": "Enabled",
      "Filter": {},
      "Expiration": {
        "ExpiredObjectDeleteMarker": true
      }
    }
  ]
}
```

### Abort Incomplete Multipart Uploads

Clean up abandoned uploads:

```json
{
  "Rules": [
    {
      "ID": "abort-incomplete-uploads",
      "Status": "Enabled",
      "Filter": {},
      "AbortIncompleteMultipartUpload": {
        "DaysAfterInitiation": 7
      }
    }
  ]
}
```

### Combined Rules

Multiple rules in one configuration:

```json
{
  "Rules": [
    {
      "ID": "logs-lifecycle",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "logs/"
      },
      "Expiration": {
        "Days": 30
      }
    },
    {
      "ID": "archive-data",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "data/"
      },
      "Transitions": [
        {
          "Days": 90,
          "StorageClass": "GLACIER"
        }
      ],
      "Expiration": {
        "Days": 365
      }
    },
    {
      "ID": "version-management",
      "Status": "Enabled",
      "Filter": {},
      "NoncurrentVersionExpiration": {
        "NoncurrentDays": 60
      }
    }
  ]
}
```

## Filter Types

### Prefix Filter

Match objects by key prefix:

```json
{
  "Filter": {
    "Prefix": "documents/2024/"
  }
}
```

### Tag Filter

Match objects by tag:

```json
{
  "Filter": {
    "Tag": {
      "Key": "environment",
      "Value": "development"
    }
  }
}
```

### Combined Filters

Use AND logic for multiple conditions:

```json
{
  "Filter": {
    "And": {
      "Prefix": "logs/",
      "Tags": [
        {
          "Key": "environment",
          "Value": "production"
        },
        {
          "Key": "retention",
          "Value": "short"
        }
      ]
    }
  }
}
```

### Object Size Filter

Match objects by size (in bytes):

```json
{
  "Filter": {
    "And": {
      "Prefix": "uploads/",
      "ObjectSizeGreaterThan": 1048576,
      "ObjectSizeLessThan": 104857600
    }
  }
}
```

## Storage Classes

NebulaIO supports these storage classes for transitions:

| Storage Class | Description | Use Case |
|---------------|-------------|----------|
| STANDARD | Default, frequent access | Active data |
| STANDARD_IA | Infrequent access | Backups, older data |
| GLACIER | Archive storage | Long-term retention |
| DEEP_ARCHIVE | Deep archive | Compliance archives |

## Processing Behavior

- Lifecycle rules are evaluated daily at midnight UTC
- Actions are processed in order: transitions → expiration
- Minimum object age for transitions: 30 days for GLACIER
- Objects smaller than 128KB are not transitioned (remain in original class)

## Configuration via YAML

```yaml
# config.yaml
lifecycle:
  evaluation_interval: 24h  # How often to check rules
  batch_size: 1000          # Objects per batch
  workers: 4                # Parallel workers

  # Default rules applied to all buckets
  default_rules:
    - id: abort-uploads
      status: Enabled
      abort_incomplete_multipart_upload:
        days_after_initiation: 7
```

## Monitoring

### Lifecycle Metrics

```bash
# Get lifecycle processing stats
curl -X GET "http://localhost:9001/api/v1/admin/lifecycle/stats" \
  -H "Authorization: Bearer $TOKEN"
```

Metrics exposed:

- `nebulaio_lifecycle_objects_expired_total`
- `nebulaio_lifecycle_objects_transitioned_total`
- `nebulaio_lifecycle_processing_duration_seconds`
- `nebulaio_lifecycle_errors_total`

## Best Practices

1. **Start with longer retention**: Begin conservative, reduce over time
2. **Test with prefixes**: Apply to test prefixes before bucket-wide
3. **Use tags for flexibility**: Tag-based rules are easier to modify
4. **Monitor storage usage**: Track savings from lifecycle policies
5. **Document retention requirements**: Map policies to business needs
6. **Combine with versioning**: Use version expiration for versioned buckets

## Troubleshooting

### Objects Not Expiring

- Verify rule status is "Enabled"
- Check filter matches the objects
- Objects may have legal hold or retention period
- Lifecycle runs daily; wait for next evaluation

### Transitions Not Working

- Minimum 30-day wait for GLACIER transitions
- Objects < 128KB are not transitioned
- Verify storage class is available in your configuration

## Related Documentation

- [Bucket Versioning](versioning.md)
- [Object Lock](object-lock.md)
- [Storage Tiering](tiering.md)
- [S3 Compatibility](../api/s3-compatibility.md)
