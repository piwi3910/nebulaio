# Hash Distribution Security Model

This document describes the security properties and considerations of NebulaIO's consistent hash-based data distribution system.

## Overview

NebulaIO uses consistent hashing to distribute data shards across storage nodes within placement groups. This approach provides predictable, deterministic placement while supporting dynamic cluster membership changes.

## Cryptographic Hash Function

### SHA-256 Based Hashing

NebulaIO uses SHA-256 for consistent hashing:

```go
func hashKey(key string) uint64 {
    h := sha256.Sum256([]byte(key))
    return binary.BigEndian.Uint64(h[:8])
}
```

**Security Properties:**
- **Collision Resistance**: SHA-256 provides strong collision resistance, making it computationally infeasible to craft keys that map to specific nodes
- **Uniform Distribution**: The cryptographic properties of SHA-256 ensure uniform distribution across the hash space
- **Deterministic**: Same input always produces the same output, enabling consistent placement lookups

### Why SHA-256?

| Property | Benefit |
|----------|---------|
| Preimage Resistance | Attackers cannot reverse-engineer keys to target specific nodes |
| Second Preimage Resistance | Cannot find alternate keys that hash to same location |
| Avalanche Effect | Small key changes produce completely different hash values |
| Wide Adoption | Well-audited, no known practical attacks |

## Consistent Hash Ring

### Virtual Nodes

Each physical node is represented by multiple virtual nodes on the hash ring:

```go
type ConsistentHashPlacement struct {
    virtualNodes int  // Default: 100 virtual nodes per physical node
}
```

**Security Benefits:**
- **Load Distribution**: Prevents hotspots even with adversarial key patterns
- **Graceful Scaling**: Adding/removing nodes affects only a fraction of keys
- **Fault Isolation**: No single point of failure for key ranges

### Shard Placement Algorithm

For each object:
1. Generate unique shard keys: `{objectKey}/shard/{shardIndex}`
2. Hash the shard key using SHA-256
3. Find the appropriate node on the hash ring
4. Balance across nodes (no node receives more than `minUsage + 1` shards)

## Security Considerations

### Data Locality Attacks

**Threat**: An attacker might try to craft object keys that cause all shards to be placed on nodes they control.

**Mitigations:**
1. **SHA-256 Preimage Resistance**: Impossible to predict which keys map to which nodes
2. **Shard Spreading**: Algorithm actively spreads shards across different nodes
3. **Virtual Nodes**: 100 virtual nodes per physical node makes targeting infeasible

### Node ID Predictability

**Threat**: If node IDs are predictable, attackers might try to join the cluster with IDs that capture desirable hash ranges.

**Mitigations:**
1. **Cluster Authentication**: Nodes must authenticate to join (see [mTLS documentation](../features/security-features.md))
2. **Audit Logging**: All node join/leave events are logged for compliance
3. **Admin Approval**: Production clusters should require admin approval for new nodes

### Hash Collision Considerations

**Threat**: Two different keys produce the same hash, causing data corruption or unauthorized access.

**Analysis:**
- SHA-256 produces 256-bit hashes; we use the first 64 bits
- Birthday paradox: ~4 billion objects before 50% collision probability on 64-bit space
- Collision affects placement only, not data integrity (shards are keyed by full object path)

**Mitigations:**
1. **Object Path Uniqueness**: Full object paths (bucket/key/version) are always unique
2. **Shard Indexing**: Each shard includes its index in the key
3. **Metadata Verification**: Object metadata includes checksums for integrity verification

### Denial of Service Through Hash Manipulation

**Threat**: Crafting many keys that hash to the same node to overload it.

**Mitigations:**
1. **Rate Limiting**: API rate limits prevent bulk key creation
2. **Capacity-Aware Placement**: Optional strategy considers node capacity:
   ```go
   NewCapacityAwarePlacement()  // Excludes nodes at capacity
   ```
3. **Load Balancing**: Virtual nodes distribute load even for adversarial patterns
4. **Monitoring**: Prometheus metrics expose per-node shard counts

## Placement Strategies

NebulaIO supports multiple placement strategies with different security characteristics:

### Consistent Hash Placement (Default)

```go
NewConsistentHashPlacement(virtualNodes int)
```

**Best For**: General use, unpredictable key patterns
**Security**: Highest resistance to adversarial key crafting

### Round Robin Placement

```go
NewRoundRobinPlacement()
```

**Best For**: Predictable key sequences, testing
**Security**: Deterministic placement (less suitable for untrusted key sources)

### Capacity-Aware Placement

```go
NewCapacityAwarePlacement()
```

**Best For**: Heterogeneous clusters, preventing node exhaustion
**Security**: Combines consistent hashing with capacity checks

## Erasure Coding Integration

Hash distribution works with Reed-Solomon erasure coding:

```
Object -> Data Shards (K) + Parity Shards (M) -> Hash Distribution -> Nodes
```

**Security Properties:**
1. **Data Durability**: Can reconstruct with any K of K+M shards
2. **Shard Independence**: Each shard goes to a different node (when possible)
3. **No Single Point of Failure**: Compromising `M` nodes doesn't expose data
4. **Byzantine Tolerance**: Parity shards can detect/correct corrupted data

### Configuration Validation

The system validates that placement groups can support the configured redundancy:

```go
// Ensures min_nodes >= data_shards + parity_shards
validateRedundancyVsNodes()
```

## Audit Trail

All placement-related operations are audited:

| Event Type | Description |
|------------|-------------|
| `cluster:PlacementGroup:NodeJoined` | Node added to placement group |
| `cluster:PlacementGroup:NodeLeft` | Node removed from placement group |
| `cluster:PlacementGroup:StatusChanged` | Group health status changed |
| `cluster:PlacementGroup:Created` | New placement group created |
| `cluster:PlacementGroup:Deleted` | Placement group removed |

See [Audit Logging](../features/audit-logging.md) for compliance integration.

## Network Security

### Inter-Node Communication

- **mTLS**: All node-to-node communication uses mutual TLS
- **Certificate Verification**: Nodes verify peer certificates on every connection
- **Gossip Encryption**: Cluster membership protocol is encrypted

### Data in Transit

- Shard transfers between nodes are encrypted
- Client-to-node communication uses TLS
- Internal APIs require authentication

## Recommendations

### Production Deployment

1. **Enable mTLS** between all cluster nodes
2. **Configure audit logging** for compliance requirements
3. **Use capacity-aware placement** in heterogeneous environments
4. **Monitor placement metrics** for anomalies
5. **Require admin approval** for new node additions

### Security Hardening

1. **Network Segmentation**: Place storage nodes on isolated network
2. **Firewall Rules**: Restrict inter-node communication to required ports
3. **Regular Rotation**: Rotate TLS certificates periodically
4. **Access Controls**: Implement least-privilege IAM policies

### Monitoring Checklist

- [ ] Per-node shard count (detect imbalance)
- [ ] Node join/leave rate (detect churn attacks)
- [ ] Hash collision metrics (if applicable)
- [ ] Placement group health status
- [ ] Cross-node data transfer volume

## Related Documentation

- [Clustering & High Availability](clustering.md)
- [Erasure Coding](../features/erasure-coding.md)
- [Security Features](../features/security-features.md)
- [Audit Logging](../features/audit-logging.md)
