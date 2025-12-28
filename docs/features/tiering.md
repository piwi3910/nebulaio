# Storage Tiering

NebulaIO provides intelligent storage tiering to optimize costs while maintaining performance. Objects automatically transition between Hot, Warm, Cold, and Archive tiers based on access patterns and lifecycle policies.

## Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Storage Tiering Flow                               │
├─────────────────────────────────────────────────────────────────────────────┤
│   New Upload ──► HOT (NVMe) ──30d──► WARM (SSD) ──90d──► COLD (HDD)        │
│                      │                                        │              │
│                      ▼                                   365d │              │
│               ┌─────────────┐                                 ▼              │
│               │ DRAM Cache  │                          ARCHIVE (Tape)       │
│               └─────────────┘                                                │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Tier Definitions

| Tier | Storage Class | Media | Latency | Cost | Use Case |
|------|--------------|-------|---------|------|----------|
| Hot | STANDARD | NVMe/SSD | < 1ms | $$$$ | Active workloads, ML training |
| Warm | STANDARD_IA | SSD/HDD | 5-20ms | $$$ | Moderate access, recent data |
| Cold | GLACIER_IR | HDD/NAS | 50-200ms | $$ | Infrequent access, backups |
| Archive | DEEP_ARCHIVE | Tape | Hours | $ | Compliance, long-term retention |

### Configuration

```yaml
tiering:
  enabled: true
  tiers:
    hot:
      storage_class: STANDARD
      media_type: nvme
      cache_eligible: true
    warm:
      storage_class: STANDARD_IA
      media_type: ssd
    cold:
      storage_class: GLACIER_IR
      media_type: hdd
    archive:
      storage_class: DEEP_ARCHIVE
      media_type: tape
      min_retention_days: 180
```

## Placement Groups

Placement groups define how nodes are organized for distributed storage operations. Within a placement group, erasure coding and tiering operate locally. Cross-placement group operations are used for disaster recovery replication.

### Architecture

```
┌─────────────────────────────────────┐   ┌─────────────────────────────────────┐
│   Placement Group 1 (Datacenter A)  │   │   Placement Group 2 (Datacenter B)  │
│  ┌─────┐  ┌─────┐  ┌─────┐          │   │  ┌─────┐  ┌─────┐  ┌─────┐          │
│  │Node1│  │Node2│  │Node3│          │   │  │Node4│  │Node5│  │Node6│          │
│  └──┬──┘  └──┬──┘  └──┬──┘          │   │  └──┬──┘  └──┬──┘  └──┬──┘          │
│     │        │        │              │   │     │        │        │              │
│     └────────┼────────┘              │   │     └────────┼────────┘              │
│              │                       │   │              │                       │
│   Erasure coding (local shards)      │   │   Erasure coding (local shards)      │
│   Tiering (hot→warm→cold)            │   │   Tiering (hot→warm→cold)            │
└──────────────────┬───────────────────┘   └──────────────────┬───────────────────┘
                   │                                          │
                   └──────── DR Replication ──────────────────┘
                          (full object copies)
```

### Key Concepts

| Concept | Scope | Description |
|---------|-------|-------------|
| **Placement Group** | Datacenter | Nodes in the same datacenter that share local storage operations |
| **Erasure Coding** | Within PG | Reed-Solomon shards distributed across nodes in the same placement group |
| **Tiering** | Within PG | Hot/warm/cold transitions happen locally within a placement group |
| **DR Replication** | Cross-PG | Full object copies replicated to other placement groups for disaster recovery |

### Configuration

```yaml
storage:
  placement_groups:
    # This node's placement group
    local_group_id: pg-dc1

    # Minimum nodes for distributed erasure coding
    min_nodes_for_erasure: 3

    # Placement groups to replicate to for DR
    replication_targets:
      - pg-dc2

    # Define all placement groups
    groups:
      - id: pg-dc1
        name: "Datacenter 1 - US East"
        datacenter: dc1
        region: us-east-1
        min_nodes: 3
        max_nodes: 10

      - id: pg-dc2
        name: "Datacenter 2 - US West"
        datacenter: dc2
        region: us-west-1
        min_nodes: 3
        max_nodes: 10
```

### Single Node vs. Multi-Node

| Mode | Behavior |
|------|----------|
| **Single Node** | All policies apply to local storage only. Erasure coding creates shards on local disk. |
| **Multi-Node (Same PG)** | Erasure shards distributed across nodes in the placement group. Tiering policies coordinated. |
| **Multi-Node (Cross-PG)** | Same as above, plus DR replication creates full object copies in remote placement groups. |

### Replication Policies

Configure cross-placement group replication for disaster recovery:

```yaml
storage:
  default_redundancy:
    enabled: true
    data_shards: 10
    parity_shards: 4
    placement_policy: spread

    # DR: replicate to 1 other placement group
    replication_factor: 1
    replication_targets:
      - pg-dc2

# Per-bucket override
buckets:
  critical-data:
    redundancy:
      replication_factor: 2  # Replicate to 2 placement groups
      replication_targets:
        - pg-dc2
        - pg-dc3
```

## Physical Device Configuration

NebulaIO supports assigning different physical devices (NVMe, SSD, HDD) to different storage tiers for optimal performance and cost efficiency.

### Raw Block Device Tiering

For maximum performance, use raw block devices directly without filesystem overhead:

```yaml
storage:
  backend: volume

  volume:
    raw_devices:
      enabled: true

      # Assign devices to tiers
      devices:
        # Hot tier - NVMe for active data
        - path: /dev/nvme0n1
          tier: hot

        - path: /dev/nvme1n1
          tier: hot

        # Warm tier - SATA SSDs for moderate access
        - path: /dev/sda
          tier: warm

        - path: /dev/sdb
          tier: warm

        # Cold tier - HDDs for infrequent access
        - path: /dev/sdc
          tier: cold

        - path: /dev/sdd
          tier: cold

tiering:
  enabled: true
  policies:
    - name: standard-lifecycle
      transitions:
        - days: 7
          from_tier: hot
          to_tier: warm
        - days: 30
          from_tier: warm
          to_tier: cold
```

