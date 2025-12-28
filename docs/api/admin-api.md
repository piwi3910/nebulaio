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

## Placement Groups

Placement groups manage data locality and fault isolation for erasure coding and tiering.

### List Placement Groups
```
GET /admin/cluster/placement-groups
```
```json
{
  "placement_groups": [
    {
      "id": "pg-dc1",
      "name": "US East Datacenter",
      "datacenter": "dc1",
      "region": "us-east-1",
      "status": "healthy",
      "node_count": 14,
      "min_nodes": 14,
      "max_nodes": 50,
      "is_local": true
    }
  ]
}
```

### Get Placement Group
```
GET /admin/cluster/placement-groups/{groupId}
```
```json
{
  "id": "pg-dc1",
  "name": "US East Datacenter",
  "datacenter": "dc1",
  "region": "us-east-1",
  "status": "healthy",
  "nodes": ["node-1", "node-2", "node-3", "..."],
  "min_nodes": 14,
  "max_nodes": 50,
  "is_local": true,
  "replication_targets": ["pg-dc2"]
}
```

### Create Placement Group
```
POST /admin/cluster/placement-groups
```
**Request:**
```json
{
  "id": "pg-dc2",
  "name": "US West Datacenter",
  "datacenter": "dc2",
  "region": "us-west-2",
  "min_nodes": 14,
  "max_nodes": 50
}
```
**Response:** `201 Created`
```json
{
  "id": "pg-dc2",
  "status": "offline",
  "message": "Placement group created. Add nodes to activate."
}
```

### Update Placement Group
```
PATCH /admin/cluster/placement-groups/{groupId}
```
**Request:**
```json
{
  "name": "Updated Name",
  "min_nodes": 10,
  "max_nodes": 100
}
```

### Delete Placement Group
```
DELETE /admin/cluster/placement-groups/{groupId}
```
**Note:** Only empty placement groups (no nodes) can be deleted.

### Add Node to Placement Group
```
POST /admin/cluster/placement-groups/{groupId}/nodes
```
**Request:**
```json
{
  "node_id": "new-node-1"
}
```
**Response:** `201 Created`
```json
{
  "group_id": "pg-dc1",
  "node_id": "new-node-1",
  "node_count": 15,
  "status": "healthy"
}
```

### Remove Node from Placement Group
```
DELETE /admin/cluster/placement-groups/{groupId}/nodes/{nodeId}
```
**Response:** `200 OK`
```json
{
  "group_id": "pg-dc1",
  "node_id": "removed-node",
  "node_count": 13,
  "status": "degraded"
}
```

### Get Placement Group Status
```
GET /admin/cluster/placement-groups/{groupId}/status
```
```json
{
  "id": "pg-dc1",
  "status": "healthy",
  "node_count": 14,
  "min_nodes": 14,
  "healthy_nodes": 14,
  "degraded_nodes": 0,
  "offline_nodes": 0,
  "storage_used_bytes": 10737418240,
  "storage_total_bytes": 107374182400,
  "storage_used_pct": 10.0
}
```

### Configure Replication Targets
```
PUT /admin/cluster/placement-groups/{groupId}/replication
```
**Request:**
```json
{
  "targets": ["pg-dc2", "pg-dc3"],
  "replication_factor": 2
}
```

### Placement Group Status Values

| Status | Value | Description |
|--------|-------|-------------|
| healthy | 1 | Node count >= min_nodes, all operations normal |
| degraded | 2 | Node count < min_nodes, reduced redundancy |
| offline | 3 | No nodes available, read-only or unavailable |
| unknown | 0 | Status not yet determined |

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

## Tiering Policy Management

NebulaIO provides comprehensive tiering policy management for automated data lifecycle management. Objects can be transitioned between storage tiers (hot, warm, cold, archive) based on access patterns, age, capacity thresholds, or ML-based predictions.

### Overview

| Property | Description |
|----------|-------------|
| **Tiers** | `hot`, `warm`, `cold`, `archive` |
| **Storage Classes** | `STANDARD`, `STANDARD_IA`, `ONEZONE_IA`, `INTELLIGENT_TIERING`, `GLACIER`, `GLACIER_IR`, `DEEP_ARCHIVE` |
| **Policy Types** | `scheduled`, `realtime`, `threshold`, `s3_lifecycle` |
| **Policy Scopes** | `global`, `bucket`, `prefix`, `object` |

---

### Policy CRUD Operations

#### List All Policies

