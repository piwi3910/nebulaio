# Tiering Policy System Architecture

This document describes the architecture of NebulaIO's comprehensive tiering policy system, including the policy engine, predictive analytics, and integration points.

## System Overview

```
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                              Tiering Policy System                                   │
├─────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                      │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐      │
│  │   REST API   │    │   S3 API     │    │  Web Console │    │     CLI      │      │
│  │  (Admin)     │    │ (Lifecycle)  │    │              │    │              │      │
│  └──────┬───────┘    └──────┬───────┘    └──────┬───────┘    └──────┬───────┘      │
│         │                   │                   │                   │               │
│         └───────────────────┴───────────────────┴───────────────────┘               │
│                                      │                                              │
│                          ┌───────────▼───────────┐                                  │
│                          │   Advanced Service    │                                  │
│                          │  (Tiering Facade)     │                                  │
│                          └───────────┬───────────┘                                  │
│                                      │                                              │
│         ┌────────────────────────────┼────────────────────────────┐                │
│         │                            │                            │                │
│         ▼                            ▼                            ▼                │
│  ┌──────────────┐         ┌──────────────────┐         ┌──────────────────┐       │
│  │ Policy Store │         │  Policy Engine   │         │ Predictive Engine│       │
│  │              │         │                  │         │                  │       │
│  │ - Hybrid     │◄───────►│ - Realtime       │         │ - ML Prediction  │       │
│  │ - File-based │         │ - Scheduled      │         │ - Time Series    │       │
│  │ - In-memory  │         │ - Threshold      │         │ - Anomaly Detect │       │
│  └──────────────┘         └────────┬─────────┘         └────────┬─────────┘       │
│                                    │                            │                  │
│                    ┌───────────────┼───────────────┐            │                  │
│                    │               │               │            │                  │
│                    ▼               ▼               ▼            ▼                  │
│            ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────┐         │
│            │  Realtime  │  │ Scheduled  │  │ Threshold  │  │  Access    │         │
│            │  Workers   │  │  Workers   │  │  Monitor   │  │  Tracker   │         │
│            └─────┬──────┘  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘         │
│                  │               │               │               │                 │
│                  └───────────────┴───────────────┴───────────────┘                 │
│                                         │                                          │
│                           ┌─────────────▼─────────────┐                            │
│                           │    Policy Executor        │                            │
│                           │                           │                            │
│                           │ - Anti-Thrash Protection  │                            │
│                           │ - Rate Limiting           │                            │
│                           │ - Distributed Execution   │                            │
│                           └─────────────┬─────────────┘                            │
│                                         │                                          │
│                           ┌─────────────▼─────────────┐                            │
│                           │      Tier Manager         │                            │
│                           │                           │                            │
│                           │ - Transition Objects      │                            │
│                           │ - Delete Objects          │                            │
│                           │ - Replicate Objects       │                            │
│                           └─────────────┬─────────────┘                            │
│                                         │                                          │
└─────────────────────────────────────────┼──────────────────────────────────────────┘
                                          │
                    ┌─────────────────────┼─────────────────────┐
                    │                     │                     │
                    ▼                     ▼                     ▼
            ┌──────────────┐      ┌──────────────┐      ┌──────────────┐
            │  Hot Tier    │      │  Warm Tier   │      │  Cold Tier   │
            │  (NVMe/SSD)  │      │  (SSD/HDD)   │      │  (HDD/Tape)  │
            └──────────────┘      └──────────────┘      └──────────────┘
```

## Component Details

### 1. Policy Store

The Policy Store manages policy persistence with a hybrid approach:

```
┌────────────────────────────────────────────────────────────┐
│                     Hybrid Policy Store                     │
├────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────────┐         ┌─────────────────┐           │
│  │  File-Based     │         │   In-Memory     │           │
│  │  Store          │◄───────►│   Cache         │           │
│  │                 │  sync   │                 │           │
│  │ - YAML/JSON     │         │ - Fast lookups  │           │
│  │ - Version ctrl  │         │ - TTL expiry    │           │
│  │ - Persistence   │         │ - Invalidation  │           │
│  └─────────────────┘         └─────────────────┘           │
│                                      │                      │
│                                      ▼                      │
│                          ┌─────────────────┐               │
│                          │  Metadata Store │               │
│                          │                 │               │
│                          │ - Stats         │               │
│                          │ - Last run time │               │
│                          │ - Version info  │               │
│                          └─────────────────┘               │
│                                                             │
└────────────────────────────────────────────────────────────┘
```

**Features:**

- File-based persistence for GitOps workflows
- In-memory caching for performance
- Automatic sync between file and memory
- Version tracking for conflict detection
- Policy validation on load

### 2. Policy Engine

