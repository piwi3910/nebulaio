# IAM Policies

NebulaIO supports AWS-compatible IAM policies for fine-grained access control to buckets and objects.

## Overview

IAM policies enable:

- **Fine-Grained Access Control**: Precise permissions for users and groups
- **Resource-Based Policies**: Bucket-level access rules
- **Condition-Based Access**: Context-aware permission decisions
- **S3 Compatibility**: AWS IAM policy syntax support

## Policy Structure

IAM policies use JSON format with these elements:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject"],
      "Resource": ["arn:aws:s3:::my-bucket/*"],
      "Principal": {"AWS": ["arn:aws:iam::123456789:user/alice"]},
      "Condition": {}
    }
  ]
}
```

### Policy Elements

| Element | Description | Required |
|---------|-------------|----------|
| Version | Policy version (always "2012-10-17") | Yes |
| Statement | Array of permission statements | Yes |
| Effect | Allow or Deny | Yes |
| Action | Actions to allow/deny | Yes |
| Resource | Resources the policy applies to | Yes |
| Principal | Users/roles policy applies to | Depends |
| Condition | Conditions for policy application | No |

## Policy Types

### User Policies

Attached directly to users:

```bash
# Create user policy
nebulaio-cli admin policy create readonly-policy --file policy.json

# Attach to user
nebulaio-cli admin user attach-policy alice readonly-policy
```

### Group Policies

Applied to all members of a group:

```bash
# Attach policy to group
nebulaio-cli admin group attach-policy developers dev-policy
```

### Bucket Policies

Resource-based policies on buckets:

```bash
# Set bucket policy
aws s3api put-bucket-policy \
  --bucket my-bucket \
  --policy file://bucket-policy.json \
  --endpoint-url http://localhost:9000
```

## Common Policy Examples

### Read-Only Access

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:GetObjectVersion",
        "s3:GetObjectAttributes",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::my-bucket",
        "arn:aws:s3:::my-bucket/*"
      ]
    }
  ]
}
```

### Read-Write Access

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::my-bucket",
        "arn:aws:s3:::my-bucket/*"
      ]
    }
  ]
}
```

### Admin Access

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:*"],
      "Resource": ["*"]
    }
  ]
}
```

### Prefix-Based Access

Restrict access to specific prefixes:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:*"],
      "Resource": [
        "arn:aws:s3:::shared-bucket",
        "arn:aws:s3:::shared-bucket/users/${aws:username}/*"
      ]
    }
  ]
}
```

### Public Read Access

Allow public read (use carefully):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": "*",
      "Action": ["s3:GetObject"],
      "Resource": ["arn:aws:s3:::public-bucket/*"]
    }
  ]
}
```

### Deny Delete

Prevent deletions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:*"],
      "Resource": ["arn:aws:s3:::my-bucket/*"]
    },
    {
      "Effect": "Deny",
      "Action": ["s3:DeleteObject", "s3:DeleteObjectVersion"],
      "Resource": ["arn:aws:s3:::my-bucket/*"]
    }
  ]
}
```

### IP-Based Access

Restrict access to specific IPs:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:*"],
      "Resource": ["arn:aws:s3:::secure-bucket/*"],
      "Condition": {
        "IpAddress": {
          "aws:SourceIp": ["10.0.0.0/8", "192.168.1.0/24"]
        }
      }
    }
  ]
}
```

### Time-Based Access

Restrict access to specific times:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:*"],
      "Resource": ["arn:aws:s3:::business-bucket/*"],
      "Condition": {
        "DateGreaterThan": {"aws:CurrentTime": "2024-01-01T09:00:00Z"},
        "DateLessThan": {"aws:CurrentTime": "2024-12-31T18:00:00Z"}
      }
    }
  ]
}
```

### Require Encryption

Require server-side encryption:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Deny",
      "Action": ["s3:PutObject"],
      "Resource": ["arn:aws:s3:::secure-bucket/*"],
      "Condition": {
        "Null": {
          "s3:x-amz-server-side-encryption": "true"
        }
      }
    }
  ]
}
```

### Require TLS

