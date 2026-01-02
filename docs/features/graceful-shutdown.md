# Graceful Shutdown

NebulaIO's graceful shutdown system ensures orderly termination of all services with data integrity, proper resource cleanup, and comprehensive observability.

## Overview

Graceful shutdown provides:

- **Phased Shutdown Sequence**: Orderly termination in the correct order
- **Configurable Timeouts**: Per-phase and total timeout protection
- **Data Integrity**: Proper flushing of metadata and storage before exit
- **Observability**: Prometheus metrics and structured logging throughout
- **Error Collection**: Non-blocking errors with comprehensive reporting

## Architecture

```text

┌─────────────────────────────────────────────────────────────────────┐
│                     Shutdown Coordinator                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐             │
│  │ Phase 1  │  │ Phase 2  │  │ Phase 3  │  │ Phase 4  │  ...        │
│  │ Draining │──│ Workers  │──│  HTTP    │──│ Cluster  │──────►      │
│  │          │  │          │  │ Servers  │  │          │             │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘             │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                    Timeout Protection                         │   │
│  │  Total: 30s │ Drain: 15s │ HTTP: 10s │ Storage: 5s           │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                    Metrics & Logging                          │   │
│  │  Phase tracking │ Duration │ Errors │ In-flight requests     │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘

```

## Shutdown Phases

The shutdown coordinator executes phases in the following order:

| Phase | Description | Default Timeout |
| ----- | ----------- | --------------- |
| **Draining** | Wait for in-flight requests to complete | 15s |
| **Workers** | Stop background workers (lifecycle manager) | 10s |
| **HTTP Servers** | Shutdown S3, Admin, and Console servers | 10s |
| **Cluster** | Stop cluster discovery and gossip | 10s |
| **Tiering** | Stop tiering service | 5s |
| **Audit** | Flush and stop audit logger | - |
| **Metadata** | Close metadata store (Raft + BadgerDB) | 10s |
| **Storage** | Close storage backends | 5s |

### Phase Details

#### 1. Draining Phase

Waits for in-flight requests to complete before proceeding:

- Tracks number of active requests
- Logs remaining request count
- Proceeds after timeout with warning if requests remain

> **Note**: The InFlightTracker integration is planned for a future release. Currently, the drain phase provides a grace period but does not actively track individual requests.

#### 2. Workers Phase

Stops background workers that may hold resources:

- Lifecycle manager for object expiration
- Background cleanup tasks

#### 3. HTTP Servers Phase

Gracefully shuts down all HTTP servers:

- S3 API server (port 9000)
- Admin API server (port 9001)
- Web Console server (port 9002)

HTTP servers are shutdown **concurrently** for efficiency while respecting the phase timeout.

#### 4. Cluster Phase

Stops cluster-related services:

- Gossip protocol (memberlist)
- Node discovery

#### 5. Tiering Phase

Stops the storage tiering service:

- Completes any in-progress tier migrations
- Flushes pending transitions

#### 6. Audit Phase

Flushes and stops the audit logger:

- Flushes buffered audit events
- Closes file handles

#### 7. Metadata Phase

Closes the metadata store:

- Raft consensus (Dragonboat)
- BadgerDB key-value store
- Flushes WAL and snapshots

#### 8. Storage Phase

Closes storage backends:

- Filesystem backend
- Volume backend
- Erasure coding backend

## Configuration

### YAML Configuration

```yaml

shutdown:
  total_timeout_seconds: 30    # Maximum time for entire shutdown
  drain_timeout_seconds: 15    # Time to wait for in-flight requests
  worker_timeout_seconds: 10   # Time to wait for background workers
  http_timeout_seconds: 10     # Time to wait for HTTP servers
  metadata_timeout_seconds: 10 # Time to wait for metadata store
  storage_timeout_seconds: 5   # Time to wait for storage backends
  force_timeout_seconds: 5     # Time after total timeout to force shutdown

```

### Environment Variables

```bash

# Shutdown timeouts (all in seconds)
NEBULAIO_SHUTDOWN_TOTAL_TIMEOUT_SECONDS=30
NEBULAIO_SHUTDOWN_DRAIN_TIMEOUT_SECONDS=15
NEBULAIO_SHUTDOWN_WORKER_TIMEOUT_SECONDS=10
NEBULAIO_SHUTDOWN_HTTP_TIMEOUT_SECONDS=10
NEBULAIO_SHUTDOWN_METADATA_TIMEOUT_SECONDS=10
NEBULAIO_SHUTDOWN_STORAGE_TIMEOUT_SECONDS=5
NEBULAIO_SHUTDOWN_FORCE_TIMEOUT_SECONDS=5

```