The Policy Engine orchestrates policy evaluation and execution:

```
┌────────────────────────────────────────────────────────────────────────────┐
│                            Policy Engine                                    │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         Event Router                                 │   │
│  │                                                                      │   │
│  │   S3 Events ──┐                                                      │   │
│  │               │     ┌──────────────┐                                 │   │
│  │   Access   ───┼────►│ Event Queue  │────► Realtime Workers           │   │
│  │   Events      │     │ (buffered)   │                                 │   │
│  │               │     └──────────────┘                                 │   │
│  │   Timer    ───┘                                                      │   │
│  │   Events                                                             │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                       Worker Pools                                   │   │
│  │                                                                      │   │
│  │   ┌──────────────────┐    ┌──────────────────┐                      │   │
│  │   │ Realtime Workers │    │ Scheduled Workers│                      │   │
│  │   │                  │    │                  │                      │   │
│  │   │ Worker 1 ────────│    │ Scheduler ───────│──► Cron Jobs         │   │
│  │   │ Worker 2 ────────│    │                  │                      │   │
│  │   │ Worker 3 ────────│    │ Executor ────────│──► Batch Processing  │   │
│  │   │ Worker 4 ────────│    │                  │                      │   │
│  │   └──────────────────┘    └──────────────────┘                      │   │
│  │                                                                      │   │
│  │   ┌──────────────────┐                                              │   │
│  │   │ Threshold Monitor│                                              │   │
│  │   │                  │                                              │   │
│  │   │ Capacity ────────│──► Trigger when threshold exceeded           │   │
│  │   │ Access Rate ─────│──► Trigger on frequency patterns             │   │
│  │   │ Custom Metrics ──│──► Extensible metric support                 │   │
│  │   └──────────────────┘                                              │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└────────────────────────────────────────────────────────────────────────────┘
```

### 3. Policy Types

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          Policy Types                                    │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌────────────────┐   ┌────────────────┐   ┌────────────────┐           │
│  │   Scheduled    │   │   Realtime     │   │   Threshold    │           │
│  │                │   │                │   │                │           │
│  │ Cron-based     │   │ Event-driven   │   │ Metric-based   │           │
│  │ execution      │   │ processing     │   │ triggers       │           │
│  │                │   │                │   │                │           │
│  │ ┌────────────┐ │   │ ┌────────────┐ │   │ ┌────────────┐ │           │
│  │ │Maintenance │ │   │ │ On Upload  │ │   │ │ Capacity   │ │           │
│  │ │Windows     │ │   │ │ On Access  │ │   │ │ > 80%      │ │           │
│  │ └────────────┘ │   │ │ On Delete  │ │   │ └────────────┘ │           │
│  │ ┌────────────┐ │   │ └────────────┘ │   │ ┌────────────┐ │           │
│  │ │ Blackout   │ │   │                │   │ │ Access     │ │           │
│  │ │ Windows    │ │   │                │   │ │ Rate < 1/d │ │           │
│  │ └────────────┘ │   │                │   │ └────────────┘ │           │
│  └────────────────┘   └────────────────┘   └────────────────┘           │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐ │
│  │                      S3 Lifecycle (Compatibility)                   │ │
│  │                                                                     │ │
│  │   S3 Lifecycle Rules ──► Adapter ──► Native Policies               │ │
│  │                                                                     │ │
│  │   Supports: Transitions, Expiration, AbortIncompleteMultipart      │ │
│  └────────────────────────────────────────────────────────────────────┘ │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 4. Policy Executor

The Policy Executor handles safe and controlled execution:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Policy Executor                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                      Pre-Execution Checks                              │  │
│  │                                                                        │  │
│  │   ┌─────────────┐     ┌─────────────┐     ┌─────────────┐             │  │
│  │   │ Anti-Thrash │     │ Rate Limit  │     │ Schedule    │             │  │
│  │   │ Check       │────►│ Check       │────►│ Window      │             │  │
│  │   │             │     │             │     │ Check       │             │  │
│  │   │ Min 24h in  │     │ Token bucket│     │ Maintenance │             │  │
│  │   │ current tier│     │ algorithm   │     │ / Blackout  │             │  │
│  │   └─────────────┘     └─────────────┘     └─────────────┘             │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                      │                                       │
│                                      ▼                                       │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                      Distributed Execution                             │  │
│  │                                                                        │  │
│  │   ┌─────────────────────────────────────────────────────────────────┐ │  │
│  │   │                    Consistent Hashing                            │ │  │
│  │   │                                                                  │ │  │
│  │   │  Object Key ──► Hash ──► Node Assignment                        │ │  │
│  │   │                                                                  │ │  │
│  │   │  node-1: hash(key) % 3 == 0                                     │ │  │
│  │   │  node-2: hash(key) % 3 == 1                                     │ │  │
│  │   │  node-3: hash(key) % 3 == 2                                     │ │  │
│  │   └─────────────────────────────────────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                      │                                       │
│                                      ▼                                       │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                         Action Execution                               │  │
│  │                                                                        │  │
│  │   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  │  │
│  │   │ Transition  │  │   Delete    │  │  Replicate  │  │   Notify    │  │  │
│  │   │             │  │             │  │             │  │             │  │  │
│  │   │ Move object │  │ Remove from │  │ Copy to     │  │ Webhook     │  │  │
│  │   │ between     │  │ all tiers   │  │ destination │  │ POST        │  │  │
│  │   │ tiers       │  │             │  │ tier/region │  │             │  │  │
│  │   └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘  │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5. Predictive Engine

