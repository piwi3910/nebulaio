# NebulaIO CLI Tool

The `nebulaio-cli` is a command-line tool for managing NebulaIO storage. It provides a familiar interface similar to `aws s3` and `mc` (MinIO Client).

## Installation

### From Source

```bash
# Build the CLI
cd nebulaio
make build-cli

# The binary will be at ./bin/nebulaio-cli
```

### Add to PATH

```bash
# Linux/macOS
sudo cp ./bin/nebulaio-cli /usr/local/bin/

# Or add to PATH
export PATH=$PATH:/path/to/nebulaio/bin
```

## Configuration

### Initial Setup

```bash
# Set endpoint
nebulaio-cli config set endpoint http://localhost:9000

# Set credentials
nebulaio-cli config set access-key YOUR_ACCESS_KEY
nebulaio-cli config set secret-key YOUR_SECRET_KEY

# Optional: Set region
nebulaio-cli config set region us-east-1
```

### View Configuration

```bash
# Show all settings
nebulaio-cli config show

# Get specific value
nebulaio-cli config get endpoint
```

### Configuration File

Configuration is stored in `~/.nebulaio/config.yaml`:

```yaml
endpoint: http://localhost:9000
access_key: YOUR_ACCESS_KEY
secret_key: YOUR_SECRET_KEY
region: us-east-1
use_ssl: false
skip_verify: false
```

### Environment Variables

You can also use environment variables (they override config file):

```bash
export NEBULAIO_ENDPOINT=http://localhost:9000
export NEBULAIO_ACCESS_KEY=YOUR_ACCESS_KEY
export NEBULAIO_SECRET_KEY=YOUR_SECRET_KEY
export NEBULAIO_REGION=us-east-1

# AWS SDK compatible variables also work:
export AWS_ACCESS_KEY_ID=YOUR_ACCESS_KEY
export AWS_SECRET_ACCESS_KEY=YOUR_SECRET_KEY
export AWS_REGION=us-east-1
```

## Commands

### Bucket Operations

```bash
# List all buckets
nebulaio-cli bucket list
nebulaio-cli bucket ls

# Create a bucket
nebulaio-cli bucket create my-bucket
nebulaio-cli mb s3://my-bucket                    # Short form
nebulaio-cli bucket create my-bucket --object-lock # With Object Lock enabled

# Delete a bucket
nebulaio-cli bucket delete my-bucket
nebulaio-cli rb s3://my-bucket                    # Short form
nebulaio-cli bucket delete my-bucket --force       # Delete all objects first

# Get bucket info
nebulaio-cli bucket info my-bucket
```

### Object Operations

```bash
# List objects
nebulaio-cli object list s3://my-bucket
nebulaio-cli ls s3://my-bucket                    # Short form
nebulaio-cli ls s3://my-bucket/prefix/            # With prefix
nebulaio-cli object list s3://my-bucket -r        # Recursive

# Upload files
nebulaio-cli object put file.txt s3://my-bucket/file.txt
nebulaio-cli cp file.txt s3://my-bucket/          # Short form
nebulaio-cli cp -r ./folder s3://my-bucket/folder # Recursive upload

# Download files
nebulaio-cli object get s3://my-bucket/file.txt ./local.txt
nebulaio-cli cp s3://my-bucket/file.txt ./        # Short form
nebulaio-cli cp -r s3://my-bucket/folder ./local  # Recursive download

# Delete objects
nebulaio-cli object delete s3://my-bucket/file.txt
nebulaio-cli rm s3://my-bucket/file.txt           # Short form
nebulaio-cli rm -r s3://my-bucket/prefix/         # Recursive delete

# View object contents
nebulaio-cli cat s3://my-bucket/file.txt

# Get object metadata
nebulaio-cli object head s3://my-bucket/file.txt
```

### Admin Operations

#### Versioning

```bash
# Enable versioning
nebulaio-cli admin versioning enable my-bucket

# Disable (suspend) versioning
nebulaio-cli admin versioning disable my-bucket

# Check status
nebulaio-cli admin versioning status my-bucket
```

#### Bucket Policies

```bash
# Set policy from file
nebulaio-cli admin policy set my-bucket policy.json

# Get current policy
nebulaio-cli admin policy get my-bucket

# Delete policy
nebulaio-cli admin policy delete my-bucket
```

Example policy file (`policy.json`):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": "*",
      "Action": ["s3:GetObject"],
      "Resource": ["arn:aws:s3:::my-bucket/*"]
    }
  ]
}
```

#### Lifecycle Rules

```bash
# Set lifecycle configuration
nebulaio-cli admin lifecycle set my-bucket lifecycle.json

# Get lifecycle configuration
nebulaio-cli admin lifecycle get my-bucket

# List lifecycle rules
nebulaio-cli admin lifecycle list my-bucket

# Delete lifecycle configuration
nebulaio-cli admin lifecycle delete my-bucket
```

Example lifecycle configuration (`lifecycle.json`):

```json
{
  "Rules": [
    {
      "ID": "expire-old-objects",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "logs/"
      },
      "Expiration": {
        "Days": 30
      }
    },
    {
      "ID": "transition-to-glacier",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "archive/"
      },
      "Transitions": [
        {
          "Days": 90,
          "StorageClass": "GLACIER"
        }
      ]
    }
  ]
}
```

#### Replication

```bash
# Set replication configuration
nebulaio-cli admin replication set my-bucket replication.json

# Get replication configuration
nebulaio-cli admin replication get my-bucket

# Delete replication configuration
nebulaio-cli admin replication delete my-bucket
```

Example replication configuration (`replication.json`):

```json
{
  "Role": "arn:aws:iam::account-id:role/replication-role",
  "Rules": [
    {
      "ID": "replicate-to-dr",
      "Status": "Enabled",
      "Priority": 1,
      "DeleteMarkerReplication": {
        "Status": "Enabled"
      },
      "Filter": {},
      "Destination": {
        "Bucket": "arn:aws:s3:::destination-bucket"
      }
    }
  ]
}
```

## Examples

### Backup Script

```bash
#!/bin/bash
# Backup local directory to NebulaIO

SOURCE_DIR="/data/important"
BUCKET="s3://backups"
DATE=$(date +%Y-%m-%d)

nebulaio-cli cp -r "$SOURCE_DIR" "$BUCKET/$DATE/"
echo "Backup completed: $BUCKET/$DATE/"
```

### Sync Script

```bash
#!/bin/bash
# Sync local directory with bucket

nebulaio-cli cp -r ./local-folder s3://my-bucket/folder/
```

### List Large Objects

```bash
# List objects and filter by size (using external tools)
nebulaio-cli ls s3://my-bucket -r | awk '$1 > 1000000000'
```

## Troubleshooting

### Connection Issues

```bash
# Verify endpoint is reachable
curl -v http://localhost:9000

# Check configuration
nebulaio-cli config show
```

### Authentication Errors

```bash
# Verify credentials
nebulaio-cli bucket list

# If using environment variables, check they're set
env | grep NEBULAIO
env | grep AWS
```

### SSL/TLS Issues

```bash
# Skip certificate verification (development only)
nebulaio-cli config set skip-verify true
```
