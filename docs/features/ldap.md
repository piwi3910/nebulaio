# LDAP/Active Directory Integration

NebulaIO supports enterprise authentication through LDAP (Lightweight Directory Access Protocol) and Microsoft Active Directory. This allows organizations to use existing directory services for centralized user management and authentication.

## Overview

LDAP integration provides:

- **Centralized Authentication**: Authenticate users against corporate directory services
- **Group-Based Authorization**: Map LDAP groups to NebulaIO policies automatically
- **User Synchronization**: Periodically sync user accounts from LDAP to NebulaIO
- **Active Directory Support**: Pre-configured settings for Microsoft AD environments
- **Secure Connections**: TLS/STARTTLS support with certificate verification

## Configuration

### Basic Configuration

```yaml

# config.yaml
auth:
  ldap:
    enabled: true
    name: corporate-ldap
    priority: 1

    # LDAP Server Settings
    serverUrl: ldap://ldap.example.com:389
    bindDn: "cn=nebulaio-svc,ou=service-accounts,dc=example,dc=com"
    bindPassword: "${LDAP_BIND_PASSWORD}"

    # User Search Configuration
    userSearch:
      baseDn: "ou=users,dc=example,dc=com"
      filter: "(&(objectClass=person)(uid={username}))"
      scope: sub
      sizeLimit: 1
      timeLimit: 10s
      attributes:
        username: uid
        displayName: cn
        email: mail
        memberOf: memberOf
        uniqueId: entryUUID

    # Group Search Configuration
    groupSearch:
      baseDn: "ou=groups,dc=example,dc=com"
      filter: "(&(objectClass=groupOfNames)(member={dn}))"
      scope: sub
      nameAttribute: cn
      sizeLimit: 100
      timeLimit: 10s

    # Policy Mappings
    groupMappings:
      - externalGroup: storage-admins
        internalPolicy: admin
      - externalGroup: developers
        internalPolicy: readwrite
      - externalGroup: viewers
        internalPolicy: readonly
    defaultPolicy: readonly

    # Connection Pool
    pool:
      maxConnections: 10
      maxIdleConnections: 5
      connectTimeout: 10s
      requestTimeout: 30s
      idleTimeout: 5m

```bash

### Active Directory Configuration

For Microsoft Active Directory, use the optimized preset:

```yaml

auth:
  ldap:
    enabled: true
    name: active-directory
    serverUrl: ldaps://dc.corp.example.com:636
    bindDn: "CN=NebulaIO Service,OU=Service Accounts,DC=corp,DC=example,DC=com"
    bindPassword: "${AD_BIND_PASSWORD}"

    # TLS Configuration
    tls:
      insecureSkipVerify: false
      caCertFile: /etc/nebulaio/certs/ad-ca.crt

    # Active Directory User Search
    userSearch:
      baseDn: "OU=Users,DC=corp,DC=example,DC=com"
      filter: "(&(objectClass=user)(sAMAccountName={username}))"
      scope: sub
      attributes:
        username: sAMAccountName
        displayName: displayName
        email: mail
        memberOf: memberOf
        uniqueId: objectGUID

    # Active Directory Group Search
    groupSearch:
      baseDn: "OU=Groups,DC=corp,DC=example,DC=com"
      filter: "(&(objectClass=group)(member={dn}))"
      scope: sub
      nameAttribute: cn

    groupMappings:
      - externalGroup: Domain Admins
        internalPolicy: admin
      - externalGroup: Storage-Users
        internalPolicy: readwrite

```bash

### TLS/STARTTLS Configuration

```yaml

auth:
  ldap:
    serverUrl: ldap://ldap.example.com:389
    tls:
      # Enable STARTTLS upgrade for non-SSL connections
      startTls: true

      # Skip certificate verification (not recommended for production)
      insecureSkipVerify: false

      # Custom CA certificate for self-signed certs
      caCertFile: /etc/nebulaio/certs/ldap-ca.crt

      # Client certificate authentication (optional)
      clientCertFile: /etc/nebulaio/certs/client.crt
      clientKeyFile: /etc/nebulaio/certs/client.key

```bash