The Predictive Engine provides ML-based insights:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Predictive Engine                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                        Data Collection                                 │  │
│  │                                                                        │  │
│  │   Access Events ──► Access Tracker ──► Time-Series Data               │  │
│  │                                                                        │  │
│  │   ┌─────────────────────────────────────────────────────────────────┐ │  │
│  │   │ Per-Object Tracking:                                             │ │  │
│  │   │   - Timestamp, Operation Type, Size                              │ │  │
│  │   │   - Rolling window: Last 30 days                                 │ │  │
│  │   │   - Aggregation: Hourly buckets                                  │ │  │
│  │   └─────────────────────────────────────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                      │                                       │
│                                      ▼                                       │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                      Time Series Analysis                              │  │
│  │                                                                        │  │
│  │   ┌─────────────────┐                                                 │  │
│  │   │  Decomposition  │                                                 │  │
│  │   │                 │                                                 │  │
│  │   │  Raw Data ──────┼──► Trend Component                              │  │
│  │   │                 │                                                 │  │
│  │   │                 ├──► Seasonal Component (Daily/Weekly)            │  │
│  │   │                 │                                                 │  │
│  │   │                 └──► Residual (Random Noise)                      │  │
│  │   └─────────────────┘                                                 │  │
│  │                                                                        │  │
│  │   ┌─────────────────┐     ┌─────────────────┐                         │  │
│  │   │ Linear          │     │ Exponential     │                         │  │
│  │   │ Regression      │     │ Smoothing       │                         │  │
│  │   │                 │     │                 │                         │  │
│  │   │ Trend slope     │     │ Short/Long term │                         │  │
│  │   │ and direction   │     │ access rates    │                         │  │
│  │   └─────────────────┘     └─────────────────┘                         │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                      │                                       │
│                                      ▼                                       │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                       Prediction Output                                │  │
│  │                                                                        │  │
│  │   ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐ │  │
│  │   │ Access          │     │ Tier            │     │ Anomaly         │ │  │
│  │   │ Prediction      │     │ Recommendation  │     │ Detection       │ │  │
│  │   │                 │     │                 │     │                 │ │  │
│  │   │ - Short-term    │     │ - Current tier  │     │ - Z-score       │ │  │
│  │   │   rate          │     │ - Suggested     │     │   analysis      │ │  │
│  │   │ - Long-term     │     │   tier          │     │ - Spike/Drop    │ │  │
│  │   │   rate          │     │ - Confidence    │     │   detection     │ │  │
│  │   │ - Trend         │     │ - Est. savings  │     │ - Severity      │ │  │
│  │   │ - Confidence    │     │                 │     │   scoring       │ │  │
│  │   └─────────────────┘     └─────────────────┘     └─────────────────┘ │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6. Anomaly Detection

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Anomaly Detector                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                      Baseline Management                               │  │
│  │                                                                        │  │
│  │   Per-Object Baseline:                                                 │  │
│  │     - Hourly mean access rate                                          │  │
│  │     - Hourly standard deviation                                        │  │
│  │     - Daily mean access rate                                           │  │
│  │     - Daily standard deviation                                         │  │
│  │     - Last updated timestamp                                           │  │
│  │                                                                        │  │
│  │   Minimum data: 168 hourly samples (1 week)                           │  │
│  │   Update frequency: Daily                                              │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                      │                                       │
│                                      ▼                                       │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                      Anomaly Detection                                 │  │
│  │                                                                        │  │
│  │   Current Access Rate                                                  │  │
│  │          │                                                             │  │
│  │          ▼                                                             │  │
│  │   ┌─────────────────────────────────────────────────────────────────┐ │  │
│  │   │ Z-Score = (actual - expected) / (stdDev * sqrt(time_period))    │ │  │
│  │   └─────────────────────────────────────────────────────────────────┘ │  │
│  │          │                                                             │  │
│  │          ▼                                                             │  │
│  │   ┌─────────────┐     ┌─────────────┐     ┌─────────────┐             │  │
│  │   │ |Z| < 3.0   │     │ Z > 3.0     │     │ Z < -3.0    │             │  │
│  │   │             │     │             │     │             │             │  │
│  │   │   Normal    │     │   SPIKE     │     │   DROP      │             │  │
│  │   │  (no alert) │     │  Detected   │     │  Detected   │             │  │
│  │   └─────────────┘     └─────────────┘     └─────────────┘             │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Data Flow

