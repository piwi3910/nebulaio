# Scaling Guide

This guide covers scaling NebulaIO clusters to meet growing capacity and performance demands.

## Table of Contents

1. [Scaling Overview](#scaling-overview)
2. [Adding Nodes](#adding-nodes)
3. [Removing Nodes](#removing-nodes)
4. [Rebalancing Data](#rebalancing-data)
5. [Capacity Planning](#capacity-planning)
6. [Performance Scaling](#performance-scaling)
7. [Kubernetes Autoscaling](#kubernetes-autoscaling)

---

## Scaling Overview

NebulaIO supports both horizontal and vertical scaling strategies.

### Horizontal Scaling

Adding more nodes to distribute load and increase capacity.

**Benefits**:
- Linear capacity growth
- Improved fault tolerance
- Better geographic distribution
- No downtime required

**Considerations**:
- Network overhead increases with node count
- Raft voter count should stay odd (3, 5, 7)
- Rebalancing required after adding nodes

### Vertical Scaling

Upgrading existing nodes with more resources.

**Benefits**:
- Simpler architecture
- Lower network overhead
- No rebalancing needed

**Considerations**:
- Hardware limits apply
- Requires node restart
- Single points of higher capacity

### Recommended Approach

| Scenario | Recommendation |
|----------|----------------|
| Capacity growth | Horizontal (add storage nodes) |
| Performance boost | Vertical (upgrade CPU/RAM) then horizontal |
| High availability | Horizontal (minimum 3 voters) |
| Geographic expansion | Horizontal (multi-region) |

---

## Adding Nodes

### Prerequisites

Before adding nodes:
- Ensure network connectivity to existing cluster
- Verify storage and compute resources
- Plan node role (management, storage, or hybrid)

### Adding a Storage Node

Storage nodes handle object data without participating in Raft voting.

**Step 1**: Configure the new node

```yaml
# config.yaml on new node
node_id: storage-node-4
cluster:
  bootstrap: false
  shard_id: 1
  replica_id: 4  # Must be unique within shard (uint64)
  node_role: storage
  voter: false
  raft_address: 10.0.1.20:9003
  join_addresses:
    - 10.0.1.10:9004
    - 10.0.1.11:9004
    - 10.0.1.12:9004
  advertise_address: 10.0.1.20
  wal_dir: /var/lib/nebulaio/wal
```

**Important**: The `replica_id` must be unique across all nodes in the shard. Dragonboat uses this to identify each node in the Raft group.

**Step 2**: Start the new node

```bash
nebulaio server --config /etc/nebulaio/config.yaml
```

**Step 3**: Verify the node joined

```bash
curl http://localhost:9001/api/v1/admin/cluster/nodes | jq
```

### Adding a Voter Node

Voter nodes participate in Raft consensus. Only add voters in odd increments.

**Step 1**: Configure as non-voter initially

```yaml
# config.yaml
node_id: mgmt-node-4
cluster:
  bootstrap: false
  shard_id: 1
  replica_id: 5  # Must be unique within shard (uint64)
  node_role: management
  voter: false
  raft_address: 10.0.1.14:9003
  join_addresses:
    - 10.0.1.10:9004
  advertise_address: 10.0.1.14
  wal_dir: /var/lib/nebulaio/wal
```

**Note**: New nodes initially join as non-voters via `SyncRequestAddNonVoting`. This allows them to catch up with the log before participating in elections.

**Step 2**: Start and let it sync

```bash
nebulaio server --config /etc/nebulaio/config.yaml
```

**Step 3**: Promote to voter

```bash
curl -X POST http://leader:9001/api/v1/admin/cluster/nodes/mgmt-node-4/promote
```

**Step 4**: Verify voter status

```bash
curl http://localhost:9001/api/v1/admin/cluster/nodes | jq '.[] | {id, voter}'
```

### Adding Nodes in Kubernetes

```bash
# Scale the StatefulSet
kubectl scale statefulset nebulaio -n nebulaio --replicas=5

# Wait for pods to be ready
kubectl rollout status statefulset/nebulaio -n nebulaio

# Verify cluster membership
kubectl exec -n nebulaio nebulaio-0 -- \
  curl -s http://localhost:9001/api/v1/admin/cluster/nodes | jq
```

---

## Removing Nodes

### Graceful Node Removal

**Step 1**: Demote from voter (if applicable)

```bash
curl -X POST http://leader:9001/api/v1/admin/cluster/nodes/{node-id}/demote
```

**Step 2**: Wait for data migration and Dragonboat synchronization

```bash
# Check rebalance progress
curl http://leader:9001/api/v1/admin/cluster/rebalance/status

# Verify Dragonboat shard membership is updated
curl http://leader:9001/api/v1/admin/cluster/dragonboat/status

# Check shard membership (shows all replicas and their addresses)
curl http://leader:9001/api/v1/admin/cluster/membership
```

**Note**: Dragonboat's `SyncGetShardMembership` returns the current membership configuration, including all voting and non-voting replicas.

**Step 3**: Remove from cluster

```bash
curl -X DELETE http://leader:9001/api/v1/admin/cluster/nodes/{node-id}
```

**Step 4**: Stop and decommission the node

```bash
systemctl stop nebulaio
```

### Emergency Node Removal

For failed nodes that cannot be gracefully removed:

```bash
# Force remove (use with caution)
curl -X DELETE "http://leader:9001/api/v1/admin/cluster/nodes/{node-id}?force=true"
```

### Removing Nodes in Kubernetes

```bash
# Scale down (removes highest-numbered pods first)
kubectl scale statefulset nebulaio -n nebulaio --replicas=3

# Delete associated PVCs if needed
kubectl delete pvc data-nebulaio-4 data-nebulaio-3 -n nebulaio
```

### Quorum Considerations

| Current Voters | Can Remove | Minimum Remaining |
|----------------|------------|-------------------|
| 3 | 1 | 2 (degraded) |
| 5 | 2 | 3 |
| 7 | 3 | 4 |

Never remove nodes below quorum. For a 3-node cluster, removing any voter degrades the cluster.

---

## Rebalancing Data

### Automatic Rebalancing

NebulaIO automatically rebalances data when nodes join or leave.

```yaml
# config.yaml
rebalancing:
  enabled: true
  mode: automatic

  # Bandwidth limit (bytes/sec per node)
  bandwidth_limit: 100MB

  # Concurrent transfers per node
  concurrent_transfers: 4

  # Schedule during low-traffic periods
  schedule:
    enabled: true
    start_time: "02:00"
    end_time: "06:00"
```

### Manual Rebalancing

Trigger immediate rebalancing:

```bash
# Start rebalance
curl -X POST http://leader:9001/api/v1/admin/cluster/rebalance/start

# Check status
curl http://leader:9001/api/v1/admin/cluster/rebalance/status

# Cancel if needed
curl -X POST http://leader:9001/api/v1/admin/cluster/rebalance/cancel
```

### Rebalance Status Response

```json
{
  "status": "in_progress",
  "started_at": "2024-01-15T02:00:00Z",
  "progress": {
    "total_objects": 1500000,
    "transferred": 750000,
    "remaining": 750000,
    "percentage": 50.0
  },
  "bandwidth": {
    "current": "95MB/s",
    "limit": "100MB/s"
  },
  "estimated_completion": "2024-01-15T04:30:00Z"
}
```

### Monitoring Rebalance

Key metrics during rebalancing:

| Metric | Description |
|--------|-------------|
| `nebulaio_rebalance_objects_total` | Total objects to transfer |
| `nebulaio_rebalance_objects_transferred` | Objects transferred |
| `nebulaio_rebalance_bytes_transferred` | Bytes transferred |
| `nebulaio_rebalance_duration_seconds` | Time elapsed |

---

## Capacity Planning

### Storage Capacity

Calculate required storage:

```
Total Storage = Raw Data * Replication Factor * Overhead Factor

Example:
- Raw Data: 100 TB
- Replication Factor: 3
- Overhead (metadata, erasure coding): 1.1
- Total: 100 * 3 * 1.1 = 330 TB
```

### Node Sizing Guidelines

| Workload | Nodes | CPU/Node | RAM/Node | Storage/Node |
|----------|-------|----------|----------|--------------|
| Small (< 10TB) | 3 | 8 cores | 32 GB | 4 TB |
| Medium (10-100TB) | 5 | 16 cores | 64 GB | 20 TB |
| Large (100TB-1PB) | 10+ | 32 cores | 128 GB | 100 TB |
| Enterprise (> 1PB) | 50+ | 64 cores | 256 GB | 200 TB |

### Growth Planning

```yaml
# capacity-plan.yaml
current:
  total_storage: 50TB
  used_storage: 35TB
  utilization: 70%
  nodes: 5

projections:
  monthly_growth_rate: 10%

  6_months:
    projected_usage: 62TB
    action: none

  12_months:
    projected_usage: 110TB
    action: add_nodes
    nodes_to_add: 3

  24_months:
    projected_usage: 195TB
    action: major_expansion
    nodes_to_add: 8
```

### Capacity Alerts

Configure alerts before reaching capacity:

```yaml
# prometheus-rules.yaml
groups:
  - name: nebulaio-capacity
    rules:
      - alert: StorageCapacityWarning
        expr: nebulaio_storage_used_bytes / nebulaio_storage_total_bytes > 0.75
        for: 1h
        labels:
          severity: warning

      - alert: StorageCapacityCritical
        expr: nebulaio_storage_used_bytes / nebulaio_storage_total_bytes > 0.90
        for: 15m
        labels:
          severity: critical
```

---

## Performance Scaling

### Scaling for Throughput

To increase read/write throughput:

1. **Add storage nodes** - Distributes I/O load
2. **Enable read replicas** - Serves reads from followers
3. **Use faster storage** - NVMe SSDs for hot data
4. **Enable caching** - DRAM cache for frequent access

```yaml
# High-throughput configuration
performance:
  read_replicas:
    enabled: true
    max_staleness: 100ms

  caching:
    enabled: true
    size: 64GB

  storage:
    tier_hot: nvme
    tier_warm: ssd
    tier_cold: hdd
```

### Scaling for IOPS

To increase operations per second:

1. **Add more CPU cores** - More concurrent request handling
2. **Increase memory** - Larger metadata cache
3. **Optimize network** - Enable RDMA where available
4. **Use connection pooling** - Reduce connection overhead

```yaml
# High-IOPS configuration
performance:
  workers: 64

  connection_pool:
    size: 10000
    idle_timeout: 60s

  metadata_cache:
    size: 32GB
    preload: true
```

### Scaling for Latency

To reduce request latency:

1. **Co-locate clients and servers** - Minimize network hops
2. **Enable S3 Express** - Single-digit millisecond access
3. **Use regional deployments** - Data locality
4. **Tune TCP settings** - Enable TCP Fast Open

---

## Kubernetes Autoscaling

### Horizontal Pod Autoscaler (HPA)

Scale based on CPU or custom metrics:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: nebulaio-hpa
  namespace: nebulaio
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: nebulaio
  minReplicas: 3
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Pods
      pods:
        metric:
          name: nebulaio_active_connections
        target:
          type: AverageValue
          averageValue: 1000
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 300
      policies:
        - type: Pods
          value: 1
          periodSeconds: 300
    scaleDown:
      stabilizationWindowSeconds: 600
      policies:
        - type: Pods
          value: 1
          periodSeconds: 600
```

### Vertical Pod Autoscaler (VPA)

Automatically adjust resource requests:

```yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: nebulaio-vpa
  namespace: nebulaio
spec:
  targetRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: nebulaio
  updatePolicy:
    updateMode: Auto
  resourcePolicy:
    containerPolicies:
      - containerName: nebulaio
        minAllowed:
          cpu: 500m
          memory: 1Gi
        maxAllowed:
          cpu: 8
          memory: 32Gi
        controlledResources:
          - cpu
          - memory
```

### Custom Metrics for Scaling

Deploy the Prometheus Adapter for custom metrics:

```yaml
# prometheus-adapter-config.yaml
rules:
  - seriesQuery: 'nebulaio_s3_requests_total{namespace!="",pod!=""}'
    resources:
      overrides:
        namespace: {resource: "namespace"}
        pod: {resource: "pod"}
    name:
      matches: "^(.*)_total$"
      as: "${1}_per_second"
    metricsQuery: 'rate(<<.Series>>{<<.LabelMatchers>>}[2m])'

  - seriesQuery: 'nebulaio_active_connections{namespace!="",pod!=""}'
    resources:
      overrides:
        namespace: {resource: "namespace"}
        pod: {resource: "pod"}
    name:
      matches: "^(.*)$"
      as: "$1"
    metricsQuery: '<<.Series>>{<<.LabelMatchers>>}'
```

### KEDA Integration

For event-driven autoscaling:

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: nebulaio-scaler
  namespace: nebulaio
spec:
  scaleTargetRef:
    name: nebulaio
  minReplicaCount: 3
  maxReplicaCount: 20
  triggers:
    - type: prometheus
      metadata:
        serverAddress: http://prometheus:9090
        metricName: nebulaio_queue_depth
        query: sum(nebulaio_request_queue_depth)
        threshold: "100"
```

---

## Best Practices

### Scaling Checklist

- [ ] Monitor current utilization before scaling
- [ ] Plan scaling during low-traffic windows
- [ ] Test scaling procedures in staging first
- [ ] Maintain odd number of Raft voters
- [ ] Allow rebalancing to complete before next change
- [ ] Update capacity alerts after scaling
- [ ] Document cluster topology changes

### Common Pitfalls

1. **Scaling too quickly** - Allow rebalancing between additions
2. **Ignoring quorum** - Never go below minimum voters
3. **Uneven distribution** - Use anti-affinity rules
4. **Network saturation** - Limit rebalancing bandwidth
5. **Forgetting PVCs** - Clean up after scale-down

---

## Next Steps

- [Clustering & High Availability](../architecture/clustering.md)
- [Performance Tuning](../PERFORMANCE_TUNING.md)
- [Kubernetes Deployment](../deployment/kubernetes.md)
- [Monitoring Setup](monitoring.md)
