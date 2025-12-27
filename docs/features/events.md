# S3 Event Notifications

NebulaIO supports S3-compatible event notifications that allow you to receive alerts when specific events occur in your buckets. Events can be delivered to webhooks, message queues, and streaming platforms.

## Overview

Event notifications enable reactive architectures by triggering actions when objects change:

- **Real-time Processing**: Process uploads immediately as they arrive
- **Data Pipelines**: Trigger ETL jobs, ML inference, or analytics workflows
- **Audit and Compliance**: Track all object operations for compliance
- **Cross-System Integration**: Notify external systems of changes
- **Cache Invalidation**: Purge CDN caches when content updates

## Supported Events

### Object Created Events

| Event Type | Description |
|------------|-------------|
| `s3:ObjectCreated:*` | Any object creation event |
| `s3:ObjectCreated:Put` | Object uploaded via PUT |
| `s3:ObjectCreated:Post` | Object uploaded via POST form |
| `s3:ObjectCreated:Copy` | Object created via COPY |
| `s3:ObjectCreated:CompleteMultipartUpload` | Multipart upload completed |

### Object Removed Events

| Event Type | Description |
|------------|-------------|
| `s3:ObjectRemoved:*` | Any object removal event |
| `s3:ObjectRemoved:Delete` | Object permanently deleted |
| `s3:ObjectRemoved:DeleteMarkerCreated` | Delete marker created (versioned bucket) |

### Object Access Events

| Event Type | Description |
|------------|-------------|
| `s3:ObjectAccessed:*` | Any object access event |
| `s3:ObjectAccessed:Get` | Object downloaded |
| `s3:ObjectAccessed:Head` | Object metadata retrieved |

### Object Restore Events

| Event Type | Description |
|------------|-------------|
| `s3:ObjectRestore:Post` | Restore from archive initiated |
| `s3:ObjectRestore:Completed` | Restore from archive completed |

### Replication Events

| Event Type | Description |
|------------|-------------|
| `s3:Replication:OperationFailedReplication` | Replication failed |
| `s3:Replication:OperationCompletedReplication` | Replication completed |

## Supported Targets

### Webhook (HTTP/HTTPS)

Deliver events to any HTTP endpoint:

```yaml
notifications:
  targets:
    - name: my-webhook
      type: webhook
      url: https://api.example.com/s3-events
      headers:
        Authorization: "Bearer ${WEBHOOK_TOKEN}"
        Content-Type: "application/json"
      timeout: 30s
      retry_count: 3
      retry_delay: 5s
```

### Apache Kafka

Stream events to Kafka topics for high-throughput processing:

```yaml
notifications:
  targets:
    - name: kafka-events
      type: kafka
      brokers:
        - kafka1.example.com:9092
        - kafka2.example.com:9092
      topic: nebulaio-events
      sasl:
        enabled: true
        mechanism: SCRAM-SHA-256
        username: ${KAFKA_USER}
        password: ${KAFKA_PASS}
      tls:
        enabled: true
        ca_cert: /etc/ssl/kafka-ca.pem
```

### AMQP (RabbitMQ)

Publish events to RabbitMQ exchanges:

```yaml
notifications:
  targets:
    - name: rabbitmq-events
      type: amqp
      url: amqp://user:pass@rabbitmq.example.com:5672/vhost
      exchange: s3-events
      exchange_type: topic
      routing_key: nebulaio.objects
      durable: true
      mandatory: true
```

### Redis (Pub/Sub and Streams)

Publish events to Redis channels or streams:

```yaml
notifications:
  targets:
    - name: redis-pubsub
      type: redis
      address: redis.example.com:6379
      password: ${REDIS_PASSWORD}
      mode: pubsub          # pubsub | stream
      channel: s3-events    # For pubsub mode
      # stream: s3-events   # For stream mode
      # max_len: 10000      # Stream max length
```

### NATS

Publish events to NATS subjects:

```yaml
notifications:
  targets:
    - name: nats-events
      type: nats
      servers:
        - nats://nats1.example.com:4222
        - nats://nats2.example.com:4222
      subject: nebulaio.events
      credentials: /etc/nats/creds.txt
      tls:
        enabled: true
```

## Bucket Notification Configuration

### Using AWS CLI

