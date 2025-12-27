# S3 API Compatibility

NebulaIO implements the Amazon S3 REST API, enabling seamless integration with existing S3 tools, SDKs, and applications. This document details supported operations, authentication, and SDK compatibility.

## Overview

NebulaIO provides near-complete S3 API compatibility, allowing you to use standard AWS tools and SDKs without modification. Simply point your S3 client to a NebulaIO endpoint and use your NebulaIO credentials.

### Key Compatibility Features

| Feature | Status | Notes |
|---------|--------|-------|
| AWS Signature V4 | Full | Required for all requests |
| AWS Signature V2 | Deprecated | Legacy support only |
| Virtual-hosted style | Full | `bucket.endpoint.com` |
| Path style | Full | `endpoint.com/bucket` |
| Presigned URLs | Full | GET, PUT, DELETE operations |
| Chunked uploads | Full | Streaming uploads supported |
| Server-side encryption | Full | SSE-S3, SSE-C, SSE-KMS |

## Bucket Operations

Operations for managing buckets (containers for objects).

| Operation | Support | Notes |
|-----------|---------|-------|
| `CreateBucket` | Full | Region constraints supported |
| `DeleteBucket` | Full | Bucket must be empty |
| `HeadBucket` | Full | Check bucket existence |
| `ListBuckets` | Full | Returns all accessible buckets |
| `GetBucketLocation` | Full | Returns configured region |
| `GetBucketVersioning` | Full | Versioning status |
| `PutBucketVersioning` | Full | Enable/suspend versioning |
| `GetBucketPolicy` | Full | JSON bucket policies |
| `PutBucketPolicy` | Full | IAM-style policies |
| `DeleteBucketPolicy` | Full | Remove bucket policy |
| `GetBucketPolicyStatus` | Full | Public access status |
| `GetBucketAcl` | Full | Access control list |
| `PutBucketAcl` | Full | Canned and custom ACLs |
| `GetBucketCors` | Full | CORS configuration |
| `PutBucketCors` | Full | Set CORS rules |
| `DeleteBucketCors` | Full | Remove CORS config |
| `GetBucketTagging` | Full | Bucket tags |
| `PutBucketTagging` | Full | Set bucket tags |
| `DeleteBucketTagging` | Full | Remove bucket tags |
| `GetBucketLifecycleConfiguration` | Full | Lifecycle rules |
| `PutBucketLifecycleConfiguration` | Full | Set lifecycle rules |
| `DeleteBucketLifecycle` | Full | Remove lifecycle config |
| `GetBucketReplication` | Full | Replication configuration |
| `PutBucketReplication` | Full | Set replication rules |
| `DeleteBucketReplication` | Full | Remove replication config |
| `GetBucketEncryption` | Full | Default encryption settings |
| `PutBucketEncryption` | Full | Set default encryption |
| `DeleteBucketEncryption` | Full | Remove default encryption |
| `GetBucketNotificationConfiguration` | Full | Event notifications |
| `PutBucketNotificationConfiguration` | Full | Set notifications |
| `GetBucketObjectLockConfiguration` | Full | Object Lock settings |
| `PutBucketObjectLockConfiguration` | Full | Enable Object Lock |

## Object Operations

Operations for managing objects (files) within buckets.

| Operation | Support | Notes |
|-----------|---------|-------|
| `PutObject` | Full | Up to 5GB single upload |
| `GetObject` | Full | Range requests supported |
| `HeadObject` | Full | Metadata without body |
| `DeleteObject` | Full | Single object deletion |
| `DeleteObjects` | Full | Bulk delete (up to 1000) |
| `CopyObject` | Full | Server-side copy |
| `ListObjects` | Full | Legacy list (max 1000) |
| `ListObjectsV2` | Full | Recommended list operation |
| `ListObjectVersions` | Full | Version history |
| `GetObjectAcl` | Full | Object ACL |
| `PutObjectAcl` | Full | Set object ACL |
| `GetObjectTagging` | Full | Object tags |
| `PutObjectTagging` | Full | Set object tags |
| `DeleteObjectTagging` | Full | Remove object tags |
| `GetObjectRetention` | Full | Retention settings |
| `PutObjectRetention` | Full | Set retention period |
| `GetObjectLegalHold` | Full | Legal hold status |
| `PutObjectLegalHold` | Full | Set legal hold |
| `RestoreObject` | Full | Restore from cold tier |
| `SelectObjectContent` | Full | S3 Select queries |
| `GetObjectAttributes` | Full | Object metadata summary |

## Multipart Upload Operations

Operations for uploading large objects in parts (recommended for objects > 100MB).