### File-Based Volume Tiering

For environments where raw device access isn't possible, use directory-based tiering:

```yaml
storage:
  backend: volume

  volume:
    # Define volume directories per tier
    tier_directories:
      hot: /mnt/nvme/nebulaio       # NVMe mount point
      warm: /mnt/ssd/nebulaio       # SSD mount point
      cold: /mnt/hdd/nebulaio       # HDD mount point

    max_volume_size: 34359738368     # 32GB per volume

tiering:
  enabled: true
```

### Tier Capacity Planning

| Tier | Recommended Media | Typical Capacity | Data Retention |
|------|------------------|------------------|----------------|
| Hot | NVMe | 5-10% of total | < 7 days since access |
| Warm | SSD | 20-30% of total | 7-30 days since access |
| Cold | HDD | 60-70% of total | > 30 days since access |

### Device Discovery

```bash
# List available block devices
nebulaio-cli storage device discover
# DEVICE         TYPE    SIZE      MOUNT     ELIGIBLE
# /dev/nvme0n1   NVMe    1.0 TB    -         ✓
# /dev/nvme1n1   NVMe    1.0 TB    -         ✓
# /dev/sda       SSD     4.0 TB    -         ✓
# /dev/sdb       SSD     4.0 TB    /data     ✗ (mounted)
# /dev/sdc       HDD     16.0 TB   -         ✓

# Initialize device for a specific tier
nebulaio-cli storage device init /dev/nvme0n1 --tier hot

# View tier assignment
nebulaio-cli storage tier devices
# TIER    DEVICES                    TOTAL CAPACITY
# hot     /dev/nvme0n1, /dev/nvme1n1 2.0 TB
# warm    /dev/sda                    4.0 TB
# cold    /dev/sdc, /dev/sdd          32.0 TB
```

---

## Advanced Policy System

NebulaIO's advanced policy system provides fine-grained control over object lifecycle management with support for multiple policy types, complex triggers, and intelligent automation.

### Policy Types

NebulaIO supports four distinct policy types, each optimized for different use cases:

| Policy Type | Execution Model | Use Case |
|-------------|-----------------|----------|
| Scheduled | Cron-based or maintenance windows | Regular batch transitions, off-peak processing |
| Realtime | Event-driven | Immediate response to access patterns |
| Threshold | Capacity/metric-based | Storage management, quota enforcement |
| S3 Lifecycle | S3 API compatibility | AWS S3 migration, existing tooling |

#### Scheduled Policies

Scheduled policies execute at defined intervals using cron expressions or during maintenance windows.

```yaml
tiering:
  policies:
    - name: nightly-archival
      type: scheduled
      schedule:
        cron: "0 2 * * *"           # Run at 2 AM daily
        timezone: "America/New_York"
      triggers:
        - type: age
          days_since_access: 90
      actions:
        - type: transition
          target_tier: archive
      filters:
        bucket_patterns:
          - "logs-*"
          - "backups-*"
        min_object_size: 1MB

    - name: weekly-cleanup
      type: scheduled
      schedule:
        cron: "0 3 * * 0"           # Run at 3 AM every Sunday
        timezone: "UTC"
      triggers:
        - type: age
          days_since_creation: 365
      actions:
        - type: delete
      filters:
        bucket_patterns:
          - "temp-*"
```

#### Realtime Policies

Realtime policies respond immediately to events such as object access, creation, or modification.

```yaml
tiering:
  policies:
    - name: hot-data-promotion
      type: realtime
      events:
        - object_accessed
        - object_downloaded
      triggers:
        - type: access_count
          threshold: 10
          window: 1h
      actions:
        - type: transition
          target_tier: hot
      priority: high

    - name: immediate-archive-notification
      type: realtime
      events:
        - tier_transitioned
      triggers:
        - type: event_match
          from_tier: warm
          to_tier: archive
      actions:
        - type: notify
          webhook: "https://ops.example.com/webhooks/tier-events"
```

#### Threshold Policies

Threshold policies activate when storage metrics exceed defined limits.

```yaml
tiering:
  policies:
    - name: hot-tier-pressure-relief
      type: threshold
      triggers:
        - type: capacity
          tier: hot
          threshold_percent: 85
          direction: above
      actions:
        - type: transition
          target_tier: warm
          selection:
            strategy: lru              # Least Recently Used
            batch_size: 1000
            max_bytes: 100GB
      cooldown: 30m                    # Wait 30 minutes before re-triggering

    - name: cold-tier-expansion
      type: threshold
      triggers:
        - type: metric
          metric_name: nebulaio_tier_bytes_total
          tier: cold
          threshold: 10TB
          direction: above
      actions:
        - type: notify
          webhook: "https://ops.example.com/webhooks/capacity-alerts"
          template: capacity_warning
```

#### S3 Lifecycle Policies

S3-compatible lifecycle policies for seamless migration from AWS S3.

```yaml
tiering:
  s3_lifecycle:
    enabled: true
    policies:
      - id: "archive-logs"
        status: Enabled
        filter:
          prefix: "logs/"
          tags:
            environment: production
        transitions:
          - days: 30
            storage_class: STANDARD_IA
          - days: 90
            storage_class: GLACIER_IR
          - days: 365
            storage_class: DEEP_ARCHIVE
        expiration:
          days: 2555                   # 7 years
        noncurrent_version_transitions:
          - noncurrent_days: 30
            storage_class: GLACIER_IR
        noncurrent_version_expiration:
          noncurrent_days: 365
```

### Policy Priority and Conflict Resolution

When multiple policies apply to the same object, NebulaIO uses priority-based conflict resolution:

```yaml
tiering:
  policy_resolution:
    mode: priority                     # priority | first_match | merge
    default_priority: 100

  policies:
    - name: compliance-retention
      priority: 10                     # Lower number = higher priority
      # ... policy definition

    - name: cost-optimization
      priority: 50
      # ... policy definition

    - name: default-lifecycle
      priority: 100
      # ... policy definition
```

---

## Policy Triggers

Triggers define the conditions that activate a policy. Multiple triggers can be combined using logical operators.

### Age-Based Triggers

Age-based triggers evaluate objects based on time since creation, modification, or last access.

```yaml
tiering:
  policies:
    - name: age-based-transitions
      triggers:
        # Days since object creation
        - type: age
          days_since_creation: 30

        # Days since last modification
        - type: age
          days_since_modification: 60

        # Days since last access (GET/HEAD)
        - type: age
          days_since_access: 90

        # Combined age conditions with AND logic
        - type: compound
          operator: AND
          conditions:
            - type: age
              days_since_creation: 7
            - type: age
              days_since_access: 3
```

### Access-Based Triggers

Access-based triggers evaluate objects based on access patterns and frequency.

```yaml
tiering:
  policies:
    - name: access-based-tiering
      triggers:
        # Minimum access count to stay in tier
        - type: access_count
          min_accesses: 5
          window: 7d
          action_when: below           # Trigger when below threshold

        # Access frequency threshold
        - type: access_frequency
          frequency: "10/day"
          action_when: above

        # Access pattern detection
        - type: access_pattern
          pattern: declining           # stable | declining | increasing | burst
          window: 30d
          sensitivity: medium          # low | medium | high

        # Last access time
        - type: last_access
          hours_ago: 168               # 7 days
```

### Capacity-Based Triggers

Capacity triggers activate based on storage tier utilization or object counts.

```yaml
tiering:
  policies:
    - name: capacity-management
      triggers:
        # Tier usage percentage
        - type: capacity
          tier: hot
          threshold_percent: 80
          direction: above

        # Absolute capacity threshold
        - type: capacity
          tier: warm
          threshold_bytes: 500GB
          direction: above

        # Object count threshold
        - type: object_count
          tier: hot
          threshold: 1000000
          direction: above

        # Per-bucket capacity
        - type: bucket_capacity
          bucket: "ml-datasets"
          threshold_percent: 90
          direction: above
```

### Cron-Based Triggers

Cron triggers activate at scheduled times using standard cron expressions.

```yaml
tiering:
  policies:
    - name: scheduled-maintenance
      triggers:
        # Standard cron expression
        - type: cron
          expression: "0 2 * * *"      # 2 AM daily
          timezone: "America/Los_Angeles"

        # Predefined schedules
        - type: cron
          preset: daily_off_peak       # Predefined: 2-6 AM local time

        # Multiple schedules
        - type: cron
          expressions:
            - "0 2 * * 1-5"            # Weekdays at 2 AM
            - "0 4 * * 0,6"            # Weekends at 4 AM
```

### Composite Triggers

Combine multiple triggers with logical operators for complex conditions.

```yaml
tiering:
  policies:
    - name: complex-trigger-policy
      triggers:
        - type: composite
          operator: AND
          conditions:
            - type: age
              days_since_access: 30
            - type: access_count
              max_accesses: 5
              window: 30d
            - type: composite
              operator: OR
              conditions:
                - type: capacity
                  tier: hot
                  threshold_percent: 70
                  direction: above
                - type: object_size
                  min_size: 100MB
```

### Trigger CLI Commands

```bash
# List triggers for a policy
nebulaio-cli policy triggers list --policy nightly-archival

# Test trigger evaluation
nebulaio-cli policy triggers test --policy nightly-archival --bucket my-bucket
# Objects matching triggers: 12,456
# Sample objects:
#   - logs/2024/01/app.log (age: 45d, accesses: 0)
#   - backups/weekly/backup-2024-01.tar (age: 60d, accesses: 2)

# View trigger history
nebulaio-cli policy triggers history --policy hot-tier-pressure-relief --days 7
# TIMESTAMP           TRIGGER TYPE    CONDITION           OBJECTS
# 2024-01-15 02:00    capacity        hot > 85%           8,234
# 2024-01-12 02:00    capacity        hot > 85%           5,123

# Manually activate a trigger (for testing)
nebulaio-cli policy triggers fire --policy test-policy --dry-run
```

---

## Policy Actions

Actions define what happens when policy triggers activate. NebulaIO supports multiple action types that can be chained together.

### Transition Actions

Move objects between storage tiers.

```yaml
tiering:
  policies:
    - name: tiered-transitions
      actions:
        # Simple transition
        - type: transition
          target_tier: warm

        # Transition with options
        - type: transition
          target_tier: cold
          options:
            preserve_metadata: true
            update_storage_class: true
            verify_integrity: true

        # Conditional transition
        - type: transition
          target_tier: archive
          conditions:
            min_object_size: 1MB
            exclude_patterns:
              - "*.index"
              - "*.manifest"
```

### Delete Actions

Remove objects based on policy conditions.

```yaml
tiering:
  policies:
    - name: cleanup-policy
      actions:
        # Standard deletion
        - type: delete

        # Soft delete (move to trash)
        - type: delete
          mode: soft
          retention_days: 30           # Keep in trash for 30 days

        # Conditional deletion
        - type: delete
          conditions:
            require_all_versions: true  # Delete only if no versions remain
            exclude_legal_hold: true    # Skip objects with legal hold

        # Delete with notification
        - type: delete
          notify_before: true
          notify_webhook: "https://ops.example.com/webhooks/deletions"
          delay_hours: 24              # Wait 24h after notification
```

### Replicate Actions

Copy objects to other tiers, regions, or clusters.