### Policy Evaluation Flow

```
┌─────────┐     ┌──────────┐     ┌──────────┐     ┌──────────┐     ┌──────────┐
│ Object  │────►│ Selector │────►│ Trigger  │────►│ Executor │────►│  Action  │
│ Event   │     │ Match    │     │ Evaluate │     │ Checks   │     │ Execute  │
└─────────┘     └──────────┘     └──────────┘     └──────────┘     └──────────┘
                     │                │                │                │
                     ▼                ▼                ▼                ▼
               - Bucket         - Age check      - Anti-thrash    - Transition
               - Prefix         - Access count   - Rate limit     - Delete
               - Size           - Capacity       - Schedule       - Replicate
               - Tags           - Frequency      - Distributed    - Notify
               - Content type   - Cron match     - Leader check
```

### Prediction Flow

```
┌──────────┐     ┌──────────┐     ┌──────────┐     ┌──────────┐     ┌──────────┐
│  Access  │────►│  Track   │────►│ Aggregate│────►│ Analyze  │────►│ Predict  │
│  Event   │     │  Record  │     │ Hourly   │     │ Patterns │     │  Output  │
└──────────┘     └──────────┘     └──────────┘     └──────────┘     └──────────┘
                                                        │
                                       ┌────────────────┼────────────────┐
                                       ▼                ▼                ▼
                                  ┌─────────┐     ┌──────────┐     ┌──────────┐
                                  │ Trend   │     │ Seasonal │     │ Residual │
                                  │ (slope) │     │ (cycles) │     │ (noise)  │
                                  └─────────┘     └──────────┘     └──────────┘
```

## Configuration

### Policy Configuration Example

```yaml
tiering:
  policies:
    - name: archive-old-logs
      type: scheduled
      enabled: true

      # Object selection
      scope:
        type: bucket
        buckets: ["logs-*"]
        prefixes: ["archived/"]

      # Trigger conditions
      triggers:
        - type: age
          days_since_access: 90

      # Actions to perform
      actions:
        - type: transition
          target_tier: archive

      # Execution schedule
      schedule:
        cron: "0 2 * * *"  # Daily at 2 AM
        maintenance_windows:
          - start: "01:00"
            end: "05:00"
            days: ["saturday", "sunday"]

      # Safety controls
      anti_thrash:
        min_age_hours: 24
        cooldown_hours: 48

      rate_limit:
        transitions_per_hour: 1000

      distributed:
        enabled: true
        leader_only: false
```

### Predictive Engine Configuration

```yaml
tiering:
  predictive:
    enabled: true

    # Data collection
    access_window_days: 30
    min_data_points: 24

    # Analysis
    trend_weight: 0.3
    seasonal_weight: 0.2
    recent_weight: 0.5

    # Tier thresholds (daily access rate)
    tier_thresholds:
      hot: 5.0      # > 5 accesses/day
      warm: 1.0     # 1-5 accesses/day
      cold: 0.1     # 0.1-1 accesses/day
      archive: 0.0  # < 0.1 accesses/day

    # Confidence requirements
    min_confidence: 0.7

    # Anomaly detection
    anomaly:
      threshold_std_devs: 3.0
      min_baseline_hours: 168  # 1 week
```

## Performance Considerations

| Component | Memory Usage | CPU Impact | Storage |
|-----------|-------------|------------|---------|
| Policy Store | ~10MB per 1000 policies | Low | Disk-backed |
| Access Tracker | ~1KB per tracked object | Low | In-memory |
| Predictive Engine | ~500 bytes per model | Medium (analysis) | In-memory |
| Policy Engine | ~100MB worker pool | Medium | - |

## Integration Points

1. **S3 API**: Lifecycle configuration via PutBucketLifecycle
2. **Admin API**: Full policy CRUD, predictions, anomalies
3. **Metrics**: Prometheus metrics for monitoring
4. **Webhooks**: Notification actions for external integrations
5. **CLI**: Management commands for policies and tiers

## Related Documentation

- [Tiering Features](../features/tiering.md)
- [Admin API Reference](../api/admin-api.md)
- [Storage Architecture](storage.md)