### Docker Compose

```yaml

services:
  nebulaio:
    environment:
      - NEBULAIO_SHUTDOWN_TOTAL_TIMEOUT_SECONDS=30
      - NEBULAIO_SHUTDOWN_DRAIN_TIMEOUT_SECONDS=15
      - NEBULAIO_SHUTDOWN_WORKER_TIMEOUT_SECONDS=10

```

### Kubernetes ConfigMap

```yaml

apiVersion: v1
kind: ConfigMap
metadata:
  name: nebulaio-config
data:
  config.yaml: |
    shutdown:
      total_timeout_seconds: 30
      drain_timeout_seconds: 15
      worker_timeout_seconds: 10
      http_timeout_seconds: 10
      metadata_timeout_seconds: 10
      storage_timeout_seconds: 5
      force_timeout_seconds: 5

```

### Helm Values

```yaml

shutdown:
  enabled: true
  totalTimeoutSeconds: 30
  drainTimeoutSeconds: 15
  workerTimeoutSeconds: 10
  httpTimeoutSeconds: 10
  metadataTimeoutSeconds: 10
  storageTimeoutSeconds: 5
  forceTimeoutSeconds: 5

```

## Timeout Behavior

### Normal Shutdown

When shutdown completes within the total timeout:

1. All phases execute in order
2. Errors are collected but don't stop shutdown
3. Metrics record successful completion
4. Process exits cleanly

### Timeout Handling

When a phase exceeds its timeout:

1. Warning logged with remaining items
2. Phase moves to next phase
3. Error recorded for reporting
4. Shutdown continues

### Force Shutdown

When total timeout + force timeout is exceeded:

1. `PhaseForcedShutdown` state is set
2. Warning logged
3. Process continues cleanup but won't wait

## Prometheus Metrics

The shutdown coordinator exposes the following metrics:

```text

# Shutdown duration histogram
nebulaio_shutdown_duration_seconds

# Current shutdown phase (gauge with labels)
nebulaio_shutdown_phase{phase="draining|workers|http_servers|cluster|..."}

# In-flight requests during drain
nebulaio_shutdown_in_flight_requests

# Workers stopped counter
nebulaio_shutdown_workers_stopped_total

# Shutdown errors counter
nebulaio_shutdown_errors_total

# Shutdown start time (Unix timestamp)
nebulaio_shutdown_start_time_seconds

```

### Grafana Dashboard Query Examples

```promql

# Shutdown duration
histogram_quantile(0.99, rate(nebulaio_shutdown_duration_seconds_bucket[1h]))

# Current phase (latest)
nebulaio_shutdown_phase == 1

# Errors during shutdown
increase(nebulaio_shutdown_errors_total[1h])

```

## Structured Logging

Shutdown produces structured JSON logs:

```json

{
  "level": "info",
  "from_phase": "workers",
  "to_phase": "http_servers",
  "elapsed": "2.5s",
  "message": "Shutdown phase transition"
}

```

### Log Messages

| Level | Message | Description |
| ----- | ------- | ----------- |
| INFO | "Initiating graceful shutdown" | Shutdown started |
| INFO | "Shutdown phase transition" | Moving between phases |
| INFO | "HTTP server shutdown complete" | Server stopped successfully |
| WARN | "Drain timeout, proceeding with shutdown" | Requests still in flight |
| WARN | "Timeout stopping component" | Component didn't stop in time |
| ERROR | "Error shutting down HTTP server" | Server shutdown failed |
| INFO | "Shutdown completed successfully" | Clean shutdown |
| WARN | "Shutdown completed with errors" | Shutdown with issues |

## Signal Handling

NebulaIO responds to the following signals:

| Signal | Behavior |
| ------ | -------- |
| SIGTERM | Initiate graceful shutdown |
| SIGINT | Initiate graceful shutdown |
| SIGQUIT | Initiate graceful shutdown |

### Kubernetes Integration

Kubernetes sends SIGTERM during pod termination. Configure the termination grace period to exceed the total shutdown timeout:

```yaml

spec:
  terminationGracePeriodSeconds: 35  # > total_timeout + force_timeout

```

