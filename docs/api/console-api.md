# Console API Reference

The Console API provides the backend interface for the NebulaIO web console, enabling user-facing operations such as browsing buckets, managing objects, and configuring personal settings.

## Base URL

```
http://localhost:9001/api/v1/console
```

## Authentication

The Console API uses JWT session-based authentication. All endpoints require a valid access token.

```http
Authorization: Bearer <access_token>
```

### Login

```http
POST /api/v1/admin/auth/login
Content-Type: application/json

{"username": "user@example.com", "password": "secure_password"}
```

**Response:**

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_in": 900,
  "token_type": "Bearer"
}
```

### Token Refresh

```http
POST /api/v1/admin/auth/refresh
{"refresh_token": "eyJhbGciOiJIUzI1NiIs..."}
```

## Dashboard Endpoints

### Get Current User

```http
GET /api/v1/console/me
```

**Response:**

```json
{"id": "usr_abc123", "username": "johndoe", "email": "john@example.com", "role": "user"}
```

### Update Password

```http
PUT /api/v1/console/me/password
{"current_password": "old_pass", "new_password": "new_pass"}
```

## Access Key Management

### List Access Keys

```http
GET /api/v1/console/me/keys
```

**Response:**

```json
{"keys": [{"access_key_id": "AKIA...", "description": "Production app", "status": "active"}]}
```

### Create Access Key

```http
POST /api/v1/console/me/keys
{"description": "CI/CD pipeline"}
```

**Response:**

```json
{"access_key_id": "AKIA...", "secret_access_key": "wJalrXUtnFEMI...", "description": "CI/CD pipeline"}
```

### Delete Access Key

```http
DELETE /api/v1/console/me/keys/{accessKeyId}
```

## Bucket Browsing

### List Buckets

```http
GET /api/v1/console/buckets
```

**Response:**

```json
{"buckets": [{"name": "my-data", "creation_date": "2024-03-15T08:00:00Z", "object_count": 1250}]}
```

### Get Bucket Settings

```http
GET /api/v1/console/buckets/{bucketName}/settings
```

**Response:**

```json
{"versioning": true, "encryption": "AES256", "public_access_blocked": true}
```

## Object Management

### List Objects

```http
GET /api/v1/console/buckets/{bucket}/objects?prefix=docs/&max_keys=100
```

| Parameter | Description |
|-----------|-------------|
| `prefix` | Filter by key prefix |
| `delimiter` | Grouping delimiter (typically `/`) |
| `max_keys` | Max objects to return (default: 1000) |
| `page_token` | Pagination token |

**Response:**

```json
{
  "objects": [{"key": "docs/report.pdf", "size": 1048576, "last_modified": "2024-12-20T14:30:00Z"}],
  "common_prefixes": ["docs/", "images/"],
  "next_page_token": "eyJrZXkiOi..."
}
```

### Upload Object

```http
POST /api/v1/console/buckets/{bucket}/objects
Content-Type: multipart/form-data

file: <binary>
path: documents/  (optional)
```

### Delete Object

```http
DELETE /api/v1/console/buckets/{bucket}/objects/{key}
```

### Get Download URL

```http
GET /api/v1/console/buckets/{bucket}/objects/{key}/download-url?expires_in=3600
```

**Response:**

```json
{"url": "https://nebulaio.example.com/bucket/file.pdf?X-Amz-...", "expires_at": "2024-12-27T11:00:00Z"}
```

## WebSocket Events

Connect to `wss://nebulaio.example.com/api/v1/console/ws` for real-time updates.

### Authentication

```javascript
ws.send(JSON.stringify({ type: 'auth', token: '<access_token>' }));
```

### Event Types

**Upload Progress:**

```json
{"type": "upload_progress", "data": {"upload_id": "upl_abc", "bucket": "my-data", "percent": 50}}
```

**Bucket Event:**

```json
{"type": "bucket_event", "data": {"event": "s3:ObjectCreated:Put", "bucket": "my-data", "key": "file.pdf"}}
```

**Quota Alert:**

```json
{"type": "quota_alert", "data": {"bucket": "my-data", "usage_percent": 90}}
```

## Error Responses

```json
{"error": {"code": "AccessDenied", "message": "Insufficient permissions", "request_id": "req_xyz789"}}
```

| Status | Code | Description |
|--------|------|-------------|
| 400 | `InvalidRequest` | Malformed request |
| 401 | `AuthenticationRequired` | Invalid token |
| 403 | `AccessDenied` | Insufficient permissions |
| 404 | `NotFound` | Resource not found |
| 429 | `TooManyRequests` | Rate limit exceeded |

## Rate Limits

- **Read operations:** 500 requests/minute
- **Write operations:** 50 requests/minute
- **Uploads:** 10 concurrent

Rate limit headers in responses:

```http
X-RateLimit-Limit: 500
X-RateLimit-Remaining: 498
X-RateLimit-Reset: 1703674800
```
