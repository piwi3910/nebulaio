# Console API Reference

The Console API provides the backend interface for the NebulaIO web console, enabling user-facing operations such as browsing buckets, managing objects, and configuring personal settings.

## Base URL

```text

http://localhost:9001/api/v1/console

```bash

## Authentication

The Console API uses JWT session-based authentication. All endpoints require a valid access token.

```http

Authorization: Bearer <access_token>

```bash

### Login

```http

POST /api/v1/admin/auth/login
Content-Type: application/json

{"username": "user@example.com", "password": "secure_password"}

```text

**Response:**

```json

{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_in": 900,
  "token_type": "Bearer"
}

```bash

### Token Refresh

```http

POST /api/v1/admin/auth/refresh
{"refresh_token": "eyJhbGciOiJIUzI1NiIs..."}

```bash

## Dashboard Endpoints

### Get Current User

```http

GET /api/v1/console/me

```text

**Response:**

```json

{"id": "usr_abc123", "username": "johndoe", "email": "john@example.com", "role": "user"}

```bash

### Update Password

```http

PUT /api/v1/console/me/password
{"current_password": "old_pass", "new_password": "new_pass"}

```bash

## Access Key Management

### List Access Keys

```http

GET /api/v1/console/me/keys

```text

**Response:**

```json

{"keys": [{"access_key_id": "AKIA...", "description": "Production app", "status": "active"}]}

```bash

### Create Access Key

```http

POST /api/v1/console/me/keys
{"description": "CI/CD pipeline"}

```text

**Response:**

```json

{"access_key_id": "AKIA...", "secret_access_key": "wJalrXUtnFEMI...", "description": "CI/CD pipeline"}

```bash

### Delete Access Key

```http

DELETE /api/v1/console/me/keys/{accessKeyId}

```bash

## Bucket Browsing

### List Buckets

```http

GET /api/v1/console/buckets

```text

**Response:**

```json

{"buckets": [{"name": "my-data", "creation_date": "2024-03-15T08:00:00Z", "object_count": 1250}]}

```bash

### Get Bucket Settings

```http

GET /api/v1/console/buckets/{bucketName}/settings

```text

**Response:**

```json

{"versioning": true, "encryption": "AES256", "public_access_blocked": true}

```bash

## Object Management

### List Objects

```http

GET /api/v1/console/buckets/{bucket}/objects?prefix=docs/&max_keys=100

```text

| Parameter | Description |
| ----------- | ------------- |
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

```bash

### Upload Object

```http

POST /api/v1/console/buckets/{bucket}/objects
Content-Type: multipart/form-data

file: <binary>
path: documents/  (optional)

```bash

### Delete Object

```http

DELETE /api/v1/console/buckets/{bucket}/objects/{key}

```bash

### Get Download URL

```http

GET /api/v1/console/buckets/{bucket}/objects/{key}/download-url?expires_in=3600

```text

**Response:**

```json

{"url": "https://nebulaio.example.com/bucket/file.pdf?X-Amz-...", "expires_at": "2024-12-27T11:00:00Z"}

```bash

## WebSocket Events

Connect to `wss://nebulaio.example.com/api/v1/console/ws` for real-time updates.

### Authentication

```javascript

ws.send(JSON.stringify({ type: 'auth', token: '<access_token>' }));

```bash

### Event Types

**Upload Progress:**

```json

{"type": "upload_progress", "data": {"upload_id": "upl_abc", "bucket": "my-data", "percent": 50}}

```text

**Bucket Event:**

```json

{"type": "bucket_event", "data": {"event": "s3:ObjectCreated:Put", "bucket": "my-data", "key": "file.pdf"}}

```text

**Quota Alert:**

```json

{"type": "quota_alert", "data": {"bucket": "my-data", "usage_percent": 90}}

```bash

## Error Responses

```json

{"error": {"code": "AccessDenied", "message": "Insufficient permissions", "request_id": "req_xyz789"}}

```text

| Status | Code | Description |
| -------- | ------ | ------------- |
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