## User and Group Mapping

### Attribute Mapping

Map LDAP attributes to NebulaIO user fields:

| NebulaIO Field | LDAP (OpenLDAP) | Active Directory | Description |
| ---------------- | ----------------- | ------------------ | ------------- |
| `username` | `uid` | `sAMAccountName` | Login username |
| `displayName` | `cn` | `displayName` | User's full name |
| `email` | `mail` | `mail` | Email address |
| `memberOf` | `memberOf` | `memberOf` | Group membership DN |
| `uniqueId` | `entryUUID` | `objectGUID` | Unique identifier |

### Custom Attribute Mapping

Retrieve additional attributes for policy decisions:

```yaml

userSearch:
  attributes:
    username: uid
    displayName: cn
    email: mail
    additional:
      department: departmentNumber
      employeeType: employeeType
      costCenter: costCenter

```bash

### Group Membership Resolution

NebulaIO supports two methods for group resolution:

1. **memberOf Attribute**: Groups listed in user's `memberOf` attribute
2. **Group Search**: Query groups that contain the user as a member

```yaml

# Method 1: Using memberOf attribute (faster)
userSearch:
  attributes:
    memberOf: memberOf

# Method 2: Explicit group search (more flexible)
groupSearch:
  baseDn: "ou=groups,dc=example,dc=com"
  filter: "(&(objectClass=groupOfNames)(member={dn}))"
  nameAttribute: cn

```bash

## Search Filters and Base DN

### Search Scopes

| Scope | Description | Use Case |
| ------- | ------------- | ---------- |
| `base` | Search only the base DN entry | Single entry lookup |
| `one` | Search immediate children only | Flat OU structure |
| `sub` | Search entire subtree | Default, nested OUs |

### Filter Placeholders

| Placeholder | Description | Example |
| ------------- | ------------- | --------- |
| `{username}` | User-provided username | `(uid={username})` |
| `{dn}` | User's distinguished name | `(member={dn})` |

### Example Filters

```yaml

# Standard OpenLDAP user filter
userSearch:
  filter: "(&(objectClass=inetOrgPerson)(uid={username}))"

# Active Directory with enabled accounts only
userSearch:
  filter: "(&(objectClass=user)(sAMAccountName={username})(!(userAccountControl:1.2.840.113556.1.4.803:=2)))"

# Multiple objectClasses
userSearch:
  filter: "(|(&(objectClass=person)(uid={username}))(&(objectClass=account)(uid={username})))"

# Group filter with specific OU
groupSearch:
  filter: "(&(objectClass=groupOfNames)(member={dn})(ou:dn:=storage-access))"

```bash

## Policy Mapping from LDAP Groups

### Group to Policy Mapping

Map LDAP groups to NebulaIO built-in or custom policies:

```yaml

groupMappings:
  # Administrative access
  - externalGroup: cn=storage-admins,ou=groups,dc=example,dc=com
    internalPolicy: admin

  # Full read/write access
  - externalGroup: cn=developers,ou=groups,dc=example,dc=com
    internalPolicy: readwrite

  # Read-only access
  - externalGroup: cn=auditors,ou=groups,dc=example,dc=com
    internalPolicy: readonly

  # Custom policy
  - externalGroup: cn=data-science,ou=groups,dc=example,dc=com
    internalPolicy: ml-bucket-access

# Fallback policy when no groups match
defaultPolicy: readonly

```bash

### Built-in Policies

| Policy | Description |
| -------- | ------------- |
| `admin` | Full administrative access |
| `readwrite` | Read and write to all buckets |
| `readonly` | Read-only access to all buckets |
| `writeonly` | Write-only access (no listing) |
| `diagnostics` | Health check and diagnostics only |

## User Synchronization

Enable periodic synchronization to cache LDAP users locally:

```yaml

auth:
  ldap:
    sync:
      enabled: true
      interval: 15m
      syncOnStartup: true
      deleteOrphans: false
      pageSize: 500
      filter: "(employeeType=active)"

```bash

### Sync Configuration Options

| Option | Default | Description |
| -------- | --------- | ------------- |
| `enabled` | `false` | Enable user synchronization |
| `interval` | `15m` | Sync frequency |
| `syncOnStartup` | `true` | Run sync when service starts |
| `deleteOrphans` | `false` | Remove users no longer in LDAP |
| `pageSize` | `500` | LDAP paging size for large directories |
| `filter` | `""` | Additional filter for sync queries |

## Testing LDAP Configuration

### Using the CLI

```bash

# Test LDAP connection
nebulaio-cli admin ldap test-connection

# Test user authentication
nebulaio-cli admin ldap test-auth --username alice --password secret

# List groups for a user
nebulaio-cli admin ldap get-groups --username alice

# Trigger manual sync
nebulaio-cli admin ldap sync --force

# View sync status
nebulaio-cli admin ldap sync-status

```bash

### Using the API

```bash

# Test connection
curl -X POST "http://localhost:9001/api/v1/admin/ldap/test" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json"

# Test authentication
curl -X POST "http://localhost:9001/api/v1/admin/ldap/test-auth" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"username": "alice", "password": "secret"}'

# Get sync status
curl -X GET "http://localhost:9001/api/v1/admin/ldap/sync/status" \
  -H "Authorization: Bearer $ADMIN_TOKEN"

```bash

## Troubleshooting

### Common Issues

#### Connection Refused

```text

Error: failed to connect to LDAP server: dial tcp: connection refused

```text

**Solutions**:

- Verify the server URL and port are correct
- Check firewall rules allow connections to LDAP port (389/636)
- Ensure the LDAP server is running and accessible

#### Bind Failed

```text

Error: failed to bind: LDAP Result Code 49 "Invalid Credentials"

```text

**Solutions**:

- Verify the bind DN is correct and fully qualified
- Check the bind password is correct
- Ensure the service account has appropriate permissions

#### User Not Found

```text

Error: user not found: alice

```text

**Solutions**:

- Verify the user search base DN is correct
- Check the search filter matches user entries
- Ensure the username attribute mapping is correct
- Test with `ldapsearch` command directly

#### Certificate Errors

```text

Error: x509: certificate signed by unknown authority

```text

**Solutions**:

- Provide the CA certificate via `tls.caCertFile`
- For testing only, set `tls.insecureSkipVerify: true`
- Ensure certificate chain is complete

### Debug Logging

Enable debug logging for LDAP operations:

```yaml

logging:
  level: debug
  components:
    auth.ldap: trace

```bash

### Testing with ldapsearch

Verify LDAP connectivity and queries directly:

```bash

# Test user search
ldapsearch -x -H ldap://ldap.example.com:389 \
  -D "cn=nebulaio-svc,ou=service-accounts,dc=example,dc=com" \
  -W \
  -b "ou=users,dc=example,dc=com" \
  "(uid=alice)"

# Test group search
ldapsearch -x -H ldap://ldap.example.com:389 \
  -D "cn=nebulaio-svc,ou=service-accounts,dc=example,dc=com" \
  -W \
  -b "ou=groups,dc=example,dc=com" \
  "(member=uid=alice,ou=users,dc=example,dc=com)"

```bash

### Connection Pool Issues

If experiencing connection timeouts or pool exhaustion:

```yaml

pool:
  maxConnections: 20      # Increase pool size
  maxIdleConnections: 10
  connectTimeout: 15s     # Increase timeout
  requestTimeout: 60s
  idleTimeout: 10m

```

## Related Documentation

- [Security Architecture](../architecture/security.md)
- [Security Features](security-features.md)
- [OIDC/SSO Integration](oidc.md)
- [IAM Policies](iam-policies.md)
