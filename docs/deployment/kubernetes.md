# Kubernetes Deployment

This guide covers deploying NebulaIO on Kubernetes using raw manifests with Kustomize overlays.

For Helm-based deployment, see [Helm Chart](helm.md).
For operator-based deployment, see [Operator Deployment](operator.md).

## Prerequisites

- Kubernetes 1.23+
- kubectl configured for your cluster
- A storage class for persistent volumes
- (Optional) Ingress controller for external access
- (Optional) cert-manager for TLS certificates

## Deployment Options

| Deployment Type | Replicas | Use Case |
|-----------------|----------|----------|
| Single Node | 1 | Development, testing |
| HA Cluster | 3 | Small production |
| Production | 5+ | Large-scale production |

---

## Quick Start {#quick-start}

### Single Node Deployment

```bash
# Apply the base configuration
kubectl apply -k https://github.com/piwi3910/nebulaio//deployments/kubernetes/overlays/single-node

# Or from local checkout
cd deployments/kubernetes
kubectl apply -k overlays/single-node
```

### HA Cluster Deployment

```bash
kubectl apply -k https://github.com/piwi3910/nebulaio//deployments/kubernetes/overlays/ha-cluster
```

### Production Deployment

```bash
kubectl apply -k https://github.com/piwi3910/nebulaio//deployments/kubernetes/overlays/production
```

---

## Directory Structure

```
deployments/kubernetes/
├── base/                          # Base manifests
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── configmap.yaml
│   ├── secret.yaml
│   ├── service.yaml
│   ├── statefulset.yaml
│   ├── rbac.yaml
│   └── pdb.yaml
└── overlays/
    ├── single-node/               # Single node overlay
    │   └── kustomization.yaml
    ├── ha-cluster/                # HA cluster overlay
    │   └── kustomization.yaml
    └── production/                # Production overlay
        ├── kustomization.yaml
        ├── ingress.yaml
        ├── servicemonitor.yaml
        └── network-policy.yaml
```

---

## Single Node Deployment {#single-node}

The single-node deployment is ideal for development and testing.

### Deploy

```bash
kubectl apply -k overlays/single-node
```

### What Gets Deployed

- Namespace: `nebulaio`
- StatefulSet with 1 replica
- ClusterIP Services for S3, Admin, and Console
- ConfigMap with basic configuration
- Secret with default credentials
- PersistentVolumeClaim (10Gi)

### Access the Services

```bash
# Port-forward to access locally
kubectl port-forward -n nebulaio svc/nebulaio 9000:9000 9001:9001 9002:9002

# Test S3 API
curl http://localhost:9000

# Access Web Console
open http://localhost:9002
```

---

## HA Cluster Deployment {#ha-cluster}

The HA cluster deployment provides high availability with 3 nodes.

### Deploy

```bash
kubectl apply -k overlays/ha-cluster
```

### Configuration

The HA overlay includes:

- **3 replicas**: Minimum for Raft consensus
- **Pod anti-affinity**: Spreads pods across nodes
- **Increased resources**: 1Gi memory, 500m CPU per pod
- **50Gi storage**: Per-node persistent volume

### Verify Cluster

```bash
# Check pods
kubectl get pods -n nebulaio-ha

# Check cluster status
kubectl exec -n nebulaio-ha nebulaio-0 -- curl -s http://localhost:9001/api/v1/admin/cluster/nodes | jq

# Check leader
kubectl exec -n nebulaio-ha nebulaio-0 -- curl -s http://localhost:9001/api/v1/admin/cluster/leader | jq
```

---

## Production Deployment {#production}

The production overlay includes additional security and monitoring features.

### Prerequisites

1. **Fast storage class**: Create or identify a fast storage class
2. **Storage nodes**: Label nodes for storage workloads
3. **Prometheus Operator**: For monitoring (optional)
4. **Ingress controller**: nginx-ingress recommended

### Prepare Nodes

```bash
# Label storage nodes
kubectl label nodes node1 node2 node3 node4 node5 node-role.kubernetes.io/storage=true
```

### Create Credentials Secret

