# Data Firewall

NebulaIO's Data Firewall provides enterprise-grade Quality of Service (QoS), rate limiting, bandwidth throttling, and access control to protect your storage infrastructure.

## Overview

The Data Firewall acts as a security and performance gateway between clients and storage, enforcing policies for:

- **Rate Limiting**: Token bucket algorithm for request throttling
- **Bandwidth Control**: Per-user and per-IP bandwidth limits
- **Connection Limits**: Maximum concurrent connections
- **IP Filtering**: Allowlist and blocklist controls
- **Time-based Rules**: Schedule-aware access policies

## Architecture

```
                    ┌─────────────────────────────────────────┐
                    │              Data Firewall               │
                    ├─────────────────────────────────────────┤
  Client Request    │  ┌─────────┐  ┌──────────┐  ┌────────┐  │
  ─────────────────►│  │   IP    │─►│   Rate   │─►│  Band- │  │
                    │  │ Filter  │  │  Limiter │  │  width │  │
                    │  └─────────┘  └──────────┘  └────────┘  │
                    │       │            │             │       │
                    │       ▼            ▼             ▼       │
                    │  ┌─────────────────────────────────────┐ │
                    │  │           Rule Engine               │ │
                    │  └─────────────────────────────────────┘ │
                    │                    │                     │
                    └────────────────────│─────────────────────┘
                                         │
                                         ▼
                              ┌─────────────────────┐
                              │   Storage Backend   │
                              └─────────────────────┘
```

## Configuration

```yaml
firewall:
  enabled: true
  default_policy: allow          # allow | deny

  # IP filtering
  ip_allowlist:
    - "10.0.0.0/8"
    - "192.168.0.0/16"
  ip_blocklist:
    - "203.0.113.0/24"

  # Rate limiting (token bucket)
  rate_limiting:
    enabled: true
    requests_per_second: 1000    # Global limit
    burst_size: 100              # Allow burst up to this size
    per_user_limit: 100          # Per-user RPS
    per_ip_limit: 50             # Per-IP RPS

  # Bandwidth throttling
  bandwidth:
    enabled: true
    max_bytes_per_second: 1073741824  # 1 GB/s global
    per_user_bytes_per_second: 104857600  # 100 MB/s per user
    per_ip_bytes_per_second: 52428800     # 50 MB/s per IP

  # Connection limits
  connections:
    max_total: 10000
    max_per_ip: 100
    max_per_user: 500

  # Audit logging
  audit_enabled: true
```

## Rate Limiting

### Token Bucket Algorithm

The firewall uses the token bucket algorithm for precise rate limiting:

```
┌─────────────────────────────────────────┐
│             Token Bucket                 │
├─────────────────────────────────────────┤
│  Capacity: 100 tokens (burst_size)      │
│  Refill Rate: 1000 tokens/sec (RPS)     │
│                                          │
│  ████████████░░░░░░░░  Current: 60      │
│                                          │
│  Request arrives:                        │
│    - Has token? → Allow & consume        │
│    - No token? → Reject (429)            │
└─────────────────────────────────────────┘
```

### Configuration

```yaml
rate_limiting:
  enabled: true
  requests_per_second: 1000  # Refill rate
  burst_size: 100            # Bucket capacity
```

### Per-Entity Limits

```yaml
rate_limiting:
  per_user_limit: 100     # Individual user limit
  per_ip_limit: 50        # Individual IP limit
  per_bucket_limit: 500   # Per-bucket limit
```

## Bandwidth Control

### Sliding Window Tracking

Bandwidth is tracked using a sliding window algorithm for accurate measurements:

```
┌─────────────────────────────────────────┐
│        Bandwidth Sliding Window          │
├─────────────────────────────────────────┤
│  Window: 1 second, 10 buckets (100ms)   │
│                                          │
│  [100MB][90MB][95MB][80MB][70MB]        │
│     ↑                                    │
│   Current bucket                         │
│                                          │
│  Total: 435MB / 1000MB allowed          │
└─────────────────────────────────────────┘
```

### Configuration

```yaml
bandwidth:
  enabled: true
  max_bytes_per_second: 1073741824      # 1 GB/s
  per_user_bytes_per_second: 104857600   # 100 MB/s
  per_ip_bytes_per_second: 52428800      # 50 MB/s
```

## Access Control Rules

### Rule Structure

```yaml
rules:
  - id: "block-large-uploads"
    priority: 100
    action: deny
    conditions:
      operation: PUT
      min_size: 1073741824  # 1GB

  - id: "allow-internal"
    priority: 50
    action: allow
    conditions:
      source_ip: "10.0.0.0/8"

  - id: "rate-limit-anonymous"
    priority: 75
    action: rate_limit
    conditions:
      user: anonymous
    rate_limit:
      requests_per_second: 10
      burst_size: 5
```

### Condition Types

| Condition | Description | Example |
|-----------|-------------|---------|
| source_ip | Client IP address (CIDR) | `10.0.0.0/8` |
| user | Username or `anonymous` | `alice` |
| bucket | Bucket name pattern | `logs-*` |
| operation | S3 operation | `GET`, `PUT`, `DELETE` |
| min_size | Minimum object size | `1048576` |
| max_size | Maximum object size | `1073741824` |
| time_window | Time range | `09:00-17:00` |
| day_of_week | Days | `Mon,Tue,Wed,Thu,Fri` |