```
GET /admin/tiering/policies
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `type` | string | Filter by policy type (`scheduled`, `realtime`, `threshold`, `s3_lifecycle`) |
| `scope` | string | Filter by policy scope (`global`, `bucket`, `prefix`, `object`) |
| `enabled` | boolean | Filter by enabled status (`true` or `false`) |

**Response:**
```json
{
  "policies": [
    {
      "id": "archive-logs-policy",
      "name": "Archive Old Logs",
      "description": "Move log files to archive tier after 90 days",
      "type": "scheduled",
      "scope": "bucket",
      "enabled": true,
      "priority": 100,
      "selector": {
        "buckets": ["logs-*"],
        "prefixes": ["app-logs/"],
        "suffixes": [".log", ".gz"],
        "tags": {"environment": "production"},
        "minSize": 1024,
        "maxSize": 104857600,
        "contentTypes": ["text/plain", "application/gzip"],
        "currentTiers": ["hot", "warm"],
        "excludeTiers": ["archive"]
      },
      "triggers": [
        {
          "type": "age",
          "age": {
            "daysSinceCreation": 90,
            "daysSinceModification": 60,
            "daysSinceAccess": 30
          }
        }
      ],
      "actions": [
        {
          "type": "transition",
          "transition": {
            "targetTier": "archive",
            "targetStorageClass": "GLACIER",
            "compression": true,
            "compressionAlgorithm": "zstd"
          },
          "stopProcessing": true
        }
      ],
      "antiThrash": {
        "enabled": true,
        "minTimeInTier": "24h",
        "cooldownAfterTransition": "12h",
        "maxTransitionsPerDay": 2,
        "stickinessWeight": 0.5,
        "requireConsecutiveEvaluations": 3
      },
      "schedule": {
        "enabled": true,
        "maintenanceWindows": [
          {
            "name": "nightly-window",
            "daysOfWeek": [0, 1, 2, 3, 4, 5, 6],
            "startTime": "02:00",
            "endTime": "06:00"
          }
        ],
        "timezone": "UTC"
      },
      "rateLimit": {
        "enabled": true,
        "maxObjectsPerSecond": 100,
        "maxBytesPerSecond": 104857600,
        "maxConcurrent": 10,
        "burstSize": 50
      },
      "distributed": {
        "enabled": true,
        "shardByBucket": true,
        "requireLeaderElection": false,
        "coordinationKey": "tiering-archive-logs"
      },
      "createdAt": "2024-01-15T10:00:00Z",
      "updatedAt": "2024-01-20T15:30:00Z",
      "lastRunAt": "2024-01-21T03:00:00Z",
      "lastRunNode": "node-1",
      "version": 3
    }
  ],
  "total_count": 1
}
```

---

#### Create Policy

```
POST /admin/tiering/policies
```

**Request:**
```json
{
  "id": "promote-hot-objects",
  "name": "Promote Frequently Accessed Objects",
  "description": "Promote objects with high access rates to hot tier",
  "type": "realtime",
  "scope": "global",
  "enabled": true,
  "priority": 50,
  "selector": {
    "currentTiers": ["warm", "cold"],
    "minSize": 1024
  },
  "triggers": [
    {
      "type": "access",
      "access": {
        "operation": "GET",
        "direction": "up",
        "countThreshold": 10,
        "periodMinutes": 60,
        "promoteOnAnyRead": false,
        "promoteToCache": true
      }
    }
  ],
  "actions": [
    {
      "type": "transition",
      "transition": {
        "targetTier": "hot",
        "targetStorageClass": "STANDARD"
      }
    }
  ],
  "antiThrash": {
    "enabled": true,
    "minTimeInTier": "1h",
    "cooldownAfterTransition": "30m",
    "maxTransitionsPerDay": 5
  }
}
```

**Response (201 Created):**
```json
{
  "id": "promote-hot-objects",
  "name": "Promote Frequently Accessed Objects",
  "type": "realtime",
  "scope": "global",
  "enabled": true,
  "createdAt": "2024-01-21T10:00:00Z",
  "updatedAt": "2024-01-21T10:00:00Z",
  "version": 1
}
```

---

#### Get Policy

```
GET /admin/tiering/policies/{id}
```

**Response:**
```json
{
  "id": "archive-logs-policy",
  "name": "Archive Old Logs",
  "description": "Move log files to archive tier after 90 days",
  "type": "scheduled",
  "scope": "bucket",
  "enabled": true,
  "priority": 100,
  "selector": {
    "buckets": ["logs-*"],
    "prefixes": ["app-logs/"]
  },
  "triggers": [
    {
      "type": "age",
      "age": {
        "daysSinceAccess": 90
      }
    }
  ],
  "actions": [
    {
      "type": "transition",
      "transition": {
        "targetTier": "archive",
        "targetStorageClass": "GLACIER"
      }
    }
  ],
  "createdAt": "2024-01-15T10:00:00Z",
  "updatedAt": "2024-01-20T15:30:00Z",
  "version": 3
}
```

---

#### Update Policy

```
PUT /admin/tiering/policies/{id}
```

**Request:**
```json
{
  "name": "Archive Old Logs (Updated)",
  "description": "Updated: Move log files to archive tier after 60 days",
  "type": "scheduled",
  "scope": "bucket",
  "enabled": true,
  "priority": 100,
  "selector": {
    "buckets": ["logs-*"],
    "prefixes": ["app-logs/"]
  },
  "triggers": [
    {
      "type": "age",
      "age": {
        "daysSinceAccess": 60
      }
    }
  ],
  "actions": [
    {
      "type": "transition",
      "transition": {
        "targetTier": "archive",
        "targetStorageClass": "DEEP_ARCHIVE"
      }
    }
  ]
}
```

**Response:**
```json
{
  "id": "archive-logs-policy",
  "name": "Archive Old Logs (Updated)",
  "updatedAt": "2024-01-21T12:00:00Z",
  "version": 4
}
```

---

#### Delete Policy

```
DELETE /admin/tiering/policies/{id}
```

**Response:** `204 No Content`

---

#### Enable Policy

```
POST /admin/tiering/policies/{id}/enable
```

**Response:**
```json
{
  "id": "archive-logs-policy",
  "enabled": true,
  "message": "Policy enabled successfully"
}
```

---

#### Disable Policy

```
POST /admin/tiering/policies/{id}/disable
```

**Response:**
```json
{
  "id": "archive-logs-policy",
  "enabled": false,
  "message": "Policy disabled successfully"
}
```

---

#### Get Policy Statistics

```
GET /admin/tiering/policies/{id}/stats
```

**Response:**
```json
{
  "policy_id": "archive-logs-policy",
  "policy_name": "Archive Old Logs",
  "enabled": true,
  "type": "scheduled",
  "last_executed": "2024-01-21T03:00:00Z",
  "total_executions": 156,
  "objects_evaluated": 125000,
  "objects_transitioned": 8500,
  "bytes_transitioned": 10737418240,
  "errors": 12,
  "last_error": "timeout connecting to cold storage backend"
}
```

---

### Access Statistics

#### Get Bucket Access Statistics

```
GET /admin/tiering/access-stats/{bucket}
```

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | integer | 100 | Maximum results (1-1000) |
| `offset` | integer | 0 | Pagination offset |

**Response:**
```json
{
  "bucket": "my-data",
  "limit": 100,
  "offset": 0,
  "stats": []
}
```

---

#### Get Object Access Statistics

```
GET /admin/tiering/access-stats/{bucket}/{key}
```

**Note:** For keys containing special characters, use the `key` query parameter instead.

**Response (when tracked):**
```json
{
  "bucket": "my-data",
  "key": "reports/2024/q1-summary.pdf",
  "tracked": true,
  "access_count": 1250,
  "last_accessed": "2024-01-21T09:45:00Z",
  "accesses_last_24h": 45,
  "accesses_last_7d": 280,
  "accesses_last_30d": 890,
  "average_accesses_day": 29.67,
  "access_trend": "increasing"
}
```

**Response (when not tracked):**
```json
{
  "bucket": "my-data",
  "key": "archive/old-file.dat",
  "tracked": false,
  "message": "No access stats tracked for this object"
}
```

---

### Tier Management

#### Manual Object Transition

```
POST /admin/tiering/transition
```

**Request:**
```json
{
  "bucket": "production-data",
  "key": "datasets/large-dataset.parquet",
  "target_tier": "cold",
  "force": false
}
```

**Tier Values:** `hot`, `warm`, `cold`, `archive`

**Response:**
```json
{
  "bucket": "production-data",
  "key": "datasets/large-dataset.parquet",
  "target_tier": "cold",
  "message": "Object transition initiated successfully"
}
```

---

### S3 Lifecycle Compatibility

NebulaIO supports S3-compatible lifecycle configuration for bucket-level tiering policies.

#### Get Bucket Lifecycle Configuration

```
GET /admin/tiering/s3-lifecycle/{bucket}
```

**Headers:**
- `Accept: application/json` (default) or `Accept: application/xml`

**Response (JSON):**
```json
{
  "Rules": [
    {
      "ID": "archive-old-objects",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "logs/"
      },
      "Transitions": [
        {
          "Days": 30,
          "StorageClass": "STANDARD_IA"
        },
        {
          "Days": 90,
          "StorageClass": "GLACIER"
        }
      ],
      "Expiration": {
        "Days": 365
      }
    }
  ]
}
```

**Response (XML):**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<LifecycleConfiguration>
  <Rule>
    <ID>archive-old-objects</ID>
    <Status>Enabled</Status>
    <Filter>
      <Prefix>logs/</Prefix>
    </Filter>
    <Transition>
      <Days>30</Days>
      <StorageClass>STANDARD_IA</StorageClass>
    </Transition>
    <Transition>
      <Days>90</Days>
      <StorageClass>GLACIER</StorageClass>
    </Transition>
    <Expiration>
      <Days>365</Days>
    </Expiration>
  </Rule>
</LifecycleConfiguration>
```