```yaml
tiering:
  policies:
    - name: replication-policy
      actions:
        # Cross-tier replication
        - type: replicate
          destination:
            tier: warm
          options:
            mode: async

        # Cross-region replication
        - type: replicate
          destination:
            region: us-west-2
            bucket: backup-bucket
          options:
            mode: sync
            encryption: AES256

        # Multi-destination replication
        - type: replicate
          destinations:
            - tier: cold
              priority: 1
            - region: eu-west-1
              bucket: dr-bucket
              priority: 2
          options:
            mode: async
            retry_count: 3
            retry_delay: 5m
```

### Notify Actions

Send notifications via webhooks or messaging systems.

```yaml
tiering:
  policies:
    - name: notification-policy
      actions:
        # Webhook notification
        - type: notify
          webhook:
            url: "https://ops.example.com/webhooks/tier-events"
            method: POST
            headers:
              Authorization: "Bearer ${WEBHOOK_TOKEN}"
            timeout: 30s
            retry_count: 3

        # Email notification
        - type: notify
          email:
            to:
              - "ops@example.com"
              - "storage-team@example.com"
            subject: "Tier Transition Alert"
            template: tier_transition

        # Slack notification
        - type: notify
          slack:
            webhook_url: "${SLACK_WEBHOOK_URL}"
            channel: "#storage-alerts"
            template: |
              :warning: *Tier Transition*
              Policy: {{ .PolicyName }}
              Objects: {{ .ObjectCount }}
              From: {{ .FromTier }} -> To: {{ .ToTier }}

        # SNS notification (AWS compatible)
        - type: notify
          sns:
            topic_arn: "arn:aws:sns:us-east-1:123456789:tier-events"
            message_template: tier_event_json
```

### Action Chaining

Execute multiple actions in sequence or parallel.

```yaml
tiering:
  policies:
    - name: complex-action-policy
      actions:
        # Sequential actions
        - type: sequence
          actions:
            - type: replicate
              destination:
                region: us-west-2
            - type: transition
              target_tier: cold
            - type: notify
              webhook:
                url: "https://ops.example.com/webhooks/completed"

        # Parallel actions
        - type: parallel
          actions:
            - type: replicate
              destination:
                region: eu-west-1
            - type: replicate
              destination:
                region: ap-northeast-1
          on_failure: continue         # continue | abort
```

### Action CLI Commands

```bash
# List actions for a policy
nebulaio-cli policy actions list --policy nightly-archival

# Preview action impact
nebulaio-cli policy actions preview --policy cleanup-policy --bucket my-bucket
# ACTION      OBJECTS     SIZE        ESTIMATED TIME
# delete      12,456      45.2 GB     ~15 minutes

# Execute action manually
nebulaio-cli policy actions execute --policy manual-archive \
  --bucket my-bucket --prefix "logs/2023/" --dry-run

# View action history
nebulaio-cli policy actions history --policy nightly-archival --days 30
# TIMESTAMP           ACTION      STATUS      OBJECTS     SIZE
# 2024-01-15 02:15    transition  completed   8,234       123.4 GB
# 2024-01-14 02:12    transition  completed   7,891       118.2 GB
```

---

## Scheduling and Windows

NebulaIO provides flexible scheduling options including maintenance windows and blackout periods to control when policies execute.

### Maintenance Windows

Define specific time windows for policy execution to minimize impact on production workloads.

```yaml
tiering:
  scheduling:
    maintenance_windows:
      # Daily maintenance window
      - name: nightly-window
        schedule:
          days: ["monday", "tuesday", "wednesday", "thursday", "friday"]
          start_time: "02:00"
          end_time: "06:00"
          timezone: "America/New_York"
        policies:
          - nightly-archival
          - cleanup-policy

      # Weekend maintenance window
      - name: weekend-window
        schedule:
          days: ["saturday", "sunday"]
          start_time: "00:00"
          end_time: "08:00"
          timezone: "UTC"
        policies:
          - heavy-transitions
          - data-migration

      # Monthly maintenance window
      - name: monthly-archive
        schedule:
          day_of_month: 1
          start_time: "01:00"
          duration: 8h
          timezone: "UTC"
        policies:
          - monthly-compliance-archive
```

### Blackout Windows

Prevent policy execution during critical periods.

```yaml
tiering:
  scheduling:
    blackout_windows:
      # Daily business hours blackout
      - name: business-hours
        schedule:
          days: ["monday", "tuesday", "wednesday", "thursday", "friday"]
          start_time: "08:00"
          end_time: "18:00"
          timezone: "America/New_York"
        applies_to:
          policy_types: [scheduled, threshold]

      # Holiday blackouts
      - name: end-of-year-freeze
        schedule:
          start_date: "2024-12-20"
          end_date: "2025-01-02"
        applies_to:
          all_policies: true
          except:
            - compliance-retention

      # Event-based blackout
      - name: peak-traffic-protection
        schedule:
          trigger:
            type: metric
            metric_name: nebulaio_requests_per_second
            threshold: 10000
            direction: above
          duration: 1h
        applies_to:
          policy_types: [scheduled, threshold]
```

### Cron Expression Reference

NebulaIO supports standard 5-field cron expressions with extensions.

```yaml
# Cron expression format: minute hour day_of_month month day_of_week
# Extended format: second minute hour day_of_month month day_of_week

tiering:
  policies:
    - name: cron-examples
      schedule:
        # Every day at 2 AM
        cron: "0 2 * * *"

        # Every hour
        cron: "0 * * * *"

        # Every 15 minutes
        cron: "*/15 * * * *"

        # Weekdays at 3 AM
        cron: "0 3 * * 1-5"

        # First day of month at midnight
        cron: "0 0 1 * *"

        # Every Sunday at 4 AM
        cron: "0 4 * * 0"

        # Last day of month (special syntax)
        cron: "0 2 L * *"

        # Timezone specification
        timezone: "Europe/London"
```

### Scheduling CLI Commands

