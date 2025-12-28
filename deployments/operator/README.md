# NebulaIO Kubernetes Operator

The NebulaIO Operator automates the deployment and management of NebulaIO clusters on Kubernetes.

## Features

- **Declarative Configuration**: Define NebulaIO clusters using Custom Resources
- **Automated Scaling**: Scale clusters up or down automatically
- **Rolling Updates**: Zero-downtime upgrades with rolling update strategy
- **Backup Management**: Schedule and manage backups with NebulaIOBackup CR
- **Health Monitoring**: Automatic health checks and self-healing
- **Prometheus Integration**: Built-in ServiceMonitor and PrometheusRule support

## Prerequisites

- Kubernetes 1.23+
- kubectl configured for your cluster
- (Optional) cert-manager for TLS certificates
- (Optional) Prometheus Operator for monitoring

## Installation

### Install CRDs

```bash
kubectl apply -k config/crd/bases/
```

### Install the Operator

```bash
kubectl apply -k config/default/
```

### Verify Installation

```bash
kubectl get pods -n nebulaio-system
kubectl get crd | grep nebulaio
```

## Usage

### Create a Credentials Secret

```bash
kubectl create namespace nebulaio

kubectl create secret generic nebulaio-credentials \
  --namespace nebulaio \
  --from-literal=password=your-secure-password
```

### Deploy a Single-Node Instance

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: nebulaio-single
  namespace: nebulaio
spec:
  replicas: 1
  auth:
    rootUser: admin
    rootPasswordSecretRef:
      name: nebulaio-credentials
      key: password
  storage:
    size: 50Gi
```

Apply with:

```bash
kubectl apply -f config/samples/nebulaio_v1alpha1_nebulaio.yaml
```

### Deploy an HA Cluster

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: nebulaio-ha
  namespace: nebulaio
spec:
  replicas: 3
  cluster:
    name: nebulaio-ha
    nodeRole: storage
  auth:
    rootUser: admin
    rootPasswordSecretRef:
      name: nebulaio-credentials
      key: password
  storage:
    storageClassName: fast-ssd
    size: 100Gi
  monitoring:
    enabled: true
    serviceMonitor: true
  podDisruptionBudget:
    enabled: true
    minAvailable: 2
```

### Schedule Backups

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIOBackup
metadata:
  name: nebulaio-backup-daily
  namespace: nebulaio
spec:
  clusterRef:
    name: nebulaio-ha
  schedule: "0 2 * * *"  # Daily at 2 AM
  destination:
    type: s3
    s3:
      bucket: my-backup-bucket
      prefix: nebulaio/
      credentialsSecretRef:
        name: backup-s3-credentials
  retention:
    keepLast: 7
```

## Custom Resource Reference

### NebulaIO

| Field | Description | Default |
|-------|-------------|---------|
| `spec.replicas` | Number of nodes | 1 |
| `spec.image.repository` | Image repository | ghcr.io/piwi3910/nebulaio |
| `spec.image.tag` | Image tag | latest |
| `spec.cluster.name` | Cluster name | nebulaio-cluster |
| `spec.cluster.nodeRole` | Node role: storage, management, hybrid | storage |
| `spec.auth.rootUser` | Admin username | admin |
| `spec.auth.rootPasswordSecretRef` | Secret reference for password | - |
| `spec.storage.size` | PVC size per node | 10Gi |
| `spec.storage.storageClassName` | Storage class | (default) |
| `spec.resources` | CPU/memory requests and limits | - |
| `spec.monitoring.serviceMonitor` | Create ServiceMonitor | false |
| `spec.podDisruptionBudget.enabled` | Enable PDB | true |

### NebulaIOBackup

| Field | Description | Default |
|-------|-------------|---------|
| `spec.clusterRef.name` | Target cluster name | - |
| `spec.schedule` | Cron schedule (empty for one-time) | - |
| `spec.destination.type` | Backup type: s3, pvc | - |
| `spec.destination.s3.*` | S3 destination config | - |
| `spec.destination.pvc.*` | PVC destination config | - |
| `spec.retention.keepLast` | Backups to retain | 5 |
| `spec.includeBuckets` | Buckets to include | (all) |

## Status Fields

Check the status of your NebulaIO cluster:

```bash
kubectl get nebulaio nebulaio-ha -n nebulaio -o yaml
```

Status fields:

- `phase`: Current phase (Pending, Creating, Running, Updating, Failed)
- `readyReplicas`: Number of ready pods
- `leader`: Current Raft leader
- `conditions`: Detailed condition status

## Scaling

### Scale Up

```bash
kubectl patch nebulaio nebulaio-ha -n nebulaio --type merge -p '{"spec":{"replicas":5}}'
```

### Scale Down

```bash
kubectl patch nebulaio nebulaio-ha -n nebulaio --type merge -p '{"spec":{"replicas":3}}'
```

## Upgrading

Update the image tag:

```bash
kubectl patch nebulaio nebulaio-ha -n nebulaio --type merge \
  -p '{"spec":{"image":{"tag":"v1.1.0"}}}'
```

The operator will perform a rolling update.

## Separate Management and Storage Planes

For large deployments, deploy separate management and storage clusters:

```yaml
# Management plane (3 voters)
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: nebulaio-management
spec:
  replicas: 3
  cluster:
    nodeRole: management
  storage:
    size: 10Gi  # Small, metadata only
---
# Storage plane (scales independently)
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: nebulaio-storage
spec:
  replicas: 10
  cluster:
    nodeRole: storage
  storage:
    size: 500Gi
```

## Uninstalling

```bash
# Delete NebulaIO instances
kubectl delete nebulaio --all -A

# Delete operator
kubectl delete -k config/default/

# Delete CRDs (this removes all NebulaIO resources!)
kubectl delete -k config/crd/bases/
```

## Troubleshooting

### Check Operator Logs

```bash
kubectl logs -n nebulaio-system deployment/nebulaio-operator
```

### Check NebulaIO Pod Logs

```bash
kubectl logs -n nebulaio -l app.kubernetes.io/name=nebulaio
```

### Check Events

```bash
kubectl get events -n nebulaio --sort-by='.lastTimestamp'
```

## License

Apache License 2.0