```bash
kubectl create namespace nebulaio-production

kubectl create secret generic nebulaio-credentials \
  --namespace nebulaio-production \
  --from-literal=root-user=admin \
  --from-literal=root-password=$(openssl rand -base64 32) \
  --from-literal=jwt-secret=$(openssl rand -base64 64)
```

### Customize the Configuration

Edit `overlays/production/kustomization.yaml`:

```yaml
# Adjust storage class and size
patches:
  - patch: |-
      - op: replace
        path: /spec/volumeClaimTemplates/0/spec/storageClassName
        value: your-fast-storage-class
      - op: replace
        path: /spec/volumeClaimTemplates/0/spec/resources/requests/storage
        value: 500Gi
    target:
      kind: StatefulSet
      name: nebulaio
```

### Configure Ingress

Edit `overlays/production/ingress.yaml`:

```yaml
spec:
  rules:
    - host: s3.your-domain.com
      # ...
    - host: admin.your-domain.com
      # ...
    - host: console.your-domain.com
      # ...
  tls:
    - hosts:
        - s3.your-domain.com
        - admin.your-domain.com
        - console.your-domain.com
      secretName: nebulaio-tls
```

### Deploy

```bash
kubectl apply -k overlays/production
```

### Production Configuration Details

| Feature | Configuration |
|---------|---------------|
| Replicas | 5 |
| Storage | 100Gi fast-ssd per node |
| Anti-affinity | Required (hard) |
| Node selector | `node-role.kubernetes.io/storage: "true"` |
| Network policy | Enabled |
| Monitoring | ServiceMonitor + PrometheusRule |

---

## Custom Overlay

Create your own overlay for specific requirements:

```bash
mkdir -p overlays/my-cluster
```

Create `overlays/my-cluster/kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: my-nebulaio

resources:
  - ../../base

replicas:
  - name: nebulaio
    count: 7

images:
  - name: ghcr.io/piwi3910/nebulaio
    newTag: v1.0.0

patches:
  # Custom resources
  - patch: |-
      - op: replace
        path: /spec/template/spec/containers/0/resources/requests/memory
        value: 4Gi
      - op: replace
        path: /spec/template/spec/containers/0/resources/limits/memory
        value: 16Gi
    target:
      kind: StatefulSet
      name: nebulaio

  # Custom storage
  - patch: |-
      - op: replace
        path: /spec/volumeClaimTemplates/0/spec/resources/requests/storage
        value: 1Ti
      - op: replace
        path: /spec/volumeClaimTemplates/0/spec/storageClassName
        value: nvme-fast
    target:
      kind: StatefulSet
      name: nebulaio

  # Expected nodes
  - patch: |-
      - op: replace
        path: /spec/template/spec/containers/0/env/2/value
        value: "7"
    target:
      kind: StatefulSet
      name: nebulaio

configMapGenerator:
  - name: nebulaio-config
    behavior: merge
    literals:
      - NEBULAIO_LOG_LEVEL=debug
```

Deploy:

```bash
kubectl apply -k overlays/my-cluster
```

---

## Separate Management and Storage Planes

For large deployments, separate the management (Raft) and storage planes.

### Management Plane

Create `overlays/management/kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: nebulaio-management

resources:
  - ../../base

namePrefix: mgmt-

replicas:
  - name: nebulaio
    count: 3

patches:
  # Management role
  - patch: |-
      - op: add
        path: /spec/template/spec/containers/0/env/-
        value:
          name: NEBULAIO_CLUSTER_NODE_ROLE
          value: management
    target:
      kind: StatefulSet
      name: nebulaio

  # Smaller storage for metadata only
  - patch: |-
      - op: replace
        path: /spec/volumeClaimTemplates/0/spec/resources/requests/storage
        value: 10Gi
    target:
      kind: StatefulSet
      name: nebulaio

  # Schedule on control plane nodes
  - patch: |-
      - op: add
        path: /spec/template/spec/nodeSelector
        value:
          node-role.kubernetes.io/control-plane: ""
    target:
      kind: StatefulSet
      name: nebulaio
```

### Storage Plane