```bash
# List all maintenance windows
nebulaio-cli schedule windows list
# NAME              SCHEDULE                    POLICIES
# nightly-window    Mon-Fri 02:00-06:00 EST    nightly-archival, cleanup-policy
# weekend-window    Sat-Sun 00:00-08:00 UTC    heavy-transitions

# List blackout windows
nebulaio-cli schedule blackouts list
# NAME              STATUS    SCHEDULE
# business-hours    active    Mon-Fri 08:00-18:00 EST
# end-of-year       pending   2024-12-20 to 2025-01-02

# Check next policy execution
nebulaio-cli schedule next --policy nightly-archival
# Next execution: 2024-01-16 02:00:00 EST (in 14 hours)

# Override blackout (with confirmation)
nebulaio-cli schedule blackout override --window business-hours --duration 1h --reason "Emergency cleanup"

# View schedule calendar
nebulaio-cli schedule calendar --days 7
# DATE          TIME          POLICY                STATUS
# 2024-01-15    02:00 EST     nightly-archival     scheduled
# 2024-01-15    08:00 EST     -                    blackout start
# 2024-01-15    18:00 EST     -                    blackout end
```

---

## Anti-Thrash Protection

Anti-thrash protection prevents objects from rapidly transitioning between tiers, which can cause performance degradation and increased costs.

### Configuration

```yaml
tiering:
  anti_thrash:
    enabled: true

    # Minimum time object must stay in a tier before transitioning
    min_time_in_tier:
      hot: 1h
      warm: 24h
      cold: 7d
      archive: 30d

    # Cooldown period after any transition
    cooldown:
      default: 6h
      per_tier:
        hot_to_warm: 12h
        warm_to_hot: 1h              # Allow faster promotion
        warm_to_cold: 24h
        cold_to_warm: 12h

    # Maximum transitions per object in time window
    transition_limits:
      max_transitions: 3
      window: 24h

    # Per-object tracking
    tracking:
      enabled: true
      history_retention: 30d
```

### Transition State Machine

```yaml
tiering:
  anti_thrash:
    state_machine:
      # Define valid state transitions
      transitions:
        hot:
          allowed_targets: [warm]
          promotion_from: [warm]

        warm:
          allowed_targets: [hot, cold]
          promotion_from: [cold]

        cold:
          allowed_targets: [warm, archive]
          promotion_from: [archive]

        archive:
          allowed_targets: [cold]
          promotion_from: []

      # Skip intermediate tiers
      skip_tiers:
        enabled: true
        max_skip: 1                   # Can skip at most 1 tier
        conditions:
          - from: hot
            to: cold
            require_days_since_access: 90
```

### Exception Handling

```yaml
tiering:
  anti_thrash:
    exceptions:
      # Allow immediate transitions for specific patterns
      bypass_patterns:
        - bucket: "temp-*"
          reason: "Temporary data doesn't need protection"
        - prefix: "cache/"
          reason: "Cache objects transition freely"

      # Admin override capability
      admin_override:
        enabled: true
        require_reason: true
        audit_log: true

      # Emergency mode
      emergency_mode:
        enabled: false
        activation:
          type: metric
          metric_name: nebulaio_tier_capacity_percent
          tier: hot
          threshold: 95
        behavior:
          bypass_min_time: true
          bypass_cooldown: true
          max_duration: 1h
```

### Anti-Thrash CLI Commands

```bash
# View anti-thrash status
nebulaio-cli tier anti-thrash status
# STATUS              ENABLED
# Min time in tier    enabled (hot: 1h, warm: 24h, cold: 7d)
# Cooldown            enabled (default: 6h)
# Transition limits   3 per 24h

# Check object thrash history
nebulaio-cli tier anti-thrash history --bucket my-bucket --key important.dat
# TIMESTAMP           FROM    TO      STATUS
# 2024-01-15 10:00    warm    hot     allowed
# 2024-01-15 08:00    hot     warm    blocked (cooldown)
# 2024-01-14 22:00    warm    hot     allowed

# List thrash-protected objects
nebulaio-cli tier anti-thrash protected --tier hot
# OBJECT                          CURRENT TIER    BLOCKED UNTIL
# my-bucket/data.bin              hot             2024-01-15 16:00
# logs/app.log                    hot             2024-01-15 14:30

# Override protection (admin only)
nebulaio-cli tier anti-thrash override --bucket my-bucket --key urgent.dat \
  --target-tier cold --reason "Compliance requirement"

# View thrash metrics
nebulaio-cli tier anti-thrash metrics
# METRIC                          VALUE
# Transitions blocked (24h)       1,234
# Objects in cooldown             5,678
# Emergency mode activations      0
```

---

## Rate Limiting

Rate limiting controls the pace of tier transitions to prevent system overload and ensure predictable performance.

### Per-Policy Rate Limits

```yaml
tiering:
  rate_limiting:
    enabled: true

    # Per-policy limits
    per_policy:
      default:
        transitions_per_second: 100
        bytes_per_second: 100MB

      policies:
        nightly-archival:
          transitions_per_second: 500
          bytes_per_second: 500MB
          burst:
            enabled: true
            multiplier: 2
            duration: 5m

        low-priority-cleanup:
          transitions_per_second: 10
          bytes_per_second: 10MB
```

### Global Rate Limits

```yaml
tiering:
  rate_limiting:
    global:
      # Overall system limits
      max_transitions_per_second: 1000
      max_bytes_per_second: 1GB

      # Per-tier limits
      per_tier:
        hot:
          inbound_per_second: 500
          outbound_per_second: 200
        warm:
          inbound_per_second: 300
          outbound_per_second: 300
        cold:
          inbound_per_second: 200
          outbound_per_second: 100
        archive:
          inbound_per_second: 50
          outbound_per_second: 20

      # Concurrent operation limits
      concurrent_transitions:
        total: 100
        per_tier: 30
        per_bucket: 10
```

### Burst Handling