```bash
# Create notification configuration
cat > notification.json << 'EOF'
{
  "TopicConfigurations": [],
  "QueueConfigurations": [],
  "LambdaFunctionConfigurations": [],
  "EventBridgeConfiguration": {},
  "CloudFunctionConfigurations": [
    {
      "Id": "upload-processor",
      "CloudFunction": "arn:nebulaio:events::webhook/my-webhook",
      "Events": ["s3:ObjectCreated:*"],
      "Filter": {
        "Key": {
          "FilterRules": [
            {"Name": "prefix", "Value": "uploads/"},
            {"Name": "suffix", "Value": ".json"}
          ]
        }
      }
    }
  ]
}
EOF

aws s3api put-bucket-notification-configuration \
  --endpoint-url http://localhost:9000 \
  --bucket my-bucket \
  --notification-configuration file://notification.json
```

### View Configuration

```bash
aws s3api get-bucket-notification-configuration \
  --endpoint-url http://localhost:9000 \
  --bucket my-bucket
```

### Delete Configuration

```bash
aws s3api put-bucket-notification-configuration \
  --endpoint-url http://localhost:9000 \
  --bucket my-bucket \
  --notification-configuration '{}'
```

### Using Admin API

```bash
# Configure bucket notifications
curl -X PUT "http://localhost:9000/admin/buckets/my-bucket/notifications" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "rules": [
      {
        "id": "image-processor",
        "events": ["s3:ObjectCreated:*"],
        "target": "my-webhook",
        "filter": {
          "prefix": "images/",
          "suffix": ".jpg"
        }
      }
    ]
  }'
```

## Event Filtering

### Prefix Filtering

Match objects with a specific key prefix:

```json
{
  "Filter": {
    "Key": {
      "FilterRules": [
        {"Name": "prefix", "Value": "logs/2024/"}
      ]
    }
  }
}
```

### Suffix Filtering

Match objects with a specific file extension:

```json
{
  "Filter": {
    "Key": {
      "FilterRules": [
        {"Name": "suffix", "Value": ".csv"}
      ]
    }
  }
}
```

### Combined Filtering

Match objects with both prefix and suffix:

```json
{
  "Filter": {
    "Key": {
      "FilterRules": [
        {"Name": "prefix", "Value": "data/"},
        {"Name": "suffix", "Value": ".parquet"}
      ]
    }
  }
}
```

### Multiple Rules Example

```bash
cat > multi-notification.json << 'EOF'
{
  "CloudFunctionConfigurations": [
    {
      "Id": "process-images",
      "CloudFunction": "arn:nebulaio:events::webhook/image-processor",
      "Events": ["s3:ObjectCreated:*"],
      "Filter": {
        "Key": {
          "FilterRules": [
            {"Name": "prefix", "Value": "images/"},
            {"Name": "suffix", "Value": ".jpg"}
          ]
        }
      }
    },
    {
      "Id": "archive-logs",
      "CloudFunction": "arn:nebulaio:events::kafka/log-archiver",
      "Events": ["s3:ObjectCreated:Put"],
      "Filter": {
        "Key": {
          "FilterRules": [
            {"Name": "prefix", "Value": "logs/"}
          ]
        }
      }
    },
    {
      "Id": "delete-audit",
      "CloudFunction": "arn:nebulaio:events::webhook/audit-webhook",
      "Events": ["s3:ObjectRemoved:*"]
    }
  ]
}
EOF

aws s3api put-bucket-notification-configuration \
  --endpoint-url http://localhost:9000 \
  --bucket my-bucket \
  --notification-configuration file://multi-notification.json
```

## Event Payload Format

### Standard Event Structure

```json
{
  "Records": [
    {
      "eventVersion": "2.1",
      "eventSource": "nebulaio:s3",
      "awsRegion": "us-east-1",
      "eventTime": "2024-01-15T10:30:00.000Z",
      "eventName": "ObjectCreated:Put",
      "userIdentity": {
        "principalId": "AIDEXAMPLE123"
      },
      "requestParameters": {
        "sourceIPAddress": "192.168.1.100"
      },
      "responseElements": {
        "x-amz-request-id": "req_abc123",
        "x-amz-id-2": "xyz789"
      },
      "s3": {
        "s3SchemaVersion": "1.0",
        "configurationId": "upload-processor",
        "bucket": {
          "name": "my-bucket",
          "ownerIdentity": {
            "principalId": "OWNER123"
          },
          "arn": "arn:aws:s3:::my-bucket"
        },
        "object": {
          "key": "uploads/document.pdf",
          "size": 1048576,
          "eTag": "d41d8cd98f00b204e9800998ecf8427e",
          "contentType": "application/pdf",
          "versionId": "v1.0",
          "sequencer": "00A1B2C3D4E5F678"
        }
      }
    }
  ]
}
```