Create `overlays/storage/kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: nebulaio-storage

resources:
  - ../../base

namePrefix: storage-

replicas:
  - name: nebulaio
    count: 10

patches:
  # Storage role
  - patch: |-
      - op: add
        path: /spec/template/spec/containers/0/env/-
        value:
          name: NEBULAIO_CLUSTER_NODE_ROLE
          value: storage
    target:
      kind: StatefulSet
      name: nebulaio

  # Large storage volumes
  - patch: |-
      - op: replace
        path: /spec/volumeClaimTemplates/0/spec/resources/requests/storage
        value: 500Gi
      - op: replace
        path: /spec/volumeClaimTemplates/0/spec/storageClassName
        value: fast-ssd
    target:
      kind: StatefulSet
      name: nebulaio

  # Schedule on storage nodes
  - patch: |-
      - op: add
        path: /spec/template/spec/nodeSelector
        value:
          node-role.kubernetes.io/storage: "true"
    target:
      kind: StatefulSet
      name: nebulaio

  # High resources
  - patch: |-
      - op: replace
        path: /spec/template/spec/containers/0/resources/requests/cpu
        value: 2000m
      - op: replace
        path: /spec/template/spec/containers/0/resources/requests/memory
        value: 4Gi
      - op: replace
        path: /spec/template/spec/containers/0/resources/limits/cpu
        value: 8000m
      - op: replace
        path: /spec/template/spec/containers/0/resources/limits/memory
        value: 16Gi
    target:
      kind: StatefulSet
      name: nebulaio
```

### Deploy Both Planes

```bash
kubectl apply -k overlays/management
kubectl apply -k overlays/storage
```

---

## Scaling

### Scale Up

```bash
# Edit the overlay kustomization.yaml and change replicas
# Or use kubectl scale
kubectl scale statefulset nebulaio -n nebulaio --replicas=5

# Wait for new pods
kubectl rollout status statefulset/nebulaio -n nebulaio

# Verify cluster
kubectl exec -n nebulaio nebulaio-0 -- curl -s http://localhost:9001/api/v1/admin/cluster/nodes | jq
```

### Scale Down

```bash
# Scale down carefully - ensure data is replicated
kubectl scale statefulset nebulaio -n nebulaio --replicas=3
```

**Important**: When scaling down, the Raft cluster needs a majority of voters. Never scale below 3 nodes for an HA cluster.

---

## Monitoring

### Prometheus Integration

The production overlay includes a ServiceMonitor:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: nebulaio
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: nebulaio
  endpoints:
    - port: admin
      path: /metrics
      interval: 30s
```

### Key Metrics

| Metric | Description |
|--------|-------------|
| `nebulaio_http_requests_total` | Request counts |
| `nebulaio_http_request_duration_seconds` | Request latency |
| `nebulaio_storage_bytes_total` | Total storage |
| `nebulaio_objects_total` | Object count |
| `nebulaio_raft_state` | Raft cluster state |

### Grafana Dashboard

Import the NebulaIO dashboard from the `monitoring/` directory.

---

## Troubleshooting

### Pod Stuck in Pending

```bash
# Check events
kubectl describe pod -n nebulaio nebulaio-0

# Check PVC
kubectl get pvc -n nebulaio
kubectl describe pvc -n nebulaio data-nebulaio-0
```

Common causes:

- No available storage class
- Insufficient node resources
- Node selector not matching

### Cluster Not Forming

```bash
# Check logs
kubectl logs -n nebulaio nebulaio-0

# Check gossip connectivity
kubectl exec -n nebulaio nebulaio-0 -- nc -zv nebulaio-1.nebulaio-headless 9004

# Check Raft connectivity
kubectl exec -n nebulaio nebulaio-0 -- nc -zv nebulaio-1.nebulaio-headless 9003
```

### Leader Election Issues

```bash
# Check all pod logs
for i in 0 1 2; do
  echo "=== nebulaio-$i ==="
  kubectl logs -n nebulaio nebulaio-$i --tail=50 | grep -i raft
done
```

---

## Next Steps

- [Helm Chart Deployment](helm.md)
- [Operator Deployment](operator.md)
- [Monitoring Setup](../operations/monitoring.md)
- [Backup & Recovery](../operations/backup.md)