| Operation | Support | Notes |
|-----------|---------|-------|
| `CreateMultipartUpload` | Full | Initiate multipart upload |
| `UploadPart` | Full | Upload individual parts |
| `UploadPartCopy` | Full | Copy part from existing object |
| `CompleteMultipartUpload` | Full | Finalize the upload |
| `AbortMultipartUpload` | Full | Cancel and clean up parts |
| `ListMultipartUploads` | Full | List in-progress uploads |
| `ListParts` | Full | List uploaded parts |

### Multipart Upload Limits

| Parameter | Limit |
|-----------|-------|
| Minimum part size | 5 MB (except last part) |
| Maximum part size | 5 GB |
| Maximum parts per upload | 10,000 |
| Maximum object size | 5 TB |

## Versioning Operations

Operations for managing object versions.

| Operation | Support | Notes |
|-----------|---------|-------|
| `GetBucketVersioning` | Full | Check versioning status |
| `PutBucketVersioning` | Full | Enable/suspend versioning |
| `ListObjectVersions` | Full | List all versions |
| `GetObject?versionId` | Full | Retrieve specific version |
| `DeleteObject?versionId` | Full | Delete specific version |
| `CopyObject` (versioned) | Full | Copy specific version |

## ACL Operations

Access Control List operations for fine-grained permissions.

| Operation | Support | Notes |
|-----------|---------|-------|
| `GetBucketAcl` | Full | Bucket ACL |
| `PutBucketAcl` | Full | Set bucket ACL |
| `GetObjectAcl` | Full | Object ACL |
| `PutObjectAcl` | Full | Set object ACL |

### Canned ACLs

| ACL | Description |
|-----|-------------|
| `private` | Owner gets FULL_CONTROL (default) |
| `public-read` | Owner FULL_CONTROL, public READ |
| `public-read-write` | Owner FULL_CONTROL, public READ/WRITE |
| `authenticated-read` | Owner FULL_CONTROL, authenticated READ |
| `bucket-owner-read` | Object owner FULL_CONTROL, bucket owner READ |
| `bucket-owner-full-control` | Both owners FULL_CONTROL |

## Lifecycle Operations

Operations for automated object management.

| Operation | Support | Notes |
|-----------|---------|-------|
| `GetBucketLifecycleConfiguration` | Full | Get lifecycle rules |
| `PutBucketLifecycleConfiguration` | Full | Set lifecycle rules |
| `DeleteBucketLifecycle` | Full | Remove lifecycle config |

### Supported Lifecycle Actions

| Action | Support | Notes |
|--------|---------|-------|
| Expiration | Full | Delete objects after N days |
| NoncurrentVersionExpiration | Full | Delete old versions |
| Transition | Full | Move to different storage tier |
| NoncurrentVersionTransition | Full | Move old versions to tier |
| AbortIncompleteMultipartUpload | Full | Clean up incomplete uploads |
| ExpiredObjectDeleteMarker | Full | Remove delete markers |

## Replication Operations

Operations for cross-region and cross-cluster replication.

| Operation | Support | Notes |
|-----------|---------|-------|
| `GetBucketReplication` | Full | Get replication config |
| `PutBucketReplication` | Full | Set replication rules |
| `DeleteBucketReplication` | Full | Remove replication |

### Replication Features

| Feature | Support | Notes |
|---------|---------|-------|
| Cross-Region Replication (CRR) | Full | Replicate across regions |
| Same-Region Replication (SRR) | Full | Replicate within region |
| Bi-directional Replication | Full | Active-active sync |
| Replication Time Control | Full | SLA-backed replication |
| Delete Marker Replication | Full | Replicate deletions |
| Existing Object Replication | Full | Backfill existing objects |

## Unsupported/Partially Supported Operations

Some AWS-specific features are not applicable or have limited support.

### Not Supported

| Operation | Reason |
|-----------|--------|
| `GetBucketAccelerateConfiguration` | AWS-specific (CloudFront) |
| `PutBucketAccelerateConfiguration` | AWS-specific (CloudFront) |
| `GetBucketRequestPayment` | AWS billing feature |
| `PutBucketRequestPayment` | AWS billing feature |
| `GetBucketLogging` | Use NebulaIO audit logging instead |
| `PutBucketLogging` | Use NebulaIO audit logging instead |
| `GetBucketWebsite` | Use dedicated web server |
| `PutBucketWebsite` | Use dedicated web server |
| `DeleteBucketWebsite` | Not applicable |

### Partial Support

