# First Steps with NebulaIO

This guide walks you through fundamental operations in NebulaIO, from understanding core concepts to performing common storage tasks.

## Prerequisites

- NebulaIO running (see [Quick Start Guide](quick-start.md))
- AWS CLI v2 installed, or MinIO Client (mc)
- Access credentials (default: `admin` / `password123`)

## Understanding NebulaIO Concepts

### Buckets

Buckets are top-level containers for storing objects. Each bucket has a unique name, can contain unlimited objects, and supports versioning, lifecycle policies, and access controls.

### Objects

Objects are fundamental storage units consisting of:

- **Key**: Unique identifier (path) within a bucket
- **Data**: File content (up to 5 GB per object)
- **Metadata**: System and user-defined key-value pairs (set at upload, immutable)
- **Tags**: Mutable key-value labels for organization and lifecycle rules (max 10 per object)

## Configure Your Client

```bash
# AWS CLI: Configure credentials and create alias
aws configure set aws_access_key_id admin
aws configure set aws_secret_access_key password123
alias nebulaio='aws --endpoint-url http://localhost:9000'

# MinIO Client: Add NebulaIO as alias
mc alias set nebulaio http://localhost:9000 admin password123
```

## Creating Your First Bucket

```bash
# AWS CLI
nebulaio s3 mb s3://my-first-bucket
nebulaio s3 ls  # Verify creation

# MinIO Client
mc mb nebulaio/my-first-bucket
mc ls nebulaio
```

**Bucket naming rules**: 3-63 chars, lowercase letters/numbers/hyphens, must start with letter or number.

## Uploading Objects

### Single File

```bash
# AWS CLI
nebulaio s3 cp document.pdf s3://my-first-bucket/documents/document.pdf
nebulaio s3 cp document.pdf s3://my-first-bucket/doc.pdf --metadata "author=john"

# MinIO Client
mc cp document.pdf nebulaio/my-first-bucket/documents/
```

### Multiple Files

```bash
# AWS CLI: Upload directory recursively
nebulaio s3 cp ./local-folder s3://my-first-bucket/backup/ --recursive
nebulaio s3 cp ./images s3://my-first-bucket/images/ --recursive --exclude "*" --include "*.jpg"

# MinIO Client
mc cp --recursive ./local-folder nebulaio/my-first-bucket/backup/
```

## Downloading Objects

```bash
# AWS CLI: Single file and directory
nebulaio s3 cp s3://my-first-bucket/documents/document.pdf ./downloads/
nebulaio s3 cp s3://my-first-bucket/backup/ ./restored/ --recursive

# MinIO Client
mc cp nebulaio/my-first-bucket/documents/document.pdf ./downloads/
mc cp --recursive nebulaio/my-first-bucket/backup/ ./restored/
```

## Listing Buckets and Objects

```bash
# AWS CLI
nebulaio s3 ls                                    # List buckets
nebulaio s3 ls s3://my-first-bucket/              # List objects
nebulaio s3 ls s3://my-first-bucket/ --recursive  # Recursive listing

# MinIO Client
mc ls nebulaio                           # List buckets
mc ls nebulaio/my-first-bucket/          # List objects
mc ls --recursive nebulaio/my-first-bucket/
```

## Setting Object Metadata and Tags

```bash
# AWS CLI: View metadata
nebulaio s3api head-object --bucket my-first-bucket --key documents/document.pdf

# AWS CLI: Set and get tags
nebulaio s3api put-object-tagging --bucket my-first-bucket --key documents/document.pdf \
  --tagging 'TagSet=[{Key=project,Value=alpha},{Key=status,Value=active}]'
nebulaio s3api get-object-tagging --bucket my-first-bucket --key documents/document.pdf

# MinIO Client
mc stat nebulaio/my-first-bucket/documents/document.pdf
mc tag set nebulaio/my-first-bucket/documents/document.pdf "project=alpha&status=active"
mc tag list nebulaio/my-first-bucket/documents/document.pdf
```

## Deleting Objects and Buckets

```bash
# AWS CLI: Delete objects
nebulaio s3 rm s3://my-first-bucket/documents/document.pdf
nebulaio s3 rm s3://my-first-bucket/logs/ --recursive

# AWS CLI: Delete bucket
nebulaio s3 rb s3://my-first-bucket              # Empty bucket
nebulaio s3 rb s3://my-first-bucket --force      # Force delete all contents

# MinIO Client
mc rm nebulaio/my-first-bucket/documents/document.pdf
mc rm --recursive nebulaio/my-first-bucket/logs/
mc rb nebulaio/my-first-bucket
mc rb --force nebulaio/my-first-bucket
```

## Using Presigned URLs

Presigned URLs provide temporary access to objects without requiring credentials.

```bash
# Generate download URL (default 1 hour expiration)
nebulaio s3 presign s3://my-first-bucket/documents/document.pdf
nebulaio s3 presign s3://my-first-bucket/documents/document.pdf --expires-in 3600

# Use the presigned URL
curl -o document.pdf "https://localhost:9000/my-first-bucket/documents/document.pdf?X-Amz-..."
curl -X PUT -T newfile.pdf "https://localhost:9000/my-first-bucket/uploads/newfile.pdf?X-Amz-..."
```

## Basic Bucket Policies

Bucket policies control access using JSON policy documents. Create `public-read-policy.json`:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Sid": "PublicReadGetObject",
    "Effect": "Allow",
    "Principal": "*",
    "Action": "s3:GetObject",
    "Resource": "arn:aws:s3:::my-first-bucket/*"
  }]
}
```

Apply and manage policies:

```bash
# Set policy
nebulaio s3api put-bucket-policy --bucket my-first-bucket --policy file://public-read-policy.json

# View policy
nebulaio s3api get-bucket-policy --bucket my-first-bucket

# Remove policy
nebulaio s3api delete-bucket-policy --bucket my-first-bucket
```

### Restrict Access to Specific Prefix

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Sid": "AllowPublicAccessToPublicFolder",
    "Effect": "Allow",
    "Principal": "*",
    "Action": "s3:GetObject",
    "Resource": "arn:aws:s3:::my-first-bucket/public/*"
  }]
}
```

## Next Steps

- [CLI Tool](cli.md) - Full command reference for nebulaio-cli
- [Bucket Versioning](../features/versioning.md) - Enable version history
- [Lifecycle Policies](../features/lifecycle.md) - Automate object expiration
- [Event Notifications](../features/events.md) - Trigger webhooks on changes
- [Object Lock](../features/object-lock.md) - WORM compliance