---

#### Set Bucket Lifecycle Configuration

```
PUT /admin/tiering/s3-lifecycle/{bucket}
```

**Headers:**
- `Content-Type: application/json` or `Content-Type: application/xml`

**Request (JSON):**
```json
{
  "Rules": [
    {
      "ID": "transition-to-ia",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "data/",
        "Tag": {
          "Key": "archive",
          "Value": "true"
        }
      },
      "Transitions": [
        {
          "Days": 30,
          "StorageClass": "STANDARD_IA"
        }
      ]
    },
    {
      "ID": "expire-temp-files",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "tmp/"
      },
      "Expiration": {
        "Days": 7
      },
      "AbortIncompleteMultipartUpload": {
        "DaysAfterInitiation": 1
      }
    }
  ]
}
```

**Response:**
```json
{
  "bucket": "my-bucket",
  "message": "Lifecycle configuration applied successfully",
  "rules": 2
}
```

---

#### Delete Bucket Lifecycle Configuration

```
DELETE /admin/tiering/s3-lifecycle/{bucket}
```

**Response:** `204 No Content`

---

### Tiering Status and Metrics

#### Get Tiering System Status

```
GET /admin/tiering/status
```

**Response:**
```json
{
  "status": "running",
  "total_policies": 15,
  "enabled_policies": 12,
  "policies_by_type": {
    "scheduled": 8,
    "realtime": 3,
    "threshold": 2,
    "s3_lifecycle": 2
  },
  "policies_by_scope": {
    "global": 2,
    "bucket": 10,
    "prefix": 3
  }
}
```