```yaml
tiering:
  rate_limiting:
    burst:
      enabled: true

      # Token bucket configuration
      bucket:
        capacity: 10000               # Maximum burst size
        refill_rate: 100              # Tokens per second

      # Priority-based burst allocation
      priority_allocation:
        high:
          burst_multiplier: 3
          reserve_percent: 50
        medium:
          burst_multiplier: 2
          reserve_percent: 30
        low:
          burst_multiplier: 1
          reserve_percent: 20

      # Adaptive burst based on system load
      adaptive:
        enabled: true
        cpu_threshold: 70             # Reduce burst above 70% CPU
        memory_threshold: 80          # Reduce burst above 80% memory
        iops_threshold: 80            # Reduce burst above 80% IOPS
```

### Queue Management

```yaml
tiering:
  rate_limiting:
    queue:
      enabled: true
      max_queue_size: 100000
      max_queue_age: 24h

      # Priority queuing
      priority_queues:
        - name: critical
          priority: 1
          max_size: 10000
        - name: standard
          priority: 2
          max_size: 50000
        - name: background
          priority: 3
          max_size: 40000

      # Queue overflow handling
      overflow:
        strategy: drop_lowest_priority  # drop_oldest | drop_newest | reject
        notify: true
```

### Rate Limiting CLI Commands

```bash
# View rate limit status
nebulaio-cli tier rate-limit status
# LIMIT TYPE          CURRENT         LIMIT           UTILIZATION
# Global TPS          450/s           1000/s          45%
# Global BPS          234 MB/s        1 GB/s          23%
# Queue size          12,456          100,000         12%

# View per-policy limits
nebulaio-cli tier rate-limit policies
# POLICY              TPS LIMIT       BPS LIMIT       CURRENT TPS
# nightly-archival    500             500 MB/s        0
# cleanup-policy      100             100 MB/s        45

# Adjust rate limits dynamically
nebulaio-cli tier rate-limit set --policy nightly-archival \
  --transitions-per-second 750 --bytes-per-second 750MB

# View queue status
nebulaio-cli tier rate-limit queue
# QUEUE               SIZE            OLDEST          PROCESSING
# critical            234             5m ago          12/s
# standard            8,567           2h ago          85/s
# background          3,655           8h ago          3/s

# Drain specific queue
nebulaio-cli tier rate-limit drain --queue background --target-rate 50

# View rate limit metrics
nebulaio-cli tier rate-limit metrics --interval 1h
# TIMESTAMP           TPS     BPS         THROTTLED    QUEUED
# 2024-01-15 14:00    456     234 MB/s    12           1,234
# 2024-01-15 13:00    678     345 MB/s    45           2,345
```

---

## ML-Based Predictive Tiering

NebulaIO includes an optional machine learning module that predicts access patterns and automatically optimizes tier placement.

### Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        ML Predictive Tiering Pipeline                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌───────────┐ │
│  │ Access Logs  │───►│ Feature      │───►│ ML Models    │───►│ Tier      │ │
│  │ Metrics      │    │ Extraction   │    │ Prediction   │    │ Actions   │ │
│  │ Object Meta  │    │              │    │              │    │           │ │
│  └──────────────┘    └──────────────┘    └──────────────┘    └───────────┘ │
│         │                   │                   │                   │       │
│         │                   ▼                   ▼                   ▼       │
│         │           ┌──────────────┐    ┌──────────────┐    ┌───────────┐ │
│         └──────────►│ Time Series  │    │ Anomaly      │    │ Feedback  │ │
│                     │ Analysis     │    │ Detection    │    │ Loop      │ │
│                     └──────────────┘    └──────────────┘    └───────────┘ │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Configuration

```yaml
tiering:
  ml_prediction:
    enabled: true

    # Model configuration
    model:
      type: ensemble                  # ensemble | lstm | random_forest
      training:
        schedule: "0 4 * * 0"         # Weekly retraining
        min_data_points: 10000
        lookback_days: 90

    # Feature extraction
    features:
      access_patterns:
        enabled: true
        granularity: 1h
        aggregations: [count, bytes, latency_p99]

      temporal:
        enabled: true
        features:
          - hour_of_day
          - day_of_week
          - day_of_month
          - is_weekend
          - is_holiday

      object_metadata:
        enabled: true
        features:
          - size_bucket
          - content_type
          - age_days
          - version_count

    # Prediction settings
    prediction:
      horizon: 7d                     # Predict 7 days ahead
      confidence_threshold: 0.75      # Minimum confidence for actions
      update_frequency: 1h
```

### Access Pattern Prediction

```yaml
tiering:
  ml_prediction:
    access_prediction:
      enabled: true

      # Prediction models
      models:
        # Short-term access prediction
        short_term:
          horizon: 24h
          features:
            - recent_access_count
            - access_velocity
            - time_since_last_access
          threshold:
            promote_if_predicted_accesses: 5

        # Long-term access prediction
        long_term:
          horizon: 7d
          features:
            - weekly_access_pattern
            - seasonal_component
            - trend_component
          threshold:
            demote_if_predicted_accesses: 0

      # Pattern categories
      patterns:
        hot_pattern:
          min_daily_accesses: 10
          variance_threshold: low

        burst_pattern:
          spike_threshold: 5x
          spike_duration: 1h

        declining_pattern:
          trend_direction: negative
          confidence: 0.8
```

### Time Series Analysis

```yaml
tiering:
  ml_prediction:
    time_series:
      enabled: true

      # Analysis methods
      methods:
        # Seasonal decomposition
        decomposition:
          enabled: true
          period: 7d                  # Weekly seasonality
          model: additive

        # Trend analysis
        trend:
          enabled: true
          method: linear_regression
          window: 30d

        # Anomaly detection in patterns
        pattern_anomaly:
          enabled: true
          method: isolation_forest
          contamination: 0.05

      # Forecast models
      forecasting:
        model: prophet                # prophet | arima | lstm
        seasonality:
          - daily
          - weekly
          - monthly
        holidays:
          enabled: true
          region: US
```

