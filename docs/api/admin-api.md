# Admin API Reference

The NebulaIO Admin API provides RESTful endpoints for managing the object storage system.

## Overview

| Property | Value |
|----------|-------|
| **Base URL** | `http://<host>:9001/api/v1` |
| **Default Port** | `9001` |
| **Protocol** | HTTP/HTTPS |
| **Format** | JSON |

The Admin API runs on a separate port from the S3 API (9000) to allow independent access control.

---

## Authentication

All endpoints (except `/admin/auth/login` and health checks) require JWT bearer token authentication.

### Obtaining a Token

```bash
curl -X POST http://localhost:9001/api/v1/admin/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "your-password"}'
```

**Response:**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_in": 86400
}
```

### Using the Token

```bash
curl -H "Authorization: Bearer <access_token>" http://localhost:9001/api/v1/admin/users
```

### Refreshing Tokens

```bash
curl -X POST http://localhost:9001/api/v1/admin/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token": "<refresh_token>"}'
```

---

## Cluster Management

### Get Cluster Status
```
GET /admin/cluster/status
```
```json
{"status": "healthy", "leader": "node-1", "nodes": 3, "quorum": true}
```

### List Cluster Nodes
```
GET /admin/cluster/nodes
```
```json
{
  "nodes": [{
    "id": "node-1", "address": "10.0.1.10:9003", "status": "leader",
    "healthy": true, "last_contact": "2024-01-15T10:30:00Z"
  }]
}
```

### Node Operations
```
POST /admin/cluster/nodes/{nodeId}/promote    # Promote to voter
DELETE /admin/cluster/nodes/{nodeId}          # Remove from cluster
```

---

## User and IAM Management

### Users

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/admin/users` | GET | List all users |
| `/admin/users` | POST | Create user |
| `/admin/users/{userId}` | GET | Get user details |
| `/admin/users/{userId}` | PUT | Update user |
| `/admin/users/{userId}` | DELETE | Delete user |

**Create User Request:**
```json
{"username": "bob", "password": "secure-password", "email": "bob@example.com", "role": "user"}
```

**Roles:** `superadmin`, `admin`, `user`, `readonly`, `service`

### Policies

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/admin/policies` | GET | List policies |
| `/admin/policies` | POST | Create policy |

**Create Policy Request:**
```json
{
  "name": "dev-access",
  "document": "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":[\"s3:*\"],\"Resource\":[\"arn:aws:s3:::dev-*/*\"]}]}"
}
```

---

## Bucket Administration

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/admin/buckets` | GET | List all buckets |
| `/admin/buckets` | POST | Create bucket |
| `/admin/buckets/{name}` | GET | Get bucket details |
| `/admin/buckets/{name}` | DELETE | Delete bucket (must be empty) |
| `/admin/buckets/{name}/versioning` | GET/PUT | Manage versioning |

**Create Bucket Request:**
```json
{"name": "new-bucket", "region": "us-east-1", "storage_class": "STANDARD"}
```

**Bucket Details Response:**
```json
{
  "name": "production-data", "size": 1073741824, "object_count": 15420,
  "versioning_enabled": true, "encryption_enabled": true,
  "lifecycle_rules": [{"id": "archive", "prefix": "logs/", "expiration_days": 365}]
}
```

---

## Storage Management

### Server Configuration

```
GET /admin/config     # Get configuration
PUT /admin/config     # Update configuration
```

**Response:**
```json
{
  "s3_express": {"enabled": true, "enable_atomic_append": true},
  "iceberg": {"enabled": true, "warehouse": "s3://warehouse/"},
  "gpudirect": {"enabled": false},
  "rdma": {"enabled": false}
}
```

### Storage Metrics

Available via Prometheus endpoint at `/metrics`:

| Metric | Description |
|--------|-------------|
| `nebulaio_storage_capacity_bytes` | Total capacity |
| `nebulaio_storage_used_bytes` | Used space |
| `nebulaio_storage_available_bytes` | Available space |
| `nebulaio_objects_total` | Object count by bucket |

---

## Replication Configuration

### Site Replication Status
```
GET /admin/site-replication/status
```
```json
{
  "site": "us-east-1", "status": "healthy",
  "peers": [{"name": "eu-west-1", "status": "connected", "lag_seconds": 0.5}]
}
```

### Initialize and Configure

```
POST /admin/site-replication/init
POST /admin/site-replication/peers
PUT /admin/site-replication/buckets/{bucketName}
```

**Add Peer Request:**
```json
{
  "name": "eu-west-1", "endpoint": "https://nebulaio-eu.example.com:9000",
  "access_key": "...", "secret_key": "...", "region": "eu-west"
}
```

---

## Health and Metrics

### Health Endpoints

| Endpoint | Description |
|----------|-------------|
| `/health` | Detailed health status |
| `/health/live` | Liveness probe (process running) |
| `/health/ready` | Readiness probe (service ready) |
| `/metrics` | Prometheus metrics |

**Health Response:**
```json
{
  "status": "healthy", "version": "1.0.0", "uptime_seconds": 86400,
  "components": {
    "storage": {"status": "healthy", "used_percent": 45.2},
    "cluster": {"status": "healthy", "role": "leader", "members": 3}
  }
}
```

### Audit Logs

```
GET /admin/audit-logs?bucket=my-bucket&user_id=usr_123&page=1&page_size=20
```

**Response:**
```json
{
  "logs": [{
    "id": "log_xyz", "timestamp": "2024-01-15T10:30:00Z", "event_type": "PutObject",
    "username": "alice", "bucket": "data", "object_key": "file.pdf", "status_code": 200
  }],
  "total": 1250, "page": 1, "page_size": 20
}
```

---

## Error Handling

**Error Response Format:**
```json
{"error": "NotFound", "message": "User not found", "code": "USER_NOT_FOUND"}
```

### HTTP Status Codes

| Code | Description |
|------|-------------|
| `200/201/204` | Success |
| `400` | Bad request |
| `401` | Unauthorized |
| `403` | Forbidden |
| `404` | Not found |
| `409` | Conflict |
| `429` | Rate limited |
| `500` | Server error |

### Rate Limiting

- **Read operations**: 1000 requests/minute
- **Write operations**: 100 requests/minute

Headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`

---

## Related Documentation

- [Configuration Reference](../getting-started/configuration.md)
- [Monitoring](../operations/monitoring.md)
- [Security Architecture](../architecture/security.md)
