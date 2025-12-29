# NebulaIO Policy System

NebulaIO provides a comprehensive policy system for managing object tiering, caching, and lifecycle operations. Policies enable automated data management based on access patterns, age, capacity thresholds, and custom rules.

## Table of Contents

- [Overview](#overview)
- [Policy Types](#policy-types)
- [Policy Structure](#policy-structure)
- [Triggers](#triggers)
- [Actions](#actions)
- [Selectors](#selectors)
- [Anti-Thrash Protection](#anti-thrash-protection)
- [Scheduling](#scheduling)
- [Distributed Execution](#distributed-execution)
- [S3 Lifecycle Compatibility](#s3-lifecycle-compatibility)
- [Examples](#examples)
- [API Reference](#api-reference)

## Overview

The policy system supports multiple execution modes:

| Mode | Description | Use Cases |
| ------ | ------------- | ----------- |
| **Scheduled** | Runs on a cron schedule | Daily archival, weekly cleanup |
| **Real-time** | Evaluates on each access | Hot promotion, cache population |
| **Threshold** | Triggers on capacity limits | Capacity-based demotion |
| **S3 Lifecycle** | S3-compatible rules | AWS compatibility |

## Policy Types

### Scheduled Policies

Scheduled policies run on a cron-like schedule, ideal for batch operations:

```yaml

type: scheduled
triggers:
  - type: cron
    cron:
      expression: "0 2 * * *"  # 2 AM daily
      timezone: "America/New_York"

```bash

### Real-time Policies

Real-time policies evaluate on every object access:

```yaml

type: realtime
triggers:
  - type: access
    access:
      direction: up
      promoteOnAnyRead: true
      promoteToCache: true

```bash

### Threshold Policies

Threshold policies trigger when tier capacity crosses defined limits:

```yaml

type: threshold
triggers:
  - type: capacity
    capacity:
      tier: hot
      highWatermark: 80  # Trigger at 80% full
      lowWatermark: 70   # Stop at 70%

```bash

## Policy Structure

A complete policy definition:

```yaml

id: "archive-inactive-logs"
name: "Archive Inactive Log Files"
description: "Move log files not accessed for 30 days to cold tier"
type: scheduled
scope: bucket
enabled: true
priority: 50  # Lower = higher priority

selector:
  buckets:
    - "logs-*"
  prefixes:
    - "access/"
    - "error/"
  suffixes:
    - ".log"
    - ".gz"
  minSize: 1024
  maxSize: 1073741824  # 1GB
  contentTypes:
    - "text/*"
    - "application/gzip"
  currentTiers:
    - hot
    - warm
  tags:
    environment: production

triggers:
  - type: age
    age:
      daysSinceAccess: 30

actions:
  - type: transition
    transition:
      targetTier: cold
      targetStorageClass: GLACIER
      compression: true
      compressionAlgorithm: zstd

antiThrash:
  enabled: true
  minTimeInTier: "7d"
  cooldownAfterTransition: "24h"
  maxTransitionsPerDay: 1

schedule:
  enabled: true
  maintenanceWindows:
    - name: "nightly"
      daysOfWeek: [0, 1, 2, 3, 4, 5, 6]
      startTime: "02:00"
      endTime: "06:00"
  blackoutWindows:
    - name: "business-hours"
      daysOfWeek: [1, 2, 3, 4, 5]  # Mon-Fri
      startTime: "09:00"
      endTime: "17:00"
  timezone: "America/New_York"

rateLimit:
  enabled: true
  maxObjectsPerSecond: 100
  maxBytesPerSecond: 104857600  # 100 MB/s
  maxConcurrent: 4
  burstSize: 10

distributed:
  enabled: true
  shardByBucket: true

```bash

## Triggers

### Age Triggers

Based on object age:

```yaml

triggers:
  - type: age
    age:
      daysSinceCreation: 90      # Days since object was created
      daysSinceModification: 60  # Days since last modification
      daysSinceAccess: 30        # Days since last access
      hoursSinceAccess: 1        # For real-time policies

```bash

### Access Triggers

Based on access patterns:

```yaml

triggers:
  - type: access
    access:
      operation: GET
      direction: up              # up = promote, down = demote
      countThreshold: 10         # Access count threshold
      periodMinutes: 60          # Time window for counting
      promoteOnAnyRead: true     # Promote immediately on read
      promoteToCache: true       # Also load into cache

```bash

### Capacity Triggers

Based on tier capacity:

```yaml

triggers:
  - type: capacity
    capacity:
      tier: hot
      highWatermark: 80         # Percentage trigger
      lowWatermark: 70          # Stop condition
      bytesThreshold: 1099511627776  # 1TB absolute

```bash

### Frequency Triggers

Based on access frequency patterns:

```yaml

triggers:
  - type: frequency
    frequency:
      minAccessesPerDay: 1.0    # Hot threshold
      maxAccessesPerDay: 0.1    # Cold threshold
      slidingWindowDays: 30
      pattern: "declining"       # declining, increasing, stable

```bash

### Cron Triggers

Schedule-based:

```yaml

triggers:
  - type: cron
    cron:
      expression: "0 2 * * 0"   # Every Sunday at 2 AM
      timezone: "UTC"

```bash

## Actions

### Transition Action

Move objects between tiers:

```yaml

actions:
  - type: transition
    transition:
      targetTier: cold
      targetStorageClass: GLACIER
      preserveCopy: false        # Delete from source
      compression: true
      compressionAlgorithm: zstd

```bash

### Delete Action

Remove objects:

```yaml

actions:
  - type: delete
    delete:
      daysAfterTransition: 365   # Delete after 1 year in cold
      expireDeleteMarkers: true  # For versioned buckets
      nonCurrentVersionDays: 30

```bash

### Replicate Action

Copy to another location:

```yaml

actions:
  - type: replicate
    replicate:
      destination: "s3://backup-bucket/archive/"
      storageClass: GLACIER

```bash

### Notify Action

Send webhooks:

```yaml

actions:
  - type: notify
    notify:
      endpoint: "https://api.example.com/webhook/tiering"
      events:
        - "transition.complete"
        - "transition.failed"

```bash

## Selectors

### Bucket Selection

```yaml

selector:
  buckets:
    - "prod-*"        # Wildcard match
    - "staging"       # Exact match
    - "logs-202*"     # Pattern match

```bash

### Prefix/Suffix Selection

```yaml

selector:
  prefixes:
    - "data/"
    - "reports/"
  suffixes:
    - ".parquet"
    - ".csv"

```bash

### Size Selection

```yaml

selector:
  minSize: 1048576      # 1MB minimum
  maxSize: 10737418240  # 10GB maximum

```bash

### Content Type Selection

```yaml

selector:
  contentTypes:
    - "image/*"         # All images
    - "video/mp4"       # Specific type
    - "application/*"

```bash

### Tag Selection

```yaml

selector:
  tags:
    environment: production
    retention: long-term

```bash

### Tier Filtering

```yaml

selector:
  currentTiers:          # Only process objects in these tiers
    - hot
    - warm
  excludeTiers:          # Skip objects in these tiers
    - archive

```bash

## Anti-Thrash Protection

Prevent objects from oscillating between tiers:

```yaml

antiThrash:
  enabled: true
  minTimeInTier: "7d"              # Must stay 7 days before moving
  cooldownAfterTransition: "24h"   # Wait 24h after any transition
  maxTransitionsPerDay: 2          # Max 2 transitions per day
  stickinessWeight: 0.1            # Resistance to movement (0-1)
  requireConsecutiveEvaluations: 3 # Must trigger 3 times

```bash

## Scheduling

### Maintenance Windows

Define when policies can execute:

```yaml

schedule:
  enabled: true
  maintenanceWindows:
    - name: "nightly"
      daysOfWeek: [0, 1, 2, 3, 4, 5, 6]  # All days
      startTime: "02:00"
      endTime: "06:00"
  timezone: "America/New_York"

```bash

### Blackout Windows

Define when policies cannot execute:

```yaml

schedule:
  blackoutWindows:
    - name: "business-hours"
      daysOfWeek: [1, 2, 3, 4, 5]  # Mon-Fri
      startTime: "09:00"
      endTime: "17:00"

```bash

## Rate Limiting

Control execution rate:

```yaml

rateLimit:
  enabled: true
  maxObjectsPerSecond: 100
  maxBytesPerSecond: 104857600  # 100 MB/s
  maxConcurrent: 4              # Parallel operations
  burstSize: 10                 # Allow bursts

```bash

## Distributed Execution

For clustered deployments:

```yaml

distributed:
  enabled: true
  shardByBucket: true            # Hash by bucket
  requireLeaderElection: false
  coordinationKey: "tiering"

```text

Buckets are distributed across nodes using consistent hashing, ensuring:

- Each bucket is processed by exactly one node
- Load is balanced across the cluster
- Node failures are handled gracefully

## S3 Lifecycle Compatibility

NebulaIO supports S3 lifecycle configuration format:

```xml

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

```text

S3 lifecycle rules are automatically converted to NebulaIO policies:

```bash

# Set lifecycle via S3 API
aws s3api put-bucket-lifecycle-configuration \
  --bucket my-bucket \
  --lifecycle-configuration file://lifecycle.json \
  --endpoint-url http://localhost:7070

# Get lifecycle
aws s3api get-bucket-lifecycle-configuration \
  --bucket my-bucket \
  --endpoint-url http://localhost:7070

```bash

## Examples

### Example 1: Archive Old Logs

Move log files older than 30 days to cold storage:

```yaml

id: "archive-old-logs"
name: "Archive Old Logs"
type: scheduled
scope: bucket
enabled: true
priority: 100

selector:
  buckets: ["logs"]
  suffixes: [".log", ".log.gz"]

triggers:
  - type: age
    age:
      daysSinceCreation: 30

actions:
  - type: transition
    transition:
      targetTier: cold
      compression: true

schedule:
  enabled: true
  maintenanceWindows:
    - name: "nightly"
      daysOfWeek: [0, 1, 2, 3, 4, 5, 6]
      startTime: "02:00"
      endTime: "04:00"

```bash

### Example 2: Hot Promotion on Read

Promote frequently accessed objects to hot tier:

```yaml

id: "promote-hot"
name: "Promote Hot Objects"
type: realtime
scope: global
enabled: true
priority: 10

selector:
  excludeTiers: [hot]

triggers:
  - type: access
    access:
      direction: up
      promoteOnAnyRead: true
      promoteToCache: true

actions:
  - type: transition
    transition:
      targetTier: hot

antiThrash:
  enabled: true
  minTimeInTier: "1h"

```bash

### Example 3: Capacity-Based Demotion

Move objects to warm when hot tier is 80% full:

```yaml

id: "hot-capacity"
name: "Hot Tier Capacity Management"
type: threshold
scope: global
enabled: true
priority: 5

selector:
  currentTiers: [hot]

triggers:
  - type: capacity
    capacity:
      tier: hot
      highWatermark: 80
      lowWatermark: 70

actions:
  - type: transition
    transition:
      targetTier: warm

rateLimit:
  enabled: true
  maxObjectsPerSecond: 50

```bash

### Example 4: Intelligent Tiering

Move declining objects down, trending objects up:

```yaml

id: "intelligent-tiering"
name: "Intelligent Access-Based Tiering"
type: scheduled
scope: global
enabled: true

triggers:
  - type: frequency
    frequency:
      maxAccessesPerDay: 0.1
      slidingWindowDays: 14
      pattern: "declining"

actions:
  - type: transition
    transition:
      targetTier: cold

antiThrash:
  enabled: true
  minTimeInTier: "7d"
  requireConsecutiveEvaluations: 2

```bash

## API Reference

### Create Policy

```bash

curl -X POST http://localhost:7070/admin/policies \
  -H "Content-Type: application/json" \
  -d @policy.json

```bash

### List Policies

```bash

curl http://localhost:7070/admin/policies

```bash

### Get Policy

```bash

curl http://localhost:7070/admin/policies/{policy-id}

```bash

### Update Policy

```bash

curl -X PUT http://localhost:7070/admin/policies/{policy-id} \
  -H "Content-Type: application/json" \
  -d @policy.json

```bash

### Delete Policy

```bash

curl -X DELETE http://localhost:7070/admin/policies/{policy-id}

```bash

### Get Policy Stats

```bash

curl http://localhost:7070/admin/policies/{policy-id}/stats

```bash

### Get Object Access Stats

```bash

curl http://localhost:7070/admin/access-stats/{bucket}/{key}

```bash

### Manually Transition Object

```bash

curl -X POST http://localhost:7070/admin/transition \
  -H "Content-Type: application/json" \
  -d '{"bucket": "my-bucket", "key": "my-key", "targetTier": "cold"}'

```

## Best Practices

1. **Start with scheduled policies** - Use scheduled policies for predictable workloads
2. **Use anti-thrash protection** - Prevent object oscillation between tiers
3. **Set appropriate rate limits** - Avoid overwhelming storage systems
4. **Use maintenance windows** - Schedule heavy operations during off-peak hours
5. **Monitor policy stats** - Track policy effectiveness and adjust thresholds
6. **Test with small prefixes** - Validate policies on subset before global rollout
7. **Use priority ordering** - Ensure critical policies execute first