| Operation | Limitation |
|-----------|------------|
| `GetBucketAnalyticsConfiguration` | Basic metrics only |
| `GetBucketInventoryConfiguration` | Simplified inventory |
| `GetBucketMetricsConfiguration` | Prometheus metrics preferred |
| `PutBucketIntelligentTieringConfiguration` | Use NebulaIO tiering instead |

## Authentication

NebulaIO uses AWS Signature Version 4 for request authentication.

### AWS Signature V4

All API requests must be signed using AWS Signature V4. The signature is calculated using:

1. **Access Key ID**: Identifies the user/service account
2. **Secret Access Key**: Used to sign requests (never transmitted)
3. **Request elements**: Method, path, headers, payload hash
4. **Timestamp**: Requests expire after 15 minutes

### Signature Components

```
Authorization: AWS4-HMAC-SHA256
Credential=<access-key>/<date>/<region>/s3/aws4_request,
SignedHeaders=<signed-headers>,
Signature=<signature>
```

### Example Signed Request

```http
GET /my-bucket/my-object HTTP/1.1
Host: s3.nebulaio.local:9000
X-Amz-Date: 20240115T120000Z
X-Amz-Content-SHA256: e3b0c44298fc1c149afbf4c8996fb924...
Authorization: AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20240115/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=5d672d79c15b13162d9279b0855cfba...
```

### Presigned URLs

Generate time-limited URLs for temporary access:

```python
# Python (boto3)
url = s3.generate_presigned_url(
    'get_object',
    Params={'Bucket': 'my-bucket', 'Key': 'my-file.pdf'},
    ExpiresIn=3600  # 1 hour
)
```

## Error Responses

NebulaIO returns standard S3 error responses in XML format.

### Error Response Format

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Error>
    <Code>NoSuchBucket</Code>
    <Message>The specified bucket does not exist</Message>
    <BucketName>my-nonexistent-bucket</BucketName>
    <RequestId>4442587FB7D0A2F9</RequestId>
    <HostId>eftixk72aD6Ap51TnqcoF8eFidJG9Z/2mkiDFu8yU9AS1ed4OpIszj7UDNEHGran</HostId>
</Error>
```

### Common Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `AccessDenied` | 403 | Access to resource denied |
| `BucketAlreadyExists` | 409 | Bucket name already taken |
| `BucketAlreadyOwnedByYou` | 409 | You already own this bucket |
| `BucketNotEmpty` | 409 | Cannot delete non-empty bucket |
| `EntityTooLarge` | 400 | Upload exceeds maximum size |
| `EntityTooSmall` | 400 | Part size below minimum |
| `InvalidAccessKeyId` | 403 | Invalid access key |
| `InvalidArgument` | 400 | Invalid request parameter |
| `InvalidBucketName` | 400 | Bucket name violates rules |
| `InvalidDigest` | 400 | Content-MD5 mismatch |
| `InvalidPart` | 400 | Invalid part in multipart |
| `InvalidPartOrder` | 400 | Parts not in ascending order |
| `InvalidRange` | 416 | Invalid byte range |
| `InvalidRequest` | 400 | Malformed request |
| `InvalidSignature` | 403 | Signature verification failed |
| `MalformedXML` | 400 | Invalid XML in request |
| `MethodNotAllowed` | 405 | HTTP method not allowed |
| `NoSuchBucket` | 404 | Bucket does not exist |
| `NoSuchKey` | 404 | Object does not exist |
| `NoSuchUpload` | 404 | Multipart upload not found |
| `NoSuchVersion` | 404 | Version ID not found |
| `NotImplemented` | 501 | Operation not supported |
| `PreconditionFailed` | 412 | Conditional request failed |
| `RequestTimeTooSkewed` | 403 | Request time too far from server |
| `ServiceUnavailable` | 503 | Server temporarily unavailable |
| `SignatureDoesNotMatch` | 403 | Signature calculation error |
| `SlowDown` | 503 | Rate limit exceeded |

## SDK Compatibility

NebulaIO works with all major AWS SDKs and S3-compatible tools.

### AWS SDK Configuration

#### Python (boto3)

```python
import boto3
from botocore.config import Config

s3 = boto3.client('s3',
    endpoint_url='http://nebulaio.local:9000',
    aws_access_key_id='YOUR_ACCESS_KEY',
    aws_secret_access_key='YOUR_SECRET_KEY',
    config=Config(
        signature_version='s3v4',
        s3={'addressing_style': 'path'}
    ),
    region_name='us-east-1'
)

# List buckets
response = s3.list_buckets()
for bucket in response['Buckets']:
    print(bucket['Name'])

# Upload file
s3.upload_file('local-file.txt', 'my-bucket', 'remote-file.txt')