Deny non-HTTPS requests:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Deny",
      "Action": ["s3:*"],
      "Resource": ["arn:aws:s3:::secure-bucket/*"],
      "Condition": {
        "Bool": {
          "aws:SecureTransport": "false"
        }
      }
    }
  ]
}
```

## Supported Actions

### Bucket Actions

| Action | Description |
|--------|-------------|
| s3:CreateBucket | Create buckets |
| s3:DeleteBucket | Delete buckets |
| s3:ListBucket | List objects in bucket |
| s3:ListBucketVersions | List object versions |
| s3:GetBucketLocation | Get bucket region |
| s3:GetBucketPolicy | Get bucket policy |
| s3:PutBucketPolicy | Set bucket policy |
| s3:DeleteBucketPolicy | Delete bucket policy |

### Object Actions

| Action | Description |
|--------|-------------|
| s3:GetObject | Download objects |
| s3:PutObject | Upload objects |
| s3:DeleteObject | Delete objects |
| s3:GetObjectVersion | Get specific version |
| s3:DeleteObjectVersion | Delete specific version |
| s3:GetObjectTagging | Get object tags |
| s3:PutObjectTagging | Set object tags |

## Condition Keys

### Global Condition Keys

| Key | Description |
|-----|-------------|
| aws:CurrentTime | Current UTC time |
| aws:SourceIp | Client IP address |
| aws:SecureTransport | Request uses HTTPS |
| aws:username | Authenticated username |

### S3 Condition Keys

| Key | Description |
|-----|-------------|
| s3:prefix | Object key prefix |
| s3:max-keys | Maximum keys in list |
| s3:x-amz-acl | Canned ACL being applied |
| s3:x-amz-server-side-encryption | SSE algorithm |
| s3:x-amz-content-sha256 | Content hash |

## Policy Variables

Use variables in policies for dynamic values:

| Variable | Description |
|----------|-------------|
| ${aws:username} | Current user's name |
| ${aws:userid} | Current user's ID |
| ${s3:prefix} | Requested prefix |

Example with variables:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:*"],
      "Resource": [
        "arn:aws:s3:::team-bucket/users/${aws:username}/*"
      ]
    }
  ]
}
```

## Policy Management

### Create Policy

```bash
nebulaio-cli admin policy create developer-policy --file developer.json
```

### List Policies

```bash
nebulaio-cli admin policy list
```

### View Policy

```bash
nebulaio-cli admin policy get developer-policy
```

### Delete Policy

```bash
nebulaio-cli admin policy delete developer-policy
```

### Attach Policy to User

```bash
nebulaio-cli admin user attach-policy alice developer-policy
```

### Detach Policy from User

```bash
nebulaio-cli admin user detach-policy alice developer-policy
```

## Policy Evaluation

Policies are evaluated in this order:

1. **Explicit Deny**: Any deny statement takes precedence
2. **Explicit Allow**: Allow statements grant access
3. **Implicit Deny**: No matching allow means denied

```
Request → Check Deny → Check Allow → Default Deny
           ↓            ↓             ↓
         DENIED       ALLOWED      DENIED
```

## Best Practices

1. **Least Privilege**: Grant minimum required permissions
2. **Use Groups**: Apply policies via groups, not individual users
3. **Explicit Denies**: Use deny statements for critical restrictions
4. **Test Policies**: Validate with policy simulator before applying
5. **Regular Audits**: Review and clean up unused policies
6. **Document Policies**: Maintain documentation for each policy
7. **Version Control**: Store policies in version control

## Troubleshooting

### Access Denied Errors

1. Check user's attached policies
2. Verify resource ARN format
3. Check for explicit deny statements
4. Verify condition requirements are met

### Policy Validation

```bash
# Validate policy syntax
nebulaio-cli admin policy validate --file policy.json
```

### Policy Simulator

```bash
# Test if action would be allowed
nebulaio-cli admin policy simulate \
  --user alice \
  --action s3:GetObject \
  --resource arn:aws:s3:::my-bucket/file.txt
```

## Related Documentation

- [LDAP Integration](ldap.md)
- [OIDC/SSO Integration](oidc.md)
- [Security Architecture](../architecture/security.md)
- [S3 Compatibility](../api/s3-compatibility.md)
