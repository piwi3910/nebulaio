# Placement Groups Operations Runbook

This runbook provides step-by-step procedures for common placement group operations and incident response.

## Table of Contents

- [Common Operations](#common-operations)
- [Incident Response](#incident-response)
- [Maintenance Procedures](#maintenance-procedures)
- [Monitoring and Alerts](#monitoring-and-alerts)

---

## Common Operations

### Adding a Node to a Placement Group

**When**: Scaling capacity or replacing failed nodes.

**Prerequisites**:
- New node is configured and running
- Node has network connectivity to other group members
- Node has sufficient storage capacity

**Procedure**:

1. **Verify node is healthy**:
   ```bash
   curl -s http://NEW_NODE:9001/health | jq .
   ```

2. **Add node via Admin API**:
   ```bash
   curl -X POST "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1/nodes" \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"node_id": "new-node-1"}'
   ```

3. **Verify node joined**:
   ```bash
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1" \
     -H "Authorization: Bearer $TOKEN" | jq '.nodes'
   ```

4. **Monitor rebalancing** (if enabled):
   ```bash
   # Watch rebalance progress
   watch -n 5 "curl -s 'http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1/status'"
   ```

**Expected Outcome**:
- Node appears in group membership
- Group status remains "healthy"
- New objects are distributed to new node

---

### Removing a Node from a Placement Group

**When**: Decommissioning hardware or performing maintenance.

**Prerequisites**:
- Group has more than `min_nodes` members
- Node is accessible (for graceful removal)

**Procedure**:

1. **Check group capacity**:
   ```bash
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1" \
     -H "Authorization: Bearer $TOKEN" | jq '{nodes: .nodes | length, min_nodes: .min_nodes}'
   ```

2. **Drain node (recommended)**:
   ```bash
   # Mark node as draining - stops new object placement
   curl -X PUT "http://NODE:9001/api/v1/admin/node/drain" \
     -H "Authorization: Bearer $TOKEN" \
     -d '{"drain": true}'
   ```

3. **Wait for active operations to complete**:
   ```bash
   # Check for in-flight operations
   curl -s "http://NODE:9001/api/v1/admin/node/operations" \
     -H "Authorization: Bearer $TOKEN" | jq '.active_operations'
   ```

4. **Remove node from group**:
   ```bash
   curl -X DELETE "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1/nodes/old-node-1" \
     -H "Authorization: Bearer $TOKEN"
   ```

5. **Verify removal**:
   ```bash
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1" \
     -H "Authorization: Bearer $TOKEN" | jq '.nodes'
   ```

**Expected Outcome**:
- Node removed from group membership
- Group may transition to "degraded" if near `min_nodes`
- Existing data on removed node is rebuilt on remaining nodes

---

### Creating a New Placement Group

**When**: Expanding to a new datacenter or creating isolation.

**Procedure**:

1. **Define group configuration**:
   ```yaml
   # Add to config.yaml
   storage:
     placement_groups:
       groups:
         - id: pg-dc2
           name: US West Datacenter
           datacenter: dc2
           region: us-west-2
           min_nodes: 14
           max_nodes: 50
   ```

2. **Create via Admin API**:
   ```bash
   curl -X POST "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups" \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{
       "id": "pg-dc2",
       "name": "US West Datacenter",
       "datacenter": "dc2",
       "region": "us-west-2",
       "min_nodes": 14,
       "max_nodes": 50
     }'
   ```

3. **Add initial nodes**:
   ```bash
   for i in {1..14}; do
     curl -X POST "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc2/nodes" \
       -H "Authorization: Bearer $TOKEN" \
       -d "{\"node_id\": \"dc2-node-$i\"}"
   done
   ```

4. **Configure replication target** (optional):
   ```bash
   curl -X PUT "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1/replication" \
     -H "Authorization: Bearer $TOKEN" \
     -d '{"targets": ["pg-dc2"]}'
   ```

---

## Incident Response

### Placement Group Offline

**Alert**: `PlacementGroupOffline`

**Impact**: Data in this placement group may be unavailable.

**Immediate Actions**:

1. **Assess scope**:
   ```bash
   # Check which group is offline
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups" \
     -H "Authorization: Bearer $TOKEN" | jq '.[] | select(.status == "offline")'
   ```

2. **Check node status**:
   ```bash
   # List nodes and their status
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/nodes" \
     -H "Authorization: Bearer $TOKEN" | jq '.nodes[] | {id, status, last_seen}'
   ```

3. **Check network connectivity**:
   ```bash
   # From a healthy node, ping affected nodes
   for node in node1 node2 node3; do
     ping -c 1 $node && echo "$node: OK" || echo "$node: UNREACHABLE"
   done
   ```

4. **Check for common failures**:
   - Power outage in datacenter
   - Network switch failure
   - DNS resolution issues
   - Firewall rule changes

**Recovery Actions**:

1. **Restore network connectivity** (if network issue)

2. **Restart affected nodes** (if node issue):
   ```bash
   systemctl restart nebulaio
   ```

3. **Force rejoin** (if nodes are healthy but not rejoining):
   ```bash
   curl -X POST "http://NODE:9001/api/v1/admin/cluster/rejoin" \
     -H "Authorization: Bearer $TOKEN"
   ```

4. **Add replacement nodes** (if hardware failure):
   ```bash
   # See "Adding a Node" procedure above
   ```

---

### Placement Group Degraded

**Alert**: `PlacementGroupDegraded`

**Impact**: Reduced redundancy, increased risk of data unavailability.

**Assessment**:

1. **Check current status**:
   ```bash
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1" \
     -H "Authorization: Bearer $TOKEN" | jq '{status, nodes: .nodes | length, min_nodes}'
   ```

2. **Identify missing nodes**:
   ```bash
   # Compare expected vs actual
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/nodes?placement_group=pg-dc1" \
     -H "Authorization: Bearer $TOKEN" | jq '.nodes[] | select(.status != "healthy")'
   ```

**Recovery Actions**:

1. **For failed nodes**: See "Node Recovery" section

2. **For capacity issues**: Add nodes to meet `min_nodes`

3. **For planned maintenance**: Expedite maintenance completion

**Escalation**:
- If degraded > 1 hour: Page on-call engineer
- If degraded + additional node failure risk: Initiate DR procedures

---

### High Node Churn

**Alert**: `PlacementGroupHighNodeChurn`

**Symptoms**: Nodes repeatedly joining/leaving the group.

**Investigation**:

1. **Check churn metrics**:
   ```bash
   curl -s "http://ADMIN_NODE:9090/metrics" | grep nebulaio_placement_group_node
   ```

2. **Review recent events**:
   ```bash
   # Check audit log
   tail -1000 /var/log/nebulaio/audit.log | grep PlacementGroup
   ```

3. **Common causes**:
   - Network instability
   - Node health check failures
   - Configuration drift
   - Resource exhaustion

**Resolution**:

1. **Network issues**: Stabilize network, adjust timeout settings
2. **Health check failures**: Review and fix health check criteria
3. **Resource issues**: Add capacity or reduce load

---

## Maintenance Procedures

### Rolling Node Upgrades

**Purpose**: Upgrade nodes without service interruption.

**Procedure**:

1. **Pre-flight checks**:
   ```bash
   # Ensure group is healthy
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1" \
     -H "Authorization: Bearer $TOKEN" | jq '.status'

   # Ensure sufficient headroom
   # nodes > min_nodes + 1
   ```

2. **For each node**:

   a. **Drain node**:
   ```bash
   curl -X PUT "http://NODE:9001/api/v1/admin/node/drain" \
     -H "Authorization: Bearer $TOKEN" -d '{"drain": true}'
   ```

   b. **Wait for drain completion**:
   ```bash
   while true; do
     active=$(curl -s "http://NODE:9001/api/v1/admin/node/operations" | jq '.active_operations')
     [ "$active" -eq 0 ] && break
     sleep 5
   done
   ```

   c. **Perform upgrade**:
   ```bash
   systemctl stop nebulaio
   apt-get update && apt-get install nebulaio
   systemctl start nebulaio
   ```

   d. **Verify node health**:
   ```bash
   curl -s "http://NODE:9001/health" | jq '.status'
   ```

   e. **Undrain node**:
   ```bash
   curl -X PUT "http://NODE:9001/api/v1/admin/node/drain" \
     -H "Authorization: Bearer $TOKEN" -d '{"drain": false}'
   ```

   f. **Wait for stabilization** (2-5 minutes) before next node

3. **Post-upgrade verification**:
   ```bash
   # Verify all nodes on same version
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/nodes" \
     -H "Authorization: Bearer $TOKEN" | jq '.nodes[] | {id, version}'
   ```

---

### Expanding Placement Group Capacity

**When**: Approaching storage limits or min_nodes threshold.

1. **Review current capacity**:
   ```bash
   curl -s "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1" \
     -H "Authorization: Bearer $TOKEN" | \
     jq '{nodes: .nodes | length, min_nodes, max_nodes, storage_used_pct}'
   ```

2. **Plan expansion**:
   - Calculate needed nodes based on growth rate
   - Ensure hardware is available and configured
   - Schedule during low-traffic period

3. **Add nodes** (see "Adding a Node" procedure)

4. **Update max_nodes if needed**:
   ```bash
   curl -X PATCH "http://ADMIN_NODE:9001/api/v1/admin/cluster/placement-groups/pg-dc1" \
     -H "Authorization: Bearer $TOKEN" \
     -d '{"max_nodes": 75}'
   ```

---

## Monitoring and Alerts

### Key Metrics to Monitor

| Metric | Warning | Critical |
|--------|---------|----------|
| `nebulaio_placement_group_nodes` | < min_nodes * 1.2 | < min_nodes |
| `nebulaio_placement_group_status` | = 2 (degraded) | = 3 (offline) |
| Node churn (joins + leaves / hour) | > 5 | > 10 |
| Storage utilization | > 70% | > 85% |

### Dashboard Queries (Prometheus)

```promql
# Placement group health overview
nebulaio_placement_group_status

# Nodes per group
nebulaio_placement_group_nodes

# Node churn rate
increase(nebulaio_placement_group_node_joined_total[1h])
  + increase(nebulaio_placement_group_node_left_total[1h])

# Capacity headroom
nebulaio_placement_group_nodes / nebulaio_placement_group_min_nodes
```

### Alert Configuration

See [Prometheus Alert Rules](/deployments/prometheus/placement-group-alerts.yaml) for complete alert definitions.

---

## Related Documentation

- [Architecture: Placement Groups](../architecture/placement-groups.md)
- [Troubleshooting Guide](troubleshooting.md)
- [Scaling Guide](scaling.md)
