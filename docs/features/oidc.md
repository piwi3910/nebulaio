# OpenID Connect (OIDC) Integration

NebulaIO supports OpenID Connect for enterprise single sign-on (SSO) authentication, enabling users to authenticate through identity providers like Keycloak, Okta, Auth0, Azure AD, and Google.

## Overview

OpenID Connect integration provides:

- **Single Sign-On**: Authenticate users via corporate identity providers
- **Claims-Based Authorization**: Map OIDC claims to NebulaIO policies automatically
- **Token Validation**: JWT validation with signature verification and expiry checks
- **Session Management**: Configurable session lifetimes and refresh token support

## Supported Providers

| Provider | Status | Notes |
|----------|--------|-------|
| Keycloak | Production | Full support with group claims |
| Okta | Production | Standard OIDC with groups scope |
| Auth0 | Production | Custom claims via Rules/Actions |
| Azure AD | Production | Microsoft Entra ID support |
| Google | Production | Google Workspace integration |

## Configuration Settings

```yaml
auth:
  oidc:
    enabled: true
    providerUrl: https://auth.example.com/realms/corporate
    clientId: nebulaio
    clientSecret: "${OIDC_CLIENT_SECRET}"
    scopes: [openid, profile, email, groups]
    redirectUri: https://storage.example.com/oauth/callback

    claims:
      username: preferred_username
      email: email
      groups: groups

    groupMappings:
      - claimValue: storage-admins
        internalPolicy: admin
      - claimValue: developers
        internalPolicy: readwrite
    defaultPolicy: readonly
```

## Claims Mapping to Policies

| OIDC Claim | NebulaIO Field | Description |
|------------|----------------|-------------|
| `sub` | `uniqueId` | Unique subject identifier |
| `preferred_username` | `username` | Login username |
| `email` | `email` | Email address |
| `groups` | `groups` | Group memberships |

### Group-to-Policy Mapping

```yaml
groupMappings:
  - claimValue: storage-admins
    internalPolicy: admin
  - claimValue: developers
    internalPolicy: readwrite
  - claimValue: auditors
    internalPolicy: readonly
defaultPolicy: readonly
```

## Token Validation

```yaml
auth:
  oidc:
    tokenValidation:
      validateIssuer: true
      validateAudience: true
      audience: nebulaio
      clockSkew: 60s
      jwksRefreshInterval: 1h
      allowedAlgorithms: [RS256, RS384, ES256]
```

## Session Management

```yaml
auth:
  oidc:
    session:
      duration: 8h
      maxDuration: 24h
      idleTimeout: 1h
      enableRefresh: true
      secureCookie: true
```

## Example Configurations for Popular Providers

### Keycloak

```yaml
auth:
  oidc:
    enabled: true
    providerUrl: https://keycloak.example.com/realms/corporate
    clientId: nebulaio
    clientSecret: "${KEYCLOAK_CLIENT_SECRET}"
    scopes: [openid, profile, email, groups]
    groupMappings:
      - claimValue: /storage-admins
        internalPolicy: admin
```

### Okta

```yaml
auth:
  oidc:
    enabled: true
    providerUrl: https://your-org.okta.com
    clientId: 0oa1234567890abcdef
    clientSecret: "${OKTA_CLIENT_SECRET}"
    scopes: [openid, profile, email, groups]
    tokenValidation:
      audience: api://nebulaio
```

### Auth0

```yaml
auth:
  oidc:
    enabled: true
    providerUrl: https://your-tenant.auth0.com
    clientId: abc123def456
    clientSecret: "${AUTH0_CLIENT_SECRET}"
    claims:
      groups: https://nebulaio.example.com/groups
    tokenValidation:
      audience: https://api.nebulaio.example.com
```

### Azure AD (Microsoft Entra ID)

```yaml
auth:
  oidc:
    enabled: true
    providerUrl: https://login.microsoftonline.com/{tenant-id}/v2.0
    clientId: 12345678-1234-1234-1234-123456789012
    clientSecret: "${AZURE_CLIENT_SECRET}"
    scopes: [openid, profile, email, GroupMember.Read.All]
    tokenValidation:
      audience: api://nebulaio
```

### Google Workspace

```yaml
auth:
  oidc:
    enabled: true
    providerUrl: https://accounts.google.com
    clientId: 123456789-abc123.apps.googleusercontent.com
    clientSecret: "${GOOGLE_CLIENT_SECRET}"
    claims:
      username: email
    domainRestriction: example.com
```

## Testing Configuration

```bash
# Test OIDC provider connectivity
nebulaio-cli admin oidc test-connection

# Validate token
nebulaio-cli admin oidc validate-token --token "$ID_TOKEN"

# List active sessions
nebulaio-cli admin oidc sessions list
```

## Troubleshooting

### Token Validation Failed

- Verify JWKS URI is accessible from the NebulaIO server
- Check `allowedAlgorithms` includes your provider's algorithm
- Ensure provider signing keys have not rotated without JWKS refresh

### Groups Claim Missing

- Verify `groups` scope is included in the OIDC request
- For Azure AD, ensure GroupMember.Read.All permission is granted
- For Auth0, configure a Rule/Action to include groups in the token

### Debug Logging

```yaml
logging:
  level: debug
  components:
    auth.oidc: trace
```

## Related Documentation

- [Security Architecture](../architecture/security.md)
- [LDAP Integration](ldap.md)
- [Security Features](security-features.md)