### Action Types

| Action | Description |
|--------|-------------|
| allow | Allow the request |
| deny | Reject with 403 Forbidden |
| rate_limit | Apply specific rate limit |
| throttle | Apply bandwidth limit |
| log | Allow but log for audit |

## Time-Based Rules

### Business Hours Access

```yaml
rules:
  - id: "business-hours-only"
    priority: 100
    action: deny
    conditions:
      bucket: "production-*"
      time_window: "00:00-08:59,17:01-23:59"
      day_of_week: "Mon,Tue,Wed,Thu,Fri"
    message: "Access restricted to business hours"
```

### Maintenance Windows

```yaml
rules:
  - id: "maintenance-window"
    priority: 1  # Highest priority
    action: deny
    conditions:
      time_window: "02:00-04:00"
      day_of_week: "Sun"
    message: "System maintenance in progress"
```

## API Usage

### Get Firewall Status

```bash
curl -X GET http://localhost:9000/admin/firewall/status \
  -H "Authorization: Bearer $TOKEN"
```

Response:
```json
{
  "enabled": true,
  "default_policy": "allow",
  "stats": {
    "requests_allowed": 1523456,
    "requests_denied": 2345,
    "requests_rate_limited": 456,
    "bytes_transferred": 1099511627776,
    "active_connections": 234
  },
  "rate_limit": {
    "current_rate": 450,
    "limit": 1000,
    "burst_available": 75
  }
}
```

### Update Rules

```bash
curl -X PUT http://localhost:9000/admin/firewall/rules \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "rules": [
      {
        "id": "block-ip",
        "priority": 1,
        "action": "deny",
        "conditions": {
          "source_ip": "192.168.1.100"
        }
      }
    ]
  }'
```

### Block IP Address

```bash
curl -X POST http://localhost:9000/admin/firewall/block \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "ip": "192.168.1.100",
    "reason": "Suspicious activity",
    "duration": "24h"
  }'
```

### Unblock IP Address

```bash
curl -X DELETE "http://localhost:9000/admin/firewall/block?ip=192.168.1.100" \
  -H "Authorization: Bearer $TOKEN"
```

## Connection Management

### Limits

```yaml
connections:
  max_total: 10000        # Total concurrent connections
  max_per_ip: 100         # Per IP address
  max_per_user: 500       # Per authenticated user
  idle_timeout: 300       # Close idle after 5 minutes
```

### Connection Tracking

The firewall tracks active connections for:
- DoS protection
- Fair resource allocation
- Audit and monitoring

## Integration with Other Features

### Audit Logging

```yaml
firewall:
  audit_enabled: true
  audit_events:
    - denied
    - rate_limited
    - blocked_ip
```

### Event Notifications

```yaml
events:
  targets:
    - name: security-alerts
      type: webhook
      url: https://security.example.com/alerts
      filter:
        event_type: "firewall:*"
```

## Monitoring

### Prometheus Metrics

```
# Requests by action
nebulaio_firewall_requests_total{action="allow|deny|rate_limit"}

# Rate limit status
nebulaio_firewall_rate_limit_current
nebulaio_firewall_rate_limit_max

# Bandwidth
nebulaio_firewall_bandwidth_bytes_total
nebulaio_firewall_bandwidth_limit_bytes

# Connections
nebulaio_firewall_connections_active
nebulaio_firewall_connections_total{status="accepted|rejected"}

# Rule hits
nebulaio_firewall_rule_hits_total{rule_id="..."}
```

### Grafana Alerts

```yaml
# Alert on high deny rate
- alert: HighFirewallDenyRate
  expr: rate(nebulaio_firewall_requests_total{action="deny"}[5m]) > 100
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High firewall deny rate"
```

## Best Practices

1. **Start permissive**: Begin with `allow` default and add specific deny rules
2. **Use CIDR ranges**: Group IPs by network for efficient rule matching
3. **Prioritize rules**: Lower priority numbers are evaluated first
4. **Monitor before blocking**: Use `log` action to observe before `deny`
5. **Set reasonable limits**: Base rate limits on actual usage patterns
6. **Plan for bursts**: Set burst_size 10-20% of requests_per_second
7. **Document rules**: Add descriptions to all firewall rules
8. **Regular review**: Audit rules quarterly

## Troubleshooting

### Request Denied Unexpectedly

1. Check firewall logs: `grep "denied" /var/log/nebulaio/firewall.log`
2. Verify IP isn't in blocklist
3. Check time-based rules against current time
4. Verify rate limit hasn't been exceeded

### Rate Limit Too Aggressive

1. Check current rate: `curl /admin/firewall/stats`
2. Increase `burst_size` for bursty workloads
3. Consider per-user limits instead of global

### Performance Impact

1. Minimize rule count (combine where possible)
2. Order rules by frequency of match
3. Use IP ranges instead of individual IPs
4. Monitor CPU usage of firewall component