### Delete Event Example

```json
{
  "Records": [
    {
      "eventVersion": "2.1",
      "eventSource": "nebulaio:s3",
      "eventTime": "2024-01-15T11:00:00.000Z",
      "eventName": "ObjectRemoved:Delete",
      "s3": {
        "bucket": {
          "name": "my-bucket"
        },
        "object": {
          "key": "old-file.txt",
          "versionId": "null",
          "sequencer": "00A1B2C3D4E5F679"
        }
      }
    }
  ]
}
```

## Monitoring Event Delivery

### Prometheus Metrics

```
# Events published by type
nebulaio_events_published_total{bucket="...",event_type="...",target="..."}

# Event delivery success/failure
nebulaio_events_delivered_total{target="...",status="success|failure"}

# Event delivery latency
nebulaio_events_delivery_latency_seconds{target="..."}

# Event queue depth
nebulaio_events_queue_depth{target="..."}

# Failed deliveries pending retry
nebulaio_events_retry_queue_depth{target="..."}
```

### Admin API Endpoints

```bash
# Get event delivery statistics
curl -X GET "http://localhost:9000/admin/events/stats" \
  -H "Authorization: Bearer $TOKEN"
```

Response:
```json
{
  "total_events": 150000,
  "delivered": 149500,
  "failed": 500,
  "pending": 250,
  "by_target": {
    "my-webhook": {
      "delivered": 50000,
      "failed": 100,
      "avg_latency_ms": 45
    },
    "kafka-events": {
      "delivered": 99500,
      "failed": 400,
      "avg_latency_ms": 12
    }
  }
}
```

```bash
# List failed deliveries
curl -X GET "http://localhost:9000/admin/events/failed?limit=10" \
  -H "Authorization: Bearer $TOKEN"

# Retry failed deliveries
curl -X POST "http://localhost:9000/admin/events/retry-failed" \
  -H "Authorization: Bearer $TOKEN"

# Check target health
curl -X GET "http://localhost:9000/admin/events/targets/health" \
  -H "Authorization: Bearer $TOKEN"
```

### Alerting Rules

```yaml
groups:
  - name: event-notifications
    rules:
      - alert: EventDeliveryFailureHigh
        expr: rate(nebulaio_events_delivered_total{status="failure"}[5m]) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High event delivery failure rate"

      - alert: EventQueueBacklog
        expr: nebulaio_events_queue_depth > 10000
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: "Event queue backlog exceeding threshold"

      - alert: EventTargetUnreachable
        expr: nebulaio_events_target_healthy == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Event notification target unreachable"
```

## Best Practices

1. **Use specific event types**: Subscribe to specific events rather than wildcards when possible
2. **Apply filters**: Use prefix/suffix filters to reduce noise and processing overhead
3. **Handle idempotency**: Design consumers to handle duplicate events gracefully
4. **Monitor queue depth**: Alert on growing queues to detect delivery issues early
5. **Configure retries**: Set appropriate retry policies for transient failures
6. **Secure endpoints**: Use TLS and authentication for webhook targets
7. **Test configurations**: Validate notification rules before production deployment
8. **Use message queues**: Prefer Kafka/AMQP for high-volume workloads over webhooks

## Troubleshooting

### Events Not Delivering

1. Verify notification configuration exists:
   ```bash
   aws s3api get-bucket-notification-configuration \
     --endpoint-url http://localhost:9000 \
     --bucket my-bucket
   ```

2. Check target health:
   ```bash
   curl -X GET "http://localhost:9000/admin/events/targets/health" \
     -H "Authorization: Bearer $TOKEN"
   ```

3. Review filter rules match your object keys

### High Latency

1. Check target endpoint performance
2. Increase worker count in configuration:
   ```yaml
   notifications:
     workers: 20
     batch_size: 100
   ```

### Duplicate Events

1. Events may be delivered more than once during retries
2. Implement idempotency using the event `sequencer` field
3. Use the `eventTime` and object `versionId` for deduplication