---

#### Get Tiering Metrics

```
GET /admin/tiering/metrics
```

**Response:**
```json
{
  "total_executions": 45678,
  "total_objects_transitioned": 2500000,
  "total_bytes_transitioned": 1099511627776,
  "total_errors": 234,
  "policy_count": 15
}
```

---

### Predictive Analytics (ML-Based)

NebulaIO includes ML-based predictive analytics for intelligent tiering decisions based on access pattern analysis.

#### Get Access Prediction for Object

```
GET /admin/tiering/predictions/{bucket}/{key}
```

**Note:** For keys containing special characters, use the `key` query parameter.

**Response (with sufficient data):**
```json
{
  "bucket": "production-data",
  "key": "datasets/analytics.parquet",
  "timestamp": "2024-01-21T10:00:00Z",
  "short_term_access_rate": 15.5,
  "short_term_confidence": 0.85,
  "medium_term_access_rate": 12.2,
  "medium_term_confidence": 0.68,
  "long_term_access_rate": 8.1,
  "long_term_confidence": 0.51,
  "trend_direction": "declining",
  "trend_strength": 0.35,
  "has_daily_pattern": true,
  "has_weekly_pattern": true,
  "peak_access_hour": 14,
  "peak_access_day": 2,
  "current_tier": "hot",
  "recommended_tier": "warm",
  "confidence": 0.85,
  "reasoning": "Predicted moderate access rate (12.2/day, declining trend). Recommend warm tier for balanced cost/performance."
}
```

**Response (insufficient data):**
```json
{
  "bucket": "production-data",
  "key": "new-file.dat",
  "message": "Insufficient data for prediction"
}
```

---

#### Get Tier Recommendations

```
GET /admin/tiering/predictions/recommendations
```

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | integer | 100 | Maximum recommendations (1-1000) |