# Download file
s3.download_file('my-bucket', 'remote-file.txt', 'downloaded.txt')
```

#### JavaScript/TypeScript (AWS SDK v3)

```javascript
import { S3Client, ListBucketsCommand, PutObjectCommand } from '@aws-sdk/client-s3';

const s3 = new S3Client({
    endpoint: 'http://nebulaio.local:9000',
    region: 'us-east-1',
    credentials: {
        accessKeyId: 'YOUR_ACCESS_KEY',
        secretAccessKey: 'YOUR_SECRET_KEY'
    },
    forcePathStyle: true
});

// List buckets
const buckets = await s3.send(new ListBucketsCommand({}));
console.log(buckets.Buckets);

// Upload object
await s3.send(new PutObjectCommand({
    Bucket: 'my-bucket',
    Key: 'my-file.txt',
    Body: 'Hello, NebulaIO!'
}));
```

#### Go

```go
package main

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials"
    "github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
    cfg, _ := config.LoadDefaultConfig(context.TODO(),
        config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
            "YOUR_ACCESS_KEY", "YOUR_SECRET_KEY", "",
        )),
        config.WithRegion("us-east-1"),
    )

    client := s3.NewFromConfig(cfg, func(o *s3.Options) {
        o.BaseEndpoint = aws.String("http://nebulaio.local:9000")
        o.UsePathStyle = true
    })

    // List buckets
    result, _ := client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
    for _, bucket := range result.Buckets {
        fmt.Println(*bucket.Name)
    }
}
```

#### Java

```java
import software.amazon.awssdk.auth.credentials.AwsBasicCredentials;
import software.amazon.awssdk.auth.credentials.StaticCredentialsProvider;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.S3Configuration;

S3Client s3 = S3Client.builder()
    .endpointOverride(URI.create("http://nebulaio.local:9000"))
    .region(Region.US_EAST_1)
    .credentialsProvider(StaticCredentialsProvider.create(
        AwsBasicCredentials.create("YOUR_ACCESS_KEY", "YOUR_SECRET_KEY")
    ))
    .serviceConfiguration(S3Configuration.builder()
        .pathStyleAccessEnabled(true)
        .build())
    .build();

// List buckets
s3.listBuckets().buckets().forEach(bucket ->
    System.out.println(bucket.name())
);
```

### CLI Tools

#### AWS CLI

```bash
# Configure AWS CLI
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY
aws configure set default.region us-east-1

# Use with NebulaIO
aws --endpoint-url http://nebulaio.local:9000 s3 ls
aws --endpoint-url http://nebulaio.local:9000 s3 mb s3://my-bucket
aws --endpoint-url http://nebulaio.local:9000 s3 cp file.txt s3://my-bucket/
```

#### s3cmd

```bash
# Configure s3cmd (~/.s3cfg)
cat > ~/.s3cfg << EOF
[default]
access_key = YOUR_ACCESS_KEY
secret_key = YOUR_SECRET_KEY
host_base = nebulaio.local:9000
host_bucket = %(bucket)s.nebulaio.local:9000
use_https = False
signature_v2 = False
EOF

# Use s3cmd
s3cmd ls
s3cmd mb s3://my-bucket
s3cmd put file.txt s3://my-bucket/
```

#### rclone

```bash
# Configure rclone
rclone config create nebulaio s3 \
    provider=Other \
    access_key_id=YOUR_ACCESS_KEY \
    secret_access_key=YOUR_SECRET_KEY \
    endpoint=http://nebulaio.local:9000

# Use rclone
rclone ls nebulaio:my-bucket
rclone copy local-dir nebulaio:my-bucket/remote-dir
rclone sync nebulaio:source-bucket nebulaio:dest-bucket
```

## Best Practices

### Performance Optimization

1. **Use multipart uploads** for objects larger than 100 MB
2. **Enable chunked encoding** for streaming uploads
3. **Use path-style addressing** for simpler DNS configuration
4. **Implement retry logic** with exponential backoff
5. **Use connection pooling** for high-throughput applications

### Security Recommendations

1. **Rotate credentials** regularly (every 90 days minimum)
2. **Use presigned URLs** for temporary access instead of embedding credentials
3. **Enable server-side encryption** for sensitive data
4. **Apply least-privilege policies** using bucket policies
5. **Enable versioning** for critical buckets

### Compatibility Tips

1. **Specify region** explicitly even though NebulaIO uses a single region
2. **Use path-style** addressing for maximum compatibility
3. **Handle pagination** properly for list operations (use ContinuationToken)
4. **Check Content-Type** headers are set correctly for uploads
5. **Validate checksums** using Content-MD5 or x-amz-checksum headers