### Tier Recommendations

```yaml
tiering:
  ml_prediction:
    recommendations:
      enabled: true

      # Recommendation engine
      engine:
        algorithm: multi_objective    # cost_optimized | performance_optimized | balanced
        objectives:
          - minimize: storage_cost
            weight: 0.6
          - maximize: access_performance
            weight: 0.3
          - minimize: transition_cost
            weight: 0.1

      # Recommendation output
      output:
        batch_size: 10000
        update_frequency: 6h

        # Automatic execution
        auto_execute:
          enabled: false              # Require approval by default
          confidence_threshold: 0.9
          max_daily_transitions: 10000

        # Human review
        review:
          enabled: true
          notification:
            webhook: "https://ops.example.com/webhooks/ml-recommendations"
            email: "ml-review@example.com"
          approval_required_above: 1000  # Objects
```

### Anomaly Detection

```yaml
tiering:
  ml_prediction:
    anomaly_detection:
      enabled: true

      # Detection methods
      methods:
        # Access pattern anomalies
        access_anomaly:
          algorithm: isolation_forest
          features:
            - access_count_deviation
            - access_time_deviation
            - access_source_diversity
          sensitivity: medium

        # Cost anomalies
        cost_anomaly:
          algorithm: prophet
          features:
            - daily_egress_cost
            - storage_cost_trend
          alert_threshold: 2.0        # 2x expected

        # Capacity anomalies
        capacity_anomaly:
          algorithm: holt_winters
          features:
            - tier_fill_rate
            - object_creation_rate
          forecast_horizon: 7d

      # Anomaly actions
      actions:
        on_access_anomaly:
          - type: notify
            urgency: medium
          - type: investigate
            auto_generate_report: true

        on_cost_anomaly:
          - type: notify
            urgency: high
          - type: throttle_transitions
            until_reviewed: true
```

### ML Model Management

```yaml
tiering:
  ml_prediction:
    model_management:
      # Model versioning
      versioning:
        enabled: true
        retention: 10                 # Keep last 10 versions

      # Model evaluation
      evaluation:
        metrics:
          - accuracy
          - precision
          - recall
          - f1_score
        validation_split: 0.2
        cross_validation: 5

      # A/B testing
      ab_testing:
        enabled: true
        traffic_split:
          control: 0.1
          treatment: 0.9
        minimum_samples: 10000

      # Model explainability
      explainability:
        enabled: true
        method: shap                  # shap | lime
        sample_explanations: 100
```

### ML CLI Commands

```bash
# View ML model status
nebulaio-cli tier ml status
# MODEL               VERSION     ACCURACY    LAST TRAINED        STATUS
# access_prediction   v23         0.87        2024-01-14 04:00    active
# anomaly_detection   v15         0.92        2024-01-14 04:00    active
# tier_recommendation v8          0.84        2024-01-14 04:00    active

# Get predictions for objects
nebulaio-cli tier ml predict --bucket my-bucket --prefix "data/"
# OBJECT                      CURRENT    PREDICTED    CONFIDENCE    RECOMMENDATION
# data/analytics.parquet      hot        warm         0.89          transition in 3d
# data/reports.csv            warm       warm         0.95          no change
# data/archive.tar            warm       cold         0.82          transition now

# View recommendations
nebulaio-cli tier ml recommendations
# RECOMMENDATION              OBJECTS     SAVINGS/MO    CONFIDENCE
# hot -> warm                 12,456      $1,234        0.87
# warm -> cold                45,678      $4,567        0.85
# cold -> archive             8,901       $890          0.92

# Apply recommendations
nebulaio-cli tier ml apply --recommendation-id rec-2024-01-15-001 --dry-run
# Would transition 12,456 objects from hot to warm
# Estimated time: 2h 15m
# Estimated savings: $1,234/month

nebulaio-cli tier ml apply --recommendation-id rec-2024-01-15-001 --confirm

# View anomalies
nebulaio-cli tier ml anomalies --days 7
# TIMESTAMP           TYPE              SEVERITY    OBJECT/TIER         DETAILS
# 2024-01-15 10:30    access_spike      medium      my-bucket/data.bin  5x normal
# 2024-01-14 15:00    cost_anomaly      high        hot tier            2.3x expected

# Retrain model manually
nebulaio-cli tier ml train --model access_prediction --force

# Export model metrics
nebulaio-cli tier ml metrics --model access_prediction --format json > metrics.json

# View feature importance
nebulaio-cli tier ml explain --model tier_recommendation
# FEATURE                     IMPORTANCE
# days_since_access           0.34
# access_count_7d             0.28
# object_size                 0.15
# content_type                0.12
# hour_of_day                 0.11
```

---

## Lifecycle Policies

Automate object transitions with lifecycle policies based on age or access patterns.

```yaml
tiering:
  lifecycle_policies:
    - name: standard-lifecycle
      transitions:
        - days: 30
          storage_class: STANDARD_IA
        - days: 90
          storage_class: GLACIER_IR
        - days: 365
          storage_class: DEEP_ARCHIVE
      expiration:
        days: 2555  # 7 years
    - name: ml-datasets
      filters:
        bucket_prefix: ml-
      transitions:
        - days: 7
          storage_class: STANDARD_IA
```

### S3 API Compatible

```bash
aws s3api put-bucket-lifecycle-configuration --bucket my-bucket \
  --lifecycle-configuration '{
    "Rules": [{"ID": "archive-logs", "Filter": {"Prefix": "logs/"},
      "Transitions": [{"Days": 30, "StorageClass": "GLACIER_IR"}]}]
  }' --endpoint-url http://localhost:9000
```

## Manual Tier Transitions