**Response:**
```json
{
  "recommendations": [
    {
      "bucket": "data-warehouse",
      "key": "reports/quarterly/2023-q4.pdf",
      "current_tier": "hot",
      "recommended_tier": "warm",
      "confidence": 0.92,
      "predicted_access": 2.5,
      "reasoning": "Predicted moderate access rate (2.5/day, declining trend). Recommend warm tier for balanced cost/performance."
    },
    {
      "bucket": "logs",
      "key": "app/2023/december.log.gz",
      "current_tier": "warm",
      "recommended_tier": "archive",
      "confidence": 0.88,
      "predicted_access": 0.02,
      "reasoning": "Predicted minimal access (0.02/day, stable trend). Recommend archive tier for long-term storage."
    }
  ],
  "count": 2
}
```

---

#### Get Predicted Hot Objects

```
GET /admin/tiering/predictions/hot-objects
```

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | integer | 50 | Maximum results (1-500) |

**Response:**
```json
{
  "hot_objects": [
    {
      "bucket": "production",
      "key": "api/config.json",
      "current_tier": "hot",
      "last_accessed": "2024-01-21T09:59:00Z",
      "access_count": 50000,
      "accesses_last_24h": 2500,
      "accesses_last_7d": 15000,
      "accesses_last_30d": 45000,
      "average_accesses_day": 1500.0,
      "access_trend": "stable"
    }
  ],
  "count": 1
}
```

---

#### Get Predicted Cold Objects

```
GET /admin/tiering/predictions/cold-objects
```

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | integer | 50 | Maximum results (1-500) |
| `inactive_days` | integer | 30 | Days of inactivity threshold (1-365) |

**Response:**
```json
{
  "cold_objects": [
    {
      "bucket": "backups",
      "key": "database/2023-01-01.sql.gz",
      "current_tier": "warm",
      "last_accessed": "2023-06-15T14:30:00Z",
      "access_count": 5,
      "accesses_last_24h": 0,
      "accesses_last_7d": 0,
      "accesses_last_30d": 0,
      "average_accesses_day": 0.01,
      "access_trend": "declining"
    }
  ],
  "count": 1,
  "inactive_days": 30
}
```

---

#### Get Access Anomalies

```
GET /admin/tiering/anomalies
```

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | integer | 50 | Maximum anomalies (1-500) |

**Response:**
```json
{
  "anomalies": [
    {
      "bucket": "api-data",
      "key": "cache/user-sessions.json",
      "detected_at": "2024-01-21T08:15:00Z",
      "anomaly_type": "spike",
      "severity": 4.2,
      "expected": 50.0,
      "actual": 850.0,
      "description": "Access rate significantly higher than baseline"
    },
    {
      "bucket": "reports",
      "key": "daily/summary.pdf",
      "detected_at": "2024-01-21T06:00:00Z",
      "anomaly_type": "drop",
      "severity": 3.8,
      "expected": 120.0,
      "actual": 5.0,
      "description": "Access rate significantly lower than baseline"
    }
  ],
  "count": 2
}
```

---

### Policy Configuration Reference

#### Trigger Types

| Type | Description | Configuration |
|------|-------------|---------------|
| `age` | Object age-based trigger | `daysSinceCreation`, `daysSinceModification`, `daysSinceAccess`, `hoursSinceAccess` |
| `access` | Access pattern trigger | `operation`, `direction`, `countThreshold`, `periodMinutes`, `promoteOnAnyRead` |
| `capacity` | Tier capacity trigger | `tier`, `highWatermark`, `lowWatermark`, `bytesThreshold`, `objectCountThreshold` |
| `frequency` | Access frequency trigger | `minAccessesPerDay`, `maxAccessesPerDay`, `slidingWindowDays`, `pattern` |
| `cron` | Schedule-based trigger | `expression`, `timezone` |

#### Action Types

| Type | Description | Configuration |
|------|-------------|---------------|
| `transition` | Move to different tier | `targetTier`, `targetStorageClass`, `preserveCopy`, `compression`, `compressionAlgorithm` |
| `delete` | Delete object | `daysAfterTransition`, `expireDeleteMarkers`, `nonCurrentVersionDays` |
| `replicate` | Copy to another location | `destination`, `storageClass` |
| `notify` | Send notification | `endpoint`, `events` |

---

## Related Documentation

- [Configuration Reference](../getting-started/configuration.md)
- [Monitoring](../operations/monitoring.md)
- [Security Architecture](../architecture/security.md)