## Shutdown Hooks

Register custom shutdown hooks for specific phases:

```go

coordinator.RegisterHook(shutdown.PhaseDraining, func(ctx context.Context) error {
    // Custom drain logic
    return nil
})

coordinator.RegisterHook(shutdown.PhaseStorage, func(ctx context.Context) error {
    // Custom storage cleanup
    return nil
})

```

Available hook phases:

- `PhaseDraining`
- `PhaseWorkers`
- `PhaseHTTPServers`
- `PhaseCluster`
- `PhaseTiering`
- `PhaseAudit`
- `PhaseMetadata`
- `PhaseStorage`

## Best Practices

### 1. Configure Appropriate Timeouts

Match timeouts to your workload:

```yaml

# For high-volume workloads with large objects
shutdown:
  total_timeout_seconds: 60
  drain_timeout_seconds: 30
  storage_timeout_seconds: 15

# For low-latency requirements
shutdown:
  total_timeout_seconds: 15
  drain_timeout_seconds: 5
  storage_timeout_seconds: 3

```

### 2. Monitor Shutdown Metrics

Set up alerts for shutdown issues:

```yaml

# Prometheus alert rule
groups:
  - name: nebulaio_shutdown
    rules:
      - alert: ShutdownTooSlow
        expr: nebulaio_shutdown_duration_seconds > 25
        for: 0s
        labels:
          severity: warning
        annotations:
          summary: "NebulaIO shutdown taking too long"

      - alert: ShutdownErrors
        expr: increase(nebulaio_shutdown_errors_total[5m]) > 0
        for: 0s
        labels:
          severity: warning
        annotations:
          summary: "Errors during NebulaIO shutdown"

```

### 3. Use Preemptive Shutdown

For maintenance windows, drain traffic before shutdown:

1. Remove node from load balancer
2. Wait for in-flight requests to complete
3. Send SIGTERM to initiate shutdown

### 4. Configure Kubernetes Probes

Ensure readiness probe fails during shutdown:

```yaml

readinessProbe:
  httpGet:
    path: /health/ready
    port: 9001
  initialDelaySeconds: 5
  periodSeconds: 5

```

## Troubleshooting

### Shutdown Hangs

If shutdown appears to hang:

1. Check logs for current phase
2. Verify timeout configuration
3. Check for blocked I/O operations
4. Review component-specific logs

### Shutdown Errors

If shutdown completes with errors:

1. Check `nebulaio_shutdown_errors_total` metric
2. Review error logs for each phase
3. Verify network connectivity (for cluster components)
4. Check storage backend health

### Data Integrity

If concerned about data integrity after forced shutdown:

1. Check metadata store consistency
2. Verify storage backend state
3. Review audit logs for last operations
4. Run consistency checks after restart

## API Reference

### Coordinator Interface

```go

// Coordinator manages graceful shutdown of all server components.
type Coordinator struct {
    config   Config
    phase    Phase
    errors   []error
    hooks    map[Phase][]ShutdownHook
}

// NewCoordinator creates a new shutdown coordinator.
func NewCoordinator(cfg Config) *Coordinator

// RegisterHook registers a shutdown hook for a specific phase.
func (c *Coordinator) RegisterHook(phase Phase, hook ShutdownHook)

// Shutdown initiates graceful shutdown of all components.
func (c *Coordinator) Shutdown(ctx context.Context, components ShutdownComponents) error

// Phase returns the current shutdown phase.
func (c *Coordinator) Phase() Phase

// IsShuttingDown returns true if shutdown has been initiated.
func (c *Coordinator) IsShuttingDown() bool

// Done returns a channel that is closed when shutdown is complete.
func (c *Coordinator) Done() <-chan struct{}

// Errors returns any errors that occurred during shutdown.
func (c *Coordinator) Errors() []error

```

### Configuration

```go

// Config holds shutdown configuration.
type Config struct {
    TotalTimeout    time.Duration  // Maximum time for entire shutdown
    DrainTimeout    time.Duration  // Time to wait for in-flight requests
    WorkerTimeout   time.Duration  // Time to wait for background workers
    HTTPTimeout     time.Duration  // Time to wait for HTTP servers
    MetadataTimeout time.Duration  // Time to wait for metadata store
    StorageTimeout  time.Duration  // Time to wait for storage backends
    ForceTimeout    time.Duration  // Time after which shutdown is forced
}

```