```bash
# Transition object to cold tier
nebulaio-cli admin tier transition --bucket my-bucket --key data.bin --target-tier cold

# Transition by prefix
nebulaio-cli admin tier transition --bucket my-bucket --prefix archived/ --target-tier archive

# Restore from archive
nebulaio-cli admin tier restore --bucket my-bucket --key data.tar.gz --days 7 --tier bulk
```

### S3 API

```bash
# Transition via CopyObject
aws s3 cp s3://bucket/data.bin s3://bucket/data.bin \
  --storage-class GLACIER_IR --endpoint-url http://localhost:9000

# Restore from Glacier
aws s3api restore-object --bucket my-bucket --key data.tar.gz \
  --restore-request '{"Days":7,"GlacierJobParameters":{"Tier":"Bulk"}}' \
  --endpoint-url http://localhost:9000
```

## DRAM Cache Integration

The DRAM cache accelerates hot data access regardless of storage tier. Objects accessed frequently are automatically cached and optionally promoted to warmer tiers.

```yaml
cache:
  enabled: true
  tier_integration:
    cache_on_tier_access: true
    auto_promote_threshold: 10  # accesses/day to promote cold to warm
    ml_prefetch_tiers: [hot, warm]
```

## Cost Optimization Strategies

### Access-Based Tiering

```yaml
tiering:
  access_based:
    enabled: true
    hot_to_warm: {days_since_access: 30, min_accesses_to_stay: 5}
    warm_to_cold: {days_since_access: 60}
    cold_to_archive: {days_since_access: 180}
```

### Intelligent Tiering

```yaml
tiering:
  intelligent:
    enabled: true
    optimization_frequency: daily
    cost_weight: 0.7
    performance_weight: 0.3
```

### Cost Savings

| Scenario | Hot Only | With Tiering | Savings |
|----------|----------|--------------|---------|
| 100TB, 10% active | $2,500/mo | $850/mo | 66% |
| 1PB, 5% active | $25,000/mo | $5,200/mo | 79% |

## Monitoring

### Prometheus Metrics

```
nebulaio_tier_objects_total{tier="hot|warm|cold|archive"}
nebulaio_tier_bytes_total{tier="hot|warm|cold|archive"}
nebulaio_tier_transitions_total{from="hot",to="warm"}
nebulaio_tier_retrievals_total{tier="cold|archive"}
nebulaio_lifecycle_transitions_total{policy="name",status="success|failed"}

# Policy metrics
nebulaio_policy_executions_total{policy="name",status="success|failed|skipped"}
nebulaio_policy_objects_processed{policy="name",action="transition|delete|replicate"}
nebulaio_policy_duration_seconds{policy="name"}
nebulaio_policy_queue_size{policy="name",priority="high|medium|low"}

# Rate limiting metrics
nebulaio_tier_rate_limit_throttled_total{policy="name"}
nebulaio_tier_rate_limit_current{type="tps|bps"}
nebulaio_tier_queue_depth{queue="critical|standard|background"}

# Anti-thrash metrics
nebulaio_tier_thrash_blocked_total{from="tier",to="tier"}
nebulaio_tier_cooldown_objects{tier="hot|warm|cold|archive"}

# ML metrics
nebulaio_ml_prediction_accuracy{model="name"}
nebulaio_ml_recommendations_total{status="applied|rejected|pending"}
nebulaio_ml_anomalies_detected_total{type="access|cost|capacity"}
```

### CLI Monitoring

```bash
nebulaio-cli admin tier stats
# Tier       Objects      Size          Percentage
# hot        1,234,567    45.2 TB       15.2%
# warm       3,456,789    123.4 TB      41.5%
# cold       2,345,678    98.7 TB       33.2%
# archive    567,890      30.1 TB       10.1%

# Policy monitoring
nebulaio-cli policy stats
# POLICY              STATUS    LAST RUN            OBJECTS     NEXT RUN
# nightly-archival    active    2024-01-15 02:15    8,234       2024-01-16 02:00
# cleanup-policy      active    2024-01-15 02:30    1,234       2024-01-16 02:00
# hot-tier-pressure   standby   2024-01-14 18:45    5,678       (on threshold)

# Detailed policy stats
nebulaio-cli policy stats --policy nightly-archival --days 30
# DATE          OBJECTS     SIZE        DURATION    STATUS
# 2024-01-15    8,234       123 GB      12m         success
# 2024-01-14    7,891       118 GB      11m         success
# 2024-01-13    0           0           1s          skipped (blackout)
```

## Best Practices

1. **Define policies early**: Set lifecycle rules before uploading data
2. **Monitor access patterns**: Use metrics to refine transition thresholds
3. **Consider retrieval costs**: Factor in fees when choosing cold/archive
4. **Use intelligent tiering**: Enable for unpredictable workloads
5. **Set minimum retention**: Archive tier should have minimum retention periods
6. **Combine with DRAM cache**: Hot objects benefit from cache acceleration
7. **Enable anti-thrash protection**: Prevent costly transition loops
8. **Use maintenance windows**: Schedule heavy transitions during off-peak hours
9. **Start with conservative ML settings**: Enable auto-execute only after validation
10. **Monitor rate limits**: Ensure transitions complete within expected timeframes

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Objects not transitioning | Check lifecycle policy filters and scanner interval |
| Slow archive retrieval | Use expedited tier (higher cost) or pre-warm objects |
| High transition costs | Increase age thresholds or use size filters |
| Policy not executing | Verify not in blackout window, check schedule and triggers |
| Transitions too slow | Increase rate limits or reduce concurrent policies |
| Thrash protection blocking | Review cooldown settings, use override if necessary |
| ML predictions inaccurate | Check training data quality, extend lookback period |
| Rate limit queues growing | Increase limits or reduce policy frequency |

## Next Steps

- [Configure DRAM Cache](dram-cache.md) - Optimize hot tier performance
- [Set up erasure coding](erasure-coding.md) - Data protection across tiers
